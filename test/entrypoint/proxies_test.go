package entrypoint_test

import (
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/dakriy/doorcode/internal/testsupport"
)

// trustedSite is a browser on a site that trusts the proxies listed, by
// the environment, not yet signed in.
func trustedSite(t *testing.T, f *testsupport.Factory, listed string) *testsupport.Browser {
	t.Helper()
	f.EnvStub["DOORCODE_TRUSTED_PROXIES"] = listed
	return startBrowser(t, f)
}

// signInFrom signs the first administrator in through b from remote, a
// host and port, sending forwarded as X-Forwarded-For, and returns the
// line the sign-in was logged with.
func signInFrom(t *testing.T, f *testsupport.Factory, b *testsupport.Browser, remote, forwarded string) testsupport.LogEntry {
	t.Helper()
	b.SetRemote(remote)
	b.SetHeader("X-Forwarded-For", forwarded)
	if got := b.Post("/sign-in", "email", adminEmail, "password", adminPassword(t, f)); got.Status != 303 {
		t.Fatalf("sign-in answered %d, want 303:\n%s", got.Status, got.Body)
	}
	return lastLog(f)
}

func TestASignInIsLoggedWithTheClientsAddress(t *testing.T) {
	t.Parallel()
	f := testsupport.NewFactory()
	b := startBrowser(t, f)

	got := signInFrom(t, f, b, "198.51.100.20:5000", "")

	want := testsupport.LogEntry{Level: "info", Message: "signed in", Fields: map[string]string{"email": adminEmail, "client": "198.51.100.20"}}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("log line (-want +got):\n%s", diff)
	}
}

func TestTheClientIsTheAddressTheTrustedProxiesForwardFor(t *testing.T) {
	t.Parallel()
	cases := map[string]struct {
		remote, forwarded, want string
	}{
		"from no proxy":                         {"198.51.100.20:5000", "", "198.51.100.20"},
		"from an address not listed":            {"198.51.100.20:5000", "192.0.2.77", "198.51.100.20"},
		"from a listed address":                 {"203.0.113.9:443", "192.0.2.77", "192.0.2.77"},
		"from a listed range":                   {"10.1.2.3:443", "192.0.2.77", "192.0.2.77"},
		"through listed proxies":                {"10.0.0.1:443", "192.0.2.77, 10.0.0.2", "192.0.2.77"},
		"past what the client says before it":   {"10.0.0.1:443", "192.0.2.66, 192.0.2.77, 10.0.0.2", "192.0.2.77"},
		"through listed proxies alone":          {"10.0.0.1:443", "10.0.0.3,10.0.0.2", "10.0.0.3"},
		"up to a hop that is not an address":    {"10.0.0.1:443", "192.0.2.77, unknown", "10.0.0.1"},
		"from a listed IPv6 range":              {"[2001:db8::5]:443", "2001:db9::7", "2001:db9::7"},
		"from a listed address written as IPv6": {"[::ffff:10.0.0.1]:443", "192.0.2.77", "192.0.2.77"},
		"from an address listed as IPv6":        {"198.51.100.1:443", "192.0.2.77", "192.0.2.77"},
		"from a remote that is not an address":  {"pipe", "192.0.2.77", "pipe"},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			f := testsupport.NewFactory()
			b := trustedSite(t, f, "203.0.113.9, 10.0.0.0/8,,2001:db8::/32, ::ffff:198.51.100.1")

			got := signInFrom(t, f, b, c.remote, c.forwarded).Fields["client"]

			if diff := cmp.Diff(c.want, got); diff != "" {
				t.Errorf("client (-want +got):\n%s", diff)
			}
		})
	}
}

func TestARefusedSignInIsLoggedWithTheClientsAddress(t *testing.T) {
	t.Parallel()
	f := testsupport.NewFactory()
	b := trustedSite(t, f, "10.0.0.0/8")
	b.SetRemote("10.0.0.1:443")
	b.SetHeader("X-Forwarded-For", "192.0.2.77")

	b.Post("/sign-in", "email", adminEmail, "password", "not the password")

	want := testsupport.LogEntry{Level: "info", Message: "refused a sign-in",
		Fields: map[string]string{"email": adminEmail, "error": "email and password do not match", "client": "192.0.2.77"}}
	if diff := cmp.Diff(want, lastLog(f)); diff != "" {
		t.Errorf("log line (-want +got):\n%s", diff)
	}
}

func TestASignInAtTheProviderIsLoggedWithTheClientsAddress(t *testing.T) {
	t.Parallel()
	f := testsupport.NewFactory()
	f.EnvStub["DOORCODE_TRUSTED_PROXIES"] = "10.0.0.0/8"
	h, _ := providerSite(t, f)
	b := testsupport.NewBrowser(h)
	b.SetRemote("10.0.0.1:443")
	b.SetHeader("X-Forwarded-For", "192.0.2.77")
	started := b.Get("/sign-in/oidc")

	b.Get(f.IdentityStub.Authorize(started.Location, annClaims()))

	want := testsupport.LogEntry{Level: "info", Message: "signed in at the identity provider",
		Fields: map[string]string{"email": "ann@example.com", "client": "192.0.2.77"}}
	if diff := cmp.Diff(want, lastLog(f)); diff != "" {
		t.Errorf("log line (-want +got):\n%s", diff)
	}
}

