package entrypoint_test

import (
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/dakriy/doorcode/internal/testsupport"
)

// setPassword is a password an administrator sets for someone in these
// tests; anotherPassword replaces it.
const (
	setPassword     = "handed over in person"
	anotherPassword = "handed over once more"
)

// signsIn is the status signing in on h with email and password answers
// with: 303 when it starts a session, 422 when it is refused.
func signsIn(b *testsupport.Browser, email, password string) int {
	return b.Another().Post("/sign-in", "email", email, "password", password).Status
}

// annWithPassword is an administrator's browser on a site where ann (2)
// has setPassword, set through the API.
func annWithPassword(t *testing.T, f *testsupport.Factory) (testsupport.Handler, *testsupport.Browser) {
	t.Helper()
	h, b := adminSession(t, f)
	createUser(t, b, ann)
	send(t, b, "PUT", "/api/v1/users/2/password", `{"password":"`+setPassword+`"}`, 204)
	return h, b
}

// signedInAnn is a new browser on b's site signed in as ann with
// password.
func signedInAnn(t *testing.T, b *testsupport.Browser, password string) *testsupport.Browser {
	t.Helper()
	person := b.Another()
	if got := person.Post("/sign-in", "email", annEmail, "password", password); got.Status != 303 {
		t.Fatalf("ann's sign-in answered %d, want 303:\n%s", got.Status, got.Body)
	}
	return person
}

func TestAnAdministratorSetsAUsersPasswordFromTheirPage(t *testing.T) {
	t.Parallel()
	b := signedInAdmin(t, testsupport.NewFactory())
	createUser(t, b, ann)
	before := pageOf(b.Get("/admin/users/2")).Notes

	set := b.Post("/admin/users/2/password", "password", setPassword, "confirm_password", setPassword)
	page := pageOf(b.Get(set.Location))

	want := []any{
		[]string{"This user has no password, so they can sign in only with single sign-on.", passwordFormNote},
		303, "/admin/users/2",
		[]string{"The password has been set and the user's sessions have ended. It is stored only as a hash and cannot be shown: pass it on to them yourself."},
		[]string{"This user has a password. It is stored only as a hash, so it cannot be shown; set a new one to replace it.", passwordFormNote},
		true, 303,
	}
	got := []any{before, set.Status, set.Location, page.Messages, page.Notes,
		testsupport.JSONAt(b.Get("/api/v1/users/2").Body, "/has_password"), signsIn(b, annEmail, setPassword)}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("notes before, status, location, messages, notes after, has password, signing in with it (-want +got):\n%s", diff)
	}
}

// passwordFormNote is what an administrator's password form on Ann
// Smith's page says about the password.
const passwordFormNote = "Use at least 12 characters. Setting it ends every session Ann Smith has; their API tokens stay. " +
	"Pass the password on to them yourself: it cannot be shown again, and they can change it from their Password page."

func TestSettingAUsersPasswordEndsTheirSessionsAndKeepsTheirTokens(t *testing.T) {
	t.Parallel()
	f := testsupport.NewFactory()
	h, b := annWithPassword(t, f)
	annToken := userToken(t, h, b, "2")
	first := signedInAnn(t, b, setPassword)
	second := signedInAnn(t, b, setPassword)

	send(t, b, "PUT", "/api/v1/users/2/password", `{"password":"`+anotherPassword+`"}`, 204)

	want := []any{401, 401, 200, 200, 1, 422, 303}
	got := []any{first.Get("/api/v1/tokens").Status, second.Get("/api/v1/tokens").Status, annToken.Get("/api/v1/tokens").Status,
		b.Get("/api/v1/tokens").Status, f.StoreStub.Sessions(), signsIn(b, annEmail, setPassword), signsIn(b, annEmail, anotherPassword)}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("ann's two sessions, her token, the administrator's session, sessions left, the old password, the new (-want +got):\n%s", diff)
	}
}

