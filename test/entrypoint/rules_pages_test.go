package entrypoint_test

import (
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/dakriy/doorcode/internal/testsupport"
)

// rulesHead is the header row of a group's access rules.
var rulesHead = []string{"Schedule", "Doors", "Zones", "Actions"}

// noRules is a group's access rules when it has none.
var noRules = [][]string{rulesHead, {"No access rules yet, so members cannot open any door with their code."}}

func TestGroupPageListsItsAccessRules(t *testing.T) {
	t.Parallel()
	b := ruleSite(t, testsupport.NewFactory())
	createRule(t, b, `{"group_id":2,"schedule_id":2,"door_ids":[3,1],"zone_ids":[1]}`)
	createRule(t, b, `{"group_id":3,"schedule_id":1,"door_ids":[2]}`)
	createRule(t, b, `{"group_id":2,"schedule_id":3,"zone_ids":[2,1]}`)

	got := pageOf(b.Get("/admin/groups/2")).Tables["Access rules"]

	want := [][]string{rulesHead,
		{"Weekdays", "Attic, Lobby", "North", "Edit Delete"},
		{"Nights", "None", "Annex, North", "Edit Delete"},
	}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("access rules (-want +got):\n%s", diff)
	}
}

func TestNewAccessRulePageOffersEveryScheduleDoorAndZone(t *testing.T) {
	t.Parallel()
	b := ruleSite(t, testsupport.NewFactory())
	send(t, b, "DELETE", "/api/v1/doors/3", "", 204)
	b.Get("/")

	got := testsupport.ParsePage(b.Get("/admin/groups/2/rules/new").Body)

	want := []any{"New access rule · doorcode", "New access rule",
		map[string][]string{"schedule_id": {"Choose a schedule (selected)", "Always", "Nights", "Weekdays"}},
		map[string]string{"door_ids=2": "Dock", "door_ids=1": "Lobby", "zone_ids=2": "Annex", "zone_ids=1": "North"},
		[]string{"schedule_id", "door_ids", "zone_ids"},
	}
	if diff := cmp.Diff(want, []any{got.Title, got.Heading, got.Options, got.Choices, got.Comboboxes}); diff != "" {
		t.Errorf("title, heading, schedules, choices, searchable selects (-want +got):\n%s", diff)
	}
}

func TestCreatingAnAccessRuleFromTheFormListsItOnTheGroupPage(t *testing.T) {
	t.Parallel()
	b := ruleSite(t, testsupport.NewFactory())
	b.Get("/")

	created := b.Post("/admin/groups/2/rules", "schedule_id", "3", "door_ids", "2", "zone_ids", "1")
	got := pageOf(b.Get(created.Location))

	want := []any{303, "/admin/groups/2", []string{"The access rule has been created."}, [][]string{rulesHead, {"Nights", "Dock", "North", "Edit Delete"}}}
	if diff := cmp.Diff(want, []any{created.Status, created.Location, got.Messages, got.Tables["Access rules"]}); diff != "" {
		t.Errorf("redirect, messages, rules (-want +got):\n%s", diff)
	}
}

func TestAccessRuleFormSaysWhatIsWrongAndKeepsWhatWasChosen(t *testing.T) {
	t.Parallel()
	for name, target := range map[string]string{"new": "/admin/groups/2/rules", "existing": "/admin/groups/2/rules/1"} {
		t.Run(name, func(t *testing.T) {
			b := ruleSite(t, testsupport.NewFactory())
			createRule(t, b, `{"group_id":2,"schedule_id":1,"door_ids":[1]}`)

			got := b.Post(target, "schedule_id", "x", "zone_ids", "9")

			page := testsupport.ParsePage(got.Body)
			want := []any{422, map[string]string{"schedule_id": "Choose a schedule.", "zone_ids": "No zone has id 9."}, []string(nil)}
			if diff := cmp.Diff(want, []any{got.Status, page.FieldErrors, page.Checked}); diff != "" {
				t.Errorf("status, field errors, checked (-want +got):\n%s", diff)
			}
		})
	}
}

func TestAccessRulePageShowsTheRule(t *testing.T) {
	t.Parallel()
	b := ruleSite(t, testsupport.NewFactory())
	createRule(t, b, `{"group_id":2,"schedule_id":3,"door_ids":[2],"zone_ids":[1]}`)
	b.Get("/")

	got := testsupport.ParsePage(b.Get("/admin/groups/2/rules/1").Body)

	want := []any{"Access rule · doorcode", "Access rule 1",
		map[string][]string{"schedule_id": {"Choose a schedule", "Always", "Nights (selected)", "Weekdays"}},
		[]string{"door_ids=2", "zone_ids=1"},
	}
	if diff := cmp.Diff(want, []any{got.Title, got.Heading, got.Options, got.Checked}); diff != "" {
		t.Errorf("title, heading, schedules, checked (-want +got):\n%s", diff)
	}
}

func TestSavingAnAccessRuleFromItsPage(t *testing.T) {
	t.Parallel()
	b := ruleSite(t, testsupport.NewFactory())
	createRule(t, b, `{"group_id":2,"schedule_id":3,"door_ids":[2],"zone_ids":[1]}`)
	b.Get("/")

	saved := b.Post("/admin/groups/2/rules/1", "schedule_id", "1", "door_ids", "3")
	got := pageOf(b.Get(saved.Location))

	want := []any{"/admin/groups/2", []string{"The access rule's changes have been saved."}, [][]string{rulesHead, {"Always", "Attic", "None", "Edit Delete"}}}
	if diff := cmp.Diff(want, []any{saved.Location, got.Messages, got.Tables["Access rules"]}); diff != "" {
		t.Errorf("redirect, messages, rules (-want +got):\n%s", diff)
	}
}

