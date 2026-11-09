// Package dnsresolver implements connector.Resolver over the system's
// resolver, as the net package reaches it.
package dnsresolver

import (
	"context"
	"net"
	"net/netip"
)

// Resolver looks names up with net.DefaultResolver.
type Resolver struct{}

// Addresses returns the IPv4 and IPv6 addresses name resolves to.
func (Resolver) Addresses(ctx context.Context, name string) ([]netip.Addr, error) {
	return net.DefaultResolver.LookupNetIP(ctx, "ip", name)
}
