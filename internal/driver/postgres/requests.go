package postgres

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/dakriy/doorcode/internal/connector"
)

// requestColumns lists the access_requests columns, with the display name
// of each one's user, in the order requestFields reads them.
const requestColumns = `access_requests.id, access_requests.user_id, users.display_name, access_requests.title,
	access_requests.body, access_requests.reply, access_requests.resolved_at, access_requests.created_at, access_requests.updated_at`

// requestSource is the access_requests table joined to their users.
const requestSource = ` from access_requests join users on users.id = access_requests.user_id`

// requestOrder puts requests newest first.
const requestOrder = ` order by access_requests.created_at desc, access_requests.id desc`

// requestFilter keeps the requests a RequestQuery selects: $1 its search,
// $2 whether it is resolved, $3 its user.
const requestFilter = ` where strpos(lower(concat_ws(' ', access_requests.title, access_requests.body,
	users.display_name, users.email)), lower($1)) > 0
	and ($2::boolean is null or (access_requests.resolved_at is not null) = $2)
	and ($3::bigint = 0 or access_requests.user_id = $3)`

func requestFields(r *connector.StoredRequest) []any {
	return []any{&r.ID, &r.UserID, &r.UserName, &r.Title, &r.Body, &r.Reply, &r.ResolvedAt, &r.CreatedAt, &r.UpdatedAt}
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
	err := s.pool.QueryRow(ctx, `select `+requestColumns+requestSource+` where access_requests.id = $1`, id).Scan(requestFields(&r)...)
	return found(r, err)
}

// UserRequests returns the requests of the user with userID, newest first.
func (s *Store) UserRequests(ctx context.Context, userID int64) ([]connector.StoredRequest, error) {
	rows, err := s.pool.Query(ctx, `select `+requestColumns+requestSource+` where access_requests.user_id = $1`+requestOrder, userID)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, scanned(requestFields))
}

// Requests returns the page of requests q selects, newest first, and how
// many it selects in all.
func (s *Store) Requests(ctx context.Context, q connector.RequestQuery) ([]connector.StoredRequest, int, error) {
	args := []any{q.Search, q.Resolved, q.UserID}
	var total int
	if err := s.pool.QueryRow(ctx, `select count(*)`+requestSource+requestFilter, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	rows, err := s.pool.Query(ctx, `select `+requestColumns+requestSource+requestFilter+requestOrder+` offset $4 limit $5`,
		append(args, q.Offset, q.Limit)...)
	if err != nil {
		return nil, 0, err
	}
	requests, err := pgx.CollectRows(rows, scanned(requestFields))
	return requests, total, err
}

// UpdateRequest stores r's title, body, and update time over the request
// with r.ID.
func (s *Store) UpdateRequest(ctx context.Context, r connector.StoredRequest) (bool, error) {
	tag, err := s.pool.Exec(ctx, `update access_requests set title = $2, body = $3, updated_at = $4 where id = $1`,
		r.ID, r.Title, r.Body, r.UpdatedAt)
	return tag.RowsAffected() == 1, err
}

// SetRequestReply stores reply as the answer to the request with id.
func (s *Store) SetRequestReply(ctx context.Context, id int64, reply string) (bool, error) {
	tag, err := s.pool.Exec(ctx, `update access_requests set reply = $2 where id = $1`, id, reply)
	return tag.RowsAffected() == 1, err
}

// SetRequestResolved stores at as when the request with id was resolved.
func (s *Store) SetRequestResolved(ctx context.Context, id int64, at *time.Time) (bool, error) {
	tag, err := s.pool.Exec(ctx, `update access_requests set resolved_at = $2 where id = $1`, id, at)
	return tag.RowsAffected() == 1, err
}

// DeleteRequest removes the request with id.
func (s *Store) DeleteRequest(ctx context.Context, id int64) (bool, error) {
	tag, err := s.pool.Exec(ctx, `delete from access_requests where id = $1`, id)
	return tag.RowsAffected() == 1, err
}
