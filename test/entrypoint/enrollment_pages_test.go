package entrypoint_test

import (
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/dakriy/doorcode/internal/testsupport"
)

// adminNav is the navigation an administrator sees on a page no link
// leads to.
var adminNav = []string{"Home", "Users", "Groups", "Schedules", "Doors", "Zones", "Settings", "API tokens", "API docs"}

// enrollmentsHead and credentialsHead are the header rows of the pending
// enrollments and of a door's credentials.
var (
	enrollmentsHead = []string{"Hardware id", "Firmware", "Signal", "Waiting since", "Actions"}
	credentialsHead = []string{"Username", "Issued", "Status"}
)

// waiting is the administrator's browser on a broker site where the
// controller A1 waits, with the application and the controller's
// connection.
func waiting(t *testing.T, f *testsupport.Factory) (*testsupport.Browser, *testsupport.Connection) {
	t.Helper()
	a, b := brokerSite(t, f)
	conn := announce(t, f, "A1")
	run(t, a)
	b.Get("/")
	return b, conn
}

func TestEnrollmentsPageListsTheWaitingControllers(t *testing.T) {
	t.Parallel()
	f := testsupport.NewFactory()
	b, _ := waiting(t, f)

	got := pageOf(b.Get("/admin/enrollments"))

	want := testsupport.Page{
		Title: "Pending enrollments · doorcode", Heading: "Pending enrollments", Nav: adminNav,
		Tables: map[string][][]string{"Pending enrollments": {
			enrollmentsHead,
			{"A1", "1.4.2", "-61 dBm", "Mon 5 Jan 2026 09:00", "Approve"},
		}},
	}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("page (-want +got):\n%s", diff)
	}
}

func TestEnrollmentsPageSaysWhenNoControllerIsWaiting(t *testing.T) {
	t.Parallel()
	b := signedInAdmin(t, testsupport.NewFactory())
	b.Get("/")

	got := pageOf(b.Get("/admin/enrollments")).Tables["Pending enrollments"]

	want := [][]string{enrollmentsHead, {"No controller is waiting. One appears here while it is connected with the enrollment credentials."}}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("table (-want +got):\n%s", diff)
	}
}

func TestEnrollmentsPageShowsWhatAControllerDoesNotReport(t *testing.T) {
	t.Parallel()
	f := testsupport.NewFactory()
	a, b := brokerSite(t, f)
	f.BrokerStub.Inject("doorcode/enroll/B2", testsupport.CBOR(map[string]any{"v": 1, "online": true}))
	run(t, a)
	b.Get("/")

	got := pageOf(b.Get("/admin/enrollments")).Tables["Pending enrollments"][1]

	if diff := cmp.Diff([]string{"B2", "Not reported", "Not reported", "Mon 5 Jan 2026 09:00", "Approve"}, got); diff != "" {
		t.Errorf("row (-want +got):\n%s", diff)
	}
}

func TestDoorsPageCountsTheWaitingControllers(t *testing.T) {
	t.Parallel()
	f := testsupport.NewFactory()
	b, _ := waiting(t, f)

	got := pageOf(b.Get("/admin/doors")).Messages

	if diff := cmp.Diff([]string{"1 waiting"}, got); diff != "" {
		t.Errorf("messages (-want +got):\n%s", diff)
	}
}

func TestApprovalPageOffersANewDoorOrAnExistingOne(t *testing.T) {
	t.Parallel()
	f := testsupport.NewFactory()
	b, _ := waiting(t, f)
	createSchedule(t, b, `{"name":"Weekdays","windows":[{"days":["mon"],"start":"08:00","end":"18:00"}]}`)
	createZone(t, b, `{"name":"North"}`)
	createDoor(t, b, `{"name":"Dock"}`)
	createDoor(t, b, `{"name":"Old"}`)
	send(t, b, "DELETE", "/api/v1/doors/2", "", 204)

	page := testsupport.ParsePage(b.Get("/admin/enrollments/A1").Body)

	got := []any{page.Title, page.Heading, page.Choices, page.Options}
	want := []any{"Approve A1 · doorcode", "Approve A1",
		map[string]string{"open_schedule_ids=1": "Always Every day, all day", "open_schedule_ids=2": "Weekdays Mon 08:00–18:00", "zone_ids=1": "North",
			"public=on": "Show in the user portal People see the door, its picture, and when their code works there."},
		map[string][]string{"door_id": {"Dock"}},
	}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("title, heading, choices, options (-want +got):\n%s", diff)
	}
}

