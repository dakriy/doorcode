package entrypoint_test

import (
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/dakriy/doorcode/internal/testsupport"
)

// ruleSite is doorSite with the doors Lobby (1) in North, Dock (2) in
// Annex, and Attic (3) in no zone, and the groups Staff (2) and Guests (3).
func ruleSite(t *testing.T, f *testsupport.Factory) *testsupport.Browser {
	t.Helper()
	return furnishRuleSite(t, doorSite(t, f))
}

// furnishRuleSite gives b's doorSite what ruleSite has, and returns b.
func furnishRuleSite(t *testing.T, b *testsupport.Browser) *testsupport.Browser {
	t.Helper()
	createDoor(t, b, `{"name":"Lobby","zone_ids":[1]}`)
	createDoor(t, b, `{"name":"Dock","zone_ids":[2]}`)
	createDoor(t, b, `{"name":"Attic"}`)
	createGroup(t, b, `{"slug":"staff","name":"Staff"}`)
	createGroup(t, b, `{"slug":"guests","name":"Guests"}`)
	return b
}

// createRule creates an access rule through the API as c's user. Rules are
// numbered from 1.
func createRule(t *testing.T, c *testsupport.Browser, body string) {
	t.Helper()
	send(t, c, "POST", "/api/v1/access-rules", body, 201)
}

func TestCreatingAnAccessRuleAnswersWithWhatItNames(t *testing.T) {
	t.Parallel()
	b := ruleSite(t, testsupport.NewFactory())

	got := b.PostJSON("/api/v1/access-rules", `{"group_id":2,"schedule_id":2,"door_ids":[3,1,3],"zone_ids":[2,1,2]}`)

	want := testsupport.Response{Status: 201, ContentType: jsonType, Body: `{"id":1,"group":{"id":2,"name":"Staff"},` +
		`"schedule":{"id":2,"name":"Weekdays"},"doors":[{"id":3,"name":"Attic"},{"id":1,"name":"Lobby"}],` +
		`"zones":[{"id":2,"name":"Annex"},{"id":1,"name":"North"}]}` + "\n"}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("response (-want +got):\n%s", diff)
	}
}

func TestAccessRulesAreListedOldestFirstAndByGroup(t *testing.T) {
	t.Parallel()
	b := ruleSite(t, testsupport.NewFactory())
	createRule(t, b, `{"group_id":3,"schedule_id":1,"zone_ids":[1]}`)
	createRule(t, b, `{"group_id":2,"schedule_id":3,"door_ids":[2]}`)

	all := testsupport.JSON(b.Get("/api/v1/access-rules").Body)
	staff := testsupport.JSON(b.Get("/api/v1/access-rules?group=2").Body)

	guests := `{"id":1,"group":{"id":3,"name":"Guests"},"schedule":{"id":1,"name":"Always"},"doors":[],"zones":[{"id":1,"name":"North"}]}`
	staffRule := `{"id":2,"group":{"id":2,"name":"Staff"},"schedule":{"id":3,"name":"Nights"},"doors":[{"id":2,"name":"Dock"}],"zones":[]}`
	want := []any{testsupport.JSON(`{"rules":[` + guests + `,` + staffRule + `]}`), testsupport.JSON(`{"rules":[` + staffRule + `]}`)}
	if diff := cmp.Diff(want, []any{all, staff}); diff != "" {
		t.Errorf("every rule, Staff's rules (-want +got):\n%s", diff)
	}
}

func TestListingAccessRulesRefusesAGroupThatIsNotAnID(t *testing.T) {
	t.Parallel()
	for _, query := range []string{"?group=staff", "?group=0"} {
		t.Run(query, func(t *testing.T) {
			got := signedInAdmin(t, testsupport.NewFactory()).Get("/api/v1/access-rules" + query)

			want := apiError(400, "group must be a group's id")
			if diff := cmp.Diff(want, got); diff != "" {
				t.Errorf("response (-want +got):\n%s", diff)
			}
		})
	}
}

func TestUpdatingAnAccessRuleReplacesWhatItNames(t *testing.T) {
	t.Parallel()
	b := ruleSite(t, testsupport.NewFactory())
	createRule(t, b, `{"group_id":2,"schedule_id":2,"door_ids":[1],"zone_ids":[1]}`)

	updated := b.PutJSON("/api/v1/access-rules/1", `{"group_id":3,"schedule_id":3,"zone_ids":[2]}`)
	got := b.Get("/api/v1/access-rules/1")

	want := `{"id":1,"group":{"id":3,"name":"Guests"},"schedule":{"id":3,"name":"Nights"},"doors":[],"zones":[{"id":2,"name":"Annex"}]}`
	if diff := cmp.Diff([]any{200, testsupport.JSON(want), testsupport.JSON(want)},
		[]any{updated.Status, testsupport.JSON(updated.Body), testsupport.JSON(got.Body)}); diff != "" {
		t.Errorf("status, update, read back (-want +got):\n%s", diff)
	}
}

