package directory

import (
	"context"
	"fmt"
	"strings"

	"github.com/dakriy/doorcode/internal/auth"
	"github.com/dakriy/doorcode/internal/connector"
)

// InUseError is the refusal to remove a user for good who uploaded the
// firmware versions Firmware or created Overrides, which stay.
type InUseError struct {
	Firmware  []string
	Overrides []connector.StoredOverride
}

func (e InUseError) Error() string {
	var made []string
	if len(e.Firmware) > 0 {
		made = append(made, "uploaded firmware "+strings.Join(e.Firmware, ", "))
	}
	if len(e.Overrides) > 0 {
		var overrides []string
		for _, o := range e.Overrides {
			overrides = append(overrides, fmt.Sprintf("%d at door %d", o.ID, o.DoorID))
		}
		made = append(made, "created overrides "+strings.Join(overrides, ", "))
	}
	return "the user " + strings.Join(made, " and ") + ", which name them; delete those first"
}

// Is reports whether target is ErrInUse.
func (e InUseError) Is(target error) bool {
	return target == ErrInUse
}

// PurgeUser removes the user whose id is id for good, deleted or not, with
// their sessions, API tokens, memberships, access requests, and the
// events that name them, so their email and user id are free again. A
// person cannot remove their own account, and a user who uploaded
// firmware or created an override is refused with an InUseError while it
// stays.
func (s Service) PurgeUser(ctx context.Context, viewer auth.User, id int64) error {
	if id == viewer.ID {
		return ErrOwnAccount
	}
	if _, err := s.User(ctx, id); err != nil {
		return err
	}
	if err := s.inUse(ctx, id); err != nil {
		return err
	}
	return s.changed(s.Store.PurgeUser(ctx, id))
}

// inUse reports, as an InUseError, what the user whose id is id made that
// names them, and nil when nothing does.
func (s Service) inUse(ctx context.Context, id int64) error {
	firmware, err := s.Store.Firmwares(ctx)
	if err != nil {
		return err
	}
	var versions []string
	for _, f := range firmware {
		if f.UploadedBy == id {
			versions = append(versions, f.Version)
		}
	}
	overrides, err := s.Store.UserOverrides(ctx, id)
	if err != nil {
		return err
	}
	if len(versions) == 0 && len(overrides) == 0 {
		return nil
	}
	return InUseError{Firmware: versions, Overrides: overrides}
}
