package entrypoint_test

import (
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/dakriy/doorcode/internal/testsupport"
)

func TestADoorsStateIsUnknownUntilAControllerEnrollsAsIt(t *testing.T) {
	t.Parallel()
	b := signedInAdmin(t, testsupport.NewFactory())
	createDoor(t, b, frontDoor)

	got := b.Get("/api/v1/doors/1/state")

	want := testsupport.Response{Status: 200, ContentType: jsonType, Body: `{"enrolled":false,"online":false,"door":"unknown",` +
		`"held_open":false,"unresponsive":false,"firmware_version":""}` + "\n"}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("response (-want +got):\n%s", diff)
	}
}

func TestATelemetryReportBecomesTheDoorsState(t *testing.T) {
	t.Parallel()
	k := keypadSite(t, testsupport.NewFactory())
	k.at("2026-01-05T09:00:30Z")

	k.report(map[string]any{"door_open": true, "firmware_version": "1.5.0", "signal": -70, "opens": 1})

	want := testsupport.JSON(`{"enrolled":true,"online":true,"door":"open","opened_at":"2026-01-05T09:00:30Z","held_open":false,
		"unresponsive":false,"firmware_version":"1.5.0","last_seen_at":"2026-01-05T09:00:30Z",
		"telemetry":{"at":"2026-01-05T09:00:30Z","signal":-70,"latency_ms":40,"uptime_s":3720,"free_bytes":120832,
		"firmware_version":"1.5.0","reconnects":2,"door_open":true,"opens":1}}`)
	if diff := cmp.Diff(want, k.state("")); diff != "" {
		t.Errorf("state (-want +got):\n%s", diff)
	}
}

func TestADoorStaysOpenSinceItOpenedWhileItReportsItOpen(t *testing.T) {
	t.Parallel()
	k := keypadSite(t, testsupport.NewFactory())
	k.says("opened")
	k.at("2026-01-05T09:00:30Z")

	k.report(map[string]any{"door_open": true})

	got := []any{k.state("/opened_at"), k.reasons()}
	if diff := cmp.Diff([]any{"2026-01-05T09:00:00Z", []any{"forced open"}}, got); diff != "" {
		t.Errorf("opened at, reasons (-want +got):\n%s", diff)
	}
}

func TestADoorPageShowsTheDoorsLiveState(t *testing.T) {
	t.Parallel()
	k := keypadSite(t, testsupport.NewFactory())
	k.says("opened")
	k.at("2026-01-05T09:00:30Z")
	k.report(map[string]any{"door_open": true, "uptime_s": 266400})
	k.at("2026-01-05T09:01:00Z")
	run(t, k.a)

	page := doorPage(t, k.b, "/admin/doors/1")

	want := []any{
		map[string]string{
			"Hardware id": "A1", "Door": "Open since Mon 5 Jan 2026 09:00:00", "Firmware": "1.4.3", "Signal": "-61 dBm", "Latency": "40 ms",
			"Uptime": "3 d 2 h", "Free memory": "118 KiB", "Reconnects": "2",
			"Last seen": "Mon 5 Jan 2026 09:00:30", "Last report": "Mon 5 Jan 2026 09:00:30",
			"Running": "1.4.3", "Target": "None", "Rollout": "Not in the rollout", "Latest update": "Never sent one",
		},
		[][]string{{"When", "Alarm", "Details"},
			{"Mon 5 Jan 2026 09:01:00", "Held open", "Open for longer than 60 seconds."},
			{"Mon 5 Jan 2026 09:00:00", "Forced open", "Nothing unlocked it in the 6 seconds before it opened."},
		},
	}
	got := []any{page.Details, page.Tables["Alarms"]}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("details, alarms (-want +got):\n%s", diff)
	}
}

func TestADoorPageShowsHowLongTheControllerHasRun(t *testing.T) {
	t.Parallel()
	cases := map[string]struct {
		seconds int
		want    string
	}{
		"days":    {266400, "3 d 2 h"},
		"hours":   {3720, "1 h 2 min"},
		"minutes": {359, "5 min"},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			k := keypadSite(t, testsupport.NewFactory())
			k.report(map[string]any{"uptime_s": c.seconds})

			got := doorPage(t, k.b, "/admin/doors/1").Details["Uptime"]

			if diff := cmp.Diff(c.want, got); diff != "" {
				t.Errorf("uptime (-want +got):\n%s", diff)
			}
		})
	}
}

