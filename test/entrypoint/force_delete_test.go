package entrypoint_test

import (
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/dakriy/doorcode/internal/testsupport"
)

func TestForcingADoorsDeleteRemovesItAndItsEventsForGood(t *testing.T) {
	t.Parallel()
	k := loggedSite(t, testsupport.NewFactory())

	send(t, k.b, "DELETE", "/api/v1/doors/1?force=true", "", 204)

	got := []any{k.b.Get("/api/v1/doors/1"), doorNames(t, k.b, ""), eventIDs(t, k.b, ""), eventsScrape(t, k.a, k.b)}
	want := []any{
		apiError(404, "no door has this id"),
		[]any{[]any{"Dock"}, float64(1)},
		[]any{[]any{float64(3)}, float64(1)},
		[]string{`doorcode_events_total{door="Dock",door_id="2",type="check",outcome="denied",reason="no rule",start_reason=""} 1`},
	}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("door, doors, events, event counts (-want +got):\n%s", diff)
	}
}

func TestAForcedDeleteDeletesTheDoorsBrokerClientsAndFreesItsName(t *testing.T) {
	t.Parallel()
	f := testsupport.NewFactory()
	_, b, conn, _ := enrolledDoor(t, f)

	send(t, b, "DELETE", "/api/v1/doors/1?force=true", "", 204)
	created := b.PostJSON("/api/v1/doors", `{"name":"Lobby"}`)

	got := []any{conn.Connected(), f.BrokerStub.Usernames(), created.Status, testsupport.JSONAt(created.Body, "/id")}
	if diff := cmp.Diff([]any{false, []string{enrollUser}, 201, float64(2)}, got); diff != "" {
		t.Errorf("connected, clients, recreating, its id (-want +got):\n%s", diff)
	}
}

func TestForcingAUsersDeleteRemovesThemAndTheirHistoryForGood(t *testing.T) {
	t.Parallel()
	k := loggedSite(t, testsupport.NewFactory())
	annClient := userToken(t, k.a.Handler(), k.b, "2")
	send(t, annClient, "POST", "/api/v1/me/requests", `{"title":"Lab access","body":"From Monday."}`, 201)

	send(t, k.b, "DELETE", "/api/v1/users/2?force=true", "", 204)
	recreated := k.b.PostJSON("/api/v1/users", ann)

	got := []any{
		annClient.Get("/api/v1/tokens").Status,
		eventIDs(t, k.b, ""),
		testsupport.JSONAt(send(t, k.b, "GET", "/api/v1/groups/2/members", "", 200).Body, "/members/0/email"),
		testsupport.JSONAt(send(t, k.b, "GET", "/api/v1/requests?state=all", "", 200).Body, "/total"),
		recreated.Status,
	}
	want := []any{401, []any{[]any{float64(5), float64(4), float64(2)}, float64(3)}, "eve@example.com", float64(0), 201}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("her token, events, Staff's first member, requests, recreating her (-want +got):\n%s", diff)
	}
}

func TestADeletedDoorOrUserCanBeDeletedForGood(t *testing.T) {
	t.Parallel()
	cases := map[string]struct {
		create  func(t *testing.T, b *testsupport.Browser)
		target  string
		missing string
	}{
		"door": {func(t *testing.T, b *testsupport.Browser) { createDoor(t, b, frontDoor) }, "/api/v1/doors/1", "no door has this id"},
		"user": {func(t *testing.T, b *testsupport.Browser) { createUser(t, b, ann) }, "/api/v1/users/2", "no user has this id"},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			b := signedInAdmin(t, testsupport.NewFactory())
			c.create(t, b)
			send(t, b, "DELETE", c.target, "", 204)

			forced := b.Delete(c.target + "?force=true")
			got := b.Get(c.target)

			want := []testsupport.Response{{Status: 204}, apiError(404, c.missing)}
			if diff := cmp.Diff(want, []testsupport.Response{forced, got}); diff != "" {
				t.Errorf("forced delete, then reading (-want +got):\n%s", diff)
			}
		})
	}
}

func TestADeleteWithForceFalseOnlyMarksItDeleted(t *testing.T) {
	t.Parallel()
	cases := map[string]struct {
		create func(t *testing.T, b *testsupport.Browser)
		target string
	}{
		"door": {func(t *testing.T, b *testsupport.Browser) { createDoor(t, b, frontDoor) }, "/api/v1/doors/1"},
		"user": {func(t *testing.T, b *testsupport.Browser) { createUser(t, b, ann) }, "/api/v1/users/2"},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			b := signedInAdmin(t, testsupport.NewFactory())
			c.create(t, b)

			send(t, b, "DELETE", c.target+"?force=false", "", 204)

			got := testsupport.JSONAt(send(t, b, "GET", c.target, "", 200).Body, "/deleted_at")
			if diff := cmp.Diff("2026-01-05T09:00:00Z", got); diff != "" {
				t.Errorf("deleted at (-want +got):\n%s", diff)
			}
		})
	}
}

