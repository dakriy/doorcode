package entrypoint_test

import (
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/dakriy/doorcode/internal/testsupport"
)

// loggedSite is the keypad site with a rule letting Staff into Lobby on
// weekdays, the door Dock (2), and five events: Ann let into Lobby at
// 09:00 (1), an unknown code denied at Lobby at 09:10 (2), Ann denied at
// Dock at 09:20 (3), all on Monday 5 January, then Cid let into Lobby on
// Tuesday at 08:30 (4), and the administrator, signed in again, opening
// Lobby on Tuesday at 09:00 (5).
func loggedSite(t *testing.T, f *testsupport.Factory) *keypad {
	t.Helper()
	k := keypadSite(t, f)
	createRule(t, k.b, `{"group_id":2,"schedule_id":2,"zone_ids":[1]}`)
	createDoor(t, k.b, `{"name":"Dock"}`)
	k.press("111111")
	f.ClockStub.Set("2026-01-05T09:10:00Z")
	k.press("999999")
	f.ClockStub.Set("2026-01-05T09:20:00Z")
	f.BrokerStub.Inject("doorcode/doors/2/check", testsupport.CBOR(map[string]any{"v": 1, "request_id": "d1", "digits": "111111"}))
	run(t, k.a)
	f.ClockStub.Set("2026-01-06T08:30:00Z")
	k.press("222222")
	f.ClockStub.Set("2026-01-06T09:00:00Z")
	k.b = signIn(t, f, k.a.Handler())
	send(t, k.b, "POST", "/api/v1/doors/1/open", "", 200)
	return k
}

// eventIDs is the id of each event the API lists for query, and how many
// the query selects in all.
func eventIDs(t *testing.T, b *testsupport.Browser, query string) []any {
	t.Helper()
	body := send(t, b, "GET", "/api/v1/events"+query, "", 200).Body
	events, _ := testsupport.JSONAt(body, "/events").([]any)
	ids := []any{}
	for _, e := range events {
		ids = append(ids, e.(map[string]any)["id"])
	}
	return []any{ids, testsupport.JSONAt(body, "/total")}
}

func TestTheEventsAPIKeepsTheEventsItsFiltersSelectAPageAtATime(t *testing.T) {
	t.Parallel()
	k := loggedSite(t, testsupport.NewFactory())
	cases := map[string]struct {
		query string
		ids   []any
		total float64
	}{
		"every event":    {"", []any{5.0, 4.0, 3.0, 2.0, 1.0}, 5},
		"a door":         {"?door=2", []any{3.0}, 1},
		"a user":         {"?user=2", []any{3.0, 1.0}, 2},
		"an outcome":     {"?outcome=denied", []any{3.0, 2.0}, 2},
		"a reason":       {"?reason=no+rule", []any{3.0, 2.0}, 2},
		"a type":         {"?type=open", []any{5.0}, 1},
		"from a time":    {"?from=2026-01-06T00:00:00Z", []any{5.0, 4.0}, 2},
		"before a time":  {"?to=2026-01-05T09:10:00Z", []any{1.0}, 1},
		"filters joined": {"?door=1&outcome=allowed", []any{5.0, 4.0, 1.0}, 3},
		"a page":         {"?per_page=2&page=2", []any{3.0, 2.0}, 5},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			if diff := cmp.Diff([]any{c.ids, c.total}, eventIDs(t, k.b, c.query)); diff != "" {
				t.Errorf("ids, total (-want +got):\n%s", diff)
			}
		})
	}
}

func TestTheEventsAPIRefusesAFilterItCannotRead(t *testing.T) {
	t.Parallel()
	b := signedInAdmin(t, testsupport.NewFactory())
	for _, query := range []string{"door=x", "user=0", "type=telemetry", "outcome=maybe", "reason=luck", "from=monday", "to=2026-01-06", "page=0", "per_page=501"} {
		t.Run(query, func(t *testing.T) {
			got := b.Get("/api/v1/events?" + query)
			if diff := cmp.Diff(400, got.Status); diff != "" {
				t.Errorf("status (-want +got):\n%s", diff)
			}
		})
	}
}

