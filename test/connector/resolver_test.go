//go:build !slow

package connector_test

import (
	"net/netip"
	"slices"
	"testing"
)

func TestResolverLooksANameUpToItsAddresses(t *testing.T) {
	for _, impl := range resolverImpls {
		t.Run(impl.name, func(t *testing.T) {
			addresses, err := impl.resolver().Addresses(t.Context(), "localhost")
			if err != nil {
				t.Fatal(err)
			}

			// Some systems answer IPv4 in its IPv4-mapped IPv6 form
			// (::ffff:127.0.0.1), which the application unmaps.
			loopback := netip.MustParseAddr("127.0.0.1")
			if !slices.ContainsFunc(addresses, func(a netip.Addr) bool { return a.Unmap() == loopback }) {
				t.Errorf("localhost resolved to %v, want 127.0.0.1 among them", addresses)
			}
		})
	}
}
