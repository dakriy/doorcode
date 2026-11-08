package app

import (
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/dakriy/doorcode/internal/check"
	"github.com/dakriy/doorcode/internal/connector"
	"github.com/dakriy/doorcode/internal/directory"
)

// directoryStatuses maps each refusal the directory reports to the status
// the API answers it with.
var directoryStatuses = []struct {
	err    error
	status int
}{
	{directory.ErrNoUser, http.StatusNotFound},
	{directory.ErrNoGroup, http.StatusNotFound},
	{directory.ErrNotMember, http.StatusNotFound},
	{directory.ErrNotHeld, http.StatusForbidden},
	{directory.ErrOwnAdmin, http.StatusConflict},
	{directory.ErrOwnAccount, http.StatusConflict},
}

// directoryError answers an API request with what err, from the
// directory, means.
func (a *App) directoryError(w http.ResponseWriter, err error) {
	var problems check.Problems
	if errors.As(err, &problems) {
		writeError(w, http.StatusBadRequest, problems.Error())
		return
	}
	for _, known := range directoryStatuses {
		if errors.Is(err, known.err) {
			writeError(w, known.status, err.Error())
			return
		}
	}
	a.internalError(w, err)
}

// pathID is the id the path parameter name holds; zero, which names
// nothing, when it holds no id.
func pathID(r *http.Request, name string) int64 {
	id, _ := strconv.ParseInt(r.PathValue(name), 10, 64)
	return id
}

// reached reports whether now is at or past at; a nil at is never reached.
func reached(at *time.Time, now time.Time) bool {
	return at != nil && now.Compare(*at) >= 0
}

// utc is t in UTC, nil when t is.
func utc(t *time.Time) *time.Time {
	if t == nil {
		return nil
	}
	in := t.UTC()
	return &in
}

type groupRef struct {
	ID   int64  `json:"id" doc:"The group's id."`
	Slug string `json:"slug" doc:"The group's slug."`
	Name string `json:"name" doc:"The group's name."`
}

type userView struct {
	ID          int64      `json:"id" doc:"Names the user in the API."`
	Email       string     `json:"email" doc:"The user's email address, lowercase."`
	UserID      *string    `json:"user_id,omitempty" doc:"The user's external id, such as an employee or student number; absent when they have none."`
	FirstName   string     `json:"first_name" doc:"The user's first name; may be empty."`
	LastName    string     `json:"last_name" doc:"The user's last name; may be empty."`
	DisplayName string     `json:"display_name" doc:"The name pages show for the user."`
	HasCode     bool       `json:"has_code" doc:"True when the user has a door code. The code itself is never shown."`
	ExpiresAt   *time.Time `json:"expires_at,omitempty" doc:"When the account stops working; absent when it never expires."`
	Expired     bool       `json:"expired" doc:"True once the account has expired."`
	DeletedAt   *time.Time `json:"deleted_at,omitempty" doc:"When the user was deleted; absent unless they are deleted."`
	CreatedAt   time.Time  `json:"created_at" doc:"When the user was created."`
	Groups      []groupRef `json:"groups" doc:"The groups the user is a member of, by name. The user holds every role these groups carry."`
}

func (a *App) viewOfUser(u connector.StoredUser) userView {
	groups := []groupRef{}
	for _, g := range u.Groups {
		groups = append(groups, groupRef{ID: g.ID, Slug: g.Slug, Name: g.Name})
	}
	return userView{
		ID: u.ID, Email: u.Email, UserID: u.UserID, FirstName: u.FirstName, LastName: u.LastName,
		DisplayName: u.DisplayName, HasCode: u.CodeHash != nil,
		ExpiresAt: utc(u.ExpiresAt), Expired: reached(u.ExpiresAt, a.clock.Now()),
		DeletedAt: utc(u.DeletedAt), CreatedAt: u.CreatedAt.UTC(), Groups: groups,
	}
}

func (a *App) viewsOfUsers(users []connector.StoredUser) []userView {
	views := make([]userView, 0, len(users))
	for _, u := range users {
		views = append(views, a.viewOfUser(u))
	}
	return views
}

type groupView struct {
	ID                 int64    `json:"id" doc:"Names the group in the API."`
	Slug               string   `json:"slug" doc:"The group's unique, URL-safe name; identity providers' entitlements match it."`
	Name               string   `json:"name" doc:"The group's name."`
	Description        string   `json:"description" doc:"What the group is for; may be empty."`
	Roles              []string `json:"roles" doc:"The roles the group carries; every member holds them."`
	PreassignedUserIDs []string `json:"preassigned_user_ids" doc:"User ids whose users join the group when they are created."`
	MemberCount        int      `json:"member_count" doc:"How many users are members."`
}

func viewOfGroup(g connector.StoredGroup) groupView {
	return groupView{
		ID: g.ID, Slug: g.Slug, Name: g.Name, Description: g.Description,
		Roles: g.Roles, PreassignedUserIDs: g.PreassignedUserIDs, MemberCount: g.MemberCount,
	}
}
