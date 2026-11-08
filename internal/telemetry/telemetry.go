// Package telemetry follows the doors' state from what their controllers
// report: their telemetry, kept as history with hourly rollups, the door
// opening and closing, the alarms that raises (a door forced open or held
// open), and controllers that have gone unresponsive.
package telemetry

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/dakriy/doorcode/internal/connector"
	"github.com/dakriy/doorcode/internal/controller"
	"github.com/dakriy/doorcode/internal/door"
	"github.com/dakriy/doorcode/internal/event"
	"github.com/dakriy/doorcode/internal/protocol"
	"github.com/dakriy/doorcode/internal/settings"
	"github.com/dakriy/doorcode/internal/timeline"
)

// ErrUnknownEvent reports a door event the server knows nothing of.
var ErrUnknownEvent = errors.New("unknown door event")

// The states of a door, as its controller says them.
const (
	Open   = "open"
	Closed = "closed"
)

// The badges door lists show of a door's state.
const (
	BadgeUnresponsive = "Unresponsive"
	BadgeOpen         = "Open"
	BadgeHeldOpen     = "Held open"
)

// Store is the part of the store this package uses.
type Store interface {
	connector.DoorsStore
	connector.TelemetryStore
}

// Service follows the doors' state.
type Service struct {
	Store    Store
	Doors    door.Service
	Events   event.Service
	Settings settings.Service
	Clock    connector.Clock
	// Changed is called whenever what door lists show of a door's state
	// changes, and Reported with the id of a door whose state changed
	// otherwise.
	Changed  func()
	Reported func(doorID int64)
}

// Receive handles payload, which the controller of the door t names
// published on its telemetry or event topic.
func (s Service) Receive(ctx context.Context, t protocol.Topic, payload []byte) error {
	if t.Leaf == protocol.LeafTelemetry {
		return s.report(ctx, t.DoorID, payload)
	}
	return s.doorEvent(ctx, t.DoorID, payload)
}

// report keeps a telemetry report of the door whose id is doorID, and the
// door's state it tells.
func (s Service) report(ctx context.Context, doorID int64, payload []byte) error {
	var m protocol.Telemetry
	if err := protocol.Decode(payload, &m); err != nil {
		return err
	}
	d, held, err := s.controllerOf(ctx, doorID)
	if err != nil {
		return err
	}
	now := s.Clock.Now()
	t := connector.StoredTelemetry{
		At: now, Signal: m.Signal, LatencyMS: m.LatencyMS, UptimeS: m.UptimeS, FreeBytes: m.FreeBytes,
		FirmwareVersion: m.FirmwareVersion, Reconnects: m.Reconnects, DoorOpen: m.DoorOpen, Opens: m.Opens,
	}
	if err := s.Store.AddSample(ctx, connector.StoredSample{DoorID: d.ID, StoredTelemetry: t}); err != nil {
		return err
	}
	c := heard(held, now)
	c.Telemetry, c.FirmwareVersion, c.Signal = &t, m.FirmwareVersion, m.Signal
	if m.DoorOpen {
		c = opened(c, now)
	} else {
		c = closed(c)
	}
	return s.save(ctx, d.ID, held, c)
}

// doorEvent handles what the controller of the door whose id is doorID
// says happened to the door. A door that opens when nothing unlocked it
// is logged as forced open.
func (s Service) doorEvent(ctx context.Context, doorID int64, payload []byte) error {
	var m protocol.DoorEvent
	if err := protocol.Decode(payload, &m); err != nil {
		return err
	}
	d, held, err := s.controllerOf(ctx, doorID)
	if err != nil {
		return err
	}
	now := s.Clock.Now()
	c := heard(held, now)
	switch m.Event {
	case protocol.EventOpened:
		if err := s.checkForced(ctx, d, held, now); err != nil {
			return err
		}
		c = opened(c, now)
	case protocol.EventClosed:
		c = closed(c)
	default:
		return fmt.Errorf("%w %q", ErrUnknownEvent, m.Event)
	}
	return s.save(ctx, d.ID, held, c)
}

// controllerOf is the door whose id is id and the controller enrolled as
// it.
func (s Service) controllerOf(ctx context.Context, id int64) (connector.StoredDoor, connector.StoredController, error) {
	d, found, err := s.Store.Door(ctx, id)
	if err != nil {
		return d, connector.StoredController{}, err
	}
	if !found || d.Controller == nil {
		return d, connector.StoredController{}, fmt.Errorf("%w %d", controller.ErrUnknownDoor, id)
	}
	return d, *d.Controller, nil
}

// heard is c heard from at now, which makes it responsive.
func heard(c connector.StoredController, now time.Time) connector.StoredController {
	c.LastSeenAt, c.Unresponsive = now, false
	return c
}

// opened is c with its door open, since now unless it was open already.
func opened(c connector.StoredController, now time.Time) connector.StoredController {
	if c.DoorState == Open {
		return c
	}
	c.DoorState, c.OpenedAt, c.HeldOpen = Open, &now, false
	return c
}

// closed is c with its door closed.
func closed(c connector.StoredController) connector.StoredController {
	c.DoorState, c.OpenedAt, c.HeldOpen = Closed, nil, false
	return c
}

