// Package settings resolves the site's settings. A value comes from the
// environment when the matching variable is set, which makes it read-only;
// otherwise from the store when an administrator saved one; otherwise from
// the setting's default.
package settings

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/dakriy/doorcode/internal/connector"
)

// ErrUnknown reports a key that names no setting.
var ErrUnknown = errors.New("unknown setting")

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

type definition struct {
	key      string
	fallback string
}

// definitions is every setting the site has, in display order.
var definitions = []definition{
	{key: "site_name", fallback: "doorcode"},
	{key: "site_timezone", fallback: "UTC"},
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
