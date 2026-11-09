package app

import (
	"net/http"

	"github.com/dakriy/doorcode/internal/auth"
	"github.com/dakriy/doorcode/internal/directory"
	"github.com/dakriy/doorcode/internal/openapi"
)

type groupList struct {
	Groups []groupView `json:"groups" doc:"Every group, by name."`
}

type memberList struct {
	Members []userView `json:"members" doc:"The group's members, by display name."`
}

type groupRequest struct {
	Slug               string   `json:"slug" doc:"Up to 64 lowercase letters and digits, in words joined by single dashes; unique."`
	Name               string   `json:"name" doc:"The group's name, up to 100 characters."`
	Description        string   `json:"description,omitempty" doc:"What the group is for, up to 1000 characters."`
	Roles              []string `json:"roles,omitempty" doc:"The roles the group carries: admin, manage-users, manage-doors, manage-groups, manage-schedules, manage-firmware, view-logs, door-commander. The caller must hold each."`
	PreassignedUserIDs []string `json:"preassigned_user_ids,omitempty" doc:"User ids whose users join the group when they are created; an id leaves the list as its user joins."`
}

var (
	groupIDParam  = openapi.Param{Name: "id", In: "path", Description: "The group's id.", Example: int64(1)}
	memberParams  = []openapi.Param{groupIDParam, {Name: "user", In: "path", Description: "The member's user id in the API (the id field of a user).", Example: int64(1)}}
	noGroup       = openapi.Response{Status: http.StatusNotFound, Description: "No group has this id.", Body: errorBody{}}
	notHeld       = openapi.Response{Status: http.StatusForbidden, Description: "The caller does not hold the manage-groups role, or does not hold every role the group carries or would carry.", Body: errorBody{}}
	ownAdminGroup = openapi.Response{Status: http.StatusConflict, Description: "The change would take the admin role from the caller.", Body: errorBody{}}
)

func (a *App) groupRoutes() []route {
	groups := holding(auth.RoleManageGroups)
	example := groupRequest{
		Slug: "night-staff", Name: "Night staff", Description: "Security and cleaning crews.",
		Roles: []string{auth.RoleViewLogs}, PreassignedUserIDs: []string{"100234", "100877"},
	}
	badGroup := openapi.Response{Status: http.StatusBadRequest, Description: "The body is not a JSON group, or a field is wrong; the error says which.", Body: errorBody{}}
	return []route{
		apiRoute(http.MethodGet, "/groups", groups, a.listGroups, apiDoc{
			id: "listGroups", summary: "List every group", tag: "groups",
			responses: []openapi.Response{{Status: http.StatusOK, Description: "Every group.", Body: groupList{}}},
		}),
		apiRoute(http.MethodPost, "/groups", groups, a.createGroup, apiDoc{
			id: "createGroup", summary: "Create a group", tag: "groups", request: example,
			responses: []openapi.Response{{Status: http.StatusCreated, Description: "The new group.", Body: groupView{}}, badGroup, notHeld},
		}),
		apiRoute(http.MethodGet, "/groups/{id}", groups, a.getGroup, apiDoc{
			id: "getGroup", summary: "Read one group", tag: "groups", params: []openapi.Param{groupIDParam},
			responses: []openapi.Response{{Status: http.StatusOK, Description: "The group.", Body: groupView{}}, noGroup},
		}),
		apiRoute(http.MethodPut, "/groups/{id}", groups, a.updateGroup, apiDoc{
			id: "updateGroup", summary: "Replace a group", tag: "groups", params: []openapi.Param{groupIDParam}, request: example,
			responses: []openapi.Response{
				{Status: http.StatusOK, Description: "The group as it is now.", Body: groupView{}}, badGroup, notHeld, noGroup, ownAdminGroup,
			},
		}),
		apiRoute(http.MethodDelete, "/groups/{id}", groups, a.deleteGroup, apiDoc{
			id: "deleteGroup", summary: "Delete a group", tag: "groups", params: []openapi.Param{groupIDParam},
			responses: []openapi.Response{
				{Status: http.StatusNoContent, Description: "The group and its memberships are gone. Its members keep their accounts."}, notHeld, noGroup, ownAdminGroup,
			},
		}),
		apiRoute(http.MethodGet, "/groups/{id}/members", groups, a.listMembers, apiDoc{
			id: "listGroupMembers", summary: "List a group's members", tag: "groups", params: []openapi.Param{groupIDParam},
			responses: []openapi.Response{{Status: http.StatusOK, Description: "The group's members.", Body: memberList{}}, noGroup},
		}),
		apiRoute(http.MethodPut, "/groups/{id}/members/{user}", groups, a.addMember, apiDoc{
			id: "addGroupMember", summary: "Add a member to a group", tag: "groups", params: memberParams,
			responses: []openapi.Response{
				{Status: http.StatusNoContent, Description: "The user is a member. Adding a member again changes nothing."}, notHeld,
				{Status: http.StatusNotFound, Description: "No group or no user has this id.", Body: errorBody{}},
			},
		}),
		apiRoute(http.MethodDelete, "/groups/{id}/members/{user}", groups, a.removeMember, apiDoc{
			id: "removeGroupMember", summary: "Remove a member from a group", tag: "groups", params: memberParams,
			responses: []openapi.Response{
				{Status: http.StatusNoContent, Description: "The user is no longer a member."}, notHeld,
				{Status: http.StatusNotFound, Description: "No group has this id, or the user is not a member.", Body: errorBody{}},
				ownAdminGroup,
			},
		}),
	}
}

