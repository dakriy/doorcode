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

var controllerSeen = time.Date(2026, 1, 5, 9, 30, 0, 0, time.UTC)

func controllerOf(hwid string) *connector.StoredController {
	return &connector.StoredController{HardwareID: hwid, FirmwareVersion: "1.4.2", Signal: -61, Online: true, LastSeenAt: controllerSeen}
}

func mustSetController(t *testing.T, store connector.Store, id int64, c *connector.StoredController) {
	t.Helper()
	if found, err := store.SetController(context.Background(), id, c); err != nil || !found {
		t.Fatalf("set controller of door %d: found %v, error %v", id, found, err)
	}
}

func TestStoreKeepsADoorsController(t *testing.T) {
	for _, impl := range storeImpls {
		t.Run(impl.name, func(t *testing.T) {
			store := migrated(t, impl)
			id := mustCreateDoor(t, store, plainDoor("Lobby"))
			mustCreateDoor(t, store, plainDoor("Dock"))

			mustSetController(t, store, id, controllerOf("A1"))
			doors, err := store.Doors(context.Background())
			if err != nil {
				t.Fatal(err)
			}

			got := []*connector.StoredController{mustDoor(t, store, id).Controller, doors[0].Controller, doors[1].Controller}
			want := []*connector.StoredController{controllerOf("A1"), nil, controllerOf("A1")}
			if diff := cmp.Diff(want, got); diff != "" {
				t.Errorf("door, Dock, Lobby controllers (-want +got):\n%s", diff)
			}
		})
	}
}

func TestStoreReplacesAndRemovesADoorsController(t *testing.T) {
	for _, impl := range storeImpls {
		t.Run(impl.name, func(t *testing.T) {
			store := migrated(t, impl)
			id := mustCreateDoor(t, store, plainDoor("Lobby"))
			mustSetController(t, store, id, controllerOf("A1"))
			offline := controllerOf("A1")
			offline.Online, offline.Signal = false, 0

			mustSetController(t, store, id, offline)
			replaced := mustDoor(t, store, id).Controller
			mustSetController(t, store, id, nil)

			got := []*connector.StoredController{replaced, mustDoor(t, store, id).Controller}
			if diff := cmp.Diff([]*connector.StoredController{offline, nil}, got); diff != "" {
				t.Errorf("replaced, removed (-want +got):\n%s", diff)
			}
		})
	}
}

func TestStoreKeepsADoorsControllerWhenTheDoorIsUpdated(t *testing.T) {
	for _, impl := range storeImpls {
		t.Run(impl.name, func(t *testing.T) {
			store := migrated(t, impl)
			id := mustCreateDoor(t, store, plainDoor("Lobby"))
			mustSetController(t, store, id, controllerOf("A1"))
			d := plainDoor("Front")
			d.ID = id

			if _, err := store.UpdateDoor(context.Background(), d); err != nil {
				t.Fatal(err)
			}

			if diff := cmp.Diff(controllerOf("A1"), mustDoor(t, store, id).Controller); diff != "" {
				t.Errorf("controller (-want +got):\n%s", diff)
			}
		})
	}
}

func TestStoreFindsTheDoorWithAHardwareID(t *testing.T) {
	for _, impl := range storeImpls {
		t.Run(impl.name, func(t *testing.T) {
			store := migrated(t, impl)
			mustCreateDoor(t, store, plainDoor("Lobby"))
			dock := mustCreateDoor(t, store, plainDoor("Dock"))
			mustSetController(t, store, dock, controllerOf("A1"))

			id, found, err := store.HardwareDoor(context.Background(), "A1")
			_, foundOther, otherErr := store.HardwareDoor(context.Background(), "B2")

			if diff := cmp.Diff([]any{dock, true, nil, false, nil}, []any{id, found, err, foundOther, otherErr}); diff != "" {
				t.Errorf("A1's door, found, error, B2 found, error (-want +got):\n%s", diff)
			}
		})
	}
}

func TestStoreRefusesAHardwareIDAnotherDoorHas(t *testing.T) {
	for _, impl := range storeImpls {
		t.Run(impl.name, func(t *testing.T) {
			store := migrated(t, impl)
			lobby := mustCreateDoor(t, store, plainDoor("Lobby"))
			dock := mustCreateDoor(t, store, plainDoor("Dock"))
			mustSetController(t, store, lobby, controllerOf("A1"))

			_, err := store.SetController(context.Background(), dock, controllerOf("A1"))

			if err == nil {
				t.Error("a second door took the same hardware id")
			}
		})
	}
}

func TestStoreReportsAControllerForADoorThatIsNotThere(t *testing.T) {
	for _, impl := range storeImpls {
		t.Run(impl.name, func(t *testing.T) {
			found, err := migrated(t, impl).SetController(context.Background(), 9, controllerOf("A1"))

			if diff := cmp.Diff([]any{false, nil}, []any{found, err}); diff != "" {
				t.Errorf("found, error (-want +got):\n%s", diff)
			}
		})
	}
}

