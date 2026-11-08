//go:build !slow

package connector_test

import (
	"testing"

	"github.com/google/go-cmp/cmp"
)

func TestCodesLoadReturnsStoredContent(t *testing.T) {
	for _, impl := range codesImpls {
		t.Run(impl.name, func(t *testing.T) {
			content := []byte("1111\n2468\n")

			got, err := impl.holding(t, content).Load()
			if err != nil {
				t.Fatal(err)
			}
			if diff := cmp.Diff(content, got); diff != "" {
				t.Errorf("content (-want +got):\n%s", diff)
			}
		})
	}
}

func TestCodesLoadReportsAnErrorWhenTheListIsMissing(t *testing.T) {
	for _, impl := range codesImpls {
		t.Run(impl.name, func(t *testing.T) {
			_, err := impl.missing(t).Load()
			if err == nil {
				t.Error("Load returned no error for a missing code list")
			}
		})
	}
}
