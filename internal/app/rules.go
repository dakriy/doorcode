package app

import (
	"context"
	"net/http"

	"github.com/dakriy/doorcode/internal/auth"
	"github.com/dakriy/doorcode/internal/connector"
	"github.com/dakriy/doorcode/internal/openapi"
	"github.com/dakriy/doorcode/internal/rule"
)

type ruleView struct {
	ID       int64     `json:"id" doc:"Names the access rule in the API."`
	Group    nameRef   `json:"group" doc:"The group whose members the rule lets in."`
	Schedule nameRef   `json:"schedule" doc:"The schedule during which it lets them in."`
	Zones    []nameRef `json:"zones" doc:"The zones it names, by name; it reaches every door in them."`
}

type ruleRequest struct {
	GroupID    int64   `json:"group_id" doc:"The id of the group whose members the rule lets in."`
	ScheduleID int64   `json:"schedule_id" doc:"The id of the schedule during which it lets them in; the Always schedule has id 1."`
	ZoneIDs    []int64 `json:"zone_ids,omitempty" doc:"The ids of the zones it names, at least one; it reaches every door in them."`
}

type ruleList struct {
	Rules []ruleView `json:"rules" doc:"The access rules, oldest first."`
}

type entrantView struct {
	RuleID   int64     `json:"rule_id" doc:"The access rule that lets them in."`
	Group    nameRef   `json:"group" doc:"The group whose members may enter."`
	Schedule nameRef   `json:"schedule" doc:"The schedule during which they may enter."`
	Zones    []nameRef `json:"zones" doc:"The zones the door is in through which the rule reaches it, by name."`
}

type entrantList struct {
	Entrants []entrantView `json:"entrants" doc:"Each access rule that reaches the door, oldest first."`
}

var (
	ruleIDParam = openapi.Param{Name: "id", In: "path", Description: "The access rule's id.", Example: int64(1)}
	noRule      = openapi.Response{Status: http.StatusNotFound, Description: "No access rule has this id.", Body: errorBody{}}
	badRule     = openapi.Response{Status: http.StatusBadRequest, Description: "The body is not a JSON access rule, or a field is wrong; the error says which.", Body: errorBody{}}
)

func (a *App) ruleRoutes() []route {
	groups := holding(auth.RoleManageGroups)
	example := ruleRequest{GroupID: 1, ScheduleID: 1, ZoneIDs: []int64{1}}
	return []route{
		apiRoute(http.MethodGet, "/access-rules", groups, a.listRules, apiDoc{
			id: "listAccessRules", summary: "List access rules", tag: "access rules",
			params: []openapi.Param{{Name: "group", In: "query", Description: "Keeps the rules of the group with this id.", Example: int64(1)}},
			responses: []openapi.Response{
				{Status: http.StatusOK, Description: "The access rules.", Body: ruleList{}},
				{Status: http.StatusBadRequest, Description: "group is not a group's id.", Body: errorBody{}},
			},
		}),
		apiRoute(http.MethodPost, "/access-rules", groups, a.createRule, apiDoc{
			id: "createAccessRule", summary: "Create an access rule", tag: "access rules", request: example,
			responses: []openapi.Response{{Status: http.StatusCreated, Description: "The new access rule.", Body: ruleView{}}, badRule},
		}),
		apiRoute(http.MethodGet, "/access-rules/{id}", groups, a.getRule, apiDoc{
			id: "getAccessRule", summary: "Read one access rule", tag: "access rules", params: []openapi.Param{ruleIDParam},
			responses: []openapi.Response{{Status: http.StatusOK, Description: "The access rule.", Body: ruleView{}}, noRule},
		}),
		apiRoute(http.MethodPut, "/access-rules/{id}", groups, a.updateRule, apiDoc{
			id: "updateAccessRule", summary: "Replace an access rule", tag: "access rules", params: []openapi.Param{ruleIDParam}, request: example,
			responses: []openapi.Response{{Status: http.StatusOK, Description: "The access rule as it is now.", Body: ruleView{}}, badRule, noRule},
		}),
		apiRoute(http.MethodDelete, "/access-rules/{id}", groups, a.deleteRule, apiDoc{
			id: "deleteAccessRule", summary: "Delete an access rule", tag: "access rules", params: []openapi.Param{ruleIDParam},
			responses: []openapi.Response{{Status: http.StatusNoContent, Description: "The access rule is gone; its members no longer get in by it."}, noRule},
		}),
		apiRoute(http.MethodGet, "/doors/{id}/entrants", holding(auth.RoleManageDoors), a.listEntrants, apiDoc{
			id: "listDoorEntrants", summary: "List who may enter a door", tag: "doors", params: []openapi.Param{doorIDParam},
			responses: []openapi.Response{{Status: http.StatusOK, Description: "Each access rule that reaches the door through its zones.", Body: entrantList{}}, noDoor},
		}),
	}
}

// names is everything an access rule names, so pages and the API can show
// a rule by name.
type names struct {
	catalog
	groups []connector.StoredGroup
	doors  []connector.StoredDoor
}

func (a *App) names(ctx context.Context) (names, error) {
	c, err := a.catalog(ctx)
	if err != nil {
		return names{}, err
	}
	groups, err := a.directory.Groups(ctx)
	if err != nil {
		return names{}, err
	}
	doors, err := a.doors.Doors(ctx)
	return names{catalog: c, groups: groups, doors: doors}, err
}

