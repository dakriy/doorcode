package app

import "time"

// The layouts pages write times and days in.
const (
	// secondLayout is an instant to the second: when an event happened, a
	// firmware image was uploaded, an update was sent, or a door reported.
	secondLayout = "Mon 2 Jan 2006 15:04:05"
	// minuteLayout is an instant to the minute: when an override starts and
	// ends, a controller announced itself, or credentials were issued.
	minuteLayout = "Mon 2 Jan 2006 15:04"
	// requestLayout is when an access request was made, answered, or
	// resolved.
	requestLayout = "2 Jan 2006 15:04"
	// chartLayout is when a chart's range starts and ends.
	chartLayout = "Mon 2 Jan 15:04"
	// occurrenceLayout is when a schedule's window opens and closes.
	occurrenceLayout = "Mon 2 Jan 15:04 MST"
	// localLayout is a calendar event's wall-clock time, without an offset.
	localLayout = "2006-01-02T15:04:05"
	// inputTimeLayout is how a datetime-local input writes a time.
	inputTimeLayout = "2006-01-02T15:04"
	// longDateLayout is a day with its year, and dayLayout one without.
	longDateLayout = "2 Jan 2006"
	dayLayout      = "Mon 2 Jan"
)

// shownAt is the instant t as a page shows it: read in zone, the site
// timezone, and written to layout. Every time a page shows is written by
// it, and the API's JSON keeps times in UTC.
func shownAt(zone *time.Location, t time.Time, layout string) string {
	return t.In(zone).Format(layout)
}
