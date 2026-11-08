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

// TimeZones looks time zones up in the tz database.
type TimeZones interface {
	// Zone returns the zone the tz database names name, such as
	// "Europe/Berlin", or an error when it has no such zone.
	Zone(name string) (*time.Location, error)
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

// SettingsStore holds the settings aggregate: values keyed by setting key,
// and the site-wide parameters the application fixes once.
type SettingsStore interface {
	// Settings returns every stored setting keyed by setting key.
	Settings(ctx context.Context) (map[string]StoredSetting, error)
	// PutSetting stores value under key, recording when it was written.
	PutSetting(ctx context.Context, key, value string, at time.Time) error
	// CodeHashing stores candidate as the site's door code hashing
	// parameters unless some are stored already, and returns the stored
	// ones.
	CodeHashing(ctx context.Context, candidate string) (string, error)
}

// StoredUser is a user as the store holds it. Nil pointers are absent
// values: no user id, no local password, no door code, no expiry, not
// deleted. Groups are the groups the user is a member of, ordered by name;
// the store fills them when it returns a user and ignores them when it
// stores one.
type StoredUser struct {
	ID           int64
	Email        string
	UserID       *string
	FirstName    string
	LastName     string
	DisplayName  string
	PasswordHash *string
	CodeHash     *string
	ExpiresAt    *time.Time
	DeletedAt    *time.Time
	CreatedAt    time.Time
	Groups       []UserGroup
}

// UserGroup is a group as one of its members sees it.
type UserGroup struct {
	ID    int64
	Slug  string
	Name  string
	Roles []string
}

// UserQuery selects a page of users. Search keeps the users whose email,
// user id when they have one, first name, last name, and display name,
// joined by single spaces, contain it ignoring case; an empty Search keeps
// every user. Deleted selects the
// deleted users instead of the others. The page skips Offset users and holds
// at most Limit.
type UserQuery struct {
	Search  string
	Deleted bool
	Offset  int
	Limit   int
}

// UsersStore holds the users aggregate. Users are never removed; a deleted
// user carries DeletedAt.
type UsersStore interface {
	// CreateFirstUser stores u, ignoring u.ID, as a member of the group
	// whose slug is groupSlug if there is one, only when no user exists at
	// all, deleted ones included. It reports whether it stored u.
	CreateFirstUser(ctx context.Context, u StoredUser, groupSlug string) (bool, error)
	// CreateUser stores u, ignoring u.ID, as a member of the groups whose
	// ids are groupIDs, and returns the id it was given.
	CreateUser(ctx context.Context, u StoredUser, groupIDs []int64) (int64, error)
	// User returns the user whose id is id, and whether there is one.
	User(ctx context.Context, id int64) (StoredUser, bool, error)
	// UserByEmail returns the user whose email is exactly email, and whether
	// there is one.
	UserByEmail(ctx context.Context, email string) (StoredUser, bool, error)
	// UserByUserID returns the user whose user id is exactly userID, and
	// whether there is one.
	UserByUserID(ctx context.Context, userID string) (StoredUser, bool, error)
	// UserByCodeHash returns the user whose door code hash is exactly hash,
	// and whether there is one.
	UserByCodeHash(ctx context.Context, hash string) (StoredUser, bool, error)
	// Users returns the page of users q selects, ordered by display name
	// compared byte by byte after lowercasing and then by id, and how many
	// users q selects on every page together.
	Users(ctx context.Context, q UserQuery) ([]StoredUser, int, error)
	// UpdateUser stores u's email, user id, names, and expiry over the user
	// whose id is u.ID, and reports whether there is one.
	UpdateUser(ctx context.Context, u StoredUser) (bool, error)
	// SetUserDeleted stores at as the deletion time of the user whose id is
	// id, nil restoring them, and reports whether there is one.
	SetUserDeleted(ctx context.Context, id int64, at *time.Time) (bool, error)
	// SetCodeHash stores hash as the door code hash of the user whose id is
	// id, nil removing the code, and reports whether there is one.
	SetCodeHash(ctx context.Context, id int64, hash *string) (bool, error)
}

// StoredGroup is a group as the store holds it. MemberCount is how many
// users are members; the store fills it when it returns a group and ignores
// it when it stores one.
type StoredGroup struct {
	ID                 int64
	Slug               string
	Name               string
	Description        string
	Roles              []string
	PreassignedUserIDs []string
	MemberCount        int
}

// GroupsStore holds the groups aggregate and its memberships.
type GroupsStore interface {
	// Groups returns every group, ordered by name compared byte by byte
	// after lowercasing and then by id.
	Groups(ctx context.Context) ([]StoredGroup, error)
	// Group returns the group whose id is id, and whether there is one.
	Group(ctx context.Context, id int64) (StoredGroup, bool, error)
	// GroupBySlug returns the group whose slug is exactly slug, and whether
	// there is one.
	GroupBySlug(ctx context.Context, slug string) (StoredGroup, bool, error)
	// CreateGroup stores g, ignoring g.ID, and returns the id it was given.
	CreateGroup(ctx context.Context, g StoredGroup) (int64, error)
	// UpdateGroup stores g over the group whose id is g.ID, and reports
	// whether there is one.
	UpdateGroup(ctx context.Context, g StoredGroup) (bool, error)
	// DeleteGroup removes the group whose id is id and its memberships, and
	// reports whether there was one.
	DeleteGroup(ctx context.Context, id int64) (bool, error)
	// GroupMembers returns the members of the group whose id is id, in the
	// order Users uses.
	GroupMembers(ctx context.Context, id int64) ([]StoredUser, error)
	// AddMember makes the user whose id is userID a member of the group
	// whose id is groupID; adding a member again changes nothing.
	AddMember(ctx context.Context, groupID, userID int64) error
	// RemoveMember ends the membership of the user whose id is userID in
	// the group whose id is groupID, and reports whether there was one.
	RemoveMember(ctx context.Context, groupID, userID int64) (bool, error)
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

// StoredWindow is one weekly window of a schedule. It opens on each of
// Days at Start minutes after midnight and closes End minutes after
// midnight, on the same day when End is after Start and on the next day
// otherwise.
type StoredWindow struct {
	Days  []time.Weekday
	Start int
	End   int
}

// StoredSchedule is a schedule as the store holds it. Dates are midnight
// UTC on the day they name. Nil StartsOn and EndsOn leave the schedule
// unbounded on that side. Windows are in the order they were stored;
// Excluded is in date order. Builtin marks the seeded Always schedule; the
// store sets it only when seeding and ignores it when it stores one.
type StoredSchedule struct {
	ID       int64
	Name     string
	StartsOn *time.Time
	EndsOn   *time.Time
	Windows  []StoredWindow
	Excluded []time.Time
	Builtin  bool
}

// SchedulesStore holds the schedules aggregate. Schedule names are unique
// ignoring case.
type SchedulesStore interface {
	// Schedules returns every schedule, ordered by name compared byte by
	// byte after lowercasing and then by id.
	Schedules(ctx context.Context) ([]StoredSchedule, error)
	// Schedule returns the schedule whose id is id, and whether there is
	// one.
	Schedule(ctx context.Context, id int64) (StoredSchedule, bool, error)
	// CreateSchedule stores s, ignoring s.ID, and returns the id it was
	// given.
	CreateSchedule(ctx context.Context, s StoredSchedule) (int64, error)
	// UpdateSchedule stores s over the schedule whose id is s.ID, replacing
	// its windows, and reports whether there is one.
	UpdateSchedule(ctx context.Context, s StoredSchedule) (bool, error)
	// DeleteSchedule removes the schedule whose id is id, and reports
	// whether there was one.
	DeleteSchedule(ctx context.Context, id int64) (bool, error)
}

// Store is the application's persistent state, one interface per aggregate.
type Store interface {
	// Migrate brings the schema up to date. It runs once at startup, before
	// any other method.
	Migrate(ctx context.Context) error
	SettingsStore
	UsersStore
	GroupsStore
	SessionsStore
	TokensStore
	SchedulesStore
}

// Factory constructs connectors. It has one method per connector.
type Factory interface {
	Clock() Clock
	Entropy() Entropy
	Environment() Environment
	TimeZones() TimeZones
	Log() Log
	Store() Store
}
