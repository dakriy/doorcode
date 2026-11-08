// Package settings resolves the site's settings. A value comes from the
// environment when the matching variable is set, which makes it read-only;
// otherwise from the store when an administrator saved one; otherwise from
// the setting's default.
package settings

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/dakriy/doorcode/internal/check"
	"github.com/dakriy/doorcode/internal/connector"
	"github.com/dakriy/doorcode/internal/schedule"
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
	keyTelemetryRetention  = "telemetry_retention_days"
	keyRollupRetention     = "telemetry_rollup_retention_days"
	keyUnresponsive        = "unresponsive_minutes"
	keyPublicURL           = "public_url"
	publicURLHint          = "must be empty or an https:// address without a query, such as https://doorcode.example.com"
	// KeyUpdateDays is the key of the setting naming the weekdays the
	// update window opens on, EveryDay when it opens daily.
	KeyUpdateDays         = "update_window_days"
	EveryDay              = "daily"
	updateDaysHint        = "must be daily or weekdays separated by commas, such as sat,sun"
	keyUpdateStart        = "update_window_start"
	updateStartHint       = "must be a time of day as HH:MM, such as 02:00"
	keyUpdateLength       = "update_window_minutes"
	keyUpdateConcurrency  = "update_concurrency"
	maxUpdateConcurrency  = 1000
	updateConcurrencyHint = "must be a whole number of doors from 1 to 1000"
	keyUpdateTimeout      = "update_timeout_minutes"
	keyNewUserExpiry      = "new_user_expiry_days"
	keyOIDCEnabled        = "oidc_enabled"
	keyOIDCAutoSignIn     = "oidc_auto_sign_in"
	keyOIDCIssuer         = "oidc_issuer"
	issuerHint            = "must be empty or the https:// address the provider names as its issuer, such as https://id.example.com"
	keyOIDCClientID       = "oidc_client_id"
	keyOIDCClientSecret   = "oidc_client_secret"
	keyOIDCScopes         = "oidc_scopes"
	scopesHint            = "must be scopes separated by spaces, openid among them"
	keyClaimUserID        = "oidc_claim_user_id"
	keyClaimEmail         = "oidc_claim_email"
	keyClaimFirstName     = "oidc_claim_first_name"
	keyClaimLastName      = "oidc_claim_last_name"
	keyClaimDisplayName   = "oidc_claim_display_name"
	keyClaimEntitlements  = "oidc_claim_entitlements"
	keyEntitlementGroups  = "oidc_entitlement_groups"
	keyDefaultGroups      = "oidc_default_groups"
	slugsHint             = "must be group slugs separated by commas, such as staff,contractors"
)

// The settings that are a length of time, parsed.
var (
	graceHours         = durationSetting(1, 8760, "hours", hoursOf)
	telemetryRetention = durationSetting(1, 365, "days", daysOf)
	rollupRetention    = durationSetting(1, 3650, "days", daysOf)
	unresponsiveAfter  = durationSetting(1, 10080, "minutes", minutesOf)
	updateLength       = durationSetting(1, 1440, "minutes", minutesOf)
	updateTimeout      = durationSetting(1, 1440, "minutes", minutesOf)
	newUserExpiry      = durationSetting(1, 36500, "days", daysOf)
)

// The lengths of time that are a number of minutes, hours, or days.
func minutesOf(n int) time.Duration { return time.Duration(n) * time.Minute }
func hoursOf(n int) time.Duration   { return time.Duration(n) * time.Hour }
func daysOf(n int) time.Duration    { return time.Duration(n) * 24 * time.Hour }

