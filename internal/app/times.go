package app

import "time"

// shownAt is the instant t as a page shows it: read in zone, the site
// timezone, and written to layout. Every time a page shows is written by
// it, and the API's JSON keeps times in UTC.
func shownAt(zone *time.Location, t time.Time, layout string) string {
	return t.In(zone).Format(layout)
}
