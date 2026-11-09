// Package tzdb implements connector.TimeZones over Go's time zone loading.
// It embeds the tz database with time/tzdata, so zones load even where the
// host has no zoneinfo files, and the names of its zones in zones.txt,
// written by `make zones` from the copy the Go toolchain ships.
package tzdb

import (
	_ "embed"
	"strings"
	"time"

	// The embedded tz database, used when the host has none.
	_ "time/tzdata"
)

// names is every zone of the embedded tz database, one per line.
//
//go:embed zones.txt
var names string

// Zones loads zones from the tz database.
type Zones struct{}

// Zone loads the zone named name.
func (Zones) Zone(name string) (*time.Location, error) {
	return time.LoadLocation(name)
}

// List returns the name of every zone of the embedded tz database.
func (Zones) List() []string {
	return strings.Fields(names)
}
