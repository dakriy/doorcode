package testsupport

import (
	"cmp"
	"context"
	"slices"
	"strings"
	"time"

	"github.com/dakriy/doorcode/internal/connector"
)

// membership is one user's membership of one group.
type membership struct {
	group int64
	user  int64
}

// CreateFirstUser stores u with the next id, as a member of the group with
// groupSlug if there is one, when no user exists.
func (s *Store) CreateFirstUser(_ context.Context, u connector.StoredUser, groupSlug string) (bool, error) {
	if err := s.usable(usersAggregate); err != nil {
		return false, err
	}
	if len(s.users) > 0 {
		return false, nil
	}
	id := s.insertUser(u)
	if g := s.groupBySlug(groupSlug); g != nil {
		s.members = append(s.members, membership{group: g.ID, user: id})
	}
	return true, nil
}

// CreateUser stores u with the next id and its memberships.
func (s *Store) CreateUser(_ context.Context, u connector.StoredUser, groupIDs []int64) (int64, error) {
	if err := s.writable(usersAggregate, userWrites); err != nil {
		return 0, err
	}
	if err := s.unique(u); err != nil {
		return 0, err
	}
	for _, g := range groupIDs {
		if s.groupByID(g) == nil {
			return 0, ErrNoSuchGroup
		}
	}
	id := s.insertUser(u)
	for _, g := range groupIDs {
		s.members = append(s.members, membership{group: g, user: id})
	}
	return id, nil
}

func (s *Store) insertUser(u connector.StoredUser) int64 {
	u.ID = int64(len(s.users) + 1)
	u.Groups = nil
	s.users = append(s.users, u)
	return u.ID
}

// unique reports ErrDuplicate when another user holds u's email, user id,
// or code hash, as the unique constraints would.
func (s *Store) unique(u connector.StoredUser) error {
	for _, held := range s.users {
		if held.ID == u.ID {
			continue
		}
		if held.Email == u.Email || sameValue(held.UserID, u.UserID) || sameValue(held.CodeHash, u.CodeHash) {
			return ErrDuplicate
		}
	}
	return nil
}

func sameValue(a, b *string) bool {
	return a != nil && b != nil && *a == *b
}

// User returns the user with id.
func (s *Store) User(_ context.Context, id int64) (connector.StoredUser, bool, error) {
	return s.oneUser(func(u connector.StoredUser) bool { return u.ID == id })
}

// UserByEmail returns the user stored under email.
func (s *Store) UserByEmail(_ context.Context, email string) (connector.StoredUser, bool, error) {
	if s.failing[emailLookups] {
		return connector.StoredUser{}, false, ErrUnavailable
	}
	return s.oneUser(func(u connector.StoredUser) bool { return u.Email == email })
}

// UserByUserID returns the user stored under userID.
func (s *Store) UserByUserID(_ context.Context, userID string) (connector.StoredUser, bool, error) {
	return s.oneUser(func(u connector.StoredUser) bool { return sameValue(u.UserID, &userID) })
}

// UserByCodeHash returns the user whose door code hash is hash.
func (s *Store) UserByCodeHash(_ context.Context, hash string) (connector.StoredUser, bool, error) {
	return s.oneUser(func(u connector.StoredUser) bool { return sameValue(u.CodeHash, &hash) })
}

func (s *Store) oneUser(match func(connector.StoredUser) bool) (connector.StoredUser, bool, error) {
	if err := s.usable(usersAggregate); err != nil {
		return connector.StoredUser{}, false, err
	}
	for _, u := range s.users {
		if match(u) {
			return s.withGroups(u), true, nil
		}
	}
	return connector.StoredUser{}, false, nil
}

// Users returns the page of users q selects.
func (s *Store) Users(_ context.Context, q connector.UserQuery) ([]connector.StoredUser, int, error) {
	if err := s.usable(usersAggregate); err != nil {
		return nil, 0, err
	}
	var selected []connector.StoredUser
	for _, u := range s.users {
		if (u.DeletedAt != nil) == q.Deleted && strings.Contains(strings.ToLower(searchText(u)), strings.ToLower(q.Search)) {
			selected = append(selected, s.withGroups(u))
		}
	}
	sortUsers(selected)
	end := min(q.Offset+q.Limit, len(selected))
	start := min(q.Offset, end)
	return slices.Clone(selected[start:end]), len(selected), nil
}

// searchText is what a user search looks in.
func searchText(u connector.StoredUser) string {
	fields := []string{u.Email}
	if u.UserID != nil {
		fields = append(fields, *u.UserID)
	}
	return strings.Join(append(fields, u.FirstName, u.LastName, u.DisplayName), " ")
}

func sortUsers(users []connector.StoredUser) {
	slices.SortFunc(users, func(a, b connector.StoredUser) int {
		return cmp.Or(strings.Compare(strings.ToLower(a.DisplayName), strings.ToLower(b.DisplayName)), cmp.Compare(a.ID, b.ID))
	})
}

