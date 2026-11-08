// Package connector declares every interface through which application code
// reaches the outside world, and the factory that supplies them.
package connector

import (
	"context"
	"time"
)

// Clock reports the current time and says when a time has come.
type Clock interface {
	Now() time.Time
	// Alarm returns a channel that receives once the clock reaches at, at
	// once when it has already.
	Alarm(at time.Time) <-chan time.Time
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
	// DeleteGroup removes the group whose id is id, its memberships, and its
	// access rules, and reports whether there was one.
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
	// DeleteExpiredSessions removes every session that expires at or
	// before now, and returns how many it removed.
	DeleteExpiredSessions(ctx context.Context, now time.Time) (int, error)
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
	// whether there was one. Doors stop being in open mode during it. It
	// refuses a schedule an access rule uses.
	DeleteSchedule(ctx context.Context, id int64) (bool, error)
}

// StoredController is the controller enrolled as a door: its hardware id,
// what it last reported of its firmware version and signal strength (in
// dBm), whether it is connected to the broker, and when the server last
// heard of it.
type StoredController struct {
	HardwareID      string
	FirmwareVersion string
	Signal          int
	Online          bool
	LastSeenAt      time.Time
}

// StoredDoor is a door as the store holds it. OpenScheduleIDs are the
// schedules during which the door is in open mode and ZoneIDs the zones it
// is in, each ascending. A nil PrimaryPictureID is a door without a primary
// picture, which the store sets only through SetPrimaryPicture and clears
// when that picture is deleted; it ignores the field when it stores a door.
// A nil DeletedAt is a door that is not deleted. A nil Controller is a door
// no controller has enrolled as; the store sets it only through
// SetController and ignores it when it stores a door. Config is the door's
// configuration overrides, each value keyed by the name of the field it
// overrides, never nil; the store sets it only through SetDoorConfig and
// ignores it when it stores a door.
type StoredDoor struct {
	ID               int64
	Name             string
	Location         string
	Notes            string
	Public           bool
	OpenScheduleIDs  []int64
	ZoneIDs          []int64
	PrimaryPictureID *int64
	DeletedAt        *time.Time
	CreatedAt        time.Time
	Controller       *StoredController
	Config           map[string]string
}

// DoorsStore holds the doors aggregate. Doors are never removed; a deleted
// door carries DeletedAt. Door names are unique ignoring case.
type DoorsStore interface {
	// Doors returns every door, deleted ones included, ordered by name
	// compared byte by byte after lowercasing and then by id.
	Doors(ctx context.Context) ([]StoredDoor, error)
	// Door returns the door whose id is id, and whether there is one.
	Door(ctx context.Context, id int64) (StoredDoor, bool, error)
	// CreateDoor stores d, ignoring d.ID, and returns the id it was given.
	CreateDoor(ctx context.Context, d StoredDoor) (int64, error)
	// UpdateDoor stores d's name, location, notes, public flag, open
	// schedules, and zones over the door whose id is d.ID, and reports
	// whether there is one.
	UpdateDoor(ctx context.Context, d StoredDoor) (bool, error)
	// SetDoorDeleted stores at as the deletion time of the door whose id is
	// id, nil restoring it, and reports whether there is one.
	SetDoorDeleted(ctx context.Context, id int64, at *time.Time) (bool, error)
	// SetController stores c as the controller of the door whose id is id,
	// nil detaching it, and reports whether there is such a door. Hardware
	// ids are unique: it refuses one another door's controller has.
	SetController(ctx context.Context, id int64, c *StoredController) (bool, error)
	// SetDoorConfig stores config as the configuration overrides of the
	// door whose id is id, replacing those it had, and reports whether
	// there is such a door.
	SetDoorConfig(ctx context.Context, id int64, config map[string]string) (bool, error)
}

// StoredCredential is one set of broker credentials issued to the
// controller of the door whose id is DoorID. The store keeps the username
// only; the broker holds the password. UsedAt is when the controller first
// connected with it, nil until it has; RevokedAt is when the broker stopped
// accepting it, nil while it is live.
type StoredCredential struct {
	ID        int64
	DoorID    int64
	Username  string
	IssuedAt  time.Time
	UsedAt    *time.Time
	RevokedAt *time.Time
}

