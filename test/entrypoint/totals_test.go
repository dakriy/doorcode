package entrypoint_test

import (
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/dakriy/doorcode/internal/testsupport"
)

// countFields are the counts a telemetry report gives, each a total since
// the controller started.
var countFields = []string{"reconnects", "opens", "checks_sent", "checks_lost", "tcp_sent", "tcp_retransmits", "tcp_drops", "wifi_drops", "roams"}

// totals is a report whose counts, in the order of countFields, are
// values.
func totals(values ...int) map[string]any {
	out := map[string]any{}
	for i, field := range countFields {
		out[field] = values[i]
	}
	return out
}

// sampleCounts is the counts of the report the telemetry API lists at
// index, in the order of countFields, and its totals.
func sampleCounts(t *testing.T, k *keypad, index string) []any {
	t.Helper()
	sample := testsupport.JSONAt(send(t, k.b, "GET", "/api/v1/doors/1/telemetry?to=2026-01-06T00:00:00Z", "", 200).Body, "/samples/"+index).(map[string]any)
	counts, reported := []any{}, []any{}
	for _, field := range countFields {
		counts = append(counts, sample[field])
		reported = append(reported, sample["totals"].(map[string]any)[field])
	}
	return []any{counts, reported}
}

func TestAReportCountsWhatItsTotalsGrewBySinceTheReportBefore(t *testing.T) {
	t.Parallel()
	k := keypadSite(t, testsupport.NewFactory())
	k.report(totals(2, 1, 6, 1, 100, 5, 2, 1, 0))
	k.at("2026-01-05T09:01:00Z")

	k.report(totals(2, 4, 10, 1, 150, 8, 2, 3, 1))

	want := []any{
		[]any{0.0, 3.0, 4.0, 0.0, 50.0, 3.0, 0.0, 2.0, 1.0},
		[]any{2.0, 4.0, 10.0, 1.0, 150.0, 8.0, 2.0, 3.0, 1.0},
	}
	if diff := cmp.Diff(want, sampleCounts(t, k, "1")); diff != "" {
		t.Errorf("counts, totals (-want +got):\n%s", diff)
	}
}

func TestATotalLowerThanInTheReportBeforeCountsInFull(t *testing.T) {
	t.Parallel()
	k := keypadSite(t, testsupport.NewFactory())
	k.report(totals(2, 5, 6, 1, 4294967200, 5, 2, 1, 0))
	k.at("2026-01-05T09:01:00Z")

	k.report(totals(2, 1, 8, 1, 100, 7, 2, 1, 0))

	want := []any{0.0, 1.0, 2.0, 0.0, 100.0, 2.0, 0.0, 0.0, 0.0}
	if diff := cmp.Diff(want, sampleCounts(t, k, "1")[0]); diff != "" {
		t.Errorf("counts (-want +got):\n%s", diff)
	}
}

func TestAReportAfterTheControllerRestartedCountsEveryTotalInFull(t *testing.T) {
	t.Parallel()
	k := keypadSite(t, testsupport.NewFactory())
	k.report(totals(2, 5, 6, 1, 100, 5, 2, 1, 0))
	k.at("2026-01-05T09:01:00Z")

	k.report(with(totals(2, 6, 7, 1, 120, 5, 2, 1, 1), map[string]any{"uptime_s": 60}))

	want := []any{2.0, 6.0, 7.0, 1.0, 120.0, 5.0, 2.0, 1.0, 1.0}
	if diff := cmp.Diff(want, sampleCounts(t, k, "1")[0]); diff != "" {
		t.Errorf("counts (-want +got):\n%s", diff)
	}
}

func TestTheFirstReportOfADoorCountsEveryTotalInFull(t *testing.T) {
	t.Parallel()
	k := keypadSite(t, testsupport.NewFactory())

	k.report(totals(2, 5, 6, 1, 100, 5, 2, 1, 3))

	want := []any{2.0, 5.0, 6.0, 1.0, 100.0, 5.0, 2.0, 1.0, 3.0}
	if diff := cmp.Diff(want, sampleCounts(t, k, "0")[0]); diff != "" {
		t.Errorf("counts (-want +got):\n%s", diff)
	}
}
