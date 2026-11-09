package directory

import (
	"context"
	"slices"
	"strings"

	"github.com/dakriy/doorcode/internal/auth"
	"github.com/dakriy/doorcode/internal/check"
	"github.com/dakriy/doorcode/internal/connector"
)

const (
	maxGroupNameLength   = 100
	maxDescriptionLength = 1000
)

// GroupFields is what an administrator says about a group. Text is trimmed
// before it is checked; roles are put in the order auth.Roles lists them,
// and pre-assigned user ids are trimmed with blanks and repeats dropped.
type GroupFields struct {
	Slug               string
	Name               string
	Description        string
	Roles              []string
	PreassignedUserIDs []string
}

// Groups returns every group in name order.
func (s Service) Groups(ctx context.Context) ([]connector.StoredGroup, error) {
	return s.Store.Groups(ctx)
}

// Group returns the group whose id is id.
func (s Service) Group(ctx context.Context, id int64) (connector.StoredGroup, error) {
	g, found, err := s.Store.Group(ctx, id)
	if err != nil {
		return connector.StoredGroup{}, err
	}
	if !found {
		return connector.StoredGroup{}, ErrNoGroup
	}
	return g, nil
}

// Members returns the group whose id is id and its members.
func (s Service) Members(ctx context.Context, id int64) (connector.StoredGroup, []connector.StoredUser, error) {
	g, err := s.Group(ctx, id)
	if err != nil {
		return g, nil, err
	}
	members, err := s.Store.GroupMembers(ctx, id)
	return g, members, err
}

// CreateGroup creates a group from f, carrying only roles viewer holds.
func (s Service) CreateGroup(ctx context.Context, viewer auth.User, f GroupFields) (connector.StoredGroup, error) {
	g, err := s.checkGroup(ctx, 0, f)
	if err != nil {
		return connector.StoredGroup{}, err
	}
	if !mayChange(viewer, g.Roles) {
		return connector.StoredGroup{}, ErrNotHeld
	}
	id, err := s.Store.CreateGroup(ctx, g)
	if err != nil {
		return connector.StoredGroup{}, err
	}
	return s.Group(ctx, id)
}

// UpdateGroup replaces the group whose id is id with f. viewer must hold
// every role the group carries before and after, and may not take the
// admin role from themselves.
func (s Service) UpdateGroup(ctx context.Context, viewer auth.User, id int64, f GroupFields) (connector.StoredGroup, error) {
	before, err := s.Group(ctx, id)
	if err != nil {
		return connector.StoredGroup{}, err
	}
	g, err := s.checkGroup(ctx, id, f)
	if err != nil {
		return connector.StoredGroup{}, err
	}
	if !mayChange(viewer, slices.Concat(before.Roles, g.Roles)) {
		return connector.StoredGroup{}, ErrNotHeld
	}
	if losesAdmin(viewer, viewer.WithGroupRoles(id, g.Roles)) {
		return connector.StoredGroup{}, ErrOwnAdmin
	}
	g.ID = id
	// The group was just read and groups are removed only by DeleteGroup,
	// so it is there to update.
	if _, err := s.Store.UpdateGroup(ctx, g); err != nil {
		return connector.StoredGroup{}, err
	}
	return s.Group(ctx, id)
}

// DeleteGroup removes the group whose id is id and its memberships.
func (s Service) DeleteGroup(ctx context.Context, viewer auth.User, id int64) error {
	if _, err := s.changeable(ctx, viewer, id); err != nil {
		return err
	}
	if losesAdmin(viewer, viewer.Without(id)) {
		return ErrOwnAdmin
	}
	_, err := s.Store.DeleteGroup(ctx, id)
	return err
}

// AddMember makes the user whose id is userID a member of the group whose
// id is groupID.
func (s Service) AddMember(ctx context.Context, viewer auth.User, groupID, userID int64) error {
	if _, err := s.changeable(ctx, viewer, groupID); err != nil {
		return err
	}
	if _, err := s.User(ctx, userID); err != nil {
		return err
	}
	return s.Store.AddMember(ctx, groupID, userID)
}

// RemoveMember ends the membership of the user whose id is userID in the
// group whose id is groupID. viewer may not take the admin role from
// themselves.
func (s Service) RemoveMember(ctx context.Context, viewer auth.User, groupID, userID int64) error {
	if _, err := s.changeable(ctx, viewer, groupID); err != nil {
		return err
	}
	if userID == viewer.ID && losesAdmin(viewer, viewer.Without(groupID)) {
		return ErrOwnAdmin
	}
	removed, err := s.Store.RemoveMember(ctx, groupID, userID)
	if err == nil && !removed {
		return ErrNotMember
	}
	return err
}

