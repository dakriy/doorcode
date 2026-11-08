package app

import (
	"errors"
	"net/http"

	"github.com/dakriy/doorcode/internal/auth"
	"github.com/dakriy/doorcode/internal/directory"
)

// importPath is the bulk import's page.
const importPath = "/admin/users/import"

func (a *App) importPageRoutes() []route {
	users := holding(auth.RoleManageUsers)
	return []route{
		pageRoute(http.MethodGet, importPath, users, a.importPage),
		pageRoute(http.MethodPost, importPath, users, a.previewImportFromForm),
		pageRoute(http.MethodPost, importPath+"/apply", users, a.applyImportFromForm),
	}
}

// importContent is the import page's content: what is wrong with the file
// uploaded, and how large one may be.
type importContent struct {
	Error  string
	MaxKiB int
}

// importedContent is the content of the page showing what importing a
// file would do or did. CSV is the file, which the page sends again to
// apply it, so the server keeps nothing between the two.
type importedContent struct {
	importReport
	Shown []importedRow
	CSV   string
	// Alert says the store stopped applying the import, naming the
	// failure.
	Alert string
}

// importedRow is a row of an import as its page shows it: Label says what
// importing it does or did, and Kind how that is drawn: create, update,
// unchanged, skip, or failed for a row the store stopped before.
type importedRow struct {
	importRowView
	Label string
	Kind  string
}

// previewLabels and appliedLabels name each outcome on the preview and on
// the result.
var (
	previewLabels = map[string]string{
		directory.ImportCreate: "New user", directory.ImportUpdate: "Joins groups",
		directory.ImportUnchanged: "Nothing to change", directory.ImportSkip: "Error",
	}
	appliedLabels = map[string]string{
		directory.ImportCreate: "Created", directory.ImportUpdate: "Added to groups",
		directory.ImportUnchanged: "Nothing to change", directory.ImportSkip: "Skipped",
	}
)

// importedOf is the page showing report, of the import file csv.
func importedOf(report importReport, csv string) importedContent {
	content := importedContent{importReport: report, CSV: csv}
	labels := previewLabels
	if report.Applied {
		labels = appliedLabels
	}
	for _, row := range report.Rows {
		shown := importedRow{importRowView: row, Label: labels[row.Outcome], Kind: row.Outcome}
		if report.Applied && !row.Applied && (row.Outcome == directory.ImportCreate || row.Outcome == directory.ImportUpdate) {
			shown.Label, shown.Kind = "Not applied", "failed"
		}
		content.Shown = append(content.Shown, shown)
	}
	return content
}

func (a *App) importPage(w http.ResponseWriter, r *http.Request, viewer auth.User) {
	a.renderImport(w, r, http.StatusOK, viewer, "")
}

func (a *App) renderImport(w http.ResponseWriter, r *http.Request, status int, viewer auth.User, problem string) {
	a.render(w, r, status, importTemplate, "Import users", &viewer, importContent{Error: problem, MaxKiB: directory.MaxImportKiB})
}

// importRefused shows the import page saying why err keeps the file from
// being read, and reports whether it did; it shows nothing for any other
// error.
func (a *App) importRefused(w http.ResponseWriter, r *http.Request, viewer auth.User, err error) bool {
	if errors.Is(err, directory.ErrImportTooBig) {
		a.renderImport(w, r, http.StatusRequestEntityTooLarge, viewer, sentence(err.Error())+".")
		return true
	}
	errs, rejected := fieldErrors(err)
	if rejected {
		a.renderImport(w, r, http.StatusUnprocessableEntity, viewer, errs[importField])
	}
	return rejected
}

func (a *App) previewImportFromForm(w http.ResponseWriter, r *http.Request, viewer auth.User) {
	zone, ok := a.siteZone(w, r)
	if !ok {
		return
	}
	file, err := readImportFile(w, r)
	if a.importRefused(w, r, viewer, err) {
		return
	}
	imp, err := a.directory.PreviewImport(r.Context(), viewer, file, zone)
	if a.importRefused(w, r, viewer, err) {
		return
	}
	if err != nil {
		a.pageError(w, err)
		return
	}
	content := importedOf(reportOf(imp, false), string(file))
	a.render(w, r, http.StatusOK, importedTemplate, "Import preview", &viewer, content)
}

func (a *App) applyImportFromForm(w http.ResponseWriter, r *http.Request, viewer auth.User) {
	zone, ok := a.siteZone(w, r)
	if !ok {
		return
	}
	imp, err := a.directory.ApplyImport(r.Context(), viewer, []byte(r.PostFormValue("csv")), zone)
	if a.importRefused(w, r, viewer, err) {
		return
	}
	content := importedOf(reportOf(imp, true), "")
	status := http.StatusOK
	if err != nil && len(imp.Rows) == 0 {
		a.pageError(w, err)
		return
	}
	if err != nil {
		status = http.StatusInternalServerError
		content.Alert = "The server failed partway, so only the rows marked done were applied. Apply the same file again to finish: " +
			"rows done already show as nothing to change. Give an administrator the request id " + a.logFailure(err) + "."
	}
	a.render(w, r, status, importedTemplate, "Import result", &viewer, content)
}
