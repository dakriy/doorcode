// Package tzdb implements connector.TimeZones over Go's time zone loading.
// It embeds the tz database with time/tzdata, so zones load even where the
// host has no zoneinfo files.
package tzdb

import (
	"time"
	// The embedded tz database, used when the host has none.
	_ "time/tzdata"
)

// Zones loads zones from the tz database.
type Zones struct{}

// Zone loads the zone named name.
func (Zones) Zone(name string) (*time.Location, error) {
	return time.LoadLocation(name)
}
