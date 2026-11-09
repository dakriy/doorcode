package entrypoint_test

import (
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/dakriy/doorcode/internal/testsupport"
)

// mainNav is the administration's navigation for the first administrator
// on the page whose link is labelled current, none when it is empty, while
// waiting access requests wait for an answer, such as " 1 waiting".
func mainNav(current, waiting string) []string {
	nav := []string{}
	for _, label := range []string{
		"Dashboard", "People: Users", "People: Requests" + waiting, "People: Groups", "Doors: Doors", "Doors: Mesh", "Doors: Zones", "Doors: Firmware",
		"Access: Schedules", "Activity: Events", "Portal", "API tokens", "Site: Settings",
	} {
		if label == current || label == current+waiting {
			label += " (current)"
		}
		nav = append(nav, label)
	}
	return nav
}

// requestsHead heads the administration's list of access requests.
var requestsHead = []string{"Request", "From", "Sent", "Status"}

func TestAnAdministratorRepliesToAndResolvesARequestAndTheRequesterSeesIt(t *testing.T) {
	t.Parallel()
	f := testsupport.NewFactory()
	admin, ann := portalSite(t, f)
	admin.Get("/")
	send(t, ann, "POST", "/api/v1/me/requests", labAccess, 201)
	before := pageOf(admin.Get("/admin/users")).Nav
	f.ClockStub.Set("2026-01-05T11:30:00Z")

	replied := admin.Post("/admin/requests/1/reply", "reply", " Granted from Monday. ", "resolve", "yes")
	shown := pageOf(admin.Get(replied.Location))
	seen := pageOf(ann.Get("/me/requests/1"))

	if diff := cmp.Diff(mainNav("People: Users", " 1 waiting"), before); diff != "" {
		t.Errorf("navigation before (-want +got):\n%s", diff)
	}
	if diff := cmp.Diff([]any{303, "/admin/requests/1", mainNav("People: Requests", ""),
		[]string{"The reply has been saved and the request resolved. The user sees both on their request."}},
		[]any{replied.Status, replied.Location, shown.Nav, shown.Messages}); diff != "" {
		t.Errorf("reply, page after (-want +got):\n%s", diff)
	}
	want := map[string]string{"Reply": "Granted from Monday.", "Resolved": "5 Jan 2026 11:30"}
	if diff := cmp.Diff(want, seen.Details); diff != "" {
		t.Errorf("what the requester sees (-want +got):\n%s", diff)
	}
}

func TestTheRequestPageShowsTheRequestItsRequesterAndTheReplyForm(t *testing.T) {
	t.Parallel()
	f := testsupport.NewFactory()
	admin, ann := portalSite(t, f)
	admin.Get("/")
	send(t, ann, "POST", "/api/v1/me/requests", labAccess, 201)
	f.ClockStub.Set("2026-01-05T10:00:00Z")
	send(t, ann, "PUT", "/api/v1/me/requests/1", `{"title":"Lab access","body":"From Monday, please."}`, 200)

	got := pageOf(admin.Get("/admin/requests/1"))

	want := testsupport.Page{
		Title: "Lab access · doorcode", Heading: "Lab access", Nav: mainNav("People: Requests", " 1 waiting"),
		Details:     map[string]string{"From": "Ann Smith", "Sent": "5 Jan 2026 09:00", "Changed": "5 Jan 2026 10:00"},
		Values:      map[string]string{"reply": ""},
		LinkTargets: map[string]string{"Requester": "/admin/users/2"},
	}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("page (-want +got):\n%s", diff)
	}
}

