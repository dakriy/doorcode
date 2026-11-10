// Package door keeps the site's doors, the zones that group them, and
// their pictures, and works out their timelines. A door exists once an
// administrator creates it; enrollment attaches the hardware later. Doors
// are soft-deleted; zones and pictures removed.
package door

import (
	"context"
	"errors"
	"slices"
	"strings"

	"github.com/dakriy/doorcode/internal/check"
	"github.com/dakriy/doorcode/internal/connector"
)

var (
	// ErrNoDoor reports an id that names no door.
	ErrNoDoor = errors.New("no door has this id")
	// ErrNoZone reports an id that names no zone.
	ErrNoZone = errors.New("no zone has this id")
	// ErrNoPicture reports an id that names no picture of the door.
	ErrNoPicture = errors.New("the door has no picture with this id")
)

const (
	maxNameLength        = 100
	maxLocationLength    = 200
	maxNotesLength       = 2000
	maxDescriptionLength = 500
)

// Store is the part of the store this package uses.
type Store interface {
	connector.DoorsStore
	connector.PicturesStore
	connector.ZonesStore
	connector.SchedulesStore
	connector.OverridesStore
}

// InPortal reports whether the user portal shows d, to everyone signed
// in: it is public and not deleted.
func InPortal(d connector.StoredDoor) bool {
	return d.Public && d.DeletedAt == nil
}

// Enrolled reports whether a controller is enrolled as d and d is not
// deleted: the doors that are sent pushes and updates, watched, and
// counted in the dashboard's door health.
func Enrolled(d connector.StoredDoor) bool {
	return d.DeletedAt == nil && d.Controller != nil
}

// Service reads and changes doors, zones, and pictures.
type Service struct {
	Store Store
	Clock connector.Clock
}

// Fields is what an administrator says about a door. Text is trimmed
// before it is checked; repeated ids are dropped.
type Fields struct {
	Name     string
	Location string
	Notes    string
	// Public doors are shown in the user portal.
	Public bool
	// ZoneIDs are the zones the door is in, which decide when it is in
	// open mode and who may enter it.
	ZoneIDs []int64
}

// FieldsOf is d as its fields are given, so one part can be changed and
// the rest kept.
func FieldsOf(d connector.StoredDoor) Fields {
	return Fields{
		Name: d.Name, Location: d.Location, Notes: d.Notes, Public: d.Public, ZoneIDs: d.ZoneIDs,
	}
}

// Query selects a page of doors; Page counts from 1. Search keeps the doors
// whose name or location contains it, ignoring case. Deleted selects the
// deleted doors instead of the others. A ZoneID other than zero keeps the
// doors in that zone.
type Query struct {
	Search  string
	Deleted bool
	ZoneID  int64
	Page    int
	PerPage int
}

// Page is one page of doors and how many doors the query selects.
type Page struct {
	Doors []connector.StoredDoor
	Total int
}

// Doors returns every door, deleted ones included, in name order.
func (s Service) Doors(ctx context.Context) ([]connector.StoredDoor, error) {
	return s.Store.Doors(ctx)
}

// CurrentDoors returns the doors that are not deleted, in name order.
func (s Service) CurrentDoors(ctx context.Context) ([]connector.StoredDoor, error) {
	return s.Store.CurrentDoors(ctx)
}

// Find returns the page of doors q selects.
func (s Service) Find(ctx context.Context, q Query) (Page, error) {
	all, err := s.Store.Doors(ctx)
	if err != nil {
		return Page{}, err
	}
	kept := []connector.StoredDoor{}
	for _, d := range all {
		if (d.DeletedAt != nil) == q.Deleted && Matches(d, q.Search) && inZone(d, q.ZoneID) {
			kept = append(kept, d)
		}
	}
	from := min((q.Page-1)*q.PerPage, len(kept))
	to := min(from+q.PerPage, len(kept))
	return Page{Doors: kept[from:to], Total: len(kept)}, nil
}

// Matches reports whether d's name or location holds search, ignoring case
// and the spaces around search; every door matches an empty search.
func Matches(d connector.StoredDoor, search string) bool {
	return strings.Contains(strings.ToLower(d.Name+" "+d.Location), strings.ToLower(strings.TrimSpace(search)))
}

func inZone(d connector.StoredDoor, zoneID int64) bool {
	return zoneID == 0 || slices.Contains(d.ZoneIDs, zoneID)
}

// Door returns the door whose id is id.
func (s Service) Door(ctx context.Context, id int64) (connector.StoredDoor, error) {
	return check.Lookup(ctx, id, s.Store.Door, ErrNoDoor)
}

