package testsupport

import (
	"cmp"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"math/big"
	"net"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/dakriy/doorcode/internal/connector"
)

// CertificateChecks returns the latest check of every endpoint, by
// address.
func (s *Store) CertificateChecks(context.Context) ([]connector.StoredCertificateCheck, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.usable(certificatesAggregate); err != nil {
		return nil, err
	}
	out := []connector.StoredCertificateCheck{}
	for _, c := range s.certChecks {
		out = append(out, clonedCheck(c))
	}
	slices.SortFunc(out, func(a, b connector.StoredCertificateCheck) int { return cmp.Compare(a.Address, b.Address) })
	return out, nil
}

// PutCertificateCheck stores c over the check of its address. It refuses
// a nil chain, which the database would hold as NULL.
func (s *Store) PutCertificateCheck(_ context.Context, c connector.StoredCertificateCheck) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.writable(certificatesAggregate, certificateWrites); err != nil {
		return err
	}
	if c.Chain == nil {
		return ErrNull
	}
	s.certChecks[c.Address] = clonedCheck(c)
	return nil
}

// clonedCheck is a copy of c sharing nothing with it.
func clonedCheck(c connector.StoredCertificateCheck) connector.StoredCertificateCheck {
	c.Chain = slices.Clone(c.Chain)
	c.Anchor, c.RunwayDays, c.LeafDays = cloned(c.Anchor), cloned(c.RunwayDays), cloned(c.LeafDays)
	return c
}

// FailCertificates makes the certificate check methods report
// ErrUnavailable.
func (s *Store) FailCertificates() {
	s.failing[certificatesAggregate] = true
}

// FailCertificateWrites makes PutCertificateCheck report ErrUnavailable,
// while reading the checks still works.
func (s *Store) FailCertificateWrites() {
	s.failing[certificateWrites] = true
}

// Probe is the stub connector.CertificateProbe: each endpoint serves the
// chain Serve gave it or fails as Fail said, and any other refuses the
// connection, as a port nothing listens on does.
type Probe struct {
	mu       sync.Mutex
	served   map[string][][]byte
	failures map[string]error
	holds    map[string]chan struct{}
	probed   []string
}

// NewProbe returns a probe that no endpoint answers.
func NewProbe() *Probe {
	return &Probe{served: map[string][][]byte{}, failures: map[string]error{}, holds: map[string]chan struct{}{}}
}

// addressOf is host and port as host:port, an IPv6 host in brackets.
func addressOf(host string, port int) string {
	return net.JoinHostPort(host, strconv.Itoa(port))
}

// Served returns what the endpoint was told to serve, or its failure, as
// they stand when it is asked; a held endpoint answers once released.
func (p *Probe) Served(_ context.Context, host string, port int) ([][]byte, error) {
	address := addressOf(host, port)
	served, err := p.answer(address)
	p.waitFor(address)
	return served, err
}

// answer records a probe of address and is what address serves, or its
// failure.
func (p *Probe) answer(address string) ([][]byte, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.probed = append(p.probed, address)
	if err, failing := p.failures[address]; failing {
		return nil, err
	}
	served, serving := p.served[address]
	if !serving {
		return nil, fmt.Errorf("dial tcp %s: connect: connection refused", address)
	}
	return slices.Clone(served), nil
}

// waitFor waits while address is held, ending the hold.
func (p *Probe) waitFor(address string) {
	p.mu.Lock()
	held, holding := p.holds[address]
	delete(p.holds, address)
	p.mu.Unlock()
	if holding {
		<-held
	}
}

// Hold makes the next probe of host at port wait, once it has read what
// the endpoint serves, until the function it returns is called, as a
// server slow to finish the handshake does.
func (p *Probe) Hold(host string, port int) (release func()) {
	p.mu.Lock()
	defer p.mu.Unlock()
	held := make(chan struct{})
	p.holds[addressOf(host, port)] = held
	return func() { close(held) }
}

// Await waits, for at most two seconds, until host at port has been
// probed.
func (p *Probe) Await(host string, port int) {
	address := addressOf(host, port)
	deadline := time.Now().Add(2 * time.Second)
	for !slices.Contains(p.Probed(), address) && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
}

// Serve has host at port serve chain, in order, from then on.
func (p *Probe) Serve(host string, port int, chain ...Cert) {
	var der [][]byte
	for _, c := range chain {
		der = append(der, c.DER)
	}
	p.ServeBytes(host, port, der...)
}

// ServeBytes has host at port serve the certificates der, which need not
// be certificates at all.
func (p *Probe) ServeBytes(host string, port int, der ...[]byte) {
	p.mu.Lock()
	defer p.mu.Unlock()
	address := addressOf(host, port)
	delete(p.failures, address)
	p.served[address] = der
}

// Fail has a connection to host at port fail, saying message.
func (p *Probe) Fail(host string, port int, message string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.failures[addressOf(host, port)] = fmt.Errorf("%s", message)
}

// Probed is the host:port of every probe asked for, oldest first.
func (p *Probe) Probed() []string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return slices.Clone(p.probed)
}

// Cert is a certificate of a test chain: its DER and the certificate it
// reads as. A CA's key signs what it issues; a certificate's key comes
// from its common name alone, so a root and its cross-sign share one.
type Cert struct {
	DER  []byte
	cert *x509.Certificate
	key  *ecdsa.PrivateKey
}

