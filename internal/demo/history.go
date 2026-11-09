package demo

import (
	"context"
	"math"
	"math/rand/v2"
	"slices"
	"time"

	"github.com/dakriy/doorcode/internal/connector"
	"github.com/dakriy/doorcode/internal/event"
)

// historyDays is how far back the demo's history goes.
const historyDays = 28

// sampleDays is how many of the last days keep every telemetry report;
// older days are kept as hourly rollups only, as the retention settings
// leave them.
const sampleDays = 6

// sampleEvery is how often the simulated controllers reported in the
// past.
const sampleEvery = 10 * time.Minute

// The kinds of day a habit happens on.
const (
	everyDay = iota
	workday
	weekend
)

// habit is something people do at a door: who, on which days, between
// which hours, how often a day, and what the server decided, by the rule
// that let them in when one did.
type habit struct {
	door     int64
	users    []int64
	days     int
	from, to int
	perDay   float64
	outcome  string
	reason   string
	rule     int64
}

// The user ids of the demo's people.
const (
	ann   = 2
	ben   = 3
	chloe = 4
	dev   = 5
	eva   = 6
	frank = 7
	hugo  = 9
	ivy   = 10
)

var staffWithCodes = []int64{ann, ben, dev, hugo, ivy}

// unknown is a habit's users when the codes belong to nobody.
var unknown []int64

var habits = []habit{
	{1, staffWithCodes, workday, 7, 9, 6, event.Allowed, event.AccessRule, 1},
	{1, staffWithCodes, workday, 16, 19, 4, event.Allowed, event.AccessRule, 1},
	{2, staffWithCodes, workday, 7, 19, 9, event.Allowed, event.AccessRule, 1},
	{2, staffWithCodes, weekend, 9, 17, 2, event.Allowed, event.AccessRule, 2},
	{1, []int64{chloe}, everyDay, 0, 24, 2, event.Allowed, event.Commander, 0},
	{3, []int64{chloe}, everyDay, 0, 24, 2, event.Allowed, event.Commander, 0},
	{3, []int64{dev}, everyDay, 22, 24, 0.4, event.Allowed, event.AccessRule, 5},
	{4, []int64{frank}, workday, 7, 19, 5, event.Allowed, event.AccessRule, 4},
	{5, []int64{frank}, workday, 7, 19, 3, event.Allowed, event.AccessRule, 4},
	{4, []int64{eva}, workday, 7, 19, 2, event.Allowed, event.AccessRule, 4},
	{5, []int64{dev}, everyDay, 0, 6, 0.5, event.Allowed, event.AccessRule, 5},
	{3, staffWithCodes, workday, 9, 18, 0.7, event.Denied, event.NoRule, 0},
	{4, []int64{ann, ben}, workday, 9, 18, 0.5, event.Denied, event.NoRule, 0},
	{1, staffWithCodes, weekend, 10, 16, 1, event.Denied, event.NoRule, 0},
	{1, unknown, everyDay, 0, 24, 0.5, event.Denied, event.NoRule, 0},
	{2, unknown, everyDay, 0, 24, 0.4, event.Denied, event.NoRule, 0},
	{4, unknown, everyDay, 0, 24, 0.3, event.Denied, event.NoRule, 0},
	{5, unknown, everyDay, 0, 24, 0.6, event.Denied, event.NoRule, 0},
}

// writeHistory writes weeks of events and telemetry into store, as if the
// site had run since site.start.
func writeHistory(ctx context.Context, store connector.Store, site Site, now time.Time) error {
	rng := rand.New(rand.NewPCG(17, 2026))
	events := pastEvents(rng, site.start, now)
	for _, e := range events {
		if _, err := store.AddEvent(ctx, e); err != nil {
			return err
		}
	}
	return writeTelemetry(ctx, store, rng, opensOf(events), site.start, now)
}

// pastEvents is every event of the history, oldest first.
func pastEvents(rng *rand.Rand, start, now time.Time) []connector.StoredEvent {
	expiry := now.AddDate(0, 0, -7)
	var events []connector.StoredEvent
	for day := start.UTC().Truncate(24 * time.Hour); day.Before(now); day = day.AddDate(0, 0, 1) {
		for _, h := range habits {
			for _, at := range h.times(rng, day) {
				events = append(events, h.event(rng, at, expiry))
			}
		}
		events = append(events, occasional(rng, day)...)
	}
	events = append(events, pastUpdates(now)...)
	events = slices.DeleteFunc(events, func(e connector.StoredEvent) bool { return !e.At.Before(now) })
	slices.SortFunc(events, func(a, b connector.StoredEvent) int { return a.At.Compare(b.At) })
	return events
}