func TestARepliedRequestStaysOpenUntilResolvedAndCanBeReopened(t *testing.T) {
	t.Parallel()
	f := testsupport.NewFactory()
	admin, ann := portalSite(t, f)
	admin.Get("/")
	send(t, ann, "POST", "/api/v1/me/requests", labAccess, 201)

	replied := admin.Post("/admin/requests/1/reply", "reply", "Which lab?")
	afterReply := pageOf(admin.Get("/admin/requests/1"))
	resolved := admin.Post("/admin/requests/1/resolve")
	f.ClockStub.Set("2026-01-05T12:00:00Z")
	again := admin.Post("/admin/requests/1/resolve")
	resolvedAt := testsupport.JSONString(send(t, admin, "GET", "/api/v1/requests/1", "", 200).Body, "/resolved_at")
	afterResolve := pageOf(admin.Get("/admin/requests/1"))
	reopened := admin.Post("/admin/requests/1/reopen")
	afterReopen := pageOf(admin.Get("/admin/requests/1"))
	changed := ann.PutJSON("/api/v1/me/requests/1", `{"title":"Electronics lab access"}`)

	got := []any{
		replied.Status, afterReply.Messages, afterReply.Values, resolved.Status, again.Status, afterResolve.Messages,
		reopened.Status, afterReopen.Messages, afterReopen.Nav[2], changed.Status,
	}
	want := []any{
		303, []string{"The reply has been saved. The user sees it on their request."}, map[string]string{"reply": "Which lab?"},
		303, 303, []string{"The request has been resolved. Its user can no longer change or withdraw it."},
		303, []string{"The request has been reopened. Its user may change or withdraw it again."}, "People: Requests 1 waiting (current)", 200,
	}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("reply, resolve twice, reopen, the user's change (-want +got):\n%s", diff)
	}
	if diff := cmp.Diff("2026-01-05T09:00:00Z", resolvedAt); diff != "" {
		t.Errorf("resolved at, after resolving again (-want +got):\n%s", diff)
	}
}

func TestTheRequestsAPIListsReadsRepliesResolvesAndReopens(t *testing.T) {
	t.Parallel()
	f := testsupport.NewFactory()
	admin, ann := portalSite(t, f)
	send(t, ann, "POST", "/api/v1/me/requests", labAccess, 201)
	f.ClockStub.Set("2026-01-05T10:00:00Z")
	send(t, admin, "POST", "/api/v1/me/requests", `{"title":"Spare badge"}`, 201)

	listed := admin.Get("/api/v1/requests")
	replied := admin.PutJSON("/api/v1/requests/1/reply", `{"reply":" Granted. "}`)
	resolved := admin.Send("POST", "/api/v1/requests/1/resolve", "")
	resolvedList := admin.Get("/api/v1/requests?state=resolved")
	reopened := admin.Send("POST", "/api/v1/requests/1/reopen", "")
	read := admin.Get("/api/v1/requests/1")

	open := `{"id":1,"user":{"id":2,"name":"Ann Smith"},"title":"Lab access","body":"I start in the lab on Monday.","resolved":false,` +
		`"created_at":"2026-01-05T09:00:00Z","updated_at":"2026-01-05T09:00:00Z"}`
	badge := `{"id":2,"user":{"id":1,"name":"Administrator"},"title":"Spare badge","body":"","resolved":false,` +
		`"created_at":"2026-01-05T10:00:00Z","updated_at":"2026-01-05T10:00:00Z"}`
	answered := `{"id":1,"user":{"id":2,"name":"Ann Smith"},"title":"Lab access","body":"I start in the lab on Monday.","reply":"Granted.",` +
		`"resolved":false,"created_at":"2026-01-05T09:00:00Z","updated_at":"2026-01-05T09:00:00Z"}`
	closed := `{"id":1,"user":{"id":2,"name":"Ann Smith"},"title":"Lab access","body":"I start in the lab on Monday.","reply":"Granted.",` +
		`"resolved":true,"resolved_at":"2026-01-05T10:00:00Z","created_at":"2026-01-05T09:00:00Z","updated_at":"2026-01-05T09:00:00Z"}`
	want := []any{
		testsupport.JSON(`{"requests":[` + badge + `,` + open + `],"total":2,"page":1,"per_page":25}`),
		testsupport.JSON(answered), testsupport.JSON(closed),
		testsupport.JSON(`{"requests":[` + closed + `],"total":1,"page":1,"per_page":25}`),
		testsupport.JSON(answered), testsupport.JSON(answered),
	}
	got := []any{
		testsupport.JSON(listed.Body), testsupport.JSON(replied.Body), testsupport.JSON(resolved.Body), testsupport.JSON(resolvedList.Body),
		testsupport.JSON(reopened.Body), testsupport.JSON(read.Body),
	}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("list, reply, resolve, resolved list, reopen, read (-want +got):\n%s", diff)
	}
}

