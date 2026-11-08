package entrypoint_test

import (
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/dakriy/doorcode/internal/testsupport"
)

// tokenNav is the navigation of the API tokens page for an administrator.
var tokenNav = []string{"Home", "Users", "Groups", "Settings", "API tokens (current)", "API docs"}

// tokenTableHead is the header row of the token table.
var tokenTableHead = []string{"Name", "Created", "Expires", "Actions"}

func TestTokensPageInvitesCreatingTheFirstToken(t *testing.T) {
	b := signedInAdmin(t, testsupport.NewFactory())
	b.Get("/")

	got := b.Get("/account/tokens")

	want := testsupport.Page{
		Title:   "API tokens · doorcode",
		Heading: "API tokens",
		Nav:     tokenNav,
		Values:  map[string]string{"name": ""},
		Table: [][]string{
			tokenTableHead,
			{"You have no API tokens yet. Create one to call the API from a script."},
		},
	}
	if diff := cmp.Diff(want, pageOf(got)); diff != "" {
		t.Errorf("page (-want +got):\n%s", diff)
	}
	if diff := cmp.Diff(200, got.Status); diff != "" {
		t.Errorf("status (-want +got):\n%s", diff)
	}
}

func TestTokenCreatedOnThePageShowsItsSecretOnce(t *testing.T) {
	b := signedInAdmin(t, testsupport.NewFactory())
	b.Get("/")

	created := b.Post("/account/tokens", "name", " deploy ", "expires_in_days", "30")
	again := b.Get("/account/tokens")

	want := testsupport.Page{
		Title:    "API tokens · doorcode",
		Heading:  "API tokens",
		Nav:      tokenNav,
		Messages: []string{"Token “deploy” created. Copy it now: it is shown only this once and cannot be recovered."},
		Values:   map[string]string{"secret": firstSecret, "name": ""},
		Table:    [][]string{tokenTableHead, {"deploy", "5 Jan 2026", "4 Feb 2026", "Revoke"}},
	}
	if diff := cmp.Diff(want, pageOf(created)); diff != "" {
		t.Errorf("page after creating (-want +got):\n%s", diff)
	}
	if diff := cmp.Diff(201, created.Status); diff != "" {
		t.Errorf("status (-want +got):\n%s", diff)
	}
	if diff := cmp.Diff(map[string]string{"name": ""}, pageOf(again).Values); diff != "" {
		t.Errorf("inputs on the next visit (-want +got):\n%s", diff)
	}
}

func TestTokenCreatedOnThePageCallsTheAPI(t *testing.T) {
	f := testsupport.NewFactory()
	h, b := adminSession(t, f)
	created := b.Post("/account/tokens", "name", "deploy", "expires_in_days", "")
	secret := pageOf(created).Values["secret"]

	got := tokenClient(h, secret).Get("/api/v1/tokens")

	want := testsupport.JSON(`[{"id":1,"name":"deploy","created_at":"2026-01-05T09:00:00Z","expired":false}]`)
	if diff := cmp.Diff(want, testsupport.JSONAt(got.Body, "/tokens")); diff != "" {
		t.Errorf("tokens (-want +got):\n%s", diff)
	}
}

func TestTokensPageShowsWhenEachTokenExpires(t *testing.T) {
	f := testsupport.NewFactory()
	h, b := adminSession(t, f)
	b.Post("/account/tokens", "name", "short", "expires_in_days", "7")
	b.Post("/account/tokens", "name", "forever", "expires_in_days", "")
	b.Post("/account/tokens", "name", "yearly", "expires_in_days", "365")
	f.ClockStub.Set("2026-01-13T08:00:00Z")

	got := signIn(t, f, h).Get("/account/tokens")

	want := [][]string{
		tokenTableHead,
		{"short", "5 Jan 2026", "Expired 12 Jan 2026", "Revoke"},
		{"forever", "5 Jan 2026", "Never", "Revoke"},
		{"yearly", "5 Jan 2026", "5 Jan 2027", "Revoke"},
	}
	if diff := cmp.Diff(want, pageOf(got).Table); diff != "" {
		t.Errorf("table (-want +got):\n%s", diff)
	}
}