func TestRemovingAUsersPasswordFromTheirPageLeavesOnlySingleSignOn(t *testing.T) {
	t.Parallel()
	f := testsupport.NewFactory()
	_, b := annWithPassword(t, f)
	annBrowser := signedInAnn(t, b, setPassword)

	cleared := b.Post("/admin/users/2/password/clear")
	page := pageOf(b.Get(cleared.Location))

	want := []any{303, "/admin/users/2",
		[]string{"The password has been removed and the user's sessions have ended. They can sign in only with single sign-on."},
		"This user has no password, so they can sign in only with single sign-on.",
		false, 401, 422}
	got := []any{cleared.Status, cleared.Location, page.Messages, page.Notes[0],
		testsupport.JSONAt(b.Get("/api/v1/users/2").Body, "/has_password"), annBrowser.Get("/api/v1/tokens").Status, signsIn(b, annEmail, setPassword)}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("status, location, messages, note, has password, ann's session, signing in (-want +got):\n%s", diff)
	}
}

func TestRemovingAUsersPasswordThroughTheAPI(t *testing.T) {
	t.Parallel()
	f := testsupport.NewFactory()
	_, b := annWithPassword(t, f)
	annBrowser := signedInAnn(t, b, setPassword)

	cleared := b.Send("DELETE", "/api/v1/users/2/password", "")

	want := []any{204, false, 401, 422}
	got := []any{cleared.Status, testsupport.JSONAt(b.Get("/api/v1/users/2").Body, "/has_password"),
		annBrowser.Get("/api/v1/tokens").Status, signsIn(b, annEmail, setPassword)}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("status, has password, ann's session, signing in (-want +got):\n%s", diff)
	}
}

func TestSettingOrRemovingAUsersPasswordIsLoggedWithoutThePassword(t *testing.T) {
	t.Parallel()
	cases := map[string]struct {
		change  func(b *testsupport.Browser) testsupport.Response
		message string
	}{
		"set from the page": {change: func(b *testsupport.Browser) testsupport.Response {
			return b.Post("/admin/users/2/password", "password", anotherPassword, "confirm_password", anotherPassword)
		}, message: "set a user's password"},
		"set through the API": {change: func(b *testsupport.Browser) testsupport.Response {
			return b.PutJSON("/api/v1/users/2/password", `{"password":"`+anotherPassword+`"}`)
		}, message: "set a user's password"},
		"removed from the page": {change: func(b *testsupport.Browser) testsupport.Response {
			return b.Post("/admin/users/2/password/clear")
		}, message: "removed a user's password"},
		"removed through the API": {change: func(b *testsupport.Browser) testsupport.Response {
			return b.Send("DELETE", "/api/v1/users/2/password", "")
		}, message: "removed a user's password"},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			f := testsupport.NewFactory()
			b := signedInAdmin(t, f)
			createUser(t, b, ann)
			logged := len(f.LogStub.Entries)

			c.change(b)

			want := []testsupport.LogEntry{{Level: "info", Message: c.message,
				Fields: map[string]string{"by": adminEmail, "email": annEmail, "client": "192.0.2.1"}}}
			if diff := cmp.Diff(want, f.LogStub.Entries[logged:]); diff != "" {
				t.Errorf("log (-want +got):\n%s", diff)
			}
		})
	}
}

func TestPasswordFormOnAUserPageSaysWhatIsWrong(t *testing.T) {
	t.Parallel()
	long := testsupport.Repeat("p", 1025)
	cases := map[string]struct {
		fields []string
		want   map[string]string
	}{
		"not repeated": {fields: []string{"password", setPassword, "confirm_password", "handed over"},
			want: map[string]string{"confirm_password": "The passwords do not match. Type the new one twice."}},
		"too short": {fields: []string{"password", "short", "confirm_password", "short"},
			want: map[string]string{"password": "Use at least 12 characters."}},
		"too long": {fields: []string{"password", long, "confirm_password", long},
			want: map[string]string{"password": "Use at most 1024 characters."}},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			b := signedInAdmin(t, testsupport.NewFactory())
			createUser(t, b, ann)

			got := b.Post("/admin/users/2/password", c.fields...)

			page := pageOf(got)
			if diff := cmp.Diff([]any{422, "Ann Smith", c.want, 422}, []any{got.Status, page.Heading, page.FieldErrors, signsIn(b, annEmail, setPassword)}); diff != "" {
				t.Errorf("status, heading, field errors, signing in (-want +got):\n%s", diff)
			}
		})
	}
}

