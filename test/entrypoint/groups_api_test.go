package entrypoint_test

import (
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/dakriy/doorcode/internal/testsupport"
)

// administrators is the seeded group as the API shows it with only the
// first administrator in it.
const administrators = `{"id":1,"slug":"administrators","name":"Administrators","description":"Members may do everything.",` +
	`"roles":["admin"],"preassigned_user_ids":[],"member_count":1}`

func TestGroupListStartsWithTheAdministrators(t *testing.T) {
	t.Parallel()
	got := signedInAdmin(t, testsupport.NewFactory()).Get("/api/v1/groups")

	want := testsupport.Response{Status: 200, ContentType: jsonType, Body: `{"groups":[` + administrators + `]}` + "\n"}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("response (-want +got):\n%s", diff)
	}
}

func TestCreatingAGroupAnswersWithTheGroupTidied(t *testing.T) {
	t.Parallel()
	b := signedInAdmin(t, testsupport.NewFactory())

	got := b.PostJSON("/api/v1/groups", `{"slug":" night-staff ","name":" Night staff ","description":" Crews. ",`+
		`"roles":["view-logs","door-commander","view-logs"],"preassigned_user_ids":[" 100234 ","","100877","100234"]}`)

	want := testsupport.Response{Status: 201, ContentType: jsonType, Body: `{"id":2,"slug":"night-staff","name":"Night staff",` +
		`"description":"Crews.","roles":["view-logs","door-commander"],"preassigned_user_ids":["100234","100877"],"member_count":0}` + "\n"}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("response (-want +got):\n%s", diff)
	}
}

func TestGroupsAreListedByName(t *testing.T) {
	t.Parallel()
	b := signedInAdmin(t, testsupport.NewFactory())
	createGroup(t, b, `{"slug":"night","name":"night"}`)
	createGroup(t, b, `{"slug":"day","name":"Day"}`)
	createGroup(t, b, `{"slug":"ann","name":"Ann"}`)

	got := b.Get("/api/v1/groups")

	var names []any
	for i := range 4 {
		names = append(names, testsupport.JSONAt(got.Body, "/groups/"+itoa(i)+"/name"))
	}
	if diff := cmp.Diff([]any{"Administrators", "Ann", "Day", "night"}, names); diff != "" {
		t.Errorf("names (-want +got):\n%s", diff)
	}
}

func TestGroupRequestIsChecked(t *testing.T) {
	t.Parallel()
	slugHint := "Use up to 64 lowercase letters and digits, in words joined by single dashes."
	cases := map[string]struct {
		body string
		want string
	}{
		"not json":             {body: `{"slug":1}`, want: `body must be a JSON object with a string \"slug\" and \"name\"`},
		"no slug":              {body: `{"name":"A"}`, want: "Enter a slug."},
		"capital letters":      {body: `{"slug":"Night","name":"A"}`, want: slugHint},
		"double dash":          {body: `{"slug":"a--b","name":"A"}`, want: slugHint},
		"trailing dash":        {body: `{"slug":"a-","name":"A"}`, want: slugHint},
		"slug too long":        {body: `{"slug":"` + repeat("a", 65) + `","name":"A"}`, want: slugHint},
		"slug taken":           {body: `{"slug":"administrators","name":"A"}`, want: "Another group has this slug."},
		"no name":              {body: `{"slug":"a","name":" "}`, want: "Enter a name."},
		"name too long":        {body: `{"slug":"a","name":"` + repeat("x", 101) + `"}`, want: "Use at most 100 characters."},
		"description too long": {body: `{"slug":"a","name":"A","description":"` + repeat("x", 1001) + `"}`, want: "Use at most 1000 characters."},
		"unknown role":         {body: `{"slug":"a","name":"A","roles":["admin","root"]}`, want: "Choose roles from the list."},
		"bad user id": {body: `{"slug":"a","name":"A","preassigned_user_ids":["1","a b"]}`,
			want: "Use up to 64 letters, digits, dots, dashes, or underscores. “a b” is not."},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			got := signedInAdmin(t, testsupport.NewFactory()).PostJSON("/api/v1/groups", c.body)

			want := apiError(400, c.want)
			if diff := cmp.Diff(want, got); diff != "" {
				t.Errorf("response (-want +got):\n%s", diff)
			}
		})
	}
}

