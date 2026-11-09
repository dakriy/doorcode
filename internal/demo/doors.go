package demo

import (
	"context"
	"errors"
	"fmt"
	"math/rand/v2"
	"net/http"
	"time"

	"github.com/dakriy/doorcode/internal/connector"
	"github.com/dakriy/doorcode/internal/protocol"
)

// enrollmentUsername is the enrollment_username setting's default, which
// the demo leaves as it is.
const enrollmentUsername = "doorcode-enroll"

// leafOnline ends the topic a door says whether it is online on.
const leafOnline = "online"

// errDropped is a connection the broker ended.
var errDropped = errors.New("the broker ended the connection")

// Start plays the controllers of site against broker, approves those of
// its doors through the API as their doors, and leaves the waiting one
// waiting. The application must be running its broker side already, so
// the enrollment credentials exist. The controllers play until ctx ends.
func Start(ctx context.Context, h http.Handler, broker *Broker, site Site, log connector.Log) error {
	for _, d := range append([]SimulatedDoor{site.Waiting}, site.Doors...) {
		c := &controller{broker: broker, log: log, door: d, codes: site.Codes, enrollment: site.EnrollmentPassword, started: time.Now()}
		go c.run(ctx)
	}
	admin, err := signIn(ctx, h, site.AdminEmail, AdminPassword)
	if err != nil {
		return err
	}
	for _, d := range site.Doors {
		if err := approve(ctx, admin, d); err != nil {
			return err
		}
	}
	return nil
}

// approve approves d's controller as its door once it waits, trying again
// for a while since it announces itself in its own time.
func approve(ctx context.Context, admin client, d SimulatedDoor) error {
	deadline := time.Now().Add(30 * time.Second)
	body := map[string]int64{"door_id": d.DoorID}
	for {
		err := admin.call(http.MethodPost, "/api/v1/enrollments/"+d.HardwareID+"/approve", body, nil)
		if err == nil || time.Now().After(deadline) {
			return err
		}
		if !pause(ctx, 100*time.Millisecond) {
			return ctx.Err()
		}
	}
}

// pause waits for d, reporting false when ctx ends first.
func pause(ctx context.Context, d time.Duration) bool {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-t.C:
		return true
	}
}

// controller plays one door controller: it enrolls, waits for approval,
// connects as its door, reports its telemetry, lets people in when the
// server says so, answers commands, and enrolls again when its
// credentials stop working.
type controller struct {
	broker *Broker
	log    connector.Log
	door   SimulatedDoor
	codes  []string
	// enrollment is the enrollment password.
	enrollment string
	creds      protocol.Credentials
	started    time.Time
	// connects is how often it connected as its door since it started.
	connects int
}

// run connects until ctx ends, pausing after a connection that failed or
// ended.
func (c *controller) run(ctx context.Context) {
	for ctx.Err() == nil {
		err := c.connect(ctx)
		switch {
		case err == nil:
			// Enrolled, or told to connect again with new credentials or firmware.
		case errors.Is(err, ErrRefused) && c.enrolled():
			// Revoked: start over as a new controller would.
			c.creds = protocol.Credentials{}
		default:
			c.complain(ctx, err)
			if !pause(ctx, 2*time.Second) {
				return
			}
		}
	}
}

// enrolled reports whether the controller holds credentials for a door.
func (c *controller) enrolled() bool {
	return c.creds.Username != ""
}

// complain logs err unless it is what a controller expects now and then:
// ctx ending, or the broker refusing or ending the connection.
func (c *controller) complain(ctx context.Context, err error) {
	if ctx.Err() != nil || errors.Is(err, ErrRefused) || errors.Is(err, errDropped) {
		return
	}
	c.log.Error("a simulated controller stopped", connector.Field{Key: "hardware_id", Value: c.door.HardwareID},
		connector.Field{Key: "error", Value: err.Error()})
}

// connect enrolls without credentials and otherwise serves as the door.
func (c *controller) connect(ctx context.Context) error {
	if !c.enrolled() {
		return c.enroll(ctx)
	}
	c.connects++
	return c.serve(ctx)
}

