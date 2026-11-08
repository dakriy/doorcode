package entrypoint_test

import (
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/dakriy/doorcode/internal/testsupport"
)

const (
	htmlType = "text/html; charset=utf-8"
	// sessionToken is the first session token the stub entropy yields after
	// the first administrator is created.
	sessionToken   = "IiMkJSYnKCkqKywtLi8wMTIzNDU2Nzg5Ojs8PT4_QEE"
	sessionCookie  = "doorcode_session=" + sessionToken + "; Path=/; Max-Age=43200; HttpOnly; SameSite=Lax"
	badCredentials = "That email and password do not match an account."
)

// pageOf reads the page a response carries, leaving out the assets every
// page loads, which TestPagesLoadTheirAssets covers.
func pageOf(r testsupport.Response) testsupport.Page {
	p := testsupport.ParsePage(r.Body)
	p.Assets = nil
	return p
}

func TestSignInPageShowsAnEmptyForm(t *testing.T) {
	got := testsupport.Get(start(t, testsupport.NewFactory()).Handler(), "/sign-in")

	want := testsupport.Page{Title: "Sign in · doorcode", Heading: "Sign in", Values: map[string]string{"email": "", "next": "/"}}
	if diff := cmp.Diff(want, pageOf(got)); diff != "" {
		t.Errorf("page (-want +got):\n%s", diff)
	}
	if diff := cmp.Diff([]any{200, htmlType}, []any{got.Status, got.ContentType}); diff != "" {
		t.Errorf("status and type (-want +got):\n%s", diff)
	}
}

func TestSignInPageNamesTheSite(t *testing.T) {
	f := testsupport.NewFactory()
	f.EnvStub["DOORCODE_SITE_NAME"] = "North Campus"

	got := testsupport.Get(start(t, f).Handler(), "/sign-in")

	if diff := cmp.Diff("Sign in · North Campus", pageOf(got).Title); diff != "" {
		t.Errorf("title (-want +got):\n%s", diff)
	}
}

func TestSigningInSetsASessionCookieAndGoesHome(t *testing.T) {
	f := testsupport.NewFactory()
	b := startBrowser(t, f)

	got := b.Post("/sign-in", "email", adminEmail, "password", adminPassword(t, f))

	want := testsupport.Response{Status: 303, Location: "/", SetCookie: []string{
		sessionCookie,
		"doorcode_flash=signed-in; Path=/; HttpOnly; SameSite=Lax",
	}}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("response (-want +got):\n%s", diff)
	}
}

func TestHomeGreetsTheSignedInAdministratorAndShowsTheFlashOnce(t *testing.T) {
	b := signedInAdmin(t, testsupport.NewFactory())

	firstResponse := b.Get("/")
	first := pageOf(firstResponse)
	second := pageOf(b.Get("/"))

	want := testsupport.Page{
		Title:    "Home · doorcode",
		Heading:  "Welcome, Administrator",
		Nav:      []string{"Home (current)", "Settings", "API tokens", "API docs"},
		Messages: []string{"You are signed in."},
	}
	if diff := cmp.Diff(want, first); diff != "" {
		t.Errorf("first visit (-want +got):\n%s", diff)
	}
	if diff := cmp.Diff([]string{"doorcode_flash=; Path=/; Max-Age=0; HttpOnly; SameSite=Lax"}, firstResponse.SetCookie); diff != "" {
		t.Errorf("first visit cookies (-want +got):\n%s", diff)
	}
	want.Messages = nil
	if diff := cmp.Diff(want, second); diff != "" {
		t.Errorf("second visit (-want +got):\n%s", diff)
	}
}

func TestSignInEmailIgnoresCaseAndSurroundingSpace(t *testing.T) {
	f := testsupport.NewFactory()
	b := startBrowser(t, f)

	got := b.Post("/sign-in", "email", "  ADMIN@Example.com ", "password", adminPassword(t, f))

	if diff := cmp.Diff(303, got.Status); diff != "" {
		t.Errorf("status (-want +got):\n%s", diff)
	}
}

func TestSignInReturnsToThePageThatAskedForIt(t *testing.T) {
	f := testsupport.NewFactory()
	b := startBrowser(t, f)

	asked := b.Get("/admin/settings?view=all")
	form := pageOf(b.Get(asked.Location))
	got := b.Post("/sign-in", "email", adminEmail, "password", adminPassword(t, f), "next", form.Values["next"])

	if diff := cmp.Diff([]string{"/sign-in?next=%2Fadmin%2Fsettings%3Fview%3Dall", "/admin/settings?view=all"},
		[]string{asked.Location, got.Location}); diff != "" {
		t.Errorf("redirects (-want +got):\n%s", diff)
	}
}

func TestSignInGoesHomeInsteadOfLeavingTheSite(t *testing.T) {
	for _, next := range []string{"https://evil.example.com/", "//evil.example.com/", `/\evil.example.com/`, "settings"} {
		t.Run(next, func(t *testing.T) {
			f := testsupport.NewFactory()
			b := startBrowser(t, f)

			got := b.Post("/sign-in", "email", adminEmail, "password", adminPassword(t, f), "next", next)

			if diff := cmp.Diff("/", got.Location); diff != "" {
				t.Errorf("location (-want +got):\n%s", diff)
			}
		})
	}
}

