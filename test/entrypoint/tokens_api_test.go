package entrypoint_test

import (
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/dakriy/doorcode/internal/testsupport"
)

// firstSecret is the secret of the first token created after signedInAdmin,
// drawn from the stub entropy.
const firstSecret = "dct_QkNERUZHSElKS0xNTk9QUVJTVFVWV1hZWltcXV5fYGE"

// createToken creates a token through the API as b's user and returns its
// secret.
func createToken(t *testing.T, b *testsupport.Browser, body string) string {
	t.Helper()
	got := b.PostJSON("/api/v1/tokens", body)
	if got.Status != 201 {
		t.Fatalf("token creation answered %d, want 201:\n%s", got.Status, got.Body)
	}
	return testsupport.JSONString(got.Body, "/secret")
}

// tokenClient is a client of h without cookies that sends secret as a
// bearer token.
func tokenClient(h testsupport.Handler, secret string) *testsupport.Browser {
	c := testsupport.NewBrowser(h)
	c.SetHeader("Authorization", "Bearer "+secret)
	return c
}

// adminWithToken starts the application from f, signs in the first
// administrator, creates a token with body as them, and returns the handler
// and the token's secret.
func adminWithToken(t *testing.T, f *testsupport.Factory, body string) (testsupport.Handler, string) {
	t.Helper()
	h, b := adminSession(t, f)
	return h, createToken(t, b, body)
}

func TestCreatingATokenAnswersWithItsSecretOnce(t *testing.T) {
	got := signedInAdmin(t, testsupport.NewFactory()).PostJSON("/api/v1/tokens", `{"name":" deploy script ","expires_in_days":90}`)

	want := testsupport.Response{Status: 201, ContentType: jsonType, Body: `{"token":{"id":1,"name":"deploy script",` +
		`"created_at":"2026-01-05T09:00:00Z","expires_at":"2026-04-05T09:00:00Z","expired":false},"secret":"` + firstSecret + `"}` + "\n"}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("response (-want +got):\n%s", diff)
	}
}

func TestTokenCallsTheAPIAsItsUser(t *testing.T) {
	f := testsupport.NewFactory()
	h, b := adminSession(t, f)
	secret := createToken(t, b, `{"name":"ci"}`)
	f.ClockStub.Set("2026-01-05T15:00:00Z")
	createToken(t, b, `{"name":"backup","expires_in_days":1}`)

	got := tokenClient(h, secret).Get("/api/v1/tokens")

	want := testsupport.Response{Status: 200, ContentType: jsonType, Body: `{"tokens":[` +
		`{"id":1,"name":"ci","created_at":"2026-01-05T09:00:00Z","expired":false},` +
		`{"id":2,"name":"backup","created_at":"2026-01-05T15:00:00Z","expires_at":"2026-01-06T15:00:00Z","expired":false}]}` + "\n"}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("response (-want +got):\n%s", diff)
	}
}

func TestTokenCarriesItsUsersRoles(t *testing.T) {
	h, secret := adminWithToken(t, testsupport.NewFactory(), `{"name":"ci"}`)

	got := tokenClient(h, secret).PutJSON("/api/v1/settings/site_name", `{"value":"North Campus"}`)

	if diff := cmp.Diff("North Campus", testsupport.JSONString(got.Body, "/value")); diff != "" {
		t.Errorf("stored value (-want +got):\n%s\n%s", diff, got.Body)
	}
}

func TestTokenIsAcceptedAsTheBasicAuthenticationPassword(t *testing.T) {
	h, secret := adminWithToken(t, testsupport.NewFactory(), `{"name":"ci"}`)
	c := testsupport.NewBrowser(h)
	c.SetHeader("Authorization", testsupport.BasicAuth("anything", secret))

	got := c.Get("/api/v1/settings/site_name")

	if diff := cmp.Diff([]any{200, "site_name"}, []any{got.Status, testsupport.JSONString(got.Body, "/key")}); diff != "" {
		t.Errorf("status and key (-want +got):\n%s\n%s", diff, got.Body)
	}
}

func TestTokenOfAUserWithoutTheRoleIsRefused(t *testing.T) {
	f := testsupport.NewFactory()
	h, secret := adminWithToken(t, f, `{"name":"ci"}`)
	f.StoreStub.RevokeRoles(adminEmail)

	got := tokenClient(h, secret).Get("/api/v1/settings")

	want := testsupport.Response{Status: 403, ContentType: jsonType, Body: `{"error":"the admin role is required"}` + "\n"}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("response (-want +got):\n%s", diff)
	}
}

