//go:build !slow

package connector_test

import (
	"crypto/tls"
	"net"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"

	"github.com/dakriy/doorcode/internal/connector"
	"github.com/dakriy/doorcode/internal/driver/tlsprobe"
	"github.com/dakriy/doorcode/internal/testsupport"
)

// servedChain is a chain as a server serves it during a root change: a
// leaf for localhost, its intermediate, and two cross-signs, none of
// which the probe trusts.
func servedChain() []testsupport.Cert {
	pki := testsupport.TestPKI()
	return []testsupport.Cert{pki.YE1.Leaf("2025-12-01", "2026-03-01", "localhost"), pki.YE1, pki.RootYEByX2, pki.X2ByX1}
}

func TestProbeReturnsEveryCertificateServedInOrderWithoutVerifyingThem(t *testing.T) {
	versions := map[string][2]uint16{
		"TLS 1.2 only": {tls.VersionTLS12, tls.VersionTLS12},
		"TLS 1.3 only": {tls.VersionTLS13, tls.VersionTLS13},
	}
	for _, impl := range probeImpls {
		for name, v := range versions {
			t.Run(impl.name+", "+name, func(t *testing.T) {
				chain := servedChain()
				probe, host, port, named := impl.open(t, probeServer{chain: chain, min: v[0], max: v[1]})

				served, err := probe.Served(t.Context(), host, port)

				var want [][]byte
				for _, c := range chain {
					want = append(want, c.DER)
				}
				if diff := cmp.Diff([]any{want, nil, "localhost"}, []any{served, err, named()}); diff != "" {
					t.Errorf("served, error, server name asked for (-want +got):\n%s", diff)
				}
			})
		}
	}
}

func TestProbeRefusesAServerSpeakingOnlyAVersionBeforeTLS12(t *testing.T) {
	for _, impl := range probeImpls {
		t.Run(impl.name, func(t *testing.T) {
			probe, host, port, _ := impl.open(t, probeServer{chain: servedChain(), min: tls.VersionTLS10, max: tls.VersionTLS11})

			_, err := probe.Served(t.Context(), host, port)

			if err == nil {
				t.Error("the probe completed a handshake, want an error")
			}
		})
	}
}

// unreachableProbes are probes each given a server on this machine that
// cannot be probed: the stub, which no endpoint answers, and the real
// probe given a port nothing listens on and a listener that never
// answers, which it gives up on after a tenth of a second.
var unreachableProbes = map[string]func(t *testing.T) (connector.CertificateProbe, int){
	"stub": func(*testing.T) (connector.CertificateProbe, int) { return testsupport.NewProbe(), 4433 },
	"tls, nothing listening": func(t *testing.T) (connector.CertificateProbe, int) {
		l := listen(t)
		port := l.Addr().(*net.TCPAddr).Port
		_ = l.Close()
		return tlsprobe.New(time.Second), port
	},
	"tls, never answering": func(t *testing.T) (connector.CertificateProbe, int) {
		return tlsprobe.New(100 * time.Millisecond), listen(t).Addr().(*net.TCPAddr).Port
	},
}

func TestProbeReportsAServerItCannotReach(t *testing.T) {
	for name, open := range unreachableProbes {
		t.Run(name, func(t *testing.T) {
			probe, port := open(t)

			_, err := probe.Served(t.Context(), "127.0.0.1", port)

			if err == nil {
				t.Error("the probe succeeded, want an error")
			}
		})
	}
}
