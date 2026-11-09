package entrypoint_test

import (
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/dakriy/doorcode/internal/testsupport"
)

// providerButton is the label of the sign-in page's link to the identity
// provider.
const providerButton = "Sign in with single sign-on"

// The messages the sign-in page shows when a sign-in at the provider is
// refused.
const (
	staleSignIn  = "This sign-in took too long or was started in another window. Try again."
	unverified   = "The identity provider's answer could not be verified. Try again."
	unreachable  = "The identity provider could not be reached. Try again in a moment."
	providerSays = "could not sign in at the identity provider"
)

// providerSite is a site whose environment turns signing in at the
// identity provider https://id.example.com on, as the client doorcode of
// https://doorcode.example.com, reading the user id from employee_id and
// entitlements from groups, and naming visitors as the group new users
// join while no entitlements claim is read. It has the groups Staff (2,
// staff), Contractors (3, contractors), Visitors (4, visitors), and Lab
// (5, lab), and returns its handler and the administrator's browser.
func providerSite(t *testing.T, f *testsupport.Factory) (testsupport.Handler, *testsupport.Browser) {
	t.Helper()
	for name, value := range map[string]string{
		"DOORCODE_OIDC_ENABLED": "true", "DOORCODE_OIDC_ISSUER": "https://id.example.com", "DOORCODE_OIDC_CLIENT_ID": "doorcode",
		"DOORCODE_PUBLIC_URL": "https://doorcode.example.com", "DOORCODE_OIDC_CLAIM_USER_ID": "employee_id",
		"DOORCODE_OIDC_CLAIM_ENTITLEMENTS": "groups",
		"DOORCODE_OIDC_DEFAULT_GROUPS":     "visitors",
	} {
		f.EnvStub[name] = value
	}
	h, b := adminSession(t, f)
	for _, group := range []string{`"staff","name":"Staff"`, `"contractors","name":"Contractors"`, `"visitors","name":"Visitors"`, `"lab","name":"Lab"`} {
		createGroup(t, b, `{"slug":`+group+`}`)
	}
	return h, b
}

// unentitledSite is providerSite reading no entitlements claim, so the
// site decides the groups of whoever signs in.
func unentitledSite(t *testing.T, f *testsupport.Factory) (testsupport.Handler, *testsupport.Browser) {
	t.Helper()
	h, b := providerSite(t, f)
	delete(f.EnvStub, "DOORCODE_OIDC_CLAIM_ENTITLEMENTS")
	return h, b
}

// createInterns creates the group Interns (6, interns), whose pre-assigned
// list holds Ann's user id.
func createInterns(t *testing.T, c *testsupport.Browser) {
	t.Helper()
	createGroup(t, c, `{"slug":"interns","name":"Interns","preassigned_user_ids":["100234"]}`)
}

// annClaims is what the provider says about Ann, with each pair of extra
// setting a claim, or removing it when its value is nil. Her employee id
// is a number, as some providers send it.
func annClaims(extra ...any) map[string]any {
	claims := map[string]any{
		"sub": "ann-sub", "email": "ann@example.com", "email_verified": true, "given_name": "Ann", "family_name": "Smith",
		"name": "Ann Smith", "employee_id": float64(100234), "groups": []any{"staff", "contractors", 7},
	}
	for i := 0; i < len(extra); i += 2 {
		name := extra[i].(string)
		if extra[i+1] == nil {
			delete(claims, name)
			continue
		}
		claims[name] = extra[i+1]
	}
	return claims
}

// viaProvider has a new browser on h sign in at the provider, where the
// person is who claims say, and returns the browser and the answer to its
// return from the provider.
func viaProvider(t *testing.T, f *testsupport.Factory, h testsupport.Handler, claims map[string]any) (*testsupport.Browser, testsupport.Response) {
	t.Helper()
	b := testsupport.NewBrowser(h)
	started := b.Get("/sign-in/oidc")
	return b, b.Get(f.IdentityStub.Authorize(started.Location, claims))
}

// groupSlugs is the slugs of the groups of the user whose id is id, as the
// API lists them through c.
func groupSlugs(t *testing.T, c *testsupport.Browser, id string) []any {
	t.Helper()
	var slugs []any
	for _, g := range testsupport.JSONAt(send(t, c, "GET", "/api/v1/users/"+id, "", 200).Body, "/groups").([]any) {
		slugs = append(slugs, g.(map[string]any)["slug"])
	}
	return slugs
}

// refusedPage is the sign-in page saying message, offering the provider
// and, unless hidden, the password form.
func refusedPage(message string, password bool) testsupport.Page {
	page := testsupport.Page{
		Title: "Sign in · doorcode", Heading: "Sign in", Messages: []string{message},
		LinkTargets: map[string]string{providerButton: "/sign-in/oidc?next=%2f"},
	}
	if password {
		page.Values = map[string]string{"email": "", "next": "/"}
	}
	return page
}

func TestANewIdentitySignsInAsAMemberOfTheGroupsItsEntitlementsName(t *testing.T) {
	t.Parallel()
	f := testsupport.NewFactory()
	h, admin := providerSite(t, f)
	createInterns(t, admin)

	b, got := viaProvider(t, f, h, annClaims())
	landed := pageOf(b.Get(got.Location))
	user := testsupport.JSON(send(t, admin, "GET", "/api/v1/users/2", "", 200).Body).(map[string]any)

	want := []any{303, "/me/code", "Door code", []string{"Welcome! Choose your door code to finish setting up your account."},
		"ann@example.com", "100234", "Ann", "Smith", "Ann Smith", "2031-01-05T09:00:00Z", []any{"contractors", "staff"}}
	if diff := cmp.Diff(want, []any{got.Status, got.Location, landed.Heading, landed.Messages, user["email"], user["user_id"], user["first_name"],
		user["last_name"], user["display_name"], user["expires_at"], groupSlugs(t, admin, "2")}); diff != "" {
		t.Errorf("status, location, heading and messages where it lands, email, user id, names, expiry, groups (-want +got):\n%s", diff)
	}
}

