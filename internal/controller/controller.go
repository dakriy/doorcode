// Package controller enrolls door controllers and looks after them once
// they are doors: their broker credentials, rotated and revoked, and their
// online state. A controller enrolls by connecting with the shared
// enrollment credentials and announcing itself; it waits, pending, for as
// long as it stays connected, until an administrator approves it as a door.
package controller

import (
	"cmp"
	"context"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"sync"
	"time"

	"github.com/dakriy/doorcode/internal/connector"
	"github.com/dakriy/doorcode/internal/door"
	"github.com/dakriy/doorcode/internal/protocol"
	"github.com/dakriy/doorcode/internal/push"
	"github.com/dakriy/doorcode/internal/settings"
)

var (
	// ErrNotPending reports a hardware id no connected controller is
	// waiting with.
	ErrNotPending = errors.New("no controller with this hardware id is waiting to be approved")
	// ErrOffline reports a door whose controller is not connected.
	ErrOffline = errors.New("the door's controller is not connected")
	// ErrHandingOver reports a door whose controller has not yet connected
	// with the newest credentials it was sent, so it is sent no more.
	ErrHandingOver = errors.New("the door's controller has not yet connected with the credentials it was last sent")
	// ErrDeleted reports a deleted door, which no controller can become.
	ErrDeleted = errors.New("the door is deleted; restore it first")
	// ErrUnknownDoor reports a message from a door that has no
	// controller.
	ErrUnknownDoor = errors.New("no controller is enrolled as door")
)

const (
	// suffixBytes is how much entropy tells a door's usernames apart.
	suffixBytes = 6
	// tokenBytes is how much entropy a door's password holds.
	tokenBytes = 24
	// commandIDBytes is how much entropy names one command.
	commandIDBytes = 8
)

// Store is the part of the store this package uses.
type Store interface {
	connector.DoorsStore
	connector.CredentialsStore
}

// Service enrolls controllers and looks after their credentials and state.
type Service struct {
	Store Store
	// Doors finds the doors whose controllers the service looks after.
	Doors   door.Service
	Broker  connector.Broker
	Clock   connector.Clock
	Entropy connector.Entropy
	Pending *Pending
	// Push sends a door that connects its configuration and timeline.
	Push push.Service
	// Changed is called whenever what a door's status or the pending
	// enrollments show has changed.
	Changed func()
	// Connected is called with the id of a door whose controller has
	// connected, once its state is stored.
	Connected func(ctx context.Context, doorID int64) error
}

// Enrollment is a controller waiting to be approved: its hardware id, what
// it says of its firmware version and signal strength, and since when it
// has waited.
type Enrollment struct {
	HardwareID      string
	FirmwareVersion string
	Signal          int
	Since           time.Time
}

// Pending holds the controllers waiting to be approved. The broker loop
// and requests share it.
type Pending struct {
	mu   sync.Mutex
	byID map[string]Enrollment
}

// NewPending returns an empty Pending.
func NewPending() *Pending {
	return &Pending{byID: map[string]Enrollment{}}
}

// List is every waiting controller, longest waiting first.
func (p *Pending) List() []Enrollment {
	p.mu.Lock()
	defer p.mu.Unlock()
	out := []Enrollment{}
	for _, e := range p.byID {
		out = append(out, e)
	}
	slices.SortFunc(out, func(a, b Enrollment) int {
		return cmp.Or(a.Since.Compare(b.Since), cmp.Compare(a.HardwareID, b.HardwareID))
	})
	return out
}

// Get returns the controller waiting with hardware id hwid.
func (p *Pending) Get(hwid string) (Enrollment, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	e, ok := p.byID[hwid]
	if !ok {
		return Enrollment{}, ErrNotPending
	}
	return e, nil
}

// set makes e wait when waiting says so, keeping when it started waiting,
// and stops it waiting otherwise.
func (p *Pending) set(e Enrollment, waiting bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if !waiting {
		delete(p.byID, e.HardwareID)
		return
	}
	if held, ok := p.byID[e.HardwareID]; ok {
		e.Since = held.Since
	}
	p.byID[e.HardwareID] = e
}

// Receive handles payload, which the broker delivered from a controller on
// the enrollment or online topic t names.
func (s Service) Receive(ctx context.Context, t protocol.Topic, payload []byte) error {
	if t.HardwareID != "" {
		return s.announced(t.HardwareID, payload)
	}
	return s.online(ctx, t.DoorID, payload)
}

