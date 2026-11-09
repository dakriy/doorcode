// Package auth signs people in and out and decides what they may do. Local
// accounts sign in with an email and password; a signed-in browser holds a
// session token whose hash the store keeps.
package auth

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"slices"
	"strings"
	"time"

	"github.com/dakriy/doorcode/internal/connector"
	"github.com/dakriy/doorcode/internal/settings"
)

// The roles a group may carry. A user holds a role when one of their groups
// carries it or carries RoleAdmin, which may do everything.
const (
	RoleAdmin           = "admin"
	RoleManageUsers     = "manage-users"
	RoleManageDoors     = "manage-doors"
	RoleManageGroups    = "manage-groups"
	RoleManageSchedules = "manage-schedules"
	RoleManageFirmware  = "manage-firmware"
	RoleViewLogs        = "view-logs"
	RoleDoorCommander   = "door-commander"
)

// Roles is every role, in the order pages list them.
var Roles = []string{
	RoleAdmin, RoleManageUsers, RoleManageDoors, RoleManageGroups,
	RoleManageSchedules, RoleManageFirmware, RoleViewLogs, RoleDoorCommander,
}

// RoleDescriptions says in one line what each role lets its holders do.
var RoleDescriptions = map[string]string{
	RoleAdmin:           "Everything, including the site settings and every other role.",
	RoleManageUsers:     "Add, import, and change users and their codes, and answer access requests.",
	RoleManageDoors:     "Add and change doors, zones, and overrides, enroll controllers, and open doors remotely.",
	RoleManageGroups:    "Add and change groups, their members, and the access rules that let them in.",
	RoleManageSchedules: "Add and change the schedules access rules and open mode follow.",
	RoleManageFirmware:  "Upload controller firmware and roll it out to doors.",
	RoleViewLogs:        "Read every door's events.",
	RoleDoorCommander:   "Get in at any door at any time, even while it is locked, and open doors remotely.",
}

// AdministratorsSlug is the slug of the group the schema seeds with the
// admin role. The first administrator joins it.
const AdministratorsSlug = "administrators"

// sessionHours is how long a sign-in lasts.
const sessionHours = 12

// SessionLifetime is how long a sign-in lasts.
func SessionLifetime() time.Duration {
	return sessionHours * time.Hour
}

const (
	sessionTokenBytes  = 32
	adminPasswordBytes = 18
	adminDisplayName   = "Administrator"
)

// ErrBadCredentials reports an email and password that do not match an
// account that may sign in with a password.
var ErrBadCredentials = errors.New("email and password do not match")

// ErrExpired reports a correct password for an account that has expired.
var ErrExpired = errors.New("account expired")

var tokenEncoding = base64.RawURLEncoding

// User is a signed-in person.
type User struct {
	ID          int64
	Email       string
	DisplayName string
	// groups are the groups u is a member of; their roles are u's.
	groups []connector.UserGroup
}

// Has reports whether u holds role: whether one of u's groups carries it or
// carries the admin role. It is the one place that decides.
func (u User) Has(role string) bool {
	for _, g := range u.groups {
		if slices.Contains(g.Roles, role) || slices.Contains(g.Roles, RoleAdmin) {
			return true
		}
	}
	return false
}

// Without is u as it would be if u were not a member of the group whose id
// is groupID.
func (u User) Without(groupID int64) User {
	u.groups = slices.DeleteFunc(slices.Clone(u.groups), func(g connector.UserGroup) bool { return g.ID == groupID })
	return u
}

// WithGroupRoles is u as it would be if the group whose id is groupID
// carried roles instead of what it carries.
func (u User) WithGroupRoles(groupID int64, roles []string) User {
	groups := make([]connector.UserGroup, 0, len(u.groups))
	for _, g := range u.groups {
		if g.ID == groupID {
			g.Roles = roles
		}
		groups = append(groups, g)
	}
	u.groups = groups
	return u
}

// Store is the part of the store this package uses.
type Store interface {
	connector.SettingsStore
	connector.UsersStore
	connector.SessionsStore
	connector.TokensStore
}

// Service signs people in and out.
type Service struct {
	Store    Store
	Settings settings.Service
	Clock    connector.Clock
	Entropy  connector.Entropy
	Log      connector.Log
}