func TestThePasswordAnAdministratorSetsFollowsTheSitesMinimumLength(t *testing.T) {
	t.Parallel()
	f := testsupport.NewFactory()
	f.EnvStub["DOORCODE_PASSWORD_MIN_LENGTH"] = "24"
	b := signedInAdmin(t, f)
	createUser(t, b, ann)

	refused := b.PutJSON("/api/v1/users/2/password", `{"password":"`+setPassword+`"}`)
	page := pageOf(b.Get("/admin/users/2"))

	want := []any{400, "Use at least 24 characters.", "Use at least 24 characters. Setting it ends every session Ann Smith has; their API tokens stay. " +
		"Pass the password on to them yourself: it cannot be shown again, and they can change it from their Password page."}
	if diff := cmp.Diff(want, []any{refused.Status, testsupport.JSONString(refused.Body, "/error"), page.Notes[1]}); diff != "" {
		t.Errorf("status, error, the form's note (-want +got):\n%s", diff)
	}
}

func TestPasswordRequestThroughTheAPIIsChecked(t *testing.T) {
	t.Parallel()
	cases := map[string]struct {
		body string
		want string
	}{
		"not json":  {body: `{"password":7}`, want: `body must be a JSON object with a string "password"`},
		"too short": {body: `{"password":"elevenchars"}`, want: "Use at least 12 characters."},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			b := signedInAdmin(t, testsupport.NewFactory())
			createUser(t, b, ann)

			got := b.PutJSON("/api/v1/users/2/password", c.body)

			if diff := cmp.Diff([]any{400, c.want}, []any{got.Status, testsupport.JSONString(got.Body, "/error")}); diff != "" {
				t.Errorf("status, error (-want +got):\n%s", diff)
			}
		})
	}
}

func TestAPasswordOfExactlyTheMinimumLengthMayBeSetForAUser(t *testing.T) {
	t.Parallel()
	b := signedInAdmin(t, testsupport.NewFactory())
	createUser(t, b, ann)

	got := b.PutJSON("/api/v1/users/2/password", `{"password":"twelve chars"}`)

	if diff := cmp.Diff([]any{204, 303}, []any{got.Status, signsIn(b, annEmail, "twelve chars")}); diff != "" {
		t.Errorf("status, signing in with it (-want +got):\n%s", diff)
	}
}

func TestAnAdministratorSetsTheirOwnPasswordOnlyAsThemselves(t *testing.T) {
	t.Parallel()
	b := signedInAdmin(t, testsupport.NewFactory())

	page := pageOf(b.Get("/admin/users/1"))
	fromPage := b.Post("/admin/users/1/password", "password", setPassword, "confirm_password", setPassword)
	clearedFromPage := b.Post("/admin/users/1/password/clear")
	fromAPI := b.PutJSON("/api/v1/users/1/password", `{"password":"`+setPassword+`"}`)
	clearedFromAPI := b.Send("DELETE", "/api/v1/users/1/password", "")

	own := "You cannot set your own password here. Change it from your Password page, which asks for the current one."
	apiOwn := "you cannot set your own password here; change it as yourself, giving your current one"
	want := []any{
		"Change your own password from your Password page, which asks for the current one.",
		409, own, 409, own,
		409, apiOwn, 409, apiOwn,
	}
	got := []any{page.Notes[1],
		fromPage.Status, lastMessage(fromPage), clearedFromPage.Status, lastMessage(clearedFromPage),
		fromAPI.Status, testsupport.JSONString(fromAPI.Body, "/error"), clearedFromAPI.Status, testsupport.JSONString(clearedFromAPI.Body, "/error")}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("the page's note, then status and refusal from the page and the API (-want +got):\n%s", diff)
	}
}

// lastMessage is the last message the page r answers with shows.
func lastMessage(r testsupport.Response) string {
	messages := pageOf(r).Messages
	return messages[len(messages)-1]
}

