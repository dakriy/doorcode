package decision

import (
	"context"
	"time"

	"github.com/dakriy/doorcode/internal/connector"
	"github.com/dakriy/doorcode/internal/door"
	"github.com/dakriy/doorcode/internal/rule"
	"github.com/dakriy/doorcode/internal/schedule"
	"github.com/dakriy/doorcode/internal/timeline"
)

// codeWorks is when the code of u opens d from from up to to with site as it
// is: the range cut wherever something the chain reads changes, keeping
// each piece At allows at its start, with touching pieces joined.
func codeWorks(u connector.StoredUser, d connector.StoredDoor, site Site, from, to time.Time) []schedule.Interval {
	works := []schedule.Interval{}
	for _, piece := range timeline.Pieces(from, to, site.changes(u, from, to)) {
		if At(&u, d, site, piece.Start).Allow {
			works = append(works, piece)
		}
	}
	return schedule.Merged(works)
}

// changes is every time from from up to to at which At's answer for u's
// code may change: the end of u's account, the start and end of each
// override, and those of each schedule's intervals.
func (s Site) changes(u connector.StoredUser, from, to time.Time) []time.Time {
	var out []time.Time
	if u.ExpiresAt != nil {
		out = append(out, *u.ExpiresAt)
	}
	for _, o := range s.Overrides {
		out = append(out, o.Start, o.End)
	}
	for _, sch := range s.Schedules {
		for _, in := range schedule.Expand(sch, s.Zone, from, to) {
			out = append(out, in.Start, in.End)
		}
	}
	return out
}

// Access is a door, the access rules of a user's groups that reach it,
// and, over a range, when their code opens it and the door's timeline.
type Access struct {
	Door  connector.StoredDoor
	Rules []connector.StoredRule
	// Reached is true when one of the rules reaches the door or the user
	// is a door commander, whose code opens every door.
	Reached bool
	Works   []schedule.Interval
	Spans   []timeline.Span
}

// Public returns each public door that is not deleted and that u either
// reaches or may enter while it is in open mode, since one of its zones has
// an open-mode schedule, in name order, with u's access there from from up
// to to, read in zone. Overrides do not keep a door listed.
func (s Service) Public(ctx context.Context, u connector.StoredUser, zone *time.Location, from, to time.Time) ([]Access, error) {
	return s.doorsWhere(ctx, u, zone, from, to, func(site Site, a Access) bool {
		return door.InPortal(a.Door) && (a.Reached || len(timeline.OpenScheduleIDs(a.Door, site.Zones)) > 0)
	})
}

// UserAccess returns each door that is not deleted which u reaches, in
// name order, with u's access there from from up to to, read in zone.
func (s Service) UserAccess(ctx context.Context, u connector.StoredUser, zone *time.Location, from, to time.Time) ([]Access, error) {
	return s.doorsWhere(ctx, u, zone, from, to, func(_ Site, a Access) bool { return a.Reached })
}

// DoorAccess is u's access at d from from up to to, read in zone.
func (s Service) DoorAccess(ctx context.Context, u connector.StoredUser, d connector.StoredDoor, zone *time.Location, from, to time.Time) (Access, error) {
	site, err := s.siteOf(ctx, zone)
	if err != nil {
		return Access{}, err
	}
	return s.over(ctx, u, site.accessTo(u, d), site, from, to)
}

// doorsWhere is u's access from from up to to, read in zone, at each door
// that is not deleted whose access keep holds with the site as it is.
func (s Service) doorsWhere(ctx context.Context, u connector.StoredUser, zone *time.Location, from, to time.Time, keep func(Site, Access) bool) ([]Access, error) {
	doors, err := s.Store.CurrentDoors(ctx)
	if err != nil {
		return nil, err
	}
	site, err := s.siteOf(ctx, zone)
	if err != nil {
		return nil, err
	}
	out := []Access{}
	for _, d := range doors {
		a := site.accessTo(u, d)
		if !keep(site, a) {
			continue
		}
		a, err = s.over(ctx, u, a, site, from, to)
		if err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, nil
}

// accessTo is d with the rules of site that reach it from u's groups,
// before any range is looked at.
func (s Site) accessTo(u connector.StoredUser, d connector.StoredDoor) Access {
	reaching := rule.ReachingFrom(d, u.Groups, s.Rules)
	return Access{Door: d, Rules: reaching, Reached: len(reaching) > 0 || commands(&u)}
}

// over is a with when u's code opens its door and the door's timeline
// from from up to to, given site and the door's overrides.
func (s Service) over(ctx context.Context, u connector.StoredUser, a Access, site Site, from, to time.Time) (Access, error) {
	overrides, err := s.Store.DoorOverrides(ctx, a.Door.ID, from)
	if err != nil {
		return a, err
	}
	site.Overrides = overrides
	a.Works = codeWorks(u, a.Door, site, from, to)
	a.Spans = timeline.Of(a.Door, site.Zones, site.Schedules, overrides, site.Zone, from, to)
	return a, nil
}
