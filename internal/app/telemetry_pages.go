package app

import (
	"context"
	"fmt"
	"math"
	"net/http"
	"slices"
	"strconv"
	"strings"
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
	Door       string
	Firmware   string
	Signal     string
	Latency    string
	Uptime     string
	Memory     string
	Reconnects string
	LastSeen   string
	LastReport string
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
// telemetry charts over the range r asks for; nil when no controller is
// enrolled as d.
func (a *App) stateOf(r *http.Request, viewer auth.User, d connector.StoredDoor, zone *time.Location) (*stateView, error) {
	if d.Controller == nil {
		return nil, nil
	}
	view := rowOfState(*d.Controller, zone)
	chosen := chosenRange(r.URL.Query().Get("range"))
	for _, c := range chartRanges {
		view.Ranges = append(view.Ranges, rangeLink{Label: c.Label, Href: doorPath(d.ID) + "?range=" + c.Name, Current: c == chosen})
	}
	to := a.clock.Now()
	from := to.Add(-time.Duration(chosen.Hours) * time.Hour)
	signal, latency, err := a.series(r.Context(), d.ID, chosen.Hourly, from, to)
	if err != nil {
		return nil, err
	}
	frame := chartFrame{from: from, to: to, zone: zone, over: "over the last " + chosen.Label, during: "in the last " + chosen.Label}
	view.Charts = []chartView{frame.chart("Signal", "dBm", signal), frame.chart("Latency", "ms", latency)}
	view.Alarms, err = a.recent(r.Context(), viewer, connector.EventQuery{DoorID: d.ID, Type: event.Alarm}, zone)
	return &view, err
}

// rowOfState is what the controller c says of itself and its door, as
// the door's page shows it.
func rowOfState(c connector.StoredController, zone *time.Location) stateView {
	view := stateView{
		Status: doorStatus(&c), Badges: telemetry.Badges(c), Door: notReported, Firmware: firmwareText(c.FirmwareVersion),
		Signal: signalText(c.Signal), Latency: notReported, Uptime: notReported, Memory: notReported, Reconnects: notReported,
		LastSeen: c.LastSeenAt.In(zone).Format(eventLayout), LastReport: notReported,
	}
	switch c.DoorState {
	case telemetry.Open:
		view.Door = "Open since " + c.OpenedAt.In(zone).Format(eventLayout)
	case telemetry.Closed:
		view.Door = "Closed"
	}
	if t := c.Telemetry; t != nil {
		view.Latency, view.Uptime = strconv.Itoa(t.LatencyMS)+" ms", uptimeText(t.UptimeS)
		view.Memory, view.Reconnects = strconv.FormatInt(t.FreeBytes/1024, 10)+" KiB", strconv.Itoa(t.Reconnects)
		view.LastReport = t.At.In(zone).Format(eventLayout)
	}
	return view
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

// series is the signal strength and the latency the controller of the
// door whose id is doorID reported from from up to to: every report, or
// the mean of each hour when hourly says so.
func (a *App) series(ctx context.Context, doorID int64, hourly bool, from, to time.Time) ([]point, []point, error) {
	var signal, latency []point
	if hourly {
		rollups, err := a.telemetry.Rollups(ctx, doorID, from, to)
		for _, h := range rollups {
			signal = append(signal, point{h.Hour, h.Signal.Mean})
			latency = append(latency, point{h.Hour, h.Latency.Mean})
		}
		return signal, latency, err
	}
	samples, err := a.telemetry.Samples(ctx, doorID, from, to)
	for _, s := range samples {
		signal = append(signal, point{s.At, float64(s.Signal)})
		latency = append(latency, point{s.At, float64(s.LatencyMS)})
	}
	return signal, latency, err
}

// The size of a chart's plot, in the units of its view box, and how far
// below the top of the view box it starts.
const (
	plotWidth  = 600
	plotHeight = 120
	plotTop    = 5
)

// chartLayout is how a chart writes the times its range starts and ends.
const chartLayout = "Mon 2 Jan 15:04"

// chartView is a chart as a page draws it: what it shows, its lines, the
// highest and lowest values, when its range starts and ends, and what it
// says when it has nothing to draw.
type chartView struct {
	Label  string
	Lines  []chartLine
	Top    string
	Bottom string
	Start  string
	End    string
	Empty  string
}

// chartLine is one line of a chart: its points in the chart's view box,
// and the class that colors it, empty for the page's accent color.
type chartLine struct {
	Points string
	Class  string
}

// chartFrame is the range of time charts span, from from up to to,
// written in zone; over names the range after a chart's name ("over the
// last 24 hours") and during names it after "Nothing reported". A
// palette frame colors each line as its series, in order.
type chartFrame struct {
	from, to     time.Time
	zone         *time.Location
	over, during string
	palette      bool
}

// chart is the chart of series, each a line of values of what name names
// in unit, all on one scale. A series without points draws no line.
func (f chartFrame) chart(name, unit string, series ...[]point) chartView {
	view := chartView{
		Label: name + " " + f.over, Start: f.from.In(f.zone).Format(chartLayout), End: f.to.In(f.zone).Format(chartLayout),
		Empty: "Nothing reported " + f.during + ".",
	}
	all := slices.Concat(series...)
	if len(all) == 0 {
		return view
	}
	low, high := all[0].Value, all[0].Value
	for _, p := range all {
		low, high = math.Min(low, p.Value), math.Max(high, p.Value)
	}
	for i, points := range series {
		if len(points) > 0 {
			view.Lines = append(view.Lines, chartLine{Points: f.line(points, low, high), Class: f.lineClass(i)})
		}
	}
	view.Top, view.Bottom = valueText(high, unit), valueText(low, unit)
	return view
}

// line is points in a chart's view box, scaled from low to high.
func (f chartFrame) line(points []point, low, high float64) string {
	span := f.to.Sub(f.from).Seconds()
	coordinates := []string{}
	for _, p := range points {
		x := p.At.Sub(f.from).Seconds() / span * plotWidth
		y := plotTop + plotHeight/2.0
		if high > low {
			y = plotTop + (high-p.Value)/(high-low)*plotHeight
		}
		coordinates = append(coordinates, fmt.Sprintf("%.1f,%.1f", x, y))
	}
	return strings.Join(coordinates, " ")
}

// lineClass is the class of the line of series i.
func (f chartFrame) lineClass(i int) string {
	if !f.palette {
		return ""
	}
	return seriesClass("viz-line", i)
}

// valueText is value in unit as a chart writes it, to a tenth.
func valueText(value float64, unit string) string {
	return strconv.FormatFloat(math.Round(value*10)/10, 'f', -1, 64) + " " + unit
}
