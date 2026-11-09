package entrypoint_test

import (
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/dakriy/doorcode/internal/testsupport"
)

// alwaysJSON is the seeded Always schedule as the API shows it.
const alwaysJSON = `{"id":1,"name":"Always","windows":[{"days":["mon","tue","wed","thu","fri","sat","sun"],"start":"00:00","end":"00:00"}],` +
	`"excluded_dates":[],"builtin":true}`

// createSchedule creates a schedule through the API as c's user. Always
// has id 1, so the first schedule created after startup has id 2.
func createSchedule(t *testing.T, c *testsupport.Browser, body string) {
	t.Helper()
	send(t, c, "POST", "/api/v1/schedules", body, 201)
}

// inZone returns a browser signed in as the first administrator of a site
// whose timezone is zone.
func inZone(t *testing.T, zone string) *testsupport.Browser {
	t.Helper()
	f := testsupport.NewFactory()
	f.EnvStub["DOORCODE_SITE_TIMEZONE"] = zone
	return signedInAdmin(t, f)
}

// occurrences is the occurrences the API lists for the schedule with id
// over the days query names.
func occurrences(t *testing.T, c *testsupport.Browser, id, query string) any {
	t.Helper()
	return testsupport.JSON(send(t, c, "GET", "/api/v1/schedules/"+id+"/occurrences?"+query, "", 200).Body)
}

func TestSchedulesStartWithTheBuiltInAlwaysSchedule(t *testing.T) {
	t.Parallel()
	got := signedInAdmin(t, testsupport.NewFactory()).Get("/api/v1/schedules")

	want := testsupport.Response{Status: 200, ContentType: jsonType, Body: `{"schedules":[` + alwaysJSON + `]}` + "\n"}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("response (-want +got):\n%s", diff)
	}
}

func TestCreatingAScheduleAnswersWithItTidied(t *testing.T) {
	t.Parallel()
	got := signedInAdmin(t, testsupport.NewFactory()).PostJSON("/api/v1/schedules", `{"name":" Night shift ",`+
		`"starts_on":"2026-01-01","ends_on":"2026-01-01",`+
		`"windows":[{"days":["FRI"," mon","mon"],"start":"22:00","end":"06:00"},{"days":["sun"],"start":" 7:05","end":"07:30"}],`+
		`"excluded_dates":["2026-12-25","2026-01-01"," ","2026-12-25"]}`)

	want := testsupport.Response{Status: 201, ContentType: jsonType, Body: `{"id":2,"name":"Night shift","starts_on":"2026-01-01","ends_on":"2026-01-01",` +
		`"windows":[{"days":["mon","fri"],"start":"22:00","end":"06:00"},{"days":["sun"],"start":"07:05","end":"07:30"}],` +
		`"excluded_dates":["2026-01-01","2026-12-25"],"builtin":false}` + "\n"}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("response (-want +got):\n%s", diff)
	}
}

func TestSchedulesAreListedByName(t *testing.T) {
	t.Parallel()
	b := signedInAdmin(t, testsupport.NewFactory())
	createSchedule(t, b, `{"name":"night","windows":[{"days":["mon"],"start":"22:00","end":"06:00"}]}`)
	createSchedule(t, b, `{"name":"Day","windows":[{"days":["mon"],"start":"09:00","end":"17:00"}]}`)

	got := testsupport.JSONAt(b.Get("/api/v1/schedules").Body, "/schedules")

	want := testsupport.JSON(`[` + alwaysJSON + `,
		{"id":3,"name":"Day","windows":[{"days":["mon"],"start":"09:00","end":"17:00"}],"excluded_dates":[],"builtin":false},
		{"id":2,"name":"night","windows":[{"days":["mon"],"start":"22:00","end":"06:00"}],"excluded_dates":[],"builtin":false}]`)
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("schedules (-want +got):\n%s", diff)
	}
}

func TestScheduleIsReadByID(t *testing.T) {
	t.Parallel()
	got := signedInAdmin(t, testsupport.NewFactory()).Get("/api/v1/schedules/1")

	want := testsupport.Response{Status: 200, ContentType: jsonType, Body: alwaysJSON + "\n"}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("response (-want +got):\n%s", diff)
	}
}

