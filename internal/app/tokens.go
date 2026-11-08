package app

import (
	"cmp"
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
		nameProblem = "Use at most 100 characters for the name."
	}
	if days != nil && (*days < 1 || *days > maxTokenDays) {
		daysProblem = "Choose an expiry from 1 to 3650 days."
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
	tokens, err := a.auth.Tokens(r.Context(), caller)
	if err != nil {
		a.internalError(w, err)
		return
	}
	views := make([]tokenView, 0, len(tokens))
	for _, t := range tokens {
		views = append(views, viewOfToken(t))
	}
	writeJSON(w, http.StatusOK, tokenList{Tokens: views})
}

func (a *App) createToken(w http.ResponseWriter, r *http.Request, caller auth.User) {
	var body tokenRequest
	if err := decodeJSON(w, r, &body); err != nil {
		writeError(w, http.StatusBadRequest, `body must be a JSON object with a string "name"`)
		return
	}
	name := strings.TrimSpace(body.Name)
	if problem := cmp.Or(tokenProblems(name, body.ExpiresInDays)); problem != "" {
		writeError(w, http.StatusBadRequest, problem)
		return
	}
	token, secret, err := a.auth.CreateToken(r.Context(), caller, name, body.ExpiresInDays)
	if err != nil {
		a.internalError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, createdToken{Token: viewOfToken(token), Secret: secret})
}

func (a *App) revokeToken(w http.ResponseWriter, r *http.Request, caller auth.User) {
	revoked, err := a.revoke(r, caller)
	if err != nil {
		a.internalError(w, err)
		return
	}
	if !revoked {
		writeError(w, http.StatusNotFound, "you have no token with this id")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// revoke revokes the caller's token the path names, and reports whether
// there was one.
func (a *App) revoke(r *http.Request, caller auth.User) (bool, error) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		return false, nil
	}
	return a.auth.RevokeToken(r.Context(), caller, id)
}

// tokensContent is the API tokens page's content.
type tokensContent struct {
	Tokens  []auth.Token
	Form    tokenForm
	Choices []expiryChoice
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

func (a *App) createTokenFromForm(w http.ResponseWriter, r *http.Request, viewer auth.User) {
	form := tokenForm{Name: strings.TrimSpace(r.PostFormValue("name")), Expiry: r.PostFormValue("expires_in_days")}
	days, err := formDays(form.Expiry)
	form.NameError, form.ExpiryError = tokenProblems(form.Name, days)
	if err != nil {
		form.ExpiryError = "Choose when the token expires."
	}
	if form.NameError != "" || form.ExpiryError != "" {
		a.renderTokens(w, r, http.StatusUnprocessableEntity, viewer, form, nil)
		return
	}
	token, secret, err := a.auth.CreateToken(r.Context(), viewer, form.Name, days)
	if err != nil {
		a.pageError(w, err)
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
	revoked, err := a.revoke(r, viewer)
	if err != nil {
		a.pageError(w, err)
		return
	}
	flash := "token-gone"
	if revoked {
		flash = "token-revoked"
	}
	setCookie(w, r, flashCookie, flash, 0)
	redirect(w, "/account/tokens")
}

func (a *App) renderTokens(w http.ResponseWriter, r *http.Request, status int, viewer auth.User, form tokenForm, created *createdSecret) {
	tokens, err := a.auth.Tokens(r.Context(), viewer)
	if err != nil {
		a.pageError(w, err)
		return
	}
	content := tokensContent{Tokens: tokens, Form: form, Choices: expiryChoices, Created: created}
	a.render(w, r, status, tokensTemplate, "API tokens", &viewer, content)
}
