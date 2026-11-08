// Package postgres implements connector.Store over PostgreSQL with pgx.
package postgres

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jackc/tern/v2/migrate"

	"github.com/dakriy/doorcode/internal/connector"
	"github.com/dakriy/doorcode/internal/driver/postgres/migrations"
)

// versionTable records which migrations have been applied.
const versionTable = "schema_version"

// Store is a connection pool to one database.
type Store struct {
	pool *pgxpool.Pool
}

// Open connects to the database at url.
func Open(ctx context.Context, url string) (*Store, error) {
	pool, err := pgxpool.New(ctx, url)
	if err != nil {
		return nil, err
	}
	return &Store{pool: pool}, nil
}

// Close releases every connection.
func (s *Store) Close() {
	s.pool.Close()
}

// Migrate applies every embedded migration not yet recorded in the version
// table, holding tern's advisory lock while it does.
func (s *Store) Migrate(ctx context.Context) error {
	conn, err := s.pool.Acquire(ctx)
	if err != nil {
		return err
	}
	defer conn.Release()
	m, err := migrate.NewMigrator(ctx, conn.Conn(), versionTable)
	if err != nil {
		return err
	}
	if err := m.LoadMigrations(migrations.FS); err != nil {
		return err
	}
	return m.Migrate(ctx)
}

// Settings returns every stored setting keyed by setting key.
func (s *Store) Settings(ctx context.Context) (map[string]connector.StoredSetting, error) {
	rows, err := s.pool.Query(ctx, `select key, value, updated_at from settings`)
	if err != nil {
		return nil, err
	}
	stored := map[string]connector.StoredSetting{}
	var key string
	var row connector.StoredSetting
	_, err = pgx.ForEachRow(rows, []any{&key, &row.Value, &row.UpdatedAt}, func() error {
		stored[key] = row
		return nil
	})
	return stored, err
}

// PutSetting inserts or replaces the setting stored under key.
func (s *Store) PutSetting(ctx context.Context, key, value string, at time.Time) error {
	_, err := s.pool.Exec(ctx, `
		insert into settings (key, value, updated_at) values ($1, $2, $3)
		on conflict (key) do update set value = excluded.value, updated_at = excluded.updated_at`,
		key, value, at)
	return err
}