func TestUpdatingAScheduleReplacesIt(t *testing.T) {
	t.Parallel()
	b := signedInAdmin(t, testsupport.NewFactory())
	createSchedule(t, b, `{"name":"Night shift","starts_on":"2026-01-01","windows":[{"days":["mon"],"start":"22:00","end":"06:00"}],`+
		`"excluded_dates":["2026-12-25"]}`)

	updated := b.PutJSON("/api/v1/schedules/2", `{"name":"night shift","windows":[{"days":["sat","sun"],"start":"10:00","end":"16:00"}]}`)
	got := b.Get("/api/v1/schedules/2")

	want := `{"id":2,"name":"night shift","windows":[{"days":["sat","sun"],"start":"10:00","end":"16:00"}],"excluded_dates":[],"builtin":false}` + "\n"
	if diff := cmp.Diff([]any{200, want, want}, []any{updated.Status, updated.Body, got.Body}); diff != "" {
		t.Errorf("status, update, read back (-want +got):\n%s", diff)
	}
}

func TestScheduleFieldsAreChecked(t *testing.T) {
	t.Parallel()
	window := `{"days":["mon"],"start":"09:00","end":"17:00"}`
	cases := map[string]struct {
		body string
		want string
	}{
		"not json":         {`windows`, `body must be a JSON object with a string \"name\" and a \"windows\" array`},
		"no name":          {`{"name":" ","windows":[` + window + `]}`, "Enter a name."},
		"a long name":      {`{"name":"` + repeat("x", 101) + `","windows":[` + window + `]}`, "Use at most 100 characters."},
		"a taken name":     {`{"name":"ALWAYS","windows":[` + window + `]}`, "Another schedule has this name."},
		"a bad start date": {`{"name":"A","starts_on":"2026-02-30","windows":[` + window + `]}`, "Enter the date as YYYY-MM-DD, or leave it empty."},
		"a bad end date":   {`{"name":"A","ends_on":"soon","windows":[` + window + `]}`, "Enter the date as YYYY-MM-DD, or leave it empty."},
		"a backwards range": {`{"name":"A","starts_on":"2026-03-02","ends_on":"2026-03-01","windows":[` + window + `]}`,
			"End the range on or after the day it starts."},
		"no windows":       {`{"name":"A","windows":[]}`, "Add at least one window."},
		"too many windows": {`{"name":"A","windows":[` + repeat(window+",", 20) + window + `]}`, "Use at most 20 windows."},
		"a window without days": {`{"name":"A","windows":[{"days":[],"start":"09:00","end":"17:00"}]}`,
			"Choose at least one day for window 1."},
		"an unknown day": {`{"name":"A","windows":[{"days":["mon","moon"],"start":"09:00","end":"17:00"}]}`,
			"Choose days from mon, tue, wed, thu, fri, sat, sun for window 1."},
		"a bad start time": {`{"name":"A","windows":[{"days":["mon"],"start":"24:00","end":"17:00"}]}`,
			"Enter the start of window 1 as HH:MM."},
		"a missing end time": {`{"name":"A","windows":[` + window + `,{"days":["mon"],"start":"09:00"}]}`,
			"Enter the end of window 2 as HH:MM."},
		"a bad excluded date": {`{"name":"A","windows":[` + window + `],"excluded_dates":["2026-12-25","2026-13-01"]}`,
			"Enter excluded dates as YYYY-MM-DD; “2026-13-01” is not one."},
		"too many excluded dates": {`{"name":"A","windows":[` + window + `],"excluded_dates":[` + repeat(`"2026-12-25",`, 366) + `"2026-12-25"]}`,
			"Exclude at most 366 dates."},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			got := signedInAdmin(t, testsupport.NewFactory()).PostJSON("/api/v1/schedules", c.body)

			want := apiError(400, c.want)
			if diff := cmp.Diff(want, got); diff != "" {
				t.Errorf("response (-want +got):\n%s", diff)
			}
		})
	}
}