func TestTokenIsRefused(t *testing.T) {
	cases := map[string]struct {
		body    string
		arrange func(t *testing.T, f *testsupport.Factory, b *testsupport.Browser)
	}{
		"once revoked": {body: `{"name":"ci"}`, arrange: func(t *testing.T, _ *testsupport.Factory, b *testsupport.Browser) {
			if got := b.Delete("/api/v1/tokens/1"); got.Status != 204 {
				t.Fatalf("revocation answered %d:\n%s", got.Status, got.Body)
			}
		}},
		"once expired": {body: `{"name":"ci","expires_in_days":1}`, arrange: func(_ *testing.T, f *testsupport.Factory, _ *testsupport.Browser) {
			f.ClockStub.Set("2026-01-06T09:00:00Z")
		}},
		"once its user has expired": {body: `{"name":"ci"}`, arrange: func(_ *testing.T, f *testsupport.Factory, _ *testsupport.Browser) {
			f.StoreStub.ExpireUser(adminEmail, "2026-01-05T09:00:00Z")
		}},
		"once its user is deleted": {body: `{"name":"ci"}`, arrange: func(_ *testing.T, f *testsupport.Factory, _ *testsupport.Browser) {
			f.StoreStub.DeleteUser(adminEmail, "2026-01-05T09:00:00Z")
		}},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			f := testsupport.NewFactory()
			h, b := adminSession(t, f)
			secret := createToken(t, b, c.body)
			c.arrange(t, f, b)

			got := tokenClient(h, secret).Get("/api/v1/tokens")

			want := testsupport.Response{Status: 401, ContentType: jsonType, Body: `{"error":"a valid API token is required"}` + "\n"}
			if diff := cmp.Diff(want, got); diff != "" {
				t.Errorf("response (-want +got):\n%s", diff)
			}
		})
	}
}

func TestTokenWorksUntilTheMomentItExpires(t *testing.T) {
	times := map[string]int{
		"2026-01-06T08:59:59Z": 200,
		"2026-01-06T09:00:00Z": 401,
	}
	for now, want := range times {
		t.Run(now, func(t *testing.T) {
			f := testsupport.NewFactory()
			h, secret := adminWithToken(t, f, `{"name":"ci","expires_in_days":1}`)
			f.ClockStub.Set(now)

			got := tokenClient(h, secret).Get("/api/v1/tokens")

			if diff := cmp.Diff(want, got.Status); diff != "" {
				t.Errorf("status (-want +got):\n%s", diff)
			}
		})
	}
}

func TestUnknownTokenIsRefusedEvenWithASessionCookie(t *testing.T) {
	b := signedInAdmin(t, testsupport.NewFactory())
	b.SetHeader("Authorization", "Bearer dct_unknown")

	got := b.Get("/api/v1/tokens")

	if diff := cmp.Diff(401, got.Status); diff != "" {
		t.Errorf("status (-want +got):\n%s", diff)
	}
}

func TestTokenListShowsExpiredTokens(t *testing.T) {
	f := testsupport.NewFactory()
	h, b := adminSession(t, f)
	createToken(t, b, `{"name":"old","expires_in_days":1}`)
	createToken(t, b, `{"name":"current"}`)
	f.ClockStub.Set("2026-01-07T00:00:00Z")

	got := tokenClient(h, "dct_YmNkZWZnaGlqa2xtbm9wcXJzdHV2d3h5ent8fX5_gIE").Get("/api/v1/tokens")

	want := testsupport.JSON(`{"id":1,"name":"old","created_at":"2026-01-05T09:00:00Z","expires_at":"2026-01-06T09:00:00Z","expired":true}`)
	if diff := cmp.Diff(want, testsupport.JSONAt(got.Body, "/tokens/0")); diff != "" {
		t.Errorf("tokens (-want +got):\n%s", diff)
	}
}

func TestTokenListIsEmptyBeforeAnyTokenIsCreated(t *testing.T) {
	got := signedInAdmin(t, testsupport.NewFactory()).Get("/api/v1/tokens")

	want := testsupport.Response{Status: 200, ContentType: jsonType, Body: `{"tokens":[]}` + "\n"}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("response (-want +got):\n%s", diff)
	}
}

func TestRevokingATokenRemovesItFromTheList(t *testing.T) {
	b := signedInAdmin(t, testsupport.NewFactory())
	createToken(t, b, `{"name":"one"}`)
	createToken(t, b, `{"name":"two"}`)

	revoked := b.Delete("/api/v1/tokens/1")
	got := b.Get("/api/v1/tokens")

	if diff := cmp.Diff(testsupport.Response{Status: 204}, revoked); diff != "" {
		t.Errorf("revocation (-want +got):\n%s", diff)
	}
	want := testsupport.JSON(`[{"id":2,"name":"two","created_at":"2026-01-05T09:00:00Z","expired":false}]`)
	if diff := cmp.Diff(want, testsupport.JSONAt(got.Body, "/tokens")); diff != "" {
		t.Errorf("tokens left (-want +got):\n%s", diff)
	}
}