func TestTheRequestsAPIFiltersAndPages(t *testing.T) {
	t.Parallel()
	admin, ann := portalSite(t, testsupport.NewFactory())
	send(t, ann, "POST", "/api/v1/me/requests", labAccess, 201)
	send(t, ann, "POST", "/api/v1/me/requests", `{"title":"Weekend hours"}`, 201)
	send(t, admin, "POST", "/api/v1/me/requests", `{"title":"Spare badge","body":"For the lab visitors."}`, 201)
	send(t, admin, "POST", "/api/v1/requests/2/resolve", "", 200)
	ids := func(query string) []any {
		var out []any
		for _, r := range testsupport.JSONAt(send(t, admin, "GET", "/api/v1/requests"+query, "", 200).Body, "/requests").([]any) {
			out = append(out, r.(map[string]any)["id"])
		}
		return out
	}

	got := []any{ids(""), ids("?state=all"), ids("?state=resolved"), ids("?q=LAB"), ids("?q=ann&state=all"), ids("?user=1"), ids("?state=all&per_page=1&page=2")}

	want := []any{[]any{3.0, 1.0}, []any{3.0, 2.0, 1.0}, []any{2.0}, []any{3.0, 1.0}, []any{2.0, 1.0}, []any{3.0}, []any{2.0}}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("open, all, resolved, search, search across states, one user's, a page (-want +got):\n%s", diff)
	}
}

func TestTheRequestsAPIRefusesWhatItCannotRead(t *testing.T) {
	t.Parallel()
	admin, _ := portalSite(t, testsupport.NewFactory())
	refusal := "page must be a whole number from 1, per_page one from 1 to 200, state open, resolved, or all, and user a user's id"

	got := []any{
		testsupport.JSONString(admin.Get("/api/v1/requests?state=closed").Body, "/error"),
		admin.Get("/api/v1/requests?user=ann").Status, admin.Get("/api/v1/requests?page=0").Status,
		admin.Get("/api/v1/requests?per_page=201").Status, admin.Get("/api/v1/requests?per_page=200").Status,
		testsupport.JSONString(admin.PutJSON("/api/v1/requests/1/reply", `{"reply":1}`).Body, "/error"),
		testsupport.JSONString(admin.Get("/api/v1/requests/9").Body, "/error"),
		admin.PutJSON("/api/v1/requests/9/reply", `{"reply":"No."}`).Status,
		admin.Send("POST", "/api/v1/requests/9/resolve", "").Status, admin.Send("POST", "/api/v1/requests/9/reopen", "").Status,
	}

	want := []any{refusal, 400, 400, 400, 200, `body must be a JSON object with a string "reply"`, "no access request has this id", 404, 404, 404}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("state, user, page, per page, the most a page, reply body, missing request (-want +got):\n%s", diff)
	}
}

func TestAReplyIsChecked(t *testing.T) {
	t.Parallel()
	admin, ann := portalSite(t, testsupport.NewFactory())
	admin.Get("/")
	send(t, ann, "POST", "/api/v1/me/requests", labAccess, 201)
	long := ""
	for range 4001 {
		long += "x"
	}

	blank := admin.PutJSON("/api/v1/requests/1/reply", `{"reply":"  "}`)
	tooLong := admin.PutJSON("/api/v1/requests/1/reply", `{"reply":"`+long+`"}`)
	longest := admin.PutJSON("/api/v1/requests/1/reply", `{"reply":"`+long[1:]+`"}`)
	page := admin.Post("/admin/requests/1/reply", "reply", " ", "resolve", "yes")
	still := testsupport.JSONAt(send(t, admin, "GET", "/api/v1/requests/1", "", 200).Body, "/resolved")

	got := []any{blank.Status, testsupport.JSONString(blank.Body, "/error"), testsupport.JSONString(tooLong.Body, "/error"), longest.Status,
		page.Status, pageOf(page).FieldErrors, pageOf(page).Values, still}
	want := []any{400, "Write a reply.", "Use at most 4000 characters.", 200, 422, map[string]string{"reply": "Write a reply."}, map[string]string{"reply": " "}, false}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("blank, too long, longest, from the page, still open (-want +got):\n%s", diff)
	}
}

