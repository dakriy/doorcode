// Package override keeps the doors' overrides: an administrator's or a
// keypad commander's open or locked state for a door over a while, which
// wins over open mode. Overrides are removed outright.
package override

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/dakriy/doorcode/internal/check"
	"github.com/dakriy/doorcode/internal/connector"
	"github.com/dakriy/doorcode/internal/door"
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

// Service reads and changes the doors' overrides.
type Service struct {
	Store connector.OverridesStore
	Doors door.Service
	Clock connector.Clock
}

// Fields is what an administrator says about an override. Kind is
// timeline.Open or timeline.Locked; a zero Start or End is one not given.
// The reason is trimmed before it is checked.
type Fields struct {
	Kind   string
	Start  time.Time
	End    time.Time
	Reason string
}

// Of returns the door whose id is doorID and its overrides that have
// not ended, the active ones and those to come, by start.
func (s Service) Of(ctx context.Context, doorID int64) (connector.StoredDoor, []connector.StoredOverride, error) {
	d, err := s.Doors.Door(ctx, doorID)
	if err != nil {
		return d, nil, err
	}
	overrides, err := s.Store.DoorOverrides(ctx, doorID, s.Clock.Now())
	return d, overrides, err
}

// Get returns the override whose id is id, of the door whose id is
// doorID.
func (s Service) Get(ctx context.Context, doorID, id int64) (connector.StoredOverride, error) {
	o, err := check.Lookup(ctx, id, s.Store.Override, ErrNoOverride)
	if err == nil && o.DoorID != doorID {
		return connector.StoredOverride{}, ErrNoOverride
	}
	return o, err
}

// Create creates an override of the door whose id is doorID from f,
// as created by the user whose id is creator.
func (s Service) Create(ctx context.Context, doorID, creator int64, f Fields) (connector.StoredOverride, error) {
	if _, err := s.Doors.Door(ctx, doorID); err != nil {
		return connector.StoredOverride{}, err
	}
	now := s.Clock.Now()
	o, err := checkFields(f, now)
	if err != nil {
		return connector.StoredOverride{}, err
	}
	o.DoorID, o.CreatedBy, o.CreatedAt = doorID, creator, now
	id, err := s.Store.CreateOverride(ctx, o)
	if err != nil {
		return connector.StoredOverride{}, err
	}
	return s.Get(ctx, doorID, id)
}

// Update replaces the override whose id is id, of the door whose
// id is doorID, with f.
func (s Service) Update(ctx context.Context, doorID, id int64, f Fields) (connector.StoredOverride, error) {
	if _, err := s.Get(ctx, doorID, id); err != nil {
		return connector.StoredOverride{}, err
	}
	o, err := checkFields(f, s.Clock.Now())
	if err != nil {
		return connector.StoredOverride{}, err
	}
	return s.store(ctx, doorID, id, o)
}

// Cancel ends the active override whose id is id, of the door
// whose id is doorID, now.
func (s Service) Cancel(ctx context.Context, doorID, id int64) (connector.StoredOverride, error) {
	o, err := s.Get(ctx, doorID, id)
	if err != nil {
		return o, err
	}
	now := s.Clock.Now()
	if !Active(o, now) {
		return o, ErrNotActive
	}
	o.End = now
	return s.store(ctx, doorID, id, o)
}

// Active reports whether o is in force at now: it has started and not
// ended.
func Active(o connector.StoredOverride, now time.Time) bool {
	return !now.Before(o.Start) && now.Before(o.End)
}

// store stores o over the override whose id is id, which was just
// read, and returns it as it is now.
func (s Service) store(ctx context.Context, doorID, id int64, o connector.StoredOverride) (connector.StoredOverride, error) {
	o.ID = id
	// Overrides are removed only by Delete; one deleted since it
	// was read is reported as missing when it is read back.
	if _, err := s.Store.UpdateOverride(ctx, o); err != nil {
		return connector.StoredOverride{}, err
	}
	return s.Get(ctx, doorID, id)
}

// Delete removes the override whose id is id, of the door whose id
// is doorID.
func (s Service) Delete(ctx context.Context, doorID, id int64) error {
	if _, err := s.Get(ctx, doorID, id); err != nil {
		return err
	}
	_, err := s.Store.DeleteOverride(ctx, id)
	return err
}

// checkFields returns f as an override to store, or the problems with
// it, when it is now.
func checkFields(f Fields, now time.Time) (connector.StoredOverride, error) {
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

func endProblem(f Fields, now time.Time) string {
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
