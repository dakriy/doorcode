package app

import (
	"errors"
	"net/http"
	"strings"

	"github.com/dakriy/doorcode/internal/auth"
	"github.com/dakriy/doorcode/internal/check"
	"github.com/dakriy/doorcode/internal/connector"
	"github.com/dakriy/doorcode/internal/settings"
)

const (
	sessionCookie = "doorcode_session"
	flashCookie   = "doorcode_flash"
)

// flashes are the one-time messages a redirect can carry. The cookie holds
// only the key, so nobody can make the site display text of their choosing.
var flashes = map[string]string{
	"signed-in":              "You are signed in.",
	"signed-out":             "You have been signed out.",
	"welcome":                "Welcome! Choose your door code to finish setting up your account.",
	"own-code-set":           "Your door code is set. Remember it: it cannot be shown again.",
	"own-password-set":       "Your password has been changed. Use it the next time you sign in.",
	"request-sent":           "Your request has been sent. An administrator will reply here.",
	"request-saved":          "Your request has been saved.",
	"request-deleted":        "Your request has been withdrawn.",
	"reply-sent":             "The reply has been saved. The user sees it on their request.",
	"reply-sent-resolved":    "The reply has been saved and the request resolved. The user sees both on their request.",
	"request-resolved":       "The request has been resolved. Its user can no longer change or withdraw it.",
	"request-reopened":       "The request has been reopened. Its user may change or withdraw it again.",
	"token-revoked":          "The token has been revoked. Anything still using it is refused from now on.",
	"token-gone":             "That token had already been revoked.",
	"user-created":           "The user has been created.",
	"user-saved":             "The user's changes have been saved.",
	"user-deleted":           "The user has been deleted. They are refused everywhere until restored.",
	"user-restored":          "The user has been restored.",
	"code-set":               "The door code has been set. It is stored only as a hash and cannot be shown.",
	"code-cleared":           "The door code has been removed.",
	"group-created":          "The group has been created.",
	"group-saved":            "The group's changes have been saved.",
	"group-deleted":          "The group has been deleted.",
	"member-added":           "The member has been added.",
	"member-removed":         "The member has been removed.",
	"schedule-created":       "The schedule has been created.",
	"schedule-saved":         "The schedule's changes have been saved.",
	"schedule-deleted":       "The schedule has been deleted.",
	"door-created":           "The door has been created. Its controller can enroll once it is connected.",
	"door-saved":             "The door's changes have been saved.",
	"firmware-uploaded":      "The firmware has been uploaded. Doors run it once it is the default or their own.",
	"firmware-default-saved": "The default firmware has been saved. The rollout sends it to the doors that follow the default in the next update window.",
	"rollout-settings-saved": "The update window and rollout settings have been saved.",
	"firmware-deleted":       "The firmware has been deleted.",
	"door-firmware-saved":    "The door's firmware has been saved. The rollout sends it in the next update window.",
	"update-sent":            "The update has been sent. The door installs it, restarts, and connects again with the new version.",
	"door-config-saved":      "The door's configuration has been saved and sent to its controller.",
	"door-config-defaults":   "The door follows the site defaults again, and its configuration has been sent to its controller.",
	"settings-saved":         "The settings have been saved. Doors receive changed configuration defaults right away.",
	"sign-in-image-saved":    "The sign-in page shows the new picture.",
	"sign-in-image-removed":  "The sign-in page shows no picture.",
	"logo-saved":             "The site shows the new logo.",
	"logo-removed":           "The site shows the keypad mark in place of a logo.",
	"controller-approved":    "The controller has been approved. Its credentials are on their way, and it connects as this door once it has them.",
	"credentials-rotated":    "New credentials are on their way to the controller. The old ones work until it connects with the new ones.",
	"credentials-revoked":    "The controller's credentials have been revoked and it has been disconnected. It connects again only by enrolling and being approved.",
	"door-zones-saved":       "The door's zones have been saved.",
	"door-deleted":           "The door has been deleted. Its history stays, and you can restore it.",
	"door-restored":          "The door has been restored.",
	"picture-added":          "The picture has been added.",
	"picture-primary":        "The primary picture has been changed.",
	"picture-deleted":        "The picture has been deleted.",
	"zone-created":           "The zone has been created.",
	"zone-saved":             "The zone's changes have been saved.",
	"zone-deleted":           "The zone has been deleted. Its doors stay.",
	"rule-created":           "The access rule has been created.",
	"rule-saved":             "The access rule's changes have been saved.",
	"rule-deleted":           "The access rule has been deleted. Its members no longer get in by it.",
	"override-created":       "The override has been created.",
	"override-saved":         "The override's changes have been saved.",
	"override-cancelled":     "The override has been cancelled. It ends now.",
	"door-opened":            "The door has been told to release its strike. The opening is logged under your name.",
	"door-rebooted":          "The controller has been told to restart. It goes offline for a moment and connects again. The reboot is logged under your name.",
	"override-deleted":       "The override has been deleted.",
}

// signInForm is the sign-in page's content. Bypass says the person asked
// for the password form, which auto sign-in otherwise skips; Provider and
// Password say whether the page offers signing in at the identity
// provider and with a password.
type signInForm struct {
	Email         string
	Next          string
	Error         string
	EmailError    string
	PasswordError string
	Bypass        bool
	Provider      bool
	Password      bool
	// Image is where the picture the page shows is served, empty when it
	// shows none.
	Image string
}

