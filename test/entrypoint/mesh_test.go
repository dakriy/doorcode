package entrypoint_test

import (
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/dakriy/doorcode/internal/app"
	"github.com/dakriy/doorcode/internal/testsupport"
)

// meshed is a push site whose Lobby (1) reported that it hops through the
// controller hardwareID at layer 2 with one controller hopping through it,
// and Dock (2) that one controller hops through it.
func meshed(t *testing.T, hardwareID string) (*app.App, *testsupport.Browser) {
	t.Helper()
	f := testsupport.NewFactory()
	a, b := pushSite(t, f)
	f.BrokerStub.Inject("doorcode/doors/1/telemetry", reportOf(map[string]any{"mesh_layer": 2, "mesh_parent": hardwareID, "mesh_children": 1}))
	f.BrokerStub.Inject("doorcode/doors/2/telemetry", reportOf(map[string]any{"mesh_layer": 1, "mesh_parent": "C3", "mesh_children": 1}))
	run(t, a)
	return a, b
}

func TestATelemetryReportsMeshIsTheDoorsState(t *testing.T) {
	t.Parallel()
	_, b := meshed(t, "B2")

	got := testsupport.JSONAt(send(t, b, "GET", "/api/v1/doors/1/state", "", 200).Body, "/telemetry")

	want := testsupport.JSON(`{"at":"2026-01-05T09:00:00Z","signal":-61,"latency_ms":40,"uptime_s":3720,"free_bytes":120832,
		"firmware_version":"1.4.3","reconnects":2,"door_open":false,"opens":0,"mesh_layer":2,"mesh_parent":"B2","mesh_children":1,
		"checks_sent":0,"checks_lost":0,"tcp_sent":0,"tcp_retransmits":0,"tcp_drops":0,"wifi_drops":0,"wifi_drop_reason":0,"roams":0,"bssid":"","channel":0,"config_digest":"",
		"totals":{"reconnects":2,"opens":0,"checks_sent":0,"checks_lost":0,"tcp_sent":0,"tcp_retransmits":0,"tcp_drops":0,"wifi_drops":0,"roams":0}}`)
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("telemetry (-want +got):\n%s", diff)
	}
}

func TestTheTelemetryAPIListsEachReportsMesh(t *testing.T) {
	t.Parallel()
	_, b := meshed(t, "B2")

	got := testsupport.JSONAt(send(t, b, "GET", "/api/v1/doors/1/telemetry?to=2026-01-06T00:00:00Z", "", 200).Body, "/samples/0")

	want := testsupport.JSON(`{"at":"2026-01-05T09:00:00Z","signal":-61,"latency_ms":40,"uptime_s":3720,"free_bytes":120832,
		"firmware_version":"1.4.3","reconnects":2,"door_open":false,"opens":0,"mesh_layer":2,"mesh_parent":"B2","mesh_children":1,
		"checks_sent":0,"checks_lost":0,"tcp_sent":0,"tcp_retransmits":0,"tcp_drops":0,"wifi_drops":0,"wifi_drop_reason":0,"roams":0,"bssid":"","channel":0,"config_digest":"",
		"totals":{"reconnects":2,"opens":0,"checks_sent":0,"checks_lost":0,"tcp_sent":0,"tcp_retransmits":0,"tcp_drops":0,"wifi_drops":0,"roams":0}}`)
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("report (-want +got):\n%s", diff)
	}
}

func TestAReportWithoutTheMeshFieldsReadsAsReachingTheAccessPointItself(t *testing.T) {
	t.Parallel()
	k := keypadSite(t, testsupport.NewFactory())
	k.report(nil)

	got := []any{
		testsupport.JSONAt(send(t, k.b, "GET", "/api/v1/doors/1/telemetry?to=2026-01-06T00:00:00Z", "", 200).Body, "/samples/0"),
		k.state("/telemetry"),
		doorPage(t, k.b, "/admin/doors").Tables["Doors"][1][2],
	}

	report := testsupport.JSON(`{"at":"2026-01-05T09:00:00Z","signal":-61,"latency_ms":40,"uptime_s":3720,"free_bytes":120832,
		"firmware_version":"1.4.3","reconnects":2,"door_open":false,"opens":0,"mesh_layer":0,"mesh_parent":"","mesh_children":0,
		"checks_sent":0,"checks_lost":0,"tcp_sent":0,"tcp_retransmits":0,"tcp_drops":0,"wifi_drops":0,"wifi_drop_reason":0,"roams":0,"bssid":"","channel":0,"config_digest":"",
		"totals":{"reconnects":2,"opens":0,"checks_sent":0,"checks_lost":0,"tcp_sent":0,"tcp_retransmits":0,"tcp_drops":0,"wifi_drops":0,"roams":0}}`)
	if diff := cmp.Diff([]any{report, report, "Online"}, got); diff != "" {
		t.Errorf("report, state, status (-want +got):\n%s", diff)
	}
}

