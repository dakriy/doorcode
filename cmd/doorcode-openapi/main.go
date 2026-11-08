// Command doorcode-openapi writes the OpenAPI document of doorcode's JSON API
// to standard output, as the server serves it. `make openapi` checks it in
// at docs/openapi.json; verify-fast fails when the two differ.
package main

import (
	"log"
	"os"

	"github.com/dakriy/doorcode/internal/app"
)

func main() {
	if _, err := os.Stdout.Write(app.OpenAPI()); err != nil {
		log.Fatal(err)
	}
}
