package entrypoint_test

import (
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/dakriy/doorcode/internal/testsupport"
)

// alertSite is a keypad site whose mail goes through smtp.example.com as
// mailSite sets it up, whose alerts mail ops@example.com and Front desk
// <desk@example.com>, and where each kind of alert in kinds is set to
// mail.
func alertSite(t *testing.T, f *testsupport.Factory, kinds ...string) *keypad {
	t.Helper()
	k := keypadSite(t, f)
	mailAlerts(t, k.b, kinds...)
	return k
}

// mailAlerts has the administrator's client c send mail through
// smtp.example.com, mail alerts to ops@example.com and Front desk
// <desk@example.com>, and set each kind of alert in kinds to mail.
func mailAlerts(t *testing.T, c *testsupport.Browser, kinds ...string) {
	t.Helper()
	for _, setting := range [][2]string{
		{"mail_host", "smtp.example.com"}, {"mail_port", "587"}, {"mail_security", "starttls"},
		{"mail_from", "Doorcode <doorcode@example.com>"},
		{"alert_recipients", "ops@example.com,\nFront desk <desk@example.com>"},
	} {
		putSetting(t, c, setting[0], setting[1])
	}
	for _, kind := range kinds {
		send(t, c, "PUT", "/api/v1/alerts/kinds/"+kind, `{"state":"mail"}`, 200)
	}
}

// putSetting stores value as the setting key through the API with c.
func putSetting(t *testing.T, c *testsupport.Browser, key, value string) {
	t.Helper()
	send(t, c, "PUT", "/api/v1/settings/"+key, `{"value":`+testsupport.Quoted(value)+`}`, 200)
}

// setting stores value as the setting key through the API.
func (k *keypad) setting(key, value string) {
	k.t.Helper()
	putSetting(k.t, k.b, key, value)
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
	return "\nThis mail goes to every alert recipient while the alert " + kind + " is set to mail on the Alerts tab of the settings.\n"
}

// heldSubject is the subject of the mail about Lobby being held open.
const heldSubject = "Door held open: Lobby"

// heldText is the text of the mail raising Lobby held open at at.
func heldText(at string) string {
	return "Door held open at Lobby, " + at + ".\n\nOpen for longer than 60 seconds.\n" + alertFooter("Door held open")
}

// holdsOpen has Lobby's door open at the RFC 3339 time opened
// and the server do its periodic work at raised, a minute later, once the
// door has stood open for its held-open time.
func (k *keypad) holdsOpen(opened, raised string) {
	k.t.Helper()
	k.at(opened)
	k.opens(true)
	k.sweepAt(raised)
}

// cleared is the subject of the mail saying the condition whose raise had
// subject cleared.
func cleared(subject string) string { return "Cleared: " + subject }

func TestEveryKindOfAlertIsOnUntilSetOtherwiseAndMailsNobodyByDefault(t *testing.T) {
	t.Parallel()
	b := signedInAdmin(t, testsupport.NewFactory())

	got := testsupport.JSONAt(send(t, b, "GET", "/api/v1/alerts", "", 200).Body, "")

	want := testsupport.JSON(`{"kinds":[
		{"key":"held_open","name":"Door held open","about":"A door stood open for longer than its held-open time while it should have been locked. It clears when the door closes or an open span of its timeline begins.","state":"on"},
		{"key":"offline","name":"Door offline","about":"A door's controller has not been connected to the broker for 5 minutes. It clears once the controller connects again.","state":"on"},
		{"key":"unresponsive","name":"Door unresponsive","about":"A door's controller has not been heard from for 5 minutes while the broker says it is connected. It clears once the server hears from it or it is shown offline.","state":"on"},
		{"key":"config_reverted","name":"Door configuration reverted","about":"A door gave up the configuration it was last sent, because it could not come online with it, and runs the one before. It clears once the door no longer shows the Reverted configuration badge: it reports the configuration it was last sent, or is sent another.","state":"on"},
		{"key":"config_restarting","name":"Door restarting with its configuration","about":"A door's controller started at least twice since the configuration it was last sent went out, with no report naming that configuration. It clears once the door no longer shows the Restarting with configuration badge.","state":"on"},
		{"key":"config_mismatch","name":"Door configuration mismatched","about":"A door's controller went on reporting a configuration other than the one it was last sent. It clears once the door no longer shows the Mismatched configuration badge.","state":"on"},
		{"key":"update_failed","name":"Door firmware update failed","about":"A door's firmware update failed: the door said it could not install it, or it did not connect again with it in time. It clears once the door's firmware no longer shows Update failed: an update of it is under way again, it runs its target, or its target changes.","state":"on"},
		{"key":"certificate","name":"Certificate","about":"` + certificateAbout + `","state":"on"}],
		"recipients":[],"mail_cleared":true,"hold_down_minutes":15,"outbox":[]}`)
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("alerts (-want +got):\n%s", diff)
	}
}

