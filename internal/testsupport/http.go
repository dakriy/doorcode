package testsupport

import (
	"bytes"
	"context"
	"encoding/base64"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
)

// Handler is the application's HTTP handler as tests hold it.
type Handler = http.Handler

// Response is what the handler returned. SetCookie holds the raw Set-Cookie
// header lines in order.
type Response struct {
	Status      int
	ContentType string
	Location    string
	SetCookie   []string
	Body        string
}

// Get sends a GET to h in-process, without cookies, and records the reply.
func Get(h http.Handler, target string) Response {
	return NewBrowser(h).Get(target)
}

// PutJSON sends a PUT with a JSON body to h in-process, without cookies, and
// records the reply.
func PutJSON(h http.Handler, target, body string) Response {
	return NewBrowser(h).PutJSON(target, body)
}

// Browser sends requests to a handler in-process the way a browser would:
// it keeps the cookies responses set and sends them back. It does not follow
// redirects. A target may be a path or a full URL; an https URL arrives as a
// TLS request.
type Browser struct {
	handler http.Handler
	cookies map[string]string
	headers map[string]string
}

// NewBrowser returns a browser with no cookies.
func NewBrowser(h http.Handler) *Browser {
	return &Browser{handler: h, cookies: map[string]string{}, headers: map[string]string{}}
}

// SetHeader sends header name with value on every later request.
func (b *Browser) SetHeader(name, value string) {
	b.headers[name] = value
}

// SetCookie stores a cookie as if a response had set it.
func (b *Browser) SetCookie(name, value string) {
	b.cookies[name] = value
}

// Cookie returns the value of the cookie the browser holds under name.
func (b *Browser) Cookie(name string) string {
	return b.cookies[name]
}

// Get sends a GET.
func (b *Browser) Get(target string) Response {
	return b.do(http.MethodGet, target, "", nil)
}

// Post submits a form; fields alternate name and value.
func (b *Browser) Post(target string, fields ...string) Response {
	form := url.Values{}
	for i := 0; i+1 < len(fields); i += 2 {
		form.Add(fields[i], fields[i+1])
	}
	return b.do(http.MethodPost, target, "application/x-www-form-urlencoded", strings.NewReader(form.Encode()))
}

// PutJSON sends a PUT with a JSON body.
func (b *Browser) PutJSON(target, body string) Response {
	return b.Send(http.MethodPut, target, body)
}

// PostJSON sends a POST with a JSON body.
func (b *Browser) PostJSON(target, body string) Response {
	return b.Send(http.MethodPost, target, body)
}

// Delete sends a DELETE.
func (b *Browser) Delete(target string) Response {
	return b.Send(http.MethodDelete, target, "")
}

// Upload posts a multipart form holding content as a file named filename
// in the field field, and then the other fields, alternating name and
// value.
func (b *Browser) Upload(target, field, filename string, content []byte, fields ...string) Response {
	var body bytes.Buffer
	form := multipart.NewWriter(&body)
	for i := 0; i+1 < len(fields); i += 2 {
		_ = form.WriteField(fields[i], fields[i+1])
	}
	file, _ := form.CreateFormFile(field, filename)
	_, _ = file.Write(content)
	_ = form.Close()
	return b.do(http.MethodPost, target, form.FormDataContentType(), &body)
}

// SendForm posts body as it is, with content type contentType.
func (b *Browser) SendForm(target, contentType, body string) Response {
	return b.do(http.MethodPost, target, contentType, strings.NewReader(body))
}

// Header sends a GET and returns the response's header name.
func (b *Browser) Header(target, name string) string {
	return b.result(http.MethodGet, target, "", nil).Header().Get(name)
}

// Send sends a request with method, and with body as JSON unless it is
// empty.
func (b *Browser) Send(method, target, body string) Response {
	if body == "" {
		return b.do(method, target, "", nil)
	}
	return b.do(method, target, "application/json", strings.NewReader(body))
}

// BasicAuth is the Authorization header value of HTTP Basic authentication
// with user and password.
func BasicAuth(user, password string) string {
	return "Basic " + base64.StdEncoding.EncodeToString([]byte(user+":"+password))
}

func (b *Browser) do(method, target, contentType string, body io.Reader) Response {
	rec := b.result(method, target, contentType, body)
	return Response{
		Status:      rec.Code,
		ContentType: rec.Header().Get("Content-Type"),
		Location:    rec.Header().Get("Location"),
		SetCookie:   rec.Header().Values("Set-Cookie"),
		Body:        rec.Body.String(),
	}
}

// result sends a request and records the reply, keeping the cookies it
// sets.
func (b *Browser) result(method, target, contentType string, body io.Reader) *httptest.ResponseRecorder {
	req := httptest.NewRequestWithContext(context.Background(), method, target, body)
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	for name, value := range b.headers {
		req.Header.Set(name, value)
	}
	for name, value := range b.cookies {
		req.AddCookie(&http.Cookie{Name: name, Value: value})
	}
	rec := httptest.NewRecorder()
	b.handler.ServeHTTP(rec, req)
	result := rec.Result()
	for _, c := range result.Cookies() {
		if c.MaxAge < 0 {
			delete(b.cookies, c.Name)
		} else {
			b.cookies[c.Name] = c.Value
		}
	}
	return rec
}
