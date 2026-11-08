package app

import (
	"cmp"
	"context"
	"errors"
	"net/http"

	"github.com/dakriy/doorcode/internal/auth"
	"github.com/dakriy/doorcode/internal/check"
	"github.com/dakriy/doorcode/internal/connector"
	"github.com/dakriy/doorcode/internal/identity"
)

// providerCookie holds a sign-in at the identity provider while the
// browser is away at it, for providerCookieSeconds at most.
const (
	providerCookie        = "doorcode_oidc"
	providerCookieSeconds = 600
)

func (a *App) providerRoutes() []route {
	return []route{
		pageRoute(http.MethodGet, "/sign-in/oidc", anyone, a.signInAtProvider),
		pageRoute(http.MethodGet, identity.CallbackPath, anyone, a.backFromProvider),
	}
}

// signInAtProvider sends the browser on to the identity provider, to come
// back to the local path the query names.
func (a *App) signInAtProvider(w http.ResponseWriter, r *http.Request, _ auth.User) {
	a.startAtProvider(w, r, localPath(r.URL.Query().Get("next")))
}

// startAtProvider sends the browser on to the identity provider, to come
// back to next once signed in.
func (a *App) startAtProvider(w http.ResponseWriter, r *http.Request, next string) {
	begun, err := a.identity.Start(r.Context(), next)
	if err != nil {
		a.providerFailed(w, r, err)
		return
	}
	setCookie(w, r, providerCookie, begun.Cookie, providerCookieSeconds)
	redirect(w, begun.URL)
}

// backFromProvider finishes a sign-in with the provider's answer and
// starts the session.
func (a *App) backFromProvider(w http.ResponseWriter, r *http.Request, _ auth.User) {
	pending := ""
	if cookie, err := r.Cookie(providerCookie); err == nil {
		pending = cookie.Value
	}
	setCookie(w, r, providerCookie, "", -1)
	done, err := a.identity.Finish(r.Context(), pending, r.URL.Query())
	if err != nil {
		a.providerFailed(w, r, err)
		return
	}
	setCookie(w, r, sessionCookie, done.Token, int(auth.SessionLifetime().Seconds()))
	flash, next := "signed-in", done.Next
	if done.Created {
		flash = "welcome"
	}
	if done.Created && next == "/" {
		next = codePath
	}
	setCookie(w, r, flashCookie, flash, 0)
	redirect(w, next)
}

// providerRefusals is what the sign-in page says, and with which status,
// about each way a sign-in at the identity provider is refused.
var providerRefusals = []struct {
	err     error
	status  int
	message string
}{
	{identity.ErrDisabled, http.StatusNotFound, "Signing in with single sign-on is not enabled on this site."},
	{identity.ErrIncomplete, http.StatusServiceUnavailable, "Single sign-on is not set up completely yet. Ask an administrator to finish its settings."},
	{identity.ErrUnreachable, http.StatusBadGateway, "The identity provider could not be reached. Try again in a moment."},
	{identity.ErrStale, http.StatusBadRequest, "This sign-in took too long or was started in another window. Try again."},
	{identity.ErrDenied, http.StatusForbidden, "The identity provider did not sign you in."},
	{identity.ErrUnverified, http.StatusForbidden, "The identity provider's answer could not be verified. Try again."},
	{identity.ErrNoEmail, http.StatusForbidden, "The identity provider did not say what your email address is, so you cannot be signed in. Ask an administrator."},
	{identity.ErrEmailUnverified, http.StatusForbidden, "The identity provider has not verified your email address, so you cannot be signed in."},
	{auth.ErrExpired, http.StatusForbidden, expiredMessage},
}

// providerFailed logs why a sign-in at the identity provider failed and
// shows the sign-in page saying so, or the error page when nothing went
// wrong that the person could act on.
func (a *App) providerFailed(w http.ResponseWriter, r *http.Request, err error) {
	status, message := providerRefusal(err)
	if message == "" {
		a.pageError(w, err)
		return
	}
	a.log.Error("could not sign in at the identity provider", connector.Field{Key: "error", Value: err.Error()})
	a.renderSignIn(w, r, status, signInForm{Next: "/", Error: message})
}

// providerRefusal is the status and message of the refusal err is, or no
// message when it is none.
func providerRefusal(err error) (int, string) {
	for _, refusal := range providerRefusals {
		if errors.Is(err, refusal.err) {
			return refusal.status, refusal.message
		}
	}
	var problems check.Problems
	if errors.As(err, &problems) {
		return http.StatusForbidden, "Your account could not be set up from what the identity provider said about you: " + problems[0].Message
	}
	return 0, ""
}

// signedOut is where the browser goes once its session has ended: the
// identity provider's end-session page when the session began there and
// the provider has one, and otherwise the sign-in page.
func (a *App) signedOut(ctx context.Context, viaIdentity bool) string {
	if !viaIdentity {
		return identity.SignedOutPath
	}
	end, err := a.identity.EndSession(ctx)
	if err != nil {
		a.log.Error("could not end the session at the identity provider", connector.Field{Key: "error", Value: err.Error()})
	}
	return cmp.Or(end, identity.SignedOutPath)
}