func TestADoorPageSaysWhatTheDoorIsWhenItIsClosed(t *testing.T) {
	t.Parallel()
	k := keypadSite(t, testsupport.NewFactory())
	k.says("opened")
	k.says("closed")

	got := doorPage(t, k.b, "/admin/doors/1").Details["Door"]

	if diff := cmp.Diff("Closed", got); diff != "" {
		t.Errorf("door (-want +got):\n%s", diff)
	}
}

func TestTheDoorsListShowsEachDoorsStateBadges(t *testing.T) {
	t.Parallel()
	cases := map[string]struct {
		arrange func(k *keypad)
		want    string
	}{
		"closed":       {func(k *keypad) { k.says("closed") }, "Online"},
		"open":         {func(k *keypad) { k.says("opened") }, "Online Open"},
		"held open":    {func(k *keypad) { k.says("opened"); k.at("2026-01-05T09:01:00Z"); run(k.t, k.a) }, "Online Held open"},
		"unresponsive": {func(k *keypad) { k.at("2026-01-05T09:05:00Z"); run(k.t, k.a) }, "Online Unresponsive"},
		"unresponsive and held open": {func(k *keypad) {
			k.says("opened")
			k.at("2026-01-05T09:05:00Z")
			run(k.t, k.a)
		}, "Online Unresponsive Held open"},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			k := keypadSite(t, testsupport.NewFactory())
			c.arrange(k)

			rows := doorPage(t, k.b, "/admin/doors").Tables["Doors"]

			if diff := cmp.Diff(c.want, rows[1][2]); diff != "" {
				t.Errorf("status (-want +got):\n%s", diff)
			}
		})
	}
}

func TestLiveStreamTellsPagesWhatChangedOfADoorsState(t *testing.T) {
	t.Parallel()
	cases := map[string]struct {
		change func(k *keypad)
		want   string
	}{
		"a report":                    {func(k *keypad) { k.report(nil) }, "event: door\ndata: 1\n\n"},
		"the door opening":            {func(k *keypad) { k.says("opened") }, "event: doors\ndata: changed\n\n"},
		"the door going unresponsive": {func(k *keypad) { k.at("2026-01-05T09:05:00Z"); run(k.t, k.a) }, "event: doors\ndata: changed\n\n"},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			k := keypadSite(t, testsupport.NewFactory())
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

// reported is a keypad site whose Lobby reported at 09:10 (-60 dBm, 40
// ms, 100000 bytes free, 2 opens), at 09:40 (-70 dBm, 60 ms, 120000
// bytes, 3 opens), and at 10:05 (-65 dBm, 50 ms, 110000 bytes, no opens)
// on Monday 5 January, with the stub clock at 10:30.
func reported(t *testing.T, f *testsupport.Factory) *keypad {
	t.Helper()
	k := keypadSite(t, f)
	for _, r := range []struct {
		at                           string
		signal, latency, free, opens int
	}{
		{"2026-01-05T09:10:00Z", -60, 40, 100000, 2},
		{"2026-01-05T09:40:00Z", -70, 60, 120000, 3},
		{"2026-01-05T10:05:00Z", -65, 50, 110000, 0},
	} {
		k.at(r.at)
		k.report(map[string]any{"signal": r.signal, "latency_ms": r.latency, "free_bytes": r.free, "opens": r.opens})
	}
	k.at("2026-01-05T10:30:00Z")
	return k
}

// sampleTimes is when each report the telemetry API lists for query was
// received.
func sampleTimes(t *testing.T, b *testsupport.Browser, query string) []any {
	t.Helper()
	samples, _ := testsupport.JSONAt(send(t, b, "GET", "/api/v1/doors/1/telemetry"+query, "", 200).Body, "/samples").([]any)
	out := []any{}
	for _, s := range samples {
		out = append(out, s.(map[string]any)["at"])
	}
	return out
}

// rollupHours is the hour of each rollup the hourly telemetry API lists
// for query.
func rollupHours(t *testing.T, b *testsupport.Browser, query string) []any {
	t.Helper()
	rollups, _ := testsupport.JSONAt(send(t, b, "GET", "/api/v1/doors/1/telemetry/hourly"+query, "", 200).Body, "/rollups").([]any)
	out := []any{}
	for _, r := range rollups {
		out = append(out, r.(map[string]any)["hour"])
	}
	return out
}

func TestTheTelemetryAPIListsADoorsReportsOverARange(t *testing.T) {
	t.Parallel()
	k := reported(t, testsupport.NewFactory())
	cases := map[string]struct {
		query string
		want  []any
	}{
		"the last 24 hours":   {"", []any{"2026-01-05T09:10:00Z", "2026-01-05T09:40:00Z", "2026-01-05T10:05:00Z"}},
		"from a time":         {"?from=2026-01-05T09:40:00Z", []any{"2026-01-05T09:40:00Z", "2026-01-05T10:05:00Z"}},
		"up to a time":        {"?to=2026-01-05T09:40:00Z", []any{"2026-01-05T09:10:00Z"}},
		"a day before a time": {"?to=2026-01-06T09:30:00Z", []any{"2026-01-05T09:40:00Z", "2026-01-05T10:05:00Z"}},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			if diff := cmp.Diff(c.want, sampleTimes(t, k.b, c.query)); diff != "" {
				t.Errorf("reports (-want +got):\n%s", diff)
			}
		})
	}
}

