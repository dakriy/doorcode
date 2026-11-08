package entrypoint_test

import (
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/dakriy/doorcode/internal/app"
	"github.com/dakriy/doorcode/internal/testsupport"
)

// The enrollment credentials the broker tests set, and the topic filters
// the enrollment client may use.
const (
	enrollUser     = "doorcode-enroll"
	enrollPassword = "enroll-secret"
)

// run runs a until it has handled everything the stub broker delivered to
// it, failing the test when it cannot start.
func run(t *testing.T, a *app.App) {
	t.Helper()
	if err := a.Run(t.Context()); err != nil {
		t.Fatal(err)
	}
}

// brokerSite starts the application from f with the enrollment password
// set, runs it once so the broker accepts the enrollment credentials, and
// returns it with a browser signed in as the administrator.
func brokerSite(t *testing.T, f *testsupport.Factory) (*app.App, *testsupport.Browser) {
	t.Helper()
	f.EnvStub["DOORCODE_ENROLLMENT_PASSWORD"] = enrollPassword
	a := start(t, f)
	run(t, a)
	return a, signIn(t, f, a.Handler())
}

// announcement is what a controller enrolling with firmware 1.4.2 and a
// signal of -61 dBm publishes.
var announcement = testsupport.CBOR(map[string]any{"v": 1, "online": true, "firmware_version": "1.4.2", "signal": -61})

// enrollWill is the last will of an enrolling controller.
var enrollWill = testsupport.CBOR(map[string]any{"v": 1, "online": false})

// announce connects a controller with hardware id hwid with the enrollment
// credentials, subscribes it to its credentials, and has it announce
// itself.
func announce(t *testing.T, f *testsupport.Factory, hwid string) *testsupport.Connection {
	t.Helper()
	topic := "doorcode/enroll/" + hwid
	conn, err := f.BrokerStub.Connect(enrollUser, enrollPassword, topic, enrollWill)
	if err != nil {
		t.Fatal(err)
	}
	if err := conn.Subscribe(topic + "/credentials"); err != nil {
		t.Fatal(err)
	}
	if err := conn.Publish(topic, announcement, true); err != nil {
		t.Fatal(err)
	}
	return conn
}

// doorCredentials is what a controller was told to connect as.
type doorCredentials struct {
	door     string
	username string
	token    string
}

// lastReceived is the payload of the last message conn received, decoded.
func lastReceived(t *testing.T, conn *testsupport.Connection) map[string]any {
	t.Helper()
	received := conn.Received()
	if len(received) == 0 {
		t.Fatal("the controller received nothing")
	}
	m, _ := testsupport.Decoded(received[len(received)-1].Payload).(map[string]any)
	return m
}

// credentialsOf is the credentials the last message conn received hands
// it.
func credentialsOf(t *testing.T, conn *testsupport.Connection) doorCredentials {
	t.Helper()
	m := lastReceived(t, conn)
	id, _ := m["door_id"].(int64)
	username, _ := m["username"].(string)
	token, _ := m["token"].(string)
	return doorCredentials{door: itoa(int(id)), username: username, token: token}
}

// onlineAs is the online status a door's controller connected as username
// publishes, or its last will when online is false.
func onlineAs(username string, online bool) []byte {
	return testsupport.CBOR(map[string]any{"v": 1, "online": online, "username": username, "firmware_version": "1.4.3", "signal": -58})
}

// connectAs connects a door's controller with creds, subscribes it to its
// commands, and has it publish its online status.
func connectAs(t *testing.T, f *testsupport.Factory, creds doorCredentials) *testsupport.Connection {
	t.Helper()
	online := "doorcode/doors/" + creds.door + "/online"
	conn, err := f.BrokerStub.Connect(creds.username, creds.token, online, onlineAs(creds.username, false))
	if err != nil {
		t.Fatal(err)
	}
	if err := conn.Subscribe("doorcode/doors/" + creds.door + "/command"); err != nil {
		t.Fatal(err)
	}
	if err := conn.Publish(online, onlineAs(creds.username, true), true); err != nil {
		t.Fatal(err)
	}
	return conn
}