// announced records what an enrolling controller says of itself. An empty
// payload is a cleared announcement.
func (s Service) announced(hwid string, payload []byte) error {
	var m protocol.Enrollment
	if len(payload) > 0 {
		if err := protocol.Decode(payload, &m); err != nil {
			return err
		}
	}
	e := Enrollment{HardwareID: hwid, FirmwareVersion: m.FirmwareVersion, Signal: m.Signal, Since: s.Clock.Now()}
	s.Pending.set(e, m.Online)
	s.Changed()
	return nil
}

// online records a door's controller connecting or going away. A
// controller that connects with a credential stops the older ones of its
// door working; a last will of a revoked credential is stale and changes
// nothing.
func (s Service) online(ctx context.Context, doorID int64, payload []byte) error {
	var m protocol.Online
	if err := protocol.Decode(payload, &m); err != nil {
		return err
	}
	d, found, err := s.Store.Door(ctx, doorID)
	if err != nil {
		return err
	}
	if !found || d.Controller == nil {
		return fmt.Errorf("%w %d", ErrUnknownDoor, doorID)
	}
	creds, err := s.Store.DoorCredentials(ctx, doorID)
	if err != nil {
		return err
	}
	i := slices.IndexFunc(creds, func(c connector.StoredCredential) bool { return c.Username == m.Username })
	if i >= 0 && creds[i].RevokedAt != nil {
		return nil
	}
	c := *d.Controller
	c.Online, c.LastSeenAt, c.Unresponsive = m.Online, s.Clock.Now(), false
	if !m.Online {
		return s.setController(ctx, doorID, &c)
	}
	c.FirmwareVersion, c.Signal = m.FirmwareVersion, m.Signal
	if err := s.used(ctx, creds, i); err != nil {
		return err
	}
	if err := s.setController(ctx, doorID, &c); err != nil {
		return err
	}
	if err := s.Connected(ctx, doorID); err != nil {
		return err
	}
	return s.Push.Connected(ctx, doorID)
}

// used records that the controller connected with creds[i], when i names
// one, and revokes the older credentials of its door.
func (s Service) used(ctx context.Context, creds []connector.StoredCredential, i int) error {
	if i < 0 {
		return nil
	}
	if creds[i].UsedAt == nil {
		if _, err := s.Store.UseCredential(ctx, creds[i].ID, s.Clock.Now()); err != nil {
			return err
		}
	}
	return s.revoke(ctx, creds[:i])
}

// revoke stops every live credential among creds working.
func (s Service) revoke(ctx context.Context, creds []connector.StoredCredential) error {
	for _, c := range creds {
		if c.RevokedAt != nil {
			continue
		}
		if err := s.Broker.DeleteClient(ctx, c.Username); err != nil {
			return err
		}
		if _, err := s.Store.RevokeCredential(ctx, c.ID, s.Clock.Now()); err != nil {
			return err
		}
	}
	return nil
}

// revokeDoor stops every credential of the door whose id is id working.
func (s Service) revokeDoor(ctx context.Context, id int64) error {
	creds, err := s.Store.DoorCredentials(ctx, id)
	if err != nil {
		return err
	}
	return s.revoke(ctx, creds)
}

func (s Service) setController(ctx context.Context, id int64, c *connector.StoredController) error {
	_, err := s.Store.SetController(ctx, id, c)
	s.Changed()
	return err
}

// PrepareEnrollment makes username and password the shared credentials
// controllers enroll with, replacing any the broker has under username. A
// controller connecting with them must use its hardware id as its client
// id, and may only announce itself on its own enroll topic and receive its
// own credentials.
func (s Service) PrepareEnrollment(ctx context.Context, username, password string) error {
	// The broker refuses to delete a client it does not have, as it will
	// on a first start; creating it says whether it is there now.
	_ = s.Broker.DeleteClient(ctx, username)
	return s.Broker.CreateClient(ctx, connector.BrokerClient{
		Username: username, Password: password,
		Publish: []string{protocol.EnrollOwnFilter}, Subscribe: []string{protocol.CredentialsOwnFilter},
	})
}

