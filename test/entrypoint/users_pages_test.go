package entrypoint_test

import (
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/dakriy/doorcode/internal/testsupport"
)

// usersNav is the navigation of a users page for an administrator.
var usersNav = mainNav("People: Users", "")

// usersHead is the header row of the user list.
var usersHead = []string{"Name", "Email", "User id", "Groups", "Status"}

// annForm is ann's profile form as her page shows it.
var annForm = map[string]string{"email": "ann@example.com", "user_id": "100234", "first_name": "Ann",
	"last_name": "Smith", "display_name": "Ann Smith", "expires_on": "", "name": ""}

func TestUsersPageListsUsersWithTheirGroupsAndStatus(t *testing.T) {
	t.Parallel()
	f := testsupport.NewFactory()
	b := signedInAdmin(t, f)
	createUser(t, b, ann)
	createUser(t, b, `{"email":"bo@example.com","display_name":"Bo","expires_at":"2026-01-05T08:00:00Z"}`)
	createGroup(t, b, `{"slug":"night","name":"Night"}`)
	join(t, b, "2", "1")
	b.Get("/")

	got := b.Get("/admin/users")

	want := testsupport.Page{
		Title:   "Users · doorcode",
		Heading: "Users",
		Nav:     usersNav,
		Values:  map[string]string{"q": ""},
		Tables: map[string][][]string{"Users": {
			usersHead,
			{"Administrator", "admin@example.com", "", "Administrators, Night", "Active"},
			{"Ann Smith", "ann@example.com", "100234", "", "Active"},
			{"Bo", "bo@example.com", "", "", "Expired"},
			{"Showing 1 to 3 of 3"},
		}},
	}
	if diff := cmp.Diff(want, pageOf(got)); diff != "" {
		t.Errorf("page (-want +got):\n%s", diff)
	}
}

func TestUsersPageSearchesAndShowsDeletedUsers(t *testing.T) {
	t.Parallel()
	cases := map[string]struct {
		query string
		rows  [][]string
		value string
	}{
		"a search": {query: "?q=dan", value: "dan", rows: [][]string{{"Dan", "dan@example.com", "", "", "Active"}, {"Showing 1 to 1 of 1"}}},
		"deleted":  {query: "?show=deleted", value: "", rows: [][]string{{"Bo", "bo@example.com", "", "", "Deleted"}, {"Showing 1 to 1 of 1"}}},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			b := fiveUsers(t, testsupport.NewFactory())

			got := pageOf(b.Get("/admin/users" + c.query))

			want := []any{append([][]string{usersHead}, c.rows...), c.value}
			if diff := cmp.Diff(want, []any{got.Tables["Users"], got.Values["q"]}); diff != "" {
				t.Errorf("table and search (-want +got):\n%s", diff)
			}
		})
	}
}

func TestUsersPageSaysWhyItIsEmpty(t *testing.T) {
	t.Parallel()
	cases := map[string]string{
		"?q=nobody":         "No users match this search.",
		"?show=deleted":     "No users are deleted.",
		"?q=x&show=deleted": "No users match this search.",
		"?page=2":           "This page is past the last one.",
	}
	for query, message := range cases {
		t.Run(query, func(t *testing.T) {
			got := pageOf(signedInAdmin(t, testsupport.NewFactory()).Get("/admin/users" + query))

			if diff := cmp.Diff([][]string{usersHead, {message}}, got.Tables["Users"]); diff != "" {
				t.Errorf("table (-want +got):\n%s", diff)
			}
		})
	}
}

// manyUsers is a browser signed in as the administrator after creating 30
// more users, named User 01 to User 30.
func manyUsers(t *testing.T) *testsupport.Browser {
	t.Helper()
	b := signedInAdmin(t, testsupport.NewFactory())
	for i := 1; i <= 30; i++ {
		n := testsupport.Itoa(i/10) + testsupport.Itoa(i%10)
		createUser(t, b, `{"email":"user`+n+`@example.com","display_name":"User `+n+`"}`)
	}
	return b
}

// pagerOf is what a page of the user list shows of its place in the list:
// how many users it lists, the first and last name, what its footer says,
// and the page links and their targets.
func pagerOf(r testsupport.Response) []any {
	p := testsupport.ParsePage(r.Body)
	rows := p.Tables["Users"]
	last := len(rows) - 1
	return []any{last - 1, rows[1][0], rows[last-1][0], rows[last][0], p.Nav[len(usersNav):], p.NavTargets[len(usersNav):]}
}

