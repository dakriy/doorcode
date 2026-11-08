// Package timeline works out a door's timeline: when, over a range of time,
// it is in open mode and when it is forced locked, from its open-mode
// schedules and its overrides. Locked overrides win over open overrides,
// which win over schedules; between the spans a timeline lists, the door is
// in its normal state, where a code opens it.
package timeline

import (
	"slices"
	"time"

	"github.com/dakriy/doorcode/internal/connector"
	"github.com/dakriy/doorcode/internal/schedule"
)

// The states a span of a timeline may be in, which are also the kinds of
// override.
const (
	// Open is open mode: any keypress unlocks the door.
	Open = "open"
	// Locked is forced locked: no code opens the door.
	Locked = "locked"
)

// Span is the time from Start up to, but not including, End during which
// a door is in State.
type Span struct {
	Start time.Time
	End   time.Time
	State string
}

// Of is the timeline of d from from up to to: Merge of the intervals its
// open-mode schedules, found among schedules, cover in zone, and of
// overrides.
func Of(d connector.StoredDoor, schedules []connector.StoredSchedule, overrides []connector.StoredOverride, zone *time.Location, from, to time.Time) []Span {
	var openMode []schedule.Interval
	for _, sch := range schedules {
		if slices.Contains(d.OpenScheduleIDs, sch.ID) {
			openMode = append(openMode, schedule.Expand(sch, zone, from, to)...)
		}
	}
	return Merge(openMode, overrides, from, to)
}

// Merge is the timeline from from up to to of a door in open mode during
// openMode with overrides: spans in order that neither overlap nor touch
// another in the same state, each cut to the range. At any moment a locked
// override covers, the door is locked; at any other moment an open
// override or openMode covers, it is open.
func Merge(openMode []schedule.Interval, overrides []connector.StoredOverride, from, to time.Time) []Span {
	bounds := []time.Time{}
	for _, in := range openMode {
		bounds = append(bounds, in.Start, in.End)
	}
	for _, o := range overrides {
		bounds = append(bounds, o.Start, o.End)
	}
	spans := []Span{}
	for _, piece := range pieces(from, to, bounds) {
		state := stateAt(piece.Start, openMode, overrides)
		if state != "" {
			spans = appendSpan(spans, Span{Start: piece.Start, End: piece.End, State: state})
		}
	}
	return spans
}

// CodeWorks is when a code that rules grant over intervals opens a door
// whose timeline is spans: during intervals, except while the door is
// locked, in order and with touching intervals joined.
func CodeWorks(intervals []schedule.Interval, spans []Span, from, to time.Time) []schedule.Interval {
	bounds := []time.Time{}
	for _, in := range intervals {
		bounds = append(bounds, in.Start, in.End)
	}
	for _, s := range spans {
		bounds = append(bounds, s.Start, s.End)
	}
	works := []schedule.Interval{}
	for _, piece := range pieces(from, to, bounds) {
		granted := slices.ContainsFunc(intervals, func(in schedule.Interval) bool { return covers(in.Start, in.End, piece.Start) })
		locked := slices.ContainsFunc(spans, func(s Span) bool { return s.State == Locked && covers(s.Start, s.End, piece.Start) })
		if granted && !locked {
			works = append(works, piece)
		}
	}
	return schedule.Merged(works)
}

// pieces cuts the range from from up to to at every one of bounds inside
// it, in order.
func pieces(from, to time.Time, bounds []time.Time) []schedule.Interval {
	cuts := []time.Time{from, to}
	for _, b := range bounds {
		if b.After(from) && b.Before(to) {
			cuts = append(cuts, b)
		}
	}
	slices.SortFunc(cuts, time.Time.Compare)
	cuts = slices.CompactFunc(cuts, time.Time.Equal)
	out := []schedule.Interval{}
	for i := 1; i < len(cuts); i++ {
		out = append(out, schedule.Interval{Start: cuts[i-1], End: cuts[i]})
	}
	return out
}

// stateAt is the state the door is in at t, empty for its normal state.
func stateAt(t time.Time, openMode []schedule.Interval, overrides []connector.StoredOverride) string {
	forced := func(kind string) bool {
		return slices.ContainsFunc(overrides, func(o connector.StoredOverride) bool { return o.Kind == kind && covers(o.Start, o.End, t) })
	}
	if forced(Locked) {
		return Locked
	}
	if forced(Open) || slices.ContainsFunc(openMode, func(in schedule.Interval) bool { return covers(in.Start, in.End, t) }) {
		return Open
	}
	return ""
}

// covers reports whether t is from start up to end.
func covers(start, end, t time.Time) bool {
	return !t.Before(start) && t.Before(end)
}

// appendSpan appends s to spans, joining it to the last one when that ends
// where s starts in the same state.
func appendSpan(spans []Span, s Span) []Span {
	n := len(spans)
	if n > 0 && spans[n-1].State == s.State && spans[n-1].End.Equal(s.Start) {
		spans[n-1].End = s.End
		return spans
	}
	return append(spans, s)
}
