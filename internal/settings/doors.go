package settings

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/dakriy/doorcode/internal/check"
)

// doorPrefix starts the key of every setting that is a door configuration
// default; the rest of the key is the field's name.
const doorPrefix = "door_"

// hostHint is what a time server must be; 253 characters is the longest
// host name there is.
const (
	maxHostLength = 253
	hostHint      = "a host name of up to 253 letters, digits, dots, and hyphens"
)

// DoorConfig is the configuration a door runs with. Durations are whole
// numbers of the unit their name ends in.
type DoorConfig struct {
	StrikePulseMS          int
	KeypadIdleClearMS      int
	MinCodeLength          int
	MaxCodeLength          int
	SubmitKey              string
	ReplyTimeoutMS         int
	TelemetryIntervalS     int
	HeldOpenS              int
	TimelineRefreshHours   int
	CredentialRotationDays int
	RateLimitPerMinute     int
	RateLimitBurst         int
	NTPServer              string
	LogLevel               string
}

// RotateAfter is how long a door's credentials work before the server
// issues new ones.
func (c DoorConfig) RotateAfter() time.Duration {
	return time.Duration(c.CredentialRotationDays) * 24 * time.Hour
}

// HeldOpen is how long the door may stay open before it is held open.
func (c DoorConfig) HeldOpen() time.Duration {
	return time.Duration(c.HeldOpenS) * time.Second
}

// StrikeWindow is how long after the server lets someone in, or tells the
// door to open, the door may open without being forced: its strike pulse,
// and its reply timeout for the time the news takes to come and go.
func (c DoorConfig) StrikeWindow() time.Duration {
	return time.Duration(c.StrikePulseMS+c.ReplyTimeoutMS) * time.Millisecond
}

// TimelineRefresh is how often the server sends a door its timeline again
// when nothing changed it.
func (c DoorConfig) TimelineRefresh() time.Duration {
	return time.Duration(c.TimelineRefreshHours) * time.Hour
}

// DoorField is one field of a door's configuration. Its site default is
// the setting whose key is "door_" and its name; a door may override it.
// Hint says what values it takes, to follow "must be" or "Enter".
type DoorField struct {
	Name  string
	Label string
	Hint  string
	// Input is how a page asks for the field's value.
	Input Input
	// fallback is the default's default, and set parses a value into a
	// configuration, reporting why it cannot.
	fallback string
	set      func(c *DoorConfig, value string) error
}

// DoorFields is every field of a door's configuration, in display order.
// The ranges of the code lengths meet but do not overlap, so the shortest
// code a door takes is never longer than the longest.
var DoorFields = []DoorField{
	wholeField("strike_pulse_ms", "Strike pulse", "3000", 100, 60000, "milliseconds", func(c *DoorConfig) *int { return &c.StrikePulseMS }),
	wholeField("keypad_idle_clear_ms", "Keypad idle clear", "10000", 1000, 600000, "milliseconds", func(c *DoorConfig) *int { return &c.KeypadIdleClearMS }),
	wholeField("min_code_length", "Shortest code", "4", 1, 16, "digits", func(c *DoorConfig) *int { return &c.MinCodeLength }),
	wholeField("max_code_length", "Longest code", "32", 16, 64, "digits", func(c *DoorConfig) *int { return &c.MaxCodeLength }),
	choiceField("submit_key", "Submit key", "#", []string{"#", "A", "B", "C", "D"}, func(c *DoorConfig) *string { return &c.SubmitKey }),
	wholeField("reply_timeout_ms", "Reply timeout", "3000", 100, 60000, "milliseconds", func(c *DoorConfig) *int { return &c.ReplyTimeoutMS }),
	wholeField("telemetry_interval_s", "Telemetry interval", "60", 5, 86400, "seconds", func(c *DoorConfig) *int { return &c.TelemetryIntervalS }),
	wholeField("held_open_s", "Held open after", "60", 5, 86400, "seconds", func(c *DoorConfig) *int { return &c.HeldOpenS }),
	wholeField("timeline_refresh_hours", "Timeline refresh", "6", 1, 24, "hours", func(c *DoorConfig) *int { return &c.TimelineRefreshHours }),
	wholeField("credential_rotation_days", "Credential rotation", "30", 1, 3650, "days", func(c *DoorConfig) *int { return &c.CredentialRotationDays }),
	wholeField("rate_limit_per_minute", "Code checks a minute", "30", 1, 6000, "checks", func(c *DoorConfig) *int { return &c.RateLimitPerMinute }),
	wholeField("rate_limit_burst", "Code checks in a burst", "10", 1, 1000, "checks", func(c *DoorConfig) *int { return &c.RateLimitBurst }),
	{
		Name: "ntp_server", Label: "Time server", Hint: hostHint, fallback: "pool.ntp.org", set: setHost},
	choiceField("log_level", "Log level", "info", []string{"error", "warn", "info", "debug"}, func(c *DoorConfig) *string { return &c.LogLevel }),
}