func TestUsersPageIsPaged(t *testing.T) {
	t.Parallel()
	deleteAll := func(t *testing.T, b *testsupport.Browser) {
		for i := 2; i <= 31; i++ {
			send(t, b, "DELETE", "/api/v1/users/"+testsupport.Itoa(i), "", 204)
		}
	}
	cases := map[string]struct {
		query   string
		arrange func(t *testing.T, b *testsupport.Browser)
		want    []any
	}{
		"first page": {query: "",
			want: []any{25, "Administrator", "User 24", "Showing 1 to 25 of 31", []string{"Next"}, []string{"/admin/users?page=2"}}},
		"second page": {query: "?page=2",
			want: []any{6, "User 25", "User 30", "Showing 26 to 31 of 31", []string{"Previous"}, []string{"/admin/users?page=1"}}},
		"unreadable page": {query: "?page=x",
			want: []any{25, "Administrator", "User 24", "Showing 1 to 25 of 31", []string{"Next"}, []string{"/admin/users?page=2"}}},
		"a search": {query: "?q=user+&show=current",
			want: []any{25, "User 01", "User 25", "Showing 1 to 25 of 30", []string{"Next"}, []string{"/admin/users?page=2&q=user+"}}},
		"deleted": {query: "?show=deleted&page=2", arrange: deleteAll,
			want: []any{5, "User 26", "User 30", "Showing 26 to 30 of 30", []string{"Previous"}, []string{"/admin/users?page=1&show=deleted"}}},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			b := manyUsers(t)
			if c.arrange != nil {
				c.arrange(t, b)
			}

			got := b.Get("/admin/users" + c.query)

			if diff := cmp.Diff(c.want, pagerOf(got)); diff != "" {
				t.Errorf("rows, first, last, page links, their targets (-want +got):\n%s", diff)
			}
		})
	}
}

// accessHead is the header row of a user's door access.
var accessHead = []string{"Door", "Through", "Calendar"}

// noAccess is a user's door access when no rule names their groups.
var noAccess = [][]string{accessHead, {"No access rule names one of their groups, so their code opens no door."}}

// userEventsHead heads the table of a user's recent events, and
// noUserEvents is the table before they did anything.
var (
	userEventsHead = []string{"When", "Door", "Outcome", "Details"}
	noUserEvents   = [][]string{userEventsHead, {"Nothing has happened here yet."}}
)

// annPage is ann's page as an administrator first sees it.
var annPage = testsupport.Page{
	Title:   "Ann Smith · doorcode",
	Heading: "Ann Smith",
	Nav:     usersNav,
	Values:  annForm,
	Tables: map[string][][]string{
		"Groups":        {{"Group", "Roles"}, {"Not a member of any group."}},
		"Door access":   noAccess,
		"API tokens":    {{"Name", "Created", "Expires", "Actions"}, {"Ann Smith has no API tokens."}},
		"Recent events": noUserEvents,
	},
}

func TestNewUserPageOffersAnEmptyForm(t *testing.T) {
	t.Parallel()
	b := signedInAdmin(t, testsupport.NewFactory())
	b.Get("/")

	got := b.Get("/admin/users/new")

	want := testsupport.Page{
		Title: "New user · doorcode", Heading: "New user", Nav: usersNav,
		Values: map[string]string{"email": "", "user_id": "", "first_name": "", "last_name": "", "display_name": "", "expires_on": ""},
	}
	if diff := cmp.Diff(want, pageOf(got)); diff != "" {
		t.Errorf("page (-want +got):\n%s", diff)
	}
}

func TestCreatingAUserFromTheFormOpensTheirPage(t *testing.T) {
	t.Parallel()
	b := signedInAdmin(t, testsupport.NewFactory())
	b.Get("/")

	created := b.Post("/admin/users", "email", "ann@example.com", "user_id", "100234", "first_name", "Ann",
		"last_name", "Smith", "display_name", "", "expires_on", "2027-03-01")
	got := pageOf(b.Get(created.Location))

	if diff := cmp.Diff([]any{303, "/admin/users/2"}, []any{created.Status, created.Location}); diff != "" {
		t.Errorf("redirect (-want +got):\n%s", diff)
	}
	want := annPage
	want.Messages = []string{"The user has been created."}
	want.Values = map[string]string{"email": "ann@example.com", "user_id": "100234", "first_name": "Ann",
		"last_name": "Smith", "display_name": "Ann Smith", "expires_on": "2027-03-01", "name": ""}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("page (-want +got):\n%s", diff)
	}
}

