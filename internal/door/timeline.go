package door

import (
	"context"
	"time"

	"github.com/dakriy/doorcode/internal/connector"
	"github.com/dakriy/doorcode/internal/timeline"
)

// Timeline returns the door whose id is doorID and its timeline from from
// up to to, its schedules read in zone.
func (s Service) Timeline(ctx context.Context, doorID int64, zone *time.Location, from, to time.Time) (connector.StoredDoor, []timeline.Span, error) {
	d, err := s.Door(ctx, doorID)
	if err != nil {
		return d, nil, err
	}
	schedules, err := s.Store.Schedules(ctx)
	if err != nil {
		return d, nil, err
	}
	overrides, err := s.Store.DoorOverrides(ctx, doorID, from)
	if err != nil {
		return d, nil, err
	}
	return d, timeline.Of(d, schedules, overrides, zone, from, to), nil
}
