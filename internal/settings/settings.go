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
	keyThemeAccent         = "theme_accent"
	keyThemeAccentDark     = "theme_accent_dark"
	defaultAccent          = "#4f39f6"
	colorHint              = "must be a color as #rrggbb, such as #4f39f6"
	siteTimezoneHint       = "must be a time zone name from the tz database, such as UTC or Europe/Berlin"
	keyAdminEmail          = "admin_email"
	keyPasswordHashMemory  = "password_hash_memory_kib"
	minPasswordHashMemory  = 8
	maxPasswordHashMemory  = 1048576 // 1 GiB
	passwordHashMemoryHint = "must be a whole number of KiB from 8 to 1048576"
	keyPasswordMinLength   = "password_min_length"
	minPasswordLength      = 8
	maxPasswordLength      = 128
	passwordMinLengthHint  = "must be a whole number of characters from 8 to 128"
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
	// validate is the values the setting takes and how a page asks for
	// one; the zero rule takes any value in a text box.
	validate rule
	// secret settings are stored encrypted and shown masked.
	secret bool
	// section is the part of the settings page that shows the setting,
	// the one whose definitions list it, under label, explained by help.
	section Section
	label   string
	help    string
}

// Section is a part of the settings page: its name and what it is about,
// and the key of the yes or no setting that turns its settings on, empty
// when they are always in use.
type Section struct {
	Name   string
	About  string
	Toggle string
}

// rule is the values a setting takes, which check rejects the others of
// (a nil check takes any), and how a page asks for one.
type rule struct {
	check func(s Service, value string) error
	input Input
}

// askedWith is the rule r with a page asking for its values with in.
func (r rule) askedWith(in Input) rule {
	r.input = in
	return r
}

// refusal is why the rule does not take value, nil when it does.
func (r rule) refusal(s Service, value string) error {
	if r.check == nil {
		return nil
	}
	return r.check(s, value)
}

// InputKind is the kind of input a page asks for a setting's value with.
type InputKind string

// The kinds of input. The zero kind is a text box.
const (
	KindNumber InputKind = "number"
	KindYesNo  InputKind = "yes-no"
	KindColor  InputKind = "color"
	KindTime   InputKind = "time"
	KindChoice InputKind = "choice"
	KindZone   InputKind = "zone"
)

// The values of a yes or no setting.
const (
	Yes = "true"
	No  = "false"
)

// Input is how a page asks for a setting's value: with an input of Kind,
// offering Choices for a choice (a zone offers every zone the tz database
// lists), and, for a setting that may be empty, the label of the choice
// to leave it so, Empty.
type Input struct {
	Kind    InputKind
	Choices []string
	Empty   string
}

// inputOf is how a page asks for a value parsed as T: a yes or no for a
// truth, a number for a whole number or a length of time, a text box for
// anything else.
func inputOf[T any]() Input {
	var zero T
	switch any(zero).(type) {
	case bool:
		return Input{Kind: KindYesNo}
	case int, uint32, time.Duration:
		return Input{Kind: KindNumber}
	}
	return Input{}
}

// section is a part of the settings page and the definitions of the
// settings it shows, in display order.
type section struct {
	Section
	definitions []definition
}

