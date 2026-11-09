package entrypoint_test

import (
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/dakriy/doorcode/internal/testsupport"
)

// groupsNav is the navigation of a groups page for an administrator.
var groupsNav = mainNav("People: Groups", "")

// groupsHead is the header row of the group list.
var groupsHead = []string{"Name", "Slug", "Roles", "Members", "Pre-assigned"}

// membersHead is the header row of a group's members.
var membersHead = []string{"Name", "Email", "Status", "Actions"}

// nightForm is the night group's form as its page shows it.
var nightForm = map[string]string{"name": "Night", "slug": "night", "description": "Late shifts.",
	"preassigned_user_ids": "100234\n100877", "member": ""}

// preassignedNote is what a group form says in place of the pre-assigned
// user ids while sign-in reads no user id claim.
const preassignedNote = "Pre-assigned user ids, which add people to the group when their account is created, " +
	"are offered here once the user id claim is set in the settings."

// readingUserIDs makes sign-in read a user id claim, so group forms offer
// the pre-assigned user ids that match it.
func readingUserIDs(f *testsupport.Factory) *testsupport.Factory {
	f.StoreStub.SeedSetting("oidc_claim_user_id", "employee_id", testsupport.DefaultNow)
	return f
}

const night = `{"slug":"night","name":"Night","description":"Late shifts.","roles":["view-logs","door-commander"],` +
	`"preassigned_user_ids":["100234","100877"]}`

func TestGroupsPageListsGroupsWithRoleBadges(t *testing.T) {
	t.Parallel()
	b := signedInAdmin(t, testsupport.NewFactory())
	createGroup(t, b, night)
	createGroup(t, b, `{"slug":"day","name":"Day"}`)
	b.Get("/")

	got := b.Get("/admin/groups")

	want := testsupport.Page{
		Title: "Groups · doorcode", Heading: "Groups", Nav: groupsNav,
		Tables: map[string][][]string{"Groups": {
			groupsHead,
			{"Administrators", "administrators", "admin", "1", "0"},
			{"Day", "day", "No roles", "0", "0"},
			{"Night", "night", "view-logs door-commander", "0", "2"},
		}},
	}
	if diff := cmp.Diff(want, pageOf(got)); diff != "" {
		t.Errorf("page (-want +got):\n%s", diff)
	}
}

func TestNewGroupPageOffersAnEmptyForm(t *testing.T) {
	t.Parallel()
	b := signedInAdmin(t, testsupport.NewFactory())
	b.Get("/")

	got := b.Get("/admin/groups/new")

	want := testsupport.Page{
		Title: "New group · doorcode", Heading: "New group", Nav: groupsNav,
		Values: map[string]string{"name": "", "slug": "", "description": ""}, Notes: []string{preassignedNote},
	}
	if diff := cmp.Diff(want, pageOf(got)); diff != "" {
		t.Errorf("page (-want +got):\n%s", diff)
	}
}

func TestTheGroupFormSaysWhatEachRoleLetsItsHoldersDo(t *testing.T) {
	t.Parallel()
	b := signedInAdmin(t, testsupport.NewFactory())

	got := testsupport.ParsePage(b.Get("/admin/groups/new").Body).Choices

	want := map[string]string{
		"roles=admin":            "admin Everything, including the site settings and every other role.",
		"roles=manage-users":     "manage-users Add, import, and change users and their codes, and answer access requests.",
		"roles=manage-doors":     "manage-doors Add and change doors, zones, and overrides, enroll controllers, and open doors remotely.",
		"roles=manage-groups":    "manage-groups Add and change groups, their members, and the access rules that let them in.",
		"roles=manage-schedules": "manage-schedules Add and change the schedules access rules and open mode follow.",
		"roles=manage-firmware":  "manage-firmware Upload controller firmware and roll it out to doors.",
		"roles=view-logs":        "view-logs Read every door's events.",
		"roles=door-commander":   "door-commander Get in at any door at any time, even while it is locked, and open doors remotely.",
	}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("role choices (-want +got):\n%s", diff)
	}
}

