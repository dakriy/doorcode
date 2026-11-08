package testsupport

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
)

// Handler is the application's HTTP handler as tests hold it.
type Handler = http.Handler

// Response is what the handler returned.
type Response struct {
	Status int
	Body   string
}

// Get sends a GET to h in-process and records the reply.
func Get(h http.Handler, path string) Response {
	return do(h, http.MethodGet, path, nil)
}

// PutJSON sends a PUT with a JSON body to h in-process and records the reply.
func PutJSON(h http.Handler, path, body string) Response {
	return do(h, http.MethodPut, path, strings.NewReader(body))
}

func do(h http.Handler, method, path string, body io.Reader) Response {
	req := httptest.NewRequestWithContext(context.Background(), method, path, body)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return Response{Status: rec.Code, Body: rec.Body.String()}
}
