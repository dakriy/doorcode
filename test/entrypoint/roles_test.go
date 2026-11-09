package entrypoint_test

import (
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/dakriy/doorcode/internal/testsupport"
)

// userToken creates a token for the user with id user as the administrator
// b is signed in as, and returns a client sending it.
func userToken(t *testing.T, h testsupport.Handler, b *testsupport.Browser, user string) *testsupport.Browser {
	t.Helper()
	got := send(t, b, "POST", "/api/v1/users/"+user+"/tokens", `{"name":"script"}`, 201)
	return tokenClient(h, testsupport.JSONString(got.Body, "/secret"))
}

func TestMemberOfAGroupHoldsTheRoleTheGroupCarries(t *testing.T) {
	t.Parallel()
	h, admin := adminSession(t, testsupport.NewFactory())
	createGroup(t, admin, `{"slug":"user-admins","name":"User admins","roles":["manage-users"]}`)
	createUser(t, admin, ann)
	annClient := userToken(t, h, admin, "2")
	before := annClient.Get("/api/v1/users/1")
	join(t, admin, "2", "2")

	after := annClient.Get("/api/v1/users/1")
	settings := annClient.Get("/api/v1/settings")

	if diff := cmp.Diff([]int{403, 200, 403}, []int{before.Status, after.Status, settings.Status}); diff != "" {
		t.Errorf("before joining, after joining, a route for another role (-want +got):\n%s", diff)
	}
}

func TestLeavingTheGroupTakesItsRole(t *testing.T) {
	t.Parallel()
	h, admin := adminSession(t, testsupport.NewFactory())
	createGroup(t, admin, `{"slug":"user-admins","name":"User admins","roles":["manage-users"]}`)
	createUser(t, admin, ann)
	join(t, admin, "2", "2")
	annClient := userToken(t, h, admin, "2")

	send(t, admin, "DELETE", "/api/v1/groups/2/members/2", "", 204)
	got := annClient.Get("/api/v1/users/1")

	want := apiError(403, "the manage-users role is required")
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("response (-want +got):\n%s", diff)
	}
}

func TestAdminRoleHoldsEveryOtherRole(t *testing.T) {
	t.Parallel()
	h, admin := adminSession(t, testsupport.NewFactory())
	createGroup(t, admin, `{"slug":"admins","name":"Admins","roles":["admin"]}`)
	createUser(t, admin, ann)
	join(t, admin, "2", "2")
	annClient := userToken(t, h, admin, "2")

	got := []int{annClient.Get("/api/v1/users").Status, annClient.Get("/api/v1/groups").Status, annClient.Get("/api/v1/settings").Status}

	if diff := cmp.Diff([]int{200, 200, 200}, got); diff != "" {
		t.Errorf("statuses (-want +got):\n%s", diff)
	}
}

func TestUserCreatedWithAPreAssignedUserIDJoinsThatGroup(t *testing.T) {
	t.Parallel()
	admin := signedInAdmin(t, testsupport.NewFactory())
	createGroup(t, admin, `{"slug":"night","name":"Night","preassigned_user_ids":["100877","100234"]}`)
	createGroup(t, admin, `{"slug":"day","name":"Day","preassigned_user_ids":["100877"]}`)

	joined := send(t, admin, "POST", "/api/v1/users", ann, 201)
	other := send(t, admin, "POST", "/api/v1/users", `{"email":"bo@example.com","display_name":"Bo","user_id":"555"}`, 201)
	night := testsupport.JSONAt(send(t, admin, "GET", "/api/v1/groups/2", "", 200).Body, "/preassigned_user_ids")

	want := testsupport.JSON(`[[{"id": 2, "slug": "night", "name": "Night"}], [], ["100877"]]`)
	got := []any{testsupport.JSONAt(joined.Body, "/groups"), testsupport.JSONAt(other.Body, "/groups"), night}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("groups of the pre-assigned user and of another, Night's pre-assigned ids (-want +got):\n%s", diff)
	}
}

func TestFirstAdministratorIsAMemberOfTheAdministratorsGroup(t *testing.T) {
	t.Parallel()
	b := signedInAdmin(t, testsupport.NewFactory())

	user := b.Get("/api/v1/users/1")
	group := b.Get("/api/v1/groups/1")
	page := b.Get("/admin/users")

	want := testsupport.JSON(`[[{"id": 1, "slug": "administrators", "name": "Administrators"}],
		{"id": 1, "slug": "administrators", "name": "Administrators", "description": "Members may do everything.",
		 "roles": ["admin"], "preassigned_user_ids": [], "member_count": 1}]`)
	if diff := cmp.Diff(want, []any{testsupport.JSONAt(user.Body, "/groups"), testsupport.JSON(group.Body)}); diff != "" {
		t.Errorf("user's groups, group (-want +got):\n%s", diff)
	}
	if diff := cmp.Diff(200, page.Status); diff != "" {
		t.Errorf("users page status (-want +got):\n%s", diff)
	}
}

