package entrypoint_test

import (
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/dakriy/doorcode/internal/testsupport"
)

// alertSite is a keypad site whose mail goes through smtp.example.com as
// mailSite sets it up, whose alerts mail ops@example.com and Front desk
// <desk@example.com>, and where each kind of alert in kinds is enabled.
func alertSite(t *testing.T, f *testsupport.Factory, kinds ...string) *keypad {
	t.Helper()
	k := keypadSite(t, f)
	mailAlerts(t, k.b, kinds...)
	return k
}

// mailAlerts has the administrator's client c send mail through
// smtp.example.com, mail alerts to ops@example.com and Front desk
// <desk@example.com>, and enable each kind of alert in kinds.
func mailAlerts(t *testing.T, c *testsupport.Browser, kinds ...string) {
	t.Helper()
	for _, setting := range [][2]string{
		{"mail_host", "smtp.example.com"}, {"mail_port", "587"}, {"mail_security", "starttls"},
		{"mail_from", "Doorcode <doorcode@example.com>"},
		{"alert_recipients", "ops@example.com,\nFront desk <desk@example.com>"},
	} {
		send(t, c, "PUT", "/api/v1/settings/"+setting[0], `{"value":`+testsupport.Quoted(setting[1])+`}`, 200)
	}
	for _, kind := range kinds {
		send(t, c, "PUT", "/api/v1/alerts/kinds/"+kind, `{"enabled":true}`, 200)
	}
}

// setting stores value as the setting key through the API.
func (k *keypad) setting(key, value string) {
	k.t.Helper()
	send(k.t, k.b, "PUT", "/api/v1/settings/"+key, `{"value":`+testsupport.Quoted(value)+`}`, 200)
}

// runAt sets the clock to the RFC 3339 time at and has the server run.
func (k *keypad) runAt(at string) {
	k.t.Helper()
	k.at(at)
	run(k.t, k.a)
}

// outbox is what the alerts API shows of the outbox at the JSON pointer
// pointer below it, all of it for the empty pointer.
func (k *keypad) outbox(pointer string) any {
	k.t.Helper()
	return testsupport.JSONAt(send(k.t, k.b, "GET", "/api/v1/alerts", "", 200).Body, "/outbox"+pointer)
}

// mailed is each message the stub mailer took: its envelope's
// recipients, its subject, and its text.
func mailed(t *testing.T, f *testsupport.Factory) [][]any {
	t.Helper()
	out := [][]any{}
	for i, msg := range sentMail(t, f) {
		out = append(out, []any{f.MailerStub.Sent()[i].Envelope.To, msg.Headers["Subject"][0], msg.Body})
	}
	return out
}

// subjects is the subject of each message the stub mailer took.
func subjects(t *testing.T, f *testsupport.Factory) []any {
	t.Helper()
	out := []any{}
	for _, m := range mailed(t, f) {
		out = append(out, m[1])
	}
	return out
}

// errorBodies compares responses with their JSON bodies read as values,
// so an error naming quotes or angle brackets compares as its text.
var errorBodies = cmp.Transformer("body", func(r testsupport.Response) []any {
	return []any{r.Status, r.ContentType, testsupport.JSONAt(r.Body, "")}
})

// recipients are the bare addresses alertSite's alerts mail.
var recipients = []string{"ops@example.com", "desk@example.com"}

// alertFooter ends every message about the kind of alert named kind.
func alertFooter(kind string) string {
	return "\nThis mail goes to every alert recipient while the alert " + kind + " is enabled on the Alerts page.\n"
}

// forcedSubject and heldSubject are the subjects of the mail about Lobby
// being forced open and held open.
const (
	forcedSubject = "Door forced open: Lobby"
	heldSubject   = "Door held open: Lobby"
)

// cleared is the subject of the mail saying the condition whose raise had
// subject cleared.
func cleared(subject string) string { return "Cleared: " + subject }