// Approve makes the waiting controller e the door whose id is doorID: it
// issues the door credentials and sends them to the controller, which
// connects with them as the door. A controller the door had before stops
// working, and so does a door this controller was.
func (s Service) Approve(ctx context.Context, e Enrollment, doorID int64) error {
	hwid := e.HardwareID
	if err := s.vacate(ctx, hwid, doorID); err != nil {
		return err
	}
	username, token, err := s.issue(ctx, doorID)
	if err != nil {
		return err
	}
	c := connector.StoredController{HardwareID: hwid, FirmwareVersion: e.FirmwareVersion, Signal: e.Signal, LastSeenAt: s.Clock.Now()}
	if _, err := s.Store.SetController(ctx, doorID, &c); err != nil {
		return err
	}
	// Send the credentials, then clear the retained announcement, so the
	// controller is not taken for waiting again when the server next
	// subscribes.
	credentials := protocol.Encode(&protocol.Credentials{DoorID: doorID, Username: username, Token: token})
	for _, m := range []connector.Message{
		{Topic: protocol.CredentialsTopic(hwid), Payload: credentials},
		{Topic: protocol.EnrollTopic(hwid), Retained: true},
	} {
		if err := s.Broker.Publish(ctx, m); err != nil {
			return err
		}
	}
	s.Pending.set(e, false)
	s.Changed()
	return nil
}

// vacate makes the door whose id is doorID ready to become the controller
// with hardware id hwid: the door must exist and not be deleted, its
// credentials stop working, and a door the controller was loses it.
func (s Service) vacate(ctx context.Context, hwid string, doorID int64) error {
	doors, err := s.Store.Doors(ctx)
	if err != nil {
		return err
	}
	i := slices.IndexFunc(doors, func(d connector.StoredDoor) bool { return d.ID == doorID })
	if i < 0 {
		return door.ErrNoDoor
	}
	if doors[i].DeletedAt != nil {
		return ErrDeleted
	}
	for _, d := range doors {
		if d.ID != doorID && d.Controller != nil && d.Controller.HardwareID == hwid {
			if err := s.detach(ctx, d.ID); err != nil {
				return err
			}
		}
	}
	return s.revokeDoor(ctx, doorID)
}

// detach revokes the credentials of the door whose id is id and takes its
// controller off it.
func (s Service) detach(ctx context.Context, id int64) error {
	if err := s.revokeDoor(ctx, id); err != nil {
		return err
	}
	return s.setController(ctx, id, nil)
}

// issue creates new broker credentials for the door whose id is doorID,
// allowed only the door's own topics, and returns them.
func (s Service) issue(ctx context.Context, doorID int64) (string, string, error) {
	username := "door-" + strconv.FormatInt(doorID, 10) + "-" + hex.EncodeToString(s.Entropy.Bytes(suffixBytes))
	token := base64.RawURLEncoding.EncodeToString(s.Entropy.Bytes(tokenBytes))
	topics := []string{protocol.DoorFilter(doorID)}
	client := connector.BrokerClient{Username: username, Password: token, Publish: topics, Subscribe: topics}
	if err := s.Broker.CreateClient(ctx, client); err != nil {
		return "", "", err
	}
	_, err := s.Store.CreateCredential(ctx, connector.StoredCredential{DoorID: doorID, Username: username, IssuedAt: s.Clock.Now()})
	return username, token, err
}

// Credentials returns the credentials of the door whose id is id, oldest
// first.
func (s Service) Credentials(ctx context.Context, id int64) ([]connector.StoredCredential, error) {
	if _, err := s.Doors.Door(ctx, id); err != nil {
		return nil, err
	}
	return s.Store.DoorCredentials(ctx, id)
}

// Rotate issues the connected controller of the door whose id is id new
// credentials over its connection. Its old ones keep working until it
// connects with the new ones, which stops them working, so a door has at
// most two live credentials: while it has not yet connected with the new
// ones, it is issued no more.
func (s Service) Rotate(ctx context.Context, id int64) error {
	d, err := s.Doors.Door(ctx, id)
	if err != nil {
		return err
	}
	if !connected(d) {
		return ErrOffline
	}
	creds, err := s.Store.DoorCredentials(ctx, id)
	if err != nil {
		return err
	}
	live := slices.DeleteFunc(creds, func(c connector.StoredCredential) bool { return c.RevokedAt != nil })
	if len(live) > 1 {
		return ErrHandingOver
	}
	username, token, err := s.issue(ctx, id)
	if err != nil {
		return err
	}
	_, err = s.send(ctx, id, protocol.Command{Command: protocol.CommandCredentials, Username: username, Token: token})
	s.Changed()
	return err
}