func TestCreatingAGroupFromTheFormOpensItsPage(t *testing.T) {
	t.Parallel()
	b := signedInAdmin(t, readingUserIDs(testsupport.NewFactory()))
	b.Get("/")

	created := b.Post("/admin/groups", "name", "Night", "slug", "night", "description", "Late shifts.",
		"roles", "door-commander", "roles", "view-logs", "preassigned_user_ids", "100234\r\n\r\n100877, 100234")
	got := pageOf(b.Get(created.Location))

	if diff := cmp.Diff([]any{303, "/admin/groups/2"}, []any{created.Status, created.Location}); diff != "" {
		t.Errorf("redirect (-want +got):\n%s", diff)
	}
	want := testsupport.Page{
		Title: "Night · doorcode", Heading: "Night", Nav: groupsNav,
		Messages: []string{"The group has been created."},
		Values:   nightForm,
		Checked:  []string{"roles=view-logs", "roles=door-commander"},
		Tables: map[string][][]string{
			"Members":      {membersHead, {"This group has no members yet. Add someone by their email or user id."}},
			"Access rules": noRules,
		},
	}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("page (-want +got):\n%s", diff)
	}
}

func TestGroupFormSaysWhatIsWrongAndKeepsWhatWasEntered(t *testing.T) {
	t.Parallel()
	got := signedInAdmin(t, readingUserIDs(testsupport.NewFactory())).Post("/admin/groups", "name", "", "slug", "administrators",
		"description", testsupport.Repeat("x", 1001), "roles", "admin", "roles", "root", "preassigned_user_ids", "a b")

	page := pageOf(got)
	want := []any{422,
		map[string]string{"slug": "Another group has this slug.", "name": "Enter a name.", "description": "Use at most 1000 characters.",
			"roles":                "Choose roles from the list.",
			"preassigned_user_ids": "Use up to 64 letters, digits, dots, dashes, or underscores. “a b” is not."},
		map[string]string{"name": "", "slug": "administrators", "description": testsupport.Repeat("x", 1001), "preassigned_user_ids": "a b"},
		[]string{"roles=admin"},
	}
	if diff := cmp.Diff(want, []any{got.Status, page.FieldErrors, page.Values, page.Checked}); diff != "" {
		t.Errorf("status, field errors, values, roles (-want +got):\n%s", diff)
	}
}

func TestCreatingAGroupWithARoleYouDoNotHoldIsRefusedOnTheForm(t *testing.T) {
	t.Parallel()
	_, b := adminHolding(t, testsupport.NewFactory(), `["manage-groups"]`)
	b.Get("/")

	got := b.Post("/admin/groups", "name", "Bosses", "slug", "bosses", "roles", "admin")

	page := pageOf(got)
	want := []any{403, []string{"You can only change groups when you hold every role they carry."}, "bosses"}
	if diff := cmp.Diff(want, []any{got.Status, page.Messages, page.Values["slug"]}); diff != "" {
		t.Errorf("status, messages, slug (-want +got):\n%s", diff)
	}
}

func TestGroupPageListsItsMembers(t *testing.T) {
	t.Parallel()
	b := signedInAdmin(t, readingUserIDs(testsupport.NewFactory()))
	createGroup(t, b, night)
	createUser(t, b, ann)
	createUser(t, b, `{"email":"bo@example.com","display_name":"Bo"}`)
	join(t, b, "2", "1")
	send(t, b, "DELETE", "/api/v1/users/3", "", 204)
	join(t, b, "2", "3")
	b.Get("/")

	got := b.Get("/admin/groups/2")

	want := testsupport.Page{
		Title: "Night · doorcode", Heading: "Night", Nav: groupsNav,
		Values: map[string]string{"name": "Night", "slug": "night", "description": "Late shifts.",
			"preassigned_user_ids": "100877", "member": ""},
		Checked: []string{"roles=view-logs", "roles=door-commander"},
		Tables: map[string][][]string{
			"Members": {
				membersHead,
				{"Administrator", "admin@example.com", "Active", "Remove"},
				{"Ann Smith", "ann@example.com", "Active", "Remove"},
				{"Bo", "bo@example.com", "Deleted", "Remove"},
			},
			"Access rules": noRules,
		},
	}
	if diff := cmp.Diff(want, pageOf(got)); diff != "" {
		t.Errorf("page (-want +got):\n%s", diff)
	}
}