func TestTakingYourOwnAdminRoleIsRefused(t *testing.T) {
	t.Parallel()
	cases := map[string]struct {
		method, target, body string
	}{
		"by taking it from your group": {method: "PUT", target: "/api/v1/groups/1",
			body: `{"slug":"administrators","name":"Administrators","roles":["manage-users"]}`},
		"by deleting your group": {method: "DELETE", target: "/api/v1/groups/1"},
		"by leaving your group":  {method: "DELETE", target: "/api/v1/groups/1/members/1"},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			b := signedInAdmin(t, testsupport.NewFactory())

			got := b.Send(c.method, c.target, c.body)

			want := apiError(409, "you cannot remove your own admin role")
			if diff := cmp.Diff(want, got); diff != "" {
				t.Errorf("response (-want +got):\n%s", diff)
			}
		})
	}
}

func TestAdministratorMayLeaveAGroupWhileAnotherGivesThemAdmin(t *testing.T) {
	t.Parallel()
	cases := map[string]struct {
		method, target, body string
		status               int
	}{
		"leave":            {method: "DELETE", target: "/api/v1/groups/1/members/1", status: 204},
		"delete the group": {method: "DELETE", target: "/api/v1/groups/1", status: 204},
		"take its role": {method: "PUT", target: "/api/v1/groups/1",
			body: `{"slug":"administrators","name":"Administrators"}`, status: 200},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			b := signedInAdmin(t, testsupport.NewFactory())
			createGroup(t, b, `{"slug":"backup","name":"Backup","roles":["admin"]}`)
			join(t, b, "2", "1")

			got := b.Send(c.method, c.target, c.body)

			if diff := cmp.Diff([]int{c.status, 200}, []int{got.Status, b.Get("/api/v1/settings").Status}); diff != "" {
				t.Errorf("status, then settings (-want +got):\n%s\n%s", diff, got.Body)
			}
		})
	}
}

func TestAdministratorMayRemoveAnotherAdministrator(t *testing.T) {
	t.Parallel()
	b := signedInAdmin(t, testsupport.NewFactory())
	createUser(t, b, ann)
	join(t, b, "1", "2")

	got := b.Send("DELETE", "/api/v1/groups/1/members/2", "")

	if diff := cmp.Diff(204, got.Status); diff != "" {
		t.Errorf("status (-want +got):\n%s", diff)
	}
}

func TestUserWithoutAdminMayChangeTheirOwnGroups(t *testing.T) {
	t.Parallel()
	_, b := adminHolding(t, testsupport.NewFactory(), `["manage-groups","view-logs"]`)

	left := b.Send("DELETE", "/api/v1/groups/2/members/1", "")

	if diff := cmp.Diff(204, left.Status); diff != "" {
		t.Errorf("status (-want +got):\n%s\n%s", diff, left.Body)
	}
}

func TestGroupManagerMayChangeOnlyGroupsWhoseRolesTheyHold(t *testing.T) {
	t.Parallel()
	cases := map[string]struct {
		method, target, body string
		status               int
	}{
		"create a group with a role they hold": {method: "POST", target: "/api/v1/groups",
			body: `{"slug":"logs","name":"Logs","roles":["view-logs"]}`, status: 201},
		"create a group with a role they do not hold": {method: "POST", target: "/api/v1/groups",
			body: `{"slug":"bosses","name":"Bosses","roles":["admin"]}`, status: 403},
		"give their group a role they do not hold": {method: "PUT", target: "/api/v1/groups/2",
			body: `{"slug":"staff","name":"Staff","roles":["manage-groups","view-logs","manage-users"]}`, status: 403},
		"rename a group carrying a role they do not hold": {method: "PUT", target: "/api/v1/groups/1",
			body: `{"slug":"administrators","name":"Admins","roles":["admin"]}`, status: 403},
		"add a member to a group carrying a role they do not hold": {method: "PUT", target: "/api/v1/groups/1/members/1", status: 403},
		"remove a member from it":                                  {method: "DELETE", target: "/api/v1/groups/1/members/1", status: 403},
		"delete it":                                                {method: "DELETE", target: "/api/v1/groups/1", status: 403},
		"add a member to their own":                                {method: "PUT", target: "/api/v1/groups/2/members/1", status: 204},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			_, b := adminHolding(t, testsupport.NewFactory(), `["manage-groups","view-logs"]`)

			got := b.Send(c.method, c.target, c.body)

			if diff := cmp.Diff(c.status, got.Status); diff != "" {
				t.Errorf("status (-want +got):\n%s\n%s", diff, got.Body)
			}
		})
	}
}

func TestGroupManagerRefusalSaysWhy(t *testing.T) {
	t.Parallel()
	_, b := adminHolding(t, testsupport.NewFactory(), `["manage-groups"]`)

	got := b.Send("DELETE", "/api/v1/groups/1", "")

	want := apiError(403, "you can only change groups whose roles you hold")
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("response (-want +got):\n%s", diff)
	}
}

func TestCreatingATokenForAnotherUserNeedsTheAdminRole(t *testing.T) {
	t.Parallel()
	_, b := adminHolding(t, testsupport.NewFactory(), `["manage-users","manage-groups"]`)

	got := b.PostJSON("/api/v1/users/1/tokens", `{"name":"x"}`)

	want := apiError(403, "the admin role is required")
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("response (-want +got):\n%s", diff)
	}
}
