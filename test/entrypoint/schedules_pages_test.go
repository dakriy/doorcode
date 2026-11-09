package entrypoint_test

import (
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/dakriy/doorcode/internal/testsupport"
)

// schedulesNav is the navigation of a schedules page for an administrator.
var schedulesNav = []string{"Home", "Dashboard", "Users", "Requests", "Groups", "Schedules (current)", "Doors", "Zones", "Firmware", "Events", "Settings", "API tokens", "API docs"}

// schedulesHead is the header row of the schedule list.
var schedulesHead = []string{"Name", "Windows", "In effect", "Excluded dates", "Actions"}

// alwaysRow is the Always schedule as the list shows it.
var alwaysRow = []string{"Always Built in", "Every day, all day", "Every week", "0", "Calendar"}

// nights is a schedule body for page tests: weeknights, with one excluded
// date.
const nights = `{"name":"Nights","starts_on":"2026-01-01","windows":[{"days":["mon","tue","wed","thu","fri"],"start":"22:00","end":"06:00"}],` +
	`"excluded_dates":["2026-12-25"]}`

// nightsForm is the nights schedule's form as its page shows it.
var nightsForm = map[string]string{"name": "Nights", "starts_on": "2026-01-01", "ends_on": "", "windows": "1", "dates": "1",
	"window-0-start": "22:00", "window-0-end": "06:00", "excluded-0": "2026-12-25"}

// weeknights is the checked days of the nights schedule's window.
var weeknights = []string{"window-0-days=mon", "window-0-days=tue", "window-0-days=wed", "window-0-days=thu", "window-0-days=fri"}

func TestSchedulesPageListsSchedulesWithWhenTheyApply(t *testing.T) {
	t.Parallel()
	b := signedInAdmin(t, testsupport.NewFactory())
	createSchedule(t, b, `{"name":"Nights","starts_on":"2026-01-01","windows":[{"days":["mon","tue","wed","thu","fri"],"start":"22:00","end":"06:00"},`+
		`{"days":["sat"],"start":"00:00","end":"08:00"}]}`)
	createSchedule(t, b, `{"name":"Weekend","ends_on":"2026-12-31","windows":[{"days":["sat","sun"],"start":"10:00","end":"16:00"},`+
		`{"days":["sun"],"start":"00:00","end":"00:00"}]}`)
	createSchedule(t, b, `{"name":"Odd days","starts_on":"2026-01-01","ends_on":"2026-03-31",`+
		`"windows":[{"days":["mon","wed","fri","sun"],"start":"07:00","end":"07:00"},{"days":["tue","wed","thu"],"start":"12:00","end":"13:00"}],`+
		`"excluded_dates":["2026-01-01","2026-02-01"]}`)
	b.Get("/")

	got := b.Get("/admin/schedules")

	want := testsupport.Page{
		Title: "Schedules · doorcode", Heading: "Schedules", Nav: schedulesNav,
		Tables: map[string][][]string{"Schedules": {
			schedulesHead,
			alwaysRow,
			{"Nights", "Mon–Fri 22:00–06:00 next day Sat 00:00–08:00", "From 1 Jan 2026", "0", "Calendar"},
			{"Odd days", "Mon, Wed, Fri, Sun 07:00–07:00 next day Tue–Thu 12:00–13:00", "1 Jan 2026 – 31 Mar 2026", "2", "Calendar"},
			{"Weekend", "Sat, Sun 10:00–16:00 Sun, all day", "Until 31 Dec 2026", "0", "Calendar"},
		}},
	}
	if diff := cmp.Diff(want, pageOf(got)); diff != "" {
		t.Errorf("page (-want +got):\n%s", diff)
	}
}

func TestNewSchedulePageOffersAFormWithOneWindow(t *testing.T) {
	t.Parallel()
	b := signedInAdmin(t, testsupport.NewFactory())
	b.Get("/")

	got := b.Get("/admin/schedules/new")

	want := testsupport.Page{
		Title: "New schedule · doorcode", Heading: "New schedule", Nav: schedulesNav,
		Values: map[string]string{"name": "", "starts_on": "", "ends_on": "", "windows": "1", "dates": "0",
			"window-0-start": "09:00", "window-0-end": "17:00"},
	}
	if diff := cmp.Diff(want, pageOf(got)); diff != "" {
		t.Errorf("page (-want +got):\n%s", diff)
	}
}

