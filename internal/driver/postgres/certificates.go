package postgres

import (
	"context"

	"github.com/jackc/pgx/v5"

	"github.com/dakriy/doorcode/internal/connector"
)

func certificateCheckFields(c *connector.StoredCertificateCheck) []any {
	return []any{&c.Address, &c.CheckedAt, &c.Error, &c.Verified, &c.Problem, &c.Chain, &c.Anchor, &c.RunwayDays, &c.LeafDays, &c.Failures, &c.Baseline}
}

// CertificateChecks returns the latest check of every endpoint, by
// address.
func (s *Store) CertificateChecks(ctx context.Context) ([]connector.StoredCertificateCheck, error) {
	rows, err := s.pool.Query(ctx, `select address, checked_at, error, verified, problem, chain, anchor, runway_days, leaf_days, failures, baseline
		from certificate_checks order by address collate "C"`)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, scanned(certificateCheckFields))
}

// PutCertificateCheck stores c over the check of its address.
func (s *Store) PutCertificateCheck(ctx context.Context, c connector.StoredCertificateCheck) error {
	_, err := s.pool.Exec(ctx, `
		insert into certificate_checks (address, checked_at, error, verified, problem, chain, anchor, runway_days, leaf_days, failures, baseline)
		values ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
		on conflict (address) do update set checked_at = excluded.checked_at, error = excluded.error,
			verified = excluded.verified, problem = excluded.problem, chain = excluded.chain, anchor = excluded.anchor,
			runway_days = excluded.runway_days, leaf_days = excluded.leaf_days,
			failures = excluded.failures, baseline = excluded.baseline`,
		c.Address, c.CheckedAt, c.Error, c.Verified, c.Problem, c.Chain, c.Anchor, c.RunwayDays, c.LeafDays, c.Failures, c.Baseline)
	return err
}

// DeleteCertificateCheck deletes the check of address, if there is one.
func (s *Store) DeleteCertificateCheck(ctx context.Context, address string) error {
	_, err := s.pool.Exec(ctx, `delete from certificate_checks where address = $1`, address)
	return err
}