func mustCreateCredential(t *testing.T, store connector.Store, doorID int64, username string) int64 {
	t.Helper()
	id, err := store.CreateCredential(context.Background(), connector.StoredCredential{DoorID: doorID, Username: username, IssuedAt: doorCreated})
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func TestStoreKeepsCredentialsAndWhenTheyWereUsedAndRevoked(t *testing.T) {
	for _, impl := range storeImpls {
		t.Run(impl.name, func(t *testing.T) {
			ctx := context.Background()
			store := migrated(t, impl)
			door := mustCreateDoor(t, store, plainDoor("Lobby"))
			first := mustCreateCredential(t, store, door, "door-1-a")
			second := mustCreateCredential(t, store, door, "door-1-b")
			used, revoked := doorCreated.Add(time.Hour), doorCreated.Add(2*time.Hour)

			usedFound, err := store.UseCredential(ctx, first, used)
			if err != nil {
				t.Fatal(err)
			}
			revokedFound, err := store.RevokeCredential(ctx, first, revoked)
			if err != nil {
				t.Fatal(err)
			}
			got, err := store.DoorCredentials(ctx, door)
			if err != nil {
				t.Fatal(err)
			}

			want := []connector.StoredCredential{
				{ID: first, DoorID: door, Username: "door-1-a", IssuedAt: doorCreated, UsedAt: &used, RevokedAt: &revoked},
				{ID: second, DoorID: door, Username: "door-1-b", IssuedAt: doorCreated},
			}
			if diff := cmp.Diff([]any{true, true, want}, []any{usedFound, revokedFound, got}); diff != "" {
				t.Errorf("used found, revoked found, credentials (-want +got):\n%s", diff)
			}
		})
	}
}

func TestStoreListsLiveCredentialsByDoor(t *testing.T) {
	for _, impl := range storeImpls {
		t.Run(impl.name, func(t *testing.T) {
			ctx := context.Background()
			store := migrated(t, impl)
			lobby := mustCreateDoor(t, store, plainDoor("Lobby"))
			dock := mustCreateDoor(t, store, plainDoor("Dock"))
			mustCreateCredential(t, store, dock, "door-2-a")
			gone := mustCreateCredential(t, store, lobby, "door-1-a")
			mustCreateCredential(t, store, lobby, "door-1-b")
			if _, err := store.RevokeCredential(ctx, gone, doorCreated); err != nil {
				t.Fatal(err)
			}

			live, err := store.LiveCredentials(ctx)
			if err != nil {
				t.Fatal(err)
			}

			var got []string
			for _, c := range live {
				got = append(got, c.Username)
			}
			if diff := cmp.Diff([]string{"door-1-b", "door-2-a"}, got); diff != "" {
				t.Errorf("live usernames (-want +got):\n%s", diff)
			}
		})
	}
}

func TestStoreRefusesWhatACredentialCannotHold(t *testing.T) {
	cases := map[string]func(t *testing.T, store connector.Store, door int64) error{
		"a door that is not there": func(_ *testing.T, store connector.Store, _ int64) error {
			_, err := store.CreateCredential(context.Background(), connector.StoredCredential{DoorID: 9, Username: "door-9-a", IssuedAt: doorCreated})
			return err
		},
		"a username another credential has": func(t *testing.T, store connector.Store, door int64) error {
			mustCreateCredential(t, store, door, "door-1-a")
			_, err := store.CreateCredential(context.Background(), connector.StoredCredential{DoorID: door, Username: "door-1-a", IssuedAt: doorCreated})
			return err
		},
	}
	for _, impl := range storeImpls {
		for name, attempt := range cases {
			t.Run(impl.name+"/"+name, func(t *testing.T) {
				store := migrated(t, impl)
				door := mustCreateDoor(t, store, plainDoor("Lobby"))

				if err := attempt(t, store, door); err == nil {
					t.Error("the store accepted it")
				}
			})
		}
	}
}

func TestStoreReportsACredentialThatIsNotThere(t *testing.T) {
	for _, impl := range storeImpls {
		t.Run(impl.name, func(t *testing.T) {
			ctx := context.Background()
			store := migrated(t, impl)

			used, err := store.UseCredential(ctx, 9, doorCreated)
			if err != nil {
				t.Fatal(err)
			}
			revoked, err := store.RevokeCredential(ctx, 9, doorCreated)
			if err != nil {
				t.Fatal(err)
			}

			if diff := cmp.Diff([]bool{false, false}, []bool{used, revoked}); diff != "" {
				t.Errorf("used, revoked found (-want +got):\n%s", diff)
			}
		})
	}
}

func TestStoreKeepsADoorsConfigurationAcrossUpdates(t *testing.T) {
	for _, impl := range storeImpls {
		t.Run(impl.name, func(t *testing.T) {
			ctx := context.Background()
			store := migrated(t, impl)
			id := mustCreateDoor(t, store, plainDoor("Lobby"))
			mustCreateDoor(t, store, plainDoor("Dock"))
			fresh := mustDoor(t, store, id).Config

			found, err := store.SetDoorConfig(ctx, id, map[string]string{"strike_pulse_ms": "500", "log_level": "debug"})
			first := mustDoor(t, store, id).Config
			if _, err := store.SetDoorConfig(ctx, id, map[string]string{"reply_timeout_ms": "5000"}); err != nil {
				t.Fatal(err)
			}
			changed := mustDoor(t, store, id)
			changed.Name = "Main lobby"
			if _, err := store.UpdateDoor(ctx, changed); err != nil {
				t.Fatal(err)
			}
			doors, listErr := store.Doors(ctx)
			if listErr != nil {
				t.Fatal(listErr)
			}

			want := []any{map[string]string{}, true, "<nil>", map[string]string{"strike_pulse_ms": "500", "log_level": "debug"},
				map[string]string{"reply_timeout_ms": "5000"}, map[string]string{}}
			got := []any{fresh, found, testsupport.ErrorText(err), first, doors[1].Config, doors[0].Config}
			if diff := cmp.Diff(want, got); diff != "" {
				t.Errorf("fresh, found, error, first, after update, other door (-want +got):\n%s", diff)
			}
		})
	}
}

func TestStoreKeepsTheConfigurationADoorWasLastSent(t *testing.T) {
	for _, impl := range storeImpls {
		t.Run(impl.name, func(t *testing.T) {
			ctx := context.Background()
			store := migrated(t, impl)
			id := mustCreateDoor(t, store, plainDoor("Lobby"))
			mustCreateDoor(t, store, plainDoor("Dock"))
			fresh := mustDoor(t, store, id).SentConfig
			first := connector.StoredSentConfig{Digest: "0123456789abcdef", At: controllerSeen}
			again := connector.StoredSentConfig{Digest: "aed54fd952c8d3a6", At: controllerSeen.Add(time.Hour)}

			found, err := store.SetSentConfig(ctx, id, first)
			kept := mustDoor(t, store, id).SentConfig
			if _, err := store.SetSentConfig(ctx, id, again); err != nil {
				t.Fatal(err)
			}
			changed := mustDoor(t, store, id)
			changed.Name, changed.SentConfig = "Main lobby", nil
			if _, err := store.UpdateDoor(ctx, changed); err != nil {
				t.Fatal(err)
			}
			doors, listErr := store.Doors(ctx)
			if listErr != nil {
				t.Fatal(listErr)
			}
			missing, missingErr := store.SetSentConfig(ctx, 7, first)

			want := []any{(*connector.StoredSentConfig)(nil), true, "<nil>", &first, &again, (*connector.StoredSentConfig)(nil), false, "<nil>"}
			got := []any{fresh, found, testsupport.ErrorText(err), kept, doors[1].SentConfig, doors[0].SentConfig, missing,
				testsupport.ErrorText(missingErr)}
			if diff := cmp.Diff(want, got); diff != "" {
				t.Errorf("fresh, found, error, first, after update, other door, missing door, error (-want +got):\n%s", diff)
			}
		})
	}
}

func TestStoreReportsAConfigurationForADoorThatIsNotThere(t *testing.T) {
	for _, impl := range storeImpls {
		t.Run(impl.name, func(t *testing.T) {
			found, err := migrated(t, impl).SetDoorConfig(context.Background(), 7, map[string]string{})

			if diff := cmp.Diff([]any{false, "<nil>"}, []any{found, testsupport.ErrorText(err)}); diff != "" {
				t.Errorf("found, error (-want +got):\n%s", diff)
			}
		})
	}
}

func TestStoreKeepsADoorsStateWithItsController(t *testing.T) {
	for _, impl := range storeImpls {
		t.Run(impl.name, func(t *testing.T) {
			store := migrated(t, impl)
			id := mustCreateDoor(t, store, plainDoor("Lobby"))
			opened := controllerSeen.Add(-time.Minute)
			c := controllerOf("A1")
			c.DoorState, c.OpenedAt, c.HeldOpen, c.Unresponsive = "open", &opened, true, true
			c.Telemetry = &connector.StoredTelemetry{
				At: controllerSeen, Signal: -61, LatencyMS: 40, UptimeS: 3720, FreeBytes: 120832, FirmwareVersion: "1.4.2",
				Reconnects: 2, DoorOpen: true, Opens: 1, ConfigDigest: "0123456789abcdef",
			}
			c.ConfigMismatchSince, c.ConfigRevertedDigest, c.ConfigRevertedAt = &opened, "aed54fd952c8d3a6", &controllerSeen

			mustSetController(t, store, id, c)

			if diff := cmp.Diff(c, mustDoor(t, store, id).Controller); diff != "" {
				t.Errorf("controller (-want +got):\n%s", diff)
			}
		})
	}
}
