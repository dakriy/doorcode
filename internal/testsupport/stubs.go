// Package testsupport holds the hand-written connector stubs, the stub
// factory, and the helpers the entrypoint suite drives the application with.
package testsupport

import (
	"bytes"
	"context"
	"errors"
	"maps"
	"slices"
	"sync"
	"time"

	// The embedded tz database, so the stub zones load on any host.
	_ "time/tzdata"

	"github.com/dakriy/doorcode/internal/connector"
)

// ErrUnavailable is what a failing stub connector reports.
var ErrUnavailable = errors.New("stub: unavailable")

// ErrNoSuchUser is what the stub store reports for a session naming a user
// it does not hold, as a foreign key would.
var ErrNoSuchUser = errors.New("stub: no such user")

// ErrNoSuchGroup is what the stub store reports for a membership naming a
// group it does not hold, as a foreign key would.
var ErrNoSuchGroup = errors.New("stub: no such group")

// ErrDuplicate is what the stub store reports for a key it already holds, as
// a unique constraint would.
var ErrDuplicate = errors.New("stub: duplicate key")

// ErrNull is what the stub store reports for a list it is given as nil,
// which the database would hold as NULL and refuse.
var ErrNull = errors.New("stub: null value")

// ErrCheck is what the stub store reports for a value a check constraint
// would refuse.
var ErrCheck = errors.New("stub: check constraint")

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

// Alarm returns a channel that has received when the stub time is at or
// past at, and one that never receives otherwise: the stub time moves only
// when a test sets it, which it does between runs.
func (c *Clock) Alarm(at time.Time) <-chan time.Time {
	if c.now.Before(at) {
		return nil
	}
	ch := make(chan time.Time, 1)
	ch <- c.now
	return ch
}

// Set moves the clock to an RFC 3339 time.
func (c *Clock) Set(rfc3339 string) {
	c.now = mustTime(rfc3339)
}

// Entropy is the stub connector.Entropy. It is predictable: it hands out
// consecutive byte values starting at 0x00, so the first 8-byte request is
// 0001020304050607 in hex and the next 08090a0b0c0d0e0f.
type Entropy struct {
	mu   sync.Mutex
	next byte
}

// Bytes returns the next n bytes of the sequence.
func (e *Entropy) Bytes(n int) []byte {
	e.mu.Lock()
	defer e.mu.Unlock()
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

// TimeZones is the stub connector.TimeZones. Zones are fixed data rather
// than state, so it loads them as the real one does, from the host's tz
// database or Go's embedded copy, and lists a few of them.
type TimeZones struct{}

// listedZones are the zones the stub lists, in ascending byte order.
var listedZones = []string{"America/New_York", "Asia/Tokyo", "Europe/Berlin", "UTC"}

// Zone loads the zone named name.
func (TimeZones) Zone(name string) (*time.Location, error) {
	return time.LoadLocation(name)
}

// List returns listedZones.
func (TimeZones) List() []string {
	return slices.Clone(listedZones)
}

// LogEntry is one line the stub log recorded.
type LogEntry struct {
	Level   string
	Message string
	Fields  map[string]string
}

// Log is the stub connector.Log: it records every line. A test reads
// Entries once the application is done.
type Log struct {
	mu      sync.Mutex
	Entries []LogEntry
}

// Info records an info line.
func (l *Log) Info(msg string, fields ...connector.Field) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.Entries = append(l.Entries, entry("info", msg, fields))
}

