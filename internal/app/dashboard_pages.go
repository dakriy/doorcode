package app

import (
	"errors"
	"fmt"
	"math"
	"net/http"
	"strconv"
	"time"

	"github.com/dakriy/doorcode/internal/auth"
	"github.com/dakriy/doorcode/internal/check"
	"github.com/dakriy/doorcode/internal/connector"
	"github.com/dakriy/doorcode/internal/stats"
)

var dashboardTemplate = shellPage("dashboard.html")

func (a *App) dashboardPageRoutes() []route {
	return []route{pageRoute(http.MethodGet, dashboardPath, administers, a.dashboardPage)}
}

const (
	// listedDoors is how many doors each of the most and least active
	// lists holds.
	listedDoors = 5
	// maxSeries is how many doors a trend chart draws, one color each.
	maxSeries = 8
	// heatLevels is how many shades the heatmap has above zero.
	heatLevels = 5
)

// dashboardContent is the dashboard's content: the live counts, the range
// chosen and how to choose another, and each part the viewer's roles
// reach, nil when they reach none.
type dashboardContent struct {
	Counts   []countTile
	Ranges   []rangeLink
	From     string
	To       string
	Problems map[string]string
	Alert    string
	Span     string
	Zone     string
	Activity *activityView
	Health   *healthView
	Versions *versionsView
}

// Ranged reports whether the page shows a part that follows the range.
func (c dashboardContent) Ranged() bool {
	return c.Activity != nil || c.Health != nil
}

// countTile is one of the live counts: what it counts, how many, where
// they are dealt with, and whether they want attention.
type countTile struct {
	Label  string
	Value  int
	Href   string
	Action string
	Alarm  bool
}

func (a *App) dashboardPage(w http.ResponseWriter, r *http.Request, viewer auth.User) {
	zone, ok := a.siteZone(w, r)
	if !ok {
		return
	}
	now := a.clock.Now()
	q := r.URL.Query()
	content := dashboardContent{From: q.Get("from"), To: q.Get("to"), Zone: zone.String()}
	chosen, err := chooseRange(r, now, zone)
	if err != nil {
		content.Problems, content.Alert = problemsOf(err), "The range could not be read, so the dashboard shows the last 7 days."
		chosen, _ = stats.Choose(stats.Choice{}, now, zone)
	}
	content.Span = spanText(chosen)
	for _, p := range stats.Presets {
		content.Ranges = append(content.Ranges, rangeLink{Label: p.Label, Href: dashboardPath + "?range=" + p.Name, Current: p.Name == chosen.Preset})
	}
	if err := a.fillDashboard(r, viewer, &content, chosen, zone); err != nil {
		a.pageError(w, r, err)
		return
	}
	a.render(w, r, http.StatusOK, dashboardTemplate, "Dashboard", &viewer, content)
}

// problemsOf is each field's problem in err.
func problemsOf(err error) map[string]string {
	var problems check.Problems
	errors.As(err, &problems)
	return problems.ByField()
}

// spanText is the days r spans, as the dashboard writes them.
func spanText(r stats.Range) string {
	if r.Days() == 1 {
		return r.First.Format("Mon " + longDateLayout)
	}
	return r.First.Format(longDateLayout) + " – " + r.Last.Format(longDateLayout)
}

// fillDashboard fills in the parts of content viewer's roles reach over
// chosen, read in zone.
func (a *App) fillDashboard(r *http.Request, viewer auth.User, content *dashboardContent, chosen stats.Range, zone *time.Location) error {
	ctx := r.Context()
	if holds(viewer, runsFirmware) {
		versions, err := a.stats.Versions(ctx)
		if err != nil {
			return err
		}
		content.Versions = versionsViewOf(versions)
	}
	if holds(viewer, readsLog) {
		entries, err := a.stats.Entries(ctx, chosen, zone)
		if err != nil {
			return err
		}
		content.Activity = activityOf(entries, chosen)
	}
	if holds(viewer, opensDoors) {
		health, err := a.stats.Health(ctx, chosen, a.clock.Now(), zone)
		if err != nil {
			return err
		}
		content.Health = healthViewOf(health, chosen, zone)
	}
	counts, err := a.liveCounts(ctx, viewer)
	content.Counts = countTiles(counts)
	return err
}

