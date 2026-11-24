// Package email writes the site's mail and hands it to the Mailer
// connector: plain text messages, from the address the mail settings
// give, each with its date from the Clock and its Message-ID from the
// Entropy connector.
package email

import (
	"bytes"
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"mime"
	"mime/quotedprintable"
	"net/mail"
	"strings"
	"time"

	"github.com/dakriy/doorcode/internal/connector"
	"github.com/dakriy/doorcode/internal/settings"
)

// ErrNoAddress reports a user whose email is not an address mail can be
// sent to.
var ErrNoAddress = errors.New("your account has no email address mail can be sent to")

// ErrNotSent reports a message the mail server did not take, or that
// could not reach it.
var ErrNotSent = errors.New("the mail server could not be reached or refused the message")

// messageIDBytes is how many entropy bytes make a Message-ID unique.
const messageIDBytes = 16

// Service sends the site's mail.
type Service struct {
	Mailer  connector.Mailer
	Clock   connector.Clock
	Entropy connector.Entropy
}

// Message is a plain text message to To about Subject, saying Text, whose
// lines end in LF.
type Message struct {
	To      []*mail.Address
	Subject string
	Text    string
}

// AddressOf is email as an address mail can be sent to, or ErrNoAddress.
func AddressOf(email string) (*mail.Address, error) {
	address, err := mail.ParseAddress(email)
	if err != nil {
		return nil, ErrNoAddress
	}
	return address, nil
}

// Send writes msg from the sender site gives and hands it to the server
// site names, reporting what the connection or the server said as
// ErrNotSent.
func (s Service) Send(ctx context.Context, site settings.Mail, msg Message) error {
	envelope := connector.Envelope{From: site.From.Address}
	for _, to := range msg.To {
		envelope.To = append(envelope.To, to.Address)
	}
	written := s.compose(site, msg)
	if err := s.Mailer.Send(ctx, site.Server, envelope, written); err != nil {
		return fmt.Errorf("%w: %w", ErrNotSent, err)
	}
	return nil
}

// compose is msg as an Internet message from site's sender: its headers,
// then its text in quoted-printable UTF-8, every line ending in CR LF.
func (s Service) compose(site settings.Mail, msg Message) []byte {
	var to []string
	for _, address := range msg.To {
		to = append(to, address.String())
	}
	var b bytes.Buffer
	header := func(name, value string) {
		b.WriteString(name + ": " + value + "\r\n")
	}
	header("From", site.From.String())
	header("To", strings.Join(to, ", "))
	if site.ReplyTo != nil {
		header("Reply-To", site.ReplyTo.String())
	}
	header("Subject", mime.QEncoding.Encode("utf-8", msg.Subject))
	header("Date", s.Clock.Now().UTC().Format(time.RFC1123Z))
	header("Message-ID", s.messageID(site.From))
	header("MIME-Version", "1.0")
	header("Content-Type", "text/plain; charset=utf-8")
	header("Content-Transfer-Encoding", "quoted-printable")
	b.WriteString("\r\n")
	body := quotedprintable.NewWriter(&b)
	// Writing to a buffer cannot fail.
	_, _ = body.Write([]byte(msg.Text))
	_ = body.Close()
	return b.Bytes()
}

// messageID is a new Message-ID in the domain of from.
func (s Service) messageID(from *mail.Address) string {
	_, domain, _ := strings.Cut(from.Address, "@")
	return "<" + hex.EncodeToString(s.Entropy.Bytes(messageIDBytes)) + "@" + domain + ">"
}