func TestExpiryTypedOnAPageIsTheStartOfThatDayInTheSiteTimezone(t *testing.T) {
	t.Parallel()
	b := inZone(t, "Europe/Berlin")
	b.Post("/admin/users", "email", "ann@example.com", "display_name", "Ann", "expires_on", "2027-03-01")

	stored := testsupport.JSONString(b.Get("/api/v1/users/2").Body, "/expires_at")
	shown := pageOf(b.Get("/admin/users/2")).Values["expires_on"]

	if diff := cmp.Diff([]string{"2027-02-28T23:00:00Z", "2027-03-01"}, []string{stored, shown}); diff != "" {
		t.Errorf("stored, shown (-want +got):\n%s", diff)
	}
}

func TestUserFormSaysWhatIsWrongAndKeepsWhatWasEntered(t *testing.T) {
	t.Parallel()
	cases := map[string]struct {
		fields []string
		errors map[string]string
		values map[string]string
	}{
		"several problems": {
			fields: []string{"email", "admin@EXAMPLE.com", "user_id", "a b", "first_name", "", "last_name", "", "display_name", "", "expires_on", ""},
			errors: map[string]string{"email": "Another user has this email address.",
				"user_id":      "Use up to 64 letters, digits, dots, dashes, or underscores.",
				"display_name": "Enter a display name, or a first or last name."},
			values: map[string]string{"email": "admin@EXAMPLE.com", "user_id": "a b", "first_name": "", "last_name": "", "display_name": "", "expires_on": ""},
		},
		"an expiry that is not a date": {
			fields: []string{"email", "", "display_name", "Bo", "expires_on", "soon"},
			errors: map[string]string{"expires_on": "Enter a date, or leave it empty for an account that never expires."},
			values: map[string]string{"email": "", "user_id": "", "first_name": "", "last_name": "", "display_name": "Bo", "expires_on": "soon"},
		},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			got := signedInAdmin(t, testsupport.NewFactory()).Post("/admin/users", c.fields...)

			page := pageOf(got)
			if diff := cmp.Diff([]any{422, c.errors, c.values}, []any{got.Status, page.FieldErrors, page.Values}); diff != "" {
				t.Errorf("status, field errors, values (-want +got):\n%s", diff)
			}
		})
	}
}

func TestUserPageShowsTheirGroupsAndTokens(t *testing.T) {
	t.Parallel()
	f := testsupport.NewFactory()
	b := signedInAdmin(t, f)
	createUser(t, b, ann)
	createGroup(t, b, `{"slug":"night","name":"Night","roles":["view-logs","door-commander"]}`)
	join(t, b, "2", "2")
	join(t, b, "1", "2")
	send(t, b, "POST", "/api/v1/users/2/tokens", `{"name":"sync"}`, 201)
	send(t, b, "PUT", "/api/v1/users/2", `{"email":"ann@example.com","first_name":"Ann","last_name":"Smith","user_id":"100234",`+
		`"expires_at":"2026-01-05T09:00:00Z"}`, 200)

	got := pageOf(b.Get("/admin/users/2"))

	want := map[string][][]string{
		"Groups":        {{"Group", "Roles"}, {"Administrators", "admin"}, {"Night", "view-logs door-commander"}},
		"Door access":   noAccess,
		"API tokens":    {{"Name", "Created", "Expires", "Actions"}, {"sync", "5 Jan 2026", "Never", "Revoke"}},
		"Recent events": noUserEvents,
	}
	if diff := cmp.Diff(want, got.Tables); diff != "" {
		t.Errorf("tables (-want +got):\n%s", diff)
	}
	if diff := cmp.Diff("2026-01-05", got.Values["expires_on"]); diff != "" {
		t.Errorf("expiry (-want +got):\n%s", diff)
	}
}

