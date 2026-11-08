package testsupport

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"maps"
	"net/url"
	"strings"

	"github.com/dakriy/doorcode/internal/connector"
)

// SecretKey is the key, in base64, the stub environment sets in
// DOORCODE_SECRET_KEY to encrypt secret settings.
const SecretKey = "MDEyMzQ1Njc4OWFiY2RlZjAxMjM0NTY3ODlhYmNkZWY="

// OpenSecret decrypts stored, the value the store holds for the secret
// setting named key, as the application encrypted it under SecretKey: its
// nonce and AES-256-GCM ciphertext, bound to key, each in base64url and
// joined by a dot.
func OpenSecret(key, stored string) (string, error) {
	raw, _ := base64.StdEncoding.DecodeString(SecretKey)
	block, _ := aes.NewCipher(raw)
	aead, _ := cipher.NewGCM(block)
	nonceText, sealedText, found := strings.Cut(stored, ".")
	nonce, nonceErr := base64.RawURLEncoding.DecodeString(nonceText)
	sealed, sealedErr := base64.RawURLEncoding.DecodeString(sealedText)
	if !found || nonceErr != nil || sealedErr != nil || len(nonce) != aead.NonceSize() {
		return "", errors.New("stub: not an encrypted value")
	}
	value, err := aead.Open(nil, nonce, sealed, []byte(key))
	return string(value), err
}

// StoredSetting is the value the store holds for key, as the application
// wrote it.
func (s *Store) StoredSetting(key string) string {
	return s.settings[key].Value
}

// Identity is the stub connector.Identity. Every issuer is discovered as a
// provider whose sign-in page is the issuer's /authorize, until
// FailDiscovery. A test plays the person at that page with Authorize, which
// returns where the provider sends the browser back to.
type Identity struct {
	// EndSession is the end_session_endpoint every provider names; empty
	// names none.
	EndSession string
	// ClientSecret, when set, is the only client secret the token
	// endpoint accepts.
	ClientSecret  string
	failDiscovery bool
	failUserInfo  bool
	grants        map[string]identityGrant
	userInfo      map[string]map[string]any
	codes         int
}

type identityGrant struct {
	claims      map[string]any
	userInfo    map[string]any
	challenge   string
	clientID    string
	redirectURL string
}

// NewIdentity returns a stub identity whose providers have no
// end_session_endpoint and accept any client secret.
func NewIdentity() *Identity {
	return &Identity{grants: map[string]identityGrant{}, userInfo: map[string]map[string]any{}}
}

// FailDiscovery makes discovering any provider fail with ErrUnavailable.
func (i *Identity) FailDiscovery() {
	i.failDiscovery = true
}

// FailUserInfo makes every userinfo request fail with ErrUnavailable.
func (i *Identity) FailUserInfo() {
	i.failUserInfo = true
}

// Authorize signs a person in at the provider's page authURL names, whose
// ID token carries claims, plus the nonce authURL carries when claims has
// none, and returns the path and query the provider sends the browser back
// to. The userinfo endpoint answers with the ID token's subject alone.
func (i *Identity) Authorize(authURL string, claims map[string]any) string {
	return i.AuthorizeWithUserInfo(authURL, claims, map[string]any{"sub": claims["sub"]})
}

// AuthorizeWithUserInfo is Authorize with the userinfo endpoint answering
// with userInfo.
func (i *Identity) AuthorizeWithUserInfo(authURL string, claims, userInfo map[string]any) string {
	query := queryOf(authURL)
	token := maps.Clone(claims)
	if _, set := token["nonce"]; !set {
		token["nonce"] = query.Get("nonce")
	}
	i.codes++
	code := fmt.Sprintf("code-%d", i.codes)
	i.grants[code] = identityGrant{
		claims: token, userInfo: userInfo, challenge: query.Get("code_challenge"),
		clientID: query.Get("client_id"), redirectURL: query.Get("redirect_uri"),
	}
	return Target(query.Get("redirect_uri") + "?" + url.Values{"code": {code}, "state": {query.Get("state")}}.Encode())
}

// Deny refuses the sign-in authURL names, as a person who cancels at the
// provider's page does, and returns where the provider sends the browser
// back to.
func (i *Identity) Deny(authURL string) string {
	query := queryOf(authURL)
	answer := url.Values{"error": {"access_denied"}, "error_description": {"The person cancelled."}, "state": {query.Get("state")}}
	return Target(query.Get("redirect_uri") + "?" + answer.Encode())
}

// QueryParam is the parameter named name of the query rawURL carries.
func QueryParam(rawURL, name string) string {
	return queryOf(rawURL).Get(name)
}

func queryOf(rawURL string) url.Values {
	u, err := url.Parse(rawURL)
	if err != nil {
		panic(err)
	}
	return u.Query()
}

// Discover returns the stub provider of issuer.
func (i *Identity) Discover(_ context.Context, issuer string) (connector.IdentityProvider, error) {
	if i.failDiscovery {
		return nil, ErrUnavailable
	}
	return identityProvider{stub: i, issuer: issuer}, nil
}

type identityProvider struct {
	stub   *Identity
	issuer string
}

// AuthURL is the provider's /authorize with the parameters a code flow
// with PKCE carries.
func (p identityProvider) AuthURL(c connector.IdentityClient, state, nonce, verifier string) string {
	query := url.Values{
		"client_id": {c.ID}, "redirect_uri": {c.RedirectURL}, "response_type": {"code"},
		"scope": {strings.Join(c.Scopes, " ")}, "state": {state}, "nonce": {nonce},
		"code_challenge": {S256(verifier)}, "code_challenge_method": {"S256"},
	}
	return strings.TrimSuffix(p.issuer, "/") + "/authorize?" + query.Encode()
}

// S256 is the PKCE challenge of verifier.
func S256(verifier string) string {
	sum := sha256.Sum256([]byte(verifier))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}

// Exchange redeems a code Authorize issued, once, refusing a verifier,
// client, or redirect address other than the sign-in's, or a client secret
// other than ClientSecret when that is set.
func (p identityProvider) Exchange(_ context.Context, c connector.IdentityClient, code, verifier string) (connector.IdentityGrant, error) {
	g, found := p.stub.grants[code]
	delete(p.stub.grants, code)
	secretRefused := p.stub.ClientSecret != "" && c.Secret != p.stub.ClientSecret
	if !found || S256(verifier) != g.challenge || c.ID != g.clientID || c.RedirectURL != g.redirectURL || secretRefused {
		return connector.IdentityGrant{}, errors.New("stub: invalid_grant")
	}
	token := "access-" + code
	p.stub.userInfo[token] = g.userInfo
	return connector.IdentityGrant{Claims: g.claims, AccessToken: token}, nil
}

// UserInfo answers for an access token Exchange issued.
func (p identityProvider) UserInfo(_ context.Context, accessToken string) (map[string]any, error) {
	info, found := p.stub.userInfo[accessToken]
	if p.stub.failUserInfo || !found {
		return nil, ErrUnavailable
	}
	return info, nil
}

// EndSessionURL is the stub's EndSession.
func (p identityProvider) EndSessionURL() string {
	return p.stub.EndSession
}