// CredentialsStore holds the broker credentials issued to door
// controllers. Usernames are unique; credentials are never removed.
type CredentialsStore interface {
	// CreateCredential stores c, ignoring c.ID, and returns the id it was
	// given.
	CreateCredential(ctx context.Context, c StoredCredential) (int64, error)
	// DoorCredentials returns the credentials of the door whose id is
	// doorID, oldest first.
	DoorCredentials(ctx context.Context, doorID int64) ([]StoredCredential, error)
	// LiveCredentials returns every credential that is not revoked, ordered
	// by door id and then by id.
	LiveCredentials(ctx context.Context) ([]StoredCredential, error)
	// UseCredential stores at as when the credential whose id is id was
	// first used, and reports whether there is one.
	UseCredential(ctx context.Context, id int64, at time.Time) (bool, error)
	// RevokeCredential stores at as when the credential whose id is id was
	// revoked, and reports whether there is one.
	RevokeCredential(ctx context.Context, id int64, at time.Time) (bool, error)
}

// StoredPicture is a picture of a door as the store holds it, without its
// image data.
type StoredPicture struct {
	ID        int64
	DoorID    int64
	Width     int
	Height    int
	CreatedAt time.Time
}

// PicturesStore holds the pictures of doors: each an image and a thumbnail
// of it.
type PicturesStore interface {
	// AddPicture stores p, ignoring p.ID, with its image and thumbnail, and
	// returns the id it was given.
	AddPicture(ctx context.Context, p StoredPicture, image, thumbnail []byte) (int64, error)
	// Pictures returns the pictures of the door whose id is doorID, oldest
	// first.
	Pictures(ctx context.Context, doorID int64) ([]StoredPicture, error)
	// PictureImage returns the image of the picture whose id is id, of the
	// door whose id is doorID, and whether there is one.
	PictureImage(ctx context.Context, doorID, id int64) ([]byte, bool, error)
	// PictureThumbnail returns the thumbnail of the picture whose id is id,
	// of the door whose id is doorID, and whether there is one.
	PictureThumbnail(ctx context.Context, doorID, id int64) ([]byte, bool, error)
	// SetPrimaryPicture makes the picture whose id is id, which must be one
	// of its own, the primary picture of the door whose id is doorID, and
	// reports whether there is such a door.
	SetPrimaryPicture(ctx context.Context, doorID, id int64) (bool, error)
	// DeletePicture removes the picture whose id is id, of the door whose id
	// is doorID, and reports whether there was one.
	DeletePicture(ctx context.Context, doorID, id int64) (bool, error)
}

// StoredZone is a zone as the store holds it. DoorIDs are the doors in it,
// ascending.
type StoredZone struct {
	ID          int64
	Name        string
	Description string
	DoorIDs     []int64
}

// ZonesStore holds the zones aggregate. Zone names are unique ignoring
// case.
type ZonesStore interface {
	// Zones returns every zone, ordered by name compared byte by byte after
	// lowercasing and then by id.
	Zones(ctx context.Context) ([]StoredZone, error)
	// Zone returns the zone whose id is id, and whether there is one.
	Zone(ctx context.Context, id int64) (StoredZone, bool, error)
	// CreateZone stores z, ignoring z.ID, and returns the id it was given.
	CreateZone(ctx context.Context, z StoredZone) (int64, error)
	// UpdateZone stores z over the zone whose id is z.ID, replacing its
	// doors, and reports whether there is one.
	UpdateZone(ctx context.Context, z StoredZone) (bool, error)
	// DeleteZone removes the zone whose id is id, taking it off the access
	// rules that name it, and reports whether there was one.
	DeleteZone(ctx context.Context, id int64) (bool, error)
}

// StoredRule is an access rule as the store holds it: members of the group
// whose id is GroupID may enter, with their code, the doors DoorIDs names
// and the doors in the zones ZoneIDs names while the schedule whose id is
// ScheduleID is active. DoorIDs and ZoneIDs are ascending.
type StoredRule struct {
	ID         int64
	GroupID    int64
	ScheduleID int64
	DoorIDs    []int64
	ZoneIDs    []int64
}