func TestScheduleAtTheLimitsIsAccepted(t *testing.T) {
	t.Parallel()
	window := `{"days":["mon"],"start":"09:00","end":"17:00"}`
	body := `{"name":"` + repeat("x", 100) + `","windows":[` + repeat(window+",", 19) + window + `],` +
		`"excluded_dates":[` + repeat(`"2026-12-25",`, 365) + `"2026-12-25"]}`

	got := signedInAdmin(t, testsupport.NewFactory()).PostJSON("/api/v1/schedules", body)

	if diff := cmp.Diff(201, got.Status); diff != "" {
		t.Errorf("status (-want +got):\n%s\n%s", diff, got.Body)
	}
}

func TestUpdatingAScheduleChecksItsFields(t *testing.T) {
	t.Parallel()
	cases := map[string]struct {
		body string
		want string
	}{
		"not json":     {`{"name":`, `body must be a JSON object with a string \"name\" and a \"windows\" array`},
		"a taken name": {`{"name":"always","windows":[{"days":["mon"],"start":"22:00","end":"06:00"}]}`, "Another schedule has this name."},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			b := signedInAdmin(t, testsupport.NewFactory())
			createSchedule(t, b, `{"name":"Nights","windows":[{"days":["mon"],"start":"22:00","end":"06:00"}]}`)

			got := b.PutJSON("/api/v1/schedules/2", c.body)

			want := apiError(400, c.want)
			if diff := cmp.Diff(want, got); diff != "" {
				t.Errorf("response (-want +got):\n%s", diff)
			}
		})
	}
}

func TestAlwaysScheduleCannotBeChangedOrDeleted(t *testing.T) {
	t.Parallel()
	b := signedInAdmin(t, testsupport.NewFactory())

	updated := b.PutJSON("/api/v1/schedules/1", `{"name":"Weekdays","windows":[{"days":["mon"],"start":"09:00","end":"17:00"}]}`)
	deleted := b.Delete("/api/v1/schedules/1")
	got := b.Get("/api/v1/schedules/1")

	refused := `{"error":"the Always schedule is built in and cannot be changed or deleted"}` + "\n"
	want := []any{409, refused, 409, refused, alwaysJSON + "\n"}
	if diff := cmp.Diff(want, []any{updated.Status, updated.Body, deleted.Status, deleted.Body, got.Body}); diff != "" {
		t.Errorf("update, delete, read back (-want +got):\n%s", diff)
	}
}

func TestDeletingAScheduleRemovesIt(t *testing.T) {
	t.Parallel()
	b := signedInAdmin(t, testsupport.NewFactory())
	createSchedule(t, b, `{"name":"Nights","windows":[{"days":["mon"],"start":"22:00","end":"06:00"}]}`)

	deleted := b.Delete("/api/v1/schedules/2")
	got := b.Get("/api/v1/schedules/2")

	want := []any{204, 404, `{"error":"no schedule has this id"}` + "\n"}
	if diff := cmp.Diff(want, []any{deleted.Status, got.Status, got.Body}); diff != "" {
		t.Errorf("delete, read back (-want +got):\n%s", diff)
	}
}

func TestScheduleThatDoesNotExistIsNotFound(t *testing.T) {
	t.Parallel()
	cases := map[string]func(b *testsupport.Browser) testsupport.Response{
		"read": func(b *testsupport.Browser) testsupport.Response { return b.Get("/api/v1/schedules/9") },
		"update": func(b *testsupport.Browser) testsupport.Response {
			return b.PutJSON("/api/v1/schedules/9", `{"name":"A"}`)
		},
		"delete": func(b *testsupport.Browser) testsupport.Response { return b.Delete("/api/v1/schedules/9") },
		"occurrences": func(b *testsupport.Browser) testsupport.Response {
			return b.Get("/api/v1/schedules/9/occurrences")
		},
	}
	for name, request := range cases {
		t.Run(name, func(t *testing.T) {
			got := request(signedInAdmin(t, testsupport.NewFactory()))

			want := apiError(404, "no schedule has this id")
			if diff := cmp.Diff(want, got); diff != "" {
				t.Errorf("response (-want +got):\n%s", diff)
			}
		})
	}
}

