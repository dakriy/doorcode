package entrypoint_test

import (
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/dakriy/doorcode/internal/testsupport"
)

// otherDigest names a configuration the server never sent.
const otherDigest = "0123456789abcdef"

// sentDigest names the configuration Lobby was last sent, over the
// payload exactly as the broker keeps it.
func (k *keypad) sentDigest() string {
	return k.f.BrokerStub.RetainedDigest("doorcode/doors/1/config")
}

// runs has Lobby's controller report, at the RFC 3339 time at, that it
// runs the configuration digest names, and the server handle it.
func (k *keypad) runs(at, digest string) {
	k.t.Helper()
	k.at(at)
	k.report(map[string]any{"config_digest": digest})
}

// givesUp has Lobby's controller say, at the RFC 3339 time at, that it
// gave up the configuration digest names, and the server handle it.
func (k *keypad) givesUp(at, digest string) {
	k.t.Helper()
	k.at(at)
	k.publish("doorcode/doors/1/event", testsupport.CBOR(map[string]any{"v": 1, "event": "config_reverted", "config_digest": digest}))
}

// configState is what the state API says of the configuration Lobby's
// controller runs: whether it is not the one last sent, whether the
// controller gave that one up, and since when.
func (k *keypad) configState() []any {
	k.t.Helper()
	return []any{k.state("/config_mismatch"), k.state("/config_reverted"), k.state("/config_mismatch_since")}
}

// configShown is what the door list shows of Lobby's status and what its
// page says of its configuration.
func (k *keypad) configShown() []any {
	k.t.Helper()
	page := doorPage(k.t, k.b, "/admin/doors/1")
	return []any{doorPage(k.t, k.b, "/admin/doors").Tables["Doors"][1][2], page.Details["Configuration"]}
}

// changeConfig has the administrator change Lobby's configuration at the
// RFC 3339 time at, which sends it again.
func (k *keypad) changeConfig(at string) {
	k.t.Helper()
	k.at(at)
	send(k.t, k.b, "PUT", "/api/v1/doors/1/config", `{"overrides":{"log_level":"debug"}}`, 200)
}

func TestTheDoorStateNamesTheConfigurationLastSentAndWhatTheControllerRuns(t *testing.T) {
	t.Parallel()
	k := keypadSite(t, testsupport.NewFactory())
	k.runs("2026-01-05T09:01:00Z", otherDigest)

	got := []any{k.state("/config_sent_digest"), k.state("/config_sent_at"), k.state("/telemetry/config_digest")}

	want := []any{k.sentDigest(), "2026-01-05T09:00:00Z", otherDigest}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("sent digest, sent at, reported digest (-want +got):\n%s", diff)
	}
}

func TestADoorIsNotFlaggedUntilItGoesOnRunningAnotherConfiguration(t *testing.T) {
	t.Parallel()
	cases := map[string]struct {
		arrange func(k *keypad)
		shown   string
	}{
		"before it reports": {func(*keypad) {}, "Not reported yet"},
		"running the configuration last sent": {func(k *keypad) {
			k.runs("2026-01-05T09:01:00Z", k.sentDigest())
		}, "The latest sent"},
		"one report of another": {func(k *keypad) {
			k.runs("2026-01-05T09:01:00Z", otherDigest)
		}, "Not reported yet"},
		"another and then the configuration last sent": {func(k *keypad) {
			k.runs("2026-01-05T09:01:00Z", otherDigest)
			k.runs("2026-01-05T09:02:00Z", k.sentDigest())
		}, "The latest sent"},
		"running its defaults": {func(k *keypad) {
			k.runs("2026-01-05T09:01:00Z", "")
			k.runs("2026-01-05T09:02:00Z", "")
		}, "Not reported yet"},
		"firmware that does not say": {func(k *keypad) {
			k.report(nil)
			k.at("2026-01-05T09:01:00Z")
			k.report(nil)
		}, "Not reported yet"},
		"another, its defaults, and another": {func(k *keypad) {
			k.runs("2026-01-05T09:01:00Z", otherDigest)
			k.runs("2026-01-05T09:02:00Z", "")
			k.runs("2026-01-05T09:03:00Z", otherDigest)
		}, "Not reported yet"},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			k := keypadSite(t, testsupport.NewFactory())
			c.arrange(k)

			got := []any{k.configState(), k.configShown()}

			want := []any{[]any{false, false, nil}, []any{"Online", c.shown}}
			if diff := cmp.Diff(want, got); diff != "" {
				t.Errorf("state, shown (-want +got):\n%s", diff)
			}
		})
	}
}

