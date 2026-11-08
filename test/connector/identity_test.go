//go:build !slow

package connector_test

import (
	"context"
	"net/url"
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/dakriy/doorcode/internal/connector"
	"github.com/dakriy/doorcode/internal/testsupport"
)

// signInClient is the client the identity tests sign in as.
var signInClient = connector.IdentityClient{
	ID: "doorcode", Secret: "s3cret", Scopes: []string{"openid", "email"}, RedirectURL: "https://doorcode.example.com/sign-in/oidc/callback",
}

// discovered discovers the provider s reaches, failing the test if it
// cannot.
func discovered(t *testing.T, s identitySession) connector.IdentityProvider {
	t.Helper()
	p, err := s.identity.Discover(context.Background(), s.issuer)
	if err != nil {
		t.Fatal(err)
	}
	return p
}

// signedIn signs Ann in at the provider s reaches and returns the provider
// and the code it issued.
func signedIn(t *testing.T, s identitySession) (connector.IdentityProvider, string) {
	t.Helper()
	p := discovered(t, s)
	authURL := p.AuthURL(signInClient, "the-state", "the-nonce", "the-verifier-of-at-least-forty-three-characters")
	code := s.authorize(t, authURL, map[string]any{"sub": "ann", "email": "ann@example.com"},
		map[string]any{"sub": "ann", "family_name": "Smith"})
	return p, code
}

func TestIdentityAuthURLAsksForACodeWithStateNonceAndAPKCEChallenge(t *testing.T) {
	for _, impl := range identityImpls {
		t.Run(impl.name, func(t *testing.T) {
			s := impl.open(t, providerSpec{})

			got, err := url.Parse(discovered(t, s).AuthURL(signInClient, "the-state", "the-nonce", "the-verifier"))
			if err != nil {
				t.Fatal(err)
			}

			want := []any{s.issuer + "/authorize", url.Values{
				"client_id": {"doorcode"}, "redirect_uri": {signInClient.RedirectURL}, "response_type": {"code"},
				"scope": {"openid email"}, "state": {"the-state"}, "nonce": {"the-nonce"},
				"code_challenge": {testsupport.S256("the-verifier")}, "code_challenge_method": {"S256"},
			}}
			if diff := cmp.Diff(want, []any{got.Scheme + "://" + got.Host + got.Path, got.Query()}); diff != "" {
				t.Errorf("sign-in page, query (-want +got):\n%s", diff)
			}
		})
	}
}

func TestIdentityExchangeReturnsTheIDTokensClaims(t *testing.T) {
	for _, impl := range identityImpls {
		t.Run(impl.name, func(t *testing.T) {
			s := impl.open(t, providerSpec{secret: "s3cret"})
			p, code := signedIn(t, s)

			grant, err := p.Exchange(context.Background(), signInClient, code, "the-verifier-of-at-least-forty-three-characters")
			if err != nil {
				t.Fatal(err)
			}

			got := []any{grant.Claims["sub"], grant.Claims["email"], grant.Claims["nonce"], grant.AccessToken != ""}
			if diff := cmp.Diff([]any{"ann", "ann@example.com", "the-nonce", true}, got); diff != "" {
				t.Errorf("subject, email, nonce, access token given (-want +got):\n%s", diff)
			}
		})
	}
}

func TestIdentityExchangeRefusesWhatDoesNotMatchTheSignIn(t *testing.T) {
	other := signInClient
	other.ID = "someone-else"
	wrongSecret := signInClient
	wrongSecret.Secret = "guess"
	cases := map[string]struct {
		client   connector.IdentityClient
		verifier string
		code     func(code string) string
	}{
		"another verifier":       {signInClient, "another-verifier-of-at-least-forty-three-chars", nil},
		"another client":         {other, "the-verifier-of-at-least-forty-three-characters", nil},
		"another client secret":  {wrongSecret, "the-verifier-of-at-least-forty-three-characters", nil},
		"a code it never issued": {signInClient, "the-verifier-of-at-least-forty-three-characters", func(string) string { return "made-up" }},
	}
	for _, impl := range identityImpls {
		for name, c := range cases {
			t.Run(impl.name+"/"+name, func(t *testing.T) {
				s := impl.open(t, providerSpec{secret: "s3cret"})
				p, code := signedIn(t, s)
				if c.code != nil {
					code = c.code(code)
				}

				_, err := p.Exchange(context.Background(), c.client, code, c.verifier)

				if err == nil {
					t.Error("the exchange was accepted")
				}
			})
		}
	}
}

func TestIdentityExchangeRedeemsACodeOnce(t *testing.T) {
	for _, impl := range identityImpls {
		t.Run(impl.name, func(t *testing.T) {
			s := impl.open(t, providerSpec{})
			p, code := signedIn(t, s)
			verifier := "the-verifier-of-at-least-forty-three-characters"
			_, first := p.Exchange(context.Background(), signInClient, code, verifier)

			_, second := p.Exchange(context.Background(), signInClient, code, verifier)

			if diff := cmp.Diff([]bool{true, false}, []bool{first == nil, second == nil}); diff != "" {
				t.Errorf("first and second exchange accepted (-want +got):\n%s", diff)
			}
		})
	}
}

func TestIdentityUserInfoAnswersForTheAccessToken(t *testing.T) {
	for _, impl := range identityImpls {
		t.Run(impl.name, func(t *testing.T) {
			s := impl.open(t, providerSpec{})
			p, code := signedIn(t, s)
			grant, err := p.Exchange(context.Background(), signInClient, code, "the-verifier-of-at-least-forty-three-characters")
			if err != nil {
				t.Fatal(err)
			}

			got, err := p.UserInfo(context.Background(), grant.AccessToken)

			if diff := cmp.Diff([]any{map[string]any{"sub": "ann", "family_name": "Smith"}, "<nil>"}, []any{got, testsupport.ErrorText(err)}); diff != "" {
				t.Errorf("claims, error (-want +got):\n%s", diff)
			}
		})
	}
}

func TestIdentityUserInfoRefusesAnUnknownAccessToken(t *testing.T) {
	for _, impl := range identityImpls {
		t.Run(impl.name, func(t *testing.T) {
			p := discovered(t, impl.open(t, providerSpec{}))

			_, err := p.UserInfo(context.Background(), "made-up")

			if err == nil {
				t.Error("an unknown access token was answered")
			}
		})
	}
}

func TestIdentityReportsTheEndSessionPageTheProviderNames(t *testing.T) {
	for _, impl := range identityImpls {
		for _, named := range []bool{true, false} {
			t.Run(impl.name+map[bool]string{true: "/named", false: "/none"}[named], func(t *testing.T) {
				s := impl.open(t, providerSpec{endSession: named})

				got := discovered(t, s).EndSessionURL()

				want := ""
				if named {
					want = s.issuer + "/logout"
				}
				if diff := cmp.Diff(want, got); diff != "" {
					t.Errorf("end-session page (-want +got):\n%s", diff)
				}
			})
		}
	}
}

func TestIdentityDiscoveryFailsWhenTheProviderDoesNotAnswer(t *testing.T) {
	for _, impl := range identityImpls {
		t.Run(impl.name, func(t *testing.T) {
			s := impl.open(t, providerSpec{unreachable: true})

			_, err := s.identity.Discover(context.Background(), s.issuer)

			if err == nil {
				t.Error("discovery succeeded")
			}
		})
	}
}