// wholeField is a field that takes a whole number of unit from lowest to
// highest, kept where at says.
func wholeField(name, label, fallback string, lowest, highest int, unit string, at func(*DoorConfig) *int) DoorField {
	hint := fmt.Sprintf("a whole number of %s from %d to %d", unit, lowest, highest)
	return DoorField{Name: name, Label: label, Hint: hint, Input: Input{Kind: KindNumber}, fallback: fallback, set: func(c *DoorConfig, value string) error {
		n, err := wholeNumber(value, lowest, highest, "must be "+hint)
		*at(c) = n
		return err
	}}
}

// choiceField is a field that takes one of choices, kept where at says.
func choiceField(name, label, fallback string, choices []string, at func(*DoorConfig) *string) DoorField {
	hint := "one of " + strings.Join(choices, ", ")
	input := Input{Kind: KindChoice, Choices: choices}
	return DoorField{Name: name, Label: label, Hint: hint, Input: input, fallback: fallback, set: func(c *DoorConfig, value string) error {
		*at(c) = value
		if !slices.Contains(choices, value) {
			return errors.New("must be " + hint)
		}
		return nil
	}}
}

// setHost keeps value as the time server when it is a host name.
func setHost(c *DoorConfig, value string) error {
	c.NTPServer = value
	if !isHostName(value) {
		return errors.New("must be " + hostHint)
	}
	return nil
}

// isHostName reports whether value is what hostHint says a host name is.
func isHostName(value string) bool {
	if value == "" || utf8.RuneCountInString(value) > maxHostLength {
		return false
	}
	for _, r := range value {
		if !hostRune(r) {
			return false
		}
	}
	return true
}

// hostRune reports whether r may appear in a host name: a letter, a
// digit, a dot, or a hyphen.
func hostRune(r rune) bool {
	letter := ('a' <= r && r <= 'z') || ('A' <= r && r <= 'Z')
	digit := '0' <= r && r <= '9'
	return letter || digit || r == '.' || r == '-'
}

// doorDefinitions is the setting of each door field's default.
func doorDefinitions() []definition {
	var out []definition
	for _, f := range DoorFields {
		check := func(_ Service, value string) error {
			return f.set(&DoorConfig{}, value)
		}
		out = append(out, definition{key: doorPrefix + f.Name, fallback: f.fallback, validate: rule{check: check, input: f.Input},
			label: f.Label, help: sentence(f.Hint)})
	}
	return out
}

// sentence is text as a sentence: its first letter upper case, ending in a
// full stop.
func sentence(text string) string {
	return strings.ToUpper(text[:1]) + text[1:] + "."
}

// doorField is the door field named name, and whether there is one.
func doorField(name string) (DoorField, bool) {
	i := slices.IndexFunc(DoorFields, func(f DoorField) bool { return f.Name == name })
	if i < 0 {
		return DoorField{}, false
	}
	return DoorFields[i], true
}

// Configures reports whether the setting named key is a door
// configuration default, so changing it changes the configuration of
// every door that does not override it.
func Configures(key string) bool {
	return slices.ContainsFunc(DoorFields, func(f DoorField) bool { return doorPrefix+f.Name == key })
}

// Reschedules reports whether the setting named key changes when the
// schedules of every door are, as the site timezone does.
func Reschedules(key string) bool {
	return key == keySiteTimezone
}

// DoorDefaults is the site's door configuration defaults, each value keyed
// by the name of its field.
type DoorDefaults map[string]string

// DoorDefaults returns the site's door configuration defaults.
func (s Service) DoorDefaults(ctx context.Context) (DoorDefaults, error) {
	all, err := s.List(ctx)
	values := valuesOf(all)
	defaults := DoorDefaults{}
	for _, f := range DoorFields {
		defaults[f.Name] = values[doorPrefix+f.Name]
	}
	return defaults, err
}

// For is the configuration of a door with overrides, each keyed by the
// name of the field it overrides: the override of each field that has
// one, the default of the others.
func (d DoorDefaults) For(overrides map[string]string) (DoorConfig, error) {
	var c DoorConfig
	for _, f := range DoorFields {
		if err := f.set(&c, d.Value(overrides, f.Name)); err != nil {
			return DoorConfig{}, fmt.Errorf("%w for %s: %w", ErrInvalid, doorPrefix+f.Name, err)
		}
	}
	return c, nil
}

// Value is the value of the field named name for a door with overrides:
// its override when it has one, the default otherwise.
func (d DoorDefaults) Value(overrides map[string]string, name string) string {
	if value, overridden := overrides[name]; overridden {
		return value
	}
	return d[name]
}

// CheckOverrides reports what is wrong with a door's configuration
// overrides, each keyed by the name of the field it overrides: a name no
// field has, or a value the field does not take.
func CheckOverrides(overrides map[string]string) error {
	var found check.List
	for _, name := range slices.Sorted(maps.Keys(overrides)) {
		found.Add(name, overrideProblem(name, overrides[name]))
	}
	return found.Err()
}

func overrideProblem(name, value string) string {
	f, ok := doorField(name)
	if !ok {
		return "No door setting has this name."
	}
	if f.set(&DoorConfig{}, value) != nil {
		return "Enter " + f.Hint + "."
	}
	return ""
}
