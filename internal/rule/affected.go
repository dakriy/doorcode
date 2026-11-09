package rule

import (
	"context"
	"slices"

	"github.com/dakriy/doorcode/internal/connector"
	"github.com/dakriy/doorcode/internal/timeline"
)

// The kinds of thing whose changes Affected traces to doors: besides the
// door itself, the only things its timeline depends on. Access rules and
// groups decide who may enter, never when a door is open or locked.
const (
	OfSchedule = "schedule"
	OfOverride = "override"
	OfZone     = "zone"
)

// Subject is one schedule, override, or zone: Kind is one of the kinds
// above and ID its id.
type Subject struct {
	Kind string
	ID   int64
}

// Affected returns the ids, ascending, of the doors that are not deleted
// whose timeline depends on subject as things stand: for a schedule, the
// doors in a zone in open mode during it; for an override, its door; for
// a zone, its doors. An id that names nothing affects no door.
func (s Service) Affected(ctx context.Context, subject Subject) ([]int64, error) {
	doors, err := s.Store.Doors(ctx)
	if err != nil {
		return nil, err
	}
	zones, err := s.Store.Zones(ctx)
	if err != nil {
		return nil, err
	}
	overridden, err := s.overriddenDoor(ctx, subject)
	if err != nil {
		return nil, err
	}
	ids := []int64{}
	for _, d := range doors {
		if d.DeletedAt == nil && (d.ID == overridden || dependsOn(subject, d, zones)) {
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

// dependsOn reports whether d's open mode, which the zones among zones
// that it is in decide, depends on subject: a schedule it is in open mode
// during, or a zone it is in.
func dependsOn(subject Subject, d connector.StoredDoor, zones []connector.StoredZone) bool {
	switch subject.Kind {
	case OfSchedule:
		return slices.Contains(timeline.OpenScheduleIDs(d, zones), subject.ID)
	case OfZone:
		return slices.Contains(d.ZoneIDs, subject.ID)
	}
	return false
}