// countTiles is each count of counts the viewer may see, as a tile.
func countTiles(counts liveCounts) []countTile {
	var tiles []countTile
	add := func(n *int, label, href, action string, alarm bool) {
		if n != nil {
			tiles = append(tiles, countTile{Label: label, Value: *n, Href: href, Action: action, Alarm: alarm && *n > 0})
		}
	}
	add(counts.OpenRequests, "Open requests", "/admin/requests", "Answer requests", false)
	add(counts.DoorsOpen, "Doors open", "/admin/doors", "See the doors", false)
	add(counts.UnresponsiveDoors, "Unresponsive doors", "/admin/doors", "See the doors", true)
	add(counts.PendingEnrollments, "Pending enrollments", "/admin/enrollments", "Approve controllers", false)
	add(counts.PendingUpdates, "Pending updates", "/admin/firmware", "See the rollout", false)
	return tiles
}

// activityView is the entries of a range, as the dashboard shows them.
type activityView struct {
	Total   int
	Allowed int
	Denied  int
	Users   int
	// Share is the share of entries allowed, empty without entries.
	Share    string
	Days     columnChart
	DayRows  []dayRow
	Heat     heatmap
	Reasons  []barRow
	Busiest  doorRanking
	Quietest doorRanking
}

// doorRanking is a list of doors ranked by their entries: its name, what
// it lists, its doors, and what it says without any.
type doorRanking struct {
	Label string
	About string
	Doors []doorBar
	Empty string
}

// barTable is a table labelled Label of rows naming Name with values in
// Unit, drawn as bars colored by Class, and what it says without rows.
type barTable struct {
	Label string
	Name  string
	Unit  string
	Class string
	Rows  []barRow
	Empty string
}

// dayRow is one day's entries, as the table under the chart lists them.
type dayRow struct {
	Day     string
	Allowed int
	Denied  int
	Total   int
}

// barRow is one row of a table that draws its value as a bar Percent of
// the widest.
type barRow struct {
	Label   string
	Value   int
	Percent int
}

// doorBar is a door's entries, drawn as a bar Percent of the busiest's.
type doorBar struct {
	ID      int64
	Name    string
	Deleted bool
	Allowed int
	Denied  int
	Total   int
	Percent int
}

func activityOf(e stats.Entries, chosen stats.Range) *activityView {
	view := &activityView{
		Total: e.Total(), Allowed: e.Allowed, Denied: e.Denied, Users: e.Users,
		Days: columnsOf(e.Days, chosen), Heat: heatmapOf(e),
	}
	if view.Total > 0 {
		view.Share = percentText(float64(e.Allowed)/float64(view.Total)) + " allowed"
	}
	for _, d := range e.Days {
		view.DayRows = append(view.DayRows, dayRow{Day: d.Date.Format(dayLayout), Allowed: d.Allowed, Denied: d.Denied, Total: d.Total()})
	}
	widest := 0
	for _, reason := range e.Reasons {
		widest = max(widest, reason.Count)
	}
	for _, reason := range e.Reasons {
		view.Reasons = append(view.Reasons, barRow{Label: sentence(reason.Reason), Value: reason.Count, Percent: share(reason.Count, widest)})
	}
	busiest, quietEmpty := 0, "No doors yet."
	if len(e.Doors) > 0 {
		busiest, quietEmpty = e.Doors[0].Total(), "Every door is among the most active."
	}
	view.Busiest = doorRanking{Label: "Most active doors", About: "The doors with the most entries.", Doors: doorBars(e.Busiest(listedDoors), busiest), Empty: "No doors yet."}
	view.Quietest = doorRanking{
		Label: "Least active doors", About: "The doors with the fewest entries, the quietest first.", Doors: doorBars(e.Quietest(listedDoors), busiest), Empty: quietEmpty,
	}
	return view
}

