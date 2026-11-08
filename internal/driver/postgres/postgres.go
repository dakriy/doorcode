// Package postgres implements connector.Store over PostgreSQL with pgx.
package postgres

import (
	"context"
	"errors"
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

// userColumns lists the users columns in the order scanUser reads them.
const userColumns = `users.id, users.email, users.display_name, users.password_hash,
	users.roles, users.expires_at, users.deleted_at, users.created_at`

func userFields(u *connector.StoredUser) []any {
	return []any{&u.ID, &u.Email, &u.DisplayName, &u.PasswordHash, &u.Roles, &u.ExpiresAt, &u.DeletedAt, &u.CreatedAt}
}

// CreateFirstUser inserts u when the users table is empty. The table lock
// keeps two servers starting together from both inserting.
func (s *Store) CreateFirstUser(ctx context.Context, u connector.StoredUser) (bool, error) {
	var created bool
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `lock table users in exclusive mode`); err != nil {
			return err
		}
		tag, err := tx.Exec(ctx, `
			insert into users (email, display_name, password_hash, roles, expires_at, deleted_at, created_at)
			select $1, $2, $3, $4, $5, $6, $7
			where not exists (select 1 from users)`,
			u.Email, u.DisplayName, u.PasswordHash, u.Roles, u.ExpiresAt, u.DeletedAt, u.CreatedAt)
		created = tag.RowsAffected() == 1
		return err
	})
	return created, err
}

// UserByEmail returns the user stored under email.
func (s *Store) UserByEmail(ctx context.Context, email string) (connector.StoredUser, bool, error) {
	var u connector.StoredUser
	err := s.pool.QueryRow(ctx, `select `+userColumns+` from users where email = $1`, email).Scan(userFields(&u)...)
	return found(u, err)
}

// CreateSession inserts a session.
func (s *Store) CreateSession(ctx context.Context, session connector.StoredSession) error {
	_, err := s.pool.Exec(ctx, `
		insert into sessions (token_hash, user_id, created_at, expires_at) values ($1, $2, $3, $4)`,
		session.TokenHash, session.UserID, session.CreatedAt, session.ExpiresAt)
	return err
}

// SessionUser returns the session stored under tokenHash and its user.
func (s *Store) SessionUser(ctx context.Context, tokenHash []byte) (connector.StoredSession, connector.StoredUser, bool, error) {
	var session connector.StoredSession
	var u connector.StoredUser
	dest := append([]any{&session.TokenHash, &session.UserID, &session.CreatedAt, &session.ExpiresAt}, userFields(&u)...)
	err := s.pool.QueryRow(ctx, `
		select sessions.token_hash, sessions.user_id, sessions.created_at, sessions.expires_at, `+userColumns+`
		from sessions join users on users.id = sessions.user_id
		where sessions.token_hash = $1`, tokenHash).Scan(dest...)
	u, ok, err := found(u, err)
	return session, u, ok, err
}

// DeleteSession removes the session stored under tokenHash.
func (s *Store) DeleteSession(ctx context.Context, tokenHash []byte) error {
	_, err := s.pool.Exec(ctx, `delete from sessions where token_hash = $1`, tokenHash)
	return err
}

// found translates pgx's no-rows error into a not-found result.
func found[T any](value T, err error) (T, bool, error) {
	if errors.Is(err, pgx.ErrNoRows) {
		return value, false, nil
	}
	return value, err == nil, err
}
