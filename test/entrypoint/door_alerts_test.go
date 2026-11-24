package entrypoint_test

import (
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/dakriy/doorcode/internal/testsupport"
)

// everyKind is the key of every kind of alert, so a test mailing about
// one shows that no other is raised along with it.
var everyKind = []string{
	"forced_open", "held_open", "offline", "unresponsive", "config_reverted", "config_restarting", "config_mismatch", "update_failed",
}

// goesOnline has Lobby's controller say, at the RFC 3339 time at, that it
// is online or that it went away, and the server handle it.
func (k *keypad) goesOnline(at string, online bool) {
	k.t.Helper()
	k.at(at)
	k.publish("doorcode/doors/1/online", onlineAs(firstUsername, online))
}

func TestADoorOfflineForTheUnresponsiveTimeMailsAndClearsOnceItConnectsAgain(t *testing.T) {
	t.Parallel()
	f := testsupport.NewFactory()
	k := alertSite(t, f, everyKind...)
	k.goesOnline("2026-01-05T09:00:00Z", false)
	k.sweepAt("2026-01-05T09:04:00Z")
	early := subjects(t, f)

	k.sweepAt("2026-01-05T09:05:00Z", "2026-01-05T09:08:00Z")
	k.goesOnline("2026-01-05T09:10:00Z", true)
	k.sweepAt("2026-01-05T09:11:00Z")

	want := []any{[]any{}, [][]any{
		{recipients, "Door offline: Lobby", "Door offline at Lobby, Mon 5 Jan 2026 09:05:00 UTC.\n\n" +
			"Its controller was last heard from Mon 5 Jan 2026 09:00:00 UTC and is not connected.\n" + alertFooter("Door offline")},
		{recipients, "Cleared: Door offline: Lobby", "Door offline at Lobby cleared, Mon 5 Jan 2026 09:11:00 UTC.\n\n" +
			"It was raised Mon 5 Jan 2026 09:05:00 UTC.\n" + alertFooter("Door offline")},
	}}
	if diff := cmp.Diff(want, []any{early, mailed(t, f)}); diff != "" {
		t.Errorf("subjects a minute early, mail (-want +got):\n%s", diff)
	}
}

func TestAnUnresponsiveDoorMailsAndClearsOnceTheServerHearsFromIt(t *testing.T) {
	t.Parallel()
	f := testsupport.NewFactory()
	k := alertSite(t, f, everyKind...)
	k.sweepAt("2026-01-05T09:04:00Z")
	early := subjects(t, f)

	k.sweepAt("2026-01-05T09:05:00Z")
	k.at("2026-01-05T09:07:00Z")
	k.report(nil)
	k.sweepAt("2026-01-05T09:08:00Z")

	want := []any{[]any{}, [][]any{
		{recipients, "Door unresponsive: Lobby", "Door unresponsive at Lobby, Mon 5 Jan 2026 09:05:00 UTC.\n\n" +
			"Its controller was last heard from Mon 5 Jan 2026 09:00:00 UTC, though the broker says it is connected.\n" + alertFooter("Door unresponsive")},
		{recipients, "Cleared: Door unresponsive: Lobby", "Door unresponsive at Lobby cleared, Mon 5 Jan 2026 09:08:00 UTC.\n\n" +
			"It was raised Mon 5 Jan 2026 09:05:00 UTC.\n" + alertFooter("Door unresponsive")},
	}}
	if diff := cmp.Diff(want, []any{early, mailed(t, f)}); diff != "" {
		t.Errorf("subjects a minute early, mail (-want +got):\n%s", diff)
	}
}

func TestAnUnresponsiveDoorThatGoesAwayClearsAndIsOfflineOnceItStaysAway(t *testing.T) {
	t.Parallel()
	f := testsupport.NewFactory()
	k := alertSite(t, f, everyKind...)
	k.sweepAt("2026-01-05T09:05:00Z")
	k.goesOnline("2026-01-05T09:06:00Z", false)

	k.sweepAt("2026-01-05T09:07:00Z", "2026-01-05T09:11:00Z")

	want := []any{"Door unresponsive: Lobby", "Cleared: Door unresponsive: Lobby", "Door offline: Lobby"}
	if diff := cmp.Diff(want, subjects(t, f)); diff != "" {
		t.Errorf("subjects (-want +got):\n%s", diff)
	}
}

