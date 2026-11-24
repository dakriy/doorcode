package entrypoint_test

import (
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/dakriy/doorcode/internal/testsupport"
)

// testMailNote is what the Mail tab says of the test email it sends to
// the viewer's address.
func testMailNote(address string) string {
	return "Sends a test email to your own address, " + address + ", through the mail settings as they are saved, " +
		"and says what the mail server answered. Save any change above first."
}

// testMailSentFlash is what the Mail tab says once the mail server took a
// test email.
const testMailSentFlash = "The mail server took the test email. It should reach your address shortly; " +
	"if it does not, look in your spam folder and in the mail server's log."

// mailSite is an administrator's browser on a site whose mail settings
// name the server smtp.example.com on port 587 over STARTTLS as the user
// doorcode with the password s3cret, sending from Doorcode
// <doorcode@example.com>, with more settings put, each a key and a value.
func mailSite(t *testing.T, f *testsupport.Factory, more ...string) *testsupport.Browser {
	t.Helper()
	b := signedInAdmin(t, f)
	settings := append([]string{
		"mail_host", "smtp.example.com", "mail_port", "587", "mail_security", "starttls",
		"mail_username", "doorcode", "mail_password", "s3cret", "mail_from", "Doorcode <doorcode@example.com>",
	}, more...)
	for i := 0; i < len(settings); i += 2 {
		putSetting(t, b, settings[i], settings[i+1])
	}
	return b
}

// sentMail is each message the stub mailer was given, read as a mail
// client reads it.
func sentMail(t *testing.T, f *testsupport.Factory) []testsupport.ReadMail {
	t.Helper()
	var read []testsupport.ReadMail
	for _, sent := range f.MailerStub.Sent() {
		msg, err := testsupport.ReadMessage(sent.Message)
		if err != nil {
			t.Fatalf("reading %q: %v", sent.Message, err)
		}
		read = append(read, msg)
	}
	return read
}

// testMailText is the text of a test email from site, sent by the first
// administrator through the server host on port over security, from
// from.
func testMailText(site, host, port, security, from string) string {
	return "This is a test email from " + site + ". Administrator sent it from the Mail settings to check that the site can send mail.\n\n" +
		"It went through the mail server " + host + " on port " + port + " with connection security " + security + ", from " + from + ".\n"
}

func TestMailSettingsRefuseWhatTheyCannotUse(t *testing.T) {
	t.Parallel()
	cases := map[string]struct{ key, value, refusal string }{
		"a host with a space":      {"mail_host", "smtp example.com", "must be empty, a host name of up to 253 letters, digits, dots, and hyphens, or an IP address"},
		"port 0":                   {"mail_port", "0", "must be a port from 1 to 65535"},
		"port 65536":               {"mail_port", "65536", "must be a port from 1 to 65535"},
		"a port that is no number": {"mail_port", "smtp", "must be a port from 1 to 65535"},
		"another security":         {"mail_security", "ssl", "must be one of tls, starttls, none"},
		"a sender with no address": {"mail_from", "Doorcode", "must be one email address, such as doorcode@example.com, alone or after a name"},
		"a reply-to of two":        {"mail_reply_to", "a@example.com, b@example.com", "must be one email address, such as doorcode@example.com, alone or after a name"},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			got := signedInAdmin(t, testsupport.NewFactory()).PutJSON("/api/v1/settings/"+c.key, `{"value":"`+c.value+`"}`)

			want := apiError(400, "invalid value for "+c.key+": "+c.refusal)
			if diff := cmp.Diff(want, got); diff != "" {
				t.Errorf("response (-want +got):\n%s", diff)
			}
		})
	}
}

func TestMailSettingsTakeWhatTheyCanUse(t *testing.T) {
	t.Parallel()
	cases := map[string]struct{ key, value string }{
		"a host name":          {"mail_host", "smtp.example.com"},
		"an IPv4 address":      {"mail_host", "192.0.2.25"},
		"an IPv6 address":      {"mail_host", "2001:db8::25"},
		"no host":              {"mail_host", ""},
		"port 1":               {"mail_port", "1"},
		"port 65535":           {"mail_port", "65535"},
		"tls":                  {"mail_security", "tls"},
		"starttls":             {"mail_security", "starttls"},
		"none":                 {"mail_security", "none"},
		"a bare sender":        {"mail_from", "doorcode@example.com"},
		"a sender with a name": {"mail_from", "Doorcode <doorcode@example.com>"},
		"no sender":            {"mail_from", ""},
		"a reply-to":           {"mail_reply_to", "Front desk <desk@example.com>"},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			got := signedInAdmin(t, testsupport.NewFactory()).PutJSON("/api/v1/settings/"+c.key, `{"value":"`+c.value+`"}`)

			if diff := cmp.Diff([]any{200, c.value}, []any{got.Status, testsupport.JSONAt(got.Body, "/value")}); diff != "" {
				t.Errorf("status, value (-want +got):\n%s", diff)
			}
		})
	}
}

