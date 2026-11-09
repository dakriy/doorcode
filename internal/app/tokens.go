package app

import (
	"cmp"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/dakriy/doorcode/internal/auth"
	"github.com/dakriy/doorcode/internal/openapi"
)

const (
	maxTokenNameLength = 100
	minTokenDays       = 1
	maxTokenDays       = 3650
	// defaultTokenExpiry is the expiry the page offers first, in days.
	defaultTokenExpiry = "30"
)

type tokenView struct {
	ID        int64      `json:"id" doc:"Names the token, to revoke it."`
	Name      string     `json:"name" doc:"What the token is for."`
	CreatedAt time.Time  `json:"created_at" doc:"When the token was created."`
	ExpiresAt *time.Time `json:"expires_at,omitempty" doc:"When the token stops working; absent when it never expires."`
	Expired   bool       `json:"expired" doc:"True once the token has expired."`
}

type tokenList struct {
	Tokens []tokenView `json:"tokens" doc:"The caller's tokens, oldest first."`
}

type tokenRequest struct {
	Name          string `json:"name" doc:"What the token is for, up to 100 characters."`
	ExpiresInDays *int   `json:"expires_in_days,omitempty" doc:"Days until the token expires, from 1 to 3650. Omit it for a token that never expires."`
}

type createdToken struct {
	Token  tokenView `json:"token" doc:"The new token."`
	Secret string    `json:"secret" doc:"The token to send. This is the only time it is shown; it cannot be recovered."`
}

// tokenID is the path parameter naming a token.
var tokenID = openapi.Param{Name: "id", In: "path", Description: "The token's id.", Example: int64(1)}

func (a *App) tokenRoutes() []route {
	days := 90
	return []route{
		pageRoute(http.MethodGet, "/account/tokens", signedInUser, a.tokensPage),
		pageRoute(http.MethodPost, "/account/tokens", signedInUser, a.createTokenFromForm),
		pageRoute(http.MethodPost, "/account/tokens/{id}/revoke", signedInUser, a.revokeTokenFromForm),
		apiRoute(http.MethodGet, "/tokens", signedInUser, a.listTokens, apiDoc{
			id: "listTokens", summary: "List your personal API tokens", tag: "tokens",
			responses: []openapi.Response{{Status: http.StatusOK, Description: "The caller's tokens, without their secrets.", Body: tokenList{}}},
		}),
		apiRoute(http.MethodPost, "/tokens", signedInUser, a.createToken, apiDoc{
			id: "createToken", summary: "Create a personal API token", tag: "tokens",
			request: tokenRequest{Name: "deploy script", ExpiresInDays: &days},
			responses: []openapi.Response{
				{Status: http.StatusCreated, Description: "The token and its secret, shown this once.", Body: createdToken{}},
				{Status: http.StatusBadRequest, Description: "The body is not a JSON token request, or the name or expiry is out of range.", Body: errorBody{}},
			},
		}),
		apiRoute(http.MethodDelete, "/tokens/{id}", signedInUser, a.revokeToken, apiDoc{
			id: "revokeToken", summary: "Revoke one of your personal API tokens", tag: "tokens",
			params: []openapi.Param{tokenID},
			responses: []openapi.Response{
				{Status: http.StatusNoContent, Description: "The token is revoked and refused from now on."},
				{Status: http.StatusNotFound, Description: "The caller has no token with this id.", Body: errorBody{}},
			},
		}),
	}
}

// tokenProblems says what is wrong with a token request, for the name and
// for the expiry; each is empty when that part is fine. name is trimmed.
func tokenProblems(name string, days *int) (nameProblem, daysProblem string) {
	if name == "" {
		nameProblem = "Enter a name for the token."
	}
	if utf8.RuneCountInString(name) > maxTokenNameLength {
		nameProblem = fmt.Sprintf("Use at most %d characters for the name.", maxTokenNameLength)
	}
	if days != nil && (*days < minTokenDays || *days > maxTokenDays) {
		daysProblem = fmt.Sprintf("Choose an expiry from %d to %d days.", minTokenDays, maxTokenDays)
	}
	return nameProblem, daysProblem
}

func viewOfToken(t auth.Token) tokenView {
	view := tokenView{ID: t.ID, Name: t.Name, CreatedAt: t.CreatedAt.UTC(), Expired: t.Expired}
	if t.ExpiresAt != nil {
		expires := t.ExpiresAt.UTC()
		view.ExpiresAt = &expires
	}
	return view
}

func (a *App) listTokens(w http.ResponseWriter, r *http.Request, caller auth.User) {
	a.writeTokens(w, r, caller.ID)
}

// writeTokens answers with the tokens of the user whose id is ownerID.
func (a *App) writeTokens(w http.ResponseWriter, r *http.Request, ownerID int64) {
	tokens, err := a.auth.Tokens(r.Context(), ownerID)
	if err != nil {
		a.internalError(w, r, err)
		return
	}
	views := make([]tokenView, 0, len(tokens))
	for _, t := range tokens {
		views = append(views, viewOfToken(t))
	}
	writeJSON(w, http.StatusOK, tokenList{Tokens: views})
}

func (a *App) createToken(w http.ResponseWriter, r *http.Request, caller auth.User) {
	a.issueToken(w, r, caller.ID)
}

