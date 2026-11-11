package settings

import (
	"context"
	"time"

	"github.com/dakriy/doorcode/internal/connector"
)

// Image returns the site's picture of kind, and whether it has one.
func (s Service) Image(ctx context.Context, kind connector.ImageKind) (connector.StoredImage, bool, error) {
	return s.Store.SiteImage(ctx, kind)
}

// ImageStoredAt returns when the site's picture of kind was stored, and
// whether it has one.
func (s Service) ImageStoredAt(ctx context.Context, kind connector.ImageKind) (time.Time, bool, error) {
	return s.Store.SiteImageStoredAt(ctx, kind)
}

// SetImage makes the picture data, made ready to keep by prepare, the
// site's picture of kind, or reports the problem prepare finds with it.
func (s Service) SetImage(ctx context.Context, kind connector.ImageKind, prepare func([]byte) ([]byte, error), data []byte) error {
	prepared, err := prepare(data)
	if err != nil {
		return err
	}
	return s.Store.PutSiteImage(ctx, kind, connector.StoredImage{Data: prepared, UpdatedAt: s.Clock.Now()})
}

// RemoveImage takes the site's picture of kind away, and reports whether
// it had one.
func (s Service) RemoveImage(ctx context.Context, kind connector.ImageKind) (bool, error) {
	return s.Store.RemoveSiteImage(ctx, kind)
}
