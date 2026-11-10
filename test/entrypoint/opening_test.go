package entrypoint_test

import (
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/dakriy/doorcode/internal/testsupport"
)

func TestADoorOpeningIsLoggedInOpenModeAndOutsideIt(t *testing.T) {
	t.Parallel()
	byEvent := func(k *keypad) { k.opens(true) }
	byReport := func(k *keypad) { k.report(map[string]any{"door_open": true}) }
	cases := map[string]struct {
		arrange func(k *keypad)
		open    func(k *keypad)
	}{
		"an opened event":                        {func(*keypad) {}, byEvent},
		"an opened event in open mode":           {(*keypad).inOpenMode, byEvent},
		"a report of the door open":              {func(*keypad) {}, byReport},
		"a report of the door open in open mode": {(*keypad).inOpenMode, byReport},
		"an opened event after a closing":        {func(k *keypad) { k.says("closed") }, byEvent},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			k := keypadSite(t, testsupport.NewFactory())
			c.arrange(k)
			k.at("2026-01-05T10:00:00Z")

			c.open(k)

			want := testsupport.JSON(`{"id":1,"at":"2026-01-05T10:00:00Z","door":{"id":1,"name":"Lobby"},"type":"door","outcome":"info","reason":"opened"}`)
			if diff := cmp.Diff([]any{"open", want, []any{"opened"}}, []any{k.state("/door"), k.lastEvent(), k.reasons()}); diff != "" {
				t.Errorf("door, event, reasons (-want +got):\n%s", diff)
			}
		})
	}
}

func TestADoorForcedOpenIsLoggedAsOpenedBesideTheAlarm(t *testing.T) {
	t.Parallel()
	k := keypadSite(t, testsupport.NewFactory())

	k.opens(false)

	events := testsupport.JSONAt(send(t, k.b, "GET", "/api/v1/events", "", 200).Body, "/events")
	want := []any{
		testsupport.JSON(`{"id":2,"at":"2026-01-05T09:00:00Z","door":{"id":1,"name":"Lobby"},"type":"alarm","outcome":"alert",` +
			`"reason":"forced open","detail":"Its controller says it was locked when it opened."}`),
		testsupport.JSON(`{"id":1,"at":"2026-01-05T09:00:00Z","door":{"id":1,"name":"Lobby"},"type":"door","outcome":"info","reason":"opened"}`),
	}
	if diff := cmp.Diff(want, events); diff != "" {
		t.Errorf("events (-want +got):\n%s", diff)
	}
}

func TestADoorSaidOpenWhileOpenIsLoggedAsOpenedOnce(t *testing.T) {
	t.Parallel()
	byEvent := func(k *keypad) { k.opens(true) }
	byReport := func(k *keypad) { k.report(map[string]any{"door_open": true}) }
	cases := map[string]struct {
		first, again func(k *keypad)
	}{
		"an opened event twice":              {byEvent, byEvent},
		"an opened event, then a report":     {byEvent, byReport},
		"a report of the door open twice":    {byReport, byReport},
		"a report, then an opened event":     {byReport, byEvent},
		"an opened event twice in open mode": {func(k *keypad) { k.inOpenMode(); byEvent(k) }, byEvent},
		"a report twice in open mode":        {func(k *keypad) { k.inOpenMode(); byReport(k) }, byReport},
		"an opened event, then one locked":   {byEvent, func(k *keypad) { k.opens(false) }},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			k := keypadSite(t, testsupport.NewFactory())
			c.first(k)
			k.at("2026-01-05T09:00:20Z")

			c.again(k)

			if diff := cmp.Diff([]any{"2026-01-05T09:00:00Z", []any{"opened"}}, []any{k.state("/opened_at"), k.reasons()}); diff != "" {
				t.Errorf("opened at, reasons (-want +got):\n%s", diff)
			}
		})
	}
}

func TestAnOpeningThatCannotBeLoggedIsLoggedAndIgnored(t *testing.T) {
	t.Parallel()
	cases := map[string]struct {
		topic   string
		payload []byte
	}{
		"an opened event":           {"doorcode/doors/1/event", testsupport.CBOR(map[string]any{"v": 1, "event": "opened", "unlocked": true})},
		"a report of the door open": {"doorcode/doors/1/telemetry", reportOf(map[string]any{"door_open": true})},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			f := testsupport.NewFactory()
			k := keypadSite(t, f)
			logged := len(f.LogStub.Entries)
			f.StoreStub.FailEventWrites()

			f.BrokerStub.Inject(c.topic, c.payload)
			run(t, k.a)

			want := []testsupport.LogEntry{{Level: "error", Message: "ignored a broker message", Fields: map[string]string{"topic": c.topic, "error": "stub: unavailable"}}}
			if diff := cmp.Diff(want, logsSince(f, logged)); diff != "" {
				t.Errorf("log (-want +got):\n%s", diff)
			}
		})
	}
}

func TestTheEventsPageFiltersByOpenings(t *testing.T) {
	t.Parallel()
	k := keypadSite(t, testsupport.NewFactory())
	k.opens(true)
	k.at("2026-01-05T09:00:20Z")
	k.closes(20000)
	k.b.Get("/")

	page := testsupport.ParsePage(k.b.Get("/admin/events?reason=opened").Body)

	want := []any{
		[][]string{eventsHead, {"Mon 5 Jan 2026 09:00:00", "Lobby", "No one", "Info Opened", "Door activity"}, {"Showing 1 to 1 of 1"}},
		[]string{
			"Every reason", "Access rule", "Open mode", "Open override", "Door commander", "Command",
			"No rule", "Locked", "Expired", "Rate limited", "Not a commander", "Bad command", "Command failed", "Unreachable",
			"Keypad open", "Keypad locked", "Keypad cancel", "Reboot sent", "Admin UI", "Forced open", "Held open",
			"Opened (selected)", "Closed", "Started", "Update sent", "Update succeeded", "Update failed",
		},
	}
	if diff := cmp.Diff(want, []any{page.Tables["Events"], page.Options["reason"]}); diff != "" {
		t.Errorf("events, reasons (-want +got):\n%s", diff)
	}
}