func TestAFailedRequestIsLoggedWithTheClientsAddress(t *testing.T) {
	t.Parallel()
	cases := map[string]struct {
		fail         func(s *testsupport.Store)
		target, want string
	}{
		"by the trusted proxy's word": {(*testsupport.Store).FailDoors, "/api/v1/doors", "192.0.2.77"},
		// The trusted proxies are settings, so with the settings unreadable
		// no proxy is believed.
		"without the settings": {(*testsupport.Store).FailSettings, "/api/v1/settings", "10.0.0.1"},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			f := testsupport.NewFactory()
			f.EnvStub["DOORCODE_TRUSTED_PROXIES"] = "10.0.0.0/8"
			b := signedInAdmin(t, f)
			b.SetRemote("10.0.0.1:443")
			b.SetHeader("X-Forwarded-For", "192.0.2.77")
			c.fail(f.StoreStub)

			b.Get(c.target)

			got := []string{lastLog(f).Message, lastLog(f).Fields["client"]}
			if diff := cmp.Diff([]string{"request failed", c.want}, got); diff != "" {
				t.Errorf("log line, client (-want +got):\n%s", diff)
			}
		})
	}
}

func TestAProxyNamedByHostNameIsTrustedOnceThePeriodicWorkLooksItUp(t *testing.T) {
	t.Parallel()
	f := testsupport.NewFactory()
	f.EnvStub["DOORCODE_ENROLLMENT_PASSWORD"] = enrollPassword
	f.EnvStub["DOORCODE_TRUSTED_PROXIES"] = "proxy.example.com"
	f.ResolverStub.Resolve("proxy.example.com", "198.51.100.30", "10.9.8.7")
	a := start(t, f)
	b := testsupport.NewBrowser(a.Handler())
	before := signInFrom(t, f, b, "10.9.8.7:443", "192.0.2.77").Fields["client"]

	run(t, a)

	after := signInFrom(t, f, b, "10.9.8.7:443", "192.0.2.77").Fields["client"]
	elsewhere := signInFrom(t, f, b, "10.9.8.6:443", "192.0.2.77").Fields["client"]
	if diff := cmp.Diff([]string{"10.9.8.7", "192.0.2.77", "10.9.8.6"}, []string{before, after, elsewhere}); diff != "" {
		t.Errorf("client before and after the periodic work, and from another address (-want +got):\n%s", diff)
	}
}

func TestAProxyNameThatCannotBeLookedUpFailsThePeriodicWorkButNotTheNamesAfterIt(t *testing.T) {
	t.Parallel()
	f := testsupport.NewFactory()
	f.EnvStub["DOORCODE_ENROLLMENT_PASSWORD"] = enrollPassword
	f.EnvStub["DOORCODE_TRUSTED_PROXIES"] = "ghost.example.com,proxy.example.com"
	f.ResolverStub.Resolve("proxy.example.com", "10.9.8.7")
	a := start(t, f)

	run(t, a)

	logged := lastLog(f)
	client := signInFrom(t, f, testsupport.NewBrowser(a.Handler()), "10.9.8.7:443", "192.0.2.77").Fields["client"]
	want := []any{testsupport.LogEntry{Level: "error", Message: "periodic work failed",
		Fields: map[string]string{"error": "look up trusted proxy ghost.example.com: no such host"}}, "192.0.2.77"}
	if diff := cmp.Diff(want, []any{logged, client}); diff != "" {
		t.Errorf("log line, client (-want +got):\n%s", diff)
	}
}

func TestTheTrustedProxiesSettingTakesAddressesRangesAndHostNames(t *testing.T) {
	t.Parallel()
	refusal := "invalid value for trusted_proxies: must be IP addresses, CIDR ranges, and host names separated by commas, such as 10.0.0.0/8,proxy.example.com"
	cases := map[string]struct {
		value string
		want  []any
	}{
		"a list of each":     {"10.0.0.0/8, proxy.example.com,203.0.113.9,,2001:db8::/32", []any{200, nil}},
		"nothing":            {"", []any{200, nil}},
		"too wide a range":   {"10.0.0.0/33", []any{400, refusal}},
		"a name with spaces": {"proxy example.com", []any{400, refusal}},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			b := signedInAdmin(t, testsupport.NewFactory())

			got := b.PutJSON("/api/v1/settings/trusted_proxies", `{"value":"`+c.value+`"}`)

			if diff := cmp.Diff(c.want, []any{got.Status, testsupport.JSONAt(got.Body, "/error")}); diff != "" {
				t.Errorf("status, error (-want +got):\n%s", diff)
			}
		})
	}
}