// RulesStore holds the access rules aggregate. A rule goes when its group
// is deleted, and a deleted zone leaves the rules that named it; a schedule
// a rule uses cannot be deleted.
type RulesStore interface {
	// Rules returns every rule, ordered by id.
	Rules(ctx context.Context) ([]StoredRule, error)
	// Rule returns the rule whose id is id, and whether there is one.
	Rule(ctx context.Context, id int64) (StoredRule, bool, error)
	// CreateRule stores r, ignoring r.ID, and returns the id it was given.
	CreateRule(ctx context.Context, r StoredRule) (int64, error)
	// UpdateRule stores r over the rule whose id is r.ID, replacing its
	// doors and zones, and reports whether there is one.
	UpdateRule(ctx context.Context, r StoredRule) (bool, error)
	// DeleteRule removes the rule whose id is id, and reports whether there
	// was one.
	DeleteRule(ctx context.Context, id int64) (bool, error)
}

// StoredOverride is an override as the store holds it: from Start up to End
// the door whose id is DoorID is forced open or locked, as Kind ("open" or
// "locked") says. CreatedBy is the id of the user who created it;
// CreatorName is that user's display name, which the store fills when it
// returns an override and ignores when it stores one.
type StoredOverride struct {
	ID          int64
	DoorID      int64
	Kind        string
	Start       time.Time
	End         time.Time
	Reason      string
	CreatedBy   int64
	CreatorName string
	CreatedAt   time.Time
}

// OverridesStore holds the overrides aggregate.
type OverridesStore interface {
	// DoorOverrides returns the overrides of the door whose id is doorID
	// that end after endsAfter, ordered by start and then by id.
	DoorOverrides(ctx context.Context, doorID int64, endsAfter time.Time) ([]StoredOverride, error)
	// Override returns the override whose id is id, and whether there is
	// one.
	Override(ctx context.Context, id int64) (StoredOverride, bool, error)
	// CreateOverride stores o, ignoring o.ID, and returns the id it was
	// given.
	CreateOverride(ctx context.Context, o StoredOverride) (int64, error)
	// UpdateOverride stores o's kind, start, end, and reason over the
	// override whose id is o.ID, and reports whether there is one.
	UpdateOverride(ctx context.Context, o StoredOverride) (bool, error)
	// DeleteOverride removes the override whose id is id, and reports
	// whether there was one.
	DeleteOverride(ctx context.Context, id int64) (bool, error)
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
	DoorsStore
	PicturesStore
	ZonesStore
	RulesStore
	OverridesStore
	CredentialsStore
}

// Message is one message the broker carries: its topic, its payload, and
// whether the broker keeps it as the topic's retained message, which a
// later subscriber receives first.
type Message struct {
	Topic    string
	Payload  []byte
	Retained bool
}

// BrokerClient is a client the broker lets connect with Username and
// Password, allowed to publish to the topics Publish matches and to
// subscribe to the topic filters Subscribe matches, and to nothing else.
// Entries are MQTT topic filters, in which %c stands for the client id of
// the connection, so one client can be shared by connections each confined
// to their own topics.
type BrokerClient struct {
	Username  string
	Password  string
	Publish   []string
	Subscribe []string
}

// Broker is the MQTT broker the door controllers connect to. The
// application connects to it as a client allowed everything, and manages
// the clients the controllers connect as.
type Broker interface {
	// Publish sends m, at least once.
	Publish(ctx context.Context, m Message) error
	// Subscribe subscribes the application to the topic filters and
	// returns the channel the broker delivers the messages they match on,
	// each retained message first. The channel is closed when the
	// connection to the broker ends.
	Subscribe(ctx context.Context, filters []string) (<-chan Message, error)
	// CreateClient adds c to the clients the broker accepts. It refuses a
	// username the broker has already.
	CreateClient(ctx context.Context, c BrokerClient) error
	// DeleteClient removes the client named username, disconnecting it.
	// It refuses a username the broker does not have.
	DeleteClient(ctx context.Context, username string) error
}

// Factory constructs connectors. It has one method per connector.
type Factory interface {
	Clock() Clock
	Entropy() Entropy
	Environment() Environment
	TimeZones() TimeZones
	Log() Log
	Store() Store
	Broker() Broker
}
