package memstore

import (
	"cmp"
	"context"
	"slices"

	"github.com/dakriy/doorcode/internal/connector"
)

// firmwareRow is a firmware with its image.
type firmwareRow struct {
	connector.StoredFirmware
	image []byte
}

// firmwareOut is f with its uploader's display name.
func (s *Store) firmwareOut(f connector.StoredFirmware) connector.StoredFirmware {
	f.UploaderName = s.users[f.UploadedBy].DisplayName
	return f
}

// AddFirmware stores f with its image and returns its id.
func (s *Store) AddFirmware(_ context.Context, f connector.StoredFirmware, image []byte) (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if image == nil {
		return 0, errNil
	}
	if _, ok := s.users[f.UploadedBy]; !ok {
		return 0, errNoReference
	}
	for _, other := range s.firmware {
		if other.Version == f.Version {
			return 0, errDuplicate
		}
	}
	f.ID = s.next("firmware")
	f.Default = false
	f.UploaderName = ""
	s.firmware[f.ID] = firmwareRow{StoredFirmware: f, image: list(image)}
	return f.ID, nil
}

// Firmwares returns every firmware, newest upload first.
func (s *Store) Firmwares(_ context.Context) ([]connector.StoredFirmware, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	all := []connector.StoredFirmware{}
	for _, row := range s.firmware {
		all = append(all, s.firmwareOut(row.StoredFirmware))
	}
	slices.SortFunc(all, func(x, y connector.StoredFirmware) int {
		if c := y.UploadedAt.Compare(x.UploadedAt); c != 0 {
			return c
		}
		return cmp.Compare(y.ID, x.ID)
	})
	return all, nil
}

// Firmware returns the firmware whose id is id.
func (s *Store) Firmware(_ context.Context, id int64) (connector.StoredFirmware, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	row, ok := s.firmware[id]
	if !ok {
		return connector.StoredFirmware{}, false, nil
	}
	return s.firmwareOut(row.StoredFirmware), true, nil
}

// FirmwareImage returns the firmware's image.
func (s *Store) FirmwareImage(_ context.Context, id int64) ([]byte, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	row, ok := s.firmware[id]
	return list(row.image), ok, nil
}

// SetDefaultFirmware makes the firmware whose id is id the only default.
func (s *Store) SetDefaultFirmware(_ context.Context, id int64) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.firmware[id]; !ok {
		return false, nil
	}
	for key, row := range s.firmware {
		row.Default = key == id
		s.firmware[key] = row
	}
	return true, nil
}

// ClearDefaultFirmware leaves the site without default firmware.
func (s *Store) ClearDefaultFirmware(_ context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for key, row := range s.firmware {
		row.Default = false
		s.firmware[key] = row
	}
	return nil
}

// DeleteFirmware removes the firmware, refusing firmware a door runs
// instead of the default.
func (s *Store) DeleteFirmware(_ context.Context, id int64) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.firmware[id]; !ok {
		return false, nil
	}
	for _, d := range s.doors {
		if d.FirmwareID != nil && *d.FirmwareID == id {
			return false, errReferenced
		}
	}
	delete(s.firmware, id)
	return true, nil
}

// updateCopy is a copy of u.
func updateCopy(u connector.StoredUpdate) connector.StoredUpdate {
	u.TokenHash = list(u.TokenHash)
	u.FinishedAt = cloneTime(u.FinishedAt)
	return u
}

// DoorUpdates returns every door's latest update, by door id.
func (s *Store) DoorUpdates(_ context.Context) ([]connector.StoredUpdate, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	updates := []connector.StoredUpdate{}
	for _, u := range s.updates {
		updates = append(updates, updateCopy(u))
	}
	byID(updates, func(u connector.StoredUpdate) int64 { return u.DoorID })
	return updates, nil
}

// PutDoorUpdate stores u as its door's latest update.
func (s *Store) PutDoorUpdate(_ context.Context, u connector.StoredUpdate) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.doors[u.DoorID]; !ok {
		return errNoReference
	}
	s.updates[u.DoorID] = updateCopy(u)
	return nil
}
