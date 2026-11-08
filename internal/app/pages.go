package app

import (
	"bytes"
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"html/template"
	"net/http"
	"slices"
	"strings"

	"github.com/dakriy/doorcode/internal/auth"
	"github.com/dakriy/doorcode/internal/connector"
)

//go:embed ui/templates/*.html
var templateFiles embed.FS

// appCSS is built from ui/tailwind.css by `make ui`; htmx, Swagger UI, and
// FullCalendar are vendored by the same target. apiDocsJS, calendarJS, and
// liveJS are ours.
var (
	//go:embed ui/static/app.css
	appCSS []byte
	//go:embed ui/static/htmx.min.js
	htmxJS []byte
	//go:embed ui/static/swagger-ui/swagger-ui.css
	swaggerCSS []byte
	//go:embed ui/static/swagger-ui/swagger-ui-bundle.js
	swaggerJS []byte
	//go:embed ui/static/api-docs.js
	apiDocsJS []byte
	//go:embed ui/static/fullcalendar/index.global.min.js
	fullCalendarJS []byte
	//go:embed ui/static/calendar.js
	calendarJS []byte
	//go:embed ui/static/live.js
	liveJS []byte
)

const (
	cssType = "text/css; charset=utf-8"
	jsType  = "text/javascript; charset=utf-8"
)

// asset is a static file a page loads. Templates refer to it by name.
type asset struct {
	name        string
	path        string
	contentType string
	content     []byte
}

var assets = []asset{
	{name: "CSS", path: "/static/app.css", contentType: cssType, content: appCSS},
	{name: "HTMX", path: "/static/htmx.min.js", contentType: jsType, content: htmxJS},
	{name: "SwaggerCSS", path: "/static/swagger-ui/swagger-ui.css", contentType: cssType, content: swaggerCSS},
	{name: "SwaggerJS", path: "/static/swagger-ui/swagger-ui-bundle.js", contentType: jsType, content: swaggerJS},
	{name: "APIDocsJS", path: "/static/api-docs.js", contentType: jsType, content: apiDocsJS},
	{name: "FullCalendarJS", path: "/static/fullcalendar/index.global.min.js", contentType: jsType, content: fullCalendarJS},
	{name: "CalendarJS", path: "/static/calendar.js", contentType: jsType, content: calendarJS},
	{name: "LiveJS", path: "/static/live.js", contentType: jsType, content: liveJS},
}

// assetURLs maps each asset's name to a URL carrying a hash of its content,
// so browsers may cache it forever.
var assetURLs = func() map[string]string {
	urls := map[string]string{}
	for _, f := range assets {
		urls[f.name] = versioned(f.path, f.content)
	}
	return urls
}()

// assetRoutes serve every asset.
func assetRoutes() []route {
	var table []route
	for _, f := range assets {
		table = append(table, pageRoute(http.MethodGet, f.path, anyone, serveAsset(f.content, f.contentType)))
	}
	return table
}

// The pages. A page in the app shell has navigation and the viewer's menu; a
// bare page has neither.
var (
	signInTemplate    = barePage("sign-in.html")
	errorTemplate     = barePage("error.html")
	homeTemplate      = shellPage("home.html")
	settingsTemplate  = shellPage("settings.html")
	forbiddenTemplate = shellPage("forbidden.html")
	tokensTemplate    = shellPage("tokens.html")
	apiDocsTemplate   = shellPage("api-docs.html")
	notFoundTemplate  = shellPage("not-found.html")
	usersTemplate     = shellPage("users.html")
	newUserTemplate   = shellPage("user-new.html")
	userTemplate      = shellPage("user.html")
	groupsTemplate    = shellPage("groups.html")
	newGroupTemplate  = shellPage("group-new.html")
	groupTemplate     = shellPage("group.html")

	schedulesTemplate        = shellPage("schedules.html")
	newScheduleTemplate      = shellPage("schedule-new.html")
	scheduleTemplate         = shellPage("schedule.html")
	scheduleCalendarTemplate = shellPage("schedule-calendar.html")

	doorsTemplate   = shellPage("doors.html")
	newDoorTemplate = shellPage("door-new.html")
	doorTemplate    = shellPage("door.html")
	zonesTemplate   = shellPage("zones.html")
	newZoneTemplate = shellPage("zone-new.html")
	zoneTemplate    = shellPage("zone.html")

	ruleTemplate           = shellPage("rule.html")
	overrideTemplate       = shellPage("override.html")
	doorCalendarTemplate   = shellPage("door-calendar.html")
	accessCalendarTemplate = shellPage("access-calendar.html")

	enrollmentsTemplate = shellPage("enrollments.html")
	approvalTemplate    = shellPage("approval.html")
)

// navItem is one link in the main navigation. A Reload link loads its page
// in full rather than through htmx, for a page whose scripts must run in
// order.
type navItem struct {
	Href    string
	Label   string
	Current bool
	Reload  bool
}

