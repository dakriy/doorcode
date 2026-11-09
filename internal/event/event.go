// Package event keeps the activity log: every code check a door asked
// about, with what was decided and why, every keypad command, every
// opening and reboot from the admin UI, every alarm a door's state raised, and every
// firmware update sent to a door and how it ended.
// Events are only ever added.
package event

import (
	"context"

	"github.com/dakriy/doorcode/internal/connector"
)

// The types of event.
const (
	// Check is a code a door asked the server about.
	Check = "check"
	// Command is a keypad command a door commander gave, or a reboot
	// sent to a door from the admin UI or the API.
	Command = "command"
	// Open is a door opened from the admin UI or the API.
	Open = "open"
	// Alarm is something wrong with a door's state, such as the door
	// forced open.
	Alarm = "alarm"
	// Update is a firmware update sent to a door, or its end.
	Update = "update"
)

// Types is every type, in the order pages list them.
var Types = []string{Check, Command, Open, Alarm, Update}

// The outcomes of an event. An alarm's outcome is Alert, and so is a
// failed firmware update's; an update sent or done is Info, since it
// lets nobody in.
const (
	Allowed = "allowed"
	Denied  = "denied"
	Alert   = "alert"
	Info    = "info"
)

// Outcomes is every outcome, in the order pages list them.
var Outcomes = []string{Allowed, Denied, Alert, Info}

// The reasons a code check was allowed.
const (
	// Commanded is a door commander's valid keypad command.
	Commanded = "command"
	// Commander is a door commander, who gets in at any door.
	Commander = "door commander"
	// OpenOverride is an open override in force.
	OpenOverride = "open override"
	// OpenMode is one of the door's open-mode schedules being active.
	OpenMode = "open mode"
	// AccessRule is an access rule letting one of the user's groups in.
	AccessRule = "access rule"
)

// The reasons a code check was denied.
const (
	// RateLimited is a door asking more often than its rate limit
	// allows; the code was not looked at.
	RateLimited = "rate limited"
	// Expired is a user whose account has expired or is deleted.
	Expired = "expired"
	// NotCommander is a keypad command from someone who is not a door
	// commander.
	NotCommander = "not a commander"
	// BadCommand is a keypad command that means nothing.
	BadCommand = "bad command"
	// CommandFailed is a valid keypad command the server could not carry
	// out.
	CommandFailed = "command failed"
	// Locked is a locked override in force.
	Locked = "locked"
	// NoRule is nothing letting the person in.
	NoRule = "no rule"
	// Unreachable is a door the server could not send its answer or
	// command to.
	Unreachable = "unreachable"
)

// The reasons of keypad command events: what the command did.
const (
	// KeypadOpen created an open override.
	KeypadOpen = "keypad open"
	// KeypadLocked created a locked override.
	KeypadLocked = "keypad locked"
	// KeypadCancel ended the overrides in force.
	KeypadCancel = "keypad cancel"
)

// FromUI is the reason of an opening from the admin UI or the API.
const FromUI = "admin UI"

// RebootSent is the reason of a reboot command sent to a door from the
// admin UI or the API.
const RebootSent = "reboot sent"

// The reasons of alarms.
const (
	// ForcedOpen is a door opened when nothing had unlocked it.
	ForcedOpen = "forced open"
	// HeldOpen is a door open for longer than its configuration allows.
	HeldOpen = "held open"
)

// The reasons of firmware update events.
const (
	// UpdateSent is an update command sent to a door.
	UpdateSent = "update sent"
	// UpdateSucceeded is a door connecting again with the version it was
	// told to install.
	UpdateSucceeded = "update succeeded"
	// UpdateFailed is a door saying it could not install the version, or
	// not connecting with it in time.
	UpdateFailed = "update failed"
)

// Reasons is every reason, in the order pages list them.
var Reasons = []string{
	AccessRule, OpenMode, OpenOverride, Commander, Commanded,
	NoRule, Locked, Expired, RateLimited, NotCommander, BadCommand, CommandFailed, Unreachable,
	KeypadOpen, KeypadLocked, KeypadCancel, RebootSent, FromUI, ForcedOpen, HeldOpen,
	UpdateSent, UpdateSucceeded, UpdateFailed,
}

// Service records and finds events.
type Service struct {
	Store connector.EventsStore
}

// Query selects a page of events: the filter of a connector.EventQuery,
// and the page, from 1, of PerPage events.
type Query struct {
	connector.EventQuery
	Page    int
	PerPage int
}

// Page is a page of events and how many events the query selects in all.
type Page struct {
	Events []connector.StoredEvent
	Total  int
}

// Record adds e to the log, and returns the id it was given.
func (s Service) Record(ctx context.Context, e connector.StoredEvent) (int64, error) {
	return s.Store.AddEvent(ctx, e)
}

// Find returns the page of events q selects, newest first.
func (s Service) Find(ctx context.Context, q Query) (Page, error) {
	filter := q.EventQuery
	filter.Offset, filter.Limit = (q.Page-1)*q.PerPage, q.PerPage
	events, total, err := s.Store.Events(ctx, filter)
	return Page{Events: events, Total: total}, err
}
