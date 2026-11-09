//go:build slow

package connector_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"

	"github.com/dakriy/doorcode/internal/connector"
)

// berlin is the zone the stats tests read days and hours in: an hour
// ahead of UTC in March 2026, until the clocks change on 29 March.
func berlin(t *testing.T) *time.Location {
	t.Helper()
	zone, err := time.LoadLocation("Europe/Berlin")
	if err != nil {
		t.Fatal(err)
	}
	return zone
}

// utcAt is a time on a day of March 2026, in UTC.
func utcAt(day, hour, minute int) time.Time {
	return time.Date(2026, 3, day, hour, minute, 0, 0, time.UTC)
}

// statsWorld is overrideWorld with Bo (2) and these events, in Berlin's
// time: before the range, a check at Lobby at 23:59 on Saturday 28
// February; in it, Ada (1) let in at Lobby at 23:30 on Sunday 1 March,
// an unknown code denied there at midnight, Bo let in and then denied as
// expired at Dock at 09:15 and 09:45, and Ada opening Lobby from the UI
// at 10:00 (not a check); after it, a check at 11:00 on Tuesday 3 March.
func statsWorld(t *testing.T, impl storeImpl) connector.Store {
	t.Helper()
	store := overrideWorld(t, impl)
	ada, bo := int64(1), mustCreateUser(t, store, storedUser("bo@example.com"))
	for _, e := range []connector.StoredEvent{
		{At: utcAt(28, 22, 59).AddDate(0, -1, 0), DoorID: 1, Type: "check", Outcome: "denied", Reason: "no rule"},
		{At: utcAt(1, 22, 30), DoorID: 1, Type: "check", Outcome: "allowed", Reason: "access rule", UserID: &ada},
		{At: utcAt(1, 23, 0), DoorID: 1, Type: "check", Outcome: "denied", Reason: "no rule"},
		{At: utcAt(2, 8, 15), DoorID: 2, Type: "check", Outcome: "allowed", Reason: "access rule", UserID: &bo},
		{At: utcAt(2, 8, 45), DoorID: 2, Type: "check", Outcome: "denied", Reason: "expired", UserID: &bo},
		{At: utcAt(2, 9, 0), DoorID: 1, Type: "open", Outcome: "allowed", Reason: "admin UI", UserID: &ada},
		{At: utcAt(3, 10, 0), DoorID: 1, Type: "check", Outcome: "denied", Reason: "no rule"},
	} {
		if _, err := store.AddEvent(context.Background(), e); err != nil {
			t.Fatal(err)
		}
	}
	return store
}

func TestStoreSumsUpTheEventsOfARangeByDayHourReasonDoorAndUser(t *testing.T) {
	for _, impl := range storeImpls {
		t.Run(impl.name, func(t *testing.T) {
			store := statsWorld(t, impl)
			zone := berlin(t)
			from, to := time.Date(2026, 3, 1, 0, 0, 0, 0, zone), time.Date(2026, 3, 3, 0, 0, 0, 0, zone)

			got, err := store.EventStats(context.Background(), connector.EventRange{Type: "check", From: from, To: to, Zone: zone})

			first, second := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC), time.Date(2026, 3, 2, 0, 0, 0, 0, time.UTC)
			want := []any{connector.StoredEventStats{
				Days: []connector.StoredDayCount{
					{Day: first, Outcome: "allowed", Count: 1}, {Day: second, Outcome: "allowed", Count: 1}, {Day: second, Outcome: "denied", Count: 2},
				},
				Hours: []connector.StoredHourCount{
					{Weekday: time.Sunday, Hour: 23, Count: 1}, {Weekday: time.Monday, Hour: 0, Count: 1}, {Weekday: time.Monday, Hour: 9, Count: 2},
				},
				Reasons: []connector.StoredReasonCount{
					{Outcome: "allowed", Reason: "access rule", Count: 2}, {Outcome: "denied", Reason: "expired", Count: 1},
					{Outcome: "denied", Reason: "no rule", Count: 1},
				},
				Doors: []connector.StoredDoorCount{
					{DoorID: 1, Outcome: "allowed", Count: 1}, {DoorID: 1, Outcome: "denied", Count: 1},
					{DoorID: 2, Outcome: "allowed", Count: 1}, {DoorID: 2, Outcome: "denied", Count: 1},
				},
				Users: 2,
			}, nil}
			if diff := cmp.Diff(want, []any{got, err}); diff != "" {
				t.Errorf("stats, error (-want +got):\n%s", diff)
			}
		})
	}
}

