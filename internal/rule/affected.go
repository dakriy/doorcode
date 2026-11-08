package rule

import (
	"context"
	"slices"

	"github.com/dakriy/doorcode/internal/connector"
)

// The kinds of thing whose changes Affected traces to doors: the only
// things besides a door's own open-mode schedules that its timeline
// depends on. Access rules, zones, and groups decide who may enter, never
// when a door is open or locked.
const (
	OfSchedule = "schedule"
	OfOverride = "override"
)

// Subject is one schedule or override: Kind is one of the kinds above and
// ID its id.
type Subject struct {
	Kind string
	ID   int64
}

// Affected returns the ids, ascending, of the doors that are not deleted
// whose timeline depends on subject as things stand: for a schedule, the
// doors in open mode during it; for an override, its door. An id that
// names nothing affects no door.
func (s Service) Affected(ctx context.Context, subject Subject) ([]int64, error) {
	doors, err := s.Store.Doors(ctx)
	if err != nil {
		return nil, err
	}
	overridden, err := s.overriddenDoor(ctx, subject)
	if err != nil {
		return nil, err
	}
	ids := []int64{}
	for _, d := range doors {
		if d.DeletedAt == nil && (d.ID == overridden || opensDuring(subject, d)) {
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

// opensDuring reports whether subject is a schedule d is in open mode
// during.
func opensDuring(subject Subject, d connector.StoredDoor) bool {
	return subject.Kind == OfSchedule && slices.Contains(d.OpenScheduleIDs, subject.ID)
}