func TestAlertMailComesFromTheSiteToEachRecipient(t *testing.T) {
	t.Parallel()
	f := testsupport.NewFactory()
	k := alertSite(t, f, "held_open")

	k.holdsOpen("2026-01-05T09:00:00Z", "2026-01-05T09:01:00Z")

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

func TestOnlyTheKindsOfAlertSetToMailMail(t *testing.T) {
	t.Parallel()
	f := testsupport.NewFactory()
	k := alertSite(t, f, "held_open")

	k.holdsOpen("2026-01-05T09:00:00Z", "2026-01-05T09:01:00Z")
	k.sweepAt("2026-01-05T09:05:00Z", "2026-01-05T09:06:00Z")

	if diff := cmp.Diff([]any{heldSubject}, subjects(t, f)); diff != "" {
		t.Errorf("subjects (-want +got):\n%s", diff)
	}
}

func TestAClearedConditionMailsOnlyWhenItsRaiseWasMailedItsKindIsStillSetToMailAndTheSettingSaysSo(t *testing.T) {
	t.Parallel()
	cases := map[string]struct {
		before, between func(k *keypad)
	}{
		"the mail when cleared setting off": {
			before:  func(k *keypad) { k.setting("alert_mail_cleared", "false") },
			between: func(*keypad) {},
		},
		"its kind set on after it was raised": {
			before: func(*keypad) {},
			between: func(k *keypad) {
				send(k.t, k.b, "PUT", "/api/v1/alerts/kinds/held_open", `{"state":"on"}`, 200)
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
			k := alertSite(t, f, "held_open")
			c.before(k)
			k.holdsOpen("2026-01-05T09:00:00Z", "2026-01-05T09:01:00Z")
			c.between(k)

			k.says("closed")

			if diff := cmp.Diff([]any{heldSubject}, subjects(t, f)); diff != "" {
				t.Errorf("subjects (-want +got):\n%s", diff)
			}
		})
	}
}

func TestAConditionThatComesBackMailsAgainOnlyOnceItStayedClearForTheHoldDown(t *testing.T) {
	t.Parallel()
	cases := map[string]struct {
		holdDown, opened, back string
		want                   []any
	}{
		"back a second before the default 15 minutes": {"15", "2026-01-05T09:15:19Z", "2026-01-05T09:16:19Z", []any{heldSubject, cleared(heldSubject)}},
		"back after the default 15 minutes":           {"15", "2026-01-05T09:15:20Z", "2026-01-05T09:16:20Z", []any{heldSubject, cleared(heldSubject), heldSubject}},
		"back at once with no hold-down":              {"0", "2026-01-05T09:01:20Z", "2026-01-05T09:02:20Z", []any{heldSubject, cleared(heldSubject), heldSubject}},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			f := testsupport.NewFactory()
			k := alertSite(t, f, "held_open")
			k.setting("alert_hold_down_minutes", c.holdDown)
			k.holdsOpen("2026-01-05T09:00:00Z", "2026-01-05T09:01:00Z")
			k.at("2026-01-05T09:01:20Z")
			k.says("closed")

			k.holdsOpen(c.opened, c.back)

			if diff := cmp.Diff(c.want, subjects(t, f)); diff != "" {
				t.Errorf("subjects (-want +got):\n%s", diff)
			}
		})
	}
}

func TestAConditionThatCameBackQuietlyClearsQuietlyAndTheHoldDownCountsFromThen(t *testing.T) {
	t.Parallel()
	f := testsupport.NewFactory()
	k := alertSite(t, f, "held_open")
	k.holdsOpen("2026-01-05T09:00:00Z", "2026-01-05T09:01:00Z")
	k.at("2026-01-05T09:01:20Z")
	k.says("closed")
	k.holdsOpen("2026-01-05T09:10:00Z", "2026-01-05T09:11:00Z")
	k.at("2026-01-05T09:11:20Z")
	k.says("closed")

	k.holdsOpen("2026-01-05T09:25:19Z", "2026-01-05T09:26:19Z")
	k.at("2026-01-05T09:26:30Z")
	k.says("closed")
	k.holdsOpen("2026-01-05T09:40:30Z", "2026-01-05T09:41:30Z")

	want := []any{heldSubject, cleared(heldSubject), heldSubject}
	if diff := cmp.Diff([]any{want, heldText("Mon 5 Jan 2026 09:41:30 UTC")},
		[]any{subjects(t, f), mailed(t, f)[2][2]}); diff != "" {
		t.Errorf("subjects, last text (-want +got):\n%s", diff)
	}
}

