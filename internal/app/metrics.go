package app

import (
	"io"
	"net/http"
	"strconv"

	"github.com/dakriy/doorcode/internal/auth"
	"github.com/dakriy/doorcode/internal/connector"
	"github.com/dakriy/doorcode/internal/door"
	"github.com/dakriy/doorcode/internal/exposition"
	"github.com/dakriy/doorcode/internal/firmware"
	"github.com/dakriy/doorcode/internal/telemetry"
)

// stateMetric is a gauge every door with a controller has, read from where
// the door stands.
type stateMetric struct {
	name, help string
	value      func(df firmware.DoorFirmware) float64
}

// reportMetric is a metric read from a controller's latest telemetry
// report.
type reportMetric struct {
	name, help string
	value      func(t connector.StoredTelemetry) float64
}

// stateMetrics are the gauges of every door that is not deleted and has a
// controller, online or not.
var stateMetrics = []stateMetric{
	{"doorcode_door_online", "1 while the door's controller is connected to the broker, else 0.",
		func(df firmware.DoorFirmware) float64 { return flag(df.Door.Controller.Online) }},
	{"doorcode_door_unresponsive", "1 while the door's controller is connected but has not been heard from for the unresponsive_minutes setting, else 0.",
		func(df firmware.DoorFirmware) float64 { return flag(df.Door.Controller.Unresponsive) }},
	{"doorcode_door_last_seen_timestamp_seconds", "When the server last heard from the door's controller, in Unix seconds.",
		func(df firmware.DoorFirmware) float64 {
			return float64(df.Door.Controller.LastSeenAt.UnixMilli()) / 1000
		}},
	{"doorcode_door_open", "1 while the door is open, as its controller last said, else 0.",
		func(df firmware.DoorFirmware) float64 { return flag(df.Door.Controller.DoorState == telemetry.Open) }},
	{"doorcode_door_held_open", "1 while the door has been open longer than its held_open_s while it should be locked, else 0.",
		func(df firmware.DoorFirmware) float64 { return flag(df.Door.Controller.HeldOpen) }},
	{"doorcode_door_config_mismatch", "1 while the door's controller runs a configuration other than the one it was last sent, else 0.",
		func(df firmware.DoorFirmware) float64 { return flag(telemetry.ConfigOf(df.Door).Mismatched()) }},
	{"doorcode_door_update_pending", "1 while the door takes part in the firmware rollout and does not run its target yet, else 0.",
		func(df firmware.DoorFirmware) float64 { return flag(df.Behind()) }},
}

// reportGauges are the gauges of a door whose controller is online, read
// from its latest report, so an offline door shows no stale values.
var reportGauges = []reportMetric{
	{"doorcode_door_signal_dbm", "The Wi-Fi signal strength the door's controller last reported, in dBm.",
		func(t connector.StoredTelemetry) float64 { return float64(t.Signal) }},
	{"doorcode_door_latency_seconds", "The round trip to the server the door's controller last reported, in seconds.",
		func(t connector.StoredTelemetry) float64 { return float64(t.LatencyMS) / 1000 }},
	{"doorcode_door_free_bytes", "The free memory the door's controller last reported, in bytes.",
		func(t connector.StoredTelemetry) float64 { return float64(t.FreeBytes) }},
	{"doorcode_door_uptime_seconds", "How long the door's controller had run when it last reported, in seconds.",
		func(t connector.StoredTelemetry) float64 { return float64(t.UptimeS) }},
	{"doorcode_door_mesh_layer", "The mesh layer the door's controller last reported, 0 when it reaches the access point itself.",
		func(t connector.StoredTelemetry) float64 { return float64(t.MeshLayer) }},
	{"doorcode_door_mesh_children", "How many controllers hopped through the door's controller when it last reported.",
		func(t connector.StoredTelemetry) float64 { return float64(t.MeshChildren) }},
	{"doorcode_door_wifi_channel", "The Wi-Fi channel of the access point the door's controller last reported, 0 with no link.",
		func(t connector.StoredTelemetry) float64 { return float64(t.Channel) }},
}

