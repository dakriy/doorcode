package firmware

import (
	"cmp"
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/dakriy/doorcode/internal/connector"
	"github.com/dakriy/doorcode/internal/door"
	"github.com/dakriy/doorcode/internal/event"
	"github.com/dakriy/doorcode/internal/protocol"
	"github.com/dakriy/doorcode/internal/schedule"
	"github.com/dakriy/doorcode/internal/settings"
)

var (
	// ErrNoTarget reports a door with no firmware to run: the site has no
	// default and the door names none of its own.
	ErrNoTarget = errors.New("the door has no firmware to run; choose a default version or one for the door")
	// ErrNoAddress reports a site whose public address is not set, so a
	// door cannot be told where to download firmware.
	ErrNoAddress = errors.New("set the public_url setting first, so doors can download firmware")
	// ErrNotAllowed reports a download token that does not let the door
	// download the version: not the one it was sent for this version, or
	// one that has expired or whose update has ended.
	ErrNotAllowed = errors.New("this token does not let the door download this version")
)

// The states of an update.
const (
	Updating  = "updating"
	Succeeded = "succeeded"
	Failed    = "failed"
)

// The statuses of a door in the rollout besides Updating and Failed: it
// waits for an update, or it runs its target.
const (
	Pending = "pending"
	Done    = "done"
)

const (
	// tokenBytes is how much entropy a download token holds.
	tokenBytes = 32
	// minutesPerDay is how many minutes a day has.
	minutesPerDay = 1440
)

// DoorFirmware is where a door stands with its firmware: the firmware it
// runs instead of the default, nil when it follows the default; its
// target, which it should run, nil when there is none; its latest update,
// nil when it was never sent one; and its status in the rollout, empty
// for a door that takes no part: one deleted, without a controller, or
// without a target.
type DoorFirmware struct {
	Door     connector.StoredDoor
	Override *connector.StoredFirmware
	Target   *connector.StoredFirmware
	Update   *connector.StoredUpdate
	Status   string
}

// Plan is every firmware, newest upload first, and where every door
// stands with it, in id order.
type Plan struct {
	Firmware []connector.StoredFirmware
	Doors    []DoorFirmware
}

// Plan returns where every door stands with its firmware.
func (s Service) Plan(ctx context.Context) (Plan, error) {
	all, err := s.Store.Firmwares(ctx)
	if err != nil {
		return Plan{}, err
	}
	doors, err := s.Store.Doors(ctx)
	if err != nil {
		return Plan{}, err
	}
	updates, err := s.Store.DoorUpdates(ctx)
	if err != nil {
		return Plan{}, err
	}
	plan := Plan{Firmware: all}
	fallback := plan.Default()
	for _, d := range doors {
		df := DoorFirmware{Door: d, Target: fallback}
		latest := slices.IndexFunc(updates, func(u connector.StoredUpdate) bool { return u.DoorID == d.ID })
		if latest >= 0 {
			df.Update = &updates[latest]
		}
		if d.FirmwareID != nil {
			i := slices.IndexFunc(all, func(f connector.StoredFirmware) bool { return f.ID == *d.FirmwareID })
			df.Override, df.Target = &all[i], &all[i]
		}
		df.Status = status(df)
		plan.Doors = append(plan.Doors, df)
	}
	slices.SortFunc(plan.Doors, func(a, b DoorFirmware) int { return cmp.Compare(a.Door.ID, b.Door.ID) })
	return plan, nil
}

// Default is the site's default firmware, nil when there is none.
func (p Plan) Default() *connector.StoredFirmware {
	for i := range p.Firmware {
		if p.Firmware[i].Default {
			return &p.Firmware[i]
		}
	}
	return nil
}

// status is df's status in the rollout.
func status(df DoorFirmware) string {
	if !door.Enrolled(df.Door) || df.Target == nil {
		return ""
	}
	u := df.Update
	if u != nil && u.State == Updating {
		return Updating
	}
	if df.Door.Controller.FirmwareVersion == df.Target.Version {
		return Done
	}
	if u != nil && u.State == Failed && u.Version == df.Target.Version {
		return Failed
	}
	return Pending
}

