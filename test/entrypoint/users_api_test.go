package entrypoint_test

import (
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/dakriy/doorcode/internal/testsupport"
)

// annJSON is ann as the API shows her right after creation.
const annJSON = `{"id":2,"email":"ann@example.com","user_id":"100234","first_name":"Ann","last_name":"Smith",` +
	`"display_name":"Ann Smith","has_code":false,"expired":false,"created_at":"2026-01-05T09:00:00Z","groups":[]}`

func TestCreatingAUserAnswersWithTheUser(t *testing.T) {
	t.Parallel()
	b := signedInAdmin(t, testsupport.NewFactory())

	got := b.PostJSON("/api/v1/users", ann)

	want := testsupport.Response{Status: 201, ContentType: jsonType, Body: annJSON + "\n"}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("response (-want +got):\n%s", diff)
	}
}

func TestCreatedUserIsTidied(t *testing.T) {
	t.Parallel()
	b := signedInAdmin(t, testsupport.NewFactory())

	got := b.PostJSON("/api/v1/users", `{"email":"  Bo@Example.COM ","first_name":" Bo ","user_id":" 7 ",`+
		`"display_name":"  ","expires_at":"2027-01-01T09:00:00+09:00"}`)

	want := testsupport.JSON(`{"id":2,"email":"bo@example.com","user_id":"7","first_name":"Bo","last_name":"",` +
		`"display_name":"Bo","has_code":false,"expires_at":"2027-01-01T00:00:00Z","expired":false,` +
		`"created_at":"2026-01-05T09:00:00Z","groups":[]}`)
	if diff := cmp.Diff(want, testsupport.JSON(got.Body)); diff != "" {
		t.Errorf("user (-want +got):\n%s", diff)
	}
}

func TestUserRequestIsChecked(t *testing.T) {
	t.Parallel()
	long := testsupport.Repeat("x", 101)
	cases := map[string]struct {
		body string
		want string
	}{
		"not json":              {body: `email=x`, want: `body must be a JSON object with a string \"email\"`},
		"no email":              {body: `{"display_name":"A"}`, want: "Enter an email address."},
		"not an email":          {body: `{"email":"ann","display_name":"A"}`, want: "Enter an email address like name@example.com."},
		"email with a name":     {body: `{"email":"Ann <ann@example.com>","display_name":"A"}`, want: "Enter an email address like name@example.com."},
		"email too long":        {body: `{"email":"` + testsupport.Repeat("a", 243) + `@example.com","display_name":"A"}`, want: "Use at most 254 characters for the email address."},
		"email taken":           {body: `{"email":"ADMIN@example.com","display_name":"A"}`, want: "Another user has this email address."},
		"user id with a space":  {body: `{"email":"a@example.com","display_name":"A","user_id":"1 2"}`, want: "Use up to 64 letters, digits, dots, dashes, or underscores."},
		"user id too long":      {body: `{"email":"a@example.com","display_name":"A","user_id":"` + testsupport.Repeat("1", 65) + `"}`, want: "Use up to 64 letters, digits, dots, dashes, or underscores."},
		"user id taken":         {body: `{"email":"a@example.com","display_name":"A","user_id":"100234"}`, want: "Another user has this user id."},
		"first name too long":   {body: `{"email":"a@example.com","first_name":"` + long + `"}`, want: "Use at most 100 characters."},
		"last name too long":    {body: `{"email":"a@example.com","last_name":"` + long + `"}`, want: "Use at most 100 characters."},
		"display name too long": {body: `{"email":"a@example.com","display_name":"` + long + `"}`, want: "Use at most 100 characters."},
		"no name at all":        {body: `{"email":"a@example.com"}`, want: "Enter a display name, or a first or last name."},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			b := signedInAdmin(t, testsupport.NewFactory())
			createUser(t, b, ann)

			got := b.PostJSON("/api/v1/users", c.body)

			want := apiError(400, c.want)
			if diff := cmp.Diff(want, got); diff != "" {
				t.Errorf("response (-want +got):\n%s", diff)
			}
		})
	}
}

