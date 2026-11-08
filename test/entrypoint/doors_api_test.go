package entrypoint_test

import (
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/dakriy/doorcode/internal/testsupport"
)

// createDoor creates a door through the API as c's user. Doors are
// numbered from 1.
func createDoor(t *testing.T, c *testsupport.Browser, body string) {
	t.Helper()
	send(t, c, "POST", "/api/v1/doors", body, 201)
}

// createZone creates a zone through the API as c's user. Zones are
// numbered from 1.
func createZone(t *testing.T, c *testsupport.Browser, body string) {
	t.Helper()
	send(t, c, "POST", "/api/v1/zones", body, 201)
}

// frontDoor is a door body with nothing but a name.
const frontDoor = `{"name":"Front door"}`

// frontDoorJSON is the front door as the API shows it once created.
const frontDoorJSON = `{"id":1,"name":"Front door","location":"","notes":"","public":false,"open_schedules":[],"zones":[],` +
	`"created_at":"2026-01-05T09:00:00Z"}`

// doorSite is a browser signed in as the administrator of a site with the
// schedules Weekdays (2) and Nights (3) and the zones North (1) and
// Annex (2).
func doorSite(t *testing.T, f *testsupport.Factory) *testsupport.Browser {
	t.Helper()
	b := signedInAdmin(t, f)
	createSchedule(t, b, `{"name":"Weekdays","windows":[{"days":["mon","tue","wed","thu","fri"],"start":"08:00","end":"18:00"}]}`)
	createSchedule(t, b, `{"name":"Nights","windows":[{"days":["mon"],"start":"22:00","end":"06:00"}]}`)
	createZone(t, b, `{"name":"North"}`)
	createZone(t, b, `{"name":"Annex"}`)
	return b
}

func TestCreatingADoorAnswersWithItTidied(t *testing.T) {
	t.Parallel()
	b := doorSite(t, testsupport.NewFactory())

	got := b.PostJSON("/api/v1/doors", `{"name":" North entrance ","location":" Building A ","notes":" Strike replaced. ","public":true,`+
		`"open_schedule_ids":[3,2,3],"zone_ids":[1,2,1]}`)

	want := testsupport.Response{Status: 201, ContentType: jsonType, Body: `{"id":1,"name":"North entrance","location":"Building A",` +
		`"notes":"Strike replaced.","public":true,"open_schedules":[{"id":3,"name":"Nights"},{"id":2,"name":"Weekdays"}],` +
		`"zones":[{"id":2,"name":"Annex"},{"id":1,"name":"North"}],"created_at":"2026-01-05T09:00:00Z"}` + "\n"}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("response (-want +got):\n%s", diff)
	}
}

func TestDoorOpenModeSchedulesRoundTrip(t *testing.T) {
	t.Parallel()
	b := doorSite(t, testsupport.NewFactory())
	createDoor(t, b, `{"name":"Lobby","open_schedule_ids":[2]}`)

	updated := b.PutJSON("/api/v1/doors/1", `{"name":"Lobby","open_schedule_ids":[1,3]}`)
	got := b.Get("/api/v1/doors/1")

	want := `[{"id":1,"name":"Always"},{"id":3,"name":"Nights"}]`
	if diff := cmp.Diff([]any{200, testsupport.JSON(want), testsupport.JSON(want)},
		[]any{updated.Status, testsupport.JSONAt(updated.Body, "/open_schedules"), testsupport.JSONAt(got.Body, "/open_schedules")}); diff != "" {
		t.Errorf("status, update, read back (-want +got):\n%s", diff)
	}
}

func TestUpdatingADoorReplacesEverythingItDescribes(t *testing.T) {
	t.Parallel()
	b := doorSite(t, testsupport.NewFactory())
	createDoor(t, b, `{"name":"Lobby","location":"Main","notes":"Old","public":true,"open_schedule_ids":[2],"zone_ids":[1]}`)

	got := b.PutJSON("/api/v1/doors/1", `{"name":"lobby"}`)

	want := testsupport.Response{Status: 200, ContentType: jsonType, Body: `{"id":1,"name":"lobby","location":"","notes":"","public":false,` +
		`"open_schedules":[],"zones":[],"created_at":"2026-01-05T09:00:00Z"}` + "\n"}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("response (-want +got):\n%s", diff)
	}
}