func TestReadingEventsNeedsTheViewLogsRole(t *testing.T) {
	t.Parallel()
	f := testsupport.NewFactory()
	_, b := adminHolding(t, f, `["manage-doors"]`)

	got := []any{b.Get("/api/v1/events").Status, b.Get("/admin/events").Status}

	if diff := cmp.Diff([]any{403, 403}, got); diff != "" {
		t.Errorf("API, page (-want +got):\n%s", diff)
	}
}

// eventsHead heads the events page's table.
var eventsHead = []string{"When", "Door", "Who", "Outcome", "Details"}

func TestTheEventsPageListsEventsWithTheirOutcomesAndReasons(t *testing.T) {
	t.Parallel()
	k := loggedSite(t, testsupport.NewFactory())
	k.b.Get("/")

	page := pageOf(k.b.Get("/admin/events"))

	want := []any{"Events · doorcode", "Activity: Events (current)", [][]string{eventsHead,
		{"Tue 6 Jan 2026 09:00:00", "Lobby", "Administrator", "Allowed Admin UI", "Opened from the UI Strike released for 3000 ms."},
		{"Tue 6 Jan 2026 08:30:00", "Lobby", "Cid Jones", "Allowed Door commander", "Code check r3"},
		{"Mon 5 Jan 2026 09:20:00", "Dock", "Ann Smith", "Denied No rule", "Code check d1"},
		{"Mon 5 Jan 2026 09:10:00", "Lobby", "Unknown code", "Denied No rule", "Code check r2"},
		{"Mon 5 Jan 2026 09:00:00", "Lobby", "Ann Smith", "Allowed Access rule", "Code check · rule 1 r1"},
		{"Showing 1 to 5 of 5"},
	}}
	if diff := cmp.Diff(want, []any{page.Title, page.Nav[9], page.Tables["Events"]}); diff != "" {
		t.Errorf("title, navigation, events (-want +got):\n%s", diff)
	}
}

func TestTheEventsPageFiltersByWhatItsFormSays(t *testing.T) {
	t.Parallel()
	k := loggedSite(t, testsupport.NewFactory())
	k.b.Get("/")

	page := testsupport.ParsePage(k.b.Get("/admin/events?user=2&door=2&outcome=denied&reason=no+rule&type=check&from=2026-01-05&to=2026-01-05").Body)

	want := []any{
		[][]string{eventsHead, {"Mon 5 Jan 2026 09:20:00", "Dock", "Ann Smith", "Denied No rule", "Code check d1"}, {"Showing 1 to 1 of 1"}},
		map[string]string{"from": "2026-01-05", "to": "2026-01-05", "user": "2", "who": ""},
		[]string{"Every door", "Dock (selected)", "Lobby"},
		[]string{"User: Ann Smith ×"},
		[]string{"Every type", "Code check (selected)", "Door command", "Opened from the UI", "Door alarm", "Door activity", "Firmware update"},
		[]string{"Every outcome", "Allowed", "Denied (selected)", "Alert", "Info"},
		[]string{"door"},
	}
	got := []any{page.Tables["Events"], page.Values, page.Options["door"], page.Messages, page.Options["type"], page.Options["outcome"], page.Comboboxes}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("events, values, doors, user, types, outcomes, searchable selects (-want +got):\n%s", diff)
	}
}

// Without script the user filter is a text box whose value names the user
// by email or user id, in place of the user the form held.
func TestTheEventsPageFiltersByTheUserWhoseEmailOrUserIDIsTyped(t *testing.T) {
	t.Parallel()
	for name, query := range map[string]string{
		"an email":                     "?user=&who=ANN@example.com",
		"a user id":                    "?who=+100234+",
		"in place of the user it held": "?user=3&who=ann@example.com",
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			k := loggedSite(t, testsupport.NewFactory())
			k.b.Get("/")

			page := pageOf(k.b.Get("/admin/events" + query))

			rows := page.Tables["Events"]
			want := []any{[]string{"User: Ann Smith ×"}, map[string]string{"user": "2", "who": "", "from": "", "to": ""}, []string{"Showing 1 to 2 of 2"}}
			if diff := cmp.Diff(want, []any{page.Messages, page.Values, rows[len(rows)-1]}); diff != "" {
				t.Errorf("user, values, events shown (-want +got):\n%s", diff)
			}
		})
	}
}

