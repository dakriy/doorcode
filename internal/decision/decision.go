// Package decision decides whether a code entered at a door's keypad opens
// it. The chain, in order: the door's rate limit, an expired or deleted
// user, a keypad command, a door commander, an override in force, the
// door's open mode, an access rule, and otherwise no.
package decision

import (
	"context"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/dakriy/doorcode/internal/auth"
	"github.com/dakriy/doorcode/internal/connector"
	"github.com/dakriy/doorcode/internal/door"
	"github.com/dakriy/doorcode/internal/event"
	"github.com/dakriy/doorcode/internal/rule"
	"github.com/dakriy/doorcode/internal/schedule"
	"github.com/dakriy/doorcode/internal/settings"
	"github.com/dakriy/doorcode/internal/timeline"
)

// Store is the part of the store this package uses.
type Store interface {
	connector.UsersStore
	connector.SchedulesStore
	connector.ZonesStore
	connector.RulesStore
	connector.OverridesStore
}

// Service decides code checks.
type Service struct {
	Store    Store
	Auth     auth.Service
	Settings settings.Service
	Clock    connector.Clock
	Limits   *Limiter
}

// Verdict is what was decided about a code: whether it opens the door, the
// reason (one of the event package's), the user whose code it is, nil for
// a code nobody has, and the access rule that let them in, zero when none
// did. Command is the keypad command to carry out, nil without one.
type Verdict struct {
	Allow   bool
	Reason  string
	User    *connector.StoredUser
	RuleID  int64
	Command *Command
}

// Denied is v denied for reason, keeping whose code it was.
func (v Verdict) Denied(reason string) Verdict {
	v.Allow, v.Reason, v.RuleID, v.Command = false, reason, 0, nil
	return v
}

func (v Verdict) allowed(reason string) Verdict {
	v.Allow, v.Reason = true, reason
	return v
}

// Decide decides whether digits, a code optionally followed by "*" and a
// keypad command, opens d, which runs with config. The steps follow spec
// section 2 in order.
func (s Service) Decide(ctx context.Context, d connector.StoredDoor, config settings.DoorConfig, digits string) (Verdict, error) {
	now := s.Clock.Now()
	// 1. The door's rate limit, before the code is looked at.
	if !s.Limits.Take(d.ID, config.RateLimitPerMinute, config.RateLimitBurst, now) {
		return Verdict{}.Denied(event.RateLimited), nil
	}
	current, err := s.Settings.Current(ctx)
	if err != nil {
		return Verdict{}, err
	}
	code, command, commanded := strings.Cut(digits, "*")
	v, err := s.holder(ctx, code)
	if err != nil {
		return v, err
	}
	known := v.User != nil
	// 2. A known user whose account has ended.
	if known && auth.AccountEnded(*v.User, now) {
		return v.Denied(event.Expired), nil
	}
	commander := known && auth.UserOf(*v.User).Has(auth.RoleDoorCommander)
	// 3. A keypad command.
	if commanded {
		return commandVerdict(v, commander, command), nil
	}
	// 4. A door commander.
	if commander {
		return v.allowed(event.Commander), nil
	}
	return s.byTime(ctx, v, d, current.Zone, now)
}

// holder is the verdict, still undecided, naming the user whose door code
// is code, or no one when nobody has it.
func (s Service) holder(ctx context.Context, code string) (Verdict, error) {
	hash, err := s.Auth.CodeHash(ctx, code)
	if err != nil {
		return Verdict{}, err
	}
	u, found, err := s.Store.UserByCodeHash(ctx, hash)
	if !found {
		return Verdict{}, err
	}
	return Verdict{User: &u}, err
}

// commandVerdict is v allowed to carry out text, a keypad command, when
// the user is a door commander and it is a valid one, and denied
// otherwise.
func commandVerdict(v Verdict, commander bool, text string) Verdict {
	if !commander {
		return v.Denied(event.NotCommander)
	}
	c, ok := parseCommand(text)
	if !ok {
		return v.Denied(event.BadCommand)
	}
	v.Command = &c
	return v.allowed(event.Commanded)
}

