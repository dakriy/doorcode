// Package app is the entrypoint. New builds the application from a connector
// factory; the application serves HTTP through Handler and the door
// controllers through Run.
package app

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/dakriy/doorcode/internal/auth"
	"github.com/dakriy/doorcode/internal/connector"
	"github.com/dakriy/doorcode/internal/controller"
	"github.com/dakriy/doorcode/internal/decision"
	"github.com/dakriy/doorcode/internal/directory"
	"github.com/dakriy/doorcode/internal/door"
	"github.com/dakriy/doorcode/internal/event"
	"github.com/dakriy/doorcode/internal/firmware"
	"github.com/dakriy/doorcode/internal/identity"
	"github.com/dakriy/doorcode/internal/override"
	"github.com/dakriy/doorcode/internal/proxy"
	"github.com/dakriy/doorcode/internal/push"
	"github.com/dakriy/doorcode/internal/request"
	"github.com/dakriy/doorcode/internal/rule"
	"github.com/dakriy/doorcode/internal/schedule"
	"github.com/dakriy/doorcode/internal/settings"
	"github.com/dakriy/doorcode/internal/stats"
	"github.com/dakriy/doorcode/internal/telemetry"
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
	overrides override.Service
	rules     rule.Service
	events    event.Service
	requests  request.Service
	// decisions decides the codes doors ask about.
	decisions decision.Service
	broker    connector.Broker
	// store is asked whether it answers when the server's readiness is.
	store connector.Store
	// pushes sends doors their configuration and timelines.
	pushes push.Service
	// controllers enrolls door controllers and looks after them; live
	// tells the pages watching it when what they show of them changes.
	controllers controller.Service
	live        *hub
	// telemetry follows the doors' state from what their controllers
	// report.
	telemetry telemetry.Service
	// firmware keeps the firmware images and rolls updates out to the
	// doors.
	firmware firmware.Service
	// stats sums up the dashboard's figures.
	stats stats.Service
	// identity signs people in through the OpenID Connect provider.
	identity identity.Service
	// proxies decides whom to believe about where a request came from.
	proxies proxy.Service
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
// when there is no user yet, and returns the application. It refuses to
// start without the key that encrypts the secret settings. It takes every
// connector from f and constructs none itself.
func New(ctx context.Context, f connector.Factory) (*App, error) {
	env := f.Environment()
	entropy := f.Entropy()
	secrets, err := settings.OpenSecrets(env, entropy)
	if err != nil {
		return nil, err
	}
	store := f.Store()
	log := f.Log()
	if err := store.Migrate(ctx); err != nil {
		return nil, fmt.Errorf("migrate schema: %w", err)
	}
	log.Info("schema migrated")

	clock := f.Clock()
	s := settings.Service{Store: store, Env: env, Clock: clock, Zones: f.TimeZones(), Secrets: secrets}
	authService := auth.Service{Store: store, Settings: s, Clock: clock, Entropy: entropy, Log: log}
	live := newHub()
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
		events:    event.Service{Store: store},
		stats:     stats.Service{Store: store},
		requests:  request.Service{Store: store, Clock: clock, Changed: live.requested},
		decisions: decision.Service{Store: store, Auth: authService, Settings: s, Clock: clock, Limits: decision.NewLimiter()},
		broker:    f.Broker(),
		store:     store,
		live:      live,
		proxies:   proxy.Service{Resolver: f.Resolver(), Resolved: proxy.NewResolved()},
	}
	a.overrides = override.Service{Store: store, Doors: a.doors, Clock: clock}
	a.pushes = push.Service{Store: store, Settings: s, Broker: a.broker, Clock: clock, Sent: push.NewSent()}
	a.controllers = controller.Service{
		Store: store, Doors: a.doors, Broker: a.broker, Clock: clock, Entropy: entropy, Pending: controller.NewPending(),
		Push: a.pushes, Changed: a.live.notify,
		Connected: func(ctx context.Context, doorID int64) error { return a.firmware.Settle(ctx, doorID) },
	}
	a.firmware = firmware.Service{
		Store: store, Doors: a.doors, Controllers: a.controllers, Events: a.events, Settings: s, Clock: clock, Entropy: entropy,
		DownloadPath: firmwareImagePath, Changed: a.live.notify,
	}
	a.identity = identity.Service{
		Settings: s, Directory: a.directory, Auth: authService, Identity: f.Identity(), Entropy: entropy, Clock: clock,
		Providers: &identity.Providers{},
	}
	a.telemetry = telemetry.Service{
		Store: store, Doors: a.doors, Events: a.events, Settings: s, Clock: clock, Changed: a.live.notify, Reported: a.live.report,
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

// healthz is the liveness probe: it answers while the process serves.
func healthz(w http.ResponseWriter, _ *http.Request, _ auth.User) {
	_, _ = io.WriteString(w, "ok\n")
}

// readyz is the readiness probe: it answers 200 when the store answers a
// ping and the broker connection is up, and otherwise 503 naming, one per
// line, what is not ready.
func (a *App) readyz(w http.ResponseWriter, r *http.Request, _ auth.User) {
	var unready []string
	if err := a.store.Ping(r.Context()); err != nil {
		unready = append(unready, "store: "+err.Error()+"\n")
	}
	if !a.broker.Connected() {
		unready = append(unready, "broker: not connected\n")
	}
	if len(unready) > 0 {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = io.WriteString(w, strings.Join(unready, ""))
		return
	}
	_, _ = io.WriteString(w, "ok\n")
}