func TestSignInPageKeepsOnlyALocalNext(t *testing.T) {
	h := start(t, testsupport.NewFactory()).Handler()

	got := pageOf(testsupport.Get(h, "/sign-in?next=%2F%2Fevil.example.com")).Values["next"]

	if diff := cmp.Diff("/", got); diff != "" {
		t.Errorf("next (-want +got):\n%s", diff)
	}
}

func TestSignInAsksForMissingFields(t *testing.T) {
	const noEmail, noPassword = "Enter your email address.", "Enter your password."
	cases := map[string]struct {
		email, password string
		want            testsupport.Page
	}{
		"both missing": {email: " ", password: "", want: testsupport.Page{
			FieldErrors: map[string]string{"email": noEmail, "password": noPassword},
			Values:      map[string]string{"email": "", "next": "/"},
		}},
		"email missing": {email: "", password: "secret", want: testsupport.Page{
			FieldErrors: map[string]string{"email": noEmail},
			Values:      map[string]string{"email": "", "next": "/"},
		}},
		"password missing": {email: adminEmail, password: "", want: testsupport.Page{
			FieldErrors: map[string]string{"password": noPassword},
			Values:      map[string]string{"email": adminEmail, "next": "/"},
		}},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			b := startBrowser(t, testsupport.NewFactory())

			got := b.Post("/sign-in", "email", c.email, "password", c.password)

			if diff := cmp.Diff(422, got.Status); diff != "" {
				t.Errorf("status (-want +got):\n%s", diff)
			}
			want := c.want
			want.Title, want.Heading = "Sign in · doorcode", "Sign in"
			if diff := cmp.Diff(want, pageOf(got)); diff != "" {
				t.Errorf("page (-want +got):\n%s", diff)
			}
		})
	}
}

func TestSignInRefusesCredentialsThatDoNotMatchAnAccount(t *testing.T) {
	cases := map[string]struct {
		arrange  func(f *testsupport.Factory)
		email    string
		password func(t *testing.T, f *testsupport.Factory) string
	}{
		"wrong password": {
			arrange: func(*testsupport.Factory) {}, email: adminEmail,
			password: func(*testing.T, *testsupport.Factory) string { return "wrong" },
		},
		"unknown email": {
			arrange: func(*testsupport.Factory) {}, email: "nobody@example.com",
			password: adminPassword,
		},
		"deleted account": {
			arrange:  func(f *testsupport.Factory) { f.StoreStub.DeleteUser(adminEmail, "2026-01-01T00:00:00Z") },
			email:    adminEmail,
			password: adminPassword,
		},
		"account whose password hash is unreadable": {
			arrange:  func(f *testsupport.Factory) { f.StoreStub.SetPasswordHash(adminEmail, "not-a-hash") },
			email:    adminEmail,
			password: adminPassword,
		},
		"account without a local password": {
			arrange:  func(f *testsupport.Factory) { f.StoreStub.RemovePassword(adminEmail) },
			email:    adminEmail,
			password: adminPassword,
		},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			f := testsupport.NewFactory()
			b := startBrowser(t, f)
			c.arrange(f)

			got := b.Post("/sign-in", "email", c.email, "password", c.password(t, f))

			want := testsupport.Page{
				Title: "Sign in · doorcode", Heading: "Sign in",
				Messages: []string{badCredentials},
				Values:   map[string]string{"email": c.email, "next": "/"},
			}
			if diff := cmp.Diff(want, pageOf(got)); diff != "" {
				t.Errorf("page (-want +got):\n%s", diff)
			}
			if diff := cmp.Diff(422, got.Status); diff != "" {
				t.Errorf("status (-want +got):\n%s", diff)
			}
		})
	}
}

func TestExpiredAccountCannotSignIn(t *testing.T) {
	const expiredMessage = "This account has expired. Ask an administrator to renew it."
	cases := map[string]struct {
		expires string
		status  int
		message []string
	}{
		"expired yesterday":      {expires: "2026-01-04T09:00:00Z", status: 422, message: []string{expiredMessage}},
		"expiring this moment":   {expires: testsupport.DefaultNow, status: 422, message: []string{expiredMessage}},
		"expiring in one second": {expires: "2026-01-05T09:00:01Z", status: 303},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			f := testsupport.NewFactory()
			b := startBrowser(t, f)
			f.StoreStub.ExpireUser(adminEmail, c.expires)

			got := b.Post("/sign-in", "email", adminEmail, "password", adminPassword(t, f))

			if diff := cmp.Diff(c.status, got.Status); diff != "" {
				t.Errorf("status (-want +got):\n%s", diff)
			}
			if diff := cmp.Diff(c.message, pageOf(got).Messages); diff != "" {
				t.Errorf("messages (-want +got):\n%s", diff)
			}
		})
	}
}

