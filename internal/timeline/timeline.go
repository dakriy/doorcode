// Package timeline works out a door's timeline: when, over a range of time,
// it is in open mode and when it is forced locked, from the open-mode
// schedules of its zones and its overrides. Locked overrides win over open
// overrides, which win over schedules; between the spans a timeline lists,
// the door is in its normal state, where a code opens it.
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

// OpenScheduleIDs are the ids, ascending and each once, of the schedules
// during which d is in open mode: those of every zone among zones that d
// is in.
func OpenScheduleIDs(d connector.StoredDoor, zones []connector.StoredZone) []int64 {
	ids := []int64{}
	for _, z := range zones {
		if slices.Contains(d.ZoneIDs, z.ID) {
			ids = append(ids, z.OpenScheduleIDs...)
		}
	}
	slices.Sort(ids)
	return slices.Compact(ids)
}

// Of is the timeline of d from from up to to: Merge of the intervals the
// open-mode schedules of its zones, found among zones and schedules,
// cover in loc, and of overrides.
func Of(d connector.StoredDoor, zones []connector.StoredZone, schedules []connector.StoredSchedule, overrides []connector.StoredOverride, loc *time.Location, from, to time.Time) []Span {
	openIDs := OpenScheduleIDs(d, zones)
	var openMode []schedule.Interval
	for _, sch := range schedules {
		if slices.Contains(openIDs, sch.ID) {
			openMode = append(openMode, schedule.Expand(sch, loc, from, to)...)
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
	for _, piece := range Pieces(from, to, bounds) {
		state := stateAt(piece.Start, openMode, overrides)
		if state != "" {
			spans = appendSpan(spans, Span{Start: piece.Start, End: piece.End, State: state})
		}
	}
	return spans
}

// Pieces cuts the range from from up to to at every one of bounds inside
// it, in order.
func Pieces(from, to time.Time, bounds []time.Time) []schedule.Interval {
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