func TestDeletingAnAccessRuleFromItsPage(t *testing.T) {
	t.Parallel()
	b := ruleSite(t, testsupport.NewFactory())
	createRule(t, b, `{"group_id":2,"schedule_id":3,"door_ids":[2]}`)
	b.Get("/")

	deleted := b.Post("/admin/groups/2/rules/1/delete")
	got := pageOf(b.Get(deleted.Location))

	want := []any{"/admin/groups/2", []string{"The access rule has been deleted. Its members no longer get in by it."}, noRules}
	if diff := cmp.Diff(want, []any{deleted.Location, got.Messages, got.Tables["Access rules"]}); diff != "" {
		t.Errorf("redirect, messages, rules (-want +got):\n%s", diff)
	}
}

func TestAccessRulePagesThatDoNotExistSaySo(t *testing.T) {
	t.Parallel()
	cases := map[string]func(b *testsupport.Browser) testsupport.Response{
		"a new rule of no group": func(b *testsupport.Browser) testsupport.Response { return b.Get("/admin/groups/9/rules/new") },
		"creating for no group": func(b *testsupport.Browser) testsupport.Response {
			return b.Post("/admin/groups/9/rules", "schedule_id", "1")
		},
		"no rule":              func(b *testsupport.Browser) testsupport.Response { return b.Get("/admin/groups/2/rules/9") },
		"another group's rule": func(b *testsupport.Browser) testsupport.Response { return b.Get("/admin/groups/3/rules/1") },
		"a rule of no group":   func(b *testsupport.Browser) testsupport.Response { return b.Get("/admin/groups/9/rules/1") },
		"saving no rule": func(b *testsupport.Browser) testsupport.Response {
			return b.Post("/admin/groups/2/rules/9", "schedule_id", "1")
		},
		"deleting no rule": func(b *testsupport.Browser) testsupport.Response { return b.Post("/admin/groups/3/rules/1/delete") },
	}
	for name, request := range cases {
		t.Run(name, func(t *testing.T) {
			b := ruleSite(t, testsupport.NewFactory())
			createRule(t, b, `{"group_id":2,"schedule_id":1,"door_ids":[1]}`)

			got := request(b)

			if diff := cmp.Diff([]any{404, "Not found"}, []any{got.Status, pageOf(got).Heading}); diff != "" {
				t.Errorf("status, heading (-want +got):\n%s", diff)
			}
		})
	}
}

func TestDeletingAScheduleAnAccessRuleUsesFromItsPageIsRefused(t *testing.T) {
	t.Parallel()
	b := ruleSite(t, testsupport.NewFactory())
	createRule(t, b, `{"group_id":2,"schedule_id":2,"door_ids":[1]}`)
	createDoor(t, b, `{"name":"Gate","open_schedule_ids":[2]}`)
	b.Get("/")

	both := b.Post("/admin/schedules/2/delete")
	send(t, b, "PUT", "/api/v1/doors/4", `{"name":"Gate"}`, 200)
	rules := b.Post("/admin/schedules/2/delete")

	want := []any{409, []string{"Doors are in open mode during this schedule: Gate. Take it off them before deleting it. " +
		"Access rules of these groups use this schedule: Staff. Change or delete those rules before deleting it."},
		409, []string{"Access rules of these groups use this schedule: Staff. Change or delete those rules before deleting it."}}
	if diff := cmp.Diff(want, []any{both.Status, pageOf(both).Messages, rules.Status, pageOf(rules).Messages}); diff != "" {
		t.Errorf("status, messages with a door and a rule, then a rule (-want +got):\n%s", diff)
	}
}

func TestAccessRulePageStoreFailureShowsARequestID(t *testing.T) {
	t.Parallel()
	cases := map[string]struct {
		fail    func(s *testsupport.Store)
		request func(b *testsupport.Browser) testsupport.Response
	}{
		"a group's rules":      {(*testsupport.Store).FailRules, func(b *testsupport.Browser) testsupport.Response { return b.Get("/admin/groups/2") }},
		"a group's rule names": {(*testsupport.Store).FailDoors, func(b *testsupport.Browser) testsupport.Response { return b.Get("/admin/groups/2") }},
		"the group":            {(*testsupport.Store).FailGroups, func(b *testsupport.Browser) testsupport.Response { return b.Get("/admin/groups/2/rules/new") }},
		"the choices":          {(*testsupport.Store).FailZones, func(b *testsupport.Browser) testsupport.Response { return b.Get("/admin/groups/2/rules/new") }},
		"the rule":             {(*testsupport.Store).FailRules, func(b *testsupport.Browser) testsupport.Response { return b.Get("/admin/groups/2/rules/1") }},
		"creating": {(*testsupport.Store).FailRuleWrites, func(b *testsupport.Browser) testsupport.Response {
			return b.Post("/admin/groups/2/rules", "schedule_id", "1", "door_ids", "1")
		}},
		"saving": {(*testsupport.Store).FailRuleWrites, func(b *testsupport.Browser) testsupport.Response {
			return b.Post("/admin/groups/2/rules/1", "schedule_id", "1", "door_ids", "1")
		}},
		"deleting": {(*testsupport.Store).FailRuleWrites, func(b *testsupport.Browser) testsupport.Response {
			return b.Post("/admin/groups/2/rules/1/delete")
		}},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			f := testsupport.NewFactory()
			b := ruleSite(t, f)
			createRule(t, b, `{"group_id":2,"schedule_id":1,"door_ids":[1]}`)
			c.fail(f.StoreStub)

			got := c.request(b)

			if diff := cmp.Diff([]any{500, "Something went wrong"}, []any{got.Status, pageOf(got).Heading}); diff != "" {
				t.Errorf("status, heading (-want +got):\n%s", diff)
			}
		})
	}
}
