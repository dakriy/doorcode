package app

import (
	"errors"
	"net/http"
	"strings"

	"github.com/dakriy/doorcode/internal/auth"
	"github.com/dakriy/doorcode/internal/connector"
	"github.com/dakriy/doorcode/internal/directory"
)

func (a *App) groupPageRoutes() []route {
	groups := holding(auth.RoleManageGroups)
	return []route{
		pageRoute(http.MethodGet, "/admin/groups", groups, a.groupsPage),
		pageRoute(http.MethodGet, "/admin/groups/new", groups, a.newGroupPage),
		pageRoute(http.MethodPost, "/admin/groups", groups, a.createGroupFromForm),
		pageRoute(http.MethodGet, "/admin/groups/{id}", groups, a.groupPage),
		pageRoute(http.MethodPost, "/admin/groups/{id}", groups, a.saveGroupFromForm),
		pageRoute(http.MethodPost, "/admin/groups/{id}/delete", groups, a.deleteGroupFromForm),
		pageRoute(http.MethodPost, "/admin/groups/{id}/members", groups, a.addMemberFromForm),
		pageRoute(http.MethodPost, "/admin/groups/{id}/members/{user}/remove", groups, a.removeMemberFromForm),
	}
}

func (a *App) groupsPage(w http.ResponseWriter, r *http.Request, viewer auth.User) {
	groups, err := a.directory.Groups(r.Context())
	if err != nil {
		a.pageError(w, err)
		return
	}
	a.render(w, r, http.StatusOK, groupsTemplate, "Groups", &viewer, groups)
}

// groupForm is the group form, as entered.
type groupForm struct {
	Slug        string
	Name        string
	Description string
	Roles       []string
	// Preassigned is the pre-assigned user ids, one per line.
	Preassigned string
	Errors      map[string]string
	// Alert is a refusal to show above the form.
	Alert string
	// Choices is every role, in order.
	Choices []string
}

func formOfGroup(g connector.StoredGroup) groupForm {
	return groupForm{
		Slug: g.Slug, Name: g.Name, Description: g.Description, Roles: g.Roles,
		Preassigned: strings.Join(g.PreassignedUserIDs, "\n"), Choices: auth.Roles,
	}
}

func readGroupForm(r *http.Request) (directory.GroupFields, groupForm) {
	_ = r.ParseForm()
	f := groupForm{
		Slug: r.PostFormValue("slug"), Name: r.PostFormValue("name"), Description: r.PostFormValue("description"),
		Roles: r.PostForm["roles"], Preassigned: r.PostFormValue("preassigned_user_ids"), Choices: auth.Roles,
	}
	ids := strings.FieldsFunc(f.Preassigned, func(c rune) bool { return c == '\n' || c == '\r' || c == ',' })
	return directory.GroupFields{Slug: f.Slug, Name: f.Name, Description: f.Description, Roles: f.Roles, PreassignedUserIDs: ids}, f
}

// rejected is f carrying what err says is wrong, and whether err is a
// problem or refusal the form shows.
func (f groupForm) rejected(err error) (groupForm, int, bool) {
	if errs, rejected := fieldErrors(err); rejected {
		f.Errors = errs
		return f, http.StatusUnprocessableEntity, true
	}
	status, message, refused := refusal(err)
	f.Alert = message
	return f, status, refused
}

func (a *App) newGroupPage(w http.ResponseWriter, r *http.Request, viewer auth.User) {
	form := groupForm{Choices: auth.Roles}
	a.render(w, r, http.StatusOK, newGroupTemplate, "New group", &viewer, form)
}

func (a *App) createGroupFromForm(w http.ResponseWriter, r *http.Request, viewer auth.User) {
	fields, form := readGroupForm(r)
	g, err := a.directory.CreateGroup(r.Context(), viewer, fields)
	if form, status, rejected := form.rejected(err); rejected {
		a.render(w, r, status, newGroupTemplate, "New group", &viewer, form)
		return
	}
	a.changed(w, r, err, groupPath(g.ID), "group-created")
}

// groupContent is the group detail page's content.
type groupContent struct {
	Group   connector.StoredGroup
	Members []userRow
	Rules   []ruleRow
	Form    groupForm
	// Member is the add member form's entry, and MemberError what is wrong
	// with it.
	Member      string
	MemberError string
	// Alert is a refusal of a change to the members or of deleting the
	// group.
	Alert string
}