func TestADoorThatGoesOnRunningAnotherConfigurationIsFlaggedSinceItsFirstReportOfIt(t *testing.T) {
	t.Parallel()
	k := keypadSite(t, testsupport.NewFactory())
	k.runs("2026-01-05T09:01:00Z", otherDigest)
	k.runs("2026-01-05T09:02:00Z", otherDigest)
	k.runs("2026-01-05T09:03:00Z", otherDigest)

	got := []any{k.configState(), k.configShown(), doorPage(t, k.b, "/admin/doors/1").Notes}

	want := []any{
		[]any{true, false, "2026-01-05T09:01:00Z"},
		[]any{"Online Older configuration", "Older"},
		[]string{"Running an older configuration since Mon 5 Jan 2026 09:01:00: the door has not taken the configuration last sent. " +
			"Changing the door's configuration, any change, makes it try again."},
	}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("state, shown, notes (-want +got):\n%s", diff)
	}
}

func TestReportsBeforeTheConfigurationWasSentAgainDoNotCount(t *testing.T) {
	t.Parallel()
	k := keypadSite(t, testsupport.NewFactory())
	k.runs("2026-01-05T09:01:00Z", otherDigest)
	k.runs("2026-01-05T09:02:00Z", otherDigest)
	k.changeConfig("2026-01-05T09:03:00Z")
	sentAgain := []any{k.configState(), k.state("/config_sent_digest"), k.state("/config_sent_at")}
	k.runs("2026-01-05T09:04:00Z", otherDigest)
	once := k.configState()
	k.runs("2026-01-05T09:05:00Z", otherDigest)

	got := []any{sentAgain, once, k.configState()}

	want := []any{
		[]any{[]any{false, false, nil}, k.sentDigest(), "2026-01-05T09:03:00Z"},
		[]any{false, false, nil},
		[]any{true, false, "2026-01-05T09:04:00Z"},
	}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("sent again, once, twice (-want +got):\n%s", diff)
	}
}

func TestTheSameConfigurationSentAgainKeepsWhenItWasFirstSent(t *testing.T) {
	t.Parallel()
	k := keypadSite(t, testsupport.NewFactory())
	k.runs("2026-01-05T09:01:00Z", otherDigest)
	k.runs("2026-01-05T09:02:00Z", otherDigest)
	k.at("2026-01-05T09:03:00Z")

	k.publish("doorcode/doors/1/online", onlineAs(firstUsername, true))

	got := []any{k.state("/config_sent_at"), k.configState()}
	if diff := cmp.Diff([]any{"2026-01-05T09:00:00Z", []any{true, false, "2026-01-05T09:01:00Z"}}, got); diff != "" {
		t.Errorf("sent at, state (-want +got):\n%s", diff)
	}
}

func TestADoorGivingUpTheConfigurationLastSentIsLoggedAndFlaggedAtOnce(t *testing.T) {
	t.Parallel()
	k := keypadSite(t, testsupport.NewFactory())
	sent := k.sentDigest()

	k.givesUp("2026-01-05T09:05:00Z", sent)

	page := doorPage(t, k.b, "/admin/doors/1")
	got := []any{k.lastEvent(), k.configState(), k.configShown(), page.Notes, page.Tables["Alarms"]}
	detail := "The door could not come online with configuration " + sent + " and went back to the one it ran before."
	want := []any{
		testsupport.JSON(`{"id":1,"at":"2026-01-05T09:05:00Z","door":{"id":1,"name":"Lobby"},"type":"alarm","outcome":"alert",` +
			`"reason":"configuration reverted","detail":"` + detail + `"}`),
		[]any{true, true, "2026-01-05T09:05:00Z"},
		[]any{"Online Configuration given up", "The previous one"},
		[]string{"The door gave up the latest configuration because it could not connect with it; it is running the previous one " +
			"since Mon 5 Jan 2026 09:05:00. It ignores that configuration if it is sent again. " +
			"Changing the door's configuration, any change, makes it try again."},
		[][]string{{"When", "Alarm", "Details"}, {"Mon 5 Jan 2026 09:05:00", "Configuration reverted", detail}},
	}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("event, state, shown, notes, alarms (-want +got):\n%s", diff)
	}
}

