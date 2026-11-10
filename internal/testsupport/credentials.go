package testsupport

import (
	"context"
	"time"

	"github.com/dakriy/doorcode/internal/connector"
)

// CreateCredential stores c with the next id, refusing a door it does not
// hold and a username it holds already.
func (s *Store) CreateCredential(_ context.Context, c connector.StoredCredential) (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.writable(credentialsAggregate, credentialWrites); err != nil {
		return 0, err
	}
	if s.doorByID(c.DoorID) == nil {
		return 0, ErrNoSuchReference
	}
	for _, held := range s.credentials {
		if held.Username == c.Username {
			return 0, ErrDuplicate
		}
	}
	c.ID = int64(len(s.credentials) + 1)
	s.credentials = append(s.credentials, c)
	return c.ID, nil
}

// DoorCredentials returns the door's credentials in id order.
func (s *Store) DoorCredentials(_ context.Context, doorID int64) ([]connector.StoredCredential, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.credentialsWhere(func(c connector.StoredCredential) bool { return c.DoorID == doorID })
}

// LiveCredentials returns the credentials not revoked, by door and id.
func (s *Store) LiveCredentials(context.Context) ([]connector.StoredCredential, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	live, err := s.credentialsWhere(func(c connector.StoredCredential) bool { return c.RevokedAt == nil })
	byDoor := []connector.StoredCredential{}
	for _, d := range s.doors {
		for _, c := range live {
			if c.DoorID == d.ID {
				byDoor = append(byDoor, c)
			}
		}
	}
	return byDoor, err
}

func (s *Store) credentialsWhere(keep func(connector.StoredCredential) bool) ([]connector.StoredCredential, error) {
	if err := s.usable(credentialsAggregate); err != nil {
		return nil, err
	}
	kept := []connector.StoredCredential{}
	for _, c := range s.credentials {
		if keep(c) {
			kept = append(kept, c)
		}
	}
	return kept, nil
}

// UseCredential stores at as when the credential with id was first used.
func (s *Store) UseCredential(_ context.Context, id int64, at time.Time) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.stampCredential(id, func(c *connector.StoredCredential) { c.UsedAt = &at })
}

// RevokeCredential stores at as when the credential with id was revoked.
func (s *Store) RevokeCredential(_ context.Context, id int64, at time.Time) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.stampCredential(id, func(c *connector.StoredCredential) { c.RevokedAt = &at })
}

func (s *Store) stampCredential(id int64, stamp func(*connector.StoredCredential)) (bool, error) {
	if err := s.writable(credentialsAggregate, credentialWrites); err != nil {
		return false, err
	}
	for i := range s.credentials {
		if s.credentials[i].ID == id {
			stamp(&s.credentials[i])
			return true, nil
		}
	}
	return false, nil
}

// FailCredentials makes the credential methods report ErrUnavailable.
func (s *Store) FailCredentials() {
	s.failing[credentialsAggregate] = true
}

// FailCredentialWrites makes the methods that change credentials report
// ErrUnavailable, while the ones that read them still work.
func (s *Store) FailCredentialWrites() {
	s.failing[credentialWrites] = true
}
