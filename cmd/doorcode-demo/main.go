// Command doorcode-demo runs the doorcode application over in-memory
// stand-ins for its database and its broker, seeded with a demo site and
// played by simulated door controllers, so the web pages can be explored
// without Postgres, a broker, or entering data. Nothing it holds outlives
// the process. It is production wiring: it builds the connectors, real
// ones where nothing has to be stood in for, and hands them to the
// entrypoint.
package main

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/dakriy/doorcode/internal/app"
	"github.com/dakriy/doorcode/internal/connector"
	"github.com/dakriy/doorcode/internal/demo"
	"github.com/dakriy/doorcode/internal/demo/memstore"
	"github.com/dakriy/doorcode/internal/driver/cryptoentropy"
	"github.com/dakriy/doorcode/internal/driver/dnsresolver"
	"github.com/dakriy/doorcode/internal/driver/oidcprovider"
	"github.com/dakriy/doorcode/internal/driver/osenv"
	"github.com/dakriy/doorcode/internal/driver/slogjson"
	"github.com/dakriy/doorcode/internal/driver/systemclock"
	"github.com/dakriy/doorcode/internal/driver/tzdb"
)

type factory struct {
	env    connector.Environment
	log    *demo.Log
	store  *memstore.Store
	broker *demo.Broker
}

func (factory) Clock() connector.Clock               { return systemclock.Clock{} }
func (factory) Entropy() connector.Entropy           { return cryptoentropy.Source{} }
func (f factory) Environment() connector.Environment { return f.env }
func (factory) Resolver() connector.Resolver         { return dnsresolver.Resolver{} }
func (factory) TimeZones() connector.TimeZones       { return tzdb.Zones{} }
func (f factory) Log() connector.Log                 { return f.log }
func (f factory) Store() connector.Store             { return f.store }
func (f factory) Broker() connector.Broker           { return f.broker }
func (factory) Identity() connector.Identity {
	return oidcprovider.New(&http.Client{Timeout: identityTimeout})
}

// identityTimeout is how long an identity provider has to answer each
// request, should the demo be pointed at one.
const identityTimeout = 10 * time.Second

func main() {
	fallback, ok := os.LookupEnv("DOORCODE_LISTEN_ADDR")
	if !ok {
		fallback = ":8080"
	}
	listen := flag.String("listen", fallback, "the address to serve the web pages on")
	flag.Parse()
	log.Fatal(run(context.Background(), *listen))
}

// run builds and seeds the demo and serves it until either side stops.
func run(ctx context.Context, listen string) error {
	f, err := newFactory()
	if err != nil {
		return err
	}
	a, err := app.New(ctx, f)
	if err != nil {
		return err
	}
	site, err := demo.Seed(ctx, a.Handler(), f.store, f.env, f.log, time.Now())
	if err != nil {
		return err
	}
	return serve(ctx, a, f, site, listen)
}

// serve runs the broker side, starts the simulated doors, and serves the
// web pages on listen, until either side stops.
func serve(ctx context.Context, a *app.App, f factory, site demo.Site, listen string) error {
	stopped := make(chan error, 2)
	go func() { stopped <- serveDoors(ctx, a) }()
	if err := demo.Start(ctx, a.Handler(), f.broker, site, f.log); err != nil {
		return err
	}
	srv := &http.Server{Addr: listen, Handler: a.Handler(), ReadHeaderTimeout: 10 * time.Second}
	go func() { stopped <- srv.ListenAndServe() }()
	fmt.Printf("doorcode demo on %s\nsign in as %s with the password %s to administer the site,\nor as %s with the password %s to see it as a member of staff\n",
		listen, site.AdminEmail, demo.AdminPassword, demo.MemberEmail, demo.MemberPassword)
	return <-stopped
}

// serveDoors serves the simulated controllers until the broker stops
// delivering, which ends the demo.
func serveDoors(ctx context.Context, a *app.App) error {
	if err := a.Run(ctx); err != nil {
		return err
	}
	return errors.New("the demo broker stopped delivering messages")
}

// newFactory builds the stand-ins and a throwaway key for the secret
// settings, which nothing outlives anyway.
func newFactory() (factory, error) {
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		return factory{}, err
	}
	env := demo.Environment{Process: osenv.Environment{}, Set: map[string]string{"DOORCODE_SECRET_KEY": base64.StdEncoding.EncodeToString(key)}}
	return factory{env: env, log: demo.NewLog(slogjson.New(os.Stderr)), store: memstore.New(), broker: demo.NewBroker()}, nil
}
