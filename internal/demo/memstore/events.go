package memstore

import (
	"cmp"
	"context"
	"slices"
	"strings"
	"time"

	"github.com/dakriy/doorcode/internal/connector"
)

// AddEvent stores e and returns its id.
func (s *Store) AddEvent(_ context.Context, e connector.StoredEvent) (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.doors[e.DoorID]; !ok {
		return 0, errNoReference
	}
	if e.UserID != nil {
		if _, ok := s.users[*e.UserID]; !ok {
			return 0, errNoReference
		}
	}
	e.ID = s.next("events")
	e.DoorName = ""
	e.UserName = ""
	e.UserID = cloneID(e.UserID)
	e.RuleID = cloneID(e.RuleID)
	s.events = append(s.events, e)
	return e.ID, nil
}

// keeps reports whether q keeps e.
func keeps(q connector.EventQuery, e connector.StoredEvent) bool {
	user := e.UserID != nil && *e.UserID == q.UserID
	switch {
	case q.DoorID != 0 && e.DoorID != q.DoorID:
		return false
	case q.UserID != 0 && !user:
		return false
	case q.Type != "" && e.Type != q.Type:
		return false
	case q.Outcome != "" && e.Outcome != q.Outcome:
		return false
	case q.Reason != "" && e.Reason != q.Reason:
		return false
	case q.From != nil && e.At.Before(*q.From):
		return false
	}
	return q.To == nil || e.At.Before(*q.To)
}

// eventOut is a copy of e with its door's name and its user's display
// name.
func (s *Store) eventOut(e connector.StoredEvent) connector.StoredEvent {
	e.DoorName = s.doors[e.DoorID].Name
	if e.UserID != nil {
		e.UserName = s.users[*e.UserID].DisplayName
	}
	e.UserID = cloneID(e.UserID)
	e.RuleID = cloneID(e.RuleID)
	return e
}

// Events returns the page of events q selects, newest first, and how many
// it selects in all.
func (s *Store) Events(_ context.Context, q connector.EventQuery) ([]connector.StoredEvent, int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var kept []connector.StoredEvent
	for _, e := range s.events {
		if keeps(q, e) {
			kept = append(kept, e)
		}
	}
	slices.SortFunc(kept, func(x, y connector.StoredEvent) int {
		if c := y.At.Compare(x.At); c != 0 {
			return c
		}
		return cmp.Compare(y.ID, x.ID)
	})
	events := []connector.StoredEvent{}
	for _, e := range page(kept, q.Offset, q.Limit) {
		events = append(events, s.eventOut(e))
	}
	return events, len(kept), nil
}

// The keys EventStats counts events under.
type (
	dayKey struct {
		day     time.Time
		outcome string
	}
	hourKey struct {
		weekday time.Weekday
		hour    int
	}
	reasonKey struct {
		outcome string
		reason  string
	}
	doorKey struct {
		doorID  int64
		outcome string
	}
)

// eventCounts is what EventStats counts as it reads the events.
type eventCounts struct {
	days    map[dayKey]int
	hours   map[hourKey]int
	reasons map[reasonKey]int
	doors   map[doorKey]int
	users   map[int64]bool
}

// add counts e, whose time read in the range's zone is local.
func (c eventCounts) add(e connector.StoredEvent, local time.Time) {
	day := time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, time.UTC)
	c.days[dayKey{day, e.Outcome}]++
	c.hours[hourKey{local.Weekday(), local.Hour()}]++
	c.reasons[reasonKey{e.Outcome, e.Reason}]++
	c.doors[doorKey{e.DoorID, e.Outcome}]++
	if e.UserID != nil {
		c.users[*e.UserID] = true
	}
}

// EventStats sums up the events q selects.
func (s *Store) EventStats(_ context.Context, q connector.EventRange) (connector.StoredEventStats, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	counts := eventCounts{
		days: map[dayKey]int{}, hours: map[hourKey]int{}, reasons: map[reasonKey]int{},
		doors: map[doorKey]int{}, users: map[int64]bool{},
	}
	for _, e := range s.events {
		if e.Type == q.Type && !e.At.Before(q.From) && e.At.Before(q.To) {
			counts.add(e, e.At.In(q.Zone))
		}
	}
	return connector.StoredEventStats{
		Days: dayCounts(counts.days), Hours: hourCounts(counts.hours), Reasons: reasonCounts(counts.reasons),
		Doors: doorCounts(counts.doors), Users: len(counts.users),
	}, nil
}

// dayCounts lists counts by day and then outcome.
func dayCounts(counts map[dayKey]int) []connector.StoredDayCount {
	out := []connector.StoredDayCount{}
	for k, n := range counts {
		out = append(out, connector.StoredDayCount{Day: k.day, Outcome: k.outcome, Count: n})
	}
	slices.SortFunc(out, func(x, y connector.StoredDayCount) int {
		if c := x.Day.Compare(y.Day); c != 0 {
			return c
		}
		return strings.Compare(x.Outcome, y.Outcome)
	})
	return out
}

// hourCounts lists counts by weekday, Sunday first, and then hour.
func hourCounts(counts map[hourKey]int) []connector.StoredHourCount {
	out := []connector.StoredHourCount{}
	for k, n := range counts {
		out = append(out, connector.StoredHourCount{Weekday: k.weekday, Hour: k.hour, Count: n})
	}
	slices.SortFunc(out, func(x, y connector.StoredHourCount) int {
		if c := cmp.Compare(x.Weekday, y.Weekday); c != 0 {
			return c
		}
		return cmp.Compare(x.Hour, y.Hour)
	})
	return out
}

// reasonCounts lists counts by outcome and then reason.
func reasonCounts(counts map[reasonKey]int) []connector.StoredReasonCount {
	out := []connector.StoredReasonCount{}
	for k, n := range counts {
		out = append(out, connector.StoredReasonCount{Outcome: k.outcome, Reason: k.reason, Count: n})
	}
	slices.SortFunc(out, func(x, y connector.StoredReasonCount) int {
		if c := strings.Compare(x.Outcome, y.Outcome); c != 0 {
			return c
		}
		return strings.Compare(x.Reason, y.Reason)
	})
	return out
}

// doorCounts lists counts by door id and then outcome.
func doorCounts(counts map[doorKey]int) []connector.StoredDoorCount {
	out := []connector.StoredDoorCount{}
	for k, n := range counts {
		out = append(out, connector.StoredDoorCount{DoorID: k.doorID, Outcome: k.outcome, Count: n})
	}
	slices.SortFunc(out, func(x, y connector.StoredDoorCount) int {
		if c := cmp.Compare(x.DoorID, y.DoorID); c != 0 {
			return c
		}
		return strings.Compare(x.Outcome, y.Outcome)
	})
	return out
}
