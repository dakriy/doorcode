//go:build slow

package connector_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"

	"github.com/dakriy/doorcode/internal/connector"
)

// telemetryStart is the hour the samples of telemetryWorld start in.
var telemetryStart = time.Date(2026, 3, 2, 8, 0, 0, 0, time.UTC)

// sampleAt is a report of the door whose id is door, minutes after
// telemetryStart, with signal.
func sampleAt(door int64, minutes, signal int) connector.StoredSample {
	return connector.StoredSample{DoorID: door, StoredTelemetry: connector.StoredTelemetry{
		At: telemetryStart.Add(time.Duration(minutes) * time.Minute), Signal: signal, LatencyMS: 40, UptimeS: 3720,
		FreeBytes: 120832, FirmwareVersion: "1.4.2", Reconnects: 2, DoorOpen: minutes%2 == 0, Opens: 1,
	}}
}

// telemetryWorld is a store with the doors Lobby (1) and Dock (2) and
// reports from Dock at 08:30 and from Lobby at 08:00, 08:59, and 09:00.
func telemetryWorld(t *testing.T, impl storeImpl) connector.Store {
	t.Helper()
	store := overrideWorld(t, impl)
	for _, s := range []connector.StoredSample{sampleAt(2, 30, -70), sampleAt(1, 0, -60), sampleAt(1, 59, -61), sampleAt(1, 60, -62)} {
		if err := store.AddSample(context.Background(), s); err != nil {
			t.Fatal(err)
		}
	}
	return store
}

// samplesRead reads samples taken from from up to to.
type samplesRead func(store connector.Store, from, to time.Time) ([]connector.StoredSample, error)

// samplesOf reads the samples of the door whose id is door.
func samplesOf(door int64) samplesRead {
	return func(store connector.Store, from, to time.Time) ([]connector.StoredSample, error) {
		return store.Samples(context.Background(), door, from, to)
	}
}

// everySample reads the samples of every door.
func everySample(store connector.Store, from, to time.Time) ([]connector.StoredSample, error) {
	return store.AllSamples(context.Background(), from, to)
}

func TestStoreListsTheSamplesOfARangeByDoorAndTime(t *testing.T) {
	cases := map[string]struct {
		read     samplesRead
		from, to int
		want     []connector.StoredSample
	}{
		"every door": {everySample, 0, 60, []connector.StoredSample{sampleAt(1, 0, -60), sampleAt(1, 59, -61), sampleAt(2, 30, -70)}},
		"one door":   {samplesOf(1), 0, 61, []connector.StoredSample{sampleAt(1, 0, -60), sampleAt(1, 59, -61), sampleAt(1, 60, -62)}},
		"a range":    {samplesOf(1), 1, 60, []connector.StoredSample{sampleAt(1, 59, -61)}},
		"none":       {samplesOf(2), 31, 90, []connector.StoredSample{}},
	}
	for _, impl := range storeImpls {
		t.Run(impl.name, func(t *testing.T) {
			store := telemetryWorld(t, impl)
			for name, c := range cases {
				t.Run(name, func(t *testing.T) {
					from, to := telemetryStart.Add(time.Duration(c.from)*time.Minute), telemetryStart.Add(time.Duration(c.to)*time.Minute)
					got, err := c.read(store, from, to)

					if diff := cmp.Diff([]any{c.want, nil}, []any{nonNil(got), err}); diff != "" {
						t.Errorf("samples, error (-want +got):\n%s", diff)
					}
				})
			}
		})
	}
}

// nonNil is samples, empty rather than nil.
func nonNil(samples []connector.StoredSample) []connector.StoredSample {
	return append([]connector.StoredSample{}, samples...)
}

func TestStoreFindsTheFirstSampleSinceATime(t *testing.T) {
	for _, impl := range storeImpls {
		t.Run(impl.name, func(t *testing.T) {
			store := telemetryWorld(t, impl)
			ctx := context.Background()

			first, found, err := store.FirstSampleSince(ctx, telemetryStart.Add(time.Minute))
			_, later, laterErr := store.FirstSampleSince(ctx, telemetryStart.Add(61*time.Minute))

			want := []any{telemetryStart.Add(30 * time.Minute), true, nil, false, nil}
			if diff := cmp.Diff(want, []any{first, found, err, later, laterErr}); diff != "" {
				t.Errorf("first, found, error, found later, error (-want +got):\n%s", diff)
			}
		})
	}
}

func TestStoreRemovesTheSamplesBeforeATime(t *testing.T) {
	for _, impl := range storeImpls {
		t.Run(impl.name, func(t *testing.T) {
			store := telemetryWorld(t, impl)
			ctx := context.Background()

			removed, err := store.DeleteSamplesBefore(ctx, telemetryStart.Add(59*time.Minute))
			left, _ := store.AllSamples(ctx, telemetryStart, telemetryStart.Add(2*time.Hour))

			want := []any{2, nil, []connector.StoredSample{sampleAt(1, 59, -61), sampleAt(1, 60, -62)}}
			if diff := cmp.Diff(want, []any{removed, err, left}); diff != "" {
				t.Errorf("removed, error, left (-want +got):\n%s", diff)
			}
		})
	}
}

