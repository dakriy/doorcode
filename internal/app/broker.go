package app

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"

	"github.com/dakriy/doorcode/internal/auth"
	"github.com/dakriy/doorcode/internal/connector"
	"github.com/dakriy/doorcode/internal/protocol"
)

// sweepEvery is how often Run does the periodic work.
const sweepEvery = time.Minute

// enrollmentPasswordBytes is how much entropy a generated enrollment
// password holds.
const enrollmentPasswordBytes = 18

// Run serves the door controllers: it lets them enroll with the enrollment
// credentials, handles every message they publish as the broker delivers
// it, and does the periodic work whenever the clock reaches its next turn.
// It returns when the broker stops delivering, and with an error when it
// cannot start. The first Run sets up the enrollment credentials; a later
// one keeps them, so controllers enrolling stay connected.
func (a *App) Run(ctx context.Context) error {
	if !a.enrolling {
		if err := a.prepareEnrollment(ctx); err != nil {
			return err
		}
		a.enrolling = true
	}
	inbound, err := a.broker.Subscribe(ctx, []string{protocol.EnrollFilter, protocol.OnlineFilter, protocol.CheckFilter})
	if err != nil {
		return fmt.Errorf("subscribe: %w", err)
	}
	for {
		a.sweepIfDue(ctx)
		select {
		case m, open := <-inbound:
			if !open {
				return nil
			}
			a.receive(ctx, m)
		case <-a.clock.Alarm(a.nextSweep):
		}
	}
}

// prepareEnrollment makes the broker accept the enrollment credentials the
// settings give. Without a password it generates one, stores it as the
// setting, and logs it, so the operator can give it to the controllers.
func (a *App) prepareEnrollment(ctx context.Context) error {
	username, password, err := a.settings.Enrollment(ctx)
	if err != nil {
		return err
	}
	if password == "" {
		password = base64.RawURLEncoding.EncodeToString(a.entropy.Bytes(enrollmentPasswordBytes))
		if err := a.settings.SetEnrollmentPassword(ctx, password); err != nil {
			return fmt.Errorf("store the generated enrollment password: %w", err)
		}
		a.log.Info("generated the enrollment password; controllers enroll with these credentials, and the enrollment_password setting changes the password",
			connector.Field{Key: "username", Value: username},
			connector.Field{Key: "password", Value: password})
	}
	if err := a.controllers.PrepareEnrollment(ctx, username, password); err != nil {
		return fmt.Errorf("create the enrollment client: %w", err)
	}
	return nil
}

// receive handles one message from a controller, logging why it was
// ignored when it was.
func (a *App) receive(ctx context.Context, m connector.Message) {
	if err := a.handle(ctx, m); err != nil {
		a.log.Error("ignored a broker message",
			connector.Field{Key: "topic", Value: m.Topic},
			connector.Field{Key: "error", Value: err.Error()})
	}
}

// handle handles one message from a controller: a code check, or what it
// says of itself while it enrolls or as a door.
func (a *App) handle(ctx context.Context, m connector.Message) error {
	t, ok := protocol.Parse(m.Topic)
	if !ok {
		return errors.New("the topic names no controller or door")
	}
	if t.Leaf == protocol.LeafCheck {
		return a.checkCode(ctx, t.DoorID, m.Payload)
	}
	return a.controllers.Receive(ctx, t, m.Payload)
}

// sweepIfDue does the periodic work when its turn has come, and sets the
// next turn.
func (a *App) sweepIfDue(ctx context.Context) {
	now := a.clock.Now()
	if now.Before(a.nextSweep) {
		return
	}
	a.nextSweep = now.Add(sweepEvery)
	err := a.sweep(ctx)
	if err != nil {
		a.log.Error("periodic work failed", connector.Field{Key: "error", Value: err.Error()})
	}
}

// sweep does the periodic work: it rotates and revokes door credentials,
// sends doors their timelines again when they are due, and removes the
// sessions that have expired. A part that fails does not stop the others.
func (a *App) sweep(ctx context.Context) error {
	defaults, err := a.settings.DoorDefaults(ctx)
	if err != nil {
		return err
	}
	current, err := a.settings.Current(ctx)
	if err != nil {
		return err
	}
	return errors.Join(
		a.controllers.Sweep(ctx, current.Grace, defaults),
		a.pushes.Refresh(ctx),
		a.auth.PurgeSessions(ctx),
	)
}

// hub tells every page watching live updates that something changed.
type hub struct {
	mu        sync.Mutex
	listeners map[chan struct{}]bool
}

func newHub() *hub {
	return &hub{listeners: map[chan struct{}]bool{}}
}

// listen returns a channel that receives after each change, changes
// coming faster than the listener reads them merged into one, and the
// function that stops listening.
func (h *hub) listen() (<-chan struct{}, func()) {
	ch := make(chan struct{}, 1)
	h.mu.Lock()
	defer h.mu.Unlock()
	h.listeners[ch] = true
	return ch, func() {
		h.mu.Lock()
		defer h.mu.Unlock()
		delete(h.listeners, ch)
	}
}

// notify tells every listener that something changed.
func (h *hub) notify() {
	h.mu.Lock()
	defer h.mu.Unlock()
	for ch := range h.listeners {
		select {
		case ch <- struct{}{}:
		default:
		}
	}
}

// liveEvent is the server-sent event that tells a page what it shows of
// doors and enrollments has changed.
const liveEvent = "event: doors\ndata: changed\n\n"

// liveStream sends a server-sent event each time a door's status or the
// pending enrollments change, until the browser goes away.
func (a *App) liveStream(w http.ResponseWriter, r *http.Request, _ auth.User) {
	changes, stop := a.live.listen()
	defer stop()
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	flush := http.NewResponseController(w).Flush
	_, _ = io.WriteString(w, ": watching doors and enrollments\n\n")
	_ = flush()
	for {
		select {
		case <-r.Context().Done():
			return
		case <-changes:
			_, _ = io.WriteString(w, liveEvent)
			_ = flush()
		}
	}
}
