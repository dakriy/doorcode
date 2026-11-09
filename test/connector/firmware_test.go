//go:build slow

package connector_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"

	"github.com/dakriy/doorcode/internal/connector"
)

var firmwareUploaded = time.Date(2026, 1, 5, 9, 0, 0, 0, time.UTC)

// firmwareOf is firmware of version uploaded by the administrator,
// minutes after firmwareUploaded.
func firmwareOf(version string, minutes int) connector.StoredFirmware {
	return connector.StoredFirmware{
		Version: version, Notes: "Release " + version + ".", SHA256: "ab12", Size: 3, UploadedBy: 1,
		UploadedAt: firmwareUploaded.Add(time.Duration(minutes) * time.Minute),
	}
}

func mustAddFirmware(t *testing.T, store connector.Store, f connector.StoredFirmware) int64 {
	t.Helper()
	id, err := store.AddFirmware(context.Background(), f, []byte{1, 2, 3})
	if err != nil {
		t.Fatal(err)
	}
	return id
}

// firmwareWorld is a store with the administrator (1), the doors Lobby (1)
// and Dock (2), and firmware 1.4.3 (1) and 1.5.0 (2), uploaded a minute
// apart.
func firmwareWorld(t *testing.T, impl storeImpl) connector.Store {
	t.Helper()
	store := overrideWorld(t, impl)
	mustAddFirmware(t, store, firmwareOf("1.4.3", 0))
	mustAddFirmware(t, store, firmwareOf("1.5.0", 1))
	return store
}

