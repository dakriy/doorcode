// Package app is the entrypoint. New builds the application from a connector
// factory; the application serves HTTP through Handler.
package app

import (
	"context"
	"fmt"
	"io"
	"net/http"

	"github.com/dakriy/doorcode/internal/auth"
	"github.com/dakriy/doorcode/internal/connector"
	"github.com/dakriy/doorcode/internal/settings"
)

// App is the running application.
type App struct {
	log      connector.Log
	entropy  connector.Entropy
	settings settings.Service
	auth     auth.Service
	handler  http.Handler
}

// New brings the store's schema up to date, creates the first administrator
// when there is no user yet, and returns the application. It takes every
// connector from f and constructs none itself.
func New(ctx context.Context, f connector.Factory) (*App, error) {
	store := f.Store()
	log := f.Log()
	if err := store.Migrate(ctx); err != nil {
		return nil, fmt.Errorf("migrate schema: %w", err)
	}
	log.Info("schema migrated")

	clock := f.Clock()
	entropy := f.Entropy()
	s := settings.Service{Store: store, Env: f.Environment(), Clock: clock}
	a := &App{
		log:      log,
		entropy:  entropy,
		settings: s,
		auth:     auth.Service{Store: store, Settings: s, Clock: clock, Entropy: entropy, Log: log},
	}
	if err := a.auth.CreateFirstAdmin(ctx); err != nil {
		return nil, fmt.Errorf("create first administrator: %w", err)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", healthz)
	mux.HandleFunc("GET /static/app.css", serveAsset(appCSS, "text/css; charset=utf-8"))
	mux.HandleFunc("GET /static/htmx.min.js", serveAsset(htmxJS, "text/javascript; charset=utf-8"))
	mux.HandleFunc("GET /sign-in", a.signInPage)
	mux.HandleFunc("POST /sign-in", a.signIn)
	mux.HandleFunc("POST /sign-out", a.signOut)
	mux.HandleFunc("GET /{$}", a.signedIn(a.home))
	mux.HandleFunc("GET /admin/settings", a.admin(a.settingsPage))
	mux.HandleFunc("GET /api/v1/settings", a.adminAPI(a.listSettings))
	mux.HandleFunc("GET /api/v1/settings/{key}", a.adminAPI(a.getSetting))
	mux.HandleFunc("PUT /api/v1/settings/{key}", a.adminAPI(a.updateSetting))
	a.handler = http.NewCrossOriginProtection().Handler(mux)
	return a, nil
}

// Handler returns the application's HTTP handler.
func (a *App) Handler() http.Handler {
	return a.handler
}

func healthz(w http.ResponseWriter, _ *http.Request) {
	_, _ = io.WriteString(w, "ok\n")
}
