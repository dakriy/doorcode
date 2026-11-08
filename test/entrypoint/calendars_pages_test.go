package entrypoint_test

import (
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/dakriy/doorcode/internal/testsupport"
)

// weekLinks are the navigation links after the main navigation of a
// calendar page.
var weekLinks = []string{"Previous week", "This week", "Next week"}

func TestDoorCalendarShowsTheTimelineAndTheAccessWindowsThatReachIt(t *testing.T) {
	t.Parallel()
	b := ruleSite(t, testsupport.NewFactory())
	send(t, b, "PUT", "/api/v1/doors/1", `{"name":"Lobby","open_schedule_ids":[3],"zone_ids":[1]}`, 200)
	createRule(t, b, `{"group_id":2,"schedule_id":2,"zone_ids":[1]}`)
	createRule(t, b, `{"group_id":3,"schedule_id":2,"door_ids":[2]}`)
	createOverride(t, b, "1", lockedToday)
	b.Get("/")

	got := b.Get("/admin/doors/1/calendar?week=2026-01-07")

	page := testsupport.ParsePage(got.Body)
	want := []any{200, "Lobby · doorcode", "Lobby", weekLinks,
		[]string{"/admin/doors/1/calendar?week=2025-12-29", "/admin/doors/1/calendar", "/admin/doors/1/calendar?week=2026-01-12"},
		[][]string{{"State", "Starts", "Ends", "Length"},
			{"Locked", "Mon 5 Jan 10:00 UTC", "Mon 5 Jan 12:00 UTC", "2 h"},
			{"Open mode", "Mon 5 Jan 22:00 UTC", "Tue 6 Jan 06:00 UTC", "8 h"},
		},
		[][]string{{"Group", "Schedule", "Starts", "Ends"},
			{"Staff", "Weekdays", "Mon 5 Jan 08:00 UTC", "Mon 5 Jan 18:00 UTC"},
			{"Staff", "Weekdays", "Tue 6 Jan 08:00 UTC", "Tue 6 Jan 18:00 UTC"},
			{"Staff", "Weekdays", "Wed 7 Jan 08:00 UTC", "Wed 7 Jan 18:00 UTC"},
			{"Staff", "Weekdays", "Thu 8 Jan 08:00 UTC", "Thu 8 Jan 18:00 UTC"},
			{"Staff", "Weekdays", "Fri 9 Jan 08:00 UTC", "Fri 9 Jan 18:00 UTC"},
		},
	}
	nav := len(page.Nav) - len(weekLinks)
	if diff := cmp.Diff(want, []any{got.Status, page.Title, page.Heading, page.Nav[nav:], page.NavTargets[nav:],
		page.Tables["Timeline"], page.Tables["Access windows"]}); diff != "" {
		t.Errorf("status, title, heading, week links, timeline, windows (-want +got):\n%s", diff)
	}
}

func TestDoorCalendarSaysWhenNothingHappensThisWeek(t *testing.T) {
	t.Parallel()
	b := signedInAdmin(t, testsupport.NewFactory())
	createDoor(t, b, frontDoor)

	page := testsupport.ParsePage(b.Get("/admin/doors/1/calendar?week=soon").Body)

	want := []any{
		[][]string{{"State", "Starts", "Ends", "Length"}, {"The door is neither in open mode nor locked this week."}},
		[][]string{{"Group", "Schedule", "Starts", "Ends"}, {"No access rule lets anyone in this week."}},
		"/admin/doors/1/calendar?week=2025-12-29",
	}
	if diff := cmp.Diff(want, []any{page.Tables["Timeline"], page.Tables["Access windows"], page.NavTargets[len(page.NavTargets)-3]}); diff != "" {
		t.Errorf("timeline, windows, previous week (-want +got):\n%s", diff)
	}
}

func TestDoorCalendarLoadsTheCalendarLibrary(t *testing.T) {
	t.Parallel()
	b := signedInAdmin(t, testsupport.NewFactory())
	createDoor(t, b, frontDoor)

	page := testsupport.ParsePage(b.Get("/admin/doors/1/calendar").Body)

	var got []testsupport.Response
	for _, asset := range page.Assets[2:] {
		response := b.Get(asset)
		got = append(got, testsupport.Response{Status: response.Status, ContentType: response.ContentType})
	}
	js := testsupport.Response{Status: 200, ContentType: "text/javascript; charset=utf-8"}
	if diff := cmp.Diff([]testsupport.Response{js, js}, got); diff != "" {
		t.Errorf("calendar scripts (-want +got):\n%s", diff)
	}
}

func TestUserPageListsTheDoorsTheirGroupsLetThemEnter(t *testing.T) {
	t.Parallel()
	b := staffAnn(t, testsupport.NewFactory())
	join(t, b, "3", "2")
	createRule(t, b, `{"group_id":2,"schedule_id":2,"zone_ids":[1]}`)
	createRule(t, b, `{"group_id":3,"schedule_id":1,"door_ids":[1,2]}`)
	createRule(t, b, `{"group_id":1,"schedule_id":1,"door_ids":[3]}`)
	b.Get("/")

	got := pageOf(b.Get("/admin/users/2")).Tables["Door access"]

	want := [][]string{accessHead, {"Dock", "Guests", "Calendar"}, {"Lobby", "Guests, Staff", "Calendar"}}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("door access (-want +got):\n%s", diff)
	}
}

