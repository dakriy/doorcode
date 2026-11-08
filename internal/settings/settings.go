// Package settings resolves the site's settings. A value comes from the
// environment when the matching variable is set, which makes it read-only;
// otherwise from the store when an administrator saved one; otherwise from
// the setting's default.
package settings

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/dakriy/doorcode/internal/connector"
)

// ErrUnknown reports a key that names no setting.
var ErrUnknown = errors.New("unknown setting")

// ErrInvalid reports a value the setting does not accept.
var ErrInvalid = errors.New("invalid value")

// ErrReadOnly reports an update to a setting the environment provides.
var ErrReadOnly = errors.New("setting is read-only")

// Source says where a setting's value came from.
type Source string

// The sources of a setting value, in order of precedence.
const (
	SourceEnvironment Source = "environment"
	SourceDatabase    Source = "database"
	SourceDefault     Source = "default"
)

// envPrefix starts the name of every variable that overrides a setting.
const envPrefix = "DOORCODE_"

// The keys of the settings the application reads.
const (
	keySiteName            = "site_name"
	keySiteTimezone        = "site_timezone"
	siteTimezoneHint       = "must be a time zone name from the tz database, such as UTC or Europe/Berlin"
	keyAdminEmail          = "admin_email"
	keyPasswordHashMemory  = "password_hash_memory_kib"
	minPasswordHashMemory  = 8
	maxPasswordHashMemory  = 1048576 // 1 GiB
	passwordHashMemoryHint = "must be a whole number of KiB from 8 to 1048576"
	keyEnrollmentUsername  = "enrollment_username"
	keyEnrollmentPassword  = "enrollment_password"
	keyRotationDays        = "door_credential_rotation_days"
	maxRotationDays        = 3650
	rotationDaysHint       = "must be a whole number of days from 1 to 3650"
	keyGraceHours          = "door_credential_grace_hours"
	maxGraceHours          = 8760
	graceHoursHint         = "must be a whole number of hours from 1 to 8760"
)

type definition struct {
	key      string
	fallback string
	// validate rejects a value the setting cannot take; nil accepts any.
	validate func(s Service, value string) error
}

// definitions is every setting the site has, in display order.
var definitions = []definition{
	{key: keySiteName, fallback: "doorcode"},
	{key: keySiteTimezone, fallback: "UTC", validate: func(s Service, value string) error {
		_, err := s.siteZone(value)
		return err
	}},
	{key: keyAdminEmail, fallback: "admin@example.com"},
	{key: keyPasswordHashMemory, fallback: "19456", validate: func(_ Service, value string) error {
		_, err := passwordHashMemory(value)
		return err
	}},
	{key: keyEnrollmentUsername, fallback: "doorcode-enroll"},
	{key: keyEnrollmentPassword, fallback: ""},
	{key: keyRotationDays, fallback: "30", validate: func(_ Service, value string) error {
		_, err := rotationInterval(value)
		return err
	}},
	{key: keyGraceHours, fallback: "24", validate: func(_ Service, value string) error {
		_, err := graceHours(value)
		return err
	}},
}

// Current is the settings the application acts on, parsed.
type Current struct {
	SiteName string
	// AdminEmail is the email of the administrator created on first start.
	AdminEmail string
	// PasswordHashMemoryKiB is the memory cost of hashing a new password.
	PasswordHashMemoryKiB uint32
	// Zone is the site timezone: weekly schedule windows and dates typed on
	// pages are in it.
	Zone *time.Location
	// RotateAfter is how long a door's credentials work before the server
	// issues new ones, and Grace how long the old ones keep working after.
	RotateAfter time.Duration
	Grace       time.Duration
}

// siteZone loads the site timezone a value names. The empty name and
// "Local", which Go reads as UTC and as the server's own zone, name none.
func (s Service) siteZone(value string) (*time.Location, error) {
	if value == "" || value == "Local" {
		return nil, errors.New(siteTimezoneHint)
	}
	zone, err := s.Zones.Zone(value)
	if err != nil {
		return nil, errors.New(siteTimezoneHint)
	}
	return zone, nil
}

// wholeNumber parses value as a whole number from lowest to highest,
// refusing anything else with hint.
func wholeNumber(value string, lowest, highest int, hint string) (int, error) {
	n, err := strconv.Atoi(value)
	if err != nil || n < lowest || n > highest {
		return 0, errors.New(hint)
	}
	return n, nil
}

// passwordHashMemory parses a password hashing memory cost in KiB.
func passwordHashMemory(value string) (uint32, error) {
	n, err := wholeNumber(value, minPasswordHashMemory, maxPasswordHashMemory, passwordHashMemoryHint)
	return uint32(n), err
}

// rotationInterval parses a credential rotation interval in days.
func rotationInterval(value string) (time.Duration, error) {
	n, err := wholeNumber(value, 1, maxRotationDays, rotationDaysHint)
	return time.Duration(n) * 24 * time.Hour, err
}

// graceHours parses a credential grace period in hours.
func graceHours(value string) (time.Duration, error) {
	n, err := wholeNumber(value, 1, maxGraceHours, graceHoursHint)
	return time.Duration(n) * time.Hour, err
}

