package app

import (
	"fmt"
	"net/http"

	"github.com/dakriy/doorcode/internal/auth"
	"github.com/dakriy/doorcode/internal/check"
	"github.com/dakriy/doorcode/internal/directory"
	"github.com/dakriy/doorcode/internal/openapi"
)

// importField is the multipart field holding an import file, named as
// the problems with a whole file are.
const importField = directory.ImportFileField

type importUpload struct {
	File openapi.File `json:"file" doc:"The CSV file. Its first line names the columns: user_id and groups (slugs separated by semicolons), and optionally email, first_name, last_name, and expires_at (YYYY-MM-DD in the site timezone). A row whose user id nobody has creates a user when it gives an email and otherwise puts the user id on the pre-assigned list of each group listed, so the person joins them on first sign-in; a row naming an existing user replaces their email, names, and expiry with the values it gives."`
}

type importProblem struct {
	Column  string `json:"column,omitempty" doc:"The column the problem is in; absent for the row as a whole."`
	Message string `json:"message" doc:"What is wrong."`
}

type importRowView struct {
	Line       int             `json:"line" doc:"The line of the file the row starts on; the header is line 1."`
	UserID     string          `json:"user_id" doc:"The user id the row names."`
	Email      string          `json:"email,omitempty" doc:"The email the row gives, lowercase: a new user's, or an existing user's new one."`
	Groups     []string        `json:"groups" doc:"The group slugs the row lists."`
	Joins      []string        `json:"joins" doc:"The slugs of the groups the user joins: those listed they are not in and, for a new user, those whose pre-assigned user ids hold theirs."`
	Preassigns []string        `json:"preassigns,omitempty" doc:"For a user id nobody has and no email, the slugs of the groups listed whose pre-assigned user ids gain it; the person joins them on first sign-in."`
	Changes    []string        `json:"changes,omitempty" doc:"For an existing user, the columns whose values replace theirs: email, first_name, last_name, or expires_at."`
	Outcome    string          `json:"outcome" doc:"create (a new user who joins the groups), update (an existing user's profile changes or they join groups), unchanged (an existing user the row changes in no way, or a user id on every listed group's pre-assigned list already), preassign (a user id nobody has, without an email, goes on the groups' pre-assigned lists, so the person joins on first sign-in), or skip (the row has problems)."`
	User       *nameRef        `json:"user,omitempty" doc:"The user, by id and display name: the existing user, or the new one once created; absent otherwise."`
	Problems   []importProblem `json:"problems,omitempty" doc:"Why a skipped row is skipped."`
	Applied    bool            `json:"applied" doc:"True when applying the import made the row's change."`
}

type importTotals struct {
	Create    int `json:"create" doc:"How many rows create a user."`
	Update    int `json:"update" doc:"How many rows update an existing user or add them to groups."`
	Unchanged int `json:"unchanged" doc:"How many rows change nothing."`
	Preassign int `json:"preassign" doc:"How many rows pre-assign a user id nobody has to groups, which the person joins on first sign-in."`
	Skip      int `json:"skip" doc:"How many rows are skipped for their problems."`
}

type importReport struct {
	Applied bool            `json:"applied" doc:"True when the import was applied, false for a preview, which changes nothing."`
	Rows    []importRowView `json:"rows" doc:"Every row below the header, in the file's order."`
	Totals  importTotals    `json:"totals" doc:"How many rows have each outcome."`
}

// importFailure is the answer to an import the server failed to apply:
// errorBody's fields and, when it failed partway, the report so far.
type importFailure struct {
	Error     string        `json:"error" doc:"What went wrong."`
	RequestID string        `json:"request_id" doc:"Names the failure in the server log."`
	Report    *importReport `json:"report,omitempty" doc:"What applying the file did until the server failed, each row's applied saying whether its change was made; importing the same file again finishes it. Absent when the server failed before reading the file's rows."`
}

// importStatuses maps each refusal an import reports to the status the
// API answers it with.
var importStatuses = []refusalStatus{{directory.ErrImportTooBig, http.StatusRequestEntityTooLarge}}

