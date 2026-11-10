// Package telemetry follows the doors' state from what their controllers
// report: their telemetry, kept as history with hourly rollups, the door
// opening and closing, how long it was open, the alarms that raises (a door
// forced open or held open), controllers starting, and controllers that
// have gone unresponsive.
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
		MeshLayer: m.MeshLayer, MeshParent: m.MeshParent, MeshChildren: m.MeshChildren,
	}
	if err := s.Store.AddSample(ctx, connector.StoredSample{DoorID: d.ID, StoredTelemetry: t}); err != nil {
		return err
	}
	c := heard(held, now)
	c.Telemetry, c.FirmwareVersion, c.Signal = &t, m.FirmwareVersion, m.Signal
	if m.DoorOpen {
		c = opened(c, now)
	} else {
		if err := s.checkClosed(ctx, d.ID, held, nil, now); err != nil {
			return err
		}
		c = closed(c)
	}
	return s.save(ctx, d.ID, held, c)
}

// doorEvent handles what the controller of the door whose id is doorID
// says happened to the door. A door that opens locked is logged as
// forced open.
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
		if err := s.checkForced(ctx, d, held, m.Unlocked, now); err != nil {
			return err
		}
		c = opened(c, now)
	case protocol.EventClosed:
		if err := s.checkClosed(ctx, d.ID, held, m.OpenMS, now); err != nil {
			return err
		}
		c = closed(c)
	case protocol.EventStarted:
		if _, err := s.Events.Record(ctx, Started(now, d.ID, m.Reason)); err != nil {
			return err
		}
	default:
		return fmt.Errorf("%w %q", ErrUnknownEvent, m.Event)
	}
	return s.save(ctx, d.ID, held, c)
}

// startedWhy is, in words, each reason a controller gives for why it
// started.
var startedWhy = map[string]string{
	protocol.StartPowerOn:  "Power on.",
	protocol.StartSoftware: "A restart its firmware asked for.",
	protocol.StartPanic:    "Its firmware panicked.",
	protocol.StartWatchdog: "Its watchdog reset it.",
	protocol.StartBrownout: "A brownout: its supply voltage dropped too low.",
	protocol.StartExternal: "Its reset pin.",
	protocol.StartUnknown:  "It does not know why.",
}

