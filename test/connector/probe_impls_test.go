package connector_test

import (
	"crypto/tls"
	"net"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/dakriy/doorcode/internal/connector"
	"github.com/dakriy/doorcode/internal/driver/tlsprobe"
	"github.com/dakriy/doorcode/internal/testsupport"
)

// probeTimeout is how long the real probe gives each handshake in the
// connector tests.
const probeTimeout = 2 * time.Second

// probeServer is a TLS server a probe test reaches: the chain it serves,
// leaf first, and the lowest and highest TLS versions it speaks.
type probeServer struct {
	chain    []testsupport.Cert
	min, max uint16
}

// probeImpl opens a probe and the server of a case, returning the probe,
// the host and port it reaches the server at, and what reads the server
// name the server was last asked for.
type probeImpl struct {
	name string
	open func(t *testing.T, s probeServer) (connector.CertificateProbe, string, int, func() string)
}

// probeImpls are the probes: the stub, serving the case's chain or
// failing as a client of TLS 1.2 and 1.3 would fail with the server, and
// the real one against a TLS listener on this machine.
var probeImpls = []probeImpl{
	{name: "stub", open: func(_ *testing.T, s probeServer) (connector.CertificateProbe, string, int, func() string) {
		p := testsupport.NewProbe()
		if s.max < tls.VersionTLS12 {
			p.Fail("localhost", 4433, "remote error: tls: protocol version not supported")
		} else {
			p.Serve("localhost", 4433, s.chain...)
		}
		return p, "localhost", 4433, func() string {
			probed := p.Probed()
			host, _, _ := strings.Cut(probed[len(probed)-1], ":")
			return host
		}
	}},
	{name: "tls", open: func(t *testing.T, s probeServer) (connector.CertificateProbe, string, int, func() string) {
		port, named := tlsServer(t, s)
		return tlsprobe.New(probeTimeout), "localhost", port, named
	}},
}

// tlsServer is a TLS server on this machine serving s, returning its port
// and what reads the server name its latest client asked for.
func tlsServer(t *testing.T, s probeServer) (int, func() string) {
	t.Helper()
	var mu sync.Mutex
	asked := ""
	config := &tls.Config{
		Certificates: []tls.Certificate{testsupport.TLSCertificate(s.chain...)},
		MinVersion:   s.min, MaxVersion: s.max,
		GetConfigForClient: func(hello *tls.ClientHelloInfo) (*tls.Config, error) {
			mu.Lock()
			defer mu.Unlock()
			asked = hello.ServerName
			return nil, nil
		},
	}
	var lc net.ListenConfig
	l, err := lc.Listen(t.Context(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = l.Close() })
	go func() {
		for {
			conn, err := l.Accept()
			if err != nil {
				return
			}
			go func() {
				server := tls.Server(conn, config)
				_ = server.HandshakeContext(t.Context())
				_ = server.Close()
			}()
		}
	}()
	return l.Addr().(*net.TCPAddr).Port, func() string {
		mu.Lock()
		defer mu.Unlock()
		return asked
	}
}