func TestStoreReadsTheDaysAndHoursOfEventsInTheZoneItIsGiven(t *testing.T) {
	for _, impl := range storeImpls {
		t.Run(impl.name, func(t *testing.T) {
			store := statsWorld(t, impl)
			from, to := utcAt(1, 0, 0), utcAt(2, 0, 0)

			got, err := store.EventStats(context.Background(), connector.EventRange{Type: "check", From: from, To: to, Zone: time.UTC})

			want := []any{
				[]connector.StoredDayCount{{Day: from, Outcome: "allowed", Count: 1}, {Day: from, Outcome: "denied", Count: 1}},
				[]connector.StoredHourCount{{Weekday: time.Sunday, Hour: 22, Count: 1}, {Weekday: time.Sunday, Hour: 23, Count: 1}},
				1, nil,
			}
			if diff := cmp.Diff(want, []any{got.Days, got.Hours, got.Users, err}); diff != "" {
				t.Errorf("days, hours, users, error (-want +got):\n%s", diff)
			}
		})
	}
}

func TestStoreSumsUpNothingForARangeWithoutEvents(t *testing.T) {
	for _, impl := range storeImpls {
		t.Run(impl.name, func(t *testing.T) {
			store := statsWorld(t, impl)

			got, err := store.EventStats(context.Background(), connector.EventRange{Type: "check", From: utcAt(4, 0, 0), To: utcAt(5, 0, 0), Zone: time.UTC})

			want := []any{connector.StoredEventStats{
				Days: []connector.StoredDayCount{}, Hours: []connector.StoredHourCount{}, Reasons: []connector.StoredReasonCount{},
				Doors: []connector.StoredDoorCount{},
			}, nil}
			if diff := cmp.Diff(want, []any{got, err}); diff != "" {
				t.Errorf("stats, error (-want +got):\n%s", diff)
			}
		})
	}
}

// trendRollup is a rollup of the door whose id is door for the hour that
// starts at hour, of samples reports with mean signal and latency and
// one opening.
func trendRollup(door int64, hour time.Time, samples int, signal, latency float64) connector.StoredRollup {
	return connector.StoredRollup{
		DoorID: door, Hour: hour, Samples: samples, Signal: connector.StoredSpread{Mean: signal, Min: -80, Max: -40},
		Latency: connector.StoredSpread{Mean: latency, Min: 10, Max: 90}, FreeBytes: connector.StoredSpread{Mean: 1000, Min: 1000, Max: 1000}, Opens: 1,
	}
}

// trendWorld is overrideWorld with rollups of Lobby (1) for the hours
// starting at 22:00 and 23:00 UTC on 1 March and 08:00 and 09:00 on 2
// March, and of Dock (2) for 08:00 on 2 March, and one of Lobby for
// 00:00 on 3 March after the range the tests read.
func trendWorld(t *testing.T, impl storeImpl) connector.Store {
	t.Helper()
	store := overrideWorld(t, impl)
	rollups := []connector.StoredRollup{
		trendRollup(1, utcAt(1, 22, 0), 1, -60, 20), trendRollup(1, utcAt(1, 23, 0), 1, -64, 40),
		trendRollup(1, utcAt(2, 8, 0), 1, -50, 30), trendRollup(1, utcAt(2, 9, 0), 2, -70, 50),
		trendRollup(2, utcAt(2, 8, 0), 4, -55, 25), trendRollup(1, utcAt(3, 0, 0), 4, -40, 10),
	}
	if err := store.PutRollups(context.Background(), rollups); err != nil {
		t.Fatal(err)
	}
	return store
}

