package entrypoint_test

import (
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/dakriy/doorcode/internal/testsupport"
)

// unsendable is each way alertSite's mail about Lobby held open cannot
// be sent, block, the save that lets it be sent again, allow, and what the
// Alerts page says of the raise's mail in between, waiting.
var unsendable = map[string]struct {
	block, allow func(k *keypad)
	waiting      string
}{
	"mail not set up, then set up through the API": {
		block:   func(k *keypad) { k.setting("mail_host", "") },
		allow:   func(k *keypad) { k.setting("mail_host", "smtp.example.com") },
		waiting: "Waiting: mailed once it can be",
	},
	"mail not set up, then set up on the settings page": {
		block:   func(k *keypad) { k.setting("mail_host", "") },
		allow:   func(k *keypad) { k.saved(k.b.Post("/admin/settings", "mail_host", "smtp.example.com")) },
		waiting: "Waiting: mailed once it can be",
	},
	"no recipients, then recipients given": {
		block:   func(k *keypad) { k.setting("alert_recipients", "") },
		allow:   func(k *keypad) { k.setting("alert_recipients", "ops@example.com,\nFront desk <desk@example.com>") },
		waiting: "Waiting: mailed once it can be",
	},
}

// setKind sets the kind of alert whose key is kind to state through the
// API.
func (k *keypad) setKind(kind, state string) {
	k.t.Helper()
	send(k.t, k.b, "PUT", "/api/v1/alerts/kinds/"+kind, `{"state":"`+state+`"}`, 200)
}

// saved fails the test unless form, a form's answer, says it was saved.
func (k *keypad) saved(form testsupport.Response) {
	k.t.Helper()
	if form.Status != 303 {
		k.t.Fatalf("form answered %d, not 303", form.Status)
	}
}

// raised is the Alerts page's table of the conditions raised now.
func (k *keypad) raised() [][]string {
	k.t.Helper()
	return pageOf(k.b.Get("/admin/alerts")).Tables["Raised now"]
}

func TestARaiseThatCouldNotBeMailedIsListedAndMailedOnceMailCanBeSent(t *testing.T) {
	t.Parallel()
	for name, c := range unsendable {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			f := testsupport.NewFactory()
			k := alertSite(t, f, "held_open")
			c.block(k)
			k.holdsOpen("2026-01-05T09:00:00Z", "2026-01-05T09:01:00Z")
			waiting := k.raised()
			k.at("2026-01-05T09:30:00Z")
			c.allow(k)
			run(t, k.a)

			k.runAt("2026-01-05T09:31:00Z")

			// The door going silent raises more by 09:30, listed after
			// held_open; only its row is compared at the end.
			want := []any{
				[][]string{raisedHead, {"Door held open", "Lobby", "Mon 5 Jan 2026 09:01:00", c.waiting}},
				[][]any{{recipients, heldSubject, heldText("Mon 5 Jan 2026 09:01:00 UTC")}},
				"2026-01-05T09:30:00Z",
				[][]string{raisedHead, {"Door held open", "Lobby", "Mon 5 Jan 2026 09:01:00", "Mailed"}},
			}
			if diff := cmp.Diff(want, []any{waiting, mailed(t, f), k.outbox("/0/created_at"), k.raised()[:2]}); diff != "" {
				t.Errorf("raised while waiting, mail, when it was queued, raised after (-want +got):\n%s", diff)
			}
		})
	}
}

func TestARaiseThatClearedBeforeItCouldBeMailedIsNeverMailedAndHoldsNothingDown(t *testing.T) {
	t.Parallel()
	for name, c := range unsendable {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			f := testsupport.NewFactory()
			k := alertSite(t, f, "held_open")
			c.block(k)
			k.holdsOpen("2026-01-05T09:00:00Z", "2026-01-05T09:01:00Z")
			k.at("2026-01-05T09:20:00Z")
			k.says("closed")
			k.at("2026-01-05T09:25:00Z")
			c.allow(k)
			run(t, k.a)

			k.holdsOpen("2026-01-05T09:30:00Z", "2026-01-05T09:31:00Z")

			if diff := cmp.Diff([][]any{{recipients, heldSubject, heldText("Mon 5 Jan 2026 09:31:00 UTC")}}, mailed(t, f)); diff != "" {
				t.Errorf("mail (-want +got):\n%s", diff)
			}
		})
	}
}

