// Package rule keeps the site's access rules and works out what they
// grant: which rules reach a door through its zones, which doors a user's
// groups let them enter and when their code works there, and whose
// timelines a change to a schedule, an override, or a zone affects.
package rule

import (
	"cmp"
	"context"
	"errors"
	"slices"
	"time"

	"github.com/dakriy/doorcode/internal/check"
	"github.com/dakriy/doorcode/internal/connector"
	"github.com/dakriy/doorcode/internal/door"
	"github.com/dakriy/doorcode/internal/schedule"
	"github.com/dakriy/doorcode/internal/timeline"
)

// ErrNoRule reports an id that names no access rule.
var ErrNoRule = errors.New("no access rule has this id")

// Store is the part of the store this package uses.
type Store interface {
	connector.RulesStore
	connector.GroupsStore
	connector.SchedulesStore
	connector.DoorsStore
	connector.ZonesStore
	connector.OverridesStore
}

// Service reads and changes access rules and works out what they grant.
type Service struct {
	Store Store
}

// Fields is what an administrator says about an access rule. Repeated ids
// are dropped.
type Fields struct {
	GroupID    int64
	ScheduleID int64
	ZoneIDs    []int64
}

// FieldsOf is r as its fields are given.
func FieldsOf(r connector.StoredRule) Fields {
	return Fields{GroupID: r.GroupID, ScheduleID: r.ScheduleID, ZoneIDs: r.ZoneIDs}
}

// Rules returns the access rules of the group whose id is groupID, or
// every rule when groupID is zero, in id order.
func (s Service) Rules(ctx context.Context, groupID int64) ([]connector.StoredRule, error) {
	all, err := s.Store.Rules(ctx)
	if err != nil || groupID == 0 {
		return all, err
	}
	return slices.DeleteFunc(all, func(r connector.StoredRule) bool { return r.GroupID != groupID }), nil
}

// Rule returns the access rule whose id is id.
func (s Service) Rule(ctx context.Context, id int64) (connector.StoredRule, error) {
	r, found, err := s.Store.Rule(ctx, id)
	if err != nil {
		return r, err
	}
	if !found {
		return r, ErrNoRule
	}
	return r, nil
}

// Create creates an access rule from f.
func (s Service) Create(ctx context.Context, f Fields) (connector.StoredRule, error) {
	r, err := s.checkRule(ctx, f)
	if err != nil {
		return r, err
	}
	id, err := s.Store.CreateRule(ctx, r)
	if err != nil {
		return r, err
	}
	return s.Rule(ctx, id)
}

// Update replaces the access rule whose id is id with f.
func (s Service) Update(ctx context.Context, id int64, f Fields) (connector.StoredRule, error) {
	if _, err := s.Rule(ctx, id); err != nil {
		return connector.StoredRule{}, err
	}
	r, err := s.checkRule(ctx, f)
	if err != nil {
		return r, err
	}
	r.ID = id
	// The rule was just read; one deleted since is reported as missing
	// when it is read back.
	if _, err := s.Store.UpdateRule(ctx, r); err != nil {
		return r, err
	}
	return s.Rule(ctx, id)
}

// Delete removes the access rule whose id is id.
func (s Service) Delete(ctx context.Context, id int64) error {
	found, err := s.Store.DeleteRule(ctx, id)
	if err == nil && !found {
		return ErrNoRule
	}
	return err
}

// checkRule returns f as a rule to store, or the problems with it.
func (s Service) checkRule(ctx context.Context, f Fields) (connector.StoredRule, error) {
	groups, groupsErr := s.Store.Groups(ctx)
	schedules, schedulesErr := s.Store.Schedules(ctx)
	zones, zonesErr := s.Store.Zones(ctx)
	if err := cmp.Or(groupsErr, schedulesErr, zonesErr); err != nil {
		return connector.StoredRule{}, err
	}
	r := connector.StoredRule{GroupID: f.GroupID, ScheduleID: f.ScheduleID, ZoneIDs: check.Distinct(f.ZoneIDs)}
	var found check.List
	found.Add("group_id", check.Unknown([]int64{r.GroupID}, groups, func(g connector.StoredGroup) int64 { return g.ID }, "No group has id %d."))
	found.Add("schedule_id", scheduleProblem(r.ScheduleID, schedules))
	if len(r.ZoneIDs) == 0 {
		found.Add("zone_ids", "Choose at least one zone.")
	}
	found.Add("zone_ids", check.Unknown(r.ZoneIDs, zones, func(z connector.StoredZone) int64 { return z.ID }, "No zone has id %d."))
	return r, found.Err()
}

func scheduleProblem(id int64, schedules []connector.StoredSchedule) string {
	if id == 0 {
		return "Choose a schedule."
	}
	return check.Unknown([]int64{id}, schedules, func(sch connector.StoredSchedule) int64 { return sch.ID }, "No schedule has id %d.")
}

// Reach is how an access rule reaches a door: through the zones ZoneIDs
// names, the ones the rule names that the door is in.
type Reach struct {
	Rule    connector.StoredRule
	ZoneIDs []int64
}

// ReachOf is how r reaches d, and whether it does.
func ReachOf(r connector.StoredRule, d connector.StoredDoor) (Reach, bool) {
	reach := Reach{Rule: r, ZoneIDs: []int64{}}
	for _, z := range r.ZoneIDs {
		if slices.Contains(d.ZoneIDs, z) {
			reach.ZoneIDs = append(reach.ZoneIDs, z)
		}
	}
	return reach, len(reach.ZoneIDs) > 0
}

