package postgres

import (
	"context"

	"github.com/jackc/pgx/v5"

	"github.com/dakriy/doorcode/internal/connector"
)

// requestColumns lists the access_requests columns in the order
// requestFields reads them.
const requestColumns = `id, user_id, title, body, reply, resolved_at, created_at, updated_at`

func requestFields(r *connector.StoredRequest) []any {
	return []any{&r.ID, &r.UserID, &r.Title, &r.Body, &r.Reply, &r.ResolvedAt, &r.CreatedAt, &r.UpdatedAt}
}

// CreateRequest inserts r and returns its id.
func (s *Store) CreateRequest(ctx context.Context, r connector.StoredRequest) (int64, error) {
	var id int64
	err := s.pool.QueryRow(ctx, `
		insert into access_requests (user_id, title, body, reply, resolved_at, created_at, updated_at)
		values ($1, $2, $3, $4, $5, $6, $7) returning id`,
		r.UserID, r.Title, r.Body, r.Reply, r.ResolvedAt, r.CreatedAt, r.UpdatedAt).Scan(&id)
	return id, err
}

// Request returns the request with id.
func (s *Store) Request(ctx context.Context, id int64) (connector.StoredRequest, bool, error) {
	var r connector.StoredRequest
	err := s.pool.QueryRow(ctx, `select `+requestColumns+` from access_requests where id = $1`, id).Scan(requestFields(&r)...)
	return found(r, err)
}

// UserRequests returns the requests of the user with userID, newest first.
func (s *Store) UserRequests(ctx context.Context, userID int64) ([]connector.StoredRequest, error) {
	rows, err := s.pool.Query(ctx, `select `+requestColumns+` from access_requests where user_id = $1
		order by created_at desc, id desc`, userID)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, scanned(requestFields))
}

// UpdateRequest stores r's title, body, and update time over the request
// with r.ID.
func (s *Store) UpdateRequest(ctx context.Context, r connector.StoredRequest) (bool, error) {
	tag, err := s.pool.Exec(ctx, `update access_requests set title = $2, body = $3, updated_at = $4 where id = $1`,
		r.ID, r.Title, r.Body, r.UpdatedAt)
	return tag.RowsAffected() == 1, err
}

// DeleteRequest removes the request with id.
func (s *Store) DeleteRequest(ctx context.Context, id int64) (bool, error) {
	tag, err := s.pool.Exec(ctx, `delete from access_requests where id = $1`, id)
	return tag.RowsAffected() == 1, err
}