func TestAClosingWithNothingRaisedLeavesTheHoldDownCountingFromTheLastClearing(t *testing.T) {
	t.Parallel()
	f := testsupport.NewFactory()
	k := alertSite(t, f, "held_open")
	k.holdsOpen("2026-01-05T09:00:00Z", "2026-01-05T09:01:00Z")
	k.at("2026-01-05T09:01:20Z")
	k.says("closed")
	k.at("2026-01-05T09:10:00Z")
	k.opens(true)
	k.at("2026-01-05T09:10:10Z")
	k.says("closed")

	k.holdsOpen("2026-01-05T09:15:20Z", "2026-01-05T09:16:20Z")

	if diff := cmp.Diff([]any{heldSubject, cleared(heldSubject), heldSubject}, subjects(t, f)); diff != "" {
		t.Errorf("subjects (-want +got):\n%s", diff)
	}
}

// refused is the error the outbox shows of a message the stub mail server
// refused with 421 Try again later.
const refused = "the mail server could not be reached or refused the message: 421 Try again later"

func TestMailTheServerRefusesIsTriedAgainAfterAPauseThatDoublesUntilItFails(t *testing.T) {
	t.Parallel()
	f := testsupport.NewFactory()
	k := alertSite(t, f, "held_open")
	f.MailerStub.FailWith("421 Try again later")
	k.holdsOpen("2026-01-05T09:00:00Z", "2026-01-05T09:01:00Z")

	var got []any
	for _, at := range []string{
		"2026-01-05T09:01:59Z", "2026-01-05T09:02:00Z", "2026-01-05T09:04:00Z", "2026-01-05T09:08:00Z", "2026-01-05T09:16:00Z",
		"2026-01-05T09:31:59Z", "2026-01-05T09:32:00Z", "2026-01-05T10:32:00Z",
	} {
		k.runAt(at)
		got = append(got, []any{k.outbox("/0/status"), k.outbox("/0/attempts"), k.outbox("/0/next_attempt_at"), k.outbox("/0/last_attempt_at")})
	}

	want := []any{
		[]any{"pending", 1.0, "2026-01-05T09:02:00Z", "2026-01-05T09:01:00Z"},
		[]any{"pending", 2.0, "2026-01-05T09:04:00Z", "2026-01-05T09:02:00Z"},
		[]any{"pending", 3.0, "2026-01-05T09:08:00Z", "2026-01-05T09:04:00Z"},
		[]any{"pending", 4.0, "2026-01-05T09:16:00Z", "2026-01-05T09:08:00Z"},
		[]any{"pending", 5.0, "2026-01-05T09:32:00Z", "2026-01-05T09:16:00Z"},
		[]any{"pending", 5.0, "2026-01-05T09:32:00Z", "2026-01-05T09:16:00Z"},
		[]any{"failed", 6.0, nil, "2026-01-05T09:32:00Z"},
		[]any{"failed", 6.0, nil, "2026-01-05T09:32:00Z"},
	}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("status, attempts, next attempt, last attempt after each run (-want +got):\n%s", diff)
	}
}