// enroll announces the controller with the enrollment credentials and
// waits until the server sends it credentials.
func (c *controller) enroll(ctx context.Context) error {
	hwid := c.door.HardwareID
	will := connector.Message{Topic: protocol.EnrollTopic(hwid), Payload: protocol.Encode(&protocol.Enrollment{Online: false}), Retained: true}
	conn, err := c.broker.Connect(hwid, enrollmentUsername, c.enrollment, will)
	if err != nil {
		return err
	}
	defer conn.Close()
	if err := conn.Subscribe(protocol.CredentialsTopic(hwid)); err != nil {
		return err
	}
	announcement := protocol.Enrollment{Online: true, FirmwareVersion: c.door.Firmware, Signal: c.door.Signal}
	conn.Publish(connector.Message{Topic: protocol.EnrollTopic(hwid), Payload: protocol.Encode(&announcement), Retained: true})
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case m, ok := <-conn.Messages():
			if !ok {
				return errDropped
			}
			var creds protocol.Credentials
			if protocol.Decode(m.Payload, &creds) == nil {
				c.creds = creds
				return nil
			}
		}
	}
}

// door is the state of the door while its controller serves it.
type door struct {
	conn     *Conn
	config   protocol.DoorConfig
	timeline protocol.Timeline
	open     bool
	opens    int
	// closing rings when the door swings shut, installing when an
	// update has been installed, each nil while nothing is under way.
	closing    *time.Timer
	installing *time.Timer
	installed  string
}

// serve connects as the door and serves it until the connection ends.
func (c *controller) serve(ctx context.Context) error {
	id := c.creds.DoorID
	online := protocol.DoorTopic(id, leafOnline)
	will := connector.Message{Topic: online, Payload: protocol.Encode(&protocol.Online{Online: false, Username: c.creds.Username}), Retained: true}
	conn, err := c.broker.Connect(c.door.HardwareID, c.creds.Username, c.creds.Token, will)
	if err != nil {
		return err
	}
	leaves := []string{protocol.LeafCommand, protocol.LeafReply, protocol.LeafConfig, protocol.LeafTimeline}
	filters := make([]string, len(leaves))
	for i, leaf := range leaves {
		filters[i] = protocol.DoorTopic(id, leaf)
	}
	if err := conn.Subscribe(filters...); err != nil {
		conn.Close()
		return err
	}
	status := protocol.Online{Online: true, Username: c.creds.Username, FirmwareVersion: c.door.Firmware, Signal: c.door.Signal}
	conn.Publish(connector.Message{Topic: online, Payload: protocol.Encode(&status), Retained: true})
	d := &door{conn: conn, config: protocol.DoorConfig{TelemetryIntervalS: 60, StrikePulseMS: 3000}}
	return c.loop(ctx, d)
}

// loop serves d until its connection ends, ctx ends, or the controller
// has to connect again with other credentials or firmware.
func (c *controller) loop(ctx context.Context, d *door) error {
	report := time.NewTimer(time.Second)
	keypad := time.NewTimer(between(10*time.Second, 90*time.Second))
	forced := time.NewTimer(between(2*time.Hour, 8*time.Hour))
	for {
		select {
		case <-ctx.Done():
			d.conn.Close()
			return ctx.Err()
		case m, ok := <-d.conn.Messages():
			if !ok {
				return errDropped
			}
			if again := c.receive(d, m); again {
				return nil
			}
		case <-report.C:
			c.report(d)
			report.Reset(time.Duration(max(d.config.TelemetryIntervalS, 5)) * time.Second)
		case <-keypad.C:
			c.press(d)
			keypad.Reset(between(40*time.Second, 150*time.Second))
		case <-forced.C:
			c.swing(d, 8*time.Second)
			forced.Reset(between(2*time.Hour, 8*time.Hour))
		case <-timerC(d.closing):
			d.closing, d.open = nil, false
			c.say(d, protocol.EventClosed)
		case <-timerC(d.installing):
			return c.restart(d)
		}
	}
}

// timerC is t's channel, nil (never ready) without a timer.
func timerC(t *time.Timer) <-chan time.Time {
	if t == nil {
		return nil
	}
	return t.C
}

// between is a random duration from lo up to hi.
func between(lo, hi time.Duration) time.Duration {
	return lo + rand.N(hi-lo)
}