// Door returns where the door whose id is id stands with its firmware.
func (s Service) Door(ctx context.Context, id int64) (DoorFirmware, error) {
	if _, err := s.Doors.Door(ctx, id); err != nil {
		return DoorFirmware{}, err
	}
	plan, err := s.Plan(ctx)
	if err != nil {
		return DoorFirmware{}, err
	}
	i := slices.IndexFunc(plan.Doors, func(df DoorFirmware) bool { return df.Door.ID == id })
	return plan.Doors[i], nil
}

// Rollout does what the rollout makes due: an update that has taken
// longer than it may has failed, and, while the update window current
// gives is open, doors waiting for their target are sent it until as
// many are updating as current allows.
func (s Service) Rollout(ctx context.Context, current settings.Current) error {
	plan, err := s.Plan(ctx)
	if err != nil {
		return err
	}
	now := s.Clock.Now()
	updating, err := s.expire(ctx, plan, now)
	if err != nil {
		return err
	}
	opened, open := windowOpened(current.UpdateWindow, current.Zone, now)
	if !open {
		return nil
	}
	for _, df := range plan.Doors {
		if updating >= current.UpdateConcurrency {
			return nil
		}
		if !due(df, opened) {
			continue
		}
		if err := s.send(ctx, df, current, nil); err != nil {
			return err
		}
		updating++
	}
	return nil
}

// expire fails every update in plan that has not ended by its expiry
// before now, and returns how many doors are updating still.
func (s Service) expire(ctx context.Context, plan Plan, now time.Time) (int, error) {
	updating := 0
	for _, df := range plan.Doors {
		if df.Status != Updating {
			continue
		}
		u := *df.Update
		if now.Before(u.ExpiresAt) {
			updating++
			continue
		}
		why := fmt.Sprintf(" It did not connect again with it within %g minutes.", u.ExpiresAt.Sub(u.SentAt).Minutes())
		if err := s.finish(ctx, u, Failed, why); err != nil {
			return 0, err
		}
	}
	return updating, nil
}

// due reports whether the rollout sends df its target now, in a window
// that opened at opened: its controller is connected and runs other
// firmware, and it was not sent its target in this window already.
func due(df DoorFirmware, opened time.Time) bool {
	if df.Status != Pending && df.Status != Failed {
		return false
	}
	if !df.Door.Controller.Online {
		return false
	}
	u := df.Update
	return u == nil || u.Version != df.Target.Version || u.SentAt.Before(opened)
}

// windowOpened is when the update window w that now falls in opened, read
// in zone, and whether now falls in one.
func windowOpened(w settings.UpdateWindow, zone *time.Location, now time.Time) (time.Time, bool) {
	minutes := int(w.Length / time.Minute)
	sch := connector.StoredSchedule{Windows: []connector.StoredWindow{{Days: w.Days, Start: w.Start, End: (w.Start + minutes) % minutesPerDay}}}
	// A window lasts a day at most, so one that is open now opened in the
	// day before.
	intervals := schedule.Expand(sch, zone, now.AddDate(0, 0, -1), now.Add(time.Nanosecond))
	n := len(intervals)
	if n == 0 || !intervals[n-1].End.After(now) {
		return time.Time{}, false
	}
	return intervals[n-1].Start, true
}

// UpdateNow sends the door whose id is id its target at once, whatever the
// update window and however many doors are updating, on behalf of the user
// whose id is by, and returns where the door stands now.
func (s Service) UpdateNow(ctx context.Context, id, by int64) (DoorFirmware, error) {
	df, err := s.Door(ctx, id)
	if err != nil {
		return DoorFirmware{}, err
	}
	if df.Target == nil {
		return DoorFirmware{}, ErrNoTarget
	}
	current, err := s.Settings.Current(ctx)
	if err != nil {
		return DoorFirmware{}, err
	}
	if err := s.send(ctx, df, current, &by); err != nil {
		return DoorFirmware{}, err
	}
	return s.Door(ctx, id)
}