func TestGroupRequestAcceptsTheEdgesOfItsRanges(t *testing.T) {
	t.Parallel()
	bodies := map[string]string{
		"64 character slug":          `{"slug":"` + repeat("a", 64) + `","name":"A"}`,
		"slug of words":              `{"slug":"a1-b2-c3","name":"A"}`,
		"100 character name":         `{"slug":"a","name":"` + repeat("é", 100) + `"}`,
		"1000 character description": `{"slug":"a","name":"A","description":"` + repeat("é", 1000) + `"}`,
		"every role":                 `{"slug":"a","name":"A","roles":["admin","manage-users","manage-doors","manage-groups","manage-schedules","manage-firmware","view-logs","door-commander"]}`,
		"a role named twice":         `{"slug":"a","name":"A","roles":["admin","admin"]}`,
	}
	for name, body := range bodies {
		t.Run(name, func(t *testing.T) {
			got := signedInAdmin(t, testsupport.NewFactory()).PostJSON("/api/v1/groups", body)

			if diff := cmp.Diff(201, got.Status); diff != "" {
				t.Errorf("status (-want +got):\n%s\n%s", diff, got.Body)
			}
		})
	}
}

func TestUpdatingAGroupReplacesIt(t *testing.T) {
	t.Parallel()
	b := signedInAdmin(t, testsupport.NewFactory())
	createGroup(t, b, `{"slug":"night","name":"Night","roles":["view-logs"],"preassigned_user_ids":["1"]}`)
	join(t, b, "2", "1")

	updated := b.PutJSON("/api/v1/groups/2", `{"slug":"night","name":"Nights","roles":["door-commander"]}`)
	got := b.Get("/api/v1/groups/2")

	want := `{"id":2,"slug":"night","name":"Nights","description":"","roles":["door-commander"],"preassigned_user_ids":[],"member_count":1}` + "\n"
	if diff := cmp.Diff([]any{200, want, want}, []any{updated.Status, updated.Body, got.Body}); diff != "" {
		t.Errorf("status, response, then read (-want +got):\n%s", diff)
	}
}

func TestUpdatingAGroupChecksIt(t *testing.T) {
	t.Parallel()
	cases := map[string]struct {
		body, want string
	}{
		"another group's slug": {body: `{"slug":"administrators","name":"Night"}`, want: "Another group has this slug."},
		"not json":             {body: `"night"`, want: `body must be a JSON object with a string \"slug\" and \"name\"`},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			b := signedInAdmin(t, testsupport.NewFactory())
			createGroup(t, b, `{"slug":"night","name":"Night"}`)

			got := b.PutJSON("/api/v1/groups/2", c.body)

			want := apiError(400, c.want)
			if diff := cmp.Diff(want, got); diff != "" {
				t.Errorf("response (-want +got):\n%s", diff)
			}
		})
	}
}

func TestDeletingAGroupRemovesItAndItsRoles(t *testing.T) {
	t.Parallel()
	h, b := adminSession(t, testsupport.NewFactory())
	createGroup(t, b, `{"slug":"user-admins","name":"User admins","roles":["manage-users"]}`)
	createUser(t, b, ann)
	join(t, b, "2", "2")
	annClient := userToken(t, h, b, "2")

	deleted := b.Delete("/api/v1/groups/2")
	got := []int{b.Get("/api/v1/groups/2").Status, annClient.Get("/api/v1/users").Status}

	if diff := cmp.Diff(testsupport.Response{Status: 204}, deleted); diff != "" {
		t.Errorf("deletion (-want +got):\n%s", diff)
	}
	if diff := cmp.Diff([]int{404, 403}, got); diff != "" {
		t.Errorf("reading the group, the former member's call (-want +got):\n%s", diff)
	}
}