// durations is each setting that is a length of time: its key, how it is
// parsed, and where Current keeps it, in display order.
var durations = []struct {
	key   string
	parse func(string) (time.Duration, error)
	at    func(c *Current) *time.Duration
}{
	{keyGraceHours, graceHours, func(c *Current) *time.Duration { return &c.Grace }},
	{keyTelemetryRetention, telemetryRetention, func(c *Current) *time.Duration { return &c.TelemetryRetention }},
	{keyRollupRetention, rollupRetention, func(c *Current) *time.Duration { return &c.RollupRetention }},
	{keyUnresponsive, unresponsiveAfter, func(c *Current) *time.Duration { return &c.UnresponsiveAfter }},
	{keyUpdateLength, updateLength, func(c *Current) *time.Duration { return &c.UpdateWindow.Length }},
	{keyUpdateTimeout, updateTimeout, func(c *Current) *time.Duration { return &c.UpdateTimeout }},
	{keyNewUserExpiry, newUserExpiry, func(c *Current) *time.Duration { return &c.NewUserExpiry }},
}

type definition struct {
	key      string
	fallback string
	// validate rejects a value the setting cannot take; nil accepts any.
	validate func(s Service, value string) error
	// secret settings are stored encrypted and shown masked.
	secret bool
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
	{key: keyNewUserExpiry, fallback: "1826", validate: accepting(newUserExpiry)},
	{key: keyOIDCEnabled, fallback: "false", validate: accepting(boolean)},
	{key: keyOIDCAutoSignIn, fallback: "false", validate: accepting(boolean)},
	{key: keyOIDCIssuer, fallback: "", validate: accepting(issuer)},
	{key: keyOIDCClientID, fallback: ""},
	{key: keyOIDCClientSecret, fallback: "", secret: true},
	{key: keyOIDCScopes, fallback: "openid email profile", validate: accepting(scopes)},
	{key: keyClaimUserID, fallback: ""},
	{key: keyClaimEmail, fallback: "email", validate: accepting(claimName)},
	{key: keyClaimFirstName, fallback: "given_name"},
	{key: keyClaimLastName, fallback: "family_name"},
	{key: keyClaimDisplayName, fallback: "name"},
	{key: keyClaimEntitlements, fallback: ""},
	{key: keyEntitlementGroups, fallback: "", validate: accepting(slugs)},
	{key: keyDefaultGroups, fallback: "", validate: accepting(slugs)},
	{key: keyEnrollmentUsername, fallback: "doorcode-enroll"},
	{key: keyEnrollmentPassword, fallback: "", secret: true},
	{key: keyGraceHours, fallback: "24", validate: accepting(graceHours)},
	{key: keyTelemetryRetention, fallback: "7", validate: accepting(telemetryRetention)},
	{key: keyRollupRetention, fallback: "730", validate: accepting(rollupRetention)},
	{key: keyUnresponsive, fallback: "5", validate: accepting(unresponsiveAfter)},
	{key: keyPublicURL, fallback: "", validate: accepting(publicURL)},
	{key: KeyUpdateDays, fallback: EveryDay, validate: accepting(updateDays)},
	{key: keyUpdateStart, fallback: "02:00", validate: accepting(timeOfDay)},
	{key: keyUpdateLength, fallback: "120", validate: accepting(updateLength)},
	{key: keyUpdateConcurrency, fallback: "5", validate: accepting(updateConcurrency)},
	{key: keyUpdateTimeout, fallback: "30", validate: accepting(updateTimeout)},
}

// UpdateKeys are the keys of the settings that say how firmware updates
// roll out, in display order.
var UpdateKeys = []string{KeyUpdateDays, keyUpdateStart, keyUpdateLength, keyUpdateConcurrency, keyUpdateTimeout}

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
	// TelemetryRetention is how long each telemetry report is kept, and
	// RollupRetention how long the hourly rollups of the reports are.
	TelemetryRetention time.Duration
	RollupRetention    time.Duration
	// UnresponsiveAfter is how long the server goes without hearing from
	// a door's controller before it marks it unresponsive.
	UnresponsiveAfter time.Duration
	// PublicURL is the address doors and browsers reach the server at,
	// without a trailing slash; empty until an administrator gives it.
	PublicURL string
	// UpdateWindow is when firmware updates roll out to the doors that
	// need one, UpdateConcurrency how many doors may be updating at once,
	// and UpdateTimeout how long a door may take to come back with the
	// new version before its update counts as failed.
	UpdateWindow      UpdateWindow
	UpdateConcurrency int
	UpdateTimeout     time.Duration
	// NewUserExpiry is how long the account of a user created by signing
	// in lasts.
	NewUserExpiry time.Duration
	// OIDC is how people sign in through an OpenID Connect provider.
	OIDC OIDC
}

