package entrypoint_test

import (
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/dakriy/doorcode/internal/testsupport"
)

// importSite is an administrator's browser on a site with the groups Staff
// (2, staff), Guests (3, guests), and Lab (4, lab, whose pre-assigned user
// ids hold 100555), and Ann (2, user id 100234) in Staff. The sign-in's
// flash is spent.
func importSite(t *testing.T, f *testsupport.Factory) *testsupport.Browser {
	t.Helper()
	b := signedInAdmin(t, f)
	createGroup(t, b, `{"slug":"staff","name":"Staff"}`)
	createGroup(t, b, `{"slug":"guests","name":"Guests"}`)
	createGroup(t, b, `{"slug":"lab","name":"Lab","preassigned_user_ids":["100555"]}`)
	createUser(t, b, ann)
	join(t, b, "2", "2")
	b.Get("/")
	return b
}

// people is an import file whose first row adds Ann to Guests, whose
// second creates Bo, who joins Guests and, pre-assigned, Lab, and whose
// third names a group that does not exist.
const people = "user_id,groups,email,first_name,last_name\n" +
	"100234,staff;guests,,,\n" +
	"100555,guests,BO@example.com,Bo,Berg\n" +
	"100777,labs,cy@example.com,Cy,\n"

// importHead heads the table of an import's rows.
var importHead = []string{"Line", "User id", "User", "Outcome", "Details"}

// previewOf previews importing csv through the API as c, and returns the
// JSON report.
func previewOf(t *testing.T, c *testsupport.Browser, csv string) string {
	t.Helper()
	got := c.Upload("/api/v1/users/import/preview", "file", "people.csv", []byte(csv))
	if got.Status != 200 {
		t.Fatalf("preview answered %d, want 200:\n%s", got.Status, got.Body)
	}
	return got.Body
}

func TestPreviewingAnImportShowsWhatEachRowWouldDoAndChangesNothing(t *testing.T) {
	t.Parallel()
	b := importSite(t, testsupport.NewFactory())

	got := pageOf(b.Upload("/admin/users/import", "file", "people.csv", []byte(people)))
	users := testsupport.JSONAt(send(t, b, "GET", "/api/v1/users", "", 200).Body, "/total")

	want := testsupport.Page{
		Title: "Import preview · doorcode", Heading: "Import preview", Nav: requestsNav("", "Users"),
		Details: map[string]string{"New users": "1", "Join groups": "1", "Nothing to change": "0", "Errors, skipped": "1"},
		Values:  map[string]string{"csv": people},
		Tables: map[string][][]string{"Rows": {
			importHead,
			{"2", "100234", "Ann Smith", "Joins groups", "Joins guests"},
			{"3", "100555", "bo@example.com", "New user", "Joins guests, lab"},
			{"4", "100777", "cy@example.com", "Error", "groups: No group has the slug “labs”."},
		}},
	}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("preview (-want +got):\n%s", diff)
	}
	if diff := cmp.Diff([]any{2.0, []any{"staff"}}, []any{users, groupSlugs(t, b, "2")}); diff != "" {
		t.Errorf("users, Ann's groups after the preview (-want +got):\n%s", diff)
	}
}

func TestApplyingAnImportCreatesTheGoodUsersInTheirGroupsAndReportsTheBadRow(t *testing.T) {
	t.Parallel()
	b := importSite(t, testsupport.NewFactory())
	preview := pageOf(b.Upload("/admin/users/import", "file", "people.csv", []byte(people)))

	got := b.Post("/admin/users/import/apply", "csv", preview.Values["csv"])
	bo := testsupport.JSON(send(t, b, "GET", "/api/v1/users/3", "", 200).Body).(map[string]any)

	want := testsupport.Page{
		Title: "Import result · doorcode", Heading: "Import result", Nav: requestsNav("", "Users"),
		Details: map[string]string{"Users created": "1", "Users added to groups": "1", "Nothing to change": "0", "Skipped": "1"},
		Tables: map[string][][]string{"Rows": {
			importHead,
			{"2", "100234", "Ann Smith", "Added to groups", "Joined guests"},
			{"3", "100555", "Bo Berg", "Created", "Joined guests, lab"},
			{"4", "100777", "cy@example.com", "Skipped", "groups: No group has the slug “labs”."},
		}},
	}
	if diff := cmp.Diff([]any{200, want}, []any{got.Status, pageOf(got)}); diff != "" {
		t.Errorf("result (-want +got):\n%s", diff)
	}
	wantBo := []any{"bo@example.com", "100555", "Bo Berg", false, []any{"guests", "lab"}}
	if diff := cmp.Diff(wantBo, []any{bo["email"], bo["user_id"], bo["display_name"], bo["has_code"], groupSlugs(t, b, "3")}); diff != "" {
		t.Errorf("Bo (-want +got):\n%s", diff)
	}
	if diff := cmp.Diff([]any{[]any{"guests", "staff"}, 3.0}, []any{groupSlugs(t, b, "2"), testsupport.JSONAt(send(t, b, "GET", "/api/v1/users", "", 200).Body, "/total")}); diff != "" {
		t.Errorf("Ann's groups, users (-want +got):\n%s", diff)
	}
}