// CreateFirstAdmin creates an administrator with a generated password when
// no user exists yet, and logs the email and password so the operator can
// sign in and change it.
func (s Service) CreateFirstAdmin(ctx context.Context) error {
	current, err := s.Settings.Current(ctx)
	if err != nil {
		return err
	}
	password := tokenEncoding.EncodeToString(s.Entropy.Bytes(adminPasswordBytes))
	hash := hashPassword(password, s.Entropy.Bytes(saltBytes), current.PasswordHashMemoryKiB)
	admin := connector.StoredUser{
		Email:        normalizeEmail(current.AdminEmail),
		DisplayName:  adminDisplayName,
		PasswordHash: &hash,
		CreatedAt:    s.Clock.Now(),
	}
	created, err := s.Store.CreateFirstUser(ctx, admin, AdministratorsSlug)
	if err != nil {
		return err
	}
	if created {
		s.Log.Info("created the first administrator; sign in with this password and change it",
			connector.Field{Key: "email", Value: admin.Email},
			connector.Field{Key: "password", Value: password})
	}
	return nil
}

// SignIn checks email and password and starts a session, returning the token
// the browser keeps.
func (s Service) SignIn(ctx context.Context, email, password string) (string, error) {
	u, found, err := s.Store.UserByEmail(ctx, normalizeEmail(email))
	if err != nil {
		return "", err
	}
	if !found || u.DeletedAt != nil || u.PasswordHash == nil {
		return "", s.refuse(ctx, password)
	}
	if !passwordMatches(*u.PasswordHash, password) {
		return "", ErrBadCredentials
	}
	if Reached(u.ExpiresAt, s.Clock.Now()) {
		return "", ErrExpired
	}
	return s.StartSession(ctx, u.ID, false)
}

// StartSession starts a session for the user whose id is userID and
// returns the token the browser keeps. viaIdentity says the person signed
// in at the identity provider.
func (s Service) StartSession(ctx context.Context, userID int64, viaIdentity bool) (string, error) {
	now := s.Clock.Now()
	token := tokenEncoding.EncodeToString(s.Entropy.Bytes(sessionTokenBytes))
	session := connector.StoredSession{
		TokenHash:   tokenHash(token),
		UserID:      userID,
		CreatedAt:   now,
		ExpiresAt:   now.Add(SessionLifetime()),
		ViaIdentity: viaIdentity,
	}
	if err := s.Store.CreateSession(ctx, session); err != nil {
		return "", err
	}
	return token, nil
}

// refuse spends the time checking a password takes, so the answer does not
// reveal whether the email belongs to an account, and refuses.
func (s Service) refuse(ctx context.Context, password string) error {
	current, err := s.Settings.Current(ctx)
	if err != nil {
		return err
	}
	hashPassword(password, make([]byte, saltBytes), current.PasswordHashMemoryKiB)
	return ErrBadCredentials
}

// Authenticate returns the user a session token belongs to, and whether the
// session and the user are still good.
func (s Service) Authenticate(ctx context.Context, token string) (User, bool, error) {
	session, u, found, err := s.Store.SessionUser(ctx, tokenHash(token))
	if err != nil {
		return User{}, false, err
	}
	return s.admit(u, found, &session.ExpiresAt)
}

// admit returns u as a signed-in user when the credential that names u was
// found, has not ended by now, and u's account has not ended either. A nil
// ends never ends.
func (s Service) admit(u connector.StoredUser, found bool, ends *time.Time) (User, bool, error) {
	now := s.Clock.Now()
	credentialEnded := !found || Reached(ends, now)
	if credentialEnded || AccountEnded(u, now) {
		return User{}, false, nil
	}
	return UserOf(u), true, nil
}

// UserOf is u as a person holding the roles of u's groups.
func UserOf(u connector.StoredUser) User {
	return User{ID: u.ID, Email: u.Email, DisplayName: u.DisplayName, groups: u.Groups}
}

// SignOut ends the session token belongs to, and reports whether it began
// with a sign-in at the identity provider.
func (s Service) SignOut(ctx context.Context, token string) (bool, error) {
	hash := tokenHash(token)
	session, _, _, err := s.Store.SessionUser(ctx, hash)
	if err != nil {
		return false, err
	}
	return session.ViaIdentity, s.Store.DeleteSession(ctx, hash)
}

// PurgeSessions removes the sessions that have expired, which no longer
// sign anyone in.
func (s Service) PurgeSessions(ctx context.Context) error {
	_, err := s.Store.DeleteExpiredSessions(ctx, s.Clock.Now())
	return err
}

// AccountEnded reports whether u's account is deleted or has expired by
// now. Such a user is refused everywhere.
func AccountEnded(u connector.StoredUser, now time.Time) bool {
	return u.DeletedAt != nil || Reached(u.ExpiresAt, now)
}

// Reached reports whether now is at or past at, the time an account, a
// session, or a token ends; a nil at is never reached. It is the one rule
// for when an expiry takes effect.
func Reached(at *time.Time, now time.Time) bool {
	return at != nil && now.Compare(*at) >= 0
}

func tokenHash(token string) []byte {
	sum := sha256.Sum256([]byte(token))
	return sum[:]
}

func normalizeEmail(email string) string {
	return strings.ToLower(strings.TrimSpace(email))
}