// OIDC is how people sign in through an OpenID Connect provider. Its
// client secret is read on its own, through Service.ClientSecret.
type OIDC struct {
	Enabled bool
	// AutoSignIn sends the sign-in page straight on to the provider.
	AutoSignIn bool
	Issuer     string
	ClientID   string
	Scopes     []string
	// Claims names the claims the provider says who someone is in.
	Claims Claims
	// EntitlementGroups are the slugs of the groups the entitlements
	// claim decides membership of: a sign-in whose entitlements hold a
	// group's slug joins it, and one whose entitlements do not leaves it.
	EntitlementGroups []string
	// DefaultGroups are the slugs of the groups a user created by signing
	// in joins.
	DefaultGroups []string
}

// Claims names the claims that hold who someone is. An empty name is a
// claim the site does not read.
type Claims struct {
	UserID       string
	Email        string
	FirstName    string
	LastName     string
	DisplayName  string
	Entitlements string
}

// HidesPasswordForms reports whether the forms that take a password, for
// signing in or for choosing one, are hidden because everyone signs in at
// the provider. It is the one place that decides.
func (o OIDC) HidesPasswordForms() bool {
	return o.Enabled && o.AutoSignIn
}

// UpdateWindow is a time in the site timezone that opens on each of Days
// at Start minutes after midnight and lasts Length.
type UpdateWindow struct {
	Days   []time.Weekday
	Start  int
	Length time.Duration
}

// publicURL parses the address the server is reached at, dropping a
// trailing slash.
func publicURL(value string) (string, error) {
	if !httpsAddress(value) {
		return "", errors.New(publicURLHint)
	}
	return strings.TrimSuffix(value, "/"), nil
}

// issuer parses the address of an OpenID Connect provider, kept exactly as
// given, since the provider's issuer must match it to the letter.
func issuer(value string) (string, error) {
	if !httpsAddress(value) {
		return "", errors.New(issuerHint)
	}
	return value, nil
}

// httpsAddress reports whether value is empty or an https:// address
// without a query or fragment.
func httpsAddress(value string) bool {
	if value == "" {
		return true
	}
	u, err := url.Parse(value)
	return err == nil && u.Scheme == "https" && u.Host != "" && u.RawQuery == "" && u.Fragment == ""
}

// boolean parses true or false.
func boolean(value string) (bool, error) {
	if value != "true" && value != "false" {
		return false, errors.New("must be true or false")
	}
	return value == "true", nil
}

// scopes parses the scopes asked of an OpenID Connect provider.
func scopes(value string) ([]string, error) {
	list := strings.Fields(value)
	if !slices.Contains(list, "openid") {
		return nil, errors.New(scopesHint)
	}
	return list, nil
}

// claimName parses the name of a claim the site cannot do without.
func claimName(value string) (string, error) {
	if strings.TrimSpace(value) == "" {
		return "", errors.New("must name a claim")
	}
	return value, nil
}

// slugs parses group slugs separated by commas.
func slugs(value string) ([]string, error) {
	var list []string
	for _, slug := range strings.Split(value, ",") {
		slug = strings.TrimSpace(slug)
		if slug == "" {
			continue
		}
		if !check.Slug(slug) {
			return nil, errors.New(slugsHint)
		}
		list = append(list, slug)
	}
	return list, nil
}

// updateDays parses the weekdays the update window opens on: every day,
// or the days named.
func updateDays(value string) ([]time.Weekday, error) {
	if value == EveryDay {
		return []time.Weekday{time.Sunday, time.Monday, time.Tuesday, time.Wednesday, time.Thursday, time.Friday, time.Saturday}, nil
	}
	var days []time.Weekday
	for _, name := range strings.Split(value, ",") {
		i := slices.Index(schedule.DayNames, strings.TrimSpace(name))
		if i < 0 {
			return nil, errors.New(updateDaysHint)
		}
		days = append(days, schedule.Weekday(i))
	}
	return days, nil
}