func TestDoorFieldsAreChecked(t *testing.T) {
	t.Parallel()
	cases := map[string]struct {
		body string
		want string
	}{
		"not json":            {`{"name":1}`, `body must be a JSON object with a string \"name\"`},
		"no name":             {`{"name":" "}`, "Enter a name."},
		"a long name":         {`{"name":"` + repeat("x", 101) + `"}`, "Use at most 100 characters."},
		"a taken name":        {`{"name":"FRONT DOOR"}`, "Another door has this name."},
		"a long location":     {`{"name":"A","location":"` + repeat("x", 201) + `"}`, "Use at most 200 characters."},
		"long notes":          {`{"name":"A","notes":"` + repeat("x", 2001) + `"}`, "Use at most 2000 characters."},
		"an unknown schedule": {`{"name":"A","open_schedule_ids":[2,4]}`, "No schedule has id 4."},
		"an unknown zone":     {`{"name":"A","zone_ids":[0]}`, "No zone has id 0."},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			b := doorSite(t, testsupport.NewFactory())
			createDoor(t, b, frontDoor)

			got := b.PostJSON("/api/v1/doors", c.body)

			want := testsupport.Response{Status: 400, ContentType: jsonType, Body: `{"error":"` + c.want + `"}` + "\n"}
			if diff := cmp.Diff(want, got); diff != "" {
				t.Errorf("response (-want +got):\n%s", diff)
			}
		})
	}
}

func TestDoorAtTheLimitsIsAccepted(t *testing.T) {
	t.Parallel()
	body := `{"name":"` + repeat("x", 100) + `","location":"` + repeat("y", 200) + `","notes":"` + repeat("z", 2000) + `"}`

	got := signedInAdmin(t, testsupport.NewFactory()).PostJSON("/api/v1/doors", body)

	if diff := cmp.Diff(201, got.Status); diff != "" {
		t.Errorf("status (-want +got):\n%s\n%s", diff, got.Body)
	}
}

func TestDoorKeepsItsOwnNameButNotADeletedDoorsName(t *testing.T) {
	t.Parallel()
	b := signedInAdmin(t, testsupport.NewFactory())
	createDoor(t, b, frontDoor)
	createDoor(t, b, `{"name":"Old door"}`)
	send(t, b, "DELETE", "/api/v1/doors/2", "", 204)

	own := b.PutJSON("/api/v1/doors/1", `{"name":"front door"}`)
	taken := b.PostJSON("/api/v1/doors", `{"name":"old door"}`)

	want := []any{200, 400, `{"error":"A deleted door has this name. Restore it, or choose another name."}` + "\n"}
	if diff := cmp.Diff(want, []any{own.Status, taken.Status, taken.Body}); diff != "" {
		t.Errorf("own name, deleted door's name (-want +got):\n%s", diff)
	}
}

func TestChangingADoorThatIsNotThereIsRefused(t *testing.T) {
	t.Parallel()
	cases := map[string]func(b *testsupport.Browser) testsupport.Response{
		"read":    func(b *testsupport.Browser) testsupport.Response { return b.Get("/api/v1/doors/9") },
		"update":  func(b *testsupport.Browser) testsupport.Response { return b.PutJSON("/api/v1/doors/9", `{"name":""}`) },
		"delete":  func(b *testsupport.Browser) testsupport.Response { return b.Delete("/api/v1/doors/9") },
		"restore": func(b *testsupport.Browser) testsupport.Response { return b.PostJSON("/api/v1/doors/9/restore", "") },
	}
	for name, request := range cases {
		t.Run(name, func(t *testing.T) {
			got := request(signedInAdmin(t, testsupport.NewFactory()))

			want := testsupport.Response{Status: 404, ContentType: jsonType, Body: `{"error":"no door has this id"}` + "\n"}
			if diff := cmp.Diff(want, got); diff != "" {
				t.Errorf("response (-want +got):\n%s", diff)
			}
		})
	}
}

