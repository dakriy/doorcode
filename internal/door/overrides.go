package door

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/dakriy/doorcode/internal/check"
	"github.com/dakriy/doorcode/internal/connector"
	"github.com/dakriy/doorcode/internal/timeline"
)

var (
	// ErrNoOverride reports an id that names no override of the door.
	ErrNoOverride = errors.New("the door has no override with this id")
	// ErrNotActive reports cancelling an override that has not started or
	// has ended.
	ErrNotActive = errors.New("only an override that has started and not ended can be cancelled; delete one that has not started")
)

const maxReasonLength = 500

// OverrideFields is what an administrator says about an override. Kind is
// timeline.Open or timeline.Locked; a zero Start or End is one not given.
// The reason is trimmed before it is checked.
type OverrideFields struct {
	Kind   string
	Start  time.Time
	End    time.Time
	Reason string
}

// Overrides returns the door whose id is doorID and its overrides that
// have not ended, the active ones and those to come, by start.
func (s Service) Overrides(ctx context.Context, doorID int64) (connector.StoredDoor, []connector.StoredOverride, error) {
	d, err := s.Door(ctx, doorID)
	if err != nil {
		return d, nil, err
	}
	overrides, err := s.Store.DoorOverrides(ctx, doorID, s.Clock.Now())
	return d, overrides, err
}

// Override returns the override whose id is id, of the door whose id is
// doorID.
func (s Service) Override(ctx context.Context, doorID, id int64) (connector.StoredOverride, error) {
	o, found, err := s.Store.Override(ctx, id)
	if err != nil {
		return o, err
	}
	if !found || o.DoorID != doorID {
		return connector.StoredOverride{}, ErrNoOverride
	}
	return o, nil
}

// CreateOverride creates an override of the door whose id is doorID from
// f, as created by the user whose id is creator.
func (s Service) CreateOverride(ctx context.Context, doorID, creator int64, f OverrideFields) (connector.StoredOverride, error) {
	if _, err := s.Door(ctx, doorID); err != nil {
		return connector.StoredOverride{}, err
	}
	now := s.Clock.Now()
	o, err := checkOverride(f, now)
	if err != nil {
		return connector.StoredOverride{}, err
	}
	o.DoorID, o.CreatedBy, o.CreatedAt = doorID, creator, now
	id, err := s.Store.CreateOverride(ctx, o)
	if err != nil {
		return connector.StoredOverride{}, err
	}
	return s.Override(ctx, doorID, id)
}

// UpdateOverride replaces the override whose id is id, of the door whose
// id is doorID, with f.
func (s Service) UpdateOverride(ctx context.Context, doorID, id int64, f OverrideFields) (connector.StoredOverride, error) {
	if _, err := s.Override(ctx, doorID, id); err != nil {
		return connector.StoredOverride{}, err
	}
	o, err := checkOverride(f, s.Clock.Now())
	if err != nil {
		return connector.StoredOverride{}, err
	}
	return s.storeOverride(ctx, doorID, id, o)
}

// CancelOverride ends the active override whose id is id, of the door
// whose id is doorID, now.
func (s Service) CancelOverride(ctx context.Context, doorID, id int64) (connector.StoredOverride, error) {
	o, err := s.Override(ctx, doorID, id)
	if err != nil {
		return o, err
	}
	now := s.Clock.Now()
	if !Active(o, now) {
		return o, ErrNotActive
	}
	o.End = now
	return s.storeOverride(ctx, doorID, id, o)
}

// Active reports whether o is in force at now: it has started and not
// ended.
func Active(o connector.StoredOverride, now time.Time) bool {
	return !now.Before(o.Start) && now.Before(o.End)
}

// storeOverride stores o over the override whose id is id, which was just
// read, and returns it as it is now.
func (s Service) storeOverride(ctx context.Context, doorID, id int64, o connector.StoredOverride) (connector.StoredOverride, error) {
	o.ID = id
	// Overrides are removed only by DeleteOverride; one deleted since it
	// was read is reported as missing when it is read back.
	if _, err := s.Store.UpdateOverride(ctx, o); err != nil {
		return connector.StoredOverride{}, err
	}
	return s.Override(ctx, doorID, id)
}

// DeleteOverride removes the override whose id is id, of the door whose id
// is doorID.
func (s Service) DeleteOverride(ctx context.Context, doorID, id int64) error {
	if _, err := s.Override(ctx, doorID, id); err != nil {
		return err
	}
	_, err := s.Store.DeleteOverride(ctx, id)
	return err
}

// checkOverride returns f as an override to store, or the problems with
// it, when it is now.
func checkOverride(f OverrideFields, now time.Time) (connector.StoredOverride, error) {
	o := connector.StoredOverride{Kind: f.Kind, Start: f.Start, End: f.End, Reason: strings.TrimSpace(f.Reason)}
	var found check.List
	if f.Kind != timeline.Open && f.Kind != timeline.Locked {
		found.Add("kind", "Choose open or locked.")
	}
	if f.Start.IsZero() {
		found.Add("start", "Enter when the override starts.")
	}
	found.Add("end", endProblem(f, now))
	found.Add("reason", check.TooLong(o.Reason, maxReasonLength, check.LongText))
	return o, found.Err()
}

func endProblem(f OverrideFields, now time.Time) string {
	switch {
	case f.End.IsZero():
		return "Enter when the override ends."
	case !f.End.After(f.Start):
		return "End the override after it starts."
	case !f.End.After(now):
		return "End the override in the future."
	}
	return ""
}

// Timeline returns the door whose id is doorID and its timeline from from
// up to to, its schedules read in zone.
func (s Service) Timeline(ctx context.Context, doorID int64, zone *time.Location, from, to time.Time) (connector.StoredDoor, []timeline.Span, error) {
	d, err := s.Door(ctx, doorID)
	if err != nil {
		return d, nil, err
	}
	zones, err := s.Store.Zones(ctx)
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
	return d, timeline.Of(d, zones, schedules, overrides, zone, from, to), nil
}