func TestApplyingTheSameImportAgainChangesNothing(t *testing.T) {
	t.Parallel()
	b := importSite(t, testsupport.NewFactory())
	b.Upload("/api/v1/users/import", "file", "people.csv", []byte(people))

	again := b.Upload("/api/v1/users/import", "file", "people.csv", []byte(people))

	want := testsupport.JSON(`{"create":0,"update":0,"unchanged":2,"skip":1}`)
	if diff := cmp.Diff([]any{200, want}, []any{again.Status, testsupport.JSONAt(again.Body, "/totals")}); diff != "" {
		t.Errorf("again (-want +got):\n%s", diff)
	}
}

func TestTheImportAPIReportsEveryRowAsJSON(t *testing.T) {
	t.Parallel()
	b := importSite(t, testsupport.NewFactory())

	preview := testsupport.JSON(previewOf(t, b, people))
	applied := b.Upload("/api/v1/users/import", "file", "people.csv", []byte(people))

	rows := func(applied bool, bo string) string {
		done := map[bool]string{true: "true", false: "false"}[applied]
		return `[{"line":2,"user_id":"100234","groups":["staff","guests"],"joins":["guests"],"outcome":"update","user":{"id":2,"name":"Ann Smith"},"applied":` + done + `},` +
			`{"line":3,"user_id":"100555","email":"bo@example.com","groups":["guests"],"joins":["guests","lab"],"outcome":"create",` + bo + `"applied":` + done + `},` +
			`{"line":4,"user_id":"100777","email":"cy@example.com","groups":["labs"],"joins":[],"outcome":"skip",` +
			`"problems":[{"column":"groups","message":"No group has the slug “labs”."}],"applied":false}]`
	}
	totals := `"totals":{"create":1,"update":1,"unchanged":0,"skip":1}`
	want := []any{
		testsupport.JSON(`{"applied":false,"rows":` + rows(false, "") + `,` + totals + `}`),
		200, testsupport.JSON(`{"applied":true,"rows":` + rows(true, `"user":{"id":3,"name":"Bo Berg"},`) + `,` + totals + `}`),
	}
	if diff := cmp.Diff(want, []any{preview, applied.Status, testsupport.JSON(applied.Body)}); diff != "" {
		t.Errorf("preview, applied (-want +got):\n%s", diff)
	}
}

