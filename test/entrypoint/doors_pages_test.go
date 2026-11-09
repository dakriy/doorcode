package entrypoint_test

import (
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/dakriy/doorcode/internal/testsupport"
)

// doorsNav is the navigation of a doors page for an administrator.
var doorsNav = []string{"Home", "Dashboard", "Users", "Requests", "Groups", "Schedules", "Doors (current)", "Zones", "Firmware", "Events", "Settings", "API tokens", "API docs"}

// doorsHead is the header row of the door list.
var doorsHead = []string{"Picture", "Name", "Status", "Zones", "Open mode", "Portal"}

// overridesHead and entrantsHead are the header rows of a door's overrides
// and of who can enter it.
var (
	overridesHead = []string{"Override", "When", "Reason", "Actions"}
	entrantsHead  = []string{"Group", "Schedule", "Through", "Rule"}
)

// freshDoorTables are the tables of a door's page without overrides or
// access rules.
var freshDoorTables = map[string][][]string{
	"Overrides":     {overridesHead, {"No overrides are in force or to come. The door follows its open mode and access rules."}},
	"Who can enter": {entrantsHead, {"No access rule reaches this door, so only open mode, overrides, and door commanders open it."}},
	"Recent events": noDoorEvents,
}

// doorEventsHead heads the table of a door's recent events, and
// noDoorEvents is the table before anything happened there.
var (
	doorEventsHead = []string{"When", "Who", "Outcome", "Details"}
	noDoorEvents   = [][]string{doorEventsHead, {"Nothing has happened here yet."}}
)

// picturesNote is what a door's page says of where its primary picture is
// shown while the portal shows no door pictures.
const picturesNote = "The primary picture is shown in the door list and at the top of this page. " +
	"People see it in the portal only while the setting Door pictures in the portal is on; it is off."

// openNote is what a door's page says while it cannot be opened.
const openNote = "The door can be opened from here once its controller is connected."

// neverOpenNote is what a door's page says of the open mode of a door
// whose zones have no open-mode schedules.
const neverOpenNote = "Never in open mode: none of its zones has an open-mode schedule. Open-mode schedules are set on each zone's page."

// doorNote is what a door's page says about what enrollment adds, why it
// cannot be opened yet, and that it is never in open mode.
var doorNote = []string{
	"No controller has enrolled as this door yet. Its hardware, credentials, configuration, and live state appear here once one does.",
	picturesNote, openNote, neverOpenNote,
}

// doorPage is the page at target, after the flash of signing in is
// shown elsewhere.
func doorPage(t *testing.T, b *testsupport.Browser, target string) testsupport.Page {
	t.Helper()
	b.Get("/")
	return pageOf(b.Get(target))
}

func TestDoorsPageListsDoorsWithTheirPrimaryPicture(t *testing.T) {
	t.Parallel()
	b := doorSite(t, testsupport.NewFactory())
	createDoor(t, b, `{"name":"Lobby","location":"Building A","public":true,"zone_ids":[1,2]}`)
	createDoor(t, b, `{"name":"Dock"}`)
	send(t, b, "PUT", "/api/v1/zones/1", `{"name":"North","door_ids":[1],"open_schedule_ids":[3]}`, 200)
	send(t, b, "PUT", "/api/v1/zones/2", `{"name":"Annex","door_ids":[1],"open_schedule_ids":[2,3]}`, 200)
	uploadPicture(t, b, "1", testsupport.PNG(8, 8))

	got := doorPage(t, b, "/admin/doors")

	want := testsupport.Page{
		Title: "Doors · doorcode", Heading: "Doors", Nav: doorsNav,
		Values: map[string]string{"q": ""},
		Tables: map[string][][]string{"Doors": {
			doorsHead,
			{"", "Dock", "Not enrolled", "None", "Never", "Hidden"},
			{"", "Lobby Building A", "Not enrolled", "Annex, North", "Nights, Weekdays", "Public"},
			{"Showing 1 to 2 of 2"},
		}},
		Images: []testsupport.Image{{Src: "/api/v1/doors/1/pictures/1/thumbnail", Alt: "Lobby"}},
	}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("page (-want +got):\n%s", diff)
	}
}

