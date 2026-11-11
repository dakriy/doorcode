package app

import (
	"math"
	"slices"
	"strconv"
	"strings"
	"time"
)

// This file holds what every chart a page draws as inline SVG shares: the
// line chart of values over time, positions in a view box, a scale's
// top, and the classes that color series.

// The size of a chart's plot, in the units of its view box, and how far
// below the top of the view box it starts.
const (
	plotWidth  = 600
	plotHeight = 120
	plotTop    = 5
)

// chartView is a chart as a page draws it: what it shows, its lines and
// what each names when it has more than one, the highest and lowest
// values, when its range starts and ends, and what it says when it has
// nothing to draw.
type chartView struct {
	Label  string
	Lines  []chartLine
	Legend []legendEntry
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
		Label: name + " " + f.over, Start: shownAt(f.zone, f.from, chartLayout), End: shownAt(f.zone, f.to, chartLayout),
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
		coordinates = append(coordinates, coordinate(x)+","+coordinate(y))
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

// valueText is value in unit as a chart writes it, to a tenth; a count
// whose chart's name says what it counts has no unit.
func valueText(value float64, unit string) string {
	if unit == "" {
		return tenths(value)
	}
	return tenths(value) + " " + unit
}

// niceTops are the values the top of a column chart's scale may take: 1,
// 2, and 5 times each power of ten, and, past any count a day can reach,
// the largest int.
var niceTops = []int{
	1, 2, 5, 10, 20, 50, 100, 200, 500, 1_000, 2_000, 5_000, 10_000, 20_000, 50_000, 100_000, 200_000, 500_000,
	1_000_000, 2_000_000, 5_000_000, 10_000_000, 20_000_000, 50_000_000, 100_000_000, math.MaxInt,
}

// niceCeiling is the smallest of niceTops that is at least n.
func niceCeiling(n int) int {
	return niceTops[slices.IndexFunc(niceTops, func(top int) bool { return top >= n })]
}

// coordinate is a position in a chart's view box, to a tenth.
func coordinate(v float64) string {
	return tenths(v)
}

// tenths is v rounded to a tenth, without a trailing zero.
func tenths(v float64) string {
	return strconv.FormatFloat(math.Round(v*10)/10, 'f', -1, 64)
}

// seriesClass is the class of prefix that colors series i.
func seriesClass(prefix string, i int) string {
	return prefix + "-" + strconv.Itoa(i+1)
}
