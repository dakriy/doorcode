// Package identity signs people in through an OpenID Connect provider: it
// sends the browser to the provider with state, a nonce, and a PKCE
// challenge, checks what comes back, finds or creates the user the
// provider names, and keeps the groups the provider's entitlements decide
// in step.
package identity

import (
	"cmp"
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"maps"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"sync"

	"github.com/dakriy/doorcode/internal/auth"
	"github.com/dakriy/doorcode/internal/connector"
	"github.com/dakriy/doorcode/internal/directory"
	"github.com/dakriy/doorcode/internal/settings"
)

// CallbackPath is where the provider sends the browser back to, after the
// site's public address.
const CallbackPath = "/sign-in/oidc/callback"

// SignedOutPath is where the provider sends the browser once it has ended
// its session, after the site's public address.
const SignedOutPath = "/sign-in"

// How many entropy bytes each value of a pending sign-in takes.
const (
	stateBytes    = 16
	nonceBytes    = 16
	verifierBytes = 32
)

var randomEncoding = base64.RawURLEncoding

// The reasons a sign-in through the provider is refused.
var (
	ErrDisabled        = errors.New("signing in through the identity provider is not enabled")
	ErrIncomplete      = errors.New("signing in through the identity provider needs the oidc_issuer, oidc_client_id, and public_url settings")
	ErrUnreachable     = errors.New("the identity provider could not be reached")
	ErrStale           = errors.New("the answer does not belong to a sign-in this browser started")
	ErrDenied          = errors.New("the identity provider did not sign the person in")
	ErrUnverified      = errors.New("the identity provider's answer could not be verified")
	ErrNoEmail         = errors.New("the identity provider gave no email address")
	ErrEmailUnverified = errors.New("the identity provider has not verified the email address")
)

// Service signs people in through the provider the settings name.
type Service struct {
	Settings  settings.Service
	Directory directory.Service
	Auth      auth.Service
	Identity  connector.Identity
	Entropy   connector.Entropy
	Clock     connector.Clock
	Providers *Providers
}

// Providers remembers the provider last discovered, so discovery happens
// on first use and again only once the issuer changes. Its zero value is
// ready to use; the issuer it remembers is never empty.
type Providers struct {
	mu       sync.Mutex
	issuer   string
	provider connector.IdentityProvider
}

// of returns the provider whose issuer is issuer, discovering it through
// identity unless it is the one remembered.
func (p *Providers) of(ctx context.Context, identity connector.Identity, issuer string) (connector.IdentityProvider, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.issuer == issuer {
		return p.provider, nil
	}
	provider, err := identity.Discover(ctx, issuer)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrUnreachable, err)
	}
	p.issuer, p.provider = issuer, provider
	return provider, nil
}

// Begun is a sign-in sent on to the provider: the address of its sign-in
// page, and what the browser keeps until it comes back.
type Begun struct {
	URL    string
	Cookie string
}

// Finished is a sign-in the provider completed: the session token the
// browser keeps, and the local path it goes on to.
type Finished struct {
	Token string
	Next  string
}

// Start begins a sign-in that returns to next.
func (s Service) Start(ctx context.Context, next string) (Begun, error) {
	_, client, provider, err := s.provider(ctx)
	if err != nil {
		return Begun{}, err
	}
	p := url.Values{}
	p.Set("state", s.random(stateBytes))
	p.Set("nonce", s.random(nonceBytes))
	p.Set("verifier", s.random(verifierBytes))
	p.Set("next", next)
	return Begun{URL: provider.AuthURL(client, p.Get("state"), p.Get("nonce"), p.Get("verifier")), Cookie: p.Encode()}, nil
}