// nameOf names the item whose id is id, found among items; one deleted
// since the id was read keeps only its id.
func nameOf[T any](items []T, id int64, ref func(T) nameRef) nameRef {
	found := nameRef{ID: id}
	for _, item := range items {
		if r := ref(item); r.ID == id {
			found = r
		}
	}
	return found
}

func groupName(g connector.StoredGroup) nameRef { return nameRef{ID: g.ID, Name: g.Name} }

func scheduleRef(sch connector.StoredSchedule) nameRef { return nameRef{ID: sch.ID, Name: sch.Name} }

func doorRef(d connector.StoredDoor) nameRef { return nameRef{ID: d.ID, Name: d.Name} }

func zoneRef(z connector.StoredZone) nameRef { return nameRef{ID: z.ID, Name: z.Name} }

func (n names) group(id int64) nameRef { return nameOf(n.groups, id, groupName) }

func (n names) schedule(id int64) nameRef { return nameOf(n.schedules, id, scheduleRef) }

func (n names) viewOfRule(r connector.StoredRule) ruleView {
	return ruleView{
		ID: r.ID, Group: n.group(r.GroupID), Schedule: n.schedule(r.ScheduleID), Zones: refs(n.zones, r.ZoneIDs, zoneRef),
	}
}

// ruleStatuses maps each refusal about access rules to the status the API
// answers it with.
var ruleStatuses = []refusalStatus{
	{rule.ErrNoRule, http.StatusNotFound},
}

// ruleError answers an API request with what err, about access rules,
// means.
func (a *App) ruleError(w http.ResponseWriter, r *http.Request, err error) {
	a.refuse(w, r, err, ruleStatuses)
}

// writeRule answers with r, which err may say could not be read or
// stored, as status.
func (a *App) writeRule(w http.ResponseWriter, r *http.Request, status int, stored connector.StoredRule, err error) {
	if err != nil {
		a.ruleError(w, r, err)
		return
	}
	n, err := a.names(r.Context())
	if err != nil {
		a.internalError(w, r, err)
		return
	}
	writeJSON(w, status, n.viewOfRule(stored))
}

func (a *App) listRules(w http.ResponseWriter, r *http.Request, _ auth.User) {
	group, ok := queryID(r, "group")
	if !ok {
		writeError(w, http.StatusBadRequest, "group must be a group's id")
		return
	}
	all, err := a.rules.Rules(r.Context(), group)
	if err != nil {
		a.internalError(w, r, err)
		return
	}
	n, err := a.names(r.Context())
	if err != nil {
		a.internalError(w, r, err)
		return
	}
	views := []ruleView{}
	for _, stored := range all {
		views = append(views, n.viewOfRule(stored))
	}
	writeJSON(w, http.StatusOK, ruleList{Rules: views})
}

// readRule reads a JSON access rule request.
func readRule(w http.ResponseWriter, r *http.Request) (rule.Fields, bool) {
	var body ruleRequest
	if err := decodeJSON(w, r, &body); err != nil {
		writeError(w, http.StatusBadRequest, `body must be a JSON object with a "group_id" and a "schedule_id"`)
		return rule.Fields{}, false
	}
	return rule.Fields(body), true
}

func (a *App) createRule(w http.ResponseWriter, r *http.Request, _ auth.User) {
	if f, ok := readRule(w, r); ok {
		stored, err := a.rules.Create(r.Context(), f)
		a.writeRule(w, r, http.StatusCreated, stored, err)
	}
}

func (a *App) getRule(w http.ResponseWriter, r *http.Request, _ auth.User) {
	stored, err := a.rules.Rule(r.Context(), pathID(r, "id"))
	a.writeRule(w, r, http.StatusOK, stored, err)
}

func (a *App) updateRule(w http.ResponseWriter, r *http.Request, _ auth.User) {
	if f, ok := readRule(w, r); ok {
		stored, err := a.rules.Update(r.Context(), pathID(r, "id"), f)
		a.writeRule(w, r, http.StatusOK, stored, err)
	}
}

func (a *App) deleteRule(w http.ResponseWriter, r *http.Request, _ auth.User) {
	if err := a.rules.Delete(r.Context(), pathID(r, "id")); err != nil {
		a.ruleError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (n names) viewOfEntrant(reach rule.Reach) entrantView {
	return entrantView{
		RuleID: reach.Rule.ID, Group: n.group(reach.Rule.GroupID), Schedule: n.schedule(reach.Rule.ScheduleID),
		Zones: refs(n.zones, reach.ZoneIDs, zoneRef),
	}
}

func (a *App) listEntrants(w http.ResponseWriter, r *http.Request, _ auth.User) {
	d, err := a.doors.Door(r.Context(), pathID(r, "id"))
	if err != nil {
		a.doorError(w, r, err)
		return
	}
	entrants, err := a.rules.Entrants(r.Context(), d)
	if err != nil {
		a.internalError(w, r, err)
		return
	}
	n, err := a.names(r.Context())
	if err != nil {
		a.internalError(w, r, err)
		return
	}
	views := []entrantView{}
	for _, reach := range entrants {
		views = append(views, n.viewOfEntrant(reach))
	}
	writeJSON(w, http.StatusOK, entrantList{Entrants: views})
}