func TestAccessCalendarShowsWhenTheUsersCodeWorksAtTheDoor(t *testing.T) {
	t.Parallel()
	b := staffAnn(t, testsupport.NewFactory())
	createRule(t, b, `{"group_id":2,"schedule_id":3,"zone_ids":[1]}`)
	createOverride(t, b, "1", `{"kind":"locked","start":"2026-01-05T23:00:00Z","end":"2026-01-06T01:00:00Z"}`)
	b.Get("/")

	got := b.Get("/admin/users/2/doors/1/calendar")

	page := testsupport.ParsePage(got.Body)
	want := []any{200, "Ann Smith at Lobby · doorcode", "Ann Smith at Lobby", []string(nil),
		[]string{"/admin/users/2/doors/1/calendar?week=2025-12-29", "/admin/users/2/doors/1/calendar", "/admin/users/2/doors/1/calendar?week=2026-01-12"},
		[][]string{{"Starts", "Ends", "Length"},
			{"Mon 5 Jan 22:00 UTC", "Mon 5 Jan 23:00 UTC", "1 h"},
			{"Tue 6 Jan 01:00 UTC", "Tue 6 Jan 06:00 UTC", "5 h"},
		},
		[][]string{{"State", "Starts", "Ends", "Length"}, {"Locked", "Mon 5 Jan 23:00 UTC", "Tue 6 Jan 01:00 UTC", "2 h"}},
	}
	targets := page.NavTargets[len(page.NavTargets)-3:]
	if diff := cmp.Diff(want, []any{got.Status, page.Title, page.Heading, page.Notes, targets,
		page.Tables["When the code works"], page.Tables["Timeline"]}); diff != "" {
		t.Errorf("status, title, heading, notes, week links, works, timeline (-want +got):\n%s", diff)
	}
}

func TestAccessCalendarOfADoorTheUserCannotEnterSaysSo(t *testing.T) {
	t.Parallel()
	b := staffAnn(t, testsupport.NewFactory())
	send(t, b, "PUT", "/api/v1/doors/2", `{"name":"Dock","open_schedule_ids":[3]}`, 200)
	createRule(t, b, `{"group_id":3,"schedule_id":1,"door_ids":[2]}`)

	page := pageOf(b.Get("/admin/users/2/doors/2/calendar"))

	want := []any{
		[]string{"No access rule naming one of Ann Smith’s groups reaches this door, so their code never opens it. The door’s own open mode is shown."},
		[][]string{{"Starts", "Ends", "Length"}, {"Their code does not open this door this week."}},
		[][]string{{"State", "Starts", "Ends", "Length"}, {"Open mode", "Mon 5 Jan 22:00 UTC", "Tue 6 Jan 06:00 UTC", "8 h"}},
	}
	if diff := cmp.Diff(want, []any{page.Notes, page.Tables["When the code works"], page.Tables["Timeline"]}); diff != "" {
		t.Errorf("notes, works, timeline (-want +got):\n%s", diff)
	}
}

func TestCalendarPagesThatDoNotExistSaySo(t *testing.T) {
	t.Parallel()
	for _, target := range []string{"/admin/doors/9/calendar", "/admin/users/9/doors/1/calendar", "/admin/users/2/doors/9/calendar"} {
		t.Run(target, func(t *testing.T) {
			b := staffAnn(t, testsupport.NewFactory())

			got := b.Get(target)

			if diff := cmp.Diff([]any{404, "Not found"}, []any{got.Status, pageOf(got).Heading}); diff != "" {
				t.Errorf("status, heading (-want +got):\n%s", diff)
			}
		})
	}
}

func TestCalendarPagesAskForTheirRoles(t *testing.T) {
	t.Parallel()
	_, users := adminHolding(t, testsupport.NewFactory(), `["manage-users"]`)
	_, doors := adminHolding(t, testsupport.NewFactory(), `["manage-doors"]`)

	got := []any{users.Get("/admin/doors/1/calendar").Status, doors.Get("/admin/users/1/doors/1/calendar").Status}

	if diff := cmp.Diff([]any{403, 403}, got); diff != "" {
		t.Errorf("door calendar without manage-doors, access calendar without manage-users (-want +got):\n%s", diff)
	}
}

func TestCalendarPageStoreFailureShowsARequestID(t *testing.T) {
	t.Parallel()
	cases := map[string]struct {
		fail   func(s *testsupport.Store)
		target string
	}{
		"a door calendar's time zone": {(*testsupport.Store).FailSettings, "/admin/doors/1/calendar"},
		"a door calendar's timeline":  {(*testsupport.Store).FailOverrides, "/admin/doors/1/calendar"},
		"a door calendar's names":     {(*testsupport.Store).FailGroups, "/admin/doors/1/calendar"},
		"a door calendar's rules":     {(*testsupport.Store).FailRules, "/admin/doors/1/calendar"},
		"an access calendar's door":   {(*testsupport.Store).FailDoors, "/admin/users/2/doors/1/calendar"},
		"an access calendar's rules":  {(*testsupport.Store).FailRules, "/admin/users/2/doors/1/calendar"},
		"a user page's door access":   {(*testsupport.Store).FailRules, "/admin/users/2"},
		"a user page's doors":         {(*testsupport.Store).FailDoors, "/admin/users/2"},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			f := testsupport.NewFactory()
			b := staffAnn(t, f)
			c.fail(f.StoreStub)

			got := b.Get(c.target)

			if diff := cmp.Diff([]any{500, "Something went wrong"}, []any{got.Status, pageOf(got).Heading}); diff != "" {
				t.Errorf("status, heading (-want +got):\n%s", diff)
			}
		})
	}
}