func TestDoorsPageSaysWhyItIsEmpty(t *testing.T) {
	t.Parallel()
	cases := map[string]struct {
		query string
		want  string
	}{
		"no doors":      {"", "No doors yet. Create one for each controller you install; it enrolls once it is connected."},
		"no deleted":    {"?show=deleted", "No doors are deleted."},
		"a search":      {"?q=zzz", "No doors match this search."},
		"a zone":        {"?zone=1", "No doors match this search."},
		"past the last": {"?page=2", "This page is past the last one."},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			b := signedInAdmin(t, testsupport.NewFactory())
			if c.query == "?page=2" {
				createDoor(t, b, frontDoor)
			}

			got := doorPage(t, b, "/admin/doors"+c.query).Tables["Doors"]

			if diff := cmp.Diff([][]string{doorsHead, {c.want}}, got); diff != "" {
				t.Errorf("table (-want +got):\n%s", diff)
			}
		})
	}
}

// manyDoors is a browser signed in as the administrator after creating 30
// doors, named Door 01 to Door 30, all in the zone Even (1), and the
// empty zone Attic (2).
func manyDoors(t *testing.T) *testsupport.Browser {
	t.Helper()
	b := signedInAdmin(t, testsupport.NewFactory())
	createZone(t, b, `{"name":"Even"}`)
	createZone(t, b, `{"name":"Attic"}`)
	for i := 1; i <= 30; i++ {
		createDoor(t, b, `{"name":"Door `+itoa(i/10)+itoa(i%10)+`","zone_ids":[1]}`)
	}
	return b
}

// doorPagerOf is what a page of the door list shows of its place in the
// list: how many doors it lists, the first and last name, what its footer
// says, and the page links and their targets.
func doorPagerOf(r testsupport.Response) []any {
	p := testsupport.ParsePage(r.Body)
	rows := p.Tables["Doors"]
	last := len(rows) - 1
	return []any{last - 1, rows[1][1], rows[last-1][1], rows[last][0], p.Nav[len(doorsNav):], p.NavTargets[len(doorsNav):]}
}

func TestDoorsPageIsPagedKeepingItsFilters(t *testing.T) {
	t.Parallel()
	cases := map[string]struct {
		query string
		want  []any
	}{
		"first page": {"", []any{25, "Door 01", "Door 25", "Showing 1 to 25 of 30", []string{"Next"}, []string{"/admin/doors?page=2"}}},
		"second page": {"?page=2",
			[]any{5, "Door 26", "Door 30", "Showing 26 to 30 of 30", []string{"Previous"}, []string{"/admin/doors?page=1"}}},
		"unreadable page": {"?page=x&zone=x",
			[]any{25, "Door 01", "Door 25", "Showing 1 to 25 of 30", []string{"Next"}, []string{"/admin/doors?page=2"}}},
		"a search in a zone": {"?q=door+0&zone=1",
			[]any{9, "Door 01", "Door 09", "Showing 1 to 9 of 9", []string{}, []string{}}},
		"a zone on a later page": {"?zone=1&page=2",
			[]any{5, "Door 26", "Door 30", "Showing 26 to 30 of 30", []string{"Previous"}, []string{"/admin/doors?page=1&zone=1"}}},
		"a search on a later page": {"?q=door&page=2&show=current",
			[]any{5, "Door 26", "Door 30", "Showing 26 to 30 of 30", []string{"Previous"}, []string{"/admin/doors?page=1&q=door"}}},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			got := doorPagerOf(manyDoors(t).Get("/admin/doors" + c.query))

			if diff := cmp.Diff(c.want, got); diff != "" {
				t.Errorf("pager (-want +got):\n%s", diff)
			}
		})
	}
}

