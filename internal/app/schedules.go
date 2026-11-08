package app

import (
	"fmt"
	"net/http"
	"time"

	"github.com/dakriy/doorcode/internal/auth"
	"github.com/dakriy/doorcode/internal/connector"
	"github.com/dakriy/doorcode/internal/openapi"
	"github.com/dakriy/doorcode/internal/schedule"
)

// maxOccurrenceSpan is how many days after from the last day of one
// request for a schedule's occurrences may be.
const maxOccurrenceSpan = 365

type windowBody struct {
	Days  []string `json:"days" doc:"The weekdays the window opens on: mon, tue, wed, thu, fri, sat, sun."`
	Start string   `json:"start" doc:"When the window opens, as HH:MM in the site timezone."`
	End   string   `json:"end" doc:"When the window closes, as HH:MM in the site timezone. An end at or before the start is on the next day, so 00:00 to 00:00 is all day."`
}

type scheduleRequest struct {
	Name          string       `json:"name" doc:"The schedule's name, up to 100 characters; unique, ignoring case."`
	StartsOn      string       `json:"starts_on,omitempty" doc:"The first day, YYYY-MM-DD, on which windows open. Omit it for a schedule that has always been in effect."`
	EndsOn        string       `json:"ends_on,omitempty" doc:"The last day, YYYY-MM-DD, on which windows open. Omit it for a schedule that never ends."`
	Windows       []windowBody `json:"windows" doc:"One to 20 weekly windows."`
	ExcludedDates []string     `json:"excluded_dates,omitempty" doc:"Up to 366 dates, YYYY-MM-DD, on which no window opens."`
}

type scheduleView struct {
	ID            int64        `json:"id" doc:"Names the schedule in the API."`
	Name          string       `json:"name" doc:"The schedule's name."`
	StartsOn      string       `json:"starts_on,omitempty" doc:"The first day, YYYY-MM-DD, on which windows open; absent when the schedule has always been in effect."`
	EndsOn        string       `json:"ends_on,omitempty" doc:"The last day, YYYY-MM-DD, on which windows open; absent when the schedule never ends."`
	Windows       []windowBody `json:"windows" doc:"The weekly windows, in the order they were given."`
	ExcludedDates []string     `json:"excluded_dates" doc:"The dates, YYYY-MM-DD, on which no window opens, in order."`
	Builtin       bool         `json:"builtin" doc:"True for the seeded Always schedule, which cannot be changed or deleted."`
}

type scheduleList struct {
	Schedules []scheduleView `json:"schedules" doc:"Every schedule, by name."`
}

type occurrenceView struct {
	Start time.Time `json:"start" doc:"When the schedule becomes active, with the site timezone's offset."`
	End   time.Time `json:"end" doc:"When it stops being active."`
}

type occurrenceList struct {
	TimeZone    string           `json:"time_zone" doc:"The site timezone the schedule's windows are read in."`
	From        string           `json:"from" doc:"The first day covered, YYYY-MM-DD."`
	To          string           `json:"to" doc:"The last day covered, YYYY-MM-DD."`
	Occurrences []occurrenceView `json:"occurrences" doc:"When the schedule is active during those days, in order. Overlapping or touching windows are joined, and an occurrence that runs past the range is cut at it."`
}

var (
	scheduleIDParam = openapi.Param{Name: "id", In: "path", Description: "The schedule's id. The Always schedule has id 1.", Example: int64(1)}
	noSchedule      = openapi.Response{Status: http.StatusNotFound, Description: "No schedule has this id.", Body: errorBody{}}
	builtinRefused  = openapi.Response{Status: http.StatusConflict, Description: "The schedule is the built-in Always schedule, which cannot be changed or deleted.", Body: errorBody{}}
	deleteRefused   = openapi.Response{Status: http.StatusConflict, Description: "The schedule is the built-in Always schedule, which cannot be deleted, or doors that are not deleted are in open mode during it; the error names them.", Body: errorBody{}}
	badSchedule     = openapi.Response{Status: http.StatusBadRequest, Description: "The body is not a JSON schedule, or a field is wrong; the error says which.", Body: errorBody{}}
	occurrenceRange = []openapi.Param{
		scheduleIDParam,
		{Name: "from", In: "query", Description: "The first day to cover, YYYY-MM-DD in the site timezone; by default the Monday of the current week.", Example: "2026-11-09"},
		{Name: "to", In: "query", Description: fmt.Sprintf("The last day to cover, YYYY-MM-DD, at most %d days after from; by default the Sunday after from.", maxOccurrenceSpan), Example: "2026-11-15"},
	}
)

