package postgres

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/dakriy/doorcode/internal/connector"
)

// userColumns lists the users columns in the order userFields reads them.
const userColumns = `users.id, users.email, users.user_id, users.first_name, users.last_name,
	users.display_name, users.password_hash, users.code_hash, users.expires_at, users.deleted_at,
	users.created_at`

func userFields(u *connector.StoredUser) []any {
	return []any{&u.ID, &u.Email, &u.UserID, &u.FirstName, &u.LastName, &u.DisplayName,
		&u.PasswordHash, &u.CodeHash, &u.ExpiresAt, &u.DeletedAt, &u.CreatedAt}
}

// userOrder is the order of users in every list.
const userOrder = `order by lower(users.display_name) collate "C", users.id`

// userFilter keeps the users a UserQuery selects; $1 is its search and $2
// whether it selects deleted users.
const userFilter = `(users.deleted_at is not null) = $2 and strpos(lower(concat_ws(' ',
	users.email, users.user_id, users.first_name, users.last_name, users.display_name)), lower($1)) > 0`

func scanUser(row pgx.CollectableRow) (connector.StoredUser, error) {
	var u connector.StoredUser
	err := row.Scan(userFields(&u)...)
	return u, err
}

// CreateFirstUser inserts u, as a member of the group with groupSlug, when
// the users table is empty. The table lock keeps two servers starting
// together from both inserting.
func (s *Store) CreateFirstUser(ctx context.Context, u connector.StoredUser, groupSlug string) (bool, error) {
	var created int
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `lock table users in exclusive mode`); err != nil {
			return err
		}
		return tx.QueryRow(ctx, `
			with created as (
				insert into users (email, user_id, first_name, last_name, display_name, password_hash,
					code_hash, expires_at, deleted_at, created_at)
				select $1, $2, $3, $4, $5, $6, $7, $8, $9, $10
				where not exists (select 1 from users)
				returning id
			), joined as (
				insert into memberships (group_id, user_id)
				select groups.id, created.id from created join groups on groups.slug = $11
			)
			select count(*) from created`,
			u.Email, u.UserID, u.FirstName, u.LastName, u.DisplayName, u.PasswordHash,
			u.CodeHash, u.ExpiresAt, u.DeletedAt, u.CreatedAt, groupSlug).Scan(&created)
	})
	return created == 1, err
}

// CreateUser inserts u and its memberships and returns its id.
func (s *Store) CreateUser(ctx context.Context, u connector.StoredUser, groupIDs []int64) (int64, error) {
	var id int64
	err := s.pool.QueryRow(ctx, `
		with created as (
			insert into users (email, user_id, first_name, last_name, display_name, password_hash,
				code_hash, expires_at, deleted_at, created_at)
			values ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
			returning id
		), joined as (
			insert into memberships (group_id, user_id)
			select unnest($11::bigint[]), created.id from created
		)
		select id from created`,
		u.Email, u.UserID, u.FirstName, u.LastName, u.DisplayName, u.PasswordHash,
		u.CodeHash, u.ExpiresAt, u.DeletedAt, u.CreatedAt, groupIDs).Scan(&id)
	return id, err
}

// User returns the user with id.
func (s *Store) User(ctx context.Context, id int64) (connector.StoredUser, bool, error) {
	return s.oneUser(ctx, `users.id = $1`, id)
}

// UserByEmail returns the user stored under email.
func (s *Store) UserByEmail(ctx context.Context, email string) (connector.StoredUser, bool, error) {
	return s.oneUser(ctx, `users.email = $1`, email)
}

// UserByUserID returns the user stored under userID.
func (s *Store) UserByUserID(ctx context.Context, userID string) (connector.StoredUser, bool, error) {
	return s.oneUser(ctx, `users.user_id = $1`, userID)
}

// UserByCodeHash returns the user whose door code hash is hash.
func (s *Store) UserByCodeHash(ctx context.Context, hash string) (connector.StoredUser, bool, error) {
	return s.oneUser(ctx, `users.code_hash = $1`, hash)
}

func (s *Store) oneUser(ctx context.Context, where string, arg any) (connector.StoredUser, bool, error) {
	var u connector.StoredUser
	err := s.pool.QueryRow(ctx, `select `+userColumns+` from users where `+where, arg).Scan(userFields(&u)...)
	return s.withGroups(ctx, u, err)
}

