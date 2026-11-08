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

// RoleAdmin is the role that may do everything.
const RoleAdmin = "admin"

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
	roles       []string
}

// Has reports whether u holds role. It is the one place that decides.
func (u User) Has(role string) bool {
	return slices.Contains(u.roles, role)
}

// Store is the part of the store this package uses.
type Store interface {
	connector.UsersStore
	connector.SessionsStore
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
		Roles:        []string{RoleAdmin},
		CreatedAt:    s.Clock.Now(),
	}
	created, err := s.Store.CreateFirstUser(ctx, admin)
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
	now := s.Clock.Now()
	if expired(u, now) {
		return "", ErrExpired
	}
	token := tokenEncoding.EncodeToString(s.Entropy.Bytes(sessionTokenBytes))
	session := connector.StoredSession{
		TokenHash: tokenHash(token),
		UserID:    u.ID,
		CreatedAt: now,
		ExpiresAt: now.Add(SessionLifetime()),
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
	now := s.Clock.Now()
	sessionEnded := !found || now.Compare(session.ExpiresAt) >= 0
	accountEnded := u.DeletedAt != nil || expired(u, now)
	if sessionEnded || accountEnded {
		return User{}, false, nil
	}
	return User{ID: u.ID, Email: u.Email, DisplayName: u.DisplayName, roles: u.Roles}, true, nil
}

// SignOut ends the session token belongs to.
func (s Service) SignOut(ctx context.Context, token string) error {
	return s.Store.DeleteSession(ctx, tokenHash(token))
}

// expired reports whether u's account has expired by now.
func expired(u connector.StoredUser, now time.Time) bool {
	return u.ExpiresAt != nil && now.Compare(*u.ExpiresAt) >= 0
}

func tokenHash(token string) []byte {
	sum := sha256.Sum256([]byte(token))
	return sum[:]
}

func normalizeEmail(email string) string {
	return strings.ToLower(strings.TrimSpace(email))
}