// enrolledDoor is the administrator's browser on a broker site where the
// controller A1 waited, was approved as the new door Lobby (1), and
// connected with its credentials, which it returns with its connection.
func enrolledDoor(t *testing.T, f *testsupport.Factory) (*app.App, *testsupport.Browser, *testsupport.Connection, doorCredentials) {
	t.Helper()
	a, b := brokerSite(t, f)
	enrolling := announce(t, f, "A1")
	run(t, a)
	send(t, b, "POST", "/api/v1/enrollments/A1/approve", `{"door":{"name":"Lobby"}}`, 200)
	creds := credentialsOf(t, enrolling)
	enrolling.Close()
	conn := connectAs(t, f, creds)
	run(t, a)
	return a, b, conn, creds
}

// firstUsername and firstToken are the credentials the first door approved
// on a broker site receives, drawn from the stub entropy after sign-in.
const (
	firstUsername = "door-1-424344454647"
	firstToken    = "SElKS0xNTk9QUVJTVFVWV1hZWltcXV5f"
)

func TestEnrollingControllerWaitsWhileConnected(t *testing.T) {
	t.Parallel()
	f := testsupport.NewFactory()
	a, b := brokerSite(t, f)
	f.ClockStub.Set("2026-01-05T09:30:00Z")
	announce(t, f, "A1")

	run(t, a)

	want := `{"enrollments":[{"hardware_id":"A1","firmware_version":"1.4.2","signal":-61,"waiting_since":"2026-01-05T09:30:00Z"}]}` + "\n"
	if diff := cmp.Diff(want, b.Get("/api/v1/enrollments").Body); diff != "" {
		t.Errorf("enrollments (-want +got):\n%s", diff)
	}
}

func TestEnrollingControllerStopsWaitingWhenItDisconnects(t *testing.T) {
	t.Parallel()
	f := testsupport.NewFactory()
	a, b := brokerSite(t, f)
	conn := announce(t, f, "A1")
	run(t, a)

	conn.Drop()
	run(t, a)

	if diff := cmp.Diff(`{"enrollments":[]}`+"\n", b.Get("/api/v1/enrollments").Body); diff != "" {
		t.Errorf("enrollments (-want +got):\n%s", diff)
	}
}

func TestPendingEnrollmentsAreListedLongestWaitingFirst(t *testing.T) {
	t.Parallel()
	f := testsupport.NewFactory()
	a, b := brokerSite(t, f)
	f.ClockStub.Set("2026-01-05T09:10:00Z")
	announce(t, f, "C3")
	run(t, a)
	f.ClockStub.Set("2026-01-05T09:20:00Z")
	announce(t, f, "B2")
	announce(t, f, "A1")
	run(t, a)
	announce(t, f, "C3")
	run(t, a)

	got := testsupport.JSONAt(b.Get("/api/v1/enrollments").Body, "/enrollments")

	want := testsupport.JSON(`[
		{"hardware_id":"C3","firmware_version":"1.4.2","signal":-61,"waiting_since":"2026-01-05T09:10:00Z"},
		{"hardware_id":"A1","firmware_version":"1.4.2","signal":-61,"waiting_since":"2026-01-05T09:20:00Z"},
		{"hardware_id":"B2","firmware_version":"1.4.2","signal":-61,"waiting_since":"2026-01-05T09:20:00Z"}]`)
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("enrollments (-want +got):\n%s", diff)
	}
}

func TestStartingLetsControllersEnrollWithTheEnrollmentCredentialsOnly(t *testing.T) {
	t.Parallel()
	f := testsupport.NewFactory()
	f.EnvStub["DOORCODE_ENROLLMENT_USERNAME"] = "enroll"
	f.EnvStub["DOORCODE_ENROLLMENT_PASSWORD"] = "first"
	run(t, start(t, f))
	f.EnvStub["DOORCODE_ENROLLMENT_PASSWORD"] = "second"

	run(t, start(t, f))

	want := []testsupport.BrokerClient{{
		Username: "enroll", Password: "second",
		Publish: []string{"doorcode/enroll/+"}, Subscribe: []string{"doorcode/enroll/+/credentials"},
	}}
	if diff := cmp.Diff(want, f.BrokerStub.Clients()); diff != "" {
		t.Errorf("clients (-want +got):\n%s", diff)
	}
}

