package app

import (
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/dakriy/doorcode/internal/auth"
	"github.com/dakriy/doorcode/internal/connector"
	"github.com/dakriy/doorcode/internal/date"
	"github.com/dakriy/doorcode/internal/schedule"
)

func (a *App) schedulePageRoutes() []route {
	schedules := holding(auth.RoleManageSchedules)
	return []route{
		pageRoute(http.MethodGet, "/admin/schedules", schedules, a.schedulesPage),
		pageRoute(http.MethodGet, "/admin/schedules/new", schedules, a.newSchedulePage),
		pageRoute(http.MethodPost, "/admin/schedules", schedules, a.createScheduleFromForm),
		pageRoute(http.MethodGet, "/admin/schedules/{id}", schedules, a.schedulePage),
		pageRoute(http.MethodPost, "/admin/schedules/{id}", schedules, a.saveScheduleFromForm),
		pageRoute(http.MethodPost, "/admin/schedules/{id}/delete", schedules, a.deleteScheduleFromForm),
		pageRoute(http.MethodGet, "/admin/schedules/{id}/calendar", schedules, a.scheduleCalendarPage),
	}
}

// dayLabels names the weekdays on pages, in the order of
// date.DayNames.
var dayLabels = []string{"Mon", "Tue", "Wed", "Thu", "Fri", "Sat", "Sun"}

// dayChoice is one weekday checkbox of the schedule form.
type dayChoice struct {
	Value string
	Label string
}

var dayChoices = func() []dayChoice {
	choices := []dayChoice{}
	for i, name := range date.DayNames {
		choices = append(choices, dayChoice{Value: name, Label: dayLabels[i]})
	}
	return choices
}()

// scheduleRow is one schedule as the list shows it.
type scheduleRow struct {
	ID       int64
	Name     string
	Builtin  bool
	Windows  []string
	Range    string
	Excluded int
}

func rowOfSchedule(sch connector.StoredSchedule) scheduleRow {
	return scheduleRow{
		ID: sch.ID, Name: sch.Name, Builtin: sch.Builtin, Windows: windowSummaries(sch.Windows),
		Range: rangeText(sch.StartsOn, sch.EndsOn), Excluded: len(sch.Excluded),
	}
}

func (a *App) schedulesPage(w http.ResponseWriter, r *http.Request, viewer auth.User) {
	all, err := a.schedules.Schedules(r.Context())
	if err != nil {
		a.pageError(w, r, err)
		return
	}
	rows := []scheduleRow{}
	for _, sch := range all {
		rows = append(rows, rowOfSchedule(sch))
	}
	a.render(w, r, http.StatusOK, schedulesTemplate, "Schedules", &viewer, rows)
}

// scheduleForm is the schedule form, as entered.
type scheduleForm struct {
	schedule.Fields
	Errors map[string]string
	// Action is where the form is submitted.
	Action string
}

// newWindow is the window a form starts with and adds.
var newWindow = schedule.WindowFields{Days: []string{}, Start: "09:00", End: "17:00"}

// formCount reads the form field name as a count of rows. It reads at
// most one more than the larger limit, on windows or excluded ranges, so a
// form over it is told so without reading without end.
func formCount(r *http.Request, name string) int {
	n, _ := strconv.Atoi(r.PostFormValue(name))
	return min(n, schedule.MaxExcluded+1)
}

func readScheduleForm(r *http.Request, action string) scheduleForm {
	_ = r.ParseForm()
	f := scheduleForm{Action: action, Fields: schedule.Fields{
		Name: r.PostFormValue("name"), StartsOn: r.PostFormValue("starts_on"), EndsOn: r.PostFormValue("ends_on"),
		Windows: []schedule.WindowFields{}, Excluded: []schedule.ExclusionFields{},
	}}
	for i := range formCount(r, "windows") {
		field := fmt.Sprintf("window-%d-", i)
		f.Windows = append(f.Windows, schedule.WindowFields{
			Days: r.PostForm[field+"days"], Start: r.PostFormValue(field + "start"), End: r.PostFormValue(field + "end"),
		})
	}
	for i := range formCount(r, "dates") {
		field := fmt.Sprintf("excluded-%d-", i)
		f.Excluded = append(f.Excluded, schedule.ExclusionFields{First: r.PostFormValue(field + "first"), Last: r.PostFormValue(field + "last")})
	}
	return f
}

// edited is f changed by the add or remove button r was submitted with,
// and whether r was submitted with one rather than to save.
func (f scheduleForm) edited(r *http.Request) (scheduleForm, bool) {
	switch {
	case r.PostForm.Has("add_window"):
		f.Windows = append(slices.Clone(f.Windows), newWindow)
	case r.PostForm.Has("remove_window"):
		f.Windows = without(f.Windows, r.PostFormValue("remove_window"))
	case r.PostForm.Has("add_date"):
		f.Excluded = append(slices.Clone(f.Excluded), schedule.ExclusionFields{})
	case r.PostForm.Has("remove_date"):
		f.Excluded = without(f.Excluded, r.PostFormValue("remove_date"))
	default:
		return f, false
	}
	return f, true
}