func TestAFailedMessageKeepsTheLastErrorItWasRefusedWith(t *testing.T) {
	t.Parallel()
	f := testsupport.NewFactory()
	k := alertSite(t, f, "held_open")
	f.MailerStub.FailWith("421 Try again later")
	k.holdsOpen("2026-01-05T09:00:00Z", "2026-01-05T09:01:00Z")

	for _, at := range []string{"2026-01-05T09:02:00Z", "2026-01-05T09:04:00Z", "2026-01-05T09:08:00Z", "2026-01-05T09:16:00Z"} {
		k.runAt(at)
	}
	f.MailerStub.FailWith("550 Mailbox unavailable")
	k.runAt("2026-01-05T09:32:00Z")

	want := testsupport.JSON(`{"id":1,"kind":"held_open","subject":"Door held open: Lobby",
		"text":"Door held open at Lobby, Mon 5 Jan 2026 09:01:00 UTC.\n\nOpen for longer than 60 seconds.\n\nThis mail goes to every alert recipient while the alert Door held open is set to mail on the Alerts tab of the settings.\n",
		"to":["ops@example.com","desk@example.com"],"status":"failed","attempts":6,"created_at":"2026-01-05T09:01:00Z",
		"last_attempt_at":"2026-01-05T09:32:00Z",
		"last_error":"the mail server could not be reached or refused the message: 550 Mailbox unavailable"}`)
	if diff := cmp.Diff(want, k.outbox("/0")); diff != "" {
		t.Errorf("message (-want +got):\n%s", diff)
	}
}

func TestMailTheServerTakesOnARetryIsSentWithoutAnError(t *testing.T) {
	t.Parallel()
	f := testsupport.NewFactory()
	k := alertSite(t, f, "held_open")
	f.MailerStub.FailWith("421 Try again later")
	k.holdsOpen("2026-01-05T09:00:00Z", "2026-01-05T09:01:00Z")
	before := k.outbox("/0/last_error")
	f.MailerStub.Fail(nil)

	k.runAt("2026-01-05T09:02:00Z")

	got := []any{before, k.outbox("/0/status"), k.outbox("/0/attempts"), k.outbox("/0/sent_at"), k.outbox("/0/last_error"), subjects(t, f)}
	if diff := cmp.Diff([]any{refused, "sent", 2.0, "2026-01-05T09:02:00Z", nil, []any{heldSubject}}, got); diff != "" {
		t.Errorf("error before, status, attempts, sent, error, subjects (-want +got):\n%s", diff)
	}
}

func TestTheEarliestMessageWaitingIsTriedWhenItIsDue(t *testing.T) {
	t.Parallel()
	f := testsupport.NewFactory()
	k := alertSite(t, f, "held_open")
	f.MailerStub.FailWith("421 Try again later")
	k.holdsOpen("2026-01-05T09:00:00Z", "2026-01-05T09:01:00Z")
	k.at("2026-01-05T09:01:30Z")
	k.says("closed")

	k.runAt("2026-01-05T09:02:00Z")

	got := []any{k.outbox("/0/attempts"), k.outbox("/0/next_attempt_at"), k.outbox("/1/attempts"), k.outbox("/1/next_attempt_at")}
	if diff := cmp.Diff([]any{1.0, "2026-01-05T09:02:30Z", 2.0, "2026-01-05T09:04:00Z"}, got); diff != "" {
		t.Errorf("cleared mail's attempts and next, raised mail's attempts and next (-want +got):\n%s", diff)
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
	k := alertSite(t, f, "held_open")
	f.MailerStub.FailWith("421 Try again later")
	k.holdsOpen("2026-01-05T09:00:00Z", "2026-01-05T09:01:00Z")
	k.at("2026-01-05T09:01:30Z")
	k.says("closed")
	f.MailerStub.Fail(nil)
	f.StoreStub.FailAlerts()
	k.runAt("2026-01-05T09:02:00Z")
	logged := lastLogged(f, "mail delivery failed")
	f.StoreStub.Recover()

	k.runAt("2026-01-05T09:02:59Z")
	early := k.outbox("/1/attempts")
	k.runAt("2026-01-05T09:03:00Z")

	want := []any{testsupport.LogEntry{Level: "error", Message: "mail delivery failed", Fields: map[string]string{"error": "stub: unavailable"}},
		1.0, "sent", "2026-01-05T09:03:00Z"}
	if diff := cmp.Diff(want, []any{logged, early, k.outbox("/1/status"), k.outbox("/1/sent_at")}); diff != "" {
		t.Errorf("log, attempts before the minute, status, sent (-want +got):\n%s", diff)
	}
}

func TestADeliveryThatCannotUpdateTheOutboxIsLogged(t *testing.T) {
	t.Parallel()
	f := testsupport.NewFactory()
	k := alertSite(t, f, "held_open")
	f.MailerStub.FailWith("421 Try again later")
	k.holdsOpen("2026-01-05T09:00:00Z", "2026-01-05T09:01:00Z")
	k.at("2026-01-05T09:01:30Z")
	k.says("closed")
	f.StoreStub.FailAlertWrites()

	k.runAt("2026-01-05T09:02:00Z")

	want := testsupport.LogEntry{Level: "error", Message: "mail delivery failed", Fields: map[string]string{"error": "stub: unavailable"}}
	if diff := cmp.Diff(want, lastLogged(f, "mail delivery failed")); diff != "" {
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
	unlisted := testsupport.LogEntry{Level: "error", Message: "periodic work failed",
		Fields: map[string]string{"error": "follow the doors' alert conditions: read the alert conditions raised: stub: unavailable\n" +
			"follow the alerts of updates failing: read the alert conditions raised: stub: unavailable"}}
	cases := map[string]struct {
		kinds  []string
		fail   func(s *testsupport.Store)
		logged testsupport.LogEntry
	}{
		"when it cannot be read":                            {[]string{"held_open"}, (*testsupport.Store).FailAlerts, unlisted},
		"when its mail cannot be queued":                    {[]string{"held_open"}, (*testsupport.Store).FailAlertWrites, notFollowed("held_open")},
		"when its condition cannot be stored, mailing none": {nil, (*testsupport.Store).FailAlertWrites, notFollowed("held_open")},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			f := testsupport.NewFactory()
			k := alertSite(t, f, c.kinds...)
			k.opens(true)
			c.fail(f.StoreStub)

			k.sweepAt("2026-01-05T09:01:00Z")

			got := []any{lastLogged(f, c.logged.Message), k.reasons(), k.state("/held_open")}
			if diff := cmp.Diff([]any{c.logged, []any{"held open", "opened"}, true}, got); diff != "" {
				t.Errorf("log, reasons, held open (-want +got):\n%s", diff)
			}
		})
	}
}

