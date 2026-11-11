// Package connector declares every interface through which application code
// reaches the outside world, and the factory that supplies them.
package connector

import (
	"context"
	"errors"
	"net/netip"
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
	// List returns the name of every zone the tz database has, in
	// ascending byte order.
	List() []string
}

// Resolver looks host names up in the DNS.
type Resolver interface {
	// Addresses returns the IP addresses name resolves to, or an error
	// when it resolves to none or cannot be looked up.
	Addresses(ctx context.Context, name string) ([]netip.Addr, error)
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
	// SignInImage returns the picture the sign-in page shows, and whether
	// one is stored.
	SignInImage(ctx context.Context) (StoredImage, bool, error)
	// PutSignInImage stores image as the picture the sign-in page shows,
	// replacing any stored before.
	PutSignInImage(ctx context.Context, image StoredImage) error
	// RemoveSignInImage removes the picture the sign-in page shows, and
	// reports whether one was stored.
	RemoveSignInImage(ctx context.Context) (bool, error)
}

// StoredImage is a picture the site shows as the store holds it: its JPEG
// data and when it was stored.
type StoredImage struct {
	Data      []byte
	UpdatedAt time.Time
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
	// SetPasswordHash stores hash as the local password hash of the user
	// whose id is id, and reports whether there is one.
	SetPasswordHash(ctx context.Context, id int64, hash string) (bool, error)
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
	// Preassign adds userID to the end of the pre-assigned user ids of the
	// group whose id is groupID; adding one it holds already changes
	// nothing, and so does naming a group there is not.
	Preassign(ctx context.Context, groupID int64, userID string) error
	// RemovePreassigned takes userID off the pre-assigned user ids of the
	// group whose id is groupID; removing one it does not hold changes
	// nothing, and so does naming a group there is not.
	RemovePreassigned(ctx context.Context, groupID int64, userID string) error
}