func (a *App) scheduleRoutes() []route {
	schedules := holding(auth.RoleManageSchedules)
	example := scheduleRequest{
		Name: "Night shift", StartsOn: "2026-01-01", EndsOn: "2026-12-31",
		Windows:       []windowBody{{Days: []string{"mon", "tue", "wed", "thu", "fri"}, Start: "22:00", End: "06:00"}},
		ExcludedDates: []string{"2026-12-25"},
	}
	return []route{
		apiRoute(http.MethodGet, "/schedules", schedules, a.listSchedules, apiDoc{
			id: "listSchedules", summary: "List every schedule", tag: "schedules",
			responses: []openapi.Response{{Status: http.StatusOK, Description: "Every schedule.", Body: scheduleList{}}},
		}),
		apiRoute(http.MethodPost, "/schedules", schedules, a.createSchedule, apiDoc{
			id: "createSchedule", summary: "Create a schedule", tag: "schedules", request: example,
			responses: []openapi.Response{{Status: http.StatusCreated, Description: "The new schedule.", Body: scheduleView{}}, badSchedule},
		}),
		apiRoute(http.MethodGet, "/schedules/{id}", schedules, a.getSchedule, apiDoc{
			id: "getSchedule", summary: "Read one schedule", tag: "schedules", params: []openapi.Param{scheduleIDParam},
			responses: []openapi.Response{{Status: http.StatusOK, Description: "The schedule.", Body: scheduleView{}}, noSchedule},
		}),
		apiRoute(http.MethodPut, "/schedules/{id}", schedules, a.updateSchedule, apiDoc{
			id: "updateSchedule", summary: "Replace a schedule", tag: "schedules", params: []openapi.Param{scheduleIDParam}, request: example,
			responses: []openapi.Response{
				{Status: http.StatusOK, Description: "The schedule as it is now.", Body: scheduleView{}}, badSchedule, noSchedule, builtinRefused,
			},
		}),
		apiRoute(http.MethodDelete, "/schedules/{id}", schedules, a.deleteSchedule, apiDoc{
			id: "deleteSchedule", summary: "Delete a schedule", tag: "schedules", params: []openapi.Param{scheduleIDParam},
			responses: []openapi.Response{{Status: http.StatusNoContent, Description: "The schedule is gone. Deleted doors stop being in open mode during it."}, noSchedule, deleteRefused},
		}),
		apiRoute(http.MethodGet, "/schedules/{id}/occurrences", schedules, a.listOccurrences, apiDoc{
			id: "listScheduleOccurrences", summary: "List when a schedule is active", tag: "schedules", params: occurrenceRange,
			responses: []openapi.Response{
				{Status: http.StatusOK, Description: "When the schedule is active during the days asked for.", Body: occurrenceList{}},
				{Status: http.StatusBadRequest, Description: "from or to is not a date, or the range is backwards or too long.", Body: errorBody{}},
				noSchedule,
			},
		}),
	}
}

func viewOfSchedule(sch connector.StoredSchedule) scheduleView {
	f := schedule.FieldsOf(sch)
	windows := []windowBody{}
	for _, w := range f.Windows {
		windows = append(windows, windowBody(w))
	}
	return scheduleView{
		ID: sch.ID, Name: f.Name, StartsOn: f.StartsOn, EndsOn: f.EndsOn,
		Windows: windows, ExcludedDates: f.Excluded, Builtin: sch.Builtin,
	}
}

// scheduleStatuses maps each refusal the schedules report to the status
// the API answers it with.
var scheduleStatuses = []refusalStatus{
	{schedule.ErrNoSchedule, http.StatusNotFound},
	{schedule.ErrBuiltin, http.StatusConflict},
	{schedule.ErrInUse, http.StatusConflict},
}

// scheduleError answers an API request with what err, from the schedules,
// means.
func (a *App) scheduleError(w http.ResponseWriter, err error) {
	a.refuse(w, err, scheduleStatuses)
}