func TestAClearingTheStoreCannotFollowIsLoggedAndTheDoorStillCloses(t *testing.T) {
	t.Parallel()
	f := testsupport.NewFactory()
	k := alertSite(t, f, "held_open")
	k.holdsOpen("2026-01-05T09:00:00Z", "2026-01-05T09:01:00Z")
	f.StoreStub.FailAlerts()

	k.says("closed")

	logged := lastLogged(f, "alert condition not followed")

	got := []any{logged, k.state("/door")}
	if diff := cmp.Diff([]any{notFollowed("held_open"), "closed"}, got); diff != "" {
		t.Errorf("logs, door state (-want +got):\n%s", diff)
	}
}

func TestAClosingTheStoreCouldNotFollowClearsItsConditionAtTheNextSweep(t *testing.T) {
	t.Parallel()
	f := testsupport.NewFactory()
	k := alertSite(t, f, "held_open")
	k.holdsOpen("2026-01-05T09:00:00Z", "2026-01-05T09:01:00Z")
	f.StoreStub.FailAlerts()
	k.at("2026-01-05T09:01:20Z")
	k.says("closed")
	f.StoreStub.Recover()
	raised := subjects(t, f)

	k.sweepAt("2026-01-05T09:02:00Z")

	want := []any{[]any{heldSubject}, []any{recipients, cleared(heldSubject), "Door held open at Lobby cleared, Mon 5 Jan 2026 09:02:00 UTC.\n\n" +
		"It was raised Mon 5 Jan 2026 09:01:00 UTC.\n" + alertFooter("Door held open")}}
	if diff := cmp.Diff(want, []any{raised, mailed(t, f)[1]}); diff != "" {
		t.Errorf("subjects after the closing, mail at the sweep (-want +got):\n%s", diff)
	}
}

// spoiledHoldDown is what is logged while the environment gives a
// hold-down that is no number.
var spoiledHoldDown = notFollowedWith("held_open", "invalid value for alert_hold_down_minutes: must be a whole number of minutes from 0 to 10080")

func TestARaiseWhoseSettingsCannotBeReadIsLoggedAndTheDoorStillOpens(t *testing.T) {
	t.Parallel()
	f := testsupport.NewFactory()
	k := alertSite(t, f, "held_open")
	f.EnvStub["DOORCODE_ALERT_HOLD_DOWN_MINUTES"] = "soon"

	k.holdsOpen("2026-01-05T09:00:00Z", "2026-01-05T09:01:00Z")

	got := []any{lastLogged(f, "alert condition not followed"), subjects(t, f), k.state("/door")}
	if diff := cmp.Diff([]any{spoiledHoldDown, []any{}, "open"}, got); diff != "" {
		t.Errorf("log, subjects, door state (-want +got):\n%s", diff)
	}
}