func TestDoorsPageLinksKeepTheDeletedFilter(t *testing.T) {
	t.Parallel()
	b := manyDoors(t)
	for i := 1; i <= 30; i++ {
		send(t, b, "DELETE", "/api/v1/doors/"+itoa(i), "", 204)
	}

	got := doorPagerOf(b.Get("/admin/doors?show=deleted"))

	want := []any{25, "Door 01", "Door 25", "Showing 1 to 25 of 30", []string{"Next"}, []string{"/admin/doors?page=2&show=deleted"}}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("pager (-want +got):\n%s", diff)
	}
}

func TestDoorsPageFiltersOfferEveryZoneAndBothLists(t *testing.T) {
	t.Parallel()
	got := testsupport.ParsePage(manyDoors(t).Get("/admin/doors?zone=1&show=deleted").Body).Options

	want := map[string][]string{
		"zone": {"Every zone", "Attic", "Even (selected)"},
		"show": {"Current doors", "Deleted doors (selected)"},
	}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("filters (-want +got):\n%s", diff)
	}
}

func TestDoorsPageFiltersStartAtEverything(t *testing.T) {
	t.Parallel()
	got := testsupport.ParsePage(signedInAdmin(t, testsupport.NewFactory()).Get("/admin/doors").Body).Options

	want := map[string][]string{"zone": {"Every zone (selected)"}, "show": {"Current doors (selected)", "Deleted doors"}}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("filters (-want +got):\n%s", diff)
	}
}

func TestNewDoorPageOffersAnEmptyForm(t *testing.T) {
	t.Parallel()
	got := doorPage(t, signedInAdmin(t, testsupport.NewFactory()), "/admin/doors/new")

	want := testsupport.Page{
		Title: "New door · doorcode", Heading: "New door", Nav: doorsNav,
		Values: map[string]string{"name": "", "location": "", "notes": ""},
	}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("page (-want +got):\n%s", diff)
	}
}

func TestCreatingADoorFromTheFormOpensItsPage(t *testing.T) {
	t.Parallel()
	b := doorSite(t, testsupport.NewFactory())
	b.Get("/")

	created := b.Post("/admin/doors", "name", "Lobby", "location", "Building A", "notes", "Side entrance.", "public", "on")
	got := pageOf(b.Get(created.Location))

	if diff := cmp.Diff([]any{303, "/admin/doors/1"}, []any{created.Status, created.Location}); diff != "" {
		t.Errorf("redirect (-want +got):\n%s", diff)
	}
	want := testsupport.Page{
		Title: "Lobby · doorcode", Heading: "Lobby", Nav: doorsNav,
		Messages: []string{"The door has been created. Its controller can enroll once it is connected."},
		Notes:    doorNote,
		Values:   map[string]string{"name": "Lobby", "location": "Building A", "notes": "Side entrance.", "picture": ""},
		Checked:  []string{"public=on"},
		Tables:   freshDoorTables,
	}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("page (-want +got):\n%s", diff)
	}
}

func TestDoorFormSaysWhatIsWrongAndKeepsWhatWasEntered(t *testing.T) {
	t.Parallel()
	b := signedInAdmin(t, testsupport.NewFactory())
	createDoor(t, b, frontDoor)

	got := b.Post("/admin/doors", "name", "front door", "location", repeat("x", 201), "notes", "Keep", "public", "on")

	page := pageOf(got)
	want := []any{422,
		map[string]string{"name": "Another door has this name.", "location": "Use at most 200 characters."},
		map[string]string{"name": "front door", "location": repeat("x", 201), "notes": "Keep"},
		[]string{"public=on"},
	}
	if diff := cmp.Diff(want, []any{got.Status, page.FieldErrors, page.Values, page.Checked}); diff != "" {
		t.Errorf("status, field errors, values, checked (-want +got):\n%s", diff)
	}
}