func TestApprovingAControllerAsANewDoorSendsItCredentials(t *testing.T) {
	t.Parallel()
	f := testsupport.NewFactory()
	a, b := brokerSite(t, f)
	conn := announce(t, f, "A1")
	run(t, a)
	f.ClockStub.Set("2026-01-05T09:40:00Z")

	got := b.PostJSON("/api/v1/enrollments/A1/approve", `{"door":{"name":"Lobby","location":"Building A"}}`)

	want := testsupport.Response{Status: 200, ContentType: jsonType, Body: `{"id":1,"name":"Lobby","location":"Building A","notes":"",` +
		`"public":false,"open_schedules":[],"zones":[],"created_at":"2026-01-05T09:40:00Z","controller":{"hardware_id":"A1",` +
		`"firmware_version":"1.4.2","signal":-61,"online":false,"last_seen_at":"2026-01-05T09:40:00Z"}}` + "\n"}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("response (-want +got):\n%s", diff)
	}
	credentials := map[string]any{"v": int64(1), "door_id": int64(1), "username": firstUsername, "token": firstToken}
	if diff := cmp.Diff(credentials, lastReceived(t, conn)); diff != "" {
		t.Errorf("credentials (-want +got):\n%s", diff)
	}
}

func TestApprovedControllerMayUseOnlyItsDoorsTopics(t *testing.T) {
	t.Parallel()
	f := testsupport.NewFactory()
	a, b := brokerSite(t, f)
	announce(t, f, "A1")
	run(t, a)

	send(t, b, "POST", "/api/v1/enrollments/A1/approve", `{"door":{"name":"Lobby"}}`, 200)

	want := []testsupport.BrokerClient{
		{Username: firstUsername, Password: firstToken, Publish: []string{"doorcode/doors/1/#"}, Subscribe: []string{"doorcode/doors/1/#"}},
		{Username: enrollUser, Password: enrollPassword, Publish: []string{"doorcode/enroll/+"}, Subscribe: []string{"doorcode/enroll/+/credentials"}},
	}
	if diff := cmp.Diff(want, f.BrokerStub.Clients()); diff != "" {
		t.Errorf("clients (-want +got):\n%s", diff)
	}
}

func TestApprovedControllerStopsWaitingAndItsAnnouncementIsCleared(t *testing.T) {
	t.Parallel()
	f := testsupport.NewFactory()
	a, b := brokerSite(t, f)
	announce(t, f, "A1")
	run(t, a)

	send(t, b, "POST", "/api/v1/enrollments/A1/approve", `{"door":{"name":"Lobby"}}`, 200)
	run(t, a)

	_, retained := f.BrokerStub.Retained("doorcode/enroll/A1")
	got := []any{b.Get("/api/v1/enrollments").Body, retained}
	if diff := cmp.Diff([]any{`{"enrollments":[]}` + "\n", false}, got); diff != "" {
		t.Errorf("enrollments, announcement retained (-want +got):\n%s", diff)
	}
}

func TestApprovedControllerIsOnlineOnceItConnectsAsItsDoor(t *testing.T) {
	t.Parallel()
	f := testsupport.NewFactory()
	a, b := brokerSite(t, f)
	enrolling := announce(t, f, "A1")
	run(t, a)
	send(t, b, "POST", "/api/v1/enrollments/A1/approve", `{"door":{"name":"Lobby"}}`, 200)
	f.ClockStub.Set("2026-01-05T09:45:00Z")

	connectAs(t, f, credentialsOf(t, enrolling))
	run(t, a)

	want := testsupport.JSON(`{"hardware_id":"A1","firmware_version":"1.4.3","signal":-58,"online":true,"last_seen_at":"2026-01-05T09:45:00Z"}`)
	if diff := cmp.Diff(want, testsupport.JSONAt(b.Get("/api/v1/doors/1").Body, "/controller")); diff != "" {
		t.Errorf("controller (-want +got):\n%s", diff)
	}
}

