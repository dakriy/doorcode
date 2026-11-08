package app

import (
	"net/http"
	"slices"

	"github.com/dakriy/doorcode/internal/auth"
	"github.com/dakriy/doorcode/internal/openapi"
)

// apiPrefix starts the path of every JSON API operation.
const apiPrefix = "/api/v1"

// route is one entry of the route table: a method and path, who may use it,
// and what serves it. An API operation also carries how the OpenAPI document
// describes it; its path is relative to apiPrefix.
type route struct {
	method string
	path   string
	access access
	handle viewerHandler
	doc    *apiDoc
}

// apiDoc is what the OpenAPI document says about an operation beyond its
// method, path, and access.
type apiDoc struct {
	id      string
	summary string
	tag     string
	params  []openapi.Param
	// request is an example JSON body; nil when the operation takes none.
	request any
	// form is a value of the type of a multipart form body; nil when the
	// operation takes none.
	form      any
	responses []openapi.Response
}

func pageRoute(method, path string, need access, handle viewerHandler) route {
	return route{method: method, path: path, access: need, handle: handle}
}

func apiRoute(method, path string, need access, handle viewerHandler, doc apiDoc) route {
	return route{method: method, path: path, access: need, handle: handle, doc: &doc}
}

// routes is the route table: every page and every API operation the
// application serves. The mux and the OpenAPI document are both built from
// it, so nothing is served that the document does not describe. It reads
// nothing from a until a request arrives, so the document can be built from
// a zero App.
func (a *App) routes() []route {
	table := []route{
		pageRoute(http.MethodGet, "/healthz", anyone, healthz),
		pageRoute(http.MethodGet, "/sign-in", anyone, a.signInPage),
		pageRoute(http.MethodPost, "/sign-in", anyone, a.signIn),
		pageRoute(http.MethodPost, "/sign-out", anyone, a.signOut),
		pageRoute(http.MethodGet, "/{$}", signedInUser, a.home),
		pageRoute(http.MethodGet, "/api-docs", signedInUser, a.apiDocsPage),
		apiRoute(http.MethodGet, "/openapi.json", anyone, a.serveOpenAPI, apiDoc{
			id: "getOpenAPI", summary: "This document", tag: "documentation",
			responses: []openapi.Response{{Status: http.StatusOK, Description: "The OpenAPI document of this API."}},
		}),
	}
	table = append(table, assetRoutes()...)
	table = append(table, a.settingsRoutes()...)
	table = append(table, a.tokenRoutes()...)
	table = append(table, a.userRoutes()...)
	table = append(table, a.userPageRoutes()...)
	table = append(table, a.groupRoutes()...)
	table = append(table, a.groupPageRoutes()...)
	table = append(table, a.scheduleRoutes()...)
	table = append(table, a.schedulePageRoutes()...)
	table = append(table, a.doorRoutes()...)
	table = append(table, a.pictureRoutes()...)
	table = append(table, a.doorPageRoutes()...)
	table = append(table, a.zoneRoutes()...)
	return append(table, a.zonePageRoutes()...)
}

// handler registers every route of table on a new mux.
func (a *App) handler(table []route) http.Handler {
	mux := http.NewServeMux()
	for _, rt := range table {
		mux.HandleFunc(rt.method+" "+rt.pattern(), a.guard(rt))
	}
	return http.NewCrossOriginProtection().Handler(mux)
}

// isAPI reports whether rt is a JSON API operation, which lives under
// apiPrefix and is described by the OpenAPI document.
func (rt route) isAPI() bool {
	return rt.doc != nil
}

func (rt route) pattern() string {
	if rt.isAPI() {
		return apiPrefix + rt.path
	}
	return rt.path
}

// guard wraps the route's handler in the access check for its kind. A
// public route has none.
func (a *App) guard(rt route) http.HandlerFunc {
	if rt.access.public {
		return unguarded(rt.handle)
	}
	if rt.isAPI() {
		return a.api(rt.access, rt.handle)
	}
	return a.page(rt.access, rt.handle)
}

// apiInfo heads the OpenAPI document.
var apiInfo = openapi.Info{
	Title:       "doorcode API",
	Version:     "1",
	Description: `Every operation except this document needs a personal API token, created on the API tokens page or with createToken, or by an administrator for another user with createUserToken. A token acts as the user it belongs to, with that user's roles. Send it as a bearer token or as the password of HTTP Basic authentication.`,
	Server:      apiPrefix,
}

// apiDocument is the OpenAPI document of the API operations in table. Each
// operation that needs a signed-in caller also lists the refusals the access
// check answers with; an operation that documents one of those statuses
// itself describes it instead.
func apiDocument(table []route) []byte {
	var ops []openapi.Operation
	for _, rt := range table {
		if !rt.isAPI() {
			continue
		}
		ops = append(ops, openapi.Operation{
			Method:    rt.method,
			Path:      rt.path,
			ID:        rt.doc.id,
			Summary:   rt.doc.summary,
			Tag:       rt.doc.tag,
			Secured:   !rt.access.public,
			Params:    rt.doc.params,
			Request:   rt.doc.request,
			Form:      rt.doc.form,
			Responses: slices.Concat(refusals(rt.access), rt.doc.responses),
		})
	}
	return openapi.Document(apiInfo, ops)
}

// refusals are the responses the access check may answer with instead of
// the operation.
func refusals(need access) []openapi.Response {
	if need.public {
		return nil
	}
	out := []openapi.Response{
		{Status: http.StatusUnauthorized, Description: "No valid API token was sent.", Body: errorBody{}},
		{Status: http.StatusInternalServerError, Description: "The server failed; the body names a request id to give an administrator.", Body: errorBody{}},
	}
	if need.role != "" {
		out = append(out, openapi.Response{Status: http.StatusForbidden, Description: "The caller does not hold the " + need.role + " role.", Body: errorBody{}})
	}
	return out
}

// OpenAPI returns the OpenAPI document of the JSON API, as the application
// serves it at /api/v1/openapi.json.
func OpenAPI() []byte {
	return apiDocument((&App{}).routes())
}

func (a *App) serveOpenAPI(w http.ResponseWriter, _ *http.Request, _ auth.User) {
	w.Header().Set("Content-Type", jsonType)
	_, _ = w.Write(a.openapi)
}