func TestDoorPageShowsTheDoorItsChoicesAndItsOpenModeThroughItsZones(t *testing.T) {
	t.Parallel()
	b := doorSite(t, testsupport.NewFactory())
	createDoor(t, b, `{"name":"Lobby","location":"Building A","notes":"Side.","zone_ids":[2]}`)
	send(t, b, "PUT", "/api/v1/zones/1", `{"name":"North","description":"North wing.","open_schedule_ids":[2]}`, 200)
	send(t, b, "PUT", "/api/v1/zones/2", `{"name":"Annex","door_ids":[1],"open_schedule_ids":[3,1]}`, 200)

	got := doorPage(t, b, "/admin/doors/1")

	want := testsupport.Page{
		Title: "Lobby · doorcode", Heading: "Lobby", Nav: doorsNav,
		Notes: []string{doorNote[0], picturesNote, openNote,
			"In open mode during Always, Nights, through its zones. Open-mode schedules are set on each zone's page."},
		Values:  map[string]string{"name": "Lobby", "location": "Building A", "notes": "Side.", "picture": ""},
		Checked: []string{"zone_ids=2"},
		Tables:  freshDoorTables,
	}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("page (-want +got):\n%s", diff)
	}
}

func TestDoorPageLabelsEachChoice(t *testing.T) {
	t.Parallel()
	b := doorSite(t, testsupport.NewFactory())
	createDoor(t, b, frontDoor)
	send(t, b, "PUT", "/api/v1/zones/1", `{"name":"North","description":"North wing."}`, 200)

	got := testsupport.ParsePage(b.Get("/admin/doors/1").Body).Choices

	want := map[string]string{
		"public=on":  "Show in the user portal People see the door, its picture, and when their code works there.",
		"zone_ids=2": "Annex",
		"zone_ids=1": "North North wing.",
	}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("choices (-want +got):\n%s", diff)
	}
}

func TestDoorPageServesThePrimaryThumbnailAndListsEveryPicture(t *testing.T) {
	t.Parallel()
	b := signedInAdmin(t, testsupport.NewFactory())
	createDoor(t, b, frontDoor)
	first := b.Upload("/admin/doors/1/pictures", "picture", "wide.png", testsupport.PNG(30, 20))
	second := b.Upload("/admin/doors/1/pictures", "picture", "tall.png", testsupport.PNG(20, 30))
	primary := b.Post("/admin/doors/1/pictures/2/primary")

	page := pageOf(b.Get(primary.Location))
	thumbnail := b.Get(page.Images[0].Src)

	want := []any{
		[]any{303, "/admin/doors/1", 303, "/admin/doors/1", 303, "/admin/doors/1"},
		[]string{"The primary picture has been changed."},
		[]testsupport.Image{
			{Src: "/api/v1/doors/1/pictures/2/thumbnail", Alt: "Front door"},
			{Src: "/api/v1/doors/1/pictures/1/thumbnail", Alt: "Picture 1"},
			{Src: "/api/v1/doors/1/pictures/2/thumbnail", Alt: "Picture 2, the primary picture"},
		},
		"jpeg 20x30",
	}
	got := []any{
		[]any{first.Status, first.Location, second.Status, second.Location, primary.Status, primary.Location},
		page.Messages, page.Images, testsupport.ImageInfo(thumbnail.Body),
	}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("redirects, messages, images, thumbnail (-want +got):\n%s", diff)
	}
}

func TestUploadingAPictureFromTheDoorPageSaysSo(t *testing.T) {
	t.Parallel()
	b := signedInAdmin(t, testsupport.NewFactory())
	createDoor(t, b, frontDoor)
	b.Get("/")

	uploaded := b.Upload("/admin/doors/1/pictures", "picture", "door.png", testsupport.PNG(8, 8))
	got := pageOf(b.Get(uploaded.Location))

	want := []any{[]string{"The picture has been added."}, []testsupport.Image{
		{Src: "/api/v1/doors/1/pictures/1/thumbnail", Alt: "Front door"},
		{Src: "/api/v1/doors/1/pictures/1/thumbnail", Alt: "Picture 1, the primary picture"},
	}}
	if diff := cmp.Diff(want, []any{got.Messages, got.Images}); diff != "" {
		t.Errorf("messages, images (-want +got):\n%s", diff)
	}
}