func TestUserRequestAcceptsTheEdgesOfItsRanges(t *testing.T) {
	t.Parallel()
	bodies := map[string]string{
		"254 character email":      `{"email":"` + testsupport.Repeat("a", 242) + `@example.com","display_name":"A"}`,
		"64 character user id":     `{"email":"a@example.com","display_name":"A","user_id":"` + testsupport.Repeat("9", 64) + `"}`,
		"100 character names":      `{"email":"a@example.com","first_name":"` + testsupport.Repeat("é", 100) + `","last_name":"` + testsupport.Repeat("é", 100) + `","display_name":"A"}`,
		"100 character first name": `{"email":"a@example.com","first_name":"` + testsupport.Repeat("é", 100) + `"}`,
		"only a last name":         `{"email":"a@example.com","last_name":"Smith"}`,
		"user id with symbols":     `{"email":"a@example.com","display_name":"A","user_id":"A-1_b.2"}`,
		"another user's user id":   `{"email":"a@example.com","display_name":"A","user_id":"100235"}`,
	}
	for name, body := range bodies {
		t.Run(name, func(t *testing.T) {
			b := signedInAdmin(t, testsupport.NewFactory())
			createUser(t, b, ann)

			got := b.PostJSON("/api/v1/users", body)

			if diff := cmp.Diff(201, got.Status); diff != "" {
				t.Errorf("status (-want +got):\n%s\n%s", diff, got.Body)
			}
		})
	}
}

// fiveUsers is a browser signed in as the administrator after creating, in
// order, Eve, Dan, cara, Bo, and Ann, who are users 2 to 6, and deleting Bo.
func fiveUsers(t *testing.T, f *testsupport.Factory) *testsupport.Browser {
	t.Helper()
	b := signedInAdmin(t, f)
	for _, name := range []string{"Eve", "Dan", "cara", "Bo", "Ann"} {
		createUser(t, b, `{"email":"`+name+`@example.com","display_name":"`+name+`"}`)
	}
	send(t, b, "DELETE", "/api/v1/users/5", "", 204)
	return b
}

func TestUserListIsPagedAndOrderedByName(t *testing.T) {
	t.Parallel()
	cases := map[string]struct {
		query string
		want  string
	}{
		"first page by default": {query: "",
			want: `{"names":["Administrator","Ann","cara","Dan","Eve"],"total":5,"page":1,"per_page":25}`},
		"a later page": {query: "?page=2&per_page=3",
			want: `{"names":["Dan","Eve"],"total":5,"page":2,"per_page":3}`},
		"the largest page": {query: "?per_page=200",
			want: `{"names":["Administrator","Ann","cara","Dan","Eve"],"total":5,"page":1,"per_page":200}`},
		"a search": {query: "?q=+EXAMPLE.com&per_page=1",
			want: `{"names":["Administrator"],"total":5,"page":1,"per_page":1}`},
		"deleted users": {query: "?show=deleted",
			want: `{"names":["Bo"],"total":1,"page":1,"per_page":25}`},
		"current users": {query: "?show=current&q=b",
			want: `{"names":[],"total":0,"page":1,"per_page":25}`},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			b := fiveUsers(t, testsupport.NewFactory())

			got := b.Get("/api/v1/users" + c.query)

			var names []any
			for i := 0; testsupport.JSONAt(got.Body, "/users/"+testsupport.Itoa(i)) != nil; i++ {
				names = append(names, testsupport.JSONAt(got.Body, "/users/"+testsupport.Itoa(i)+"/display_name"))
			}
			if names == nil {
				names = []any{}
			}
			gotPage := map[string]any{"names": names, "total": testsupport.JSONAt(got.Body, "/total"),
				"page": testsupport.JSONAt(got.Body, "/page"), "per_page": testsupport.JSONAt(got.Body, "/per_page")}
			if diff := cmp.Diff(testsupport.JSON(c.want), any(gotPage)); diff != "" {
				t.Errorf("page (-want +got):\n%s", diff)
			}
		})
	}
}