func (a *App) importRoutes() []route {
	users := holding(auth.RoleManageUsers)
	refused := []openapi.Response{
		{Status: http.StatusBadRequest, Description: "The body is not a multipart form with the file in the field file, or the file cannot be read as an import: it is not CSV, its header lacks user_id or groups or names another column, or it has no rows or more than 5000.", Body: errorBody{}},
		{Status: http.StatusRequestEntityTooLarge, Description: fmt.Sprintf("The file is larger than %d KiB.", directory.MaxImportKiB), Body: errorBody{}},
	}
	return []route{
		apiRoute(http.MethodPost, "/users/import/preview", users, a.previewImport, apiDoc{
			id: "previewUserImport", summary: "Preview importing users and their groups from CSV", tag: "users", form: importUpload{},
			responses: append([]openapi.Response{{Status: http.StatusOK, Description: "What importing the file would do, row by row. Nothing is changed.", Body: importReport{}}}, refused...),
		}),
		apiRoute(http.MethodPost, "/users/import", users, a.applyImport, apiDoc{
			id: "importUsers", summary: "Import users and their groups from CSV", tag: "users", form: importUpload{},
			responses: append([]openapi.Response{
				{Status: http.StatusOK, Description: "What importing the file did, row by row: users created with no door code and no password until they sign in, existing users updated and added to groups, user ids pre-assigned to groups for people who join them on first sign-in, and skipped rows with their problems. Importing the same file again is safe.", Body: importReport{}},
				{Status: http.StatusInternalServerError, Description: "The server failed; the body names a request id to give an administrator and, when it failed partway through applying the file, reports which rows were applied.", Body: importFailure{}},
			}, refused...),
		}),
	}
}

// notImport is the problem with a body that is not an import upload.
var notImport = check.Problems{{Field: importField, Message: "Send the file as a multipart form with the file in the field file."}}

// readImportFile reads the import file r uploads.
func readImportFile(w http.ResponseWriter, r *http.Request) ([]byte, error) {
	data, ok := readFile(w, r, importField, directory.MaxImportBytes)
	if !ok {
		return nil, notImport
	}
	return data, nil
}

func reportOf(imp directory.Import, applied bool) importReport {
	report := importReport{
		Applied: applied, Rows: []importRowView{},
		Totals: importTotals{
			Create: imp.Count(directory.ImportCreate), Update: imp.Count(directory.ImportUpdate),
			Unchanged: imp.Count(directory.ImportUnchanged), Preassign: imp.Count(directory.ImportPreassign),
			Skip: imp.Count(directory.ImportSkip),
		},
	}
	for _, row := range imp.Rows {
		view := importRowView{
			Line: row.Line, UserID: row.UserID, Email: row.Email, Groups: row.Groups, Joins: row.Joins,
			Preassigns: row.Preassigns, Changes: row.Changes, Outcome: row.Outcome, Applied: row.Applied,
		}
		if row.User != 0 {
			view.User = &nameRef{ID: row.User, Name: row.Name}
		}
		for _, p := range row.Problems {
			view.Problems = append(view.Problems, importProblem{Column: p.Field, Message: p.Message})
		}
		report.Rows = append(report.Rows, view)
	}
	return report
}

// importing reads the file r uploads and previews or, when apply is true,
// applies importing it on behalf of caller, answering with the report.
func (a *App) importing(w http.ResponseWriter, r *http.Request, caller auth.User, apply bool) {
	current, err := a.settings.Current(r.Context())
	if err != nil {
		a.internalError(w, r, err)
		return
	}
	file, err := readImportFile(w, r)
	if err != nil {
		a.refuse(w, r, err, importStatuses)
		return
	}
	run := a.directory.PreviewImport
	if apply {
		run = a.directory.ApplyImport
	}
	imp, err := run(r.Context(), caller, file, current.Zone)
	if err != nil && len(imp.Rows) > 0 {
		report := reportOf(imp, apply)
		failure := importFailure{Error: "internal error", RequestID: a.logFailure(r, err), Report: &report}
		writeJSON(w, http.StatusInternalServerError, failure)
		return
	}
	if err != nil {
		a.refuse(w, r, err, importStatuses)
		return
	}
	writeJSON(w, http.StatusOK, reportOf(imp, apply))
}

func (a *App) previewImport(w http.ResponseWriter, r *http.Request, caller auth.User) {
	a.importing(w, r, caller, false)
}

func (a *App) applyImport(w http.ResponseWriter, r *http.Request, caller auth.User) {
	a.importing(w, r, caller, true)
}
