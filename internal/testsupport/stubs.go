// Package testsupport holds the hand-written connector stubs, the stub
// factory, and the helpers the entrypoint suite drives the application with.
package testsupport

import (
	"context"
	"errors"
	"maps"
	"slices"
	"time"

	"github.com/dakriy/doorcode/internal/connector"
)

// ErrUnavailable is what a failing stub connector reports.
var ErrUnavailable = errors.New("stub: unavailable")

// ErrNoSuchUser is what the stub store reports for a session naming a user
// it does not hold, as a foreign key would.
var ErrNoSuchUser = errors.New("stub: no such user")

// ErrDuplicate is what the stub store reports for a key it already holds, as
// a unique constraint would.
var ErrDuplicate = errors.New("stub: duplicate key")

// ErrNotMigrated is what the stub store reports when used before Migrate,
// as a real database without its tables would.
var ErrNotMigrated = errors.New("stub: schema not migrated")

// Clock is the stub connector.Clock: it reports a fixed time until Set.
type Clock struct {
	now time.Time
}

// Now returns the stub's current time.
func (c *Clock) Now() time.Time {
	return c.now
}

// Set moves the clock to an RFC 3339 time.
func (c *Clock) Set(rfc3339 string) {
	c.now = mustTime(rfc3339)
}

// Entropy is the stub connector.Entropy. It is predictable: it hands out
// consecutive byte values starting at 0x00, so the first 8-byte request is
// 0001020304050607 in hex and the next 08090a0b0c0d0e0f.
type Entropy struct {
	next byte
}

// Bytes returns the next n bytes of the sequence.
func (e *Entropy) Bytes(n int) []byte {
	out := make([]byte, n)
	for i := range out {
		out[i] = e.next
		e.next++
	}
	return out
}

// Environment is the stub connector.Environment: a map of variables.
type Environment map[string]string

// Lookup returns the variable's value and whether it is set.
func (e Environment) Lookup(name string) (string, bool) {
	value, ok := e[name]
	return value, ok
}

// LogEntry is one line the stub log recorded.
type LogEntry struct {
	Level   string
	Message string
	Fields  map[string]string
}

// Log is the stub connector.Log: it records every line.
type Log struct {
	Entries []LogEntry
}

// Info records an info line.
func (l *Log) Info(msg string, fields ...connector.Field) {
	l.Entries = append(l.Entries, entry("info", msg, fields))
}

// Error records an error line.
func (l *Log) Error(msg string, fields ...connector.Field) {
	l.Entries = append(l.Entries, entry("error", msg, fields))
}

func entry(level, msg string, fields []connector.Field) LogEntry {
	var byKey map[string]string
	if len(fields) > 0 {
		byKey = map[string]string{}
	}
	for _, f := range fields {
		byKey[f.Key] = f.Value
	}
	return LogEntry{Level: level, Message: msg, Fields: byKey}
}

// Store is the stub connector.Store, held in memory.
type Store struct {
	migrated   bool
	settings   map[string]connector.StoredSetting
	users      []connector.StoredUser
	sessions   map[string]connector.StoredSession
	migrateErr error
	failing    map[aggregate]bool
}

type aggregate int

const (
	settingsAggregate aggregate = iota
	usersAggregate
	sessionsAggregate
)

// NewStore returns an empty, unmigrated stub store.
func NewStore() *Store {
	return &Store{
		settings: map[string]connector.StoredSetting{},
		sessions: map[string]connector.StoredSession{},
		failing:  map[aggregate]bool{},
	}
}

// Migrate marks the schema as present.
func (s *Store) Migrate(context.Context) error {
	if s.migrateErr != nil {
		return s.migrateErr
	}
	s.migrated = true
	return nil
}

// Settings returns a copy of the stored settings.
func (s *Store) Settings(context.Context) (map[string]connector.StoredSetting, error) {
	if err := s.usable(settingsAggregate); err != nil {
		return nil, err
	}
	return maps.Clone(s.settings), nil
}

// PutSetting stores a setting value.
func (s *Store) PutSetting(_ context.Context, key, value string, at time.Time) error {
	if err := s.usable(settingsAggregate); err != nil {
		return err
	}
	s.settings[key] = connector.StoredSetting{Value: value, UpdatedAt: at}
	return nil
}

// CreateFirstUser stores u with the next id when no user exists.
func (s *Store) CreateFirstUser(_ context.Context, u connector.StoredUser) (bool, error) {
	if err := s.usable(usersAggregate); err != nil {
		return false, err
	}
	if len(s.users) > 0 {
		return false, nil
	}
	u.ID = 1
	u.Roles = slices.Clone(u.Roles)
	s.users = append(s.users, u)
	return true, nil
}

// UserByEmail returns the user stored under email.
func (s *Store) UserByEmail(_ context.Context, email string) (connector.StoredUser, bool, error) {
	if err := s.usable(usersAggregate); err != nil {
		return connector.StoredUser{}, false, err
	}
	u := s.findUser(email)
	if u == nil {
		return connector.StoredUser{}, false, nil
	}
	return *u, true, nil
}

// CreateSession stores a session.
func (s *Store) CreateSession(_ context.Context, session connector.StoredSession) error {
	if err := s.usable(sessionsAggregate); err != nil {
		return err
	}
	if s.userByID(session.UserID) == nil {
		return ErrNoSuchUser
	}
	if _, taken := s.sessions[string(session.TokenHash)]; taken {
		return ErrDuplicate
	}
	s.sessions[string(session.TokenHash)] = session
	return nil
}