func TestTheEventsPageShowsEveryUsersEventsWhenNobodyHasTheTypedEmailOrUserID(t *testing.T) {
	t.Parallel()
	k := loggedSite(t, testsupport.NewFactory())
	k.b.Get("/")

	page := pageOf(k.b.Get("/admin/events?user=2&who=nobody&page=x"))

	rows := page.Tables["Events"]
	want := []any{
		[]string{"Nobody has the email or user id you typed, so events of every user are shown."},
		map[string]string{"user": "", "who": "nobody", "from": "", "to": ""}, []string{"Showing 1 to 5 of 5"},
	}
	if diff := cmp.Diff(want, []any{page.Messages, page.Values, rows[len(rows)-1]}); diff != "" {
		t.Errorf("messages, values, events shown (-want +got):\n%s", diff)
	}
}

func TestTheEventsPageClearsItsUserKeepingItsOtherFilters(t *testing.T) {
	t.Parallel()
	k := loggedSite(t, testsupport.NewFactory())
	k.b.Get("/")

	clear := pageOf(k.b.Get("/admin/events?user=2&door=1")).LinkTargets["Stop filtering by user"]
	page := pageOf(k.b.Get(clear))

	rows := page.Tables["Events"]
	want := []any{"/admin/events?door=1", map[string]string{"user": "", "who": "", "from": "", "to": ""}, []string{"Showing 1 to 4 of 4"}}
	if diff := cmp.Diff(want, []any{clear, page.Values, rows[len(rows)-1]}); diff != "" {
		t.Errorf("link, values, events shown (-want +got):\n%s", diff)
	}
}

// With script the user filter's text box asks the server for the users
// matching what is typed, a page at most, and choosing one fills the
// form's user, as TestTheEventsPageFiltersByWhatItsFormSays submits it.
func TestTheEventsPageUserFilterAsksTheServerAsYouType(t *testing.T) {
	t.Parallel()
	k := loggedSite(t, testsupport.NewFactory())
	k.b.Get("/")

	searches := pageOf(k.b.Get("/admin/events")).UserSearches
	got := testsupport.JSON(k.b.Get(searches["who"] + "?q=smith").Body)

	want := []any{
		map[string]string{"who": "/api/v1/users/search"},
		testsupport.JSON(`{"users":[{"id":2,"name":"Ann Smith","email":"ann@example.com"}],"total":1}`),
	}
	if diff := cmp.Diff(want, []any{searches, got}); diff != "" {
		t.Errorf("user searches, what one finds (-want +got):\n%s", diff)
	}
}

func TestTheEventsPageReadsItsDatesAsWholeDaysInTheSiteTimezone(t *testing.T) {
	t.Parallel()
	k := loggedSite(t, testsupport.NewFactory())
	k.b.Get("/")

	monday := pageOf(k.b.Get("/admin/events?to=2026-01-05")).Tables["Events"]
	tuesday := pageOf(k.b.Get("/admin/events?from=2026-01-06")).Tables["Events"]

	want := []any{[]string{"Showing 1 to 3 of 3"}, []string{"Showing 1 to 2 of 2"}}
	if diff := cmp.Diff(want, []any{monday[len(monday)-1], tuesday[len(tuesday)-1]}); diff != "" {
		t.Errorf("up to Monday, from Tuesday (-want +got):\n%s", diff)
	}
}

func TestTheEventsPageSaysWhyItListsNoEvents(t *testing.T) {
	t.Parallel()
	cases := map[string]struct {
		logged bool
		query  string
		want   string
	}{
		"nothing logged yet":       {false, "", "No events yet. Codes entered at keypads, keypad commands, doors opened from here, and door alarms are logged as they happen."},
		"filters no event matches": {true, "?door=2&outcome=allowed", "No events match these filters."},
		"a page past the last":     {true, "?page=9", "This page is past the last one."},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			f := testsupport.NewFactory()
			b := signedInAdmin(t, f)
			if c.logged {
				b = loggedSite(t, f).b
			}
			b.Get("/")

			got := pageOf(b.Get("/admin/events" + c.query)).Tables["Events"]

			if diff := cmp.Diff([][]string{eventsHead, {c.want}}, got); diff != "" {
				t.Errorf("events (-want +got):\n%s", diff)
			}
		})
	}
}