func TestEveryKindOfAlertIsDisabledUntilEnabledAndMailsNobodyByDefault(t *testing.T) {
	t.Parallel()
	b := signedInAdmin(t, testsupport.NewFactory())

	got := testsupport.JSONAt(send(t, b, "GET", "/api/v1/alerts", "", 200).Body, "")

	want := testsupport.JSON(`{"kinds":[
		{"key":"forced_open","name":"Door forced open","about":"A door opened while nothing unlocked it. It clears when the door closes.","enabled":false},
		{"key":"held_open","name":"Door held open","about":"A door stood open for longer than its held-open time while it should have been locked. It clears when the door closes or an open span of its timeline begins.","enabled":false},
		{"key":"offline","name":"Door offline","about":"A door's controller has not been connected to the broker for the minutes of the unresponsive_minutes setting. It clears once the controller connects again.","enabled":false},
		{"key":"unresponsive","name":"Door unresponsive","about":"A door's controller has not been heard from for the minutes of the unresponsive_minutes setting while the broker says it is connected. It clears once the server hears from it or it is shown offline.","enabled":false},
		{"key":"config_reverted","name":"Door configuration reverted","about":"A door gave up the configuration it was last sent, because it could not come online with it, and runs the one before. It clears once the door no longer shows the Reverted configuration badge: it reports the configuration it was last sent, or is sent another.","enabled":false},
		{"key":"config_restarting","name":"Door restarting with its configuration","about":"A door's controller started at least twice since the configuration it was last sent went out, with no report naming that configuration. It clears once the door no longer shows the Restarting with configuration badge.","enabled":false},
		{"key":"config_mismatch","name":"Door configuration mismatched","about":"A door's controller went on reporting a configuration other than the one it was last sent. It clears once the door no longer shows the Mismatched configuration badge.","enabled":false},
		{"key":"update_failed","name":"Door firmware update failed","about":"A door's firmware update failed: the door said it could not install it, or it did not connect again with it in time. It clears once the door's firmware no longer shows Update failed: an update of it is under way again, it runs its target, or its target changes.","enabled":false}],
		"recipients":[],"mail_cleared":true,"hold_down_minutes":15,"outbox":[]}`)
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("alerts (-want +got):\n%s", diff)
	}
}

func TestADoorForcedOpenMailsTheRecipientsOnceAndAgainWhenItCloses(t *testing.T) {
	t.Parallel()
	f := testsupport.NewFactory()
	k := alertSite(t, f, "forced_open")

	k.opens(false)
	k.at("2026-01-05T09:00:10Z")
	k.report(map[string]any{"door_open": true})
	k.at("2026-01-05T09:00:20Z")
	k.says("closed")

	want := [][]any{
		{recipients, forcedSubject, "Door forced open at Lobby, Mon 5 Jan 2026 09:00:00 UTC.\n\n" +
			"Its controller says it was locked when it opened.\n" + alertFooter("Door forced open")},
		{recipients, cleared(forcedSubject), "Door forced open at Lobby cleared, Mon 5 Jan 2026 09:00:20 UTC.\n\n" +
			"It was raised Mon 5 Jan 2026 09:00:00 UTC.\n" + alertFooter("Door forced open")},
	}
	if diff := cmp.Diff(want, mailed(t, f)); diff != "" {
		t.Errorf("mail (-want +got):\n%s", diff)
	}
}

func TestTheMailOfADoorForcedOpenComesFromTheSiteToEachRecipient(t *testing.T) {
	t.Parallel()
	f := testsupport.NewFactory()
	k := alertSite(t, f, "forced_open")

	k.opens(false)

	sent := f.MailerStub.Sent()[0]
	headers := sentMail(t, f)[0].Headers
	want := []any{testsupport.MailServer("smtp.example.com", 587, "starttls", "", ""), testsupport.Envelope("doorcode@example.com", recipients...),
		[]string{`"Doorcode" <doorcode@example.com>`}, []string{"<ops@example.com>, <desk@example.com>"}}
	if diff := cmp.Diff(want, []any{sent.Server, sent.Envelope, headers["From"], headers["To"]}); diff != "" {
		t.Errorf("server, envelope, From, To (-want +got):\n%s", diff)
	}
}

func TestADoorHeldOpenMailsWhenRaisedAndWhenItCloses(t *testing.T) {
	t.Parallel()
	f := testsupport.NewFactory()
	k := alertSite(t, f, "held_open")
	k.opens(true)

	k.sweepAt("2026-01-05T09:01:00Z", "2026-01-05T09:02:00Z")
	k.at("2026-01-05T09:02:30Z")
	k.says("closed")

	want := [][]any{
		{recipients, heldSubject, "Door held open at Lobby, Mon 5 Jan 2026 09:01:00 UTC.\n\n" +
			"Open for longer than 60 seconds.\n" + alertFooter("Door held open")},
		{recipients, cleared(heldSubject), "Door held open at Lobby cleared, Mon 5 Jan 2026 09:02:30 UTC.\n\n" +
			"It was raised Mon 5 Jan 2026 09:01:00 UTC.\n" + alertFooter("Door held open")},
	}
	if diff := cmp.Diff(want, mailed(t, f)); diff != "" {
		t.Errorf("mail (-want +got):\n%s", diff)
	}
}

