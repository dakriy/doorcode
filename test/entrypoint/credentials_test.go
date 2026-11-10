package entrypoint_test

import (
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/dakriy/doorcode/internal/testsupport"
)

// secondUsername is the username of the credentials the first rotation
// issues the door enrolledDoor gives.
const secondUsername = "door-1-606162636465"

// connects reports, for each of creds, whether the broker accepts a
// connection with it.
func connects(f *testsupport.Factory, creds ...doorCredentials) []bool {
	var out []bool
	for _, c := range creds {
		conn, err := f.BrokerStub.Connect("door-"+c.door, c.username, c.token, "", nil)
		out = append(out, err == nil)
		if conn != nil {
			conn.Close()
		}
	}
	return out
}

// statuses is the status of each credential of the door with id, oldest
// first.
func statuses(t *testing.T, b *testsupport.Browser, id string) []string {
	t.Helper()
	var out []string
	body := b.Get("/api/v1/doors/" + id + "/credentials").Body
	creds, _ := testsupport.JSONAt(body, "/credentials").([]any)
	for i := range creds {
		out = append(out, testsupport.JSONString(body, "/credentials/"+testsupport.Itoa(i)+"/status"))
	}
	return out
}

func TestRotatingSendsNewCredentialsOverTheDoorsConnection(t *testing.T) {
	t.Parallel()
	f := testsupport.NewFactory()
	_, b, conn, _ := enrolledDoor(t, f)

	send(t, b, "POST", "/api/v1/doors/1/credentials/rotate", "", 204)

	got := lastReceived(t, conn)
	want := map[string]any{"v": int64(1), "id": "7e7f808182838485", "command": "credentials", "username": secondUsername,
		"token": "ZmdoaWprbG1ub3BxcnN0dXZ3eHl6e3x9"}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("command (-want +got):\n%s", diff)
	}
}

func TestRotatedCredentialsBothWorkUntilTheDoorConnectsWithTheNewOnes(t *testing.T) {
	t.Parallel()
	f := testsupport.NewFactory()
	a, b, conn, old := enrolledDoor(t, f)
	send(t, b, "POST", "/api/v1/doors/1/credentials/rotate", "", 204)
	rotated := credentialsOf(t, conn)
	rotated.door = "1"
	before := connects(f, old, rotated)

	conn.Close()
	connectAs(t, f, rotated)
	run(t, a)

	got := []any{before, connects(f, old, rotated), statuses(t, b, "1")}
	want := []any{[]bool{true, true}, []bool{false, true}, []string{"revoked", "in-use"}}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("before, after, statuses (-want +got):\n%s", diff)
	}
}

func TestOldCredentialsKeepWorkingHoweverLongTheDoorTakesToConnectWithTheNewOnes(t *testing.T) {
	t.Parallel()
	f := testsupport.NewFactory()
	a, b, conn, old := enrolledDoor(t, f)
	send(t, b, "POST", "/api/v1/doors/1/credentials/rotate", "", 204)
	rotated := credentialsOf(t, conn)
	rotated.door = "1"
	f.ClockStub.Set("2026-12-31T09:00:00Z")
	run(t, a)
	b = signIn(t, f, a.Handler())
	before := []any{connects(f, old), statuses(t, b, "1")}

	connectAs(t, f, rotated)
	run(t, a)

	got := []any{before, connects(f, old), statuses(t, b, "1")}
	want := []any{[]any{[]bool{true}, []string{"in-use", "issued"}}, []bool{false}, []string{"revoked", "in-use"}}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("before connecting with the new ones, after: old works, statuses (-want +got):\n%s", diff)
	}
}

func TestRotatingAgainIsRefusedUntilTheDoorConnectsWithTheNewCredentials(t *testing.T) {
	t.Parallel()
	f := testsupport.NewFactory()
	_, b, _, _ := enrolledDoor(t, f)
	send(t, b, "POST", "/api/v1/doors/1/credentials/rotate", "", 204)

	got := b.PostJSON("/api/v1/doors/1/credentials/rotate", "")

	want := []any{apiError(409, "the door's controller has not yet connected with the credentials it was last sent"), []string{"in-use", "issued"}}
	if diff := cmp.Diff(want, []any{got, statuses(t, b, "1")}); diff != "" {
		t.Errorf("second rotation, statuses (-want +got):\n%s", diff)
	}
}

