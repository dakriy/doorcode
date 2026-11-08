//go:build slow

package connector_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"

	"github.com/dakriy/doorcode/internal/connector"
)

var doorCreated = time.Date(2026, 1, 5, 9, 0, 0, 0, time.UTC)

// plainDoor is a door with a name and nothing else.
func plainDoor(name string) connector.StoredDoor {
	return connector.StoredDoor{Name: name, OpenScheduleIDs: []int64{}, ZoneIDs: []int64{}, CreatedAt: doorCreated}
}

func mustCreateDoor(t *testing.T, store connector.Store, d connector.StoredDoor) int64 {
	t.Helper()
	id, err := store.CreateDoor(context.Background(), d)
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func mustDoor(t *testing.T, store connector.Store, id int64) connector.StoredDoor {
	t.Helper()
	d, found, err := store.Door(context.Background(), id)
	if err != nil || !found {
		t.Fatalf("door %d: found %v, error %v", id, found, err)
	}
	return d
}

func mustCreateZone(t *testing.T, store connector.Store, z connector.StoredZone) int64 {
	t.Helper()
	id, err := store.CreateZone(context.Background(), z)
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func mustAddPicture(t *testing.T, store connector.Store, doorID int64, image, thumbnail string) int64 {
	t.Helper()
	p := connector.StoredPicture{DoorID: doorID, Width: 30, Height: 20, CreatedAt: doorCreated}
	id, err := store.AddPicture(context.Background(), p, []byte(image), []byte(thumbnail))
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func TestStoreCreatesADoorWithItsSchedulesAndZones(t *testing.T) {
	for _, impl := range storeImpls {
		t.Run(impl.name, func(t *testing.T) {
			store := migrated(t, impl)
			nights := mustCreateSchedule(t, store, plainSchedule("Nights"))
			zone := mustCreateZone(t, store, connector.StoredZone{Name: "North", DoorIDs: []int64{}})
			d := connector.StoredDoor{
				Name: "Lobby", Location: "Building A", Notes: "Side.", Public: true,
				OpenScheduleIDs: []int64{1, nights}, ZoneIDs: []int64{zone}, CreatedAt: doorCreated,
			}

			id := mustCreateDoor(t, store, d)
			got := mustDoor(t, store, id)

			want := d
			want.ID, want.Config = 1, map[string]string{}
			if diff := cmp.Diff(want, got); diff != "" {
				t.Errorf("door (-want +got):\n%s", diff)
			}
		})
	}
}

func TestStoreListsDoorsByNameIgnoringCase(t *testing.T) {
	for _, impl := range storeImpls {
		t.Run(impl.name, func(t *testing.T) {
			store := migrated(t, impl)
			mustCreateDoor(t, store, plainDoor("lobby"))
			mustCreateDoor(t, store, plainDoor("Dock"))

			all, err := store.Doors(context.Background())
			if err != nil {
				t.Fatal(err)
			}

			var got []any
			for _, d := range all {
				got = append(got, []any{d.ID, d.Name, d.OpenScheduleIDs, d.ZoneIDs})
			}
			want := []any{[]any{int64(2), "Dock", []int64{}, []int64{}}, []any{int64(1), "lobby", []int64{}, []int64{}}}
			if diff := cmp.Diff(want, got); diff != "" {
				t.Errorf("doors (-want +got):\n%s", diff)
			}
		})
	}
}

func TestStoreUpdatesADoorReplacingItsSchedulesAndZones(t *testing.T) {
	for _, impl := range storeImpls {
		t.Run(impl.name, func(t *testing.T) {
			store := migrated(t, impl)
			nights := mustCreateSchedule(t, store, plainSchedule("Nights"))
			north := mustCreateZone(t, store, connector.StoredZone{Name: "North", DoorIDs: []int64{}})
			annex := mustCreateZone(t, store, connector.StoredZone{Name: "Annex", DoorIDs: []int64{}})
			start := plainDoor("Lobby")
			start.OpenScheduleIDs, start.ZoneIDs = []int64{1}, []int64{north}
			id := mustCreateDoor(t, store, start)
			changed := connector.StoredDoor{
				ID: id, Name: "Main lobby", Location: "B", Notes: "N", Public: true,
				OpenScheduleIDs: []int64{nights}, ZoneIDs: []int64{annex}, CreatedAt: time.Date(2030, 1, 1, 0, 0, 0, 0, time.UTC),
			}

			updated, err := store.UpdateDoor(context.Background(), changed)
			got := mustDoor(t, store, id)

			want := changed
			want.CreatedAt, want.Config = doorCreated, map[string]string{}
			if diff := cmp.Diff([]any{true, nil, want}, []any{updated, err, got}); diff != "" {
				t.Errorf("updated, error, door (-want +got):\n%s", diff)
			}
		})
	}
}

func TestStoreDeletesAndRestoresADoor(t *testing.T) {
	for _, impl := range storeImpls {
		t.Run(impl.name, func(t *testing.T) {
			ctx := context.Background()
			store := migrated(t, impl)
			id := mustCreateDoor(t, store, plainDoor("Lobby"))
			at := time.Date(2026, 2, 1, 12, 0, 0, 0, time.UTC)

			deleted, deleteErr := store.SetDoorDeleted(ctx, id, &at)
			whileDeleted := mustDoor(t, store, id).DeletedAt
			restored, restoreErr := store.SetDoorDeleted(ctx, id, nil)
			after := mustDoor(t, store, id).DeletedAt

			want := []any{true, nil, &at, true, nil, (*time.Time)(nil)}
			if diff := cmp.Diff(want, []any{deleted, deleteErr, whileDeleted, restored, restoreErr, after}); diff != "" {
				t.Errorf("delete, restore (-want +got):\n%s", diff)
			}
		})
	}
}

func TestStoreReportsADoorThatIsNotThere(t *testing.T) {
	for _, impl := range storeImpls {
		t.Run(impl.name, func(t *testing.T) {
			ctx := context.Background()
			store := migrated(t, impl)
			missing := plainDoor("Missing")
			missing.ID = 9

			_, found, readErr := store.Door(ctx, 9)
			updated, updateErr := store.UpdateDoor(ctx, missing)
			deleted, deleteErr := store.SetDoorDeleted(ctx, 9, nil)
			primary, primaryErr := store.SetPrimaryPicture(ctx, 9, 1)

			want := []any{false, nil, false, nil, false, nil, false, nil}
			if diff := cmp.Diff(want, []any{found, readErr, updated, updateErr, deleted, deleteErr, primary, primaryErr}); diff != "" {
				t.Errorf("read, update, delete, primary picture (-want +got):\n%s", diff)
			}
		})
	}
}

func TestStoreRefusesWhatADoorCannotHold(t *testing.T) {
	cases := map[string]func(d connector.StoredDoor) connector.StoredDoor{
		"a taken name ignoring case": func(d connector.StoredDoor) connector.StoredDoor {
			d.Name = "LOBBY"
			return d
		},
		"an unknown schedule": func(d connector.StoredDoor) connector.StoredDoor {
			d.OpenScheduleIDs = []int64{9}
			return d
		},
		"an unknown zone": func(d connector.StoredDoor) connector.StoredDoor {
			d.ZoneIDs = []int64{9}
			return d
		},
	}
	for _, impl := range storeImpls {
		for name, change := range cases {
			t.Run(impl.name+"/"+name, func(t *testing.T) {
				store := migrated(t, impl)
				mustCreateDoor(t, store, plainDoor("Lobby"))

				_, err := store.CreateDoor(context.Background(), change(plainDoor("Dock")))

				if err == nil {
					t.Error("CreateDoor stored a door with " + name)
				}
			})
		}
	}
}

func TestStoreTakesADeletedScheduleOffItsDoors(t *testing.T) {
	for _, impl := range storeImpls {
		t.Run(impl.name, func(t *testing.T) {
			ctx := context.Background()
			store := migrated(t, impl)
			nights := mustCreateSchedule(t, store, plainSchedule("Nights"))
			d := plainDoor("Lobby")
			d.OpenScheduleIDs = []int64{1, nights}
			id := mustCreateDoor(t, store, d)

			if _, err := store.DeleteSchedule(ctx, nights); err != nil {
				t.Fatal(err)
			}

			if diff := cmp.Diff([]int64{1}, mustDoor(t, store, id).OpenScheduleIDs); diff != "" {
				t.Errorf("open schedules (-want +got):\n%s", diff)
			}
		})
	}
}

func TestStoreKeepsPicturesOfEachDoor(t *testing.T) {
	for _, impl := range storeImpls {
		t.Run(impl.name, func(t *testing.T) {
			ctx := context.Background()
			store := migrated(t, impl)
			lobby := mustCreateDoor(t, store, plainDoor("Lobby"))
			dock := mustCreateDoor(t, store, plainDoor("Dock"))
			first := mustAddPicture(t, store, lobby, "image one", "thumb one")
			mustAddPicture(t, store, dock, "image two", "thumb two")
			third := mustAddPicture(t, store, lobby, "image three", "thumb three")

			pictures, err := store.Pictures(ctx, lobby)
			image, imageFound, imageErr := store.PictureImage(ctx, lobby, third)
			thumbnail, thumbnailFound, thumbnailErr := store.PictureThumbnail(ctx, lobby, first)
			_, otherFound, otherErr := store.PictureImage(ctx, dock, first)

			want := []any{
				[]connector.StoredPicture{
					{ID: first, DoorID: lobby, Width: 30, Height: 20, CreatedAt: doorCreated},
					{ID: third, DoorID: lobby, Width: 30, Height: 20, CreatedAt: doorCreated},
				}, nil,
				"image three", true, nil, "thumb one", true, nil, false, nil,
			}
			got := []any{pictures, err, string(image), imageFound, imageErr, string(thumbnail), thumbnailFound, thumbnailErr, otherFound, otherErr}
			if diff := cmp.Diff(want, got); diff != "" {
				t.Errorf("pictures, image, thumbnail, another door's (-want +got):\n%s", diff)
			}
		})
	}
}

func TestStoreSetsAndClearsADoorsPrimaryPicture(t *testing.T) {
	for _, impl := range storeImpls {
		t.Run(impl.name, func(t *testing.T) {
			ctx := context.Background()
			store := migrated(t, impl)
			id := mustCreateDoor(t, store, plainDoor("Lobby"))
			picture := mustAddPicture(t, store, id, "image", "thumb")

			set, setErr := store.SetPrimaryPicture(ctx, id, picture)
			primary := mustDoor(t, store, id).PrimaryPictureID
			deleted, deleteErr := store.DeletePicture(ctx, id, picture)
			after := mustDoor(t, store, id).PrimaryPictureID
			again, againErr := store.DeletePicture(ctx, id, picture)

			want := []any{true, nil, &picture, true, nil, (*int64)(nil), false, nil}
			if diff := cmp.Diff(want, []any{set, setErr, primary, deleted, deleteErr, after, again, againErr}); diff != "" {
				t.Errorf("set, primary, delete, after, delete again (-want +got):\n%s", diff)
			}
		})
	}
}

func TestStoreRefusesAnotherDoorsPictureAsPrimary(t *testing.T) {
	for _, impl := range storeImpls {
		t.Run(impl.name, func(t *testing.T) {
			store := migrated(t, impl)
			lobby := mustCreateDoor(t, store, plainDoor("Lobby"))
			dock := mustCreateDoor(t, store, plainDoor("Dock"))
			picture := mustAddPicture(t, store, dock, "image", "thumb")

			_, err := store.SetPrimaryPicture(context.Background(), lobby, picture)

			if err == nil {
				t.Error("SetPrimaryPicture made another door's picture the primary one")
			}
		})
	}
}

func TestStoreRefusesAPictureWithoutItsDoorOrData(t *testing.T) {
	cases := map[string]struct {
		door             int64
		image, thumbnail []byte
	}{
		"an unknown door": {9, []byte("image"), []byte("thumb")},
		"no image":        {1, nil, []byte("thumb")},
		"no thumbnail":    {1, []byte("image"), nil},
	}
	for _, impl := range storeImpls {
		for name, c := range cases {
			t.Run(impl.name+"/"+name, func(t *testing.T) {
				store := migrated(t, impl)
				mustCreateDoor(t, store, plainDoor("Lobby"))
				p := connector.StoredPicture{DoorID: c.door, Width: 1, Height: 1, CreatedAt: doorCreated}

				_, err := store.AddPicture(context.Background(), p, c.image, c.thumbnail)

				if err == nil {
					t.Error("AddPicture stored a picture with " + name)
				}
			})
		}
	}
}

func TestStoreKeepsZonesAndTheirDoors(t *testing.T) {
	for _, impl := range storeImpls {
		t.Run(impl.name, func(t *testing.T) {
			ctx := context.Background()
			store := migrated(t, impl)
			lobby := mustCreateDoor(t, store, plainDoor("Lobby"))
			dock := mustCreateDoor(t, store, plainDoor("Dock"))
			west := mustCreateZone(t, store, connector.StoredZone{Name: "west", Description: "West wing.", DoorIDs: []int64{dock, lobby}})
			east := mustCreateZone(t, store, connector.StoredZone{Name: "East", DoorIDs: []int64{lobby}})

			zones, err := store.Zones(ctx)
			zone, found, zoneErr := store.Zone(ctx, west)
			door := mustDoor(t, store, lobby).ZoneIDs

			want := []any{
				[]connector.StoredZone{
					{ID: east, Name: "East", DoorIDs: []int64{lobby}},
					{ID: west, Name: "west", Description: "West wing.", DoorIDs: []int64{lobby, dock}},
				}, nil,
				connector.StoredZone{ID: west, Name: "west", Description: "West wing.", DoorIDs: []int64{lobby, dock}}, true, nil,
				[]int64{west, east},
			}
			if diff := cmp.Diff(want, []any{zones, err, zone, found, zoneErr, door}); diff != "" {
				t.Errorf("zones, zone, the lobby's zones (-want +got):\n%s", diff)
			}
		})
	}
}

func TestStoreUpdatesAZoneReplacingItsDoors(t *testing.T) {
	for _, impl := range storeImpls {
		t.Run(impl.name, func(t *testing.T) {
			ctx := context.Background()
			store := migrated(t, impl)
			lobby := mustCreateDoor(t, store, plainDoor("Lobby"))
			dock := mustCreateDoor(t, store, plainDoor("Dock"))
			id := mustCreateZone(t, store, connector.StoredZone{Name: "West", DoorIDs: []int64{lobby}})
			changed := connector.StoredZone{ID: id, Name: "West wing", Description: "D", DoorIDs: []int64{dock}}

			updated, err := store.UpdateZone(ctx, changed)
			got, _, _ := store.Zone(ctx, id)

			if diff := cmp.Diff([]any{true, nil, changed}, []any{updated, err, got}); diff != "" {
				t.Errorf("updated, error, zone (-want +got):\n%s", diff)
			}
		})
	}
}

func TestStoreDeletesAZoneKeepingItsDoors(t *testing.T) {
	for _, impl := range storeImpls {
		t.Run(impl.name, func(t *testing.T) {
			ctx := context.Background()
			store := migrated(t, impl)
			lobby := mustCreateDoor(t, store, plainDoor("Lobby"))
			id := mustCreateZone(t, store, connector.StoredZone{Name: "West", DoorIDs: []int64{lobby}})

			deleted, err := store.DeleteZone(ctx, id)
			_, found, _ := store.Zone(ctx, id)
			door := mustDoor(t, store, lobby).ZoneIDs

			if diff := cmp.Diff([]any{true, nil, false, []int64{}}, []any{deleted, err, found, door}); diff != "" {
				t.Errorf("deleted, error, still there, the door's zones (-want +got):\n%s", diff)
			}
		})
	}
}

func TestStoreReportsAZoneThatIsNotThere(t *testing.T) {
	for _, impl := range storeImpls {
		t.Run(impl.name, func(t *testing.T) {
			ctx := context.Background()
			store := migrated(t, impl)

			_, found, readErr := store.Zone(ctx, 9)
			updated, updateErr := store.UpdateZone(ctx, connector.StoredZone{ID: 9, Name: "X", DoorIDs: []int64{}})
			deleted, deleteErr := store.DeleteZone(ctx, 9)

			want := []any{false, nil, false, nil, false, nil}
			if diff := cmp.Diff(want, []any{found, readErr, updated, updateErr, deleted, deleteErr}); diff != "" {
				t.Errorf("read, update, delete (-want +got):\n%s", diff)
			}
		})
	}
}

func TestStoreRefusesWhatAZoneCannotHold(t *testing.T) {
	cases := map[string]connector.StoredZone{
		"a taken name ignoring case": {Name: "WEST", DoorIDs: []int64{}},
		"an unknown door":            {Name: "East", DoorIDs: []int64{9}},
	}
	for _, impl := range storeImpls {
		for name, z := range cases {
			t.Run(impl.name+"/"+name, func(t *testing.T) {
				store := migrated(t, impl)
				mustCreateZone(t, store, connector.StoredZone{Name: "West", DoorIDs: []int64{}})

				_, err := store.CreateZone(context.Background(), z)

				if err == nil {
					t.Error("CreateZone stored a zone with " + name)
				}
			})
		}
	}
}