func TestEachConfigurationStateDoorListsFlagMailsWhenRaisedAndWhenItClears(t *testing.T) {
	t.Parallel()
	cases := map[string]struct {
		arrange func(k *keypad)
		name    string
		detail  func(sent string) string
		clear   func(k *keypad)
	}{
		"given up": {
			arrange: func(k *keypad) { k.givesUp("2026-01-05T09:01:00Z", k.sentDigest()) },
			name:    "Door configuration reverted",
			detail: func(sent string) string {
				return "At Mon 5 Jan 2026 09:01:00 UTC it gave up configuration " + sent + ", the one it was last sent, " +
					"because it could not come online with it, and went back to the one it ran before."
			},
			clear: func(k *keypad) { k.changeConfig("2026-01-05T09:04:00Z") },
		},
		"restarting with it": {
			arrange: func(k *keypad) {
				k.restarts("2026-01-05T09:01:00Z")
				k.restarts("2026-01-05T09:02:00Z")
			},
			name: "Door restarting with its configuration",
			detail: func(sent string) string {
				return "It started 2 times from Mon 5 Jan 2026 09:01:00 UTC on without reporting configuration " + sent + ", the one it was last sent."
			},
			clear: func(k *keypad) { k.runs("2026-01-05T09:04:00Z", k.sentDigest()) },
		},
		"going on running another": {
			arrange: func(k *keypad) {
				k.runs("2026-01-05T09:01:00Z", otherDigest)
				k.runs("2026-01-05T09:02:00Z", otherDigest)
			},
			name: "Door configuration mismatched",
			detail: func(sent string) string {
				return "It has reported configuration " + otherDigest + " since Mon 5 Jan 2026 09:01:00 UTC, not " + sent + ", the one it was last sent."
			},
			clear: func(k *keypad) { k.runs("2026-01-05T09:04:00Z", k.sentDigest()) },
		},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			f := testsupport.NewFactory()
			k := alertSite(t, f, everyKind...)
			sent := k.sentDigest()
			c.arrange(k)
			k.sweepAt("2026-01-05T09:03:00Z")
			c.clear(k)

			k.sweepAt("2026-01-05T09:05:00Z")

			subject := c.name + ": Lobby"
			want := [][]any{
				{recipients, subject, c.name + " at Lobby, Mon 5 Jan 2026 09:03:00 UTC.\n\n" + c.detail(sent) + "\n" + alertFooter(c.name)},
				{recipients, cleared(subject), c.name + " at Lobby cleared, Mon 5 Jan 2026 09:05:00 UTC.\n\n" +
					"It was raised Mon 5 Jan 2026 09:03:00 UTC.\n" + alertFooter(c.name)},
			}
			if diff := cmp.Diff(want, mailed(t, f)); diff != "" {
				t.Errorf("mail (-want +got):\n%s", diff)
			}
		})
	}
}

func TestADoorStillInAConditionMailsOnlyOnce(t *testing.T) {
	t.Parallel()
	f := testsupport.NewFactory()
	k := alertSite(t, f, everyKind...)
	k.goesOnline("2026-01-05T09:00:00Z", false)

	k.sweepAt("2026-01-05T09:05:00Z", "2026-01-05T09:06:00Z", "2026-01-05T09:30:00Z")

	if diff := cmp.Diff([]any{"Door offline: Lobby"}, subjects(t, f)); diff != "" {
		t.Errorf("subjects (-want +got):\n%s", diff)
	}
}