// StoredSession is a sign-in session as the store holds it. The store keeps
// only a hash of the token the browser holds.
type StoredSession struct {
	TokenHash []byte
	UserID    int64
	CreatedAt time.Time
	ExpiresAt time.Time
	// ViaIdentity says the session began with a sign-in at the identity
	// provider rather than with a password.
	ViaIdentity bool
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

// StoredExclusion is a range of days on which a schedule's windows do not
// open, from First to Last, both included; Last is never before First. A
// single day is a range whose First and Last are the same.
type StoredExclusion struct {
	First time.Time
	Last  time.Time
}

// StoredSchedule is a schedule as the store holds it. Dates are midnight
// UTC on the day they name. Nil StartsOn and EndsOn leave the schedule
// unbounded on that side. Windows and Excluded are in the order they were
// stored. Builtin marks the seeded Always schedule; the store sets it only
// when seeding and ignores it when it stores one.
type StoredSchedule struct {
	ID       int64
	Name     string
	StartsOn *time.Time
	EndsOn   *time.Time
	Windows  []StoredWindow
	Excluded []StoredExclusion
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
	// its windows and exclusions, and reports whether there is one.
	UpdateSchedule(ctx context.Context, s StoredSchedule) (bool, error)
	// DeleteSchedule removes the schedule whose id is id, and reports
	// whether there was one. Zones stop being in open mode during it. It
	// refuses a schedule an access rule uses.
	DeleteSchedule(ctx context.Context, id int64) (bool, error)
}

// StoredController is the controller enrolled as a door: its hardware id,
// whether it is connected to the broker, and what it last told of itself
// and its door.
type StoredController struct {
	HardwareID string
	Online     bool
	StoredDoorState
}

// StoredDoorState is what a door's controller last told of itself and its
// door: its firmware version and signal strength (in dBm), and when the
// server last heard from it. DoorState is "open" or "closed" as the
// controller last said, empty until it has; OpenedAt is when the door
// opened, nil unless it is open; HeldOpen is whether it has been logged as
// held open since then or since the latest open span of its timeline
// ended, and is false while one holds it open. Unresponsive marks a
// controller the server has not heard from for too long. Telemetry is its
// latest report, nil until its first.
type StoredDoorState struct {
	FirmwareVersion string
	Signal          int
	LastSeenAt      time.Time
	DoorState       string
	OpenedAt        *time.Time
	HeldOpen        bool
	Unresponsive    bool
	Telemetry       *StoredTelemetry
}

// StoredTelemetry is one report of a door's controller, received at At:
// its signal strength in dBm, the round trip to the server in
// milliseconds, how long it has run in seconds, its free memory in bytes,
// its firmware version, how often it connected again since it started,
// whether the door was open, how often it was opened since its last
// report, where it sat in the site's mesh: its layer (0 when it
// reached the access point itself), the hardware id of the controller it
// hopped through, and how many controllers hopped through it, how its
// links fared since its last report, and the Wi-Fi link it had.
type StoredTelemetry struct {
	At              time.Time
	Signal          int
	LatencyMS       int
	UptimeS         int64
	FreeBytes       int64
	FirmwareVersion string
	Reconnects      int
	DoorOpen        bool
	Opens           int
	MeshLayer       int
	MeshParent      string
	MeshChildren    int
	StoredLinkCounts
	StoredLink
}

// StoredLinkCounts is how a controller's links fared over a span of time:
// the code checks it sent and those whose reply never came, the TCP
// segments it sent for the first time, sent again, and dropped, and the
// times its Wi-Fi station lost its access point and moved to another by
// its own choice.
type StoredLinkCounts struct {
	ChecksSent     int
	ChecksLost     int
	TCPSent        int64
	TCPRetransmits int64
	TCPDrops       int64
	WifiDrops      int
	Roams          int
}

// StoredLink is the Wi-Fi link a controller reported: the system's reason
// code for its latest loss of the access point since it started (0 when
// there was none), and the BSSID and channel of the access point it had
// joined, empty and 0 with no link.
type StoredLink struct {
	WifiDropReason int
	BSSID          string
	Channel        int
}

// StoredSample is a report of the controller of the door whose id is
// DoorID, as the telemetry history keeps it.
type StoredSample struct {
	DoorID int64
	StoredTelemetry
}

// StoredSpread is what a set of values ranged over: their mean, their
// smallest, and their largest.
type StoredSpread struct {
	Mean float64
	Min  int64
	Max  int64
}

// StoredRollup sums up the Samples reports the controller of the door
// whose id is DoorID made in the hour that starts at Hour: the spread of
// their signal strength, latency, and free memory, how often the door
// was opened in all, how its links fared in all, and the mesh layer and
// children and the Wi-Fi link of its last report.
type StoredRollup struct {
	DoorID       int64
	Hour         time.Time
	Samples      int
	Signal       StoredSpread
	Latency      StoredSpread
	FreeBytes    StoredSpread
	Opens        int
	MeshLayer    int
	MeshChildren int
	StoredLinkCounts
	StoredLink
}

// TelemetryStore holds the doors' telemetry history: each report as a
// sample, and the hourly rollups of the samples, which are kept longer.
type TelemetryStore interface {
	// AddSample stores s.
	AddSample(ctx context.Context, s StoredSample) error
	// Samples returns the samples of the door whose id is doorID taken
	// at or after from and before to, oldest first.
	Samples(ctx context.Context, doorID int64, from, to time.Time) ([]StoredSample, error)
	// AllSamples returns the samples of every door taken at or after from
	// and before to, ordered by door id and then by time.
	AllSamples(ctx context.Context, from, to time.Time) ([]StoredSample, error)
	// FirstSampleSince returns when the earliest sample taken at or after
	// since was taken, and whether there is one.
	FirstSampleSince(ctx context.Context, since time.Time) (time.Time, bool, error)
	// DeleteSamplesBefore removes every sample taken before before, and
	// returns how many it removed.
	DeleteSamplesBefore(ctx context.Context, before time.Time) (int, error)
	// PutRollups stores rollups, each replacing the one of the same door
	// and hour.
	PutRollups(ctx context.Context, rollups []StoredRollup) error
	// NewestRollup returns the latest hour any rollup sums up, and whether
	// there is one.
	NewestRollup(ctx context.Context) (time.Time, bool, error)
	// Rollups returns the rollups of the door whose id is doorID whose
	// hour starts at or after from and before to, oldest first.
	Rollups(ctx context.Context, doorID int64, from, to time.Time) ([]StoredRollup, error)
	// DeleteRollupsBefore removes every rollup whose hour starts before
	// before, and returns how many it removed.
	DeleteRollupsBefore(ctx context.Context, before time.Time) (int, error)
	// Trends sums up the rollups q selects, ordered by door id and then by
	// start.
	Trends(ctx context.Context, q TrendQuery) ([]StoredTrend, error)
}

// The units a TrendQuery sums rollups up by.
const (
	ByHour = "hour"
	ByDay  = "day"
)

// TrendQuery selects the rollups whose hour starts at or after From and
// before To, summed up by door and by the hour or day (Unit, ByHour or
// ByDay) of Zone they start in.
type TrendQuery struct {
	From time.Time
	To   time.Time
	Zone *time.Location
	Unit string
}

// StoredTrend sums up the rollups of the door whose id is DoorID whose
// hour starts in the hour or day that starts at Start: how many there are
// (the hours its controller reported in), how many reports they sum up,
// the means of their signal strength and latency means weighted by their
// reports, and how often the door was opened in all.
type StoredTrend struct {
	DoorID  int64
	Start   time.Time
	Hours   int
	Samples int
	Signal  float64
	Latency float64
	Opens   int
}

// StoredDoor is a door as the store holds it. ZoneIDs are the zones it is
// in, ascending. A nil PrimaryPictureID is a door without a primary
// picture, which the store sets only through SetPrimaryPicture and clears
// when that picture is deleted; it ignores the field when it stores a door.
// A nil DeletedAt is a door that is not deleted. A nil Controller is a door
// no controller has enrolled as; the store sets it only through
// SetController and ignores it when it stores a door. Config is the door's
// configuration overrides, each value keyed by the name of the field it
// overrides, never nil; the store sets it only through SetDoorConfig and
// ignores it when it stores a door. A nil FirmwareID is a door that runs
// the site's default firmware; otherwise it names the firmware the door
// runs instead, which the store sets only through SetDoorFirmware and
// ignores when it stores a door.
type StoredDoor struct {
	ID               int64
	Name             string
	Location         string
	Notes            string
	Public           bool
	ZoneIDs          []int64
	PrimaryPictureID *int64
	DeletedAt        *time.Time
	CreatedAt        time.Time
	Controller       *StoredController
	Config           map[string]string
	FirmwareID       *int64
}

// DoorsStore holds the doors aggregate. Doors are never removed; a deleted
// door carries DeletedAt. Door names are unique ignoring case.
type DoorsStore interface {
	// Doors returns every door, deleted ones included, ordered by name
	// compared byte by byte after lowercasing and then by id.
	Doors(ctx context.Context) ([]StoredDoor, error)
	// CurrentDoors returns the doors that are not deleted, ordered as
	// Doors orders them.
	CurrentDoors(ctx context.Context) ([]StoredDoor, error)
	// Door returns the door whose id is id, and whether there is one.
	Door(ctx context.Context, id int64) (StoredDoor, bool, error)
	// HardwareDoor returns the id of the door whose controller has
	// hardware id hardwareID, and whether there is one.
	HardwareDoor(ctx context.Context, hardwareID string) (int64, bool, error)
	// DoorNamed returns the door, deleted or not, whose name is name when
	// both are lowercased, and whether there is one. Names are unique so
	// compared, so there is at most one.
	DoorNamed(ctx context.Context, name string) (StoredDoor, bool, error)
	// CreateDoor stores d, ignoring d.ID, and returns the id it was given.
	CreateDoor(ctx context.Context, d StoredDoor) (int64, error)
	// UpdateDoor stores d's name, location, notes, public flag, and zones
	// over the door whose id is d.ID, and reports whether there is one.
	UpdateDoor(ctx context.Context, d StoredDoor) (bool, error)
	// SetDoorDeleted stores at as the deletion time of the door whose id is
	// id, nil restoring it, and reports whether there is one.
	SetDoorDeleted(ctx context.Context, id int64, at *time.Time) (bool, error)
	// SetController stores c as the controller of the door whose id is id,
	// nil detaching it, and reports whether there is such a door. Hardware
	// ids are unique: it refuses one another door's controller has.
	SetController(ctx context.Context, id int64, c *StoredController) (bool, error)
	// SetDoorState stores st as what the controller of the door whose id
	// is id last told of itself and its door, leaving its hardware id and
	// whether it is connected as they are, and reports whether the door
	// has a controller.
	SetDoorState(ctx context.Context, id int64, st StoredDoorState) (bool, error)
	// SetDoorFirmware makes the firmware whose id is firmwareID the one
	// the door whose id is id runs instead of the default, nil making it
	// follow the default, and reports whether there is such a door. It
	// refuses firmware the store does not hold.
	SetDoorFirmware(ctx context.Context, id int64, firmwareID *int64) (bool, error)
	// SetDoorConfig stores config as the configuration overrides of the
	// door whose id is id, replacing those it had, and reports whether
	// there is such a door.
	SetDoorConfig(ctx context.Context, id int64, config map[string]string) (bool, error)
	// FirmwareVersions returns how many doors that are not deleted have a
	// controller that last reported each firmware version, most doors
	// first and then by version.
	FirmwareVersions(ctx context.Context) ([]StoredVersionCount, error)
}

// StoredVersionCount is how many doors' controllers last reported
// firmware Version.
type StoredVersionCount struct {
	Version string
	Doors   int
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

// StoredZone is a zone as the store holds it. DoorIDs are the doors in it
// and OpenScheduleIDs the schedules during which its doors are in open
// mode, each ascending.
type StoredZone struct {
	ID              int64
	Name            string
	Description     string
	DoorIDs         []int64
	OpenScheduleIDs []int64
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
	// doors and open-mode schedules, and reports whether there is one.
	UpdateZone(ctx context.Context, z StoredZone) (bool, error)
	// DeleteZone removes the zone whose id is id, taking it off the access
	// rules that name it, and reports whether there was one.
	DeleteZone(ctx context.Context, id int64) (bool, error)
}

// StoredRule is an access rule as the store holds it: members of the group
// whose id is GroupID may enter, with their code, the doors in the zones
// ZoneIDs names while the schedule whose id is ScheduleID is active.
// ZoneIDs are ascending.
type StoredRule struct {
	ID         int64
	GroupID    int64
	ScheduleID int64
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
	// zones, and reports whether there is one.
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

// StoredEvent is one thing a door did or was told to do, as the store
// holds it: at At, at the door whose id is DoorID, an event of Type, whose
// Outcome is "allowed" or "denied" for Reason. UserID is the user it
// concerns, nil when there is none or the code was unknown; RuleID the
// access rule that decided, nil when none did. RequestID is the id the
// door gave the code check, empty for other events, and Detail says more
// in words. OpenMS is how many milliseconds the door was open, nil but for
// a door closing. StartReason is why a door's controller said it started,
// as it said it, empty for every other event and for a start it gave no
// reason for. DoorName and UserName are the door's name and the user's
// display name, which the store fills when it returns an event (UserName
// empty without a user) and ignores when it stores one.
type StoredEvent struct {
	ID          int64
	At          time.Time
	DoorID      int64
	DoorName    string
	Type        string
	Outcome     string
	Reason      string
	UserID      *int64
	UserName    string
	RuleID      *int64
	RequestID   string
	Detail      string
	OpenMS      *int64
	StartReason string
}

// EventQuery selects a page of events. A zero DoorID or UserID, and an
// empty Type, Outcome, Reason, or StartReason, keeps every event;
// otherwise an event is kept when it has that value. A nil From or To
// leaves the range open on that side; otherwise an event is kept when it
// is at or after From and before To. The page skips Offset events and
// holds at most Limit.
type EventQuery struct {
	DoorID      int64
	UserID      int64
	Type        string
	Outcome     string
	Reason      string
	StartReason string
	From        *time.Time
	To          *time.Time
	Offset      int
	Limit       int
}

// StoredRequest is an access request as the store holds it: what the user
// whose id is UserID asked an administrator, under Title and in Body, and
// when they asked and last changed it. Reply is the administrator's answer,
// empty until there is one, and ResolvedAt when the request was resolved,
// nil while it is open. UserName is the user's display name, which the
// store fills when it returns a request and ignores when it stores one.
type StoredRequest struct {
	ID         int64
	UserID     int64
	UserName   string
	Title      string
	Body       string
	Reply      string
	ResolvedAt *time.Time
	CreatedAt  time.Time
	UpdatedAt  time.Time
}

// RequestQuery selects a page of access requests. An empty Search keeps
// every request; otherwise a request is kept when its title or body, or
// its user's display name or email, contains Search ignoring case. A nil
// Resolved keeps both; otherwise a request is kept when whether it is
// resolved is *Resolved. A zero UserID keeps every user's. The page skips
// Offset requests and holds at most Limit.
type RequestQuery struct {
	Search   string
	Resolved *bool
	UserID   int64
	Offset   int
	Limit    int
}

// RequestsStore holds the access requests aggregate.
type RequestsStore interface {
	// Requests returns the page of requests q selects, newest first and
	// then by id descending, and how many requests q selects on every
	// page together.
	Requests(ctx context.Context, q RequestQuery) ([]StoredRequest, int, error)
	// CreateRequest stores r, ignoring r.ID, and returns the id it was
	// given.
	CreateRequest(ctx context.Context, r StoredRequest) (int64, error)
	// Request returns the request whose id is id, and whether there is
	// one.
	Request(ctx context.Context, id int64) (StoredRequest, bool, error)
	// UserRequests returns the requests of the user whose id is userID,
	// newest first and then by id descending.
	UserRequests(ctx context.Context, userID int64) ([]StoredRequest, error)
	// UpdateRequest stores r's title, body, and update time over the
	// request whose id is r.ID, and reports whether there is one.
	UpdateRequest(ctx context.Context, r StoredRequest) (bool, error)
	// SetRequestReply stores reply as the answer to the request whose id
	// is id, and reports whether there is one.
	SetRequestReply(ctx context.Context, id int64, reply string) (bool, error)
	// SetRequestResolved stores at as when the request whose id is id was
	// resolved, nil for an open one, and reports whether there is one.
	SetRequestResolved(ctx context.Context, id int64, at *time.Time) (bool, error)
	// DeleteRequest removes the request whose id is id, and reports
	// whether there was one.
	DeleteRequest(ctx context.Context, id int64) (bool, error)
}

// EventsStore holds the events aggregate, the activity log. Events are
// only ever added; they outlive the deletion of their door and user.
type EventsStore interface {
	// AddEvent stores e, ignoring e.ID, and returns the id it was given.
	AddEvent(ctx context.Context, e StoredEvent) (int64, error)
	// Events returns the page of events q selects, newest first and then
	// by id descending, and how many events q selects on every page
	// together.
	Events(ctx context.Context, q EventQuery) ([]StoredEvent, int, error)
	// EventStats sums up the events q selects.
	EventStats(ctx context.Context, q EventRange) (StoredEventStats, error)
}

// EventRange selects the events of type Type at or after From and before
// To, whose days, weekdays, and hours of day are read in Zone.
type EventRange struct {
	Type string
	From time.Time
	To   time.Time
	Zone *time.Location
}

// StoredEventStats sums up a range of events: how many fell on each day
// with each outcome, in each hour of each weekday, had each outcome and
// reason, and happened at each door with each outcome, and how many users
// they named. A day, weekday, or hour without events, and a door without
// any, is left out.
type StoredEventStats struct {
	// Days are ordered by day and then by outcome.
	Days []StoredDayCount
	// Hours are ordered by weekday, Sunday first, and then by hour.
	Hours []StoredHourCount
	// Reasons are ordered by outcome and then by reason.
	Reasons []StoredReasonCount
	// Doors are ordered by door id and then by outcome.
	Doors []StoredDoorCount
	// Users is how many users the events name, each counted once.
	Users int
}

// StoredDayCount is how many events with Outcome fell on Day, the date as
// midnight UTC on it.
type StoredDayCount struct {
	Day     time.Time
	Outcome string
	Count   int
}

// StoredHourCount is how many events fell in the hour of day that starts
// at Hour (0 to 23) on a Weekday.
type StoredHourCount struct {
	Weekday time.Weekday
	Hour    int
	Count   int
}

// StoredReasonCount is how many events had Outcome for Reason.
type StoredReasonCount struct {
	Outcome string
	Reason  string
	Count   int
}

// StoredDoorCount is how many events with Outcome happened at the door
// whose id is DoorID.
type StoredDoorCount struct {
	DoorID  int64
	Outcome string
	Count   int
}

// StoredFirmware is an uploaded firmware image as the store holds it,
// without the image itself: its version, unique, what the uploader said
// of it, the SHA-256 of the image in hex and its size in bytes, and who
// uploaded it when. UploaderName is the uploader's display name, which the
// store fills when it returns firmware and ignores when it stores it.
// Default marks the site's default firmware, which the store sets only
// through SetDefaultFirmware and ClearDefaultFirmware and ignores when it
// stores firmware.
type StoredFirmware struct {
	ID           int64
	Version      string
	Notes        string
	SHA256       string
	Size         int
	UploadedBy   int64
	UploaderName string
	UploadedAt   time.Time
	Default      bool
}

// StoredUpdate is the latest firmware update the server sent the
// controller of the door whose id is DoorID: the version it was told to
// install, the id of the command that told it, a hash of the token that
// lets it download the image, when it was sent, and when the update stops
// counting as under way and the token stops working. State is "updating"
// until it "succeeded" or "failed", at FinishedAt, nil until then; Detail
// says why it failed.
type StoredUpdate struct {
	DoorID     int64
	Version    string
	CommandID  string
	TokenHash  []byte
	SentAt     time.Time
	ExpiresAt  time.Time
	State      string
	FinishedAt *time.Time
	Detail     string
}

// FirmwareStore holds the firmware images and the updates the server sent
// the doors, one per door, the latest.
type FirmwareStore interface {
	// AddFirmware stores f, ignoring f.ID and f.Default, with its image,
	// and returns the id it was given. It refuses a version it holds
	// already.
	AddFirmware(ctx context.Context, f StoredFirmware, image []byte) (int64, error)
	// Firmwares returns every firmware, without images, newest upload
	// first and then by id descending.
	Firmwares(ctx context.Context) ([]StoredFirmware, error)
	// Firmware returns the firmware whose id is id, and whether there is
	// one.
	Firmware(ctx context.Context, id int64) (StoredFirmware, bool, error)
	// FirmwareImage returns the image of the firmware whose id is id, and
	// whether there is one.
	FirmwareImage(ctx context.Context, id int64) ([]byte, bool, error)
	// SetDefaultFirmware makes the firmware whose id is id the site's
	// default, and no other, and reports whether there is such firmware.
	SetDefaultFirmware(ctx context.Context, id int64) (bool, error)
	// ClearDefaultFirmware leaves the site without default firmware.
	ClearDefaultFirmware(ctx context.Context) error
	// DeleteFirmware removes the firmware whose id is id, and reports
	// whether there was one. It refuses firmware a door runs instead of
	// the default.
	DeleteFirmware(ctx context.Context, id int64) (bool, error)
	// DoorUpdates returns the latest update of every door that was sent
	// one, ordered by door id.
	DoorUpdates(ctx context.Context) ([]StoredUpdate, error)
	// PutDoorUpdate stores u as the latest update of its door, replacing
	// the one it had. It refuses a door the store does not hold.
	PutDoorUpdate(ctx context.Context, u StoredUpdate) error
}

// Store is the application's persistent state, one interface per aggregate.
type Store interface {
	// Migrate brings the schema up to date. It runs once at startup, before
	// any other method.
	Migrate(ctx context.Context) error
	// Ping asks the database a trivial question, reporting why it could
	// not answer.
	Ping(ctx context.Context) error
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
	EventsStore
	TelemetryStore
	FirmwareStore
	RequestsStore
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

// ErrNoClient is what Broker.DeleteClient reports for a username the
// broker does not have.
var ErrNoClient = errors.New("the broker has no such client")

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
	// It refuses a username the broker does not have with ErrNoClient.
	DeleteClient(ctx context.Context, username string) error
	// Connected reports whether the application's connection to the
	// broker is up.
	Connected() bool
}

// IdentityClient is the site as a client an OpenID Connect provider knows:
// the id and secret it was registered with, the scopes it asks for, and the
// address the provider sends the browser back to.
type IdentityClient struct {
	ID          string
	Secret      string
	Scopes      []string
	RedirectURL string
}

// IdentityGrant is what a provider gave for a sign-in: the claims of its ID
// token and the access token its userinfo endpoint takes.
type IdentityGrant struct {
	Claims      map[string]any
	AccessToken string
}

// Identity reaches OpenID Connect providers.
type Identity interface {
	// Discover reads the configuration the provider whose issuer is issuer
	// publishes.
	Discover(ctx context.Context, issuer string) (IdentityProvider, error)
}

// IdentityProvider is one OpenID Connect provider, discovered.
type IdentityProvider interface {
	// AuthURL is the address of the provider's sign-in page for c, asking
	// for an authorization code and carrying state, nonce, and the S256
	// challenge of verifier.
	AuthURL(c IdentityClient, state, nonce, verifier string) string
	// Exchange redeems code, with verifier, at the provider's token
	// endpoint, and returns the claims of the ID token it answers with once
	// the token's signature, issuer, audience (c's id), and expiry are
	// verified.
	Exchange(ctx context.Context, c IdentityClient, code, verifier string) (IdentityGrant, error)
	// UserInfo returns the claims the provider's userinfo endpoint answers
	// accessToken with.
	UserInfo(ctx context.Context, accessToken string) (map[string]any, error)
	// EndSessionURL is the provider's end_session_endpoint, empty when its
	// configuration names none.
	EndSessionURL() string
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
	Identity() Identity
	Resolver() Resolver
}
