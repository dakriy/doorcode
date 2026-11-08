// Command doorcode serves the doorcode application: its web pages and API,
// and the door controllers over the broker. It is production wiring: it
// reads the environment, builds the real connectors, and hands them to the
// entrypoint.
package main

import (
	"context"
	"errors"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/dakriy/doorcode/internal/app"
	"github.com/dakriy/doorcode/internal/connector"
	"github.com/dakriy/doorcode/internal/driver/cryptoentropy"
	"github.com/dakriy/doorcode/internal/driver/dynsec"
	"github.com/dakriy/doorcode/internal/driver/oidcprovider"
	"github.com/dakriy/doorcode/internal/driver/osenv"
	"github.com/dakriy/doorcode/internal/driver/postgres"
	"github.com/dakriy/doorcode/internal/driver/slogjson"
	"github.com/dakriy/doorcode/internal/driver/systemclock"
	"github.com/dakriy/doorcode/internal/driver/tzdb"
)

type factory struct {
	store  *postgres.Store
	broker *dynsec.Broker
}

func (factory) Clock() connector.Clock             { return systemclock.Clock{} }
func (factory) Entropy() connector.Entropy         { return cryptoentropy.Source{} }
func (factory) Environment() connector.Environment { return osenv.Environment{} }
func (factory) TimeZones() connector.TimeZones     { return tzdb.Zones{} }
func (factory) Log() connector.Log                 { return slogjson.New(os.Stderr) }
func (f factory) Store() connector.Store           { return f.store }
func (f factory) Broker() connector.Broker         { return f.broker }
func (factory) Identity() connector.Identity {
	return oidcprovider.New(&http.Client{Timeout: identityTimeout})
}

func requireEnv(name string) string {
	value := os.Getenv(name)
	if value == "" {
		log.Fatalf("%s must be set", name)
	}
	return value
}

// brokerCommandTimeout is how long the broker has to answer a command
// that manages its clients.
const brokerCommandTimeout = 10 * time.Second

// identityTimeout is how long an identity provider has to answer each
// request: discovery, a code exchange, or a userinfo request.
const identityTimeout = 10 * time.Second

func main() {
	if err := run(context.Background()); err != nil {
		log.Fatal(err)
	}
}

func run(ctx context.Context) error {
	store, err := postgres.Open(ctx, requireEnv("DOORCODE_DATABASE_URL"))
	if err != nil {
		return err
	}
	defer store.Close()
	broker, err := dynsec.Open(ctx, requireEnv("DOORCODE_BROKER_URL"), "doorcode-server", brokerCommandTimeout)
	if err != nil {
		return err
	}
	defer broker.Close()

	a, err := app.New(ctx, factory{store: store, broker: broker})
	if err != nil {
		return err
	}
	srv := &http.Server{
		Addr:              requireEnv("DOORCODE_LISTEN_ADDR"),
		Handler:           a.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
	}
	stopped := make(chan error, 2)
	go func() { stopped <- serveDoors(ctx, a) }()
	go func() { stopped <- srv.ListenAndServe() }()
	return <-stopped
}

// serveDoors serves the door controllers until the broker stops delivering,
// which ends the process.
func serveDoors(ctx context.Context, a *app.App) error {
	if err := a.Run(ctx); err != nil {
		return err
	}
	return errors.New("the broker stopped delivering messages")
}
