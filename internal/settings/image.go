package settings

import (
	"context"
	"errors"

	"github.com/dakriy/doorcode/internal/connector"
	"github.com/dakriy/doorcode/internal/picture"
)

// ErrNoSignInImage reports that the sign-in page shows no picture.
var ErrNoSignInImage = errors.New("the sign-in page has no picture")

// SignInImage returns the picture the sign-in page shows, and whether it
// shows one.
func (s Service) SignInImage(ctx context.Context) (connector.StoredImage, bool, error) {
	return s.Store.SignInImage(ctx)
}

// SetSignInImage makes the picture data, reduced as door pictures are, the
// one the sign-in page shows, or reports the problem with it.
func (s Service) SetSignInImage(ctx context.Context, data []byte) error {
	prepared, err := picture.Prepare(data)
	if err != nil {
		return err
	}
	return s.Store.PutSignInImage(ctx, connector.StoredImage{Data: prepared.Image, UpdatedAt: s.Clock.Now()})
}

// RemoveSignInImage takes the picture off the sign-in page, reporting
// ErrNoSignInImage when it shows none.
func (s Service) RemoveSignInImage(ctx context.Context) error {
	removed, err := s.Store.RemoveSignInImage(ctx)
	if err == nil && !removed {
		return ErrNoSignInImage
	}
	return err
}

// ErrNoLogo reports that the site has no logo.
var ErrNoLogo = errors.New("the site has no logo")

// Logo returns the site's logo, and whether it has one.
func (s Service) Logo(ctx context.Context) (connector.StoredImage, bool, error) {
	return s.Store.Logo(ctx)
}

// SetLogo makes the picture data, reduced and kept transparent, the
// site's logo, or reports the problem with it.
func (s Service) SetLogo(ctx context.Context, data []byte) error {
	prepared, err := picture.PrepareLogo(data)
	if err != nil {
		return err
	}
	return s.Store.PutLogo(ctx, connector.StoredImage{Data: prepared, UpdatedAt: s.Clock.Now()})
}

// RemoveLogo takes the site's logo away, reporting ErrNoLogo when it has
// none.
func (s Service) RemoveLogo(ctx context.Context) error {
	removed, err := s.Store.RemoveLogo(ctx)
	if err == nil && !removed {
		return ErrNoLogo
	}
	return err
}
