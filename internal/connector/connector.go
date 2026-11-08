// Package connector declares every interface through which application code
// reaches the outside world, and the factory that supplies them.
package connector

import (
	"context"
	"time"
)

// Clock reports the current time.
type Clock interface {
	Now() time.Time
}

// Entropy supplies unpredictable bytes.
type Entropy interface {
	Bytes(n int) []byte
}

// Environment looks up variables the process was started with.
type Environment interface {
	Lookup(name string) (value string, ok bool)
}

// Field is one key and value attached to a log line.
type Field struct {
	Key   string
	Value string
}

// Log writes operator-facing log lines.
type Log interface {
	Info(msg string, fields ...Field)
	Error(msg string, fields ...Field)
}

// StoredSetting is a setting value as the store holds it.
type StoredSetting struct {
	Value     string
	UpdatedAt time.Time
}

// SettingsStore holds the settings aggregate: values keyed by setting key.
type SettingsStore interface {
	// Settings returns every stored setting keyed by setting key.
	Settings(ctx context.Context) (map[string]StoredSetting, error)
	// PutSetting stores value under key, recording when it was written.
	PutSetting(ctx context.Context, key, value string, at time.Time) error
}

// StoredUser is a user as the store holds it. Nil pointers are absent
// values: no local password, no expiry, not deleted.
type StoredUser struct {
	ID           int64
	Email        string
	DisplayName  string
	PasswordHash *string
	Roles        []string
	ExpiresAt    *time.Time
	DeletedAt    *time.Time
	CreatedAt    time.Time
}

// UsersStore holds the users aggregate. Users are never removed; a deleted
// user carries DeletedAt.
type UsersStore interface {
	// CreateFirstUser stores u, ignoring u.ID, only when no user exists at
	// all, deleted ones included. It reports whether it stored u.
	CreateFirstUser(ctx context.Context, u StoredUser) (bool, error)
	// UserByEmail returns the user whose email is exactly email, and whether
	// there is one.
	UserByEmail(ctx context.Context, email string) (StoredUser, bool, error)
}

// StoredSession is a sign-in session as the store holds it. The store keeps
// only a hash of the token the browser holds.
type StoredSession struct {
	TokenHash []byte
	UserID    int64
	CreatedAt time.Time
	ExpiresAt time.Time
}

// SessionsStore holds the sessions aggregate.
type SessionsStore interface {
	// CreateSession stores s.
	CreateSession(ctx context.Context, s StoredSession) error
	// SessionUser returns the session whose token hash is tokenHash, the user
	// it belongs to, and whether there is one.
	SessionUser(ctx context.Context, tokenHash []byte) (StoredSession, StoredUser, bool, error)
	// DeleteSession removes the session whose token hash is tokenHash, if any.
	DeleteSession(ctx context.Context, tokenHash []byte) error
}

// StoredToken is a personal API token as the store holds it. The store keeps
// only a hash of the secret the owner holds. A nil ExpiresAt never expires.
type StoredToken struct {
	ID        int64
	UserID    int64
	Name      string
	TokenHash []byte
	CreatedAt time.Time
	ExpiresAt *time.Time
}

// TokensStore holds the personal API tokens aggregate. A revoked token is
// removed.
type TokensStore interface {
	// CreateToken stores t, ignoring t.ID, and returns the id it was given.
	CreateToken(ctx context.Context, t StoredToken) (int64, error)
	// UserTokens returns every token of the user whose id is userID, oldest
	// first.
	UserTokens(ctx context.Context, userID int64) ([]StoredToken, error)
	// TokenUser returns the token whose hash is tokenHash, the user it
	// belongs to, and whether there is one.
	TokenUser(ctx context.Context, tokenHash []byte) (StoredToken, StoredUser, bool, error)
	// DeleteToken removes the token with id that belongs to the user whose
	// id is userID, and reports whether there was one.
	DeleteToken(ctx context.Context, userID, id int64) (bool, error)
}

// Store is the application's persistent state, one interface per aggregate.
type Store interface {
	// Migrate brings the schema up to date. It runs once at startup, before
	// any other method.
	Migrate(ctx context.Context) error
	SettingsStore
	UsersStore
	SessionsStore
	TokensStore
}

// Factory constructs connectors. It has one method per connector.
type Factory interface {
	Clock() Clock
	Entropy() Entropy
	Environment() Environment
	Log() Log
	Store() Store
}
