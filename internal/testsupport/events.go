package testsupport

import (
	"cmp"
	"context"
	"slices"

	"github.com/dakriy/doorcode/internal/connector"
)

// AddEvent stores e with the next id.
func (s *Store) AddEvent(_ context.Context, e connector.StoredEvent) (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.writable(eventsAggregate, eventWrites); err != nil {
		return 0, err
	}
	if s.doorByID(e.DoorID) == nil || (e.UserID != nil && s.userByID(*e.UserID) == nil) {
		return 0, ErrNoSuchReference
	}
	e.ID = int64(len(s.events) + 1)
	e.DoorName, e.UserName = "", ""
	e.UserID, e.RuleID, e.OpenMS = clonedID(e.UserID), clonedID(e.RuleID), clonedID(e.OpenMS)
	s.events = append(s.events, e)
	return e.ID, nil
}

// Events returns the page of events q selects, newest first, and how many
// it selects in all.
func (s *Store) Events(_ context.Context, q connector.EventQuery) ([]connector.StoredEvent, int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.usable(eventsAggregate); err != nil {
		return nil, 0, err
	}
	var kept []connector.StoredEvent
	for _, e := range s.events {
		if selects(q, e) {
			kept = append(kept, s.namedEvent(e))
		}
	}
	slices.SortFunc(kept, func(a, b connector.StoredEvent) int {
		return cmp.Or(b.At.Compare(a.At), cmp.Compare(b.ID, a.ID))
	})
	page := []connector.StoredEvent{}
	for i, e := range kept {
		if i >= q.Offset && i < q.Offset+q.Limit {
			page = append(page, e)
		}
	}
	return page, len(kept), nil
}

// selects reports whether q keeps e.
func selects(q connector.EventQuery, e connector.StoredEvent) bool {
	user := int64(0)
	if e.UserID != nil {
		user = *e.UserID
	}
	return (q.DoorID == 0 || q.DoorID == e.DoorID) && (q.UserID == 0 || q.UserID == user) &&
		keptBy(q.Type, e.Type) && keptBy(q.Outcome, e.Outcome) &&
		keptBy(q.Reason, e.Reason) && keptBy(q.StartReason, e.StartReason) &&
		(q.From == nil || !e.At.Before(*q.From)) && (q.To == nil || e.At.Before(*q.To))
}

// keptBy reports whether a query's want, empty for any, keeps got.
func keptBy(want, got string) bool {
	return want == "" || want == got
}

// namedEvent is e with its door's name and its user's display name.
func (s *Store) namedEvent(e connector.StoredEvent) connector.StoredEvent {
	e.DoorName = s.doorByID(e.DoorID).Name
	if e.UserID != nil {
		e.UserName = s.userByID(*e.UserID).DisplayName
	}
	e.UserID, e.RuleID, e.OpenMS = clonedID(e.UserID), clonedID(e.RuleID), clonedID(e.OpenMS)
	return e
}

func clonedID(id *int64) *int64 {
	if id == nil {
		return nil
	}
	held := *id
	return &held
}

// FailEvents makes the event methods report ErrUnavailable.
func (s *Store) FailEvents() {
	s.failing[eventsAggregate] = true
}

// FailEventWrites makes AddEvent report ErrUnavailable, while Events still
// works.
func (s *Store) FailEventWrites() {
	s.failing[eventWrites] = true
}
