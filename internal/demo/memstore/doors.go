package memstore

import (
	"cmp"
	"context"
	"maps"
	"slices"
	"strings"
	"time"

	"github.com/dakriy/doorcode/internal/connector"
)

// controllerCopy is a copy of c.
func controllerCopy(c *connector.StoredController) *connector.StoredController {
	if c == nil {
		return nil
	}
	copied := *c
	copied.StoredDoorState = stateCopy(c.StoredDoorState)
	return &copied
}

// stateCopy is a copy of st.
func stateCopy(st connector.StoredDoorState) connector.StoredDoorState {
	st.OpenedAt = cloneTime(st.OpenedAt)
	if st.Telemetry != nil {
		t := *st.Telemetry
		st.Telemetry = &t
	}
	return st
}

// doorOut is a copy of d with the zones it is in.
func (s *Store) doorOut(d connector.StoredDoor) connector.StoredDoor {
	d.OpenScheduleIDs = sortedIDs(d.OpenScheduleIDs)
	d.ZoneIDs = s.zonesOf(d.ID)
	d.PrimaryPictureID = cloneID(d.PrimaryPictureID)
	d.DeletedAt = cloneTime(d.DeletedAt)
	d.Controller = controllerCopy(d.Controller)
	d.Config = maps.Clone(d.Config)
	d.FirmwareID = cloneID(d.FirmwareID)
	return d
}

// zonesOf is the zones the door whose id is id is in, ascending.
func (s *Store) zonesOf(id int64) []int64 {
	zones := []int64{}
	for link := range s.zoneDoors {
		if link.b == id {
			zones = append(zones, link.a)
		}
	}
	slices.Sort(zones)
	return zones
}

// doorNameTaken reports whether a door other than the one whose id is id
// has name, ignoring case.
func (s *Store) doorNameTaken(name string, id int64) bool {
	for _, other := range s.doors {
		if other.ID != id && sameName(other.Name, name) {
			return true
		}
	}
	return false
}

// checkDoorLinks refuses open schedules and zones the store does not hold.
func (s *Store) checkDoorLinks(d connector.StoredDoor) error {
	for _, id := range d.OpenScheduleIDs {
		if _, ok := s.schedules[id]; !ok {
			return errNoReference
		}
	}
	for _, id := range d.ZoneIDs {
		if _, ok := s.zones[id]; !ok {
			return errNoReference
		}
	}
	return nil
}

// linkZones makes the zones whose ids are zoneIDs the zones of the door
// whose id is doorID.
func (s *Store) linkZones(doorID int64, zoneIDs []int64) {
	for link := range s.zoneDoors {
		if link.b == doorID {
			delete(s.zoneDoors, link)
		}
	}
	for _, zoneID := range zoneIDs {
		s.zoneDoors[pair{zoneID, doorID}] = true
	}
}

// Doors returns every door in name order.
func (s *Store) Doors(_ context.Context) ([]connector.StoredDoor, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	doors := []connector.StoredDoor{}
	for _, d := range s.doors {
		doors = append(doors, s.doorOut(d))
	}
	byName(doors, func(d connector.StoredDoor) string { return d.Name }, func(d connector.StoredDoor) int64 { return d.ID })
	return doors, nil
}

// Door returns the door whose id is id.
func (s *Store) Door(_ context.Context, id int64) (connector.StoredDoor, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	d, ok := s.doors[id]
	if !ok {
		return connector.StoredDoor{}, false, nil
	}
	return s.doorOut(d), true, nil
}

// CreateDoor stores d and returns its id.
func (s *Store) CreateDoor(_ context.Context, d connector.StoredDoor) (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.doorNameTaken(d.Name, 0) {
		return 0, errDuplicate
	}
	if err := s.checkDoorLinks(d); err != nil {
		return 0, err
	}
	stored := connector.StoredDoor{
		ID: s.next("doors"), Name: d.Name, Location: d.Location, Notes: d.Notes, Public: d.Public,
		OpenScheduleIDs: sortedIDs(d.OpenScheduleIDs), DeletedAt: cloneTime(d.DeletedAt), CreatedAt: d.CreatedAt,
		Config: map[string]string{},
	}
	s.doors[stored.ID] = stored
	s.linkZones(stored.ID, d.ZoneIDs)
	return stored.ID, nil
}