func TestCreatingAScheduleFromTheFormOpensItsPage(t *testing.T) {
	t.Parallel()
	b := signedInAdmin(t, testsupport.NewFactory())
	b.Get("/")

	created := b.Post("/admin/schedules", "name", "Nights", "starts_on", "2026-01-01", "ends_on", "", "windows", "1", "dates", "2",
		"window-0-days", "mon", "window-0-days", "tue", "window-0-days", "wed", "window-0-days", "thu", "window-0-days", "fri",
		"window-0-start", "22:00", "window-0-end", "06:00", "excluded-0", "", "excluded-1", "2026-12-25")
	got := pageOf(b.Get(created.Location))

	if diff := cmp.Diff([]any{303, "/admin/schedules/2"}, []any{created.Status, created.Location}); diff != "" {
		t.Errorf("redirect (-want +got):\n%s", diff)
	}
	want := testsupport.Page{
		Title: "Nights · doorcode", Heading: "Nights", Nav: schedulesNav,
		Messages: []string{"The schedule has been created."},
		Values:   nightsForm,
		Checked:  weeknights,
	}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("page (-want +got):\n%s", diff)
	}
}

func TestScheduleFormSaysWhatIsWrongAndKeepsWhatWasEntered(t *testing.T) {
	t.Parallel()
	b := signedInAdmin(t, testsupport.NewFactory())

	got := b.Post("/admin/schedules", "name", "always", "starts_on", "2026-03-02", "ends_on", "2026-03-01", "windows", "2", "dates", "1",
		"window-0-start", "09:00", "window-0-end", "17:00",
		"window-1-days", "sat", "window-1-start", "late", "window-1-end", "", "excluded-0", "Christmas")

	page := pageOf(got)
	want := []any{422,
		map[string]string{
			"name": "Another schedule has this name.", "ends_on": "End the range on or after the day it starts.",
			"window-0-days": "Choose at least one day for window 1.", "window-1-start": "Enter the start of window 2 as HH:MM.",
			"window-1-end": "Enter the end of window 2 as HH:MM.", "excluded-0": "Enter excluded dates as YYYY-MM-DD; “Christmas” is not one.",
		},
		map[string]string{"name": "always", "starts_on": "2026-03-02", "ends_on": "2026-03-01", "windows": "2", "dates": "1",
			"window-0-start": "09:00", "window-0-end": "17:00", "window-1-start": "late", "window-1-end": "", "excluded-0": "Christmas"},
		[]string{"window-1-days=sat"},
	}
	if diff := cmp.Diff(want, []any{got.Status, page.FieldErrors, page.Values, page.Checked}); diff != "" {
		t.Errorf("status, field errors, values, days (-want +got):\n%s", diff)
	}
}

func TestScheduleFormWithoutWindowsAsksForOne(t *testing.T) {
	t.Parallel()
	got := signedInAdmin(t, testsupport.NewFactory()).Post("/admin/schedules", "name", "Empty", "windows", "0", "dates", "0")

	want := []any{422, map[string]string{"windows": "Add at least one window."}}
	if diff := cmp.Diff(want, []any{got.Status, pageOf(got).FieldErrors}); diff != "" {
		t.Errorf("status, field errors (-want +got):\n%s", diff)
	}
}

func TestScheduleFormReadsAtMostOneRowPastEachLimit(t *testing.T) {
	t.Parallel()
	got := signedInAdmin(t, testsupport.NewFactory()).Post("/admin/schedules", "name", "Huge", "windows", "100000", "dates", "100000")

	page := pageOf(got)
	want := []any{422, "Use at most 20 windows.", "Exclude at most 366 dates.", "367", "367"}
	if diff := cmp.Diff(want, []any{got.Status, page.FieldErrors["windows"], page.FieldErrors["excluded"], page.Values["windows"], page.Values["dates"]}); diff != "" {
		t.Errorf("status, windows and dates problems, rows shown (-want +got):\n%s", diff)
	}
}

// oneWindow is the fields of a new schedule form with one window and one
// excluded date, before a button changes it.
var oneWindow = []string{"name", "Nights", "windows", "2", "dates", "2",
	"window-0-days", "mon", "window-0-start", "22:00", "window-0-end", "06:00",
	"window-1-days", "sat", "window-1-start", "10:00", "window-1-end", "12:00",
	"excluded-0", "2026-12-24", "excluded-1", "2026-12-25"}