// reportCounters are the totals since the controller started that its
// latest report gave, kept while it is offline.
var reportCounters = []reportMetric{
	{"doorcode_door_opens_total", "How often the door was opened since its controller started.",
		func(t connector.StoredTelemetry) float64 { return float64(t.Totals.Opens) }},
	{"doorcode_door_reconnects_total", "How often the door's controller connected to the broker again since it started.",
		func(t connector.StoredTelemetry) float64 { return float64(t.Totals.Reconnects) }},
	{"doorcode_door_checks_sent_total", "The code checks the door's controller sent since it started.",
		func(t connector.StoredTelemetry) float64 { return float64(t.Totals.ChecksSent) }},
	{"doorcode_door_checks_lost_total", "The code checks whose reply never reached the door's controller since it started.",
		func(t connector.StoredTelemetry) float64 { return float64(t.Totals.ChecksLost) }},
	{"doorcode_door_tcp_segments_total", "The TCP segments the door's controller sent for the first time since it started.",
		func(t connector.StoredTelemetry) float64 { return float64(t.Totals.TCPSent) }},
	{"doorcode_door_tcp_retransmits_total", "The TCP segments the door's controller sent again since it started.",
		func(t connector.StoredTelemetry) float64 { return float64(t.Totals.TCPRetransmits) }},
	{"doorcode_door_tcp_drops_total", "The TCP segments the door's controller dropped since it started.",
		func(t connector.StoredTelemetry) float64 { return float64(t.Totals.TCPDrops) }},
	{"doorcode_door_wifi_drops_total", "How often the door's controller lost its Wi-Fi access point since it started.",
		func(t connector.StoredTelemetry) float64 { return float64(t.Totals.WifiDrops) }},
	{"doorcode_door_roams_total", "How often the door's controller moved to another access point by its own choice since it started.",
		func(t connector.StoredTelemetry) float64 { return float64(t.Totals.Roams) }},
}

// metrics answers a Prometheus scrape: every door's metrics and whether
// the broker and the store answer. A store that fails the scrape's reads
// leaves the doors out and says so in doorcode_store_up.
func (a *App) metrics(w http.ResponseWriter, r *http.Request, _ auth.User) {
	storeUp := 1.0
	plan, err := a.firmware.Plan(r.Context())
	if err != nil {
		a.log.Error("could not read the doors for metrics", connector.Field{Key: "error", Value: err.Error()})
		storeUp = 0
	}
	families := doorFamilies(plan.Doors)
	families = append(families,
		siteGauge("doorcode_broker_connected", "1 while the server is connected to the broker, else 0.", flag(a.broker.Connected())),
		siteGauge("doorcode_store_up", "1 when the store answered the scrape's reads, else 0.", storeUp))
	w.Header().Set("Content-Type", exposition.ContentType)
	_, _ = io.WriteString(w, exposition.Text(families))
}

// doorFamilies is the per-door families of the doors in doors that are
// not deleted and have a controller, in id order.
func doorFamilies(doors []firmware.DoorFirmware) []exposition.Family {
	var enrolled, reported, online []firmware.DoorFirmware
	for _, df := range doors {
		if !door.Enrolled(df.Door) {
			continue
		}
		enrolled = append(enrolled, df)
		c := df.Door.Controller
		if c.Telemetry == nil {
			continue
		}
		reported = append(reported, df)
		if c.Online {
			online = append(online, df)
		}
	}
	var out []exposition.Family
	for _, m := range stateMetrics {
		f := exposition.Family{Name: m.name, Help: m.help, Type: exposition.Gauge}
		for _, df := range enrolled {
			f.Samples = append(f.Samples, exposition.Sample{Labels: doorLabels(df.Door), Value: m.value(df)})
		}
		out = append(out, f)
	}
	out = append(out, firmwareInfo(enrolled))
	out = append(out, reportFamilies(reportGauges, exposition.Gauge, online)...)
	return append(out, reportFamilies(reportCounters, exposition.Counter, reported)...)
}

// firmwareInfo is the firmware version each door's controller runs, as
// the label of a sample of 1.
func firmwareInfo(doors []firmware.DoorFirmware) exposition.Family {
	f := exposition.Family{
		Name: "doorcode_door_firmware_info", Help: "The firmware version the door's controller runs, in its version label; always 1.", Type: exposition.Gauge,
	}
	for _, df := range doors {
		labels := append(doorLabels(df.Door), exposition.Label{Name: "version", Value: df.Door.Controller.FirmwareVersion})
		f.Samples = append(f.Samples, exposition.Sample{Labels: labels, Value: 1})
	}
	return f
}

// reportFamilies is metrics of kind, each read from the latest report of
// every door in doors.
func reportFamilies(metrics []reportMetric, kind string, doors []firmware.DoorFirmware) []exposition.Family {
	var out []exposition.Family
	for _, m := range metrics {
		f := exposition.Family{Name: m.name, Help: m.help, Type: kind}
		for _, df := range doors {
			f.Samples = append(f.Samples, exposition.Sample{Labels: doorLabels(df.Door), Value: m.value(*df.Door.Controller.Telemetry)})
		}
		out = append(out, f)
	}
	return out
}

// doorLabels names d in a sample: by name and by id.
func doorLabels(d connector.StoredDoor) []exposition.Label {
	return []exposition.Label{{Name: "door", Value: d.Name}, {Name: "door_id", Value: strconv.FormatInt(d.ID, 10)}}
}

func siteGauge(name, help string, value float64) exposition.Family {
	return exposition.Family{Name: name, Help: help, Type: exposition.Gauge, Samples: []exposition.Sample{{Value: value}}}
}

// flag is 1 for true and 0 for false, as gauges say yes or no.
func flag(b bool) float64 {
	if b {
		return 1
	}
	return 0
}
