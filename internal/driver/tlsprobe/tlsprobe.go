// Package tlsprobe implements connector.CertificateProbe with crypto/tls.
package tlsprobe

import (
	"context"
	"crypto/tls"
	"net"
	"strconv"
	"time"
)

// Probe completes a TLS handshake to see what a server serves.
type Probe struct {
	timeout time.Duration
}

// New returns a Probe that gives each handshake timeout, from dialing to
// its end.
func New(timeout time.Duration) Probe {
	return Probe{timeout: timeout}
}

// Served dials host at port, completes a TLS 1.2 or 1.3 handshake naming
// host as the server, and returns the certificates the server served. It
// verifies none of them: judging them is the caller's.
func (p Probe) Served(ctx context.Context, host string, port int) ([][]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, p.timeout)
	defer cancel()
	d := tls.Dialer{Config: &tls.Config{
		ServerName:         host,
		InsecureSkipVerify: true, //nolint:gosec // the caller verifies what was served against its own trust list
		MinVersion:         tls.VersionTLS12,
		MaxVersion:         tls.VersionTLS13,
	}}
	conn, err := d.DialContext(ctx, "tcp", net.JoinHostPort(host, strconv.Itoa(port)))
	if err != nil {
		return nil, err
	}
	defer func() { _ = conn.Close() }()
	var served [][]byte
	for _, c := range conn.(*tls.Conn).ConnectionState().PeerCertificates {
		served = append(served, c.Raw)
	}
	return served, nil
}
