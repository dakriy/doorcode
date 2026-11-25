//go:build slow

package connector_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"

	"github.com/dakriy/doorcode/internal/connector"
)

func TestMigrationCountsEveryReportOfAStoredRollupAsCarryingItsLatency(t *testing.T) {
	ctx := context.Background()
	store, conn := migratedTo(t, 34)
	exec(t, conn,
		`insert into doors (name, location, notes, public, created_at) values ('Lobby', '', '', true, now())`,
		`insert into telemetry_rollups (door_id, hour, samples, signal_mean, signal_min, signal_max, latency_mean, latency_min, latency_max,
			free_bytes_mean, free_bytes_min, free_bytes_max, opens) values (1, '2026-03-02T08:00:00Z', 3, -60, -61, -59, 40, 30, 50, 1000, 1000, 1000, 2)`,
	)

	if err := store.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	rollups, err := store.Rollups(ctx, 1, telemetryStart, telemetryStart.Add(time.Hour))
	if len(rollups) != 1 {
		t.Fatalf("rollups %v, error %v", rollups, err)
	}

	want := []any{3, 3, &connector.StoredSpread{Mean: 40, Min: 30, Max: 50}, nil}
	got := []any{rollups[0].Samples, rollups[0].LatencySamples, rollups[0].Latency, err}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("samples, samples with a latency, latency, error (-want +got):\n%s", diff)
	}
}