// Open tells the connected controller of d to release its strike for
// strikeMS milliseconds.
func (s Service) Open(ctx context.Context, d connector.StoredDoor, strikeMS int) error {
	if !connected(d) {
		return ErrOffline
	}
	_, err := s.send(ctx, d.ID, protocol.Command{Command: protocol.CommandOpen, StrikeMS: strikeMS})
	return err
}

// Update tells the connected controller of d to install the firmware
// version whose image has the SHA-256 sha256, in hex, from url, and
// returns the id of the command that told it.
func (s Service) Update(ctx context.Context, d connector.StoredDoor, version, sha256, url string) (string, error) {
	if !connected(d) {
		return "", ErrOffline
	}
	return s.send(ctx, d.ID, protocol.Command{Command: protocol.CommandUpdate, Version: version, SHA256: sha256, URL: url})
}

// connected reports whether a controller enrolled as d is connected.
func connected(d connector.StoredDoor) bool {
	return d.Controller != nil && d.Controller.Online
}

// send gives c an id of its own, publishes it on the command topic of the
// door whose id is id, and returns the id.
func (s Service) send(ctx context.Context, id int64, c protocol.Command) (string, error) {
	c.ID = hex.EncodeToString(s.Entropy.Bytes(commandIDBytes))
	return c.ID, s.Broker.Publish(ctx, connector.Message{Topic: protocol.DoorTopic(id, protocol.LeafCommand), Payload: protocol.Encode(&c)})
}

// Revoke stops every credential of the door whose id is id working, which
// disconnects its controller. The controller connects again only by
// enrolling and being approved.
func (s Service) Revoke(ctx context.Context, id int64) error {
	d, err := s.Doors.Door(ctx, id)
	if err != nil {
		return err
	}
	if err := s.revokeDoor(ctx, id); err != nil {
		return err
	}
	if d.Controller == nil {
		return nil
	}
	// A door that is not connected is shown offline, so it is not
	// unresponsive as well.
	c := *d.Controller
	c.Online, c.Unresponsive = false, false
	return s.setController(ctx, id, &c)
}

// Delete deletes the door whose id is id and stops its credentials
// working, which disconnects its controller.
func (s Service) Delete(ctx context.Context, id int64) error {
	if err := s.Doors.Delete(ctx, id); err != nil {
		return err
	}
	return s.Revoke(ctx, id)
}

// Sweep does what time makes due: a door whose newest credentials were
// issued its credential rotation interval ago or longer, by defaults and
// its own overrides, gets new ones when its controller is connected and
// has connected with the newest it was sent.
func (s Service) Sweep(ctx context.Context, defaults settings.DoorDefaults) error {
	live, err := s.Store.LiveCredentials(ctx)
	if err != nil {
		return err
	}
	rotateAfter, err := s.rotationIntervals(ctx, defaults)
	if err != nil {
		return err
	}
	byDoor := map[int64][]connector.StoredCredential{}
	var doors []int64
	for _, c := range live {
		if _, seen := byDoor[c.DoorID]; !seen {
			doors = append(doors, c.DoorID)
		}
		byDoor[c.DoorID] = append(byDoor[c.DoorID], c)
	}
	for _, id := range doors {
		if err := s.sweepDoor(ctx, byDoor[id], rotateAfter[id]); err != nil {
			return err
		}
	}
	return nil
}

// rotationIntervals is how long each door's credentials work before it
// gets new ones, by door id, by defaults and the door's own overrides.
func (s Service) rotationIntervals(ctx context.Context, defaults settings.DoorDefaults) (map[int64]time.Duration, error) {
	doors, err := s.Store.Doors(ctx)
	if err != nil {
		return nil, err
	}
	intervals := map[int64]time.Duration{}
	for _, d := range doors {
		config, err := defaults.For(d.Config)
		if err != nil {
			return nil, err
		}
		intervals[d.ID] = config.RotateAfter()
	}
	return intervals, nil
}

// sweepDoor rotates one door's credentials when they are due, given its
// live credentials, oldest first. A door that cannot be sent new ones now
// is sent them by a later sweep.
func (s Service) sweepDoor(ctx context.Context, live []connector.StoredCredential, rotateAfter time.Duration) error {
	newest := live[len(live)-1]
	if s.Clock.Now().Before(newest.IssuedAt.Add(rotateAfter)) {
		return nil
	}
	err := s.Rotate(ctx, newest.DoorID)
	if errors.Is(err, ErrOffline) || errors.Is(err, ErrHandingOver) {
		return nil
	}
	return err
}
