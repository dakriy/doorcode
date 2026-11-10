package entrypoint_test

import (
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/dakriy/doorcode/internal/testsupport"
)

// closes has Lobby's controller say the door closed after openMS
// milliseconds, and the server handle it.
func (k *keypad) closes(openMS int) {
	k.t.Helper()
	k.publish("doorcode/doors/1/event", testsupport.CBOR(map[string]any{"v": 1, "event": "closed", "open_ms": openMS}))
}

func TestADoorClosingIsLoggedWithHowLongItWasOpen(t *testing.T) {
	t.Parallel()
	byEvent := func(k *keypad) { k.opens(true) }
	byReport := func(k *keypad) { k.report(map[string]any{"door_open": true}) }
	cases := map[string]struct {
		open, close func(k *keypad)
		want        int
	}{
		"a closed event saying how long":  {byEvent, func(k *keypad) { k.closes(18500) }, 18500},
		"a closed event saying nothing":   {byEvent, func(k *keypad) { k.says("closed") }, 20000},
		"a report of the door closed":     {byEvent, func(k *keypad) { k.report(nil) }, 20000},
		"opened by a report, then closed": {byReport, func(k *keypad) { k.says("closed") }, 20000},
		"opened and closed by reports":    {byReport, func(k *keypad) { k.report(nil) }, 20000},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			k := keypadSite(t, testsupport.NewFactory())
			c.open(k)
			k.at("2026-01-05T09:00:20Z")

			c.close(k)

			want := testsupport.JSON(`{"id":1,"at":"2026-01-05T09:00:20Z","door":{"id":1,"name":"Lobby"},"type":"door","outcome":"info",` +
				`"reason":"closed","open_ms":` + testsupport.Itoa(c.want) + `}`)
			if diff := cmp.Diff([]any{want, "closed"}, []any{k.lastEvent(), k.state("/door")}); diff != "" {
				t.Errorf("event, door (-want +got):\n%s", diff)
			}
		})
	}
}

func TestADoorClosingIsLoggedOnlyWhenTheServerHadItOpen(t *testing.T) {
	t.Parallel()
	cases := map[string]struct {
		arrange func(k *keypad)
		want    []any
	}{
		"a closed event of a door never open":      {func(k *keypad) { k.says("closed") }, []any{}},
		"a closed event saying how long, not open": {func(k *keypad) { k.closes(4000) }, []any{}},
		"a report of a door never open":            {func(k *keypad) { k.report(nil) }, []any{}},
		"a second closed event": {func(k *keypad) {
			k.opens(true)
			k.says("closed")
			k.says("closed")
		}, []any{"closed"}},
		"a second report of the door closed": {func(k *keypad) {
			k.opens(true)
			k.report(nil)
			k.report(nil)
		}, []any{"closed"}},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			k := keypadSite(t, testsupport.NewFactory())

			c.arrange(k)

			if diff := cmp.Diff([]any{"closed", c.want}, []any{k.state("/door"), k.reasons()}); diff != "" {
				t.Errorf("door, reasons (-want +got):\n%s", diff)
			}
		})
	}
}

func TestADoorClosingIsNotLoggedForAnOpeningThatBeganWhileItsTimelineHeldItOpen(t *testing.T) {
	t.Parallel()
	closeEvent := func(k *keypad) { k.closes(15000) }
	cases := map[string]struct {
		arrange func(k *keypad)
		close   func(k *keypad)
		want    []any
	}{
		"an open override in force":                {func(k *keypad) { createOverride(k.t, k.b, "1", openNow) }, closeEvent, []any{}},
		"an open override in force, then a report": {func(k *keypad) { createOverride(k.t, k.b, "1", openNow) }, func(k *keypad) { k.report(nil) }, []any{}},
		"open mode": {func(k *keypad) {
			send(k.t, k.b, "PUT", "/api/v1/zones/1", `{"name":"North","door_ids":[1],"open_schedule_ids":[2]}`, 200)
		}, closeEvent, []any{}},
		"an open override that ended before it opened": {func(k *keypad) {
			createOverride(k.t, k.b, "1", `{"kind":"open","start":"2026-01-05T08:00:00Z","end":"2026-01-05T09:59:59Z"}`)
		}, closeEvent, []any{"closed"}},
		"an open override that began after it opened": {func(k *keypad) {
			createOverride(k.t, k.b, "1", `{"kind":"open","start":"2026-01-05T10:00:01Z","end":"2026-01-05T11:00:00Z"}`)
		}, closeEvent, []any{"closed"}},
		"a locked override in force": {func(k *keypad) { createOverride(k.t, k.b, "1", lockedNow) }, closeEvent, []any{"closed"}},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			k := keypadSite(t, testsupport.NewFactory())
			c.arrange(k)
			k.at("2026-01-05T10:00:00Z")
			k.opens(true)
			k.at("2026-01-05T10:00:15Z")

			c.close(k)

			if diff := cmp.Diff([]any{"closed", c.want}, []any{k.state("/door"), k.reasons()}); diff != "" {
				t.Errorf("door, reasons (-want +got):\n%s", diff)
			}
		})
	}
}

func TestAClosingThatCannotBeHandledIsLoggedAndIgnored(t *testing.T) {
	t.Parallel()
	event, telemetry := "doorcode/doors/1/event", "doorcode/doors/1/telemetry"
	closed := testsupport.CBOR(map[string]any{"v": 1, "event": "closed"})
	cases := map[string]struct {
		topic   string
		payload []byte
		fail    func(f *testsupport.Factory)
	}{
		"a closed event when settings cannot be read":  {event, closed, func(f *testsupport.Factory) { f.StoreStub.FailSettings() }},
		"a closed event when schedules cannot be read": {event, closed, func(f *testsupport.Factory) { f.StoreStub.FailSchedules() }},
		"a closed event that cannot be logged":         {event, closed, func(f *testsupport.Factory) { f.StoreStub.FailEventWrites() }},
		"a report of the door closed that cannot be logged": {
			telemetry, reportOf(nil), func(f *testsupport.Factory) { f.StoreStub.FailEventWrites() },
		},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			f := testsupport.NewFactory()
			k := keypadSite(t, f)
			k.opens(true)
			logged := len(f.LogStub.Entries)
			c.fail(f)

			f.BrokerStub.Inject(c.topic, c.payload)
			run(t, k.a)

			want := []testsupport.LogEntry{{Level: "error", Message: "ignored a broker message", Fields: map[string]string{"topic": c.topic, "error": "stub: unavailable"}}}
			if diff := cmp.Diff(want, logsSince(f, logged)); diff != "" {
				t.Errorf("log (-want +got):\n%s", diff)
			}
		})
	}
}

func TestTheEventsPageShowsHowLongADoorWasOpen(t *testing.T) {
	t.Parallel()
	k := keypadSite(t, testsupport.NewFactory())
	k.opens(true)
	k.at("2026-01-05T09:00:50Z")
	k.closes(48250)
	k.b.Get("/")

	page := pageOf(k.b.Get("/admin/events"))

	want := [][]string{eventsHead,
		{"Mon 5 Jan 2026 09:00:50", "Lobby", "No one", "Info Closed", "Door activity Open for 48.3s"},
		{"Showing 1 to 1 of 1"},
	}
	if diff := cmp.Diff(want, page.Tables["Events"]); diff != "" {
		t.Errorf("events (-want +got):\n%s", diff)
	}
}
