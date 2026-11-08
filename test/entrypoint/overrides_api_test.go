package entrypoint_test

import (
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/dakriy/doorcode/internal/testsupport"
)

// createOverride creates an override of the door with id door through the
// API as c's user. Overrides are numbered from 1.
func createOverride(t *testing.T, c *testsupport.Browser, door, body string) {
	t.Helper()
	send(t, c, "POST", "/api/v1/doors/"+door+"/overrides", body, 201)
}

// lockedToday is an override that locks a door from 10:00 to 12:00 UTC on
// the stub clock's day, Monday 5 January 2026, whose clock reads 09:00.
const lockedToday = `{"kind":"locked","start":"2026-01-05T10:00:00Z","end":"2026-01-05T12:00:00Z","reason":"Floor polishing."}`

func TestCreatingAnOverrideAnswersWithItAndItsCreator(t *testing.T) {
	t.Parallel()
	b := zonedDoors(t, testsupport.NewFactory())

	got := b.PostJSON("/api/v1/doors/1/overrides", `{"kind":"open","start":"2026-01-05T08:00:00+01:00","end":"2026-01-05T10:00:00Z","reason":" Delivery. "}`)

	want := testsupport.Response{Status: 201, ContentType: jsonType, Body: `{"id":1,"door_id":1,"kind":"open",` +
		`"start":"2026-01-05T07:00:00Z","end":"2026-01-05T10:00:00Z","reason":"Delivery.",` +
		`"created_by":{"id":1,"name":"Administrator"},"created_at":"2026-01-05T09:00:00Z","active":true}` + "\n"}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("response (-want +got):\n%s", diff)
	}
}

func TestADoorListsTheOverridesInForceAndToComeByStart(t *testing.T) {
	t.Parallel()
	f := testsupport.NewFactory()
	b := zonedDoors(t, f)
	createOverride(t, b, "1", lockedToday)
	createOverride(t, b, "1", `{"kind":"open","start":"2026-01-05T08:00:00Z","end":"2026-01-05T09:30:00Z"}`)
	createOverride(t, b, "1", `{"kind":"open","start":"2026-01-05T08:00:00Z","end":"2026-01-05T10:00:00Z"}`)
	createOverride(t, b, "2", lockedToday)
	f.ClockStub.Set("2026-01-05T09:30:00Z")

	got := b.Get("/api/v1/doors/1/overrides")

	var listed []any
	for _, o := range testsupport.JSONAt(got.Body, "/overrides").([]any) {
		view := o.(map[string]any)
		listed = append(listed, []any{view["id"], view["active"]})
	}
	if diff := cmp.Diff([]any{[]any{3.0, true}, []any{1.0, false}}, listed); diff != "" {
		t.Errorf("ids and whether active (-want +got):\n%s", diff)
	}
}

func TestUpdatingAnOverrideReplacesItButKeepsItsCreator(t *testing.T) {
	t.Parallel()
	b := zonedDoors(t, testsupport.NewFactory())
	createOverride(t, b, "1", lockedToday)

	updated := b.PutJSON("/api/v1/doors/1/overrides/1", `{"kind":"open","start":"2026-01-05T11:00:00Z","end":"2026-01-06T11:00:00Z"}`)
	got := b.Get("/api/v1/doors/1/overrides/1")

	want := `{"id":1,"door_id":1,"kind":"open","start":"2026-01-05T11:00:00Z","end":"2026-01-06T11:00:00Z","reason":"",` +
		`"created_by":{"id":1,"name":"Administrator"},"created_at":"2026-01-05T09:00:00Z","active":false}`
	if diff := cmp.Diff([]any{200, testsupport.JSON(want), testsupport.JSON(want)},
		[]any{updated.Status, testsupport.JSON(updated.Body), testsupport.JSON(got.Body)}); diff != "" {
		t.Errorf("status, update, read back (-want +got):\n%s", diff)
	}
}

