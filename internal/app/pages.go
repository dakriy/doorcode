package app

import (
	"bytes"
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"html/template"
	"net/http"

	"github.com/dakriy/doorcode/internal/auth"
	"github.com/dakriy/doorcode/internal/connector"
)

//go:embed ui/templates/*.html
var templateFiles embed.FS

// appCSS is built from ui/tailwind.css by `make ui`; htmxJS is vendored by
// the same target.
var (
	//go:embed ui/static/app.css
	appCSS []byte
	//go:embed ui/static/htmx.min.js
	htmxJS []byte
)

// The pages. A page in the app shell has navigation and the viewer's menu; a
// bare page has neither.
var (
	signInTemplate    = barePage("sign-in.html")
	errorTemplate     = barePage("error.html")
	homeTemplate      = shellPage("home.html")
	settingsTemplate  = shellPage("settings.html")
	forbiddenTemplate = shellPage("forbidden.html")
)

// assetURLs carry a hash of the content, so browsers may cache them forever.
var assetURLs = map[string]string{
	"CSS":  versioned("/static/app.css", appCSS),
	"HTMX": versioned("/static/htmx.min.js", htmxJS),
}

// navItem is one link in the main navigation.
type navItem struct {
	Href    string
	Label   string
	Current bool
}

var templateFuncs = template.FuncMap{
	"navlink": func(href, label, path string) navItem {
		return navItem{Href: href, Label: label, Current: href == path}
	},
}

func barePage(name string) *template.Template {
	return parsePage("ui/templates/base.html", "ui/templates/"+name)
}

func shellPage(name string) *template.Template {
	return parsePage("ui/templates/base.html", "ui/templates/shell.html", "ui/templates/"+name)
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
	// Path is the requested path, for marking the current navigation link.
	Path string
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

func serveAsset(content []byte, contentType string) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", contentType)
		w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		_, _ = w.Write(content)
	}
}

func (a *App) home(w http.ResponseWriter, r *http.Request, viewer auth.User) {
	a.render(w, r, http.StatusOK, homeTemplate, "Home", &viewer, nil)
}

func (a *App) settingsPage(w http.ResponseWriter, r *http.Request, viewer auth.User) {
	all, err := a.settings.List(r.Context())
	if err != nil {
		a.pageError(w, err)
		return
	}
	a.render(w, r, http.StatusOK, settingsTemplate, "Settings", &viewer, all)
}