func TestADoorPageShowsWhereItsControllerSitsInTheMesh(t *testing.T) {
	t.Parallel()
	cases := map[string]struct {
		parent string
		want   []any
	}{
		"through a door's controller":      {"B2", []any{[]string{"Online", "Mesh, layer 2 via Dock"}, "2", "Dock", "1"}},
		"through a controller no door has": {"FF09", []any{[]string{"Online", "Mesh, layer 2 via FF09"}, "2", "FF09", "1"}},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			_, b := meshed(t, c.parent)

			b.Get("/")
			page := testsupport.ParsePage(b.Get("/admin/doors/1").Body)

			got := []any{page.Lists["State"], page.Details["Mesh layer"], page.Details["Mesh parent"], page.Details["Mesh children"]}
			if diff := cmp.Diff(c.want, got); diff != "" {
				t.Errorf("badges, layer, parent, children (-want +got):\n%s", diff)
			}
		})
	}
}

func TestADoorPageSaysWhenItsControllerReachesTheAccessPointItself(t *testing.T) {
	t.Parallel()
	k := keypadSite(t, testsupport.NewFactory())
	k.report(map[string]any{"mesh_children": 3})
	k.b.Get("/")

	page := testsupport.ParsePage(k.b.Get("/admin/doors/1").Body)

	got := []any{page.Lists["State"], page.Details["Mesh layer"], page.Details["Mesh parent"], page.Details["Mesh children"]}
	want := []any{[]string{"Online"}, "0, the access point itself", "None", "3"}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("badges, layer, parent, children (-want +got):\n%s", diff)
	}
}

func TestTheDoorsListShowsWhichDoorsMesh(t *testing.T) {
	t.Parallel()
	_, b := meshed(t, "B2")

	got := doorPage(t, b, "/admin/doors").Tables["Doors"]

	want := [][]string{
		{"Attic", "Online"},
		{"Dock", "Online Mesh, layer 1 via Attic"},
		{"Lobby", "Online Mesh, layer 2 via Dock"},
	}
	if diff := cmp.Diff(want, nameAndStatus(got[1:4])); diff != "" {
		t.Errorf("doors (-want +got):\n%s", diff)
	}
}

// nameAndStatus is the name and the status of each row of the doors list.
func nameAndStatus(rows [][]string) [][]string {
	out := [][]string{}
	for _, row := range rows {
		out = append(out, []string{row[1], row[2]})
	}
	return out
}

func TestLiveStreamTellsEveryPageWhenADoorsMeshChanges(t *testing.T) {
	t.Parallel()
	cases := map[string]struct {
		report map[string]any
		want   string
	}{
		"its layer":    {map[string]any{"mesh_layer": 1}, "event: doors\ndata: changed\n\n"},
		"its parent":   {map[string]any{"mesh_parent": "B2"}, "event: doors\ndata: changed\n\n"},
		"its children": {map[string]any{"mesh_children": 2}, "event: door\ndata: 1\n\n"},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			k := keypadSite(t, testsupport.NewFactory())
			k.report(nil)
			stream := k.b.Stream("/admin/live")
			defer stream.Close()
			stream.Next()

			k.report(c.report)

			if diff := cmp.Diff(c.want, stream.Next()); diff != "" {
				t.Errorf("event (-want +got):\n%s", diff)
			}
		})
	}
}