func TestAnImportNamesWhatIsWrongWithEachRow(t *testing.T) {
	t.Parallel()
	f := testsupport.NewFactory()
	b := importSite(t, f)
	createUser(t, b, `{"email":"dan@example.com","display_name":"Dan","user_id":"100900"}`)
	send(t, b, "DELETE", "/api/v1/users/3", "", 204)
	long := ""
	for range 101 {
		long += "x"
	}
	cases := map[string]struct {
		row  string
		want string
	}{
		"no user id":                {`,staff,new@example.com,,,`, `[{"column":"user_id","message":"Enter the user's id."}]`},
		"a malformed user id":       {`10 02,staff,new@example.com,,,`, `[{"column":"user_id","message":"Use up to 64 letters, digits, dots, dashes, or underscores."}]`},
		"an unknown slug":           {`100234,staff;nope;gone,,,,`, `[{"column":"groups","message":"No group has the slug “nope”."},{"column":"groups","message":"No group has the slug “gone”."}]`},
		"a deleted user":            {`100900,staff,,,,`, `[{"column":"user_id","message":"This user is deleted. Restore them before importing them."}]`},
		"a new user without email":  {`100555,staff,,Bo,,`, `[{"column":"email","message":"Nobody has this user id yet, and a new user needs an email address."}]`},
		"a malformed email":         {`100555,staff,bo-at-example,,,`, `[{"column":"email","message":"Enter an email address like name@example.com."}]`},
		"an email another user has": {`100555,staff,ANN@example.com,,,`, `[{"column":"email","message":"Another user has this email address."}]`},
		"a malformed expiry":        {`100555,staff,bo@example.com,,,30/06/2030`, `[{"column":"expires_at","message":"Enter a date like 2030-06-30, or leave it empty for an account that never expires."}]`},
		"a long name":               {`100555,staff,bo@example.com,` + long + `,,`, `[{"column":"first_name","message":"Use at most 100 characters."},{"column":"display_name","message":"Use at most 100 characters."}]`},
		"a short row":               {`100555,staff,bo@example.com`, `[{"column":"first_name","message":"The row ends before the first_name column."}]`},
		"a long row":                {`100555,staff,bo@example.com,,,,extra`, `[{"message":"The row has more values than the header has columns."}]`},
		"a bad expiry and no email": {`100555,staff,,,,someday`, `[{"column":"expires_at","message":"Enter a date like 2030-06-30, or leave it empty for an account that never expires."},{"column":"email","message":"Nobody has this user id yet, and a new user needs an email address."}]`},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			got := previewOf(t, b, "user_id,groups,email,first_name,last_name,expires_at\n"+c.row+"\n")

			if diff := cmp.Diff(testsupport.JSON(c.want), testsupport.JSONAt(got, "/rows/0/problems")); diff != "" {
				t.Errorf("problems (-want +got):\n%s", diff)
			}
		})
	}
}

func TestAnImportNamesRowsRepeatingAUserIDOrANewUsersEmail(t *testing.T) {
	t.Parallel()
	b := importSite(t, testsupport.NewFactory())

	got := previewOf(t, b, "user_id,groups,email\n100555,staff,bo@example.com\n100555,guests,bo@example.com\n100556,,BO@example.com\n")

	want := []any{
		nil,
		testsupport.JSON(`[{"column":"user_id","message":"This user id is also on line 2; list each user once."}]`),
		testsupport.JSON(`[{"column":"email","message":"This email is also on line 2 for another new user."}]`),
	}
	if diff := cmp.Diff(want, []any{testsupport.JSONAt(got, "/rows/0/problems"), testsupport.JSONAt(got, "/rows/1/problems"), testsupport.JSONAt(got, "/rows/2/problems")}); diff != "" {
		t.Errorf("problems of each row (-want +got):\n%s", diff)
	}
}

func TestAnImportAddsPeopleOnlyToGroupsWhoseRolesTheImporterHolds(t *testing.T) {
	t.Parallel()
	f := testsupport.NewFactory()
	b := signedInAdmin(t, f)
	createGroup(t, b, `{"slug":"helpers","name":"Helpers","roles":["manage-users"]}`)
	createUser(t, b, ann)
	join(t, b, "2", "1")
	f.StoreStub.LeaveGroup(adminEmail, "administrators")

	got := previewOf(t, b, "user_id,groups\n100234,administrators;helpers;nope\n")

	want := []any{
		testsupport.JSON(`[{"column":"groups","message":"The group “administrators” carries roles you do not hold, so you cannot add people to it."},` +
			`{"column":"groups","message":"No group has the slug “nope”."}]`),
		"skip",
	}
	if diff := cmp.Diff(want, []any{testsupport.JSONAt(got, "/rows/0/problems"), testsupport.JSONAt(got, "/rows/0/outcome")}); diff != "" {
		t.Errorf("problems, outcome (-want +got):\n%s", diff)
	}
}

