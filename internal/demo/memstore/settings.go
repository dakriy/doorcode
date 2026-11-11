package memstore

import (
	"context"
	"maps"
	"time"

	"github.com/dakriy/doorcode/internal/connector"
)

// Settings returns every stored setting.
func (s *Store) Settings(_ context.Context) (map[string]connector.StoredSetting, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return maps.Clone(s.settings), nil
}

// PutSetting stores value under key.
func (s *Store) PutSetting(_ context.Context, key, value string, at time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.settings[key] = connector.StoredSetting{Value: value, UpdatedAt: at}
	return nil
}

// CodeHashing stores candidate unless parameters are stored, and returns
// the stored ones.
func (s *Store) CodeHashing(_ context.Context, candidate string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.codeHashing == nil {
		s.codeHashing = &candidate
	}
	return *s.codeHashing, nil
}

// SiteImage returns the site picture of kind.
func (s *Store) SiteImage(_ context.Context, kind connector.ImageKind) (connector.StoredImage, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	image, had := s.images[kind]
	if !had {
		return connector.StoredImage{}, false, nil
	}
	image.Data = list(image.Data)
	return image, true, nil
}

// SiteImageStoredAt returns when the site picture of kind was stored.
func (s *Store) SiteImageStoredAt(_ context.Context, kind connector.ImageKind) (time.Time, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	image, had := s.images[kind]
	return image.UpdatedAt, had, nil
}

// PutSiteImage stores the site picture of kind.
func (s *Store) PutSiteImage(_ context.Context, kind connector.ImageKind, image connector.StoredImage) error {
	if image.Data == nil {
		return errNil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	image.Data = list(image.Data)
	s.images[kind] = image
	return nil
}

// RemoveSiteImage removes the site picture of kind.
func (s *Store) RemoveSiteImage(_ context.Context, kind connector.ImageKind) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, had := s.images[kind]
	delete(s.images, kind)
	return had, nil
}