// sections is every part of the settings page, in display order: the
// site's own settings, then the door configuration defaults.
var sections = []section{
	{Section{Name: "Site", About: "What the site is called and the timezone every time it shows is read in."}, []definition{
		{key: keySiteName, fallback: "doorcode", label: "Site name", help: "Shown in the header and every page title."},
		{key: keySiteTimezone, fallback: "UTC", validate: rule{check: func(s Service, value string) error {
			_, err := s.siteZone(value)
			return err
		}, input: Input{Kind: KindZone}}, label: "Timezone", help: "A zone of the tz database, such as Europe/Berlin; type to find one. Schedules, dates typed on pages, and every time shown are read in it."},
		{key: keyAdminEmail, fallback: "admin@example.com", label: "First administrator's email",
			help: "The account created on first start when there are no users."},
	}},
	{Section{Name: "Theme", About: "The accent color of buttons, links, and highlights. Lighter and darker tones are worked out from it for both the light and the dark theme."}, []definition{
		{key: keyThemeAccent, fallback: defaultAccent, validate: accepting(color).askedWith(Input{Kind: KindColor}), label: "Accent color",
			help: "A color as #rrggbb, such as #4f39f6."},
		{key: keyThemeAccentDark, fallback: "", validate: accepting(optionalColor).askedWith(Input{Kind: KindColor, Empty: "Use the accent color"}),
			label: "Accent color in the dark theme", help: "A color as #rrggbb, or empty to use the accent color."},
	}},
	{Section{Name: "Accounts and passwords", About: "How passwords are hashed and chosen, and how long accounts made at sign-in last."}, []definition{
		{key: keyPasswordHashMemory, fallback: "19456", validate: accepting(passwordHashMemory), label: "Password hashing memory",
			help: "KiB of memory hashing one password takes, from 8 to 1048576. More is slower to guess."},
		{key: keyPasswordMinLength, fallback: "12", validate: accepting(passwordMinLength), label: "Shortest password",
			help: "How many characters a password chosen in the portal has at least, from 8 to 128."},
		{key: keyNewUserExpiry, fallback: "1826", validate: accepting(newUserExpiry), label: "New account lifetime",
			help: "Days until the account of a user created by signing in at the provider expires."},
	}},
	{Section{Name: "Single sign-on", About: "Signing in through an OpenID Connect provider. Register public_url followed by /sign-in/oidc/callback as the redirect address, and public_url followed by /sign-in as the address to return to after signing out.", Toggle: keyOIDCEnabled}, []definition{
		{key: keyOIDCEnabled, fallback: "false", validate: accepting(boolean), label: "Single sign-on enabled",
			help: "Offers signing in at the provider set up here, beside signing in with a password."},
		{key: keyOIDCAutoSignIn, fallback: "false", validate: accepting(boolean), label: "Automatic sign-in",
			help: "Sends the sign-in page straight on to the provider and hides the password forms; add ?auto_sign_in=false to reach them."},
		{key: keyOIDCIssuer, fallback: "", validate: accepting(issuer), label: "Issuer",
			help: "The provider's https:// address, exactly as it names itself."},
		{key: keyOIDCClientID, fallback: "", label: "Client id", help: "The id the site is registered with at the provider."},
		{key: keyOIDCClientSecret, fallback: "", secret: true, label: "Client secret",
			help: "The secret the site is registered with, if the provider gave one."},
		{key: keyOIDCScopes, fallback: "openid email profile", validate: accepting(scopes), label: "Scopes",
			help: "Scopes asked for, separated by spaces, openid among them."},
	}},
	{Section{Name: "Identity claims", About: "The claims of the provider's ID token or userinfo that say who someone is and which groups they are in. An empty claim name is a claim the site does not read.", Toggle: keyOIDCEnabled}, []definition{
		{key: keyClaimUserID, fallback: "", label: "User id claim", help: "The claim holding the user id people are matched by first."},
		{key: keyClaimEmail, fallback: "email", validate: accepting(claimName), label: "Email claim", help: "Required."},
		{key: keyClaimFirstName, fallback: "given_name", label: "First name claim"},
		{key: keyClaimLastName, fallback: "family_name", label: "Last name claim"},
		{key: keyClaimDisplayName, fallback: "name", label: "Display name claim"},
		{key: keyClaimEntitlements, fallback: "", label: "Entitlements claim",
			help: "The claim listing the slugs of someone's groups. A sign-in that carries it makes them a member of exactly the groups it names, ignoring slugs no group has; one without it leaves their groups alone. While it is set, new users join no default or pre-assigned groups."},
		{key: keyDefaultGroups, fallback: "", validate: accepting(slugs), label: "Groups new users join",
			help: "Group slugs separated by commas. New users join these, and the groups whose pre-assigned list holds their user id, only while the entitlements claim is empty."},
	}},
	{Section{Name: "Door enrollment", About: "The broker account every new controller enrolls with, and how long a door's old credentials keep working. A changed enrollment username or password takes effect when the server next starts."}, []definition{
		{key: keyEnrollmentUsername, fallback: "doorcode-enroll", label: "Enrollment username"},
		{key: keyEnrollmentPassword, fallback: "", secret: true, label: "Enrollment password",
			help: "The password a new door controller signs in to the broker with, beside the enrollment username, until an administrator approves it and it gets credentials of its own. Enter both in the settings of each controller you install. When it is empty on first start, the server makes one up and writes it to its log."},
		{key: keyGraceHours, fallback: "24", validate: accepting(graceHours), label: "Credential grace period",
			help: "Hours a door's old credentials keep working after new ones are issued."},
	}},
	{Section{Name: "Telemetry", About: "How long door reports are kept and when a silent door counts as unresponsive."}, []definition{
		{key: keyTelemetryRetention, fallback: "7", validate: accepting(telemetryRetention), label: "Reports kept",
			help: "Days each telemetry report is kept."},
		{key: keyRollupRetention, fallback: "730", validate: accepting(rollupRetention), label: "Hourly summaries kept",
			help: "Days the hourly summaries of the reports are kept."},
		{key: keyUnresponsive, fallback: "5", validate: accepting(unresponsiveAfter), label: "Unresponsive after",
			help: "Minutes a connected door may stay silent before it counts as unresponsive."},
	}},
	{Section{Name: "Firmware updates", About: "Where doors download firmware from, and when and how fast updates roll out."}, []definition{
		{key: keyPublicURL, fallback: "", validate: accepting(publicURL), label: "Public address",
			help: "The https:// address doors and browsers reach the server at. Nothing is sent to doors to download while it is empty."},
		{key: KeyUpdateDays, fallback: EveryDay, validate: accepting(updateDays), label: "Update days",
			help: "daily, or weekdays separated by commas such as sat,sun."},
		{key: keyUpdateStart, fallback: "02:00", validate: accepting(timeOfDay).askedWith(Input{Kind: KindTime}), label: "Update window opens", help: "A time of day in the site timezone."},
		{key: keyUpdateLength, fallback: "120", validate: accepting(updateLength), label: "Update window length", help: "Minutes."},
		{key: keyUpdateConcurrency, fallback: "5", validate: accepting(updateConcurrency), label: "Doors updating at once"},
		{key: keyUpdateTimeout, fallback: "30", validate: accepting(updateTimeout), label: "Update timeout",
			help: "Minutes a door may take to come back on its new version before the update fails."},
	}},
	{Section{Name: "Door defaults", About: "The configuration every door runs with unless its own page overrides a field. Doors receive a changed default as soon as it is saved."}, doorDefinitions()},
}