func TestAClearingWhoseSettingsCannotBeReadIsLoggedAndTheDoorStillCloses(t *testing.T) {
	t.Parallel()
	f := testsupport.NewFactory()
	k := alertSite(t, f, "held_open")
	k.holdsOpen("2026-01-05T09:00:00Z", "2026-01-05T09:01:00Z")
	f.EnvStub["DOORCODE_ALERT_HOLD_DOWN_MINUTES"] = "soon"

	k.says("closed")

	got := []any{lastLogged(f, "alert condition not followed"), subjects(t, f), k.state("/door")}
	if diff := cmp.Diff([]any{spoiledHoldDown, []any{heldSubject}, "closed"}, got); diff != "" {
		t.Errorf("log, subjects, door state (-want +got):\n%s", diff)
	}
}

func TestAClearedMailTheStoreCannotQueueIsLogged(t *testing.T) {
	t.Parallel()
	f := testsupport.NewFactory()
	k := alertSite(t, f, "held_open")
	k.holdsOpen("2026-01-05T09:00:00Z", "2026-01-05T09:01:00Z")
	f.StoreStub.FailAlertWrites()

	k.says("closed")

	if diff := cmp.Diff(notFollowed("held_open"), lastLogged(f, "alert condition not followed")); diff != "" {
		t.Errorf("log (-want +got):\n%s", diff)
	}
}

func TestAKindOfAlertIsSetOffOnOrToMailThroughTheAPI(t *testing.T) {
	t.Parallel()
	b := signedInAdmin(t, testsupport.NewFactory())

	mail := send(t, b, "PUT", "/api/v1/alerts/kinds/held_open", `{"state":"mail"}`, 200)
	shown := testsupport.JSONAt(send(t, b, "GET", "/api/v1/alerts", "", 200).Body, "/kinds/0/state")
	off := send(t, b, "PUT", "/api/v1/alerts/kinds/held_open", `{"state":"off"}`, 200)
	on := send(t, b, "PUT", "/api/v1/alerts/kinds/held_open", `{"state":"on"}`, 200)

	want := []any{testsupport.JSON(`{"key":"held_open","name":"Door held open","about":"A door stood open for longer than its held-open time while it should have been locked. It clears when the door closes or an open span of its timeline begins.","state":"mail"}`),
		"mail", "off", "on"}
	got := []any{testsupport.JSONAt(mail.Body, ""), shown, testsupport.JSONAt(off.Body, "/state"), testsupport.JSONAt(on.Body, "/state")}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("set to mail, shown, set off, set on (-want +got):\n%s", diff)
	}
}

func TestSettingAKindOfAlertIsRefusedWhenItCannotBe(t *testing.T) {
	t.Parallel()
	works := func(*testsupport.Store) {}
	cases := map[string]struct {
		target, body string
		fail         func(s *testsupport.Store)
		want         testsupport.Response
	}{
		"an unknown kind":        {"/api/v1/alerts/kinds/door_on_fire", `{"state":"mail"}`, works, apiError(404, "no kind of alert has this key: door_on_fire")},
		"no state":               {"/api/v1/alerts/kinds/held_open", `{}`, works, apiError(400, "a kind of alert is off, on, or mail")},
		"a state there is not":   {"/api/v1/alerts/kinds/held_open", `{"state":"loud"}`, works, apiError(400, "a kind of alert is off, on, or mail")},
		"a state that is a flag": {"/api/v1/alerts/kinds/held_open", `{"state":true}`, works, apiError(400, "body must be a JSON object with state off, on, or mail")},
		"a body that is no JSON": {"/api/v1/alerts/kinds/held_open", `on`, works, apiError(400, "body must be a JSON object with state off, on, or mail")},
		"a store that cannot store it": {"/api/v1/alerts/kinds/held_open", `{"state":"mail"}`, (*testsupport.Store).FailAlertWrites,
			internalError("767778797a7b7c7d")},
		"a store whose conditions cannot be read, setting it off": {"/api/v1/alerts/kinds/held_open", `{"state":"off"}`, (*testsupport.Store).FailAlerts,
			internalError("767778797a7b7c7d")},
		"a store whose settings cannot be read": {"/api/v1/alerts/kinds/offline", `{"state":"mail"}`, (*testsupport.Store).FailSettings,
			internalError("767778797a7b7c7d")},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			f := testsupport.NewFactory()
			b := signedInAdmin(t, f)
			c.fail(f.StoreStub)

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