func TestTheMailPasswordIsStoredEncryptedAndShownMasked(t *testing.T) {
	t.Parallel()
	f := testsupport.NewFactory()
	b := signedInAdmin(t, f)

	got := send(t, b, "PUT", "/api/v1/settings/mail_password", `{"value":"s3cret"}`, 200)
	stored, err := testsupport.OpenSecret("mail_password", f.StoreStub.StoredSetting("mail_password"))

	want := []any{"********", true, "s3cret", "<nil>"}
	if diff := cmp.Diff(want, []any{testsupport.JSONAt(got.Body, "/value"), testsupport.JSONAt(got.Body, "/secret"), stored, testsupport.ErrorText(err)}); diff != "" {
		t.Errorf("shown, secret, stored, error (-want +got):\n%s", diff)
	}
}

func TestTheMailTabOffersATestEmailToTheViewersOwnAddress(t *testing.T) {
	t.Parallel()
	b := signedInAdmin(t, testsupport.NewFactory())

	got := testsupport.Forms(b.Get("/admin/settings?tab=mail").Body)

	want := map[string]string{"/sign-out": "Sign out", "/admin/settings": "Save settings", "/admin/settings/mail/test": "Send test email"}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("forms (-want +got):\n%s", diff)
	}
}

func TestTheMailTabAsksForEachMailSettingWithItsControl(t *testing.T) {
	t.Parallel()
	b := signedInAdmin(t, testsupport.NewFactory())

	page := testsupport.ParsePage(b.Get("/admin/settings?tab=mail").Body)

	keys := []string{"mail_host", "mail_port", "mail_security", "mail_username", "mail_password", "mail_from", "mail_reply_to"}
	want := []any{
		map[string]string{
			"mail_host": "text", "mail_port": "number", "mail_security": "select", "mail_username": "text",
			"mail_password": "password", "mail_from": "text", "mail_reply_to": "text",
		},
		map[string]string{
			"mail_host": "smtp.example.com", "mail_port": "", "mail_security": "", "mail_username": "", "mail_password": "Not set",
			"mail_from": "Doorcode <doorcode@example.com>", "mail_reply_to": "help@example.com",
		},
		map[string]string{"mail_host": "", "mail_port": "1 to 65535 by 1", "mail_security": "", "mail_username": "", "mail_password": "", "mail_from": "", "mail_reply_to": ""},
	}
	got := []any{entriesOf(page.Inputs, keys...), entriesOf(page.Placeholders, keys...), entriesOf(page.Limits, keys...)}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("inputs, placeholders, limits (-want +got):\n%s", diff)
	}
}

func TestATestEmailGoesToTheCallersOwnAddressThroughTheSavedServer(t *testing.T) {
	t.Parallel()
	f := testsupport.NewFactory()
	b := mailSite(t, f)

	got := b.Send("POST", "/api/v1/settings/mail/test", "")

	want := testsupport.Response{Status: 200, ContentType: jsonType,
		Body: `{"sent_to":"admin@example.com","host":"smtp.example.com","port":587,"security":"starttls"}` + "\n"}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("response (-want +got):\n%s", diff)
	}
	sent := f.MailerStub.Sent()
	wantSent := []testsupport.SentMail{{
		Server:   testsupport.MailServer("smtp.example.com", 587, "starttls", "doorcode", "s3cret"),
		Envelope: testsupport.Envelope("doorcode@example.com", "admin@example.com"),
	}}
	for i := range sent {
		sent[i].Message = ""
	}
	if diff := cmp.Diff(wantSent, sent); diff != "" {
		t.Errorf("sent (-want +got):\n%s", diff)
	}
}