func TestUserListQueryIsChecked(t *testing.T) {
	t.Parallel()
	for _, query := range []string{"?page=0", "?page=x", "?page=1000001", "?per_page=0", "?per_page=201", "?show=all"} {
		t.Run(query, func(t *testing.T) {
			got := signedInAdmin(t, testsupport.NewFactory()).Get("/api/v1/users" + query)

			want := apiError(400, "page must be a whole number from 1, per_page one from 1 to 200, and show current or deleted")
			if diff := cmp.Diff(want, got); diff != "" {
				t.Errorf("response (-want +got):\n%s", diff)
			}
		})
	}
}

func TestUserListAcceptsTheLastPage(t *testing.T) {
	t.Parallel()
	got := signedInAdmin(t, testsupport.NewFactory()).Get("/api/v1/users?page=1000000")

	want := testsupport.JSON(`{"users":[],"total":1,"page":1000000,"per_page":25}`)
	if diff := cmp.Diff(want, testsupport.JSON(got.Body)); diff != "" {
		t.Errorf("page (-want +got):\n%s", diff)
	}
}

func TestReadingAUser(t *testing.T) {
	t.Parallel()
	b := signedInAdmin(t, testsupport.NewFactory())
	createUser(t, b, ann)

	got := b.Get("/api/v1/users/2")

	want := testsupport.Response{Status: 200, ContentType: jsonType, Body: annJSON + "\n"}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("response (-want +got):\n%s", diff)
	}
}

func TestUserThatDoesNotExistIsNotFound(t *testing.T) {
	t.Parallel()
	cases := map[string]struct{ method, target, body string }{
		"read":         {method: "GET", target: "/api/v1/users/9"},
		"read by name": {method: "GET", target: "/api/v1/users/ann"},
		"update":       {method: "PUT", target: "/api/v1/users/9", body: `{"email":"x@example.com","display_name":"X"}`},
		"delete":       {method: "DELETE", target: "/api/v1/users/9"},
		"restore":      {method: "POST", target: "/api/v1/users/9/restore"},
		"set code":     {method: "PUT", target: "/api/v1/users/9/code", body: `{"code":"123456"}`},
		"clear code":   {method: "DELETE", target: "/api/v1/users/9/code"},
		"list tokens":  {method: "GET", target: "/api/v1/users/9/tokens"},
		"create token": {method: "POST", target: "/api/v1/users/9/tokens", body: `{"name":"x"}`},
		"revoke token": {method: "DELETE", target: "/api/v1/users/9/tokens/1"},
		"add to group": {method: "PUT", target: "/api/v1/groups/1/members/9"},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			got := signedInAdmin(t, testsupport.NewFactory()).Send(c.method, c.target, c.body)

			want := apiError(404, "no user has this id")
			if diff := cmp.Diff(want, got); diff != "" {
				t.Errorf("response (-want +got):\n%s", diff)
			}
		})
	}
}

func TestUpdatingAUserReplacesTheirProfile(t *testing.T) {
	t.Parallel()
	b := signedInAdmin(t, testsupport.NewFactory())
	createUser(t, b, ann)

	got := b.PutJSON("/api/v1/users/2", `{"email":"ann@example.com","display_name":"Annie","user_id":"100234",`+
		`"expires_at":"2026-01-05T09:00:00Z"}`)

	want := testsupport.Response{Status: 200, ContentType: jsonType, Body: `{"id":2,"email":"ann@example.com",` +
		`"user_id":"100234","first_name":"","last_name":"","display_name":"Annie","has_code":false,` +
		`"expires_at":"2026-01-05T09:00:00Z","expired":true,"created_at":"2026-01-05T09:00:00Z","groups":[]}` + "\n"}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("response (-want +got):\n%s", diff)
	}
}