func TestApprovalPageSaysWhichDoorsHaveAControllerAlready(t *testing.T) {
	t.Parallel()
	f := testsupport.NewFactory()
	a, b, _, _ := enrolledDoor(t, f)
	createDoor(t, b, `{"name":"Dock"}`)
	announce(t, f, "B2")
	run(t, a)

	got := testsupport.ParsePage(b.Get("/admin/enrollments/B2").Body).Options

	if diff := cmp.Diff(map[string][]string{"door_id": {"Dock", "Lobby (replaces A1)"}}, got); diff != "" {
		t.Errorf("options (-want +got):\n%s", diff)
	}
}

func TestApprovalPageOfAControllerThatIsNotWaitingIsNotFound(t *testing.T) {
	t.Parallel()
	b := signedInAdmin(t, testsupport.NewFactory())

	got := b.Get("/admin/enrollments/A1")

	page := pageOf(got)
	if diff := cmp.Diff([]any{404, "Not found"}, []any{got.Status, page.Heading}); diff != "" {
		t.Errorf("status, heading (-want +got):\n%s", diff)
	}
}

func TestApprovingAsANewDoorFromTheFormOpensTheDoor(t *testing.T) {
	t.Parallel()
	f := testsupport.NewFactory()
	b, conn := waiting(t, f)
	createZone(t, b, `{"name":"North"}`)

	approved := b.Post("/admin/enrollments/A1/new-door", "name", "Lobby", "location", "Building A", "open_schedule_ids", "1", "zone_ids", "1")
	got := pageOf(b.Get(approved.Location))

	if diff := cmp.Diff([]any{303, "/admin/doors/1", firstUsername}, []any{approved.Status, approved.Location, credentialsOf(t, conn).username}); diff != "" {
		t.Errorf("redirect, credentials sent (-want +got):\n%s", diff)
	}
	want := []any{
		[]string{"The controller has been approved. Its credentials are on their way, and it connects as this door once it has them."},
		[]string{"New credentials can be sent only while the controller is connected."},
		[]string{"open_schedule_ids=1", "zone_ids=1"},
		[][]string{credentialsHead, {firstUsername, "Mon 5 Jan 2026 09:00", "Issued, not yet used"}},
	}
	if diff := cmp.Diff(want, []any{got.Messages, got.Notes, got.Checked, got.Tables["Credentials"]}); diff != "" {
		t.Errorf("messages, notes, checked, credentials (-want +got):\n%s", diff)
	}
}

func TestApprovalFormSaysWhatIsWrongWithTheNewDoor(t *testing.T) {
	t.Parallel()
	f := testsupport.NewFactory()
	b, _ := waiting(t, f)

	got := b.Post("/admin/enrollments/A1/new-door", "name", "", "location", "Building A", "zone_ids", "9")

	page := pageOf(got)
	want := []any{422,
		map[string]string{"name": "Enter a name.", "zone_ids": "No zone has id 9."},
		map[string]string{"name": "", "location": "Building A", "notes": ""},
	}
	if diff := cmp.Diff(want, []any{got.Status, page.FieldErrors, page.Values}); diff != "" {
		t.Errorf("status, field errors, values (-want +got):\n%s", diff)
	}
}

func TestApprovingAsAnExistingDoorFromTheForm(t *testing.T) {
	t.Parallel()
	f := testsupport.NewFactory()
	b, conn := waiting(t, f)
	createDoor(t, b, frontDoor)

	got := b.Post("/admin/enrollments/A1/door", "door_id", "1")

	want := []any{303, "/admin/doors/1", "1"}
	if diff := cmp.Diff(want, []any{got.Status, got.Location, credentialsOf(t, conn).door}); diff != "" {
		t.Errorf("status, location, door sent (-want +got):\n%s", diff)
	}
}

func TestApprovalFormRefusesADoorThatIsNotListed(t *testing.T) {
	t.Parallel()
	cases := map[string]string{"a missing door": "9", "a deleted door": "1", "no door": ""}
	for name, door := range cases {
		t.Run(name, func(t *testing.T) {
			f := testsupport.NewFactory()
			b, _ := waiting(t, f)
			createDoor(t, b, frontDoor)
			createDoor(t, b, `{"name":"Dock"}`)
			send(t, b, "DELETE", "/api/v1/doors/1", "", 204)

			got := b.Post("/admin/enrollments/A1/door", "door_id", door)

			page := pageOf(got)
			if diff := cmp.Diff([]any{422, map[string]string{"door_id": "Choose one of the doors listed."}}, []any{got.Status, page.FieldErrors}); diff != "" {
				t.Errorf("status, field errors (-want +got):\n%s", diff)
			}
		})
	}
}

