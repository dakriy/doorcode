//go:build slow

package connector_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"

	"github.com/dakriy/doorcode/internal/connector"
)

// ruleWorld is a migrated store with the group Staff (2), the schedule
// Nights (2), the doors Lobby (1) and Dock (2), and the zones North (1)
// and Annex (2).
func ruleWorld(t *testing.T, impl storeImpl) connector.Store {
	t.Helper()
	store := migrated(t, impl)
	mustCreateGroup(t, store, group("staff", "Staff"))
	mustCreateSchedule(t, store, plainSchedule("Nights"))
	mustCreateDoor(t, store, plainDoor("Lobby"))
	mustCreateDoor(t, store, plainDoor("Dock"))
	mustCreateZone(t, store, connector.StoredZone{Name: "North", DoorIDs: []int64{}})
	mustCreateZone(t, store, connector.StoredZone{Name: "Annex", DoorIDs: []int64{}})
	return store
}

func mustCreateRule(t *testing.T, store connector.Store, r connector.StoredRule) int64 {
	t.Helper()
	id, err := store.CreateRule(context.Background(), r)
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func mustRules(t *testing.T, store connector.Store) []connector.StoredRule {
	t.Helper()
	rules, err := store.Rules(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	return rules
}

func TestStoreCreatesAndListsAccessRulesInIDOrder(t *testing.T) {
	for _, impl := range storeImpls {
		t.Run(impl.name, func(t *testing.T) {
			store := ruleWorld(t, impl)
			first := connector.StoredRule{GroupID: 2, ScheduleID: 2, DoorIDs: []int64{2, 1}, ZoneIDs: []int64{2}}
			second := connector.StoredRule{GroupID: 1, ScheduleID: 1, DoorIDs: []int64{}, ZoneIDs: []int64{2, 1}}

			mustCreateRule(t, store, first)
			mustCreateRule(t, store, second)
			one, found, err := store.Rule(context.Background(), 2)

			want := []any{
				[]connector.StoredRule{
					{ID: 1, GroupID: 2, ScheduleID: 2, DoorIDs: []int64{1, 2}, ZoneIDs: []int64{2}},
					{ID: 2, GroupID: 1, ScheduleID: 1, DoorIDs: []int64{}, ZoneIDs: []int64{1, 2}},
				},
				connector.StoredRule{ID: 2, GroupID: 1, ScheduleID: 1, DoorIDs: []int64{}, ZoneIDs: []int64{1, 2}}, true, nil,
			}
			if diff := cmp.Diff(want, []any{mustRules(t, store), one, found, err}); diff != "" {
				t.Errorf("rules, one rule (-want +got):\n%s", diff)
			}
		})
	}
}

func TestStoreUpdatesAnAccessRuleReplacingItsDoorsAndZones(t *testing.T) {
	for _, impl := range storeImpls {
		t.Run(impl.name, func(t *testing.T) {
			store := ruleWorld(t, impl)
			id := mustCreateRule(t, store, connector.StoredRule{GroupID: 2, ScheduleID: 2, DoorIDs: []int64{1}, ZoneIDs: []int64{1}})
			changed := connector.StoredRule{ID: id, GroupID: 1, ScheduleID: 1, DoorIDs: []int64{2}, ZoneIDs: []int64{}}

			updated, err := store.UpdateRule(context.Background(), changed)

			if diff := cmp.Diff([]any{true, nil, []connector.StoredRule{changed}}, []any{updated, err, mustRules(t, store)}); diff != "" {
				t.Errorf("updated, error, rules (-want +got):\n%s", diff)
			}
		})
	}
}

func TestStoreDeletesAnAccessRule(t *testing.T) {
	for _, impl := range storeImpls {
		t.Run(impl.name, func(t *testing.T) {
			store := ruleWorld(t, impl)
			id := mustCreateRule(t, store, connector.StoredRule{GroupID: 2, ScheduleID: 2, DoorIDs: []int64{1}, ZoneIDs: []int64{1}})

			deleted, err := store.DeleteRule(context.Background(), id)

			if diff := cmp.Diff([]any{true, nil, []connector.StoredRule{}}, []any{deleted, err, mustRules(t, store)}); diff != "" {
				t.Errorf("deleted, error, rules (-want +got):\n%s", diff)
			}
		})
	}
}

func TestStoreReportsAnAccessRuleThatIsNotThere(t *testing.T) {
	for _, impl := range storeImpls {
		t.Run(impl.name, func(t *testing.T) {
			ctx := context.Background()
			store := ruleWorld(t, impl)

			_, found, readErr := store.Rule(ctx, 9)
			updated, updateErr := store.UpdateRule(ctx, connector.StoredRule{ID: 9, GroupID: 1, ScheduleID: 1, DoorIDs: []int64{}, ZoneIDs: []int64{}})
			deleted, deleteErr := store.DeleteRule(ctx, 9)

			want := []any{false, nil, false, nil, false, nil}
			if diff := cmp.Diff(want, []any{found, readErr, updated, updateErr, deleted, deleteErr}); diff != "" {
				t.Errorf("read, update, delete (-want +got):\n%s", diff)
			}
		})
	}
}

func TestStoreRefusesAnAccessRuleNamingWhatItDoesNotHold(t *testing.T) {
	cases := map[string]connector.StoredRule{
		"a group":    {GroupID: 9, ScheduleID: 1, DoorIDs: []int64{}, ZoneIDs: []int64{}},
		"a schedule": {GroupID: 1, ScheduleID: 9, DoorIDs: []int64{}, ZoneIDs: []int64{}},
		"a door":     {GroupID: 1, ScheduleID: 1, DoorIDs: []int64{9}, ZoneIDs: []int64{}},
		"a zone":     {GroupID: 1, ScheduleID: 1, DoorIDs: []int64{}, ZoneIDs: []int64{9}},
	}
	for _, impl := range storeImpls {
		for name, r := range cases {
			t.Run(impl.name+"/"+name, func(t *testing.T) {
				store := ruleWorld(t, impl)

				_, err := store.CreateRule(context.Background(), r)

				if err == nil {
					t.Error("CreateRule stored a rule naming " + name + " that is not there")
				}
			})
		}
	}
}

func TestStoreDropsTheRulesOfADeletedGroupAndADeletedZoneFromRules(t *testing.T) {
	for _, impl := range storeImpls {
		t.Run(impl.name, func(t *testing.T) {
			ctx := context.Background()
			store := ruleWorld(t, impl)
			mustCreateRule(t, store, connector.StoredRule{GroupID: 2, ScheduleID: 2, DoorIDs: []int64{1}, ZoneIDs: []int64{}})
			kept := mustCreateRule(t, store, connector.StoredRule{GroupID: 1, ScheduleID: 2, DoorIDs: []int64{}, ZoneIDs: []int64{1, 2}})

			if _, err := store.DeleteGroup(ctx, 2); err != nil {
				t.Fatal(err)
			}
			if _, err := store.DeleteZone(ctx, 1); err != nil {
				t.Fatal(err)
			}

			want := []connector.StoredRule{{ID: kept, GroupID: 1, ScheduleID: 2, DoorIDs: []int64{}, ZoneIDs: []int64{2}}}
			if diff := cmp.Diff(want, mustRules(t, store)); diff != "" {
				t.Errorf("rules (-want +got):\n%s", diff)
			}
		})
	}
}

func TestStoreRefusesToDeleteAScheduleAnAccessRuleUses(t *testing.T) {
	for _, impl := range storeImpls {
		t.Run(impl.name, func(t *testing.T) {
			store := ruleWorld(t, impl)
			mustCreateRule(t, store, connector.StoredRule{GroupID: 2, ScheduleID: 2, DoorIDs: []int64{1}, ZoneIDs: []int64{}})

			_, err := store.DeleteSchedule(context.Background(), 2)

			if err == nil {
				t.Error("DeleteSchedule removed a schedule an access rule uses")
			}
		})
	}
}

var overrideCreated = time.Date(2026, 1, 5, 9, 0, 0, 0, time.UTC)

// override is an override of the door with id door from start for an
// hour, made by the first user.
func override(door int64, kind string, start time.Time) connector.StoredOverride {
	return connector.StoredOverride{DoorID: door, Kind: kind, Start: start, End: start.Add(time.Hour), Reason: "Because", CreatedBy: 1, CreatedAt: overrideCreated}
}

// overrideWorld is withAdmin with the doors Lobby (1) and Dock (2).
func overrideWorld(t *testing.T, impl storeImpl) connector.Store {
	t.Helper()
	store := withAdmin(t, impl)
	mustCreateDoor(t, store, plainDoor("Lobby"))
	mustCreateDoor(t, store, plainDoor("Dock"))
	return store
}

func mustCreateOverride(t *testing.T, store connector.Store, o connector.StoredOverride) int64 {
	t.Helper()
	id, err := store.CreateOverride(context.Background(), o)
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func TestStoreCreatesAnOverrideNamingItsCreator(t *testing.T) {
	for _, impl := range storeImpls {
		t.Run(impl.name, func(t *testing.T) {
			store := overrideWorld(t, impl)
			o := override(1, "locked", overrideCreated)

			id := mustCreateOverride(t, store, o)
			got, found, err := store.Override(context.Background(), id)

			want := o
			want.ID, want.CreatorName = 1, "Administrator"
			if diff := cmp.Diff([]any{want, true, nil}, []any{got, found, err}); diff != "" {
				t.Errorf("override, found, error (-want +got):\n%s", diff)
			}
		})
	}
}

func TestStoreListsADoorsOverridesEndingAfterATimeByStart(t *testing.T) {
	for _, impl := range storeImpls {
		t.Run(impl.name, func(t *testing.T) {
			store := overrideWorld(t, impl)
			late := mustCreateOverride(t, store, override(1, "open", overrideCreated.Add(3*time.Hour)))
			ended := mustCreateOverride(t, store, override(1, "open", overrideCreated.Add(-time.Hour)))
			early := mustCreateOverride(t, store, override(1, "locked", overrideCreated.Add(time.Hour)))
			sameStart := mustCreateOverride(t, store, override(1, "open", overrideCreated.Add(time.Hour)))
			mustCreateOverride(t, store, override(2, "open", overrideCreated.Add(time.Hour)))

			listed, err := store.DoorOverrides(context.Background(), 1, overrideCreated)

			var got []int64
			for _, o := range listed {
				got = append(got, o.ID)
			}
			if diff := cmp.Diff([]any{[]int64{early, sameStart, late}, nil}, []any{got, err}); diff != "" {
				t.Errorf("ids, error; %d ended (-want +got):\n%s", ended, diff)
			}
		})
	}
}

func TestStoreUpdatesAnOverrideKeepingItsDoorAndCreator(t *testing.T) {
	for _, impl := range storeImpls {
		t.Run(impl.name, func(t *testing.T) {
			ctx := context.Background()
			store := overrideWorld(t, impl)
			id := mustCreateOverride(t, store, override(1, "locked", overrideCreated))
			changed := connector.StoredOverride{
				ID: id, DoorID: 2, Kind: "open", Start: overrideCreated.Add(time.Hour), End: overrideCreated.Add(5 * time.Hour),
				Reason: "Changed", CreatedBy: 9, CreatedAt: overrideCreated.Add(time.Hour),
			}

			updated, err := store.UpdateOverride(ctx, changed)
			got, _, _ := store.Override(ctx, id)

			want := connector.StoredOverride{
				ID: id, DoorID: 1, Kind: "open", Start: changed.Start, End: changed.End, Reason: "Changed",
				CreatedBy: 1, CreatorName: "Administrator", CreatedAt: overrideCreated,
			}
			if diff := cmp.Diff([]any{true, nil, want}, []any{updated, err, got}); diff != "" {
				t.Errorf("updated, error, override (-want +got):\n%s", diff)
			}
		})
	}
}

func TestStoreDeletesAnOverrideAndReportsOneThatIsNotThere(t *testing.T) {
	for _, impl := range storeImpls {
		t.Run(impl.name, func(t *testing.T) {
			ctx := context.Background()
			store := overrideWorld(t, impl)
			id := mustCreateOverride(t, store, override(1, "locked", overrideCreated))

			deleted, deleteErr := store.DeleteOverride(ctx, id)
			_, found, readErr := store.Override(ctx, id)
			updated, updateErr := store.UpdateOverride(ctx, override(1, "open", overrideCreated))
			again, againErr := store.DeleteOverride(ctx, id)

			want := []any{true, nil, false, nil, false, nil, false, nil}
			if diff := cmp.Diff(want, []any{deleted, deleteErr, found, readErr, updated, updateErr, again, againErr}); diff != "" {
				t.Errorf("delete, read, update, delete again (-want +got):\n%s", diff)
			}
		})
	}
}

func TestStoreRefusesAnOverrideOfNoDoorOrByNoUser(t *testing.T) {
	cases := map[string]connector.StoredOverride{
		"no door": override(9, "open", overrideCreated),
		"no user": func() connector.StoredOverride { o := override(1, "open", overrideCreated); o.CreatedBy = 9; return o }(),
	}
	for _, impl := range storeImpls {
		for name, o := range cases {
			t.Run(impl.name+"/"+name, func(t *testing.T) {
				store := overrideWorld(t, impl)

				_, err := store.CreateOverride(context.Background(), o)

				if err == nil {
					t.Error("CreateOverride stored an override with " + name)
				}
			})
		}
	}
}