func doorBars(doors []stats.DoorEntries, busiest int) []doorBar {
	out := []doorBar{}
	for _, d := range doors {
		out = append(out, doorBar{ID: d.ID, Name: d.Name, Deleted: d.Deleted, Allowed: d.Allowed, Denied: d.Denied, Total: d.Total(), Percent: share(d.Total(), busiest)})
	}
	return out
}

// share is n as a whole percentage of of, zero when of is.
func share(n, of int) int {
	if of == 0 {
		return 0
	}
	return n * 100 / of
}

// percentText is a share from 0 to 1 as a percentage to a tenth.
func percentText(fraction float64) string {
	return strconv.FormatFloat(math.Round(fraction*1000)/10, 'f', -1, 64) + "%"
}

// The plot of a column chart, in the units of its view box: its width,
// its height, and how far below the top it starts.
const (
	columnsWidth  = 600
	columnsHeight = 150
	columnsTop    = 10
	// maxColumn is the widest a column is drawn, and columnGap the gap
	// between a day's allowed and denied entries.
	maxColumn = 24
	columnGap = 2
)

// columnChart is the entries of every day of a range as stacked columns:
// what it shows, each day's column, the scale's marks, and the first and
// last day.
type columnChart struct {
	Label   string
	Columns []column
	Ticks   []mark
	Start   string
	End     string
	Empty   string
}

// column is one day's column: its left edge and width, and the top and
// height of its allowed and denied parts, with a title naming them.
type column struct {
	X, W               string
	AllowedY, AllowedH string
	DeniedY, DeniedH   string
	Title              string
}

// mark is a label on a chart's scale: where it is drawn, along the
// scale, and what it says.
type mark struct {
	At   string
	Text string
}

// columnsOf is the column chart of days, the days of chosen.
func columnsOf(days []stats.Day, chosen stats.Range) columnChart {
	chart := columnChart{
		Label: "Entries per day, " + spanText(chosen), Start: days[0].Date.Format(dayLayout), End: days[len(days)-1].Date.Format(dayLayout),
		Empty: "No codes were entered at the doors in this range.",
	}
	highest := 0
	for _, d := range days {
		highest = max(highest, d.Total())
	}
	if highest == 0 {
		return chart
	}
	top := niceCeiling(highest)
	baseline := float64(columnsTop + columnsHeight)
	chart.Ticks = []mark{{At: coordinate(columnsTop), Text: strconv.Itoa(top)}, {At: coordinate(baseline), Text: "0"}}
	slot := float64(columnsWidth) / float64(len(days))
	width := math.Min(maxColumn, slot*0.6)
	for i, d := range days {
		allowed := float64(d.Allowed) / float64(top) * columnsHeight
		denied := float64(d.Denied) / float64(top) * columnsHeight
		deniedBottom := baseline - allowed
		if d.Allowed > 0 && d.Denied > 0 {
			deniedBottom -= columnGap
		}
		chart.Columns = append(chart.Columns, column{
			X: coordinate(float64(i)*slot + (slot-width)/2), W: coordinate(width),
			AllowedY: coordinate(baseline - allowed), AllowedH: coordinate(allowed),
			DeniedY: coordinate(deniedBottom - denied), DeniedH: coordinate(denied),
			Title: fmt.Sprintf("%s: %d allowed, %d denied", d.Date.Format(dayLayout), d.Allowed, d.Denied),
		})
	}
	return chart
}

// The layout of the heatmap, in the units of its view box: where the
// cells start, and each cell's size with the gap after it.
const (
	heatLeft   = 40
	heatTop    = 20
	heatWidth  = 25
	heatHeight = 24
)

// heatmap is the entries of each hour of each weekday, as shaded cells
// with each weekday's total, and the same figures as a table.
type heatmap struct {
	Rows []heatRow
	// Totals is each hour's entries over every weekday.
	Totals []int
	Total  int
}

// heatRow is one weekday's row of cells.
type heatRow struct {
	Day    string
	Y      string
	Cells  []heatCell
	Counts []int
	Total  int
}

// heatCell is one hour's cell: where it is drawn, its shade from 0 (no
// entries) to heatLevels, and a title naming it.
type heatCell struct {
	X     string
	Level int
	Title string
}

