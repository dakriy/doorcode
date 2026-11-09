package memstore

import (
	"context"
	"time"

	"github.com/dakriy/doorcode/internal/connector"
)

// storedUser is a copy of u as the store keeps it, without groups.
func storedUser(u connector.StoredUser) connector.StoredUser {
	u.UserID = clone(u.UserID)
	u.PasswordHash = clone(u.PasswordHash)
	u.CodeHash = clone(u.CodeHash)
	u.ExpiresAt = clone(u.ExpiresAt)
	u.DeletedAt = clone(u.DeletedAt)
	u.Groups = nil
	return u
}

// userOut is a copy of the user whose id is id with the groups it is a
// member of, ordered by name.
func (s *Store) userOut(id int64) connector.StoredUser {
	u := storedUser(s.users[id])
	var groups []connector.UserGroup
	for m := range s.members {
		if m.b != id {
			continue
		}
		g := s.groups[m.a]
		groups = append(groups, connector.UserGroup{ID: g.ID, Slug: g.Slug, Name: g.Name, Roles: list(g.Roles)})
	}
	byName(groups, func(g connector.UserGroup) string { return g.Name }, func(g connector.UserGroup) int64 { return g.ID })
	u.Groups = groups
	return u
}

// usersOut is a copy of each of ids' users, in the order lists of users
// take.
func (s *Store) usersOut(ids []int64) []connector.StoredUser {
	users := []connector.StoredUser{}
	for _, id := range ids {
		users = append(users, s.userOut(id))
	}
	byName(users, func(u connector.StoredUser) string { return u.DisplayName }, func(u connector.StoredUser) int64 { return u.ID })
	return users
}

// equalPtr reports whether a and b are both set to the same value.
func equalPtr(a, b *string) bool {
	return a != nil && b != nil && *a == *b
}

// userClash reports whether u would take the email, user id, or code hash
// of a user other than itself.
func (s *Store) userClash(u connector.StoredUser) bool {
	for id, other := range s.users {
		if id == u.ID {
			continue
		}
		if other.Email == u.Email || equalPtr(other.UserID, u.UserID) || equalPtr(other.CodeHash, u.CodeHash) {
			return true
		}
	}
	return false
}

// insertUser stores u under a new id and makes it a member of groupIDs.
func (s *Store) insertUser(u connector.StoredUser, groupIDs []int64) (int64, error) {
	u.ID = 0
	if s.userClash(u) {
		return 0, errDuplicate
	}
	if !allExist(s.groups, groupIDs) {
		return 0, errNoReference
	}
	u = storedUser(u)
	u.ID = s.next("users")
	s.users[u.ID] = u
	for _, g := range groupIDs {
		s.members[pair{g, u.ID}] = true
	}
	return u.ID, nil
}

// CreateFirstUser stores u as a member of the group whose slug is
// groupSlug when there is no user yet.
func (s *Store) CreateFirstUser(_ context.Context, u connector.StoredUser, groupSlug string) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.users) > 0 {
		return false, nil
	}
	var groupIDs []int64
	if g, ok := s.groupBySlug(groupSlug); ok {
		groupIDs = append(groupIDs, g.ID)
	}
	_, err := s.insertUser(u, groupIDs)
	return err == nil, err
}

// CreateUser stores u as a member of groupIDs.
func (s *Store) CreateUser(_ context.Context, u connector.StoredUser, groupIDs []int64) (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.insertUser(u, groupIDs)
}

// User returns the user whose id is id.
func (s *Store) User(_ context.Context, id int64) (connector.StoredUser, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.users[id]; !ok {
		return connector.StoredUser{}, false, nil
	}
	return s.userOut(id), true, nil
}

// userWhere returns the user keep holds for, if any.
func (s *Store) userWhere(keep func(connector.StoredUser) bool) (connector.StoredUser, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for id, u := range s.users {
		if keep(u) {
			return s.userOut(id), true, nil
		}
	}
	return connector.StoredUser{}, false, nil
}

// UserByEmail returns the user whose email is email.
func (s *Store) UserByEmail(_ context.Context, email string) (connector.StoredUser, bool, error) {
	return s.userWhere(func(u connector.StoredUser) bool { return u.Email == email })
}

// UserByUserID returns the user whose user id is userID.
func (s *Store) UserByUserID(_ context.Context, userID string) (connector.StoredUser, bool, error) {
	return s.userWhere(func(u connector.StoredUser) bool { return equalPtr(u.UserID, &userID) })
}

// UserByCodeHash returns the user whose door code hash is hash.
func (s *Store) UserByCodeHash(_ context.Context, hash string) (connector.StoredUser, bool, error) {
	return s.userWhere(func(u connector.StoredUser) bool { return equalPtr(u.CodeHash, &hash) })
}

// selects reports whether q keeps u.
func selects(q connector.UserQuery, u connector.StoredUser) bool {
	if (u.DeletedAt != nil) != q.Deleted {
		return false
	}
	fields := []string{u.Email}
	if u.UserID != nil {
		fields = append(fields, *u.UserID)
	}
	fields = append(fields, u.FirstName, u.LastName, u.DisplayName)
	return contains(q.Search, fields...)
}

// Users returns the page of users q selects and how many it selects.
func (s *Store) Users(_ context.Context, q connector.UserQuery) ([]connector.StoredUser, int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var ids []int64
	for id, u := range s.users {
		if selects(q, u) {
			ids = append(ids, id)
		}
	}
	users := s.usersOut(ids)
	return page(users, q.Offset, q.Limit), len(users), nil
}

// UpdateUser stores u's email, user id, names, and expiry.
func (s *Store) UpdateUser(_ context.Context, u connector.StoredUser) (bool, error) {
	return s.changeUser(u.ID, func(stored *connector.StoredUser) {
		stored.Email = u.Email
		stored.UserID = clone(u.UserID)
		stored.FirstName = u.FirstName
		stored.LastName = u.LastName
		stored.DisplayName = u.DisplayName
		stored.ExpiresAt = clone(u.ExpiresAt)
	})
}

// changeUser applies change to the user whose id is id, when there is one
// and the changed user takes no other user's unique values.
func (s *Store) changeUser(id int64, change func(u *connector.StoredUser)) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	u, ok := s.users[id]
	if !ok {
		return false, nil
	}
	change(&u)
	if s.userClash(u) {
		return false, errDuplicate
	}
	s.users[id] = u
	return true, nil
}

// SetUserDeleted stores at as the user's deletion time.
func (s *Store) SetUserDeleted(_ context.Context, id int64, at *time.Time) (bool, error) {
	return s.changeUser(id, func(u *connector.StoredUser) { u.DeletedAt = clone(at) })
}

// SetCodeHash stores hash as the user's door code hash.
func (s *Store) SetCodeHash(_ context.Context, id int64, hash *string) (bool, error) {
	return s.changeUser(id, func(u *connector.StoredUser) { u.CodeHash = clone(hash) })
}

// SetPasswordHash stores hash as the user's local password hash.
func (s *Store) SetPasswordHash(_ context.Context, id int64, hash string) (bool, error) {
	return s.changeUser(id, func(u *connector.StoredUser) { u.PasswordHash = &hash })
}