// changeable returns the group whose id is id when viewer holds every role
// it carries.
func (s Service) changeable(ctx context.Context, viewer auth.User, id int64) (connector.StoredGroup, error) {
	g, err := s.Group(ctx, id)
	if err != nil {
		return g, err
	}
	if !mayChange(viewer, g.Roles) {
		return g, ErrNotHeld
	}
	return g, nil
}

// checkGroup returns f tidied as a group to store, or the problems with
// it. id is the group f describes, zero for a new one, whose own slug is no
// conflict.
func (s Service) checkGroup(ctx context.Context, id int64, f GroupFields) (connector.StoredGroup, error) {
	g := connector.StoredGroup{
		Slug:               strings.TrimSpace(f.Slug),
		Name:               strings.TrimSpace(f.Name),
		Description:        strings.TrimSpace(f.Description),
		Roles:              knownRoles(f.Roles),
		PreassignedUserIDs: distinctUserIDs(f.PreassignedUserIDs),
	}
	var found check.List
	slugProblem, err := s.slugProblem(ctx, id, g.Slug)
	if err != nil {
		return g, err
	}
	found.Add("slug", slugProblem)
	found.Add("name", check.Required(g.Name, "Enter a name."))
	found.Add("name", check.TooLong(g.Name, maxGroupNameLength, check.LongText))
	found.Add("description", check.TooLong(g.Description, maxDescriptionLength, check.LongText))
	if len(g.Roles) != len(slices.Compact(slices.Sorted(slices.Values(f.Roles)))) {
		found.Add("roles", "Choose roles from the list.")
	}
	found.Add("preassigned_user_ids", badUserIDs(g.PreassignedUserIDs))
	return g, found.Err()
}

func (s Service) slugProblem(ctx context.Context, id int64, slug string) (string, error) {
	if slug == "" {
		return "Enter a slug.", nil
	}
	if !check.Slug(slug) {
		return check.SlugHint, nil
	}
	holder, taken, err := s.Store.GroupBySlug(ctx, slug)
	return conflict(id, holder.ID, taken, "Another group has this slug."), err
}

// knownRoles is the roles of auth.Roles that roles names, in that order.
func knownRoles(roles []string) []string {
	known := []string{}
	for _, role := range auth.Roles {
		if slices.Contains(roles, role) {
			known = append(known, role)
		}
	}
	return known
}

// distinctUserIDs is ids trimmed, without blanks or repeats, in order.
func distinctUserIDs(ids []string) []string {
	out := []string{}
	for _, id := range ids {
		id = strings.TrimSpace(id)
		if id != "" && !slices.Contains(out, id) {
			out = append(out, id)
		}
	}
	return out
}

// badUserIDs names the first id that is not a valid user id.
func badUserIDs(ids []string) string {
	for _, id := range ids {
		if !userIDPattern.MatchString(id) {
			return userIDHint + " “" + id + "” is not."
		}
	}
	return ""
}

// Join makes the user whose id is userID a member of the groups whose
// slugs are in slugs, as signing in for the first time decides. Slugs
// that name no group are passed over, and nobody's roles limit it.
func (s Service) Join(ctx context.Context, userID int64, slugs []string) error {
	groups, err := s.Store.Groups(ctx)
	if err != nil {
		return err
	}
	for _, g := range groups {
		if slices.Contains(slugs, g.Slug) {
			if err := s.Store.AddMember(ctx, g.ID, userID); err != nil {
				return err
			}
		}
	}
	return nil
}

// Entitle makes u a member of exactly the groups whose slugs are in slugs,
// as the identity provider's entitlements decide: it joins those u is not
// in and leaves every other. Slugs that name no group are passed over, no
// group is created, and nobody's roles limit it.
func (s Service) Entitle(ctx context.Context, u connector.StoredUser, slugs []string) error {
	groups, err := s.Store.Groups(ctx)
	if err != nil {
		return err
	}
	for _, g := range groups {
		member := slices.ContainsFunc(u.Groups, func(m connector.UserGroup) bool { return m.ID == g.ID })
		entitled := slices.Contains(slugs, g.Slug)
		if entitled && !member {
			err = s.Store.AddMember(ctx, g.ID, u.ID)
		} else if !entitled && member {
			_, err = s.Store.RemoveMember(ctx, g.ID, u.ID)
		}
		if err != nil {
			return err
		}
	}
	return nil
}
