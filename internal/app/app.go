// Package app is the entrypoint: the HTTP handler for the doorcode API.
package app

import (
	"io"
	"net/http"

	"github.com/dakriy/doorcode/internal/access"
	"github.com/dakriy/doorcode/internal/connector"
)

// Handler returns the doorcode API. It takes every connector from f and
// constructs none itself.
func Handler(f connector.Factory) http.Handler {
	codes := f.Codes()
	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/check", func(w http.ResponseWriter, r *http.Request) {
		check(codes, w, r)
	})
	return mux
}

func check(codes connector.Codes, w http.ResponseWriter, r *http.Request) {
	content, err := codes.Load()
	if err != nil {
		http.Error(w, "unavailable", http.StatusServiceUnavailable)
		return
	}
	if !access.Allowed(content, r.FormValue("code")) {
		http.Error(w, "denied", http.StatusForbidden)
		return
	}
	_, _ = io.WriteString(w, "open\n")
}