func TestWithoutAnEntitlementsClaimNameANewIdentityJoinsTheDefaultAndPreassignedGroups(t *testing.T) {
	t.Parallel()
	f := testsupport.NewFactory()
	h, admin := unentitledSite(t, f)
	createInterns(t, admin)

	viaProvider(t, f, h, annClaims())

	if diff := cmp.Diff([]any{"interns", "visitors"}, groupSlugs(t, admin, "2")); diff != "" {
		t.Errorf("groups (-want +got):\n%s", diff)
	}
}

func TestAFirstSignInTakesTheUserIDOffTheListsOfTheGroupsItJoins(t *testing.T) {
	t.Parallel()
	f := testsupport.NewFactory()
	h, admin := unentitledSite(t, f)
	createInterns(t, admin)
	createGroup(t, admin, `{"slug":"night","name":"Night","preassigned_user_ids":["100877","100234"]}`)

	viaProvider(t, f, h, annClaims())
	interns := testsupport.JSONAt(send(t, admin, "GET", "/api/v1/groups/6", "", 200).Body, "/preassigned_user_ids")
	night := pageOf(admin.Get("/admin/groups/7")).Values["preassigned_user_ids"]

	if diff := cmp.Diff([]any{[]any{}, "100877", []any{"interns", "night", "visitors"}}, []any{interns, night, groupSlugs(t, admin, "2")}); diff != "" {
		t.Errorf("Interns' pre-assigned ids, Night's on its page, Ann's groups (-want +got):\n%s", diff)
	}
}

func TestAFirstSignInThatCannotTakeTheUserIDOffAListSeesTheErrorPage(t *testing.T) {
	t.Parallel()
	f := testsupport.NewFactory()
	h, admin := unentitledSite(t, f)
	createInterns(t, admin)
	b := testsupport.NewBrowser(h)
	back := f.IdentityStub.Authorize(b.Get("/sign-in/oidc").Location, annClaims())
	f.StoreStub.FailGroupWrites()

	got := b.Get(back)

	if diff := cmp.Diff([]any{500, "stub: unavailable"}, []any{got.Status, lastLog(f).Fields["error"]}); diff != "" {
		t.Errorf("status, log (-want +got):\n%s", diff)
	}
}

func TestASignInMakesTheUserAMemberOfExactlyTheExistingGroupsItsEntitlementsName(t *testing.T) {
	t.Parallel()
	f := testsupport.NewFactory()
	h, admin := providerSite(t, f)
	viaProvider(t, f, h, annClaims())
	join(t, admin, "5", "2")

	_, got := viaProvider(t, f, h, annClaims("groups", []any{"staff", "visitors", "no-such-group"}))
	groups := testsupport.JSONAt(send(t, admin, "GET", "/api/v1/groups", "", 200).Body, "/groups").([]any)

	if diff := cmp.Diff([]any{303, []any{"staff", "visitors"}, 5}, []any{got.Status, groupSlugs(t, admin, "2"), len(groups)}); diff != "" {
		t.Errorf("status, groups, how many groups there are (-want +got):\n%s", diff)
	}
}

func TestASignInWithoutTheEntitlementsClaimLeavesTheUsersGroupsAlone(t *testing.T) {
	t.Parallel()
	f := testsupport.NewFactory()
	h, admin := providerSite(t, f)
	viaProvider(t, f, h, annClaims())
	join(t, admin, "5", "2")

	_, got := viaProvider(t, f, h, annClaims("groups", nil))

	if diff := cmp.Diff([]any{303, []any{"contractors", "lab", "staff"}}, []any{got.Status, groupSlugs(t, admin, "2")}); diff != "" {
		t.Errorf("status, groups (-want +got):\n%s", diff)
	}
}

func TestANewIdentityWithoutTheEntitlementsClaimJoinsNoDefaultOrPreassignedGroup(t *testing.T) {
	t.Parallel()
	f := testsupport.NewFactory()
	h, admin := providerSite(t, f)
	createInterns(t, admin)

	_, got := viaProvider(t, f, h, annClaims("groups", nil))
	interns := testsupport.JSONAt(send(t, admin, "GET", "/api/v1/groups/6", "", 200).Body, "/preassigned_user_ids")

	if diff := cmp.Diff([]any{303, []any(nil), []any{"100234"}}, []any{got.Status, groupSlugs(t, admin, "2"), interns}); diff != "" {
		t.Errorf("status, groups, Interns' pre-assigned ids (-want +got):\n%s", diff)
	}
}

func TestWithoutAnEntitlementsClaimNameSigningInLeavesTheUsersGroupsAlone(t *testing.T) {
	t.Parallel()
	f := testsupport.NewFactory()
	h, admin := unentitledSite(t, f)
	createUser(t, admin, `{"email":"ann@example.com","first_name":"Ann"}`)
	join(t, admin, "3", "2")

	viaProvider(t, f, h, annClaims("groups", []any{"staff"}))

	if diff := cmp.Diff([]any{"contractors"}, groupSlugs(t, admin, "2")); diff != "" {
		t.Errorf("groups (-want +got):\n%s", diff)
	}
}

func TestAnExistingUserMatchedByEmailIsUpdated(t *testing.T) {
	t.Parallel()
	f := testsupport.NewFactory()
	h, admin := providerSite(t, f)
	createUser(t, admin, `{"email":"ann@example.com","first_name":"Annie","last_name":"S","display_name":"Annie"}`)

	_, got := viaProvider(t, f, h, annClaims("email", "ANN@example.com", "employee_id", "100234"))
	user := testsupport.JSON(send(t, admin, "GET", "/api/v1/users/2", "", 200).Body).(map[string]any)
	total := testsupport.JSONAt(send(t, admin, "GET", "/api/v1/users", "", 200).Body, "/total")

	want := []any{303, "ann@example.com", "100234", "Ann", "Smith", "Ann Smith", nil, []any{"contractors", "staff"}, float64(2)}
	if diff := cmp.Diff(want, []any{got.Status, user["email"], user["user_id"], user["first_name"], user["last_name"],
		user["display_name"], user["expires_at"], groupSlugs(t, admin, "2"), total}); diff != "" {
		t.Errorf("status, email, user id, names, expiry, groups, users (-want +got):\n%s", diff)
	}
}