// Finish completes the sign-in cookie holds with the answer query
// carries, and starts a session for the user the provider names.
func (s Service) Finish(ctx context.Context, cookie string, query url.Values) (Finished, error) {
	secret, err := s.Settings.ClientSecret(ctx)
	if err != nil {
		return Finished{}, err
	}
	current, client, provider, err := s.provider(ctx)
	if err != nil {
		return Finished{}, err
	}
	client.Secret = secret
	pending, _ := url.ParseQuery(cookie)
	if pending.Get("state") == "" || query.Get("state") != pending.Get("state") {
		return Finished{}, ErrStale
	}
	if answer := query.Get("error"); answer != "" {
		return Finished{}, fmt.Errorf("%w: %s: %s", ErrDenied, answer, query.Get("error_description"))
	}
	grant, err := provider.Exchange(ctx, client, query.Get("code"), pending.Get("verifier"))
	if err != nil {
		return Finished{}, fmt.Errorf("%w: %w", ErrUnverified, err)
	}
	if grant.Claims["nonce"] != pending.Get("nonce") {
		return Finished{}, fmt.Errorf("%w: the ID token does not carry the nonce sent", ErrUnverified)
	}
	claims, err := s.claimsOf(ctx, provider, current.OIDC.Claims, grant)
	if err != nil {
		return Finished{}, err
	}
	u, err := s.account(ctx, current, claims)
	if err != nil {
		return Finished{}, err
	}
	token, err := s.Auth.StartSession(ctx, u.ID, true)
	return Finished{Token: token, Next: pending.Get("next")}, err
}

// EndSession returns the address of the provider's page that ends its
// session and then sends the browser back to the sign-in page, or empty
// when the provider has none.
func (s Service) EndSession(ctx context.Context) (string, error) {
	current, client, provider, err := s.provider(ctx)
	if err != nil {
		return "", err
	}
	end := provider.EndSessionURL()
	if end == "" {
		return "", nil
	}
	u, err := url.Parse(end)
	if err != nil {
		return "", fmt.Errorf("%w: its end_session_endpoint: %w", ErrUnreachable, err)
	}
	q := u.Query()
	q.Set("client_id", client.ID)
	q.Set("post_logout_redirect_uri", current.PublicURL+SignedOutPath)
	u.RawQuery = q.Encode()
	return u.String(), nil
}

// provider returns the settings, the site as the provider's client
// without its secret, and the provider, once signing in through it is
// enabled and set up.
func (s Service) provider(ctx context.Context) (settings.Current, connector.IdentityClient, connector.IdentityProvider, error) {
	current, err := s.Settings.Current(ctx)
	if err != nil {
		return settings.Current{}, connector.IdentityClient{}, nil, err
	}
	o := current.OIDC
	if !o.Enabled {
		return settings.Current{}, connector.IdentityClient{}, nil, ErrDisabled
	}
	if o.Issuer == "" || o.ClientID == "" || current.PublicURL == "" {
		return settings.Current{}, connector.IdentityClient{}, nil, ErrIncomplete
	}
	provider, err := s.Providers.of(ctx, s.Identity, o.Issuer)
	client := connector.IdentityClient{ID: o.ClientID, Scopes: o.Scopes, RedirectURL: current.PublicURL + CallbackPath}
	return current, client, provider, err
}

func (s Service) random(n int) string {
	return randomEncoding.EncodeToString(s.Entropy.Bytes(n))
}

// claimsOf is the claims of grant's ID token, with those the provider's
// userinfo endpoint answers for any claim names reads that the ID token
// lacks.
func (s Service) claimsOf(ctx context.Context, provider connector.IdentityProvider, names settings.Claims, grant connector.IdentityGrant) (map[string]any, error) {
	if !lacks(grant.Claims, names) {
		return grant.Claims, nil
	}
	info, err := provider.UserInfo(ctx, grant.AccessToken)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrUnreachable, err)
	}
	if info["sub"] != grant.Claims["sub"] {
		return nil, fmt.Errorf("%w: the userinfo subject is not the ID token's", ErrUnverified)
	}
	merged := maps.Clone(info)
	maps.Copy(merged, grant.Claims)
	return merged, nil
}

// lacks reports whether claims lacks a claim names reads.
func lacks(claims map[string]any, names settings.Claims) bool {
	for _, name := range []string{names.UserID, names.Email, names.FirstName, names.LastName, names.DisplayName, names.Entitlements} {
		if _, held := claims[name]; name != "" && !held {
			return true
		}
	}
	return false
}

