//go:build slow

package connector_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"

	"github.com/dakriy/doorcode/internal/connector"
)

func TestStoreKeepsTheLatestCertificateCheckOfEachAddress(t *testing.T) {
	for _, impl := range storeImpls {
		t.Run(impl.name, func(t *testing.T) {
			store := migrated(t, impl)
			ctx := context.Background()
			checked := time.Date(2026, 1, 5, 9, 0, 0, 0, time.UTC)
			runway, leaf := 970, 54
			anchor := connector.StoredCertificate{Subject: "CN=Root X2", Issuer: "CN=Root X2", NotAfter: time.Date(2040, 9, 17, 0, 0, 0, 0, time.UTC)}
			verified := connector.StoredCertificateCheck{
				Address: "broker.example.com:8883", CheckedAt: checked, Verified: true,
				Chain: []connector.StoredCertificate{
					{Subject: "CN=broker.example.com", Issuer: "CN=YE1", NotAfter: time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)},
					{Subject: "CN=YE1", Issuer: "CN=Root X2", NotAfter: time.Date(2028, 9, 2, 0, 0, 0, 0, time.UTC)},
				},
				Anchor: &anchor, RunwayDays: &runway, LeafDays: &leaf,
				Baseline: &connector.StoredChainShape{Links: []connector.StoredChainLink{
					{Subject: "CN=broker.example.com", Issuer: "CN=YE1"}, {Subject: "CN=YE1", Issuer: "CN=Root X2"}, {Subject: "CN=Root X2", Issuer: "CN=Root X2"},
				}, Since: checked.Add(-time.Hour)},
			}
			unreachable := connector.StoredCertificateCheck{
				Address: "[2001:db8::1]:443", CheckedAt: checked, Error: "connection refused", Chain: []connector.StoredCertificate{}, Failures: 4,
			}
			later := connector.StoredCertificateCheck{
				Address: "broker.example.com:8883", CheckedAt: checked.Add(time.Hour), Problem: "the server served no certificate",
				Chain: []connector.StoredCertificate{}, Baseline: verified.Baseline,
			}

			none, noneErr := store.CertificateChecks(ctx)
			errs := []error{
				store.PutCertificateCheck(ctx, verified),
				store.PutCertificateCheck(ctx, unreachable),
			}
			first, firstErr := store.CertificateChecks(ctx)
			replaced := store.PutCertificateCheck(ctx, later)
			second, secondErr := store.CertificateChecks(ctx)

			want := []any{
				[]connector.StoredCertificateCheck{}, nil, []error{nil, nil},
				[]connector.StoredCertificateCheck{unreachable, verified}, nil, nil,
				[]connector.StoredCertificateCheck{unreachable, later}, nil,
			}
			got := []any{none, noneErr, errs, first, firstErr, replaced, second, secondErr}
			if diff := cmp.Diff(want, got); diff != "" {
				t.Errorf("none, stored, replaced (-want +got):\n%s", diff)
			}
		})
	}
}

func TestStoreDeletesTheCertificateCheckOfAnAddressAlone(t *testing.T) {
	for _, impl := range storeImpls {
		t.Run(impl.name, func(t *testing.T) {
			store := migrated(t, impl)
			ctx := context.Background()
			checked := time.Date(2026, 1, 5, 9, 0, 0, 0, time.UTC)
			broker := connector.StoredCertificateCheck{Address: "broker.example.com:8883", CheckedAt: checked, Error: "i/o timeout", Chain: []connector.StoredCertificate{}, Failures: 2}
			device := connector.StoredCertificateCheck{Address: "devices.example.com:443", CheckedAt: checked, Problem: "the server served no certificate", Chain: []connector.StoredCertificate{}}
			errs := []error{
				store.PutCertificateCheck(ctx, broker),
				store.PutCertificateCheck(ctx, device),
				store.DeleteCertificateCheck(ctx, "broker.example.com:8883"),
				store.DeleteCertificateCheck(ctx, "never.example.com:443"),
			}
			left, leftErr := store.CertificateChecks(ctx)

			want := []any{[]error{nil, nil, nil, nil}, []connector.StoredCertificateCheck{device}, nil}
			if diff := cmp.Diff(want, []any{errs, left, leftErr}); diff != "" {
				t.Errorf("errors, checks left (-want +got):\n%s", diff)
			}
		})
	}
}