func TestSessionLastsTwelveHours(t *testing.T) {
	cases := map[string]struct {
		at     string
		status int
	}{
		"one second before": {at: "2026-01-05T20:59:59Z", status: 200},
		"at twelve hours":   {at: "2026-01-05T21:00:00Z", status: 303},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			f := testsupport.NewFactory()
			b := signedInAdmin(t, f)
			f.ClockStub.Set(c.at)

			got := b.Get("/")

			if diff := cmp.Diff(c.status, got.Status); diff != "" {
				t.Errorf("status (-want +got):\n%s", diff)
			}
		})
	}
}

func TestSessionEndsWhenTheAccountExpiresOrIsDeleted(t *testing.T) {
	cases := map[string]func(f *testsupport.Factory){
		"expired": func(f *testsupport.Factory) {
			f.StoreStub.ExpireUser(adminEmail, "2026-01-05T10:00:00Z")
			f.ClockStub.Set("2026-01-05T10:00:00Z")
		},
		"deleted": func(f *testsupport.Factory) {
			f.StoreStub.DeleteUser(adminEmail, "2026-01-05T10:00:00Z")
		},
	}
	for name, arrange := range cases {
		t.Run(name, func(t *testing.T) {
			f := testsupport.NewFactory()
			b := signedInAdmin(t, f)
			arrange(f)

			got := b.Get("/admin/settings")

			want := testsupport.Response{Status: 303, Location: "/sign-in?next=%2Fadmin%2Fsettings"}
			if diff := cmp.Diff(want, got); diff != "" {
				t.Errorf("response (-want +got):\n%s", diff)
			}
		})
	}
}

func TestUnknownSessionTokenIsTreatedAsSignedOut(t *testing.T) {
	b := startBrowser(t, testsupport.NewFactory())
	b.SetCookie("doorcode_session", "forged")

	got := b.Get("/")

	want := testsupport.Response{Status: 303, Location: "/sign-in?next=%2F"}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("response (-want +got):\n%s", diff)
	}
}

func TestSigningOutClearsTheCookieAndSaysSo(t *testing.T) {
	b := signedInAdmin(t, testsupport.NewFactory())

	got := b.Post("/sign-out")
	page := pageOf(b.Get(got.Location))

	want := testsupport.Response{Status: 303, Location: "/sign-in", SetCookie: []string{
		"doorcode_session=; Path=/; Max-Age=0; HttpOnly; SameSite=Lax",
		"doorcode_flash=signed-out; Path=/; HttpOnly; SameSite=Lax",
	}}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("response (-want +got):\n%s", diff)
	}
	if diff := cmp.Diff([]string{"You have been signed out."}, page.Messages); diff != "" {
		t.Errorf("sign-in page messages (-want +got):\n%s", diff)
	}
}

func TestSignedOutTokenNoLongerWorks(t *testing.T) {
	f := testsupport.NewFactory()
	h := start(t, f).Handler()
	b := signIn(t, f, h)
	token := b.Cookie("doorcode_session")
	b.Post("/sign-out")
	replay := testsupport.NewBrowser(h)
	replay.SetCookie("doorcode_session", token)

	got := replay.Get("/")

	if diff := cmp.Diff(303, got.Status); diff != "" {
		t.Errorf("status (-want +got):\n%s", diff)
	}
}

func TestSigningOutWithoutASessionGoesToSignIn(t *testing.T) {
	got := startBrowser(t, testsupport.NewFactory()).Post("/sign-out")

	if diff := cmp.Diff([]any{303, "/sign-in"}, []any{got.Status, got.Location}); diff != "" {
		t.Errorf("status and location (-want +got):\n%s", diff)
	}
}

func TestSessionCookieIsSecureWhenTheBrowserUsesHTTPS(t *testing.T) {
	cases := map[string]struct {
		target string
		header string
	}{
		"directly":        {target: "https://doors.example.com/sign-in"},
		"through a proxy": {target: "/sign-in", header: "https"},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			f := testsupport.NewFactory()
			b := startBrowser(t, f)
			b.SetHeader("X-Forwarded-Proto", c.header)

			got := b.Post(c.target, "email", adminEmail, "password", adminPassword(t, f))

			want := []string{
				"doorcode_session=" + sessionToken + "; Path=/; Max-Age=43200; HttpOnly; Secure; SameSite=Lax",
				"doorcode_flash=signed-in; Path=/; HttpOnly; Secure; SameSite=Lax",
			}
			if diff := cmp.Diff(want, got.SetCookie); diff != "" {
				t.Errorf("cookies (-want +got):\n%s", diff)
			}
		})
	}
}

func TestCrossSiteFormPostIsRefused(t *testing.T) {
	f := testsupport.NewFactory()
	b := startBrowser(t, f)
	b.SetHeader("Sec-Fetch-Site", "cross-site")

	got := b.Post("/sign-in", "email", adminEmail, "password", adminPassword(t, f))

	if diff := cmp.Diff(403, got.Status); diff != "" {
		t.Errorf("status (-want +got):\n%s", diff)
	}
}