func TestTheRequestsPageListsWhatWaitsAndFilters(t *testing.T) {
	t.Parallel()
	f := testsupport.NewFactory()
	admin, ann := portalSite(t, f)
	admin.Get("/")
	send(t, ann, "POST", "/api/v1/me/requests", labAccess, 201)
	f.ClockStub.Set("2026-01-05T10:00:00Z")
	send(t, ann, "POST", "/api/v1/me/requests", `{"title":"Weekend hours"}`, 201)
	send(t, admin, "POST", "/api/v1/me/requests", `{"title":"Spare badge"}`, 201)
	send(t, admin, "POST", "/api/v1/requests/2/resolve", "", 200)

	waiting := pageOf(admin.Get("/admin/requests"))
	everyone := pageOf(admin.Get("/admin/requests?state=all&user=2")).Tables["Access requests"]
	searched := pageOf(admin.Get("/admin/requests?state=all&q=weekend")).Tables["Access requests"]
	nothing := pageOf(admin.Get("/admin/requests?q=nobody")).Tables["Access requests"]
	byUser := testsupport.ParsePage(admin.Get("/admin/requests?user=2").Body)

	lab := []string{"Lab access I start in the lab on Monday.", "Ann Smith", "5 Jan 2026 09:00", "Waiting for an answer"}
	badge := []string{"Spare badge", "Administrator", "5 Jan 2026 10:00", "Waiting for an answer"}
	weekend := []string{"Weekend hours", "Ann Smith", "5 Jan 2026 10:00", "Resolved 5 Jan 2026 10:00"}
	wantWaiting := testsupport.Page{
		Title: "Access requests · doorcode", Heading: "Access requests", Nav: mainNav("People: Requests", " 2 waiting"),
		Values: map[string]string{"q": ""},
		Tables: map[string][][]string{"Access requests": {requestsHead, badge, lab, {"Showing 1 to 2 of 2"}}},
	}
	if diff := cmp.Diff(wantWaiting, waiting); diff != "" {
		t.Errorf("waiting (-want +got):\n%s", diff)
	}
	want := []any{
		[][]string{requestsHead, weekend, lab, {"Showing 1 to 2 of 2"}},
		[][]string{requestsHead, weekend, {"Showing 1 to 1 of 1"}},
		[][]string{requestsHead, {"No requests match these filters."}},
	}
	if diff := cmp.Diff(want, []any{everyone, searched, nothing}); diff != "" {
		t.Errorf("one user's, searched, nothing (-want +got):\n%s", diff)
	}
	if diff := cmp.Diff([]any{[]string{"User: Ann Smith ×"}, "/admin/requests?state=open", "2"},
		[]any{byUser.Messages, byUser.LinkTargets["Stop filtering by user"], byUser.Values["user"]}); diff != "" {
		t.Errorf("filtered by user (-want +got):\n%s", diff)
	}
}

func TestTheRequestsPageSaysWhyItIsEmpty(t *testing.T) {
	t.Parallel()
	f := testsupport.NewFactory()
	admin, ann := portalSite(t, f)
	admin.Get("/")
	none := pageOf(admin.Get("/admin/requests?state=all")).Tables["Access requests"]
	send(t, ann, "POST", "/api/v1/me/requests", labAccess, 201)
	send(t, admin, "POST", "/api/v1/requests/1/resolve", "", 200)

	waiting := pageOf(admin.Get("/admin/requests")).Tables["Access requests"]
	pastPage := testsupport.ParsePage(admin.Get("/admin/requests?state=resolved&user=2&page=2").Body)
	past := pastPage.Tables["Access requests"]

	want := []any{
		[][]string{requestsHead, {"No requests yet. People ask for access from the portal, and their requests appear here."}},
		[][]string{requestsHead, {"Nothing is waiting for an answer. Requests people make in the portal appear here."}},
		[][]string{requestsHead, {"This page is past the last one."}},
	}
	if diff := cmp.Diff(want, []any{none, waiting, past}); diff != "" {
		t.Errorf("none, none waiting, past the last page (-want +got):\n%s", diff)
	}
	if diff := cmp.Diff("/admin/requests?page=1&state=resolved&user=2", pastPage.NavTargets[len(pastPage.NavTargets)-1]); diff != "" {
		t.Errorf("previous page (-want +got):\n%s", diff)
	}
}