func TestApprovalFormOfAControllerThatStoppedWaitingIsNotFound(t *testing.T) {
	t.Parallel()
	cases := map[string][]string{
		"a new door":       {"/admin/enrollments/B2/new-door", "name", "Lobby"},
		"an existing door": {"/admin/enrollments/B2/door", "door_id", "1"},
	}
	for name, post := range cases {
		t.Run(name, func(t *testing.T) {
			f := testsupport.NewFactory()
			b, _ := waiting(t, f)
			createDoor(t, b, frontDoor)

			got := b.Post(post[0], post[1:]...)

			page := pageOf(got)
			if diff := cmp.Diff([]any{404, "Not found"}, []any{got.Status, page.Heading}); diff != "" {
				t.Errorf("status, heading (-want +got):\n%s", diff)
			}
		})
	}
}

func TestApprovalPageFails(t *testing.T) {
	t.Parallel()
	cases := map[string]struct {
		arrange func(f *testsupport.Factory)
		post    []string
	}{
		"to show when the schedules cannot be read": {func(f *testsupport.Factory) { f.StoreStub.FailSchedules() }, nil},
		"to show when the site timezone is wrong": {
			func(f *testsupport.Factory) {
				f.StoreStub.SeedSetting("site_timezone", "Mars/Olympus", testsupport.DefaultNow)
			}, nil,
		},
		"to approve when the broker fails": {func(f *testsupport.Factory) { f.BrokerStub.FailCreating() }, []string{"/admin/enrollments/A1/new-door", "name", "Lobby"}},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			f := testsupport.NewFactory()
			b, _ := waiting(t, f)
			c.arrange(f)

			var got testsupport.Response
			if c.post == nil {
				got = b.Get("/admin/enrollments/A1")
			} else {
				got = b.Post(c.post[0], c.post[1:]...)
			}

			if diff := cmp.Diff(500, got.Status); diff != "" {
				t.Errorf("status (-want +got):\n%s", diff)
			}
		})
	}
}

func TestEnrollmentsPageFailsWhenTheSiteTimezoneIsWrong(t *testing.T) {
	t.Parallel()
	f := testsupport.NewFactory()
	b, _ := waiting(t, f)
	f.StoreStub.SeedSetting("site_timezone", "Mars/Olympus", testsupport.DefaultNow)

	if diff := cmp.Diff(500, b.Get("/admin/enrollments").Status); diff != "" {
		t.Errorf("status (-want +got):\n%s", diff)
	}
}

func TestDoorPageShowsItsControllerAndCredentials(t *testing.T) {
	t.Parallel()
	f := testsupport.NewFactory()
	a, b, conn, _ := enrolledDoor(t, f)
	send(t, b, "POST", "/api/v1/doors/1/credentials/rotate", "", 204)
	rotated := credentialsOf(t, conn)
	rotated.door = "1"
	f.ClockStub.Set("2026-01-05T09:30:00Z")
	connectAs(t, f, rotated)
	run(t, a)

	page := doorPage(t, b, "/admin/doors/1")

	got := []any{page.Details, page.Tables["Credentials"]}
	want := []any{
		map[string]string{"Hardware id": "A1", "Firmware": "1.4.3", "Signal": "-58 dBm", "Last seen": "Mon 5 Jan 2026 09:30"},
		[][]string{credentialsHead,
			{secondUsername, "Mon 5 Jan 2026 09:00", "In use"},
			{firstUsername, "Mon 5 Jan 2026 09:00", "Revoked"}},
	}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("notes, credentials, table (-want +got):\n%s", diff)
	}
}

func TestDoorsPageShowsWhetherEachDoorIsOnline(t *testing.T) {
	t.Parallel()
	f := testsupport.NewFactory()
	a, b, _, _ := enrolledDoor(t, f)
	createDoor(t, b, `{"name":"Attic"}`)
	conn := announce(t, f, "B2")
	run(t, a)
	send(t, b, "POST", "/api/v1/enrollments/B2/approve", `{"door":{"name":"Dock"}}`, 200)
	conn.Close()

	rows := doorPage(t, b, "/admin/doors").Tables["Doors"]

	var got [][]string
	for _, row := range rows[1 : len(rows)-1] {
		got = append(got, row[1:3])
	}
	want := [][]string{{"Attic", "Not enrolled"}, {"Dock", "Offline"}, {"Lobby", "Online"}}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("names and statuses (-want +got):\n%s", diff)
	}
}

