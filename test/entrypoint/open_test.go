package entrypoint_test

import (
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/dakriy/doorcode/internal/testsupport"
)

// holdingOnly leaves the administrator of b's site a member of only a new
// group carrying roles, a JSON array, whatever groups the site has.
func holdingOnly(t *testing.T, f *testsupport.Factory, b *testsupport.Browser, roles string) {
	t.Helper()
	created := send(t, b, "POST", "/api/v1/groups", `{"slug":"holding","name":"Holding","roles":`+roles+`}`, 201)
	join(t, b, testsupport.Itoa(int(testsupport.JSONAt(created.Body, "/id").(float64))), "1")
	f.StoreStub.LeaveGroup(adminEmail, "administrators")
}

// commandOf is the last command conn received, without its id.
func commandOf(t *testing.T, conn *testsupport.Connection) map[string]any {
	t.Helper()
	m := lastReceived(t, conn)
	delete(m, "id")
	return m
}

func TestOpeningADoorFromTheAPISendsItTheCommandAndLogsTheCaller(t *testing.T) {
	t.Parallel()
	f := testsupport.NewFactory()
	_, b, conn, _ := enrolledDoor(t, f)
	send(t, b, "PUT", "/api/v1/doors/1/config", `{"overrides":{"strike_pulse_ms":"500"}}`, 200)

	got := send(t, b, "POST", "/api/v1/doors/1/open", "", 200)

	opened := testsupport.JSON(`{"id":1,"at":"2026-01-05T09:00:00Z","door":{"id":1,"name":"Lobby"},"type":"open","outcome":"allowed",` +
		`"reason":"admin UI","user":{"id":1,"name":"Administrator"},"detail":"Strike released for 500 ms."}`)
	want := []any{opened, map[string]any{"v": int64(1), "command": "open", "strike_ms": int64(500)}, opened}
	if diff := cmp.Diff(want, []any{testsupport.JSON(got.Body), commandOf(t, conn),
		testsupport.JSONAt(b.Get("/api/v1/events").Body, "/events/0")}); diff != "" {
		t.Errorf("answer, command, event (-want +got):\n%s", diff)
	}
}

func TestADoorCommanderOpensDoorsAndManagesOverridesButChangesNothingElse(t *testing.T) {
	t.Parallel()
	f := testsupport.NewFactory()
	_, b, conn, _ := enrolledDoor(t, f)
	holdingOnly(t, f, b, `["door-commander"]`)

	opened := b.Post("/admin/doors/1/open")
	got := []any{opened.Status, opened.Location, commandOf(t, conn)["command"], b.Get("/api/v1/doors").Status,
		b.Send("POST", "/api/v1/doors/1/overrides", lockedToday).Status, b.Post("/admin/doors/1/overrides/1/delete").Status,
		testsupport.JSON(b.Send("PUT", "/api/v1/doors/1", `{"name":"Hall"}`).Body), b.Get("/admin/doors/1/calendar").Status}

	want := []any{303, "/admin/doors/1?tab=overview", "open", 403, 201, 303, testsupport.JSON(`{"error":"the manage-doors role is required"}`), 200}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("open, where it goes, command, doors API, override, deleting it, changing the door, timeline (-want +got):\n%s", diff)
	}
}

func TestADoorCommandersDoorPageOffersOpeningAndOverridesOnly(t *testing.T) {
	t.Parallel()
	f := testsupport.NewFactory()
	_, b, _, _ := enrolledDoor(t, f)
	holdingOnly(t, f, b, `["door-commander"]`)
	b.Get("/")

	page := pageOf(b.Get("/admin/doors/1?tab=access"))
	list := pageOf(b.Get("/admin/doors"))

	want := []any{
		[]string{"Dashboard", "Doors: Doors (current)", "Doors: Mesh", "Portal", "API tokens", "Overview", "Access (current)"},
		freshAccessTables,
		[]string{"", "Lobby", "Online", "None", "Never", "Hidden"},
	}
	if diff := cmp.Diff(want, []any{page.Nav, page.Tables, list.Tables["Doors"][1]}); diff != "" {
		t.Errorf("navigation, tables, listed door (-want +got):\n%s", diff)
	}
}

func TestOpeningADoorNeedsADoorRoleAndItsControllerConnected(t *testing.T) {
	t.Parallel()
	cases := map[string]struct {
		arrange func(t *testing.T, f *testsupport.Factory, b *testsupport.Browser, conn *testsupport.Connection)
		target  string
		status  int
		body    string
	}{
		"a user holding neither role": {func(t *testing.T, f *testsupport.Factory, b *testsupport.Browser, _ *testsupport.Connection) {
			holdingOnly(t, f, b, `["view-logs"]`)
		}, "/api/v1/doors/1/open", 403, `{"error":"the manage-doors or door-commander role is required"}`},
		"a door that is not there": {nil, "/api/v1/doors/9/open", 404, `{"error":"no door has this id"}`},
		"a door no controller is enrolled as": {func(t *testing.T, _ *testsupport.Factory, b *testsupport.Browser, _ *testsupport.Connection) {
			createDoor(t, b, `{"name":"Dock"}`)
		}, "/api/v1/doors/2/open", 409, `{"error":"the door's controller is not connected"}`},
		"a door whose controller went away": {func(t *testing.T, f *testsupport.Factory, b *testsupport.Browser, conn *testsupport.Connection) {
			conn.Drop()
		}, "/api/v1/doors/1/open", 409, `{"error":"the door's controller is not connected"}`},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			f := testsupport.NewFactory()
			a, b, conn, _ := enrolledDoor(t, f)
			if c.arrange != nil {
				c.arrange(t, f, b, conn)
				run(t, a)
			}

			got := b.Post(c.target)

			if diff := cmp.Diff([]any{c.status, testsupport.JSON(c.body)}, []any{got.Status, testsupport.JSON(got.Body)}); diff != "" {
				t.Errorf("status, body (-want +got):\n%s", diff)
			}
		})
	}
}

