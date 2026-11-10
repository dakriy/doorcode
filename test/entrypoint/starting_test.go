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
		"power on":                 {func(k *keypad) { k.starts("power_on") }, `,"detail":"Power on.","start_reason":"power_on"`},
		"software":                 {func(k *keypad) { k.starts("software") }, `,"detail":"A restart its firmware asked for.","start_reason":"software"`},
		"panic":                    {func(k *keypad) { k.starts("panic") }, `,"detail":"Its firmware panicked.","start_reason":"panic"`},
		"watchdog":                 {func(k *keypad) { k.starts("watchdog") }, `,"detail":"Its watchdog reset it.","start_reason":"watchdog"`},
		"brownout":                 {func(k *keypad) { k.starts("brownout") }, `,"detail":"A brownout: its supply voltage dropped too low.","start_reason":"brownout"`},
		"external":                 {func(k *keypad) { k.starts("external") }, `,"detail":"Its reset pin.","start_reason":"external"`},
		"unknown":                  {func(k *keypad) { k.starts("unknown") }, `,"detail":"It does not know why.","start_reason":"unknown"`},
		"a value it does not know": {func(k *keypad) { k.starts("cosmic_ray") }, `,"detail":"cosmic_ray","start_reason":"cosmic_ray"`},
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

// startedSite is the keypad site where Lobby's controller started on power
// on (1), after a panic (2), for a reason the server does not know (3),
// without saying why (4), and after a panic again (5), with a code
// entered between (6).
func startedSite(t *testing.T) *keypad {
	t.Helper()
	k := keypadSite(t, testsupport.NewFactory())
	k.starts("power_on")
	k.starts("panic")
	k.starts("cosmic_ray")
	k.says("started")
	k.starts("panic")
	k.press("000000")
	return k
}

func TestTheEventsAPIKeepsTheStartsThatSaidWhyTheyStarted(t *testing.T) {
	t.Parallel()
	k := startedSite(t)
	cases := map[string]struct {
		query string
		ids   []any
		total float64
	}{
		"a reason it knows":        {"?start_reason=panic", []any{5.0, 2.0}, 2},
		"a value it does not know": {"?start_reason=cosmic_ray", []any{3.0}, 1},
		"a reason no start gave":   {"?start_reason=brownout", []any{}, 0},
		"with the reason filter":   {"?reason=started&start_reason=power_on", []any{1.0}, 1},
		"every start without it":   {"?reason=started", []any{5.0, 4.0, 3.0, 2.0, 1.0}, 5},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			if diff := cmp.Diff([]any{c.ids, c.total}, eventIDs(t, k.b, c.query)); diff != "" {
				t.Errorf("ids, total (-want +got):\n%s", diff)
			}
		})
	}
}

// startReasons is what the events page's start reason filter offers, the
// one chosen marked.
func startReasons(chosen string) []string {
	out := []string{"Every start reason"}
	for _, r := range []string{"Power on", "Software restart", "Panic", "Watchdog", "Brownout", "Reset pin", "Unknown"} {
		if r == chosen {
			r += " (selected)"
		}
		out = append(out, r)
	}
	return out
}

func TestTheEventsPageFiltersByWhyAControllerStarted(t *testing.T) {
	t.Parallel()
	k := startedSite(t)
	k.b.Get("/")

	page := testsupport.ParsePage(k.b.Get("/admin/events?start_reason=panic").Body)

	panicked := []string{"Mon 5 Jan 2026 09:00:00", "Lobby", "No one", "Info Started", "Door activity Its firmware panicked."}
	want := []any{[][]string{eventsHead, panicked, panicked, {"Showing 1 to 2 of 2"}}, startReasons("Panic")}
	if diff := cmp.Diff(want, []any{page.Tables["Events"], page.Options["start_reason"]}); diff != "" {
		t.Errorf("events, start reasons (-want +got):\n%s", diff)
	}
}

func TestTheEventsPageOffersAStartReasonItDoesNotKnowAsGiven(t *testing.T) {
	t.Parallel()
	k := startedSite(t)
	k.b.Get("/")

	page := testsupport.ParsePage(k.b.Get("/admin/events?start_reason=cosmic_ray").Body)

	rows := page.Tables["Events"]
	want := []any{append(startReasons(""), "cosmic_ray (selected)"), []string{"Showing 1 to 1 of 1"}}
	if diff := cmp.Diff(want, []any{page.Options["start_reason"], rows[len(rows)-1]}); diff != "" {
		t.Errorf("start reasons, events shown (-want +got):\n%s", diff)
	}
}

func TestTheEventsPageClearsItsUserKeepingItsStartReason(t *testing.T) {
	t.Parallel()
	k := startedSite(t)
	k.b.Get("/")

	got := pageOf(k.b.Get("/admin/events?user=2&start_reason=panic")).LinkTargets["Stop filtering by user"]

	if diff := cmp.Diff("/admin/events?start_reason=panic", got); diff != "" {
		t.Errorf("link (-want +got):\n%s", diff)
	}
}

func TestTheEventsPageIsPagedKeepingItsStartReason(t *testing.T) {
	t.Parallel()
	k := keypadSite(t, testsupport.NewFactory())
	for range 51 {
		if err := k.conn.Publish("doorcode/doors/1/event", testsupport.CBOR(map[string]any{"v": 1, "event": "started", "reason": "watchdog"}), false); err != nil {
			t.Fatal(err)
		}
	}
	run(t, k.a)
	k.b.Get("/")

	first := testsupport.ParsePage(k.b.Get("/admin/events?start_reason=watchdog").Body)
	second := testsupport.ParsePage(k.b.Get(first.NavTargets[len(first.NavTargets)-1]).Body)

	want := []any{
		"/admin/events?page=2&start_reason=watchdog", []string{"Showing 51 to 51 of 51"}, "/admin/events?page=1&start_reason=watchdog",
	}
	got := []any{first.NavTargets[len(first.NavTargets)-1], second.Tables["Events"][2], second.NavTargets[len(second.NavTargets)-1]}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("older page, what it shows, newer page (-want +got):\n%s", diff)
	}
}