// timeOfDay parses a time of day as minutes after midnight.
func timeOfDay(value string) (int, error) {
	t, err := time.Parse("15:04", value)
	if err != nil {
		return 0, errors.New(updateStartHint)
	}
	return t.Hour()*60 + t.Minute(), nil
}

// updateConcurrency parses how many doors may be updating at once.
func updateConcurrency(value string) (int, error) {
	return wholeNumber(value, 1, maxUpdateConcurrency, updateConcurrencyHint)
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

// durationSetting parses a length of time given as a whole number of
// units, named units, from lowest to highest, which of makes a length of
// time.
func durationSetting(lowest, highest int, units string, of func(n int) time.Duration) func(string) (time.Duration, error) {
	hint := fmt.Sprintf("must be a whole number of %s from %d to %d", units, lowest, highest)
	return func(value string) (time.Duration, error) {
		n, err := wholeNumber(value, lowest, highest, hint)
		return of(n), err
	}
}

// Setting is a resolved setting.
type Setting struct {
	Key    string
	Value  string
	Source Source
	// EnvVar is the environment variable that overrides this setting.
	EnvVar string
	// Secret says the setting is stored encrypted; Value is then Mask when
	// it has one and empty when it has none.
	Secret bool
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
	// Secrets encrypts and decrypts the values of secret settings.
	Secrets Secrets
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
	c := Current{SiteName: values[keySiteName], AdminEmail: values[keyAdminEmail], PasswordHashMemoryKiB: memory, Zone: zone}
	for _, d := range durations {
		if *d.at(&c), err = parsed(values, d.key, d.parse); err != nil {
			return Current{}, err
		}
	}
	c, err = c.withUpdates(values)
	if err != nil {
		return Current{}, err
	}
	return c.withSignIn(values)
}

// withSignIn is c with the settings of signing in through an OpenID
// Connect provider among values.
func (c Current) withSignIn(values map[string]string) (Current, error) {
	o := OIDC{ClientID: values[keyOIDCClientID], Claims: Claims{
		UserID: values[keyClaimUserID], FirstName: values[keyClaimFirstName], LastName: values[keyClaimLastName],
		DisplayName: values[keyClaimDisplayName], Entitlements: values[keyClaimEntitlements],
	}}
	var errs [7]error
	o.Enabled, errs[0] = parsed(values, keyOIDCEnabled, boolean)
	o.AutoSignIn, errs[1] = parsed(values, keyOIDCAutoSignIn, boolean)
	o.Issuer, errs[2] = parsed(values, keyOIDCIssuer, issuer)
	o.Scopes, errs[3] = parsed(values, keyOIDCScopes, scopes)
	o.Claims.Email, errs[4] = parsed(values, keyClaimEmail, claimName)
	o.EntitlementGroups, errs[5] = parsed(values, keyEntitlementGroups, slugs)
	o.DefaultGroups, errs[6] = parsed(values, keyDefaultGroups, slugs)
	c.OIDC = o
	return c, errors.Join(errs[:]...)
}

// withUpdates is c with the settings of firmware updates among values.
func (c Current) withUpdates(values map[string]string) (Current, error) {
	var err error
	if c.PublicURL, err = parsed(values, keyPublicURL, publicURL); err != nil {
		return Current{}, err
	}
	if c.UpdateWindow.Days, err = parsed(values, KeyUpdateDays, updateDays); err != nil {
		return Current{}, err
	}
	if c.UpdateWindow.Start, err = parsed(values, keyUpdateStart, timeOfDay); err != nil {
		return Current{}, err
	}
	if c.UpdateConcurrency, err = parsed(values, keyUpdateConcurrency, updateConcurrency); err != nil {
		return Current{}, err
	}
	return c, nil
}

// Enrollment returns the broker username and password every controller
// enrolls with, which do not depend on any other setting being valid.
func (s Service) Enrollment(ctx context.Context) (username, password string, err error) {
	stored, err := s.Store.Settings(ctx)
	if err != nil {
		return "", "", err
	}
	def, _ := lookup(keyEnrollmentUsername)
	password, err = s.secret(keyEnrollmentPassword, stored)
	return s.resolve(def, stored).Value, password, err
}

// ClientSecret returns the secret the site was registered with at its
// OpenID Connect provider, empty when it has none.
func (s Service) ClientSecret(ctx context.Context) (string, error) {
	stored, err := s.Store.Settings(ctx)
	if err != nil {
		return "", err
	}
	return s.secret(keyOIDCClientSecret, stored)
}

// secret returns the value of the secret setting named key: as the
// environment gives it, decrypted as stored holds it, or empty, which
// every secret defaults to.
func (s Service) secret(key string, stored map[string]connector.StoredSetting) (string, error) {
	if value, set := s.Env.Lookup(envVarFor(key)); set {
		return value, nil
	}
	row, found := stored[key]
	if !found {
		return "", nil
	}
	return s.Secrets.open(key, row.Value)
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
	if def.secret && value == "" {
		return s.Get(ctx, key)
	}
	if def.validate != nil {
		if err := def.validate(s, value); err != nil {
			return Setting{}, fmt.Errorf("%w for %s: %w", ErrInvalid, key, err)
		}
	}
	row := connector.StoredSetting{Value: s.storable(def, value), UpdatedAt: s.Clock.Now()}
	if err := s.Store.PutSetting(ctx, def.key, row.Value, row.UpdatedAt); err != nil {
		return Setting{}, err
	}
	return fromStore(def, row), nil
}

// storable is value as the store holds it for the setting def: encrypted
// when def is secret.
func (s Service) storable(def definition, value string) string {
	if def.secret {
		return s.Secrets.seal(def.key, value)
	}
	return value
}

// UpdateAll stores each of values, keyed by setting key, that differs from
// the setting's value now, and returns the keys of those it stored, in
// display order. It stores none when one is refused, and reports each
// refusal as the problem with its key. It passes over values for keys no
// setting has and for settings the environment provides, which a form
// does not offer. An empty value for a secret setting leaves it as it is.
func (s Service) UpdateAll(ctx context.Context, values map[string]string) ([]string, error) {
	all, err := s.List(ctx)
	if err != nil {
		return nil, err
	}
	var found check.List
	var changed []definition
	for i, def := range definitions {
		value, given := values[def.key]
		if !given || value == all[i].Value || all[i].ReadOnly() || (def.secret && value == "") {
			continue
		}
		found.Add(def.key, s.refusal(def, value))
		changed = append(changed, def)
	}
	if err := found.Err(); err != nil {
		return nil, err
	}
	now := s.Clock.Now()
	keys := make([]string, 0, len(changed))
	for _, def := range changed {
		if err := s.Store.PutSetting(ctx, def.key, s.storable(def, values[def.key]), now); err != nil {
			return nil, err
		}
		keys = append(keys, def.key)
	}
	return keys, nil
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
		return shown(def, Setting{Key: def.key, Value: value, Source: SourceEnvironment, EnvVar: envVar})
	}
	if row, found := stored[def.key]; found {
		return fromStore(def, row)
	}
	return shown(def, Setting{Key: def.key, Value: def.fallback, Source: SourceDefault, EnvVar: envVar})
}

// fromStore is the setting as an administrator saved it.
func fromStore(def definition, row connector.StoredSetting) Setting {
	return shown(def, Setting{Key: def.key, Value: row.Value, Source: SourceDatabase, EnvVar: envVarFor(def.key), UpdatedAt: row.UpdatedAt})
}

// shown is s as the setting def lets it be seen: a secret's value masked.
func shown(def definition, s Setting) Setting {
	s.Secret = def.secret
	if def.secret && s.Value != "" {
		s.Value = Mask
	}
	return s
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