func TestADoorHeldOpenClearsWhenAnOpenSpanBegins(t *testing.T) {
	t.Parallel()
	f := testsupport.NewFactory()
	k := alertSite(t, f, "held_open")
	k.opens(true)
	k.sweepAt("2026-01-05T09:01:00Z")
	k.at("2026-01-05T09:01:30Z")
	createOverride(t, k.b, "1", `{"kind":"open","start":"2026-01-05T09:01:30Z","end":"2026-01-05T09:03:00Z"}`)

	k.sweepAt("2026-01-05T09:02:00Z")

	if diff := cmp.Diff([]any{heldSubject, cleared(heldSubject)}, subjects(t, f)); diff != "" {
		t.Errorf("subjects (-want +got):\n%s", diff)
	}
}

func TestOnlyTheEnabledKindsOfAlertMail(t *testing.T) {
	t.Parallel()
	f := testsupport.NewFactory()
	k := alertSite(t, f, "held_open")

	k.opens(false)
	k.sweepAt("2026-01-05T09:01:00Z")

	if diff := cmp.Diff([]any{heldSubject}, subjects(t, f)); diff != "" {
		t.Errorf("subjects (-want +got):\n%s", diff)
	}
}

func TestAClearedConditionMailsOnlyWhenItsRaiseWasMailedItsKindIsStillEnabledAndTheSettingSaysSo(t *testing.T) {
	t.Parallel()
	cases := map[string]struct {
		before, between func(k *keypad)
	}{
		"the mail when cleared setting off": {
			before:  func(k *keypad) { k.setting("alert_mail_cleared", "false") },
			between: func(*keypad) {},
		},
		"its kind disabled after it was raised": {
			before: func(*keypad) {},
			between: func(k *keypad) {
				send(k.t, k.b, "PUT", "/api/v1/alerts/kinds/forced_open", `{"enabled":false}`, 200)
			},
		},
		"the recipients emptied after it was raised": {
			before:  func(*keypad) {},
			between: func(k *keypad) { k.setting("alert_recipients", "") },
		},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			f := testsupport.NewFactory()
			k := alertSite(t, f, "forced_open")
			c.before(k)
			k.opens(false)
			c.between(k)

			k.says("closed")

			if diff := cmp.Diff([]any{forcedSubject}, subjects(t, f)); diff != "" {
				t.Errorf("subjects (-want +got):\n%s", diff)
			}
		})
	}
}

func TestAConditionRaisedWhileNobodyIsMailedIsFollowedAndMailsWhenItComesBack(t *testing.T) {
	t.Parallel()
	f := testsupport.NewFactory()
	k := alertSite(t, f, "forced_open")
	k.setting("alert_recipients", "")
	k.opens(false)
	k.setting("alert_recipients", "ops@example.com")
	k.at("2026-01-05T09:00:20Z")
	k.says("closed")

	k.at("2026-01-05T09:15:20Z")
	k.opens(false)

	if diff := cmp.Diff([][]any{{[]string{"ops@example.com"}, forcedSubject, "Door forced open at Lobby, Mon 5 Jan 2026 09:15:20 UTC.\n\n" +
		"Its controller says it was locked when it opened.\n" + alertFooter("Door forced open")}}, mailed(t, f)); diff != "" {
		t.Errorf("mail (-want +got):\n%s", diff)
	}
}

func TestAConditionThatComesBackMailsAgainOnlyOnceItStayedClearForTheHoldDown(t *testing.T) {
	t.Parallel()
	cases := map[string]struct {
		holdDown, back string
		want           []any
	}{
		"back a second before the default 15 minutes": {"15", "2026-01-05T09:15:19Z", []any{forcedSubject, cleared(forcedSubject)}},
		"back after the default 15 minutes":           {"15", "2026-01-05T09:15:20Z", []any{forcedSubject, cleared(forcedSubject), forcedSubject}},
		"back at once with no hold-down":              {"0", "2026-01-05T09:00:20Z", []any{forcedSubject, cleared(forcedSubject), forcedSubject}},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			f := testsupport.NewFactory()
			k := alertSite(t, f, "forced_open")
			k.setting("alert_hold_down_minutes", c.holdDown)
			k.opens(false)
			k.at("2026-01-05T09:00:20Z")
			k.says("closed")

			k.at(c.back)
			k.opens(false)

			if diff := cmp.Diff(c.want, subjects(t, f)); diff != "" {
				t.Errorf("subjects (-want +got):\n%s", diff)
			}
		})
	}
}