func TestAnExistingUserMatchedByUserIDTakesTheProvidersEmailAndKeepsWhatTheProviderDoesNotSay(t *testing.T) {
	t.Parallel()
	f := testsupport.NewFactory()
	h, admin := providerSite(t, f)
	createUser(t, admin, `{"email":"old@example.com","user_id":"100234","first_name":"Annie","last_name":"S","display_name":"Annie S"}`)
	f.EnvStub["DOORCODE_OIDC_CLAIM_DISPLAY_NAME"] = ""
	f.EnvStub["DOORCODE_OIDC_CLAIM_LAST_NAME"] = ""
	f.EnvStub["DOORCODE_OIDC_CLAIM_FIRST_NAME"] = "first"

	viaProvider(t, f, h, annClaims("employee_id", "100234", "email", "new@example.com", "first", "Ann"))
	user := testsupport.JSON(send(t, admin, "GET", "/api/v1/users/2", "", 200).Body).(map[string]any)

	want := []any{"new@example.com", "Ann", "S", "Annie S"}
	if diff := cmp.Diff(want, []any{user["email"], user["first_name"], user["last_name"], user["display_name"]}); diff != "" {
		t.Errorf("email, names (-want +got):\n%s", diff)
	}
}

func TestANewIdentityWithoutNamesIsCalledByItsEmail(t *testing.T) {
	t.Parallel()
	f := testsupport.NewFactory()
	h, admin := providerSite(t, f)

	viaProvider(t, f, h, annClaims("given_name", "", "family_name", "", "name", ""))

	got := testsupport.JSONAt(send(t, admin, "GET", "/api/v1/users/2", "", 200).Body, "/display_name")
	if diff := cmp.Diff("ann@example.com", got); diff != "" {
		t.Errorf("display name (-want +got):\n%s", diff)
	}
}

func TestClaimsTheIDTokenLacksComeFromTheUserinfoEndpoint(t *testing.T) {
	t.Parallel()
	f := testsupport.NewFactory()
	h, admin := providerSite(t, f)
	b := testsupport.NewBrowser(h)
	started := b.Get("/sign-in/oidc")
	back := f.IdentityStub.AuthorizeWithUserInfo(started.Location, annClaims("family_name", nil, "groups", nil),
		map[string]any{"sub": "ann-sub", "family_name": "Jones", "given_name": "Not used", "groups": "staff"})

	got := b.Get(back)
	user := testsupport.JSON(send(t, admin, "GET", "/api/v1/users/2", "", 200).Body).(map[string]any)

	want := []any{303, "Ann", "Jones", []any{"staff"}}
	if diff := cmp.Diff(want, []any{got.Status, user["first_name"], user["last_name"], groupSlugs(t, admin, "2")}); diff != "" {
		t.Errorf("status, names, groups (-want +got):\n%s", diff)
	}
}

func TestASignInIsRefusedWithWhatWentWrong(t *testing.T) {
	t.Parallel()
	cases := map[string]struct {
		arrange func(f *testsupport.Factory, admin *testsupport.Browser)
		claims  map[string]any
		status  int
		message string
		logged  string
	}{
		"when the ID token carries another nonce": {
			claims: annClaims("nonce", "another"), status: 403, message: unverified,
			logged: "the identity provider's answer could not be verified: the ID token does not carry the nonce sent",
		},
		"when the token endpoint refuses the client secret": {
			arrange: func(f *testsupport.Factory, _ *testsupport.Browser) { f.IdentityStub.ClientSecret = "s3cret" },
			claims:  annClaims(), status: 403, message: unverified,
			logged: "the identity provider's answer could not be verified: stub: invalid_grant",
		},
		"when the userinfo endpoint fails": {
			arrange: func(f *testsupport.Factory, _ *testsupport.Browser) { f.IdentityStub.FailUserInfo() },
			claims:  annClaims("name", nil), status: 502, message: unreachable,
			logged: "the identity provider could not be reached: stub: unavailable",
		},
		"when the provider gives no email": {
			claims: annClaims("email", nil), status: 403,
			message: "The identity provider did not say what your email address is, so you cannot be signed in. Ask an administrator.",
			logged:  "the identity provider gave no email address",
		},
		"when the provider has not verified the email": {
			claims: annClaims("email_verified", false), status: 403,
			message: "The identity provider has not verified your email address, so you cannot be signed in.",
			logged:  "the identity provider has not verified the email address",
		},
		"when the account has expired": {
			arrange: func(f *testsupport.Factory, admin *testsupport.Browser) {
				createUser(t, admin, `{"email":"ann@example.com","first_name":"Ann"}`)
				f.StoreStub.ExpireUser("ann@example.com", testsupport.DefaultNow)
			},
			claims: annClaims(), status: 403, message: "This account has expired. Ask an administrator to renew it.", logged: "account expired",
		},
		"when the account is deleted": {
			arrange: func(f *testsupport.Factory, admin *testsupport.Browser) {
				createUser(t, admin, `{"email":"ann@example.com","first_name":"Ann"}`)
				f.StoreStub.DeleteUser("ann@example.com", testsupport.DefaultNow)
			},
			claims: annClaims(), status: 403, message: "This account has expired. Ask an administrator to renew it.", logged: "account expired",
		},
		"when the user id is not one": {
			claims: annClaims("employee_id", "not one!"), status: 403,
			message: "Your account could not be set up from what the identity provider said about you: " +
				"Use up to 64 letters, digits, dots, dashes, or underscores.",
			logged: "Use up to 64 letters, digits, dots, dashes, or underscores.",
		},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			f := testsupport.NewFactory()
			h, admin := providerSite(t, f)
			if c.arrange != nil {
				c.arrange(f, admin)
			}

			b, got := viaProvider(t, f, h, c.claims)

			want := []any{c.status, refusedPage(c.message, true), "/sign-in?next=%2F",
				testsupport.LogEntry{Level: "error", Message: providerSays, Fields: map[string]string{"error": c.logged, "client": testsupport.ClientAddress}}}
			if diff := cmp.Diff(want, []any{got.Status, pageOf(got), b.Get("/").Location, lastLog(f)}); diff != "" {
				t.Errorf("status, page, where home sends the browser, log (-want +got):\n%s", diff)
			}
		})
	}
}

