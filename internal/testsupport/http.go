package testsupport

import (
	"context"
	"io"
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
	return b.do(http.MethodPut, target, "application/json", strings.NewReader(body))
}

func (b *Browser) do(method, target, contentType string, body io.Reader) Response {
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
	return Response{
		Status:      rec.Code,
		ContentType: rec.Header().Get("Content-Type"),
		Location:    rec.Header().Get("Location"),
		SetCookie:   rec.Header().Values("Set-Cookie"),
		Body:        rec.Body.String(),
	}
}
