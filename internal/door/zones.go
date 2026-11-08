package door

import (
	"cmp"
	"context"
	"slices"
	"strings"

	"github.com/dakriy/doorcode/internal/check"
	"github.com/dakriy/doorcode/internal/connector"
)

// ZoneFields is what an administrator says about a zone. Text is trimmed
// before it is checked; repeated ids are dropped.
type ZoneFields struct {
	Name        string
	Description string
	DoorIDs     []int64
}

// ZoneFieldsOf is z as its fields are given.
func ZoneFieldsOf(z connector.StoredZone) ZoneFields {
	return ZoneFields{Name: z.Name, Description: z.Description, DoorIDs: z.DoorIDs}
}

// Zones returns every zone in name order.
func (s Service) Zones(ctx context.Context) ([]connector.StoredZone, error) {
	return s.Store.Zones(ctx)
}

// Zone returns the zone whose id is id.
func (s Service) Zone(ctx context.Context, id int64) (connector.StoredZone, error) {
	z, found, err := s.Store.Zone(ctx, id)
	if err != nil {
		return connector.StoredZone{}, err
	}
	if !found {
		return connector.StoredZone{}, ErrNoZone
	}
	return z, nil
}

// CreateZone creates a zone from f.
func (s Service) CreateZone(ctx context.Context, f ZoneFields) (connector.StoredZone, error) {
	z, err := s.checkZone(ctx, 0, f)
	if err != nil {
		return connector.StoredZone{}, err
	}
	id, err := s.Store.CreateZone(ctx, z)
	if err != nil {
		return connector.StoredZone{}, err
	}
	return s.Zone(ctx, id)
}

// UpdateZone replaces the zone whose id is id with f.
func (s Service) UpdateZone(ctx context.Context, id int64, f ZoneFields) (connector.StoredZone, error) {
	if _, err := s.Zone(ctx, id); err != nil {
		return connector.StoredZone{}, err
	}
	z, err := s.checkZone(ctx, id, f)
	if err != nil {
		return connector.StoredZone{}, err
	}
	z.ID = id
	// The zone was just read; a zone deleted since is reported as missing
	// when it is read back.
	if _, err := s.Store.UpdateZone(ctx, z); err != nil {
		return connector.StoredZone{}, err
	}
	return s.Zone(ctx, id)
}

// DeleteZone removes the zone whose id is id. Its doors stay.
func (s Service) DeleteZone(ctx context.Context, id int64) error {
	found, err := s.Store.DeleteZone(ctx, id)
	if err == nil && !found {
		return ErrNoZone
	}
	return err
}

// checkZone returns f as a zone to store, or the problems with it. id is
// the zone f describes, zero for a new one, whose own name is no conflict.
func (s Service) checkZone(ctx context.Context, id int64, f ZoneFields) (connector.StoredZone, error) {
	zones, zonesErr := s.Store.Zones(ctx)
	doors, doorsErr := s.Store.Doors(ctx)
	if err := cmp.Or(zonesErr, doorsErr); err != nil {
		return connector.StoredZone{}, err
	}
	z := connector.StoredZone{
		Name: strings.TrimSpace(f.Name), Description: strings.TrimSpace(f.Description), DoorIDs: distinct(f.DoorIDs),
	}
	var found check.List
	found.Add("name", zoneNameProblem(zones, id, z.Name))
	found.Add("description", check.TooLong(z.Description, maxDescriptionLength, check.LongText))
	found.Add("door_ids", unknown(z.DoorIDs, ids(doors, func(d connector.StoredDoor) int64 { return d.ID }), "No door has id %d."))
	return z, found.Err()
}

func zoneNameProblem(zones []connector.StoredZone, id int64, name string) string {
	if problem := nameProblem(name); problem != "" {
		return problem
	}
	if slices.ContainsFunc(zones, func(other connector.StoredZone) bool { return other.ID != id && strings.EqualFold(other.Name, name) }) {
		return "Another zone has this name."
	}
	return ""
}
