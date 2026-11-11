package testsupport

import (
	"cmp"
	"context"
	"errors"
	"maps"
	"slices"
	"strings"
	"time"

	"github.com/dakriy/doorcode/internal/connector"
)

// ErrNoSuchReference is what the stub store reports for an id naming a
// schedule, zone, door, or picture it does not hold, as a foreign key
// would.
var ErrNoSuchReference = errors.New("stub: no such reference")

// zoneDoor is one door's place in one zone.
type zoneDoor struct {
	zone int64
	door int64
}

// storedPicture is a picture with its image data.
type storedPicture struct {
	connector.StoredPicture
	image     []byte
	thumbnail []byte
}

// Doors returns every door in name order.
func (s *Store) Doors(context.Context) ([]connector.StoredDoor, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.doorsCalls++
	return s.doorsWhere(func(connector.StoredDoor) bool { return true })
}

// CurrentDoors returns the doors that are not deleted in name order.
func (s *Store) CurrentDoors(context.Context) ([]connector.StoredDoor, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.doorsWhere(func(d connector.StoredDoor) bool { return d.DeletedAt == nil })
}

// doorsWhere returns the doors keep holds for in name order. The caller
// holds s.mu.
func (s *Store) doorsWhere(keep func(connector.StoredDoor) bool) ([]connector.StoredDoor, error) {
	if err := s.usable(doorsAggregate); err != nil {
		return nil, err
	}
	kept := []connector.StoredDoor{}
	for _, d := range s.doors {
		if keep(d) {
			kept = append(kept, s.cloneDoor(d))
		}
	}
	slices.SortFunc(kept, func(a, b connector.StoredDoor) int {
		return cmp.Or(strings.Compare(strings.ToLower(a.Name), strings.ToLower(b.Name)), cmp.Compare(a.ID, b.ID))
	})
	return kept, nil
}

// DoorsCalls is how many times Doors was called, so a test can show an
// action reads only the doors it needs rather than every door.
func (s *Store) DoorsCalls() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.doorsCalls
}

// Door returns the door with id.
func (s *Store) Door(_ context.Context, id int64) (connector.StoredDoor, bool, error) {
	s.waitFor(id)
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.writable(doorsAggregate, doorLookups); err != nil {
		return connector.StoredDoor{}, false, err
	}
	held := s.doorByID(id)
	if held == nil {
		return connector.StoredDoor{}, false, nil
	}
	return s.cloneDoor(*held), true, nil
}

// HardwareDoor returns the id of the door whose controller has hardwareID.
func (s *Store) HardwareDoor(_ context.Context, hardwareID string) (int64, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.writable(doorsAggregate, controllerReads); err != nil {
		return 0, false, err
	}
	for _, d := range s.doors {
		if d.Controller != nil && d.Controller.HardwareID == hardwareID {
			return d.ID, true, nil
		}
	}
	return 0, false, nil
}

// DoorNamed returns the door whose name is name ignoring case.
func (s *Store) DoorNamed(_ context.Context, name string) (connector.StoredDoor, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.usable(doorsAggregate); err != nil {
		return connector.StoredDoor{}, false, err
	}
	for _, d := range s.doors {
		if strings.EqualFold(d.Name, name) {
			return s.cloneDoor(d), true, nil
		}
	}
	return connector.StoredDoor{}, false, nil
}

// CreateDoor stores d with the next id, without a primary picture.
func (s *Store) CreateDoor(_ context.Context, d connector.StoredDoor) (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.storableDoor(d); err != nil {
		return 0, err
	}
	s.nextDoor++
	d.ID = s.nextDoor
	d.PrimaryPictureID, d.Controller, d.Config, d.FirmwareID, d.SentConfig = nil, nil, map[string]string{}, nil, nil
	s.placeDoor(d.ID, d.ZoneIDs)
	d.ZoneIDs = nil
	s.doors = append(s.doors, d)
	return d.ID, nil
}