// without is items without the one at the index value names, unchanged
// when it names none.
func without[T any](items []T, value string) []T {
	i, err := strconv.Atoi(value)
	if err != nil || i < 0 || i >= len(items) {
		return items
	}
	return slices.Delete(slices.Clone(items), i, i+1)
}

// rejected is f carrying the problems err holds, and whether it holds any.
func (f scheduleForm) rejected(err error) (scheduleForm, bool) {
	errs, rejected := fieldErrors(err)
	f.Errors = errs
	return f, rejected
}

func (a *App) newSchedulePage(w http.ResponseWriter, r *http.Request, viewer auth.User) {
	form := scheduleForm{Action: "/admin/schedules", Fields: schedule.Fields{Windows: []schedule.WindowFields{newWindow}, Excluded: []schedule.ExclusionFields{}}}
	a.render(w, r, http.StatusOK, newScheduleTemplate, "New schedule", &viewer, form)
}

func (a *App) createScheduleFromForm(w http.ResponseWriter, r *http.Request, viewer auth.User) {
	form := readScheduleForm(r, "/admin/schedules")
	if form, edited := form.edited(r); edited {
		a.render(w, r, http.StatusOK, newScheduleTemplate, "New schedule", &viewer, form)
		return
	}
	sch, err := a.schedules.Create(r.Context(), form.Fields)
	if form, rejected := form.rejected(err); rejected {
		a.render(w, r, http.StatusUnprocessableEntity, newScheduleTemplate, "New schedule", &viewer, form)
		return
	}
	a.changed(w, r, err, schedulePath(sch.ID), "schedule-created")
}

func schedulePath(id int64) string {
	return "/admin/schedules/" + strconv.FormatInt(id, 10)
}

// scheduleContent is the schedule detail page's content.
type scheduleContent struct {
	Schedule connector.StoredSchedule
	Row      scheduleRow
	Form     scheduleForm
	// Alert is a refusal of a change to the schedule.
	Alert string
}

// pageSchedule returns the content of the page about the schedule the path
// names, with its form as it starts, after showing that there is none or
// that the store failed.
func (a *App) pageSchedule(w http.ResponseWriter, r *http.Request, viewer auth.User) (scheduleContent, bool) {
	sch, err := a.schedules.Schedule(r.Context(), pathID(r, "id"))
	if a.unloaded(w, r, viewer, err, schedule.ErrNoSchedule, "There is no schedule here. It may have been deleted.") {
		return scheduleContent{}, false
	}
	form := scheduleForm{Fields: schedule.FieldsOf(sch), Action: schedulePath(sch.ID)}
	return scheduleContent{Schedule: sch, Row: rowOfSchedule(sch), Form: form}, true
}

func (a *App) renderSchedule(w http.ResponseWriter, r *http.Request, status int, viewer auth.User, content scheduleContent) {
	a.render(w, r, status, scheduleTemplate, content.Schedule.Name, &viewer, content)
}

func (a *App) schedulePage(w http.ResponseWriter, r *http.Request, viewer auth.User) {
	if content, ok := a.pageSchedule(w, r, viewer); ok {
		a.renderSchedule(w, r, http.StatusOK, viewer, content)
	}
}

func (a *App) saveScheduleFromForm(w http.ResponseWriter, r *http.Request, viewer auth.User) {
	content, ok := a.pageSchedule(w, r, viewer)
	if !ok {
		return
	}
	form := readScheduleForm(r, content.Form.Action)
	if form, edited := form.edited(r); edited {
		content.Form = form
		a.renderSchedule(w, r, http.StatusOK, viewer, content)
		return
	}
	_, err := a.changeSchedule(r.Context(), content.Schedule.ID, form.Fields)
	if form, rejected := form.rejected(err); rejected {
		content.Form = form
		a.renderSchedule(w, r, http.StatusUnprocessableEntity, viewer, content)
		return
	}
	a.scheduleChanged(w, r, viewer, content, err, schedulePath(content.Schedule.ID), "schedule-saved")
}

func (a *App) deleteScheduleFromForm(w http.ResponseWriter, r *http.Request, viewer auth.User) {
	if content, ok := a.pageSchedule(w, r, viewer); ok {
		err := a.schedules.Delete(r.Context(), content.Schedule.ID)
		a.scheduleChanged(w, r, viewer, content, err, "/admin/schedules", "schedule-deleted")
	}
}