func TestARaiseMailedOnceItCouldBeIsFollowedByItsClearedMail(t *testing.T) {
	t.Parallel()
	f := testsupport.NewFactory()
	k := alertSite(t, f, "held_open")
	k.setting("alert_recipients", "")
	k.holdsOpen("2026-01-05T09:00:00Z", "2026-01-05T09:01:00Z")
	k.at("2026-01-05T09:30:00Z")
	k.setting("alert_recipients", "ops@example.com,\nFront desk <desk@example.com>")
	run(t, k.a)

	k.at("2026-01-05T09:40:00Z")
	k.says("closed")

	want := [][]any{
		{recipients, heldSubject, heldText("Mon 5 Jan 2026 09:01:00 UTC")},
		{recipients, cleared(heldSubject), "Door held open at Lobby cleared, Mon 5 Jan 2026 09:40:00 UTC.\n\n" +
			"It was raised Mon 5 Jan 2026 09:01:00 UTC.\n" + alertFooter("Door held open")},
	}
	if diff := cmp.Diff(want, mailed(t, f)); diff != "" {
		t.Errorf("mail (-want +got):\n%s", diff)
	}
}

func TestMailWaitingWhileMailIsNotSetUpUsesNoAttemptAndGoesOutOnceItIs(t *testing.T) {
	t.Parallel()
	f := testsupport.NewFactory()
	k := alertSite(t, f, "held_open")
	f.MailerStub.FailWith("421 Try again later")
	k.holdsOpen("2026-01-05T09:00:00Z", "2026-01-05T09:01:00Z")
	k.setting("mail_host", "")
	var during []any
	for _, at := range []string{"2026-01-05T09:02:00Z", "2026-01-05T09:32:00Z", "2026-01-05T10:32:00Z"} {
		k.runAt(at)
		during = append(during, []any{k.outbox("/0/status"), k.outbox("/0/attempts")})
	}
	page := pageOf(k.b.Get("/admin/alerts"))
	f.MailerStub.Fail(nil)
	k.at("2026-01-05T10:40:00Z")

	k.setting("mail_host", "smtp.example.com")
	run(t, k.a)

	want := []any{
		[]any{[]any{"pending", 1.0}, []any{"pending", 1.0}, []any{"pending", 1.0}},
		[]string{mailNotSetUpNote, nothingToCheck},
		[]string{"Mon 5 Jan 2026 09:01:00", heldSubject, "ops@example.com, desk@example.com", "Waiting for mail to be set up", "1", refused},
		[]any{"sent", 2.0, "2026-01-05T10:40:00Z"},
		[]any{heldSubject},
	}
	got := []any{during, page.Notes, page.Tables["Outbox"][1], []any{k.outbox("/0/status"), k.outbox("/0/attempts"), k.outbox("/0/sent_at")}, subjects(t, f)}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("status and attempts while not set up, notes, outbox row, once set up, subjects (-want +got):\n%s", diff)
	}
}

// raisedAs is the Alerts page's row of the condition of the kind of alert
// named name about Lobby, nil when none is listed.
func (k *keypad) raisedAs(name string) []string {
	k.t.Helper()
	return lobbyRow(k.raised(), name)
}

// lobbyRow is the row of raised, the Alerts page's Raised now, of the
// condition of the kind of alert named name about Lobby, nil when none
// is listed.
func lobbyRow(raised [][]string, name string) []string {
	for _, row := range raised {
		if row[0] == name && row[1] == "Lobby" {
			return row
		}
	}
	return nil
}

// muted is what the Alerts page says of the mail of a condition raised
// while its alert was on.
const muted = "Not mailed: this alert was on, not set to mail"

