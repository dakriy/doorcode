package entrypoint_test

import (
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/dakriy/doorcode/internal/testsupport"
)

func TestHomeAsksForSignIn(t *testing.T) {
	t.Parallel()
	got := testsupport.Get(start(t, testsupport.NewFactory()).Handler(), "/")

	want := testsupport.Response{Status: 303, Location: "/sign-in?next=%2F"}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("response (-want +got):\n%s", diff)
	}
}

func TestAdministratorReachesTheSettingsPage(t *testing.T) {
	t.Parallel()
	f := testsupport.NewFactory()
	f.StoreStub.SeedSetting("site_name", "North Campus", "2026-01-02T08:00:00Z")
	b := signedInAdmin(t, f)
	b.Get("/")

	got := b.Get("/admin/settings")

	want := testsupport.Page{
		Title:   "Settings · North Campus",
		Heading: "Settings",
		Nav:     []string{"Home", "Users", "Groups", "Schedules", "Doors", "Zones", "Settings (current)", "API tokens", "API docs"},
		Table: [][]string{
			{"Setting", "Value", "Source", "Environment variable"},
			{"site_name", "North Campus", "database", "DOORCODE_SITE_NAME"},
			{"site_timezone", "UTC", "default", "DOORCODE_SITE_TIMEZONE"},
			{"admin_email", "admin@example.com", "default", "DOORCODE_ADMIN_EMAIL"},
			{"password_hash_memory_kib", "8", "environment, read-only", "DOORCODE_PASSWORD_HASH_MEMORY_KIB"},
		},
	}
	if diff := cmp.Diff(want, pageOf(got)); diff != "" {
		t.Errorf("page (-want +got):\n%s", diff)
	}
	if diff := cmp.Diff(200, got.Status); diff != "" {
		t.Errorf("status (-want +got):\n%s", diff)
	}
}

func TestUserWithoutTheAdminRoleIsRefusedTheSettingsPage(t *testing.T) {
	t.Parallel()
	f := testsupport.NewFactory()
	b := signedInAdmin(t, f)
	b.Get("/")
	f.StoreStub.RevokeRoles(adminEmail)

	got := b.Get("/admin/settings")

	want := testsupport.Page{
		Title:   "Not allowed · doorcode",
		Heading: "You do not have access to this page",
		Nav:     []string{"Home", "API tokens", "API docs"},
	}
	if diff := cmp.Diff(want, pageOf(got)); diff != "" {
		t.Errorf("page (-want +got):\n%s", diff)
	}
	if diff := cmp.Diff(403, got.Status); diff != "" {
		t.Errorf("status (-want +got):\n%s", diff)
	}
}

func TestHomeOffersOnlyWhatTheUserMayUse(t *testing.T) {
	t.Parallel()
	f := testsupport.NewFactory()
	b := signedInAdmin(t, f)
	f.StoreStub.RevokeRoles(adminEmail)

	got := pageOf(b.Get("/"))

	if diff := cmp.Diff([]string{"Home (current)", "API tokens", "API docs"}, got.Nav); diff != "" {
		t.Errorf("nav (-want +got):\n%s", diff)
	}
}

func TestPagesLoadTheirAssets(t *testing.T) {
	t.Parallel()
	h := start(t, testsupport.NewFactory()).Handler()
	assets := testsupport.ParsePage(testsupport.Get(h, "/sign-in").Body).Assets

	var got []string
	for _, asset := range assets {
		r := testsupport.Get(h, asset)
		got = append(got, r.ContentType)
	}

	want := []string{"text/css; charset=utf-8", "text/javascript; charset=utf-8"}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("asset types (-want +got):\n%s", diff)
	}
}