func TestAGroupFormOffersThePreassignedUserIDsOnceSignInReadsAUserIDClaim(t *testing.T) {
	t.Parallel()
	f := testsupport.NewFactory()
	b := signedInAdmin(t, f)
	createGroup(t, b, night)

	before := pageOf(b.Get("/admin/groups/2"))
	send(t, b, "PUT", "/api/v1/settings/oidc_claim_user_id", `{"value":"employee_id"}`, 200)
	after := pageOf(b.Get("/admin/groups/2"))

	want := []any{[]string{"This group keeps 2 pre-assigned user ids. " + preassignedNote}, "", "100234\n100877"}
	if diff := cmp.Diff(want, []any{before.Notes, before.Values["preassigned_user_ids"], after.Values["preassigned_user_ids"]}); diff != "" {
		t.Errorf("notes and pre-assigned user ids before, pre-assigned user ids after the claim is set (-want +got):\n%s", diff)
	}
}

func TestSavingAGroupWhoseFormDoesNotOfferThePreassignedUserIDsKeepsThem(t *testing.T) {
	t.Parallel()
	b := signedInAdmin(t, testsupport.NewFactory())
	createGroup(t, b, night)
	createGroup(t, b, `{"slug":"day","name":"Day","preassigned_user_ids":["100555"]}`)

	refused := pageOf(b.Post("/admin/groups/2", "name", "Nights", "slug", "Night"))
	saved := b.Post("/admin/groups/3", "name", "Days", "slug", "day")

	want := []any{[]string{"This group keeps 2 pre-assigned user ids. " + preassignedNote}, 303,
		[]any{"100555"}}
	got := []any{refused.Notes, saved.Status, testsupport.JSONAt(b.Get("/api/v1/groups/3").Body, "/preassigned_user_ids")}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("refused form's notes, saved status, pre-assigned user ids kept (-want +got):\n%s", diff)
	}
}

func TestAGroupFormSaysWhenTheGroupKeepsOnePreassignedUserID(t *testing.T) {
	t.Parallel()
	b := signedInAdmin(t, testsupport.NewFactory())
	createGroup(t, b, `{"slug":"day","name":"Day","preassigned_user_ids":["100555"]}`)

	got := pageOf(b.Get("/admin/groups/2"))

	if diff := cmp.Diff([]string{"This group keeps 1 pre-assigned user id. " + preassignedNote}, got.Notes); diff != "" {
		t.Errorf("notes (-want +got):\n%s", diff)
	}
}

func TestGroupPageThatDoesNotExistSaysSo(t *testing.T) {
	t.Parallel()
	cases := map[string]func(b *testsupport.Browser) testsupport.Response{
		"an unknown id": func(b *testsupport.Browser) testsupport.Response { return b.Get("/admin/groups/9") },
		"a slug":        func(b *testsupport.Browser) testsupport.Response { return b.Get("/admin/groups/administrators") },
		"deleting it":   func(b *testsupport.Browser) testsupport.Response { return b.Post("/admin/groups/9/delete") },
		"saving it": func(b *testsupport.Browser) testsupport.Response {
			return b.Post("/admin/groups/9", "name", "X", "slug", "x")
		},
		"adding a member": func(b *testsupport.Browser) testsupport.Response {
			return b.Post("/admin/groups/9/members", "member", "x")
		},
		"removing a member": func(b *testsupport.Browser) testsupport.Response { return b.Post("/admin/groups/9/members/1/remove") },
	}
	for name, request := range cases {
		t.Run(name, func(t *testing.T) {
			b := signedInAdmin(t, testsupport.NewFactory())
			b.Get("/")

			got := request(b)

			want := testsupport.Page{Title: "Not found · doorcode", Heading: "Not found", Nav: groupsNav}
			if diff := cmp.Diff([]any{404, want}, []any{got.Status, pageOf(got)}); diff != "" {
				t.Errorf("status and page (-want +got):\n%s", diff)
			}
		})
	}
}

