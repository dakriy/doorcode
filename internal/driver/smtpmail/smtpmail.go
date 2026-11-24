// Package smtpmail implements connector.Mailer with net/smtp and
// crypto/tls.
package smtpmail

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"net"
	"net/smtp"
	"strconv"
	"time"

	"github.com/dakriy/doorcode/internal/connector"
)

// Mailer talks SMTP to the server each message names.
type Mailer struct {
	timeout time.Duration
	roots   *x509.CertPool
}

// New returns a Mailer that gives each message timeout, from dialing to
// the server's last answer, and verifies servers against roots, the
// system's roots when nil.
func New(timeout time.Duration, roots *x509.CertPool) Mailer {
	return Mailer{timeout: timeout, roots: roots}
}

// Send connects to server, protects the connection as server says,
// signs in when server names a user, and delivers message.
func (m Mailer) Send(ctx context.Context, server connector.MailServer, envelope connector.Envelope, message []byte) error {
	ctx, cancel := context.WithTimeout(ctx, m.timeout)
	defer cancel()
	conn, err := m.dial(ctx, server)
	if err != nil {
		return err
	}
	deadline, _ := ctx.Deadline()
	_ = conn.SetDeadline(deadline)
	c, err := smtp.NewClient(conn, server.Host)
	if err != nil {
		_ = conn.Close()
		return err
	}
	defer func() { _ = c.Close() }()
	if err := m.upgrade(c, server); err != nil {
		return err
	}
	return deliver(c, server, envelope, message)
}

// tlsConfig is how the connection to host is protected: TLS 1.2 or later,
// the certificate verified for host.
func (m Mailer) tlsConfig(host string) *tls.Config {
	return &tls.Config{ServerName: host, RootCAs: m.roots, MinVersion: tls.VersionTLS12}
}

// dial opens the connection to server, in TLS from the start for
// implicit TLS.
func (m Mailer) dial(ctx context.Context, server connector.MailServer) (net.Conn, error) {
	address := net.JoinHostPort(server.Host, strconv.Itoa(server.Port))
	if server.Security == connector.MailTLS {
		d := tls.Dialer{Config: m.tlsConfig(server.Host)}
		return d.DialContext(ctx, "tcp", address)
	}
	var d net.Dialer
	return d.DialContext(ctx, "tcp", address)
}

// upgrade asks the server for STARTTLS when server says to.
func (m Mailer) upgrade(c *smtp.Client, server connector.MailServer) error {
	if server.Security != connector.MailStartTLS {
		return nil
	}
	return c.StartTLS(m.tlsConfig(server.Host))
}

// deliver signs in when server names a user, then sends message from and
// to the envelope's addresses and ends the conversation.
func deliver(c *smtp.Client, server connector.MailServer, envelope connector.Envelope, message []byte) error {
	if err := authenticate(c, server); err != nil {
		return err
	}
	if err := c.Mail(envelope.From); err != nil {
		return err
	}
	if err := recipients(c, envelope.To); err != nil {
		return err
	}
	if err := data(c, message); err != nil {
		return err
	}
	return c.Quit()
}

// authenticate signs in as the user server names, when it names one.
func authenticate(c *smtp.Client, server connector.MailServer) error {
	if server.Username == "" {
		return nil
	}
	return c.Auth(plainAuth{username: server.Username, password: server.Password})
}

// plainAuth is PLAIN authentication (RFC 4616) over whatever connection
// the client holds, protected or not; smtp.PlainAuth refuses a
// connection in the clear to any host but localhost.
type plainAuth struct {
	username string
	password string
}

// Start sends the username and password in the initial response.
func (a plainAuth) Start(*smtp.ServerInfo) (string, []byte, error) {
	return "PLAIN", []byte("\x00" + a.username + "\x00" + a.password), nil
}

// Next fails on a challenge, which PLAIN never has.
func (a plainAuth) Next(_ []byte, more bool) ([]byte, error) {
	if more {
		return nil, errors.New("unexpected server challenge")
	}
	return nil, nil
}

// recipients names each of to to the server.
func recipients(c *smtp.Client, to []string) error {
	for _, address := range to {
		if err := c.Rcpt(address); err != nil {
			return err
		}
	}
	return nil
}

// data sends message as the mail's content.
func data(c *smtp.Client, message []byte) error {
	w, err := c.Data()
	if err != nil {
		return err
	}
	if _, err := w.Write(message); err != nil {
		return err
	}
	return w.Close()
}