func TestDueRotationWaitsUntilTheDoorConnectsWithTheNewCredentials(t *testing.T) {
	t.Parallel()
	f := testsupport.NewFactory()
	a, b, conn, _ := enrolledDoor(t, f)
	send(t, b, "POST", "/api/v1/doors/1/credentials/rotate", "", 204)
	rotated := credentialsOf(t, conn)
	rotated.door = "1"
	f.ClockStub.Set("2026-02-04T09:00:00Z")
	logged := len(f.LogStub.Entries)
	run(t, a)
	logs := logsSince(f, logged)
	b = signIn(t, f, a.Handler())
	waiting := statuses(t, b, "1")

	connectAs(t, f, rotated)
	run(t, a)
	f.ClockStub.Set("2026-02-04T09:01:00Z")
	run(t, a)

	want := []any{[]testsupport.LogEntry{}, []string{"in-use", "issued"}, []string{"revoked", "in-use", "issued"}}
	if diff := cmp.Diff(want, []any{logs, waiting, statuses(t, b, "1")}); diff != "" {
		t.Errorf("log and statuses while waiting, statuses once connected (-want +got):\n%s", diff)
	}
}

func TestCredentialsRotateOnTheirOwnOnceTheIntervalPasses(t *testing.T) {
	t.Parallel()
	cases := map[string]struct {
		at   string
		want []string
	}{
		"before the interval": {"2026-02-04T08:59:59Z", []string{"in-use"}},
		"after the interval":  {"2026-02-04T09:00:00Z", []string{"in-use", "issued"}},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			f := testsupport.NewFactory()
			a, _, _, _ := enrolledDoor(t, f)
			f.ClockStub.Set(c.at)

			run(t, a)

			if diff := cmp.Diff(c.want, statuses(t, signIn(t, f, a.Handler()), "1")); diff != "" {
				t.Errorf("statuses (-want +got):\n%s", diff)
			}
		})
	}
}

func TestPeriodicWorkReadsOnlyTheDoorsThatAreNotDeleted(t *testing.T) {
	t.Parallel()
	f := testsupport.NewFactory()
	a, b, _, _ := enrolledDoor(t, f)
	createDoor(t, b, `{"name":"Dock"}`)
	send(t, b, "DELETE", "/api/v1/doors/2", "", 204)
	before := f.StoreStub.DoorsCalls()
	f.ClockStub.Set("2026-02-04T09:00:00Z")

	run(t, a)

	got := []any{statuses(t, signIn(t, f, a.Handler()), "1"), f.StoreStub.DoorsCalls()}
	if diff := cmp.Diff([]any{[]string{"in-use", "issued"}, before}, got); diff != "" {
		t.Errorf("statuses, reads of every door (-want +got):\n%s", diff)
	}
}

func TestRotationIntervalComesFromTheSettings(t *testing.T) {
	t.Parallel()
	f := testsupport.NewFactory()
	f.EnvStub["DOORCODE_DOOR_CREDENTIAL_ROTATION_DAYS"] = "1"
	a, _, _, _ := enrolledDoor(t, f)
	f.ClockStub.Set("2026-01-06T09:00:00Z")

	run(t, a)

	if diff := cmp.Diff([]string{"in-use", "issued"}, statuses(t, signIn(t, f, a.Handler()), "1")); diff != "" {
		t.Errorf("statuses (-want +got):\n%s", diff)
	}
}

func TestOfflineDoorIsNotRotated(t *testing.T) {
	t.Parallel()
	f := testsupport.NewFactory()
	a, _, conn, _ := enrolledDoor(t, f)
	conn.Drop()
	run(t, a)
	f.ClockStub.Set("2026-03-01T09:00:00Z")

	run(t, a)

	b := signIn(t, f, a.Handler())
	got := []any{statuses(t, b, "1"), b.PostJSON("/api/v1/doors/1/credentials/rotate", "")}
	want := []any{[]string{"in-use"}, apiError(409, "the door's controller is not connected")}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("statuses, rotation (-want +got):\n%s", diff)
	}
}