// withGroups is u with its groups, when reading u found it.
func (s *Store) withGroups(ctx context.Context, u connector.StoredUser, err error) (connector.StoredUser, bool, error) {
	u, ok, err := found(u, err)
	if !ok {
		return u, false, err
	}
	users, err := s.attachGroups(ctx, []connector.StoredUser{u})
	return users[0], err == nil, err
}

// Users returns the page of users q selects and how many it selects.
func (s *Store) Users(ctx context.Context, q connector.UserQuery) ([]connector.StoredUser, int, error) {
	var total int
	err := s.pool.QueryRow(ctx, `select count(*) from users where `+userFilter, q.Search, q.Deleted).Scan(&total)
	if err != nil {
		return nil, 0, err
	}
	rows, err := s.pool.Query(ctx, `select `+userColumns+` from users where `+userFilter+` `+userOrder+`
		offset $3 limit $4`, q.Search, q.Deleted, q.Offset, q.Limit)
	if err != nil {
		return nil, 0, err
	}
	users, err := s.collectUsers(ctx, rows)
	return users, total, err
}

// collectUsers reads users from rows and attaches their groups.
func (s *Store) collectUsers(ctx context.Context, rows pgx.Rows) ([]connector.StoredUser, error) {
	users, err := pgx.CollectRows(rows, scanUser)
	if err != nil {
		return nil, err
	}
	return s.attachGroups(ctx, users)
}

type membership struct {
	userID int64
	group  connector.UserGroup
}

// attachGroups returns users with the groups each is a member of.
func (s *Store) attachGroups(ctx context.Context, users []connector.StoredUser) ([]connector.StoredUser, error) {
	ids := make([]int64, 0, len(users))
	for _, u := range users {
		ids = append(ids, u.ID)
	}
	rows, err := s.pool.Query(ctx, `
		select memberships.user_id, groups.id, groups.slug, groups.name, groups.roles
		from memberships join groups on groups.id = memberships.group_id
		where memberships.user_id = any($1)
		order by lower(groups.name) collate "C", groups.id`, ids)
	if err != nil {
		return nil, err
	}
	held, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (membership, error) {
		var m membership
		err := row.Scan(&m.userID, &m.group.ID, &m.group.Slug, &m.group.Name, &m.group.Roles)
		return m, err
	})
	byUser := map[int64][]connector.UserGroup{}
	for _, m := range held {
		byUser[m.userID] = append(byUser[m.userID], m.group)
	}
	for i := range users {
		users[i].Groups = byUser[users[i].ID]
	}
	return users, err
}

// UpdateUser stores u's profile fields over the user with u.ID.
func (s *Store) UpdateUser(ctx context.Context, u connector.StoredUser) (bool, error) {
	tag, err := s.pool.Exec(ctx, `
		update users set email = $2, user_id = $3, first_name = $4, last_name = $5,
			display_name = $6, expires_at = $7
		where id = $1`,
		u.ID, u.Email, u.UserID, u.FirstName, u.LastName, u.DisplayName, u.ExpiresAt)
	return tag.RowsAffected() == 1, err
}

// SetUserDeleted stores the deletion time of the user with id.
func (s *Store) SetUserDeleted(ctx context.Context, id int64, at *time.Time) (bool, error) {
	tag, err := s.pool.Exec(ctx, `update users set deleted_at = $2 where id = $1`, id, at)
	return tag.RowsAffected() == 1, err
}

// SetCodeHash stores the door code hash of the user with id.
func (s *Store) SetCodeHash(ctx context.Context, id int64, hash *string) (bool, error) {
	tag, err := s.pool.Exec(ctx, `update users set code_hash = $2 where id = $1`, id, hash)
	return tag.RowsAffected() == 1, err
}

// CodeHashing inserts candidate as the code hashing parameters unless a row
// exists, and returns the row's parameters. Within one statement the select
// does not see the insert, so exactly one branch of the union has a row.
func (s *Store) CodeHashing(ctx context.Context, candidate string) (string, error) {
	var stored string
	err := s.pool.QueryRow(ctx, `
		with inserted as (
			insert into code_hashing (parameters) values ($1)
			on conflict do nothing
			returning parameters
		)
		select parameters from inserted
		union all
		select parameters from code_hashing`, candidate).Scan(&stored)
	return stored, err
}