func TestDoorGoesOfflineWhenItsControllerDisconnects(t *testing.T) {
	t.Parallel()
	f := testsupport.NewFactory()
	a, b, conn, _ := enrolledDoor(t, f)
	f.ClockStub.Set("2026-01-05T10:00:00Z")

	conn.Drop()
	run(t, a)

	got := []any{testsupport.JSONAt(b.Get("/api/v1/doors/1").Body, "/controller/online"), testsupport.JSONAt(b.Get("/api/v1/doors/1").Body, "/controller/last_seen_at")}
	if diff := cmp.Diff([]any{false, "2026-01-05T10:00:00Z"}, got); diff != "" {
		t.Errorf("online, last seen (-want +got):\n%s", diff)
	}
}

func TestCredentialsListShowsWhenTheControllerFirstUsedThem(t *testing.T) {
	t.Parallel()
	f := testsupport.NewFactory()
	a, b, conn, creds := enrolledDoor(t, f)
	conn.Close()
	f.ClockStub.Set("2026-01-05T11:00:00Z")
	connectAs(t, f, creds)
	run(t, a)

	got := b.Get("/api/v1/doors/1/credentials").Body

	want := `{"credentials":[{"id":1,"username":"` + firstUsername + `","issued_at":"2026-01-05T09:00:00Z",` +
		`"used_at":"2026-01-05T09:00:00Z","status":"in-use"}]}` + "\n"
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("credentials (-want +got):\n%s", diff)
	}
}

func TestApprovalNeedsAWaitingControllerAndOneDoor(t *testing.T) {
	t.Parallel()
	cases := map[string]struct {
		target string
		body   string
		want   testsupport.Response
	}{
		"a controller that is not waiting": {"/api/v1/enrollments/B2/approve", `{"door_id":1}`, testsupport.Response{Status: 404, ContentType: jsonType,
			Body: `{"error":"no controller with this hardware id is waiting to be approved"}` + "\n"}},
		"a missing door": {"/api/v1/enrollments/A1/approve", `{"door_id":9}`, testsupport.Response{Status: 404, ContentType: jsonType,
			Body: `{"error":"no door has this id"}` + "\n"}},
		"a deleted door": {"/api/v1/enrollments/A1/approve", `{"door_id":1}`, testsupport.Response{Status: 409, ContentType: jsonType,
			Body: `{"error":"the door is deleted; restore it first"}` + "\n"}},
		"both kinds of door": {"/api/v1/enrollments/A1/approve", `{"door_id":1,"door":{"name":"Lobby"}}`, testsupport.Response{Status: 400,
			ContentType: jsonType, Body: `{"error":"body must be a JSON object with either a door_id or a door to create"}` + "\n"}},
		"no door": {"/api/v1/enrollments/A1/approve", `{}`, testsupport.Response{Status: 400,
			ContentType: jsonType, Body: `{"error":"body must be a JSON object with either a door_id or a door to create"}` + "\n"}},
		"not JSON": {"/api/v1/enrollments/A1/approve", `door`, testsupport.Response{Status: 400,
			ContentType: jsonType, Body: `{"error":"body must be a JSON object with either a door_id or a door to create"}` + "\n"}},
		"a new door with a wrong field": {"/api/v1/enrollments/A1/approve", `{"door":{"name":""}}`, testsupport.Response{Status: 400,
			ContentType: jsonType, Body: `{"error":"Enter a name."}` + "\n"}},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			f := testsupport.NewFactory()
			a, b := brokerSite(t, f)
			createDoor(t, b, frontDoor)
			send(t, b, "DELETE", "/api/v1/doors/1", "", 204)
			announce(t, f, "A1")
			run(t, a)

			got := b.PostJSON(c.target, c.body)

			if diff := cmp.Diff(c.want, got); diff != "" {
				t.Errorf("response (-want +got):\n%s", diff)
			}
		})
	}
}