// account returns the user claims name, created or brought up to date,
// with the groups the sign-in decides. A user whose account has ended is
// refused.
func (s Service) account(ctx context.Context, current settings.Current, claims map[string]any) (connector.StoredUser, error) {
	names := current.OIDC.Claims
	email, _ := text(claims, names.Email)
	if email == "" {
		return connector.StoredUser{}, ErrNoEmail
	}
	if claims["email_verified"] == false {
		return connector.StoredUser{}, ErrEmailUnverified
	}
	userID, _ := text(claims, names.UserID)
	u, found, err := s.Directory.Match(ctx, userID, email)
	if err != nil {
		return connector.StoredUser{}, err
	}
	if found && auth.AccountEnded(u, s.Clock.Now()) {
		return connector.StoredUser{}, auth.ErrExpired
	}
	profile := profileOf(u, claims, names, email)
	if found {
		u, err = s.Directory.UpdateUser(ctx, u.ID, profile)
	} else {
		expires := s.Clock.Now().Add(current.NewUserExpiry)
		profile.ExpiresAt = &expires
		u, err = s.Directory.CreateUser(ctx, profile)
	}
	if err != nil {
		return connector.StoredUser{}, err
	}
	join, leave := memberships(current.OIDC, claims, !found)
	return u, s.Directory.Sync(ctx, u.ID, join, leave)
}

// profileOf is u's profile as claims, read by names, tell it: each field
// from its claim when claims holds it and as u has it otherwise, with the
// email given. Without a display name, the user is called by their names
// or else their email.
func profileOf(u connector.StoredUser, claims map[string]any, names settings.Claims, email string) directory.Profile {
	first := claimOr(claims, names.FirstName, u.FirstName)
	last := claimOr(claims, names.LastName, u.LastName)
	return directory.Profile{
		Email:       email,
		UserID:      claimOr(claims, names.UserID, valueOf(u.UserID)),
		FirstName:   first,
		LastName:    last,
		DisplayName: cmp.Or(claimOr(claims, names.DisplayName, u.DisplayName), strings.TrimSpace(first+" "+last), email),
		ExpiresAt:   u.ExpiresAt,
	}
}

// valueOf is the string p points to, empty when p is nil.
func valueOf(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}

// claimOr is the claim named name as text, or fallback when claims does
// not hold it.
func claimOr(claims map[string]any, name, fallback string) string {
	if value, held := text(claims, name); held {
		return value
	}
	return fallback
}

// text is the claim named name as text, and whether claims holds it as a
// string or a number.
func text(claims map[string]any, name string) (string, bool) {
	switch value := claims[name].(type) {
	case string:
		return strings.TrimSpace(value), true
	case float64:
		return strconv.FormatFloat(value, 'f', -1, 64), true
	}
	return "", false
}

// memberships is the slugs of the groups a sign-in with claims joins and
// leaves: a new user's default groups, and each entitlement group the
// entitlements claim holds; and each entitlement group it does not hold.
// Without an entitlements claim to read, entitlements decide nothing.
func memberships(o settings.OIDC, claims map[string]any, created bool) (join, leave []string) {
	if created {
		join = slices.Clone(o.DefaultGroups)
	}
	if o.Claims.Entitlements == "" {
		return join, nil
	}
	held := entitlements(claims[o.Claims.Entitlements])
	for _, slug := range o.EntitlementGroups {
		if slices.Contains(held, slug) {
			join = append(join, slug)
		} else {
			leave = append(leave, slug)
		}
	}
	return join, leave
}

// entitlements is the values of an entitlements claim: a list of strings
// or a single one.
func entitlements(claim any) []string {
	switch value := claim.(type) {
	case string:
		return []string{value}
	case []any:
		var held []string
		for _, v := range value {
			if s, ok := v.(string); ok {
				held = append(held, s)
			}
		}
		return held
	}
	return nil
}
