//go:build slow

package connector_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"

	"github.com/dakriy/doorcode/internal/connector"
)

func TestStoreReportsAnErrorBeforeTheSchemaIsMigrated(t *testing.T) {
	for _, impl := range storeImpls {
		t.Run(impl.name, func(t *testing.T) {
			_, err := impl.fresh(t).Settings(context.Background())

			if err == nil {
				t.Error("Settings succeeded on an unmigrated store")
			}
		})
	}
}

func TestStoreMigrationCanRunAgain(t *testing.T) {
	for _, impl := range storeImpls {
		t.Run(impl.name, func(t *testing.T) {
			store := migrated(t, impl)

			if err := store.Migrate(context.Background()); err != nil {
				t.Errorf("second Migrate: %v", err)
			}
		})
	}
}

func TestStoreStartsWithNoSettings(t *testing.T) {
	for _, impl := range storeImpls {
		t.Run(impl.name, func(t *testing.T) {
			got, err := migrated(t, impl).Settings(context.Background())
			if err != nil {
				t.Fatal(err)
			}

			if diff := cmp.Diff(map[string]connector.StoredSetting{}, got); diff != "" {
				t.Errorf("settings (-want +got):\n%s", diff)
			}
		})
	}
}

func TestStoreReturnsEachPutSettingWithItsTime(t *testing.T) {
	for _, impl := range storeImpls {
		t.Run(impl.name, func(t *testing.T) {
			ctx := context.Background()
			store := migrated(t, impl)
			nameAt := time.Date(2026, 3, 1, 9, 30, 15, 123456000, time.UTC)
			zoneAt := time.Date(2026, 3, 2, 18, 0, 0, 0, time.UTC)

			if err := store.PutSetting(ctx, "site_name", "North Campus", nameAt); err != nil {
				t.Fatal(err)
			}
			if err := store.PutSetting(ctx, "site_timezone", "Asia/Tokyo", zoneAt); err != nil {
				t.Fatal(err)
			}
			got, err := store.Settings(ctx)
			if err != nil {
				t.Fatal(err)
			}

			want := map[string]connector.StoredSetting{
				"site_name":     {Value: "North Campus", UpdatedAt: nameAt},
				"site_timezone": {Value: "Asia/Tokyo", UpdatedAt: zoneAt},
			}
			if diff := cmp.Diff(want, got); diff != "" {
				t.Errorf("settings (-want +got):\n%s", diff)
			}
		})
	}
}

func TestStorePutReplacesTheEarlierValue(t *testing.T) {
	for _, impl := range storeImpls {
		t.Run(impl.name, func(t *testing.T) {
			ctx := context.Background()
			store := migrated(t, impl)
			later := time.Date(2026, 3, 2, 8, 0, 0, 0, time.UTC)

			if err := store.PutSetting(ctx, "site_name", "Old", time.Date(2026, 3, 1, 8, 0, 0, 0, time.UTC)); err != nil {
				t.Fatal(err)
			}
			if err := store.PutSetting(ctx, "site_name", "New", later); err != nil {
				t.Fatal(err)
			}
			got, err := store.Settings(ctx)
			if err != nil {
				t.Fatal(err)
			}

			want := map[string]connector.StoredSetting{"site_name": {Value: "New", UpdatedAt: later}}
			if diff := cmp.Diff(want, got); diff != "" {
				t.Errorf("settings (-want +got):\n%s", diff)
			}
		})
	}
}