func TestDoorPageRefusesUploadsThatAreNotPictures(t *testing.T) {
	t.Parallel()
	cases := map[string]struct {
		content []byte
		status  int
		want    string
	}{
		"too large":  {testsupport.Padded(testsupport.PNG(4, 4), 10<<20+1), 413, "The picture is larger than 10 MB. Choose a smaller file."},
		"not a file": {[]byte("hello"), 422, "The file is not a JPEG, PNG, GIF, or WebP image that can be read."},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			b := signedInAdmin(t, testsupport.NewFactory())
			createDoor(t, b, frontDoor)
			b.Get("/")

			got := b.Upload("/admin/doors/1/pictures", "picture", "door.png", c.content)

			page := pageOf(got)
			want := []any{c.status, "Front door", map[string]string{"picture": c.want}}
			if diff := cmp.Diff(want, []any{got.Status, page.Heading, page.FieldErrors}); diff != "" {
				t.Errorf("status, heading, field errors (-want +got):\n%s", diff)
			}
		})
	}
}

func TestDeletingAPictureFromTheDoorPage(t *testing.T) {
	t.Parallel()
	b := doorWithPictures(t, testsupport.NewFactory())
	b.Get("/")

	deleted := b.Post("/admin/doors/1/pictures/1/delete")
	got := pageOf(b.Get(deleted.Location))

	want := []any{"/admin/doors/1", []string{"The picture has been deleted."}, []testsupport.Image{
		{Src: "/api/v1/doors/1/pictures/2/thumbnail", Alt: "Front door"},
		{Src: "/api/v1/doors/1/pictures/2/thumbnail", Alt: "Picture 1, the primary picture"},
	}}
	if diff := cmp.Diff(want, []any{deleted.Location, got.Messages, got.Images}); diff != "" {
		t.Errorf("redirect, messages, images (-want +got):\n%s", diff)
	}
}

func TestChangingAPictureThatIsGoneFromTheDoorPageSaysSo(t *testing.T) {
	t.Parallel()
	for _, action := range []string{"primary", "delete"} {
		t.Run(action, func(t *testing.T) {
			b := doorWithPictures(t, testsupport.NewFactory())
			b.Get("/")

			got := b.Post("/admin/doors/1/pictures/9/" + action)

			page := pageOf(got)
			want := []any{404, "Front door", []string{"That picture is not there any more. It may have been deleted already."}}
			if diff := cmp.Diff(want, []any{got.Status, page.Heading, page.Messages}); diff != "" {
				t.Errorf("status, heading, messages (-want +got):\n%s", diff)
			}
		})
	}
}

func TestSavingADoorsDetailsFromItsPageKeepsTheRest(t *testing.T) {
	t.Parallel()
	b := doorSite(t, testsupport.NewFactory())
	createDoor(t, b, `{"name":"Lobby","public":true,"zone_ids":[1]}`)
	b.Get("/")

	saved := b.Post("/admin/doors/1", "name", "Main lobby", "location", "Building B", "notes", "")
	got := pageOf(b.Get(saved.Location))

	want := []any{"/admin/doors/1", []string{"The door's changes have been saved."}, "Main lobby",
		map[string]string{"name": "Main lobby", "location": "Building B", "notes": "", "picture": ""},
		[]string{"zone_ids=1"}}
	if diff := cmp.Diff(want, []any{saved.Location, got.Messages, got.Heading, got.Values, got.Checked}); diff != "" {
		t.Errorf("redirect, messages, heading, values, checked (-want +got):\n%s", diff)
	}
}