func TestScheduleCrossingMidnightRunsOvernightThroughTheDaylightSavingChange(t *testing.T) {
	t.Parallel()
	b := inZone(t, "America/New_York")
	createSchedule(t, b, `{"name":"Nights","windows":[{"days":["fri","sat"],"start":"22:00","end":"06:00"}]}`)

	got := occurrences(t, b, "2", "from=2026-03-06&to=2026-03-08")

	want := testsupport.JSON(`{"time_zone":"America/New_York","from":"2026-03-06","to":"2026-03-08","occurrences":[
		{"start":"2026-03-06T22:00:00-05:00","end":"2026-03-07T06:00:00-05:00"},
		{"start":"2026-03-07T22:00:00-05:00","end":"2026-03-08T06:00:00-04:00"}]}`)
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("occurrences (-want +got):\n%s", diff)
	}
}

func TestExcludedDateDropsTheWindowsThatOpenOnIt(t *testing.T) {
	t.Parallel()
	b := signedInAdmin(t, testsupport.NewFactory())
	createSchedule(t, b, `{"name":"Office","windows":[{"days":["mon","tue","wed","thu","fri"],"start":"09:00","end":"17:00"},`+
		`{"days":["tue"],"start":"22:00","end":"02:00"}],"excluded_dates":["2026-11-11"]}`)

	got := occurrences(t, b, "2", "from=2026-11-09&to=2026-11-15")

	want := testsupport.JSON(`{"time_zone":"UTC","from":"2026-11-09","to":"2026-11-15","occurrences":[
		{"start":"2026-11-09T09:00:00Z","end":"2026-11-09T17:00:00Z"},
		{"start":"2026-11-10T09:00:00Z","end":"2026-11-10T17:00:00Z"},
		{"start":"2026-11-10T22:00:00Z","end":"2026-11-11T02:00:00Z"},
		{"start":"2026-11-12T09:00:00Z","end":"2026-11-12T17:00:00Z"},
		{"start":"2026-11-13T09:00:00Z","end":"2026-11-13T17:00:00Z"}]}`)
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("occurrences (-want +got):\n%s", diff)
	}
}

func TestEffectiveRangeLimitsTheDaysWindowsOpenOn(t *testing.T) {
	t.Parallel()
	b := signedInAdmin(t, testsupport.NewFactory())
	createSchedule(t, b, `{"name":"Visit","starts_on":"2026-11-10","ends_on":"2026-11-12",`+
		`"windows":[{"days":["mon","tue","wed","thu","fri","sat","sun"],"start":"23:00","end":"01:00"}]}`)

	got := testsupport.JSONAt(send(t, b, "GET", "/api/v1/schedules/2/occurrences?from=2026-11-09&to=2026-11-15", "", 200).Body, "/occurrences")

	want := testsupport.JSON(`[
		{"start":"2026-11-10T23:00:00Z","end":"2026-11-11T01:00:00Z"},
		{"start":"2026-11-11T23:00:00Z","end":"2026-11-12T01:00:00Z"},
		{"start":"2026-11-12T23:00:00Z","end":"2026-11-13T01:00:00Z"}]`)
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("occurrences (-want +got):\n%s", diff)
	}
}

func TestAlwaysIsOneOccurrenceThroughTheDaylightSavingChange(t *testing.T) {
	t.Parallel()
	b := inZone(t, "America/New_York")

	got := occurrences(t, b, "1", "from=2026-10-31&to=2026-11-01")

	want := testsupport.JSON(`{"time_zone":"America/New_York","from":"2026-10-31","to":"2026-11-01","occurrences":[
		{"start":"2026-10-31T00:00:00-04:00","end":"2026-11-02T00:00:00-05:00"}]}`)
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("occurrences (-want +got):\n%s", diff)
	}
}

func TestTouchingAndOverlappingWindowsAreJoined(t *testing.T) {
	t.Parallel()
	b := signedInAdmin(t, testsupport.NewFactory())
	createSchedule(t, b, `{"name":"Split","windows":[{"days":["mon"],"start":"12:00","end":"15:00"},`+
		`{"days":["mon"],"start":"09:00","end":"12:00"},{"days":["mon"],"start":"10:00","end":"11:00"},`+
		`{"days":["mon"],"start":"16:00","end":"17:00"}]}`)

	got := testsupport.JSONAt(send(t, b, "GET", "/api/v1/schedules/2/occurrences?from=2026-11-09&to=2026-11-09", "", 200).Body, "/occurrences")

	want := testsupport.JSON(`[
		{"start":"2026-11-09T09:00:00Z","end":"2026-11-09T15:00:00Z"},
		{"start":"2026-11-09T16:00:00Z","end":"2026-11-09T17:00:00Z"}]`)
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("occurrences (-want +got):\n%s", diff)
	}
}

