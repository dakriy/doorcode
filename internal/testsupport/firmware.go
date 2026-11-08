package testsupport

import (
	"cmp"
	"context"
	"slices"

	"github.com/dakriy/doorcode/internal/connector"
)

// storedFirmware is firmware with its image.
type storedFirmware struct {
	connector.StoredFirmware
	image []byte
}

// AddFirmware stores f with the next id and its image, refusing a version
// it holds, an uploader it does not hold, and a nil image.
func (s *Store) AddFirmware(_ context.Context, f connector.StoredFirmware, image []byte) (int64, error) {
	if err := s.writable(firmwareAggregate, firmwareWrites); err != nil {
		return 0, err
	}
	if image == nil {
		return 0, ErrNull
	}
	if s.userByID(f.UploadedBy) == nil {
		return 0, ErrNoSuchUser
	}
	for _, held := range s.firmware {
		if held.Version == f.Version {
			return 0, ErrDuplicate
		}
	}
	s.nextFirmware++
	f.ID, f.Default, f.UploaderName = s.nextFirmware, false, ""
	s.firmware = append(s.firmware, storedFirmware{StoredFirmware: f, image: slices.Clone(image)})
	return f.ID, nil
}

// Firmwares returns every firmware, newest upload first.
func (s *Store) Firmwares(context.Context) ([]connector.StoredFirmware, error) {
	if err := s.usable(firmwareAggregate); err != nil {
		return nil, err
	}
	all := []connector.StoredFirmware{}
	for _, f := range s.firmware {
		all = append(all, s.uploaded(f.StoredFirmware))
	}
	slices.SortFunc(all, func(a, b connector.StoredFirmware) int {
		return cmp.Or(b.UploadedAt.Compare(a.UploadedAt), cmp.Compare(b.ID, a.ID))
	})
	return all, nil
}

// Firmware returns the firmware with id.
func (s *Store) Firmware(_ context.Context, id int64) (connector.StoredFirmware, bool, error) {
	if err := s.usable(firmwareAggregate); err != nil {
		return connector.StoredFirmware{}, false, err
	}
	held := s.firmwareByID(id)
	if held == nil {
		return connector.StoredFirmware{}, false, nil
	}
	return s.uploaded(held.StoredFirmware), true, nil
}

// uploaded is f with its uploader's display name.
func (s *Store) uploaded(f connector.StoredFirmware) connector.StoredFirmware {
	f.UploaderName = s.userByID(f.UploadedBy).DisplayName
	return f
}

// FirmwareImage returns the image of the firmware with id.
func (s *Store) FirmwareImage(_ context.Context, id int64) ([]byte, bool, error) {
	if err := s.usable(firmwareAggregate); err != nil {
		return nil, false, err
	}
	held := s.firmwareByID(id)
	if held == nil {
		return nil, false, nil
	}
	return slices.Clone(held.image), true, nil
}

// SetDefaultFirmware makes the firmware with id the only default one.
func (s *Store) SetDefaultFirmware(_ context.Context, id int64) (bool, error) {
	if err := s.writable(firmwareAggregate, firmwareWrites); err != nil {
		return false, err
	}
	if s.firmwareByID(id) == nil {
		return false, nil
	}
	for i := range s.firmware {
		s.firmware[i].Default = s.firmware[i].ID == id
	}
	return true, nil
}

// ClearDefaultFirmware makes no firmware the default.
func (s *Store) ClearDefaultFirmware(context.Context) error {
	if err := s.writable(firmwareAggregate, firmwareWrites); err != nil {
		return err
	}
	for i := range s.firmware {
		s.firmware[i].Default = false
	}
	return nil
}

// DeleteFirmware removes the firmware with id, refusing firmware a door
// names, as the foreign key would.
func (s *Store) DeleteFirmware(_ context.Context, id int64) (bool, error) {
	if err := s.writable(firmwareAggregate, firmwareWrites); err != nil {
		return false, err
	}
	for _, d := range s.doors {
		if d.FirmwareID != nil && *d.FirmwareID == id {
			return false, ErrReferenced
		}
	}
	held := len(s.firmware)
	s.firmware = slices.DeleteFunc(s.firmware, func(f storedFirmware) bool { return f.ID == id })
	return len(s.firmware) < held, nil
}

// DoorUpdates returns every door's latest update, by door.
func (s *Store) DoorUpdates(context.Context) ([]connector.StoredUpdate, error) {
	if err := s.usable(updatesAggregate); err != nil {
		return nil, err
	}
	all := []connector.StoredUpdate{}
	for _, u := range s.updates {
		all = append(all, cloneUpdate(u))
	}
	slices.SortFunc(all, func(a, b connector.StoredUpdate) int { return cmp.Compare(a.DoorID, b.DoorID) })
	return all, nil
}

// PutDoorUpdate stores u over its door's latest update, refusing a door
// the stub does not hold.
func (s *Store) PutDoorUpdate(_ context.Context, u connector.StoredUpdate) error {
	if err := s.writable(updatesAggregate, firmwareWrites); err != nil {
		return err
	}
	if s.doorByID(u.DoorID) == nil {
		return ErrNoSuchReference
	}
	s.updates = slices.DeleteFunc(s.updates, func(held connector.StoredUpdate) bool { return held.DoorID == u.DoorID })
	s.updates = append(s.updates, cloneUpdate(u))
	return nil
}

// cloneUpdate is a copy of u sharing nothing with it.
func cloneUpdate(u connector.StoredUpdate) connector.StoredUpdate {
	u.TokenHash = slices.Clone(u.TokenHash)
	if u.FinishedAt != nil {
		at := *u.FinishedAt
		u.FinishedAt = &at
	}
	return u
}

func (s *Store) firmwareByID(id int64) *storedFirmware {
	for i := range s.firmware {
		if s.firmware[i].ID == id {
			return &s.firmware[i]
		}
	}
	return nil
}

// FailFirmware makes the methods of the firmware images report
// ErrUnavailable, while the doors' updates still work.
func (s *Store) FailFirmware() {
	s.failing[firmwareAggregate] = true
}

// FailUpdates makes DoorUpdates and PutDoorUpdate report ErrUnavailable,
// while the firmware images still work.
func (s *Store) FailUpdates() {
	s.failing[updatesAggregate] = true
}

// FailFirmwareWrites makes the methods that change firmware or updates
// report ErrUnavailable, while they can still be read.
func (s *Store) FailFirmwareWrites() {
	s.failing[firmwareWrites] = true
}