func TestAnAnswerThatIsNotTheBrowsersSignInIsRefused(t *testing.T) {
	t.Parallel()
	cases := map[string]func(f *testsupport.Factory, h testsupport.Handler) (*testsupport.Browser, testsupport.Response){
		"with another state": func(f *testsupport.Factory, h testsupport.Handler) (*testsupport.Browser, testsupport.Response) {
			b := testsupport.NewBrowser(h)
			f.IdentityStub.Authorize(b.Get("/sign-in/oidc").Location, annClaims())
			return b, b.Get("/sign-in/oidc/callback?code=code-1&state=another")
		},
		"in a browser that started none": func(f *testsupport.Factory, h testsupport.Handler) (*testsupport.Browser, testsupport.Response) {
			back := f.IdentityStub.Authorize(testsupport.NewBrowser(h).Get("/sign-in/oidc").Location, annClaims())
			b := testsupport.NewBrowser(h)
			return b, b.Get(back)
		},
	}
	for name, answer := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			f := testsupport.NewFactory()
			h, _ := providerSite(t, f)

			b, got := answer(f, h)

			want := []any{400, refusedPage(staleSignIn, true), "/sign-in?next=%2F", testsupport.LogEntry{Level: "error", Message: providerSays,
				Fields: map[string]string{"error": "the answer does not belong to a sign-in this browser started", "client": testsupport.ClientAddress}}}
			if diff := cmp.Diff(want, []any{got.Status, pageOf(got), b.Get("/").Location, lastLog(f)}); diff != "" {
				t.Errorf("status, page, where home sends the browser, log (-want +got):\n%s", diff)
			}
		})
	}
}

func TestAPersonTheProviderDoesNotSignInIsToldSo(t *testing.T) {
	t.Parallel()
	f := testsupport.NewFactory()
	h, _ := providerSite(t, f)
	b := testsupport.NewBrowser(h)

	got := b.Get(f.IdentityStub.Deny(b.Get("/sign-in/oidc").Location))

	want := []any{403, refusedPage("The identity provider did not sign you in.", true), testsupport.LogEntry{Level: "error", Message: providerSays,
		Fields: map[string]string{"error": "the identity provider did not sign the person in: access_denied: The person cancelled.", "client": testsupport.ClientAddress}}}
	if diff := cmp.Diff(want, []any{got.Status, pageOf(got), lastLog(f)}); diff != "" {
		t.Errorf("status, page, log (-want +got):\n%s", diff)
	}
}

func TestStartingASignInAtTheProviderSendsTheBrowserThereWithAShortLivedCookie(t *testing.T) {
	t.Parallel()
	f := testsupport.NewFactory()
	h, _ := providerSite(t, f)
	b := testsupport.NewBrowser(h)

	got := b.Get("/sign-in/oidc?next=%2Fadmin%2Fdoors")
	pending := b.Cookie("doorcode_oidc")
	done := b.Get(f.IdentityStub.Authorize(got.Location, annClaims()))

	want := []any{303, "https://doorcode.example.com/sign-in/oidc/callback", "doorcode", "openid email profile", "S256",
		[]string{"doorcode_oidc=" + pending + "; Path=/; Max-Age=600; HttpOnly; SameSite=Lax"},
		"/admin/doors", "doorcode_oidc=; Path=/; Max-Age=0; HttpOnly; SameSite=Lax"}
	if diff := cmp.Diff(want, []any{got.Status, testsupport.QueryParam(got.Location, "redirect_uri"),
		testsupport.QueryParam(got.Location, "client_id"), testsupport.QueryParam(got.Location, "scope"),
		testsupport.QueryParam(got.Location, "code_challenge_method"), got.SetCookie, done.Location, done.SetCookie[0]}); diff != "" {
		t.Errorf("status, redirect address, client, scopes, challenge method, cookie, where it ends, cookie cleared (-want +got):\n%s", diff)
	}
}

func TestSigningInAtTheProviderIsRefusedUntilItIsSetUp(t *testing.T) {
	t.Parallel()
	incomplete := "Single sign-on is not set up completely yet. Ask an administrator to finish its settings."
	cases := map[string]struct {
		unset   string
		status  int
		message string
		logged  string
	}{
		"while it is disabled": {"DOORCODE_OIDC_ENABLED", 404, "Signing in with single sign-on is not enabled on this site.",
			"signing in through the identity provider is not enabled"},
		"without an issuer":        {"DOORCODE_OIDC_ISSUER", 503, incomplete, "signing in through the identity provider needs the oidc_issuer, oidc_client_id, and public_url settings"},
		"without a client id":      {"DOORCODE_OIDC_CLIENT_ID", 503, incomplete, "signing in through the identity provider needs the oidc_issuer, oidc_client_id, and public_url settings"},
		"without a public address": {"DOORCODE_PUBLIC_URL", 503, incomplete, "signing in through the identity provider needs the oidc_issuer, oidc_client_id, and public_url settings"},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			f := testsupport.NewFactory()
			h, _ := providerSite(t, f)
			delete(f.EnvStub, c.unset)

			got := testsupport.Get(h, "/sign-in/oidc")

			page := refusedPage(c.message, true)
			if c.unset == "DOORCODE_OIDC_ENABLED" {
				page.LinkTargets = nil
			}
			want := []any{c.status, page, testsupport.LogEntry{Level: "error", Message: providerSays, Fields: map[string]string{"error": c.logged, "client": testsupport.ClientAddress}}}
			if diff := cmp.Diff(want, []any{got.Status, pageOf(got), lastLog(f)}); diff != "" {
				t.Errorf("status, page, log (-want +got):\n%s", diff)
			}
		})
	}
}

