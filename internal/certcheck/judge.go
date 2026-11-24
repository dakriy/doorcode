package certcheck

import (
	"bytes"
	"crypto/x509"
	"encoding/pem"
	"slices"
	"time"

	"github.com/dakriy/doorcode/internal/connector"
)

// The problems a check finds with what an endpoint served.
const (
	servedNothing  = "the server served no certificate"
	unreadable     = "the server served a certificate that cannot be read: "
	nothingTrusted = "door_broker_ca_pem holds no certificate to verify against"
	noPath         = "no chain from the certificates served reaches a certificate of door_broker_ca_pem"
	wrongName      = "the certificate is not valid for "
)

// TrustList is the certificates of text, PEM blocks such as the setting
// door_broker_ca_pem holds, in order; a block that is not a certificate
// is passed over.
func TrustList(text string) []*x509.Certificate {
	var trust []*x509.Certificate
	block, rest := pem.Decode([]byte(text))
	for block != nil {
		if c, err := x509.ParseCertificate(block.Bytes); err == nil {
			trust = append(trust, c)
		}
		block, rest = pem.Decode(rest)
	}
	return trust
}

// Judge is what a door would make at now of served, the certificates an
// endpoint at host served, leaf first, given trust: it walks from the
// leaf through the certificates served, in any order, to the first one
// a certificate of trust issued, the shortest such path winning, then
// checks the leaf is valid for host and every certificate of the path
// and the anchor is valid at now. The check's address and time are left
// to the caller.
func Judge(host string, served [][]byte, trust []*x509.Certificate, now time.Time) connector.StoredCertificateCheck {
	judged := connector.StoredCertificateCheck{Chain: []connector.StoredCertificate{}}
	if len(served) == 0 {
		judged.Problem = servedNothing
		return judged
	}
	certs := make([]*x509.Certificate, 0, len(served))
	for _, der := range served {
		c, err := x509.ParseCertificate(der)
		if err != nil {
			judged.Problem = unreadable + err.Error()
			return judged
		}
		certs = append(certs, c)
	}
	leafDays := daysLeft(certs[0].NotAfter, now)
	judged.LeafDays = &leafDays
	judged.Chain = described(certs)
	if len(trust) == 0 {
		judged.Problem = nothingTrusted
		return judged
	}
	path, anchor := shortestPath(certs, trust)
	if anchor == nil {
		judged.Problem = noPath
		return judged
	}
	kept := describe(anchor)
	runway := daysLeft(earliestExpiry(path[1:], anchor), now)
	judged.Chain, judged.Anchor, judged.RunwayDays = described(path), &kept, &runway
	judged.Problem = problemOf(host, path, anchor, now)
	judged.Verified = judged.Problem == ""
	return judged
}

// problemOf is what is wrong with path, ending at anchor, at now for
// host: a leaf not valid for host, or a certificate not valid at now.
// It is empty when nothing is.
func problemOf(host string, path []*x509.Certificate, anchor *x509.Certificate, now time.Time) string {
	if err := path[0].VerifyHostname(host); err != nil {
		return wrongName + host + ": " + err.Error()
	}
	for _, c := range append(slices.Clone(path), anchor) {
		if now.Before(c.NotBefore) {
			return c.Subject.String() + " is not valid before " + stamp(c.NotBefore)
		}
		if now.After(c.NotAfter) {
			return c.Subject.String() + " expired " + stamp(c.NotAfter)
		}
	}
	return ""
}

// shortestPath is the path with the fewest certificates from the leaf,
// certs[0], through certs to one that a certificate of trust issued, and
// that certificate, its anchor; nil and nil when no path reaches one.
// Paths of one length are tried in the order certs and trust list them.
func shortestPath(certs, trust []*x509.Certificate) ([]*x509.Certificate, *x509.Certificate) {
	queue := [][]*x509.Certificate{{certs[0]}}
	for len(queue) > 0 {
		path := queue[0]
		queue = queue[1:]
		last := path[len(path)-1]
		if anchor := issuerAmong(last, trust); anchor != nil {
			return path, anchor
		}
		queue = append(queue, extended(path, certs[1:])...)
	}
	return nil, nil
}

// extended is path extended by each of served not on it yet that issued
// its last certificate, one new path each, in the order served lists
// them.
func extended(path, served []*x509.Certificate) [][]*x509.Certificate {
	last := path[len(path)-1]
	var out [][]*x509.Certificate
	for _, c := range served {
		if slices.Contains(path, c) {
			continue
		}
		if issued(c, last) {
			out = append(out, append(slices.Clone(path), c))
		}
	}
	return out
}

// issuerAmong is the first of candidates that issued c, nil when none
// did.
func issuerAmong(c *x509.Certificate, candidates []*x509.Certificate) *x509.Certificate {
	for _, candidate := range candidates {
		if issued(candidate, c) {
			return candidate
		}
	}
	return nil
}

// issued reports whether parent issued child: child names parent's
// subject as its issuer, and parent, a CA allowed to sign certificates,
// signed it.
func issued(parent, child *x509.Certificate) bool {
	return bytes.Equal(child.RawIssuer, parent.RawSubject) && child.CheckSignatureFrom(parent) == nil
}

// earliestExpiry is the earliest NotAfter among certs and anchor.
func earliestExpiry(certs []*x509.Certificate, anchor *x509.Certificate) time.Time {
	earliest := anchor.NotAfter
	for _, c := range certs {
		if c.NotAfter.Before(earliest) {
			earliest = c.NotAfter
		}
	}
	return earliest
}

// daysLeft is the whole days from now until t, negative once t passed.
func daysLeft(t, now time.Time) int {
	return wholeDays(t.Sub(now))
}

// wholeDays is the whole days d lasts.
func wholeDays(d time.Duration) int {
	return int(d / (24 * time.Hour))
}

// described is what a check keeps of each of certs.
func described(certs []*x509.Certificate) []connector.StoredCertificate {
	out := make([]connector.StoredCertificate, 0, len(certs))
	for _, c := range certs {
		out = append(out, describe(c))
	}
	return out
}

// describe is what a check keeps of c.
func describe(c *x509.Certificate) connector.StoredCertificate {
	return connector.StoredCertificate{Subject: c.Subject.String(), Issuer: c.Issuer.String(), NotAfter: c.NotAfter.UTC()}
}

// stamp is t as a problem writes it.
func stamp(t time.Time) string {
	return t.UTC().Format(time.RFC3339)
}

// The roles of the certificates a check found.
const (
	leafRole         = "Leaf"
	intermediateRole = "Intermediate"
	anchorRole       = "Trust anchor"
	servedRole       = "Served"
)

// Found is a certificate a check found and its Role in what was found.
type Found struct {
	Role string
	connector.StoredCertificate
}

// FoundIn is each certificate c found, in order, with its role: the
// leaf, the intermediates and the trust anchor of the path when c
// reached an anchor, else each certificate served.
func FoundIn(c connector.StoredCertificateCheck) []Found {
	found := make([]Found, 0, len(c.Chain)+1)
	if c.Anchor == nil {
		for _, cert := range c.Chain {
			found = append(found, Found{servedRole, cert})
		}
		return found
	}
	for i, cert := range c.Chain {
		role := intermediateRole
		if i == 0 {
			role = leafRole
		}
		found = append(found, Found{role, cert})
	}
	return append(found, Found{anchorRole, *c.Anchor})
}