func TestAConditionThatCameBackQuietlyClearsQuietlyAndTheHoldDownCountsFromThen(t *testing.T) {
	t.Parallel()
	f := testsupport.NewFactory()
	k := alertSite(t, f, "forced_open")
	k.opens(false)
	k.at("2026-01-05T09:00:20Z")
	k.says("closed")
	k.at("2026-01-05T09:10:00Z")
	k.opens(false)
	k.at("2026-01-05T09:10:20Z")
	k.says("closed")

	k.at("2026-01-05T09:25:19Z")
	k.opens(false)
	k.at("2026-01-05T09:25:30Z")
	k.says("closed")
	k.at("2026-01-05T09:40:30Z")
	k.opens(false)

	want := []any{forcedSubject, cleared(forcedSubject), forcedSubject}
	if diff := cmp.Diff([]any{want, "Door forced open at Lobby, Mon 5 Jan 2026 09:40:30 UTC.\n\n" +
		"Its controller says it was locked when it opened.\n" + alertFooter("Door forced open")},
		[]any{subjects(t, f), mailed(t, f)[2][2]}); diff != "" {
		t.Errorf("subjects, last text (-want +got):\n%s", diff)
	}
}

func TestAClosingWithNothingRaisedLeavesTheHoldDownCountingFromTheLastClearing(t *testing.T) {
	t.Parallel()
	f := testsupport.NewFactory()
	k := alertSite(t, f, "forced_open")
	k.opens(false)
	k.at("2026-01-05T09:00:20Z")
	k.says("closed")
	k.at("2026-01-05T09:10:00Z")
	k.opens(true)
	k.at("2026-01-05T09:10:10Z")
	k.says("closed")

	k.at("2026-01-05T09:15:20Z")
	k.opens(false)

	if diff := cmp.Diff([]any{forcedSubject, cleared(forcedSubject), forcedSubject}, subjects(t, f)); diff != "" {
		t.Errorf("subjects (-want +got):\n%s", diff)
	}
}

// refused is the error the outbox shows of a message the stub mail server
// refused with 421 Try again later.
const refused = "the mail server could not be reached or refused the message: 421 Try again later"

func TestMailTheServerRefusesIsTriedAgainAfterAPauseThatDoublesUntilItFails(t *testing.T) {
	t.Parallel()
	f := testsupport.NewFactory()
	k := alertSite(t, f, "forced_open")
	f.MailerStub.FailWith("421 Try again later")
	k.opens(false)

	var got []any
	for _, at := range []string{
		"2026-01-05T09:00:59Z", "2026-01-05T09:01:00Z", "2026-01-05T09:03:00Z", "2026-01-05T09:07:00Z", "2026-01-05T09:15:00Z",
		"2026-01-05T09:30:59Z", "2026-01-05T09:31:00Z", "2026-01-05T10:31:00Z",
	} {
		k.runAt(at)
		got = append(got, []any{k.outbox("/0/status"), k.outbox("/0/attempts"), k.outbox("/0/next_attempt_at"), k.outbox("/0/last_attempt_at")})
	}

	want := []any{
		[]any{"pending", 1.0, "2026-01-05T09:01:00Z", "2026-01-05T09:00:00Z"},
		[]any{"pending", 2.0, "2026-01-05T09:03:00Z", "2026-01-05T09:01:00Z"},
		[]any{"pending", 3.0, "2026-01-05T09:07:00Z", "2026-01-05T09:03:00Z"},
		[]any{"pending", 4.0, "2026-01-05T09:15:00Z", "2026-01-05T09:07:00Z"},
		[]any{"pending", 5.0, "2026-01-05T09:31:00Z", "2026-01-05T09:15:00Z"},
		[]any{"pending", 5.0, "2026-01-05T09:31:00Z", "2026-01-05T09:15:00Z"},
		[]any{"failed", 6.0, nil, "2026-01-05T09:31:00Z"},
		[]any{"failed", 6.0, nil, "2026-01-05T09:31:00Z"},
	}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("status, attempts, next attempt, last attempt after each run (-want +got):\n%s", diff)
	}
}