func TestAForceThatIsNeitherTrueNorFalseIsRefused(t *testing.T) {
	t.Parallel()
	for _, target := range []string{"/api/v1/doors/1?force=yes", "/api/v1/users/2?force=1"} {
		t.Run(target, func(t *testing.T) {
			t.Parallel()
			got := signedInAdmin(t, testsupport.NewFactory()).Delete(target)

			if diff := cmp.Diff(apiError(400, "force must be true or false"), got); diff != "" {
				t.Errorf("response (-want +got):\n%s", diff)
			}
		})
	}
}

func TestOnlyAnAdministratorMayDeleteForGood(t *testing.T) {
	t.Parallel()
	cases := map[string]struct {
		roles  string
		create func(t *testing.T, b *testsupport.Browser)
		target string
	}{
		"door": {`["manage-doors"]`, func(t *testing.T, b *testsupport.Browser) { createDoor(t, b, frontDoor) }, "/api/v1/doors/1"},
		"user": {`["manage-users"]`, func(t *testing.T, b *testsupport.Browser) { createUser(t, b, ann) }, "/api/v1/users/2"},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			_, b := adminHolding(t, testsupport.NewFactory(), c.roles)
			c.create(t, b)

			forced := b.Delete(c.target + "?force=true")
			plain := b.Delete(c.target)

			want := []testsupport.Response{apiError(403, "the admin role is required to delete for good"), {Status: 204}}
			if diff := cmp.Diff(want, []testsupport.Response{forced, plain}); diff != "" {
				t.Errorf("forced, plain (-want +got):\n%s", diff)
			}
		})
	}
}

func TestDeletingForGoodWhatIsNotThereIsRefused(t *testing.T) {
	t.Parallel()
	cases := map[string]testsupport.Response{
		"/api/v1/doors/9?force=true": apiError(404, "no door has this id"),
		"/api/v1/users/9?force=true": apiError(404, "no user has this id"),
	}
	for target, want := range cases {
		t.Run(target, func(t *testing.T) {
			t.Parallel()
			got := signedInAdmin(t, testsupport.NewFactory()).Delete(target)

			if diff := cmp.Diff(want, got); diff != "" {
				t.Errorf("response (-want +got):\n%s", diff)
			}
		})
	}
}

func TestDeletingYourOwnAccountForGoodIsRefused(t *testing.T) {
	t.Parallel()
	got := signedInAdmin(t, testsupport.NewFactory()).Delete("/api/v1/users/1?force=true")

	if diff := cmp.Diff(apiError(409, "you cannot delete your own account"), got); diff != "" {
		t.Errorf("response (-want +got):\n%s", diff)
	}
}

func TestAUserNamedByFirmwareOrAnOverrideTheyMadeCannotBeDeletedForGood(t *testing.T) {
	t.Parallel()
	cases := map[string]struct {
		make func(t *testing.T, c *testsupport.Browser)
		want string
	}{
		"firmware": {
			func(t *testing.T, c *testsupport.Browser) { uploadFirmware(t, c, "1.5.0") },
			"the user uploaded firmware 1.5.0, which name them; delete those first",
		},
		"override": {
			func(t *testing.T, c *testsupport.Browser) {
				createDoor(t, c, frontDoor)
				send(t, c, "POST", "/api/v1/doors/1/overrides", lockedNow, 201)
			},
			"the user created overrides 1 at door 1, which name them; delete those first",
		},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			h, admin := adminSession(t, testsupport.NewFactory())
			createUser(t, admin, ann)
			join(t, admin, "1", "2")
			c.make(t, userToken(t, h, admin, "2"))

			got := admin.Delete("/api/v1/users/2?force=true")

			if diff := cmp.Diff(apiError(409, c.want), got); diff != "" {
				t.Errorf("response (-want +got):\n%s", diff)
			}
		})
	}
}

func TestAUserIsNotDeletedForGoodWhenTheStoreFails(t *testing.T) {
	t.Parallel()
	cases := map[string]func(s *testsupport.Store){
		"firmware":    (*testsupport.Store).FailFirmware,
		"overrides":   (*testsupport.Store).FailOverrides,
		"user writes": (*testsupport.Store).FailUserWrites,
	}
	for name, fail := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			f := testsupport.NewFactory()
			b := signedInAdmin(t, f)
			createUser(t, b, ann)
			fail(f.StoreStub)

			forced := b.Delete("/api/v1/users/2?force=true")
			got := b.Get("/api/v1/users/2")

			if diff := cmp.Diff([]int{500, 200}, []int{forced.Status, got.Status}); diff != "" {
				t.Errorf("forced delete, then reading (-want +got):\n%s", diff)
			}
		})
	}
}

func TestADoorIsNotDeletedForGoodWhenItsClientsOrTheStoreFail(t *testing.T) {
	t.Parallel()
	cases := map[string]func(f *testsupport.Factory){
		"broker":      func(f *testsupport.Factory) { f.BrokerStub.FailDeleting() },
		"door writes": func(f *testsupport.Factory) { f.StoreStub.FailDoorWrites() },
	}
	for name, fail := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			f := testsupport.NewFactory()
			_, b, _, _ := enrolledDoor(t, f)
			fail(f)

			forced := b.Delete("/api/v1/doors/1?force=true")
			got := b.Get("/api/v1/doors/1")

			if diff := cmp.Diff([]int{500, 200}, []int{forced.Status, got.Status}); diff != "" {
				t.Errorf("forced delete, then reading (-want +got):\n%s", diff)
			}
		})
	}
}
