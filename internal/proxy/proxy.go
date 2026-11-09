// Package proxy decides whom the server believes about where a request
// came from. A proxy in front of the server says, in the X-Forwarded-For
// and X-Forwarded-Proto headers, who its client was and whether it was
// reached over HTTPS; the server takes its word only when the proxy is on
// the trusted list: an IP address, a CIDR range, or a host name, whose
// addresses the periodic work looks up and remembers.
package proxy

import (
	"context"
	"errors"
	"fmt"
	"net/netip"
	"slices"
	"strings"
	"sync"

	"github.com/dakriy/doorcode/internal/check"
	"github.com/dakriy/doorcode/internal/connector"
)

// hint is what the trusted list must be.
const hint = "must be IP addresses, CIDR ranges, and host names separated by commas, such as 10.0.0.0/8,proxy.example.com"

// List is the proxies the site trusts: the addresses in Ranges (a single
// address is a range of one), and the addresses Names resolve to.
type List struct {
	Ranges []netip.Prefix
	Names  []string
}

// Parse reads a list of IP addresses, CIDR ranges, and host names
// separated by commas, ignoring empty entries.
func Parse(value string) (List, error) {
	var l List
	for _, entry := range strings.Split(value, ",") {
		entry = strings.TrimSpace(entry)
		if entry == "" {
			continue
		}
		if prefix, err := netip.ParsePrefix(entry); err == nil {
			l.Ranges = append(l.Ranges, prefix)
			continue
		}
		if addr, err := netip.ParseAddr(entry); err == nil {
			addr = addr.Unmap()
			l.Ranges = append(l.Ranges, netip.PrefixFrom(addr, addr.BitLen()))
			continue
		}
		if !check.HostName(entry) {
			return List{}, errors.New(hint)
		}
		l.Names = append(l.Names, entry)
	}
	return l, nil
}

// Resolved is the addresses each name resolved to when it was last
// looked up. It is shared by the periodic work, which looks names up, and
// every request, which reads them.
type Resolved struct {
	mu        sync.Mutex
	addresses map[string][]netip.Addr
}

// NewResolved returns a memory of no names.
func NewResolved() *Resolved {
	return &Resolved{addresses: map[string][]netip.Addr{}}
}

func (r *Resolved) set(name string, addresses []netip.Addr) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.addresses[name] = addresses
}

func (r *Resolved) has(name string, addr netip.Addr) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return slices.ContainsFunc(r.addresses[name], func(a netip.Addr) bool { return a.Unmap() == addr })
}

// Service looks the trusted names up and decides whom to believe.
type Service struct {
	Resolver connector.Resolver
	Resolved *Resolved
}

// Resolve looks up every name of l and remembers its addresses. A name
// that cannot be looked up keeps the addresses it had, if any.
func (s Service) Resolve(ctx context.Context, l List) error {
	var errs []error
	for _, name := range l.Names {
		addresses, err := s.Resolver.Addresses(ctx, name)
		if err != nil {
			errs = append(errs, fmt.Errorf("look up trusted proxy %s: %w", name, err))
			continue
		}
		s.Resolved.set(name, addresses)
	}
	return errors.Join(errs...)
}

// trusts reports whether l lists addr, in a range or among the addresses
// a name last resolved to.
func (s Service) trusts(l List, addr netip.Addr) bool {
	inRange := slices.ContainsFunc(l.Ranges, func(p netip.Prefix) bool { return p.Contains(addr) })
	return inRange || slices.ContainsFunc(l.Names, func(name string) bool { return s.Resolved.has(name, addr) })
}

// remoteAddr is the address of a connection's far end, written as host
// and port.
func remoteAddr(remote string) (netip.Addr, error) {
	addrPort, err := netip.ParseAddrPort(remote)
	return addrPort.Addr().Unmap(), err
}

// Client is the address of whoever made a request that reached the
// server from remote (a host and port) with the X-Forwarded-For headers
// forwarded. Each proxy appends the address it was reached from, so the
// client is found by walking the addresses back from the connection's
// own for as long as each is a trusted proxy. A remote that is not an
// address is given as it is.
func (s Service) Client(l List, remote string, forwarded []string) string {
	client, err := remoteAddr(remote)
	if err != nil {
		return remote
	}
	hops := strings.Split(strings.Join(forwarded, ","), ",")
	for i := len(hops) - 1; i >= 0 && s.trusts(l, client); i-- {
		hop, err := netip.ParseAddr(strings.TrimSpace(hops[i]))
		if err != nil {
			break
		}
		client = hop.Unmap()
	}
	return client.String()
}

// HTTPS reports whether a request that reached the server from remote
// with the X-Forwarded-Proto header proto came over HTTPS by the word of
// a trusted proxy.
func (s Service) HTTPS(l List, remote, proto string) bool {
	// A remote that is not an address is the zero address, which no
	// range or name holds.
	addr, _ := remoteAddr(remote)
	return proto == "https" && s.trusts(l, addr)
}