func TestTokenFormSaysWhatIsWrong(t *testing.T) {
	cases := map[string]struct {
		fields []string
		want   map[string]string
	}{
		"blank name": {fields: []string{"name", "  ", "expires_in_days", "30"},
			want: map[string]string{"name": "Enter a name for the token."}},
		"name too long": {fields: []string{"name", repeat("x", 101), "expires_in_days", "30"},
			want: map[string]string{"name": "Use at most 100 characters for the name."}},
		"expiry not a number": {fields: []string{"name", "ci", "expires_in_days", "soon"},
			want: map[string]string{"expires_in_days": "Choose when the token expires."}},
		"expiry out of range": {fields: []string{"name", "ci", "expires_in_days", "0"},
			want: map[string]string{"expires_in_days": "Choose an expiry from 1 to 3650 days."}},
		"both": {fields: []string{"name", "", "expires_in_days", "soon"},
			want: map[string]string{"name": "Enter a name for the token.", "expires_in_days": "Choose when the token expires."}},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			got := signedInAdmin(t, testsupport.NewFactory()).Post("/account/tokens", c.fields...)

			if diff := cmp.Diff(c.want, pageOf(got).FieldErrors); diff != "" {
				t.Errorf("field errors (-want +got):\n%s", diff)
			}
			if diff := cmp.Diff(422, got.Status); diff != "" {
				t.Errorf("status (-want +got):\n%s", diff)
			}
		})
	}
}

func TestRejectedTokenFormKeepsWhatWasEntered(t *testing.T) {
	got := signedInAdmin(t, testsupport.NewFactory()).Post("/account/tokens", "name", " ci ", "expires_in_days", "0")

	if diff := cmp.Diff(map[string]string{"name": "ci"}, pageOf(got).Values); diff != "" {
		t.Errorf("inputs (-want +got):\n%s", diff)
	}
}

func TestRevokingATokenFromThePageConfirmsAndEmptiesTheList(t *testing.T) {
	b := signedInAdmin(t, testsupport.NewFactory())
	b.Post("/account/tokens", "name", "deploy", "expires_in_days", "30")

	revoked := b.Post("/account/tokens/1/revoke")
	got := pageOf(b.Get("/account/tokens"))

	if diff := cmp.Diff([]any{303, "/account/tokens"}, []any{revoked.Status, revoked.Location}); diff != "" {
		t.Errorf("redirect (-want +got):\n%s", diff)
	}
	want := []any{
		[]string{"The token has been revoked. Anything still using it is refused from now on."},
		[][]string{tokenTableHead, {"You have no API tokens yet. Create one to call the API from a script."}},
	}
	if diff := cmp.Diff(want, []any{got.Messages, got.Table}); diff != "" {
		t.Errorf("messages and table (-want +got):\n%s", diff)
	}
}

func TestRevokingAnUnknownTokenFromThePageSaysSo(t *testing.T) {
	for _, id := range []string{"7", "first"} {
		t.Run(id, func(t *testing.T) {
			b := signedInAdmin(t, testsupport.NewFactory())
			b.Post("/account/tokens/" + id + "/revoke")

			got := pageOf(b.Get("/account/tokens"))

			if diff := cmp.Diff([]string{"That token had already been revoked."}, got.Messages); diff != "" {
				t.Errorf("messages (-want +got):\n%s", diff)
			}
		})
	}
}

func TestTokensPageAsksForSignIn(t *testing.T) {
	got := testsupport.Get(start(t, testsupport.NewFactory()).Handler(), "/account/tokens")

	want := testsupport.Response{Status: 303, Location: "/sign-in?next=%2Faccount%2Ftokens"}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("response (-want +got):\n%s", diff)
	}
}

func TestAPIDocsPageShowsTheDocumentationToASignedInUser(t *testing.T) {
	f := testsupport.NewFactory()
	b := signedInAdmin(t, f)
	f.StoreStub.RevokeRoles(adminEmail)
	b.Get("/")

	got := b.Get("/api-docs")

	want := testsupport.Page{
		Title:   "API documentation · doorcode",
		Heading: "API documentation",
		Nav:     []string{"Home", "API tokens", "API docs (current)"},
	}
	if diff := cmp.Diff(want, pageOf(got)); diff != "" {
		t.Errorf("page (-want +got):\n%s", diff)
	}
}

func TestAPIDocsPageLoadsSwaggerUI(t *testing.T) {
	f := testsupport.NewFactory()
	b := signedInAdmin(t, f)
	assets := testsupport.ParsePage(b.Get("/api-docs").Body).Assets

	var got []string
	for _, asset := range assets {
		r := b.Get(asset)
		got = append(got, r.ContentType)
	}

	css, js := "text/css; charset=utf-8", "text/javascript; charset=utf-8"
	if diff := cmp.Diff([]string{css, js, css, js, js}, got); diff != "" {
		t.Errorf("asset types (-want +got):\n%s", diff)
	}
}

func TestAPIDocsPageAsksForSignIn(t *testing.T) {
	got := testsupport.Get(start(t, testsupport.NewFactory()).Handler(), "/api-docs")

	want := testsupport.Response{Status: 303, Location: "/sign-in?next=%2Fapi-docs"}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("response (-want +got):\n%s", diff)
	}
}
