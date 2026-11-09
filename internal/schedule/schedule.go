// Package schedule keeps the site's schedules: named weekly patterns of
// windows, with an optional effective date range and excluded dates, and
// turns them into the absolute intervals they cover. The seeded Always
// schedule is built in and cannot be changed or deleted.
package schedule

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/dakriy/doorcode/internal/check"
	"github.com/dakriy/doorcode/internal/connector"
	"github.com/dakriy/doorcode/internal/date"
)

var (
	// ErrNoSchedule reports an id that names no schedule.
	ErrNoSchedule = errors.New("no schedule has this id")
	// ErrBuiltin reports a change to the built-in Always schedule.
	ErrBuiltin = errors.New("the Always schedule is built in and cannot be changed or deleted")
	// ErrInUse is what every InUseError is, whatever uses the schedule.
	ErrInUse = errors.New("the schedule is in use")
)

// InUseError reports the deletion of a schedule something uses. Doors names
// the doors in open mode during it and Groups the groups whose access rules
// use it, each by name.
type InUseError struct {
	Doors  []string
	Groups []string
}

func (e InUseError) Error() string {
	var parts []string
	if e.Doors != nil {
		parts = append(parts, "doors are in open mode during this schedule: "+strings.Join(e.Doors, ", "))
	}
	if e.Groups != nil {
		parts = append(parts, "access rules of these groups use this schedule: "+strings.Join(e.Groups, ", "))
	}
	return strings.Join(parts, "; ")
}

// Is reports whether target is ErrInUse.
func (e InUseError) Is(target error) bool {
	return target == ErrInUse
}

const (
	maxNameLength = 100
	// MaxWindows is how many windows a schedule may have.
	MaxWindows = 20
	// MaxExcluded is how many excluded dates a schedule may list.
	MaxExcluded = 366
)

// clockLayout is how a time of day is written.
const clockLayout = "15:04"

// Fields is what an administrator says about a schedule, as typed. Text is
// trimmed before it is checked.
type Fields struct {
	Name string
	// StartsOn and EndsOn are dates, or empty for a range open on that
	// side.
	StartsOn string
	EndsOn   string
	Windows  []WindowFields
	// Excluded is dates on which no window opens; blank entries are
	// ignored, repeats dropped, and the rest kept in date order.
	Excluded []string
}

// WindowFields is one weekly window as typed: the days it opens on, named
// as date.DayNames names them, and its start and end times of day. An end
// at or before the start is on the next day.
type WindowFields struct {
	Days  []string
	Start string
	End   string
}

// Store is the part of the store this package uses.
type Store interface {
	connector.SchedulesStore
	connector.DoorsStore
	connector.RulesStore
	connector.GroupsStore
}

// Service reads and changes schedules.
type Service struct {
	Store Store
}

// Schedules returns every schedule in name order.
func (s Service) Schedules(ctx context.Context) ([]connector.StoredSchedule, error) {
	return s.Store.Schedules(ctx)
}

// Schedule returns the schedule whose id is id.
func (s Service) Schedule(ctx context.Context, id int64) (connector.StoredSchedule, error) {
	sch, found, err := s.Store.Schedule(ctx, id)
	if err != nil {
		return connector.StoredSchedule{}, err
	}
	if !found {
		return connector.StoredSchedule{}, ErrNoSchedule
	}
	return sch, nil
}

// Create creates a schedule from f.
func (s Service) Create(ctx context.Context, f Fields) (connector.StoredSchedule, error) {
	all, err := s.Store.Schedules(ctx)
	if err != nil {
		return connector.StoredSchedule{}, err
	}
	sch, err := checkSchedule(all, 0, f)
	if err != nil {
		return connector.StoredSchedule{}, err
	}
	id, err := s.Store.CreateSchedule(ctx, sch)
	if err != nil {
		return connector.StoredSchedule{}, err
	}
	return s.Schedule(ctx, id)
}

