package entrypoint_test

import (
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/dakriy/doorcode/internal/testsupport"
)

func TestDoorPageListsWhoCanEnterAndTheOverrides(t *testing.T) {
	t.Parallel()
	f := testsupport.NewFactory()
	b := ruleSite(t, f)
	send(t, b, "PUT", "/api/v1/doors/1", `{"name":"Lobby","zone_ids":[1,2]}`, 200)
	createRule(t, b, `{"group_id":2,"schedule_id":2,"zone_ids":[1]}`)
	createRule(t, b, `{"group_id":3,"schedule_id":3,"door_ids":[1],"zone_ids":[2,1]}`)
	createRule(t, b, `{"group_id":3,"schedule_id":1,"door_ids":[1]}`)
	createRule(t, b, `{"group_id":2,"schedule_id":1,"door_ids":[2]}`)
	createOverride(t, b, "1", lockedToday)
	createOverride(t, b, "1", `{"kind":"open","start":"2026-01-05T08:00:00Z","end":"2026-01-06T08:00:00Z"}`)
	b.Get("/")

	got := pageOf(b.Get("/admin/doors/1")).Tables

	want := map[string][][]string{
		"Overrides": {overridesHead,
			{"Open In force", "Mon 5 Jan 2026 08:00 – Tue 6 Jan 2026 08:00", "by Administrator", "Edit End now Delete"},
			{"Locked Upcoming", "Mon 5 Jan 2026 10:00 – Mon 5 Jan 2026 12:00", "Floor polishing. by Administrator", "Edit Delete"},
		},
		"Who can enter": {entrantsHead,
			{"Staff", "Weekdays", "Zone North", "Rule 1"},
			{"Guests", "Nights", "This door; Zones Annex, North", "Rule 2"},
			{"Guests", "Always", "This door", "Rule 3"},
		},
	}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("tables (-want +got):\n%s", diff)
	}
}

func TestNewOverridePageStartsNowInTheSiteTimezone(t *testing.T) {
	t.Parallel()
	b := inZone(t, "Europe/Berlin")
	createDoor(t, b, frontDoor)
	b.Get("/")

	got := testsupport.ParsePage(b.Get("/admin/doors/1/overrides/new").Body)

	want := []any{"New override · doorcode", "New override",
		map[string]string{"start": "2026-01-05T10:00", "end": "", "reason": ""},
		map[string][]string{"kind": {"Open", "Locked (selected)"}},
	}
	if diff := cmp.Diff(want, []any{got.Title, got.Heading, got.Values, got.Options}); diff != "" {
		t.Errorf("title, heading, values, kinds (-want +got):\n%s", diff)
	}
}

func TestCreatingAnOverrideFromTheFormReadsItsTimesInTheSiteTimezone(t *testing.T) {
	t.Parallel()
	b := inZone(t, "Europe/Berlin")
	createDoor(t, b, frontDoor)
	b.Get("/")

	created := b.Post("/admin/doors/1/overrides", "kind", "open", "start", "2026-01-05T11:00", "end", "2026-01-05T12:30", "reason", "Fair")
	page := pageOf(b.Get(created.Location))
	stored := testsupport.JSON(b.Get("/api/v1/doors/1/overrides/1").Body)

	want := []any{303, "/admin/doors/1", []string{"The override has been created."},
		[][]string{overridesHead, {"Open Upcoming", "Mon 5 Jan 2026 11:00 – Mon 5 Jan 2026 12:30", "Fair by Administrator", "Edit Delete"}},
		"2026-01-05T10:00:00Z",
	}
	got := []any{created.Status, created.Location, page.Messages, page.Tables["Overrides"], stored.(map[string]any)["start"]}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("redirect, messages, overrides, stored start (-want +got):\n%s", diff)
	}
}

func TestOverrideFormSaysWhatIsWrongAndKeepsWhatWasEntered(t *testing.T) {
	t.Parallel()
	for name, target := range map[string]string{"new": "/admin/doors/1/overrides", "existing": "/admin/doors/1/overrides/1"} {
		t.Run(name, func(t *testing.T) {
			b := signedInAdmin(t, testsupport.NewFactory())
			createDoor(t, b, frontDoor)
			createOverride(t, b, "1", lockedToday)

			got := b.Post(target, "kind", "ajar", "start", "soon", "end", "2026-01-05T08:00", "reason", "Why")

			page := testsupport.ParsePage(got.Body)
			want := []any{422,
				map[string]string{"kind": "Choose open or locked.", "start": "Enter when the override starts.", "end": "End the override in the future."},
				map[string]string{"start": "soon", "end": "2026-01-05T08:00", "reason": "Why"},
			}
			if diff := cmp.Diff(want, []any{got.Status, page.FieldErrors, page.Values}); diff != "" {
				t.Errorf("status, field errors, values (-want +got):\n%s", diff)
			}
		})
	}
}