// UpdateUser stores u's profile fields over the user with u.ID.
func (s *Store) UpdateUser(_ context.Context, u connector.StoredUser) (bool, error) {
	if err := s.writable(usersAggregate, userWrites); err != nil {
		return false, err
	}
	held := s.userByID(u.ID)
	if held == nil {
		return false, nil
	}
	if err := s.unique(u); err != nil {
		return false, err
	}
	held.Email, held.UserID, held.FirstName, held.LastName = u.Email, u.UserID, u.FirstName, u.LastName
	held.DisplayName, held.ExpiresAt = u.DisplayName, u.ExpiresAt
	return true, nil
}

// SetUserDeleted stores the deletion time of the user with id.
func (s *Store) SetUserDeleted(_ context.Context, id int64, at *time.Time) (bool, error) {
	return s.changeUser(id, func(u *connector.StoredUser) { u.DeletedAt = at })
}

// SetCodeHash stores the door code hash of the user with id.
func (s *Store) SetCodeHash(_ context.Context, id int64, hash *string) (bool, error) {
	if err := s.writable(usersAggregate, userWrites); err != nil {
		return false, err
	}
	if s.userByID(id) == nil {
		return false, nil
	}
	if err := s.unique(connector.StoredUser{ID: id, CodeHash: hash}); err != nil {
		return false, err
	}
	return s.changeUser(id, func(u *connector.StoredUser) { u.CodeHash = hash })
}

func (s *Store) changeUser(id int64, change func(u *connector.StoredUser)) (bool, error) {
	if err := s.writable(usersAggregate, userWrites); err != nil {
		return false, err
	}
	u := s.userByID(id)
	if u == nil {
		return false, nil
	}
	change(u)
	return true, nil
}

// withGroups is u with the groups it is a member of, in name order.
func (s *Store) withGroups(u connector.StoredUser) connector.StoredUser {
	var held []connector.StoredGroup
	for _, m := range s.members {
		if m.user == u.ID {
			held = append(held, *s.groupByID(m.group))
		}
	}
	sortGroups(held)
	u.Groups = nil
	for _, g := range held {
		u.Groups = append(u.Groups, connector.UserGroup{ID: g.ID, Slug: g.Slug, Name: g.Name, Roles: slices.Clone(g.Roles)})
	}
	return u
}

func sortGroups(groups []connector.StoredGroup) {
	slices.SortFunc(groups, func(a, b connector.StoredGroup) int {
		return cmp.Or(strings.Compare(strings.ToLower(a.Name), strings.ToLower(b.Name)), cmp.Compare(a.ID, b.ID))
	})
}

// Groups returns every group in name order with its member count.
func (s *Store) Groups(context.Context) ([]connector.StoredGroup, error) {
	if err := s.usable(groupsAggregate); err != nil {
		return nil, err
	}
	all := []connector.StoredGroup{}
	for _, g := range s.groups {
		all = append(all, s.counted(g))
	}
	sortGroups(all)
	return all, nil
}

// Group returns the group with id.
func (s *Store) Group(_ context.Context, id int64) (connector.StoredGroup, bool, error) {
	return s.oneGroup(s.groupByID(id))
}

// GroupBySlug returns the group stored under slug.
func (s *Store) GroupBySlug(_ context.Context, slug string) (connector.StoredGroup, bool, error) {
	return s.oneGroup(s.groupBySlug(slug))
}

func (s *Store) oneGroup(g *connector.StoredGroup) (connector.StoredGroup, bool, error) {
	if err := s.usable(groupsAggregate); err != nil {
		return connector.StoredGroup{}, false, err
	}
	if g == nil {
		return connector.StoredGroup{}, false, nil
	}
	return s.counted(*g), true, nil
}

func (s *Store) counted(g connector.StoredGroup) connector.StoredGroup {
	g = cloneGroup(g)
	for _, m := range s.members {
		if m.group == g.ID {
			g.MemberCount++
		}
	}
	return g
}

// CreateGroup stores g with the next id.
func (s *Store) CreateGroup(_ context.Context, g connector.StoredGroup) (int64, error) {
	if err := s.storable(g); err != nil {
		return 0, err
	}
	s.nextGroup++
	g = cloneGroup(g)
	g.ID = s.nextGroup
	g.MemberCount = 0
	s.groups = append(s.groups, g)
	return g.ID, nil
}

// UpdateGroup stores g over the group with g.ID.
func (s *Store) UpdateGroup(_ context.Context, g connector.StoredGroup) (bool, error) {
	if err := s.storable(g); err != nil {
		return false, err
	}
	held := s.groupByID(g.ID)
	if held == nil {
		return false, nil
	}
	g = cloneGroup(g)
	g.MemberCount = 0
	*held = g
	return true, nil
}