func TestTheEventsPageShowsEveryEventWhenItsFiltersCannotBeRead(t *testing.T) {
	t.Parallel()
	k := loggedSite(t, testsupport.NewFactory())
	k.b.Get("/")

	page := pageOf(k.b.Get("/admin/events?from=monday&door=2"))

	rows := page.Tables["Events"]
	want := []any{[]string{"Some filters could not be read, so every event is shown."}, []string{"Showing 1 to 5 of 5"}}
	if diff := cmp.Diff(want, []any{page.Messages, rows[len(rows)-1]}); diff != "" {
		t.Errorf("messages, events shown (-want +got):\n%s", diff)
	}
}

func TestTheEventsPageNamesAUserItsFilterNamesThoughNobodyHasTheID(t *testing.T) {
	t.Parallel()
	b := signedInAdmin(t, testsupport.NewFactory())
	b.Get("/")

	got := testsupport.ParsePage(b.Get("/admin/events?user=99").Body).Messages

	if diff := cmp.Diff([]string{"User: No such user ×"}, got); diff != "" {
		t.Errorf("user (-want +got):\n%s", diff)
	}
}

func TestTheEventsPageNamesADeletedUserItsFilterNames(t *testing.T) {
	t.Parallel()
	k := loggedSite(t, testsupport.NewFactory())
	k.b.Post("/admin/users/2/delete")
	k.b.Get("/")

	got := testsupport.ParsePage(k.b.Get("/admin/events?user=2").Body).Messages

	if diff := cmp.Diff([]string{"User: Ann Smith ×"}, got); diff != "" {
		t.Errorf("user (-want +got):\n%s", diff)
	}
}

func TestTheEventsPageIsPagedKeepingItsFilters(t *testing.T) {
	t.Parallel()
	b := openedTimes(t, testsupport.NewFactory())

	first := testsupport.ParsePage(b.Get("/admin/events?door=&type=open&outcome=").Body)
	second := testsupport.ParsePage(b.Get("/admin/events?type=open&page=2").Body)

	want := []any{
		[]string{"Showing 1 to 50 of 51"}, []string{"Site: Settings", "Older"}, "/admin/events?page=2&type=open",
		[]string{"Showing 51 to 51 of 51"}, []string{"Site: Settings", "Newer"}, "/admin/events?page=1&type=open",
	}
	got := []any{
		first.Tables["Events"][51], first.Nav[len(first.Nav)-2:], first.NavTargets[len(first.NavTargets)-1],
		second.Tables["Events"][2], second.Nav[len(second.Nav)-2:], second.NavTargets[len(second.NavTargets)-1],
	}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("first page, its pager, second page, its pager (-want +got):\n%s", diff)
	}
}

func TestDoorAndUserPagesShowTheirRecentEvents(t *testing.T) {
	t.Parallel()
	k := loggedSite(t, testsupport.NewFactory())
	k.b.Get("/")

	door := pageOf(k.b.Get("/admin/doors/2")).Tables["Recent events"]
	user := pageOf(k.b.Get("/admin/users/2")).Tables["Recent events"]

	want := []any{
		[][]string{doorEventsHead, {"Mon 5 Jan 2026 09:20:00", "Ann Smith", "Denied No rule", "Code check d1"}},
		[][]string{userEventsHead,
			{"Mon 5 Jan 2026 09:20:00", "Dock", "Denied No rule", "Code check d1"},
			{"Mon 5 Jan 2026 09:00:00", "Lobby", "Allowed Access rule", "Code check · rule 1 r1"},
		},
	}
	if diff := cmp.Diff(want, []any{door, user}); diff != "" {
		t.Errorf("Dock's, Ann's (-want +got):\n%s", diff)
	}
}