func TestUpdatingAUserChecksTheProfile(t *testing.T) {
	t.Parallel()
	cases := map[string]struct {
		body string
		want string
	}{
		"not json":                {body: `[]`, want: `body must be a JSON object with a string \"email\"`},
		"another user's email":    {body: `{"email":"admin@example.com","display_name":"A"}`, want: "Another user has this email address."},
		"another user's user id":  {body: `{"email":"ann@example.com","display_name":"A","user_id":"7"}`, want: "Another user has this user id."},
		"an email and a bad name": {body: `{"email":"","display_name":""}`, want: "Enter an email address."},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			b := signedInAdmin(t, testsupport.NewFactory())
			createUser(t, b, ann)
			createUser(t, b, `{"email":"bo@example.com","display_name":"Bo","user_id":"7"}`)

			got := b.PutJSON("/api/v1/users/2", c.body)

			want := apiError(400, c.want)
			if diff := cmp.Diff(want, got); diff != "" {
				t.Errorf("response (-want +got):\n%s", diff)
			}
		})
	}
}

func TestDeletingAUserMarksThemDeletedOnce(t *testing.T) {
	t.Parallel()
	f := testsupport.NewFactory()
	b := signedInAdmin(t, f)
	createUser(t, b, ann)

	first := b.Delete("/api/v1/users/2")
	f.ClockStub.Set("2026-01-05T10:00:00Z")
	again := b.Delete("/api/v1/users/2")
	got := b.Get("/api/v1/users/2")

	if diff := cmp.Diff([]int{204, 204}, []int{first.Status, again.Status}); diff != "" {
		t.Errorf("statuses (-want +got):\n%s", diff)
	}
	if diff := cmp.Diff("2026-01-05T09:00:00Z", testsupport.JSONString(got.Body, "/deleted_at")); diff != "" {
		t.Errorf("deleted at (-want +got):\n%s", diff)
	}
}

func TestDeletedUserIsRefusedEverywhere(t *testing.T) {
	t.Parallel()
	h, admin := adminSession(t, testsupport.NewFactory())
	createUser(t, admin, ann)
	join(t, admin, "1", "2")
	annClient := userToken(t, h, admin, "2")

	send(t, admin, "DELETE", "/api/v1/users/2", "", 204)
	got := annClient.Get("/api/v1/tokens")

	if diff := cmp.Diff(401, got.Status); diff != "" {
		t.Errorf("status (-want +got):\n%s", diff)
	}
}

func TestRestoringAUserUndoesTheDeletion(t *testing.T) {
	t.Parallel()
	b := signedInAdmin(t, testsupport.NewFactory())
	createUser(t, b, ann)
	send(t, b, "DELETE", "/api/v1/users/2", "", 204)

	restored := b.Send("POST", "/api/v1/users/2/restore", "")
	got := b.Get("/api/v1/users/2")

	if diff := cmp.Diff(testsupport.Response{Status: 204}, restored); diff != "" {
		t.Errorf("restore (-want +got):\n%s", diff)
	}
	if diff := cmp.Diff(testsupport.JSON(annJSON), testsupport.JSON(got.Body)); diff != "" {
		t.Errorf("user (-want +got):\n%s", diff)
	}
}

func TestDeletingYourOwnAccountIsRefused(t *testing.T) {
	t.Parallel()
	got := signedInAdmin(t, testsupport.NewFactory()).Delete("/api/v1/users/1")

	want := apiError(409, "you cannot delete your own account")
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("response (-want +got):\n%s", diff)
	}
}

func TestSettingADoorCodeIsShownOnlyAsHavingOne(t *testing.T) {
	t.Parallel()
	b := signedInAdmin(t, testsupport.NewFactory())
	createUser(t, b, ann)

	set := b.PutJSON("/api/v1/users/2/code", `{"code":"12345678901234567890"}`)
	got := b.Get("/api/v1/users/2")

	if diff := cmp.Diff(testsupport.Response{Status: 204}, set); diff != "" {
		t.Errorf("setting (-want +got):\n%s", diff)
	}
	want := testsupport.JSON(annJSON)
	want.(map[string]any)["has_code"] = true
	if diff := cmp.Diff(want, testsupport.JSON(got.Body)); diff != "" {
		t.Errorf("user (-want +got):\n%s", diff)
	}
}

