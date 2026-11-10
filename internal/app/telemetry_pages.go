package app

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/dakriy/doorcode/internal/auth"
	"github.com/dakriy/doorcode/internal/connector"
	"github.com/dakriy/doorcode/internal/event"
	"github.com/dakriy/doorcode/internal/telemetry"
)

// notReported is what a door's page shows of what its controller has not
// reported yet.
const notReported = "Not reported yet"

// stateView is the live state of a door, as its page shows it.
type stateView struct {
	// Status is whether the controller is connected, and Badges what else
	// the door list shows of its state.
	Status     string
	Badges     []string
	Mesh       string
	Door       string
	Firmware   string
	Signal     string
	Latency    string
	Uptime     string
	Memory     string
	Reconnects string
	// MeshLayer, MeshParent, and MeshChildren are where the controller
	// sits in the mesh, as its last report said.
	MeshLayer    string
	MeshParent   string
	MeshChildren string
	LastSeen     string
	LastReport   string
	// Ranges are the telemetry ranges the charts can show, and Charts the
	// charts of the one chosen.
	Ranges []rangeLink
	Charts []chartView
	// Alarms is the door's newest alarms, nil when the viewer may not
	// read the log.
	Alarms *recentLog
}

// chartRange is a range of time a door's charts can show: the last Hours
// hours, read from the hourly rollups when Hourly says so and from every
// report otherwise.
type chartRange struct {
	Name   string
	Label  string
	Hours  int
	Hourly bool
}

// chartRanges is every range the charts can show, in the order the page
// offers them; the second is the one shown unless another is asked for.
var chartRanges = []chartRange{
	{"6h", "6 hours", 6, false},
	{"24h", "24 hours", 24, false},
	{"7d", "7 days", 168, true},
	{"30d", "30 days", 720, true},
}

// rangeLink is a link to a door's page showing a range of telemetry.
type rangeLink struct {
	Label   string
	Href    string
	Current bool
}

// rangeKey is the query parameter naming the chart range a door's page
// shows.
const rangeKey = "range"

// withRange is the address of the door page at path, which names its tab,
// showing the chart range named name.
func withRange(path, name string) string {
	return path + "&" + rangeKey + "=" + url.QueryEscape(name)
}

// chosenRange is the chart range named name, the default when none is.
func chosenRange(name string) chartRange {
	for _, c := range chartRanges {
		if c.Name == name {
			return c
		}
	}
	return chartRanges[1]
}

// stateOf is the live state of d for its page, as viewer sees it, with
// telemetry charts over the range r asks for, naming doors from c; nil
// when no controller is enrolled as d.
func (a *App) stateOf(r *http.Request, viewer auth.User, c catalog, d connector.StoredDoor, zone *time.Location) (*stateView, error) {
	if d.Controller == nil {
		return nil, nil
	}
	view := c.rowOfState(*d.Controller, zone)
	chosen := chosenRange(r.URL.Query().Get(rangeKey))
	for _, cr := range chartRanges {
		view.Ranges = append(view.Ranges, rangeLink{Label: cr.Label, Href: withRange(doorTabPath(d.ID, doorOverview), cr.Name), Current: cr == chosen})
	}
	to := a.clock.Now()
	from := to.Add(-time.Duration(chosen.Hours) * time.Hour)
	s, err := a.series(r.Context(), d.ID, chosen.Hourly, from, to)
	if err != nil {
		return nil, err
	}
	frame := chartFrame{from: from, to: to, zone: zone, over: "over the last " + chosen.Label, during: "in the last " + chosen.Label}
	view.Charts = []chartView{
		frame.chart("Signal", "dBm", s.signal), frame.chart("Latency", "ms", s.latency),
		frame.chart("Mesh layer", "", s.meshLayer), frame.chart("Mesh children", "", s.meshChildren),
	}
	view.Alarms, err = a.recent(r.Context(), viewer, connector.EventQuery{DoorID: d.ID, Type: event.Alarm}, zone)
	return &view, err
}