// storable reports why g cannot be stored, as the database's constraints
// would.
func (s *Store) storable(g connector.StoredGroup) error {
	if err := s.writable(groupsAggregate, groupWrites); err != nil {
		return err
	}
	if g.Roles == nil || g.PreassignedUserIDs == nil {
		return ErrNull
	}
	if held := s.groupBySlug(g.Slug); held != nil && held.ID != g.ID {
		return ErrDuplicate
	}
	return nil
}

// DeleteGroup removes the group with id, its memberships, and its rules.
func (s *Store) DeleteGroup(_ context.Context, id int64) (bool, error) {
	if err := s.writable(groupsAggregate, groupWrites); err != nil {
		return false, err
	}
	i := slices.IndexFunc(s.groups, func(g connector.StoredGroup) bool { return g.ID == id })
	if i < 0 {
		return false, nil
	}
	s.groups = slices.Delete(slices.Clone(s.groups), i, i+1)
	s.members = slices.DeleteFunc(slices.Clone(s.members), func(m membership) bool { return m.group == id })
	s.dropGroupRules(id)
	return true, nil
}

// GroupMembers returns the group's members in user order.
func (s *Store) GroupMembers(_ context.Context, id int64) ([]connector.StoredUser, error) {
	if err := s.usable(groupsAggregate); err != nil {
		return nil, err
	}
	var members []connector.StoredUser
	for _, m := range s.members {
		if m.group == id {
			members = append(members, s.withGroups(*s.userByID(m.user)))
		}
	}
	sortUsers(members)
	return members, nil
}

// AddMember stores the membership unless it exists.
func (s *Store) AddMember(_ context.Context, groupID, userID int64) error {
	if err := s.writable(groupsAggregate, groupWrites); err != nil {
		return err
	}
	if s.groupByID(groupID) == nil {
		return ErrNoSuchGroup
	}
	if s.userByID(userID) == nil {
		return ErrNoSuchUser
	}
	m := membership{group: groupID, user: userID}
	if !slices.Contains(s.members, m) {
		s.members = append(s.members, m)
	}
	return nil
}

// Preassign appends userID to the group's pre-assigned user ids unless
// they hold it.
func (s *Store) Preassign(_ context.Context, groupID int64, userID string) error {
	if err := s.writable(groupsAggregate, groupWrites); err != nil {
		return err
	}
	held := s.groupByID(groupID)
	if held != nil && !slices.Contains(held.PreassignedUserIDs, userID) {
		held.PreassignedUserIDs = append(slices.Clone(held.PreassignedUserIDs), userID)
	}
	return nil
}

// RemoveMember removes the membership.
func (s *Store) RemoveMember(_ context.Context, groupID, userID int64) (bool, error) {
	if err := s.writable(groupsAggregate, groupWrites); err != nil {
		return false, err
	}
	m := membership{group: groupID, user: userID}
	held := slices.Contains(s.members, m)
	s.members = slices.DeleteFunc(slices.Clone(s.members), func(other membership) bool { return other == m })
	return held, nil
}

func (s *Store) userByID(id int64) *connector.StoredUser {
	for i := range s.users {
		if s.users[i].ID == id {
			return &s.users[i]
		}
	}
	return nil
}

func (s *Store) groupByID(id int64) *connector.StoredGroup {
	for i := range s.groups {
		if s.groups[i].ID == id {
			return &s.groups[i]
		}
	}
	return nil
}

func (s *Store) groupBySlug(slug string) *connector.StoredGroup {
	for i := range s.groups {
		if s.groups[i].Slug == slug {
			return &s.groups[i]
		}
	}
	return nil
}

func cloneGroup(g connector.StoredGroup) connector.StoredGroup {
	g.Roles = slices.Clone(g.Roles)
	g.PreassignedUserIDs = slices.Clone(g.PreassignedUserIDs)
	return g
}

// userByEmail is the user a test arranges by email, which must exist.
func (s *Store) userByEmail(email string) *connector.StoredUser {
	for i := range s.users {
		if s.users[i].Email == email {
			return &s.users[i]
		}
	}
	panic("stub: no user with email " + email)
}

// LeaveGroup ends the membership of the user with email in the group with
// slug, as another administrator might have.
func (s *Store) LeaveGroup(email, slug string) {
	m := membership{group: s.groupBySlug(slug).ID, user: s.userByEmail(email).ID}
	s.members = slices.DeleteFunc(slices.Clone(s.members), func(other membership) bool { return other == m })
}

// SetPasswordHash stores the local password hash of the user with id.
func (s *Store) SetPasswordHash(_ context.Context, id int64, hash string) (bool, error) {
	return s.changeUser(id, func(u *connector.StoredUser) { u.PasswordHash = &hash })
}

// FailEmailLookups makes UserByEmail report ErrUnavailable, while users
// can still be read otherwise.
func (s *Store) FailEmailLookups() {
	s.failing[emailLookups] = true
}