func TestApprovingAsADoorThatHasAControllerReplacesIt(t *testing.T) {
	t.Parallel()
	f := testsupport.NewFactory()
	a, b, conn, old := enrolledDoor(t, f)
	announce(t, f, "B2")
	run(t, a)

	approved := b.PostJSON("/api/v1/enrollments/B2/approve", `{"door_id":1}`)

	got := []any{testsupport.JSONString(approved.Body, "/controller/hardware_id"), conn.Connected(), connects(f, old), statuses(t, b, "1")}
	want := []any{"B2", false, []bool{false}, []string{"revoked", "issued"}}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("hardware id, connected, connects, statuses (-want +got):\n%s", diff)
	}
}

func TestApprovingAControllerAsAnotherDoorTakesItOffTheDoorItWas(t *testing.T) {
	t.Parallel()
	f := testsupport.NewFactory()
	a, b, _, old := enrolledDoor(t, f)
	createDoor(t, b, `{"name":"Dock"}`)
	announce(t, f, "A1")
	run(t, a)

	send(t, b, "POST", "/api/v1/enrollments/A1/approve", `{"door_id":2}`, 200)

	got := []any{testsupport.JSONAt(b.Get("/api/v1/doors/1").Body, "/controller"), connects(f, old),
		testsupport.JSONString(b.Get("/api/v1/doors/2").Body, "/controller/hardware_id")}
	if diff := cmp.Diff([]any{nil, []bool{false}, "A1"}, got); diff != "" {
		t.Errorf("first door's controller, old credentials connect, second door's (-want +got):\n%s", diff)
	}
}

func TestApprovingAControllerAsAnotherDoorReplacesThatDoorsController(t *testing.T) {
	t.Parallel()
	f := testsupport.NewFactory()
	a, b, _, _ := enrolledDoor(t, f)
	announce(t, f, "B2")
	run(t, a)
	send(t, b, "POST", "/api/v1/enrollments/B2/approve", `{"door":{"name":"Dock"}}`, 200)
	announce(t, f, "A1")
	run(t, a)

	send(t, b, "POST", "/api/v1/enrollments/A1/approve", `{"door_id":2}`, 200)

	got := []any{statuses(t, b, "1"), statuses(t, b, "2")}
	want := []any{[]string{"revoked"}, []string{"revoked", "issued"}}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("first and second door's credentials (-want +got):\n%s", diff)
	}
}

func TestClearedAnnouncementStopsAControllerWaiting(t *testing.T) {
	t.Parallel()
	f := testsupport.NewFactory()
	a, b := brokerSite(t, f)
	announce(t, f, "A1")
	run(t, a)

	f.BrokerStub.Inject("doorcode/enroll/A1", nil)
	run(t, a)

	if diff := cmp.Diff(`{"enrollments":[]}`+"\n", b.Get("/api/v1/enrollments").Body); diff != "" {
		t.Errorf("enrollments (-want +got):\n%s", diff)
	}
}

func TestApprovalFails(t *testing.T) {
	t.Parallel()
	cases := map[string]func(f *testsupport.Factory){
		"when doors cannot be read":                  func(f *testsupport.Factory) { f.StoreStub.FailDoors() },
		"when credentials cannot be read":            func(f *testsupport.Factory) { f.StoreStub.FailCredentials() },
		"when the broker refuses the client":         func(f *testsupport.Factory) { f.BrokerStub.FailCreating() },
		"when the credentials cannot be stored":      func(f *testsupport.Factory) { f.StoreStub.FailCredentialWrites() },
		"when the controller cannot be stored":       func(f *testsupport.Factory) { f.StoreStub.FailControllerWrites() },
		"when the credentials cannot be published":   func(f *testsupport.Factory) { f.BrokerStub.FailPublishing() },
		"when the old credentials cannot be revoked": func(f *testsupport.Factory) { f.BrokerStub.FailDeleting() },
	}
	for name, arrange := range cases {
		t.Run(name, func(t *testing.T) {
			f := testsupport.NewFactory()
			a, b, _, _ := enrolledDoor(t, f)
			createDoor(t, b, `{"name":"Dock"}`)
			announce(t, f, "A1")
			run(t, a)
			arrange(f)

			got := b.PostJSON("/api/v1/enrollments/A1/approve", `{"door_id":2}`)

			if diff := cmp.Diff(500, got.Status); diff != "" {
				t.Errorf("status (-want +got):\n%s", diff)
			}
		})
	}
}