// SessionUser returns the session stored under tokenHash and its user.
func (s *Store) SessionUser(_ context.Context, tokenHash []byte) (connector.StoredSession, connector.StoredUser, bool, error) {
	if err := s.usable(sessionsAggregate); err != nil {
		return connector.StoredSession{}, connector.StoredUser{}, false, err
	}
	session, ok := s.sessions[string(tokenHash)]
	if !ok {
		return connector.StoredSession{}, connector.StoredUser{}, false, nil
	}
	return session, *s.userByID(session.UserID), true, nil
}

// DeleteSession removes the session stored under tokenHash.
func (s *Store) DeleteSession(_ context.Context, tokenHash []byte) error {
	if err := s.usable(sessionsAggregate); err != nil {
		return err
	}
	delete(s.sessions, string(tokenHash))
	return nil
}

func (s *Store) userByID(id int64) *connector.StoredUser {
	for i := range s.users {
		if s.users[i].ID == id {
			return &s.users[i]
		}
	}
	return nil
}

func (s *Store) findUser(email string) *connector.StoredUser {
	for i := range s.users {
		if s.users[i].Email == email {
			return &s.users[i]
		}
	}
	return nil
}

// userByEmail is the user a test arranges by email, which must exist.
func (s *Store) userByEmail(email string) *connector.StoredUser {
	u := s.findUser(email)
	if u == nil {
		panic("stub: no user with email " + email)
	}
	return u
}

func (s *Store) usable(a aggregate) error {
	if s.failing[a] {
		return ErrUnavailable
	}
	if !s.migrated {
		return ErrNotMigrated
	}
	return nil
}

// SeedSetting stores a setting value as if an administrator saved it at an
// RFC 3339 time, without needing the schema migrated.
func (s *Store) SeedSetting(key, value, at string) {
	s.settings[key] = connector.StoredSetting{Value: value, UpdatedAt: mustTime(at)}
}

// SeedUser stores a user with the next id, as an earlier run would have, and
// no local password.
func (s *Store) SeedUser(email, displayName string) {
	s.users = append(s.users, connector.StoredUser{
		ID: int64(len(s.users) + 1), Email: email, DisplayName: displayName,
		Roles: []string{}, CreatedAt: mustTime(DefaultNow),
	})
}

// ExpireUser makes the user with email expire at an RFC 3339 time.
func (s *Store) ExpireUser(email, at string) {
	expires := mustTime(at)
	s.userByEmail(email).ExpiresAt = &expires
}

// DeleteUser soft-deletes the user with email at an RFC 3339 time.
func (s *Store) DeleteUser(email, at string) {
	deleted := mustTime(at)
	s.userByEmail(email).DeletedAt = &deleted
}

// RevokeRoles takes every role from the user with email.
func (s *Store) RevokeRoles(email string) {
	s.userByEmail(email).Roles = []string{}
}

// RemovePassword takes the local password from the user with email.
func (s *Store) RemovePassword(email string) {
	s.userByEmail(email).PasswordHash = nil
}

// SetPasswordHash replaces the stored password hash of the user with email,
// as a damaged or foreign record would hold.
func (s *Store) SetPasswordHash(email, hash string) {
	s.userByEmail(email).PasswordHash = &hash
}

// FailMigration makes Migrate report ErrUnavailable.
func (s *Store) FailMigration() {
	s.migrateErr = ErrUnavailable
}

// Fail makes every aggregate method report ErrUnavailable.
func (s *Store) Fail() {
	s.FailSettings()
	s.FailUsers()
	s.FailSessions()
}

// FailSettings makes the settings methods report ErrUnavailable.
func (s *Store) FailSettings() {
	s.failing[settingsAggregate] = true
}

// FailUsers makes the users methods report ErrUnavailable.
func (s *Store) FailUsers() {
	s.failing[usersAggregate] = true
}

// FailSessions makes the session methods report ErrUnavailable.
func (s *Store) FailSessions() {
	s.failing[sessionsAggregate] = true
}

// Factory is the stub connector.Factory. Tests arrange and inspect the
// stubs through its fields.
type Factory struct {
	ClockStub   *Clock
	EntropyStub *Entropy
	EnvStub     Environment
	LogStub     *Log
	StoreStub   *Store
}

// DefaultNow is the stub clock's time until a test sets another.
const DefaultNow = "2026-01-05T09:00:00Z"

// FastHashMemory is the password hashing memory cost, in KiB, the stub
// environment sets so the suite hashes passwords quickly. It is the lowest
// the setting accepts.
const FastHashMemory = "8"

// NewFactory returns stubs with an empty store and an environment that sets
// only DOORCODE_PASSWORD_HASH_MEMORY_KIB, to FastHashMemory.
func NewFactory() *Factory {
	return &Factory{
		ClockStub:   &Clock{now: mustTime(DefaultNow)},
		EntropyStub: &Entropy{},
		EnvStub:     Environment{"DOORCODE_PASSWORD_HASH_MEMORY_KIB": FastHashMemory},
		LogStub:     &Log{},
		StoreStub:   NewStore(),
	}
}

// Clock returns the stub clock.
func (f *Factory) Clock() connector.Clock { return f.ClockStub }

// Entropy returns the stub entropy source.
func (f *Factory) Entropy() connector.Entropy { return f.EntropyStub }

// Environment returns the stub environment.
func (f *Factory) Environment() connector.Environment { return f.EnvStub }

// Log returns the stub log.
func (f *Factory) Log() connector.Log { return f.LogStub }

// Store returns the stub store.
func (f *Factory) Store() connector.Store { return f.StoreStub }

func mustTime(rfc3339 string) time.Time {
	t, err := time.Parse(time.RFC3339, rfc3339)
	if err != nil {
		panic(err)
	}
	return t
}

// ErrorText is err's message, or "<nil>" when there is no error, so a test
// can compare an error that may be missing.
func ErrorText(err error) string {
	if err == nil {
		return "<nil>"
	}
	return err.Error()
}