func (a *App) listSchedules(w http.ResponseWriter, r *http.Request, _ auth.User) {
	all, err := a.schedules.Schedules(r.Context())
	if err != nil {
		a.internalError(w, err)
		return
	}
	views := []scheduleView{}
	for _, sch := range all {
		views = append(views, viewOfSchedule(sch))
	}
	writeJSON(w, http.StatusOK, scheduleList{Schedules: views})
}

// readSchedule reads a JSON schedule request.
func readSchedule(w http.ResponseWriter, r *http.Request) (schedule.Fields, bool) {
	var body scheduleRequest
	if err := decodeJSON(w, r, &body); err != nil {
		writeError(w, http.StatusBadRequest, `body must be a JSON object with a string "name" and a "windows" array`)
		return schedule.Fields{}, false
	}
	f := schedule.Fields{Name: body.Name, StartsOn: body.StartsOn, EndsOn: body.EndsOn, Excluded: body.ExcludedDates}
	for _, w := range body.Windows {
		f.Windows = append(f.Windows, schedule.WindowFields(w))
	}
	return f, true
}

func (a *App) createSchedule(w http.ResponseWriter, r *http.Request, _ auth.User) {
	f, ok := readSchedule(w, r)
	if !ok {
		return
	}
	sch, err := a.schedules.Create(r.Context(), f)
	if err != nil {
		a.scheduleError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, viewOfSchedule(sch))
}

func (a *App) getSchedule(w http.ResponseWriter, r *http.Request, _ auth.User) {
	sch, err := a.schedules.Schedule(r.Context(), pathID(r, "id"))
	if err != nil {
		a.scheduleError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, viewOfSchedule(sch))
}

func (a *App) updateSchedule(w http.ResponseWriter, r *http.Request, _ auth.User) {
	f, ok := readSchedule(w, r)
	if !ok {
		return
	}
	sch, err := a.schedules.Update(r.Context(), pathID(r, "id"), f)
	if err != nil {
		a.scheduleError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, viewOfSchedule(sch))
}

func (a *App) deleteSchedule(w http.ResponseWriter, r *http.Request, _ auth.User) {
	if err := a.schedules.Delete(r.Context(), pathID(r, "id")); err != nil {
		a.scheduleError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (a *App) listOccurrences(w http.ResponseWriter, r *http.Request, _ auth.User) {
	current, err := a.settings.Current(r.Context())
	if err != nil {
		a.internalError(w, err)
		return
	}
	first, last, ok := occurrenceDays(r, a.today(current.Zone))
	if !ok {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("from and to must be dates as YYYY-MM-DD, with to on or after from and at most %d days after it", maxOccurrenceSpan))
		return
	}
	sch, err := a.schedules.Schedule(r.Context(), pathID(r, "id"))
	if err != nil {
		a.scheduleError(w, err)
		return
	}
	views := []occurrenceView{}
	for _, in := range schedule.ExpandDays(sch, current.Zone, first, last) {
		views = append(views, occurrenceView{Start: in.Start, End: in.End})
	}
	writeJSON(w, http.StatusOK, occurrenceList{
		TimeZone: current.Zone.String(), From: first.Format(schedule.DateLayout), To: last.Format(schedule.DateLayout), Occurrences: views,
	})
}

// occurrenceDays reads the days an occurrences request covers, first to
// last, by default the week today falls in, and whether they are a range
// it may ask for.
func occurrenceDays(r *http.Request, today time.Time) (time.Time, time.Time, bool) {
	first, firstOK := queryDate(r, "from", monday(today))
	last, lastOK := queryDate(r, "to", first.AddDate(0, 0, 6))
	inRange := !last.Before(first) && !last.After(first.AddDate(0, 0, maxOccurrenceSpan))
	return first, last, firstOK && lastOK && inRange
}

// queryDate reads the query parameter name as a date, fallback when it is
// absent.
func queryDate(r *http.Request, name string, fallback time.Time) (time.Time, bool) {
	value := r.URL.Query().Get(name)
	if value == "" {
		return fallback, true
	}
	day, err := time.Parse(schedule.DateLayout, value)
	return day, err == nil
}

// today is the date it is now in zone, as midnight UTC on that date.
func (a *App) today(zone *time.Location) time.Time {
	return schedule.DateOf(a.clock.Now(), zone)
}

// monday is the Monday on or before day.
func monday(day time.Time) time.Time {
	back := (int(day.Weekday()) + 6) % 7
	return day.AddDate(0, 0, -back)
}