func (a *App) listGroups(w http.ResponseWriter, r *http.Request, _ auth.User) {
	groups, err := a.directory.Groups(r.Context())
	if err != nil {
		a.internalError(w, err)
		return
	}
	views := make([]groupView, 0, len(groups))
	for _, g := range groups {
		views = append(views, viewOfGroup(g))
	}
	writeJSON(w, http.StatusOK, groupList{Groups: views})
}

// readGroup reads a JSON group request.
func readGroup(w http.ResponseWriter, r *http.Request) (directory.GroupFields, bool) {
	var body groupRequest
	if err := decodeJSON(w, r, &body); err != nil {
		writeError(w, http.StatusBadRequest, `body must be a JSON object with a string "slug" and "name"`)
		return directory.GroupFields{}, false
	}
	return directory.GroupFields(body), true
}

func (a *App) createGroup(w http.ResponseWriter, r *http.Request, caller auth.User) {
	f, ok := readGroup(w, r)
	if !ok {
		return
	}
	g, err := a.directory.CreateGroup(r.Context(), caller, f)
	if err != nil {
		a.directoryError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, viewOfGroup(g))
}

func (a *App) getGroup(w http.ResponseWriter, r *http.Request, _ auth.User) {
	g, err := a.directory.Group(r.Context(), pathID(r, "id"))
	if err != nil {
		a.directoryError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, viewOfGroup(g))
}

func (a *App) updateGroup(w http.ResponseWriter, r *http.Request, caller auth.User) {
	f, ok := readGroup(w, r)
	if !ok {
		return
	}
	g, err := a.directory.UpdateGroup(r.Context(), caller, pathID(r, "id"), f)
	if err != nil {
		a.directoryError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, viewOfGroup(g))
}

func (a *App) deleteGroup(w http.ResponseWriter, r *http.Request, caller auth.User) {
	a.noContent(w, a.directory.DeleteGroup(r.Context(), caller, pathID(r, "id")))
}

func (a *App) listMembers(w http.ResponseWriter, r *http.Request, _ auth.User) {
	_, members, err := a.directory.Members(r.Context(), pathID(r, "id"))
	if err != nil {
		a.directoryError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, memberList{Members: a.viewsOfUsers(members)})
}

func (a *App) addMember(w http.ResponseWriter, r *http.Request, caller auth.User) {
	a.noContent(w, a.directory.AddMember(r.Context(), caller, pathID(r, "id"), pathID(r, "user")))
}

func (a *App) removeMember(w http.ResponseWriter, r *http.Request, caller auth.User) {
	a.noContent(w, a.directory.RemoveMember(r.Context(), caller, pathID(r, "id"), pathID(r, "user")))
}
