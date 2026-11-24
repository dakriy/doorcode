package settings

import (
	"context"
	"errors"
	"net/mail"
	"slices"
	"strings"

	"github.com/dakriy/doorcode/internal/check"
	"github.com/dakriy/doorcode/internal/connector"
)

// The keys of the mail settings.
const (
	keyMailHost     = "mail_host"
	keyMailPort     = "mail_port"
	keyMailSecurity = "mail_security"
	keyMailUsername = "mail_username"
	keyMailPassword = "mail_password"
	keyMailFrom     = "mail_from"
	keyMailReplyTo  = "mail_reply_to"
	mailAddressHint = "must be one email address, such as doorcode@example.com, alone or after a name"
	mailHostExample = "smtp.example.com"
	mailFromExample = "Doorcode <doorcode@example.com>"
	replyToExample  = "help@example.com"
)

// mailSecurities are the ways the site may reach its mail server, in the
// order a page offers them.
var mailSecurities = []string{string(connector.MailTLS), string(connector.MailStartTLS), string(connector.MailNone)}

// ErrMailNotConfigured reports that the site has no mail server or no
// address to send from.
var ErrMailNotConfigured = errors.New("mail is not set up: give the mail server's host and the address mail is sent from on the Mail tab of the settings")

// mailSection is the settings of the server the site sends mail through.
var mailSection = section{Section{Name: "Mail", About: "The SMTP server the site sends mail through, and the address mail is sent from. Nothing is sent while the server's host or the sending address is empty. Save the settings before sending a test email: it uses the settings as saved.", Tab: TabMail}, []definition{
	{key: keyMailHost, fallback: "", validate: accepting(mailHost).askedWith(Input{Kind: KindHost, Example: mailHostExample}), label: "Mail server",
		help: "The host name or IP address of the SMTP server, such as smtp.example.com."},
	{key: keyMailPort, fallback: "465", validate: accepting(mailPort).askedWith(port.input()), label: "Mail server port",
		help: "Usually 465 for TLS, 587 for STARTTLS, and 25 for none."},
	{key: keyMailSecurity, fallback: string(connector.MailTLS), validate: accepting(mailSecurity).askedWith(Input{Kind: KindChoice, Choices: mailSecurities}),
		label: "Connection security",
		help:  "tls speaks TLS from the start; starttls connects in the clear and refuses to send unless the server takes STARTTLS; none stays in the clear, so the mail password is sent unencrypted. The server's certificate is verified against the system's trusted roots."},
	{key: keyMailUsername, fallback: "", label: "Mail username", help: "The user to sign in to the server as, or empty to send without signing in."},
	{key: keyMailPassword, fallback: "", secret: true, label: "Mail password", help: "The password of the mail username."},
	{key: keyMailFrom, fallback: "", validate: accepting(optionalAddress).askedWith(Input{Example: mailFromExample}), label: "Sent from",
		help: "The address mail comes from, with a name if you like, such as Doorcode <doorcode@example.com>. The server must let the mail username send from it."},
	{key: keyMailReplyTo, fallback: "", validate: accepting(optionalAddress).askedWith(Input{Example: replyToExample}), label: "Replies to",
		help: "The address replies go to, or empty for the address mail is sent from."},
}}

// Mail is how the site sends mail: the server, the address mail comes
// from, and the address replies go to, nil for none; Site is the site's
// name, which its mail is signed with.
type Mail struct {
	Site    string
	Server  connector.MailServer
	From    *mail.Address
	ReplyTo *mail.Address
}

// Mail returns how the site sends mail, or ErrMailNotConfigured while the
// server's host or the sending address is empty. It reads only the mail
// settings, so no other setting stops mail.
func (s Service) Mail(ctx context.Context) (Mail, error) {
	stored, err := s.Store.Settings(ctx)
	if err != nil {
		return Mail{}, err
	}
	values := s.valuesOf(mailSection, stored)
	if values[keyMailHost] == "" || values[keyMailFrom] == "" {
		return Mail{}, ErrMailNotConfigured
	}
	var errs [6]error
	var m Mail
	m.Server.Host, errs[0] = parsed(values, keyMailHost, mailHost)
	m.Server.Port, errs[1] = parsed(values, keyMailPort, mailPort)
	m.Server.Security, errs[2] = parsed(values, keyMailSecurity, mailSecurity)
	m.Server.Password, errs[3] = s.secret(keyMailPassword, stored)
	m.From, errs[4] = parsed(values, keyMailFrom, optionalAddress)
	m.ReplyTo, errs[5] = parsed(values, keyMailReplyTo, optionalAddress)
	m.Server.Username = values[keyMailUsername]
	site, _ := lookup(keySiteName)
	m.Site = s.resolve(site, stored).Value
	return m, errors.Join(errs[:]...)
}

// mailHost parses the mail server's host: empty, a host name, or an IP
// address.
func mailHost(value string) (string, error) {
	if !optionalHost(value) {
		return "", errors.New("must be empty, " + check.HostHint + ", or an IP address")
	}
	return value, nil
}

// mailPort parses the mail server's port.
func mailPort(value string) (int, error) {
	return wholeNumber(value, port.lowest, port.highest, "must be "+portHint)
}

// mailSecurity parses how the connection to the mail server is
// protected.
func mailSecurity(value string) (connector.MailSecurity, error) {
	if !slices.Contains(mailSecurities, value) {
		return "", errors.New("must be one of " + strings.Join(mailSecurities, ", "))
	}
	return connector.MailSecurity(value), nil
}

// optionalAddress parses an email address, with or without a name, or the
// empty value, which is none.
func optionalAddress(value string) (*mail.Address, error) {
	if value == "" {
		return nil, nil
	}
	address, err := mail.ParseAddress(value)
	if err != nil {
		return nil, errors.New(mailAddressHint)
	}
	return address, nil
}