// rowOfState is what the controller ctrl says of itself and its door,
// as the door's page shows it, naming doors from c.
func (c catalog) rowOfState(ctrl connector.StoredController, zone *time.Location) stateView {
	view := stateView{
		Status: doorStatus(&ctrl), Badges: telemetry.Badges(ctrl), Mesh: c.meshBadge(ctrl), Door: notReported,
		Firmware: firmwareText(ctrl.FirmwareVersion), Signal: signalText(ctrl.Signal), Latency: notReported, Uptime: notReported,
		Memory: notReported, Reconnects: notReported, MeshLayer: notReported, MeshParent: notReported, MeshChildren: notReported,
		LastSeen: shownAt(zone, ctrl.LastSeenAt, secondLayout), LastReport: notReported,
	}
	switch ctrl.DoorState {
	case telemetry.Open:
		view.Door = "Open since " + shownAt(zone, *ctrl.OpenedAt, secondLayout)
	case telemetry.Closed:
		view.Door = "Closed"
	}
	if t := ctrl.Telemetry; t != nil {
		view.Latency, view.Uptime = strconv.Itoa(t.LatencyMS)+" ms", uptimeText(t.UptimeS)
		view.Memory, view.Reconnects = strconv.FormatInt(t.FreeBytes/1024, 10)+" KiB", strconv.Itoa(t.Reconnects)
		view.MeshLayer, view.MeshParent, view.MeshChildren = meshLayerText(t.MeshLayer), c.parentText(t.MeshParent), strconv.Itoa(t.MeshChildren)
		view.LastReport = shownAt(zone, t.At, secondLayout)
	}
	return view
}

// meshLayerText is a controller's mesh layer, as its door's page shows
// it.
func meshLayerText(layer int) string {
	if layer == 0 {
		return "0, the access point itself"
	}
	return strconv.Itoa(layer)
}

// parentText names the controller whose hardware id is hardwareID, which
// a controller hops through, as its door's page shows it.
func (c catalog) parentText(hardwareID string) string {
	if hardwareID == "" {
		return "None"
	}
	return c.controllerName(hardwareID)
}

// controllerName names the door whose controller has the hardware id
// hardwareID, or else gives the hardware id.
func (c catalog) controllerName(hardwareID string) string {
	for _, d := range c.doors {
		if d.Controller != nil && d.Controller.HardwareID == hardwareID {
			return d.Name
		}
	}
	return hardwareID
}

// meshBadge is what door lists show of where the controller c sits in
// the mesh: nothing while it reaches the access point itself, otherwise
// its layer and the door it hops through.
func (c catalog) meshBadge(ctrl connector.StoredController) string {
	mesh := telemetry.MeshOf(ctrl)
	if mesh.Layer == 0 {
		return ""
	}
	return "Mesh, layer " + strconv.Itoa(mesh.Layer) + " via " + c.controllerName(mesh.Parent)
}

// uptimeText is a number of seconds a controller has run, as its door's
// page shows it.
func uptimeText(seconds int64) string {
	minutes := seconds / 60
	hours, days := minutes/60, minutes/(60*24)
	if days > 0 {
		return fmt.Sprintf("%d d %d h", days, hours%24)
	}
	if hours > 0 {
		return fmt.Sprintf("%d h %d min", hours, minutes%60)
	}
	return fmt.Sprintf("%d min", minutes)
}

// point is a value a chart draws at a time.
type point struct {
	At    time.Time
	Value float64
}

// telemetrySeries is what a door's charts draw: the signal strength, the
// latency, the mesh layer, and the mesh children its controller reported.
type telemetrySeries struct {
	signal, latency, meshLayer, meshChildren []point
}

// series is what the controller of the door whose id is doorID reported
// from from up to to: every report, or each hour's means and the mesh of
// its last report when hourly says so.
func (a *App) series(ctx context.Context, doorID int64, hourly bool, from, to time.Time) (telemetrySeries, error) {
	var out telemetrySeries
	if hourly {
		rollups, err := a.telemetry.Rollups(ctx, doorID, from, to)
		for _, h := range rollups {
			out.signal = append(out.signal, point{h.Hour, h.Signal.Mean})
			out.latency = append(out.latency, point{h.Hour, h.Latency.Mean})
			out.meshLayer = append(out.meshLayer, point{h.Hour, float64(h.MeshLayer)})
			out.meshChildren = append(out.meshChildren, point{h.Hour, float64(h.MeshChildren)})
		}
		return out, err
	}
	samples, err := a.telemetry.Samples(ctx, doorID, from, to)
	for _, s := range samples {
		out.signal = append(out.signal, point{s.At, float64(s.Signal)})
		out.latency = append(out.latency, point{s.At, float64(s.LatencyMS)})
		out.meshLayer = append(out.meshLayer, point{s.At, float64(s.MeshLayer)})
		out.meshChildren = append(out.meshChildren, point{s.At, float64(s.MeshChildren)})
	}
	return out, err
}
