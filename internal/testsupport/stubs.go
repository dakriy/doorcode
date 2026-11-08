// Package testsupport holds the hand-written connector stubs, the stub
// factory, and the helpers the entrypoint suite drives the application with.
package testsupport

import (
	"context"
	"errors"
	"maps"
	"time"

	"github.com/dakriy/doorcode/internal/connector"
)

// ErrUnavailable is what a failing stub connector reports.
var ErrUnavailable = errors.New("stub: unavailable")

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
	migrateErr error
	err        error
}

// NewStore returns an empty, unmigrated stub store.
func NewStore() *Store {
	return &Store{settings: map[string]connector.StoredSetting{}}
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
	if err := s.usable(); err != nil {
		return nil, err
	}
	return maps.Clone(s.settings), nil
}

// PutSetting stores a setting value.
func (s *Store) PutSetting(_ context.Context, key, value string, at time.Time) error {
	if err := s.usable(); err != nil {
		return err
	}
	s.settings[key] = connector.StoredSetting{Value: value, UpdatedAt: at}
	return nil
}

func (s *Store) usable() error {
	if s.err != nil {
		return s.err
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

// FailMigration makes Migrate report ErrUnavailable.
func (s *Store) FailMigration() {
	s.migrateErr = ErrUnavailable
}

// Fail makes every aggregate method report ErrUnavailable.
func (s *Store) Fail() {
	s.err = ErrUnavailable
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

// NewFactory returns stubs with an empty environment and an empty store.
func NewFactory() *Factory {
	return &Factory{
		ClockStub:   &Clock{now: mustTime(DefaultNow)},
		EntropyStub: &Entropy{},
		EnvStub:     Environment{},
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
