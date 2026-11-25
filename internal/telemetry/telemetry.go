// Package telemetry follows the doors' state from what their controllers
// report: their telemetry, kept as history with hourly rollups, the door
// opening and closing, how long it was open, the alarm that raises (a door
// held open), controllers starting, and controllers that have gone
// unresponsive.
package telemetry

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/dakriy/doorcode/internal/alert"
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
	Store  Store
	Doors  door.Service
	Events event.Service
	Clock  connector.Clock
	// Alerts is told when a door is held open and when it closes, and
	// follows the conditions the periodic work finds a door
	// in.
	Alerts alert.Service
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
	totals := totalsOf(m)
	t := connector.StoredTelemetry{
		At: now, Signal: m.Signal, LatencyMS: m.LatencyMS, UptimeS: m.UptimeS, FreeBytes: m.FreeBytes,
		FirmwareVersion: m.FirmwareVersion, DoorOpen: m.DoorOpen,
		MeshLayer: m.MeshLayer, MeshParent: m.MeshParent, MeshChildren: m.MeshChildren,
		StoredCounts: counted(totals, totalsBefore(held, m.UptimeS)),
		StoredLink:   connector.StoredLink{WifiDropReason: m.WifiDropReason, BSSID: m.BSSID, Channel: m.Channel},
		ConfigDigest: m.ConfigDigest, Totals: totals,
	}
	if err := s.Store.AddSample(ctx, connector.StoredSample{DoorID: d.ID, StoredTelemetry: t}); err != nil {
		return err
	}
	c := heard(held, now)
	c.Telemetry, c.FirmwareVersion, c.Signal = &t, m.FirmwareVersion, m.Signal
	c.ConfigMismatchSince = mismatchSince(d, held, t)
	if sent := d.SentConfig; sent != nil && t.ConfigDigest == sent.Digest {
		c.ConfigStarts, c.ConfigStartsSince = 0, nil
	}
	if m.DoorOpen {
		if err := s.checkOpened(ctx, d.ID, held, nil, now); err != nil {
			return err
		}
		c = opened(c, now)
	} else {
		if err := s.checkClosed(ctx, d.ID, held, nil, now); err != nil {
			return err
		}
		c = closed(c)
	}
	return s.save(ctx, d, held, c, now)
}

