// Package oidcprovider implements connector.Identity with go-oidc and
// golang.org/x/oauth2.
package oidcprovider

import (
	"context"
	"net/http"

	"github.com/coreos/go-oidc/v3/oidc"
	"golang.org/x/oauth2"

	"github.com/dakriy/doorcode/internal/connector"
)

// Identity discovers providers over an HTTP client.
type Identity struct {
	client *http.Client
}

// New returns an Identity that reaches providers with client.
func New(client *http.Client) Identity {
	return Identity{client: client}
}

// Discover fetches the discovery document of the provider at issuer.
func (i Identity) Discover(ctx context.Context, issuer string) (connector.IdentityProvider, error) {
	p, err := oidc.NewProvider(oidc.ClientContext(ctx, i.client), issuer)
	if err != nil {
		return nil, err
	}
	var metadata struct {
		EndSession string `json:"end_session_endpoint"`
	}
	err = p.Claims(&metadata)
	return provider{client: i.client, oidc: p, endSession: metadata.EndSession}, err
}

type provider struct {
	client     *http.Client
	oidc       *oidc.Provider
	endSession string
}

func (p provider) config(c connector.IdentityClient) *oauth2.Config {
	return &oauth2.Config{
		ClientID:     c.ID,
		ClientSecret: c.Secret,
		Endpoint:     p.oidc.Endpoint(),
		RedirectURL:  c.RedirectURL,
		Scopes:       c.Scopes,
	}
}

// AuthURL builds the authorization request.
func (p provider) AuthURL(c connector.IdentityClient, state, nonce, verifier string) string {
	return p.config(c).AuthCodeURL(state, oidc.Nonce(nonce), oauth2.S256ChallengeOption(verifier))
}

// Exchange redeems the code and verifies the ID token it comes with.
func (p provider) Exchange(ctx context.Context, c connector.IdentityClient, code, verifier string) (connector.IdentityGrant, error) {
	ctx = oidc.ClientContext(ctx, p.client)
	token, err := p.config(c).Exchange(ctx, code, oauth2.VerifierOption(verifier))
	if err != nil {
		return connector.IdentityGrant{}, err
	}
	raw, _ := token.Extra("id_token").(string)
	idToken, err := p.oidc.Verifier(&oidc.Config{ClientID: c.ID}).Verify(ctx, raw)
	if err != nil {
		return connector.IdentityGrant{}, err
	}
	var claims map[string]any
	err = idToken.Claims(&claims)
	return connector.IdentityGrant{Claims: claims, AccessToken: token.AccessToken}, err
}

// UserInfo asks the userinfo endpoint about accessToken.
func (p provider) UserInfo(ctx context.Context, accessToken string) (map[string]any, error) {
	source := oauth2.StaticTokenSource(&oauth2.Token{AccessToken: accessToken})
	info, err := p.oidc.UserInfo(oidc.ClientContext(ctx, p.client), source)
	if err != nil {
		return nil, err
	}
	var claims map[string]any
	err = info.Claims(&claims)
	return claims, err
}

// EndSessionURL is the end_session_endpoint discovery named.
func (p provider) EndSessionURL() string {
	return p.endSession
}
