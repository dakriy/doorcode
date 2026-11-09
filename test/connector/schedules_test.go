//go:build slow

package connector_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"

	"github.com/dakriy/doorcode/internal/connector"
	"github.com/dakriy/doorcode/internal/testsupport"
)

func day(year int, month time.Month, d int) time.Time {
	return time.Date(year, month, d, 0, 0, 0, 0, time.UTC)
}

// nightShift is a schedule with every field set.
func nightShift() connector.StoredSchedule {
	starts, ends := day(2026, 1, 1), day(2026, 12, 31)
	return connector.StoredSchedule{
		Name: "Night shift", StartsOn: &starts, EndsOn: &ends,
		Windows: []connector.StoredWindow{
			{Days: []time.Weekday{time.Monday, time.Friday}, Start: 22 * 60, End: 6 * 60},
			{Days: []time.Weekday{time.Sunday}, Start: 7*60 + 5, End: 7*60 + 30},
		},
		Excluded: []connector.StoredExclusion{
			{First: day(2026, 1, 1), Last: day(2026, 1, 1)},
			{First: day(2026, 12, 24), Last: day(2026, 12, 26)},
		},
	}
}

// plainSchedule is a schedule with one window and nothing else.
func plainSchedule(name string) connector.StoredSchedule {
	return connector.StoredSchedule{
		Name:     name,
		Windows:  []connector.StoredWindow{{Days: []time.Weekday{time.Saturday}, Start: 600, End: 960}},
		Excluded: []connector.StoredExclusion{},
	}
}