// doorEvent handles what the controller of the door whose id is doorID
// says happened to the door.
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
		if err := s.checkOpened(ctx, d.ID, held, m.Unlocked, now); err != nil {
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
		c.ConfigStarts, c.ConfigStartsSince = startsAfter(d, held, now)
	case protocol.EventConfigReverted:
		detail := "The door could not come online with configuration " + m.ConfigDigest + " and went back to the one it ran before."
		if _, err := s.Events.Record(ctx, alarm(now, d.ID, event.ConfigReverted, detail)); err != nil {
			return err
		}
		c.ConfigRevertedDigest, c.ConfigRevertedAt = m.ConfigDigest, &now
	default:
		return fmt.Errorf("%w %q", ErrUnknownEvent, m.Event)
	}
	return s.save(ctx, d, held, c, now)
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
// starting, at at, for reason, which it keeps as given; its detail says
// the reason in words when the server knows it, else as given, and
// nothing when it is empty.
func Started(at time.Time, doorID int64, reason string) connector.StoredEvent {
	detail, known := startedWhy[reason]
	if !known {
		detail = reason
	}
	return connector.StoredEvent{At: at, DoorID: doorID, Type: event.Door, Outcome: event.Info, Reason: event.Started, Detail: detail, StartReason: reason}
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

// save stores the state of c as that of the controller of d, which was
// held, at now, resolves the alerts its door closing clears, and says
// what changed. It stores only the state, so a change to the
// controller's credentials or connection made meanwhile stands.
func (s Service) save(ctx context.Context, d connector.StoredDoor, held, c connector.StoredController, now time.Time) error {
	if _, err := s.Store.SetDoorState(ctx, d.ID, c.StoredDoorState); err != nil {
		return err
	}
	if held.HeldOpen && !c.HeldOpen {
		s.Alerts.Resolve(ctx, alert.DoorCondition(alert.HeldOpen, d), now)
	}
	if slices.Equal(badgesAs(d, held), badgesAs(d, c)) && MeshOf(held) == MeshOf(c) {
		s.Reported(d.ID)
	} else {
		s.Changed()
	}
	return nil
}

// badgesAs is what door lists show of d with c as its controller.
func badgesAs(d connector.StoredDoor, c connector.StoredController) []string {
	d.Controller = &c
	return DoorBadges(d)
}

// DoorBadges is what door lists show of the state of d, whose controller
// is enrolled, beyond whether it is connected and where it sits in the
// mesh: its controller's Badges, and whether it runs a configuration
// other than the one d was last sent.
func DoorBadges(d connector.StoredDoor) []string {
	badges := Badges(*d.Controller)
	if badge := ConfigOf(d).Badge(); badge != "" {
		badges = append(badges, badge)
	}
	return badges
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

// checkOpened logs the door whose id is doorID as opened at now, unless
// its controller, which was held, had it open already. When its
// controller said whether the strike was unlocked as the door opened,
// the event's detail says so.
func (s Service) checkOpened(ctx context.Context, doorID int64, held connector.StoredController, unlocked *bool, now time.Time) error {
	if held.DoorState == Open {
		return nil
	}
	_, err := s.Events.Record(ctx, connector.StoredEvent{
		At: now, DoorID: doorID, Type: event.Door, Outcome: event.Info, Reason: event.Opened, Detail: strikeDetail(unlocked),
	})
	return err
}

// strikeDetail is what an opening's event says of unlocked, whether its
// controller said the strike was unlocked as the door opened: nothing
// when it said nothing.
func strikeDetail(unlocked *bool) string {
	if unlocked == nil {
		return ""
	}
	if *unlocked {
		return "Its controller says the strike was unlocked when it opened."
	}
	return "Its controller says the strike was locked when it opened."
}

// checkClosed logs how long the door whose id is doorID was open when it
// closes at now, if its controller, which was held, had it open. The
// length is the openMS its controller said, when it said one, else the
// time since the door opened.
func (s Service) checkClosed(ctx context.Context, doorID int64, held connector.StoredController, openMS *uint64, now time.Time) error {
	if held.DoorState != Open {
		return nil
	}
	length := now.Sub(*held.OpenedAt).Milliseconds()
	if openMS != nil {
		length = int64(*openMS)
	}
	_, err := s.Events.Record(ctx, connector.StoredEvent{
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

// watch logs the doors held open, marks the controllers that have gone
// unresponsive, as current says, and follows the alert conditions each
// door is in. The conditions raised now are read once, so a door whose
// conditions stay as they were costs no store work for them; when they
// cannot be read, the doors are watched without following them.
func (s Service) watch(ctx context.Context, current settings.Current, defaults settings.DoorDefaults) error {
	doors, err := s.Store.CurrentDoors(ctx)
	if err != nil {
		return err
	}
	raised, raisedErr := s.Alerts.Raised(ctx)
	if raisedErr != nil {
		raisedErr = fmt.Errorf("follow the doors' alert conditions: %w", raisedErr)
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
		c, err := s.watchDoor(ctx, d, config.HeldOpen(), current, now)
		if err != nil {
			return err
		}
		if raisedErr == nil {
			d.Controller = &c
			s.follow(ctx, raised, d, current.UnresponsiveAfter, now)
		}
	}
	return raisedErr
}

// watchDoor logs d held open at now as checkHeld says, and marks its
// controller unresponsive when it has not been heard from for as long as
// current says while the broker says it is connected; one that is not is
// shown offline instead. It returns the controller as it stands now.
func (s Service) watchDoor(ctx context.Context, d connector.StoredDoor, heldOpen time.Duration, current settings.Current, now time.Time) (connector.StoredController, error) {
	held := *d.Controller
	c, err := s.checkHeld(ctx, d, held, heldOpen, current.Zone, now)
	if err != nil {
		return c, err
	}
	if c.Online && !now.Before(c.LastSeenAt.Add(current.UnresponsiveAfter)) {
		c.Unresponsive = true
	}
	if c == held {
		return c, nil
	}
	return c, s.save(ctx, d, held, c, now)
}

// badgeKinds is the kind of alert raised while door lists show each
// badge of a door.
var badgeKinds = []struct {
	badge string
	kind  alert.Kind
}{
	{BadgeUnresponsive, alert.Unresponsive},
	{BadgeRevertedConfig, alert.ConfigReverted},
	{BadgeRestartingConfig, alert.ConfigRestarting},
	{BadgeMismatchedConfig, alert.ConfigMismatch},
}

// follow keeps the alert conditions d is in at now as raised saw them:
// offline once its controller has not been connected for silent, and
// each kind of badgeKinds while door lists show its badge.
func (s Service) follow(ctx context.Context, raised alert.Raised, d connector.StoredDoor, silent time.Duration, now time.Time) {
	c := *d.Controller
	offline := !c.Online && !now.Before(c.LastSeenAt.Add(silent))
	s.Alerts.Follow(ctx, raised, alert.DoorCondition(alert.Offline, d), offline, now, heardFrom(c)+" and is not connected.")
	badges := DoorBadges(d)
	for _, k := range badgeKinds {
		s.Alerts.Follow(ctx, raised, alert.DoorCondition(k.kind, d), slices.Contains(badges, k.badge), now, badgeDetail(d, k.badge))
	}
}

// heardFrom is what alert mail says of when c was last heard from.
func heardFrom(c connector.StoredController) string {
	return "Its controller was last heard from " + alert.Stamp(c.LastSeenAt)
}

// badgeDetail is what alert mail says of d showing badge.
func badgeDetail(d connector.StoredDoor, badge string) string {
	if badge == BadgeUnresponsive {
		return heardFrom(*d.Controller) + ", though the broker says it is connected."
	}
	return configDetail(d, ConfigOf(d))
}

// configDetail is what alert mail says of check, how what the controller
// of d runs stands against the configuration d was last sent, when door
// lists flag it, and nothing otherwise.
func configDetail(d connector.StoredDoor, check ConfigCheck) string {
	switch check.State {
	case ConfigReverted:
		return fmt.Sprintf("At %s it gave up configuration %s, the one it was last sent, because it could not come online with it, and went back to the one it ran before.",
			alert.Stamp(*check.Since), d.SentConfig.Digest)
	case ConfigRestarting:
		return fmt.Sprintf("It started %d times from %s on without reporting configuration %s, the one it was last sent.",
			check.Starts, alert.Stamp(*check.Since), d.SentConfig.Digest)
	case ConfigOlder:
		return fmt.Sprintf("It has reported configuration %s since %s, not %s, the one it was last sent.",
			d.Controller.Telemetry.ConfigDigest, alert.Stamp(*check.Since), d.SentConfig.Digest)
	}
	return ""
}

// checkHeld is c, the controller of d, with its door held open at now or
// not. Only time it should be locked counts: in
// an open span of its timeline, in zone, it is not held open, and after
// one the time counts from the span's end. Once it has been open that
// long for heldOpen, it is logged as held open and its alert raised, once
// until a span or a closing starts the count again.
func (s Service) checkHeld(ctx context.Context, d connector.StoredDoor, c connector.StoredController, heldOpen time.Duration, zone *time.Location, now time.Time) (connector.StoredController, error) {
	if c.DoorState != Open {
		return c, nil
	}
	since, err := s.lockedSince(ctx, d.ID, *c.OpenedAt, zone, now)
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
	if _, err := s.Events.Record(ctx, alarm(now, d.ID, event.HeldOpen, detail)); err != nil {
		return c, err
	}
	s.Alerts.Raise(ctx, alert.DoorCondition(alert.HeldOpen, d), now, detail)
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
	if err != nil {
		return openedAt, err
	}
	since := openedAt
	for _, span := range spans {
		if span.State == timeline.Open {
			since = span.End
		}
	}
	return since, nil
}
