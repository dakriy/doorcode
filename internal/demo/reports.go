package demo

import (
	"context"
	"math/rand/v2"
	"time"

	"github.com/dakriy/doorcode/internal/connector"
)

// profile is a door's simulated controller and how it reports: its
// hardware id, its usual signal and latency, the firmware it ran before
// and after its last update, which it runs now, and the controller it
// hops through in the mesh, empty when it reaches the access point itself.
type profile struct {
	door             int64
	hardwareID       string
	signal, latency  int
	before, after    string
	updatedDaysAgo   int
	outageDaysAgo    int
	outageHour       int
	outageHours      int
	openModeOpenings int
	meshParent       string
}

// profiles are the controllers of the doors that have one, in door order.
var profiles = []profile{
	{mainEntrance, "a4cf12e0b101", -54, 28, "1.4.0", "1.4.1", 10, 0, 0, 0, 3, ""},
	{staffEntrance, "a4cf12e0b102", -63, 34, "1.4.0", "1.4.1", 10, 16, 14, 1, 0, ""},
	{serverRoom, "a4cf12e0b103", -71, 41, "1.4.0", "1.4.1", 10, 4, 10, 2, 0, "a4cf12e0b102"},
	{workshop, "a4cf12e0b104", -58, 30, "1.3.2", "1.4.0", 20, 0, 0, 0, 0, "a4cf12e0b101"},
	{loadingDock, "a4cf12e0b105", -77, 85, "1.3.2", "1.3.2", 0, 9, 3, 7, 0, "a4cf12e0b103"},
}

// meshLayer is how many controllers p's controller hops through to reach
// the access point.
func (p profile) meshLayer() int {
	layer := 0
	for parent := p.meshParent; parent != ""; layer++ {
		parent = profileOf(parent).meshParent
	}
	return layer
}

// meshChildren is how many controllers hop through p's.
func (p profile) meshChildren() int {
	children := 0
	for _, other := range profiles {
		if other.meshParent == p.hardwareID {
			children++
		}
	}
	return children
}

// profileOf is the profile of the controller whose hardware id is
// hardwareID.
func profileOf(hardwareID string) profile {
	for _, p := range profiles {
		if p.hardwareID == hardwareID {
			return p
		}
	}
	return profile{}
}

// controller is the simulated controller p describes, approved as its
// door.
func (p profile) controller() SimulatedDoor {
	return SimulatedDoor{
		HardwareID: p.hardwareID, Firmware: p.after, Signal: p.signal, DoorID: p.door,
		MeshLayer: p.meshLayer(), MeshParent: p.meshParent, MeshChildren: p.meshChildren(),
	}
}

// outage is when p's controller went quiet in the past and when it came
// back, as seen from now; they are the same when it never did.
func (p profile) outage(now time.Time) (time.Time, time.Time) {
	from := now.UTC().Truncate(24*time.Hour).AddDate(0, 0, -p.outageDaysAgo).Add(time.Duration(p.outageHour) * time.Hour)
	return from, from.Add(time.Duration(p.outageHours) * time.Hour)
}

// report is what p's controller would have reported at at, opens being
// how often its door opened since its report before, and whether it
// reported at all.
func (p profile) report(rng *rand.Rand, now, at time.Time, opens int) (connector.StoredTelemetry, bool) {
	outage, back := p.outage(now)
	if p.outageHours > 0 && !at.Before(outage) && at.Before(back) {
		return connector.StoredTelemetry{}, false
	}
	version := p.after
	if at.Before(now.AddDate(0, 0, -p.updatedDaysAgo)) {
		version = p.before
	}
	busy := at.Hour() >= 7 && at.Hour() < 19
	latency := p.latency + rng.IntN(15)
	if busy {
		latency += rng.IntN(20)
	}
	if openMode(at) {
		opens += rng.IntN(p.openModeOpenings + 1)
	}
	return connector.StoredTelemetry{
		At: at, Signal: p.signal + rng.IntN(7) - 3, LatencyMS: latency, UptimeS: int64(at.Sub(at.Truncate(7 * 24 * time.Hour)).Seconds()),
		FreeBytes: 182000 + rng.Int64N(6000), FirmwareVersion: version, Reconnects: rng.IntN(3), Opens: opens,
		MeshLayer: p.meshLayer(), MeshParent: p.meshParent, MeshChildren: p.meshChildren(),
	}, true
}

// writeTelemetry writes what the doors reported since start: every report
// of the last sampleDays, and hourly rollups of the reports before.
func writeTelemetry(ctx context.Context, store connector.Store, rng *rand.Rand, opens map[slot]int, start, now time.Time) error {
	samplesFrom := now.UTC().Truncate(time.Hour).AddDate(0, 0, -sampleDays)
	for _, p := range profiles {
		var rollups []connector.StoredRollup
		for hour := start.UTC().Truncate(time.Hour); hour.Before(samplesFrom); hour = hour.Add(time.Hour) {
			if r, ok := p.rollup(rng, now, hour, opens); ok {
				rollups = append(rollups, r)
			}
		}
		if err := store.PutRollups(ctx, rollups); err != nil {
			return err
		}
		if err := p.writeSamples(ctx, store, rng, opens, samplesFrom, now); err != nil {
			return err
		}
	}
	return nil
}

// writeSamples writes every report p's controller made from from up to
// now.
func (p profile) writeSamples(ctx context.Context, store connector.Store, rng *rand.Rand, opens map[slot]int, from, now time.Time) error {
	for at := from; at.Before(now); at = at.Add(sampleEvery) {
		t, ok := p.report(rng, now, at, opens[slot{p.door, at}])
		if !ok {
			continue
		}
		if err := store.AddSample(ctx, connector.StoredSample{DoorID: p.door, StoredTelemetry: t}); err != nil {
			return err
		}
	}
	return nil
}

// rollup sums up what p's controller reported in the hour from hour, and
// whether it reported at all.
func (p profile) rollup(rng *rand.Rand, now, hour time.Time, opens map[slot]int) (connector.StoredRollup, bool) {
	r := connector.StoredRollup{DoorID: p.door, Hour: hour}
	var signal, latency, free spread
	for at := hour; at.Before(hour.Add(time.Hour)); at = at.Add(sampleEvery) {
		t, ok := p.report(rng, now, at, opens[slot{p.door, at}])
		if !ok {
			continue
		}
		r.Samples++
		r.Opens += t.Opens
		signal.add(int64(t.Signal))
		latency.add(int64(t.LatencyMS))
		free.add(t.FreeBytes)
	}
	r.Signal, r.Latency, r.FreeBytes = signal.stored(), latency.stored(), free.stored()
	r.MeshLayer, r.MeshChildren = p.meshLayer(), p.meshChildren()
	return r, r.Samples > 0
}

// spread follows the mean, smallest, and largest of values as they come.
type spread struct {
	n             int
	sum, min, max int64
}

func (s *spread) add(v int64) {
	if s.n == 0 || v < s.min {
		s.min = v
	}
	if s.n == 0 || v > s.max {
		s.max = v
	}
	s.n++
	s.sum += v
}

func (s spread) stored() connector.StoredSpread {
	if s.n == 0 {
		return connector.StoredSpread{}
	}
	return connector.StoredSpread{Mean: float64(s.sum) / float64(s.n), Min: s.min, Max: s.max}
}

// openMode reports whether the lobby open hours schedule was active at at,
// roughly: on workdays from 9:00 to 11:00 and 14:00 to 16:00.
func openMode(at time.Time) bool {
	hour := at.Hour()
	return !isWeekend(at) && (hour == 9 || hour == 10 || hour == 14 || hour == 15)
}
