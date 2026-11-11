//go:build slow

package connector_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"

	"github.com/dakriy/doorcode/internal/connector"
)

// purgeWorld is eventWorld where Lobby (1) and the deleted Dock (2) are
// both in the zone Building A (1) and each has a picture, Dock's its
// primary, an override, a controller (A1 and B2), a credential, a
// telemetry sample and rollup, and a firmware update.
func purgeWorld(t *testing.T, impl storeImpl) connector.Store {
	t.Helper()
	ctx := context.Background()
	store := eventWorld(t, impl)
	mustCreateZone(t, store, connector.StoredZone{Name: "Building A", DoorIDs: []int64{1, 2}, OpenScheduleIDs: []int64{}})
	for door, hwid := range map[int64]string{1: "A1", 2: "B2"} {
		picture := mustAddPicture(t, store, door, "image", "thumbnail")
		if _, err := store.SetPrimaryPicture(ctx, door, picture); err != nil {
			t.Fatal(err)
		}
		mustCreateOverride(t, store, override(door, "locked", overrideCreated))
		mustSetController(t, store, door, controllerOf(hwid))
		mustCreateCredential(t, store, door, "door-"+hwid)
		if err := store.AddSample(ctx, sampleAt(door, 0, -60)); err != nil {
			t.Fatal(err)
		}
		if err := store.PutRollups(ctx, []connector.StoredRollup{rollupAt(door, 0, 1)}); err != nil {
			t.Fatal(err)
		}
		if err := store.PutDoorUpdate(ctx, updateOf(door, "1.5.0")); err != nil {
			t.Fatal(err)
		}
	}
	return store
}

// doorsOf is the door id of each of rows, in order.
func doorsOf[T any](rows []T, door func(T) int64) []int64 {
	out := []int64{}
	for _, r := range rows {
		out = append(out, door(r))
	}
	return out
}

// whatRefersToDoors is, in order, the ids of the events, the doors of the
// samples, rollups, updates, live credentials, overrides, and pictures,
// and the doors of zone 1 that store holds.
func whatRefersToDoors(t *testing.T, store connector.Store) [][]int64 {
	t.Helper()
	ctx := context.Background()
	events, _, eventsErr := store.Events(ctx, connector.EventQuery{Limit: 10})
	samples, samplesErr := store.AllSamples(ctx, telemetryStart, telemetryStart.Add(time.Hour))
	var rollups []connector.StoredRollup
	var overrides []connector.StoredOverride
	var pictures []connector.StoredPicture
	for _, door := range []int64{1, 2} {
		r, rErr := store.Rollups(ctx, door, telemetryStart, telemetryStart.Add(time.Hour))
		o, oErr := store.DoorOverrides(ctx, door, overrideCreated)
		p, pErr := store.Pictures(ctx, door)
		if err := errors.Join(rErr, oErr, pErr); err != nil {
			t.Fatal(err)
		}
		rollups, overrides, pictures = append(rollups, r...), append(overrides, o...), append(pictures, p...)
	}
	updates, updatesErr := store.DoorUpdates(ctx)
	creds, credsErr := store.LiveCredentials(ctx)
	zone, _, zoneErr := store.Zone(ctx, 1)
	if err := errors.Join(eventsErr, samplesErr, updatesErr, credsErr, zoneErr); err != nil {
		t.Fatal(err)
	}
	return [][]int64{
		doorsOf(events, func(e connector.StoredEvent) int64 { return e.ID }),
		doorsOf(samples, func(s connector.StoredSample) int64 { return s.DoorID }),
		doorsOf(rollups, func(r connector.StoredRollup) int64 { return r.DoorID }),
		doorsOf(updates, func(u connector.StoredUpdate) int64 { return u.DoorID }),
		doorsOf(creds, func(c connector.StoredCredential) int64 { return c.DoorID }),
		doorsOf(overrides, func(o connector.StoredOverride) int64 { return o.DoorID }),
		doorsOf(pictures, func(p connector.StoredPicture) int64 { return p.DoorID }),
		zone.DoorIDs,
	}
}

func TestStorePurgesADoorWithEverythingThatRefersToIt(t *testing.T) {
	for _, impl := range storeImpls {
		t.Run(impl.name, func(t *testing.T) {
			ctx := context.Background()
			store := purgeWorld(t, impl)

			purged, err := store.PurgeDoor(ctx, 2)
			_, found, foundErr := store.Door(ctx, 2)
			left := whatRefersToDoors(t, store)
			again := mustCreateDoor(t, store, plainDoor("Dock"))
			_, controllerErr := store.SetController(ctx, again, controllerOf("B2"))

			want := []any{true, nil, false, nil, [][]int64{{4, 1}, {1}, {1}, {1}, {1}, {1}, {1}, {1}}, nil}
			if diff := cmp.Diff(want, []any{purged, err, found, foundErr, left, controllerErr}); diff != "" {
				t.Errorf("purged, error, found, error, what refers to doors, its hardware again (-want +got):\n%s", diff)
			}
		})
	}
}

