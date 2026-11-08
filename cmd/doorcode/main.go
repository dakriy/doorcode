// Command doorcode serves the doorcode application. It is production wiring:
// it reads the environment, builds the real connectors, and hands them to the
// entrypoint.
package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/dakriy/doorcode/internal/app"
	"github.com/dakriy/doorcode/internal/connector"
	"github.com/dakriy/doorcode/internal/driver/cryptoentropy"
	"github.com/dakriy/doorcode/internal/driver/osenv"
	"github.com/dakriy/doorcode/internal/driver/postgres"
	"github.com/dakriy/doorcode/internal/driver/slogjson"
	"github.com/dakriy/doorcode/internal/driver/systemclock"
	"github.com/dakriy/doorcode/internal/driver/tzdb"
)

type factory struct {
	store *postgres.Store
}

func (factory) Clock() connector.Clock             { return systemclock.Clock{} }
func (factory) Entropy() connector.Entropy         { return cryptoentropy.Source{} }
func (factory) Environment() connector.Environment { return osenv.Environment{} }
func (factory) TimeZones() connector.TimeZones     { return tzdb.Zones{} }
func (factory) Log() connector.Log                 { return slogjson.New(os.Stderr) }
func (f factory) Store() connector.Store           { return f.store }

func requireEnv(name string) string {
	value := os.Getenv(name)
	if value == "" {
		log.Fatalf("%s must be set", name)
	}
	return value
}

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

	a, err := app.New(ctx, factory{store: store})
	if err != nil {
		return err
	}
	srv := &http.Server{
		Addr:              requireEnv("DOORCODE_LISTEN_ADDR"),
		Handler:           a.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
	}
	return srv.ListenAndServe()
}