func TestAnAnswerArrivingOnceSignInIsDisabledIsRefused(t *testing.T) {
	t.Parallel()
	f := testsupport.NewFactory()
	h, _ := providerSite(t, f)
	b := testsupport.NewBrowser(h)
	back := f.IdentityStub.Authorize(b.Get("/sign-in/oidc").Location, annClaims())
	f.EnvStub["DOORCODE_OIDC_ENABLED"] = "false"

	got := b.Get(back)

	if diff := cmp.Diff([]any{404, []string{"Signing in with single sign-on is not enabled on this site."}}, []any{got.Status, pageOf(got).Messages}); diff != "" {
		t.Errorf("status, messages (-want +got):\n%s", diff)
	}
}

func TestTheProviderIsDiscoveredOnFirstUseAndAgainOnlyOnceTheIssuerChanges(t *testing.T) {
	t.Parallel()
	f := testsupport.NewFactory()
	h, _ := providerSite(t, f)
	_, first := viaProvider(t, f, h, annClaims())
	f.IdentityStub.FailDiscovery()
	_, second := viaProvider(t, f, h, annClaims())
	f.EnvStub["DOORCODE_OIDC_ISSUER"] = "https://other.example.com"

	moved := testsupport.Get(h, "/sign-in/oidc")

	if diff := cmp.Diff([]any{303, 303, 502}, []any{first.Status, second.Status, moved.Status}); diff != "" {
		t.Errorf("first sign-in, second sign-in, after the issuer changed (-want +got):\n%s", diff)
	}
}

func TestAutoSignInSendsTheSignInPageOnToTheProvider(t *testing.T) {
	t.Parallel()
	f := testsupport.NewFactory()
	h, _ := providerSite(t, f)
	f.EnvStub["DOORCODE_OIDC_AUTO_SIGN_IN"] = "true"
	b := testsupport.NewBrowser(h)

	got := b.Get("/sign-in?next=%2Fadmin%2Fusers")
	done := b.Get(f.IdentityStub.Authorize(got.Location, annClaims()))

	want := []any{303, "https://doorcode.example.com/sign-in/oidc/callback", 303, "/admin/users"}
	if diff := cmp.Diff(want, []any{got.Status, testsupport.QueryParam(got.Location, "redirect_uri"), done.Status, done.Location}); diff != "" {
		t.Errorf("status, redirect address, sign-in status, where it ends (-want +got):\n%s", diff)
	}
}

func TestAutoSignInFalseShowsTheSignInPageWithThePasswordForm(t *testing.T) {
	t.Parallel()
	f := testsupport.NewFactory()
	h, _ := providerSite(t, f)
	f.EnvStub["DOORCODE_OIDC_AUTO_SIGN_IN"] = "true"

	got := testsupport.Get(h, "/sign-in?auto_sign_in=false")

	want := testsupport.Page{Title: "Sign in · doorcode", Heading: "Sign in", Values: map[string]string{"email": "", "next": "/"},
		LinkTargets: map[string]string{providerButton: "/sign-in/oidc?next=%2f"}}
	if diff := cmp.Diff([]any{200, want}, []any{got.Status, pageOf(got)}); diff != "" {
		t.Errorf("status, page (-want +got):\n%s", diff)
	}
}

func TestUnderAutoSignInAProviderThatCannotBeReachedLeavesOnlyItsButton(t *testing.T) {
	t.Parallel()
	f := testsupport.NewFactory()
	h, _ := providerSite(t, f)
	f.EnvStub["DOORCODE_OIDC_AUTO_SIGN_IN"] = "true"
	f.IdentityStub.FailDiscovery()

	got := testsupport.Get(h, "/sign-in")

	want := []any{502, refusedPage(unreachable, false), testsupport.LogEntry{Level: "error", Message: providerSays,
		Fields: map[string]string{"error": "the identity provider could not be reached: stub: unavailable", "client": testsupport.ClientAddress}}}
	if diff := cmp.Diff(want, []any{got.Status, pageOf(got), lastLog(f)}); diff != "" {
		t.Errorf("status, page, log (-want +got):\n%s", diff)
	}
}

func TestAPasswordSignInUnderAutoSignInKeepsThePasswordForm(t *testing.T) {
	t.Parallel()
	f := testsupport.NewFactory()
	h, _ := providerSite(t, f)
	f.EnvStub["DOORCODE_OIDC_AUTO_SIGN_IN"] = "true"

	got := testsupport.NewBrowser(h).Post("/sign-in", "email", adminEmail, "password", "wrong")

	want := testsupport.Page{Title: "Sign in · doorcode", Heading: "Sign in", Messages: []string{badCredentials},
		Values: map[string]string{"email": adminEmail, "next": "/"}, LinkTargets: map[string]string{providerButton: "/sign-in/oidc?next=%2f"}}
	if diff := cmp.Diff([]any{422, want}, []any{got.Status, pageOf(got)}); diff != "" {
		t.Errorf("status, page (-want +got):\n%s", diff)
	}
}

func TestASignInThatCannotBeHandledShowsTheErrorPage(t *testing.T) {
	t.Parallel()
	cases := map[string]struct {
		fail   func(f *testsupport.Factory)
		logged string
	}{
		"when settings cannot be read":   {func(f *testsupport.Factory) { f.StoreStub.FailSettings() }, "stub: unavailable"},
		"when users cannot be read":      {func(f *testsupport.Factory) { f.StoreStub.FailUsers() }, "stub: unavailable"},
		"when users cannot be written":   {func(f *testsupport.Factory) { f.StoreStub.FailUserWrites() }, "stub: unavailable"},
		"when groups cannot be changed":  {func(f *testsupport.Factory) { f.StoreStub.FailGroupWrites() }, "stub: unavailable"},
		"when sessions cannot be stored": {func(f *testsupport.Factory) { f.StoreStub.FailSessions() }, "stub: unavailable"},
		"when the client secret cannot be decrypted": {
			func(f *testsupport.Factory) {
				f.StoreStub.SeedSetting("oidc_client_secret", "AAAAAAAAAAAAAAAA.AAAA", testsupport.DefaultNow)
			},
			"the stored oidc_client_secret cannot be decrypted with the key in DOORCODE_SECRET_KEY; store it again",
		},
		"when the stored client secret has a nonce of another length": {
			func(f *testsupport.Factory) {
				f.StoreStub.SeedSetting("oidc_client_secret", "AAAA.AAAA", testsupport.DefaultNow)
			},
			"the stored oidc_client_secret cannot be decrypted with the key in DOORCODE_SECRET_KEY; store it again",
		},
		"when the stored client secret is not encrypted": {
			func(f *testsupport.Factory) {
				f.StoreStub.SeedSetting("oidc_client_secret", "plain secret", testsupport.DefaultNow)
			},
			"the stored oidc_client_secret cannot be decrypted with the key in DOORCODE_SECRET_KEY; store it again",
		},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			f := testsupport.NewFactory()
			h, _ := providerSite(t, f)
			b := testsupport.NewBrowser(h)
			back := f.IdentityStub.Authorize(b.Get("/sign-in/oidc").Location, annClaims())
			c.fail(f)

			got := b.Get(back)

			log := lastLog(f)
			if diff := cmp.Diff([]any{500, "Something went wrong", "request failed", c.logged},
				[]any{got.Status, pageOf(got).Title, log.Message, log.Fields["error"]}); diff != "" {
				t.Errorf("status, title, log (-want +got):\n%s", diff)
			}
		})
	}
}