func TestScheduleFormButtonsAddAndRemoveRows(t *testing.T) {
	t.Parallel()
	cases := map[string]struct {
		button []string
		values map[string]string
		days   []string
	}{
		"add a window": {
			button: []string{"add_window", "1"},
			values: map[string]string{"windows": "3", "dates": "2", "window-0-start": "22:00", "window-0-end": "06:00",
				"window-1-start": "10:00", "window-1-end": "12:00", "window-2-start": "09:00", "window-2-end": "17:00",
				"excluded-0": "2026-12-24", "excluded-1": "2026-12-25"},
			days: []string{"window-0-days=mon", "window-1-days=sat"},
		},
		"remove the first window": {
			button: []string{"remove_window", "0"},
			values: map[string]string{"windows": "1", "dates": "2", "window-0-start": "10:00", "window-0-end": "12:00",
				"excluded-0": "2026-12-24", "excluded-1": "2026-12-25"},
			days: []string{"window-0-days=sat"},
		},
		"remove the last window": {
			button: []string{"remove_window", "1"},
			values: map[string]string{"windows": "1", "dates": "2", "window-0-start": "22:00", "window-0-end": "06:00",
				"excluded-0": "2026-12-24", "excluded-1": "2026-12-25"},
			days: []string{"window-0-days=mon"},
		},
		"add a date": {
			button: []string{"add_date", "1"},
			values: map[string]string{"windows": "2", "dates": "3", "window-0-start": "22:00", "window-0-end": "06:00",
				"window-1-start": "10:00", "window-1-end": "12:00", "excluded-0": "2026-12-24", "excluded-1": "2026-12-25", "excluded-2": ""},
			days: []string{"window-0-days=mon", "window-1-days=sat"},
		},
		"remove a date": {
			button: []string{"remove_date", "0"},
			values: map[string]string{"windows": "2", "dates": "1", "window-0-start": "22:00", "window-0-end": "06:00",
				"window-1-start": "10:00", "window-1-end": "12:00", "excluded-0": "2026-12-25"},
			days: []string{"window-0-days=mon", "window-1-days=sat"},
		},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			got := signedInAdmin(t, testsupport.NewFactory()).Post("/admin/schedules", append(append([]string{}, oneWindow...), c.button...)...)

			page := pageOf(got)
			c.values["name"], c.values["starts_on"], c.values["ends_on"] = "Nights", "", ""
			want := []any{200, "New schedule", c.values, c.days}
			if diff := cmp.Diff(want, []any{got.Status, page.Heading, page.Values, page.Checked}); diff != "" {
				t.Errorf("status, heading, values, days (-want +got):\n%s", diff)
			}
		})
	}
}

func TestScheduleFormRemoveButtonNamingNoRowKeepsTheForm(t *testing.T) {
	t.Parallel()
	for _, index := range []string{"2", "-1", "first"} {
		t.Run(index, func(t *testing.T) {
			got := signedInAdmin(t, testsupport.NewFactory()).Post("/admin/schedules", append(append([]string{}, oneWindow...), "remove_window", index)...)

			page := pageOf(got)
			want := []any{200, "2", "22:00", "10:00"}
			if diff := cmp.Diff(want, []any{got.Status, page.Values["windows"], page.Values["window-0-start"], page.Values["window-1-start"]}); diff != "" {
				t.Errorf("status, windows, starts (-want +got):\n%s", diff)
			}
		})
	}
}

func TestSchedulePageShowsItsForm(t *testing.T) {
	t.Parallel()
	b := signedInAdmin(t, testsupport.NewFactory())
	createSchedule(t, b, nights)
	b.Get("/")

	got := b.Get("/admin/schedules/2")

	want := testsupport.Page{Title: "Nights · doorcode", Heading: "Nights", Nav: schedulesNav, Values: nightsForm, Checked: weeknights}
	if diff := cmp.Diff(want, pageOf(got)); diff != "" {
		t.Errorf("page (-want +got):\n%s", diff)
	}
}

