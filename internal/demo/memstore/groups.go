package memstore

import (
	"context"
	"maps"
	"slices"

	"github.com/dakriy/doorcode/internal/connector"
)

// groupOut is a copy of g with its member count.
func (s *Store) groupOut(g connector.StoredGroup) connector.StoredGroup {
	g.Roles = list(g.Roles)
	g.PreassignedUserIDs = list(g.PreassignedUserIDs)
	g.MemberCount = 0
	for m := range s.members {
		if m.a == g.ID {
			g.MemberCount++
		}
	}
	return g
}

// groupBySlug returns the group whose slug is slug.
func (s *Store) groupBySlug(slug string) (connector.StoredGroup, bool) {
	for _, g := range s.groups {
		if g.Slug == slug {
			return g, true
		}
	}
	return connector.StoredGroup{}, false
}

// slugTaken reports whether a group other than the one whose id is id has
// slug.
func (s *Store) slugTaken(slug string, id int64) bool {
	g, ok := s.groupBySlug(slug)
	return ok && g.ID != id
}

// Groups returns every group in name order.
func (s *Store) Groups(_ context.Context) ([]connector.StoredGroup, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	groups := []connector.StoredGroup{}
	for _, g := range s.groups {
		groups = append(groups, s.groupOut(g))
	}
	byName(groups, func(g connector.StoredGroup) string { return g.Name }, func(g connector.StoredGroup) int64 { return g.ID })
	return groups, nil
}

// Group returns the group whose id is id.
func (s *Store) Group(_ context.Context, id int64) (connector.StoredGroup, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	g, ok := s.groups[id]
	if !ok {
		return connector.StoredGroup{}, false, nil
	}
	return s.groupOut(g), true, nil
}

// GroupBySlug returns the group whose slug is slug.
func (s *Store) GroupBySlug(_ context.Context, slug string) (connector.StoredGroup, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	g, ok := s.groupBySlug(slug)
	if !ok {
		return connector.StoredGroup{}, false, nil
	}
	return s.groupOut(g), true, nil
}

// storedGroup is a copy of g as the store keeps it.
func storedGroup(g connector.StoredGroup) (connector.StoredGroup, error) {
	if g.Roles == nil || g.PreassignedUserIDs == nil {
		return g, errNil
	}
	g.Roles = list(g.Roles)
	g.PreassignedUserIDs = list(g.PreassignedUserIDs)
	g.MemberCount = 0
	return g, nil
}

// CreateGroup stores g and returns its id.
func (s *Store) CreateGroup(_ context.Context, g connector.StoredGroup) (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	g, err := storedGroup(g)
	if err != nil {
		return 0, err
	}
	if s.slugTaken(g.Slug, 0) {
		return 0, errDuplicate
	}
	g.ID = s.next("groups")
	s.groups[g.ID] = g
	return g.ID, nil
}

// UpdateGroup stores g over the group whose id is g.ID.
func (s *Store) UpdateGroup(_ context.Context, g connector.StoredGroup) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.groups[g.ID]; !ok {
		return false, nil
	}
	g, err := storedGroup(g)
	if err != nil {
		return false, err
	}
	if s.slugTaken(g.Slug, g.ID) {
		return false, errDuplicate
	}
	s.groups[g.ID] = g
	return true, nil
}

// DeleteGroup removes the group, its memberships, and its access rules.
func (s *Store) DeleteGroup(_ context.Context, id int64) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.groups[id]; !ok {
		return false, nil
	}
	delete(s.groups, id)
	maps.DeleteFunc(s.members, func(m pair, _ bool) bool { return m.a == id })
	maps.DeleteFunc(s.rules, func(_ int64, r connector.StoredRule) bool { return r.GroupID == id })
	return true, nil
}

// GroupMembers returns the group's members in user order.
func (s *Store) GroupMembers(_ context.Context, id int64) ([]connector.StoredUser, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var ids []int64
	for m := range s.members {
		if m.a == id {
			ids = append(ids, m.b)
		}
	}
	return s.usersOut(ids), nil
}

// AddMember makes the user a member of the group.
func (s *Store) AddMember(_ context.Context, groupID, userID int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, group := s.groups[groupID]
	_, user := s.users[userID]
	if !group || !user {
		return errNoReference
	}
	s.members[pair{groupID, userID}] = true
	return nil
}

// RemoveMember ends the user's membership of the group.
func (s *Store) RemoveMember(_ context.Context, groupID, userID int64) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	m := pair{groupID, userID}
	had := s.members[m]
	delete(s.members, m)
	return had, nil
}

// Preassign adds userID to the group's pre-assigned user ids once.
func (s *Store) Preassign(_ context.Context, groupID int64, userID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	g, ok := s.groups[groupID]
	if !ok || slices.Contains(g.PreassignedUserIDs, userID) {
		return nil
	}
	g.PreassignedUserIDs = append(list(g.PreassignedUserIDs), userID)
	s.groups[groupID] = g
	return nil
}