func TestDoorCodeIsChecked(t *testing.T) {
	t.Parallel()
	cases := map[string]struct {
		body string
		want string
	}{
		"not json":       {body: `{"code":123456}`, want: `body must be a JSON object with a string \"code\"`},
		"too short":      {body: `{"code":"12345"}`, want: "Enter 6 to 20 digits."},
		"too long":       {body: `{"code":"123456789012345678901"}`, want: "Enter 6 to 20 digits."},
		"not digits":     {body: `{"code":"12345a"}`, want: "Enter 6 to 20 digits."},
		"with a space":   {body: `{"code":"123 456"}`, want: "Enter 6 to 20 digits."},
		"another's code": {body: `{"code":"654321"}`, want: "Another user has this code. Choose a different one."},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			b := signedInAdmin(t, testsupport.NewFactory())
			createUser(t, b, ann)
			send(t, b, "PUT", "/api/v1/users/1/code", `{"code":"654321"}`, 204)

			got := b.PutJSON("/api/v1/users/2/code", c.body)

			want := apiError(400, c.want)
			if diff := cmp.Diff(want, got); diff != "" {
				t.Errorf("response (-want +got):\n%s", diff)
			}
		})
	}
}

func TestUserMaySetTheCodeTheyAlreadyHave(t *testing.T) {
	t.Parallel()
	b := signedInAdmin(t, testsupport.NewFactory())
	send(t, b, "PUT", "/api/v1/users/1/code", `{"code":"654321"}`, 204)

	got := b.PutJSON("/api/v1/users/1/code", `{"code":"654321"}`)

	if diff := cmp.Diff(204, got.Status); diff != "" {
		t.Errorf("status (-want +got):\n%s", diff)
	}
}

func TestDoorCodesStayUniqueWhenTheHashingCostChanges(t *testing.T) {
	t.Parallel()
	f := testsupport.NewFactory()
	b := signedInAdmin(t, f)
	createUser(t, b, ann)
	send(t, b, "PUT", "/api/v1/users/1/code", `{"code":"654321"}`, 204)
	delete(f.EnvStub, "DOORCODE_PASSWORD_HASH_MEMORY_KIB")
	send(t, b, "PUT", "/api/v1/settings/password_hash_memory_kib", `{"value":"16"}`, 200)

	got := b.PutJSON("/api/v1/users/2/code", `{"code":"654321"}`)

	if diff := cmp.Diff([]any{400, "Another user has this code. Choose a different one."},
		[]any{got.Status, testsupport.JSONString(got.Body, "/error")}); diff != "" {
		t.Errorf("status and error (-want +got):\n%s", diff)
	}
}

func TestRemovingADoorCode(t *testing.T) {
	t.Parallel()
	b := signedInAdmin(t, testsupport.NewFactory())
	createUser(t, b, ann)
	send(t, b, "PUT", "/api/v1/users/2/code", `{"code":"654321"}`, 204)

	cleared := b.Delete("/api/v1/users/2/code")
	reused := b.PutJSON("/api/v1/users/1/code", `{"code":"654321"}`)
	got := b.Get("/api/v1/users/2")

	if diff := cmp.Diff([]any{204, 204, false}, []any{cleared.Status, reused.Status, testsupport.JSONAt(got.Body, "/has_code")}); diff != "" {
		t.Errorf("cleared, code reused, has code (-want +got):\n%s", diff)
	}
}

func TestDoorCodeHashingThatCannotBeReadAnswersWithARequestID(t *testing.T) {
	t.Parallel()
	f := testsupport.NewFactory()
	b := signedInAdmin(t, f)
	f.StoreStub.SeedCodeHashing("not parameters")

	got := b.PutJSON("/api/v1/users/1/code", `{"code":"654321"}`)

	want := internalError("5253545556575859")
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("response (-want +got):\n%s", diff)
	}
}

