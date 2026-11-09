package testsupport

import (
	"context"
	"errors"
	"net/netip"
	"sync"
)

// ErrNoSuchHost is what the stub resolver reports for a name it has no
// addresses for.
var ErrNoSuchHost = errors.New("no such host")

// Resolver is the stub connector.Resolver: names resolve to the addresses
// a test gave them, and every other name is ErrNoSuchHost.
type Resolver struct {
	mu        sync.Mutex
	addresses map[string][]netip.Addr
}

// Resolve makes name resolve to addresses, each an IP address as text,
// from now on.
func (r *Resolver) Resolve(name string, addresses ...string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.addresses == nil {
		r.addresses = map[string][]netip.Addr{}
	}
	var parsed []netip.Addr
	for _, a := range addresses {
		parsed = append(parsed, netip.MustParseAddr(a))
	}
	r.addresses[name] = parsed
}

// Addresses returns the addresses name was given.
func (r *Resolver) Addresses(_ context.Context, name string) ([]netip.Addr, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	found, ok := r.addresses[name]
	if !ok {
		return nil, ErrNoSuchHost
	}
	return found, nil
}