var templateFuncs = template.FuncMap{
	"navlink": func(href, label, path string) navItem {
		return navItem{Href: href, Label: label, Current: within(path, href)}
	},
	"reloadlink": func(href, label, path string) navItem {
		return navItem{Href: href, Label: label, Current: within(path, href), Reload: true}
	},
	"field": func(name, label, kind, value string, errors map[string]string, help string) fieldView {
		return fieldView{Name: name, Label: label, Type: kind, Value: value, Error: errors[name], Help: help}
	},
	"has":      slices.Contains[[]string],
	"weekdays": func() []dayChoice { return dayChoices },
	"link": func(href, label string) navItem {
		return navItem{Href: href, Label: label}
	},
	"choicelist": func(name, legend string, choices []choice, problem, empty string) choiceList {
		return choiceList{Name: name, Legend: legend, Choices: choices, Error: problem, Empty: empty}
	},
	"list": func(items ...string) []string { return items },
	"thumb": func(row doorRow, size string) thumbnailView {
		return thumbnailView{Row: row, Size: size}
	},
}

// choiceList is a list of checkboxes sharing the form field Name, with
// what is wrong with them and what to say when there is nothing to choose.
type choiceList struct {
	Name    string
	Legend  string
	Choices []choice
	Error   string
	Empty   string
}

// thumbnailView is a door's primary picture as a thumbnail of Size, given
// as classes.
type thumbnailView struct {
	Row  doorRow
	Size string
}

// within reports whether path is the page href or, for any page but home,
// one below it.
func within(path, href string) bool {
	return path == href || (href != "/" && strings.HasPrefix(path, href+"/"))
}

// fieldView is one labelled input of a form, with its problem or, without
// one, its help.
type fieldView struct {
	Name  string
	Label string
	Type  string
	Value string
	Error string
	Help  string
}

func barePage(name string) *template.Template {
	return parsePage("ui/templates/base.html", "ui/templates/"+name)
}

func shellPage(name string) *template.Template {
	return parsePage("ui/templates/base.html", "ui/templates/shell.html", "ui/templates/parts.html", "ui/templates/"+name)
}

func parsePage(files ...string) *template.Template {
	return template.Must(template.New("").Funcs(templateFuncs).ParseFS(templateFiles, files...))
}

// versioned is path with a query naming a hash of content.
func versioned(path string, content []byte) string {
	sum := sha256.Sum256(content)
	return path + "?v=" + hex.EncodeToString(sum[:6])
}

// pageData is what every page template receives.
type pageData struct {
	Title  string
	Site   string
	Assets map[string]string
	// Path is the requested path, for marking the current navigation link,
	// and URI the path with its query, for reloading part of the page.
	Path string
	URI  string
	// Viewer is the signed-in person, nil on a page for signed-out people.
	Viewer  *auth.User
	IsAdmin bool
	Flash   string
	// Content is the page's own data.
	Content any
}

// render writes page with status. It takes the flash message, if any, off
// the request so it shows once.
func (a *App) render(w http.ResponseWriter, r *http.Request, status int, page *template.Template, title string, viewer *auth.User, content any) {
	current, err := a.settings.Current(r.Context())
	if err != nil {
		a.pageError(w, err)
		return
	}
	data := pageData{
		Title:   title + " · " + current.SiteName,
		Site:    current.SiteName,
		Assets:  assetURLs,
		Path:    r.URL.Path,
		URI:     r.URL.RequestURI(),
		Viewer:  viewer,
		IsAdmin: viewer != nil && viewer.Has(auth.RoleAdmin),
		Flash:   takeFlash(w, r),
		Content: content,
	}
	writePage(w, status, page, data)
}

// pageError logs err under a fresh request id and shows a page naming only
// the id. It reads no settings, since they may be what failed.
func (a *App) pageError(w http.ResponseWriter, err error) {
	id := a.logFailure(err)
	writePage(w, http.StatusInternalServerError, errorTemplate, pageData{
		Title:   "Something went wrong",
		Assets:  assetURLs,
		Content: id,
	})
}

func (a *App) logFailure(err error) string {
	id := hex.EncodeToString(a.entropy.Bytes(requestIDBytes))
	a.log.Error("request failed",
		connector.Field{Key: "request_id", Value: id},
		connector.Field{Key: "error", Value: err.Error()})
	return id
}

// writePage executes page fully before writing, so a template failure cannot
// leave half a page behind a success status. The templates are fixed and
// every one is rendered by the entrypoint suite, so execution does not fail.
func writePage(w http.ResponseWriter, status int, page *template.Template, data pageData) {
	var out bytes.Buffer
	_ = page.ExecuteTemplate(&out, "base", data)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	_, _ = out.WriteTo(w)
}

func serveAsset(content []byte, contentType string) viewerHandler {
	return func(w http.ResponseWriter, _ *http.Request, _ auth.User) {
		w.Header().Set("Content-Type", contentType)
		w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		_, _ = w.Write(content)
	}
}

func (a *App) home(w http.ResponseWriter, r *http.Request, viewer auth.User) {
	a.render(w, r, http.StatusOK, homeTemplate, "Home", &viewer, nil)
}

func (a *App) apiDocsPage(w http.ResponseWriter, r *http.Request, viewer auth.User) {
	a.render(w, r, http.StatusOK, apiDocsTemplate, "API documentation", &viewer, apiPrefix+"/openapi.json")
}

func (a *App) settingsPage(w http.ResponseWriter, r *http.Request, viewer auth.User) {
	all, err := a.settings.List(r.Context())
	if err != nil {
		a.pageError(w, err)
		return
	}
	a.render(w, r, http.StatusOK, settingsTemplate, "Settings", &viewer, all)
}