func TestAdministratorManagesAnotherUsersTokens(t *testing.T) {
	t.Parallel()
	h, b := adminSession(t, testsupport.NewFactory())
	createUser(t, b, ann)

	created := b.PostJSON("/api/v1/users/2/tokens", `{"name":" sync ","expires_in_days":30}`)
	listed := b.Get("/api/v1/users/2/tokens")
	own := b.Get("/api/v1/tokens")
	asAnn := tokenClient(h, firstSecret).Get("/api/v1/tokens")
	revoked := b.Delete("/api/v1/users/2/tokens/1")
	again := b.Delete("/api/v1/users/2/tokens/1")

	token := `{"id":1,"name":"sync","created_at":"2026-01-05T09:00:00Z","expires_at":"2026-02-04T09:00:00Z","expired":false}`
	if diff := cmp.Diff(testsupport.JSON(`{"token":`+token+`,"secret":"`+firstSecret+`"}`), testsupport.JSON(created.Body)); diff != "" {
		t.Errorf("created (-want +got):\n%s", diff)
	}
	want := []any{testsupport.JSON(`{"tokens":[` + token + `]}`), testsupport.JSON(`{"tokens":[]}`), testsupport.JSON(`{"tokens":[` + token + `]}`)}
	if diff := cmp.Diff(want, []any{testsupport.JSON(listed.Body), testsupport.JSON(own.Body), testsupport.JSON(asAnn.Body)}); diff != "" {
		t.Errorf("ann's tokens, the administrator's, and ann's through her token (-want +got):\n%s", diff)
	}
	if diff := cmp.Diff([]any{204, 404, `{"error":"the user has no token with this id"}` + "\n"},
		[]any{revoked.Status, again.Status, again.Body}); diff != "" {
		t.Errorf("revoked, again (-want +got):\n%s", diff)
	}
}

func TestTokenRequestForAnotherUserIsChecked(t *testing.T) {
	t.Parallel()
	b := signedInAdmin(t, testsupport.NewFactory())
	createUser(t, b, ann)

	got := b.PostJSON("/api/v1/users/2/tokens", `{"name":""}`)

	want := apiError(400, "Enter a name for the token.")
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("response (-want +got):\n%s", diff)
	}
}

func TestUserStoreFailureAnswersWithARequestID(t *testing.T) {
	t.Parallel()
	cases := map[string]struct {
		fail   func(s *testsupport.Store)
		method string
		target string
		body   string
		id     string
	}{
		"list":                                  {fail: (*testsupport.Store).FailUsers, method: "GET", target: "/api/v1/users"},
		"create":                                {fail: (*testsupport.Store).FailUsers, method: "POST", target: "/api/v1/users", body: ann},
		"create, storing it":                    {fail: (*testsupport.Store).FailUserWrites, method: "POST", target: "/api/v1/users", body: ann},
		"create, reading groups":                {fail: (*testsupport.Store).FailGroups, method: "POST", target: "/api/v1/users", body: ann},
		"create without a user id":              {fail: (*testsupport.Store).FailUsers, method: "POST", target: "/api/v1/users", body: `{"email":"x@example.com","display_name":"X"}`},
		"read":                                  {fail: (*testsupport.Store).FailUsers, method: "GET", target: "/api/v1/users/1"},
		"update":                                {fail: (*testsupport.Store).FailUsers, method: "PUT", target: "/api/v1/users/1", body: ann},
		"delete":                                {fail: (*testsupport.Store).FailUsers, method: "DELETE", target: "/api/v1/users/2"},
		"restore":                               {fail: (*testsupport.Store).FailUsers, method: "POST", target: "/api/v1/users/2/restore"},
		"set code":                              {fail: (*testsupport.Store).FailUsers, method: "PUT", target: "/api/v1/users/1/code", body: `{"code":"123456"}`, id: "5253545556575859"},
		"set code, storing it":                  {fail: (*testsupport.Store).FailUserWrites, method: "PUT", target: "/api/v1/users/1/code", body: `{"code":"123456"}`, id: "5253545556575859"},
		"set code, fixing how codes are hashed": {fail: (*testsupport.Store).FailSettingWrites, method: "PUT", target: "/api/v1/users/1/code", body: `{"code":"123456"}`, id: "5253545556575859"},
		"set code without settings":             {fail: (*testsupport.Store).FailSettings, method: "PUT", target: "/api/v1/users/1/code", body: `{"code":"123456"}`},
		"clear code":                            {fail: (*testsupport.Store).FailUsers, method: "DELETE", target: "/api/v1/users/1/code"},
		"list tokens":                           {fail: (*testsupport.Store).FailTokens, method: "GET", target: "/api/v1/users/1/tokens"},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			f := testsupport.NewFactory()
			b := signedInAdmin(t, f)
			c.fail(f.StoreStub)

			got := b.Send(c.method, c.target, c.body)

			id := c.id
			if id == "" {
				id = "4243444546474849"
			}
			want := internalError(id)
			if diff := cmp.Diff(want, got); diff != "" {
				t.Errorf("response (-want +got):\n%s", diff)
			}
		})
	}
}