// Started is the event of the controller of the door whose id is doorID
// starting, at at, for reason, said in words when the server knows it,
// else as given, and not at all when it is empty.
func Started(at time.Time, doorID int64, reason string) connector.StoredEvent {
	detail, known := startedWhy[reason]
	if !known {
		detail = reason
	}
	return connector.StoredEvent{At: at, DoorID: doorID, Type: event.Door, Outcome: event.Info, Reason: event.Started, Detail: detail}
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

// save stores the state of c as that of the controller of the door whose
// id is id, which was held, and says what changed. It stores only the
// state, so a change to the controller's credentials or connection made
// meanwhile stands.
func (s Service) save(ctx context.Context, id int64, held, c connector.StoredController) error {
	if _, err := s.Store.SetDoorState(ctx, id, c.StoredDoorState); err != nil {
		return err
	}
	if slices.Equal(Badges(held), Badges(c)) && MeshOf(held) == MeshOf(c) {
		s.Reported(id)
	} else {
		s.Changed()
	}
	return nil
}

// Badges is what door lists show of the state of the door c is enrolled
// as, beyond whether it is connected and where it sits in the mesh.
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

// Mesh is where a controller last said it sits in the site's mesh: its
// Layer, 0 when it reaches the building's access point itself, and the
// hardware id of the controller it hops through, its Parent.
type Mesh struct {
	Layer  int
	Parent string
}

// MeshOf is where c last said it sits in the mesh, at layer 0 until its
// first report.
func MeshOf(c connector.StoredController) Mesh {
	if c.Telemetry == nil {
		return Mesh{}
	}
	return Mesh{Layer: c.Telemetry.MeshLayer, Parent: c.Telemetry.MeshParent}
}

// checkForced logs d as forced open at now, unless its door, whose
// controller was held, was open already or was unlocked when it opened.
// Whether it was unlocked is what its controller said, when it said
// anything, else what checkWindow finds.
func (s Service) checkForced(ctx context.Context, d connector.StoredDoor, held connector.StoredController, said *bool, now time.Time) error {
	if held.DoorState == Open {
		return nil
	}
	if said == nil {
		return s.checkWindow(ctx, d, now)
	}
	if *said {
		return nil
	}
	return s.forced(ctx, d.ID, now, "Its controller says it was locked when it opened.")
}

// checkWindow logs d as forced open at now unless something unlocked it
// within its strike window before now.
func (s Service) checkWindow(ctx context.Context, d connector.StoredDoor, now time.Time) error {
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
	return s.forced(ctx, d.ID, now, fmt.Sprintf("Nothing unlocked it in the %g seconds before it opened.", window.Seconds()))
}

// forced logs the door whose id is doorID as forced open at now, with
// detail.
func (s Service) forced(ctx context.Context, doorID int64, now time.Time, detail string) error {
	_, err := s.Events.Record(ctx, alarm(now, doorID, event.ForcedOpen, detail))
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
	return s.openDuring(ctx, doorID, since, now)
}

// openDuring reports whether the timeline of the door whose id is doorID
// holds it open at any time from from up to to.
func (s Service) openDuring(ctx context.Context, doorID int64, from, to time.Time) (bool, error) {
	current, err := s.Settings.Current(ctx)
	if err != nil {
		return false, err
	}
	_, spans, err := s.Doors.Timeline(ctx, doorID, current.Zone, from, to)
	return slices.ContainsFunc(spans, func(span timeline.Span) bool { return span.State == timeline.Open }), err
}

// checkClosed logs how long the door whose id is doorID was open when it
// closes at now, if its controller, which was held, had it open and the
// opening did not begin while its timeline held it open. The length is
// the openMS its controller said, when it said one, else the time since
// the door opened.
func (s Service) checkClosed(ctx context.Context, doorID int64, held connector.StoredController, openMS *uint64, now time.Time) error {
	if held.DoorState != Open {
		return nil
	}
	since := *held.OpenedAt
	// The opening began in an open span when the timeline holds the door
	// open at the instant it opened, the shortest range from since.
	spanned, err := s.openDuring(ctx, doorID, since, since.Add(time.Nanosecond))
	if err != nil || spanned {
		return err
	}
	length := now.Sub(since).Milliseconds()
	if openMS != nil {
		length = int64(*openMS)
	}
	_, err = s.Events.Record(ctx, connector.StoredEvent{
		At: now, DoorID: doorID, Type: event.Door, Outcome: event.Info, Reason: event.Closed, OpenMS: &length,
	})
	return err
}

// alarm is the alarm of the door whose id is doorID at at, for reason.
func alarm(at time.Time, doorID int64, reason, detail string) connector.StoredEvent {
	return connector.StoredEvent{At: at, DoorID: doorID, Type: event.Alarm, Outcome: event.Alert, Reason: reason, Detail: detail}
}

// Sweep does what time makes due: a door open longer than its held-open
// time, by defaults and its own overrides, while it should be locked is
// logged as held open, a controller not heard from for as long as current
// says is marked unresponsive, the reports of every hour that has ended
// are rolled up, and reports and rollups older than current keeps them
// are removed. A part that fails does not stop the others.
func (s Service) Sweep(ctx context.Context, current settings.Current, defaults settings.DoorDefaults) error {
	return errors.Join(s.watch(ctx, current, defaults), s.rollUp(ctx), s.purge(ctx, current))
}

// watch logs the doors held open and marks the controllers that have gone
// unresponsive, as current says.
func (s Service) watch(ctx context.Context, current settings.Current, defaults settings.DoorDefaults) error {
	doors, err := s.Store.Doors(ctx)
	if err != nil {
		return err
	}
	now := s.Clock.Now()
	for _, d := range doors {
		if !door.Enrolled(d) {
			continue
		}
		config, err := defaults.For(d.Config)
		if err != nil {
			return err
		}
		if err := s.watchDoor(ctx, d, config.HeldOpen(), current, now); err != nil {
			return err
		}
	}
	return nil
}

// watchDoor logs d held open at now as checkHeld says, and marks its
// controller unresponsive when it has not been heard from for as long as
// current says while the broker says it is connected; one that is not is
// shown offline instead.
func (s Service) watchDoor(ctx context.Context, d connector.StoredDoor, heldOpen time.Duration, current settings.Current, now time.Time) error {
	held := *d.Controller
	c, err := s.checkHeld(ctx, d.ID, held, heldOpen, current.Zone, now)
	if err != nil {
		return err
	}
	if c.Online && !now.Before(c.LastSeenAt.Add(current.UnresponsiveAfter)) {
		c.Unresponsive = true
	}
	if c == held {
		return nil
	}
	return s.save(ctx, d.ID, held, c)
}

// checkHeld is c, the controller of the door whose id is doorID, with its
// door held open at now or not. Only time it should be locked counts: in
// an open span of its timeline, in zone, it is not held open, and after
// one the time counts from the span's end. Once it has been open that
// long for heldOpen, it is logged as held open, once until a span or a
// closing starts the count again.
func (s Service) checkHeld(ctx context.Context, doorID int64, c connector.StoredController, heldOpen time.Duration, zone *time.Location, now time.Time) (connector.StoredController, error) {
	if c.DoorState != Open {
		return c, nil
	}
	since, err := s.lockedSince(ctx, doorID, *c.OpenedAt, zone, now)
	if err != nil {
		return c, err
	}
	if since.After(now) {
		c.HeldOpen = false
		return c, nil
	}
	if c.HeldOpen || now.Before(since.Add(heldOpen)) {
		return c, nil
	}
	detail := fmt.Sprintf("Open for longer than %g seconds.", heldOpen.Seconds())
	if _, err := s.Events.Record(ctx, alarm(now, doorID, event.HeldOpen, detail)); err != nil {
		return c, err
	}
	c.HeldOpen = true
	return c, nil
}

// lockedSince is when the door whose id is doorID, open since openedAt,
// was last left by an open span of its timeline in zone, or openedAt when
// none came since; it is after now while a span holds the door open at
// now.
func (s Service) lockedSince(ctx context.Context, doorID int64, openedAt time.Time, zone *time.Location, now time.Time) (time.Time, error) {
	// The timeline runs to just past now, so a span in force at now ends
	// after it.
	_, spans, err := s.Doors.Timeline(ctx, doorID, zone, openedAt, now.Add(time.Nanosecond))
	since := openedAt
	for _, span := range spans {
		if span.State == timeline.Open {
			since = span.End
		}
	}
	return since, err
}