// UpdateDoor stores d over the door with d.ID, keeping its primary
// picture, deletion, and creation time.
func (s *Store) UpdateDoor(_ context.Context, d connector.StoredDoor) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.storableDoor(d); err != nil {
		return false, err
	}
	held := s.doorByID(d.ID)
	if held == nil {
		return false, nil
	}
	held.Name, held.Location, held.Notes, held.Public = d.Name, d.Location, d.Notes, d.Public
	s.placeDoor(d.ID, d.ZoneIDs)
	return true, nil
}

// SetController stores c as the controller of the door with id, refusing
// a hardware id another door's controller has.
func (s *Store) SetController(_ context.Context, id int64, c *connector.StoredController) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.writable(doorsAggregate, doorWrites); err != nil {
		return false, err
	}
	if s.failing[controllerWrites] {
		return false, ErrUnavailable
	}
	for _, held := range s.doors {
		if c != nil && held.ID != id && held.Controller != nil && held.Controller.HardwareID == c.HardwareID {
			return false, ErrDuplicate
		}
	}
	held := s.doorByID(id)
	if held == nil {
		return false, nil
	}
	held.Controller = clonedController(c)
	return true, nil
}

// clonedController is a copy of c sharing nothing with it.
func clonedController(c *connector.StoredController) *connector.StoredController {
	if c == nil {
		return nil
	}
	copied := *c
	copied.OpenedAt = cloneTime(c.OpenedAt)
	if c.Telemetry != nil {
		t := *c.Telemetry
		copied.Telemetry = &t
	}
	copied.ConfigMismatchSince = cloneTime(c.ConfigMismatchSince)
	copied.ConfigRevertedAt = cloneTime(c.ConfigRevertedAt)
	return &copied
}

// cloneTime is a copy of at sharing nothing with it.
func cloneTime(at *time.Time) *time.Time {
	if at == nil {
		return nil
	}
	copied := *at
	return &copied
}

// SetDoorState stores st as the state of the controller of the door with
// id, keeping its hardware id and online status.
func (s *Store) SetDoorState(_ context.Context, id int64, st connector.StoredDoorState) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.writable(doorsAggregate, doorWrites); err != nil {
		return false, err
	}
	if s.failing[controllerWrites] {
		return false, ErrUnavailable
	}
	held := s.doorByID(id)
	if held == nil || held.Controller == nil {
		return false, nil
	}
	c := *held.Controller
	c.StoredDoorState = st
	held.Controller = clonedController(&c)
	return true, nil
}

// SetDoorFirmware stores firmwareID as the firmware the door with id runs
// instead of the default, refusing firmware the stub does not hold.
func (s *Store) SetDoorFirmware(_ context.Context, id int64, firmwareID *int64) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.writable(doorsAggregate, doorWrites); err != nil {
		return false, err
	}
	if firmwareID != nil && s.firmwareByID(*firmwareID) == nil {
		return false, ErrNoSuchReference
	}
	held := s.doorByID(id)
	if held == nil {
		return false, nil
	}
	held.FirmwareID = cloneID(firmwareID)
	return true, nil
}

// cloneID is a copy of id sharing nothing with it.
func cloneID(id *int64) *int64 {
	if id == nil {
		return nil
	}
	copied := *id
	return &copied
}

// SetDoorConfig stores config as the configuration overrides of the door
// with id, refusing a nil one as the column's constraint would.
func (s *Store) SetDoorConfig(_ context.Context, id int64, config map[string]string) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.writable(doorsAggregate, doorWrites); err != nil {
		return false, err
	}
	if config == nil {
		return false, ErrNull
	}
	held := s.doorByID(id)
	if held == nil {
		return false, nil
	}
	held.Config = maps.Clone(config)
	return true, nil
}

// SetSentConfig stores sent as the configuration the door with id was
// last sent.
func (s *Store) SetSentConfig(_ context.Context, id int64, sent connector.StoredSentConfig) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.writable(doorsAggregate, doorWrites); err != nil {
		return false, err
	}
	if s.failing[sentConfigWrites] {
		return false, ErrUnavailable
	}
	held := s.doorByID(id)
	if held == nil {
		return false, nil
	}
	held.SentConfig = &sent
	return true, nil
}