func TestAUserManagerSetsThePasswordOnlyOfThoseWhoseRolesTheyHold(t *testing.T) {
	t.Parallel()
	outranked := "You can only set or remove the password of a user when you hold every role they hold."
	apiOutranked := "you can only set the password of a user whose roles you all hold"
	cases := map[string]struct {
		// roles are the roles of the group (2) ann (2) joins.
		roles    string
		status   int
		pageNote string
	}{
		"a user without roles":                    {roles: `[]`, status: 204, pageNote: passwordFormNote},
		"a user holding a role the manager holds": {roles: `["manage-users"]`, status: 204, pageNote: passwordFormNote},
		"a user holding a role the manager lacks": {roles: `["manage-doors"]`, status: 403,
			pageNote: "Ann Smith holds a role you do not, so only someone who holds every role they hold can set their password."},
		"an administrator": {roles: `["admin"]`, status: 403,
			pageNote: "Ann Smith holds a role you do not, so only someone who holds every role they hold can set their password."},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			f := testsupport.NewFactory()
			admin := signedInAdmin(t, f)
			createUser(t, admin, ann)
			createGroup(t, admin, `{"slug":"ann","name":"Ann's","roles":`+c.roles+`}`)
			join(t, admin, "2", "2")
			manager := userManager(t, admin)

			page := pageOf(manager.Get("/admin/users/2"))
			set := manager.PutJSON("/api/v1/users/2/password", `{"password":"`+setPassword+`"}`)

			if diff := cmp.Diff([]any{c.pageNote, c.status}, []any{page.Notes[1], set.Status}); diff != "" {
				t.Errorf("the page's note, setting through the API (-want +got):\n%s", diff)
			}
			if c.status == 403 {
				fromPage := manager.Post("/admin/users/2/password", "password", setPassword, "confirm_password", setPassword)
				cleared := manager.Send("DELETE", "/api/v1/users/2/password", "")
				want := []any{apiOutranked, 403, outranked, 403, 422}
				got := []any{testsupport.JSONString(set.Body, "/error"), fromPage.Status, lastMessage(fromPage), cleared.Status, signsIn(admin, annEmail, setPassword)}
				if diff := cmp.Diff(want, got); diff != "" {
					t.Errorf("the API's refusal, the page's, removing, signing in (-want +got):\n%s", diff)
				}
			}
		})
	}
}

// userManager creates bo (3), a member of a group (3) carrying only
// manage-users, as the administrator b, and returns a browser signed in
// as bo.
func userManager(t *testing.T, b *testsupport.Browser) *testsupport.Browser {
	t.Helper()
	createUser(t, b, `{"email":"bo@example.com","display_name":"Bo"}`)
	createGroup(t, b, `{"slug":"user-managers","name":"User managers","roles":["manage-users"]}`)
	join(t, b, "3", "3")
	send(t, b, "PUT", "/api/v1/users/3/password", `{"password":"bo's own password"}`, 204)
	bo := b.Another()
	if got := bo.Post("/sign-in", "email", "bo@example.com", "password", "bo's own password"); got.Status != 303 {
		t.Fatalf("bo's sign-in answered %d, want 303:\n%s", got.Status, got.Body)
	}
	bo.Get("/")
	return bo
}

func TestSettingAPasswordNeedsTheRoleThatManagesUsers(t *testing.T) {
	t.Parallel()
	h, b := annWithPassword(t, testsupport.NewFactory())
	annToken := userToken(t, h, b, "2")
	annBrowser := signedInAnn(t, b, setPassword)

	want := []any{403, 403, 403, 403}
	got := []any{annToken.PutJSON("/api/v1/users/1/password", `{"password":"`+setPassword+`"}`).Status,
		annToken.Send("DELETE", "/api/v1/users/1/password", "").Status,
		annBrowser.Post("/admin/users/1/password", "password", setPassword, "confirm_password", setPassword).Status,
		annBrowser.Post("/admin/users/1/password/clear").Status}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("setting and removing through the API and from the page (-want +got):\n%s", diff)
	}
}