func TestSavingADoorsDetailsFromItsPageSaysWhatIsWrong(t *testing.T) {
	t.Parallel()
	b := signedInAdmin(t, testsupport.NewFactory())
	createDoor(t, b, frontDoor)

	got := b.Post("/admin/doors/1", "name", "", "location", "Side", "notes", repeat("x", 2001), "public", "on")

	page := pageOf(got)
	want := []any{422, "Front door",
		map[string]string{"name": "Enter a name.", "notes": "Use at most 2000 characters."},
		map[string]string{"name": "", "location": "Side", "notes": repeat("x", 2001), "picture": ""},
		[]string{"public=on"}}
	if diff := cmp.Diff(want, []any{got.Status, page.Heading, page.FieldErrors, page.Values, page.Checked}); diff != "" {
		t.Errorf("status, heading, field errors, values, checked (-want +got):\n%s", diff)
	}
}

func TestSavingADoorsZonesFromItsPage(t *testing.T) {
	t.Parallel()
	b := doorSite(t, testsupport.NewFactory())
	createDoor(t, b, `{"name":"Lobby","zone_ids":[1]}`)
	send(t, b, "PUT", "/api/v1/zones/2", `{"name":"Annex","open_schedule_ids":[3]}`, 200)
	b.Get("/")

	saved := b.Post("/admin/doors/1/zones", "zone_ids", "2")
	got := pageOf(b.Get(saved.Location))

	want := []any{"/admin/doors/1", []string{"The door's zones have been saved."}, []string{"zone_ids=2"},
		"In open mode during Nights, through its zones. Open-mode schedules are set on each zone's page."}
	if diff := cmp.Diff(want, []any{saved.Location, got.Messages, got.Checked, got.Notes[3]}); diff != "" {
		t.Errorf("redirect, messages, checked, open mode (-want +got):\n%s", diff)
	}
}

func TestSavingUnknownChoicesFromADoorsPageSaysSo(t *testing.T) {
	t.Parallel()
	cases := map[string]struct {
		target string
		fields []string
		want   map[string]string
	}{
		"a zone": {"/admin/doors/1/zones", []string{"zone_ids", "9"}, map[string]string{"zone_ids": "No zone has id 9."}},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			b := doorSite(t, testsupport.NewFactory())
			createDoor(t, b, frontDoor)

			got := b.Post(c.target, c.fields...)

			page := pageOf(got)
			if diff := cmp.Diff([]any{422, c.want}, []any{got.Status, page.FieldErrors}); diff != "" {
				t.Errorf("status, field errors (-want +got):\n%s", diff)
			}
		})
	}
}

func TestDeletingAndRestoringADoorFromItsPage(t *testing.T) {
	t.Parallel()
	b := signedInAdmin(t, testsupport.NewFactory())
	createDoor(t, b, frontDoor)
	b.Get("/")

	deleted := b.Post("/admin/doors/1/delete")
	whileDeleted := pageOf(b.Get(deleted.Location))
	restored := b.Post("/admin/doors/1/restore")
	afterwards := pageOf(b.Get(restored.Location))

	want := []any{"/admin/doors/1", []string{"The door has been deleted. Its history stays, and you can restore it."},
		"/admin/doors/1", []string{"The door has been restored."}, []string{doorNote[0], picturesNote, openNote, neverOpenNote, "There are no zones yet. Create one on the zones page."}}
	if diff := cmp.Diff(want, []any{deleted.Location, whileDeleted.Messages, restored.Location, afterwards.Messages, afterwards.Notes}); diff != "" {
		t.Errorf("delete, restore (-want +got):\n%s", diff)
	}
}

func TestDoorPageThatDoesNotExistSaysSo(t *testing.T) {
	t.Parallel()
	for _, target := range []string{"/admin/doors/9", "/admin/doors/front"} {
		t.Run(target, func(t *testing.T) {
			got := signedInAdmin(t, testsupport.NewFactory())
			got.Get("/")

			response := got.Get(target)

			want := testsupport.Page{Title: "Not found · doorcode", Heading: "Not found", Nav: doorsNav}
			if diff := cmp.Diff([]any{404, want}, []any{response.Status, pageOf(response)}); diff != "" {
				t.Errorf("status and page (-want +got):\n%s", diff)
			}
		})
	}
}