// SetDoorDeleted stores at as the deletion time of the door with id.
func (s *Store) SetDoorDeleted(_ context.Context, id int64, at *time.Time) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.writable(doorsAggregate, doorWrites); err != nil {
		return false, err
	}
	held := s.doorByID(id)
	if held == nil {
		return false, nil
	}
	held.DeletedAt = at
	return true, nil
}

// storableDoor reports why d cannot be stored, as the database's
// constraints would.
func (s *Store) storableDoor(d connector.StoredDoor) error {
	if err := s.writable(doorsAggregate, doorWrites); err != nil {
		return err
	}
	for _, id := range d.ZoneIDs {
		if s.zoneByID(id) == nil {
			return ErrNoSuchReference
		}
	}
	for _, held := range s.doors {
		if held.ID != d.ID && strings.EqualFold(held.Name, d.Name) {
			return ErrDuplicate
		}
	}
	return nil
}

func (s *Store) doorByID(id int64) *connector.StoredDoor {
	for i := range s.doors {
		if s.doors[i].ID == id {
			return &s.doors[i]
		}
	}
	return nil
}

// cloneDoor is a copy of d sharing nothing with it, with the zones that
// hold it.
func (s *Store) cloneDoor(d connector.StoredDoor) connector.StoredDoor {
	d.Controller = clonedController(d.Controller)
	d.Config = maps.Clone(d.Config)
	d.FirmwareID = cloneID(d.FirmwareID)
	if d.SentConfig != nil {
		sent := *d.SentConfig
		d.SentConfig = &sent
	}
	d.ZoneIDs = []int64{}
	for _, p := range s.zoneDoors {
		if p.door == d.ID {
			d.ZoneIDs = append(d.ZoneIDs, p.zone)
		}
	}
	slices.Sort(d.ZoneIDs)
	return d
}

// placeDoor makes zones the zones the door with id is in.
func (s *Store) placeDoor(id int64, zones []int64) {
	s.zoneDoors = slices.DeleteFunc(slices.Clone(s.zoneDoors), func(p zoneDoor) bool { return p.door == id })
	for _, z := range zones {
		s.zoneDoors = append(s.zoneDoors, zoneDoor{zone: z, door: id})
	}
}

// fillZone makes doors the doors in the zone with id.
func (s *Store) fillZone(id int64, doors []int64) {
	s.zoneDoors = slices.DeleteFunc(slices.Clone(s.zoneDoors), func(p zoneDoor) bool { return p.zone == id })
	for _, d := range doors {
		s.zoneDoors = append(s.zoneDoors, zoneDoor{zone: id, door: d})
	}
}

// AddPicture stores p with the next id.
func (s *Store) AddPicture(_ context.Context, p connector.StoredPicture, image, thumbnail []byte) (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.writable(doorsAggregate, doorWrites); err != nil {
		return 0, err
	}
	if s.doorByID(p.DoorID) == nil {
		return 0, ErrNoSuchReference
	}
	if image == nil || thumbnail == nil {
		return 0, ErrNull
	}
	s.nextPicture++
	p.ID = s.nextPicture
	s.pictures = append(s.pictures, storedPicture{StoredPicture: p, image: slices.Clone(image), thumbnail: slices.Clone(thumbnail)})
	return p.ID, nil
}

// Pictures returns the door's pictures in id order.
func (s *Store) Pictures(_ context.Context, doorID int64) ([]connector.StoredPicture, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.usable(doorsAggregate); err != nil {
		return nil, err
	}
	held := []connector.StoredPicture{}
	for _, p := range s.pictures {
		if p.DoorID == doorID {
			held = append(held, p.StoredPicture)
		}
	}
	return held, nil
}

// PictureImage returns the image of the door's picture with id.
func (s *Store) PictureImage(_ context.Context, doorID, id int64) ([]byte, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.pictureData(doorID, id, func(p storedPicture) []byte { return p.image })
}

// PictureThumbnail returns the thumbnail of the door's picture with id.
func (s *Store) PictureThumbnail(_ context.Context, doorID, id int64) ([]byte, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.pictureData(doorID, id, func(p storedPicture) []byte { return p.thumbnail })
}

