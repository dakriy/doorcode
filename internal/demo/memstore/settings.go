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

// SignInImage returns the sign-in page picture.
func (s *Store) SignInImage(_ context.Context) (connector.StoredImage, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.signInImage == nil {
		return connector.StoredImage{}, false, nil
	}
	image := *s.signInImage
	image.Data = list(image.Data)
	return image, true, nil
}

// PutSignInImage stores the sign-in page picture.
func (s *Store) PutSignInImage(_ context.Context, image connector.StoredImage) error {
	if image.Data == nil {
		return errNil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	image.Data = list(image.Data)
	s.signInImage = &image
	return nil
}

// RemoveSignInImage removes the sign-in page picture.
func (s *Store) RemoveSignInImage(_ context.Context) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	had := s.signInImage != nil
	s.signInImage = nil
	return had, nil
}

// Logo returns the site logo.
func (s *Store) Logo(_ context.Context) (connector.StoredImage, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.logo == nil {
		return connector.StoredImage{}, false, nil
	}
	image := *s.logo
	image.Data = list(image.Data)
	return image, true, nil
}

// PutLogo stores the site logo.
func (s *Store) PutLogo(_ context.Context, image connector.StoredImage) error {
	if image.Data == nil {
		return errNil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	image.Data = list(image.Data)
	s.logo = &image
	return nil
}

// RemoveLogo removes the site logo.
func (s *Store) RemoveLogo(_ context.Context) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	had := s.logo != nil
	s.logo = nil
	return had, nil
}
