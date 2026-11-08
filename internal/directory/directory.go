// Package directory keeps the people and groups the site knows: users with
// their profiles and door codes, and groups with their roles, members, and
// pre-assigned user ids. Who holds which role is decided by auth.User.Has;
// this package only changes what it reads.
package directory

import (
	"errors"
	"fmt"
	"regexp"

	"github.com/dakriy/doorcode/internal/auth"
	"github.com/dakriy/doorcode/internal/connector"
)

// Store is the part of the store this package uses.
type Store interface {
	connector.UsersStore
	connector.GroupsStore
}

// Service reads and changes users and groups.
type Service struct {
	Store Store
	Auth  auth.Service
	Clock connector.Clock
}

var (
	// ErrNoUser reports an id that names no user.
	ErrNoUser = errors.New("no user has this id")
	// ErrNoGroup reports an id that names no group.
	ErrNoGroup = errors.New("no group has this id")
	// ErrNotMember reports a user who is not a member of the group.
	ErrNotMember = errors.New("the user is not a member of this group")
	// ErrOwnAdmin reports a change that would take the admin role from the
	// person making it.
	ErrOwnAdmin = errors.New("you cannot remove your own admin role")
	// ErrOwnAccount reports a person deleting their own account.
	ErrOwnAccount = errors.New("you cannot delete your own account")
	// ErrNotHeld reports a change to a group carrying a role the person
	// making it does not hold.
	ErrNotHeld = errors.New("you can only change groups whose roles you hold")
)

const maxUserIDLength = 64

// userIDPattern is what a user id may look like.
var userIDPattern = regexp.MustCompile(fmt.Sprintf(`^[A-Za-z0-9._-]{1,%d}$`, maxUserIDLength))

// userIDHint says what a user id may look like.
var userIDHint = fmt.Sprintf("Use up to %d letters, digits, dots, dashes, or underscores.", maxUserIDLength)

// conflict is message when the value is taken by a holder, whose id is
// holderID, other than the user or group whose id is id.
func conflict(id, holderID int64, taken bool, message string) string {
	if taken && holderID != id {
		return message
	}
	return ""
}

// losesAdmin reports whether viewer holds the admin role and would not
// once the change that turns them into after is made.
func losesAdmin(viewer, after auth.User) bool {
	return viewer.Has(auth.RoleAdmin) && !after.Has(auth.RoleAdmin)
}

// mayChange reports whether viewer holds every role in roles, as changing a
// group that carries them needs.
func mayChange(viewer auth.User, roles []string) bool {
	for _, role := range roles {
		if !viewer.Has(role) {
			return false
		}
	}
	return true
}