func TestUserPageThatDoesNotExistSaysSo(t *testing.T) {
	t.Parallel()
	cases := map[string]func(b *testsupport.Browser) testsupport.Response{
		"an unknown id": func(b *testsupport.Browser) testsupport.Response { return b.Get("/admin/users/9") },
		"not an id":     func(b *testsupport.Browser) testsupport.Response { return b.Get("/admin/users/ann") },
		"deleting them": func(b *testsupport.Browser) testsupport.Response { return b.Post("/admin/users/9/delete") },
		"saving them": func(b *testsupport.Browser) testsupport.Response {
			return b.Post("/admin/users/9", "email", "x@example.com", "display_name", "X")
		},
		"setting their code": func(b *testsupport.Browser) testsupport.Response {
			return b.Post("/admin/users/9/code", "code", "123456")
		},
		"giving them a token":  func(b *testsupport.Browser) testsupport.Response { return b.Post("/admin/users/9/tokens", "name", "x") },
		"revoking their token": func(b *testsupport.Browser) testsupport.Response { return b.Post("/admin/users/9/tokens/1/revoke") },
	}
	for name, request := range cases {
		t.Run(name, func(t *testing.T) {
			b := signedInAdmin(t, testsupport.NewFactory())
			b.Get("/")

			got := request(b)

			want := testsupport.Page{Title: "Not found · doorcode", Heading: "Not found", Nav: usersNav}
			if diff := cmp.Diff([]any{404, want}, []any{got.Status, pageOf(got)}); diff != "" {
				t.Errorf("status and page (-want +got):\n%s", diff)
			}
		})
	}
}

func TestSavingAUserFromTheirPage(t *testing.T) {
	t.Parallel()
	b := signedInAdmin(t, testsupport.NewFactory())
	createUser(t, b, ann)

	saved := b.Post("/admin/users/2", "email", "ann@example.com", "user_id", "", "first_name", "Ann",
		"last_name", "Jones", "display_name", "", "expires_on", "")
	got := pageOf(b.Get(saved.Location))

	if diff := cmp.Diff([]any{303, "/admin/users/2"}, []any{saved.Status, saved.Location}); diff != "" {
		t.Errorf("redirect (-want +got):\n%s", diff)
	}
	want := []any{"Ann Jones", []string{"The user's changes have been saved."}, ""}
	if diff := cmp.Diff(want, []any{got.Heading, got.Messages, got.Values["user_id"]}); diff != "" {
		t.Errorf("heading, messages, user id (-want +got):\n%s", diff)
	}
}

func TestSavingAUserFromTheirPageSaysWhatIsWrong(t *testing.T) {
	t.Parallel()
	cases := map[string]struct {
		fields []string
		errors map[string]string
	}{
		"a problem": {fields: []string{"email", "nobody", "display_name", "Ann"},
			errors: map[string]string{"email": "Enter an email address like name@example.com."}},
		"not a date": {fields: []string{"email", "ann@example.com", "display_name", "Ann", "expires_on", "2027-02-30"},
			errors: map[string]string{"expires_on": "Enter a date, or leave it empty for an account that never expires."}},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			b := signedInAdmin(t, testsupport.NewFactory())
			createUser(t, b, ann)

			got := b.Post("/admin/users/2", c.fields...)

			page := pageOf(got)
			if diff := cmp.Diff([]any{422, "Ann Smith", c.errors}, []any{got.Status, page.Heading, page.FieldErrors}); diff != "" {
				t.Errorf("status, heading, field errors (-want +got):\n%s", diff)
			}
		})
	}
}

func TestSettingADoorCodeFromTheUserPage(t *testing.T) {
	t.Parallel()
	b := signedInAdmin(t, testsupport.NewFactory())
	createUser(t, b, ann)

	set := b.Post("/admin/users/2/code", "code", "24681357")
	setPage := pageOf(b.Get(set.Location))
	hadCode := testsupport.JSONAt(b.Get("/api/v1/users/2").Body, "/has_code")
	cleared := b.Post("/admin/users/2/code/clear")
	clearedPage := pageOf(b.Get(cleared.Location))
	hasCode := testsupport.JSONAt(b.Get("/api/v1/users/2").Body, "/has_code")

	want := []any{
		"/admin/users/2", []string{"The door code has been set. It is stored only as a hash and cannot be shown."}, true,
		"/admin/users/2", []string{"The door code has been removed."}, false,
	}
	got := []any{set.Location, setPage.Messages, hadCode, cleared.Location, clearedPage.Messages, hasCode}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("after setting and after removing: redirect, messages, has code (-want +got):\n%s", diff)
	}
}

func TestDoorCodeFormSaysWhatIsWrong(t *testing.T) {
	t.Parallel()
	cases := map[string]struct {
		code, want string
	}{
		"not digits":     {code: "abcdef", want: "Enter 6 to 20 digits."},
		"another's code": {code: "654321", want: "Another user has this code. Choose a different one."},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			b := signedInAdmin(t, testsupport.NewFactory())
			createUser(t, b, ann)
			send(t, b, "PUT", "/api/v1/users/1/code", `{"code":"654321"}`, 204)

			got := b.Post("/admin/users/2/code", "code", c.code)

			if diff := cmp.Diff([]any{422, map[string]string{"code": c.want}}, []any{got.Status, pageOf(got).FieldErrors}); diff != "" {
				t.Errorf("status, field errors (-want +got):\n%s", diff)
			}
		})
	}
}

