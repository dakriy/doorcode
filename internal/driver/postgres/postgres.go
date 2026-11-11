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

// pingTimeout is how long Ping waits for the database's answer.
const pingTimeout = 2 * time.Second

// Ping runs select 1, waiting at most pingTimeout.
func (s *Store) Ping(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, pingTimeout)
	defer cancel()
	_, err := s.pool.Exec(ctx, "select 1")
	return err
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

// SiteImage returns the stored site picture of kind.
func (s *Store) SiteImage(ctx context.Context, kind connector.ImageKind) (connector.StoredImage, bool, error) {
	var image connector.StoredImage
	err := s.pool.QueryRow(ctx, `select image, updated_at from site_images where kind = $1`, kind).Scan(&image.Data, &image.UpdatedAt)
	return found(image, err)
}

// SiteImageStoredAt returns when the site picture of kind was stored.
func (s *Store) SiteImageStoredAt(ctx context.Context, kind connector.ImageKind) (time.Time, bool, error) {
	var at time.Time
	err := s.pool.QueryRow(ctx, `select updated_at from site_images where kind = $1`, kind).Scan(&at)
	return found(at, err)
}

// PutSiteImage inserts or replaces the site picture of kind.
func (s *Store) PutSiteImage(ctx context.Context, kind connector.ImageKind, image connector.StoredImage) error {
	_, err := s.pool.Exec(ctx, `
		insert into site_images (kind, image, updated_at) values ($1, $2, $3)
		on conflict (kind) do update set image = excluded.image, updated_at = excluded.updated_at`,
		kind, image.Data, image.UpdatedAt)
	return err
}

// RemoveSiteImage deletes the site picture of kind.
func (s *Store) RemoveSiteImage(ctx context.Context, kind connector.ImageKind) (bool, error) {
	tag, err := s.pool.Exec(ctx, `delete from site_images where kind = $1`, kind)
	return tag.RowsAffected() == 1, err
}

// CreateSession inserts a session.
func (s *Store) CreateSession(ctx context.Context, session connector.StoredSession) error {
	_, err := s.pool.Exec(ctx, `
		insert into sessions (token_hash, user_id, created_at, expires_at, via_identity) values ($1, $2, $3, $4, $5)`,
		session.TokenHash, session.UserID, session.CreatedAt, session.ExpiresAt, session.ViaIdentity)
	return err
}

// DeleteExpiredSessions deletes the sessions expiring at or before now.
func (s *Store) DeleteExpiredSessions(ctx context.Context, now time.Time) (int, error) {
	tag, err := s.pool.Exec(ctx, `delete from sessions where expires_at <= $1`, now)
	return int(tag.RowsAffected()), err
}

// SessionUser returns the session stored under tokenHash and its user.
func (s *Store) SessionUser(ctx context.Context, tokenHash []byte) (connector.StoredSession, connector.StoredUser, bool, error) {
	var session connector.StoredSession
	var u connector.StoredUser
	dest := append([]any{&session.TokenHash, &session.UserID, &session.CreatedAt, &session.ExpiresAt, &session.ViaIdentity}, userFields(&u)...)
	err := s.pool.QueryRow(ctx, `
		select sessions.token_hash, sessions.user_id, sessions.created_at, sessions.expires_at, sessions.via_identity, `+userColumns+`
		from sessions join users on users.id = sessions.user_id
		where sessions.token_hash = $1`, tokenHash).Scan(dest...)
	u, ok, err := s.withGroups(ctx, u, err)
	return session, u, ok, err
}

// DeleteSession removes the session stored under tokenHash.
func (s *Store) DeleteSession(ctx context.Context, tokenHash []byte) error {
	_, err := s.pool.Exec(ctx, `delete from sessions where token_hash = $1`, tokenHash)
	return err
}

// tokenColumns lists the api_tokens columns in the order tokenFields reads
// them.
const tokenColumns = `api_tokens.id, api_tokens.user_id, api_tokens.name, api_tokens.token_hash,
	api_tokens.created_at, api_tokens.expires_at`

func tokenFields(t *connector.StoredToken) []any {
	return []any{&t.ID, &t.UserID, &t.Name, &t.TokenHash, &t.CreatedAt, &t.ExpiresAt}
}

// CreateToken inserts a token and returns its id.
func (s *Store) CreateToken(ctx context.Context, t connector.StoredToken) (int64, error) {
	var id int64
	err := s.pool.QueryRow(ctx, `
		insert into api_tokens (user_id, name, token_hash, created_at, expires_at)
		values ($1, $2, $3, $4, $5) returning id`,
		t.UserID, t.Name, t.TokenHash, t.CreatedAt, t.ExpiresAt).Scan(&id)
	return id, err
}

// UserTokens returns the user's tokens in id order.
func (s *Store) UserTokens(ctx context.Context, userID int64) ([]connector.StoredToken, error) {
	rows, err := s.pool.Query(ctx, `select `+tokenColumns+` from api_tokens where user_id = $1 order by id`, userID)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, scanned(tokenFields))
}

// TokenUser returns the token stored under tokenHash and its user.
func (s *Store) TokenUser(ctx context.Context, tokenHash []byte) (connector.StoredToken, connector.StoredUser, bool, error) {
	var t connector.StoredToken
	var u connector.StoredUser
	err := s.pool.QueryRow(ctx, `
		select `+tokenColumns+`, `+userColumns+`
		from api_tokens join users on users.id = api_tokens.user_id
		where api_tokens.token_hash = $1`, tokenHash).Scan(append(tokenFields(&t), userFields(&u)...)...)
	u, ok, err := s.withGroups(ctx, u, err)
	return t, u, ok, err
}

// DeleteToken removes the user's token with id.
func (s *Store) DeleteToken(ctx context.Context, userID, id int64) (bool, error) {
	tag, err := s.pool.Exec(ctx, `delete from api_tokens where id = $1 and user_id = $2`, id, userID)
	return tag.RowsAffected() == 1, err
}

// scanned reads a row into a T through the pointers fields gives for its
// columns.
func scanned[T any](fields func(*T) []any) pgx.RowToFunc[T] {
	return func(row pgx.CollectableRow) (T, error) {
		var value T
		err := row.Scan(fields(&value)...)
		return value, err
	}
}

// found translates pgx's no-rows error into a not-found result.
func found[T any](value T, err error) (T, bool, error) {
	if errors.Is(err, pgx.ErrNoRows) {
		return value, false, nil
	}
	return value, err == nil, err
}