func TestAccessRuleFieldsAreChecked(t *testing.T) {
	t.Parallel()
	cases := map[string]struct {
		body string
		want string
	}{
		"not JSON":            {`[`, `body must be a JSON object with a \"group_id\" and a \"schedule_id\"`},
		"an unknown group":    {`{"group_id":9,"schedule_id":1,"door_ids":[1]}`, "No group has id 9."},
		"no schedule":         {`{"group_id":2,"door_ids":[1]}`, "Choose a schedule."},
		"an unknown schedule": {`{"group_id":2,"schedule_id":9,"door_ids":[1]}`, "No schedule has id 9."},
		"no door or zone":     {`{"group_id":2,"schedule_id":1}`, "Choose at least one door or zone."},
		"an unknown door":     {`{"group_id":2,"schedule_id":1,"door_ids":[1,9]}`, "No door has id 9."},
		"an unknown zone":     {`{"group_id":2,"schedule_id":1,"zone_ids":[9]}`, "No zone has id 9."},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			b := ruleSite(t, testsupport.NewFactory())
			createRule(t, b, `{"group_id":2,"schedule_id":1,"door_ids":[1]}`)

			created := b.PostJSON("/api/v1/access-rules", c.body)
			updated := b.PutJSON("/api/v1/access-rules/1", c.body)

			want := apiError(400, c.want)
			if diff := cmp.Diff([]testsupport.Response{want, want}, []testsupport.Response{created, updated}); diff != "" {
				t.Errorf("create, update (-want +got):\n%s", diff)
			}
		})
	}
}

func TestDeletingAnAccessRule(t *testing.T) {
	t.Parallel()
	b := ruleSite(t, testsupport.NewFactory())
	createRule(t, b, `{"group_id":2,"schedule_id":1,"door_ids":[1]}`)

	deleted := b.Delete("/api/v1/access-rules/1")
	got := testsupport.JSON(b.Get("/api/v1/access-rules").Body)

	if diff := cmp.Diff([]any{204, testsupport.JSON(`{"rules":[]}`)}, []any{deleted.Status, got}); diff != "" {
		t.Errorf("status, rules (-want +got):\n%s", diff)
	}
}

func TestAnAccessRuleThatIsNotThereIsNotFound(t *testing.T) {
	t.Parallel()
	b := ruleSite(t, testsupport.NewFactory())

	got := []testsupport.Response{
		b.Get("/api/v1/access-rules/9"),
		b.PutJSON("/api/v1/access-rules/9", `{"group_id":2,"schedule_id":1,"door_ids":[1]}`),
		b.Delete("/api/v1/access-rules/9"),
	}

	missing := apiError(404, "no access rule has this id")
	if diff := cmp.Diff([]testsupport.Response{missing, missing, missing}, got); diff != "" {
		t.Errorf("read, update, delete (-want +got):\n%s", diff)
	}
}

func TestAnAccessRuleGoesWithItsGroupAndLosesADeletedZone(t *testing.T) {
	t.Parallel()
	b := ruleSite(t, testsupport.NewFactory())
	createRule(t, b, `{"group_id":2,"schedule_id":1,"zone_ids":[1,2]}`)
	createRule(t, b, `{"group_id":3,"schedule_id":1,"door_ids":[1]}`)

	send(t, b, "DELETE", "/api/v1/zones/1", "", 204)
	send(t, b, "DELETE", "/api/v1/groups/3", "", 204)
	got := testsupport.JSON(b.Get("/api/v1/access-rules").Body)

	want := `{"rules":[{"id":1,"group":{"id":2,"name":"Staff"},"schedule":{"id":1,"name":"Always"},"doors":[],"zones":[{"id":2,"name":"Annex"}]}]}`
	if diff := cmp.Diff(testsupport.JSON(want), got); diff != "" {
		t.Errorf("rules (-want +got):\n%s", diff)
	}
}

func TestDeletingAScheduleAnAccessRuleUsesIsRefused(t *testing.T) {
	t.Parallel()
	b := ruleSite(t, testsupport.NewFactory())
	createRule(t, b, `{"group_id":3,"schedule_id":2,"door_ids":[1]}`)
	createRule(t, b, `{"group_id":2,"schedule_id":2,"door_ids":[2]}`)
	createRule(t, b, `{"group_id":2,"schedule_id":2,"door_ids":[3]}`)
	createRule(t, b, `{"group_id":2,"schedule_id":3,"door_ids":[3]}`)
	createDoor(t, b, `{"name":"Gate","open_schedule_ids":[2]}`)

	refused := b.Delete("/api/v1/schedules/2")
	unused := b.Delete("/api/v1/schedules/3")
	send(t, b, "DELETE", "/api/v1/access-rules/4", "", 204)
	freed := b.Delete("/api/v1/schedules/3")

	want := []testsupport.Response{
		apiError(409, "doors are in open mode during this schedule: Gate; access rules of these groups use this schedule: Guests, Staff"),
		apiError(409, "access rules of these groups use this schedule: Staff"),
		{Status: 204},
	}
	if diff := cmp.Diff(want, []testsupport.Response{refused, unused, freed}); diff != "" {
		t.Errorf("deleting a schedule two groups' rules use, one a rule uses, one no longer used (-want +got):\n%s", diff)
	}
}

