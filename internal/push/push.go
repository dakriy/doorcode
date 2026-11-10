// Package push keeps what door controllers hold current: it publishes, as
// the retained messages of their topics, each door's configuration (the
// site's defaults with the door's own overrides) and its timeline for the
// next 25 hours. It sends to every door a controller is enrolled as and
// that is not deleted, connected or not, so a controller that connects
// finds what is current waiting for it.
package push

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/dakriy/doorcode/internal/connector"
	"github.com/dakriy/doorcode/internal/door"
	"github.com/dakriy/doorcode/internal/protocol"
	"github.com/dakriy/doorcode/internal/settings"
	"github.com/dakriy/doorcode/internal/timeline"
)

// horizonHours is how far ahead of now a door's timeline reaches.
const horizonHours = 25

// Store is the part of the store this package uses.
type Store interface {
	connector.DoorsStore
	connector.ZonesStore
	connector.SchedulesStore
	connector.OverridesStore
}

// Service publishes doors' configurations and timelines.
type Service struct {
	Store    Store
	Settings settings.Service
	Broker   connector.Broker
	Clock    connector.Clock
	Sent     *Sent
}

// Sent remembers when each door was last sent its timeline. Requests and
// the broker loop share it.
type Sent struct {
	mu sync.Mutex
	at map[int64]time.Time
}

// NewSent returns a Sent that remembers nothing yet.
func NewSent() *Sent {
	return &Sent{at: map[int64]time.Time{}}
}

func (s *Sent) mark(id int64, at time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.at[id] = at
}

// due reports whether the door whose id is id was not sent its timeline
// in the every before now.
func (s *Sent) due(id int64, now time.Time, every time.Duration) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	last, sent := s.at[id]
	return !sent || !now.Before(last.Add(every))
}

// target is a door to send to, with what sending needs.
type target struct {
	door      connector.StoredDoor
	config    settings.DoorConfig
	zone      *time.Location
	zones     []connector.StoredZone
	schedules []connector.StoredSchedule
}

// sender sends one door something.
type sender func(s Service, ctx context.Context, t target) error

// Connected sends the door whose id is id its configuration and its
// timeline, as when its controller connects.
func (s Service) Connected(ctx context.Context, id int64) error {
	return s.each(ctx, []int64{id}, func(s Service, ctx context.Context, t target) error {
		return errors.Join(sendConfig(s, ctx, t), sendTimeline(s, ctx, t))
	})
}

// Configs sends each door whose id is among ids its configuration.
func (s Service) Configs(ctx context.Context, ids []int64) error {
	return s.each(ctx, ids, sendConfig)
}

// Timelines sends each door whose id is among ids its timeline.
func (s Service) Timelines(ctx context.Context, ids []int64) error {
	return s.each(ctx, ids, sendTimeline)
}

// AllConfigs sends every door its configuration, as when a default
// changes.
func (s Service) AllConfigs(ctx context.Context) error {
	return s.each(ctx, nil, sendConfig)
}

// AllTimelines sends every door its timeline, as when the site timezone
// changes.
func (s Service) AllTimelines(ctx context.Context) error {
	return s.each(ctx, nil, sendTimeline)
}

// Refresh sends every door that was not sent its timeline within its
// timeline refresh interval its timeline again.
func (s Service) Refresh(ctx context.Context) error {
	return s.each(ctx, nil, func(s Service, ctx context.Context, t target) error {
		if !s.Sent.due(t.door.ID, s.Clock.Now(), t.config.TimelineRefresh()) {
			return nil
		}
		return sendTimeline(s, ctx, t)
	})
}

// each sends every door whose id is among ids, or every door when ids is
// nil, what send sends it, carrying on past a door it cannot send to. It
// sends only to a door a controller is enrolled as that is not deleted.
func (s Service) each(ctx context.Context, ids []int64, send sender) error {
	doors, err := s.doors(ctx, ids)
	if err != nil {
		return err
	}
	zones, err := s.Store.Zones(ctx)
	if err != nil {
		return err
	}
	schedules, err := s.Store.Schedules(ctx)
	if err != nil {
		return err
	}
	defaults, err := s.Settings.DoorDefaults(ctx)
	if err != nil {
		return err
	}
	current, err := s.Settings.Current(ctx)
	if err != nil {
		return err
	}
	var errs []error
	for _, d := range doors {
		if !door.Enrolled(d) {
			continue
		}
		config, err := defaults.For(d.Config)
		if err == nil {
			err = send(s, ctx, target{door: d, config: config, zone: current.Zone, zones: zones, schedules: schedules})
		}
		errs = append(errs, err)
	}
	return errors.Join(errs...)
}

// doors returns the doors whose ids are among ids, or every door that is
// not deleted when ids is nil. Given ids, it reads only those doors, so a door connecting does
// not read every door.
func (s Service) doors(ctx context.Context, ids []int64) ([]connector.StoredDoor, error) {
	if ids == nil {
		return s.Store.CurrentDoors(ctx)
	}
	doors := []connector.StoredDoor{}
	for _, id := range ids {
		d, found, err := s.Store.Door(ctx, id)
		if err != nil {
			return nil, err
		}
		if found {
			doors = append(doors, d)
		}
	}
	return doors, nil
}

func sendConfig(s Service, ctx context.Context, t target) error {
	m := protocol.Config{DoorConfig: protocol.DoorConfig(t.config)}
	return s.publish(ctx, t.door.ID, protocol.LeafConfig, &m)
}

func sendTimeline(s Service, ctx context.Context, t target) error {
	now := s.Clock.Now()
	to := now.Add(horizonHours * time.Hour)
	overrides, err := s.Store.DoorOverrides(ctx, t.door.ID, now)
	if err != nil {
		return err
	}
	m := protocol.Timeline{From: now.Unix(), To: to.Unix(), Spans: []protocol.Span{}}
	for _, span := range timeline.Of(t.door, t.zones, t.schedules, overrides, t.zone, now, to) {
		m.Spans = append(m.Spans, protocol.Span{Start: span.Start.Unix(), End: span.End.Unix(), State: span.State})
	}
	if err := s.publish(ctx, t.door.ID, protocol.LeafTimeline, &m); err != nil {
		return err
	}
	s.Sent.mark(t.door.ID, now)
	return nil
}

func (s Service) publish(ctx context.Context, id int64, leaf string, m protocol.Message) error {
	return s.Broker.Publish(ctx, connector.Message{Topic: protocol.DoorTopic(id, leaf), Payload: protocol.Encode(m), Retained: true})
}