func TestTheTelemetryAPIShowsEachReport(t *testing.T) {
	t.Parallel()
	k := reported(t, testsupport.NewFactory())

	got := testsupport.JSONAt(send(t, k.b, "GET", "/api/v1/doors/1/telemetry", "", 200).Body, "/samples/0")

	want := testsupport.JSON(`{"at":"2026-01-05T09:10:00Z","signal":-60,"latency_ms":40,"uptime_s":3720,"free_bytes":100000,
		"firmware_version":"1.4.3","reconnects":2,"door_open":false,"opens":2}`)
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("report (-want +got):\n%s", diff)
	}
}

func TestThePeriodicWorkRollsUpEachHourThatEndedFromItsReports(t *testing.T) {
	t.Parallel()
	k := reported(t, testsupport.NewFactory())
	k.at("2026-01-05T11:00:00Z")
	run(t, k.a)

	got := testsupport.JSONAt(send(t, k.b, "GET", "/api/v1/doors/1/telemetry/hourly", "", 200).Body, "")

	want := testsupport.JSON(`{"rollups":[
		{"hour":"2026-01-05T09:00:00Z","samples":2,"signal":{"mean":-65,"min":-70,"max":-60},
			"latency_ms":{"mean":50,"min":40,"max":60},"free_bytes":{"mean":110000,"min":100000,"max":120000},"opens":5},
		{"hour":"2026-01-05T10:00:00Z","samples":1,"signal":{"mean":-65,"min":-65,"max":-65},
			"latency_ms":{"mean":50,"min":50,"max":50},"free_bytes":{"mean":110000,"min":110000,"max":110000},"opens":0}]}`)
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("rollups (-want +got):\n%s", diff)
	}
}

func TestThePeriodicWorkRollsUpOnlyHoursThatEnded(t *testing.T) {
	t.Parallel()
	k := reported(t, testsupport.NewFactory())
	k.at("2026-01-05T10:59:59Z")
	run(t, k.a)

	if diff := cmp.Diff([]any{"2026-01-05T09:00:00Z"}, rollupHours(t, k.b, "")); diff != "" {
		t.Errorf("hours (-want +got):\n%s", diff)
	}
}

func TestEachDoorsReportsAreRolledUpApart(t *testing.T) {
	t.Parallel()
	f := testsupport.NewFactory()
	a, b := pushSite(t, f)
	for _, door := range []string{"1", "2"} {
		f.BrokerStub.Inject("doorcode/doors/"+door+"/telemetry", reportOf(map[string]any{"signal": -50}))
	}
	f.BrokerStub.Inject("doorcode/doors/2/telemetry", reportOf(map[string]any{"signal": -70}))
	run(t, a)
	f.ClockStub.Set("2026-01-05T10:00:00Z")
	run(t, a)

	got := []any{
		testsupport.JSONAt(send(t, b, "GET", "/api/v1/doors/1/telemetry/hourly", "", 200).Body, "/rollups/0/signal"),
		testsupport.JSONAt(send(t, b, "GET", "/api/v1/doors/2/telemetry/hourly", "", 200).Body, "/rollups/0/signal"),
	}
	want := []any{testsupport.JSON(`{"mean":-50,"min":-50,"max":-50}`), testsupport.JSON(`{"mean":-60,"min":-70,"max":-50}`)}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("Lobby's, Dock's signal (-want +got):\n%s", diff)
	}
}