func TestAFailedMessageKeepsTheLastErrorItWasRefusedWith(t *testing.T) {
	t.Parallel()
	f := testsupport.NewFactory()
	k := alertSite(t, f, "forced_open")
	f.MailerStub.FailWith("421 Try again later")
	k.opens(false)

	for _, at := range []string{"2026-01-05T09:01:00Z", "2026-01-05T09:03:00Z", "2026-01-05T09:07:00Z", "2026-01-05T09:15:00Z"} {
		k.runAt(at)
	}
	f.MailerStub.FailWith("550 Mailbox unavailable")
	k.runAt("2026-01-05T09:31:00Z")

	want := testsupport.JSON(`{"id":1,"kind":"forced_open","subject":"Door forced open: Lobby",
		"text":"Door forced open at Lobby, Mon 5 Jan 2026 09:00:00 UTC.\n\nIts controller says it was locked when it opened.\n\nThis mail goes to every alert recipient while the alert Door forced open is enabled on the Alerts page.\n",
		"to":["ops@example.com","desk@example.com"],"status":"failed","attempts":6,"created_at":"2026-01-05T09:00:00Z",
		"last_attempt_at":"2026-01-05T09:31:00Z",
		"last_error":"the mail server could not be reached or refused the message: 550 Mailbox unavailable"}`)
	if diff := cmp.Diff(want, k.outbox("/0")); diff != "" {
		t.Errorf("message (-want +got):\n%s", diff)
	}
}

func TestMailTheServerTakesOnARetryIsSentWithoutAnError(t *testing.T) {
	t.Parallel()
	f := testsupport.NewFactory()
	k := alertSite(t, f, "forced_open")
	f.MailerStub.FailWith("421 Try again later")
	k.opens(false)
	before := k.outbox("/0/last_error")
	f.MailerStub.Fail(nil)

	k.runAt("2026-01-05T09:01:00Z")

	got := []any{before, k.outbox("/0/status"), k.outbox("/0/attempts"), k.outbox("/0/sent_at"), k.outbox("/0/last_error"), subjects(t, f)}
	if diff := cmp.Diff([]any{refused, "sent", 2.0, "2026-01-05T09:01:00Z", nil, []any{forcedSubject}}, got); diff != "" {
		t.Errorf("error before, status, attempts, sent, error, subjects (-want +got):\n%s", diff)
	}
}

func TestTheEarliestMessageWaitingIsTriedWhenItIsDue(t *testing.T) {
	t.Parallel()
	f := testsupport.NewFactory()
	k := alertSite(t, f, "forced_open")
	f.MailerStub.FailWith("421 Try again later")
	k.opens(false)
	k.at("2026-01-05T09:00:30Z")
	k.says("closed")

	k.runAt("2026-01-05T09:01:00Z")

	got := []any{k.outbox("/0/attempts"), k.outbox("/0/next_attempt_at"), k.outbox("/1/attempts"), k.outbox("/1/next_attempt_at")}
	if diff := cmp.Diff([]any{1.0, "2026-01-05T09:01:30Z", 2.0, "2026-01-05T09:03:00Z"}, got); diff != "" {
		t.Errorf("cleared mail's attempts and next, raised mail's attempts and next (-want +got):\n%s", diff)
	}
}

func TestAlertMailWaitsWithAClearErrorWhileMailIsNotSetUp(t *testing.T) {
	t.Parallel()
	f := testsupport.NewFactory()
	k := alertSite(t, f, "forced_open")
	send(t, k.b, "PUT", "/api/v1/settings/mail_host", `{"value":""}`, 200)

	k.opens(false)

	got := []any{k.outbox("/0/status"), k.outbox("/0/last_error"), pageOf(k.b.Get("/admin/alerts")).Notes}
	want := []any{"pending", "mail is not set up: give the mail server's host and the address mail is sent from on the Mail tab of the settings",
		[]string{"Mail is not set up, so alert mail waits in the outbox and fails after six attempts. " +
			"Give the mail server's host and the address mail is sent from on the Mail tab of the settings.", nothingToCheck}}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("status, error, page notes (-want +got):\n%s", diff)
	}
}

// lastLogged is the newest line f's log holds with message, the zero
// entry when it holds none.
func lastLogged(f *testsupport.Factory, message string) testsupport.LogEntry {
	var last testsupport.LogEntry
	for _, e := range f.LogStub.Entries {
		if e.Message == message {
			last = e
		}
	}
	return last
}