func TestWithoutAnEntitlementsClaimNameANewIdentityThatCannotBeSavedSeesTheErrorPage(t *testing.T) {
	t.Parallel()
	cases := map[string]func(f *testsupport.Factory){
		"when users cannot be written":  func(f *testsupport.Factory) { f.StoreStub.FailUserWrites() },
		"when groups cannot be read":    func(f *testsupport.Factory) { f.StoreStub.FailGroups() },
		"when groups cannot be changed": func(f *testsupport.Factory) { f.StoreStub.FailGroupWrites() },
	}
	for name, fail := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			f := testsupport.NewFactory()
			h, _ := unentitledSite(t, f)
			b := testsupport.NewBrowser(h)
			back := f.IdentityStub.Authorize(b.Get("/sign-in/oidc").Location, annClaims("employee_id", nil))
			fail(f)

			got := b.Get(back)

			if diff := cmp.Diff([]any{500, "stub: unavailable"}, []any{got.Status, lastLog(f).Fields["error"]}); diff != "" {
				t.Errorf("status, log (-want +got):\n%s", diff)
			}
		})
	}
}

func TestPagesShowTheErrorPageWhenSettingsCannotBeRead(t *testing.T) {
	t.Parallel()
	requests := map[string]func(b *testsupport.Browser) testsupport.Response{
		"the home page":    func(b *testsupport.Browser) testsupport.Response { return b.Get("/") },
		"the sign-in page": func(b *testsupport.Browser) testsupport.Response { return b.Get("/sign-in") },
		"to refuse a password": func(b *testsupport.Browser) testsupport.Response {
			return b.Post("/sign-in", "email", "", "password", "")
		},
	}
	for name, request := range requests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			f := testsupport.NewFactory()
			b := signedInAdmin(t, f)
			f.StoreStub.FailSettings()

			got := request(b)

			if diff := cmp.Diff([]any{500, "request failed"}, []any{got.Status, lastLog(f).Message}); diff != "" {
				t.Errorf("status, log (-want +got):\n%s", diff)
			}
		})
	}
}

func TestSigningOutOfAProviderSessionEndsItAtTheProvider(t *testing.T) {
	t.Parallel()
	f := testsupport.NewFactory()
	h, _ := providerSite(t, f)
	f.IdentityStub.EndSession = "https://id.example.com/logout?ui=plain"
	b, _ := viaProvider(t, f, h, annClaims())

	got := b.Post("/sign-out")

	want := "https://id.example.com/logout?client_id=doorcode&post_logout_redirect_uri=https%3A%2F%2Fdoorcode.example.com%2Fsign-in&ui=plain"
	if diff := cmp.Diff([]any{303, want, "/sign-in?next=%2F"}, []any{got.Status, got.Location, b.Get("/").Location}); diff != "" {
		t.Errorf("status, location, where home sends the browser (-want +got):\n%s", diff)
	}
}

func TestSigningOutGoesToTheSignInPageWhenTheProviderCannotEndTheSession(t *testing.T) {
	t.Parallel()
	cases := map[string]struct {
		arrange func(f *testsupport.Factory)
		logged  []testsupport.LogEntry
	}{
		"when it names no end-session page": {func(*testsupport.Factory) {}, []testsupport.LogEntry{}},
		"when its end-session page is not an address": {
			func(f *testsupport.Factory) { f.IdentityStub.EndSession = "https://id.example.com/%zz" },
			[]testsupport.LogEntry{{Level: "error", Message: "could not end the session at the identity provider", Fields: map[string]string{
				"error": `the identity provider could not be reached: its end_session_endpoint: parse "https://id.example.com/%zz": invalid URL escape "%zz"`}}},
		},
		"when signing in at it has been disabled": {
			func(f *testsupport.Factory) { f.EnvStub["DOORCODE_OIDC_ENABLED"] = "false" },
			[]testsupport.LogEntry{{Level: "error", Message: "could not end the session at the identity provider", Fields: map[string]string{
				"error": "signing in through the identity provider is not enabled"}}},
		},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			f := testsupport.NewFactory()
			h, _ := providerSite(t, f)
			b, _ := viaProvider(t, f, h, annClaims())
			c.arrange(f)
			logged := len(f.LogStub.Entries)

			got := b.Post("/sign-out")

			if diff := cmp.Diff([]any{303, "/sign-in", c.logged}, []any{got.Status, got.Location, logsSince(f, logged)}); diff != "" {
				t.Errorf("status, location, log (-want +got):\n%s", diff)
			}
		})
	}
}

func TestSigningOutOfAPasswordSessionStaysOnTheSite(t *testing.T) {
	t.Parallel()
	f := testsupport.NewFactory()
	f.IdentityStub.EndSession = "https://id.example.com/logout"
	_, b := providerSite(t, f)

	got := b.Post("/sign-out")

	if diff := cmp.Diff([]any{303, "/sign-in"}, []any{got.Status, got.Location}); diff != "" {
		t.Errorf("status, location (-want +got):\n%s", diff)
	}
}