func TestOverridePageShowsTheOverride(t *testing.T) {
	t.Parallel()
	b := signedInAdmin(t, testsupport.NewFactory())
	createDoor(t, b, frontDoor)
	createOverride(t, b, "1", lockedToday)
	b.Get("/")

	got := testsupport.ParsePage(b.Get("/admin/doors/1/overrides/1").Body)

	want := []any{"Override · doorcode", "Override of Front door",
		map[string]string{"start": "2026-01-05T10:00", "end": "2026-01-05T12:00", "reason": "Floor polishing."},
		map[string][]string{"kind": {"Open", "Locked (selected)"}},
	}
	if diff := cmp.Diff(want, []any{got.Title, got.Heading, got.Values, got.Options}); diff != "" {
		t.Errorf("title, heading, values, kinds (-want +got):\n%s", diff)
	}
}

func TestSavingAnOverrideFromItsPage(t *testing.T) {
	t.Parallel()
	b := signedInAdmin(t, testsupport.NewFactory())
	createDoor(t, b, frontDoor)
	createOverride(t, b, "1", lockedToday)
	b.Get("/")

	saved := b.Post("/admin/doors/1/overrides/1", "kind", "open", "start", "2026-01-05T10:30", "end", "2026-01-05T11:00", "reason", "")
	got := pageOf(b.Get(saved.Location))

	want := []any{"/admin/doors/1", []string{"The override's changes have been saved."},
		[][]string{overridesHead, {"Open Upcoming", "Mon 5 Jan 2026 10:30 – Mon 5 Jan 2026 11:00", "by Administrator", "Edit Delete"}}}
	if diff := cmp.Diff(want, []any{saved.Location, got.Messages, got.Tables["Overrides"]}); diff != "" {
		t.Errorf("redirect, messages, overrides (-want +got):\n%s", diff)
	}
}

func TestCancellingAndDeletingOverridesFromTheDoorPage(t *testing.T) {
	t.Parallel()
	f := testsupport.NewFactory()
	b := signedInAdmin(t, f)
	createDoor(t, b, frontDoor)
	createOverride(t, b, "1", lockedToday)
	createOverride(t, b, "1", `{"kind":"open","start":"2026-01-05T13:00:00Z","end":"2026-01-05T14:00:00Z"}`)
	b.Get("/")
	f.ClockStub.Set("2026-01-05T11:00:00Z")

	cancelled := b.Post("/admin/doors/1/overrides/1/cancel")
	afterCancel := pageOf(b.Get(cancelled.Location))
	deleted := b.Post("/admin/doors/1/overrides/2/delete")
	afterDelete := pageOf(b.Get(deleted.Location))

	want := []any{
		"/admin/doors/1", []string{"The override has been cancelled. It ends now."},
		[][]string{overridesHead, {"Open Upcoming", "Mon 5 Jan 2026 13:00 – Mon 5 Jan 2026 14:00", "by Administrator", "Edit Delete"}},
		"/admin/doors/1", []string{"The override has been deleted."}, freshDoorTables["Overrides"],
	}
	got := []any{cancelled.Location, afterCancel.Messages, afterCancel.Tables["Overrides"], deleted.Location, afterDelete.Messages, afterDelete.Tables["Overrides"]}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("cancel, delete (-want +got):\n%s", diff)
	}
}

func TestChangingAnOverrideFromTheDoorPageThatCannotBeChangedSaysSo(t *testing.T) {
	t.Parallel()
	cases := map[string]struct {
		target string
		status int
		want   string
	}{
		"cancelling one to come": {"/admin/doors/1/overrides/1/cancel", 409, "Only an override in force can be cancelled. Delete one that has not started."},
		"cancelling one gone":    {"/admin/doors/1/overrides/9/cancel", 404, "That override is not there any more. It may have been deleted already."},
		"deleting one gone":      {"/admin/doors/1/overrides/9/delete", 404, "That override is not there any more. It may have been deleted already."},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			b := signedInAdmin(t, testsupport.NewFactory())
			createDoor(t, b, frontDoor)
			createOverride(t, b, "1", lockedToday)
			b.Get("/")

			got := b.Post(c.target)

			page := pageOf(got)
			if diff := cmp.Diff([]any{c.status, "Front door", []string{c.want}}, []any{got.Status, page.Heading, page.Messages}); diff != "" {
				t.Errorf("status, heading, messages (-want +got):\n%s", diff)
			}
		})
	}
}

