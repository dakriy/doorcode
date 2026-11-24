package app

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/dakriy/doorcode/internal/auth"
	"github.com/dakriy/doorcode/internal/connector"
	"github.com/dakriy/doorcode/internal/controller"
	"github.com/dakriy/doorcode/internal/decision"
	"github.com/dakriy/doorcode/internal/event"
	"github.com/dakriy/doorcode/internal/firmware"
	"github.com/dakriy/doorcode/internal/override"
	"github.com/dakriy/doorcode/internal/protocol"
	"github.com/dakriy/doorcode/internal/timeline"
)

// keypadReason is the reason of an override a keypad command makes.
const keypadReason = "Keypad command"

// errDeletedDoor reports a code check for a deleted door.
var errDeletedDoor = errors.New("the door is deleted")

// checkCode answers the code check the door whose id is doorID published
// with what the decision chain decides, carrying out a keypad command it
// allows, and logs it as an event. An answer that cannot be sent is
// logged as denied, the door being unreachable.
func (a *App) checkCode(ctx context.Context, doorID int64, payload []byte) error {
	var m protocol.Check
	if err := protocol.Decode(payload, &m); err != nil {
		return err
	}
	d, err := a.doors.Door(ctx, doorID)
	if err != nil {
		return err
	}
	// Only a door's own controller can publish its checks, and deleting
	// the door stopped its credentials working, so a check for a deleted
	// door was sent before then and is not answered.
	if d.DeletedAt != nil {
		return errDeletedDoor
	}
	config, _, err := a.effectiveConfig(ctx, d)
	if err != nil {
		return err
	}
	v, err := a.decisions.Decide(ctx, d, config, m.Digits)
	if err != nil {
		return err
	}
	if v.Command != nil {
		v = a.carryOut(ctx, d, v)
	}
	reply := protocol.Reply{RequestID: m.RequestID, Allow: v.Allow}
	if v.Allow {
		reply.StrikeMS = config.StrikePulseMS
	}
	if err := a.broker.Publish(ctx, connector.Message{Topic: protocol.DoorTopic(d.ID, protocol.LeafReply), Payload: protocol.Encode(&reply)}); err != nil {
		// The door never heard the decision, so nobody got in; the rule
		// that decided it stays on the event.
		v.Allow, v.Reason = false, event.Unreachable
	}
	if err := a.rebootIfCommanded(ctx, d, v); err != nil {
		return err
	}
	_, err = a.events.Record(ctx, checkEvent(a.clock.Now(), d.ID, m.RequestID, v))
	return err
}

// checkEvent is the event of a code check, at the door whose id is doorID
// with the request id requestID, that v decided at at.
func checkEvent(at time.Time, doorID int64, requestID string, v decision.Verdict) connector.StoredEvent {
	e := connector.StoredEvent{At: at, DoorID: doorID, Type: event.Check, Outcome: outcome(v.Allow), Reason: v.Reason, RequestID: requestID}
	if v.User != nil {
		e.UserID = &v.User.ID
	}
	if v.RuleID != 0 {
		e.RuleID = &v.RuleID
	}
	return e
}

func outcome(allowed bool) string {
	if allowed {
		return event.Allowed
	}
	return event.Denied
}

// carryOut carries out the keypad command v allows at d, as v's user, and
// logs it as an event. It returns v, or v denied when the command could
// not be carried out, which is logged. A reboot is only checked here; it
// is sent once the door has its reply.
func (a *App) carryOut(ctx context.Context, d connector.StoredDoor, v decision.Verdict) decision.Verdict {
	if v.Command.Kind == decision.Reboot {
		return a.mayReboot(ctx, d.ID, v)
	}
	doorID, commander := d.ID, v.User.ID
	reason, detail, err := a.keypadCommand(ctx, doorID, commander, *v.Command)
	if err == nil {
		_, err = a.events.Record(ctx, connector.StoredEvent{
			At: a.clock.Now(), DoorID: doorID, Type: event.Command, Outcome: event.Allowed, Reason: reason, UserID: &commander, Detail: detail,
		})
	}
	if err != nil {
		return a.commandFailed(doorID, v, err)
	}
	return v
}

// commandFailed is v denied, its keypad command at the door whose id is
// doorID not carried out for err, which is logged.
func (a *App) commandFailed(doorID int64, v decision.Verdict, err error) decision.Verdict {
	a.log.Error("could not carry out a keypad command",
		connector.Field{Key: "door", Value: fmt.Sprint(doorID)}, connector.Field{Key: "error", Value: err.Error()})
	return v.Denied(event.CommandFailed)
}

// keypadRebootMinutes is how long after a reboot from its keypad a door
// can be rebooted from its keypad again, so that a keypad cannot hold its
// door in a loop of restarts.
const keypadRebootMinutes = 10

// mayReboot is v, a keypad reboot of the door whose id is doorID, denied
// while the door is installing firmware, which a restart would cut
// short, or when it was rebooted from its keypad no more than
// keypadRebootMinutes ago.
func (a *App) mayReboot(ctx context.Context, doorID int64, v decision.Verdict) decision.Verdict {
	df, err := a.firmware.Door(ctx, doorID)
	if err != nil {
		return a.commandFailed(doorID, v, err)
	}
	if df.Status == firmware.Updating {
		return v.Denied(event.UpdateUnderWay)
	}
	since := a.clock.Now().Add(-keypadRebootMinutes * time.Minute)
	recent, err := a.events.Find(ctx, event.Query{
		EventQuery: connector.EventQuery{DoorID: doorID, Type: event.Command, Reason: event.KeypadReboot, From: &since}, Page: 1, PerPage: 1,
	})
	if err != nil {
		return a.commandFailed(doorID, v, err)
	}
	if recent.Total > 0 {
		return v.Denied(event.RebootTooSoon)
	}
	return v
}