func TestRotatingFromTheDoorPage(t *testing.T) {
	t.Parallel()
	f := testsupport.NewFactory()
	_, b, _, _ := enrolledDoor(t, f)
	b.Get("/")

	rotated := b.Post("/admin/doors/1/credentials/rotate")
	got := pageOf(b.Get(rotated.Location))

	want := []any{303, []string{"New credentials are on their way to the controller. The old ones work until it connects with the new ones or the grace period passes."},
		[][]string{credentialsHead, {secondUsername, "Mon 5 Jan 2026 09:00", "Issued, not yet used"}, {firstUsername, "Mon 5 Jan 2026 09:00", "In use"}}}
	if diff := cmp.Diff(want, []any{rotated.Status, got.Messages, got.Tables["Credentials"]}); diff != "" {
		t.Errorf("status, messages, credentials (-want +got):\n%s", diff)
	}
}

func TestRotatingFromTheDoorPageNeedsTheControllerConnected(t *testing.T) {
	t.Parallel()
	f := testsupport.NewFactory()
	a, b, conn, _ := enrolledDoor(t, f)
	conn.Drop()
	run(t, a)
	b.Get("/")

	got := b.Post("/admin/doors/1/credentials/rotate")

	page := pageOf(got)
	want := []any{409, []string{"The controller is not connected, so it cannot be sent new credentials. Try again once it is online."}}
	if diff := cmp.Diff(want, []any{got.Status, page.Messages}); diff != "" {
		t.Errorf("status, messages (-want +got):\n%s", diff)
	}
}

func TestRevokingFromTheDoorPage(t *testing.T) {
	t.Parallel()
	f := testsupport.NewFactory()
	_, b, conn, _ := enrolledDoor(t, f)
	b.Get("/")

	revoked := b.Post("/admin/doors/1/credentials/revoke")
	got := pageOf(b.Get(revoked.Location))

	want := []any{303, false, []string{"The controller's credentials have been revoked and it has been disconnected. It connects again only by enrolling and being approved."},
		[][]string{credentialsHead, {firstUsername, "Mon 5 Jan 2026 09:00", "Revoked"}}}
	if diff := cmp.Diff(want, []any{revoked.Status, conn.Connected(), got.Messages, got.Tables["Credentials"]}); diff != "" {
		t.Errorf("status, connected, messages, credentials (-want +got):\n%s", diff)
	}
}

func TestDoorPageFailsWhenCredentialsCannotBeRead(t *testing.T) {
	t.Parallel()
	f := testsupport.NewFactory()
	b := signedInAdmin(t, f)
	createDoor(t, b, frontDoor)
	f.StoreStub.FailCredentials()

	if diff := cmp.Diff(500, b.Get("/admin/doors/1").Status); diff != "" {
		t.Errorf("status (-want +got):\n%s", diff)
	}
}

func TestLiveStreamTellsPagesWhenDoorsOrEnrollmentsChange(t *testing.T) {
	t.Parallel()
	f := testsupport.NewFactory()
	a, b := brokerSite(t, f)
	stream := b.Stream("/admin/live")
	defer stream.Close()
	opened := stream.Next()

	announce(t, f, "A1")
	run(t, a)

	got := []any{stream.Status(), stream.ContentType(), opened, stream.Next()}
	want := []any{200, "text/event-stream", ": watching doors and enrollments\n\n", "event: doors\ndata: changed\n\n"}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("status, type, opening, event (-want +got):\n%s", diff)
	}
}

func TestPagesThatShowLiveStateLoadTheLiveScript(t *testing.T) {
	t.Parallel()
	f := testsupport.NewFactory()
	b, _ := waiting(t, f)
	createDoor(t, b, frontDoor)

	var got []testsupport.Response
	for _, target := range []string{"/admin/doors", "/admin/doors/1", "/admin/enrollments"} {
		assets := testsupport.ParsePage(b.Get(target).Body).Assets
		script := b.Get(assets[len(assets)-1])
		got = append(got, testsupport.Response{Status: script.Status, ContentType: script.ContentType})
	}

	js := testsupport.Response{Status: 200, ContentType: "text/javascript; charset=utf-8"}
	if diff := cmp.Diff([]testsupport.Response{js, js, js}, got); diff != "" {
		t.Errorf("live scripts (-want +got):\n%s", diff)
	}
}
