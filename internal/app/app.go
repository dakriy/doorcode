// Package app is the entrypoint. New builds the application from a connector
// factory; the application serves HTTP through Handler and the door
// controllers through Run.
package app

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"

	"github.com/dakriy/doorcode/internal/auth"
	"github.com/dakriy/doorcode/internal/connector"
	"github.com/dakriy/doorcode/internal/controller"
	"github.com/dakriy/doorcode/internal/directory"
	"github.com/dakriy/doorcode/internal/door"
	"github.com/dakriy/doorcode/internal/push"
	"github.com/dakriy/doorcode/internal/rule"
	"github.com/dakriy/doorcode/internal/schedule"
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
	schedules schedule.Service
	doors     door.Service
	rules     rule.Service
	broker    connector.Broker
	// pushes sends doors their configuration and timelines.
	pushes push.Service
	// controllers enrolls door controllers and looks after them; live
	// tells the pages watching it when what they show of them changes.
	controllers controller.Service
	live        *hub
	// enrolling says Run has set up the enrollment credentials, and
	// nextSweep is when it next does the periodic work. Only Run reads and
	// changes them.
	enrolling bool
	nextSweep time.Time
	root      http.Handler
	// openapi builds the OpenAPI document of the route table's API
	// operations the first time it is served, so constructing an App does
	// not pay for it.
	openapi func() []byte
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
	s := settings.Service{Store: store, Env: f.Environment(), Clock: clock, Zones: f.TimeZones()}
	authService := auth.Service{Store: store, Settings: s, Clock: clock, Entropy: entropy, Log: log}
	a := &App{
		log:       log,
		entropy:   entropy,
		clock:     clock,
		settings:  s,
		auth:      authService,
		directory: directory.Service{Store: store, Auth: authService, Clock: clock},
		schedules: schedule.Service{Store: store},
		doors:     door.Service{Store: store, Clock: clock},
		rules:     rule.Service{Store: store},
		broker:    f.Broker(),
		live:      newHub(),
	}
	a.pushes = push.Service{Store: store, Settings: s, Broker: a.broker, Clock: clock, Sent: push.NewSent()}
	a.controllers = controller.Service{
		Store: store, Doors: a.doors, Broker: a.broker, Clock: clock, Entropy: entropy, Pending: controller.NewPending(),
		Push: a.pushes, Changed: a.live.notify,
	}
	if err := a.auth.CreateFirstAdmin(ctx); err != nil {
		return nil, fmt.Errorf("create first administrator: %w", err)
	}

	table := a.routes()
	a.openapi = sync.OnceValue(func() []byte { return apiDocument(table) })
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
