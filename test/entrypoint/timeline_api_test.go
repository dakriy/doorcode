package entrypoint_test

import (
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/dakriy/doorcode/internal/testsupport"
)

// timeline is the spans of the timeline the API answers with for the door
// with id over the days query names.
func timeline(t *testing.T, c *testsupport.Browser, door, query string) any {
	t.Helper()
	return testsupport.JSONAt(send(t, c, "GET", "/api/v1/doors/"+door+"/timeline?"+query, "", 200).Body, "/spans")
}

// monday is the query for the stub clock's day, Monday 5 January 2026.
const monday = "from=2026-01-05&to=2026-01-05"

func TestALockedOverrideCutsAHoleInAnOpenModeWindow(t *testing.T) {
	t.Parallel()
	b := doorSite(t, testsupport.NewFactory())
	send(t, b, "PUT", "/api/v1/zones/1", `{"name":"North","open_schedule_ids":[2]}`, 200)
	createDoor(t, b, `{"name":"Lobby","zone_ids":[1]}`)
	createOverride(t, b, "1", `{"kind":"locked","start":"2026-01-05T12:00:00Z","end":"2026-01-05T13:00:00Z"}`)

	got := timeline(t, b, "1", monday)

	want := testsupport.JSON(`[
		{"start":"2026-01-05T08:00:00Z","end":"2026-01-05T12:00:00Z","state":"open"},
		{"start":"2026-01-05T12:00:00Z","end":"2026-01-05T13:00:00Z","state":"locked"},
		{"start":"2026-01-05T13:00:00Z","end":"2026-01-05T18:00:00Z","state":"open"}]`)
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("spans (-want +got):\n%s", diff)
	}
}

func TestAnOpenOverrideExtendsOpenModeOverACodeAccessPeriod(t *testing.T) {
	t.Parallel()
	b := ruleSite(t, testsupport.NewFactory())
	send(t, b, "PUT", "/api/v1/zones/1", `{"name":"North","door_ids":[1],"open_schedule_ids":[2]}`, 200)
	createRule(t, b, `{"group_id":2,"schedule_id":1,"zone_ids":[1]}`)
	createOverride(t, b, "1", `{"kind":"open","start":"2026-01-05T17:00:00Z","end":"2026-01-05T20:00:00Z"}`)

	got := timeline(t, b, "1", monday)

	want := testsupport.JSON(`[{"start":"2026-01-05T08:00:00Z","end":"2026-01-05T20:00:00Z","state":"open"}]`)
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("spans (-want +got):\n%s", diff)
	}
}

func TestTimelineMergesOverridesWithLockedWinning(t *testing.T) {
	t.Parallel()
	cases := map[string]struct {
		overrides []string
		want      string
	}{
		"locked inside open": {
			[]string{`{"kind":"open","start":"2026-01-05T09:00:00Z","end":"2026-01-05T15:00:00Z"}`, `{"kind":"locked","start":"2026-01-05T10:00:00Z","end":"2026-01-05T11:00:00Z"}`},
			`[{"start":"2026-01-05T09:00:00Z","end":"2026-01-05T10:00:00Z","state":"open"},` +
				`{"start":"2026-01-05T10:00:00Z","end":"2026-01-05T11:00:00Z","state":"locked"},` +
				`{"start":"2026-01-05T11:00:00Z","end":"2026-01-05T15:00:00Z","state":"open"}]`,
		},
		"locked touching locked": {
			[]string{`{"kind":"locked","start":"2026-01-05T09:30:00Z","end":"2026-01-05T10:00:00Z"}`, `{"kind":"locked","start":"2026-01-05T10:00:00Z","end":"2026-01-05T11:00:00Z"}`},
			`[{"start":"2026-01-05T09:30:00Z","end":"2026-01-05T11:00:00Z","state":"locked"}]`,
		},
		"open apart from open": {
			[]string{`{"kind":"open","start":"2026-01-05T09:30:00Z","end":"2026-01-05T10:00:00Z"}`, `{"kind":"open","start":"2026-01-05T10:01:00Z","end":"2026-01-05T11:00:00Z"}`},
			`[{"start":"2026-01-05T09:30:00Z","end":"2026-01-05T10:00:00Z","state":"open"},{"start":"2026-01-05T10:01:00Z","end":"2026-01-05T11:00:00Z","state":"open"}]`,
		},
		"across the range's edges": {
			[]string{`{"kind":"locked","start":"2026-01-04T20:00:00Z","end":"2026-01-05T09:30:00Z"}`, `{"kind":"open","start":"2026-01-05T23:00:00Z","end":"2026-01-06T02:00:00Z"}`},
			`[{"start":"2026-01-05T00:00:00Z","end":"2026-01-05T09:30:00Z","state":"locked"},{"start":"2026-01-05T23:00:00Z","end":"2026-01-06T00:00:00Z","state":"open"}]`,
		},
		"one after the range": {
			[]string{`{"kind":"locked","start":"2026-01-06T00:00:00Z","end":"2026-01-06T02:00:00Z"}`, `{"kind":"open","start":"2026-01-05T23:00:00Z","end":"2026-01-05T23:30:00Z"}`},
			`[{"start":"2026-01-05T23:00:00Z","end":"2026-01-05T23:30:00Z","state":"open"}]`,
		},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			b := signedInAdmin(t, testsupport.NewFactory())
			createDoor(t, b, frontDoor)
			for _, o := range c.overrides {
				createOverride(t, b, "1", o)
			}

			got := timeline(t, b, "1", monday)

			if diff := cmp.Diff(testsupport.JSON(c.want), got); diff != "" {
				t.Errorf("spans (-want +got):\n%s", diff)
			}
		})
	}
}

