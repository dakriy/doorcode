package demo

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
)

// client calls the application's handler in process, as a person would
// through the API, so everything the demo seeds passes the application's
// own checks.
type client struct {
	ctx     context.Context
	handler http.Handler
	// name and value are the header, Cookie or Authorization, every call
	// sends; none when name is empty.
	name, value string
}

// call sends body as JSON (none when nil) and decodes the answer into out
// (unless nil), failing unless the answer has status 2xx.
func (c client) call(method, path string, body, out any) error {
	var reader io.Reader
	if body != nil {
		encoded, err := json.Marshal(body)
		if err != nil {
			return err
		}
		reader = bytes.NewReader(encoded)
	}
	return c.send(method, path, "application/json", reader, out)
}

// upload sends a multipart form of fields and one file in field and
// decodes the answer into out (unless nil).
func (c client) upload(method, path string, fields map[string]string, field, filename string, data []byte, out any) error {
	var body bytes.Buffer
	w := multipart.NewWriter(&body)
	for name, value := range fields {
		if err := w.WriteField(name, value); err != nil {
			return err
		}
	}
	part, err := w.CreateFormFile(field, filename)
	if err == nil {
		_, err = part.Write(data)
	}
	if err == nil {
		err = w.Close()
	}
	if err != nil {
		return err
	}
	return c.send(method, path, w.FormDataContentType(), &body, out)
}

func (c client) send(method, path, contentType string, body io.Reader, out any) error {
	w := c.serve(method, path, contentType, body)
	if w.Code < 200 || w.Code > 299 {
		return fmt.Errorf("%s %s answered %d: %s", method, path, w.Code, strings.TrimSpace(w.Body.String()))
	}
	if out == nil {
		return nil
	}
	return json.Unmarshal(w.Body.Bytes(), out)
}

func (c client) serve(method, path, contentType string, body io.Reader) *httptest.ResponseRecorder {
	r := httptest.NewRequestWithContext(c.ctx, method, path, body)
	if body != nil {
		r.Header.Set("Content-Type", contentType)
	}
	if c.name != "" {
		r.Header.Set(c.name, c.value)
	}
	w := httptest.NewRecorder()
	c.handler.ServeHTTP(w, r)
	return w
}

// signIn signs in with email and password through the sign-in form and
// returns a client carrying the session.
func signIn(ctx context.Context, h http.Handler, email, password string) (client, error) {
	form := url.Values{"email": {email}, "password": {password}}
	anonymous := client{ctx: ctx, handler: h}
	w := anonymous.serve(http.MethodPost, "/sign-in", "application/x-www-form-urlencoded", strings.NewReader(form.Encode()))
	res := w.Result()
	defer func() { _ = res.Body.Close() }()
	for _, cookie := range res.Cookies() {
		if cookie.Name == "doorcode_session" {
			return client{ctx: ctx, handler: h, name: "Cookie", value: cookie.Name + "=" + cookie.Value}, nil
		}
	}
	return client{}, fmt.Errorf("signing in as %s answered %d", email, w.Code)
}

// as returns a client calling the API with a personal API token.
func (c client) as(secret string) client {
	return client{ctx: c.ctx, handler: c.handler, name: "Authorization", value: "Bearer " + secret}
}