func TestPageStoreFailureShowsARequestIDThatTheLogCarries(t *testing.T) {
	t.Parallel()
	cases := map[string]struct {
		request func(t *testing.T, f *testsupport.Factory) testsupport.Response
		id      string
	}{
		"sign-in page without settings": {id: "2223242526272829", request: func(t *testing.T, f *testsupport.Factory) testsupport.Response {
			h := start(t, f).Handler()
			f.StoreStub.FailSettings()
			return testsupport.Get(h, "/sign-in")
		}},
		"sign-in without users": {id: "2223242526272829", request: func(t *testing.T, f *testsupport.Factory) testsupport.Response {
			h := start(t, f).Handler()
			f.StoreStub.FailUsers()
			return testsupport.NewBrowser(h).Post("/sign-in", "email", adminEmail, "password", adminPassword(t, f))
		}},
		"sign-in of an unknown email without settings": {id: "2223242526272829", request: func(t *testing.T, f *testsupport.Factory) testsupport.Response {
			h := start(t, f).Handler()
			f.StoreStub.FailSettings()
			return testsupport.NewBrowser(h).Post("/sign-in", "email", "nobody@example.com", "password", "x")
		}},
		"sign-in without sessions": {id: "4243444546474849", request: func(t *testing.T, f *testsupport.Factory) testsupport.Response {
			h := start(t, f).Handler()
			f.StoreStub.FailSessions()
			return testsupport.NewBrowser(h).Post("/sign-in", "email", adminEmail, "password", adminPassword(t, f))
		}},
		"home without sessions": {id: "4243444546474849", request: func(t *testing.T, f *testsupport.Factory) testsupport.Response {
			b := signedInAdmin(t, f)
			f.StoreStub.FailSessions()
			return b.Get("/")
		}},
		"settings page without settings": {id: "4243444546474849", request: func(t *testing.T, f *testsupport.Factory) testsupport.Response {
			b := signedInAdmin(t, f)
			f.StoreStub.FailSettings()
			return b.Get("/admin/settings")
		}},
		"tokens page without tokens": {id: "4243444546474849", request: func(t *testing.T, f *testsupport.Factory) testsupport.Response {
			b := signedInAdmin(t, f)
			f.StoreStub.FailTokens()
			return b.Get("/account/tokens")
		}},
		"token creation without tokens, after drawing the secret": {id: "6263646566676869", request: func(t *testing.T, f *testsupport.Factory) testsupport.Response {
			b := signedInAdmin(t, f)
			f.StoreStub.FailTokens()
			return b.Post("/account/tokens", "name", "ci", "expires_in_days", "30")
		}},
		"rejected token form without tokens": {id: "4243444546474849", request: func(t *testing.T, f *testsupport.Factory) testsupport.Response {
			b := signedInAdmin(t, f)
			f.StoreStub.FailTokens()
			return b.Post("/account/tokens", "name", "", "expires_in_days", "30")
		}},
		"token revocation without tokens": {id: "4243444546474849", request: func(t *testing.T, f *testsupport.Factory) testsupport.Response {
			b := signedInAdmin(t, f)
			f.StoreStub.FailTokens()
			return b.Post("/account/tokens/1/revoke")
		}},
		"sign-out without sessions": {id: "4243444546474849", request: func(t *testing.T, f *testsupport.Factory) testsupport.Response {
			b := signedInAdmin(t, f)
			f.StoreStub.FailSessions()
			return b.Post("/sign-out")
		}},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			f := testsupport.NewFactory()

			got := c.request(t, f)

			want := testsupport.Page{Title: "Something went wrong", Heading: "Something went wrong", Messages: []string{c.id}}
			if diff := cmp.Diff(want, pageOf(got)); diff != "" {
				t.Errorf("page (-want +got):\n%s", diff)
			}
			if diff := cmp.Diff([]any{500, htmlType}, []any{got.Status, got.ContentType}); diff != "" {
				t.Errorf("status and type (-want +got):\n%s", diff)
			}
			wantLog := testsupport.LogEntry{Level: "error", Message: "request failed",
				Fields: map[string]string{"request_id": c.id, "error": "stub: unavailable"}}
			if diff := cmp.Diff(wantLog, f.LogStub.Entries[len(f.LogStub.Entries)-1]); diff != "" {
				t.Errorf("last log line (-want +got):\n%s", diff)
			}
		})
	}
}
