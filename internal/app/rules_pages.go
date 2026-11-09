package app

import (
	"errors"
	"net/http"
	"slices"
	"strconv"
	"strings"

	"github.com/dakriy/doorcode/internal/auth"
	"github.com/dakriy/doorcode/internal/connector"
	"github.com/dakriy/doorcode/internal/directory"
	"github.com/dakriy/doorcode/internal/rule"
)

func (a *App) rulePageRoutes() []route {
	groups := holding(auth.RoleManageGroups)
	return []route{
		pageRoute(http.MethodGet, "/admin/groups/{id}/rules/new", groups, a.newRulePage),
		pageRoute(http.MethodPost, "/admin/groups/{id}/rules", groups, a.createRuleFromForm),
		pageRoute(http.MethodGet, "/admin/groups/{id}/rules/{rule}", groups, a.rulePage),
		pageRoute(http.MethodPost, "/admin/groups/{id}/rules/{rule}", groups, a.saveRuleFromForm),
		pageRoute(http.MethodPost, "/admin/groups/{id}/rules/{rule}/delete", groups, a.deleteRuleFromForm),
	}
}

func rulePath(groupID, id int64) string {
	return groupPath(groupID) + "/rules/" + strconv.FormatInt(id, 10)
}

// ruleRow is one access rule as a group's page lists it.
type ruleRow struct {
	ID       int64
	Schedule nameRef
	Zones    []nameRef
}

func (n names) rowOfRule(r connector.StoredRule) ruleRow {
	view := n.viewOfRule(r)
	return ruleRow{ID: r.ID, Schedule: view.Schedule, Zones: view.Zones}
}

// option is one option of a select.
type option struct {
	ID       int64
	Name     string
	Selected bool
}

// ruleContent is the access rule pages' content: the group, the rule,
// zero on the page that creates one, and its form as entered, with every
// schedule and zone to choose from.
type ruleContent struct {
	Group     connector.StoredGroup
	Rule      connector.StoredRule
	Errors    map[string]string
	Schedules []option
	Zones     []choice
	// Action is where the form is submitted.
	Action string
}

// pageRuleGroup returns the group the path names, after showing that there
// is none or that the store failed.
func (a *App) pageRuleGroup(w http.ResponseWriter, r *http.Request, viewer auth.User) (connector.StoredGroup, bool) {
	g, err := a.directory.Group(r.Context(), pathID(r, "id"))
	if a.unloaded(w, r, viewer, err, directory.ErrNoGroup, noGroupHere) {
		return g, false
	}
	return g, true
}

// pageRule returns the group and the access rule of it the path names,
// after showing that there is none or that the store failed.
func (a *App) pageRule(w http.ResponseWriter, r *http.Request, viewer auth.User) (connector.StoredGroup, connector.StoredRule, bool) {
	g, ok := a.pageRuleGroup(w, r, viewer)
	if !ok {
		return g, connector.StoredRule{}, false
	}
	stored, err := a.rules.Rule(r.Context(), pathID(r, "rule"))
	if err != nil && !errors.Is(err, rule.ErrNoRule) {
		a.pageError(w, r, err)
		return g, stored, false
	}
	if stored.GroupID != g.ID {
		a.notFound(w, r, viewer, "There is no access rule here. It may have been deleted.")
		return g, stored, false
	}
	return g, stored, true
}

func readRuleForm(r *http.Request, groupID int64) rule.Fields {
	_ = r.ParseForm()
	schedule, _ := strconv.ParseInt(r.PostFormValue("schedule_id"), 10, 64)
	return rule.Fields{GroupID: groupID, ScheduleID: schedule, ZoneIDs: formIDs(r, "zone_ids")}
}

// showRule shows the page about stored, a rule of g, or the page that
// creates one when stored is zero, with the form holding f.
func (a *App) showRule(w http.ResponseWriter, r *http.Request, status int, viewer auth.User, g connector.StoredGroup, stored connector.StoredRule, f rule.Fields, errs map[string]string) {
	n, err := a.names(r.Context())
	if err != nil {
		a.pageError(w, r, err)
		return
	}
	content := ruleContent{Group: g, Rule: stored, Errors: errs, Action: groupPath(g.ID) + "/rules"}
	for _, sch := range n.schedules {
		content.Schedules = append(content.Schedules, option{ID: sch.ID, Name: sch.Name, Selected: sch.ID == f.ScheduleID})
	}
	for _, z := range n.zones {
		content.Zones = append(content.Zones, choice{ID: z.ID, Name: z.Name, Detail: z.Description, Checked: slices.Contains(f.ZoneIDs, z.ID)})
	}
	title := "New access rule"
	if stored.ID != 0 {
		title, content.Action = "Access rule", rulePath(g.ID, stored.ID)
	}
	a.render(w, r, status, ruleTemplate, title, &viewer, content)
}

func (a *App) newRulePage(w http.ResponseWriter, r *http.Request, viewer auth.User) {
	if g, ok := a.pageRuleGroup(w, r, viewer); ok {
		a.showRule(w, r, http.StatusOK, viewer, g, connector.StoredRule{}, rule.Fields{GroupID: g.ID}, nil)
	}
}

func (a *App) createRuleFromForm(w http.ResponseWriter, r *http.Request, viewer auth.User) {
	g, ok := a.pageRuleGroup(w, r, viewer)
	if !ok {
		return
	}
	f := readRuleForm(r, g.ID)
	_, err := a.rules.Create(r.Context(), f)
	if errs, rejected := fieldErrors(err); rejected {
		a.showRule(w, r, http.StatusUnprocessableEntity, viewer, g, connector.StoredRule{}, f, errs)
		return
	}
	a.changed(w, r, err, groupPath(g.ID), "rule-created")
}

func (a *App) rulePage(w http.ResponseWriter, r *http.Request, viewer auth.User) {
	if g, stored, ok := a.pageRule(w, r, viewer); ok {
		a.showRule(w, r, http.StatusOK, viewer, g, stored, rule.FieldsOf(stored), nil)
	}
}

func (a *App) saveRuleFromForm(w http.ResponseWriter, r *http.Request, viewer auth.User) {
	g, stored, ok := a.pageRule(w, r, viewer)
	if !ok {
		return
	}
	f := readRuleForm(r, g.ID)
	_, err := a.rules.Update(r.Context(), stored.ID, f)
	if errs, rejected := fieldErrors(err); rejected {
		a.showRule(w, r, http.StatusUnprocessableEntity, viewer, g, stored, f, errs)
		return
	}
	a.changed(w, r, err, groupPath(g.ID), "rule-saved")
}

func (a *App) deleteRuleFromForm(w http.ResponseWriter, r *http.Request, viewer auth.User) {
	if g, stored, ok := a.pageRule(w, r, viewer); ok {
		a.changed(w, r, a.rules.Delete(r.Context(), stored.ID), groupPath(g.ID), "rule-deleted")
	}
}

// throughText says through which of the zones named a rule reaches a
// door.
func throughText(zones []nameRef) string {
	var zoneNames []string
	for _, z := range zones {
		zoneNames = append(zoneNames, z.Name)
	}
	if len(zoneNames) == 1 {
		return "Zone " + zoneNames[0]
	}
	return "Zones " + strings.Join(zoneNames, ", ")
}