// UpdateKeys are the keys of the settings that say how firmware updates
// roll out, in display order.
var UpdateKeys = []string{KeyUpdateDays, keyUpdateStart, keyUpdateLength, keyUpdateConcurrency, keyUpdateTimeout}

// definitions is every setting the site has, in display order, each
// knowing its section.
var definitions = inSections(sections)

// inSections is the definitions of sections, in order, each given the
// section that shows it.
func inSections(sections []section) []definition {
	var out []definition
	for _, s := range sections {
		for _, def := range s.definitions {
			def.section = s.Section
			out = append(out, def)
		}
	}
	return out
}

// Current is the settings the application acts on, parsed.
type Current struct {
	SiteName string
	// AdminEmail is the email of the administrator created on first start.
	AdminEmail string
	// PasswordHashMemoryKiB is the memory cost of hashing a new password.
	PasswordHashMemoryKiB uint32
	// PasswordMinLength is how many characters a password chosen in the
	// portal must have at least.
	PasswordMinLength int
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
	// Theme is the colors pages are drawn in.
	Theme Theme
}

// Theme is the colors pages are drawn in: Accent, as #rrggbb, and
// AccentDark in the dark theme, empty to use Accent there too.
type Theme struct {
	Accent     string
	AccentDark string
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
	// DefaultGroups are the slugs of the groups a user created by signing
	// in joins while Claims.Entitlements is empty.
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
	if value != Yes && value != No {
		return false, errors.New("must be true or false")
	}
	return value == Yes, nil
}

// color parses a color written as #rrggbb, kept in lowercase.
func color(value string) (string, error) {
	if len(value) != 7 || value[0] != '#' || strings.Trim(strings.ToLower(value[1:]), "0123456789abcdef") != "" {
		return "", errors.New(colorHint)
	}
	return strings.ToLower(value), nil
}

