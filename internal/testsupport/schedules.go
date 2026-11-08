package testsupport

import (
	"cmp"
	"context"
	"slices"
	"strings"
	"time"

	"github.com/dakriy/doorcode/internal/connector"
)

// AlwaysSchedule is the schedule the schema migration seeds: every day, all
// day.
var AlwaysSchedule = connector.StoredSchedule{
	ID: 1, Name: "Always",
	Windows: []connector.StoredWindow{{
		Days:  []time.Weekday{time.Sunday, time.Monday, time.Tuesday, time.Wednesday, time.Thursday, time.Friday, time.Saturday},
		Start: 0, End: 0,
	}},
	Excluded: []time.Time{},
	Builtin:  true,
}

// Schedules returns every schedule in name order.
func (s *Store) Schedules(context.Context) ([]connector.StoredSchedule, error) {
	if err := s.usable(schedulesAggregate); err != nil {
		return nil, err
	}
	all := []connector.StoredSchedule{}
	for _, sch := range s.schedules {
		all = append(all, cloneSchedule(sch))
	}
	slices.SortFunc(all, func(a, b connector.StoredSchedule) int {
		return cmp.Or(strings.Compare(strings.ToLower(a.Name), strings.ToLower(b.Name)), cmp.Compare(a.ID, b.ID))
	})
	return all, nil
}

// Schedule returns the schedule with id.
func (s *Store) Schedule(_ context.Context, id int64) (connector.StoredSchedule, bool, error) {
	if err := s.usable(schedulesAggregate); err != nil {
		return connector.StoredSchedule{}, false, err
	}
	held := s.scheduleByID(id)
	if held == nil {
		return connector.StoredSchedule{}, false, nil
	}
	return cloneSchedule(*held), true, nil
}

// CreateSchedule stores sch with the next id.
func (s *Store) CreateSchedule(_ context.Context, sch connector.StoredSchedule) (int64, error) {
	if err := s.storableSchedule(sch); err != nil {
		return 0, err
	}
	s.nextSched++
	sch = cloneSchedule(sch)
	sch.ID = s.nextSched
	sch.Builtin = false
	s.schedules = append(s.schedules, sch)
	return sch.ID, nil
}

// UpdateSchedule stores sch over the schedule with sch.ID, keeping whether
// it is built in.
func (s *Store) UpdateSchedule(_ context.Context, sch connector.StoredSchedule) (bool, error) {
	if err := s.storableSchedule(sch); err != nil {
		return false, err
	}
	held := s.scheduleByID(sch.ID)
	if held == nil {
		return false, nil
	}
	sch = cloneSchedule(sch)
	sch.Builtin = held.Builtin
	*held = sch
	return true, nil
}

// DeleteSchedule removes the schedule with id, and takes it from the doors
// in open mode during it. It refuses a schedule an access rule uses.
func (s *Store) DeleteSchedule(_ context.Context, id int64) (bool, error) {
	if err := s.writable(schedulesAggregate, scheduleWrites); err != nil {
		return false, err
	}
	if s.scheduleInUse(id) {
		return false, ErrReferenced
	}
	i := slices.IndexFunc(s.schedules, func(sch connector.StoredSchedule) bool { return sch.ID == id })
	if i < 0 {
		return false, nil
	}
	s.schedules = slices.Delete(slices.Clone(s.schedules), i, i+1)
	for j := range s.doors {
		s.doors[j].OpenScheduleIDs = slices.DeleteFunc(slices.Clone(s.doors[j].OpenScheduleIDs), func(held int64) bool { return held == id })
	}
	return true, nil
}

// storableSchedule reports why sch cannot be stored, as the database's
// constraints would.
func (s *Store) storableSchedule(sch connector.StoredSchedule) error {
	if err := s.writable(schedulesAggregate, scheduleWrites); err != nil {
		return err
	}
	if sch.Excluded == nil {
		return ErrNull
	}
	for _, w := range sch.Windows {
		if w.Days == nil {
			return ErrNull
		}
	}
	for _, held := range s.schedules {
		if held.ID != sch.ID && strings.EqualFold(held.Name, sch.Name) {
			return ErrDuplicate
		}
	}
	return nil
}

func (s *Store) scheduleByID(id int64) *connector.StoredSchedule {
	for i := range s.schedules {
		if s.schedules[i].ID == id {
			return &s.schedules[i]
		}
	}
	return nil
}

func cloneSchedule(sch connector.StoredSchedule) connector.StoredSchedule {
	windows := make([]connector.StoredWindow, 0, len(sch.Windows))
	for _, w := range sch.Windows {
		w.Days = slices.Clone(w.Days)
		windows = append(windows, w)
	}
	if sch.Windows == nil {
		windows = nil
	}
	sch.Windows = windows
	sch.Excluded = slices.Clone(sch.Excluded)
	return sch
}
