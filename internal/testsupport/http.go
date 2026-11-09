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
	"time"
)

// ClientAddress is the address every request comes from, as
// httptest.NewRequest gives it, unless a browser is given another with
// SetRemote.
const ClientAddress = "192.0.2.1"

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
	// remote is the address, host and port, requests come from; empty
	// is httptest's, ClientAddress.
	remote string
}

// NewBrowser returns a browser with no cookies.
func NewBrowser(h http.Handler) *Browser {
	return &Browser{handler: h, cookies: map[string]string{}, headers: map[string]string{}}
}

// Another is a new browser on the same site, with no cookies or headers,
// as another person would open it.
func (b *Browser) Another() *Browser {
	return NewBrowser(b.handler)
}

// SetHeader sends header name with value on every later request.
func (b *Browser) SetHeader(name, value string) {
	b.headers[name] = value
}

// SetRemote sends every later request from remote, a host and port such
// as 10.0.0.5:4000, as a proxy in front of the server would.
func (b *Browser) SetRemote(remote string) {
	b.remote = remote
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
	return b.UploadWith(http.MethodPost, target, field, filename, content, fields...)
}

// UploadWith sends, with method, what Upload posts.
func (b *Browser) UploadWith(method, target, field, filename string, content []byte, fields ...string) Response {
	var body bytes.Buffer
	form := multipart.NewWriter(&body)
	for i := 0; i+1 < len(fields); i += 2 {
		_ = form.WriteField(fields[i], fields[i+1])
	}
	file, _ := form.CreateFormFile(field, filename)
	_, _ = file.Write(content)
	_ = form.Close()
	return b.do(method, target, form.FormDataContentType(), &body)
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

// request is a request as the browser sends it, with its headers and
// cookies.
func (b *Browser) request(ctx context.Context, method, target, contentType string, body io.Reader) *http.Request {
	req := httptest.NewRequestWithContext(ctx, method, target, body)
	if b.remote != "" {
		req.RemoteAddr = b.remote
	}
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	for name, value := range b.headers {
		req.Header.Set(name, value)
	}
	for name, value := range b.cookies {
		req.AddCookie(&http.Cookie{Name: name, Value: value})
	}
	return req
}

// result sends a request and records the reply, keeping the cookies it
// sets.
func (b *Browser) result(method, target, contentType string, body io.Reader) *httptest.ResponseRecorder {
	req := b.request(context.Background(), method, target, contentType, body)
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

// streamWait is how long Stream.Next waits for the handler to flush.
const streamWait = 5 * time.Second

// Stream is a response the handler keeps writing, such as a stream of
// server-sent events, read a flushed part at a time.
type Stream struct {
	cancel context.CancelFunc
	done   chan struct{}
	w      *streamWriter
}

// Stream sends a GET whose response the handler keeps writing until the
// stream is closed.
func (b *Browser) Stream(target string) *Stream {
	ctx, cancel := context.WithCancel(context.Background())
	req := b.request(ctx, http.MethodGet, target, "", nil)
	s := &Stream{cancel: cancel, done: make(chan struct{}), w: &streamWriter{header: http.Header{}, status: http.StatusOK, parts: make(chan string, 64)}}
	go func() {
		defer close(s.done)
		b.handler.ServeHTTP(s.w, req)
		s.w.Flush()
	}()
	return s
}

// Next is the next part the handler flushed, or "" when it flushes nothing
// for five seconds.
func (s *Stream) Next() string {
	select {
	case part := <-s.w.parts:
		return part
	case <-time.After(streamWait):
		return ""
	}
}

// Status and ContentType are the response's, once Next has returned a
// part.
func (s *Stream) Status() int { return s.w.status }

// ContentType is the response's content type, once Next has returned a
// part.
func (s *Stream) ContentType() string { return s.w.header.Get("Content-Type") }

// Close ends the request, as a browser leaving the page does, and waits
// for the handler to return.
func (s *Stream) Close() {
	s.cancel()
	<-s.done
}

// streamWriter hands each flushed part of a response to a Stream.
type streamWriter struct {
	header http.Header
	status int
	buf    bytes.Buffer
	parts  chan string
}

func (w *streamWriter) Header() http.Header { return w.header }

func (w *streamWriter) WriteHeader(status int) { w.status = status }

func (w *streamWriter) Write(p []byte) (int, error) { return w.buf.Write(p) }

func (w *streamWriter) Flush() {
	if w.buf.Len() > 0 {
		w.parts <- w.buf.String()
		w.buf.Reset()
	}
}
