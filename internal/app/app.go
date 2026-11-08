// Package app is the entrypoint. New builds the application from a connector
// factory; the application serves HTTP through Handler.
package app

import (
	"context"
	"fmt"
	"io"
	"net/http"

	"github.com/dakriy/doorcode/internal/connector"
	"github.com/dakriy/doorcode/internal/settings"
)

// App is the running application.
type App struct {
	log      connector.Log
	entropy  connector.Entropy
	settings settings.Service
	mux      *http.ServeMux
}

// New brings the store's schema up to date and returns the application. It
// takes every connector from f and constructs none itself.
func New(ctx context.Context, f connector.Factory) (*App, error) {
	store := f.Store()
	log := f.Log()
	if err := store.Migrate(ctx); err != nil {
		return nil, fmt.Errorf("migrate schema: %w", err)
	}
	log.Info("schema migrated")

	a := &App{
		log:      log,
		entropy:  f.Entropy(),
		settings: settings.Service{Store: store, Env: f.Environment(), Clock: f.Clock()},
		mux:      http.NewServeMux(),
	}
	a.mux.HandleFunc("GET /healthz", healthz)
	a.mux.HandleFunc("GET /api/v1/settings", a.listSettings)
	a.mux.HandleFunc("GET /api/v1/settings/{key}", a.getSetting)
	a.mux.HandleFunc("PUT /api/v1/settings/{key}", a.updateSetting)
	return a, nil
}

// Handler returns the application's HTTP handler.
func (a *App) Handler() http.Handler {
	return a.mux
}

func healthz(w http.ResponseWriter, _ *http.Request) {
	_, _ = io.WriteString(w, "ok\n")
}
