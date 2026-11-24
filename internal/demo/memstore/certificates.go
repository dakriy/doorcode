package memstore

import (
	"cmp"
	"context"
	"slices"

	"github.com/dakriy/doorcode/internal/connector"
)

// CertificateChecks returns the latest check of every endpoint, by
// address.
func (s *Store) CertificateChecks(_ context.Context) ([]connector.StoredCertificateCheck, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := []connector.StoredCertificateCheck{}
	for _, c := range s.certChecks {
		out = append(out, checkOut(c))
	}
	slices.SortFunc(out, func(a, b connector.StoredCertificateCheck) int { return cmp.Compare(a.Address, b.Address) })
	return out, nil
}

// PutCertificateCheck stores c over the check of its address.
func (s *Store) PutCertificateCheck(_ context.Context, c connector.StoredCertificateCheck) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.certChecks[c.Address] = checkOut(c)
	return nil
}

// DeleteCertificateCheck deletes the check of address, if there is one.
func (s *Store) DeleteCertificateCheck(_ context.Context, address string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.certChecks, address)
	return nil
}

// checkOut is a copy of c sharing nothing with it.
func checkOut(c connector.StoredCertificateCheck) connector.StoredCertificateCheck {
	c.Chain = slices.Clone(c.Chain)
	c.Anchor, c.RunwayDays, c.LeafDays = clone(c.Anchor), clone(c.RunwayDays), clone(c.LeafDays)
	if c.Baseline != nil {
		c.Baseline = &connector.StoredChainShape{Links: slices.Clone(c.Baseline.Links), Since: c.Baseline.Since}
	}
	return c
}