func (s *Store) pictureData(doorID, id int64, data func(storedPicture) []byte) ([]byte, bool, error) {
	if err := s.usable(doorsAggregate); err != nil {
		return nil, false, err
	}
	p := s.pictureOf(doorID, id)
	if p == nil {
		return nil, false, nil
	}
	return slices.Clone(data(*p)), true, nil
}

// SetPrimaryPicture makes the picture with id the door's primary picture.
func (s *Store) SetPrimaryPicture(_ context.Context, doorID, id int64) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.writable(doorsAggregate, doorWrites); err != nil {
		return false, err
	}
	if s.failing[primaryPictureWrites] {
		return false, ErrUnavailable
	}
	d := s.doorByID(doorID)
	if d == nil {
		return false, nil
	}
	if s.pictureOf(doorID, id) == nil {
		return false, ErrNoSuchReference
	}
	d.PrimaryPictureID = &id
	return true, nil
}

// DeletePicture removes the door's picture with id; a door whose primary
// picture it was has none.
func (s *Store) DeletePicture(_ context.Context, doorID, id int64) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.writable(doorsAggregate, doorWrites); err != nil {
		return false, err
	}
	i := slices.IndexFunc(s.pictures, func(p storedPicture) bool { return p.DoorID == doorID && p.ID == id })
	if i < 0 {
		return false, nil
	}
	s.pictures = slices.Delete(slices.Clone(s.pictures), i, i+1)
	if d := s.doorByID(doorID); d.PrimaryPictureID != nil && *d.PrimaryPictureID == id {
		d.PrimaryPictureID = nil
	}
	return true, nil
}

func (s *Store) pictureOf(doorID, id int64) *storedPicture {
	for i := range s.pictures {
		if s.pictures[i].DoorID == doorID && s.pictures[i].ID == id {
			return &s.pictures[i]
		}
	}
	return nil
}

// Zones returns every zone in name order.
func (s *Store) Zones(context.Context) ([]connector.StoredZone, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.usable(zonesAggregate); err != nil {
		return nil, err
	}
	all := []connector.StoredZone{}
	for _, z := range s.zones {
		all = append(all, s.cloneZone(z))
	}
	slices.SortFunc(all, func(a, b connector.StoredZone) int {
		return cmp.Or(strings.Compare(strings.ToLower(a.Name), strings.ToLower(b.Name)), cmp.Compare(a.ID, b.ID))
	})
	return all, nil
}

// Zone returns the zone with id.
func (s *Store) Zone(_ context.Context, id int64) (connector.StoredZone, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.usable(zonesAggregate); err != nil {
		return connector.StoredZone{}, false, err
	}
	held := s.zoneByID(id)
	if held == nil {
		return connector.StoredZone{}, false, nil
	}
	return s.cloneZone(*held), true, nil
}

// CreateZone stores z with the next id.
func (s *Store) CreateZone(_ context.Context, z connector.StoredZone) (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.storableZone(z); err != nil {
		return 0, err
	}
	s.nextZone++
	z.ID = s.nextZone
	s.fillZone(z.ID, z.DoorIDs)
	z.DoorIDs, z.OpenScheduleIDs = nil, slices.Clone(z.OpenScheduleIDs)
	s.zones = append(s.zones, z)
	return z.ID, nil
}

// UpdateZone stores z over the zone with z.ID.
func (s *Store) UpdateZone(_ context.Context, z connector.StoredZone) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.storableZone(z); err != nil {
		return false, err
	}
	held := s.zoneByID(z.ID)
	if held == nil {
		return false, nil
	}
	held.Name, held.Description, held.OpenScheduleIDs = z.Name, z.Description, slices.Clone(z.OpenScheduleIDs)
	s.fillZone(z.ID, z.DoorIDs)
	return true, nil
}