// Setting is a resolved setting.
type Setting struct {
	Key    string
	Value  string
	Source Source
	// EnvVar is the environment variable that overrides this setting.
	EnvVar string
	// UpdatedAt is when the stored value was written; zero unless Source is
	// SourceDatabase.
	UpdatedAt time.Time
}

// ReadOnly reports whether the setting cannot be changed from the
// application because the environment provides it.
func (s Setting) ReadOnly() bool {
	return s.Source == SourceEnvironment
}

// Service reads and updates settings.
type Service struct {
	Store connector.SettingsStore
	Env   connector.Environment
	Clock connector.Clock
	Zones connector.TimeZones
}

// List returns every setting in display order.
func (s Service) List(ctx context.Context) ([]Setting, error) {
	stored, err := s.Store.Settings(ctx)
	if err != nil {
		return nil, err
	}
	resolved := make([]Setting, 0, len(definitions))
	for _, def := range definitions {
		resolved = append(resolved, s.resolve(def, stored))
	}
	return resolved, nil
}

// Current resolves and parses the settings the application acts on.
func (s Service) Current(ctx context.Context) (Current, error) {
	all, err := s.List(ctx)
	if err != nil {
		return Current{}, err
	}
	values := valuesOf(all)
	memory, err := passwordHashMemory(values[keyPasswordHashMemory])
	if err != nil {
		return Current{}, fmt.Errorf("%w for %s: %w", ErrInvalid, keyPasswordHashMemory, err)
	}
	zone, err := s.siteZone(values[keySiteTimezone])
	if err != nil {
		return Current{}, fmt.Errorf("%w for %s: %w", ErrInvalid, keySiteTimezone, err)
	}
	rotateAfter, err := rotationInterval(values[keyRotationDays])
	if err != nil {
		return Current{}, fmt.Errorf("%w for %s: %w", ErrInvalid, keyRotationDays, err)
	}
	grace, err := graceHours(values[keyGraceHours])
	if err != nil {
		return Current{}, fmt.Errorf("%w for %s: %w", ErrInvalid, keyGraceHours, err)
	}
	return Current{
		SiteName:              values[keySiteName],
		AdminEmail:            values[keyAdminEmail],
		PasswordHashMemoryKiB: memory,
		Zone:                  zone,
		RotateAfter:           rotateAfter,
		Grace:                 grace,
	}, nil
}

// Enrollment returns the broker username and password every controller
// enrolls with, which do not depend on any other setting being valid.
func (s Service) Enrollment(ctx context.Context) (username, password string, err error) {
	all, err := s.List(ctx)
	values := valuesOf(all)
	return values[keyEnrollmentUsername], values[keyEnrollmentPassword], err
}

// valuesOf maps the key of each setting among all to its value.
func valuesOf(all []Setting) map[string]string {
	values := map[string]string{}
	for _, setting := range all {
		values[setting.Key] = setting.Value
	}
	return values
}

// Get returns the setting named key.
func (s Service) Get(ctx context.Context, key string) (Setting, error) {
	def, err := lookup(key)
	if err != nil {
		return Setting{}, err
	}
	stored, err := s.Store.Settings(ctx)
	if err != nil {
		return Setting{}, err
	}
	return s.resolve(def, stored), nil
}

// Update stores value for the setting named key and returns the result.
func (s Service) Update(ctx context.Context, key, value string) (Setting, error) {
	def, err := lookup(key)
	if err != nil {
		return Setting{}, err
	}
	envVar := envVarFor(def.key)
	if _, set := s.Env.Lookup(envVar); set {
		return Setting{}, fmt.Errorf("%w: %s is set by environment variable %s", ErrReadOnly, key, envVar)
	}
	if def.validate != nil {
		if err := def.validate(s, value); err != nil {
			return Setting{}, fmt.Errorf("%w for %s: %w", ErrInvalid, key, err)
		}
	}
	row := connector.StoredSetting{Value: value, UpdatedAt: s.Clock.Now()}
	if err := s.Store.PutSetting(ctx, def.key, row.Value, row.UpdatedAt); err != nil {
		return Setting{}, err
	}
	return fromStore(def, row), nil
}

func (s Service) resolve(def definition, stored map[string]connector.StoredSetting) Setting {
	envVar := envVarFor(def.key)
	if value, set := s.Env.Lookup(envVar); set {
		return Setting{Key: def.key, Value: value, Source: SourceEnvironment, EnvVar: envVar}
	}
	if row, found := stored[def.key]; found {
		return fromStore(def, row)
	}
	return Setting{Key: def.key, Value: def.fallback, Source: SourceDefault, EnvVar: envVar}
}

// fromStore is the setting as an administrator saved it.
func fromStore(def definition, row connector.StoredSetting) Setting {
	return Setting{Key: def.key, Value: row.Value, Source: SourceDatabase, EnvVar: envVarFor(def.key), UpdatedAt: row.UpdatedAt}
}

func lookup(key string) (definition, error) {
	for _, def := range definitions {
		if def.key == key {
			return def, nil
		}
	}
	return definition{}, fmt.Errorf("%w %q", ErrUnknown, key)
}

func envVarFor(key string) string {
	return envPrefix + strings.ToUpper(key)
}