func TestADeliveryThatCannotReadTheOutboxIsLoggedAndTriedAgainAMinuteLater(t *testing.T) {
	t.Parallel()
	f := testsupport.NewFactory()
	k := alertSite(t, f, "forced_open")
	f.MailerStub.FailWith("421 Try again later")
	k.opens(false)
	k.at("2026-01-05T09:00:30Z")
	k.says("closed")
	f.MailerStub.Fail(nil)
	f.StoreStub.FailAlerts()
	k.runAt("2026-01-05T09:01:00Z")
	logged := lastLogged(f, "mail delivery failed")
	f.StoreStub.Recover()

	k.runAt("2026-01-05T09:01:59Z")
	early := k.outbox("/1/attempts")
	k.runAt("2026-01-05T09:02:00Z")

	want := []any{testsupport.LogEntry{Level: "error", Message: "mail delivery failed", Fields: map[string]string{"error": "stub: unavailable"}},
		1.0, "sent", "2026-01-05T09:02:00Z"}
	if diff := cmp.Diff(want, []any{logged, early, k.outbox("/1/status"), k.outbox("/1/sent_at")}); diff != "" {
		t.Errorf("log, attempts before the minute, status, sent (-want +got):\n%s", diff)
	}
}

func TestADeliveryThatCannotUpdateTheOutboxIsLogged(t *testing.T) {
	t.Parallel()
	f := testsupport.NewFactory()
	k := alertSite(t, f, "forced_open")
	f.MailerStub.FailWith("421 Try again later")
	k.opens(false)
	k.at("2026-01-05T09:00:30Z")
	k.says("closed")
	f.StoreStub.FailAlertWrites()

	k.runAt("2026-01-05T09:01:00Z")

	want := testsupport.LogEntry{Level: "error", Message: "mail delivery failed", Fields: map[string]string{"error": "stub: unavailable"}}
	if diff := cmp.Diff(want, lastLog(f)); diff != "" {
		t.Errorf("log (-want +got):\n%s", diff)
	}
}

// notFollowed is the line logged when the store stops the alert condition
// of kind about Lobby being followed.
func notFollowed(kind string) testsupport.LogEntry {
	return notFollowedWith(kind, "stub: unavailable")
}

// notFollowedWith is notFollowed logging err.
func notFollowedWith(kind, err string) testsupport.LogEntry {
	return testsupport.LogEntry{Level: "error", Message: "alert condition not followed",
		Fields: map[string]string{"kind": kind, "subject": "door 1", "error": err}}
}

func TestAnAlertTheStoreCannotFollowIsLoggedAndTheDoorIsHandledAll(t *testing.T) {
	t.Parallel()
	cases := map[string]struct {
		kinds  []string
		fail   func(s *testsupport.Store)
		logged testsupport.LogEntry
	}{
		"when it cannot be read":                            {[]string{"forced_open"}, (*testsupport.Store).FailAlerts, notFollowed("forced_open")},
		"when its mail cannot be queued":                    {[]string{"forced_open"}, (*testsupport.Store).FailAlertWrites, notFollowed("forced_open")},
		"when its condition cannot be stored, mailing none": {nil, (*testsupport.Store).FailAlertWrites, notFollowed("forced_open")},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			f := testsupport.NewFactory()
			k := alertSite(t, f, c.kinds...)
			c.fail(f.StoreStub)

			k.opens(false)

			got := []any{lastLog(f), k.reasons(), k.state("/door")}
			if diff := cmp.Diff([]any{c.logged, []any{"forced open", "opened"}, "open"}, got); diff != "" {
				t.Errorf("log, reasons, door state (-want +got):\n%s", diff)
			}
		})
	}
}

func TestAClearingTheStoreCannotFollowIsLoggedAndTheDoorStillCloses(t *testing.T) {
	t.Parallel()
	f := testsupport.NewFactory()
	k := alertSite(t, f, "forced_open")
	k.opens(false)
	f.StoreStub.FailAlerts()

	k.says("closed")

	logged := lastLog(f)

	got := []any{logged, k.state("/door")}
	if diff := cmp.Diff([]any{notFollowed("forced_open"), "closed"}, got); diff != "" {
		t.Errorf("logs, door state (-want +got):\n%s", diff)
	}
}