func TestATestEmailSaysWhoSentItAndWhereItWent(t *testing.T) {
	t.Parallel()
	f := testsupport.NewFactory()
	b := mailSite(t, f)

	send(t, b, "POST", "/api/v1/settings/mail/test", "", 200)

	want := []testsupport.ReadMail{{
		Headers: map[string][]string{
			"From":                      {`"Doorcode" <doorcode@example.com>`},
			"To":                        {"<admin@example.com>"},
			"Subject":                   {"Test email from doorcode"},
			"Date":                      {"Mon, 05 Jan 2026 09:00:00 +0000"},
			"Message-Id":                {"<" + testMessageID + "@example.com>"},
			"Mime-Version":              {"1.0"},
			"Content-Type":              {"text/plain; charset=utf-8"},
			"Content-Transfer-Encoding": {"quoted-printable"},
		},
		Body: testMailText("doorcode", "smtp.example.com", "587", "starttls", `"Doorcode" <doorcode@example.com>`),
	}}
	if diff := cmp.Diff(want, sentMail(t, f)); diff != "" {
		t.Errorf("mail (-want +got):\n%s", diff)
	}
}

// testMessageID is the Message-ID's unique part of the first mail
// mailSite's site sends: 16 entropy bytes, as hex, the ones after the 12
// of the mail password's nonce (0x76 to 0x81, following the first
// administrator's session).
const testMessageID = "82838485868788898a8b8c8d8e8f9091"

func TestATestEmailNamesTheSiteRepliesGoToAndTheSenderAsGiven(t *testing.T) {
	t.Parallel()
	f := testsupport.NewFactory()
	f.EnvStub["DOORCODE_SITE_NAME"] = "Café Nord"
	b := mailSite(t, f, "mail_from", "doorcode@example.com", "mail_reply_to", "Front desk <desk@example.com>")

	send(t, b, "POST", "/api/v1/settings/mail/test", "", 200)

	got := sentMail(t, f)[0]
	want := []any{[]string{"<doorcode@example.com>"}, []string{`"Front desk" <desk@example.com>`}, []string{"Test email from Café Nord"},
		testMailText("Café Nord", "smtp.example.com", "587", "starttls", "<doorcode@example.com>")}
	if diff := cmp.Diff(want, []any{got.Headers["From"], got.Headers["Reply-To"], got.Headers["Subject"], got.Body}); diff != "" {
		t.Errorf("from, reply-to, subject, body (-want +got):\n%s", diff)
	}
}

// unsendableTestMails are sites on which a test email cannot be sent,
// each arranged on a factory and returning an administrator's browser,
// with the status and the refusal the server answers with.
var unsendableTestMails = map[string]struct {
	arrange func(t *testing.T, f *testsupport.Factory) *testsupport.Browser
	status  int
	refusal string
}{
	"no server": {func(t *testing.T, f *testsupport.Factory) *testsupport.Browser {
		return mailSite(t, f, "mail_host", "")
	}, 409, mailNotSetUp},
	"no sender": {func(t *testing.T, f *testsupport.Factory) *testsupport.Browser {
		return mailSite(t, f, "mail_from", "")
	}, 409, mailNotSetUp},
	"a port the environment gets wrong": {func(t *testing.T, f *testsupport.Factory) *testsupport.Browser {
		f.EnvStub["DOORCODE_MAIL_PORT"] = "submission"
		b := signedInAdmin(t, f)
		putSettings(t, b, []string{"mail_host", `"smtp.example.com"`, "mail_from", `"doorcode@example.com"`})
		return b
	}, 409, "invalid value for mail_port: must be a port from 1 to 65535"},
	"a caller with no address": {func(t *testing.T, f *testsupport.Factory) *testsupport.Browser {
		f.EnvStub["DOORCODE_ADMIN_EMAIL"] = "admin"
		b := testsupport.NewBrowser(start(t, f).Handler())
		b.Post("/sign-in", "email", "admin", "password", adminPassword(t, f))
		return b
	}, 409, "your account has no email address mail can be sent to"},
	"a server that refuses it": {func(t *testing.T, f *testsupport.Factory) *testsupport.Browser {
		f.MailerStub.FailWith("535 5.7.8 Authentication credentials invalid")
		return mailSite(t, f)
	}, 502, "the mail server could not be reached or refused the message: 535 5.7.8 Authentication credentials invalid"},
}

// mailNotSetUp is the refusal of a test email while the site has no mail
// server or no sending address.
const mailNotSetUp = "mail is not set up: give the mail server's host and the address mail is sent from on the Mail tab of the settings"