// times is when h happens on day: as often as its rate says, at random
// within its hours.
func (h habit) times(rng *rand.Rand, day time.Time) []time.Time {
	weekday := day.Weekday()
	isWeekend := weekday == time.Saturday || weekday == time.Sunday
	if (h.days == workday && isWeekend) || (h.days == weekend && !isWeekend) {
		return nil
	}
	whole, part := math.Modf(h.perDay)
	n := int(whole)
	if rng.Float64() < part {
		n++
	}
	var out []time.Time
	for range n {
		offset := time.Duration(h.from)*time.Hour + time.Duration(rng.Int64N(int64(time.Duration(h.to-h.from)*time.Hour)))
		out = append(out, day.Add(offset).Truncate(time.Second))
	}
	return out
}

// event is one check of h at at by one of its users, chosen at random. A
// user whose account ended at expiry is denied from then on.
func (h habit) event(rng *rand.Rand, at, expiry time.Time) connector.StoredEvent {
	e := connector.StoredEvent{At: at, DoorID: h.door, Type: event.Check, Outcome: h.outcome, Reason: h.reason, RequestID: requestID(rng)}
	if len(h.users) == 0 {
		return e
	}
	user := h.users[rng.IntN(len(h.users))]
	e.UserID = &user
	if user == eva && !at.Before(expiry) {
		e.Outcome, e.Reason = event.Denied, event.Expired
		return e
	}
	if h.rule != 0 {
		rule := h.rule
		e.RuleID = &rule
	}
	return e
}

// occasional is what happens now and then on day: an opening from the UI,
// an alarm, a keypad command, a firmware update.
func occasional(rng *rand.Rand, day time.Time) []connector.StoredEvent {
	var out []connector.StoredEvent
	at := func(from, to int) time.Time {
		return day.Add(time.Duration(from)*time.Hour + time.Duration(rng.Int64N(int64(time.Duration(to-from)*time.Hour)))).Truncate(time.Second)
	}
	admin := int64(1)
	if rng.IntN(3) == 0 {
		out = append(out, connector.StoredEvent{At: at(9, 17), DoorID: 4, Type: event.Open, Outcome: event.Allowed, Reason: event.FromUI,
			UserID: &admin, Detail: "Strike released for 3000 ms."})
	}
	if rng.IntN(5) == 0 {
		out = append(out, connector.StoredEvent{At: at(0, 24), DoorID: 5, Type: event.Alarm, Outcome: event.Alert, Reason: event.ForcedOpen,
			Detail: "Nothing unlocked it in the 6 seconds before it opened."})
	}
	if rng.IntN(4) == 0 {
		out = append(out, connector.StoredEvent{At: at(8, 18), DoorID: 4, Type: event.Alarm, Outcome: event.Alert, Reason: event.HeldOpen,
			Detail: "Open for longer than 60 seconds."})
	}
	if rng.IntN(6) == 0 {
		out = append(out, keypadOpen(rng, at(7, 9))...)
	}
	return out
}

// keypadOpen is Chloe opening the main entrance for half an hour from
// the keypad at at: the command and the check it came with.
func keypadOpen(rng *rand.Rand, at time.Time) []connector.StoredEvent {
	user := int64(chloe)
	return []connector.StoredEvent{
		{At: at, DoorID: 1, Type: event.Command, Outcome: event.Allowed, Reason: event.KeypadOpen, UserID: &user, Detail: "Open for 30 minutes."},
		{At: at, DoorID: 1, Type: event.Check, Outcome: event.Allowed, Reason: event.Commanded, UserID: &user, RequestID: requestID(rng)},
	}
}

// requestID is an id a controller might give a check.
func requestID(rng *rand.Rand) string {
	const hex = "0123456789abcdef"
	out := make([]byte, 8)
	for i := range out {
		out[i] = hex[rng.IntN(len(hex))]
	}
	return string(out)
}

// slot is a door and the start of a sampleEvery-long time.
type slot struct {
	door int64
	at   time.Time
}

// opensOf counts the events that let someone in by door and slot.
func opensOf(events []connector.StoredEvent) map[slot]int {
	opens := map[slot]int{}
	for _, e := range events {
		if e.Outcome == event.Allowed {
			opens[slot{e.DoorID, e.At.Truncate(sampleEvery)}]++
		}
	}
	return opens
}

// pastUpdates is the firmware updates the doors installed before, each
// sent shortly after 02:00 and done minutes later.
func pastUpdates(now time.Time) []connector.StoredEvent {
	var out []connector.StoredEvent
	for _, p := range profiles {
		if p.before == p.after {
			continue
		}
		sent := now.UTC().Truncate(24*time.Hour).AddDate(0, 0, -p.updatedDaysAgo).Add(2*time.Hour + 4*time.Minute)
		detail := "Version " + p.after + "."
		out = append(out,
			connector.StoredEvent{At: sent, DoorID: p.door, Type: event.Update, Outcome: event.Info, Reason: event.UpdateSent, Detail: detail},
			connector.StoredEvent{At: sent.Add(5 * time.Minute), DoorID: p.door, Type: event.Update, Outcome: event.Info, Reason: event.UpdateSucceeded, Detail: detail})
	}
	return out
}