func TestRevokingATokenTheCallerDoesNotHaveIsNotFound(t *testing.T) {
	for _, id := range []string{"7", "first"} {
		t.Run(id, func(t *testing.T) {
			b := signedInAdmin(t, testsupport.NewFactory())
			createToken(t, b, `{"name":"one"}`)

			got := b.Delete("/api/v1/tokens/" + id)

			want := testsupport.Response{Status: 404, ContentType: jsonType, Body: `{"error":"you have no token with this id"}` + "\n"}
			if diff := cmp.Diff(want, got); diff != "" {
				t.Errorf("response (-want +got):\n%s", diff)
			}
		})
	}
}

func TestTokenRequestIsChecked(t *testing.T) {
	long := `"` + repeat("x", 101) + `"`
	cases := map[string]struct {
		body string
		want string
	}{
		"not json":          {body: `name=ci`, want: `body must be a JSON object with a string \"name\"`},
		"name not a string": {body: `{"name":3}`, want: `body must be a JSON object with a string \"name\"`},
		"no name":           {body: `{}`, want: "Enter a name for the token."},
		"blank name":        {body: `{"name":"  "}`, want: "Enter a name for the token."},
		"name too long":     {body: `{"name":` + long + `}`, want: "Use at most 100 characters for the name."},
		"no days":           {body: `{"name":"ci","expires_in_days":0}`, want: "Choose an expiry from 1 to 3650 days."},
		"too many days":     {body: `{"name":"ci","expires_in_days":3651}`, want: "Choose an expiry from 1 to 3650 days."},
		"both wrong":        {body: `{"name":"","expires_in_days":-1}`, want: "Enter a name for the token."},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			got := signedInAdmin(t, testsupport.NewFactory()).PostJSON("/api/v1/tokens", c.body)

			want := testsupport.Response{Status: 400, ContentType: jsonType, Body: `{"error":"` + c.want + `"}` + "\n"}
			if diff := cmp.Diff(want, got); diff != "" {
				t.Errorf("response (-want +got):\n%s", diff)
			}
		})
	}
}

func TestTokenRequestAcceptsTheEdgesOfItsRanges(t *testing.T) {
	bodies := map[string]string{
		"100 characters": `{"name":"` + repeat("é", 100) + `"}`,
		"one day":        `{"name":"ci","expires_in_days":1}`,
		"3650 days":      `{"name":"ci","expires_in_days":3650}`,
	}
	for name, body := range bodies {
		t.Run(name, func(t *testing.T) {
			got := signedInAdmin(t, testsupport.NewFactory()).PostJSON("/api/v1/tokens", body)

			if diff := cmp.Diff(201, got.Status); diff != "" {
				t.Errorf("status (-want +got):\n%s\n%s", diff, got.Body)
			}
		})
	}
}

func TestTokenStoreFailureAnswersWithARequestID(t *testing.T) {
	cases := map[string]struct {
		request func(b *testsupport.Browser) testsupport.Response
		id      string
	}{
		"list": {id: "4243444546474849", request: func(b *testsupport.Browser) testsupport.Response {
			return b.Get("/api/v1/tokens")
		}},
		"create, after drawing the secret": {id: "6263646566676869", request: func(b *testsupport.Browser) testsupport.Response {
			return b.PostJSON("/api/v1/tokens", `{"name":"ci"}`)
		}},
		"revoke": {id: "4243444546474849", request: func(b *testsupport.Browser) testsupport.Response {
			return b.Delete("/api/v1/tokens/1")
		}},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			f := testsupport.NewFactory()
			b := signedInAdmin(t, f)
			f.StoreStub.FailTokens()

			got := c.request(b)

			want := testsupport.Response{Status: 500, ContentType: jsonType, Body: `{"error":"internal error","request_id":"` + c.id + `"}` + "\n"}
			if diff := cmp.Diff(want, got); diff != "" {
				t.Errorf("response (-want +got):\n%s", diff)
			}
		})
	}
}

func TestTokenThatCannotBeCheckedAnswersWithARequestID(t *testing.T) {
	f := testsupport.NewFactory()
	h, secret := adminWithToken(t, f, `{"name":"ci"}`)
	f.StoreStub.FailTokens()

	got := tokenClient(h, secret).Get("/api/v1/settings")

	want := testsupport.Response{Status: 500, ContentType: jsonType, Body: `{"error":"internal error","request_id":"6263646566676869"}` + "\n"}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("response (-want +got):\n%s", diff)
	}
}

// repeat is s n times.
func repeat(s string, n int) string {
	out := ""
	for range n {
		out += s
	}
	return out
}
