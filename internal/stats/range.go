// Package stats sums up what the dashboard shows over a range of days:
// the doors' entries, their contact with the server and their telemetry,
// and the firmware they run. The store does the counting; this package
// fills in what it leaves out, such as days without entries, and puts
// the figures in the order the dashboard shows them.
package stats

import (
	"fmt"
	"time"

	"github.com/dakriy/doorcode/internal/check"
	"github.com/dakriy/doorcode/internal/schedule"
)

// Preset is a range the dashboard offers by name: the last Days days,
// today included.
type Preset struct {
	Name  string
	Label string
	Days  int
}

// Presets is every preset, in the order the dashboard offers them.
var Presets = []Preset{
	{"today", "Today", 1},
	{"7d", "7 days", 7},
	{"30d", "30 days", 30},
	{"90d", "90 days", 90},
}

const (
	// DefaultPreset is the range shown unless another is asked for.
	DefaultPreset = "7d"
	// Custom names a range of days chosen by their dates.
	Custom = "custom"
	// MaxDays is the longest custom range.
	MaxDays = 366
)

// The problems with a range that cannot be read.
const (
	UnknownPreset = "Choose today, 7d, 30d, or 90d."
	DateNeeded    = "Enter a date, such as 2026-03-01."
	BadDate       = "Enter a date as year, month, and day, such as 2026-03-01."
	Backwards     = "Choose a day on or after the first."
)

// TooLong is the problem with a custom range of more than MaxDays days.
var TooLong = fmt.Sprintf("Choose at most %d days.", MaxDays)

// Choice is a range as asked for: the name of a preset, or the first and
// last dates of a custom range, written as schedule.DateLayout.
type Choice struct {
	Preset string
	From   string
	To     string
}

// Range is a range of whole days in the site timezone: its preset's name
// or Custom, its first and last days (dates, as midnight UTC on the day
// they name), and the instants it starts and ends, at the start of its
// first day and of the day after its last.
type Range struct {
	Preset string
	First  time.Time
	Last   time.Time
	From   time.Time
	To     time.Time
}

// Days is how many days r spans.
func (r Range) Days() int {
	return int(r.Last.Sub(r.First).Hours()/24) + 1
}

// Choose returns the range c asks for when it is now in zone: a custom
// range when c names a date, else its preset, the default when it names
// none. It reports check.Problems for the fields range, from, and to.
func Choose(c Choice, now time.Time, zone *time.Location) (Range, error) {
	if c.From != "" || c.To != "" {
		return custom(c, zone)
	}
	name := c.Preset
	if name == "" {
		name = DefaultPreset
	}
	for _, p := range Presets {
		if p.Name == name {
			today := schedule.DateOf(now, zone)
			return within(p.Name, today.AddDate(0, 0, 1-p.Days), today, zone), nil
		}
	}
	return Range{}, check.Problems{{Field: "range", Message: UnknownPreset}}
}

// custom is the range from c.From through c.To.
func custom(c Choice, zone *time.Location) (Range, error) {
	var problems check.List
	first, fromProblem := dateOf(c.From)
	last, toProblem := dateOf(c.To)
	problems.Add("from", fromProblem)
	problems.Add("to", toProblem)
	if err := problems.Err(); err != nil {
		return Range{}, err
	}
	r := within(Custom, first, last, zone)
	if r.Last.Before(r.First) {
		problems.Add("to", Backwards)
	} else if r.Days() > MaxDays {
		problems.Add("to", TooLong)
	}
	return r, problems.Err()
}

// dateOf reads value as a date, and says what is wrong when it cannot.
func dateOf(value string) (time.Time, string) {
	if value == "" {
		return time.Time{}, DateNeeded
	}
	day, err := time.Parse(schedule.DateLayout, value)
	if err != nil {
		return time.Time{}, BadDate
	}
	return day, ""
}

// within is the range named preset from first through last in zone.
func within(preset string, first, last time.Time, zone *time.Location) Range {
	return Range{
		Preset: preset, First: first, Last: last,
		From: schedule.StartOf(first, zone), To: schedule.StartOf(last.AddDate(0, 0, 1), zone),
	}
}