func TestTimelineJoinsTheOpenModeOfEveryZoneTheDoorIsIn(t *testing.T) {
	t.Parallel()
	b := doorSite(t, testsupport.NewFactory())
	send(t, b, "PUT", "/api/v1/zones/1", `{"name":"North","open_schedule_ids":[3]}`, 200)
	send(t, b, "PUT", "/api/v1/zones/2", `{"name":"Annex","open_schedule_ids":[2]}`, 200)
	createZone(t, b, `{"name":"Lounge","open_schedule_ids":[1]}`)
	createDoor(t, b, `{"name":"Lobby","zone_ids":[1,2]}`)
	createDoor(t, b, `{"name":"Dock","zone_ids":[3,2]}`)

	got := []any{timeline(t, b, "1", "from=2026-01-06&to=2026-01-06"), timeline(t, b, "2", "from=2026-01-06&to=2026-01-06")}

	want := []any{
		testsupport.JSON(`[{"start":"2026-01-06T00:00:00Z","end":"2026-01-06T06:00:00Z","state":"open"},` +
			`{"start":"2026-01-06T08:00:00Z","end":"2026-01-06T18:00:00Z","state":"open"}]`),
		testsupport.JSON(`[{"start":"2026-01-06T00:00:00Z","end":"2026-01-07T00:00:00Z","state":"open"}]`),
	}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("spans of Lobby, Dock (-want +got):\n%s", diff)
	}
}

func TestTimelineIsInTheSiteTimezoneAndCoversThisWeekByDefault(t *testing.T) {
	t.Parallel()
	b := inZone(t, "America/New_York")
	createSchedule(t, b, `{"name":"Nights","windows":[{"days":["sun"],"start":"22:00","end":"06:00"}]}`)
	createZone(t, b, `{"name":"North","open_schedule_ids":[2]}`)
	createDoor(t, b, `{"name":"Lobby","zone_ids":[1]}`)

	got := testsupport.JSON(b.Get("/api/v1/doors/1/timeline").Body)

	want := testsupport.JSON(`{"time_zone":"America/New_York","from":"2026-01-05","to":"2026-01-11","spans":[` +
		`{"start":"2026-01-05T00:00:00-05:00","end":"2026-01-05T06:00:00-05:00","state":"open"},` +
		`{"start":"2026-01-11T22:00:00-05:00","end":"2026-01-12T00:00:00-05:00","state":"open"}]}`)
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("timeline (-want +got):\n%s", diff)
	}
}

func TestTimelineRefusesWhatItCannotAnswer(t *testing.T) {
	t.Parallel()
	b := signedInAdmin(t, testsupport.NewFactory())
	createDoor(t, b, frontDoor)

	got := []any{b.Get("/api/v1/doors/1/timeline?from=2026-01-05&to=2026-01-04").Status, b.Get("/api/v1/doors/9/timeline").Status}

	if diff := cmp.Diff([]any{400, 404}, got); diff != "" {
		t.Errorf("a backwards range, no door (-want +got):\n%s", diff)
	}
}