// issueToken creates the token the request asks for, for the user whose id
// is ownerID.
func (a *App) issueToken(w http.ResponseWriter, r *http.Request, ownerID int64) {
	var body tokenRequest
	if !readJSON(w, r, &body, `a JSON object with a string "name"`) {
		return
	}
	name := strings.TrimSpace(body.Name)
	nameProblem, daysProblem := tokenProblems(name, body.ExpiresInDays)
	if problem := cmp.Or(nameProblem, daysProblem); problem != "" {
		writeError(w, http.StatusBadRequest, problem)
		return
	}
	token, secret, err := a.auth.CreateToken(r.Context(), ownerID, name, body.ExpiresInDays)
	if err != nil {
		a.internalError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, createdToken{Token: viewOfToken(token), Secret: secret})
}

func (a *App) revokeToken(w http.ResponseWriter, r *http.Request, caller auth.User) {
	a.withdrawToken(w, r, caller.ID, "id", "you have no token with this id")
}

// withdrawToken revokes the token the path parameter param names of the
// user whose id is ownerID, answering with missing when they have none.
func (a *App) withdrawToken(w http.ResponseWriter, r *http.Request, ownerID int64, param, missing string) {
	revoked, err := a.revoke(r, ownerID, param)
	if err != nil {
		a.internalError(w, r, err)
		return
	}
	if !revoked {
		writeError(w, http.StatusNotFound, missing)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// revoke revokes the token of the user whose id is ownerID that the path
// parameter param names, and reports whether there was one.
func (a *App) revoke(r *http.Request, ownerID int64, param string) (bool, error) {
	return a.auth.RevokeToken(r.Context(), ownerID, pathID(r, param))
}

// tokensContent is the API tokens page's content.
type tokensContent struct {
	Tokens  []auth.Token
	Form    tokenForm
	Choices []expiryChoice
	// MaxNameLength is the longest name the form accepts.
	MaxNameLength int
	// Created is the token just created, whose secret the page shows once.
	Created *createdSecret
}

type tokenForm struct {
	Name        string
	Expiry      string
	NameError   string
	ExpiryError string
}

type createdSecret struct {
	Name   string
	Secret string
}

// expiryChoice is one option of the page's expiry menu; an empty Days never
// expires.
type expiryChoice struct {
	Days  string
	Label string
}

var expiryChoices = []expiryChoice{
	{Days: "7", Label: "7 days"},
	{Days: defaultTokenExpiry, Label: "30 days"},
	{Days: "90", Label: "90 days"},
	{Days: "365", Label: "1 year"},
	{Days: "", Label: "Never"},
}

func (a *App) tokensPage(w http.ResponseWriter, r *http.Request, viewer auth.User) {
	a.renderTokens(w, r, http.StatusOK, viewer, tokenForm{Expiry: defaultTokenExpiry}, nil)
}

// readTokenForm reads the token form, and reports whether it is fine; when
// it is not, the form says why.
func readTokenForm(r *http.Request) (tokenForm, *int, bool) {
	form := tokenForm{Name: strings.TrimSpace(r.PostFormValue("name")), Expiry: r.PostFormValue("expires_in_days")}
	days, err := formDays(form.Expiry)
	form.NameError, form.ExpiryError = tokenProblems(form.Name, days)
	if err != nil {
		form.ExpiryError = "Choose when the token expires."
	}
	return form, days, form.NameError == "" && form.ExpiryError == ""
}

func (a *App) createTokenFromForm(w http.ResponseWriter, r *http.Request, viewer auth.User) {
	form, days, ok := readTokenForm(r)
	if !ok {
		a.renderTokens(w, r, http.StatusUnprocessableEntity, viewer, form, nil)
		return
	}
	token, secret, err := a.auth.CreateToken(r.Context(), viewer.ID, form.Name, days)
	if err != nil {
		a.pageError(w, r, err)
		return
	}
	created := &createdSecret{Name: token.Name, Secret: secret}
	a.renderTokens(w, r, http.StatusCreated, viewer, tokenForm{Expiry: defaultTokenExpiry}, created)
}

// formDays reads the expiry menu's value: a number of days, or empty for
// never.
func formDays(value string) (*int, error) {
	if value == "" {
		return nil, nil
	}
	days, err := strconv.Atoi(value)
	return &days, err
}

func (a *App) revokeTokenFromForm(w http.ResponseWriter, r *http.Request, viewer auth.User) {
	a.revokeFromForm(w, r, viewer.ID, "id", "/account/tokens")
}

// revokeFromForm revokes the token the path parameter param names of the
// user whose id is ownerID, and returns the browser to back saying what
// happened.
func (a *App) revokeFromForm(w http.ResponseWriter, r *http.Request, ownerID int64, param, back string) {
	revoked, err := a.revoke(r, ownerID, param)
	if err != nil {
		a.pageError(w, r, err)
		return
	}
	flash := "token-gone"
	if revoked {
		flash = "token-revoked"
	}
	a.done(w, r, back, flash)
}

func (a *App) renderTokens(w http.ResponseWriter, r *http.Request, status int, viewer auth.User, form tokenForm, created *createdSecret) {
	tokens, err := a.auth.Tokens(r.Context(), viewer.ID)
	if err != nil {
		a.pageError(w, r, err)
		return
	}
	content := tokensContent{Tokens: tokens, Form: form, Choices: expiryChoices, MaxNameLength: maxTokenNameLength, Created: created}
	a.render(w, r, status, tokensTemplate, "API tokens", &viewer, content)
}