// scheduleChanged finishes a change made from the page content describes:
// a refusal is shown on the page, a failure as an error, and success by
// going to location with flash.
func (a *App) scheduleChanged(w http.ResponseWriter, r *http.Request, viewer auth.User, content scheduleContent, err error, location, flash string) {
	var inUse schedule.InUseError
	if errors.Is(err, schedule.ErrBuiltin) {
		content.Alert = "Always is built in: it cannot be changed or deleted. Create another schedule for other times."
	} else if errors.As(err, &inUse) {
		content.Alert = inUseText(inUse)
	}
	if content.Alert != "" {
		a.renderSchedule(w, r, http.StatusConflict, viewer, content)
		return
	}
	a.changed(w, r, err, location, flash)
}

// inUseText says what uses a schedule that cannot be deleted, and what to
// do about it.
func inUseText(inUse schedule.InUseError) string {
	var parts []string
	if inUse.Zones != nil {
		parts = append(parts, "Zones are in open mode during this schedule: "+strings.Join(inUse.Zones, ", ")+". Take it off them before deleting it.")
	}
	if inUse.Groups != nil {
		parts = append(parts, "Access rules of these groups use this schedule: "+strings.Join(inUse.Groups, ", ")+". Change or delete those rules before deleting it.")
	}
	return strings.Join(parts, " ")
}

// calendarContent is the schedule calendar page's content.
type calendarContent struct {
	Calendar week
	Schedule connector.StoredSchedule
	Row      scheduleRow
	Rows     []occurrenceRow
}

func (a *App) scheduleCalendarPage(w http.ResponseWriter, r *http.Request, viewer auth.User) {
	zone, ok := a.siteZone(w, r)
	if !ok {
		return
	}
	content, ok := a.pageSchedule(w, r, viewer)
	if !ok {
		return
	}
	calendar := calendarContent{
		Calendar: a.weekOf(r, zone, schedulePath(content.Schedule.ID)+"/calendar"), Schedule: content.Schedule, Row: content.Row, Rows: []occurrenceRow{},
	}
	events := []calendarEvent{}
	for _, in := range schedule.ExpandDays(content.Schedule, zone, calendar.Calendar.days.first, calendar.Calendar.days.last) {
		calendar.Rows = append(calendar.Rows, rowOfOccurrence(in))
		events = append(events, eventOf(in, content.Schedule.Name, eventAccess))
	}
	calendar.Calendar.Events = eventsText(events)
	a.render(w, r, http.StatusOK, scheduleCalendarTemplate, content.Schedule.Name, &viewer, calendar)
}

// lengthText says how long d is, in hours and minutes.
func lengthText(d time.Duration) string {
	hours, minutes := int(d.Hours()), int(d.Minutes())%60
	if minutes == 0 {
		return fmt.Sprintf("%d h", hours)
	}
	if hours == 0 {
		return fmt.Sprintf("%d min", minutes)
	}
	return fmt.Sprintf("%d h %d min", hours, minutes)
}

// windowSummaries says when each window opens and closes.
func windowSummaries(windows []connector.StoredWindow) []string {
	out := []string{}
	for _, w := range windows {
		out = append(out, windowSummary(w))
	}
	return out
}

func windowSummary(w connector.StoredWindow) string {
	days := daysText(w.Days)
	if w.Start == 0 && w.End == 0 {
		return days + ", all day"
	}
	times := schedule.ClockText(w.Start) + "–" + schedule.ClockText(w.End)
	if w.End <= w.Start {
		times += " next day"
	}
	return days + " " + times
}

// daysText names days, which hold no repeats, Monday first, with runs of
// three or more consecutive days named from the first to the last.
func daysText(days []time.Weekday) string {
	if len(days) == len(dayLabels) {
		return "Every day"
	}
	held := func(i int) bool { return slices.Contains(days, date.Weekday(i)) }
	var parts []string
	// start is where the run of held days that reaches i begins.
	start := 0
	for i := range dayLabels {
		if !held(i) {
			start = i + 1
			continue
		}
		if i+1 == len(dayLabels) || !held(i+1) {
			parts = append(parts, runText(start, i))
		}
	}
	return strings.Join(parts, ", ")
}

// runText names the consecutive days from index first to index last.
func runText(first, last int) string {
	switch last - first {
	case 0:
		return dayLabels[first]
	case 1:
		return dayLabels[first] + ", " + dayLabels[last]
	}
	return dayLabels[first] + "–" + dayLabels[last]
}

// rangeText says when a schedule is in effect.
func rangeText(startsOn, endsOn *time.Time) string {
	if startsOn != nil && endsOn != nil {
		return startsOn.Format(longDateLayout) + " – " + endsOn.Format(longDateLayout)
	}
	if startsOn != nil {
		return "From " + startsOn.Format(longDateLayout)
	}
	if endsOn != nil {
		return "Until " + endsOn.Format(longDateLayout)
	}
	return "Every week"
}