func TestGroupThatDoesNotExistIsNotFound(t *testing.T) {
	t.Parallel()
	cases := map[string]struct{ method, target, body string }{
		"read":          {method: "GET", target: "/api/v1/groups/9"},
		"read by slug":  {method: "GET", target: "/api/v1/groups/administrators"},
		"update":        {method: "PUT", target: "/api/v1/groups/9", body: `{"slug":"x","name":"X"}`},
		"delete":        {method: "DELETE", target: "/api/v1/groups/9"},
		"list members":  {method: "GET", target: "/api/v1/groups/9/members"},
		"add member":    {method: "PUT", target: "/api/v1/groups/9/members/1"},
		"remove member": {method: "DELETE", target: "/api/v1/groups/9/members/1"},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			got := signedInAdmin(t, testsupport.NewFactory()).Send(c.method, c.target, c.body)

			want := apiError(404, "no group has this id")
			if diff := cmp.Diff(want, got); diff != "" {
				t.Errorf("response (-want +got):\n%s", diff)
			}
		})
	}
}

func TestGroupMembersAreListedByName(t *testing.T) {
	t.Parallel()
	b := signedInAdmin(t, testsupport.NewFactory())
	createUser(t, b, ann)
	createUser(t, b, `{"email":"bo@example.com","display_name":"Bo"}`)
	join(t, b, "1", "3")
	join(t, b, "1", "2")
	join(t, b, "1", "3")

	got := b.Get("/api/v1/groups/1/members")

	var names []any
	for i := range 3 {
		names = append(names, testsupport.JSONAt(got.Body, "/members/"+itoa(i)+"/display_name"))
	}
	if diff := cmp.Diff([]any{"Administrator", "Ann Smith", "Bo"}, names); diff != "" {
		t.Errorf("members (-want +got):\n%s", diff)
	}
	admins := `[{"id":1,"slug":"administrators","name":"Administrators"}]`
	if diff := cmp.Diff(testsupport.JSON(admins), testsupport.JSONAt(got.Body, "/members/2/groups")); diff != "" {
		t.Errorf("a member's groups (-want +got):\n%s", diff)
	}
}

func TestRemovingSomeoneWhoIsNotAMemberIsNotFound(t *testing.T) {
	t.Parallel()
	b := signedInAdmin(t, testsupport.NewFactory())
	createUser(t, b, ann)

	got := b.Delete("/api/v1/groups/1/members/2")

	want := apiError(404, "the user is not a member of this group")
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("response (-want +got):\n%s", diff)
	}
}

func TestGroupStoreFailureAnswersWithARequestID(t *testing.T) {
	t.Parallel()
	cases := map[string]struct {
		fail           func(s *testsupport.Store)
		method, target string
		body           string
	}{
		"list":                         {fail: (*testsupport.Store).FailGroups, method: "GET", target: "/api/v1/groups"},
		"create":                       {fail: (*testsupport.Store).FailGroups, method: "POST", target: "/api/v1/groups", body: `{"slug":"a","name":"A"}`},
		"create, storing it":           {fail: (*testsupport.Store).FailGroupWrites, method: "POST", target: "/api/v1/groups", body: `{"slug":"a","name":"A"}`},
		"read":                         {fail: (*testsupport.Store).FailGroups, method: "GET", target: "/api/v1/groups/1"},
		"update":                       {fail: (*testsupport.Store).FailGroups, method: "PUT", target: "/api/v1/groups/1", body: `{"slug":"a","name":"A"}`},
		"delete":                       {fail: (*testsupport.Store).FailGroups, method: "DELETE", target: "/api/v1/groups/1"},
		"list members":                 {fail: (*testsupport.Store).FailGroups, method: "GET", target: "/api/v1/groups/1/members"},
		"add member":                   {fail: (*testsupport.Store).FailGroups, method: "PUT", target: "/api/v1/groups/1/members/1"},
		"add member, reading the user": {fail: (*testsupport.Store).FailUsers, method: "PUT", target: "/api/v1/groups/1/members/1"},
		"remove member":                {fail: (*testsupport.Store).FailGroups, method: "DELETE", target: "/api/v1/groups/1/members/1"},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			f := testsupport.NewFactory()
			b := signedInAdmin(t, f)
			c.fail(f.StoreStub)

			got := b.Send(c.method, c.target, c.body)

			want := internalError("4243444546474849")
			if diff := cmp.Diff(want, got); diff != "" {
				t.Errorf("response (-want +got):\n%s", diff)
			}
		})
	}
}