// reaches reports whether r reaches d.
func reaches(r connector.StoredRule, d connector.StoredDoor) bool {
	_, ok := ReachOf(r, d)
	return ok
}

// Entrants returns how each access rule that reaches d reaches it, in
// rule order: its members may enter while its schedule is active.
func (s Service) Entrants(ctx context.Context, d connector.StoredDoor) ([]Reach, error) {
	rules, err := s.Store.Rules(ctx)
	if err != nil {
		return nil, err
	}
	out := []Reach{}
	for _, r := range rules {
		if reach, ok := ReachOf(r, d); ok {
			out = append(out, reach)
		}
	}
	return out, nil
}

// Window is a time an access rule lets its members in.
type Window struct {
	Rule connector.StoredRule
	schedule.Interval
}

// Windows is when each of rules, whose schedules are among schedules, is
// active from from up to to with its schedule read in zone, rule by rule.
func Windows(rules []connector.StoredRule, schedules []connector.StoredSchedule, zone *time.Location, from, to time.Time) []Window {
	out := []Window{}
	for _, r := range rules {
		for _, sch := range schedules {
			if sch.ID != r.ScheduleID {
				continue
			}
			for _, in := range schedule.Expand(sch, zone, from, to) {
				out = append(out, Window{Rule: r, Interval: in})
			}
		}
	}
	return out
}

// DoorAccess is a door, the access rules of a user's groups that reach
// it, and, over a range, when their code works there and the door's
// timeline.
type DoorAccess struct {
	Door  connector.StoredDoor
	Rules []connector.StoredRule
	Works []schedule.Interval
	Spans []timeline.Span
}

// Reached reports whether an access rule of the user's groups reaches the
// door.
func (d DoorAccess) Reached() bool {
	return len(d.Rules) > 0
}

// Reachable returns each door that is not deleted which an access rule
// naming one of groups reaches, in name order, with those rules.
func (s Service) Reachable(ctx context.Context, groups []connector.UserGroup) ([]DoorAccess, error) {
	doors, rules, err := s.doorsAndRules(ctx)
	if err != nil {
		return nil, err
	}
	out := []DoorAccess{}
	for _, d := range doors {
		reaching := reachingFrom(d, groups, rules)
		if d.DeletedAt == nil && len(reaching) > 0 {
			out = append(out, DoorAccess{Door: d, Rules: reaching})
		}
	}
	return out, nil
}

// reachingFrom is the rules among rules that name one of groups and reach
// d.
func reachingFrom(d connector.StoredDoor, groups []connector.UserGroup, rules []connector.StoredRule) []connector.StoredRule {
	var out []connector.StoredRule
	for _, r := range rules {
		named := slices.ContainsFunc(groups, func(g connector.UserGroup) bool { return g.ID == r.GroupID })
		if named && reaches(r, d) {
			out = append(out, r)
		}
	}
	return out
}

// UserAccess is Reachable with when the rules let a code work at each door
// from from up to to, read in zone: while one of their schedules is active
// and the door is not locked.
func (s Service) UserAccess(ctx context.Context, groups []connector.UserGroup, zone *time.Location, from, to time.Time) ([]DoorAccess, error) {
	doors, err := s.Reachable(ctx, groups)
	if err != nil {
		return nil, err
	}
	return s.over(ctx, doors, zone, from, to)
}

// Public returns each public door that is not deleted, in name order, with
// the access rules naming one of groups that reach it, when they let a
// code work there from from up to to, read in zone, and its timeline then.
func (s Service) Public(ctx context.Context, groups []connector.UserGroup, zone *time.Location, from, to time.Time) ([]DoorAccess, error) {
	doors, rules, err := s.doorsAndRules(ctx)
	if err != nil {
		return nil, err
	}
	public := []DoorAccess{}
	for _, d := range doors {
		if door.InPortal(d) {
			public = append(public, DoorAccess{Door: d, Rules: reachingFrom(d, groups, rules)})
		}
	}
	return s.over(ctx, public, zone, from, to)
}

// over is doors with when their rules let a code work and their timelines
// from from up to to, read in zone.
func (s Service) over(ctx context.Context, doors []DoorAccess, zone *time.Location, from, to time.Time) ([]DoorAccess, error) {
	zones, err := s.Store.Zones(ctx)
	if err != nil {
		return nil, err
	}
	schedules, err := s.Store.Schedules(ctx)
	if err != nil {
		return nil, err
	}
	for i, d := range doors {
		overrides, err := s.Store.DoorOverrides(ctx, d.Door.ID, from)
		if err != nil {
			return nil, err
		}
		var granted []schedule.Interval
		for _, w := range Windows(d.Rules, schedules, zone, from, to) {
			granted = append(granted, w.Interval)
		}
		doors[i].Spans = timeline.Of(d.Door, zones, schedules, overrides, zone, from, to)
		doors[i].Works = timeline.CodeWorks(granted, doors[i].Spans, from, to)
	}
	return doors, nil
}

func (s Service) doorsAndRules(ctx context.Context) ([]connector.StoredDoor, []connector.StoredRule, error) {
	doors, err := s.Store.Doors(ctx)
	if err != nil {
		return nil, nil, err
	}
	rules, err := s.Store.Rules(ctx)
	return doors, rules, err
}
