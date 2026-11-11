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

func TestADoorPageShowsTheAccessPointAndWhyTheLinkLastDropped(t *testing.T) {
	t.Parallel()
	cases := map[string]struct {
		report map[string]any
		want   []string
	}{
		"a reason it names": {
			map[string]any{"bssid": "f09fc2a1e001", "channel": 11, "wifi_drop_reason": 200},
			[]string{"f0:9f:c2:a1:e0:01, channel 11", "It stopped hearing the access point (code 200)"},
		},
		"a reason it does not name": {
			map[string]any{"bssid": "0a0b0c0d0e0f", "channel": 1, "wifi_drop_reason": 57},
			[]string{"0a:0b:0c:0d:0e:0f, channel 1", "Code 57"},
		},
		"a report from before the link was reported": {nil, []string{"None reported", "None reported"}},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			k := keypadSite(t, testsupport.NewFactory())
			k.report(c.report)

			page := doorPage(t, k.b, "/admin/doors/1")

			if diff := cmp.Diff(c.want, []string{page.Details["Access point"], page.Details["Last Wi-Fi drop"]}); diff != "" {
				t.Errorf("access point, last drop (-want +got):\n%s", diff)
			}
		})
	}
}

// linkHours has Lobby report how its links fared at 06:40, 09:10, 09:40,
// 10:05, and 11:20 on Monday 5 January: in the hour from 06:00 nothing
// lost; from 09:00 4 of 20 checks lost, 20 of 200 TCP segments sent again
// and 3 dropped, a Wi-Fi drop and 2 roams; from 10:00 nothing sent; and
// from 11:00 none of 4 checks lost, none of 50 segments sent again, and 2
// Wi-Fi drops and a roam.
func linkHours(k *keypad) {
	k.t.Helper()
	for _, r := range []struct {
		at                                        string
		sent, lost, tcp, again, drops, wifi, roam int
	}{
		{"2026-01-05T06:40:00Z", 5, 0, 10, 0, 0, 0, 0},
		{"2026-01-05T09:10:00Z", 10, 1, 100, 5, 2, 1, 0},
		{"2026-01-05T09:40:00Z", 10, 3, 100, 15, 1, 0, 2},
		{"2026-01-05T10:05:00Z", 0, 0, 0, 0, 0, 0, 0},
		{"2026-01-05T11:20:00Z", 4, 0, 50, 0, 0, 2, 1},
	} {
		k.at(r.at)
		k.report(map[string]any{
			"checks_sent": r.sent, "checks_lost": r.lost, "tcp_sent": r.tcp, "tcp_retransmits": r.again, "tcp_drops": r.drops,
			"wifi_drops": r.wifi, "roams": r.roam,
		})
	}
	k.at("2026-01-05T12:30:00Z")
}

func TestADoorPageChartsHowItsLinksFaredHourByHour(t *testing.T) {
	t.Parallel()
	k := keypadSite(t, testsupport.NewFactory())
	linkHours(k)
	k.b.Get("/")

	page := testsupport.ParsePage(k.b.Get("/admin/doors/1?range=6h").Body)

	times := []string{"Mon 5 Jan 06:30", "Mon 5 Jan 12:30"}
	want := []any{
		map[string]testsupport.Chart{
			"Code checks lost over the last 6 hours": {Lines: []string{"0,125 250,5 450,125"}, Labels: append([]string{"20 %", "0 %"}, times...)},
			"TCP retransmits over the last 6 hours":  {Lines: []string{"0,125 250,5 450,125"}, Labels: append([]string{"10 %", "0 %"}, times...)},
			"TCP drops per hour over the last 6 hours": {
				Lines: []string{"0,125 250,5 350,125 450,125"}, Labels: append([]string{"3", "0"}, times...),
			},
			"Wi-Fi drops and roams per hour over the last 6 hours": {
				Lines:  []string{"0,125 250,65 350,125 450,5", "0,125 250,5 350,125 450,65"},
				Series: []string{"viz-line-1", "viz-line-2"}, Labels: append([]string{"2", "0"}, times...),
			},
		},
		[]string{"Drops", "Roams"},
	}
	got := []any{map[string]testsupport.Chart{}, page.Lists["Lines of Wi-Fi drops and roams per hour over the last 6 hours"]}
	for label := range want[0].(map[string]testsupport.Chart) {
		got[0].(map[string]testsupport.Chart)[label] = page.Charts[label]
	}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("link charts, legend (-want +got):\n%s", diff)
	}
}

func TestADoorPageChartsItsLinksOverLongRangesFromTheHourlyRollups(t *testing.T) {
	t.Parallel()
	k := keypadSite(t, testsupport.NewFactory())
	linkHours(k)
	run(t, k.a)

	got := doorPage(t, k.b, "/admin/doors/1?range=7d").Charts["Code checks lost over the last 7 days"]

	want := testsupport.Chart{Lines: []string{"576.8,125 587.5,5 594.6,125"}, Labels: []string{"20 %", "0 %", "Mon 29 Dec 12:30", "Mon 5 Jan 12:30"}}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("check loss chart (-want +got):\n%s", diff)
	}
}

func TestADoorPageSaysWhenNoHourSentWhatItsLossChartsShareOut(t *testing.T) {
	t.Parallel()
	k := keypadSite(t, testsupport.NewFactory())
	k.report(nil)
	k.at("2026-01-05T09:30:00Z")

	charts := doorPage(t, k.b, "/admin/doors/1").Charts

	times := []string{"Sun 4 Jan 09:30", "Mon 5 Jan 09:30"}
	want := []testsupport.Chart{
		{Labels: append([]string{"No code checks sent in the last 24 hours."}, times...)},
		{Labels: append([]string{"No TCP segments sent in the last 24 hours."}, times...)},
		{Lines: []string{"587.5,65"}, Labels: append([]string{"0", "0"}, times...)},
	}
	got := []testsupport.Chart{
		charts["Code checks lost over the last 24 hours"], charts["TCP retransmits over the last 24 hours"],
		charts["TCP drops per hour over the last 24 hours"],
	}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("loss, retransmits, drops charts (-want +got):\n%s", diff)
	}
}