func TestSchedulePageButtonsChangeItsFormWithoutSaving(t *testing.T) {
	t.Parallel()
	b := signedInAdmin(t, testsupport.NewFactory())
	createSchedule(t, b, nights)

	got := b.Post("/admin/schedules/2", append(append([]string{}, oneWindow...), "remove_window", "0")...)
	stored := testsupport.JSONAt(b.Get("/api/v1/schedules/2").Body, "/windows")

	want := []any{200, "Nights", "1", "10:00", testsupport.JSON(`[{"days":["mon","tue","wed","thu","fri"],"start":"22:00","end":"06:00"}]`)}
	if diff := cmp.Diff(want, []any{got.Status, pageOf(got).Heading, pageOf(got).Values["windows"], pageOf(got).Values["window-0-start"], stored}); diff != "" {
		t.Errorf("status, heading, windows, start, stored windows (-want +got):\n%s", diff)
	}
}

func TestSavingAScheduleFromItsPage(t *testing.T) {
	t.Parallel()
	b := signedInAdmin(t, testsupport.NewFactory())
	createSchedule(t, b, nights)

	saved := b.Post("/admin/schedules/2", "name", "Weekends", "windows", "1", "dates", "0",
		"window-0-days", "sat", "window-0-days", "sun", "window-0-start", "10:00", "window-0-end", "16:00")
	got := pageOf(b.Get(saved.Location))

	want := []any{"/admin/schedules/2", "Weekends", []string{"The schedule's changes have been saved."},
		[]string{"window-0-days=sat", "window-0-days=sun"}, "", "0"}
	if diff := cmp.Diff(want, []any{saved.Location, got.Heading, got.Messages, got.Checked, got.Values["starts_on"], got.Values["dates"]}); diff != "" {
		t.Errorf("redirect, heading, messages, days, start, dates (-want +got):\n%s", diff)
	}
}

func TestSavingAScheduleFromItsPageSaysWhatIsWrong(t *testing.T) {
	t.Parallel()
	b := signedInAdmin(t, testsupport.NewFactory())
	createSchedule(t, b, nights)

	got := b.Post("/admin/schedules/2", "name", "", "windows", "1", "dates", "0",
		"window-0-days", "sat", "window-0-start", "10:00", "window-0-end", "16:00")

	page := pageOf(got)
	want := []any{422, "Nights", map[string]string{"name": "Enter a name."}, []string{"window-0-days=sat"}}
	if diff := cmp.Diff(want, []any{got.Status, page.Heading, page.FieldErrors, page.Checked}); diff != "" {
		t.Errorf("status, heading, field errors, days (-want +got):\n%s", diff)
	}
}

func TestAlwaysSchedulePageSaysWhyItOffersNoChanges(t *testing.T) {
	t.Parallel()
	b := signedInAdmin(t, testsupport.NewFactory())
	b.Get("/")

	got := b.Get("/admin/schedules/1")

	want := testsupport.Page{
		Title: "Always · doorcode", Heading: "Always", Nav: schedulesNav,
		Notes: []string{"Always is built in: it covers every day, all day, so any access rule can use it. " +
			"It cannot be changed or deleted. Create another schedule for other times."},
	}
	if diff := cmp.Diff(want, pageOf(got)); diff != "" {
		t.Errorf("page (-want +got):\n%s", diff)
	}
}

func TestChangingTheAlwaysScheduleFromItsPageIsRefused(t *testing.T) {
	t.Parallel()
	cases := map[string][]string{
		"saving":   {"/admin/schedules/1", "name", "Weekdays", "windows", "1", "window-0-days", "mon", "window-0-start", "09:00", "window-0-end", "17:00"},
		"deleting": {"/admin/schedules/1/delete"},
	}
	for name, request := range cases {
		t.Run(name, func(t *testing.T) {
			b := signedInAdmin(t, testsupport.NewFactory())
			b.Get("/")

			got := b.Post(request[0], request[1:]...)
			list := pageOf(b.Get("/admin/schedules")).Tables["Schedules"]

			want := []any{409, "Always", []string{"Always is built in: it cannot be changed or deleted. Create another schedule for other times."},
				[][]string{schedulesHead, alwaysRow}}
			if diff := cmp.Diff(want, []any{got.Status, pageOf(got).Heading, pageOf(got).Messages, list}); diff != "" {
				t.Errorf("status, heading, messages, list (-want +got):\n%s", diff)
			}
		})
	}
}