func TestStoreSumsUpRollupsByDoorAndTheDaysOfAZone(t *testing.T) {
	for _, impl := range storeImpls {
		t.Run(impl.name, func(t *testing.T) {
			store := trendWorld(t, impl)
			zone := berlin(t)

			got, err := store.Trends(context.Background(), connector.TrendQuery{From: utcAt(1, 0, 0), To: utcAt(3, 0, 0), Zone: zone, Unit: connector.ByDay})

			want := []any{[]connector.StoredTrend{
				{DoorID: 1, Start: time.Date(2026, 3, 1, 0, 0, 0, 0, zone), Hours: 1, Samples: 1, Signal: -60, Latency: 20, Opens: 1},
				{DoorID: 1, Start: time.Date(2026, 3, 2, 0, 0, 0, 0, zone), Hours: 3, Samples: 4, Signal: -63.5, Latency: 42.5, Opens: 3},
				{DoorID: 2, Start: time.Date(2026, 3, 2, 0, 0, 0, 0, zone), Hours: 1, Samples: 4, Signal: -55, Latency: 25, Opens: 1},
			}, nil}
			if diff := cmp.Diff(want, []any{got, err}); diff != "" {
				t.Errorf("trends, error (-want +got):\n%s", diff)
			}
		})
	}
}

func TestStoreSumsUpRollupsByTheHoursOfAZone(t *testing.T) {
	for _, impl := range storeImpls {
		t.Run(impl.name, func(t *testing.T) {
			store := trendWorld(t, impl)
			zone, err := time.LoadLocation("Asia/Kolkata")
			if err != nil {
				t.Fatal(err)
			}

			got, err := store.Trends(context.Background(), connector.TrendQuery{From: utcAt(2, 0, 0), To: utcAt(2, 9, 0), Zone: zone, Unit: connector.ByHour})

			want := []any{[]connector.StoredTrend{
				{DoorID: 1, Start: utcAt(2, 7, 30), Hours: 1, Samples: 1, Signal: -50, Latency: 30, Opens: 1},
				{DoorID: 2, Start: utcAt(2, 7, 30), Hours: 1, Samples: 4, Signal: -55, Latency: 25, Opens: 1},
			}, nil}
			if diff := cmp.Diff(want, []any{got, err}); diff != "" {
				t.Errorf("trends, error (-want +got):\n%s", diff)
			}
		})
	}
}

func TestStoreCountsTheDoorsReportingEachFirmwareVersion(t *testing.T) {
	for _, impl := range storeImpls {
		t.Run(impl.name, func(t *testing.T) {
			store := overrideWorld(t, impl)
			for _, name := range []string{"Attic", "Cellar", "Annex"} {
				mustCreateDoor(t, store, plainDoor(name))
			}
			for id, version := range map[int64]string{1: "1.4.2", 2: "1.5.0", 3: "1.4.2", 4: "1.5.0", 5: "0.9.0"} {
				c := controllerOf("hw" + version + string(rune('a'+id)))
				c.FirmwareVersion = version
				mustSetController(t, store, id, c)
			}
			if _, err := store.SetDoorDeleted(context.Background(), 5, &controllerSeen); err != nil {
				t.Fatal(err)
			}
			mustCreateDoor(t, store, plainDoor("Garage"))

			got, err := store.FirmwareVersions(context.Background())

			want := []any{[]connector.StoredVersionCount{{Version: "1.4.2", Doors: 2}, {Version: "1.5.0", Doors: 2}}, nil}
			if diff := cmp.Diff(want, []any{got, err}); diff != "" {
				t.Errorf("versions, error (-want +got):\n%s", diff)
			}
		})
	}
}