func TestUpdatingADoorWithABadBodyIsRefused(t *testing.T) {
	t.Parallel()
	b := signedInAdmin(t, testsupport.NewFactory())
	createDoor(t, b, frontDoor)

	got := b.PutJSON("/api/v1/doors/1", `[]`)

	want := testsupport.Response{Status: 400, ContentType: jsonType, Body: `{"error":"body must be a JSON object with a string \"name\""}` + "\n"}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("response (-want +got):\n%s", diff)
	}
}

func TestDeletedDoorKeepsItsDeletionTimeUntilRestored(t *testing.T) {
	t.Parallel()
	f := testsupport.NewFactory()
	b := signedInAdmin(t, f)
	createDoor(t, b, frontDoor)

	send(t, b, "DELETE", "/api/v1/doors/1", "", 204)
	f.ClockStub.Set("2026-01-05T10:00:00Z")
	send(t, b, "DELETE", "/api/v1/doors/1", "", 204)
	deleted := testsupport.JSONAt(send(t, b, "GET", "/api/v1/doors/1", "", 200).Body, "/deleted_at")
	send(t, b, "POST", "/api/v1/doors/1/restore", "", 204)
	restored := send(t, b, "GET", "/api/v1/doors/1", "", 200).Body

	if diff := cmp.Diff([]any{"2026-01-05T09:00:00Z", frontDoorJSON + "\n"}, []any{deleted, restored}); diff != "" {
		t.Errorf("deleted at, restored door (-want +got):\n%s", diff)
	}
}

// doorNames is the names of the doors the door list answers query with,
// and the total it reports.
func doorNames(t *testing.T, b *testsupport.Browser, query string) []any {
	t.Helper()
	body := send(t, b, "GET", "/api/v1/doors"+query, "", 200).Body
	var names []any
	for _, d := range testsupport.JSONAt(body, "/doors").([]any) {
		names = append(names, d.(map[string]any)["name"])
	}
	return []any{names, testsupport.JSONAt(body, "/total")}
}

// fiveDoors is a browser signed in as the administrator of a site with
// five doors: Annex (1), Back door (2, deleted), Cellar (3, in zone 1),
// Dock (4, located in the North wing), and East gate (5, in zone 1).
func fiveDoors(t *testing.T) *testsupport.Browser {
	t.Helper()
	b := signedInAdmin(t, testsupport.NewFactory())
	createZone(t, b, `{"name":"Basement"}`)
	createDoor(t, b, `{"name":"Annex"}`)
	createDoor(t, b, `{"name":"Back door"}`)
	createDoor(t, b, `{"name":"Cellar","zone_ids":[1]}`)
	createDoor(t, b, `{"name":"Dock","location":"North wing"}`)
	createDoor(t, b, `{"name":"East gate","zone_ids":[1]}`)
	send(t, b, "DELETE", "/api/v1/doors/2", "", 204)
	return b
}

func TestDoorListIsSearchedFilteredAndPaged(t *testing.T) {
	t.Parallel()
	cases := map[string]struct {
		query string
		want  []any
	}{
		"every current door":   {"", []any{[]any{"Annex", "Cellar", "Dock", "East gate"}, 4.0}},
		"deleted doors":        {"?show=deleted", []any{[]any{"Back door"}, 1.0}},
		"current doors":        {"?show=current", []any{[]any{"Annex", "Cellar", "Dock", "East gate"}, 4.0}},
		"a search by name":     {"?q=+ANN+", []any{[]any{"Annex"}, 1.0}},
		"a search by location": {"?q=north", []any{[]any{"Dock"}, 1.0}},
		"a zone":               {"?zone=1", []any{[]any{"Cellar", "East gate"}, 2.0}},
		"a zone and a search":  {"?zone=1&q=east", []any{[]any{"East gate"}, 1.0}},
		"a first page":         {"?per_page=3", []any{[]any{"Annex", "Cellar", "Dock"}, 4.0}},
		"a second page":        {"?per_page=3&page=2", []any{[]any{"East gate"}, 4.0}},
		"a page past the last": {"?per_page=3&page=3", []any{[]any(nil), 4.0}},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			got := doorNames(t, fiveDoors(t), c.query)

			if diff := cmp.Diff(c.want, got); diff != "" {
				t.Errorf("names, total (-want +got):\n%s", diff)
			}
		})
	}
}

