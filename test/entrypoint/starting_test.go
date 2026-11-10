package entrypoint_test

import (
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/dakriy/doorcode/internal/testsupport"
)

// starts has Lobby's controller say it started for reason, and the server
// handle it.
func (k *keypad) starts(reason string) {
	k.t.Helper()
	k.publish("doorcode/doors/1/event", testsupport.CBOR(map[string]any{"v": 1, "event": "started", "reason": reason}))
}

func TestAControllerStartingIsLoggedWithWhyItStarted(t *testing.T) {
	t.Parallel()
	cases := map[string]struct {
		start func(k *keypad)
		want  string
	}{
		"power on":                 {func(k *keypad) { k.starts("power_on") }, `,"detail":"Power on."`},
		"software":                 {func(k *keypad) { k.starts("software") }, `,"detail":"A restart its firmware asked for."`},
		"panic":                    {func(k *keypad) { k.starts("panic") }, `,"detail":"Its firmware panicked."`},
		"watchdog":                 {func(k *keypad) { k.starts("watchdog") }, `,"detail":"Its watchdog reset it."`},
		"brownout":                 {func(k *keypad) { k.starts("brownout") }, `,"detail":"A brownout: its supply voltage dropped too low."`},
		"external":                 {func(k *keypad) { k.starts("external") }, `,"detail":"Its reset pin."`},
		"unknown":                  {func(k *keypad) { k.starts("unknown") }, `,"detail":"It does not know why."`},
		"a value it does not know": {func(k *keypad) { k.starts("cosmic_ray") }, `,"detail":"cosmic_ray"`},
		"no reason":                {func(k *keypad) { k.says("started") }, ``},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			k := keypadSite(t, testsupport.NewFactory())

			c.start(k)

			want := testsupport.JSON(`{"id":1,"at":"2026-01-05T09:00:00Z","door":{"id":1,"name":"Lobby"},"type":"door","outcome":"info",` +
				`"reason":"started"` + c.want + `}`)
			if diff := cmp.Diff(want, k.lastEvent()); diff != "" {
				t.Errorf("event (-want +got):\n%s", diff)
			}
		})
	}
}

func TestAControllerStartingLeavesTheDoorAsItWas(t *testing.T) {
	t.Parallel()
	cases := map[string]struct {
		arrange func(k *keypad)
		want    []any
	}{
		"not yet known": {func(*keypad) {}, []any{"unknown", nil, []any{"started"}}},
		"closed":        {func(k *keypad) { k.says("closed") }, []any{"closed", nil, []any{"started"}}},
		"open":          {func(k *keypad) { k.opens(true) }, []any{"open", "2026-01-05T09:00:00Z", []any{"started"}}},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			k := keypadSite(t, testsupport.NewFactory())
			c.arrange(k)
			k.at("2026-01-05T09:00:20Z")

			k.starts("power_on")

			got := []any{k.state("/door"), k.state("/opened_at"), k.reasons()}
			if diff := cmp.Diff(c.want, got); diff != "" {
				t.Errorf("door, opened at, reasons (-want +got):\n%s", diff)
			}
		})
	}
}

func TestStartsAreListedByTheReasonFilter(t *testing.T) {
	t.Parallel()
	k := keypadSite(t, testsupport.NewFactory())
	k.press("000000")
	k.starts("watchdog")

	got := testsupport.JSONAt(send(t, k.b, "GET", "/api/v1/events?reason=started", "", 200).Body, "/total")

	if diff := cmp.Diff(1.0, got); diff != "" {
		t.Errorf("total (-want +got):\n%s", diff)
	}
}

func TestTheEventsPageShowsAControllerStartingAndWhy(t *testing.T) {
	t.Parallel()
	k := keypadSite(t, testsupport.NewFactory())
	k.starts("brownout")
	k.b.Get("/")

	page := pageOf(k.b.Get("/admin/events"))

	want := [][]string{eventsHead,
		{"Mon 5 Jan 2026 09:00:00", "Lobby", "No one", "Info Started", "Door activity A brownout: its supply voltage dropped too low."},
		{"Showing 1 to 1 of 1"},
	}
	if diff := cmp.Diff(want, page.Tables["Events"]); diff != "" {
		t.Errorf("events (-want +got):\n%s", diff)
	}
}