func TestCancellingAnOverrideEndsItNow(t *testing.T) {
	t.Parallel()
	f := testsupport.NewFactory()
	b := zonedDoors(t, f)
	createOverride(t, b, "1", lockedToday)
	f.ClockStub.Set("2026-01-05T10:00:00Z")

	cancelled := b.Post("/api/v1/doors/1/overrides/1/cancel")
	listed := testsupport.JSON(b.Get("/api/v1/doors/1/overrides").Body)

	want := []any{200, "2026-01-05T10:00:00Z", false, testsupport.JSON(`{"overrides":[]}`)}
	if diff := cmp.Diff(want, []any{cancelled.Status, testsupport.JSONAt(cancelled.Body, "/end"), testsupport.JSONAt(cancelled.Body, "/active"), listed}); diff != "" {
		t.Errorf("status, end, active, listed (-want +got):\n%s", diff)
	}
}

func TestOnlyAnOverrideInForceCanBeCancelled(t *testing.T) {
	t.Parallel()
	for name, now := range map[string]string{"not started": "2026-01-05T09:59:59Z", "ended": "2026-01-05T12:00:00Z"} {
		t.Run(name, func(t *testing.T) {
			f := testsupport.NewFactory()
			b := zonedDoors(t, f)
			createOverride(t, b, "1", lockedToday)
			f.ClockStub.Set(now)

			got := b.Post("/api/v1/doors/1/overrides/1/cancel")

			want := testsupport.Response{Status: 409, ContentType: jsonType,
				Body: `{"error":"only an override that has started and not ended can be cancelled; delete one that has not started"}` + "\n"}
			if diff := cmp.Diff(want, got); diff != "" {
				t.Errorf("response (-want +got):\n%s", diff)
			}
		})
	}
}

func TestDeletingAnOverride(t *testing.T) {
	t.Parallel()
	b := zonedDoors(t, testsupport.NewFactory())
	createOverride(t, b, "1", lockedToday)

	deleted := b.Delete("/api/v1/doors/1/overrides/1")
	got := b.Get("/api/v1/doors/1/overrides/1")

	if diff := cmp.Diff([]any{204, 404}, []any{deleted.Status, got.Status}); diff != "" {
		t.Errorf("delete, read (-want +got):\n%s", diff)
	}
}

func TestAnOverrideIsFoundOnlyOnItsOwnDoor(t *testing.T) {
	t.Parallel()
	b := zonedDoors(t, testsupport.NewFactory())
	createOverride(t, b, "1", lockedToday)

	got := []testsupport.Response{
		b.Get("/api/v1/doors/2/overrides/1"),
		b.PutJSON("/api/v1/doors/2/overrides/1", lockedToday),
		b.Post("/api/v1/doors/2/overrides/1/cancel"),
		b.Delete("/api/v1/doors/2/overrides/1"),
		b.Get("/api/v1/doors/1/overrides/9"),
	}

	missing := testsupport.Response{Status: 404, ContentType: jsonType, Body: `{"error":"the door has no override with this id"}` + "\n"}
	if diff := cmp.Diff([]testsupport.Response{missing, missing, missing, missing, missing}, got); diff != "" {
		t.Errorf("read, update, cancel, delete, another id (-want +got):\n%s", diff)
	}
}

func TestOverridesOfADoorThatIsNotThereAreNotFound(t *testing.T) {
	t.Parallel()
	b := signedInAdmin(t, testsupport.NewFactory())

	got := []testsupport.Response{b.Get("/api/v1/doors/9/overrides"), b.PostJSON("/api/v1/doors/9/overrides", lockedToday)}

	missing := testsupport.Response{Status: 404, ContentType: jsonType, Body: `{"error":"no door has this id"}` + "\n"}
	if diff := cmp.Diff([]testsupport.Response{missing, missing}, got); diff != "" {
		t.Errorf("list, create (-want +got):\n%s", diff)
	}
}

