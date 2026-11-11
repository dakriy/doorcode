package entrypoint_test

import (
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/dakriy/doorcode/internal/testsupport"
)

// linkReport is how Lobby's links fared since its last report: checks
// sent and lost, TCP segments sent, sent again, and dropped, Wi-Fi drops
// and roams, the reason of its latest drop, and the access point it is on:
// each count times scale and the reason 200 plus scale.
func linkReport(scale int, bssid string, channel int) map[string]any {
	return map[string]any{
		"checks_sent": 6 * scale, "checks_lost": scale, "tcp_sent": 5000000000 * int64(scale), "tcp_retransmits": 9 * scale,
		"tcp_drops": 2 * scale, "wifi_drops": 3 * scale, "wifi_drop_reason": 200 + scale, "roams": 4 * scale,
		"bssid": bssid, "channel": channel,
	}
}

func TestATelemetryReportsLinkIsInTheDoorsStateAndItsReports(t *testing.T) {
	t.Parallel()
	k := keypadSite(t, testsupport.NewFactory())

	k.report(linkReport(1, "f09fc2a1e001", 11))

	got := []any{k.state("/telemetry"), testsupport.JSONAt(send(t, k.b, "GET", "/api/v1/doors/1/telemetry?to=2026-01-06T00:00:00Z", "", 200).Body, "/samples/0")}

	report := testsupport.JSON(`{"at":"2026-01-05T09:00:00Z","signal":-61,"latency_ms":40,"uptime_s":3720,"free_bytes":120832,
		"firmware_version":"1.4.3","reconnects":2,"door_open":false,"opens":0,"mesh_layer":0,"mesh_parent":"","mesh_children":0,
		"checks_sent":6,"checks_lost":1,"tcp_sent":5000000000,"tcp_retransmits":9,"tcp_drops":2,"wifi_drops":3,"wifi_drop_reason":201,
		"roams":4,"bssid":"f09fc2a1e001","channel":11}`)
	if diff := cmp.Diff([]any{report, report}, got); diff != "" {
		t.Errorf("state, report (-want +got):\n%s", diff)
	}
}

func TestAReportWithoutTheLinkFieldsReadsAsNothingCountedAndNoAccessPoint(t *testing.T) {
	t.Parallel()
	k := keypadSite(t, testsupport.NewFactory())

	k.report(nil)

	var got []any
	for _, field := range []string{"checks_sent", "checks_lost", "tcp_sent", "tcp_retransmits", "tcp_drops", "wifi_drops", "wifi_drop_reason", "roams", "bssid", "channel"} {
		got = append(got, k.state("/telemetry/"+field))
	}
	want := []any{0.0, 0.0, 0.0, 0.0, 0.0, 0.0, 0.0, 0.0, "", 0.0}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("link fields (-want +got):\n%s", diff)
	}
}

func TestEachHourSumsItsLinkCountsAndKeepsTheLinkOfItsLastReport(t *testing.T) {
	t.Parallel()
	k := keypadSite(t, testsupport.NewFactory())
	for _, r := range []struct {
		at      string
		scale   int
		bssid   string
		channel int
	}{
		{"2026-01-05T09:10:00Z", 1, "f09fc2a1e001", 1},
		{"2026-01-05T09:40:00Z", 2, "f09fc2a1e002", 6},
		{"2026-01-05T10:05:00Z", 3, "f09fc2a1e003", 11},
	} {
		k.at(r.at)
		k.report(linkReport(r.scale, r.bssid, r.channel))
	}
	k.at("2026-01-05T11:00:00Z")
	run(t, k.a)

	got := testsupport.JSONAt(send(t, k.b, "GET", "/api/v1/doors/1/telemetry/hourly", "", 200).Body, "")

	want := testsupport.JSON(`{"rollups":[
		{"hour":"2026-01-05T09:00:00Z","samples":2,"signal":{"mean":-61,"min":-61,"max":-61},
			"latency_ms":{"mean":40,"min":40,"max":40},"free_bytes":{"mean":120832,"min":120832,"max":120832},"opens":0,
			"mesh_layer":0,"mesh_children":0,
			"checks_sent":18,"checks_lost":3,"tcp_sent":15000000000,"tcp_retransmits":27,"tcp_drops":6,"wifi_drops":9,
			"wifi_drop_reason":202,"roams":12,"bssid":"f09fc2a1e002","channel":6},
		{"hour":"2026-01-05T10:00:00Z","samples":1,"signal":{"mean":-61,"min":-61,"max":-61},
			"latency_ms":{"mean":40,"min":40,"max":40},"free_bytes":{"mean":120832,"min":120832,"max":120832},"opens":0,
			"mesh_layer":0,"mesh_children":0,
			"checks_sent":18,"checks_lost":3,"tcp_sent":15000000000,"tcp_retransmits":27,"tcp_drops":6,"wifi_drops":9,
			"wifi_drop_reason":203,"roams":12,"bssid":"f09fc2a1e003","channel":11}]}`)
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("rollups (-want +got):\n%s", diff)
	}
}
