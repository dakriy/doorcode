//go:build slow

package connector_test

import (
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/dakriy/doorcode/internal/connector"
	"github.com/dakriy/doorcode/internal/testsupport"
)

func TestMailerDeliversAMessageEachWayItCanReachAServer(t *testing.T) {
	cases := map[string]mailCase{
		"in the clear": {server: plainMailpit, security: connector.MailNone, accepted: true},
		"in the clear signing in to a host other than localhost": {server: plainMailpit, security: connector.MailNone, password: "doorcode", accepted: true, elsewhere: true},
		"over STARTTLS":           {server: startTLSMailpit, security: connector.MailStartTLS, password: "doorcode", trusted: true, accepted: true},
		"over TLS from the start": {server: tlsMailpit, security: connector.MailTLS, password: "doorcode", trusted: true, accepted: true},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			for _, impl := range mailerImpls {
				t.Run(impl.name, func(t *testing.T) {
					mailer, server, received := impl.open(t, c)
					to := testRecipient(t)

					err := mailer.Send(t.Context(), server, connector.Envelope{From: "doorcode@example.com", To: []string{to}}, testMessage(to))

					got := subjectsAndBodies(t, received(to))
					want := []testsupport.ReadMail{{Headers: map[string][]string{"Subject": {"Connector test"}}, Body: "A line.\n.A line starting with a dot.\n"}}
					if diff := cmp.Diff([]any{"<nil>", want}, []any{testsupport.ErrorText(err), got}); diff != "" {
						t.Errorf("error, received (-want +got):\n%s", diff)
					}
				})
			}
		})
	}
}

func TestMailerReportsAServerThatDoesNotTakeTheMessage(t *testing.T) {
	cases := map[string]mailCase{
		"STARTTLS to a server that does not offer it": {server: plainMailpit, security: connector.MailStartTLS},
		"a password the server refuses":               {server: startTLSMailpit, security: connector.MailStartTLS, password: "wrong", trusted: true},
		"a certificate the mailer does not trust":     {server: tlsMailpit, security: connector.MailTLS, password: "doorcode"},
		"TLS to a server in the clear":                {server: plainMailpit, security: connector.MailTLS},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			for _, impl := range mailerImpls {
				t.Run(impl.name, func(t *testing.T) {
					mailer, server, _ := impl.open(t, c)
					to := testRecipient(t)

					err := mailer.Send(t.Context(), server, connector.Envelope{From: "doorcode@example.com", To: []string{to}}, testMessage(to))

					if err == nil {
						t.Errorf("sending %s succeeded, want an error", name)
					}
				})
			}
		})
	}
}

// subjectsAndBodies is each of raw, a message, read for its subject and
// its body.
func subjectsAndBodies(t *testing.T, raw []string) []testsupport.ReadMail {
	t.Helper()
	var read []testsupport.ReadMail
	for _, r := range raw {
		msg, err := testsupport.ReadMessage(r)
		if err != nil {
			t.Fatal(err)
		}
		read = append(read, testsupport.ReadMail{Headers: map[string][]string{"Subject": msg.Headers["Subject"]}, Body: msg.Body})
	}
	return read
}