// Update replaces the schedule whose id is id with f.
func (s Service) Update(ctx context.Context, id int64, f Fields) (connector.StoredSchedule, error) {
	all, err := s.Store.Schedules(ctx)
	if err != nil {
		return connector.StoredSchedule{}, err
	}
	i := slices.IndexFunc(all, func(other connector.StoredSchedule) bool { return other.ID == id })
	if i < 0 {
		return connector.StoredSchedule{}, ErrNoSchedule
	}
	if all[i].Builtin {
		return connector.StoredSchedule{}, ErrBuiltin
	}
	sch, err := checkSchedule(all, id, f)
	if err != nil {
		return connector.StoredSchedule{}, err
	}
	sch.ID = id
	// The schedule was just read and schedules are removed only by Delete,
	// so it is there to update.
	if _, err := s.Store.UpdateSchedule(ctx, sch); err != nil {
		return connector.StoredSchedule{}, err
	}
	return s.Schedule(ctx, id)
}

// Delete removes the schedule whose id is id, unless a door that is not
// deleted is in open mode during it or an access rule uses it. Deleted
// doors stop being in open mode during it.
func (s Service) Delete(ctx context.Context, id int64) error {
	sch, err := s.Schedule(ctx, id)
	if err != nil {
		return err
	}
	if sch.Builtin {
		return ErrBuiltin
	}
	inUse, err := s.users(ctx, id)
	if err != nil {
		return err
	}
	if inUse.Doors != nil || inUse.Groups != nil {
		return inUse
	}
	_, err = s.Store.DeleteSchedule(ctx, id)
	return err
}

// users names what uses the schedule whose id is id: the doors that are
// not deleted in open mode during it, and the groups, in name order, whose
// access rules use it.
func (s Service) users(ctx context.Context, id int64) (InUseError, error) {
	doors, err := s.Store.Doors(ctx)
	if err != nil {
		return InUseError{}, err
	}
	rules, err := s.Store.Rules(ctx)
	if err != nil {
		return InUseError{}, err
	}
	groups, err := s.Store.Groups(ctx)
	if err != nil {
		return InUseError{}, err
	}
	var inUse InUseError
	for _, d := range doors {
		if d.DeletedAt == nil && slices.Contains(d.OpenScheduleIDs, id) {
			inUse.Doors = append(inUse.Doors, d.Name)
		}
	}
	for _, g := range groups {
		if slices.ContainsFunc(rules, func(r connector.StoredRule) bool { return r.GroupID == g.ID && r.ScheduleID == id }) {
			inUse.Groups = append(inUse.Groups, g.Name)
		}
	}
	return inUse, nil
}

// checkSchedule returns f as a schedule to store, or the problems with it.
// all is every schedule; id is the one f describes, zero for a new one,
// whose own name is no conflict.
func checkSchedule(all []connector.StoredSchedule, id int64, f Fields) (connector.StoredSchedule, error) {
	name := strings.TrimSpace(f.Name)
	var found check.List
	found.Add("name", nameProblem(all, id, name))
	startsOn, startProblem := optionalDate(f.StartsOn)
	endsOn, endProblem := optionalDate(f.EndsOn)
	found.Add("starts_on", startProblem)
	found.Add("ends_on", endProblem)
	if startsOn != nil && endsOn != nil && endsOn.Before(*startsOn) {
		found.Add("ends_on", "End the range on or after the day it starts.")
	}
	windows := checkWindows(&found, f.Windows)
	excluded := checkExcluded(&found, f.Excluded)
	sch := connector.StoredSchedule{Name: name, StartsOn: startsOn, EndsOn: endsOn, Windows: windows, Excluded: excluded}
	return sch, found.Err()
}

func nameProblem(all []connector.StoredSchedule, id int64, name string) string {
	if problem := check.Required(name, "Enter a name."); problem != "" {
		return problem
	}
	if problem := check.TooLong(name, maxNameLength, check.LongText); problem != "" {
		return problem
	}
	for _, other := range all {
		if other.ID != id && strings.EqualFold(other.Name, name) {
			return "Another schedule has this name."
		}
	}
	return ""
}

// optionalDate reads a date that may be left empty, and the problem with
// it.
func optionalDate(value string) (*time.Time, string) {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil, ""
	}
	day, err := time.Parse(date.Layout, value)
	if err != nil {
		return nil, "Enter the date as YYYY-MM-DD, or leave it empty."
	}
	return &day, ""
}

