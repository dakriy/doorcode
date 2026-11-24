package testsupport

import (
	"context"
	"errors"
	"io"
	"mime"
	"mime/quotedprintable"
	"net/mail"
	"strings"
	"sync"

	"github.com/dakriy/doorcode/internal/connector"
)

// Mailer is the stub connector.Mailer: it records every message it is
// given and delivers none, or fails every send with the error Fail gave
// it.
type Mailer struct {
	mu      sync.Mutex
	sent    []SentMail
	failure error
}

// SentMail is one message the stub mailer was given: the server it was
// sent through, its envelope, and the message as sent.
type SentMail struct {
	Server   connector.MailServer
	Envelope connector.Envelope
	Message  string
}

// Send records the message, or reports the failure Fail gave.
func (m *Mailer) Send(_ context.Context, server connector.MailServer, envelope connector.Envelope, message []byte) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.failure != nil {
		return m.failure
	}
	m.sent = append(m.sent, SentMail{Server: server, Envelope: envelope, Message: string(message)})
	return nil
}

// Fail makes every later send report err, as a server refusing the
// message or a connection failing would.
func (m *Mailer) Fail(err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.failure = err
}

// FailWith is Fail of an error saying message.
func (m *Mailer) FailWith(message string) {
	m.Fail(errors.New(message))
}

// Sent is every message recorded, oldest first.
func (m *Mailer) Sent() []SentMail {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]SentMail(nil), m.sent...)
}

// MailServer is the mail server at host and port reached with security,
// signing in as username with password.
func MailServer(host string, port int, security, username, password string) connector.MailServer {
	return connector.MailServer{Host: host, Port: port, Security: connector.MailSecurity(security), Username: username, Password: password}
}

// Envelope is a message's envelope from from to to.
func Envelope(from string, to ...string) connector.Envelope {
	return connector.Envelope{From: from, To: to}
}

// ReadMail is a message as a mail client shows it: its headers, each by
// name with its values, the subject decoded, and its body decoded from
// its transfer encoding with LF line ends. It is worked out with net/mail,
// apart from the application.
type ReadMail struct {
	Headers map[string][]string
	Body    string
}

// ReadMessage reads raw, an Internet message, as a mail client would, or
// returns the error that stops it.
func ReadMessage(raw string) (ReadMail, error) {
	msg, err := mail.ReadMessage(strings.NewReader(raw))
	if err != nil {
		return ReadMail{}, err
	}
	body := msg.Body
	if msg.Header.Get("Content-Transfer-Encoding") == "quoted-printable" {
		body = quotedprintable.NewReader(body)
	}
	content, err := io.ReadAll(body)
	if err != nil {
		return ReadMail{}, err
	}
	headers := map[string][]string{}
	for name, values := range msg.Header {
		headers[name] = values
	}
	var decoder mime.WordDecoder
	if subject, err := decoder.DecodeHeader(msg.Header.Get("Subject")); err == nil && subject != "" {
		headers["Subject"] = []string{subject}
	}
	return ReadMail{Headers: headers, Body: strings.ReplaceAll(string(content), "\r\n", "\n")}, nil
}