func TestDoorListSaysWhichPageItIs(t *testing.T) {
	t.Parallel()
	got := testsupport.JSON(send(t, fiveDoors(t), "GET", "/api/v1/doors?per_page=2&page=2", "", 200).Body)

	want := testsupport.JSON(`{"doors":[` +
		`{"id":4,"name":"Dock","location":"North wing","notes":"","public":false,"open_schedules":[],"zones":[],"created_at":"2026-01-05T09:00:00Z"},` +
		`{"id":5,"name":"East gate","location":"","notes":"","public":false,"open_schedules":[],"zones":[{"id":1,"name":"Basement"}],"created_at":"2026-01-05T09:00:00Z"}],` +
		`"total":4,"page":2,"per_page":2}`)
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("list (-want +got):\n%s", diff)
	}
}

func TestDoorListQueryIsChecked(t *testing.T) {
	t.Parallel()
	for _, query := range []string{"page=0", "per_page=0", "per_page=201", "zone=0", "zone=x", "show=all"} {
		t.Run(query, func(t *testing.T) {
			got := signedInAdmin(t, testsupport.NewFactory()).Get("/api/v1/doors?" + query)

			want := testsupport.Response{Status: 400, ContentType: jsonType,
				Body: `{"error":"page must be a whole number from 1, per_page one from 1 to 200, zone a zone's id, and show current or deleted"}` + "\n"}
			if diff := cmp.Diff(want, got); diff != "" {
				t.Errorf("response (-want +got):\n%s", diff)
			}
		})
	}
}

func TestDoorListAtTheLimitsIsAnswered(t *testing.T) {
	t.Parallel()
	got := signedInAdmin(t, testsupport.NewFactory()).Get("/api/v1/doors?per_page=200&page=1000000&zone=1")

	if diff := cmp.Diff(200, got.Status); diff != "" {
		t.Errorf("status (-want +got):\n%s", diff)
	}
}

func TestDoorsAnswerWithARequestIDWhenTheStoreFails(t *testing.T) {
	t.Parallel()
	cases := map[string]struct {
		fail   func(s *testsupport.Store)
		method string
		target string
		body   string
	}{
		"listing doors":                {(*testsupport.Store).FailDoors, "GET", "/api/v1/doors", ""},
		"naming a listed door's zones": {(*testsupport.Store).FailZones, "GET", "/api/v1/doors", ""},
		"naming a door's schedules":    {(*testsupport.Store).FailSchedules, "GET", "/api/v1/doors/1", ""},
		"checking a new door's name":   {(*testsupport.Store).FailDoors, "POST", "/api/v1/doors", `{"name":"Lobby"}`},
		"creating a door":              {(*testsupport.Store).FailDoorWrites, "POST", "/api/v1/doors", `{"name":"Lobby"}`},
		"reading a door to update":     {(*testsupport.Store).FailDoors, "PUT", "/api/v1/doors/1", `{"name":"Lobby"}`},
		"checking a door's zones":      {(*testsupport.Store).FailZones, "PUT", "/api/v1/doors/1", `{"name":"Lobby"}`},
		"updating a door":              {(*testsupport.Store).FailDoorWrites, "PUT", "/api/v1/doors/1", `{"name":"Lobby"}`},
		"deleting a door":              {(*testsupport.Store).FailDoorWrites, "DELETE", "/api/v1/doors/1", ""},
		"restoring a door":             {(*testsupport.Store).FailDoorWrites, "POST", "/api/v1/doors/1/restore", ""},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			f := testsupport.NewFactory()
			b := signedInAdmin(t, f)
			createDoor(t, b, frontDoor)
			c.fail(f.StoreStub)

			got := b.Send(c.method, c.target, c.body)

			want := testsupport.Response{Status: 500, ContentType: jsonType, Body: `{"error":"internal error","request_id":"4243444546474849"}` + "\n"}
			if diff := cmp.Diff(want, got); diff != "" {
				t.Errorf("response (-want +got):\n%s", diff)
			}
		})
	}
}
