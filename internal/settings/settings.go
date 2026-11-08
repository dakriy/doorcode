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

	"github.com/dakriy/doorcode/internal/check"
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

// siteDefinitions is every setting that is not a door configuration
// default, in display order.
var siteDefinitions = []definition{
	{key: keySiteName, fallback: "doorcode"},
	{key: keySiteTimezone, fallback: "UTC", validate: func(s Service, value string) error {
		_, err := s.siteZone(value)
		return err
	}},
	{key: keyAdminEmail, fallback: "admin@example.com"},
	{key: keyPasswordHashMemory, fallback: "19456", validate: accepting(passwordHashMemory)},
	{key: keyEnrollmentUsername, fallback: "doorcode-enroll"},
	{key: keyEnrollmentPassword, fallback: ""},
	{key: keyGraceHours, fallback: "24", validate: accepting(graceHours)},
}

// definitions is every setting the site has, in display order: the
// site's own, then the door configuration defaults.
var definitions = append(siteDefinitions, doorDefinitions()...)

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
	// Grace is how long a door's old credentials keep working after the
	// server issues new ones.
	Grace time.Duration
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
	memory, err := parsed(values, keyPasswordHashMemory, passwordHashMemory)
	if err != nil {
		return Current{}, err
	}
	zone, err := parsed(values, keySiteTimezone, s.siteZone)
	if err != nil {
		return Current{}, err
	}
	grace, err := parsed(values, keyGraceHours, graceHours)
	if err != nil {
		return Current{}, err
	}
	return Current{
		SiteName:              values[keySiteName],
		AdminEmail:            values[keyAdminEmail],
		PasswordHashMemoryKiB: memory,
		Zone:                  zone,
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

// SetEnrollmentPassword stores password as the one every controller
// enrolls with.
func (s Service) SetEnrollmentPassword(ctx context.Context, password string) error {
	_, err := s.Update(ctx, keyEnrollmentPassword, password)
	return err
}

// accepting is a validator that accepts the values parse accepts.
func accepting[T any](parse func(string) (T, error)) func(Service, string) error {
	return func(_ Service, value string) error {
		_, err := parse(value)
		return err
	}
}

// parsed is the value of the setting named key among values, parsed by
// parse, or why it is invalid.
func parsed[T any](values map[string]string, key string, parse func(string) (T, error)) (T, error) {
	v, err := parse(values[key])
	if err != nil {
		return v, fmt.Errorf("%w for %s: %w", ErrInvalid, key, err)
	}
	return v, nil
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

// UpdateAll stores each of values, keyed by setting key, that differs from
// the setting's value now, and returns the keys of those it stored, in
// display order. It stores none when one is refused, and reports each
// refusal as the problem with its key. It passes over values for keys no
// setting has and for settings the environment provides, which a form
// does not offer.
func (s Service) UpdateAll(ctx context.Context, values map[string]string) ([]string, error) {
	all, err := s.List(ctx)
	if err != nil {
		return nil, err
	}
	var found check.List
	var changed []string
	for i, def := range definitions {
		value, given := values[def.key]
		if !given || value == all[i].Value || all[i].ReadOnly() {
			continue
		}
		found.Add(def.key, s.refusal(def, value))
		changed = append(changed, def.key)
	}
	if err := found.Err(); err != nil {
		return nil, err
	}
	now := s.Clock.Now()
	for _, key := range changed {
		if err := s.Store.PutSetting(ctx, key, values[key], now); err != nil {
			return nil, err
		}
	}
	return changed, nil
}

// refusal is why the setting def cannot take value, as a sentence; empty
// when it can.
func (s Service) refusal(def definition, value string) string {
	if def.validate == nil {
		return ""
	}
	if err := def.validate(s, value); err != nil {
		text := err.Error()
		return strings.ToUpper(text[:1]) + text[1:] + "."
	}
	return ""
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