func TestEveryKindOfDoorAlertRaisedWhileOnIsNotMailedOnceSetToMail(t *testing.T) {
	t.Parallel()
	cases := map[string]struct {
		raise func(k *keypad)
		name  string
		since string
	}{
		"held_open": {func(k *keypad) {
			k.opens(true)
			k.sweepAt("2026-01-05T09:01:00Z")
		}, "Door held open", "Mon 5 Jan 2026 09:01:00"},
		"offline": {func(k *keypad) {
			k.goesOnline("2026-01-05T09:00:00Z", false)
			k.sweepAt("2026-01-05T09:05:00Z")
		}, "Door offline", "Mon 5 Jan 2026 09:05:00"},
		"unresponsive": {func(k *keypad) { k.sweepAt("2026-01-05T09:05:00Z") }, "Door unresponsive", "Mon 5 Jan 2026 09:05:00"},
		"config_reverted": {func(k *keypad) {
			k.givesUp("2026-01-05T09:01:00Z", k.sentDigest())
			k.sweepAt("2026-01-05T09:03:00Z")
		}, "Door configuration reverted", "Mon 5 Jan 2026 09:03:00"},
		"config_restarting": {func(k *keypad) {
			k.restarts("2026-01-05T09:01:00Z")
			k.restarts("2026-01-05T09:02:00Z")
			k.sweepAt("2026-01-05T09:03:00Z")
		}, "Door restarting with its configuration", "Mon 5 Jan 2026 09:03:00"},
		"config_mismatch": {func(k *keypad) {
			k.runs("2026-01-05T09:01:00Z", otherDigest)
			k.runs("2026-01-05T09:02:00Z", otherDigest)
			k.sweepAt("2026-01-05T09:03:00Z")
		}, "Door configuration mismatched", "Mon 5 Jan 2026 09:03:00"},
	}
	for kind, c := range cases {
		t.Run(kind, func(t *testing.T) {
			t.Parallel()
			f := testsupport.NewFactory()
			k := alertSite(t, f)
			c.raise(k)
			k.setKind(kind, "mail")

			run(t, k.a)
			run(t, k.a)

			want := []any{[]any{}, []string{c.name, "Lobby", c.since, muted}}
			if diff := cmp.Diff(want, []any{subjects(t, f), k.raisedAs(c.name)}); diff != "" {
				t.Errorf("subjects, raised (-want +got):\n%s", diff)
			}
		})
	}
}

func TestAFailedUpdateRaisedWhileItsAlertIsOnIsNotMailedOnceSetToMail(t *testing.T) {
	t.Parallel()
	f := testsupport.NewFactory()
	fl := fleetSite(t, f)
	mailAlerts(t, fl.b)
	fl.at("2026-01-06T02:00:00Z")
	fl.acks(1, "failed", "image does not fit")
	fl.at("2026-01-06T02:01:00Z")
	send(t, fl.b, "PUT", "/api/v1/alerts/kinds/update_failed", `{"state":"mail"}`, 200)

	run(t, fl.a)

	raised := lobbyRow(pageOf(signIn(t, f, fl.a.Handler()).Get("/admin/alerts")).Tables["Raised now"], "Door firmware update failed")
	if diff := cmp.Diff([]any{[][]any{}, muted}, []any{mailOf(t, f, failedSubject), raised[3]}); diff != "" {
		t.Errorf("Lobby's mail, raised (-want +got):\n%s", diff)
	}
}

func TestACertificateAlertRaisedWhileItIsOnIsNotMailedOnceSetToMail(t *testing.T) {
	t.Parallel()
	f := testsupport.NewFactory()
	c := certificateSite(t, f)
	mailAlerts(t, c.api)
	c.serves(c.leaf(), c.pki.YE1, c.pki.RootYE)
	c.checkAt(testsupport.DefaultNow)
	send(t, c.api, "PUT", "/api/v1/alerts/kinds/certificate", `{"state":"mail"}`, 200)

	run(t, c.a)
	run(t, c.a)

	raised := pageOf(c.b.Get("/admin/alerts")).Tables["Raised now"]
	if diff := cmp.Diff([]any{[]any{}, muted}, []any{subjects(t, f), raised[1][3]}); diff != "" {
		t.Errorf("subjects, raised mail (-want +got):\n%s", diff)
	}
}