func TestSavingAGroupFromItsPage(t *testing.T) {
	t.Parallel()
	b := signedInAdmin(t, testsupport.NewFactory())
	createGroup(t, b, night)

	saved := b.Post("/admin/groups/2", "name", "Nights", "slug", "nights", "description", "", "roles", "view-logs",
		"preassigned_user_ids", "")
	got := pageOf(b.Get(saved.Location))

	want := []any{"/admin/groups/2", "Nights", []string{"The group's changes have been saved."}, []string{"roles=view-logs"}, "nights"}
	if diff := cmp.Diff(want, []any{saved.Location, got.Heading, got.Messages, got.Checked, got.Values["slug"]}); diff != "" {
		t.Errorf("redirect, heading, messages, roles, slug (-want +got):\n%s", diff)
	}
}

func TestSavingAGroupFromItsPageSaysWhatIsWrong(t *testing.T) {
	t.Parallel()
	b := signedInAdmin(t, testsupport.NewFactory())
	createGroup(t, b, night)

	got := b.Post("/admin/groups/2", "name", "Nights", "slug", "Night")

	page := pageOf(got)
	want := []any{422, "Night", map[string]string{"slug": "Use up to 64 lowercase letters and digits, in words joined by single dashes."}, "Nights"}
	if diff := cmp.Diff(want, []any{got.Status, page.Heading, page.FieldErrors, page.Values["name"]}); diff != "" {
		t.Errorf("status, heading, field errors, name (-want +got):\n%s", diff)
	}
}

func TestTakingYourOwnAdminRoleFromTheGroupPageIsRefused(t *testing.T) {
	t.Parallel()
	cases := map[string]struct {
		request func(b *testsupport.Browser) testsupport.Response
		roles   []string
	}{
		"saving without the role": {roles: []string{"roles=manage-users"}, request: func(b *testsupport.Browser) testsupport.Response {
			return b.Post("/admin/groups/1", "name", "Administrators", "slug", "administrators", "roles", "manage-users")
		}},
		"deleting the group": {roles: []string{"roles=admin"}, request: func(b *testsupport.Browser) testsupport.Response {
			return b.Post("/admin/groups/1/delete")
		}},
		"leaving the group": {roles: []string{"roles=admin"}, request: func(b *testsupport.Browser) testsupport.Response {
			return b.Post("/admin/groups/1/members/1/remove")
		}},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			b := signedInAdmin(t, testsupport.NewFactory())
			b.Get("/")

			got := c.request(b)

			page := pageOf(got)
			want := []any{409, "Administrators",
				[]string{"You cannot remove your own admin role. Ask another administrator to do it."}, c.roles}
			if diff := cmp.Diff(want, []any{got.Status, page.Heading, page.Messages, page.Checked}); diff != "" {
				t.Errorf("status, heading, messages, roles in the form (-want +got):\n%s", diff)
			}
		})
	}
}

func TestAddingAndRemovingMembersFromTheGroupPage(t *testing.T) {
	t.Parallel()
	b := signedInAdmin(t, testsupport.NewFactory())
	createGroup(t, b, night)
	createUser(t, b, ann)
	createUser(t, b, `{"email":"bo@example.com","display_name":"Bo"}`)

	byEmail := b.Post("/admin/groups/2/members", "member", " BO@example.com ")
	added := pageOf(b.Get(byEmail.Location))
	byUserID := b.Post("/admin/groups/2/members", "member", "100234")
	removed := b.Post("/admin/groups/2/members/3/remove")
	after := pageOf(b.Get(removed.Location))

	want := []any{
		"/admin/groups/2", []string{"The member has been added."},
		"/admin/groups/2",
		"/admin/groups/2", []string{"The member has been removed."},
		[][]string{membersHead, {"Ann Smith", "ann@example.com", "Active", "Remove"}},
	}
	got := []any{byEmail.Location, added.Messages, byUserID.Location, removed.Location, after.Messages, after.Tables["Members"]}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("adding by email, adding by user id, removing (-want +got):\n%s", diff)
	}
}

func TestAddingSomeoneUnknownToAGroupSaysSo(t *testing.T) {
	t.Parallel()
	b := signedInAdmin(t, testsupport.NewFactory())

	got := b.Post("/admin/groups/1/members", "member", " nobody ")

	page := pageOf(got)
	want := []any{422, map[string]string{"member": "No user has this email address or user id."}, "nobody"}
	if diff := cmp.Diff(want, []any{got.Status, page.FieldErrors, page.Values["member"]}); diff != "" {
		t.Errorf("status, field errors, entry (-want +got):\n%s", diff)
	}
}