// receive handles a message from the server, reporting whether the
// controller must connect again.
func (c *controller) receive(d *door, m connector.Message) bool {
	t, _ := protocol.Parse(m.Topic)
	switch t.Leaf {
	case protocol.LeafConfig:
		var config protocol.Config
		if protocol.Decode(m.Payload, &config) == nil {
			d.config = config.DoorConfig
		}
	case protocol.LeafTimeline:
		var timeline protocol.Timeline
		if protocol.Decode(m.Payload, &timeline) == nil {
			d.timeline = timeline
		}
	case protocol.LeafReply:
		var reply protocol.Reply
		if protocol.Decode(m.Payload, &reply) == nil && reply.Allow {
			c.swing(d, between(3*time.Second, 9*time.Second))
		}
	case protocol.LeafCommand:
		return c.command(d, m.Payload)
	}
	return false
}

// command carries out a command, reporting whether the controller must
// connect again: with new credentials or after a reboot at once, or after
// installing firmware.
func (c *controller) command(d *door, payload []byte) bool {
	var cmd protocol.Command
	if protocol.Decode(payload, &cmd) != nil {
		return false
	}
	switch cmd.Command {
	case protocol.CommandOpen:
		c.ack(d, cmd.ID)
		c.swing(d, between(4*time.Second, 10*time.Second))
	case protocol.CommandCredentials:
		c.creds.Username, c.creds.Token = cmd.Username, cmd.Token
		d.conn.Close()
		return true
	case protocol.CommandUpdate:
		d.installed = cmd.Version
		d.installing = time.NewTimer(between(5*time.Second, 15*time.Second))
	case protocol.CommandReboot:
		c.ack(d, cmd.ID)
		d.installed = c.door.Firmware
		return c.restart(d) == nil
	}
	return false
}

// restart is the controller rebooting into the firmware it installed, or
// the one it runs when told to reboot: it drops off the broker and
// connects again reporting that version.
func (c *controller) restart(d *door) error {
	d.conn.Drop()
	c.door.Firmware = d.installed
	c.started = time.Now()
	c.connects = 0
	return nil
}

// publish sends m on the door's topic that ends in leaf.
func (c *controller) publish(d *door, leaf string, m protocol.Message) {
	d.conn.Publish(connector.Message{Topic: protocol.DoorTopic(c.creds.DoorID, leaf), Payload: protocol.Encode(m)})
}

// ack answers the command whose id is id as carried out.
func (c *controller) ack(d *door, id string) {
	c.publish(d, protocol.LeafAck, &protocol.Ack{ID: id, Result: "done"})
}

// press is someone at the keypad: in open mode the door just opens;
// otherwise the controller asks the server about a code, mostly one
// somebody has.
func (c *controller) press(d *door) {
	if d.state(time.Now()) == "open" {
		d.opens++
		return
	}
	digits := fmt.Sprintf("%06d", rand.IntN(1000000))
	if len(c.codes) > 0 && rand.IntN(4) != 0 {
		digits = c.codes[rand.IntN(len(c.codes))]
	}
	c.publish(d, protocol.LeafCheck, &protocol.Check{RequestID: fmt.Sprintf("%016x", rand.Uint64()), Digits: digits})
}

// state is what d's timeline says the door is at at: "open", "locked",
// or empty for its normal state.
func (d *door) state(at time.Time) string {
	now := at.Unix()
	for _, s := range d.timeline.Spans {
		if s.Start <= now && now < s.End {
			return s.State
		}
	}
	return ""
}

// swing opens the door, unless it is open already, and has it close after
// open. Now and then someone holds it open far longer.
func (c *controller) swing(d *door, open time.Duration) {
	if d.open {
		return
	}
	if rand.IntN(40) == 0 {
		open = time.Duration(d.config.HeldOpenS)*time.Second + between(5*time.Second, 30*time.Second)
	}
	d.open = true
	d.opens++
	d.closing = time.NewTimer(open)
	c.say(d, protocol.EventOpened)
}

// say tells the server what happened to the door.
func (c *controller) say(d *door, what string) {
	c.publish(d, protocol.LeafEvent, &protocol.DoorEvent{Event: what})
}

// report publishes the controller's telemetry.
func (c *controller) report(d *door) {
	t := protocol.Telemetry{
		Signal: c.door.Signal + rand.IntN(7) - 3, LatencyMS: 20 + rand.IntN(40), UptimeS: int64(time.Since(c.started).Seconds()),
		FreeBytes: 182000 + rand.Int64N(6000), FirmwareVersion: c.door.Firmware, Reconnects: c.connects - 1, DoorOpen: d.open, Opens: d.opens,
	}
	d.opens = 0
	c.publish(d, protocol.LeafTelemetry, &t)
}