func TestOccurrencesRunningPastTheRangeAreCutAtIt(t *testing.T) {
	t.Parallel()
	b := signedInAdmin(t, testsupport.NewFactory())
	createSchedule(t, b, `{"name":"Late","windows":[{"days":["fri"],"start":"22:00","end":"06:00"},`+
		`{"days":["sat"],"start":"23:00","end":"01:00"}]}`)

	got := testsupport.JSONAt(send(t, b, "GET", "/api/v1/schedules/2/occurrences?from=2026-11-14&to=2026-11-14", "", 200).Body, "/occurrences")

	want := testsupport.JSON(`[
		{"start":"2026-11-14T00:00:00Z","end":"2026-11-14T06:00:00Z"},
		{"start":"2026-11-14T23:00:00Z","end":"2026-11-15T00:00:00Z"}]`)
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("occurrences (-want +got):\n%s", diff)
	}
}

func TestOccurrencesCoverTheCurrentWeekInTheSiteTimezoneByDefault(t *testing.T) {
	t.Parallel()
	cases := map[string]struct {
		zone, now, query string
		want             []any
	}{
		"on a Monday":             {"UTC", "2026-01-05T09:00:00Z", "", []any{"2026-01-05", "2026-01-11"}},
		"on a Sunday where it is": {"America/New_York", "2026-01-05T03:00:00Z", "", []any{"2025-12-29", "2026-01-04"}},
		"from a given day":        {"UTC", "2026-01-05T09:00:00Z", "from=2026-02-11", []any{"2026-02-11", "2026-02-17"}},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			f := testsupport.NewFactory()
			f.EnvStub["DOORCODE_SITE_TIMEZONE"] = c.zone
			f.ClockStub.Set(c.now)
			b := signedInAdmin(t, f)

			body := send(t, b, "GET", "/api/v1/schedules/1/occurrences?"+c.query, "", 200).Body

			got := []any{testsupport.JSONAt(body, "/from"), testsupport.JSONAt(body, "/to")}
			if diff := cmp.Diff(c.want, got); diff != "" {
				t.Errorf("from, to (-want +got):\n%s", diff)
			}
		})
	}
}

func TestOccurrencesAcceptOneDayToAYear(t *testing.T) {
	t.Parallel()
	queries := map[string]int{
		"one day":           200,
		"a year":            200,
		"over a year":       400,
		"a backwards range": 400,
		"a bad from":        400,
		"a bad to":          400,
	}
	targets := map[string]string{
		"one day":           "from=2026-03-01&to=2026-03-01",
		"a year":            "from=2026-01-01&to=2027-01-01",
		"over a year":       "from=2026-01-01&to=2027-01-02",
		"a backwards range": "from=2026-03-02&to=2026-03-01",
		"a bad from":        "from=March",
		"a bad to":          "to=2026-02-30",
	}
	for name, want := range queries {
		t.Run(name, func(t *testing.T) {
			got := signedInAdmin(t, testsupport.NewFactory()).Get("/api/v1/schedules/1/occurrences?" + targets[name])

			if diff := cmp.Diff(want, got.Status); diff != "" {
				t.Errorf("status (-want +got):\n%s\n%s", diff, got.Body)
			}
		})
	}
}

func TestOccurrencesOutsideTheirRangeSayWhatIsAccepted(t *testing.T) {
	t.Parallel()
	got := signedInAdmin(t, testsupport.NewFactory()).Get("/api/v1/schedules/1/occurrences?from=2026-03-02&to=2026-03-01")

	want := apiError(400, "from and to must be dates as YYYY-MM-DD, with to on or after from and at most 365 days after it")
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("response (-want +got):\n%s", diff)
	}
}