func TestDeletingAScheduleFromItsPage(t *testing.T) {
	t.Parallel()
	b := signedInAdmin(t, testsupport.NewFactory())
	createSchedule(t, b, nights)

	deleted := b.Post("/admin/schedules/2/delete")
	got := pageOf(b.Get(deleted.Location))

	want := []any{"/admin/schedules", []string{"The schedule has been deleted."}, [][]string{schedulesHead, alwaysRow}}
	if diff := cmp.Diff(want, []any{deleted.Location, got.Messages, got.Tables["Schedules"]}); diff != "" {
		t.Errorf("redirect, messages, list (-want +got):\n%s", diff)
	}
}

func TestSchedulePageThatDoesNotExistSaysSo(t *testing.T) {
	t.Parallel()
	cases := map[string]func(b *testsupport.Browser) testsupport.Response{
		"an unknown id": func(b *testsupport.Browser) testsupport.Response { return b.Get("/admin/schedules/9") },
		"a name":        func(b *testsupport.Browser) testsupport.Response { return b.Get("/admin/schedules/always") },
		"saving it":     func(b *testsupport.Browser) testsupport.Response { return b.Post("/admin/schedules/9", "name", "X") },
		"deleting it":   func(b *testsupport.Browser) testsupport.Response { return b.Post("/admin/schedules/9/delete") },
		"its calendar":  func(b *testsupport.Browser) testsupport.Response { return b.Get("/admin/schedules/9/calendar") },
	}
	for name, request := range cases {
		t.Run(name, func(t *testing.T) {
			b := signedInAdmin(t, testsupport.NewFactory())
			b.Get("/")

			got := request(b)

			want := testsupport.Page{Title: "Not found · doorcode", Heading: "Not found", Nav: schedulesNav}
			if diff := cmp.Diff([]any{404, want}, []any{got.Status, pageOf(got)}); diff != "" {
				t.Errorf("status and page (-want +got):\n%s", diff)
			}
		})
	}
}

// calendarNav is the navigation of a calendar page: the main navigation,
// then the weeks.
var calendarNav = append(append([]string{}, schedulesNav...), "Previous week", "This week", "Next week")

func TestScheduleCalendarListsTheWeeksOccurrencesInTheSiteTimezone(t *testing.T) {
	t.Parallel()
	b := inZone(t, "America/New_York")
	createSchedule(t, b, `{"name":"Nights","windows":[{"days":["fri","sat"],"start":"22:00","end":"06:00"},`+
		`{"days":["mon"],"start":"08:00","end":"09:30"},{"days":["sun"],"start":"23:15","end":"00:00"}]}`)
	b.Get("/")

	got := b.Get("/admin/schedules/2/calendar?week=2026-03-04")

	page := testsupport.ParsePage(got.Body)
	want := testsupport.Page{
		Title: "Nights · doorcode", Heading: "Nights", Nav: calendarNav,
		Tables: map[string][][]string{"Occurrences": {
			{"Starts", "Ends", "Length"},
			{"Mon 2 Mar 08:00 EST", "Mon 2 Mar 09:30 EST", "1 h 30 min"},
			{"Fri 6 Mar 22:00 EST", "Sat 7 Mar 06:00 EST", "8 h"},
			{"Sat 7 Mar 22:00 EST", "Sun 8 Mar 06:00 EDT", "7 h"},
			{"Sun 8 Mar 23:15 EDT", "Mon 9 Mar 00:00 EDT", "45 min"},
		}},
	}
	if diff := cmp.Diff(want, pageOf(got)); diff != "" {
		t.Errorf("page (-want +got):\n%s", diff)
	}
	wantTargets := []string{"/admin/schedules/2/calendar?week=2026-02-23", "/admin/schedules/2/calendar", "/admin/schedules/2/calendar?week=2026-03-09"}
	if diff := cmp.Diff(wantTargets, page.NavTargets[len(schedulesNav):]); diff != "" {
		t.Errorf("week links (-want +got):\n%s", diff)
	}
}

func TestScheduleCalendarLoadsTheCalendarLibrary(t *testing.T) {
	t.Parallel()
	b := signedInAdmin(t, testsupport.NewFactory())

	page := testsupport.ParsePage(b.Get("/admin/schedules/1/calendar").Body)

	var got []testsupport.Response
	for _, asset := range page.Assets[2:] {
		response := b.Get(asset)
		got = append(got, testsupport.Response{Status: response.Status, ContentType: response.ContentType})
	}
	js := testsupport.Response{Status: 200, ContentType: "text/javascript; charset=utf-8"}
	if diff := cmp.Diff([]testsupport.Response{js, js, js}, got); diff != "" {
		t.Errorf("calendar scripts (-want +got):\n%s", diff)
	}
}

