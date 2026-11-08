package testsupport

import (
	"cmp"
	"context"
	"errors"
	"slices"
	"time"

	"github.com/dakriy/doorcode/internal/connector"
)

// ErrReferenced is what the stub store reports for deleting a schedule an
// access rule uses, as a foreign key would.
var ErrReferenced = errors.New("stub: still referenced")

// Rules returns every rule in id order.
func (s *Store) Rules(context.Context) ([]connector.StoredRule, error) {
	if err := s.usable(rulesAggregate); err != nil {
		return nil, err
	}
	all := []connector.StoredRule{}
	for _, r := range s.rules {
		all = append(all, cloneRule(r))
	}
	return all, nil
}

// Rule returns the rule with id.
func (s *Store) Rule(_ context.Context, id int64) (connector.StoredRule, bool, error) {
	if err := s.usable(rulesAggregate); err != nil {
		return connector.StoredRule{}, false, err
	}
	held := s.ruleByID(id)
	if held == nil {
		return connector.StoredRule{}, false, nil
	}
	return cloneRule(*held), true, nil
}

// CreateRule stores r with the next id.
func (s *Store) CreateRule(_ context.Context, r connector.StoredRule) (int64, error) {
	if err := s.storableRule(r); err != nil {
		return 0, err
	}
	s.nextRule++
	r = cloneRule(r)
	r.ID = s.nextRule
	s.rules = append(s.rules, r)
	return r.ID, nil
}

// UpdateRule stores r over the rule with r.ID.
func (s *Store) UpdateRule(_ context.Context, r connector.StoredRule) (bool, error) {
	if err := s.storableRule(r); err != nil {
		return false, err
	}
	held := s.ruleByID(r.ID)
	if held == nil {
		return false, nil
	}
	*held = cloneRule(r)
	return true, nil
}

// DeleteRule removes the rule with id.
func (s *Store) DeleteRule(_ context.Context, id int64) (bool, error) {
	if err := s.writable(rulesAggregate, ruleWrites); err != nil {
		return false, err
	}
	i := slices.IndexFunc(s.rules, func(r connector.StoredRule) bool { return r.ID == id })
	if i < 0 {
		return false, nil
	}
	s.rules = slices.Delete(slices.Clone(s.rules), i, i+1)
	return true, nil
}

// storableRule reports why r cannot be stored, as the database's
// constraints would.
func (s *Store) storableRule(r connector.StoredRule) error {
	if err := s.writable(rulesAggregate, ruleWrites); err != nil {
		return err
	}
	missing := s.groupByID(r.GroupID) == nil || s.scheduleByID(r.ScheduleID) == nil ||
		slices.ContainsFunc(r.DoorIDs, func(id int64) bool { return s.doorByID(id) == nil }) ||
		slices.ContainsFunc(r.ZoneIDs, func(id int64) bool { return s.zoneByID(id) == nil })
	if missing {
		return ErrNoSuchReference
	}
	return nil
}

func (s *Store) ruleByID(id int64) *connector.StoredRule {
	for i := range s.rules {
		if s.rules[i].ID == id {
			return &s.rules[i]
		}
	}
	return nil
}

// scheduleInUse reports whether a rule uses the schedule with id.
func (s *Store) scheduleInUse(id int64) bool {
	return slices.ContainsFunc(s.rules, func(r connector.StoredRule) bool { return r.ScheduleID == id })
}

// dropGroupRules removes the rules of the group with id.
func (s *Store) dropGroupRules(id int64) {
	s.rules = slices.DeleteFunc(slices.Clone(s.rules), func(r connector.StoredRule) bool { return r.GroupID == id })
}

// dropRuleZone takes the zone with id off every rule.
func (s *Store) dropRuleZone(id int64) {
	for i := range s.rules {
		s.rules[i].ZoneIDs = slices.DeleteFunc(slices.Clone(s.rules[i].ZoneIDs), func(z int64) bool { return z == id })
	}
}