func TestOverridePagesThatDoNotExistSaySo(t *testing.T) {
	t.Parallel()
	cases := map[string]func(b *testsupport.Browser) testsupport.Response{
		"a new override of no door": func(b *testsupport.Browser) testsupport.Response { return b.Get("/admin/doors/9/overrides/new") },
		"creating for no door": func(b *testsupport.Browser) testsupport.Response {
			return b.Post("/admin/doors/9/overrides", "kind", "open")
		},
		"no override":            func(b *testsupport.Browser) testsupport.Response { return b.Get("/admin/doors/1/overrides/9") },
		"an override of no door": func(b *testsupport.Browser) testsupport.Response { return b.Get("/admin/doors/9/overrides/1") },
		"saving no override": func(b *testsupport.Browser) testsupport.Response {
			return b.Post("/admin/doors/2/overrides/1", "kind", "open")
		},
		"cancelling for no door": func(b *testsupport.Browser) testsupport.Response { return b.Post("/admin/doors/9/overrides/1/cancel") },
		"deleting for no door":   func(b *testsupport.Browser) testsupport.Response { return b.Post("/admin/doors/9/overrides/1/delete") },
	}
	for name, request := range cases {
		t.Run(name, func(t *testing.T) {
			b := zonedDoors(t, testsupport.NewFactory())
			createOverride(t, b, "1", lockedToday)

			got := request(b)

			if diff := cmp.Diff([]any{404, "Not found"}, []any{got.Status, pageOf(got).Heading}); diff != "" {
				t.Errorf("status, heading (-want +got):\n%s", diff)
			}
		})
	}
}

func TestOverridePageStoreFailureShowsARequestID(t *testing.T) {
	t.Parallel()
	form := []string{"kind", "open", "start", "2026-01-05T10:00", "end", "2026-01-05T11:00"}
	cases := map[string]struct {
		fail    func(s *testsupport.Store)
		request func(b *testsupport.Browser) testsupport.Response
	}{
		"the door page's overrides": {(*testsupport.Store).FailOverrides, func(b *testsupport.Browser) testsupport.Response { return b.Get("/admin/doors/1") }},
		"the door page's rules":     {(*testsupport.Store).FailRules, func(b *testsupport.Browser) testsupport.Response { return b.Get("/admin/doors/1") }},
		"the door page's groups":    {(*testsupport.Store).FailGroups, func(b *testsupport.Browser) testsupport.Response { return b.Get("/admin/doors/1") }},
		"the door page's time zone": {(*testsupport.Store).FailSettings, func(b *testsupport.Browser) testsupport.Response { return b.Get("/admin/doors/1") }},
		"a new override's zone": {(*testsupport.Store).FailSettings, func(b *testsupport.Browser) testsupport.Response {
			return b.Get("/admin/doors/1/overrides/new")
		}},
		"the override": {(*testsupport.Store).FailOverrides, func(b *testsupport.Browser) testsupport.Response {
			return b.Get("/admin/doors/1/overrides/1")
		}},
		"creating": {(*testsupport.Store).FailOverrideWrites, func(b *testsupport.Browser) testsupport.Response {
			return b.Post("/admin/doors/1/overrides", form...)
		}},
		"saving": {(*testsupport.Store).FailOverrideWrites, func(b *testsupport.Browser) testsupport.Response {
			return b.Post("/admin/doors/1/overrides/1", form...)
		}},
		"cancelling": {(*testsupport.Store).FailOverrideWrites, func(b *testsupport.Browser) testsupport.Response {
			return b.Post("/admin/doors/1/overrides/1/cancel")
		}},
		"deleting": {(*testsupport.Store).FailOverrideWrites, func(b *testsupport.Browser) testsupport.Response {
			return b.Post("/admin/doors/1/overrides/1/delete")
		}},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			f := testsupport.NewFactory()
			b := signedInAdmin(t, f)
			createDoor(t, b, frontDoor)
			createOverride(t, b, "1", `{"kind":"locked","start":"2026-01-05T08:00:00Z","end":"2026-01-05T12:00:00Z"}`)
			c.fail(f.StoreStub)

			got := c.request(b)

			if diff := cmp.Diff([]any{500, "Something went wrong"}, []any{got.Status, pageOf(got).Heading}); diff != "" {
				t.Errorf("status, heading (-want +got):\n%s", diff)
			}
		})
	}
}