func TestADoorThatGaveUpTheConfigurationStaysFlaggedWhileItRunsAnother(t *testing.T) {
	t.Parallel()
	k := keypadSite(t, testsupport.NewFactory())
	k.givesUp("2026-01-05T09:05:00Z", k.sentDigest())
	k.runs("2026-01-05T09:06:00Z", otherDigest)
	k.runs("2026-01-05T09:07:00Z", otherDigest)

	if diff := cmp.Diff([]any{true, true, "2026-01-05T09:05:00Z"}, k.configState()); diff != "" {
		t.Errorf("state (-want +got):\n%s", diff)
	}
}

func TestAGivenUpConfigurationIsNoLongerFlaggedOnceTheDoorRunsWhatWasLastSent(t *testing.T) {
	t.Parallel()
	cases := map[string]func(k *keypad){
		"the configuration changed": func(k *keypad) { k.changeConfig("2026-01-05T09:06:00Z") },
		"the door runs the configuration last sent": func(k *keypad) {
			k.runs("2026-01-05T09:06:00Z", k.sentDigest())
		},
	}
	for name, then := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			k := keypadSite(t, testsupport.NewFactory())
			k.givesUp("2026-01-05T09:05:00Z", k.sentDigest())

			then(k)

			if diff := cmp.Diff([]any{false, false, nil}, k.configState()); diff != "" {
				t.Errorf("state (-want +got):\n%s", diff)
			}
		})
	}
}

func TestADoorGivingUpAConfigurationOtherThanTheOneLastSentIsLoggedButNotFlagged(t *testing.T) {
	t.Parallel()
	k := keypadSite(t, testsupport.NewFactory())

	k.givesUp("2026-01-05T09:05:00Z", otherDigest)

	got := []any{k.reasons(), k.configState()}
	if diff := cmp.Diff([]any{[]any{"configuration reverted"}, []any{false, false, nil}}, got); diff != "" {
		t.Errorf("reason, state (-want +got):\n%s", diff)
	}
}

func TestADoorWhoseSentConfigurationWasNotKeptIsNotJudged(t *testing.T) {
	t.Parallel()
	f := testsupport.NewFactory()
	f.StoreStub.FailSentConfigWrites()
	k := keypadSite(t, f)
	f.StoreStub.Recover()
	k.runs("2026-01-05T09:01:00Z", otherDigest)
	k.runs("2026-01-05T09:02:00Z", otherDigest)
	k.givesUp("2026-01-05T09:03:00Z", otherDigest)

	got := []any{k.state("/config_sent_digest"), k.configState(), k.configShown()}

	want := []any{"", []any{false, false, nil}, []any{"Online", "Not reported yet"}}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("sent digest, state, shown (-want +got):\n%s", diff)
	}
}

func TestLiveStreamTellsEveryPageWhenADoorIsFlaggedForItsConfiguration(t *testing.T) {
	t.Parallel()
	cases := map[string]struct {
		change func(k *keypad)
		want   string
	}{
		"another report of another": {func(k *keypad) { k.runs("2026-01-05T09:02:00Z", otherDigest) }, "event: doors\ndata: changed\n\n"},
		"a report of the latest":    {func(k *keypad) { k.runs("2026-01-05T09:02:00Z", k.sentDigest()) }, "event: door\ndata: 1\n\n"},
		"giving the configuration up": {func(k *keypad) {
			k.givesUp("2026-01-05T09:02:00Z", k.sentDigest())
		}, "event: doors\ndata: changed\n\n"},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			k := keypadSite(t, testsupport.NewFactory())
			k.runs("2026-01-05T09:01:00Z", otherDigest)
			stream := k.b.Stream("/admin/live")
			defer stream.Close()
			stream.Next()

			c.change(k)

			if diff := cmp.Diff(c.want, stream.Next()); diff != "" {
				t.Errorf("event (-want +got):\n%s", diff)
			}
		})
	}
}