// keyOf is the P-256 key of the certificate named name, the same every
// time: its scalar is the SHA-256 of the name.
func keyOf(name string) *ecdsa.PrivateKey {
	scalar := sha256.Sum256([]byte("doorcode test key " + name))
	key, err := ecdsa.ParseRawPrivateKey(elliptic.P256(), scalar[:])
	if err != nil {
		panic(err)
	}
	return key
}

// day is the start of the date given as 2006-01-02, in UTC.
func day(date string) time.Time {
	t, err := time.Parse(time.DateOnly, date)
	if err != nil {
		panic(err)
	}
	return t
}

// testOrganization is the organization every test certificate names.
const testOrganization = "Example Trust"

// issue is the certificate template describes, signed by issuer's key
// (by its own key when issuer is nil), its key that of name.
func issue(template *x509.Certificate, name string, issuer *Cert) Cert {
	key := keyOf(name)
	parent, signer := template, key
	if issuer != nil {
		parent, signer = issuer.cert, issuer.key
	}
	serial := sha256.Sum256([]byte(name + "|" + parent.Subject.CommonName + "|" + template.NotAfter.String()))
	template.SerialNumber = new(big.Int).SetBytes(serial[:8])
	der, err := x509.CreateCertificate(rand.Reader, template, parent, &key.PublicKey, signer)
	if err != nil {
		panic(err)
	}
	cert, err := x509.ParseCertificate(der)
	if err != nil {
		panic(err)
	}
	return Cert{DER: der, cert: cert, key: key}
}

// caTemplate is a CA certificate named name valid from from to to.
func caTemplate(name, from, to string) *x509.Certificate {
	return &x509.Certificate{
		Subject:   pkix.Name{CommonName: name, Organization: []string{testOrganization}},
		NotBefore: day(from), NotAfter: day(to),
		IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign | x509.KeyUsageCRLSign,
	}
}

// Root is a self-signed root CA named name, valid from from to to, both
// dates as 2006-01-02.
func Root(name, from, to string) Cert {
	return issue(caTemplate(name, from, to), name, nil)
}

// Impostor is a self-signed root CA named name, valid from from to to,
// whose key is not the key of the root Root names so: it names the same
// subject but signs nothing that root signed.
func Impostor(name, from, to string) Cert {
	return issue(caTemplate(name, from, to), "impostor "+name, nil)
}

// Issue is an intermediate CA named name that c issued, valid from from
// to to.
func (c Cert) Issue(name, from, to string) Cert {
	return issue(caTemplate(name, from, to), name, &c)
}

// CrossSign is subject's name and key certified by c, valid from from to
// to: what a server serves so that clients trusting c reach subject.
func (c Cert) CrossSign(subject Cert, from, to string) Cert {
	return c.Issue(subject.cert.Subject.CommonName, from, to)
}

// Leaf is a server certificate for hosts that c issued, valid from from
// to to, named after the first host.
func (c Cert) Leaf(from, to string, hosts ...string) Cert {
	template := &x509.Certificate{
		Subject:   pkix.Name{CommonName: hosts[0]},
		NotBefore: day(from), NotAfter: day(to), DNSNames: hosts,
		KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	return issue(template, "leaf "+strings.Join(hosts, " "), &c)
}

// Subject is the certificate's subject as the application writes it.
func (c Cert) Subject() string {
	return c.cert.Subject.String()
}

// PEMOf is certs as PEM, one block each, in order: a trust list.
func PEMOf(certs ...Cert) string {
	var b strings.Builder
	for _, c := range certs {
		_ = pem.Encode(&b, &pem.Block{Type: "CERTIFICATE", Bytes: c.DER})
	}
	return b.String()
}

// TLSCertificate is what a TLS server serves for chain, leaf first, with
// the leaf's key.
func TLSCertificate(chain ...Cert) tls.Certificate {
	served := tls.Certificate{PrivateKey: chain[0].key, Leaf: chain[0].cert}
	for _, c := range chain {
		served.Certificate = append(served.Certificate, c.DER)
	}
	return served
}

// PKI is a public key infrastructure shaped as Let's Encrypt's is during
// a root change: the roots X1 and X2, X2 cross-signed by X1, the root
// Root YE cross-signed by X2 as well as self-signed, and the
// intermediate YE1 under Root YE. The cross-signs expire 2032-09-02.
type PKI struct {
	X1, X2, X2ByX1, RootYE, RootYEByX2, YE1 Cert
}

// TestPKI is the PKI the tests use, made once.
var TestPKI = sync.OnceValue(func() PKI {
	x1 := Root("Root X1", "2015-06-04", "2035-06-04")
	x2 := Root("Root X2", "2020-09-04", "2040-09-17")
	rootYE := Root("Root YE", "2025-09-03", "2045-09-03")
	return PKI{
		X1: x1, X2: x2, X2ByX1: x1.CrossSign(x2, "2024-03-13", "2032-09-02"),
		RootYE: rootYE, RootYEByX2: x2.CrossSign(rootYE, "2025-09-03", "2032-09-02"),
		YE1: rootYE.Issue("YE1", "2025-09-03", "2028-09-02"),
	}
})
