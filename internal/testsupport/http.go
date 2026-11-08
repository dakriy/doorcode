package testsupport

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
)

// Response is what the handler returned.
type Response struct {
	Status int
	Body   string
}

// PostForm sends a form-encoded POST to h in-process and records the reply.
func PostForm(h http.Handler, path string, form map[string]string) Response {
	values := url.Values{}
	for k, v := range form {
		values.Set(k, v)
	}
	req := httptest.NewRequestWithContext(context.Background(), http.MethodPost, path, strings.NewReader(values.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return Response{Status: rec.Code, Body: rec.Body.String()}
}
