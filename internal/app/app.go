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
	"github.com/dakriy/doorcode/internal/directory"
	"github.com/dakriy/doorcode/internal/settings"
)

// App is the running application.
type App struct {
	log       connector.Log
	entropy   connector.Entropy
	clock     connector.Clock
	settings  settings.Service
	auth      auth.Service
	directory directory.Service
	root      http.Handler
	// openapi is the OpenAPI document of the route table's API operations.
	openapi []byte
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
	authService := auth.Service{Store: store, Settings: s, Clock: clock, Entropy: entropy, Log: log}
	a := &App{
		log:       log,
		entropy:   entropy,
		clock:     clock,
		settings:  s,
		auth:      authService,
		directory: directory.Service{Store: store, Auth: authService, Clock: clock},
	}
	if err := a.auth.CreateFirstAdmin(ctx); err != nil {
		return nil, fmt.Errorf("create first administrator: %w", err)
	}

	table := a.routes()
	a.openapi = apiDocument(table)
	a.root = a.handler(table)
	return a, nil
}

// Handler returns the application's HTTP handler.
func (a *App) Handler() http.Handler {
	return a.root
}

func healthz(w http.ResponseWriter, _ *http.Request, _ auth.User) {
	_, _ = io.WriteString(w, "ok\n")
}
