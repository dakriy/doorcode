package testsupport

import (
	"context"
	"slices"

	"github.com/dakriy/doorcode/internal/connector"
)

// without is held less the items that refer, as a copy.
func without[T any](held []T, refer func(T) bool) []T {
	return slices.DeleteFunc(slices.Clone(held), refer)
}

// PurgeDoor removes the door with id and everything that refers to it.
func (s *Store) PurgeDoor(_ context.Context, id int64) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.writable(doorsAggregate, doorWrites); err != nil {
		return false, err
	}
	if s.doorByID(id) == nil {
		return false, nil
	}
	s.doors = without(s.doors, func(d connector.StoredDoor) bool { return d.ID == id })
	s.zoneDoors = without(s.zoneDoors, func(p zoneDoor) bool { return p.door == id })
	s.pictures = without(s.pictures, func(p storedPicture) bool { return p.DoorID == id })
	s.overrides = without(s.overrides, func(o connector.StoredOverride) bool { return o.DoorID == id })
	s.credentials = without(s.credentials, func(c connector.StoredCredential) bool { return c.DoorID == id })
	s.events = without(s.events, func(e connector.StoredEvent) bool { return e.DoorID == id })
	s.samples = without(s.samples, func(sample connector.StoredSample) bool { return sample.DoorID == id })
	s.rollups = without(s.rollups, func(r connector.StoredRollup) bool { return r.DoorID == id })
	s.updates = without(s.updates, func(u connector.StoredUpdate) bool { return u.DoorID == id })
	return true, nil
}

// PurgeUser removes the user with id and everything that refers to them,
// refusing, as the foreign keys would, a user firmware or an override
// names.
func (s *Store) PurgeUser(_ context.Context, id int64) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.writable(usersAggregate, userWrites); err != nil {
		return false, err
	}
	if s.userByID(id) == nil {
		return false, nil
	}
	uploaded := slices.ContainsFunc(s.firmware, func(f storedFirmware) bool { return f.UploadedBy == id })
	created := slices.ContainsFunc(s.overrides, func(o connector.StoredOverride) bool { return o.CreatedBy == id })
	if uploaded || created {
		return false, ErrReferenced
	}
	s.users = without(s.users, func(u connector.StoredUser) bool { return u.ID == id })
	s.members = without(s.members, func(m membership) bool { return m.user == id })
	s.tokens = without(s.tokens, func(t connector.StoredToken) bool { return t.UserID == id })
	s.requests = without(s.requests, func(r connector.StoredRequest) bool { return r.UserID == id })
	s.events = without(s.events, func(e connector.StoredEvent) bool { return e.UserID != nil && *e.UserID == id })
	sessions := map[string]connector.StoredSession{}
	for hash, session := range s.sessions {
		if session.UserID != id {
			sessions[hash] = session
		}
	}
	s.sessions = sessions
	return true, nil
}