// send tells the controller of df's door to install its target, with a
// download token of its own, and logs it, naming the user whose id is by
// when there is one.
func (s Service) send(ctx context.Context, df DoorFirmware, current settings.Current, by *int64) error {
	if current.PublicURL == "" {
		return ErrNoAddress
	}
	target := *df.Target
	token := base64.RawURLEncoding.EncodeToString(s.Entropy.Bytes(tokenBytes))
	url := current.PublicURL + s.DownloadPath(df.Door.ID, target.Version) + "?token=" + token
	id, err := s.Controllers.Update(ctx, df.Door, target.Version, target.SHA256, url)
	if err != nil {
		return err
	}
	now := s.Clock.Now()
	hash := sha256.Sum256([]byte(token))
	u := connector.StoredUpdate{
		DoorID: df.Door.ID, Version: target.Version, CommandID: id, TokenHash: hash[:], SentAt: now, ExpiresAt: now.Add(current.UpdateTimeout),
		State: Updating,
	}
	if err := s.Store.PutDoorUpdate(ctx, u); err != nil {
		return err
	}
	s.Changed()
	_, err = s.Events.Record(ctx, connector.StoredEvent{
		At: now, DoorID: df.Door.ID, Type: event.Update, Outcome: event.Info, Reason: event.UpdateSent, UserID: by,
		Detail: versionDetail(target.Version, ""),
	})
	return err
}

// Acked handles what the controller of the door whose id is doorID
// answered a command with, payload: an update it says failed has failed.
func (s Service) Acked(ctx context.Context, doorID int64, payload []byte) error {
	var m protocol.Ack
	if err := protocol.Decode(payload, &m); err != nil {
		return err
	}
	u, err := s.updating(ctx, doorID)
	if err != nil || u == nil {
		return err
	}
	failed := u.CommandID == m.ID && m.Result == protocol.AckFailed
	if !failed {
		return nil
	}
	why := " The door could not install it."
	if m.Detail != "" {
		why = fmt.Sprintf(" The door could not install it: %s.", m.Detail)
	}
	return s.finish(ctx, *u, Failed, why)
}

// Settle ends the update of the door whose id is id, whose controller has
// just connected, when it connected with the version it was sent.
func (s Service) Settle(ctx context.Context, id int64) error {
	u, err := s.updating(ctx, id)
	if err != nil || u == nil {
		return err
	}
	d, err := s.Doors.Door(ctx, id)
	if err != nil || d.Controller.FirmwareVersion != u.Version {
		return err
	}
	return s.finish(ctx, *u, Succeeded, "")
}

// updating is the update of the door whose id is doorID that is under
// way, nil when none is.
func (s Service) updating(ctx context.Context, doorID int64) (*connector.StoredUpdate, error) {
	updates, err := s.Store.DoorUpdates(ctx)
	if err != nil {
		return nil, err
	}
	for _, u := range updates {
		if u.DoorID == doorID && u.State == Updating {
			return &u, nil
		}
	}
	return nil, nil
}

// finish ends u in state, which more explains, and logs it.
func (s Service) finish(ctx context.Context, u connector.StoredUpdate, state, more string) error {
	now := s.Clock.Now()
	u.State, u.FinishedAt, u.Detail = state, &now, strings.TrimSpace(more)
	if err := s.Store.PutDoorUpdate(ctx, u); err != nil {
		return err
	}
	s.Changed()
	e := connector.StoredEvent{At: now, DoorID: u.DoorID, Type: event.Update, Outcome: event.Info, Reason: event.UpdateSucceeded, Detail: versionDetail(u.Version, more)}
	if state == Failed {
		e.Outcome, e.Reason = event.Alert, event.UpdateFailed
	}
	_, err := s.Events.Record(ctx, e)
	return err
}

// Download returns the image of version for the controller of the door
// whose id is doorID, which token must let it download: the token it was
// sent with its update to version, under way and not expired.
func (s Service) Download(ctx context.Context, doorID int64, version, token string) ([]byte, error) {
	u, err := s.updating(ctx, doorID)
	if err != nil {
		return nil, err
	}
	if u == nil {
		return nil, ErrNotAllowed
	}
	hash := sha256.Sum256([]byte(token))
	fits := u.Version == version && subtle.ConstantTimeCompare(hash[:], u.TokenHash) == 1
	live := s.Clock.Now().Before(u.ExpiresAt)
	if !fits || !live {
		return nil, ErrNotAllowed
	}
	all, err := s.Store.Firmwares(ctx)
	if err != nil {
		return nil, err
	}
	i := slices.IndexFunc(all, func(f connector.StoredFirmware) bool { return f.Version == version })
	if i < 0 {
		return nil, ErrNoFirmware
	}
	image, _, err := s.Store.FirmwareImage(ctx, all[i].ID)
	return image, err
}
