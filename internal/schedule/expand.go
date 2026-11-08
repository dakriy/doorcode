package schedule

import (
	"slices"
	"time"

	"github.com/dakriy/doorcode/internal/connector"
)

// Interval is the span of time from Start up to, but not including, End.
type Interval struct {
	Start time.Time
	End   time.Time
}

// Expand returns the intervals sch covers from from up to to, in order,
// with intervals that overlap or touch merged into one and every interval
// cut to the range. Windows are read in zone: a window opens on each of its
// days at its start time on that day's local clock, and closes at its end
// time on the same day or, when the end is not after the start, the next
// one. A window does not open on an excluded date, nor on a date outside
// the effective range; one that opens inside it may close after it.
func Expand(sch connector.StoredSchedule, zone *time.Location, from, to time.Time) []Interval {
	var found []Interval
	// A window opens at most a day before it closes, so the day before the
	// range is the earliest whose windows can reach it.
	last := civil(to, zone)
	for day := civil(from, zone).AddDate(0, 0, -1); !day.After(last); day = day.AddDate(0, 0, 1) {
		if !opensOn(sch, day) {
			continue
		}
		for _, w := range sch.Windows {
			if !slices.Contains(w.Days, day.Weekday()) {
				continue
			}
			found = appendCut(found, occurrence(w, day, zone), from, to)
		}
	}
	return merged(found)
}

// civil is the date t falls on in zone, as midnight UTC on that date.
func civil(t time.Time, zone *time.Location) time.Time {
	local := t.In(zone)
	return time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, time.UTC)
}

// opensOn reports whether sch's windows may open on day.
func opensOn(sch connector.StoredSchedule, day time.Time) bool {
	if sch.StartsOn != nil && day.Before(*sch.StartsOn) {
		return false
	}
	if sch.EndsOn != nil && day.After(*sch.EndsOn) {
		return false
	}
	return !slices.ContainsFunc(sch.Excluded, day.Equal)
}

// occurrence is the interval w covers when it opens on day.
func occurrence(w connector.StoredWindow, day time.Time, zone *time.Location) Interval {
	closes := day
	if w.End <= w.Start {
		closes = day.AddDate(0, 0, 1)
	}
	return Interval{Start: at(day, w.Start, zone), End: at(closes, w.End, zone)}
}

// at is minutes after midnight on day's local clock in zone.
func at(day time.Time, minutes int, zone *time.Location) time.Time {
	return time.Date(day.Year(), day.Month(), day.Day(), minutes/60, minutes%60, 0, 0, zone)
}

// appendCut appends the part of in that lies from from up to to, if any.
func appendCut(found []Interval, in Interval, from, to time.Time) []Interval {
	if in.Start.Before(from) {
		in.Start = from
	}
	if in.End.After(to) {
		in.End = to
	}
	if !in.Start.Before(in.End) {
		return found
	}
	return append(found, in)
}

// merged is intervals in start order, with those that overlap or touch
// joined.
func merged(intervals []Interval) []Interval {
	slices.SortFunc(intervals, func(a, b Interval) int { return a.Start.Compare(b.Start) })
	out := []Interval{}
	for _, in := range intervals {
		n := len(out)
		if n > 0 && !in.Start.After(out[n-1].End) {
			if in.End.After(out[n-1].End) {
				out[n-1].End = in.End
			}
			continue
		}
		out = append(out, in)
	}
	return out
}