func TestApprovalFailsWhenTheControllerCannotBeStored(t *testing.T) {
	t.Parallel()
	f := testsupport.NewFactory()
	a, b := brokerSite(t, f)
	announce(t, f, "A1")
	run(t, a)
	f.StoreStub.FailControllerWrites()

	got := b.PostJSON("/api/v1/enrollments/A1/approve", `{"door":{"name":"Lobby"}}`)

	if diff := cmp.Diff(500, got.Status); diff != "" {
		t.Errorf("status (-want +got):\n%s", diff)
	}
}

func TestApprovalFailsWhenTheOldDoorCannotLoseItsController(t *testing.T) {
	t.Parallel()
	f := testsupport.NewFactory()
	a, b, _, _ := enrolledDoor(t, f)
	createDoor(t, b, `{"name":"Dock"}`)
	announce(t, f, "A1")
	run(t, a)
	f.StoreStub.FailControllerWrites()

	got := b.PostJSON("/api/v1/enrollments/A1/approve", `{"door_id":2}`)

	if diff := cmp.Diff([]any{500, "A1"}, []any{got.Status, testsupport.JSONString(b.Get("/api/v1/doors/1").Body, "/controller/hardware_id")}); diff != "" {
		t.Errorf("status, first door's controller (-want +got):\n%s", diff)
	}
}

func TestCredentialChangesFail(t *testing.T) {
	t.Parallel()
	cases := map[string]struct {
		arrange func(f *testsupport.Factory)
		method  string
		target  string
	}{
		"to list when doors cannot be read":         {func(f *testsupport.Factory) { f.StoreStub.FailDoors() }, "GET", "credentials"},
		"to list when credentials cannot be read":   {func(f *testsupport.Factory) { f.StoreStub.FailCredentials() }, "GET", "credentials"},
		"to rotate when doors cannot be read":       {func(f *testsupport.Factory) { f.StoreStub.FailDoors() }, "POST", "credentials/rotate"},
		"to rotate when the broker refuses":         {func(f *testsupport.Factory) { f.BrokerStub.FailCreating() }, "POST", "credentials/rotate"},
		"to rotate when the command cannot be sent": {func(f *testsupport.Factory) { f.BrokerStub.FailPublishing() }, "POST", "credentials/rotate"},
		"to revoke when doors cannot be read":       {func(f *testsupport.Factory) { f.StoreStub.FailDoors() }, "POST", "credentials/revoke"},
		"to revoke when the broker refuses":         {func(f *testsupport.Factory) { f.BrokerStub.FailDeleting() }, "POST", "credentials/revoke"},
		"to revoke when revoking cannot be stored":  {func(f *testsupport.Factory) { f.StoreStub.FailCredentialWrites() }, "POST", "credentials/revoke"},
		"to revoke when the state cannot be stored": {func(f *testsupport.Factory) { f.StoreStub.FailControllerWrites() }, "POST", "credentials/revoke"},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			f := testsupport.NewFactory()
			_, b, _, _ := enrolledDoor(t, f)
			c.arrange(f)

			got := b.Send(c.method, "/api/v1/doors/1/"+c.target, "")

			if diff := cmp.Diff(500, got.Status); diff != "" {
				t.Errorf("status (-want +got):\n%s", diff)
			}
		})
	}
}