func TestTheAlertsPageListsARaiseWithinTheHoldDownAsNotMailedInTheSiteTimezone(t *testing.T) {
	t.Parallel()
	f := testsupport.NewFactory()
	k := alertSite(t, f, "held_open")
	k.inZone("Europe/Berlin")
	k.holdsOpen("2026-01-05T09:00:00Z", "2026-01-05T09:01:00Z")
	k.at("2026-01-05T09:01:20Z")
	k.says("closed")

	k.holdsOpen("2026-01-05T09:03:00Z", "2026-01-05T09:04:00Z")

	want := [][]string{raisedHead, {"Door held open", "Lobby", "Mon 5 Jan 2026 10:04:00", "Not mailed: raised again within the hold-down"}}
	if diff := cmp.Diff(want, k.raised()); diff != "" {
		t.Errorf("raised (-want +got):\n%s", diff)
	}
}

func TestOwedMailTheDeliveryCannotQueueOrReadTheSettingsForIsLoggedAndQueuedAMinuteLater(t *testing.T) {
	t.Parallel()
	cases := map[string]struct {
		spoil, mend func(f *testsupport.Factory)
		error       string
	}{
		"the outbox refusing it": {
			spoil: func(f *testsupport.Factory) { f.StoreStub.FailAlertWrites() },
			mend:  func(f *testsupport.Factory) { f.StoreStub.Recover() },
			error: "stub: unavailable",
		},
		"a hold-down that is no number": {
			spoil: func(f *testsupport.Factory) { f.EnvStub["DOORCODE_ALERT_HOLD_DOWN_MINUTES"] = "soon" },
			mend:  func(f *testsupport.Factory) { delete(f.EnvStub, "DOORCODE_ALERT_HOLD_DOWN_MINUTES") },
			error: "invalid value for alert_hold_down_minutes: must be a whole number of minutes from 0 to 10080",
		},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			f := testsupport.NewFactory()
			k := alertSite(t, f, "held_open")
			k.setting("alert_recipients", "")
			k.holdsOpen("2026-01-05T09:00:00Z", "2026-01-05T09:01:00Z")
			k.at("2026-01-05T09:30:00Z")
			k.setting("alert_recipients", "ops@example.com,\nFront desk <desk@example.com>")
			c.spoil(f)
			run(t, k.a)
			logged := lastLogged(f, "mail delivery failed")
			c.mend(f)

			k.runAt("2026-01-05T09:31:00Z")

			want := []any{testsupport.LogEntry{Level: "error", Message: "mail delivery failed", Fields: map[string]string{"error": c.error}},
				[][]any{{recipients, heldSubject, heldText("Mon 5 Jan 2026 09:01:00 UTC")}}}
			if diff := cmp.Diff(want, []any{logged, mailed(t, f)}); diff != "" {
				t.Errorf("log, mail a minute later (-want +got):\n%s", diff)
			}
		})
	}
}

func TestARetryIsTriedWhenDueWhileNothingIsOwedThoughTheAlertSettingsCannotBeRead(t *testing.T) {
	t.Parallel()
	f := testsupport.NewFactory()
	k := alertSite(t, f, "held_open")
	f.MailerStub.FailWith("421 Try again later")
	k.holdsOpen("2026-01-05T09:00:00Z", "2026-01-05T09:01:00Z")
	f.EnvStub["DOORCODE_ALERT_HOLD_DOWN_MINUTES"] = "soon"
	k.at("2026-01-05T09:01:30Z")
	k.setting("site_name", "Front office")
	run(t, k.a)

	k.runAt("2026-01-05T09:02:00Z")

	delete(f.EnvStub, "DOORCODE_ALERT_HOLD_DOWN_MINUTES")
	got := []any{k.outbox("/0/attempts"), k.outbox("/0/last_attempt_at")}
	if diff := cmp.Diff([]any{2.0, "2026-01-05T09:02:00Z"}, got); diff != "" {
		t.Errorf("attempts, last attempt (-want +got):\n%s", diff)
	}
}