// checkWindows reads the windows, adding their problems to found.
func checkWindows(found *check.List, fields []WindowFields) []connector.StoredWindow {
	if len(fields) == 0 {
		found.Add("windows", "Add at least one window.")
	}
	if len(fields) > MaxWindows {
		found.Add("windows", fmt.Sprintf("Use at most %d windows.", MaxWindows))
	}
	windows := []connector.StoredWindow{}
	for i, f := range fields {
		windows = append(windows, checkWindow(found, i, f))
	}
	return windows
}

// checkWindow reads the window at index i, adding its problems to found
// under the field names the schedule form gives it.
func checkWindow(found *check.List, i int, f WindowFields) connector.StoredWindow {
	field := fmt.Sprintf("window-%d-", i)
	n := i + 1
	days, known := weekdays(f.Days)
	if !known {
		found.Add(field+"days", fmt.Sprintf("Choose days from %s for window %d.", strings.Join(date.DayNames, ", "), n))
	} else if len(days) == 0 {
		found.Add(field+"days", fmt.Sprintf("Choose at least one day for window %d.", n))
	}
	start, startOK := clock(f.Start)
	if !startOK {
		found.Add(field+"start", fmt.Sprintf("Enter the start of window %d as HH:MM.", n))
	}
	end, endOK := clock(f.End)
	if !endOK {
		found.Add(field+"end", fmt.Sprintf("Enter the end of window %d as HH:MM.", n))
	}
	return connector.StoredWindow{Days: days, Start: start, End: end}
}

// weekdays reads day names into weekdays from Sunday on, without repeats,
// and reports whether every name is one of date.DayNames.
func weekdays(names []string) ([]time.Weekday, bool) {
	days := []time.Weekday{}
	for _, name := range names {
		i := slices.Index(date.DayNames, strings.ToLower(strings.TrimSpace(name)))
		if i < 0 {
			return days, false
		}
		days = append(days, date.Weekday(i))
	}
	return slices.Compact(slices.Sorted(slices.Values(days))), true
}

// clock reads a time of day as minutes after midnight.
func clock(value string) (int, bool) {
	t, err := time.Parse(clockLayout, strings.TrimSpace(value))
	return t.Hour()*60 + t.Minute(), err == nil
}

// checkExcluded reads the excluded dates, adding their problems to found
// under the field names the schedule form gives them.
func checkExcluded(found *check.List, values []string) []time.Time {
	if len(values) > MaxExcluded {
		found.Add("excluded", fmt.Sprintf("Exclude at most %d dates.", MaxExcluded))
	}
	dates := []time.Time{}
	for i, value := range values {
		value = strings.TrimSpace(value)
		day, err := time.Parse(date.Layout, value)
		if value != "" && err != nil {
			found.Add(fmt.Sprintf("excluded-%d", i), fmt.Sprintf("Enter excluded dates as YYYY-MM-DD; “%s” is not one.", value))
		}
		if err == nil && !slices.ContainsFunc(dates, day.Equal) {
			dates = append(dates, day)
		}
	}
	slices.SortFunc(dates, time.Time.Compare)
	return dates
}

// FieldsOf is sch as its fields are typed, so a form or the API can show
// it.
func FieldsOf(sch connector.StoredSchedule) Fields {
	f := Fields{Name: sch.Name, StartsOn: dateText(sch.StartsOn), EndsOn: dateText(sch.EndsOn), Excluded: []string{}}
	for _, w := range sch.Windows {
		f.Windows = append(f.Windows, WindowFields{Days: DayNamesOf(w.Days), Start: ClockText(w.Start), End: ClockText(w.End)})
	}
	for _, day := range sch.Excluded {
		f.Excluded = append(f.Excluded, day.Format(date.Layout))
	}
	return f
}

// DayNamesOf names days as date.DayNames does, Monday first.
func DayNamesOf(days []time.Weekday) []string {
	names := []string{}
	for i, name := range date.DayNames {
		if slices.Contains(days, date.Weekday(i)) {
			names = append(names, name)
		}
	}
	return names
}

func dateText(day *time.Time) string {
	if day == nil {
		return ""
	}
	return day.Format(date.Layout)
}

// ClockText writes minutes after midnight as a time of day.
func ClockText(minutes int) string {
	return fmt.Sprintf("%02d:%02d", minutes/60, minutes%60)
}