func TestSchedulesAPIStoreFailureAnswersWithARequestID(t *testing.T) {
	t.Parallel()
	body := `{"name":"Nights","windows":[{"days":["mon"],"start":"22:00","end":"06:00"}]}`
	other := `{"name":"Days","windows":[{"days":["mon"],"start":"09:00","end":"17:00"}]}`
	cases := map[string]struct {
		fail   func(s *testsupport.Store)
		method string
		target string
		body   string
	}{
		"list":                   {(*testsupport.Store).FailSchedules, "GET", "/api/v1/schedules", ""},
		"read":                   {(*testsupport.Store).FailSchedules, "GET", "/api/v1/schedules/1", ""},
		"create":                 {(*testsupport.Store).FailSchedules, "POST", "/api/v1/schedules", other},
		"store a new one":        {(*testsupport.Store).FailScheduleWrites, "POST", "/api/v1/schedules", other},
		"update":                 {(*testsupport.Store).FailSchedules, "PUT", "/api/v1/schedules/2", body},
		"store a change":         {(*testsupport.Store).FailScheduleWrites, "PUT", "/api/v1/schedules/2", body},
		"delete":                 {(*testsupport.Store).FailSchedules, "DELETE", "/api/v1/schedules/2", ""},
		"remove one":             {(*testsupport.Store).FailScheduleWrites, "DELETE", "/api/v1/schedules/2", ""},
		"occurrences":            {(*testsupport.Store).FailSchedules, "GET", "/api/v1/schedules/2/occurrences", ""},
		"occurrences' time zone": {(*testsupport.Store).FailSettings, "GET", "/api/v1/schedules/2/occurrences", ""},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			f := testsupport.NewFactory()
			b := signedInAdmin(t, f)
			createSchedule(t, b, body)
			c.fail(f.StoreStub)

			got := b.Send(c.method, c.target, c.body)

			want := internalError("4243444546474849")
			if diff := cmp.Diff(want, got); diff != "" {
				t.Errorf("response (-want +got):\n%s", diff)
			}
		})
	}
}

func TestSchedulesAPIRefusesAUserWithoutTheRole(t *testing.T) {
	t.Parallel()
	_, b := adminHolding(t, testsupport.NewFactory(), `["manage-doors"]`)

	got := b.Get("/api/v1/schedules")

	want := apiError(403, "the manage-schedules role is required")
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("response (-want +got):\n%s", diff)
	}
}

func TestDeletingAScheduleADoorIsInOpenModeDuringIsRefused(t *testing.T) {
	t.Parallel()
	b := doorSite(t, testsupport.NewFactory())
	createDoor(t, b, `{"name":"Lobby","open_schedule_ids":[2]}`)
	createDoor(t, b, `{"name":"Atrium","open_schedule_ids":[2,3]}`)

	got := b.Delete("/api/v1/schedules/2")

	want := apiError(409, "doors are in open mode during this schedule: Atrium, Lobby")
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("response (-want +got):\n%s", diff)
	}
}

func TestDeletingAScheduleTakesItOffDeletedDoors(t *testing.T) {
	t.Parallel()
	b := doorSite(t, testsupport.NewFactory())
	createDoor(t, b, `{"name":"Lobby","open_schedule_ids":[2,3]}`)
	send(t, b, "DELETE", "/api/v1/doors/1", "", 204)

	deleted := b.Delete("/api/v1/schedules/2")
	got := testsupport.JSONAt(b.Get("/api/v1/doors/1").Body, "/open_schedules")

	if diff := cmp.Diff([]any{204, testsupport.JSON(`[{"id":3,"name":"Nights"}]`)}, []any{deleted.Status, got}); diff != "" {
		t.Errorf("status, the door's open schedules (-want +got):\n%s", diff)
	}
}

func TestDeletingAScheduleAnswersWithARequestIDWhenDoorsCannotBeRead(t *testing.T) {
	t.Parallel()
	f := testsupport.NewFactory()
	b := doorSite(t, f)
	f.StoreStub.FailDoors()

	got := b.Delete("/api/v1/schedules/2")

	want := internalError("4243444546474849")
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("response (-want +got):\n%s", diff)
	}
}
