package memstore

import (
	"cmp"
	"context"
	"maps"
	"slices"

	"github.com/dakriy/doorcode/internal/connector"
)

// PurgeDoor removes the door and every row that refers to it.
func (s *Store) PurgeDoor(_ context.Context, id int64) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.doors[id]; !ok {
		return false, nil
	}
	delete(s.doors, id)
	delete(s.updates, id)
	maps.DeleteFunc(s.zoneDoors, func(p pair, _ bool) bool { return p.b == id })
	maps.DeleteFunc(s.pictures, func(_ int64, p picture) bool { return p.DoorID == id })
	maps.DeleteFunc(s.overrides, func(_ int64, o connector.StoredOverride) bool { return o.DoorID == id })
	maps.DeleteFunc(s.credentials, func(_ int64, c connector.StoredCredential) bool { return c.DoorID == id })
	maps.DeleteFunc(s.rollups, func(k rollupKey, _ connector.StoredRollup) bool { return k.doorID == id })
	s.events = slices.DeleteFunc(slices.Clone(s.events), func(e connector.StoredEvent) bool { return e.DoorID == id })
	s.samples = slices.DeleteFunc(slices.Clone(s.samples), func(sample connector.StoredSample) bool { return sample.DoorID == id })
	return true, nil
}

// PurgeUser removes the user and every row that refers to them, refusing
// a user firmware or an override names.
func (s *Store) PurgeUser(_ context.Context, id int64) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.users[id]; !ok {
		return false, nil
	}
	for _, f := range s.firmware {
		if f.UploadedBy == id {
			return false, errReferenced
		}
	}
	for _, o := range s.overrides {
		if o.CreatedBy == id {
			return false, errReferenced
		}
	}
	delete(s.users, id)
	maps.DeleteFunc(s.members, func(m pair, _ bool) bool { return m.b == id })
	maps.DeleteFunc(s.sessions, func(_ string, session connector.StoredSession) bool { return session.UserID == id })
	maps.DeleteFunc(s.tokens, func(_ int64, t connector.StoredToken) bool { return t.UserID == id })
	maps.DeleteFunc(s.requests, func(_ int64, r connector.StoredRequest) bool { return r.UserID == id })
	s.events = slices.DeleteFunc(slices.Clone(s.events), func(e connector.StoredEvent) bool { return e.UserID != nil && *e.UserID == id })
	return true, nil
}

// UserOverrides returns the overrides the user created, by id.
func (s *Store) UserOverrides(_ context.Context, userID int64) ([]connector.StoredOverride, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	overrides := []connector.StoredOverride{}
	for _, o := range s.overrides {
		if o.CreatedBy == userID {
			overrides = append(overrides, s.overrideOut(o))
		}
	}
	slices.SortFunc(overrides, func(x, y connector.StoredOverride) int { return cmp.Compare(x.ID, y.ID) })
	return overrides, nil
}