// Error records an error line.
func (l *Log) Error(msg string, fields ...connector.Field) {
	l.mu.Lock()
	defer l.mu.Unlock()
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

// Store is the stub connector.Store, held in memory. Its connector
// methods are safe for concurrent use, as a database is; a test arranges
// it before the application runs and reads it after.
type Store struct {
	mu           sync.Mutex
	holds        map[int64]chan struct{}
	migrated     bool
	settings     map[string]connector.StoredSetting
	users        []connector.StoredUser
	groups       []connector.StoredGroup
	members      []membership
	nextGroup    int64
	codeParams   string
	images       map[connector.ImageKind]connector.StoredImage
	sessions     map[string]connector.StoredSession
	tokens       []connector.StoredToken
	nextToken    int64
	schedules    []connector.StoredSchedule
	nextSched    int64
	doors        []connector.StoredDoor
	nextDoor     int64
	doorsCalls   int
	pictures     []storedPicture
	nextPicture  int64
	zones        []connector.StoredZone
	nextZone     int64
	zoneDoors    []zoneDoor
	rules        []connector.StoredRule
	nextRule     int64
	overrides    []connector.StoredOverride
	nextOverride int64
	credentials  []connector.StoredCredential
	events       []connector.StoredEvent
	samples      []connector.StoredSample
	rollups      []connector.StoredRollup
	firmware     []storedFirmware
	nextFirmware int64
	updates      []connector.StoredUpdate
	requests     []connector.StoredRequest
	nextRequest  int64
	migrateErr   error
	pingErr      error
	failing      map[aggregate]bool
}

type aggregate int

const (
	settingsAggregate aggregate = iota
	usersAggregate
	groupsAggregate
	settingWrites
	userWrites
	groupWrites
	sessionsAggregate
	tokensAggregate
	schedulesAggregate
	scheduleWrites
	doorsAggregate
	doorWrites
	zonesAggregate
	zoneWrites
	primaryPictureWrites
	signInImageReads
	logoReads
	faviconReads
	rulesAggregate
	ruleWrites
	overridesAggregate
	overrideWrites
	credentialsAggregate
	credentialWrites
	controllerWrites
	controllerReads
	sentConfigWrites
	eventsAggregate
	eventWrites
	eventReads
	alarmWrites
	telemetryAggregate
	telemetryWrites
	sampleReads
	firmwareAggregate
	updatesAggregate
	firmwareWrites
	requestsAggregate
	requestWrites
	emailLookups
	doorLookups
)

// NewStore returns an empty, unmigrated stub store.
func NewStore() *Store {
	return &Store{
		settings: map[string]connector.StoredSetting{},
		images:   map[connector.ImageKind]connector.StoredImage{},
		sessions: map[string]connector.StoredSession{},
		failing:  map[aggregate]bool{},
		holds:    map[int64]chan struct{}{},
	}
}

// AdministratorsGroup is the group the schema migration seeds, carrying the
// admin role.
var AdministratorsGroup = connector.StoredGroup{
	ID: 1, Slug: "administrators", Name: "Administrators", Description: "Members may do everything.",
	Roles: []string{"admin"}, PreassignedUserIDs: []string{},
}

// Ping reports ErrUnavailable once FailPing was called.
func (s *Store) Ping(context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.pingErr
}

// Migrate marks the schema as present and, the first time, seeds the
// administrators group and the Always schedule as the real migrations do.
func (s *Store) Migrate(context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.migrateErr != nil {
		return s.migrateErr
	}
	if !s.migrated {
		s.groups = append(s.groups, cloneGroup(AdministratorsGroup))
		s.nextGroup = AdministratorsGroup.ID
		s.schedules = append(s.schedules, cloneSchedule(AlwaysSchedule))
		s.nextSched = AlwaysSchedule.ID
	}
	s.migrated = true
	return nil
}

// Settings returns a copy of the stored settings.
func (s *Store) Settings(context.Context) (map[string]connector.StoredSetting, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.usable(settingsAggregate); err != nil {
		return nil, err
	}
	return maps.Clone(s.settings), nil
}

// PutSetting stores a setting value.
func (s *Store) PutSetting(_ context.Context, key, value string, at time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.writable(settingsAggregate, settingWrites); err != nil {
		return err
	}
	s.settings[key] = connector.StoredSetting{Value: value, UpdatedAt: at}
	return nil
}

// CodeHashing stores candidate unless parameters are stored, and returns
// the stored ones.
func (s *Store) CodeHashing(_ context.Context, candidate string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.writable(settingsAggregate, settingWrites); err != nil {
		return "", err
	}
	if s.codeParams == "" {
		s.codeParams = candidate
	}
	return s.codeParams, nil
}

// imageReads is the aggregate whose failure fails reading each site
// picture.
var imageReads = map[connector.ImageKind]aggregate{
	connector.SignInImage:  signInImageReads,
	connector.LogoImage:    logoReads,
	connector.FaviconImage: faviconReads,
}

// SiteImage returns a copy of the stored site picture of kind.
func (s *Store) SiteImage(_ context.Context, kind connector.ImageKind) (connector.StoredImage, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.writable(settingsAggregate, imageReads[kind]); err != nil {
		return connector.StoredImage{}, false, err
	}
	image, held := s.images[kind]
	image.Data = bytes.Clone(image.Data)
	return image, held, nil
}

// SiteImageStoredAt returns when the site picture of kind was stored.
func (s *Store) SiteImageStoredAt(_ context.Context, kind connector.ImageKind) (time.Time, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.writable(settingsAggregate, imageReads[kind]); err != nil {
		return time.Time{}, false, err
	}
	image, held := s.images[kind]
	return image.UpdatedAt, held, nil
}

// PutSiteImage stores a copy of image as the site picture of kind.
func (s *Store) PutSiteImage(_ context.Context, kind connector.ImageKind, image connector.StoredImage) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.writable(settingsAggregate, settingWrites); err != nil {
		return err
	}
	image.Data = bytes.Clone(image.Data)
	s.images[kind] = image
	return nil
}