func TestThePeriodicWorkRemovesReportsAndRollupsPastTheirRetention(t *testing.T) {
	t.Parallel()
	f := testsupport.NewFactory()
	f.StoreStub.SeedSetting("telemetry_retention_days", "1", testsupport.DefaultNow)
	f.StoreStub.SeedSetting("telemetry_rollup_retention_days", "1", testsupport.DefaultNow)
	k := reported(t, f)
	k.at("2026-01-06T10:00:00Z")
	run(t, k.a)
	b := signIn(t, f, k.a.Handler())

	got := []any{sampleTimes(t, b, "?from=2026-01-05T00:00:00Z"), rollupHours(t, b, "?from=2026-01-05T00:00:00Z")}

	want := []any{[]any{"2026-01-05T10:05:00Z"}, []any{"2026-01-05T10:00:00Z"}}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("reports, rollup hours (-want +got):\n%s", diff)
	}
}

func TestARollupKeepsSummingUpReportsRemovedSince(t *testing.T) {
	t.Parallel()
	f := testsupport.NewFactory()
	f.StoreStub.SeedSetting("telemetry_retention_days", "1", testsupport.DefaultNow)
	k := reported(t, f)
	k.at("2026-01-06T09:20:00Z")
	run(t, k.a)
	k.at("2026-01-06T09:21:00Z")
	run(t, k.a)
	b := signIn(t, f, k.a.Handler())

	got := testsupport.JSONAt(send(t, b, "GET", "/api/v1/doors/1/telemetry/hourly", "", 200).Body, "/rollups/0")

	want := testsupport.JSON(`{"hour":"2026-01-05T09:00:00Z","samples":2,"signal":{"mean":-65,"min":-70,"max":-60},
		"latency_ms":{"mean":50,"min":40,"max":60},"free_bytes":{"mean":110000,"min":100000,"max":120000},"opens":5}`)
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("first rollup (-want +got):\n%s", diff)
	}
}

func TestTheTelemetryAPIRefusesARangeItCannotRead(t *testing.T) {
	t.Parallel()
	k := keypadSite(t, testsupport.NewFactory())
	var got []any
	for _, target := range []string{"/telemetry?from=monday", "/telemetry?to=monday", "/telemetry?from=2026-01-05T10:00:00Z&to=2026-01-05T10:00:00Z",
		"/telemetry/hourly?from=monday"} {
		got = append(got, k.b.Get("/api/v1/doors/1"+target).Status)
	}

	if diff := cmp.Diff([]any{400, 400, 400, 400}, got); diff != "" {
		t.Errorf("statuses (-want +got):\n%s", diff)
	}
}

func TestTheStateAndTelemetryOfADoorThatIsNotThereAreNotFound(t *testing.T) {
	t.Parallel()
	b := signedInAdmin(t, testsupport.NewFactory())
	var got []any
	for _, target := range []string{"/state", "/telemetry", "/telemetry/hourly"} {
		got = append(got, b.Get("/api/v1/doors/9"+target).Status)
	}

	if diff := cmp.Diff([]any{404, 404, 404}, got); diff != "" {
		t.Errorf("statuses (-want +got):\n%s", diff)
	}
}