func TestOpeningADoorFromThePageOfADoorThatIsNotThereSaysSo(t *testing.T) {
	t.Parallel()
	b := signedInAdmin(t, testsupport.NewFactory())
	b.Get("/")

	got := b.Post("/admin/doors/9/open")

	if diff := cmp.Diff([]any{404, "Not found"}, []any{got.Status, pageOf(got).Heading}); diff != "" {
		t.Errorf("status, heading (-want +got):\n%s", diff)
	}
}

func TestOpeningADoorFromItsPageWhileItsControllerIsAwaySaysSo(t *testing.T) {
	t.Parallel()
	f := testsupport.NewFactory()
	a, b, conn, _ := enrolledDoor(t, f)
	conn.Drop()
	run(t, a)
	b.Get("/")

	got := b.Post("/admin/doors/1/open")

	want := []any{409, []string{"The controller is not connected, so the door cannot be opened from here. Try again once it is online."}}
	if diff := cmp.Diff(want, []any{got.Status, pageOf(got).Messages}); diff != "" {
		t.Errorf("status, messages (-want +got):\n%s", diff)
	}
}

func TestOpeningADoorThatCannotBeSentTheCommandIsLoggedAsUnreachable(t *testing.T) {
	t.Parallel()
	f := testsupport.NewFactory()
	_, b, _, _ := enrolledDoor(t, f)
	f.BrokerStub.FailPublishing()

	got := b.Post("/api/v1/doors/1/open")

	want := []any{500, "denied", "unreachable"}
	events := b.Get("/api/v1/events").Body
	if diff := cmp.Diff(want, []any{got.Status, testsupport.JSONAt(events, "/events/0/outcome"), testsupport.JSONAt(events, "/events/0/reason")}); diff != "" {
		t.Errorf("status, outcome, reason (-want +got):\n%s", diff)
	}
}

func TestOpeningADoorFailsWhenTheStoreDoes(t *testing.T) {
	t.Parallel()
	cases := map[string]func(f *testsupport.Factory){
		"its settings": func(f *testsupport.Factory) { f.StoreStub.FailSettings() },
		"the events":   func(f *testsupport.Factory) { f.StoreStub.FailEventWrites() },
	}
	for name, fail := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			f := testsupport.NewFactory()
			_, b, _, _ := enrolledDoor(t, f)
			fail(f)

			if diff := cmp.Diff(500, b.Post("/api/v1/doors/1/open").Status); diff != "" {
				t.Errorf("status (-want +got):\n%s", diff)
			}
		})
	}
}

func TestDoorCommandersAndFirmwareManagersLoadThePicturesOfDoorsThePortalDoesNotShow(t *testing.T) {
	t.Parallel()
	for _, roles := range []string{`["door-commander"]`, `["manage-firmware"]`} {
		t.Run(roles, func(t *testing.T) {
			t.Parallel()
			f := testsupport.NewFactory()
			_, b, _, _ := enrolledDoor(t, f)
			uploadPicture(t, b, "1", testsupport.PNG(30, 20))
			holdingOnly(t, f, b, roles)

			got := []any{
				testsupport.ImageInfo(b.Get("/api/v1/doors/1/pictures/1").Body),
				testsupport.ImageInfo(b.Get("/api/v1/doors/1/pictures/1/thumbnail").Body),
			}

			if diff := cmp.Diff([]any{"jpeg 30x20", "jpeg 30x20"}, got); diff != "" {
				t.Errorf("image, thumbnail (-want +got):\n%s", diff)
			}
		})
	}
}

func TestAFirmwareManagersDoorPageIsReadOnlyApartFromTheFirmwareCard(t *testing.T) {
	t.Parallel()
	f := testsupport.NewFactory()
	_, b, _, _ := enrolledDoor(t, f)
	createOverride(t, b, "1", lockedToday)
	holdingOnly(t, f, b, `["manage-firmware"]`)

	overview := doorPage(t, b, "/admin/doors/1")
	access := pageOf(b.Get("/admin/doors/1?tab=access"))
	page := pageOf(b.Get("/admin/doors/1?tab=controller"))
	saved := b.Post("/admin/doors/1/firmware", "firmware_id", "")

	want := []any{
		[]string{"Dashboard", "Doors: Firmware", "Portal", "API tokens", "Overview", "Access", "Controller (current)"},
		[]string{"Opening the door needs the manage-doors or door-commander role."},
		[]string{"Overrides are made and ended by holders of the manage-doors or door-commander role."},
		[]string{"Locked Upcoming", "Mon 5 Jan 2026 10:00 – Mon 5 Jan 2026 12:00", "Floor polishing. by Administrator", ""},
		[]string{"Choose a default version on the firmware page, or one for this door, to update it."},
		[]string{"Running", "Target", "Rollout", "Latest update"},
		[]any{303, "/admin/doors/1?tab=controller"},
	}
	firmware := []string{}
	for _, term := range []string{"Running", "Target", "Rollout", "Latest update"} {
		if _, ok := page.Details[term]; ok {
			firmware = append(firmware, term)
		}
	}
	got := []any{page.Nav, overview.Notes, access.Notes, access.Tables["Overrides"][1], page.Notes, firmware, []any{saved.Status, saved.Location}}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("navigation, overview's notes, access's notes and override, controller's notes and firmware card, saving its firmware (-want +got):\n%s", diff)
	}
}