// UpdateDoor stores d's name, location, notes, public flag, open
// schedules, and zones.
func (s *Store) UpdateDoor(_ context.Context, d connector.StoredDoor) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	stored, ok := s.doors[d.ID]
	if !ok {
		return false, nil
	}
	if s.doorNameTaken(d.Name, d.ID) {
		return false, errDuplicate
	}
	if err := s.checkDoorLinks(d); err != nil {
		return false, err
	}
	stored.Name = d.Name
	stored.Location = d.Location
	stored.Notes = d.Notes
	stored.Public = d.Public
	stored.OpenScheduleIDs = sortedIDs(d.OpenScheduleIDs)
	s.doors[d.ID] = stored
	s.linkZones(d.ID, d.ZoneIDs)
	return true, nil
}

// changeDoor applies change to the door whose id is id, when there is one.
func (s *Store) changeDoor(id int64, change func(d *connector.StoredDoor) error) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	d, ok := s.doors[id]
	if !ok {
		return false, nil
	}
	if err := change(&d); err != nil {
		return false, err
	}
	s.doors[id] = d
	return true, nil
}

// SetDoorDeleted stores at as the door's deletion time.
func (s *Store) SetDoorDeleted(_ context.Context, id int64, at *time.Time) (bool, error) {
	return s.changeDoor(id, func(d *connector.StoredDoor) error {
		d.DeletedAt = cloneTime(at)
		return nil
	})
}

// hardwareTaken reports whether a door other than the one whose id is id
// has a controller with hardwareID.
func (s *Store) hardwareTaken(hardwareID string, id int64) bool {
	for _, other := range s.doors {
		if other.ID != id && other.Controller != nil && other.Controller.HardwareID == hardwareID {
			return true
		}
	}
	return false
}

// SetController stores c as the door's controller, nil detaching it.
func (s *Store) SetController(_ context.Context, id int64, c *connector.StoredController) (bool, error) {
	return s.changeDoor(id, func(d *connector.StoredDoor) error {
		if c != nil && s.hardwareTaken(c.HardwareID, id) {
			return errDuplicate
		}
		d.Controller = controllerCopy(c)
		return nil
	})
}

// SetDoorState stores st as what the door's controller last told.
func (s *Store) SetDoorState(_ context.Context, id int64, st connector.StoredDoorState) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	d, ok := s.doors[id]
	if !ok || d.Controller == nil {
		return false, nil
	}
	c := controllerCopy(d.Controller)
	c.StoredDoorState = stateCopy(st)
	d.Controller = c
	s.doors[id] = d
	return true, nil
}

// SetDoorFirmware makes firmwareID the firmware the door runs instead of
// the default.
func (s *Store) SetDoorFirmware(_ context.Context, id int64, firmwareID *int64) (bool, error) {
	return s.changeDoor(id, func(d *connector.StoredDoor) error {
		if firmwareID != nil {
			if _, ok := s.firmware[*firmwareID]; !ok {
				return errNoReference
			}
		}
		d.FirmwareID = cloneID(firmwareID)
		return nil
	})
}

// SetDoorConfig stores config as the door's configuration overrides.
func (s *Store) SetDoorConfig(_ context.Context, id int64, config map[string]string) (bool, error) {
	if config == nil {
		return false, errNil
	}
	return s.changeDoor(id, func(d *connector.StoredDoor) error {
		d.Config = maps.Clone(config)
		return nil
	})
}

// FirmwareVersions counts the doors that are not deleted by the firmware
// version their controller last reported.
func (s *Store) FirmwareVersions(_ context.Context) ([]connector.StoredVersionCount, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	counts := map[string]int{}
	for _, d := range s.doors {
		if d.DeletedAt == nil && d.Controller != nil {
			counts[d.Controller.FirmwareVersion]++
		}
	}
	versions := []connector.StoredVersionCount{}
	for version, doors := range counts {
		versions = append(versions, connector.StoredVersionCount{Version: version, Doors: doors})
	}
	slices.SortFunc(versions, func(x, y connector.StoredVersionCount) int {
		if c := cmp.Compare(y.Doors, x.Doors); c != 0 {
			return c
		}
		return strings.Compare(x.Version, y.Version)
	})
	return versions, nil
}