func TestTheRequestsPageIsPagedKeepingItsFilters(t *testing.T) {
	t.Parallel()
	admin, ann := portalSite(t, testsupport.NewFactory())
	admin.Get("/")
	for range 26 {
		send(t, ann, "POST", "/api/v1/me/requests", labAccess, 201)
	}

	first := testsupport.ParsePage(admin.Get("/admin/requests?q=lab").Body)
	second := testsupport.ParsePage(admin.Get(first.NavTargets[len(first.NavTargets)-1]).Body)

	got := []any{first.Tables["Access requests"][26], second.Tables["Access requests"][1:], second.NavTargets[len(second.NavTargets)-1]}
	want := []any{
		[]string{"Showing 1 to 25 of 26"},
		[][]string{{"Lab access I start in the lab on Monday.", "Ann Smith", "5 Jan 2026 09:00", "Waiting for an answer"}, {"Showing 26 to 26 of 26"}},
		"/admin/requests?page=1&q=lab&state=open",
	}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("first page's total, second page, its previous link (-want +got):\n%s", diff)
	}
}

func TestTheRequestsPageShowsWhatItCouldReadOfItsFilters(t *testing.T) {
	t.Parallel()
	admin, ann := portalSite(t, testsupport.NewFactory())
	admin.Get("/")
	send(t, ann, "POST", "/api/v1/me/requests", labAccess, 201)

	badState := pageOf(admin.Get("/admin/requests?state=closed&q=nothing"))
	badUser := pageOf(admin.Get("/admin/requests?user=ann"))
	badPage := pageOf(admin.Get("/admin/requests?page=zero"))
	missingUser := testsupport.ParsePage(admin.Get("/admin/requests?user=9").Body)

	filters := []string{"Some filters could not be read, so the requests waiting for an answer are shown."}
	rows := [][]string{requestsHead, {"Lab access I start in the lab on Monday.", "Ann Smith", "5 Jan 2026 09:00", "Waiting for an answer"}, {"Showing 1 to 1 of 1"}}
	want := []any{filters, rows, filters, rows, []string{"The page number could not be read, so the first page is shown."}, rows, []string{"User: No such user ×"}}
	got := []any{badState.Messages, badState.Tables["Access requests"], badUser.Messages, badUser.Tables["Access requests"], badPage.Messages,
		badPage.Tables["Access requests"], missingUser.Messages}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("bad state, bad user, bad page, missing user (-want +got):\n%s", diff)
	}
}

func TestARequestThatDoesNotExistIsNotFound(t *testing.T) {
	t.Parallel()
	admin, _ := portalSite(t, testsupport.NewFactory())
	admin.Get("/")
	notes := func(r testsupport.Response) []any { return []any{r.Status, pageOf(r).Heading} }

	got := []any{
		notes(admin.Get("/admin/requests/9")), notes(admin.Post("/admin/requests/9/reply", "reply", "No.")),
		notes(admin.Post("/admin/requests/9/resolve")), notes(admin.Post("/admin/requests/9/reopen")),
	}

	found := []any{404, "Not found"}
	if diff := cmp.Diff([]any{found, found, found, found}, got); diff != "" {
		t.Errorf("page, reply, resolve, reopen (-want +got):\n%s", diff)
	}
}

func TestAccessRequestsNeedTheManageUsersRole(t *testing.T) {
	t.Parallel()
	_, b := adminHolding(t, testsupport.NewFactory(), `["manage-groups"]`)
	b.Get("/")

	got := []any{
		b.Get("/admin/requests").Status, b.Get("/admin/requests/1").Status, b.Post("/admin/requests/1/resolve").Status,
		b.Get("/api/v1/requests").Status, b.Send("POST", "/api/v1/requests/1/reopen", "").Status,
		pageOf(b.Get("/admin/groups")).Nav,
	}

	want := []any{403, 403, 403, 403, 403, []string{"Dashboard", "People: Groups (current)", "Portal", "API tokens"}}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("pages, API, navigation (-want +got):\n%s", diff)
	}
}