// RemoveSiteImage removes the site picture of kind.
func (s *Store) RemoveSiteImage(_ context.Context, kind connector.ImageKind) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.writable(settingsAggregate, settingWrites); err != nil {
		return false, err
	}
	_, held := s.images[kind]
	delete(s.images, kind)
	return held, nil
}

// CreateSession stores a session.
func (s *Store) CreateSession(_ context.Context, session connector.StoredSession) error {
	s.mu.Lock()
	defer s.mu.Unlock()
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
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.usable(sessionsAggregate); err != nil {
		return connector.StoredSession{}, connector.StoredUser{}, false, err
	}
	session, ok := s.sessions[string(tokenHash)]
	if !ok {
		return connector.StoredSession{}, connector.StoredUser{}, false, nil
	}
	return session, s.withGroups(*s.userByID(session.UserID)), true, nil
}

// DeleteSession removes the session stored under tokenHash.
func (s *Store) DeleteSession(_ context.Context, tokenHash []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.usable(sessionsAggregate); err != nil {
		return err
	}
	delete(s.sessions, string(tokenHash))
	return nil
}

// DeleteExpiredSessions removes the sessions expiring at or before now.
func (s *Store) DeleteExpiredSessions(_ context.Context, now time.Time) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.usable(sessionsAggregate); err != nil {
		return 0, err
	}
	removed := 0
	for hash, session := range s.sessions {
		if !session.ExpiresAt.After(now) {
			delete(s.sessions, hash)
			removed++
		}
	}
	return removed, nil
}

// Sessions is how many sessions the store holds.
func (s *Store) Sessions() int {
	return len(s.sessions)
}

// CreateToken stores a token with the next id.
func (s *Store) CreateToken(_ context.Context, t connector.StoredToken) (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.usable(tokensAggregate); err != nil {
		return 0, err
	}
	if s.userByID(t.UserID) == nil {
		return 0, ErrNoSuchUser
	}
	for _, held := range s.tokens {
		if string(held.TokenHash) == string(t.TokenHash) {
			return 0, ErrDuplicate
		}
	}
	s.nextToken++
	t.ID = s.nextToken
	s.tokens = append(s.tokens, t)
	return t.ID, nil
}

// UserTokens returns the user's tokens in id order.
func (s *Store) UserTokens(_ context.Context, userID int64) ([]connector.StoredToken, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.usable(tokensAggregate); err != nil {
		return nil, err
	}
	var held []connector.StoredToken
	for _, t := range s.tokens {
		if t.UserID == userID {
			held = append(held, t)
		}
	}
	return held, nil
}