func TestUnderAutoSignInAnAdministratorStillSetsAndRemovesAUsersPassword(t *testing.T) {
	t.Parallel()
	f := testsupport.NewFactory()
	autoSignIn(f)
	b := signedInAdmin(t, f)
	createUser(t, b, ann)

	page := pageOf(b.Get("/admin/users/2"))
	setFromPage := b.Post("/admin/users/2/password", "password", setPassword, "confirm_password", setPassword)
	signedInFromPage := signsIn(b, annEmail, setPassword)
	clearedFromPage := b.Post("/admin/users/2/password/clear")
	setFromAPI := b.PutJSON("/api/v1/users/2/password", `{"password":"`+anotherPassword+`"}`)
	signedInFromAPI := signsIn(b, annEmail, anotherPassword)
	clearedFromAPI := b.Send("DELETE", "/api/v1/users/2/password", "")

	want := []any{"Auto sign-in sends everyone to single sign-on, so they reach the password form at /sign-in?auto_sign_in=false.",
		303, 303, 303, 204, 303, 204, false}
	got := []any{page.Notes[2], setFromPage.Status, signedInFromPage, clearedFromPage.Status,
		setFromAPI.Status, signedInFromAPI, clearedFromAPI.Status, testsupport.JSONAt(b.Get("/api/v1/users/2").Body, "/has_password")}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("the page's note; from the page: setting, signing in, removing; through the API: the same; has password (-want +got):\n%s", diff)
	}
}

func TestUnderAutoSignInAnAdministratorChangesTheirOwnPasswordInThePortal(t *testing.T) {
	t.Parallel()
	f := testsupport.NewFactory()
	autoSignIn(f)
	b := signedInAdmin(t, f)
	b.Get("/")
	current := adminPassword(t, f)

	page := pageOf(b.Get("/me/password"))
	wrong := b.Post("/me/password", "current_password", "a guess", "new_password", setPassword, "confirm_password", setPassword)
	changed := b.Post("/me/password", "current_password", current, "new_password", setPassword, "confirm_password", setPassword)
	signedIn := signsIn(b, adminEmail, setPassword)
	throughAPI := b.PutJSON("/api/v1/me/password", `{"current_password":"`+setPassword+`","new_password":"`+anotherPassword+`"}`)

	want := []any{
		testsupport.Page{Title: "Password · doorcode", Heading: "Password", Nav: heldPortalNav("Password"), Notes: []string{lengthNote}},
		422, map[string]string{"current_password": "That is not your current password."},
		303, 303, 204, 303,
	}
	got := []any{page, wrong.Status, pageOf(wrong).FieldErrors, changed.Status, signedIn, throughAPI.Status, signsIn(b, adminEmail, anotherPassword)}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("page; a wrong current password; changing, signing in; changing through the API, signing in (-want +got):\n%s", diff)
	}
}

func TestSettingThePasswordOfAUserWhoDoesNotExist(t *testing.T) {
	t.Parallel()
	b := signedInAdmin(t, testsupport.NewFactory())
	b.Get("/")

	fromPage := b.Post("/admin/users/9/password", "password", setPassword, "confirm_password", setPassword)
	clearedFromPage := b.Post("/admin/users/9/password/clear")
	fromAPI := b.PutJSON("/api/v1/users/9/password", `{"password":"`+setPassword+`"}`)
	clearedFromAPI := b.Send("DELETE", "/api/v1/users/9/password", "")

	want := []any{404, "Not found", 404, "Not found", 404, "no user has this id", 404}
	got := []any{fromPage.Status, pageOf(fromPage).Heading, clearedFromPage.Status, pageOf(clearedFromPage).Heading,
		fromAPI.Status, testsupport.JSONString(fromAPI.Body, "/error"), clearedFromAPI.Status}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("statuses and what they say (-want +got):\n%s", diff)
	}
}

func TestSettingAPasswordWhenTheStoreFailsAnswersWithARequestID(t *testing.T) {
	t.Parallel()
	setFromPage := func(b, _ *testsupport.Browser) testsupport.Response {
		return b.Post("/admin/users/2/password", "password", setPassword, "confirm_password", setPassword)
	}
	setFromAPI := func(_, token *testsupport.Browser) testsupport.Response {
		return token.PutJSON("/api/v1/users/2/password", `{"password":"`+setPassword+`"}`)
	}
	clearFromPage := func(b, _ *testsupport.Browser) testsupport.Response { return b.Post("/admin/users/2/password/clear") }
	clearFromAPI := func(_, token *testsupport.Browser) testsupport.Response {
		return token.Send("DELETE", "/api/v1/users/2/password", "")
	}
	cases := map[string]struct {
		fail func(s *testsupport.Store)
		// request is made by an administrator's browser or token.
		request func(b, token *testsupport.Browser) testsupport.Response
	}{
		"setting without user writes":  {fail: (*testsupport.Store).FailUserWrites, request: setFromPage},
		"setting without sessions":     {fail: (*testsupport.Store).FailSessions, request: setFromAPI},
		"setting without settings":     {fail: (*testsupport.Store).FailSettings, request: setFromAPI},
		"setting without users":        {fail: (*testsupport.Store).FailUsers, request: setFromAPI},
		"removing without user writes": {fail: (*testsupport.Store).FailUserWrites, request: clearFromPage},
		"removing without sessions":    {fail: (*testsupport.Store).FailSessions, request: clearFromAPI},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			f := testsupport.NewFactory()
			h, b := adminSession(t, f)
			createUser(t, b, ann)
			token := tokenClient(h, createToken(t, b, `{"name":"admin"}`))
			c.fail(f.StoreStub)

			got := c.request(b, token)

			if diff := cmp.Diff(500, got.Status); diff != "" {
				t.Errorf("status (-want +got):\n%s", diff)
			}
		})
	}
}