func TestATestEmailIsRefusedWhileMailIsNotSetUpOrTheCallerHasNoAddress(t *testing.T) {
	t.Parallel()
	for name, c := range unsendableTestMails {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			f := testsupport.NewFactory()
			b := c.arrange(t, f)

			got := b.Send("POST", "/api/v1/settings/mail/test", "")

			if diff := cmp.Diff(apiError(c.status, c.refusal), got); diff != "" {
				t.Errorf("response (-want +got):\n%s", diff)
			}
		})
	}
}

func TestATestEmailFromTheMailTabGoesBackToItSayingSo(t *testing.T) {
	t.Parallel()
	f := testsupport.NewFactory()
	b := mailSite(t, f)

	sent := b.Post("/admin/settings/mail/test")
	page := pageOf(b.Get(sent.Location))

	saved := map[string]shownSetting{}
	for key, value := range map[string]string{
		"mail_host": "smtp.example.com", "mail_port": "587", "mail_security": "starttls",
		"mail_username": "doorcode", "mail_password": "********", "mail_from": "Doorcode <doorcode@example.com>",
	} {
		saved[key] = shownSetting{key, value, "database"}
	}
	want := settingsPage("Mail", defaultSettings, saved, nil, nil)
	want.Messages = []string{testMailSentFlash}
	if diff := cmp.Diff([]any{303, "/admin/settings?tab=mail", want, 1}, []any{sent.Status, sent.Location, page, len(f.MailerStub.Sent())}); diff != "" {
		t.Errorf("status, location, page, messages sent (-want +got):\n%s", diff)
	}
}

func TestATestEmailGoesThroughTheMailSettingsTheEnvironmentGivesWhichTheMailTabShowsAsReadOnly(t *testing.T) {
	t.Parallel()
	f := testsupport.NewFactory()
	given := map[string]string{
		"mail_host": "mailpit", "mail_port": "1025", "mail_security": "none", "mail_from": "doorcode dev <doorcode@example.com>",
	}
	shown := map[string]shownSetting{}
	for key, value := range given {
		f.EnvStub["DOORCODE_"+testsupport.Upper(key)] = value
		shown[key] = shownSetting{key, value, "environment, read-only"}
	}
	b := signedInAdmin(t, f)

	sent := b.Post("/admin/settings/mail/test")
	page := pageOf(b.Get(sent.Location))

	want := settingsPage("Mail", defaultSettings, shown, nil, nil)
	want.Messages = []string{testMailSentFlash}
	wantSent := []testsupport.SentMail{{
		Server:   testsupport.MailServer("mailpit", 1025, "none", "", ""),
		Envelope: testsupport.Envelope("doorcode@example.com", "admin@example.com"),
	}}
	got := f.MailerStub.Sent()
	for i := range got {
		got[i].Message = ""
	}
	if diff := cmp.Diff([]any{303, want, wantSent}, []any{sent.Status, page, got}); diff != "" {
		t.Errorf("status, page, sent (-want +got):\n%s", diff)
	}
}

func TestATestEmailTheMailTabCannotSendShowsItWithWhy(t *testing.T) {
	t.Parallel()
	for name, c := range unsendableTestMails {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			f := testsupport.NewFactory()
			b := c.arrange(t, f)
			b.Get("/")

			got := b.Post("/admin/settings/mail/test")

			page := pageOf(got)
			want := []any{c.status, "Mail (current)", []string{"No test email was sent: " + c.refusal + "."}}
			if diff := cmp.Diff(want, []any{got.Status, page.Nav[len(page.Nav)-2], page.Messages}); diff != "" {
				t.Errorf("status, tab, messages (-want +got):\n%s", diff)
			}
		})
	}
}

func TestATestEmailWhoseSettingsCannotBeReadAnswersWithARequestID(t *testing.T) {
	t.Parallel()
	f := testsupport.NewFactory()
	b := mailSite(t, f)
	f.StoreStub.FailSettings()

	got := b.Send("POST", "/api/v1/settings/mail/test", "")

	if diff := cmp.Diff(internalError("8283848586878889"), got); diff != "" {
		t.Errorf("response (-want +got):\n%s", diff)
	}
}

func TestATestEmailFromTheMailTabWhoseSettingsCannotBeReadShowsTheErrorPage(t *testing.T) {
	t.Parallel()
	f := testsupport.NewFactory()
	b := mailSite(t, f)
	f.StoreStub.FailSettings()

	got := b.Post("/admin/settings/mail/test")

	if diff := cmp.Diff([]any{500, "Something went wrong"}, []any{got.Status, pageOf(got).Title}); diff != "" {
		t.Errorf("status, title (-want +got):\n%s", diff)
	}
}