func mustCreateSchedule(t *testing.T, store connector.Store, sch connector.StoredSchedule) int64 {
	t.Helper()
	id, err := store.CreateSchedule(context.Background(), sch)
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func mustSchedule(t *testing.T, store connector.Store, id int64) connector.StoredSchedule {
	t.Helper()
	sch, found, err := store.Schedule(context.Background(), id)
	if err != nil || !found {
		t.Fatalf("schedule %d: found %v, error %v", id, found, err)
	}
	return sch
}

func TestStoreSeedsTheAlwaysSchedule(t *testing.T) {
	for _, impl := range storeImpls {
		t.Run(impl.name, func(t *testing.T) {
			got, err := migrated(t, impl).Schedules(context.Background())
			if err != nil {
				t.Fatal(err)
			}

			if diff := cmp.Diff([]connector.StoredSchedule{testsupport.AlwaysSchedule}, got); diff != "" {
				t.Errorf("schedules (-want +got):\n%s", diff)
			}
		})
	}
}

func TestStoreCreatesAScheduleThatIsNeverBuiltIn(t *testing.T) {
	for _, impl := range storeImpls {
		t.Run(impl.name, func(t *testing.T) {
			store := migrated(t, impl)
			sch := nightShift()
			sch.Builtin = true

			id := mustCreateSchedule(t, store, sch)
			got := mustSchedule(t, store, id)

			want := nightShift()
			want.ID = 2
			if diff := cmp.Diff(want, got); diff != "" {
				t.Errorf("schedule (-want +got):\n%s", diff)
			}
		})
	}
}

func TestStoreListsSchedulesByNameIgnoringCase(t *testing.T) {
	for _, impl := range storeImpls {
		t.Run(impl.name, func(t *testing.T) {
			store := migrated(t, impl)
			mustCreateSchedule(t, store, plainSchedule("night"))
			mustCreateSchedule(t, store, plainSchedule("Day"))

			all, err := store.Schedules(context.Background())
			if err != nil {
				t.Fatal(err)
			}

			var got []any
			for _, sch := range all {
				got = append(got, []any{sch.ID, sch.Name, len(sch.Windows)})
			}
			want := []any{[]any{int64(1), "Always", 1}, []any{int64(3), "Day", 1}, []any{int64(2), "night", 1}}
			if diff := cmp.Diff(want, got); diff != "" {
				t.Errorf("schedules (-want +got):\n%s", diff)
			}
		})
	}
}

func TestStoreUpdatesAScheduleReplacingItsWindows(t *testing.T) {
	for _, impl := range storeImpls {
		t.Run(impl.name, func(t *testing.T) {
			store := migrated(t, impl)
			id := mustCreateSchedule(t, store, nightShift())
			changed := plainSchedule("Weekend")
			changed.ID = id

			updated, err := store.UpdateSchedule(context.Background(), changed)
			got := mustSchedule(t, store, id)

			if diff := cmp.Diff([]any{true, nil, changed}, []any{updated, err, got}); diff != "" {
				t.Errorf("updated, error, schedule (-want +got):\n%s", diff)
			}
		})
	}
}

func TestStoreKeepsTheAlwaysScheduleBuiltInWhenUpdated(t *testing.T) {
	for _, impl := range storeImpls {
		t.Run(impl.name, func(t *testing.T) {
			store := migrated(t, impl)
			changed := plainSchedule("Always")
			changed.ID = 1

			if _, err := store.UpdateSchedule(context.Background(), changed); err != nil {
				t.Fatal(err)
			}

			if diff := cmp.Diff(true, mustSchedule(t, store, 1).Builtin); diff != "" {
				t.Errorf("built in (-want +got):\n%s", diff)
			}
		})
	}
}

func TestStoreReportsAScheduleThatIsNotThere(t *testing.T) {
	for _, impl := range storeImpls {
		t.Run(impl.name, func(t *testing.T) {
			ctx := context.Background()
			store := migrated(t, impl)
			missing := plainSchedule("Missing")
			missing.ID = 9

			_, found, readErr := store.Schedule(ctx, 9)
			updated, updateErr := store.UpdateSchedule(ctx, missing)
			deleted, deleteErr := store.DeleteSchedule(ctx, 9)

			want := []any{false, nil, false, nil, false, nil}
			if diff := cmp.Diff(want, []any{found, readErr, updated, updateErr, deleted, deleteErr}); diff != "" {
				t.Errorf("read, update, delete (-want +got):\n%s", diff)
			}
		})
	}
}

func TestStoreDeletesASchedule(t *testing.T) {
	for _, impl := range storeImpls {
		t.Run(impl.name, func(t *testing.T) {
			ctx := context.Background()
			store := migrated(t, impl)
			id := mustCreateSchedule(t, store, nightShift())

			deleted, err := store.DeleteSchedule(ctx, id)
			_, found, _ := store.Schedule(ctx, id)

			if diff := cmp.Diff([]any{true, nil, false}, []any{deleted, err, found}); diff != "" {
				t.Errorf("deleted, error, still there (-want +got):\n%s", diff)
			}
		})
	}
}

func TestStoreRefusesAScheduleNameTakenIgnoringCase(t *testing.T) {
	for _, impl := range storeImpls {
		t.Run(impl.name, func(t *testing.T) {
			_, err := migrated(t, impl).CreateSchedule(context.Background(), plainSchedule("ALWAYS"))

			if err == nil {
				t.Error("CreateSchedule stored a second schedule named Always")
			}
		})
	}
}

func TestStoreRefusesAScheduleItsConstraintsWould(t *testing.T) {
	cases := map[string]func(sch connector.StoredSchedule) connector.StoredSchedule{
		"without a window's days": func(sch connector.StoredSchedule) connector.StoredSchedule {
			sch.Windows = []connector.StoredWindow{{Start: 1, End: 2}}
			return sch
		},
		"with an exclusion ending before it starts": func(sch connector.StoredSchedule) connector.StoredSchedule {
			sch.Excluded = []connector.StoredExclusion{{First: day(2026, 12, 26), Last: day(2026, 12, 25)}}
			return sch
		},
	}
	for _, impl := range storeImpls {
		for name, change := range cases {
			t.Run(impl.name+"/"+name, func(t *testing.T) {
				ctx := context.Background()
				store := migrated(t, impl)
				id := mustCreateSchedule(t, store, plainSchedule("Kept"))
				changed := change(plainSchedule("Changed"))
				changed.ID = id

				_, createErr := store.CreateSchedule(ctx, change(plainSchedule("Lists")))
				_, updateErr := store.UpdateSchedule(ctx, changed)

				if diff := cmp.Diff([]any{true, true, "Kept"}, []any{createErr != nil, updateErr != nil, mustSchedule(t, store, id).Name}); diff != "" {
					t.Errorf("create refused, update refused, name kept (-want +got):\n%s", diff)
				}
			})
		}
	}
}

func TestStoreReadsAScheduleStoredWithoutExclusionsAsHavingNone(t *testing.T) {
	for _, impl := range storeImpls {
		t.Run(impl.name, func(t *testing.T) {
			store := migrated(t, impl)
			sch := plainSchedule("None")
			sch.Excluded = nil

			got := mustSchedule(t, store, mustCreateSchedule(t, store, sch)).Excluded

			if diff := cmp.Diff([]connector.StoredExclusion{}, got); diff != "" {
				t.Errorf("exclusions (-want +got):\n%s", diff)
			}
		})
	}
}