func TestADoorPageChartsItsSignalAndLatencyOverTheRangeChosen(t *testing.T) {
	t.Parallel()
	k := reported(t, testsupport.NewFactory())
	k.at("2026-01-05T12:00:00Z")

	charts := doorPage(t, k.b, "/admin/doors/1?range=6h").Charts

	want := map[string]testsupport.Chart{
		"Signal over the last 6 hours": {
			Lines:  []string{"316.7,5.0 366.7,125.0 408.3,65.0"},
			Labels: []string{"-60 dBm", "-70 dBm", "Mon 5 Jan 06:00", "Mon 5 Jan 12:00"},
		},
		"Latency over the last 6 hours": {
			Lines:  []string{"316.7,125.0 366.7,5.0 408.3,65.0"},
			Labels: []string{"60 ms", "40 ms", "Mon 5 Jan 06:00", "Mon 5 Jan 12:00"},
		},
	}
	if diff := cmp.Diff(want, charts); diff != "" {
		t.Errorf("charts (-want +got):\n%s", diff)
	}
}

func TestADoorPageChartsLongRangesFromTheHourlyRollups(t *testing.T) {
	t.Parallel()
	k := reported(t, testsupport.NewFactory())
	k.at("2026-01-05T12:00:00Z")
	run(t, k.a)

	charts := doorPage(t, k.b, "/admin/doors/1?range=7d").Charts

	want := testsupport.Chart{
		Lines:  []string{"589.3,65.0 592.9,65.0"},
		Labels: []string{"50 ms", "50 ms", "Mon 29 Dec 12:00", "Mon 5 Jan 12:00"},
	}
	if diff := cmp.Diff(want, charts["Latency over the last 7 days"]); diff != "" {
		t.Errorf("latency chart (-want +got):\n%s", diff)
	}
}

func TestADoorPageChartsTheLast24HoursUnlessAskedForAnotherRange(t *testing.T) {
	t.Parallel()
	k := keypadSite(t, testsupport.NewFactory())

	page := doorPage(t, k.b, "/admin/doors/1?range=1y")

	want := []any{
		testsupport.Chart{Labels: []string{"Nothing reported in the last 24 hours.", "Sun 4 Jan 09:00", "Mon 5 Jan 09:00"}},
		map[string]string{
			"Show the last 6 hours": "/admin/doors/1?range=6h", "Show the last 7 days": "/admin/doors/1?range=7d",
			"Show the last 30 days": "/admin/doors/1?range=30d",
		},
	}
	if diff := cmp.Diff(want, []any{page.Charts["Signal over the last 24 hours"], page.LinkTargets}); diff != "" {
		t.Errorf("signal chart, ranges offered (-want +got):\n%s", diff)
	}
}

func TestTelemetryPagesAndTheAPIFailWhenTheStoreDoes(t *testing.T) {
	t.Parallel()
	cases := map[string]struct {
		fail   func(f *testsupport.Factory)
		target string
	}{
		"a door page's reports": {func(f *testsupport.Factory) { f.StoreStub.FailTelemetry() }, "/admin/doors/1"},
		"a door page's rollups": {func(f *testsupport.Factory) { f.StoreStub.FailTelemetry() }, "/admin/doors/1?range=30d"},
		"the reports API":       {func(f *testsupport.Factory) { f.StoreStub.FailTelemetry() }, "/api/v1/doors/1/telemetry"},
		"the rollups API":       {func(f *testsupport.Factory) { f.StoreStub.FailTelemetry() }, "/api/v1/doors/1/telemetry/hourly"},
		"the state API":         {func(f *testsupport.Factory) { f.StoreStub.FailDoors() }, "/api/v1/doors/1/state"},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			f := testsupport.NewFactory()
			_, b, _, _ := enrolledDoor(t, f)
			b.Get("/")
			c.fail(f)

			if diff := cmp.Diff(500, b.Get(c.target).Status); diff != "" {
				t.Errorf("status (-want +got):\n%s", diff)
			}
		})
	}
}

func TestADoorCommanderSeesTheDoorsLiveState(t *testing.T) {
	t.Parallel()
	f := testsupport.NewFactory()
	a, b, _, _ := enrolledDoor(t, f)
	f.BrokerStub.Inject("doorcode/doors/1/event", testsupport.CBOR(map[string]any{"v": 1, "event": "closed"}))
	run(t, a)
	holdingOnly(t, f, b, `["door-commander"]`)

	got := doorPage(t, b, "/admin/doors/1").Details["Door"]

	if diff := cmp.Diff("Closed", got); diff != "" {
		t.Errorf("door (-want +got):\n%s", diff)
	}
}
