// Command doorcode serves the doorcode API. It is production wiring: it
// builds the real connectors and hands them to the entrypoint.
package main

import (
	"log"
	"net/http"
	"os"
	"time"

	"github.com/dakriy/doorcode/internal/app"
	"github.com/dakriy/doorcode/internal/connector"
	"github.com/dakriy/doorcode/internal/driver/codesfile"
)

type factory struct {
	codesPath string
}

func (f factory) Codes() connector.Codes {
	return codesfile.File{Path: f.codesPath}
}

func main() {
	f := factory{codesPath: os.Getenv("DOORCODE_CODES_FILE")}
	srv := &http.Server{
		Addr:              os.Getenv("DOORCODE_ADDR"),
		Handler:           app.Handler(f),
		ReadHeaderTimeout: 10 * time.Second,
	}
	log.Fatal(srv.ListenAndServe())
}
