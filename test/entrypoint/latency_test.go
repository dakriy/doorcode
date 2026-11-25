package entrypoint_test

import (
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/dakriy/doorcode/internal/testsupport"
)

// reportsUntimed has Lobby's controller report its telemetry as
// reportOf says, but for changed, with no latency, and the server handle
// it.
func (k *keypad) reportsUntimed(changed map[string]any) {
	k.t.Helper()
	fields := reportFields(changed)
	delete(fields, "latency_ms")
	k.publish("doorcode/doors/1/telemetry", testsupport.CBOR(fields))
}

// latencyShown is the latency the telemetry API gives of Lobby's latest
// report, what Lobby's page shows of it, and the latency gauge's samples.
func (k *keypad) latencyShown() []any {
	k.t.Helper()
	sample := testsupport.JSONAt(send(k.t, k.b, "GET", "/api/v1/doors/1/telemetry", "", 200).Body, "/samples")
	samples, _ := sample.([]any)
	last := samples[len(samples)-1].(map[string]any)["latency_ms"]
	gauge := testsupport.Samples(scrapeAs(k.t, k.a.Handler(), k.b), "doorcode_door_latency_seconds")
	return []any{last, doorPage(k.t, k.b, "/admin/doors/1").Details["Latency"], gauge}
}

func TestAReportWithoutALatencyIsKeptAndShownAsNoneAndOneWithALatencyAsIt(t *testing.T) {
	t.Parallel()
	k := keypadSite(t, testsupport.NewFactory())
	k.reportsUntimed(nil)
	k.at("2026-01-05T09:00:30Z")
	untimed := k.latencyShown()
	k.at("2026-01-05T09:01:00Z")
	k.report(map[string]any{"latency_ms": 25})

	k.at("2026-01-05T09:01:30Z")

	want := []any{
		[]any{nil, "None measured", []string{}},
		[]any{25.0, "25 ms", []string{`doorcode_door_latency_seconds{door="Lobby",door_id="1"} 0.025`}},
	}
	if diff := cmp.Diff(want, []any{untimed, k.latencyShown()}); diff != "" {
		t.Errorf("without a latency, with one: API, page, gauge (-want +got):\n%s", diff)
	}
}

func TestAnHourSumsUpTheLatencyOfOnlyTheReportsThatCarryOne(t *testing.T) {
	t.Parallel()
	k := keypadSite(t, testsupport.NewFactory())
	k.at("2026-01-05T09:10:00Z")
	k.reportsUntimed(nil)
	k.at("2026-01-05T09:20:00Z")
	k.report(map[string]any{"latency_ms": 30})
	k.at("2026-01-05T09:30:00Z")
	k.report(map[string]any{"latency_ms": 50})
	k.at("2026-01-05T10:10:00Z")
	k.reportsUntimed(nil)
	k.at("2026-01-05T11:00:00Z")
	run(t, k.a)

	body := send(t, k.b, "GET", "/api/v1/doors/1/telemetry/hourly", "", 200).Body

	want := []any{3.0, 2.0, testsupport.JSON(`{"mean":40,"min":30,"max":50}`), 1.0, 0.0, nil}
	got := []any{testsupport.JSONAt(body, "/rollups/0/samples"), testsupport.JSONAt(body, "/rollups/0/latency_samples"), testsupport.JSONAt(body, "/rollups/0/latency_ms"),
		testsupport.JSONAt(body, "/rollups/1/samples"), testsupport.JSONAt(body, "/rollups/1/latency_samples"), testsupport.JSONAt(body, "/rollups/1/latency_ms")}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("each hour's samples, samples with a latency, latency (-want +got):\n%s", diff)
	}
}

func TestTheDashboardWeighsEachHoursLatencyByTheReportsThatCarryOne(t *testing.T) {
	t.Parallel()
	k := keypadSite(t, testsupport.NewFactory())
	for _, r := range []struct {
		at      string
		latency any
	}{
		{"2026-01-05T09:10:00Z", 40}, {"2026-01-05T09:20:00Z", 60},
		{"2026-01-05T10:10:00Z", nil}, {"2026-01-05T10:20:00Z", nil}, {"2026-01-05T10:30:00Z", 110},
		{"2026-01-05T11:10:00Z", nil},
	} {
		k.at(r.at)
		if r.latency == nil {
			k.reportsUntimed(nil)
		} else {
			k.report(map[string]any{"latency_ms": r.latency})
		}
	}
	k.at("2026-01-05T12:00:00Z")
	run(t, k.a)

	body := send(t, k.b, "GET", "/api/v1/stats/doors?range=today", "", 200).Body
	row := pageOf(k.b.Get("/admin?range=today")).Tables["Door uptime"][1]

	want := []any{6.0, 70.0, 50.0, 110.0, nil, "70 ms"}
	got := []any{testsupport.JSONAt(body, "/doors/0/samples"), testsupport.JSONAt(body, "/doors/0/latency_ms"),
		testsupport.JSONAt(body, "/doors/0/trend/0/latency_ms"), testsupport.JSONAt(body, "/doors/0/trend/1/latency_ms"),
		testsupport.JSONAt(body, "/doors/0/trend/2/latency_ms"), row[len(row)-1]}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("samples, mean latency, each hour's, the page's (-want +got):\n%s", diff)
	}
}

func TestADoorWhoseReportsCarryNoLatencyShowsNoneOnTheDashboard(t *testing.T) {
	t.Parallel()
	k := keypadSite(t, testsupport.NewFactory())
	k.at("2026-01-05T09:10:00Z")
	k.reportsUntimed(nil)
	k.at("2026-01-05T10:00:00Z")
	run(t, k.a)

	row := pageOf(k.b.Get("/admin?range=today")).Tables["Door uptime"][1]
	body := send(t, k.b, "GET", "/api/v1/stats/doors?range=today", "", 200).Body

	want := []any{"None measured", 1.0, nil}
	if diff := cmp.Diff(want, []any{row[len(row)-1], testsupport.JSONAt(body, "/doors/0/samples"), testsupport.JSONAt(body, "/doors/0/latency_ms")}); diff != "" {
		t.Errorf("page, samples, latency (-want +got):\n%s", diff)
	}
}

func TestADoorsHourlyLatencyChartLeavesOutTheHoursWithoutOne(t *testing.T) {
	t.Parallel()
	k := keypadSite(t, testsupport.NewFactory())
	k.at("2026-01-05T09:10:00Z")
	k.report(map[string]any{"latency_ms": 30})
	k.at("2026-01-05T10:10:00Z")
	k.reportsUntimed(nil)
	k.at("2026-01-05T11:10:00Z")
	k.report(map[string]any{"latency_ms": 50})
	k.at("2026-01-05T12:00:00Z")
	run(t, k.a)

	got := doorPage(t, k.b, "/admin/doors/1?range=7d").Charts["Latency over the last 7 days"]

	want := testsupport.Chart{Lines: []string{"589.3,125 596.4,5"}, Labels: []string{"50 ms", "30 ms", "Mon 29 Dec 12:00", "Mon 5 Jan 12:00"}}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("latency chart (-want +got):\n%s", diff)
	}
}