func TestTheImportPageOffersAFileAndDescribesIt(t *testing.T) {
	t.Parallel()
	b := importSite(t, testsupport.NewFactory())

	got := pageOf(b.Get("/admin/users/import"))

	want := testsupport.Page{
		Title: "Import users · doorcode", Heading: "Import users", Nav: requestsNav("", "Users"),
		Values: map[string]string{"file": ""},
		Tables: map[string][][]string{"Columns": {
			{"Column", "Holds"},
			{"user_id", "Required. The person's user id, such as an employee or student number."},
			{"groups", "Required. The slugs of the groups they join, separated by semicolons; may be empty."},
			{"email", "Needed to create a user; ignored for someone who exists."},
			{"first_name, last_name", "Optional names for a new user, who is otherwise called by their email."},
			{"expires_at", "Optional date, like 2030-06-30, when a new user's account ends; empty for never."},
		}},
	}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("page (-want +got):\n%s", diff)
	}
}

func TestAnImportFileThatCannotBeReadIsRefused(t *testing.T) {
	t.Parallel()
	hint := " The first line names the columns: user_id and groups, and optionally email, first_name, last_name, and expires_at."
	rows := "user_id,groups\n"
	for range 5001 {
		rows += "100234,staff\n"
	}
	cases := map[string]struct {
		file string
		want string
	}{
		"an empty file":      {"", "The file is empty." + hint},
		"only blank lines":   {"\n\n", "The file is empty." + hint},
		"only a header":      {"user_id,groups\n", "The file has no rows below its header, so there is nothing to import."},
		"an unknown column":  {"user_id,groups,phone\n100234,staff,1\n", "The header names a column “phone” that imports do not have." + hint},
		"a repeated column":  {"user_id,groups,User_ID\n100234,staff,1\n", "The header names the column user_id twice."},
		"no user id column":  {"email,groups\nann@example.com,staff\n", "The header has no user_id column." + hint},
		"no groups column":   {"user_id\n100234\n", "The header has no groups column." + hint},
		"a bad header quote": {"user_id,\"groups\n", "The file is not valid CSV: parse error on line 1, column 17: extraneous or missing \" in quoted-field."},
		"a bad row quote":    {"user_id,groups\n100234,sta\"ff\n", "The file is not valid CSV: parse error on line 2, column 11: bare \" in non-quoted-field."},
		"too many rows":      {rows, "The file has more than 5000 rows. Split it and import each part."},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			b := importSite(t, testsupport.NewFactory())

			api := b.Upload("/api/v1/users/import/preview", "file", "people.csv", []byte(c.file))
			applied := b.Upload("/api/v1/users/import", "file", "people.csv", []byte(c.file))
			page := b.Upload("/admin/users/import", "file", "people.csv", []byte(c.file))
			resent := b.Post("/admin/users/import/apply", "csv", c.file)

			want := []any{400, c.want, 400, 422, map[string]string{"file": c.want}, 422, map[string]string{"file": c.want}}
			got := []any{api.Status, testsupport.JSONString(api.Body, "/error"), applied.Status, page.Status, pageOf(page).FieldErrors, resent.Status, pageOf(resent).FieldErrors}
			if diff := cmp.Diff(want, got); diff != "" {
				t.Errorf("API preview, API import, page preview, page apply (-want +got):\n%s", diff)
			}
		})
	}
}

func TestAnImportRefusesAFileTooLargeOrABodyWithoutOne(t *testing.T) {
	t.Parallel()
	b := importSite(t, testsupport.NewFactory())
	large := testsupport.Padded([]byte("user_id,groups\n"), 1048577)

	tooBig := b.Upload("/api/v1/users/import/preview", "file", "people.csv", large)
	pageTooBig := b.Upload("/admin/users/import", "file", "people.csv", large)
	largest := b.Upload("/api/v1/users/import/preview", "file", "people.csv", testsupport.Padded([]byte("user_id,groups\n"), 1048576))
	elsewhere := b.Upload("/api/v1/users/import/preview", "csv", "people.csv", []byte(people))
	notForm := b.Send("POST", "/api/v1/users/import", `{"file":"user_id,groups"}`)
	pageNotForm := b.Post("/admin/users/import", "file", people)

	sent := "Send the file as a multipart form with the file in the field file."
	want := []any{
		413, "the file is larger than 1024 KiB", 413, map[string]string{"file": "The file is larger than 1024 KiB."}, 200,
		sent, sent, 422, map[string]string{"file": sent},
	}
	got := []any{
		tooBig.Status, testsupport.JSONString(tooBig.Body, "/error"), pageTooBig.Status, pageOf(pageTooBig).FieldErrors, largest.Status,
		testsupport.JSONString(elsewhere.Body, "/error"), testsupport.JSONString(notForm.Body, "/error"), pageNotForm.Status, pageOf(pageNotForm).FieldErrors,
	}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("too big, from the page, the largest, another field, not a form, not a form from the page (-want +got):\n%s", diff)
	}
}

