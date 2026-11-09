package postgres

import (
	"context"

	"github.com/jackc/pgx/v5"

	"github.com/dakriy/doorcode/internal/connector"
)

// groupColumns lists the groups columns in the order groupFields reads them.
const groupColumns = `groups.id, groups.slug, groups.name, groups.description, groups.roles,
	groups.preassigned_user_ids,
	(select count(*) from memberships where memberships.group_id = groups.id)`

func groupFields(g *connector.StoredGroup) []any {
	return []any{&g.ID, &g.Slug, &g.Name, &g.Description, &g.Roles, &g.PreassignedUserIDs, &g.MemberCount}
}

// Groups returns every group in name order.
func (s *Store) Groups(ctx context.Context) ([]connector.StoredGroup, error) {
	rows, err := s.pool.Query(ctx, `select `+groupColumns+` from groups order by lower(groups.name) collate "C", groups.id`)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, scanned(groupFields))
}

// Group returns the group with id.
func (s *Store) Group(ctx context.Context, id int64) (connector.StoredGroup, bool, error) {
	return s.oneGroup(ctx, `groups.id = $1`, id)
}

// GroupBySlug returns the group stored under slug.
func (s *Store) GroupBySlug(ctx context.Context, slug string) (connector.StoredGroup, bool, error) {
	return s.oneGroup(ctx, `groups.slug = $1`, slug)
}

func (s *Store) oneGroup(ctx context.Context, where string, arg any) (connector.StoredGroup, bool, error) {
	var g connector.StoredGroup
	err := s.pool.QueryRow(ctx, `select `+groupColumns+` from groups where `+where, arg).Scan(groupFields(&g)...)
	return found(g, err)
}

// CreateGroup inserts g and returns its id.
func (s *Store) CreateGroup(ctx context.Context, g connector.StoredGroup) (int64, error) {
	var id int64
	err := s.pool.QueryRow(ctx, `
		insert into groups (slug, name, description, roles, preassigned_user_ids)
		values ($1, $2, $3, $4, $5) returning id`,
		g.Slug, g.Name, g.Description, g.Roles, g.PreassignedUserIDs).Scan(&id)
	return id, err
}

// UpdateGroup stores g over the group with g.ID.
func (s *Store) UpdateGroup(ctx context.Context, g connector.StoredGroup) (bool, error) {
	tag, err := s.pool.Exec(ctx, `
		update groups set slug = $2, name = $3, description = $4, roles = $5, preassigned_user_ids = $6
		where id = $1`,
		g.ID, g.Slug, g.Name, g.Description, g.Roles, g.PreassignedUserIDs)
	return tag.RowsAffected() == 1, err
}

// DeleteGroup removes the group with id; its memberships go with it.
func (s *Store) DeleteGroup(ctx context.Context, id int64) (bool, error) {
	tag, err := s.pool.Exec(ctx, `delete from groups where id = $1`, id)
	return tag.RowsAffected() == 1, err
}

// GroupMembers returns the group's members in user order.
func (s *Store) GroupMembers(ctx context.Context, id int64) ([]connector.StoredUser, error) {
	rows, err := s.pool.Query(ctx, `
		select `+userColumns+` from users join memberships on memberships.user_id = users.id
		where memberships.group_id = $1 `+userOrder, id)
	if err != nil {
		return nil, err
	}
	return s.collectUsers(ctx, rows)
}

// AddMember inserts the membership unless it exists.
func (s *Store) AddMember(ctx context.Context, groupID, userID int64) error {
	_, err := s.pool.Exec(ctx, `
		insert into memberships (group_id, user_id) values ($1, $2) on conflict do nothing`, groupID, userID)
	return err
}

// RemoveMember deletes the membership.
func (s *Store) RemoveMember(ctx context.Context, groupID, userID int64) (bool, error) {
	tag, err := s.pool.Exec(ctx, `delete from memberships where group_id = $1 and user_id = $2`, groupID, userID)
	return tag.RowsAffected() == 1, err
}

// Preassign appends userID to the group's pre-assigned user ids unless
// they hold it.
func (s *Store) Preassign(ctx context.Context, groupID int64, userID string) error {
	_, err := s.pool.Exec(ctx, `
		update groups set preassigned_user_ids = array_append(preassigned_user_ids, $2)
		where id = $1 and not ($2 = any(preassigned_user_ids))`, groupID, userID)
	return err
}

// RemovePreassigned removes userID from the group's pre-assigned user ids.
func (s *Store) RemovePreassigned(ctx context.Context, groupID int64, userID string) error {
	_, err := s.pool.Exec(ctx, `
		update groups set preassigned_user_ids = array_remove(preassigned_user_ids, $2)
		where id = $1`, groupID, userID)
	return err
}
