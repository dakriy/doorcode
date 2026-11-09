package memstore

import (
	"context"
	"slices"

	"github.com/dakriy/doorcode/internal/connector"
)

// scheduleCopy is a copy of sch.
func scheduleCopy(sch connector.StoredSchedule) connector.StoredSchedule {
	sch.StartsOn = clone(sch.StartsOn)
	sch.EndsOn = clone(sch.EndsOn)
	sch.Excluded = list(sch.Excluded)
	windows := []connector.StoredWindow{}
	for _, w := range sch.Windows {
		w.Days = list(w.Days)
		windows = append(windows, w)
	}
	sch.Windows = windows
	return sch
}

// storedSchedule is a copy of sch as the store keeps it, refusing what the
// database's columns and checks would refuse: nil weekday lists and
// exclusions that end before they start.
func storedSchedule(sch connector.StoredSchedule) (connector.StoredSchedule, error) {
	for _, r := range sch.Excluded {
		if r.Last.Before(r.First) {
			return sch, errCheck
		}
	}
	for _, w := range sch.Windows {
		if w.Days == nil {
			return sch, errNil
		}
	}
	sch.Builtin = false
	return scheduleCopy(sch), nil
}

// scheduleNameTaken reports whether a schedule other than the one whose id
// is id has name, ignoring case.
func (s *Store) scheduleNameTaken(name string, id int64) bool {
	return nameTaken(s.schedules, func(sch connector.StoredSchedule) string { return sch.Name }, name, id)
}

// Schedules returns every schedule in name order.
func (s *Store) Schedules(_ context.Context) ([]connector.StoredSchedule, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	schedules := []connector.StoredSchedule{}
	for _, sch := range s.schedules {
		schedules = append(schedules, scheduleCopy(sch))
	}
	byName(schedules, func(x connector.StoredSchedule) string { return x.Name }, func(x connector.StoredSchedule) int64 { return x.ID })
	return schedules, nil
}

// Schedule returns the schedule whose id is id.
func (s *Store) Schedule(_ context.Context, id int64) (connector.StoredSchedule, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	sch, ok := s.schedules[id]
	if !ok {
		return connector.StoredSchedule{}, false, nil
	}
	return scheduleCopy(sch), true, nil
}

// CreateSchedule stores sch and returns its id.
func (s *Store) CreateSchedule(_ context.Context, sch connector.StoredSchedule) (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	sch, err := storedSchedule(sch)
	if err != nil {
		return 0, err
	}
	if s.scheduleNameTaken(sch.Name, 0) {
		return 0, errDuplicate
	}
	sch.ID = s.next("schedules")
	s.schedules[sch.ID] = sch
	return sch.ID, nil
}

// UpdateSchedule stores sch over the schedule whose id is sch.ID.
func (s *Store) UpdateSchedule(_ context.Context, sch connector.StoredSchedule) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	old, ok := s.schedules[sch.ID]
	if !ok {
		return false, nil
	}
	sch, err := storedSchedule(sch)
	if err != nil {
		return false, err
	}
	if s.scheduleNameTaken(sch.Name, sch.ID) {
		return false, errDuplicate
	}
	sch.Builtin = old.Builtin
	s.schedules[sch.ID] = sch
	return true, nil
}

// DeleteSchedule removes the schedule, taking it off the zones in open
// mode during it; it refuses one an access rule uses.
func (s *Store) DeleteSchedule(_ context.Context, id int64) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.schedules[id]; !ok {
		return false, nil
	}
	for _, r := range s.rules {
		if r.ScheduleID == id {
			return false, errReferenced
		}
	}
	delete(s.schedules, id)
	for zoneID, z := range s.zones {
		z.OpenScheduleIDs = slices.DeleteFunc(list(z.OpenScheduleIDs), func(x int64) bool { return x == id })
		s.zones[zoneID] = z
	}
	return true, nil
}