func TestAConditionStillRaisedMailsNoSecondRaiseAndClearsOnTheNextClosing(t *testing.T) {
	t.Parallel()
	f := testsupport.NewFactory()
	k := alertSite(t, f, "forced_open")
	k.opens(false)
	f.StoreStub.FailAlerts()
	k.at("2026-01-05T09:00:20Z")
	k.says("closed")
	f.StoreStub.Recover()
	k.at("2026-01-05T09:30:00Z")
	k.opens(false)
	raised := subjects(t, f)

	k.at("2026-01-05T09:30:20Z")
	k.says("closed")

	if diff := cmp.Diff([]any{[]any{forcedSubject}, []any{forcedSubject, cleared(forcedSubject)}}, []any{raised, subjects(t, f)}); diff != "" {
		t.Errorf("subjects after the second opening, after its closing (-want +got):\n%s", diff)
	}
}

// spoiledHoldDown is what is logged while the environment gives a
// hold-down that is no number.
var spoiledHoldDown = notFollowedWith("forced_open", "invalid value for alert_hold_down_minutes: must be a whole number of minutes from 0 to 10080")

func TestARaiseWhoseSettingsCannotBeReadIsLoggedAndTheDoorStillOpens(t *testing.T) {
	t.Parallel()
	f := testsupport.NewFactory()
	k := alertSite(t, f, "forced_open")
	f.EnvStub["DOORCODE_ALERT_HOLD_DOWN_MINUTES"] = "soon"

	k.opens(false)

	got := []any{lastLog(f), subjects(t, f), k.state("/door")}
	if diff := cmp.Diff([]any{spoiledHoldDown, []any{}, "open"}, got); diff != "" {
		t.Errorf("log, subjects, door state (-want +got):\n%s", diff)
	}
}

func TestAClearingWhoseSettingsCannotBeReadIsLoggedAndTheDoorStillCloses(t *testing.T) {
	t.Parallel()
	f := testsupport.NewFactory()
	k := alertSite(t, f, "forced_open")
	k.opens(false)
	f.EnvStub["DOORCODE_ALERT_HOLD_DOWN_MINUTES"] = "soon"

	k.says("closed")

	got := []any{lastLog(f), subjects(t, f), k.state("/door")}
	if diff := cmp.Diff([]any{spoiledHoldDown, []any{forcedSubject}, "closed"}, got); diff != "" {
		t.Errorf("log, subjects, door state (-want +got):\n%s", diff)
	}
}

func TestAClearedMailTheStoreCannotQueueIsLogged(t *testing.T) {
	t.Parallel()
	f := testsupport.NewFactory()
	k := alertSite(t, f, "forced_open")
	k.opens(false)
	f.StoreStub.FailAlertWrites()

	k.says("closed")

	if diff := cmp.Diff(notFollowed("forced_open"), lastLog(f)); diff != "" {
		t.Errorf("log (-want +got):\n%s", diff)
	}
}

func TestAKindOfAlertIsEnabledAndDisabledThroughTheAPI(t *testing.T) {
	t.Parallel()
	b := signedInAdmin(t, testsupport.NewFactory())

	on := send(t, b, "PUT", "/api/v1/alerts/kinds/held_open", `{"enabled":true}`, 200)
	shown := testsupport.JSONAt(send(t, b, "GET", "/api/v1/alerts", "", 200).Body, "/kinds/1/enabled")
	off := send(t, b, "PUT", "/api/v1/alerts/kinds/held_open", `{"enabled":false}`, 200)

	want := []any{testsupport.JSON(`{"key":"held_open","name":"Door held open","about":"A door stood open for longer than its held-open time while it should have been locked. It clears when the door closes or an open span of its timeline begins.","enabled":true}`),
		true, false}
	if diff := cmp.Diff(want, []any{testsupport.JSONAt(on.Body, ""), shown, testsupport.JSONAt(off.Body, "/enabled")}); diff != "" {
		t.Errorf("enabled, shown, disabled (-want +got):\n%s", diff)
	}
}

func TestEnablingAKindOfAlertIsRefusedWhenItCannotBe(t *testing.T) {
	t.Parallel()
	cases := map[string]struct {
		target, body string
		fail         bool
		want         testsupport.Response
	}{
		"an unknown kind":        {"/api/v1/alerts/kinds/door_on_fire", `{"enabled":true}`, false, apiError(404, "no kind of alert has this key: door_on_fire")},
		"no enabled":             {"/api/v1/alerts/kinds/held_open", `{}`, false, apiError(400, "body must be a JSON object with enabled true or false")},
		"a body that is no JSON": {"/api/v1/alerts/kinds/held_open", `on`, false, apiError(400, "body must be a JSON object with enabled true or false")},
		"a store that fails":     {"/api/v1/alerts/kinds/held_open", `{"enabled":true}`, true, internalError("767778797a7b7c7d")},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			f := testsupport.NewFactory()
			b := signedInAdmin(t, f)
			if c.fail {
				f.StoreStub.FailAlertWrites()
			}

			got := b.Send("PUT", c.target, c.body)

			if diff := cmp.Diff(c.want, got, errorBodies); diff != "" {
				t.Errorf("response (-want +got):\n%s", diff)
			}
		})
	}
}