func TestEventPagesFailWhenTheStoreDoes(t *testing.T) {
	t.Parallel()
	cases := map[string]struct {
		fail   func(f *testsupport.Factory)
		target string
	}{
		"the events page's events":     {func(f *testsupport.Factory) { f.StoreStub.FailEvents() }, "/admin/events"},
		"the events page's doors":      {func(f *testsupport.Factory) { f.StoreStub.FailDoors() }, "/admin/events"},
		"the events page's user":       {func(f *testsupport.Factory) { f.StoreStub.FailUsers() }, "/admin/events?user=1"},
		"the events page's typed user": {func(f *testsupport.Factory) { f.StoreStub.FailUsers() }, "/admin/events?who=ann@example.com"},
		"the user search":              {func(f *testsupport.Factory) { f.StoreStub.FailUsers() }, "/api/v1/users/search?q=a"},
		"the events page's timezone":   {func(f *testsupport.Factory) { f.StoreStub.FailSettings() }, "/admin/events"},
		"the events API":               {func(f *testsupport.Factory) { f.StoreStub.FailEvents() }, "/api/v1/events"},
		"a door page's events":         {func(f *testsupport.Factory) { f.StoreStub.FailEvents() }, "/admin/doors/1"},
		"a user page's events":         {func(f *testsupport.Factory) { f.StoreStub.FailEvents() }, "/admin/users/1"},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			f := testsupport.NewFactory()
			b := signedInAdmin(t, f)
			createDoor(t, b, frontDoor)
			c.fail(f)

			if diff := cmp.Diff(500, b.Get(c.target).Status); diff != "" {
				t.Errorf("status (-want +got):\n%s", diff)
			}
		})
	}
}

// openedTimes is a site whose Lobby (1) was opened from the API 51 times,
// one more than a page of events holds, with the administrator's browser
// on its home page.
func openedTimes(t *testing.T, f *testsupport.Factory) *testsupport.Browser {
	t.Helper()
	_, b, _, _ := enrolledDoor(t, f)
	for range 51 {
		send(t, b, "POST", "/api/v1/doors/1/open", "", 200)
	}
	b.Get("/")
	return b
}

func TestTheEventsPagePagesEveryEventWithoutTheFiltersItCouldNotRead(t *testing.T) {
	t.Parallel()
	b := openedTimes(t, testsupport.NewFactory())

	page := testsupport.ParsePage(b.Get("/admin/events?door=x&type=open").Body)

	want := []any{
		[]string{"Some filters could not be read, so every event is shown."},
		[]string{"Showing 1 to 50 of 51"}, "/admin/events?page=2",
	}
	got := []any{page.Messages, page.Tables["Events"][51], page.NavTargets[len(page.NavTargets)-1]}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("messages, events shown, older page (-want +got):\n%s", diff)
	}
}

func TestTheEventsPageShowsTheFirstPageKeepingItsFiltersWhenThePageNumberCannotBeRead(t *testing.T) {
	t.Parallel()
	b := openedTimes(t, testsupport.NewFactory())

	page := testsupport.ParsePage(b.Get("/admin/events?type=open&page=x").Body)

	want := []any{
		[]string{"The page number could not be read, so the first page is shown."},
		[]string{"Showing 1 to 50 of 51"}, "/admin/events?page=2&type=open", "Opened from the UI (selected)",
	}
	got := []any{page.Messages, page.Tables["Events"][51], page.NavTargets[len(page.NavTargets)-1], page.Options["type"][3]}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("messages, events shown, older page, type (-want +got):\n%s", diff)
	}
}

// The events page loads the script of its searchable select and user
// search once, and the live script.
func TestTheEventsPageLoadsTheScriptOfItsSearchableSelects(t *testing.T) {
	t.Parallel()
	b := signedInAdmin(t, testsupport.NewFactory())
	b.Get("/")

	page := testsupport.ParsePage(b.Get("/admin/events").Body)

	var got []testsupport.Response
	for _, asset := range page.Assets[2:] {
		response := b.Get(asset)
		got = append(got, testsupport.Response{Status: response.Status, ContentType: response.ContentType})
	}
	js := testsupport.Response{Status: 200, ContentType: "text/javascript; charset=utf-8"}
	if diff := cmp.Diff([]testsupport.Response{js, js}, got); diff != "" {
		t.Errorf("scripts (-want +got):\n%s", diff)
	}
}