func TestEachHourKeepsTheMeshOfItsLastReport(t *testing.T) {
	t.Parallel()
	k := keypadSite(t, testsupport.NewFactory())
	for _, r := range []struct {
		at              string
		layer, children int
	}{
		{"2026-01-05T09:10:00Z", 1, 0},
		{"2026-01-05T09:40:00Z", 2, 3},
	} {
		k.at(r.at)
		k.report(map[string]any{"mesh_layer": r.layer, "mesh_parent": "B2", "mesh_children": r.children})
	}
	k.at("2026-01-05T10:00:00Z")
	run(t, k.a)

	got := testsupport.JSONAt(send(t, k.b, "GET", "/api/v1/doors/1/telemetry/hourly", "", 200).Body, "/rollups/0")

	want := testsupport.JSON(`{"hour":"2026-01-05T09:00:00Z","samples":2,"signal":{"mean":-61,"min":-61,"max":-61},
		"latency_ms":{"mean":40,"min":40,"max":40},"free_bytes":{"mean":120832,"min":120832,"max":120832},"opens":0,
		"mesh_layer":2,"mesh_children":3,
		"checks_sent":0,"checks_lost":0,"tcp_sent":0,"tcp_retransmits":0,"tcp_drops":0,"wifi_drops":0,"wifi_drop_reason":0,"roams":0,"bssid":"","channel":0}`)
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("rollup (-want +got):\n%s", diff)
	}
}

func TestADoorPageChartsItsMeshOverTheRangeChosen(t *testing.T) {
	t.Parallel()
	cases := map[string]struct {
		query string
		hours string
		want  map[string]testsupport.Chart
	}{
		"every report": {"?range=6h", "6 hours", map[string]testsupport.Chart{
			"Mesh layer over the last 6 hours": {
				Lines:  []string{"316.7,125 366.7,5"},
				Labels: []string{"2", "1", "Mon 5 Jan 06:00", "Mon 5 Jan 12:00"},
			},
			"Mesh children over the last 6 hours": {
				Lines:  []string{"316.7,125 366.7,5"},
				Labels: []string{"3", "0", "Mon 5 Jan 06:00", "Mon 5 Jan 12:00"},
			},
		}},
		"each hour": {"?range=7d", "7 days", map[string]testsupport.Chart{
			"Mesh layer over the last 7 days": {
				Lines:  []string{"589.3,65"},
				Labels: []string{"2", "2", "Mon 29 Dec 12:00", "Mon 5 Jan 12:00"},
			},
			"Mesh children over the last 7 days": {
				Lines:  []string{"589.3,65"},
				Labels: []string{"3", "3", "Mon 29 Dec 12:00", "Mon 5 Jan 12:00"},
			},
		}},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			k := keypadSite(t, testsupport.NewFactory())
			k.at("2026-01-05T09:10:00Z")
			k.report(map[string]any{"mesh_layer": 1, "mesh_parent": "B2", "mesh_children": 0})
			k.at("2026-01-05T09:40:00Z")
			k.report(map[string]any{"mesh_layer": 2, "mesh_parent": "B2", "mesh_children": 3})
			k.at("2026-01-05T12:00:00Z")
			run(t, k.a)

			charts := doorPage(t, k.b, "/admin/doors/1"+c.query).Charts

			got := map[string]testsupport.Chart{}
			for label := range c.want {
				got[label] = charts[label]
			}
			if diff := cmp.Diff(c.want, got); diff != "" {
				t.Errorf("charts (-want +got):\n%s", diff)
			}
		})
	}
}

func TestADoorPageReadsOnlyTheDoorsItNames(t *testing.T) {
	t.Parallel()
	f := testsupport.NewFactory()
	a, b := pushSite(t, f)
	f.BrokerStub.Inject("doorcode/doors/1/telemetry", reportOf(map[string]any{"mesh_layer": 2, "mesh_parent": "B2", "mesh_children": 1}))
	run(t, a)
	b.Get("/")
	before := f.StoreStub.DoorsCalls()

	page := testsupport.ParsePage(b.Get("/admin/doors/1").Body)

	got := []any{page.Details["Mesh parent"], f.StoreStub.DoorsCalls()}
	if diff := cmp.Diff([]any{"Dock", before}, got); diff != "" {
		t.Errorf("mesh parent, reads of every door (-want +got):\n%s", diff)
	}
}