func TestARuleThroughAZoneReachesTheDoorsInIt(t *testing.T) {
	t.Parallel()
	b := ruleSite(t, testsupport.NewFactory())
	createRule(t, b, `{"group_id":2,"schedule_id":2,"zone_ids":[1]}`)
	createRule(t, b, `{"group_id":3,"schedule_id":3,"door_ids":[1,2],"zone_ids":[2]}`)
	createRule(t, b, `{"group_id":2,"schedule_id":1,"door_ids":[3]}`)
	send(t, b, "PUT", "/api/v1/doors/1", `{"name":"Lobby","zone_ids":[1,2]}`, 200)

	lobby := testsupport.JSON(b.Get("/api/v1/doors/1/entrants").Body)
	dock := testsupport.JSON(b.Get("/api/v1/doors/2/entrants").Body)

	want := []any{
		testsupport.JSON(`{"entrants":[` +
			`{"rule_id":1,"group":{"id":2,"name":"Staff"},"schedule":{"id":2,"name":"Weekdays"},"direct":false,"zones":[{"id":1,"name":"North"}]},` +
			`{"rule_id":2,"group":{"id":3,"name":"Guests"},"schedule":{"id":3,"name":"Nights"},"direct":true,"zones":[{"id":2,"name":"Annex"}]}]}`),
		testsupport.JSON(`{"entrants":[` +
			`{"rule_id":2,"group":{"id":3,"name":"Guests"},"schedule":{"id":3,"name":"Nights"},"direct":true,"zones":[{"id":2,"name":"Annex"}]}]}`),
	}
	if diff := cmp.Diff(want, []any{lobby, dock}); diff != "" {
		t.Errorf("entrants of Lobby, Dock (-want +got):\n%s", diff)
	}
}

func TestEntrantsOfADoorThatIsNotThereAreNotFound(t *testing.T) {
	t.Parallel()
	got := signedInAdmin(t, testsupport.NewFactory()).Get("/api/v1/doors/9/entrants")

	want := apiError(404, "no door has this id")
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("response (-want +got):\n%s", diff)
	}
}

func TestAccessRulesAPIAsksForTheRole(t *testing.T) {
	t.Parallel()
	_, b := adminHolding(t, testsupport.NewFactory(), `["manage-doors"]`)

	got := b.Get("/api/v1/access-rules")

	want := apiError(403, "the manage-groups role is required")
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("response (-want +got):\n%s", diff)
	}
}

func TestAccessRulesAPIAnswersWithARequestIDWhenTheStoreFails(t *testing.T) {
	t.Parallel()
	cases := map[string]struct {
		fail   func(s *testsupport.Store)
		method string
		target string
		body   string
	}{
		"listing":                     {(*testsupport.Store).FailRules, "GET", "/api/v1/access-rules", ""},
		"listing's names":             {(*testsupport.Store).FailGroups, "GET", "/api/v1/access-rules", ""},
		"reading":                     {(*testsupport.Store).FailRules, "GET", "/api/v1/access-rules/1", ""},
		"reading's schedule names":    {(*testsupport.Store).FailSchedules, "GET", "/api/v1/access-rules/1", ""},
		"reading's door names":        {(*testsupport.Store).FailDoors, "GET", "/api/v1/access-rules/1", ""},
		"creating":                    {(*testsupport.Store).FailRuleWrites, "POST", "/api/v1/access-rules", `{"group_id":2,"schedule_id":1,"door_ids":[1]}`},
		"checking a new rule's zones": {(*testsupport.Store).FailZones, "POST", "/api/v1/access-rules", `{"group_id":2,"schedule_id":1,"door_ids":[1]}`},
		"updating":                    {(*testsupport.Store).FailRuleWrites, "PUT", "/api/v1/access-rules/1", `{"group_id":2,"schedule_id":1,"door_ids":[1]}`},
		"deleting":                    {(*testsupport.Store).FailRuleWrites, "DELETE", "/api/v1/access-rules/1", ""},
		"listing entrants":            {(*testsupport.Store).FailRules, "GET", "/api/v1/doors/1/entrants", ""},
		"naming entrants":             {(*testsupport.Store).FailGroups, "GET", "/api/v1/doors/1/entrants", ""},
		"a schedule's users":          {(*testsupport.Store).FailRules, "DELETE", "/api/v1/schedules/2", ""},
		"a schedule's groups":         {(*testsupport.Store).FailGroups, "DELETE", "/api/v1/schedules/2", ""},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			f := testsupport.NewFactory()
			b := ruleSite(t, f)
			createRule(t, b, `{"group_id":2,"schedule_id":3,"door_ids":[1]}`)
			c.fail(f.StoreStub)

			got := b.Send(c.method, c.target, c.body)

			if diff := cmp.Diff([]any{500, "internal error"}, []any{got.Status, testsupport.JSONAt(got.Body, "/error")}); diff != "" {
				t.Errorf("status, error (-want +got):\n%s", diff)
			}
		})
	}
}