// DeleteZone removes the zone with id and takes it off the rules; its
// doors stay.
func (s *Store) DeleteZone(_ context.Context, id int64) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.writable(zonesAggregate, zoneWrites); err != nil {
		return false, err
	}
	i := slices.IndexFunc(s.zones, func(z connector.StoredZone) bool { return z.ID == id })
	if i < 0 {
		return false, nil
	}
	s.zones = slices.Delete(slices.Clone(s.zones), i, i+1)
	s.fillZone(id, nil)
	s.dropRuleZone(id)
	return true, nil
}

// storableZone reports why z cannot be stored, as the database's
// constraints would.
func (s *Store) storableZone(z connector.StoredZone) error {
	if err := s.writable(zonesAggregate, zoneWrites); err != nil {
		return err
	}
	missing := slices.ContainsFunc(z.DoorIDs, func(id int64) bool { return s.doorByID(id) == nil }) ||
		slices.ContainsFunc(z.OpenScheduleIDs, func(id int64) bool { return s.scheduleByID(id) == nil })
	if missing {
		return ErrNoSuchReference
	}
	for _, held := range s.zones {
		if held.ID != z.ID && strings.EqualFold(held.Name, z.Name) {
			return ErrDuplicate
		}
	}
	return nil
}

func (s *Store) zoneByID(id int64) *connector.StoredZone {
	for i := range s.zones {
		if s.zones[i].ID == id {
			return &s.zones[i]
		}
	}
	return nil
}

// cloneZone is z with the doors in it, sharing nothing with it.
func (s *Store) cloneZone(z connector.StoredZone) connector.StoredZone {
	z.OpenScheduleIDs = append([]int64{}, slices.Sorted(slices.Values(z.OpenScheduleIDs))...)
	z.DoorIDs = []int64{}
	for _, p := range s.zoneDoors {
		if p.zone == z.ID {
			z.DoorIDs = append(z.DoorIDs, p.door)
		}
	}
	slices.Sort(z.DoorIDs)
	return z
}

// FailDoors makes the door and picture methods report ErrUnavailable.
func (s *Store) FailDoors() {
	s.failing[doorsAggregate] = true
}

// FailDoorLookups makes Door report ErrUnavailable, while every door can
// still be read with Doors.
func (s *Store) FailDoorLookups() {
	s.failing[doorLookups] = true
}

// FailDoorWrites makes the methods that change doors and pictures report
// ErrUnavailable, while the ones that read them still work.
func (s *Store) FailDoorWrites() {
	s.failing[doorWrites] = true
}

// FailPrimaryPictures makes SetPrimaryPicture report ErrUnavailable,
// while pictures can still be added and deleted.
func (s *Store) FailPrimaryPictures() {
	s.failing[primaryPictureWrites] = true
}

// FailControllerWrites makes SetController and SetDoorState report
// ErrUnavailable, while doors can still be changed otherwise.
func (s *Store) FailControllerWrites() {
	s.failing[controllerWrites] = true
}

// FailSentConfigWrites makes SetSentConfig report ErrUnavailable, while
// doors can still be changed otherwise.
func (s *Store) FailSentConfigWrites() {
	s.failing[sentConfigWrites] = true
}

// FailControllerReads makes HardwareDoor report ErrUnavailable, while
// doors can still be read otherwise.
func (s *Store) FailControllerReads() {
	s.failing[controllerReads] = true
}

// FailZones makes the zone methods report ErrUnavailable.
func (s *Store) FailZones() {
	s.failing[zonesAggregate] = true
}

// FailZoneWrites makes the methods that change zones report
// ErrUnavailable, while the ones that read them still work.
func (s *Store) FailZoneWrites() {
	s.failing[zoneWrites] = true
}

// HoldDoor makes the next lookup of the door whose id is id wait until the
// function it returns is called, while every other lookup is answered.
func (s *Store) HoldDoor(id int64) (release func()) {
	s.mu.Lock()
	defer s.mu.Unlock()
	held := make(chan struct{})
	s.holds[id] = held
	return func() { close(held) }
}

// waitFor waits while the door whose id is id is held, ending the hold.
func (s *Store) waitFor(id int64) {
	s.mu.Lock()
	held, holding := s.holds[id]
	delete(s.holds, id)
	s.mu.Unlock()
	if holding {
		<-held
	}
}