// searchSite is the administrator, Ann Smith (2), and 21 users named Zed
// 11 to Zed 31 (3 to 23), with Zed 32 (24) deleted.
func searchSite(t *testing.T, f *testsupport.Factory) *testsupport.Browser {
	t.Helper()
	b := signedInAdmin(t, f)
	createUser(t, b, ann)
	for i := range 22 {
		n := testsupport.Itoa(i + 11)
		createUser(t, b, `{"email":"zed`+n+`@example.com","display_name":"Zed `+n+`"}`)
	}
	send(t, b, "DELETE", "/api/v1/users/24", "", 204)
	return b
}

func TestTheUserSearchFindsCurrentUsersByNameEmailOrUserID(t *testing.T) {
	t.Parallel()
	annFound := `{"users":[{"id":2,"name":"Ann Smith","email":"ann@example.com"}],"total":1}`
	for query, want := range map[string]string{"q=smith": annFound, "q=+ANN@EXAMPLE": annFound, "q=100234": annFound, "q=nobody": `{"users":[],"total":0}`} {
		t.Run(query, func(t *testing.T) {
			t.Parallel()
			b := searchSite(t, testsupport.NewFactory())

			got := b.Get("/api/v1/users/search?" + query)

			if diff := cmp.Diff(testsupport.JSON(want), testsupport.JSON(got.Body)); diff != "" {
				t.Errorf("users found (-want +got):\n%s", diff)
			}
		})
	}
}

func TestTheUserSearchReturnsTheFirstTwentyMatchesAndHowManyMatch(t *testing.T) {
	t.Parallel()
	b := searchSite(t, testsupport.NewFactory())

	got := b.Get("/api/v1/users/search?q=zed")

	want := []any{"Zed 11", "Zed 30", 20.0, 21.0}
	users, _ := testsupport.JSONAt(got.Body, "/users").([]any)
	gotFound := []any{testsupport.JSONAt(got.Body, "/users/0/name"), testsupport.JSONAt(got.Body, "/users/19/name"), float64(len(users)), testsupport.JSONAt(got.Body, "/total")}
	if diff := cmp.Diff(want, gotFound); diff != "" {
		t.Errorf("first, twentieth, how many returned, how many match (-want +got):\n%s", diff)
	}
}

func TestTheUserSearchIsForThoseWhoManageUsersOrViewTheLog(t *testing.T) {
	t.Parallel()
	for roles, want := range map[string]int{`["manage-users"]`: 200, `["view-logs"]`: 200, `["manage-doors"]`: 403} {
		t.Run(roles, func(t *testing.T) {
			t.Parallel()
			_, b := adminHolding(t, testsupport.NewFactory(), roles)

			if diff := cmp.Diff(want, b.Get("/api/v1/users/search?q=a").Status); diff != "" {
				t.Errorf("status (-want +got):\n%s", diff)
			}
		})
	}
}