// pageGroup returns the content of the page about the group the path
// names, with its form as it starts, after showing that there is none or
// that the store failed.
func (a *App) pageGroup(w http.ResponseWriter, r *http.Request, viewer auth.User) (groupContent, bool) {
	g, members, err := a.directory.Members(r.Context(), pathID(r, "id"))
	if errors.Is(err, directory.ErrNoGroup) {
		a.notFound(w, r, viewer, "There is no group here. It may have been deleted.")
		return groupContent{}, false
	}
	if err != nil {
		a.pageError(w, err)
		return groupContent{}, false
	}
	rules, err := a.rules.Rules(r.Context(), g.ID)
	if err != nil {
		a.pageError(w, err)
		return groupContent{}, false
	}
	n, err := a.names(r.Context())
	if err != nil {
		a.pageError(w, err)
		return groupContent{}, false
	}
	content := groupContent{Group: g, Members: a.rowsOf(members), Rules: []ruleRow{}, Form: formOfGroup(g)}
	for _, stored := range rules {
		content.Rules = append(content.Rules, n.rowOfRule(stored))
	}
	return content, true
}

func (a *App) groupPage(w http.ResponseWriter, r *http.Request, viewer auth.User) {
	if content, ok := a.pageGroup(w, r, viewer); ok {
		a.renderGroup(w, r, http.StatusOK, viewer, content)
	}
}

func (a *App) renderGroup(w http.ResponseWriter, r *http.Request, status int, viewer auth.User, content groupContent) {
	a.render(w, r, status, groupTemplate, content.Group.Name, &viewer, content)
}

func (a *App) saveGroupFromForm(w http.ResponseWriter, r *http.Request, viewer auth.User) {
	content, ok := a.pageGroup(w, r, viewer)
	if !ok {
		return
	}
	fields, form := readGroupForm(r)
	_, err := a.directory.UpdateGroup(r.Context(), viewer, content.Group.ID, fields)
	if form, status, rejected := form.rejected(err); rejected {
		content.Form = form
		a.renderGroup(w, r, status, viewer, content)
		return
	}
	a.groupChanged(w, r, viewer, content, err, groupPath(content.Group.ID), "group-saved")
}

// groupChanged finishes a change made from the page content describes: a
// refusal is shown on the page, a failure as an error, and success by
// going to location with flash.
func (a *App) groupChanged(w http.ResponseWriter, r *http.Request, viewer auth.User, content groupContent, err error, location, flash string) {
	if status, message, refused := refusal(err); refused {
		content.Alert = message
		a.renderGroup(w, r, status, viewer, content)
		return
	}
	a.changed(w, r, err, location, flash)
}

func (a *App) deleteGroupFromForm(w http.ResponseWriter, r *http.Request, viewer auth.User) {
	if content, ok := a.pageGroup(w, r, viewer); ok {
		err := a.directory.DeleteGroup(r.Context(), viewer, content.Group.ID)
		a.groupChanged(w, r, viewer, content, err, "/admin/groups", "group-deleted")
	}
}

func (a *App) addMemberFromForm(w http.ResponseWriter, r *http.Request, viewer auth.User) {
	content, ok := a.pageGroup(w, r, viewer)
	if !ok {
		return
	}
	handle := strings.TrimSpace(r.PostFormValue("member"))
	u, found, err := a.directory.FindUser(r.Context(), handle)
	if err == nil && !found {
		content.Member, content.MemberError = handle, "No user has this email address or user id."
		a.renderGroup(w, r, http.StatusUnprocessableEntity, viewer, content)
		return
	}
	if err == nil {
		err = a.directory.AddMember(r.Context(), viewer, content.Group.ID, u.ID)
	}
	a.groupChanged(w, r, viewer, content, err, groupPath(content.Group.ID), "member-added")
}

func (a *App) removeMemberFromForm(w http.ResponseWriter, r *http.Request, viewer auth.User) {
	if content, ok := a.pageGroup(w, r, viewer); ok {
		err := a.directory.RemoveMember(r.Context(), viewer, content.Group.ID, pathID(r, "user"))
		a.groupChanged(w, r, viewer, content, err, groupPath(content.Group.ID), "member-removed")
	}
}