func TestAnImportReadsASpreadsheetsFileAndCreatesUsersWithTheirExpiry(t *testing.T) {
	t.Parallel()
	f := testsupport.NewFactory()
	f.EnvStub["DOORCODE_SITE_TIMEZONE"] = "America/New_York"
	b := importSite(t, f)
	file := "\uFEFF Expires_At ,EMAIL,User_ID,Groups\r\n2030-06-30,cy@example.com,100777,\r\n,dee@example.com,100778, ;staff; staff\r\n"

	got := b.Upload("/api/v1/users/import", "file", "people.csv", []byte(file))
	cy := testsupport.JSON(send(t, b, "GET", "/api/v1/users/3", "", 200).Body).(map[string]any)
	dee := testsupport.JSON(send(t, b, "GET", "/api/v1/users/4", "", 200).Body).(map[string]any)

	want := []any{200, "2030-06-30T04:00:00Z", "cy@example.com", []any(nil), nil, "dee@example.com", []any{"staff"}}
	if diff := cmp.Diff(want, []any{got.Status, cy["expires_at"], cy["display_name"], groupSlugs(t, b, "3"), dee["expires_at"], dee["display_name"], groupSlugs(t, b, "4")}); diff != "" {
		t.Errorf("status, Cy's expiry, name, groups, Dee's expiry, name, groups (-want +got):\n%s", diff)
	}
}

func TestAnImportThatWouldChangeNothingOffersNoApplying(t *testing.T) {
	t.Parallel()
	b := importSite(t, testsupport.NewFactory())

	got := pageOf(b.Upload("/admin/users/import", "file", "people.csv", []byte("user_id,groups\n100234,staff\n100999,staff\n")))

	want := []any{
		[]string{"Applying this file would change nothing. Fix the rows with errors and preview it again."},
		[][]string{importHead,
			{"2", "100234", "Ann Smith", "Nothing to change", "Already in every group listed"},
			{"3", "100999", "", "Error", "email: Nobody has this user id yet, and a new user needs an email address."},
		},
		map[string]string(nil),
	}
	if diff := cmp.Diff(want, []any{got.Notes, got.Tables["Rows"], got.Values}); diff != "" {
		t.Errorf("notes, rows, form (-want +got):\n%s", diff)
	}
}

func TestANewUserImportedWithoutGroupsJoinsNone(t *testing.T) {
	t.Parallel()
	b := importSite(t, testsupport.NewFactory())

	preview := pageOf(b.Upload("/admin/users/import", "file", "people.csv", []byte("user_id,groups,email\n100999,,eve@example.com\n")))
	applied := pageOf(b.Post("/admin/users/import/apply", "csv", preview.Values["csv"]))

	want := []any{
		[]string{"2", "100999", "eve@example.com", "New user", "Joins no groups"},
		[]string{"2", "100999", "eve@example.com", "Created", "Joined no groups"},
	}
	if diff := cmp.Diff(want, []any{preview.Tables["Rows"][1], applied.Tables["Rows"][1]}); diff != "" {
		t.Errorf("preview, result (-want +got):\n%s", diff)
	}
}

