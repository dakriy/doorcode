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

			if !slices.Contains(addresses, netip.MustParseAddr("127.0.0.1")) {
				t.Errorf("localhost resolved to %v, want 127.0.0.1 among them", addresses)
			}
		})
	}
}