// mustFirmwares is every firmware store holds.
func mustFirmwares(t *testing.T, store connector.Store) []connector.StoredFirmware {
	t.Helper()
	all, err := store.Firmwares(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	return all
}

// mustSetDoorFirmware makes the firmware whose id is id the one the door
// whose id is door runs, nil the default.
func mustSetDoorFirmware(t *testing.T, store connector.Store, door int64, id *int64) {
	t.Helper()
	if found, err := store.SetDoorFirmware(context.Background(), door, id); err != nil || !found {
		t.Fatalf("set the firmware of door %d: found %v, error %v", door, found, err)
	}
}

// named is f stored as firmware id, uploaded by the administrator.
func named(f connector.StoredFirmware, id int64, isDefault bool) connector.StoredFirmware {
	f.ID, f.UploaderName, f.Default = id, "Administrator", isDefault
	return f
}

func TestStoreListsFirmwareNewestFirstWithItsUploader(t *testing.T) {
	for _, impl := range storeImpls {
		t.Run(impl.name, func(t *testing.T) {
			store := firmwareWorld(t, impl)

			all, err := store.Firmwares(context.Background())
			one, found, oneErr := store.Firmware(context.Background(), 1)
			image, imageFound, imageErr := store.FirmwareImage(context.Background(), 2)

			want := []any{
				[]connector.StoredFirmware{named(firmwareOf("1.5.0", 1), 2, false), named(firmwareOf("1.4.3", 0), 1, false)},
				named(firmwareOf("1.4.3", 0), 1, false), true, []byte{1, 2, 3}, true,
			}
			if diff := cmp.Diff(want, []any{all, one, found, image, imageFound}); diff != "" {
				t.Errorf("list, one, image (-want +got):\n%s", diff)
			}
			for _, e := range []error{err, oneErr, imageErr} {
				if e != nil {
					t.Fatal(e)
				}
			}
		})
	}
}

func TestStoreReportsFirmwareItDoesNotHold(t *testing.T) {
	for _, impl := range storeImpls {
		t.Run(impl.name, func(t *testing.T) {
			store := firmwareWorld(t, impl)

			_, found, err := store.Firmware(context.Background(), 9)
			_, imageFound, imageErr := store.FirmwareImage(context.Background(), 9)
			defaultFound, defaultErr := store.SetDefaultFirmware(context.Background(), 9)
			deleted, deleteErr := store.DeleteFirmware(context.Background(), 9)

			for _, e := range []error{err, imageErr, defaultErr, deleteErr} {
				if e != nil {
					t.Fatal(e)
				}
			}
			if diff := cmp.Diff([]bool{false, false, false, false}, []bool{found, imageFound, defaultFound, deleted}); diff != "" {
				t.Errorf("found firmware, image, default, deleted (-want +got):\n%s", diff)
			}
		})
	}
}

func TestStoreKeepsOneDefaultFirmware(t *testing.T) {
	for _, impl := range storeImpls {
		t.Run(impl.name, func(t *testing.T) {
			store := firmwareWorld(t, impl)
			defaults := func() []bool {
				all := mustFirmwares(t, store)
				return []bool{all[0].Default, all[1].Default}
			}

			var got [][]bool
			for _, id := range []int64{1, 2} {
				if found, err := store.SetDefaultFirmware(context.Background(), id); err != nil || !found {
					t.Fatalf("set default %d: found %v, error %v", id, found, err)
				}
				got = append(got, defaults())
			}
			if err := store.ClearDefaultFirmware(context.Background()); err != nil {
				t.Fatal(err)
			}
			got = append(got, defaults())

			if diff := cmp.Diff([][]bool{{false, true}, {true, false}, {false, false}}, got); diff != "" {
				t.Errorf("1.5.0 and 1.4.3 default after choosing 1.4.3, 1.5.0, none (-want +got):\n%s", diff)
			}
		})
	}
}

func TestStoreKeepsTheDefaultFirmwareWhenAskedForFirmwareItDoesNotHold(t *testing.T) {
	for _, impl := range storeImpls {
		t.Run(impl.name, func(t *testing.T) {
			store := firmwareWorld(t, impl)
			if found, err := store.SetDefaultFirmware(context.Background(), 1); err != nil || !found {
				t.Fatalf("set default 1: found %v, error %v", found, err)
			}

			found, err := store.SetDefaultFirmware(context.Background(), 9)
			all := mustFirmwares(t, store)

			if diff := cmp.Diff([]any{false, nil, false, true}, []any{found, err, all[0].Default, all[1].Default}); diff != "" {
				t.Errorf("found, error, 1.5.0 and 1.4.3 default (-want +got):\n%s", diff)
			}
		})
	}
}

func TestStoreRefusesWhatFirmwareCannotHold(t *testing.T) {
	cases := map[string]struct {
		firmware connector.StoredFirmware
		image    []byte
	}{
		"a version it holds":   {firmwareOf("1.4.3", 5), []byte{1}},
		"an uploader it lacks": {func() connector.StoredFirmware { f := firmwareOf("2.0.0", 5); f.UploadedBy = 9; return f }(), []byte{1}},
		"an image that is nil": {firmwareOf("2.0.0", 5), nil},
	}
	for _, impl := range storeImpls {
		for name, c := range cases {
			t.Run(impl.name+"/"+name, func(t *testing.T) {
				store := firmwareWorld(t, impl)

				_, err := store.AddFirmware(context.Background(), c.firmware, c.image)

				if err == nil {
					t.Error("AddFirmware stored firmware with " + name)
				}
			})
		}
	}
}

func TestStoreKeepsTheFirmwareADoorRunsAndRefusesToDeleteIt(t *testing.T) {
	for _, impl := range storeImpls {
		t.Run(impl.name, func(t *testing.T) {
			store := firmwareWorld(t, impl)
			second, unknown := int64(2), int64(9)

			mustSetDoorFirmware(t, store, 1, &second)
			_, deleteErr := store.DeleteFirmware(context.Background(), 2)
			_, unknownErr := store.SetDoorFirmware(context.Background(), 1, &unknown)
			missing, missingErr := store.SetDoorFirmware(context.Background(), 9, nil)
			kept := mustDoor(t, store, 1).FirmwareID

			if missingErr != nil {
				t.Fatal(missingErr)
			}
			if diff := cmp.Diff([]any{&second, false}, []any{kept, missing}); diff != "" {
				t.Errorf("firmware kept, missing door (-want +got):\n%s", diff)
			}
			if deleteErr == nil || unknownErr == nil {
				t.Errorf("deleting firmware a door runs said %v, and naming unknown firmware %v", deleteErr, unknownErr)
			}
		})
	}
}

func TestStoreDeletesFirmwareNoDoorRuns(t *testing.T) {
	for _, impl := range storeImpls {
		t.Run(impl.name, func(t *testing.T) {
			store := firmwareWorld(t, impl)
			second := int64(2)
			mustSetDoorFirmware(t, store, 1, &second)
			mustSetDoorFirmware(t, store, 1, nil)

			deleted, err := store.DeleteFirmware(context.Background(), 2)
			if err != nil {
				t.Fatal(err)
			}

			want := []any{true, []connector.StoredFirmware{named(firmwareOf("1.4.3", 0), 1, false)}}
			if diff := cmp.Diff(want, []any{deleted, mustFirmwares(t, store)}); diff != "" {
				t.Errorf("deleted, left (-want +got):\n%s", diff)
			}
		})
	}
}

// updateOf is an update of the door whose id is door to version, sent at
// firmwareUploaded.
func updateOf(door int64, version string) connector.StoredUpdate {
	return connector.StoredUpdate{
		DoorID: door, Version: version, CommandID: "0001020304050607", TokenHash: []byte{9, 8, 7}, SentAt: firmwareUploaded,
		ExpiresAt: firmwareUploaded.Add(30 * time.Minute), State: "updating",
	}
}

func TestStoreKeepsTheLatestUpdateOfEachDoor(t *testing.T) {
	for _, impl := range storeImpls {
		t.Run(impl.name, func(t *testing.T) {
			store := firmwareWorld(t, impl)
			finished := firmwareUploaded.Add(5 * time.Minute)
			done := updateOf(2, "1.5.0")
			done.State, done.FinishedAt, done.Detail = "failed", &finished, "It did not come back."

			for _, u := range []connector.StoredUpdate{updateOf(2, "1.4.3"), updateOf(1, "1.5.0"), done} {
				if err := store.PutDoorUpdate(context.Background(), u); err != nil {
					t.Fatal(err)
				}
			}
			got, err := store.DoorUpdates(context.Background())
			if err != nil {
				t.Fatal(err)
			}

			if diff := cmp.Diff([]connector.StoredUpdate{updateOf(1, "1.5.0"), done}, got); diff != "" {
				t.Errorf("updates (-want +got):\n%s", diff)
			}
		})
	}
}

func TestStoreRefusesAnUpdateOfADoorItDoesNotHold(t *testing.T) {
	for _, impl := range storeImpls {
		t.Run(impl.name, func(t *testing.T) {
			store := firmwareWorld(t, impl)

			if err := store.PutDoorUpdate(context.Background(), updateOf(9, "1.5.0")); err == nil {
				t.Error("PutDoorUpdate stored an update of a door the store does not hold")
			}
		})
	}
}

func TestStoreSetsADoorsStateLeavingItsControllerAsItIs(t *testing.T) {
	for _, impl := range storeImpls {
		t.Run(impl.name, func(t *testing.T) {
			store := migrated(t, impl)
			id := mustCreateDoor(t, store, plainDoor("Lobby"))
			mustCreateDoor(t, store, plainDoor("Dock"))
			mustSetController(t, store, id, controllerOf("A1"))
			opened := controllerSeen.Add(time.Minute)
			st := connector.StoredDoorState{
				FirmwareVersion: "1.5.0", Signal: -70, LastSeenAt: opened, DoorState: "open", OpenedAt: &opened, HeldOpen: true, Unresponsive: true,
				Telemetry: &connector.StoredTelemetry{At: opened, Signal: -70, FirmwareVersion: "1.5.0", DoorOpen: true},
			}

			found, err := store.SetDoorState(context.Background(), id, st)
			if err != nil {
				t.Fatal(err)
			}
			unenrolled, err := store.SetDoorState(context.Background(), 2, st)
			if err != nil {
				t.Fatal(err)
			}

			want := []any{true, false, &connector.StoredController{HardwareID: "A1", Online: true, StoredDoorState: st}, (*connector.StoredController)(nil)}
			got := []any{found, unenrolled, mustDoor(t, store, id).Controller, mustDoor(t, store, 2).Controller}
			if diff := cmp.Diff(want, got); diff != "" {
				t.Errorf("found, found without a controller, controllers (-want +got):\n%s", diff)
			}
		})
	}
}