func TestChangingADoorThatDoesNotExistFromAPageSaysSo(t *testing.T) {
	t.Parallel()
	for _, target := range []string{"/admin/doors/9", "/admin/doors/9/delete", "/admin/doors/9/restore", "/admin/doors/9/pictures",
		"/admin/doors/9/pictures/1/primary", "/admin/doors/9/pictures/1/delete", "/admin/doors/9/zones"} {
		t.Run(target, func(t *testing.T) {
			got := signedInAdmin(t, testsupport.NewFactory()).Post(target, "name", "X")

			if diff := cmp.Diff([]any{404, "Not found"}, []any{got.Status, pageOf(got).Heading}); diff != "" {
				t.Errorf("status, heading (-want +got):\n%s", diff)
			}
		})
	}
}

func TestDoorPagesAskForTheRole(t *testing.T) {
	t.Parallel()
	_, b := adminHolding(t, testsupport.NewFactory(), `["manage-schedules"]`)

	doors := b.Get("/admin/doors")
	got := []any{doors.Status, b.Get("/admin/zones").Status, testsupport.ParsePage(doors.Body).Nav}

	want := []any{403, 403, []string{"Home", "Dashboard", "Schedules", "API tokens", "API docs"}}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("doors, zones, navigation (-want +got):\n%s", diff)
	}
}

func TestDoorPageStoreFailureShowsARequestID(t *testing.T) {
	t.Parallel()
	png := testsupport.PNG(4, 4)
	cases := map[string]struct {
		fail    func(s *testsupport.Store)
		request func(b *testsupport.Browser) testsupport.Response
	}{
		"list":            {(*testsupport.Store).FailDoors, func(b *testsupport.Browser) testsupport.Response { return b.Get("/admin/doors") }},
		"list's zones":    {(*testsupport.Store).FailZones, func(b *testsupport.Browser) testsupport.Response { return b.Get("/admin/doors") }},
		"one":             {(*testsupport.Store).FailDoors, func(b *testsupport.Browser) testsupport.Response { return b.Get("/admin/doors/1") }},
		"one's schedules": {(*testsupport.Store).FailSchedules, func(b *testsupport.Browser) testsupport.Response { return b.Get("/admin/doors/1") }},
		"create": {(*testsupport.Store).FailDoorWrites, func(b *testsupport.Browser) testsupport.Response {
			return b.Post("/admin/doors", "name", "Lobby")
		}},
		"save": {(*testsupport.Store).FailDoorWrites, func(b *testsupport.Browser) testsupport.Response {
			return b.Post("/admin/doors/1", "name", "Lobby")
		}},
		"delete":  {(*testsupport.Store).FailDoorWrites, func(b *testsupport.Browser) testsupport.Response { return b.Post("/admin/doors/1/delete") }},
		"restore": {(*testsupport.Store).FailDoorWrites, func(b *testsupport.Browser) testsupport.Response { return b.Post("/admin/doors/1/restore") }},
		"upload": {(*testsupport.Store).FailDoorWrites, func(b *testsupport.Browser) testsupport.Response {
			return b.Upload("/admin/doors/1/pictures", "picture", "door.png", png)
		}},
		"set primary": {(*testsupport.Store).FailDoorWrites, func(b *testsupport.Browser) testsupport.Response {
			return b.Post("/admin/doors/1/pictures/1/primary")
		}},
		"delete a picture": {(*testsupport.Store).FailDoorWrites, func(b *testsupport.Browser) testsupport.Response {
			return b.Post("/admin/doors/1/pictures/1/delete")
		}},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			f := testsupport.NewFactory()
			b := signedInAdmin(t, f)
			createDoor(t, b, frontDoor)
			uploadPicture(t, b, "1", png)
			c.fail(f.StoreStub)

			got := c.request(b)

			if diff := cmp.Diff([]any{500, "Something went wrong"}, []any{got.Status, pageOf(got).Heading}); diff != "" {
				t.Errorf("status, heading (-want +got):\n%s", diff)
			}
		})
	}
}