func TestDeletingAndRestoringAUserFromTheirPage(t *testing.T) {
	t.Parallel()
	b := signedInAdmin(t, testsupport.NewFactory())
	createUser(t, b, ann)

	deleted := b.Post("/admin/users/2/delete")
	deletedPage := pageOf(b.Get(deleted.Location))
	listed := pageOf(b.Get("/admin/users?show=deleted")).Tables["Users"]
	restored := b.Post("/admin/users/2/restore")
	restoredPage := pageOf(b.Get(restored.Location))

	want := []any{
		"/admin/users/2", []string{"The user has been deleted. They are refused everywhere until restored."},
		[]string{"Ann Smith", "ann@example.com", "100234", "", "Deleted"},
		"/admin/users/2", []string{"The user has been restored."},
	}
	got := []any{deleted.Location, deletedPage.Messages, listed[1], restored.Location, restoredPage.Messages}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("deleting, the deleted list, restoring (-want +got):\n%s", diff)
	}
}

func TestYourOwnPageSaysWhyItOffersNoDeletion(t *testing.T) {
	t.Parallel()
	got := pageOf(signedInAdmin(t, testsupport.NewFactory()).Get("/admin/users/1"))

	want := []string{"This is your own account. You cannot delete it, make it expire today or earlier, or remove your own admin role."}
	if diff := cmp.Diff(want, got.Notes); diff != "" {
		t.Errorf("notes (-want +got):\n%s", diff)
	}
}

func TestDeletingYourOwnAccountFromYourPageIsRefused(t *testing.T) {
	t.Parallel()
	got := signedInAdmin(t, testsupport.NewFactory()).Post("/admin/users/1/delete")

	page := pageOf(got)
	want := []any{409, "Administrator", []string{"You are signed in.", "You cannot delete your own account. Ask another administrator to do it."}}
	if diff := cmp.Diff(want, []any{got.Status, page.Heading, page.Messages}); diff != "" {
		t.Errorf("status, heading, messages (-want +got):\n%s", diff)
	}
}

func TestCreatingATokenForAUserFromTheirPageShowsItOnce(t *testing.T) {
	t.Parallel()
	h, b := adminSession(t, testsupport.NewFactory())
	createUser(t, b, ann)
	b.Get("/")

	created := b.Post("/admin/users/2/tokens", "name", "sync", "expires_in_days", "")
	page := pageOf(created)
	asAnn := tokenClient(h, page.Values["secret"]).Get("/api/v1/tokens")

	want := []any{201, firstSecret,
		[]string{"Token “sync” created. Copy it now: it is shown only this once and cannot be recovered."},
		[][]string{{"Name", "Created", "Expires", "Actions"}, {"sync", "5 Jan 2026", "Never", "Revoke"}}}
	if diff := cmp.Diff(want, []any{created.Status, page.Values["secret"], page.Messages, page.Tables["API tokens"]}); diff != "" {
		t.Errorf("status, secret, messages, tokens (-want +got):\n%s", diff)
	}
	if diff := cmp.Diff(200, asAnn.Status); diff != "" {
		t.Errorf("the token's call (-want +got):\n%s", diff)
	}
}

func TestTokenFormOnAUserPageSaysWhatIsWrong(t *testing.T) {
	t.Parallel()
	b := signedInAdmin(t, testsupport.NewFactory())
	createUser(t, b, ann)

	got := b.Post("/admin/users/2/tokens", "name", "", "expires_in_days", "")

	page := pageOf(got)
	if diff := cmp.Diff([]any{422, map[string]string{"name": "Enter a name for the token."}}, []any{got.Status, page.FieldErrors}); diff != "" {
		t.Errorf("status, field errors (-want +got):\n%s", diff)
	}
}