// save stores c as the controller of the door whose id is id, which was
// held, and says what changed.
func (s Service) save(ctx context.Context, id int64, held, c connector.StoredController) error {
	if _, err := s.Store.SetController(ctx, id, &c); err != nil {
		return err
	}
	if slices.Equal(Badges(held), Badges(c)) {
		s.Reported(id)
	} else {
		s.Changed()
	}
	return nil
}

// Badges is what door lists show of the state of the door c is enrolled
// as, beyond whether it is connected.
func Badges(c connector.StoredController) []string {
	var out []string
	if c.Unresponsive {
		out = append(out, BadgeUnresponsive)
	}
	if c.HeldOpen {
		return append(out, BadgeHeldOpen)
	}
	if c.DoorState == Open {
		return append(out, BadgeOpen)
	}
	return out
}

// checkForced logs d as forced open at now, unless its door, whose
// controller was held, was open already or something unlocked it within
// its strike window before now.
func (s Service) checkForced(ctx context.Context, d connector.StoredDoor, held connector.StoredController, now time.Time) error {
	if held.DoorState == Open {
		return nil
	}
	defaults, err := s.Settings.DoorDefaults(ctx)
	if err != nil {
		return err
	}
	config, err := defaults.For(d.Config)
	if err != nil {
		return err
	}
	window := config.StrikeWindow()
	unlocked, err := s.unlocked(ctx, d.ID, now.Add(-window), now)
	if err != nil || unlocked {
		return err
	}
	detail := fmt.Sprintf("Nothing unlocked it in the %g seconds before it opened.", window.Seconds())
	_, err = s.Events.Record(ctx, alarm(now, d.ID, event.ForcedOpen, detail))
	return err
}

// unlocked reports whether something unlocked the door whose id is doorID
// from since up to now: a code or a command it was allowed, an opening
// from the admin UI, or its timeline holding it open.
func (s Service) unlocked(ctx context.Context, doorID int64, since, now time.Time) (bool, error) {
	allowed, err := s.Events.Find(ctx, event.Query{
		EventQuery: connector.EventQuery{DoorID: doorID, Outcome: event.Allowed, From: &since}, Page: 1, PerPage: 1,
	})
	if err != nil {
		return false, err
	}
	if allowed.Total > 0 {
		return true, nil
	}
	current, err := s.Settings.Current(ctx)
	if err != nil {
		return false, err
	}
	_, spans, err := s.Doors.Timeline(ctx, doorID, current.Zone, since, now)
	return slices.ContainsFunc(spans, func(span timeline.Span) bool { return span.State == timeline.Open }), err
}

// alarm is the alarm of the door whose id is doorID at at, for reason.
func alarm(at time.Time, doorID int64, reason, detail string) connector.StoredEvent {
	return connector.StoredEvent{At: at, DoorID: doorID, Type: event.Alarm, Outcome: event.Alert, Reason: reason, Detail: detail}
}

// Sweep does what time makes due: a door open longer than its held-open
// time, by defaults and its own overrides, is logged as held open, a
// controller not heard from for as long as current says is marked
// unresponsive, the reports of every hour that has ended are rolled up,
// and reports and rollups older than current keeps them are removed. A
// part that fails does not stop the others.
func (s Service) Sweep(ctx context.Context, current settings.Current, defaults settings.DoorDefaults) error {
	return errors.Join(s.watch(ctx, current.UnresponsiveAfter, defaults), s.rollUp(ctx), s.purge(ctx, current))
}

// watch logs the doors held open and marks the controllers that have gone
// unresponsive after unresponsiveAfter.
func (s Service) watch(ctx context.Context, unresponsiveAfter time.Duration, defaults settings.DoorDefaults) error {
	doors, err := s.Store.Doors(ctx)
	if err != nil {
		return err
	}
	now := s.Clock.Now()
	for _, d := range doors {
		if d.Controller == nil || d.DeletedAt != nil {
			continue
		}
		config, err := defaults.For(d.Config)
		if err != nil {
			return err
		}
		if err := s.watchDoor(ctx, d, config.HeldOpen(), unresponsiveAfter, now); err != nil {
			return err
		}
	}
	return nil
}

// watchDoor logs d held open at now when its door has been open for
// heldOpen, and marks its controller unresponsive when it has not been
// heard from for unresponsiveAfter.
func (s Service) watchDoor(ctx context.Context, d connector.StoredDoor, heldOpen, unresponsiveAfter time.Duration, now time.Time) error {
	held := *d.Controller
	c := held
	if c.DoorState == Open && !c.HeldOpen && !now.Before(c.OpenedAt.Add(heldOpen)) {
		detail := fmt.Sprintf("Open for longer than %g seconds.", heldOpen.Seconds())
		if _, err := s.Events.Record(ctx, alarm(now, d.ID, event.HeldOpen, detail)); err != nil {
			return err
		}
		c.HeldOpen = true
	}
	if !now.Before(c.LastSeenAt.Add(unresponsiveAfter)) {
		c.Unresponsive = true
	}
	if c == held {
		return nil
	}
	return s.save(ctx, d.ID, held, c)
}