func TestOverrideFieldsAreChecked(t *testing.T) {
	t.Parallel()
	cases := map[string]struct {
		body string
		want string
	}{
		"not JSON":            {`{"start":"soon"}`, `body must be a JSON object with a string \"kind\" and RFC 3339 times \"start\" and \"end\"`},
		"an unknown kind":     {`{"kind":"ajar","start":"2026-01-05T10:00:00Z","end":"2026-01-05T11:00:00Z"}`, "Choose open or locked."},
		"no start":            {`{"kind":"open","end":"2026-01-05T11:00:00Z"}`, "Enter when the override starts."},
		"no end":              {`{"kind":"open","start":"2026-01-05T10:00:00Z"}`, "Enter when the override ends."},
		"an end at the start": {`{"kind":"open","start":"2026-01-05T10:00:00Z","end":"2026-01-05T10:00:00Z"}`, "End the override after it starts."},
		"an end that is now":  {`{"kind":"open","start":"2026-01-05T08:00:00Z","end":"2026-01-05T09:00:00Z"}`, "End the override in the future."},
		"a long reason": {`{"kind":"open","start":"2026-01-05T10:00:00Z","end":"2026-01-05T11:00:00Z","reason":"` + repeat("x", 501) + `"}`,
			"Use at most 500 characters."},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			b := zonedDoors(t, testsupport.NewFactory())
			createOverride(t, b, "1", lockedToday)

			created := b.PostJSON("/api/v1/doors/1/overrides", c.body)
			updated := b.PutJSON("/api/v1/doors/1/overrides/1", c.body)

			want := testsupport.Response{Status: 400, ContentType: jsonType, Body: `{"error":"` + c.want + `"}` + "\n"}
			if diff := cmp.Diff([]testsupport.Response{want, want}, []testsupport.Response{created, updated}); diff != "" {
				t.Errorf("create, update (-want +got):\n%s", diff)
			}
		})
	}
}

func TestAnOverrideMayEndJustAfterNowAndHoldFiveHundredCharacters(t *testing.T) {
	t.Parallel()
	b := zonedDoors(t, testsupport.NewFactory())

	got := b.PostJSON("/api/v1/doors/1/overrides", `{"kind":"locked","start":"2026-01-05T08:59:59Z","end":"2026-01-05T09:00:01Z","reason":"`+repeat("x", 500)+`"}`)

	if diff := cmp.Diff([]any{201, true}, []any{got.Status, testsupport.JSONAt(got.Body, "/active")}); diff != "" {
		t.Errorf("status, active (-want +got):\n%s", diff)
	}
}

func TestOverridesAPIAnswersWithARequestIDWhenTheStoreFails(t *testing.T) {
	t.Parallel()
	cases := map[string]struct {
		fail   func(s *testsupport.Store)
		method string
		target string
		body   string
	}{
		"listing":            {(*testsupport.Store).FailOverrides, "GET", "/api/v1/doors/1/overrides", ""},
		"listing's door":     {(*testsupport.Store).FailDoors, "GET", "/api/v1/doors/1/overrides", ""},
		"reading":            {(*testsupport.Store).FailOverrides, "GET", "/api/v1/doors/1/overrides/1", ""},
		"creating":           {(*testsupport.Store).FailOverrideWrites, "POST", "/api/v1/doors/1/overrides", lockedToday},
		"creating's door":    {(*testsupport.Store).FailDoors, "POST", "/api/v1/doors/1/overrides", lockedToday},
		"updating":           {(*testsupport.Store).FailOverrideWrites, "PUT", "/api/v1/doors/1/overrides/1", lockedToday},
		"updating's reading": {(*testsupport.Store).FailOverrides, "PUT", "/api/v1/doors/1/overrides/1", lockedToday},
		"cancelling":         {(*testsupport.Store).FailOverrides, "POST", "/api/v1/doors/1/overrides/1/cancel", ""},
		"deleting":           {(*testsupport.Store).FailOverrideWrites, "DELETE", "/api/v1/doors/1/overrides/1", ""},
		"deleting's reading": {(*testsupport.Store).FailOverrides, "DELETE", "/api/v1/doors/1/overrides/1", ""},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			f := testsupport.NewFactory()
			b := zonedDoors(t, f)
			createOverride(t, b, "1", lockedToday)
			c.fail(f.StoreStub)

			got := b.Send(c.method, c.target, c.body)

			if diff := cmp.Diff([]any{500, "internal error"}, []any{got.Status, testsupport.JSONAt(got.Body, "/error")}); diff != "" {
				t.Errorf("status, error (-want +got):\n%s", diff)
			}
		})
	}
}