// TokenUser returns the token stored under tokenHash and its user.
func (s *Store) TokenUser(_ context.Context, tokenHash []byte) (connector.StoredToken, connector.StoredUser, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.usable(tokensAggregate); err != nil {
		return connector.StoredToken{}, connector.StoredUser{}, false, err
	}
	for _, t := range s.tokens {
		if string(t.TokenHash) == string(tokenHash) {
			return t, s.withGroups(*s.userByID(t.UserID)), true, nil
		}
	}
	return connector.StoredToken{}, connector.StoredUser{}, false, nil
}

// DeleteToken removes the user's token with id.
func (s *Store) DeleteToken(_ context.Context, userID, id int64) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.usable(tokensAggregate); err != nil {
		return false, err
	}
	for i, t := range s.tokens {
		if t.ID == id && t.UserID == userID {
			s.tokens = slices.Delete(slices.Clone(s.tokens), i, i+1)
			return true, nil
		}
	}
	return false, nil
}

// writable is usable for a method that changes aggregate a, whose writes
// fail when w does.
func (s *Store) writable(a, w aggregate) error {
	if s.failing[w] {
		return ErrUnavailable
	}
	return s.usable(a)
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
// no local password, without needing the schema migrated.
func (s *Store) SeedUser(email, displayName string) {
	s.users = append(s.users, connector.StoredUser{
		ID: int64(len(s.users) + 1), Email: email, DisplayName: displayName, CreatedAt: mustTime(DefaultNow),
	})
}

// SeedCodeHashing stores the site's door code hashing parameters, as a
// damaged or foreign record would hold them.
func (s *Store) SeedCodeHashing(parameters string) {
	s.codeParams = parameters
}

// StoredCodeHashing is the site's door code hashing parameters the store
// holds, empty until a code is first hashed.
func (s *Store) StoredCodeHashing() string {
	return s.codeParams
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

// RevokeRoles takes every role from the user with email by ending all their
// memberships.
func (s *Store) RevokeRoles(email string) {
	id := s.userByEmail(email).ID
	var kept []membership
	for _, m := range s.members {
		if m.user != id {
			kept = append(kept, m)
		}
	}
	s.members = kept
}

// RemovePassword takes the local password from the user with email.
func (s *Store) RemovePassword(email string) {
	s.userByEmail(email).PasswordHash = nil
}

// ReplacePasswordHash replaces the stored password hash of the user with
// email, as a damaged or foreign record would hold.
func (s *Store) ReplacePasswordHash(email, hash string) {
	s.userByEmail(email).PasswordHash = &hash
}

// FailMigration makes Migrate report ErrUnavailable.
func (s *Store) FailMigration() {
	s.migrateErr = ErrUnavailable
}

// FailPing makes Ping report ErrUnavailable, as a database that does not
// answer would.
func (s *Store) FailPing() {
	s.pingErr = ErrUnavailable
}

// FailSignInImageReads makes reading the sign-in page picture report
// ErrUnavailable, while the settings can still be read.
func (s *Store) FailSignInImageReads() {
	s.failing[signInImageReads] = true
}

// FailLogoReads makes reading the logo report ErrUnavailable, while the
// settings can still be read.
func (s *Store) FailLogoReads() {
	s.failing[logoReads] = true
}

// FailFaviconReads makes reading the favicon report ErrUnavailable, while
// the settings can still be read.
func (s *Store) FailFaviconReads() {
	s.failing[faviconReads] = true
}

// Fail makes every aggregate method report ErrUnavailable.
func (s *Store) Fail() {
	s.FailSettings()
	s.FailUsers()
	s.FailGroups()
	s.FailSessions()
	s.FailTokens()
	s.FailSchedules()
	s.FailDoors()
	s.FailZones()
	s.FailRules()
	s.FailOverrides()
	s.FailCredentials()
	s.FailEvents()
	s.FailTelemetry()
	s.FailFirmware()
	s.FailUpdates()
	s.FailRequests()
}

// Recover makes every method a Fail method made fail work again.
func (s *Store) Recover() {
	s.mu.Lock()
	defer s.mu.Unlock()
	clear(s.failing)
}

// FailSettings makes the settings methods report ErrUnavailable.
func (s *Store) FailSettings() {
	s.failing[settingsAggregate] = true
}

// FailUsers makes the users methods report ErrUnavailable.
func (s *Store) FailUsers() {
	s.failing[usersAggregate] = true
}

// FailGroups makes the group and membership methods report ErrUnavailable.
func (s *Store) FailGroups() {
	s.failing[groupsAggregate] = true
}

// FailSettingWrites makes the methods that change settings and site
// parameters report ErrUnavailable, while the ones that read them still
// work.
func (s *Store) FailSettingWrites() {
	s.failing[settingWrites] = true
}

// FailUserWrites makes the methods that change users report
// ErrUnavailable, while the ones that read them still work.
func (s *Store) FailUserWrites() {
	s.failing[userWrites] = true
}

// FailGroupWrites makes the methods that change groups and memberships
// report ErrUnavailable, while the ones that read them still work.
func (s *Store) FailGroupWrites() {
	s.failing[groupWrites] = true
}

// FailSessions makes the session methods report ErrUnavailable.
func (s *Store) FailSessions() {
	s.failing[sessionsAggregate] = true
}

// FailTokens makes the API token methods report ErrUnavailable.
func (s *Store) FailTokens() {
	s.failing[tokensAggregate] = true
}

// FailSchedules makes the schedule methods report ErrUnavailable.
func (s *Store) FailSchedules() {
	s.failing[schedulesAggregate] = true
}

// FailScheduleWrites makes the methods that change schedules report
// ErrUnavailable, while the ones that read them still work.
func (s *Store) FailScheduleWrites() {
	s.failing[scheduleWrites] = true
}

// Factory is the stub connector.Factory. Tests arrange and inspect the
// stubs through its fields.
type Factory struct {
	ClockStub    *Clock
	EntropyStub  *Entropy
	EnvStub      Environment
	ZonesStub    TimeZones
	LogStub      *Log
	StoreStub    *Store
	BrokerStub   *Broker
	IdentityStub *Identity
	ResolverStub *Resolver
}

// DefaultNow is the stub clock's time until a test sets another.
const DefaultNow = "2026-01-05T09:00:00Z"

// FastHashMemory is the password hashing memory cost, in KiB, the stub
// environment sets so the suite hashes passwords quickly. It is the lowest
// the setting accepts.
const FastHashMemory = "8"

// NewFactory returns stubs with an empty store and an environment that sets
// only DOORCODE_PASSWORD_HASH_MEMORY_KIB, to FastHashMemory, and
// DOORCODE_SECRET_KEY, to SecretKey.
func NewFactory() *Factory {
	return &Factory{
		ClockStub:    &Clock{now: mustTime(DefaultNow)},
		EntropyStub:  &Entropy{},
		EnvStub:      Environment{"DOORCODE_PASSWORD_HASH_MEMORY_KIB": FastHashMemory, "DOORCODE_SECRET_KEY": SecretKey},
		LogStub:      &Log{},
		StoreStub:    NewStore(),
		BrokerStub:   NewBroker(),
		IdentityStub: NewIdentity(),
		ResolverStub: &Resolver{},
	}
}

// Clock returns the stub clock.
func (f *Factory) Clock() connector.Clock { return f.ClockStub }

// Entropy returns the stub entropy source.
func (f *Factory) Entropy() connector.Entropy { return f.EntropyStub }

// Environment returns the stub environment.
func (f *Factory) Environment() connector.Environment { return f.EnvStub }

// TimeZones returns the stub time zones.
func (f *Factory) TimeZones() connector.TimeZones { return f.ZonesStub }

// Log returns the stub log.
func (f *Factory) Log() connector.Log { return f.LogStub }

// Store returns the stub store.
func (f *Factory) Store() connector.Store { return f.StoreStub }

// Broker returns the stub broker.
func (f *Factory) Broker() connector.Broker { return f.BrokerStub }

// Identity returns the stub identity.
func (f *Factory) Identity() connector.Identity { return f.IdentityStub }

// Resolver returns the stub resolver.
func (f *Factory) Resolver() connector.Resolver { return f.ResolverStub }

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