// Create creates a door from f.
func (s Service) Create(ctx context.Context, f Fields) (connector.StoredDoor, error) {
	d, err := s.checkDoor(ctx, 0, f)
	if err != nil {
		return connector.StoredDoor{}, err
	}
	d.CreatedAt = s.Clock.Now()
	id, err := s.Store.CreateDoor(ctx, d)
	if err != nil {
		return connector.StoredDoor{}, err
	}
	return s.Door(ctx, id)
}

// Update replaces what f describes of the door whose id is id.
func (s Service) Update(ctx context.Context, id int64, f Fields) (connector.StoredDoor, error) {
	if _, err := s.Door(ctx, id); err != nil {
		return connector.StoredDoor{}, err
	}
	d, err := s.checkDoor(ctx, id, f)
	if err != nil {
		return connector.StoredDoor{}, err
	}
	d.ID = id
	// The door was just read and doors are never removed, so it is there
	// to update.
	if _, err := s.Store.UpdateDoor(ctx, d); err != nil {
		return connector.StoredDoor{}, err
	}
	return s.Door(ctx, id)
}

// Delete soft-deletes the door whose id is id. A door deleted already
// keeps its deletion time.
func (s Service) Delete(ctx context.Context, id int64) error {
	d, err := s.Door(ctx, id)
	if err != nil || d.DeletedAt != nil {
		return err
	}
	now := s.Clock.Now()
	_, err = s.Store.SetDoorDeleted(ctx, id, &now)
	return err
}

// Restore undoes the deletion of the door whose id is id.
func (s Service) Restore(ctx context.Context, id int64) error {
	found, err := s.Store.SetDoorDeleted(ctx, id, nil)
	if err == nil && !found {
		return ErrNoDoor
	}
	return err
}

// checkDoor returns f as a door to store, or the problems with it. id is
// the door f describes, zero for a new one, whose own name is no conflict.
func (s Service) checkDoor(ctx context.Context, id int64, f Fields) (connector.StoredDoor, error) {
	zones, err := s.Store.Zones(ctx)
	if err != nil {
		return connector.StoredDoor{}, err
	}
	d := connector.StoredDoor{
		Name: strings.TrimSpace(f.Name), Location: strings.TrimSpace(f.Location), Notes: strings.TrimSpace(f.Notes),
		Public: f.Public, ZoneIDs: check.Distinct(f.ZoneIDs),
	}
	nameIssue, err := s.doorNameProblem(ctx, id, d.Name)
	if err != nil {
		return connector.StoredDoor{}, err
	}
	var found check.List
	found.Add("name", nameIssue)
	found.Add("location", check.TooLong(d.Location, maxLocationLength, check.LongText))
	found.Add("notes", check.TooLong(d.Notes, maxNotesLength, check.LongText))
	found.Add("zone_ids", check.Unknown(d.ZoneIDs, zones, zoneID, "No zone has id %d."))
	return d, found.Err()
}

// doorNameProblem is the problem with name as the name of the door whose
// id is id. Names differing only in case are the same name.
func (s Service) doorNameProblem(ctx context.Context, id int64, name string) (string, error) {
	if problem := nameProblem(name); problem != "" {
		return problem, nil
	}
	other, taken, err := s.Store.DoorNamed(ctx, name)
	if err != nil || !taken || other.ID == id {
		return "", err
	}
	return takenBy(other), nil
}

// takenBy is the problem with a name other already has.
func takenBy(other connector.StoredDoor) string {
	if other.DeletedAt != nil {
		return "A deleted door has this name. Restore it, or choose another name."
	}
	return "Another door has this name."
}

func nameProblem(name string) string {
	if problem := check.Required(name, "Enter a name."); problem != "" {
		return problem
	}
	return check.TooLong(name, maxNameLength, check.LongText)
}

func zoneID(z connector.StoredZone) int64 { return z.ID }

func doorID(d connector.StoredDoor) int64 { return d.ID }

func scheduleID(sch connector.StoredSchedule) int64 { return sch.ID }

// Configure makes overrides, each keyed by the name of the field it
// overrides and checked and sealed by settings.Service.SealOverrides, the
// configuration overrides of the door whose id is id, which the caller has
// read, and returns the door as it is now.
func (s Service) Configure(ctx context.Context, id int64, overrides map[string]string) (connector.StoredDoor, error) {
	// Doors are never removed, so the door the caller read is there to update.
	if _, err := s.Store.SetDoorConfig(ctx, id, overrides); err != nil {
		return connector.StoredDoor{}, err
	}
	return s.Door(ctx, id)
}