func TestAnImportTheStoreStopsReportsWhatWasApplied(t *testing.T) {
	t.Parallel()
	f := testsupport.NewFactory()
	b := importSite(t, f)
	f.StoreStub.FailUserWrites()

	got := b.Post("/admin/users/import/apply", "csv", people)

	want := []any{
		500,
		[]string{"The server failed partway, so only the rows marked done were applied. Apply the same file again to finish: rows done already show as nothing to change. Give an administrator the request id " + lastLog(f).Fields["request_id"] + "."},
		[][]string{importHead,
			{"2", "100234", "Ann Smith", "Added to groups", "Joined guests"},
			{"3", "100555", "bo@example.com", "Not applied", "Joins guests, lab"},
			{"4", "100777", "cy@example.com", "Skipped", "groups: No group has the slug “labs”."},
		},
		[]any{"guests", "staff"},
	}
	page := pageOf(got)
	if diff := cmp.Diff(want, []any{got.Status, page.Messages, page.Tables["Rows"], groupSlugs(t, b, "2")}); diff != "" {
		t.Errorf("status, alert, rows, Ann's groups (-want +got):\n%s", diff)
	}
}

func TestAnImportTheStoreStopsBeforeApplyingShowsTheErrorPage(t *testing.T) {
	t.Parallel()
	f := testsupport.NewFactory()
	b := importSite(t, f)
	f.StoreStub.FailGroups()

	got := b.Post("/admin/users/import/apply", "csv", people)

	if diff := cmp.Diff([]any{500, "Something went wrong"}, []any{got.Status, pageOf(got).Title}); diff != "" {
		t.Errorf("status, title (-want +got):\n%s", diff)
	}
}

func TestImportsShowARequestIDWhenTheStoreFails(t *testing.T) {
	t.Parallel()
	upload := func(target string) func(b *testsupport.Browser) testsupport.Response {
		return func(b *testsupport.Browser) testsupport.Response {
			return b.Upload(target, "file", "people.csv", []byte(people))
		}
	}
	apply := func(b *testsupport.Browser) testsupport.Response {
		return b.Post("/admin/users/import/apply", "csv", people)
	}
	cases := map[string]struct {
		fail    func(s *testsupport.Store)
		request func(b *testsupport.Browser) testsupport.Response
	}{
		"the page without settings":           {fail: (*testsupport.Store).FailSettings, request: func(b *testsupport.Browser) testsupport.Response { return b.Get("/admin/users/import") }},
		"a preview without settings":          {fail: (*testsupport.Store).FailSettings, request: upload("/admin/users/import")},
		"a preview without groups":            {fail: (*testsupport.Store).FailGroups, request: upload("/admin/users/import")},
		"a preview without users":             {fail: (*testsupport.Store).FailUsers, request: upload("/admin/users/import")},
		"a preview without email lookups":     {fail: (*testsupport.Store).FailEmailLookups, request: upload("/admin/users/import")},
		"applying without settings":           {fail: (*testsupport.Store).FailSettings, request: apply},
		"applying without groups":             {fail: (*testsupport.Store).FailGroups, request: apply},
		"the API preview without settings":    {fail: (*testsupport.Store).FailSettings, request: upload("/api/v1/users/import/preview")},
		"the API preview without groups":      {fail: (*testsupport.Store).FailGroups, request: upload("/api/v1/users/import/preview")},
		"the API import without group writes": {fail: (*testsupport.Store).FailGroupWrites, request: upload("/api/v1/users/import")},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			f := testsupport.NewFactory()
			b := importSite(t, f)
			c.fail(f.StoreStub)

			got := c.request(b)

			if diff := cmp.Diff([]any{500, "request failed"}, []any{got.Status, lastLog(f).Message}); diff != "" {
				t.Errorf("status, logged (-want +got):\n%s", diff)
			}
		})
	}
}

func TestImportingNeedsTheManageUsersRole(t *testing.T) {
	t.Parallel()
	_, b := adminHolding(t, testsupport.NewFactory(), `["manage-groups"]`)

	got := []int{
		b.Get("/admin/users/import").Status, b.Upload("/admin/users/import", "file", "people.csv", []byte(people)).Status,
		b.Post("/admin/users/import/apply", "csv", people).Status,
		b.Upload("/api/v1/users/import/preview", "file", "people.csv", []byte(people)).Status,
		b.Upload("/api/v1/users/import", "file", "people.csv", []byte(people)).Status,
	}

	if diff := cmp.Diff([]int{403, 403, 403, 403, 403}, got); diff != "" {
		t.Errorf("page, preview, apply, API preview, API import (-want +got):\n%s", diff)
	}
}
