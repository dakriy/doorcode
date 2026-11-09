//go:build slow

package connector_test

import "testing"

// The real resolver asks the host's name servers about a name in the
// .invalid domain, which none has (RFC 6761), so this test is slow.
func TestResolverReportsANameNobodyHas(t *testing.T) {
	for _, impl := range resolverImpls {
		t.Run(impl.name, func(t *testing.T) {
			addresses, err := impl.resolver().Addresses(t.Context(), "doorcode.invalid")

			if err == nil {
				t.Errorf("doorcode.invalid resolved to %v, want an error", addresses)
			}
		})
	}
}