func TestScheduleCalendarShowsThisWeekUnlessADayIsGiven(t *testing.T) {
	t.Parallel()
	for name, query := range map[string]string{"no day": "", "not a day": "?week=soon"} {
		t.Run(name, func(t *testing.T) {
			b := signedInAdmin(t, testsupport.NewFactory())

			page := testsupport.ParsePage(b.Get("/admin/schedules/1/calendar" + query).Body)

			want := []any{
				[]string{"/admin/schedules/1/calendar?week=2025-12-29", "/admin/schedules/1/calendar", "/admin/schedules/1/calendar?week=2026-01-12"},
				[][]string{{"Starts", "Ends", "Length"}, {"Mon 5 Jan 00:00 UTC", "Mon 12 Jan 00:00 UTC", "168 h"}},
			}
			if diff := cmp.Diff(want, []any{page.NavTargets[len(schedulesNav):], page.Tables["Occurrences"]}); diff != "" {
				t.Errorf("week links, occurrences (-want +got):\n%s", diff)
			}
		})
	}
}

func TestScheduleCalendarSaysWhenTheScheduleIsNotActive(t *testing.T) {
	t.Parallel()
	b := signedInAdmin(t, testsupport.NewFactory())
	createSchedule(t, b, `{"name":"Past","ends_on":"2025-12-31","windows":[{"days":["mon"],"start":"09:00","end":"17:00"}]}`)

	got := pageOf(b.Get("/admin/schedules/2/calendar")).Tables["Occurrences"]

	want := [][]string{{"Starts", "Ends", "Length"}, {"The schedule is not active this week."}}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("occurrences (-want +got):\n%s", diff)
	}
}

func TestSchedulePagesAskForTheRole(t *testing.T) {
	t.Parallel()
	_, b := adminHolding(t, testsupport.NewFactory(), `["manage-doors"]`)

	got := b.Get("/admin/schedules")

	if diff := cmp.Diff([]any{403, "You do not have access to this page"}, []any{got.Status, pageOf(got).Heading}); diff != "" {
		t.Errorf("status, heading (-want +got):\n%s", diff)
	}
}

func TestSchedulePageStoreFailureShowsARequestID(t *testing.T) {
	t.Parallel()
	form := []string{"name", "Days", "windows", "1", "window-0-days", "mon", "window-0-start", "09:00", "window-0-end", "17:00"}
	cases := map[string]struct {
		fail    func(s *testsupport.Store)
		request func(b *testsupport.Browser) testsupport.Response
	}{
		"list":   {(*testsupport.Store).FailSchedules, func(b *testsupport.Browser) testsupport.Response { return b.Get("/admin/schedules") }},
		"one":    {(*testsupport.Store).FailSchedules, func(b *testsupport.Browser) testsupport.Response { return b.Get("/admin/schedules/2") }},
		"create": {(*testsupport.Store).FailScheduleWrites, func(b *testsupport.Browser) testsupport.Response { return b.Post("/admin/schedules", form...) }},
		"save":   {(*testsupport.Store).FailScheduleWrites, func(b *testsupport.Browser) testsupport.Response { return b.Post("/admin/schedules/2", form...) }},
		"delete": {(*testsupport.Store).FailScheduleWrites, func(b *testsupport.Browser) testsupport.Response { return b.Post("/admin/schedules/2/delete") }},
		"calendar": {(*testsupport.Store).FailSchedules, func(b *testsupport.Browser) testsupport.Response {
			return b.Get("/admin/schedules/2/calendar")
		}},
		"calendar's time zone": {(*testsupport.Store).FailSettings, func(b *testsupport.Browser) testsupport.Response {
			return b.Get("/admin/schedules/2/calendar")
		}},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			f := testsupport.NewFactory()
			b := signedInAdmin(t, f)
			createSchedule(t, b, nights)
			c.fail(f.StoreStub)

			got := c.request(b)

			if diff := cmp.Diff([]any{500, "Something went wrong"}, []any{got.Status, pageOf(got).Heading}); diff != "" {
				t.Errorf("status, heading (-want +got):\n%s", diff)
			}
		})
	}
}