func cloneRule(r connector.StoredRule) connector.StoredRule {
	r.DoorIDs = slices.Sorted(slices.Values(slices.Clone(r.DoorIDs)))
	r.ZoneIDs = slices.Sorted(slices.Values(slices.Clone(r.ZoneIDs)))
	if r.DoorIDs == nil {
		r.DoorIDs = []int64{}
	}
	if r.ZoneIDs == nil {
		r.ZoneIDs = []int64{}
	}
	return r
}

// DoorOverrides returns the door's overrides that end after endsAfter, by
// start and then id.
func (s *Store) DoorOverrides(_ context.Context, doorID int64, endsAfter time.Time) ([]connector.StoredOverride, error) {
	if err := s.usable(overridesAggregate); err != nil {
		return nil, err
	}
	held := []connector.StoredOverride{}
	for _, o := range s.overrides {
		if o.DoorID == doorID && o.End.After(endsAfter) {
			held = append(held, s.named(o))
		}
	}
	slices.SortFunc(held, func(a, b connector.StoredOverride) int {
		return cmp.Or(a.Start.Compare(b.Start), cmp.Compare(a.ID, b.ID))
	})
	return held, nil
}

// Override returns the override with id.
func (s *Store) Override(_ context.Context, id int64) (connector.StoredOverride, bool, error) {
	if err := s.usable(overridesAggregate); err != nil {
		return connector.StoredOverride{}, false, err
	}
	held := s.overrideByID(id)
	if held == nil {
		return connector.StoredOverride{}, false, nil
	}
	return s.named(*held), true, nil
}

// CreateOverride stores o with the next id.
func (s *Store) CreateOverride(_ context.Context, o connector.StoredOverride) (int64, error) {
	if err := s.writable(overridesAggregate, overrideWrites); err != nil {
		return 0, err
	}
	if s.doorByID(o.DoorID) == nil || s.userByID(o.CreatedBy) == nil {
		return 0, ErrNoSuchReference
	}
	s.nextOverride++
	o.ID = s.nextOverride
	o.CreatorName = ""
	s.overrides = append(s.overrides, o)
	return o.ID, nil
}

// UpdateOverride stores o's kind, start, end, and reason over the override
// with o.ID.
func (s *Store) UpdateOverride(_ context.Context, o connector.StoredOverride) (bool, error) {
	if err := s.writable(overridesAggregate, overrideWrites); err != nil {
		return false, err
	}
	held := s.overrideByID(o.ID)
	if held == nil {
		return false, nil
	}
	held.Kind, held.Start, held.End, held.Reason = o.Kind, o.Start, o.End, o.Reason
	return true, nil
}

// DeleteOverride removes the override with id.
func (s *Store) DeleteOverride(_ context.Context, id int64) (bool, error) {
	if err := s.writable(overridesAggregate, overrideWrites); err != nil {
		return false, err
	}
	i := slices.IndexFunc(s.overrides, func(o connector.StoredOverride) bool { return o.ID == id })
	if i < 0 {
		return false, nil
	}
	s.overrides = slices.Delete(slices.Clone(s.overrides), i, i+1)
	return true, nil
}

func (s *Store) overrideByID(id int64) *connector.StoredOverride {
	for i := range s.overrides {
		if s.overrides[i].ID == id {
			return &s.overrides[i]
		}
	}
	return nil
}

// named is o with its creator's display name.
func (s *Store) named(o connector.StoredOverride) connector.StoredOverride {
	o.CreatorName = s.userByID(o.CreatedBy).DisplayName
	return o
}

// FailRules makes the access rule methods report ErrUnavailable.
func (s *Store) FailRules() {
	s.failing[rulesAggregate] = true
}

// FailRuleWrites makes the methods that change access rules report
// ErrUnavailable, while the ones that read them still work.
func (s *Store) FailRuleWrites() {
	s.failing[ruleWrites] = true
}

// FailOverrides makes the override methods report ErrUnavailable.
func (s *Store) FailOverrides() {
	s.failing[overridesAggregate] = true
}

// FailOverrideWrites makes the methods that change overrides report
// ErrUnavailable, while the ones that read them still work.
func (s *Store) FailOverrideWrites() {
	s.failing[overrideWrites] = true
}