func TestDoorConditionsTheStoreCannotListAreLoggedAndTheDoorsAreStillWatched(t *testing.T) {
	t.Parallel()
	f := testsupport.NewFactory()
	k := alertSite(t, f, everyKind...)
	f.StoreStub.FailAlerts()

	k.sweepAt("2026-01-05T09:05:00Z")

	logged := testsupport.LogEntry{Level: "error", Message: "periodic work failed",
		Fields: map[string]string{"error": "follow the doors' alert conditions: read the alert conditions raised: stub: unavailable\n" +
			"follow the alerts of updates failing: read the alert conditions raised: stub: unavailable"}}
	if diff := cmp.Diff([]any{logged, "Online Unresponsive"}, []any{lastLogged(f, "periodic work failed"), k.badges()}); diff != "" {
		t.Errorf("log, badges (-want +got):\n%s", diff)
	}
}

// failedSubject is the subject of the mail about Lobby's update failing.
const failedSubject = "Door firmware update failed: Lobby"

// failedText is the text of the mail raising Lobby's update failing,
// raised at the RFC 3339 time raised for an update that failed at the
// mail's time failed, as why says.
func failedText(raised, failed, why string) string {
	return "Door firmware update failed at Lobby, " + raised + ".\n\n" +
		"Its update to version 1.5.0 failed at " + failed + ". " + why + "\n" + alertFooter("Door firmware update failed")
}

// clearedFailedText is the text of the mail clearing Lobby's update
// failing at cleared, raised at raised.
func clearedFailedText(cleared, raised string) string {
	return "Door firmware update failed at Lobby cleared, " + cleared + ".\n\n" +
		"It was raised " + raised + ".\n" + alertFooter("Door firmware update failed")
}

func TestAFailedUpdateMailsAtTheNextPassAndClearsOnceTheDoorIsUpdatingAgain(t *testing.T) {
	t.Parallel()
	f := testsupport.NewFactory()
	fl := fleetSite(t, f)
	mailAlerts(t, fl.b, "update_failed")
	fl.at("2026-01-06T02:00:00Z")
	fl.acks(1, "failed", "image does not fit")
	early := subjects(t, f)
	fl.at("2026-01-06T02:01:00Z")
	fl.at("2026-01-07T02:00:00Z")
	again := fl.status(1)

	fl.at("2026-01-07T02:01:00Z")

	want := []any{[]any{}, "updating", [][]any{
		{recipients, failedSubject, failedText("Tue 6 Jan 2026 02:01:00 UTC", "Tue 6 Jan 2026 02:00:00 UTC", "The door could not install it: image does not fit.")},
		{recipients, cleared(failedSubject), clearedFailedText("Wed 7 Jan 2026 02:01:00 UTC", "Tue 6 Jan 2026 02:01:00 UTC")},
	}}
	if diff := cmp.Diff(want, []any{early, again, mailOf(t, f, failedSubject)}); diff != "" {
		t.Errorf("subjects before the next pass, status sent again, Lobby's mail (-want +got):\n%s", diff)
	}
}

// mailOf is the mail the stub mailer took whose subject is subject, or
// clears it.
func mailOf(t *testing.T, f *testsupport.Factory, subject string) [][]any {
	t.Helper()
	out := [][]any{}
	for _, m := range mailed(t, f) {
		if m[1] == subject || m[1] == cleared(subject) {
			out = append(out, m)
		}
	}
	return out
}

func TestAFailedUpdateClearsOnceTheDoorRunsItsTargetAnyHow(t *testing.T) {
	t.Parallel()
	f := testsupport.NewFactory()
	fl := fleetSite(t, f)
	mailAlerts(t, fl.b, "update_failed")
	fl.at("2026-01-06T02:00:00Z")
	fl.acks(1, "failed", "")
	fl.at("2026-01-06T02:01:00Z")
	fl.at("2026-01-06T03:00:00Z")
	fl.reconnects(1, "1.5.0")

	fl.at("2026-01-06T03:01:00Z")

	want := []any{"done", [][]any{
		{recipients, failedSubject, failedText("Tue 6 Jan 2026 02:01:00 UTC", "Tue 6 Jan 2026 02:00:00 UTC", "The door could not install it.")},
		{recipients, cleared(failedSubject), clearedFailedText("Tue 6 Jan 2026 03:01:00 UTC", "Tue 6 Jan 2026 02:01:00 UTC")},
	}}
	if diff := cmp.Diff(want, []any{fl.status(1), mailOf(t, f, failedSubject)}); diff != "" {
		t.Errorf("status, Lobby's mail (-want +got):\n%s", diff)
	}
}