// optionalColor parses a color as color does, or the empty value.
func optionalColor(value string) (string, error) {
	if value == "" {
		return "", nil
	}
	return color(value)
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

// passwordMinLength parses how many characters a chosen password must
// have at least.
func passwordMinLength(value string) (int, error) {
	return wholeNumber(value, minPasswordLength, maxPasswordLength, passwordMinLengthHint)
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
	// Section is the part of the settings page that shows the setting,
	// under Label, explained by Help, asking for a value with Input.
	Section Section
	Label   string
	Help    string
	Input   Input
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

// Choices is the values a page offers for setting: every zone the tz
// database lists for a zone, the choices of its input otherwise.
func (s Service) Choices(setting Setting) []string {
	if setting.Input.Kind == KindZone {
		return s.Zones.List()
	}
	return setting.Input.Choices
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
	minLength, err := parsed(values, keyPasswordMinLength, passwordMinLength)
	if err != nil {
		return Current{}, err
	}
	c := Current{SiteName: values[keySiteName], AdminEmail: values[keyAdminEmail], PasswordHashMemoryKiB: memory, PasswordMinLength: minLength, Zone: zone}
	for _, d := range durations {
		if *d.at(&c), err = parsed(values, d.key, d.parse); err != nil {
			return Current{}, err
		}
	}
	c, err = c.withUpdates(values)
	if err != nil {
		return Current{}, err
	}
	if c.Theme.Accent, err = parsed(values, keyThemeAccent, color); err != nil {
		return Current{}, err
	}
	if c.Theme.AccentDark, err = parsed(values, keyThemeAccentDark, optionalColor); err != nil {
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
	var errs [6]error
	o.Enabled, errs[0] = parsed(values, keyOIDCEnabled, boolean)
	o.AutoSignIn, errs[1] = parsed(values, keyOIDCAutoSignIn, boolean)
	o.Issuer, errs[2] = parsed(values, keyOIDCIssuer, issuer)
	o.Scopes, errs[3] = parsed(values, keyOIDCScopes, scopes)
	o.Claims.Email, errs[4] = parsed(values, keyClaimEmail, claimName)
	o.DefaultGroups, errs[5] = parsed(values, keyDefaultGroups, slugs)
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

// SetEnrollmentPassword stores password as the one every controller
// enrolls with.
func (s Service) SetEnrollmentPassword(ctx context.Context, password string) error {
	_, err := s.Update(ctx, keyEnrollmentPassword, password)
	return err
}

// accepting is the rule that takes the values parse takes, asked for as
// inputOf says for what parse makes.
func accepting[T any](parse func(string) (T, error)) rule {
	return rule{check: func(_ Service, value string) error {
		_, err := parse(value)
		return err
	}, input: inputOf[T]()}
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
// An empty value leaves a secret as it is.
func (s Service) Update(ctx context.Context, key, value string) (Setting, error) {
	return s.put(ctx, key, value, false)
}

// Clear stores the empty value for the setting named key, a secret
// included, and returns the result.
func (s Service) Clear(ctx context.Context, key string) (Setting, error) {
	return s.put(ctx, key, "", true)
}

// put stores value for the setting named key, even an empty value for a
// secret when clearing, and returns the result.
func (s Service) put(ctx context.Context, key, value string, clearing bool) (Setting, error) {
	def, err := lookup(key)
	if err != nil {
		return Setting{}, err
	}
	envVar := envVarFor(def.key)
	if _, set := s.Env.Lookup(envVar); set {
		return Setting{}, fmt.Errorf("%w: %s is set by environment variable %s", ErrReadOnly, key, envVar)
	}
	if keeps(def, value, clearing) {
		return s.Get(ctx, key)
	}
	if err := def.validate.refusal(s, value); err != nil {
		return Setting{}, fmt.Errorf("%w for %s: %w", ErrInvalid, key, err)
	}
	row := connector.StoredSetting{Value: s.storable(def, value), UpdatedAt: s.Clock.Now()}
	if err := s.Store.PutSetting(ctx, def.key, row.Value, row.UpdatedAt); err != nil {
		return Setting{}, err
	}
	return fromStore(def, row), nil
}

// UpdateAll stores each of values, keyed by setting key, that differs from
// the setting's value now, and the empty value for each secret setting
// whose key is in cleared, and returns the keys of those it stored, in
// display order. It stores none when one is refused, and reports each
// refusal as the problem with its key. It passes over values for keys no
// setting has and for settings the environment provides, which a form
// does not offer. An empty value for a secret setting leaves it as it is,
// unless the secret is cleared.
func (s Service) UpdateAll(ctx context.Context, values map[string]string, cleared []string) ([]string, error) {
	all, err := s.List(ctx)
	if err != nil {
		return nil, err
	}
	var found check.List
	var changed []definition
	stored := map[string]string{}
	for i, def := range definitions {
		value, given, clearing := formValue(def, values, cleared)
		if !given || value == all[i].Value || all[i].ReadOnly() || keeps(def, value, clearing) {
			continue
		}
		found.Add(def.key, s.refusal(def, value))
		changed = append(changed, def)
		stored[def.key] = value
	}
	if err := found.Err(); err != nil {
		return nil, err
	}
	now := s.Clock.Now()
	keys := make([]string, 0, len(changed))
	for _, def := range changed {
		if err := s.Store.PutSetting(ctx, def.key, s.storable(def, stored[def.key]), now); err != nil {
			return nil, err
		}
		keys = append(keys, def.key)
	}
	return keys, nil
}

// formValue is the value values or cleared give the setting def, whether
// they give one, and whether it clears the setting, which only cleared
// does, for a secret or a setting whose input offers to leave it empty.
func formValue(def definition, values map[string]string, cleared []string) (value string, given, clearing bool) {
	clearable := def.secret || def.validate.input.Empty != ""
	if clearable && slices.Contains(cleared, def.key) {
		return "", true, true
	}
	value, given = values[def.key]
	return value, given, false
}

// refusal is why the setting def cannot take value, as a sentence; empty
// when it can.
func (s Service) refusal(def definition, value string) string {
	if err := def.validate.refusal(s, value); err != nil {
		return sentence(err.Error())
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
