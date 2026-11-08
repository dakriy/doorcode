package rule

import (
	"context"
	"slices"

	"github.com/dakriy/doorcode/internal/connector"
)

// The kinds of thing whose changes Affected traces to doors.
const (
	OfSchedule = "schedule"
	OfOverride = "override"
	OfZone     = "zone"
	OfGroup    = "group"
	OfRule     = "rule"
)

// Subject is one schedule, override, zone, group, or access rule: Kind is
// one of the kinds above and ID its id.
type Subject struct {
	Kind string
	ID   int64
}

// Affected returns the ids, ascending, of the doors that are not deleted
// whose timeline or access depends on subject as things stand: for a
// schedule, the doors in open mode during it and those its rules reach; for
// an override, its door; for a zone, its doors when a rule names it; for a
// group or a rule, the doors its rules reach. A change that can move doors
// in or out of that set, such as a zone's doors or a rule's doors and zones,
// affects the doors listed before it and after it. An id that names nothing
// affects no door.
func (s Service) Affected(ctx context.Context, subject Subject) ([]int64, error) {
	doors, rules, err := s.doorsAndRules(ctx)
	if err != nil {
		return nil, err
	}
	overridden, err := s.overriddenDoor(ctx, subject)
	if err != nil {
		return nil, err
	}
	ids := []int64{}
	for _, d := range doors {
		if d.DeletedAt == nil && (d.ID == overridden || depends(subject, d, rules)) {
			ids = append(ids, d.ID)
		}
	}
	slices.Sort(ids)
	return ids, nil
}

// overriddenDoor is the id of the door subject overrides, zero when it is
// no override or names none.
func (s Service) overriddenDoor(ctx context.Context, subject Subject) (int64, error) {
	if subject.Kind != OfOverride {
		return 0, nil
	}
	o, _, err := s.Store.Override(ctx, subject.ID)
	return o.DoorID, err
}

// depends reports whether d's timeline or access depends on subject,
// which is no override, given every rule.
func depends(subject Subject, d connector.StoredDoor, rules []connector.StoredRule) bool {
	id := subject.ID
	if subject.Kind == OfSchedule && slices.Contains(d.OpenScheduleIDs, id) {
		return true
	}
	if subject.Kind == OfZone {
		return slices.Contains(d.ZoneIDs, id) && slices.ContainsFunc(rules, func(r connector.StoredRule) bool { return slices.Contains(r.ZoneIDs, id) })
	}
	return slices.ContainsFunc(rules, func(r connector.StoredRule) bool { return names(subject, r) && reaches(r, d) })
}

// names reports whether r is subject, belongs to it, or uses it.
func names(subject Subject, r connector.StoredRule) bool {
	switch subject.Kind {
	case OfSchedule:
		return r.ScheduleID == subject.ID
	case OfGroup:
		return r.GroupID == subject.ID
	case OfRule:
		return r.ID == subject.ID
	}
	return false
}