func TestTheAlertsAPIFailsWhenTheStoreDoes(t *testing.T) {
	t.Parallel()
	cases := map[string]func(s *testsupport.Store){
		"the alerts":   (*testsupport.Store).FailAlerts,
		"the settings": (*testsupport.Store).FailSettings,
	}
	for name, fail := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			f := testsupport.NewFactory()
			b := signedInAdmin(t, f)
			fail(f.StoreStub)

			got := b.Get("/api/v1/alerts")

			if diff := cmp.Diff(500, got.Status); diff != "" {
				t.Errorf("status (-want +got):\n%s", diff)
			}
		})
	}
}

func TestAlertRecipientsAreReadAsBareAddresses(t *testing.T) {
	t.Parallel()
	cases := map[string]struct {
		value string
		want  []any
	}{
		"one address":                       {"ops@example.com", []any{"ops@example.com"}},
		"with names, by commas":             {"Ops <ops@example.com>, Front desk <desk@example.com>", []any{"ops@example.com", "desk@example.com"}},
		"one a line, with CR LF and spaces": {" ops@example.com \r\n\r\n desk@example.com\r\n", []any{"ops@example.com", "desk@example.com"}},
		"an empty entry between commas":     {"ops@example.com, ,desk@example.com", []any{"ops@example.com", "desk@example.com"}},
		"none":                              {"", []any{}},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			b := signedInAdmin(t, testsupport.NewFactory())
			send(t, b, "PUT", "/api/v1/settings/alert_recipients", `{"value":`+testsupport.Quoted(c.value)+`}`, 200)

			got := testsupport.JSONAt(send(t, b, "GET", "/api/v1/alerts", "", 200).Body, "/recipients")

			if diff := cmp.Diff(c.want, got); diff != "" {
				t.Errorf("recipients (-want +got):\n%s", diff)
			}
		})
	}
}

func TestAlertSettingsRefuseWhatTheyCannotUse(t *testing.T) {
	t.Parallel()
	recipientsHint := "must be email addresses separated by commas or new lines, such as ops@example.com, Front desk <desk@example.com>"
	cases := map[string]struct{ key, value, refusal string }{
		"a recipient with no address":   {"alert_recipients", "ops@example.com, Front desk", recipientsHint},
		"recipients on a line together": {"alert_recipients", "ops@example.com desk@example.com", recipientsHint},
		"mail when cleared of maybe":    {"alert_mail_cleared", "maybe", "must be true or false"},
		"a hold-down below 0":           {"alert_hold_down_minutes", "-1", "must be a whole number of minutes from 0 to 10080"},
		"a hold-down above a week":      {"alert_hold_down_minutes", "10081", "must be a whole number of minutes from 0 to 10080"},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			got := signedInAdmin(t, testsupport.NewFactory()).PutJSON("/api/v1/settings/"+c.key, `{"value":`+testsupport.Quoted(c.value)+`}`)

			if diff := cmp.Diff(apiError(400, "invalid value for "+c.key+": "+c.refusal), got, errorBodies); diff != "" {
				t.Errorf("response (-want +got):\n%s", diff)
			}
		})
	}
}

func TestAlertSettingsTakeTheEdgesOfWhatTheyCanUse(t *testing.T) {
	t.Parallel()
	for _, value := range []string{"0", "10080"} {
		t.Run(value, func(t *testing.T) {
			t.Parallel()
			b := signedInAdmin(t, testsupport.NewFactory())
			send(t, b, "PUT", "/api/v1/settings/alert_hold_down_minutes", `{"value":"`+value+`"}`, 200)

			got := testsupport.JSONAt(send(t, b, "GET", "/api/v1/alerts", "", 200).Body, "/hold_down_minutes")

			if diff := cmp.Diff(testsupport.JSON(value), got); diff != "" {
				t.Errorf("hold-down (-want +got):\n%s", diff)
			}
		})
	}
}