// signInPage shows the sign-in page, or sends the browser on to the
// identity provider under auto sign-in unless the query says
// auto_sign_in=false.
func (a *App) signInPage(w http.ResponseWriter, r *http.Request, _ auth.User) {
	query := r.URL.Query()
	form := signInForm{Next: localPath(query.Get("next")), Bypass: query.Get("auto_sign_in") == "false"}
	current, err := a.settings.Current(r.Context())
	if err != nil {
		a.pageError(w, r, err)
		return
	}
	if current.OIDC.HidesPasswordForms() && !form.Bypass {
		a.startAtProvider(w, r, form.Next)
		return
	}
	a.showSignIn(w, r, http.StatusOK, current.OIDC, form)
}

func (a *App) signIn(w http.ResponseWriter, r *http.Request, _ auth.User) {
	email := strings.TrimSpace(r.PostFormValue("email"))
	password := r.PostFormValue("password")
	form := signInForm{
		Email:         email,
		Next:          localPath(r.PostFormValue("next")),
		Bypass:        true,
		EmailError:    check.Required(email, "Enter your email address."),
		PasswordError: check.Required(password, "Enter your password."),
	}
	if form.EmailError != "" || form.PasswordError != "" {
		a.renderSignIn(w, r, http.StatusUnprocessableEntity, form)
		return
	}

	token, err := a.auth.SignIn(r.Context(), email, password)
	form.Error = signInRefusal(err)
	if form.Error != "" {
		a.log.Info("refused a sign-in", connector.Field{Key: "email", Value: email},
			connector.Field{Key: "error", Value: err.Error()}, connector.Field{Key: "client", Value: a.client(r)})
		a.renderSignIn(w, r, http.StatusUnprocessableEntity, form)
		return
	}
	if err != nil {
		a.pageError(w, r, err)
		return
	}
	a.log.Info("signed in", connector.Field{Key: "email", Value: email}, connector.Field{Key: "client", Value: a.client(r)})
	a.setCookie(w, r, sessionCookie, token, int(auth.SessionLifetime().Seconds()))
	a.setCookie(w, r, flashCookie, "signed-in", 0)
	redirect(w, form.Next)
}

func (a *App) renderSignIn(w http.ResponseWriter, r *http.Request, status int, form signInForm) {
	current, err := a.settings.Current(r.Context())
	if err != nil {
		a.pageError(w, r, err)
		return
	}
	a.showSignIn(w, r, status, current.OIDC, form)
}

// showSignIn shows the sign-in page with form, offering what o allows:
// the identity provider when it is enabled, and the password form unless
// it is hidden and the person did not ask for it.
func (a *App) showSignIn(w http.ResponseWriter, r *http.Request, status int, o settings.OIDC, form signInForm) {
	form.Provider = o.Enabled
	form.Password = !o.HidesPasswordForms() || form.Bypass
	image, err := a.signInImageURL(r)
	if err != nil {
		a.pageError(w, r, err)
		return
	}
	form.Image = image
	a.render(w, r, status, signInTemplate, "Sign in", nil, form)
}

// expiredMessage is what the sign-in page says to someone whose account has
// ended.
const expiredMessage = "This account has expired. Ask an administrator to renew it."

// signInRefusal is what the sign-in page says about err, or nothing when err
// is not a refusal.
func signInRefusal(err error) string {
	if errors.Is(err, auth.ErrBadCredentials) {
		return "That email and password do not match an account."
	}
	if errors.Is(err, auth.ErrExpired) {
		return expiredMessage
	}
	return ""
}

// signOut ends the session and goes to the sign-in page, by way of the
// identity provider's end-session page when the session began there.
func (a *App) signOut(w http.ResponseWriter, r *http.Request, _ auth.User) {
	viaIdentity := false
	if cookie, err := r.Cookie(sessionCookie); err == nil {
		viaIdentity, err = a.auth.SignOut(r.Context(), cookie.Value)
		if err != nil {
			a.pageError(w, r, err)
			return
		}
	}
	a.setCookie(w, r, sessionCookie, "", -1)
	a.setCookie(w, r, flashCookie, "signed-out", 0)
	redirect(w, a.signedOut(r.Context(), viaIdentity))
}

// takeFlash returns the flash message r carries, if any, and clears it.
func (a *App) takeFlash(w http.ResponseWriter, r *http.Request) string {
	cookie, err := r.Cookie(flashCookie)
	if err != nil {
		return ""
	}
	a.setCookie(w, r, flashCookie, "", -1)
	return flashes[cookie.Value]
}

// setCookie sets a cookie scripts cannot read and other sites do not get.
// maxAge is in seconds: zero lasts until the browser closes, negative
// deletes. The cookie is marked Secure when the browser reached the site
// over HTTPS, directly or through a trusted proxy that says so.
func (a *App) setCookie(w http.ResponseWriter, r *http.Request, name, value string, maxAge int) {
	http.SetCookie(w, &http.Cookie{
		Name:     name,
		Value:    value,
		Path:     "/",
		MaxAge:   maxAge,
		HttpOnly: true,
		Secure:   r.TLS != nil || a.proxies.HTTPS(a.trustedProxies(r), r.RemoteAddr, r.Header.Get("X-Forwarded-Proto")),
		SameSite: http.SameSiteLaxMode,
	})
}

// localPath returns next when it is a path on this site, so the sign-in
// page cannot be used to send people elsewhere, and the home page otherwise.
func localPath(next string) string {
	if strings.HasPrefix(next, "/") && !strings.HasPrefix(next, "//") && !strings.HasPrefix(next, "/\\") {
		return next
	}
	return "/"
}