func TestTheLiveStreamTellsUserManagersWhenRequestsChange(t *testing.T) {
	t.Parallel()
	f := testsupport.NewFactory()
	b := signedInAdmin(t, f)
	createUser(t, b, ann)
	ann := asUser(t, b, "2")
	createGroup(t, b, `{"slug":"staff","name":"Staff","roles":["manage-users"]}`)
	join(t, b, "2", "1")
	f.StoreStub.LeaveGroup(adminEmail, "administrators")
	stream := b.Stream("/admin/live")
	defer stream.Close()
	stream.Next()

	send(t, ann, "POST", "/api/v1/me/requests", labAccess, 201)
	made := stream.Next()
	send(t, b, "POST", "/api/v1/requests/1/resolve", "", 200)
	resolved := stream.Next()

	want := []any{"event: requests\ndata: changed\n\n", "event: requests\ndata: changed\n\n"}
	if diff := cmp.Diff(want, []any{made, resolved}); diff != "" {
		t.Errorf("made, resolved (-want +got):\n%s", diff)
	}
}

func TestEveryAdministrationPageOfAUserManagerLoadsTheLiveScript(t *testing.T) {
	t.Parallel()
	b := signedInAdmin(t, testsupport.NewFactory())
	last := func(target string) string {
		assets := testsupport.ParsePage(b.Get(target).Body).Assets
		return assets[len(assets)-1]
	}

	got := []string{last("/admin/users"), last("/admin/groups"), last("/admin/settings")}

	doors := last("/admin/doors")
	if diff := cmp.Diff([]string{doors, doors, doors}, got); diff != "" {
		t.Errorf("last script of users, groups, settings (-want +got):\n%s", diff)
	}
}

func TestTheRequestsPagesAndAPIShowARequestIDWhenTheStoreFails(t *testing.T) {
	t.Parallel()
	type request struct{ method, target string }
	cases := map[string]struct {
		fail func(s *testsupport.Store)
		request
		fields []string
		body   string
	}{
		"the navigation's count":         {fail: (*testsupport.Store).FailRequests, request: request{"GET", "/admin/users"}},
		"the list without settings":      {fail: (*testsupport.Store).FailSettings, request: request{"GET", "/admin/requests"}},
		"the list without requests":      {fail: (*testsupport.Store).FailRequests, request: request{"GET", "/admin/requests?state=all"}},
		"the list's user":                {fail: (*testsupport.Store).FailUsers, request: request{"GET", "/admin/requests?user=2"}},
		"a request without settings":     {fail: (*testsupport.Store).FailSettings, request: request{"GET", "/admin/requests/1"}},
		"a request without requests":     {fail: (*testsupport.Store).FailRequests, request: request{"GET", "/admin/requests/1"}},
		"a reply without writes":         {fail: (*testsupport.Store).FailRequestWrites, request: request{"POST", "/admin/requests/1/reply"}, fields: []string{"reply", "Yes."}},
		"a resolving without writes":     {fail: (*testsupport.Store).FailRequestWrites, request: request{"POST", "/admin/requests/1/resolve"}},
		"a reopening without writes":     {fail: (*testsupport.Store).FailRequestWrites, request: request{"POST", "/admin/requests/1/reopen"}},
		"the API list":                   {fail: (*testsupport.Store).FailRequests, request: request{"GET", "/api/v1/requests"}},
		"the API read":                   {fail: (*testsupport.Store).FailRequests, request: request{"GET", "/api/v1/requests/1"}},
		"the API reply without writes":   {fail: (*testsupport.Store).FailRequestWrites, request: request{"PUT", "/api/v1/requests/1/reply"}, body: `{"reply":"Yes."}`},
		"the API reply without requests": {fail: (*testsupport.Store).FailRequests, request: request{"PUT", "/api/v1/requests/1/reply"}, body: `{"reply":"Yes."}`},
		"the API resolve without writes": {fail: (*testsupport.Store).FailRequestWrites, request: request{"POST", "/api/v1/requests/1/resolve"}},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			f := testsupport.NewFactory()
			admin, ann := portalSite(t, f)
			send(t, ann, "POST", "/api/v1/me/requests", labAccess, 201)
			c.fail(f.StoreStub)

			var got testsupport.Response
			if c.method == "POST" && c.body == "" {
				got = admin.Post(c.target, c.fields...)
			} else {
				got = admin.Send(c.method, c.target, c.body)
			}

			if diff := cmp.Diff([]any{500, "request failed"}, []any{got.Status, lastLog(f).Message}); diff != "" {
				t.Errorf("status, logged (-want +got):\n%s", diff)
			}
		})
	}
}
