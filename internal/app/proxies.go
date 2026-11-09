package app

import (
	"net/http"

	"github.com/dakriy/doorcode/internal/proxy"
)

// trustedProxies is the proxies the settings trust. When the settings
// cannot be read it is none, so a failing store never makes the server
// believe a header.
func (a *App) trustedProxies(r *http.Request) proxy.List {
	current, _ := a.settings.Current(r.Context())
	return current.TrustedProxies
}

// client is the address of whoever made r, by the word of the trusted
// proxies it passed through.
func (a *App) client(r *http.Request) string {
	return a.proxies.Client(a.trustedProxies(r), r.RemoteAddr, r.Header.Values("X-Forwarded-For"))
}