func TestRemovingSomeoneWhoIsNotAMemberFromTheGroupPageSaysSo(t *testing.T) {
	t.Parallel()
	b := signedInAdmin(t, testsupport.NewFactory())
	createUser(t, b, ann)
	b.Get("/")

	got := b.Post("/admin/groups/1/members/2/remove")

	want := []any{404, []string{"That user is not a member of this group."}}
	if diff := cmp.Diff(want, []any{got.Status, pageOf(got).Messages}); diff != "" {
		t.Errorf("status, messages (-want +got):\n%s", diff)
	}
}

func TestDeletingAGroupFromItsPage(t *testing.T) {
	t.Parallel()
	b := signedInAdmin(t, testsupport.NewFactory())
	createGroup(t, b, night)

	deleted := b.Post("/admin/groups/2/delete")
	got := pageOf(b.Get(deleted.Location))

	want := []any{"/admin/groups", []string{"The group has been deleted."},
		[][]string{groupsHead, {"Administrators", "administrators", "admin", "1", "0"}}}
	if diff := cmp.Diff(want, []any{deleted.Location, got.Messages, got.Tables["Groups"]}); diff != "" {
		t.Errorf("redirect, messages, groups (-want +got):\n%s", diff)
	}
}

func TestGroupManagerWithoutUserManagementSeesMembersWithoutLinks(t *testing.T) {
	t.Parallel()
	_, b := adminHolding(t, testsupport.NewFactory(), `["manage-groups"]`)
	b.Get("/")

	got := pageOf(b.Get("/admin/groups/2"))

	want := []any{[]string{"Dashboard", "People: Groups (current)", "Portal", "API tokens", "API docs"},
		[][]string{membersHead, {"Administrator", "admin@example.com", "Active", "Remove"}}}
	if diff := cmp.Diff(want, []any{got.Nav, got.Tables["Members"]}); diff != "" {
		t.Errorf("nav, members (-want +got):\n%s", diff)
	}
}

func TestGroupPageStoreFailureShowsARequestID(t *testing.T) {
	t.Parallel()
	cases := map[string]struct {
		fail    func(s *testsupport.Store)
		request func(b *testsupport.Browser) testsupport.Response
	}{
		"list":  {fail: (*testsupport.Store).FailGroups, request: func(b *testsupport.Browser) testsupport.Response { return b.Get("/admin/groups") }},
		"group": {fail: (*testsupport.Store).FailGroups, request: func(b *testsupport.Browser) testsupport.Response { return b.Get("/admin/groups/1") }},
		"create": {fail: (*testsupport.Store).FailGroups, request: func(b *testsupport.Browser) testsupport.Response {
			return b.Post("/admin/groups", "name", "A", "slug", "a")
		}},
		"save": {fail: (*testsupport.Store).FailGroupWrites, request: func(b *testsupport.Browser) testsupport.Response {
			return b.Post("/admin/groups/1", "name", "A", "slug", "a", "roles", "admin")
		}},
		"delete": {fail: (*testsupport.Store).FailGroupWrites, request: func(b *testsupport.Browser) testsupport.Response {
			return b.Post("/admin/groups/2/delete")
		}},
		"find a member": {fail: (*testsupport.Store).FailUsers, request: func(b *testsupport.Browser) testsupport.Response {
			return b.Post("/admin/groups/1/members", "member", "x")
		}},
		"add a member": {fail: (*testsupport.Store).FailGroupWrites, request: func(b *testsupport.Browser) testsupport.Response {
			return b.Post("/admin/groups/1/members", "member", "admin@example.com")
		}},
		"remove a member": {fail: (*testsupport.Store).FailGroupWrites, request: func(b *testsupport.Browser) testsupport.Response {
			return b.Post("/admin/groups/2/members/1/remove")
		}},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			f := testsupport.NewFactory()
			b := signedInAdmin(t, f)
			createGroup(t, b, `{"slug":"night","name":"Night"}`)
			c.fail(f.StoreStub)

			got := c.request(b)

			if diff := cmp.Diff([]any{500, "Something went wrong"}, []any{got.Status, pageOf(got).Heading}); diff != "" {
				t.Errorf("status, heading (-want +got):\n%s", diff)
			}
		})
	}
}