// rebootIfCommanded tells d's controller to restart when v allowed a
// keypad reboot and the door was told so, and logs it as an event naming
// the commander: a reboot that could not be sent as denied, the door
// being unreachable.
func (a *App) rebootIfCommanded(ctx context.Context, d connector.StoredDoor, v decision.Verdict) error {
	if !v.Allow || v.Command == nil || v.Command.Kind != decision.Reboot {
		return nil
	}
	e := connector.StoredEvent{
		At: a.clock.Now(), DoorID: d.ID, Type: event.Command, Outcome: event.Allowed, Reason: event.KeypadReboot,
		UserID: &v.User.ID, Detail: "Told to restart and connect again.",
	}
	if err := a.controllers.Reboot(ctx, d); err != nil {
		e.Outcome, e.Reason, e.Detail = event.Denied, event.Unreachable, "Could not be told to restart: "+err.Error()
	}
	_, err := a.events.Record(ctx, e)
	return err
}

// keypadCommand carries out c at the door whose id is doorID as the user
// whose id is commander, and returns the reason and the detail of its
// event.
func (a *App) keypadCommand(ctx context.Context, doorID, commander int64, c decision.Command) (string, string, error) {
	if c.Kind == decision.Cancel {
		ended, err := a.endOverridesInForce(ctx, doorID)
		return event.KeypadCancel, fmt.Sprintf("Overrides in force ended: %d.", ended), err
	}
	now := a.clock.Now()
	f := override.Fields{Kind: c.Kind, Start: now, End: now.Add(time.Duration(c.Minutes) * time.Minute), Reason: keypadReason}
	_, err := a.newOverride(ctx, doorID, commander, f)
	return keypadReasons[c.Kind], fmt.Sprintf("%s for %d minutes.", kindLabels[c.Kind], c.Minutes), err
}

// keypadReasons is the reason of the event of a command that makes an
// override of each kind.
var keypadReasons = map[string]string{timeline.Open: event.KeypadOpen, timeline.Locked: event.KeypadLocked}

// endOverridesInForce ends every override of the door whose id is doorID
// that is in force, and returns how many it ended.
func (a *App) endOverridesInForce(ctx context.Context, doorID int64) (int, error) {
	_, overrides, err := a.overrides.Of(ctx, doorID)
	if err != nil {
		return 0, err
	}
	now := a.clock.Now()
	ended := 0
	for _, o := range overrides {
		if override.Active(o, now) {
			if _, err := a.endOverride(ctx, doorID, o.ID); err != nil {
				return 0, err
			}
			ended++
		}
	}
	return ended, nil
}

// openDoor tells the controller of the door whose id is doorID to release
// its strike for as long as its configuration says, as by, and logs it as
// an event, which it returns. A door whose controller is not connected is
// refused with controller.ErrOffline; one that cannot be sent the command
// is logged as denied, being unreachable, and the error returned.
func (a *App) openDoor(ctx context.Context, doorID int64, by auth.User) (connector.StoredEvent, error) {
	d, err := a.doors.Door(ctx, doorID)
	if err != nil {
		return connector.StoredEvent{}, err
	}
	config, _, err := a.effectiveConfig(ctx, d)
	if err != nil {
		return connector.StoredEvent{}, err
	}
	e := connector.StoredEvent{
		At: a.clock.Now(), DoorID: d.ID, DoorName: d.Name, Type: event.Open, Outcome: event.Allowed, Reason: event.FromUI,
		UserID: &by.ID, UserName: by.DisplayName, Detail: fmt.Sprintf("Strike released for %d ms.", config.StrikePulseMS),
	}
	sendErr := a.controllers.Open(ctx, d, config.StrikePulseMS)
	if errors.Is(sendErr, controller.ErrOffline) {
		return e, sendErr
	}
	if sendErr != nil {
		e.Outcome, e.Reason = event.Denied, event.Unreachable
	}
	id, err := a.events.Record(ctx, e)
	e.ID = id
	return e, cmp.Or(sendErr, err)
}

// rebootDoor tells the controller of the door whose id is doorID to
// restart, as by, and logs it as an event, which it returns. A door whose
// controller is not connected is refused with controller.ErrOffline; one
// that cannot be sent the command is not logged, and the error returned.
func (a *App) rebootDoor(ctx context.Context, doorID int64, by auth.User) (connector.StoredEvent, error) {
	d, err := a.doors.Door(ctx, doorID)
	if err != nil {
		return connector.StoredEvent{}, err
	}
	if err := a.controllers.Reboot(ctx, d); err != nil {
		return connector.StoredEvent{}, err
	}
	e := connector.StoredEvent{
		At: a.clock.Now(), DoorID: d.ID, DoorName: d.Name, Type: event.Command, Outcome: event.Info, Reason: event.RebootSent,
		UserID: &by.ID, UserName: by.DisplayName, Detail: "Told to restart and connect again.",
	}
	e.ID, err = a.events.Record(ctx, e)
	return e, err
}
