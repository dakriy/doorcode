package connector_test

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	"github.com/coreos/go-oidc/v3/oidc/oidctest"

	"github.com/dakriy/doorcode/internal/connector"
	"github.com/dakriy/doorcode/internal/driver/oidcprovider"
	"github.com/dakriy/doorcode/internal/testsupport"
)

// providerSpec is how the provider an identity test reaches is set up.
type providerSpec struct {
	// secret is the client secret its token endpoint takes; empty takes
	// any.
	secret string
	// endSession says it names an end-session page, issuer/logout.
	endSession bool
	// unreachable says nothing answers at its issuer.
	unreachable bool
}

// identitySession is an Identity with the issuer of the provider it
// reaches, and authorize, which signs a person in at the sign-in page
// authURL names, the ID token carrying claims and the userinfo endpoint
// answering with userInfo, and returns the code the provider issues.
type identitySession struct {
	identity  connector.Identity
	issuer    string
	authorize func(t *testing.T, authURL string, claims, userInfo map[string]any) string
}

type identityImpl struct {
	name string
	open func(t *testing.T, spec providerSpec) identitySession
}

var identityImpls = []identityImpl{
	{name: "stub", open: openStubIdentity},
	{name: "go-oidc", open: openFakeProvider},
}

func openStubIdentity(_ *testing.T, spec providerSpec) identitySession {
	stub := testsupport.NewIdentity()
	issuer := "https://id.example.com"
	stub.ClientSecret = spec.secret
	if spec.endSession {
		stub.EndSession = issuer + "/logout"
	}
	if spec.unreachable {
		stub.FailDiscovery()
	}
	return identitySession{identity: stub, issuer: issuer, authorize: func(_ *testing.T, authURL string, claims, userInfo map[string]any) string {
		return testsupport.QueryParam(stub.AuthorizeWithUserInfo(authURL, claims, userInfo), "code")
	}}
}

// fakeProvider is a local OpenID Connect provider over TLS: discovery, its
// signing keys, a token endpoint that checks the client, the redirect
// address, and the PKCE verifier, and a userinfo endpoint.
type fakeProvider struct {
	spec   providerSpec
	url    string
	key    *ecdsa.PrivateKey
	keys   *oidctest.Server
	mu     sync.Mutex
	codes  map[string]fakeGrant
	tokens map[string]map[string]any
}

type fakeGrant struct {
	claims, userInfo                 map[string]any
	clientID, redirectURL, challenge string
}

const fakeKeyID = "key-1"

func openFakeProvider(t *testing.T, spec providerSpec) identitySession {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	p := &fakeProvider{
		spec: spec, key: key, codes: map[string]fakeGrant{}, tokens: map[string]map[string]any{},
		keys: &oidctest.Server{PublicKeys: []oidctest.PublicKey{{PublicKey: key.Public(), KeyID: fakeKeyID, Algorithm: oidc.ES256}}},
	}
	srv := httptest.NewTLSServer(p)
	p.url = srv.URL
	if spec.unreachable {
		srv.Close()
	} else {
		t.Cleanup(srv.Close)
	}
	return identitySession{identity: oidcprovider.New(srv.Client()), issuer: srv.URL, authorize: p.authorize}
}

// authorize plays the person at the provider's sign-in page.
func (p *fakeProvider) authorize(t *testing.T, authURL string, claims, userInfo map[string]any) string {
	t.Helper()
	if !strings.HasPrefix(authURL, p.url+"/authorize?") {
		t.Fatalf("sign-in page %s is not the provider's", authURL)
	}
	token := map[string]any{"nonce": testsupport.QueryParam(authURL, "nonce")}
	for name, value := range claims {
		token[name] = value
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	code := "code-" + string(rune('a'+len(p.codes)))
	p.codes[code] = fakeGrant{
		claims: token, userInfo: userInfo, clientID: testsupport.QueryParam(authURL, "client_id"),
		redirectURL: testsupport.QueryParam(authURL, "redirect_uri"), challenge: testsupport.QueryParam(authURL, "code_challenge"),
	}
	return code
}

func (p *fakeProvider) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	switch r.URL.Path {
	case "/.well-known/openid-configuration":
		p.discovery(w)
	case "/keys":
		p.keys.ServeHTTP(w, r)
	case "/token":
		p.token(w, r)
	case "/userinfo":
		p.userInfo(w, r)
	default:
		http.NotFound(w, r)
	}
}

func (p *fakeProvider) discovery(w http.ResponseWriter) {
	doc := map[string]any{
		"issuer": p.url, "authorization_endpoint": p.url + "/authorize", "token_endpoint": p.url + "/token",
		"jwks_uri": p.url + "/keys", "userinfo_endpoint": p.url + "/userinfo",
		"response_types_supported": []string{"code"}, "subject_types_supported": []string{"public"},
		"id_token_signing_alg_values_supported": []string{oidc.ES256},
	}
	if p.spec.endSession {
		doc["end_session_endpoint"] = p.url + "/logout"
	}
	writeFakeJSON(w, http.StatusOK, doc)
}

func (p *fakeProvider) token(w http.ResponseWriter, r *http.Request) {
	clientID, secret, _ := r.BasicAuth()
	if err := r.ParseForm(); err != nil || clientID == "" {
		clientID, secret = r.PostForm.Get("client_id"), r.PostForm.Get("client_secret")
	}
	p.mu.Lock()
	g, found := p.codes[r.PostForm.Get("code")]
	delete(p.codes, r.PostForm.Get("code"))
	p.mu.Unlock()
	refused := !found || clientID != g.clientID || (p.spec.secret != "" && secret != p.spec.secret) ||
		r.PostForm.Get("redirect_uri") != g.redirectURL || testsupport.S256(r.PostForm.Get("code_verifier")) != g.challenge
	if refused {
		writeFakeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid_grant"})
		return
	}
	claims := map[string]any{"iss": p.url, "aud": clientID, "iat": time.Now().Unix(), "exp": time.Now().Add(time.Hour).Unix()}
	for name, value := range g.claims {
		claims[name] = value
	}
	body, _ := json.Marshal(claims)
	access := "access-" + r.PostForm.Get("code")
	p.mu.Lock()
	p.tokens[access] = g.userInfo
	p.mu.Unlock()
	writeFakeJSON(w, http.StatusOK, map[string]any{
		"access_token": access, "token_type": "Bearer", "expires_in": 3600,
		"id_token": oidctest.SignIDToken(p.key, fakeKeyID, oidc.ES256, string(body)),
	})
}

func (p *fakeProvider) userInfo(w http.ResponseWriter, r *http.Request) {
	p.mu.Lock()
	info, found := p.tokens[strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")]
	p.mu.Unlock()
	if !found {
		writeFakeJSON(w, http.StatusUnauthorized, map[string]any{"error": "invalid_token"})
		return
	}
	writeFakeJSON(w, http.StatusOK, info)
}

func writeFakeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