func TestTheClientSecretIsStoredEncryptedShownMaskedAndReachesTheProvider(t *testing.T) {
	t.Parallel()
	f := testsupport.NewFactory()
	h, admin := providerSite(t, f)
	f.IdentityStub.ClientSecret = "s3cret"

	saved := send(t, admin, "PUT", "/api/v1/settings/oidc_client_secret", `{"value":"s3cret"}`, 200)
	stored, err := testsupport.OpenSecret("oidc_client_secret", f.StoreStub.StoredSetting("oidc_client_secret"))
	read := send(t, admin, "GET", "/api/v1/settings/oidc_client_secret", "", 200)
	_, signedIn := viaProvider(t, f, h, annClaims())

	shown := `{"key":"oidc_client_secret","value":"********","source":"database","read_only":false,` +
		`"env_var":"DOORCODE_OIDC_CLIENT_SECRET","secret":true,"updated_at":"` + testsupport.DefaultNow + `"}` + "\n"
	want := []any{shown, "s3cret", "<nil>", shown, 303, "/me/code"}
	if diff := cmp.Diff(want, []any{saved.Body, stored, testsupport.ErrorText(err), read.Body, signedIn.Status, signedIn.Location}); diff != "" {
		t.Errorf("saved, decrypted, error, read, sign-in status and location (-want +got):\n%s", diff)
	}
}

func TestABlankSecretLeavesTheStoredOneAsItIs(t *testing.T) {
	t.Parallel()
	requests := map[string]func(b *testsupport.Browser) testsupport.Response{
		"through the API": func(b *testsupport.Browser) testsupport.Response {
			return b.PutJSON("/api/v1/settings/oidc_client_secret", `{"value":""}`)
		},
		"through the settings page": func(b *testsupport.Browser) testsupport.Response {
			return b.Post("/admin/settings", "oidc_client_secret", "", "site_name", "doorcode")
		},
	}
	for name, request := range requests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			f := testsupport.NewFactory()
			b := signedInAdmin(t, f)
			send(t, b, "PUT", "/api/v1/settings/oidc_client_secret", `{"value":"s3cret"}`, 200)
			before := f.StoreStub.StoredSetting("oidc_client_secret")

			got := request(b)

			if diff := cmp.Diff([]any{true, before}, []any{got.Status < 400, f.StoreStub.StoredSetting("oidc_client_secret")}); diff != "" {
				t.Errorf("accepted, stored (-want +got):\n%s", diff)
			}
		})
	}
}

func TestTheSettingsPageOffersSecretsAsBlankPasswordFields(t *testing.T) {
	t.Parallel()
	f := testsupport.NewFactory()
	b := signedInAdmin(t, f)
	b.Get("/")
	send(t, b, "PUT", "/api/v1/settings/oidc_client_secret", `{"value":"s3cret"}`, 200)

	got := b.Post("/admin/settings", "oidc_client_secret", "an0ther", "password_min_length", "12")
	stored, _ := testsupport.OpenSecret("oidc_client_secret", f.StoreStub.StoredSetting("oidc_client_secret"))
	page := pageOf(b.Get(got.Location))

	want := settingsPage("Sign-in", defaultSettings, map[string]shownSetting{"oidc_client_secret": {"oidc_client_secret", "********", "database"}}, nil, nil)
	want.Messages = []string{"The settings have been saved. Doors receive changed configuration defaults right away."}
	if diff := cmp.Diff([]any{"an0ther", want}, []any{stored, page}); diff != "" {
		t.Errorf("stored, page (-want +got):\n%s\n%v", diff, got)
	}
}

func TestSignInSettingsAcceptOnlyWhatTheyCanUse(t *testing.T) {
	t.Parallel()
	cases := map[string]struct {
		key, value, refusal string
	}{
		"enabled":                      {"oidc_enabled", "yes", "must be true or false"},
		"auto sign-in":                 {"oidc_auto_sign_in", "1", "must be true or false"},
		"an http issuer":               {"oidc_issuer", "http://id.example.com", "must be empty or the https:// address the provider names as its issuer, such as https://id.example.com"},
		"scopes":                       {"oidc_scopes", "email profile", "must be scopes separated by spaces, openid among them"},
		"the email claim":              {"oidc_claim_email", " ", "must name a claim"},
		"default groups":               {"oidc_default_groups", "-", "must be group slugs separated by commas, such as staff,contractors"},
		"a blank among default groups": {"oidc_default_groups", "staff,,Not A Slug", "must be group slugs separated by commas, such as staff,contractors"},
		"new user expiry":              {"new_user_expiry_days", "0", "must be a whole number of days from 1 to 36500"},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			got := signedInAdmin(t, testsupport.NewFactory()).PutJSON("/api/v1/settings/"+c.key, `{"value":"`+c.value+`"}`)

			want := apiError(400, "invalid value for "+c.key+": "+c.refusal)
			if diff := cmp.Diff(want, got); diff != "" {
				t.Errorf("response (-want +got):\n%s", diff)
			}
		})
	}
}

func TestSignInSettingsTakeWhatTheyCanUse(t *testing.T) {
	t.Parallel()
	cases := map[string]string{
		"oidc_enabled=true": "", "oidc_auto_sign_in=false": "", "oidc_issuer=https://id.example.com/realms/staff": "",
		"oidc_scopes=openid email": "", "oidc_default_groups=": "", "oidc_default_groups=staff, ,lab-1": "",
	}
	for setting := range cases {
		t.Run(setting, func(t *testing.T) {
			t.Parallel()
			key, value := splitSetting(setting)
			got := signedInAdmin(t, testsupport.NewFactory()).PutJSON("/api/v1/settings/"+key, `{"value":"`+value+`"}`)

			if diff := cmp.Diff(200, got.Status); diff != "" {
				t.Errorf("status (-want +got):\n%s\n%s", diff, got.Body)
			}
		})
	}
}

func TestASignInSettingTheEnvironmentSetsWronglyIsReported(t *testing.T) {
	t.Parallel()
	f := testsupport.NewFactory()
	h, _ := providerSite(t, f)
	f.EnvStub["DOORCODE_OIDC_SCOPES"] = "email"
	b := testsupport.NewBrowser(h)

	got := b.Get("/sign-in/oidc")

	if diff := cmp.Diff([]any{500, "invalid value for oidc_scopes: " + "must be scopes separated by spaces, openid among them"},
		[]any{got.Status, lastLog(f).Fields["error"]}); diff != "" {
		t.Errorf("status, log (-want +got):\n%s", diff)
	}
}