func TestAnUpdateThatRunsOutOfTimeMailsThatItFailedInTheSamePass(t *testing.T) {
	t.Parallel()
	f := testsupport.NewFactory()
	fl := fleetSite(t, f)
	mailAlerts(t, fl.b, "update_failed")
	fl.at("2026-01-06T02:00:00Z")

	fl.at("2026-01-06T02:30:00Z")

	want := []any{[]any{failedSubject, "Door firmware update failed: Dock"}, [][]any{
		{recipients, failedSubject, failedText("Tue 6 Jan 2026 02:30:00 UTC", "Tue 6 Jan 2026 02:30:00 UTC", "It did not connect again with it within 30 minutes.")},
	}}
	if diff := cmp.Diff(want, []any{subjects(t, f), mailOf(t, f, failedSubject)}); diff != "" {
		t.Errorf("subjects, Lobby's mail (-want +got):\n%s", diff)
	}
}

func TestAnUpdateThatSucceedsWithNothingRaisedMailsNothing(t *testing.T) {
	t.Parallel()
	f := testsupport.NewFactory()
	fl := fleetSite(t, f)
	mailAlerts(t, fl.b, "update_failed")
	fl.at("2026-01-06T02:00:00Z")
	fl.reconnects(1, "1.5.0")

	fl.at("2026-01-06T02:01:00Z")

	if diff := cmp.Diff([]any{"done", []any{}}, []any{fl.status(1), subjects(t, f)}); diff != "" {
		t.Errorf("status, subjects (-want +got):\n%s", diff)
	}
}

func TestUpdatesAreStillRolledOutWhenTheAlertConditionsCannotBeRead(t *testing.T) {
	t.Parallel()
	f := testsupport.NewFactory()
	fl := fleetSite(t, f)
	f.StoreStub.FailAlerts()

	fl.at("2026-01-06T02:00:00Z")

	f.StoreStub.Recover()
	logged := testsupport.LogEntry{Level: "error", Message: "periodic work failed",
		Fields: map[string]string{"error": "follow the doors' alert conditions: read the alert conditions raised: stub: unavailable\n" +
			"follow the alerts of updates failing: read the alert conditions raised: stub: unavailable"}}
	if diff := cmp.Diff([]any{logged, "updating"}, []any{lastLogged(f, "periodic work failed"), fl.status(1)}); diff != "" {
		t.Errorf("log, status (-want +got):\n%s", diff)
	}
}

func TestAFailedUpdateOfADoorWhoseControllerMovedToAnotherDoorStaysRaised(t *testing.T) {
	t.Parallel()
	f := testsupport.NewFactory()
	fl := fleetSite(t, f)
	mailAlerts(t, fl.b, "update_failed")
	fl.at("2026-01-06T02:00:00Z")
	fl.acks(1, "failed", "")
	fl.acks(2, "failed", "")
	fl.at("2026-01-06T02:01:00Z")
	fl.at("2026-01-06T03:00:00Z")
	createDoor(t, fl.b, `{"name":"Annex"}`)
	enrollWith(t, f, fl.a, fl.b, "A1", "4")
	fl.reconnects(2, "1.5.0")

	fl.at("2026-01-06T03:01:00Z")

	want := []any{failedSubject, "Door firmware update failed: Dock", "Door firmware update failed: Attic", cleared("Door firmware update failed: Dock")}
	if diff := cmp.Diff(want, subjects(t, f)); diff != "" {
		t.Errorf("subjects (-want +got):\n%s", diff)
	}
}