// staffAnn is ruleSite with Ann (2) a member of Staff (2).
func staffAnn(t *testing.T, f *testsupport.Factory) *testsupport.Browser {
	t.Helper()
	b := ruleSite(t, f)
	createUser(t, b, ann)
	join(t, b, "2", "2")
	return b
}

func TestARuleThroughAZoneLetsTheUsersCodeWorkWhenTheDoorIsNotLocked(t *testing.T) {
	t.Parallel()
	b := staffAnn(t, testsupport.NewFactory())
	createRule(t, b, `{"group_id":2,"schedule_id":2,"zone_ids":[1]}`)
	createRule(t, b, `{"group_id":2,"schedule_id":3,"zone_ids":[1]}`)
	createRule(t, b, `{"group_id":3,"schedule_id":1,"zone_ids":[2]}`)
	createOverride(t, b, "1", `{"kind":"locked","start":"2026-01-05T12:00:00Z","end":"2026-01-05T13:00:00Z"}`)
	createOverride(t, b, "1", `{"kind":"open","start":"2026-01-05T19:00:00Z","end":"2026-01-05T20:00:00Z"}`)
	send(t, b, "DELETE", "/api/v1/doors/3", "", 204)

	got := testsupport.JSON(b.Get("/api/v1/users/2/access?" + monday).Body)

	want := testsupport.JSON(`{"time_zone":"UTC","from":"2026-01-05","to":"2026-01-05","doors":[{"door":{"id":1,"name":"Lobby"},"rule_ids":[1,2],"works":[` +
		`{"start":"2026-01-05T08:00:00Z","end":"2026-01-05T12:00:00Z"},` +
		`{"start":"2026-01-05T13:00:00Z","end":"2026-01-05T18:00:00Z"},` +
		`{"start":"2026-01-05T19:00:00Z","end":"2026-01-05T20:00:00Z"},` +
		`{"start":"2026-01-05T22:00:00Z","end":"2026-01-06T00:00:00Z"}]}]}`)
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("access (-want +got):\n%s", diff)
	}
}

func TestUserAccessRefusesWhatItCannotAnswer(t *testing.T) {
	t.Parallel()
	b := signedInAdmin(t, testsupport.NewFactory())

	got := []any{b.Get("/api/v1/users/1/access?from=soon").Status, b.Get("/api/v1/users/9/access").Status}

	if diff := cmp.Diff([]any{400, 404}, got); diff != "" {
		t.Errorf("not a date, no user (-want +got):\n%s", diff)
	}
}

func TestTimelineAndAccessAnswerWithARequestIDWhenTheStoreFails(t *testing.T) {
	t.Parallel()
	cases := map[string]struct {
		fail   func(s *testsupport.Store)
		target string
	}{
		"a timeline's door":      {(*testsupport.Store).FailDoors, "/api/v1/doors/1/timeline"},
		"a timeline's zones":     {(*testsupport.Store).FailZones, "/api/v1/doors/1/timeline"},
		"a timeline's schedules": {(*testsupport.Store).FailSchedules, "/api/v1/doors/1/timeline"},
		"a timeline's overrides": {(*testsupport.Store).FailOverrides, "/api/v1/doors/1/timeline"},
		"a timeline's time zone": {(*testsupport.Store).FailSettings, "/api/v1/doors/1/timeline"},
		"access's user":          {(*testsupport.Store).FailUsers, "/api/v1/users/2/access"},
		"access's rules":         {(*testsupport.Store).FailRules, "/api/v1/users/2/access"},
		"access's schedules":     {(*testsupport.Store).FailSchedules, "/api/v1/users/2/access"},
		"access's zones":         {(*testsupport.Store).FailZones, "/api/v1/users/2/access"},
		"access's overrides":     {(*testsupport.Store).FailOverrides, "/api/v1/users/2/access"},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			f := testsupport.NewFactory()
			b := staffAnn(t, f)
			createRule(t, b, `{"group_id":2,"schedule_id":1,"zone_ids":[1]}`)
			createOverride(t, b, "1", lockedToday)
			c.fail(f.StoreStub)

			got := b.Get(c.target)

			if diff := cmp.Diff([]any{500, "internal error"}, []any{got.Status, testsupport.JSONAt(got.Body, "/error")}); diff != "" {
				t.Errorf("status, error (-want +got):\n%s", diff)
			}
		})
	}
}