// heatmapOf is the heatmap of e's entries by weekday and hour.
func heatmapOf(e stats.Entries) heatmap {
	busiest := 0
	for _, d := range e.Weekdays {
		for _, n := range d.Hours {
			busiest = max(busiest, n)
		}
	}
	h := heatmap{Totals: e.Hours[:], Total: e.Total()}
	for i, d := range e.Weekdays {
		y := float64(heatTop + i*heatHeight)
		row := heatRow{Day: d.Day.String()[:3], Y: coordinate(y), Counts: d.Hours[:], Total: d.Total}
		for hour, n := range d.Hours {
			row.Cells = append(row.Cells, heatCell{
				X: coordinate(float64(heatLeft + hour*heatWidth)), Level: heatLevel(n, busiest),
				Title: fmt.Sprintf("%s %s–%s: %d", row.Day, hourText(hour), hourText(hour+1), n),
			})
		}
		h.Rows = append(h.Rows, row)
	}
	return h
}

// heatLevel is the shade of a cell of n entries when the busiest has
// busiest: 0 for none, else up to heatLevels for the busiest.
func heatLevel(n, busiest int) int {
	if n == 0 {
		return 0
	}
	return (n*heatLevels + busiest - 1) / busiest
}

// hourText is the start of an hour of the day, such as 09:00.
func hourText(hour int) string {
	return fmt.Sprintf("%02d:00", hour%24)
}

// healthView is the doors' uptime and telemetry over a range.
type healthView struct {
	Rows   []healthRow
	Charts []chartView
	Legend []legendEntry
	// More is how many doors the charts leave out, past maxSeries.
	More int
	Unit string
}

// healthRow is one door's uptime and telemetry, as the table lists them.
type healthRow struct {
	ID       int64
	Name     string
	Uptime   string
	Percent  int
	Reported string
	Opens    int
	Signal   string
	Latency  string
}

func healthViewOf(h stats.Health, chosen stats.Range, zone *time.Location) *healthView {
	view := &healthView{Unit: map[string]string{connector.ByHour: "hour", connector.ByDay: "day"}[h.Unit]}
	frame := chartFrame{from: chosen.From, to: chosen.To, zone: zone, over: "by door, " + spanText(chosen), during: "in this range", palette: true}
	var signal, latency [][]point
	var names []string
	for i, d := range h.Doors {
		view.Rows = append(view.Rows, healthRowOf(h, d))
		if i >= maxSeries {
			view.More++
			continue
		}
		names = append(names, d.Name)
		var s, l []point
		for _, t := range d.Trend {
			s, l = append(s, point{t.Start, t.Signal}), append(l, point{t.Start, t.Latency})
		}
		signal, latency = append(signal, s), append(latency, l)
	}
	view.Charts = []chartView{frame.chart("Signal", "dBm", signal...), frame.chart("Latency", "ms", latency...)}
	view.Legend = legend(names)
	return view
}

func healthRowOf(h stats.Health, d stats.DoorHealth) healthRow {
	row := healthRow{ID: d.ID, Name: d.Name, Uptime: "No hour has ended yet", Reported: fmt.Sprintf("%d of %d hours", d.Hours, h.Hours), Opens: d.Opens,
		Signal: notReported, Latency: notReported}
	if uptime, ok := h.Uptime(d); ok {
		row.Uptime, row.Percent = percentText(uptime), int(math.Round(uptime*100))
	}
	if d.Signal != nil {
		row.Signal, row.Latency = valueText(*d.Signal, "dBm"), valueText(*d.Latency, "ms")
	}
	return row
}

// versionsView is how many doors run each firmware version.
type versionsView struct {
	Rows []barRow
}

func versionsViewOf(versions []connector.StoredVersionCount) *versionsView {
	view := &versionsView{Rows: []barRow{}}
	for _, v := range versions {
		view.Rows = append(view.Rows, barRow{Label: firmwareText(v.Version), Value: v.Doors, Percent: share(v.Doors, versions[0].Doors)})
	}
	return view
}