// rollupAt is a rollup of the door whose id is door for the hour hours
// after telemetryStart, of samples reports.
func rollupAt(door int64, hours, samples int) connector.StoredRollup {
	return connector.StoredRollup{
		DoorID: door, Hour: telemetryStart.Add(time.Duration(hours) * time.Hour), Samples: samples,
		Signal: connector.StoredSpread{Mean: -60.5, Min: -61, Max: -60}, Latency: connector.StoredSpread{Mean: 40, Min: 40, Max: 40},
		FreeBytes: connector.StoredSpread{Mean: 120832.25, Min: 120000, Max: 121000}, Opens: 3,
	}
}

func TestStoreKeepsRollupsReplacingThoseOfTheSameDoorAndHour(t *testing.T) {
	for _, impl := range storeImpls {
		t.Run(impl.name, func(t *testing.T) {
			store := overrideWorld(t, impl)
			ctx := context.Background()
			if err := store.PutRollups(ctx, []connector.StoredRollup{rollupAt(1, 0, 2), rollupAt(1, 1, 1), rollupAt(2, 0, 1)}); err != nil {
				t.Fatal(err)
			}

			err := store.PutRollups(ctx, []connector.StoredRollup{rollupAt(1, 1, 5)})
			got, listErr := store.Rollups(ctx, 1, telemetryStart, telemetryStart.Add(2*time.Hour))

			want := []any{nil, []connector.StoredRollup{rollupAt(1, 0, 2), rollupAt(1, 1, 5)}, nil}
			if diff := cmp.Diff(want, []any{err, got, listErr}); diff != "" {
				t.Errorf("error, rollups, error (-want +got):\n%s", diff)
			}
		})
	}
}

func TestStoreListsTheRollupsOfARangeAndTheNewest(t *testing.T) {
	for _, impl := range storeImpls {
		t.Run(impl.name, func(t *testing.T) {
			store := overrideWorld(t, impl)
			ctx := context.Background()
			_, none, noneErr := store.NewestRollup(ctx)
			if err := store.PutRollups(ctx, []connector.StoredRollup{rollupAt(1, 2, 1), rollupAt(1, 0, 1), rollupAt(2, 3, 1)}); err != nil {
				t.Fatal(err)
			}

			ranged, err := store.Rollups(ctx, 1, telemetryStart.Add(time.Hour), telemetryStart.Add(3*time.Hour))
			newest, found, newestErr := store.NewestRollup(ctx)

			want := []any{false, nil, []connector.StoredRollup{rollupAt(1, 2, 1)}, nil, telemetryStart.Add(3 * time.Hour), true, nil}
			if diff := cmp.Diff(want, []any{none, noneErr, ranged, err, newest, found, newestErr}); diff != "" {
				t.Errorf("found before, error, ranged, error, newest, found, error (-want +got):\n%s", diff)
			}
		})
	}
}

func TestStoreRemovesTheRollupsBeforeATime(t *testing.T) {
	for _, impl := range storeImpls {
		t.Run(impl.name, func(t *testing.T) {
			store := overrideWorld(t, impl)
			ctx := context.Background()
			if err := store.PutRollups(ctx, []connector.StoredRollup{rollupAt(1, 0, 1), rollupAt(1, 1, 1), rollupAt(2, 0, 1)}); err != nil {
				t.Fatal(err)
			}

			removed, err := store.DeleteRollupsBefore(ctx, telemetryStart.Add(time.Hour))
			left, _ := store.Rollups(ctx, 1, telemetryStart, telemetryStart.Add(2*time.Hour))

			want := []any{2, nil, []connector.StoredRollup{rollupAt(1, 1, 1)}}
			if diff := cmp.Diff(want, []any{removed, err, left}); diff != "" {
				t.Errorf("removed, error, left (-want +got):\n%s", diff)
			}
		})
	}
}

func TestStoreRefusesTelemetryOfADoorItDoesNotHold(t *testing.T) {
	for _, impl := range storeImpls {
		t.Run(impl.name, func(t *testing.T) {
			store := overrideWorld(t, impl)
			ctx := context.Background()

			sampleErr := store.AddSample(ctx, sampleAt(9, 0, -60))
			rollupErr := store.PutRollups(ctx, []connector.StoredRollup{rollupAt(9, 0, 1)})

			if diff := cmp.Diff([]bool{true, true}, []bool{sampleErr != nil, rollupErr != nil}); diff != "" {
				t.Errorf("refused sample, rollup (-want +got):\n%s", diff)
			}
		})
	}
}

func TestStoreKeepsAlarmEvents(t *testing.T) {
	for _, impl := range storeImpls {
		t.Run(impl.name, func(t *testing.T) {
			store := overrideWorld(t, impl)
			ctx := context.Background()
			e := connector.StoredEvent{At: telemetryStart, DoorID: 1, Type: "alarm", Outcome: "alert", Reason: "forced open", Detail: "Forced."}
			if _, err := store.AddEvent(ctx, e); err != nil {
				t.Fatal(err)
			}

			got, _, err := store.Events(ctx, connector.EventQuery{Outcome: "alert", Limit: 10})

			e.ID, e.DoorName = 1, "Lobby"
			if diff := cmp.Diff([]any{[]connector.StoredEvent{e}, nil}, []any{got, err}); diff != "" {
				t.Errorf("events, error (-want +got):\n%s", diff)
			}
		})
	}
}