// adminClaims is what the provider says about the first administrator,
// keeping them in Administrators.
func adminClaims() map[string]any {
	return map[string]any{"sub": "admin-sub", "email": adminEmail, "email_verified": true, "name": "Administrator", "groups": []any{"administrators"}}
}

func TestUnderAutoSignInTheOwnPasswordFormIsHiddenFromAProviderSession(t *testing.T) {
	t.Parallel()
	f := testsupport.NewFactory()
	h, _ := providerSite(t, f)
	ann, _ := viaProvider(t, f, h, annClaims())
	f.EnvStub["DOORCODE_OIDC_AUTO_SIGN_IN"] = "true"

	home := pageOf(ann.Get("/"))
	page := pageOf(ann.Get("/me/password"))
	posted := ann.Post("/me/password", "new_password", setPassword, "confirm_password", setPassword)
	throughAPI := ann.PutJSON("/api/v1/me/password", `{"new_password":"`+setPassword+`"}`)

	cards := homeCards(false, "")
	delete(cards, "Password")
	want := []any{hiddenPasswordPage, cards, 403, hiddenPasswordPage, 403, "passwords are not used here; everyone signs in with single sign-on"}
	got := []any{page, home.Details, posted.Status, pageOf(posted),
		throughAPI.Status, testsupport.JSONString(throughAPI.Body, "/error")}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("page, home card, posting, the API (-want +got):\n%s", diff)
	}
}

func TestUnderAutoSignInAnAdministratorKeepsTheOwnPasswordFormAfterSigningInAtTheProvider(t *testing.T) {
	t.Parallel()
	f := testsupport.NewFactory()
	h, _ := providerSite(t, f)
	admin, _ := viaProvider(t, f, h, adminClaims())
	f.EnvStub["DOORCODE_OIDC_AUTO_SIGN_IN"] = "true"

	page := pageOf(admin.Get("/me/password"))
	changed := admin.Post("/me/password", "current_password", adminPassword(t, f), "new_password", setPassword, "confirm_password", setPassword)

	want := []any{[]string{lengthNote}, 303, 303}
	if diff := cmp.Diff(want, []any{page.Notes, changed.Status, signsIn(admin, adminEmail, setPassword)}); diff != "" {
		t.Errorf("the page's notes, changing, signing in (-want +got):\n%s", diff)
	}
}

func TestUnderAutoSignInAnAPITokenChangesItsOwnersPasswordOnlyForAnAdministrator(t *testing.T) {
	t.Parallel()
	f := testsupport.NewFactory()
	autoSignIn(f)
	h, b := annWithPassword(t, f)
	adminToken := tokenClient(h, createToken(t, b, `{"name":"admin"}`))
	annToken := userToken(t, h, b, "2")

	byAdmin := adminToken.PutJSON("/api/v1/me/password", `{"current_password":"`+adminPassword(t, f)+`","new_password":"`+anotherPassword+`"}`)
	byAnn := annToken.PutJSON("/api/v1/me/password", `{"current_password":"`+setPassword+`","new_password":"`+anotherPassword+`"}`)

	want := []any{204, 303, 403, 303}
	got := []any{byAdmin.Status, signsIn(b, adminEmail, anotherPassword), byAnn.Status, signsIn(b, annEmail, setPassword)}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("the administrator's token, signing in; ann's token, signing in with her old password (-want +got):\n%s", diff)
	}
}