func TestAReturningUserWhoseGroupsCannotBeReadSeesTheErrorPage(t *testing.T) {
	t.Parallel()
	f := testsupport.NewFactory()
	h, _ := providerSite(t, f)
	viaProvider(t, f, h, annClaims())
	b := testsupport.NewBrowser(h)
	back := f.IdentityStub.Authorize(b.Get("/sign-in/oidc").Location, annClaims())
	f.StoreStub.FailGroups()

	got := b.Get(back)

	if diff := cmp.Diff([]any{500, "stub: unavailable"}, []any{got.Status, lastLog(f).Fields["error"]}); diff != "" {
		t.Errorf("status, log (-want +got):\n%s", diff)
	}
}

func TestUserinfoAboutAnotherSubjectIsRefused(t *testing.T) {
	t.Parallel()
	f := testsupport.NewFactory()
	h, _ := providerSite(t, f)
	b := testsupport.NewBrowser(h)
	back := f.IdentityStub.AuthorizeWithUserInfo(b.Get("/sign-in/oidc").Location, annClaims("name", nil),
		map[string]any{"sub": "someone-else", "name": "Eve"})

	got := b.Get(back)

	want := []any{403, refusedPage(unverified, true), testsupport.LogEntry{Level: "error", Message: providerSays,
		Fields: map[string]string{"error": "the identity provider's answer could not be verified: the userinfo subject is not the ID token's", "client": testsupport.ClientAddress}}}
	if diff := cmp.Diff(want, []any{got.Status, pageOf(got), lastLog(f)}); diff != "" {
		t.Errorf("status, page, log (-want +got):\n%s", diff)
	}
}

func TestAnEntitlementsClaimThatIsNeitherTextNorAListHoldsNone(t *testing.T) {
	t.Parallel()
	f := testsupport.NewFactory()
	h, admin := providerSite(t, f)

	viaProvider(t, f, h, annClaims())
	join(t, admin, "5", "2")

	viaProvider(t, f, h, annClaims("groups", float64(5)))

	if diff := cmp.Diff([]any(nil), groupSlugs(t, admin, "2")); diff != "" {
		t.Errorf("groups (-want +got):\n%s", diff)
	}
}

func TestASignInNeedsNoUserinfoWhenTheIDTokenHoldsEveryClaimRead(t *testing.T) {
	t.Parallel()
	f := testsupport.NewFactory()
	h, _ := unentitledSite(t, f)
	f.IdentityStub.FailUserInfo()

	_, got := viaProvider(t, f, h, annClaims())

	if diff := cmp.Diff([]any{303, "/me/code"}, []any{got.Status, got.Location}); diff != "" {
		t.Errorf("status, location (-want +got):\n%s", diff)
	}
}

func TestClearingTheClientSecretSignsInWithoutOne(t *testing.T) {
	t.Parallel()
	f := testsupport.NewFactory()
	h, admin := providerSite(t, f)
	send(t, admin, "PUT", "/api/v1/settings/oidc_client_secret", `{"value":"s3cret"}`, 200)

	cleared := send(t, admin, "PUT", "/api/v1/settings/oidc_client_secret", `{"clear":true}`, 200)
	_, signedIn := viaProvider(t, f, h, annClaims())

	shown := `{"key":"oidc_client_secret","value":"","source":"database","read_only":false,` +
		`"env_var":"DOORCODE_OIDC_CLIENT_SECRET","secret":true,"updated_at":"` + testsupport.DefaultNow + `"}` + "\n"
	want := []any{shown, "", 303, "/me/code"}
	if diff := cmp.Diff(want, []any{cleared.Body, f.StoreStub.StoredSetting("oidc_client_secret"), signedIn.Status, signedIn.Location}); diff != "" {
		t.Errorf("cleared, stored, sign-in status and location (-want +got):\n%s", diff)
	}
}

func TestTickingClearOnTheSettingsPageEmptiesASecret(t *testing.T) {
	t.Parallel()
	cases := map[string][]string{
		"left blank":       {"clear", "enrollment_password", "enrollment_password", "", "enrollment_username", "doorcode-enroll"},
		"with a new value": {"clear", "enrollment_password", "enrollment_password", "n3w", "enrollment_username", "doorcode-enroll"},
	}
	for name, form := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			f := testsupport.NewFactory()
			b := signedInAdmin(t, f)
			b.Get("/")
			send(t, b, "PUT", "/api/v1/settings/enrollment_password", `{"value":"s3cret"}`, 200)

			saved := b.Post("/admin/settings", form...)
			page := pageOf(b.Get(saved.Location))

			wantPage := settingsPage("Doors", defaultSettings, map[string]shownSetting{"enrollment_password": {"enrollment_password", "", "database"}}, nil, nil)
			wantPage.Messages = []string{"The settings have been saved. Doors receive changed configuration defaults right away."}
			if diff := cmp.Diff([]any{303, "", wantPage}, []any{saved.Status, f.StoreStub.StoredSetting("enrollment_password"), page}); diff != "" {
				t.Errorf("status, stored, page (-want +got):\n%s", diff)
			}
		})
	}
}

func TestClearingASecretThatHasNoValueLeavesItAtItsDefault(t *testing.T) {
	t.Parallel()
	b := signedInAdmin(t, testsupport.NewFactory())

	saved := b.Post("/admin/settings", "clear", "oidc_client_secret", "clear", "site_name", "site_name", "doorcode")

	got := []any{saved.Status, testsupport.JSONAt(b.Get("/api/v1/settings/oidc_client_secret").Body, "/source"),
		testsupport.JSONAt(b.Get("/api/v1/settings/site_name").Body, "/value")}
	if diff := cmp.Diff([]any{303, "default", "doorcode"}, got); diff != "" {
		t.Errorf("status, secret's source, site name (-want +got):\n%s", diff)
	}
}