// userPurgeWorld is the directory with Lobby (1), where Ann (4) has a
// session, a token, membership of the administrators group, an access
// request, and an event in which she entered her code, and the
// administrator (1) has an event of their own.
func userPurgeWorld(t *testing.T, impl storeImpl) connector.Store {
	t.Helper()
	ctx := context.Background()
	store := directory(t, impl)
	mustCreateDoor(t, store, plainDoor("Lobby"))
	ann, admin := int64(4), int64(1)
	annSession, annToken := session("ann"), token("ann", nil)
	annSession.UserID, annToken.UserID = ann, ann
	err := errors.Join(
		store.CreateSession(ctx, annSession),
		store.AddMember(ctx, 1, ann),
	)
	_, tokenErr := store.CreateToken(ctx, annToken)
	_, requestErr := store.CreateRequest(ctx, connector.StoredRequest{UserID: ann, Title: "Lab access", CreatedAt: requestsStart, UpdatedAt: requestsStart})
	_, annEventErr := store.AddEvent(ctx, connector.StoredEvent{At: eventsStart, DoorID: 1, Type: "check", Outcome: "allowed", Reason: "access rule", UserID: &ann})
	_, adminEventErr := store.AddEvent(ctx, connector.StoredEvent{At: eventsStart, DoorID: 1, Type: "open", Outcome: "allowed", Reason: "opened", UserID: &admin})
	if err := errors.Join(err, tokenErr, requestErr, annEventErr, adminEventErr); err != nil {
		t.Fatal(err)
	}
	return store
}

func TestStorePurgesAUserWithEverythingThatRefersToThem(t *testing.T) {
	for _, impl := range storeImpls {
		t.Run(impl.name, func(t *testing.T) {
			ctx := context.Background()
			store := userPurgeWorld(t, impl)

			purged, err := store.PurgeUser(ctx, 4)
			_, found, foundErr := store.User(ctx, 4)
			_, _, session, sessionErr := store.SessionUser(ctx, []byte("ann"))
			_, _, token, tokenErr := store.TokenUser(ctx, []byte("ann"))
			members, membersErr := store.GroupMembers(ctx, 1)
			_, requests, requestsErr := store.Requests(ctx, connector.RequestQuery{Limit: 10})
			events, _, eventsErr := store.Events(ctx, connector.EventQuery{Limit: 10})
			_, againErr := store.CreateUser(ctx, person("ann@example.com", "Ann"), nil)

			got := []any{
				purged, err, found, session, token, ids(members), requests,
				doorsOf(events, func(e connector.StoredEvent) int64 { return *e.UserID }),
				errors.Join(foundErr, sessionErr, tokenErr, membersErr, requestsErr, eventsErr, againErr),
			}
			want := []any{true, nil, false, false, false, []int64{1}, 0, []int64{1}, nil}
			if diff := cmp.Diff(want, got); diff != "" {
				t.Errorf("purged, error, found, session, token, members, requests, events' users, errors (-want +got):\n%s", diff)
			}
		})
	}
}

func TestStoreRefusesToPurgeAUserFirmwareOrAnOverrideNames(t *testing.T) {
	made := map[string]func(t *testing.T, store connector.Store){
		"firmware": func(t *testing.T, store connector.Store) { mustAddFirmware(t, store, firmwareOf("1.5.0", 0)) },
		"override": func(t *testing.T, store connector.Store) {
			mustCreateOverride(t, store, override(1, "locked", overrideCreated))
		},
	}
	for _, impl := range storeImpls {
		for name, arrange := range made {
			t.Run(impl.name+"/"+name, func(t *testing.T) {
				store := overrideWorld(t, impl)
				arrange(t, store)

				_, err := store.PurgeUser(context.Background(), 1)
				_, found, foundErr := store.User(context.Background(), 1)

				if diff := cmp.Diff([]any{true, true, nil}, []any{err != nil, found, foundErr}); diff != "" {
					t.Errorf("refused, found, error (-want +got):\n%s", diff)
				}
			})
		}
	}
}

func TestStoreReportsPurgingWhatIsNotThere(t *testing.T) {
	for _, impl := range storeImpls {
		t.Run(impl.name, func(t *testing.T) {
			store := withAdmin(t, impl)

			door, doorErr := store.PurgeDoor(context.Background(), 9)
			user, userErr := store.PurgeUser(context.Background(), 9)

			if diff := cmp.Diff([]any{false, nil, false, nil}, []any{door, doorErr, user, userErr}); diff != "" {
				t.Errorf("door, error, user, error (-want +got):\n%s", diff)
			}
		})
	}
}

func TestStoreListsTheOverridesAUserCreatedByID(t *testing.T) {
	for _, impl := range storeImpls {
		t.Run(impl.name, func(t *testing.T) {
			store := overrideWorld(t, impl)
			mustCreateUser(t, store, person("ann@example.com", "Ann"))
			later := override(1, "open", overrideCreated.Add(time.Hour))
			byAnn := override(2, "locked", overrideCreated)
			byAnn.CreatedBy = 2
			for _, o := range []connector.StoredOverride{later, byAnn, override(2, "open", overrideCreated)} {
				mustCreateOverride(t, store, o)
			}

			got, err := store.UserOverrides(context.Background(), 1)

			first, second := later, override(2, "open", overrideCreated)
			first.ID, first.CreatorName = 1, "Administrator"
			second.ID, second.CreatorName = 3, "Administrator"
			if diff := cmp.Diff([]any{[]connector.StoredOverride{first, second}, nil}, []any{got, err}); diff != "" {
				t.Errorf("overrides, error (-want +got):\n%s", diff)
			}
		})
	}
}