func TestRevokingAUsersTokenFromTheirPage(t *testing.T) {
	t.Parallel()
	cases := map[string]struct {
		token, message string
	}{
		"held":    {token: "1", message: "The token has been revoked. Anything still using it is refused from now on."},
		"unknown": {token: "7", message: "That token had already been revoked."},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			b := signedInAdmin(t, testsupport.NewFactory())
			createUser(t, b, ann)
			send(t, b, "POST", "/api/v1/users/2/tokens", `{"name":"sync"}`, 201)

			revoked := b.Post("/admin/users/2/tokens/" + c.token + "/revoke")
			page := pageOf(b.Get(revoked.Location))

			if diff := cmp.Diff([]any{"/admin/users/2", []string{c.message}}, []any{revoked.Location, page.Messages}); diff != "" {
				t.Errorf("redirect, messages (-want +got):\n%s", diff)
			}
		})
	}
}

func TestUserManagerWithoutAdminSeesUsersButNotTheirTokens(t *testing.T) {
	t.Parallel()
	_, b := adminHolding(t, testsupport.NewFactory(), `["manage-users"]`)
	b.Get("/")

	page := pageOf(b.Get("/admin/users/1"))
	token := b.Post("/admin/users/1/tokens", "name", "x", "expires_in_days", "")

	want := []any{
		[]string{"Dashboard", "People: Users (current)", "People: Requests", "Portal", "API tokens", "API docs"},
		map[string][][]string{"Groups": {{"Group", "Roles"}, {"Staff", "manage-users"}}, "Door access": noAccess},
		403,
	}
	if diff := cmp.Diff(want, []any{page.Nav, page.Tables, token.Status}); diff != "" {
		t.Errorf("nav, tables, creating a token (-want +got):\n%s", diff)
	}
}

func TestUserPagesAskForTheRole(t *testing.T) {
	t.Parallel()
	f := testsupport.NewFactory()
	b := signedInAdmin(t, f)
	f.StoreStub.RevokeRoles(adminEmail)

	got := b.Get("/admin/users")

	if diff := cmp.Diff([]any{403, "You do not have access to this page"}, []any{got.Status, pageOf(got).Heading}); diff != "" {
		t.Errorf("status, heading (-want +got):\n%s", diff)
	}
}

func TestUserPageStoreFailureShowsARequestID(t *testing.T) {
	t.Parallel()
	cases := map[string]struct {
		fail    func(s *testsupport.Store)
		request func(b *testsupport.Browser) testsupport.Response
	}{
		"list":         {fail: (*testsupport.Store).FailUsers, request: func(b *testsupport.Browser) testsupport.Response { return b.Get("/admin/users") }},
		"user":         {fail: (*testsupport.Store).FailUsers, request: func(b *testsupport.Browser) testsupport.Response { return b.Get("/admin/users/1") }},
		"user's dates": {fail: (*testsupport.Store).FailSettings, request: func(b *testsupport.Browser) testsupport.Response { return b.Get("/admin/users/1") }},
		"create's dates": {fail: (*testsupport.Store).FailSettings, request: func(b *testsupport.Browser) testsupport.Response {
			return b.Post("/admin/users", "email", "a@example.com", "display_name", "A")
		}},
		"user's tokens": {fail: (*testsupport.Store).FailTokens, request: func(b *testsupport.Browser) testsupport.Response { return b.Get("/admin/users/1") }},
		"create": {fail: (*testsupport.Store).FailUsers, request: func(b *testsupport.Browser) testsupport.Response {
			return b.Post("/admin/users", "email", "a@example.com", "display_name", "A")
		}},
		"save": {fail: (*testsupport.Store).FailUserWrites, request: func(b *testsupport.Browser) testsupport.Response {
			return b.Post("/admin/users/1", "email", "a@example.com", "display_name", "A")
		}},
		"set a code": {fail: (*testsupport.Store).FailUserWrites, request: func(b *testsupport.Browser) testsupport.Response {
			return b.Post("/admin/users/1/code", "code", "123456")
		}},
		"create a token": {fail: (*testsupport.Store).FailTokens, request: func(b *testsupport.Browser) testsupport.Response {
			return b.Post("/admin/users/1/tokens", "name", "x", "expires_in_days", "")
		}},
		"revoke a token": {fail: (*testsupport.Store).FailTokens, request: func(b *testsupport.Browser) testsupport.Response {
			return b.Post("/admin/users/1/tokens/1/revoke")
		}},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			f := testsupport.NewFactory()
			b := signedInAdmin(t, f)
			c.fail(f.StoreStub)

			got := c.request(b)

			if diff := cmp.Diff([]any{500, "Something went wrong"}, []any{got.Status, pageOf(got).Heading}); diff != "" {
				t.Errorf("status, heading (-want +got):\n%s", diff)
			}
		})
	}
}
