package entrypoint_test

import (
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/dakriy/doorcode/internal/testsupport"
)

// zonesNav is the navigation of a zones page for an administrator.
var zonesNav = []string{"Home", "Users", "Groups", "Schedules", "Doors", "Zones (current)", "Settings", "API tokens", "API docs"}

// zonesHead is the header row of the zone list.
var zonesHead = []string{"Name", "Description", "Doors"}

func TestZonesPageListsZonesWithTheirDoors(t *testing.T) {
	t.Parallel()
	b := zonedDoors(t, testsupport.NewFactory())
	createZone(t, b, `{"name":"West","description":"West wing.","door_ids":[1,2]}`)
	createZone(t, b, `{"name":"Attic"}`)

	got := doorPage(t, b, "/admin/zones")

	want := testsupport.Page{
		Title: "Zones · doorcode", Heading: "Zones", Nav: zonesNav,
		Tables: map[string][][]string{"Zones": {zonesHead, {"Attic", "", "None"}, {"West", "West wing.", "Back door, Front door"}}},
	}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("page (-want +got):\n%s", diff)
	}
}

func TestZonesPageSaysWhenThereAreNone(t *testing.T) {
	t.Parallel()
	got := doorPage(t, signedInAdmin(t, testsupport.NewFactory()), "/admin/zones").Tables["Zones"]

	want := [][]string{zonesHead, {"There are no zones yet. Create one to group doors for access rules."}}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("table (-want +got):\n%s", diff)
	}
}

func TestNewZonePageOffersEveryDoorThatIsNotDeleted(t *testing.T) {
	t.Parallel()
	b := zonedDoors(t, testsupport.NewFactory())
	send(t, b, "PUT", "/api/v1/doors/1", `{"name":"Front door","location":"Street side"}`, 200)
	createDoor(t, b, `{"name":"Old door"}`)
	send(t, b, "DELETE", "/api/v1/doors/3", "", 204)
	b.Get("/")

	got := b.Get("/admin/zones/new")

	page := pageOf(got)
	want := []any{"New zone", map[string]string{"name": "", "description": ""}, map[string]string{"door_ids=2": "Back door", "door_ids=1": "Front door Street side"}}
	if diff := cmp.Diff(want, []any{page.Heading, page.Values, testsupport.ParsePage(got.Body).Choices}); diff != "" {
		t.Errorf("heading, values, choices (-want +got):\n%s", diff)
	}
}

func TestNewZonePageSaysWhenThereAreNoDoors(t *testing.T) {
	t.Parallel()
	got := pageOf(signedInAdmin(t, testsupport.NewFactory()).Get("/admin/zones/new")).Notes

	if diff := cmp.Diff([]string{"There are no doors yet. Create them on the doors page."}, got); diff != "" {
		t.Errorf("notes (-want +got):\n%s", diff)
	}
}

func TestCreatingAZoneFromTheFormOpensItsPage(t *testing.T) {
	t.Parallel()
	b := zonedDoors(t, testsupport.NewFactory())
	b.Get("/")

	created := b.Post("/admin/zones", "name", "Ground floor", "description", "Lobby level.", "door_ids", "2")
	got := pageOf(b.Get(created.Location))

	want := []any{"/admin/zones/1", testsupport.Page{
		Title: "Ground floor · doorcode", Heading: "Ground floor", Nav: zonesNav,
		Messages: []string{"The zone has been created."},
		Values:   map[string]string{"name": "Ground floor", "description": "Lobby level."},
		Checked:  []string{"door_ids=2"},
	}}
	if diff := cmp.Diff(want, []any{created.Location, got}); diff != "" {
		t.Errorf("redirect, page (-want +got):\n%s", diff)
	}
}

func TestZoneFormSaysWhatIsWrongAndKeepsWhatWasEntered(t *testing.T) {
	t.Parallel()
	b := zonedDoors(t, testsupport.NewFactory())
	createZone(t, b, `{"name":"West"}`)

	got := b.Post("/admin/zones", "name", "west", "description", repeat("x", 501), "door_ids", "1", "door_ids", "7")

	page := pageOf(got)
	want := []any{422, "New zone",
		map[string]string{"name": "Another zone has this name.", "description": "Use at most 500 characters.", "door_ids": "No door has id 7."},
		map[string]string{"name": "west", "description": repeat("x", 501)},
		[]string{"door_ids=1"}}
	if diff := cmp.Diff(want, []any{got.Status, page.Heading, page.FieldErrors, page.Values, page.Checked}); diff != "" {
		t.Errorf("status, heading, field errors, values, checked (-want +got):\n%s", diff)
	}
}

func TestZonePageOffersTheDeletedDoorsItHolds(t *testing.T) {
	t.Parallel()
	b := zonedDoors(t, testsupport.NewFactory())
	createZone(t, b, `{"name":"West","door_ids":[1]}`)
	send(t, b, "DELETE", "/api/v1/doors/1", "", 204)

	got := testsupport.ParsePage(b.Get("/admin/zones/1").Body)

	want := []any{"West", map[string]string{"door_ids=2": "Back door", "door_ids=1": "Front door Deleted"}, []string{"door_ids=1"}}
	if diff := cmp.Diff(want, []any{got.Heading, got.Choices, got.Checked}); diff != "" {
		t.Errorf("heading, choices, checked (-want +got):\n%s", diff)
	}
}

