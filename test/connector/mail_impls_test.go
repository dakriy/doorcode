package connector_test

import (
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/smtp"
	"net/url"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/dakriy/doorcode/internal/connector"
	"github.com/dakriy/doorcode/internal/driver/smtpmail"
	"github.com/dakriy/doorcode/internal/testsupport"
)

// mailTimeout is how long the real mailer gives each message in the
// connector tests.
const mailTimeout = 5 * time.Second

// mailCase is a mail server a test sends through and how: the mailpit
// whose address the environment variable server names (plain, which
// offers no STARTTLS, STARTTLS, or TLS), how the connection is
// protected, the password the user doorcode signs in with (none when
// empty), whether the mailer trusts the server's certificate, whether
// the server takes the message, and whether the real mailer names the
// server by a host net/smtp does not count as localhost.
type mailCase struct {
	server    string
	security  connector.MailSecurity
	password  string
	trusted   bool
	accepted  bool
	elsewhere bool
}

// The mailpits the slow tests reach, by the variable naming each.
const (
	plainMailpit    = "DOORCODE_TEST_MAILPIT_URL"
	startTLSMailpit = "DOORCODE_TEST_MAILPIT_STARTTLS_URL"
	tlsMailpit      = "DOORCODE_TEST_MAILPIT_TLS_URL"
)

// mailerImpl opens a mailer and the server of a case, and reads back
// the raw messages that server received for a recipient.
type mailerImpl struct {
	name string
	open func(t *testing.T, c mailCase) (connector.Mailer, connector.MailServer, func(to string) []string)
}

// mailerImpls are the mailers: the stub, which takes what the case's
// server takes, and the real one against the mailpits.
var mailerImpls = []mailerImpl{
	{name: "stub", open: func(_ *testing.T, c mailCase) (connector.Mailer, connector.MailServer, func(string) []string) {
		m := &testsupport.Mailer{}
		if !c.accepted {
			m.FailWith("refused")
		}
		server := connector.MailServer{Host: "localhost", Port: 25, Security: c.security}
		return m, server, func(to string) []string {
			var found []string
			for _, sent := range m.Sent() {
				if strings.Join(sent.Envelope.To, ",") == to {
					found = append(found, sent.Message)
				}
			}
			return found
		}
	}},
	{name: "smtp", open: func(t *testing.T, c mailCase) (connector.Mailer, connector.MailServer, func(string) []string) {
		smtpAddress, web := mailpitAt(t, c.server)
		host, port, err := net.SplitHostPort(smtpAddress)
		if err != nil {
			t.Fatal(err)
		}
		n, _ := strconv.Atoi(port)
		if c.elsewhere {
			host = notLocalhost(host)
		}
		server := connector.MailServer{Host: host, Port: n, Security: c.security}
		if c.password != "" {
			server.Username, server.Password = "doorcode", c.password
		}
		var roots *x509.CertPool
		if c.trusted {
			roots = servedCertificate(t, smtpAddress, c.server)
		}
		return smtpmail.New(mailTimeout, roots), server, func(to string) []string { return caught(t, web, to) }
	}},
}

// notLocalhost is a name for host that net/smtp does not count as
// localhost: 127.0.0.2, another address of the loopback network, for
// localhost and 127.0.0.1, and host itself otherwise.
func notLocalhost(host string) string {
	if host == "localhost" || host == "127.0.0.1" {
		return "127.0.0.2"
	}
	return host
}

// mailpitAt is the SMTP address and the web address of the mailpit the
// environment variable name gives as smtp://host:port?web=<address>.
func mailpitAt(t *testing.T, name string) (string, string) {
	t.Helper()
	raw := os.Getenv(name)
	if raw == "" {
		t.Fatalf("%s is not set; see AGENTS.md, slow connector test prerequisites", name)
	}
	u, err := url.Parse(raw)
	if err != nil {
		t.Fatalf("%s: %v", name, err)
	}
	return u.Host, u.Query().Get("web")
}

// servedCertificate is a pool holding the certificate the mailpit at
// address serves, read without verifying it, over TLS from the start for
// the TLS mailpit and after STARTTLS otherwise.
func servedCertificate(t *testing.T, address, server string) *x509.CertPool {
	t.Helper()
	insecure := &tls.Config{InsecureSkipVerify: true} //nolint:gosec // reading the certificate to trust
	var state tls.ConnectionState
	if server == tlsMailpit {
		d := tls.Dialer{Config: insecure}
		conn, err := d.DialContext(t.Context(), "tcp", address)
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = conn.Close() }()
		state = conn.(*tls.Conn).ConnectionState()
	} else {
		c, err := smtp.Dial(address)
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = c.Close() }()
		if err := c.StartTLS(insecure); err != nil {
			t.Fatal(err)
		}
		state, _ = c.TLSConnectionState()
	}
	pool := x509.NewCertPool()
	pool.AddCert(state.PeerCertificates[0])
	return pool
}

// caught is every raw message the mailpit at web holds for to, waiting
// up to two seconds for one to appear.
func caught(t *testing.T, web, to string) []string {
	t.Helper()
	for range 20 {
		var found struct {
			Messages []struct {
				ID string `json:"ID"`
			} `json:"messages"`
		}
		getMailpit(t, web+"/api/v1/search?query="+url.QueryEscape(`to:"`+to+`"`), func(body []byte) {
			if err := json.Unmarshal(body, &found); err != nil {
				t.Fatal(err)
			}
		})
		if len(found.Messages) > 0 {
			var raw []string
			for _, m := range found.Messages {
				getMailpit(t, web+"/api/v1/message/"+m.ID+"/raw", func(body []byte) { raw = append(raw, string(body)) })
			}
			return raw
		}
		time.Sleep(100 * time.Millisecond)
	}
	return nil
}

// getMailpit asks the mailpit's API for target and hands read the body.
func getMailpit(t *testing.T, target string, read func([]byte)) {
	t.Helper()
	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, target, nil)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	read(body)
}

// testRecipient is an address no other test sends to.
func testRecipient(t *testing.T) string {
	name := strings.NewReplacer("/", "-", "_", "-").Replace(strings.ToLower(t.Name()))
	return name + "-" + strconv.FormatInt(time.Now().UnixNano(), 36) + "@example.com"
}

// testMessage is a message from doorcode@example.com to to.
func testMessage(to string) []byte {
	return []byte("From: <doorcode@example.com>\r\nTo: <" + to + ">\r\nSubject: Connector test\r\n" +
		"Content-Type: text/plain; charset=utf-8\r\n\r\nA line.\r\n.A line starting with a dot.\r\n")
}