// byTime decides the steps that depend on what is in force at d now, with
// schedules read in zone: an override, its open mode, and the access rules
// that let v's user in.
func (s Service) byTime(ctx context.Context, v Verdict, d connector.StoredDoor, zone *time.Location, now time.Time) (Verdict, error) {
	overrides, err := s.Store.DoorOverrides(ctx, d.ID, now)
	if err != nil {
		return v, err
	}
	// 5. A locked, then an open override in force.
	if inForce(overrides, timeline.Locked, now) {
		return v.Denied(event.Locked), nil
	}
	if inForce(overrides, timeline.Open, now) {
		return v.allowed(event.OpenOverride), nil
	}
	zones, err := s.Store.Zones(ctx)
	if err != nil {
		return v, err
	}
	schedules, err := s.Store.Schedules(ctx)
	if err != nil {
		return v, err
	}
	active := func(id int64) bool { return activeAt(schedules, id, zone, now) }
	// 6. One of the open-mode schedules of the door's zones, for any code.
	if slices.ContainsFunc(timeline.OpenScheduleIDs(d, zones), active) {
		return v.allowed(event.OpenMode), nil
	}
	rules, err := s.Store.Rules(ctx)
	if err != nil {
		return v, err
	}
	// 7. An access rule letting the user in now.
	for _, r := range rules {
		if letsIn(r, v.User, d) && active(r.ScheduleID) {
			v.RuleID = r.ID
			return v.allowed(event.AccessRule), nil
		}
	}
	// 8. Nothing else.
	return v.Denied(event.NoRule), nil
}

// inForce reports whether one of overrides of kind is in force at now.
func inForce(overrides []connector.StoredOverride, kind string, now time.Time) bool {
	return slices.ContainsFunc(overrides, func(o connector.StoredOverride) bool { return o.Kind == kind && door.Active(o, now) })
}

// activeAt reports whether the schedule among schedules whose id is id is
// active at now, read in zone.
func activeAt(schedules []connector.StoredSchedule, id int64, zone *time.Location, now time.Time) bool {
	i := slices.IndexFunc(schedules, func(sch connector.StoredSchedule) bool { return sch.ID == id })
	return i >= 0 && len(schedule.Expand(schedules[i], zone, now, now.Add(time.Nanosecond))) > 0
}

// letsIn reports whether r names one of u's groups and reaches d through
// one of its zones; it
// names none of a code nobody has.
func letsIn(r connector.StoredRule, u *connector.StoredUser, d connector.StoredDoor) bool {
	if u == nil {
		return false
	}
	_, reaches := rule.ReachOf(r, d)
	return reaches && slices.ContainsFunc(u.Groups, func(g connector.UserGroup) bool { return g.ID == r.GroupID })
}

// Cancel is the kind of keypad command that ends the overrides in force.
const Cancel = "cancel"

// opcode starts every keypad command.
const opcode = "00"

// maxMinutes is the longest override a keypad command makes: a day.
const maxMinutes = 1440

// Command is a keypad command: an override of Kind (timeline.Open or
// timeline.Locked) for Minutes from now, or, when Kind is Cancel, the end
// of the door's overrides in force.
type Command struct {
	Kind    string
	Minutes int
}

// commandKinds maps the digit after the opcode to the override a command
// makes.
var commandKinds = map[string]string{"1": timeline.Open, "2": timeline.Locked}

// parseCommand reads text, what follows the "*" of a keypad entry, as a
// command, and reports whether it is one: "001" and a number of minutes
// from 1 to 1440 for an open override, "002" and one for a locked
// override, and "000" to cancel the overrides in force.
func parseCommand(text string) (Command, bool) {
	rest, ok := strings.CutPrefix(text, opcode)
	if !ok || rest == "" {
		return Command{}, false
	}
	op, minutes := rest[:1], rest[1:]
	if op == "0" {
		return Command{Kind: Cancel}, minutes == ""
	}
	kind, known := commandKinds[op]
	n, err := strconv.Atoi(minutes)
	inRange := err == nil && n >= 1 && n <= maxMinutes
	return Command{Kind: kind, Minutes: n}, known && inRange
}