func TestSavingAZoneFromItsPage(t *testing.T) {
	t.Parallel()
	b := zonedDoors(t, testsupport.NewFactory())
	createZone(t, b, `{"name":"West","door_ids":[1]}`)
	b.Get("/")

	saved := b.Post("/admin/zones/1", "name", "West wing", "description", "", "door_ids", "2")
	got := pageOf(b.Get(saved.Location))

	want := []any{"/admin/zones/1", []string{"The zone's changes have been saved."}, "West wing", []string{"door_ids=2"}}
	if diff := cmp.Diff(want, []any{saved.Location, got.Messages, got.Heading, got.Checked}); diff != "" {
		t.Errorf("redirect, messages, heading, checked (-want +got):\n%s", diff)
	}
}

func TestSavingAZoneFromItsPageSaysWhatIsWrong(t *testing.T) {
	t.Parallel()
	b := signedInAdmin(t, testsupport.NewFactory())
	createZone(t, b, `{"name":"West"}`)

	got := b.Post("/admin/zones/1", "name", " ")

	page := pageOf(got)
	if diff := cmp.Diff([]any{422, "West", map[string]string{"name": "Enter a name."}}, []any{got.Status, page.Heading, page.FieldErrors}); diff != "" {
		t.Errorf("status, heading, field errors (-want +got):\n%s", diff)
	}
}

func TestDeletingAZoneFromItsPage(t *testing.T) {
	t.Parallel()
	b := signedInAdmin(t, testsupport.NewFactory())
	createZone(t, b, `{"name":"West"}`)
	b.Get("/")

	deleted := b.Post("/admin/zones/1/delete")
	got := pageOf(b.Get(deleted.Location))

	want := []any{"/admin/zones", []string{"The zone has been deleted. Its doors stay."},
		[][]string{zonesHead, {"There are no zones yet. Create one to group doors for access rules."}}}
	if diff := cmp.Diff(want, []any{deleted.Location, got.Messages, got.Tables["Zones"]}); diff != "" {
		t.Errorf("redirect, messages, list (-want +got):\n%s", diff)
	}
}

func TestZonePageThatDoesNotExistSaysSo(t *testing.T) {
	t.Parallel()
	cases := map[string]func(b *testsupport.Browser) testsupport.Response{
		"an unknown id": func(b *testsupport.Browser) testsupport.Response { return b.Get("/admin/zones/9") },
		"saving it":     func(b *testsupport.Browser) testsupport.Response { return b.Post("/admin/zones/9", "name", "X") },
		"deleting it":   func(b *testsupport.Browser) testsupport.Response { return b.Post("/admin/zones/9/delete") },
	}
	for name, request := range cases {
		t.Run(name, func(t *testing.T) {
			b := signedInAdmin(t, testsupport.NewFactory())
			b.Get("/")

			got := request(b)

			want := testsupport.Page{Title: "Not found · doorcode", Heading: "Not found", Nav: zonesNav}
			if diff := cmp.Diff([]any{404, want}, []any{got.Status, pageOf(got)}); diff != "" {
				t.Errorf("status and page (-want +got):\n%s", diff)
			}
		})
	}
}

func TestZonePageStoreFailureShowsARequestID(t *testing.T) {
	t.Parallel()
	cases := map[string]struct {
		fail    func(s *testsupport.Store)
		request func(b *testsupport.Browser) testsupport.Response
	}{
		"list":         {(*testsupport.Store).FailZones, func(b *testsupport.Browser) testsupport.Response { return b.Get("/admin/zones") }},
		"list's doors": {(*testsupport.Store).FailDoors, func(b *testsupport.Browser) testsupport.Response { return b.Get("/admin/zones") }},
		"new":          {(*testsupport.Store).FailDoors, func(b *testsupport.Browser) testsupport.Response { return b.Get("/admin/zones/new") }},
		"one":          {(*testsupport.Store).FailZones, func(b *testsupport.Browser) testsupport.Response { return b.Get("/admin/zones/1") }},
		"create": {(*testsupport.Store).FailZoneWrites, func(b *testsupport.Browser) testsupport.Response {
			return b.Post("/admin/zones", "name", "East")
		}},
		"save": {(*testsupport.Store).FailZoneWrites, func(b *testsupport.Browser) testsupport.Response {
			return b.Post("/admin/zones/1", "name", "East")
		}},
		"delete": {(*testsupport.Store).FailZoneWrites, func(b *testsupport.Browser) testsupport.Response { return b.Post("/admin/zones/1/delete") }},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			f := testsupport.NewFactory()
			b := signedInAdmin(t, f)
			createZone(t, b, `{"name":"West"}`)
			c.fail(f.StoreStub)

			got := c.request(b)

			if diff := cmp.Diff([]any{500, "Something went wrong"}, []any{got.Status, pageOf(got).Heading}); diff != "" {
				t.Errorf("status, heading (-want +got):\n%s", diff)
			}
		})
	}
}