func TestRevokingDisconnectsTheDoorAndStopsItsCredentialsWorking(t *testing.T) {
	t.Parallel()
	f := testsupport.NewFactory()
	_, b, conn, creds := enrolledDoor(t, f)

	send(t, b, "POST", "/api/v1/doors/1/credentials/revoke", "", 204)

	got := []any{conn.Connected(), connects(f, creds), f.BrokerStub.Usernames(), testsupport.JSONAt(b.Get("/api/v1/doors/1").Body, "/controller/online")}
	if diff := cmp.Diff([]any{false, []bool{false}, []string{enrollUser}, false}, got); diff != "" {
		t.Errorf("connected, connects, clients, online (-want +got):\n%s", diff)
	}
}

func TestRevokingAfterARotationRevokesOnlyWhatStillWorks(t *testing.T) {
	t.Parallel()
	f := testsupport.NewFactory()
	a, b, conn, _ := enrolledDoor(t, f)
	send(t, b, "POST", "/api/v1/doors/1/credentials/rotate", "", 204)
	rotated := credentialsOf(t, conn)
	rotated.door = "1"
	connectAs(t, f, rotated)
	run(t, a)

	send(t, b, "POST", "/api/v1/doors/1/credentials/revoke", "", 204)

	if diff := cmp.Diff([]string{"revoked", "revoked"}, statuses(t, b, "1")); diff != "" {
		t.Errorf("statuses (-want +got):\n%s", diff)
	}
}

func TestStaleLastWillOfRevokedCredentialsChangesNothing(t *testing.T) {
	t.Parallel()
	f := testsupport.NewFactory()
	a, b, conn, _ := enrolledDoor(t, f)
	send(t, b, "POST", "/api/v1/doors/1/credentials/rotate", "", 204)
	rotated := credentialsOf(t, conn)
	rotated.door = "1"
	connectAs(t, f, rotated)
	run(t, a)

	f.BrokerStub.Inject("doorcode/doors/1/online", onlineAs(firstUsername, false))
	run(t, a)

	if diff := cmp.Diff(true, testsupport.JSONAt(b.Get("/api/v1/doors/1").Body, "/controller/online")); diff != "" {
		t.Errorf("online (-want +got):\n%s", diff)
	}
}

func TestDoorConnectingWithCredentialsItWasNotIssuedIsStillOnline(t *testing.T) {
	t.Parallel()
	f := testsupport.NewFactory()
	a, b, conn, _ := enrolledDoor(t, f)
	conn.Drop()
	run(t, a)

	f.BrokerStub.Inject("doorcode/doors/1/online", onlineAs("someone-else", true))
	run(t, a)

	got := []any{testsupport.JSONAt(b.Get("/api/v1/doors/1").Body, "/controller/online"), statuses(t, b, "1")}
	if diff := cmp.Diff([]any{true, []string{"in-use"}}, got); diff != "" {
		t.Errorf("online, statuses (-want +got):\n%s", diff)
	}
}

func TestCredentialsOfAMissingDoorAreNotFound(t *testing.T) {
	t.Parallel()
	b := signedInAdmin(t, testsupport.NewFactory())

	got := []int{
		b.Get("/api/v1/doors/9/credentials").Status,
		b.PostJSON("/api/v1/doors/9/credentials/rotate", "").Status,
		b.PostJSON("/api/v1/doors/9/credentials/revoke", "").Status,
	}

	if diff := cmp.Diff([]int{404, 404, 404}, got); diff != "" {
		t.Errorf("statuses (-want +got):\n%s", diff)
	}
}

func TestRevokingADoorWithoutAControllerChangesNothing(t *testing.T) {
	t.Parallel()
	b := signedInAdmin(t, testsupport.NewFactory())
	createDoor(t, b, frontDoor)

	got := []any{b.PostJSON("/api/v1/doors/1/credentials/revoke", "").Status, b.Get("/api/v1/doors/1").Body}

	if diff := cmp.Diff([]any{204, frontDoorJSON + "\n"}, got); diff != "" {
		t.Errorf("status, door (-want +got):\n%s", diff)
	}
}
