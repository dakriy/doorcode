package entrypoint_test

import (
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/dakriy/doorcode/internal/testsupport"
)

// portalNav is the portal's navigation with the link labelled current
// marked as the page shown; an empty current marks none.
func portalNav(current string) []string {
	nav := []string{}
	for _, label := range []string{"Home", "Door code", "Password", "Access", "Entries", "Requests", "API tokens"} {
		if label == current {
			label += " (current)"
		}
		nav = append(nav, label)
	}
	return nav
}

// toAdministration is where the portal's Administration link leads the
// first administrator.
var toAdministration = map[string]string{"Administration": "/admin"}

// noUserIDNote is what the portal says to a user without a user id about
// their door code.
const noUserIDNote = "You have no user id, so you cannot choose a door code; an administrator can set one for you."

// homeCards is what each card on home says, for a user who has a door
// code when coded is true and has open requests waiting.
func homeCards(coded bool, waiting string) map[string]string {
	cards := map[string]string{
		"Door code": "Choose the code you type at the keypad.", "Password": "Change the password you sign in with.",
		"Access": "When each door is open and when your code works there.", "Entries": "Every time you entered your code, and what the door decided.",
		"Requests":   "Ask an administrator for access to a door or a change.",
		"API tokens": "Tokens that let your scripts call the API as you.",
	}
	if coded {
		cards["Door code"] = "Your code is set. Choose a new one at any time."
	}
	if waiting != "" {
		cards["Requests"] = waiting + " waiting for an answer."
	}
	return cards
}

// noPortalDoors and noEntries are the rows home's tables show when the
// portal shows no door and the viewer has no entries.
var (
	noPortalDoors = [][]string{{"Door", "Now", "Calendar"}, {"No doors are shown in the portal yet. An administrator decides which doors appear here."}}
	noEntries     = [][]string{{"When", "Door", "Outcome"}, {"No entries yet."}}
)

// portalPassword is the password users choose in the portal's tests.
const portalPassword = "correct horse battery staple"

// annEmail is Ann's email.
const annEmail = "ann@example.com"

// signedInAs returns a new browser on b's site signed in as the user with
// id user and email, who first chooses portalPassword through a token the
// administrator b creates for them; the sign-in's flash is spent.
func signedInAs(t *testing.T, b *testsupport.Browser, user, email string) *testsupport.Browser {
	t.Helper()
	send(t, asUser(t, b, user), "PUT", "/api/v1/me/password", `{"new_password":"`+portalPassword+`"}`, 204)
	person := b.Another()
	if got := person.Post("/sign-in", "email", email, "password", portalPassword); got.Status != 303 {
		t.Fatalf("sign-in as %s answered %d, want 303:\n%s", email, got.Status, got.Body)
	}
	person.Get("/")
	return person
}

// asUser is a client on b's site holding a token the administrator b
// creates for the user with id user.
func asUser(t *testing.T, b *testsupport.Browser, user string) *testsupport.Browser {
	t.Helper()
	created := send(t, b, "POST", "/api/v1/users/"+user+"/tokens", `{"name":"portal"}`, 201)
	c := b.Another()
	c.SetHeader("Authorization", "Bearer "+testsupport.JSONString(created.Body, "/secret"))
	return c
}

// portalSite is staffAnn with Lobby (1) public, at Building A, and in open
// mode during Nights (3) through its zone North (1), and a rule (1)
// letting Staff into North on Weekdays (2). It returns the administrator's
// browser and Ann's, signed in.
func portalSite(t *testing.T, f *testsupport.Factory) (admin, ann *testsupport.Browser) {
	t.Helper()
	admin = staffAnn(t, f)
	send(t, admin, "PUT", "/api/v1/doors/1", `{"name":"Lobby","location":"Building A","public":true,"zone_ids":[1]}`, 200)
	send(t, admin, "PUT", "/api/v1/zones/1", `{"name":"North","door_ids":[1],"open_schedule_ids":[3]}`, 200)
	createRule(t, admin, `{"group_id":2,"schedule_id":2,"zone_ids":[1]}`)
	return admin, signedInAs(t, admin, "2", annEmail)
}

func TestChoosingADoorCodeStoresTheHashOfTheUserIDWithoutLeadingZerosAndTheDigits(t *testing.T) {
	t.Parallel()
	f := testsupport.NewFactory()
	b := signedInAdmin(t, f)
	createUser(t, b, `{"email":"cara@example.com","display_name":"Cara","user_id":"000417"}`)
	cara := asUser(t, b, "2")
	before := testsupport.JSON(send(t, cara, "GET", "/api/v1/me/code", "", 200).Body)

	set := cara.PutJSON("/api/v1/me/code", `{"digits":"4821"}`)
	after := testsupport.JSON(send(t, cara, "GET", "/api/v1/me/code", "", 200).Body)

	want := []any{testsupport.JSON(`{"has_code":false,"prefix":"417"}`), 204, testsupport.JSON(`{"has_code":true,"prefix":"417"}`), f.StoreStub.HashCode("4174821")}
	if diff := cmp.Diff(want, []any{before, set.Status, after, f.StoreStub.StoredCodeHash("cara@example.com")}); diff != "" {
		t.Errorf("before, setting, after, stored hash (-want +got):\n%s", diff)
	}
}

func TestSettingTheDoorCodeFromThePortalConfirmsWithoutShowingIt(t *testing.T) {
	t.Parallel()
	f := testsupport.NewFactory()
	_, ann := portalSite(t, f)

	set := ann.Post("/me/code", "digits", "4821")
	got := pageOf(ann.Get("/me/code"))

	want := testsupport.Page{
		Title: "Door code · doorcode", Heading: "Door code", Nav: portalNav("Door code"),
		Messages: []string{"Your door code is set. Remember it: it is stored only as a hash and cannot be shown again."},
	}
	if diff := cmp.Diff([]any{303, "/me/code", want, f.StoreStub.HashCode("1002344821")},
		[]any{set.Status, set.Location, got, f.StoreStub.StoredCodeHash(annEmail)}); diff != "" {
		t.Errorf("status, location, page, stored hash (-want +got):\n%s", diff)
	}
}

func TestAChosenCodeMayHaveFourToEightDigits(t *testing.T) {
	t.Parallel()
	f := testsupport.NewFactory()
	ann := asUser(t, staffAnn(t, f), "2")

	shortest := ann.PutJSON("/api/v1/me/code", `{"digits":"1234"}`)
	longest := ann.PutJSON("/api/v1/me/code", `{"digits":"12345678"}`)

	if diff := cmp.Diff([]any{204, 204, f.StoreStub.HashCode("10023412345678")}, []any{shortest.Status, longest.Status, f.StoreStub.StoredCodeHash(annEmail)}); diff != "" {
		t.Errorf("four digits, eight digits, stored hash (-want +got):\n%s", diff)
	}
}

func TestTheDoorCodeFormSaysWhatIsWrong(t *testing.T) {
	t.Parallel()
	cases := map[string]struct {
		digits string
		want   string
	}{
		"too few":        {"123", "Enter 4 to 8 digits."},
		"too many":       {"123456789", "Enter 4 to 8 digits."},
		"not digits":     {"12a4", "Enter 4 to 8 digits."},
		"another's code": {"4821", "Another user has this code. Choose a different one."},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			admin, ann := portalSite(t, testsupport.NewFactory())
			send(t, admin, "PUT", "/api/v1/users/1/code", `{"code":"1002344821"}`, 204)

			got := ann.Post("/me/code", "digits", c.digits)

			want := testsupport.Page{Title: "Door code · doorcode", Heading: "Door code", Nav: portalNav("Door code"), FieldErrors: map[string]string{"digits": c.want}}
			if diff := cmp.Diff([]any{422, want}, []any{got.Status, pageOf(got)}); diff != "" {
				t.Errorf("status, page (-want +got):\n%s", diff)
			}
		})
	}
}

func TestChoosingADoorCodeThroughTheAPIIsChecked(t *testing.T) {
	t.Parallel()
	cases := map[string]struct {
		body string
		want string
	}{
		"not json":       {`{"digits":4821}`, `body must be a JSON object with a string \"digits\"`},
		"too few":        {`{"digits":"123"}`, "Enter 4 to 8 digits."},
		"another's code": {`{"digits":"4821"}`, "Another user has this code. Choose a different one."},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			admin := staffAnn(t, testsupport.NewFactory())
			send(t, admin, "PUT", "/api/v1/users/1/code", `{"code":"1002344821"}`, 204)

			got := asUser(t, admin, "2").PutJSON("/api/v1/me/code", c.body)

			want := apiError(400, c.want)
			if diff := cmp.Diff(want, got); diff != "" {
				t.Errorf("response (-want +got):\n%s", diff)
			}
		})
	}
}

func TestAUserWithoutAUsableUserIDIsToldWhyTheyCannotChooseACode(t *testing.T) {
	t.Parallel()
	notNumber := "Your user id is not a number, so you cannot choose a door code; an administrator can set one for you."
	cases := map[string]struct {
		userID string
		note   string
	}{
		"none":         {"", noUserIDNote},
		"with letters": {`,"user_id":"A100"`, notNumber},
		"only zeros":   {`,"user_id":"000"`, notNumber},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			admin := signedInAdmin(t, testsupport.NewFactory())
			createUser(t, admin, `{"email":"bo@example.com","display_name":"Bo"`+c.userID+`}`)
			bo := signedInAs(t, admin, "2", "bo@example.com")

			page := bo.Get("/me/code")
			posted := bo.Post("/me/code", "digits", "4821")
			api := asUser(t, admin, "2").PutJSON("/api/v1/me/code", `{"digits":"4821"}`)

			want := testsupport.Page{Title: "Door code · doorcode", Heading: "Door code", Nav: portalNav("Door code"), Notes: []string{c.note}}
			if diff := cmp.Diff([]any{200, want, 409, want, 409, c.note},
				[]any{page.Status, pageOf(page), posted.Status, pageOf(posted), api.Status, sentence(testsupport.JSONString(api.Body, "/error"))}); diff != "" {
				t.Errorf("page, posting, the API (-want +got):\n%s", diff)
			}
		})
	}
}

// sentence is what the API says as a sentence on its own.
func sentence(s string) string {
	return string(s[0]-'a'+'A') + s[1:] + "."
}

// lengthNote is what the password form says about a password's length.
const lengthNote = "Use at least 12 characters. A few unrelated words make a strong password that is easy to remember."

// autoSignIn turns signing in through the identity provider on, with
// auto sign-in, on f's site.
func autoSignIn(f *testsupport.Factory) {
	f.EnvStub["DOORCODE_OIDC_ENABLED"] = "true"
	f.EnvStub["DOORCODE_OIDC_AUTO_SIGN_IN"] = "true"
}

func TestThePasswordFormIsOfferedUnlessEveryoneSignsInAtTheProvider(t *testing.T) {
	t.Parallel()
	f := testsupport.NewFactory()
	_, ann := portalSite(t, f)
	offered := ann.Get("/me/password")
	autoSignIn(f)

	hidden := ann.Get("/me/password")
	posted := ann.Post("/me/password", "current_password", portalPassword, "new_password", "a new and longer one", "confirm_password", "a new and longer one")

	withForm := testsupport.Page{Title: "Password · doorcode", Heading: "Password", Nav: portalNav("Password"), Notes: []string{lengthNote}}
	without := testsupport.Page{
		Title: "Password · doorcode", Heading: "Password",
		Nav:   []string{"Home", "Door code", "Access", "Entries", "Requests", "API tokens"},
		Notes: []string{"Everyone signs in to this site with single sign-on, so it has no password for you to change. Change your password with your organization’s identity provider instead."},
	}
	if diff := cmp.Diff([]any{withForm, without, 403, without}, []any{pageOf(offered), pageOf(hidden), posted.Status, pageOf(posted)}); diff != "" {
		t.Errorf("offered, hidden, posting while hidden (-want +got):\n%s", diff)
	}
}

func TestChangingThePasswordFromThePortalSignsInWithTheNewOne(t *testing.T) {
	t.Parallel()
	_, ann := portalSite(t, testsupport.NewFactory())

	changed := ann.Post("/me/password", "current_password", portalPassword, "new_password", "a new and longer one", "confirm_password", "a new and longer one")
	page := pageOf(ann.Get("/me/password"))
	withNew := ann.Another().Post("/sign-in", "email", annEmail, "password", "a new and longer one")
	withOld := ann.Another().Post("/sign-in", "email", annEmail, "password", portalPassword)

	want := testsupport.Page{
		Title: "Password · doorcode", Heading: "Password", Nav: portalNav("Password"),
		Messages: []string{"Your password has been changed. Use it the next time you sign in."}, Notes: []string{lengthNote},
	}
	if diff := cmp.Diff([]any{303, "/me/password", want, 303, 422}, []any{changed.Status, changed.Location, page, withNew.Status, withOld.Status}); diff != "" {
		t.Errorf("status, location, page, signing in with the new password and the old (-want +got):\n%s", diff)
	}
}

func TestAUserWithoutAPasswordChoosesOneWithoutGivingTheCurrent(t *testing.T) {
	t.Parallel()
	f := testsupport.NewFactory()
	b := signedInAdmin(t, f)
	b.Get("/")
	f.StoreStub.RemovePassword(adminEmail)

	page := pageOf(b.Get("/me/password"))
	chosen := b.Post("/me/password", "new_password", "a brand new password", "confirm_password", "a brand new password")
	signedIn := b.Another().Post("/sign-in", "email", adminEmail, "password", "a brand new password")

	want := testsupport.Page{
		Title: "Password · doorcode", Heading: "Password", Nav: portalNav("Password"),
		Notes:       []string{"You have no password yet. Choose one to sign in with your email address and password.", lengthNote},
		LinkTargets: toAdministration,
	}
	if diff := cmp.Diff([]any{want, 303, 303}, []any{page, chosen.Status, signedIn.Status}); diff != "" {
		t.Errorf("page, choosing, signing in (-want +got):\n%s", diff)
	}
}

func TestThePasswordFormSaysWhatIsWrong(t *testing.T) {
	t.Parallel()
	cases := map[string]struct {
		fields []string
		want   map[string]string
	}{
		"not repeated": {[]string{"current_password", portalPassword, "new_password", "a new and longer one", "confirm_password", "a new and longer"},
			map[string]string{"confirm_password": "The passwords do not match. Type the new one twice."}},
		"wrong current": {[]string{"current_password", "a guess", "new_password", "a new and longer one", "confirm_password", "a new and longer one"},
			map[string]string{"current_password": "That is not your current password."}},
		"too short": {[]string{"current_password", portalPassword, "new_password", "short", "confirm_password", "short"},
			map[string]string{"new_password": "Use at least 12 characters."}},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			_, ann := portalSite(t, testsupport.NewFactory())

			got := ann.Post("/me/password", c.fields...)

			want := testsupport.Page{Title: "Password · doorcode", Heading: "Password", Nav: portalNav("Password"), Notes: []string{lengthNote}, FieldErrors: c.want}
			if diff := cmp.Diff([]any{422, want}, []any{got.Status, pageOf(got)}); diff != "" {
				t.Errorf("status, page (-want +got):\n%s", diff)
			}
		})
	}
}

func TestChoosingAPasswordThroughTheAPIIsChecked(t *testing.T) {
	t.Parallel()
	long := ""
	for range 1025 {
		long += "x"
	}
	cases := map[string]struct {
		arrange func(f *testsupport.Factory)
		body    string
		status  int
		want    string
	}{
		"not json":      {body: `{"new_password":7}`, status: 400, want: `body must be a JSON object with a string \"new_password\"`},
		"wrong current": {body: `{"current_password":"a guess","new_password":"a new and longer one"}`, status: 400, want: "That is not your current password."},
		"too short":     {body: `{"current_password":"` + portalPassword + `","new_password":"elevenchars"}`, status: 400, want: "Use at least 12 characters."},
		"too long":      {body: `{"current_password":"` + portalPassword + `","new_password":"` + long + `"}`, status: 400, want: "Use at most 1024 characters."},
		"under auto sign-in": {arrange: autoSignIn, body: `{"current_password":"` + portalPassword + `","new_password":"a new and longer one"}`,
			status: 403, want: "passwords are not used here; everyone signs in with single sign-on"},
		"a longer minimum": {arrange: func(f *testsupport.Factory) { f.EnvStub["DOORCODE_PASSWORD_MIN_LENGTH"] = "24" },
			body: `{"current_password":"` + portalPassword + `","new_password":"a new and longer one"}`, status: 400, want: "Use at least 24 characters."},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			f := testsupport.NewFactory()
			admin := staffAnn(t, f)
			signedInAs(t, admin, "2", annEmail)
			ann := asUser(t, admin, "2")
			if c.arrange != nil {
				c.arrange(f)
			}

			got := ann.PutJSON("/api/v1/me/password", c.body)

			want := apiError(c.status, c.want)
			if diff := cmp.Diff(want, got); diff != "" {
				t.Errorf("response (-want +got):\n%s", diff)
			}
		})
	}
}

func TestAPasswordOfExactlyTheMinimumLengthIsAccepted(t *testing.T) {
	t.Parallel()
	admin := staffAnn(t, testsupport.NewFactory())

	got := asUser(t, admin, "2").PutJSON("/api/v1/me/password", `{"new_password":"twelve chars"}`)
	long := ""
	for range 1024 {
		long += "x"
	}
	longest := asUser(t, admin, "2").PutJSON("/api/v1/me/password", `{"current_password":"twelve chars","new_password":"`+long+`"}`)

	if diff := cmp.Diff([]int{204, 204}, []int{got.Status, longest.Status}); diff != "" {
		t.Errorf("shortest, longest (-want +got):\n%s", diff)
	}
}

func TestTheAccessCalendarShowsWhenTheDoorIsUnlockedAndWhenTheUsersCodeWorks(t *testing.T) {
	t.Parallel()
	admin, ann := portalSite(t, testsupport.NewFactory())
	createOverride(t, admin, "1", `{"kind":"open","start":"2026-01-07T12:00:00Z","end":"2026-01-07T13:00:00Z"}`)
	createOverride(t, admin, "1", `{"kind":"locked","start":"2026-01-09T08:00:00Z","end":"2026-01-09T10:00:00Z"}`)

	got := ann.Get("/me/access/1")

	want := testsupport.Page{
		Title: "Lobby · doorcode", Heading: "Lobby", Nav: append(portalNav("Access"), "Previous week", "This week", "Next week"),
		Tables: map[string][][]string{
			"When your code works": {
				{"Starts", "Ends", "Length"},
				{"Mon 5 Jan 08:00 UTC", "Mon 5 Jan 18:00 UTC", "10 h"},
				{"Tue 6 Jan 08:00 UTC", "Tue 6 Jan 18:00 UTC", "10 h"},
				{"Wed 7 Jan 08:00 UTC", "Wed 7 Jan 18:00 UTC", "10 h"},
				{"Thu 8 Jan 08:00 UTC", "Thu 8 Jan 18:00 UTC", "10 h"},
				{"Fri 9 Jan 10:00 UTC", "Fri 9 Jan 18:00 UTC", "8 h"},
			},
			"Open and locked": {
				{"State", "Starts", "Ends", "Length"},
				{"Open mode", "Mon 5 Jan 22:00 UTC", "Tue 6 Jan 06:00 UTC", "8 h"},
				{"Open mode", "Wed 7 Jan 12:00 UTC", "Wed 7 Jan 13:00 UTC", "1 h"},
				{"Locked", "Fri 9 Jan 08:00 UTC", "Fri 9 Jan 10:00 UTC", "2 h"},
			},
		},
	}
	if diff := cmp.Diff([]any{200, want}, []any{got.Status, pageOf(got)}); diff != "" {
		t.Errorf("status, page (-want +got):\n%s", diff)
	}
}

func TestTheAccessCalendarRefusesADoorThePortalDoesNotShow(t *testing.T) {
	t.Parallel()
	cases := map[string]struct {
		arrange func(t *testing.T, admin *testsupport.Browser)
		target  string
	}{
		"not public": {target: "/me/access/2"},
		"deleted": {target: "/me/access/1", arrange: func(t *testing.T, admin *testsupport.Browser) {
			send(t, admin, "DELETE", "/api/v1/doors/1", "", 204)
		}},
		"missing": {target: "/me/access/9"},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			admin, ann := portalSite(t, testsupport.NewFactory())
			if c.arrange != nil {
				c.arrange(t, admin)
			}

			got := ann.Get(c.target)

			want := testsupport.Page{Title: "Not found · doorcode", Heading: "Not found", Nav: portalNav("Access")}
			if diff := cmp.Diff([]any{404, want}, []any{got.Status, pageOf(got)}); diff != "" {
				t.Errorf("status, page (-want +got):\n%s", diff)
			}
		})
	}
}

// accessDoors is portalSite with Dock (2) public, open from 08:30 to 09:30
// today, Attic (3) public, locked from 08:00 to 10:00, and the door Cellar
// (4) public in Annex (2), which Staff enter at Nights (3) through a rule
// (2) on the zone. The clock reads Monday 09:00.
func accessDoors(t *testing.T, f *testsupport.Factory) (admin, ann *testsupport.Browser) {
	t.Helper()
	admin, ann = portalSite(t, f)
	send(t, admin, "PUT", "/api/v1/doors/2", `{"name":"Dock","public":true,"zone_ids":[2]}`, 200)
	send(t, admin, "PUT", "/api/v1/doors/3", `{"name":"Attic","public":true}`, 200)
	createDoor(t, admin, `{"name":"Cellar","location":"Basement","public":true,"zone_ids":[2]}`)
	createOverride(t, admin, "2", `{"kind":"open","start":"2026-01-05T08:30:00Z","end":"2026-01-05T09:30:00Z"}`)
	createOverride(t, admin, "3", `{"kind":"locked","start":"2026-01-05T08:00:00Z","end":"2026-01-05T10:00:00Z"}`)
	createRule(t, admin, `{"group_id":2,"schedule_id":3,"zone_ids":[2]}`)
	return admin, ann
}

func TestAccessListsThePortalsDoorsWithWhatEachDoesNow(t *testing.T) {
	t.Parallel()
	_, ann := accessDoors(t, testsupport.NewFactory())

	got := ann.Get("/me/access")

	want := testsupport.Page{
		Title: "Access · doorcode", Heading: "Access", Nav: portalNav("Access"),
		Tables: map[string][][]string{"Your doors": {
			{"Door", "Now", "Calendar"},
			{"Attic", "Locked now: no code opens it", "Calendar"},
			{"Cellar Basement", "Your code works here at set times", "Calendar"},
			{"Dock", "Open mode now: any keypress unlocks it", "Calendar"},
			{"Lobby Building A", "Your code works now", "Calendar"},
		}},
		LinkTargets: map[string]string{
			"Calendar of Attic": "/me/access/3", "Calendar of Cellar": "/me/access/4", "Calendar of Dock": "/me/access/2", "Calendar of Lobby": "/me/access/1",
		},
	}
	if diff := cmp.Diff([]any{200, want}, []any{got.Status, pageOf(got)}); diff != "" {
		t.Errorf("status, page (-want +got):\n%s", diff)
	}
}

func TestAccessSaysWhenTheUsersCodeNeverOpensADoor(t *testing.T) {
	t.Parallel()
	admin, ann := portalSite(t, testsupport.NewFactory())
	send(t, admin, "PUT", "/api/v1/doors/3", `{"name":"Attic","public":true}`, 200)

	got := pageOf(ann.Get("/me/access/3"))

	want := testsupport.Page{
		Title: "Attic · doorcode", Heading: "Attic", Nav: append(portalNav("Access"), "Previous week", "This week", "Next week"),
		Notes: []string{"None of your groups has access to this door, so your code does not open it; you get in only while it is in open mode. Ask for access ."},
		Tables: map[string][][]string{
			"When your code works": {{"Starts", "Ends", "Length"}, {"Your code does not open this door this week."}},
			"Open and locked":      {{"State", "Starts", "Ends", "Length"}, {"The door is neither in open mode nor locked this week."}},
		},
	}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("page (-want +got):\n%s", diff)
	}
	if diff := cmp.Diff("Your code never opens this door", pageOf(ann.Get("/me/access")).Tables["Your doors"][1][1]); diff != "" {
		t.Errorf("access list (-want +got):\n%s", diff)
	}
}

func TestMyAccessAnswersWithThePortalsDoorsAndWhenTheUsersCodeWorks(t *testing.T) {
	t.Parallel()
	admin, _ := portalSite(t, testsupport.NewFactory())
	uploadPicture(t, admin, "1", testsupport.PNG(30, 20))
	send(t, admin, "PUT", "/api/v1/doors/3", `{"name":"Attic","public":true}`, 200)

	got := testsupport.JSON(send(t, asUser(t, admin, "2"), "GET", "/api/v1/me/access?from=2026-01-05&to=2026-01-05", "", 200).Body)

	want := testsupport.JSON(`{"time_zone":"UTC","from":"2026-01-05","to":"2026-01-05","doors":[` +
		`{"door":{"id":3,"name":"Attic"},"location":"","reached":false,"works":[],"spans":[]},` +
		`{"door":{"id":1,"name":"Lobby"},"location":"Building A","thumbnail_url":"/api/v1/doors/1/pictures/1/thumbnail","reached":true,` +
		`"works":[{"start":"2026-01-05T08:00:00Z","end":"2026-01-05T18:00:00Z"}],` +
		`"spans":[{"start":"2026-01-05T22:00:00Z","end":"2026-01-06T00:00:00Z","state":"open"}]}]}`)
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("access (-want +got):\n%s", diff)
	}
}

func TestMyEntriesAreOnlyTheUsersOwn(t *testing.T) {
	t.Parallel()
	k := loggedSite(t, testsupport.NewFactory())
	ann := signedInAs(t, k.b, "2", annEmail)

	page := ann.Get("/me/entries")
	api := testsupport.JSON(send(t, asUser(t, k.b, "2"), "GET", "/api/v1/me/entries?per_page=1&page=2", "", 200).Body)

	want := testsupport.Page{
		Title: "Entries · doorcode", Heading: "Entries", Nav: portalNav("Entries"),
		Tables: map[string][][]string{"Entries": {
			{"When", "Door", "Outcome", "What"},
			{"Mon 5 Jan 2026 09:20:00", "Dock", "Denied No rule", "Code check"},
			{"Mon 5 Jan 2026 09:00:00", "Lobby", "Allowed Access rule", "Code check"},
			{"Showing 1 to 2 of 2"},
		}},
	}
	if diff := cmp.Diff([]any{200, want}, []any{page.Status, pageOf(page)}); diff != "" {
		t.Errorf("status, page (-want +got):\n%s", diff)
	}
	wantAPI := testsupport.JSON(`{"events":[{"id":1,"at":"2026-01-05T09:00:00Z","door":{"id":1,"name":"Lobby"},"type":"check","outcome":"allowed",` +
		`"reason":"access rule","user":{"id":2,"name":"Ann Smith"},"rule_id":1,"request_id":"r1"}],"total":2,"page":2,"per_page":1}`)
	if diff := cmp.Diff(wantAPI, api); diff != "" {
		t.Errorf("API (-want +got):\n%s", diff)
	}
}

func TestMyEntriesArePaged(t *testing.T) {
	t.Parallel()
	b := openedTimes(t, testsupport.NewFactory())

	first := testsupport.ParsePage(b.Get("/me/entries?page=x").Body)
	last := testsupport.ParsePage(b.Get("/me/entries?page=3").Body)
	past := pageOf(b.Get("/me/entries?page=4")).Tables["Entries"]

	got := []any{len(first.Tables["Entries"]), first.NavTargets[len(first.NavTargets)-1], len(last.Tables["Entries"]), last.NavTargets[len(last.NavTargets)-1], past}
	want := []any{27, "/me/entries?page=2", 3, "/me/entries?page=2", [][]string{{"When", "Door", "Outcome", "What"}, {"This page is past the last one."}}}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("rows and last link of the first page, of the last page, a page past the last (-want +got):\n%s", diff)
	}
}

func TestMyEntriesSayHowEntriesAppearWhenThereAreNone(t *testing.T) {
	t.Parallel()
	_, ann := portalSite(t, testsupport.NewFactory())

	got := pageOf(ann.Get("/me/entries")).Tables["Entries"]

	want := [][]string{{"When", "Door", "Outcome", "What"}, {"You have no entries yet. Each time you enter your code at a door, it is listed here with what the door decided."}}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("entries (-want +got):\n%s", diff)
	}
}

// labAccess is the body of an access request.
const labAccess = `{"title":" Lab access ","body":" I start in the lab on Monday. "}`

// labAccessJSON is the request labAccess makes, as the API shows it.
const labAccessJSON = `{"id":1,"title":"Lab access","body":"I start in the lab on Monday.","resolved":false,` +
	`"created_at":"2026-01-05T09:00:00Z","updated_at":"2026-01-05T09:00:00Z"}`

func TestARequestIsCreatedChangedAndWithdrawnWhileItIsOpen(t *testing.T) {
	t.Parallel()
	f := testsupport.NewFactory()
	ann := asUser(t, staffAnn(t, f), "2")

	created := ann.PostJSON("/api/v1/me/requests", labAccess)
	f.ClockStub.Set("2026-01-05T10:00:00Z")
	changed := ann.PutJSON("/api/v1/me/requests/1", `{"title":"Lab and store access"}`)
	listed := ann.Get("/api/v1/me/requests")
	withdrawn := ann.Delete("/api/v1/me/requests/1")
	gone := ann.Get("/api/v1/me/requests/1")

	changedJSON := `{"id":1,"title":"Lab and store access","body":"","resolved":false,"created_at":"2026-01-05T09:00:00Z","updated_at":"2026-01-05T10:00:00Z"}`
	want := []any{201, testsupport.JSON(labAccessJSON), 200, testsupport.JSON(changedJSON), testsupport.JSON(`{"requests":[` + changedJSON + `]}`), 204, 404}
	got := []any{created.Status, testsupport.JSON(created.Body), changed.Status, testsupport.JSON(changed.Body), testsupport.JSON(listed.Body), withdrawn.Status, gone.Status}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("created, changed, listed, withdrawn, gone (-want +got):\n%s", diff)
	}
}

func TestAResolvedRequestShowsTheReplyAndCanNoLongerChange(t *testing.T) {
	t.Parallel()
	f := testsupport.NewFactory()
	admin, ann := portalSite(t, f)
	send(t, ann, "POST", "/api/v1/me/requests", labAccess, 201)
	f.ClockStub.Set("2026-01-05T15:30:00Z")
	send(t, admin, "PUT", "/api/v1/requests/1/reply", `{"reply":"Granted until June."}`, 200)
	send(t, admin, "POST", "/api/v1/requests/1/resolve", "", 200)

	read := ann.Get("/api/v1/me/requests/1")
	changed := ann.PutJSON("/api/v1/me/requests/1", `{"title":"Something else"}`)
	withdrawn := ann.Delete("/api/v1/me/requests/1")
	page := ann.Get("/me/requests/1")
	saved := ann.Post("/me/requests/1", "title", "Something else")
	deleted := ann.Post("/me/requests/1/delete")
	listed := pageOf(ann.Get("/me/requests")).Tables["Your requests"]

	resolvedJSON := `{"id":1,"title":"Lab access","body":"I start in the lab on Monday.","reply":"Granted until June.","resolved":true,` +
		`"resolved_at":"2026-01-05T15:30:00Z","created_at":"2026-01-05T09:00:00Z","updated_at":"2026-01-05T09:00:00Z"}`
	refused := `{"error":"this access request is resolved and can no longer be changed"}` + "\n"
	if diff := cmp.Diff([]any{testsupport.JSON(resolvedJSON), 409, refused, 409, refused},
		[]any{testsupport.JSON(read.Body), changed.Status, changed.Body, withdrawn.Status, withdrawn.Body}); diff != "" {
		t.Errorf("API read, change, withdrawal (-want +got):\n%s", diff)
	}
	note := "This request is resolved, so it can no longer be changed or withdrawn. Make a new request if you need something else."
	shown := testsupport.Page{
		Title: "Lab access · doorcode", Heading: "Lab access", Nav: portalNav("Requests"), Notes: []string{note},
		Details: map[string]string{"Reply": "Granted until June.", "Resolved": "5 Jan 2026 15:30"},
	}
	alert := "This request is resolved, so it can no longer be changed or withdrawn. Make a new request instead."
	refusedPage := shown
	refusedPage.Messages = []string{alert}
	if diff := cmp.Diff([]any{200, shown, 409, refusedPage, 409, refusedPage},
		[]any{page.Status, pageOf(page), saved.Status, pageOf(saved), deleted.Status, pageOf(deleted)}); diff != "" {
		t.Errorf("page, saving, withdrawing (-want +got):\n%s", diff)
	}
	wantListed := [][]string{{"Request", "Sent", "Status"}, {"Lab access Reply: Granted until June.", "5 Jan 2026 09:00", "Resolved 5 Jan 2026 15:30"}}
	if diff := cmp.Diff(wantListed, listed); diff != "" {
		t.Errorf("list (-want +got):\n%s", diff)
	}
}

func TestAnotherUsersRequestIsNotFound(t *testing.T) {
	t.Parallel()
	admin, ann := portalSite(t, testsupport.NewFactory())
	send(t, admin, "POST", "/api/v1/me/requests", labAccess, 201)

	got := []any{
		ann.Get("/api/v1/me/requests/1").Status, ann.PutJSON("/api/v1/me/requests/1", labAccess).Status, ann.Delete("/api/v1/me/requests/1").Status,
		ann.Get("/me/requests/1").Status, ann.Post("/me/requests/1", "title", "Mine now").Status, ann.Post("/me/requests/1/delete").Status,
		testsupport.JSON(ann.Get("/api/v1/me/requests").Body),
	}

	if diff := cmp.Diff([]any{404, 404, 404, 404, 404, 404, testsupport.JSON(`{"requests":[]}`)}, got); diff != "" {
		t.Errorf("API read, change, withdrawal, page, saving, withdrawing, list (-want +got):\n%s", diff)
	}
}

func TestARequestIsChecked(t *testing.T) {
	t.Parallel()
	long := func(n int) string {
		s := ""
		for range n {
			s += "x"
		}
		return s
	}
	cases := map[string]struct {
		body string
		want string
	}{
		"not json":     {`{"title":1}`, `body must be a JSON object with a string \"title\"`},
		"no title":     {`{"title":"  ","body":"Please."}`, "Give the request a title."},
		"a long title": {`{"title":"` + long(121) + `"}`, "Use at most 120 characters."},
		"a long body":  {`{"title":"Lab","body":"` + long(4001) + `"}`, "Use at most 4000 characters."},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			ann := asUser(t, staffAnn(t, testsupport.NewFactory()), "2")
			send(t, ann, "POST", "/api/v1/me/requests", labAccess, 201)

			created := ann.PostJSON("/api/v1/me/requests", c.body)
			changed := ann.PutJSON("/api/v1/me/requests/1", c.body)

			want := apiError(400, c.want)
			if diff := cmp.Diff([]any{want, want}, []any{created, changed}); diff != "" {
				t.Errorf("creating, changing (-want +got):\n%s", diff)
			}
		})
	}
	edges := asUser(t, staffAnn(t, testsupport.NewFactory()), "2")
	if got := edges.PostJSON("/api/v1/me/requests", `{"title":"`+long(120)+`","body":"`+long(4000)+`"}`); got.Status != 201 {
		t.Errorf("the longest title and body answered %d, want 201", got.Status)
	}
}

func TestRequestsAreMadeChangedAndWithdrawnFromThePortal(t *testing.T) {
	t.Parallel()
	f := testsupport.NewFactory()
	_, ann := portalSite(t, f)
	empty := pageOf(ann.Get("/me/requests"))

	made := ann.Post("/me/requests", "title", "Lab access", "body", "From Monday.")
	listed := pageOf(ann.Get("/me/requests"))
	f.ClockStub.Set("2026-01-05T10:15:00Z")
	saved := ann.Post("/me/requests/1", "title", "Lab and store access", "body", "From Monday.")
	shown := pageOf(ann.Get("/me/requests/1"))
	withdrawn := ann.Post("/me/requests/1/delete")
	after := pageOf(ann.Get("/me/requests"))

	head := []string{"Request", "Sent", "Status"}
	none := [][]string{head, {"You have made no requests. Use the form to ask for what you need."}}
	form := map[string]string{"title": "", "body": ""}
	page := func(messages []string, rows [][]string) testsupport.Page {
		return testsupport.Page{Title: "Access requests · doorcode", Heading: "Access requests", Nav: portalNav("Requests"), Messages: messages,
			Values: form, Tables: map[string][][]string{"Your requests": rows}}
	}
	if diff := cmp.Diff(page(nil, none), empty); diff != "" {
		t.Errorf("before (-want +got):\n%s", diff)
	}
	if diff := cmp.Diff([]any{303, "/me/requests", page([]string{"Your request has been sent. An administrator will reply here."},
		[][]string{head, {"Lab access", "5 Jan 2026 09:00", "Waiting for an answer"}})}, []any{made.Status, made.Location, listed}); diff != "" {
		t.Errorf("made (-want +got):\n%s", diff)
	}
	wantShown := testsupport.Page{
		Title: "Lab and store access · doorcode", Heading: "Lab and store access", Nav: portalNav("Requests"),
		Messages: []string{"Your request has been saved."}, Values: map[string]string{"title": "Lab and store access", "body": "From Monday."},
	}
	if diff := cmp.Diff([]any{303, "/me/requests/1", wantShown}, []any{saved.Status, saved.Location, shown}); diff != "" {
		t.Errorf("saved (-want +got):\n%s", diff)
	}
	if diff := cmp.Diff([]any{303, "/me/requests", page([]string{"Your request has been withdrawn."}, none)}, []any{withdrawn.Status, withdrawn.Location, after}); diff != "" {
		t.Errorf("withdrawn (-want +got):\n%s", diff)
	}
}

func TestTheRequestFormsSayWhatIsWrong(t *testing.T) {
	t.Parallel()
	_, ann := portalSite(t, testsupport.NewFactory())
	send(t, ann, "POST", "/api/v1/me/requests", labAccess, 201)

	made := ann.Post("/me/requests", "title", " ", "body", "Please.")
	saved := ann.Post("/me/requests/1", "title", "", "body", "Still please.")

	if diff := cmp.Diff([]any{422, map[string]string{"title": "Give the request a title."}, map[string]string{"title": " ", "body": "Please."}},
		[]any{made.Status, pageOf(made).FieldErrors, pageOf(made).Values}); diff != "" {
		t.Errorf("making (-want +got):\n%s", diff)
	}
	if diff := cmp.Diff([]any{422, map[string]string{"title": "Give the request a title."}, map[string]string{"title": "", "body": "Still please."}},
		[]any{saved.Status, pageOf(saved).FieldErrors, pageOf(saved).Values}); diff != "" {
		t.Errorf("saving (-want +got):\n%s", diff)
	}
}

func TestMakingYourOwnAccountExpireInThePastIsRefused(t *testing.T) {
	t.Parallel()
	b := signedInAdmin(t, testsupport.NewFactory())
	createUser(t, b, ann)
	admin := `{"email":"admin@example.com","display_name":"Administrator","expires_at":"`

	now := b.PutJSON("/api/v1/users/1", admin+`2026-01-05T09:00:00Z"}`)
	past := b.PutJSON("/api/v1/users/1", admin+`2025-12-31T00:00:00Z"}`)
	page := b.Post("/admin/users/1", "email", adminEmail, "display_name", "Administrator", "expires_on", "2026-01-05")
	soon := b.PutJSON("/api/v1/users/1", admin+`2026-01-05T09:00:01Z"}`)
	others := b.PutJSON("/api/v1/users/2", `{"email":"ann@example.com","display_name":"Ann","expires_at":"2025-12-31T00:00:00Z"}`)

	refused := `{"error":"you cannot make your own account expire now or earlier"}` + "\n"
	want := []any{409, refused, 409, refused, 409,
		[]string{"You are signed in.", "You cannot make your own account expire today or earlier. Ask another administrator to end it."}, 200, 200}
	got := []any{now.Status, now.Body, past.Status, past.Body, page.Status, pageOf(page).Messages, soon.Status, others.Status}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("now, in the past, from the page, a moment later, another user's (-want +got):\n%s", diff)
	}
}

func TestAnyoneSignedInSeesThePicturesOfDoorsThePortalShows(t *testing.T) {
	t.Parallel()
	admin, ann := portalSite(t, testsupport.NewFactory())
	uploadPicture(t, admin, "1", testsupport.PNG(30, 20))
	uploadPicture(t, admin, "2", testsupport.PNG(30, 20))
	createDoor(t, admin, `{"name":"Gone","public":true}`)
	uploadPicture(t, admin, "4", testsupport.PNG(30, 20))
	send(t, admin, "DELETE", "/api/v1/doors/4", "", 204)

	got := []any{
		ann.Get("/api/v1/doors/1/pictures/1/thumbnail").Status, ann.Get("/api/v1/doors/1/pictures/1").Status,
		ann.Get("/api/v1/doors/2/pictures/2/thumbnail").Status, ann.Get("/api/v1/doors/4/pictures/3/thumbnail").Status,
		ann.Get("/api/v1/doors/9/pictures/1/thumbnail").Status, admin.Get("/api/v1/doors/2/pictures/2/thumbnail").Status,
		testsupport.JSONString(ann.Get("/api/v1/doors/2/pictures/2").Body, "/error"),
	}

	want := []any{200, 200, 403, 403, 404, 200, "the manage-doors or door-commander or manage-firmware role is required for the pictures of a door the portal does not show"}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("public thumbnail and image, private, deleted, missing, private for a door manager, refusal (-want +got):\n%s", diff)
	}
}

func TestHomeShowsTheUsersCodeDoorsRequestsAndEntries(t *testing.T) {
	t.Parallel()
	k := loggedSite(t, testsupport.NewFactory())
	send(t, k.b, "PUT", "/api/v1/doors/1", `{"name":"Lobby","public":true,"zone_ids":[1]}`, 200)
	ann := signedInAs(t, k.b, "2", annEmail)
	send(t, ann, "POST", "/api/v1/me/requests", labAccess, 201)
	send(t, ann, "POST", "/api/v1/me/requests", `{"title":"Store access"}`, 201)
	send(t, ann, "POST", "/api/v1/me/requests", `{"title":"Weekend hours"}`, 201)
	send(t, k.b, "POST", "/api/v1/requests/3/resolve", "", 200)

	got := pageOf(ann.Get("/"))

	want := testsupport.Page{
		Title: "Home · doorcode", Heading: "Welcome, Ann Smith", Nav: portalNav("Home"),
		Tables: map[string][][]string{
			"Your doors": {{"Door", "Now", "Calendar"}, {"Lobby", "Your code works now", "Calendar"}},
			"Your recent entries": {
				{"When", "Door", "Outcome"}, {"Mon 5 Jan 2026 09:20:00", "Dock", "Denied"}, {"Mon 5 Jan 2026 09:00:00", "Lobby", "Allowed"},
			},
		},
		Details:     homeCards(true, "2"),
		LinkTargets: map[string]string{"Calendar of Lobby": "/me/access/1"},
	}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("page (-want +got):\n%s", diff)
	}
}

func TestHomeAsksAUserWithoutACodeToChooseOne(t *testing.T) {
	t.Parallel()
	f := testsupport.NewFactory()
	_, ann := portalSite(t, f)
	offered := pageOf(ann.Get("/"))
	autoSignIn(f)

	hidden := pageOf(ann.Get("/"))

	note := []string{"You have no door code yet. Choose one to open the doors your groups give you."}
	withoutPassword := homeCards(false, "")
	delete(withoutPassword, "Password")
	got := []any{offered.Notes, offered.Details, hidden.Details, hidden.Nav}
	want := []any{note, homeCards(false, ""), withoutPassword, []string{"Home (current)", "Door code", "Access", "Entries", "Requests", "API tokens"}}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("notes and cards, cards and navigation under auto sign-in (-want +got):\n%s", diff)
	}
}

func TestThePortalLeadsEveryRoleHolderToTheDashboard(t *testing.T) {
	t.Parallel()
	for _, roles := range []string{
		`["manage-users"]`, `["manage-groups"]`, `["manage-schedules"]`, `["manage-doors"]`, `["door-commander"]`, `["manage-firmware"]`,
		`["view-logs"]`, `["view-logs","manage-groups"]`,
	} {
		t.Run(roles, func(t *testing.T) {
			t.Parallel()
			_, b := adminHolding(t, testsupport.NewFactory(), roles)

			got := pageOf(b.Get("/me/entries")).LinkTargets

			if diff := cmp.Diff(toAdministration, got); diff != "" {
				t.Errorf("links (-want +got):\n%s", diff)
			}
		})
	}
}

func TestThePortalAnswersWhatItCannotReadWithABadRequest(t *testing.T) {
	t.Parallel()
	_, ann := portalSite(t, testsupport.NewFactory())

	got := []any{
		ann.Get("/api/v1/me/access?from=soon").Status,
		testsupport.JSONString(ann.Get("/api/v1/me/entries?per_page=501").Body, "/error"),
		ann.Get("/api/v1/me/entries?page=0").Status,
		ann.Get("/api/v1/me/entries?per_page=500").Status,
	}

	want := []any{400, "page must be a whole number from 1, and per_page one from 1 to 500", 400, 200}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("access from no date, too many entries a page, page zero, the most a page (-want +got):\n%s", diff)
	}
}

func TestThePortalShowsARequestIDWhenTheStoreFails(t *testing.T) {
	t.Parallel()
	type request struct{ method, target string }
	get := func(target string) request { return request{"GET", target} }
	post := func(target string) request { return request{"POST", target} }
	put := func(target string) request { return request{"PUT", target} }
	cases := map[string]struct {
		fail func(s *testsupport.Store)
		request
		fields []string
		body   string
	}{
		"home without settings":                {fail: (*testsupport.Store).FailSettings, request: get("/")},
		"home without users":                   {fail: (*testsupport.Store).FailUsers, request: get("/")},
		"home without doors":                   {fail: (*testsupport.Store).FailDoors, request: get("/")},
		"home without requests":                {fail: (*testsupport.Store).FailRequests, request: get("/")},
		"home without events":                  {fail: (*testsupport.Store).FailEvents, request: get("/")},
		"code page without settings":           {fail: (*testsupport.Store).FailSettings, request: get("/me/code")},
		"code page without users":              {fail: (*testsupport.Store).FailUsers, request: get("/me/code")},
		"code without user writes":             {fail: (*testsupport.Store).FailUserWrites, request: post("/me/code"), fields: []string{"digits", "4821"}},
		"password page without settings":       {fail: (*testsupport.Store).FailSettings, request: get("/me/password")},
		"password page without users":          {fail: (*testsupport.Store).FailUsers, request: get("/me/password")},
		"password without user writes":         {fail: (*testsupport.Store).FailUserWrites, request: post("/me/password"), fields: []string{"current_password", portalPassword, "new_password", "a new and longer one", "confirm_password", "a new and longer one"}},
		"access without settings":              {fail: (*testsupport.Store).FailSettings, request: get("/me/access")},
		"access without users":                 {fail: (*testsupport.Store).FailUsers, request: get("/me/access")},
		"access without doors":                 {fail: (*testsupport.Store).FailDoors, request: get("/me/access")},
		"a door's access without settings":     {fail: (*testsupport.Store).FailSettings, request: get("/me/access/1")},
		"a door's access without users":        {fail: (*testsupport.Store).FailUsers, request: get("/me/access/1")},
		"a door's access without schedules":    {fail: (*testsupport.Store).FailSchedules, request: get("/me/access/1")},
		"a door's access without overrides":    {fail: (*testsupport.Store).FailOverrides, request: get("/me/access/1")},
		"entries without settings":             {fail: (*testsupport.Store).FailSettings, request: get("/me/entries")},
		"entries without events":               {fail: (*testsupport.Store).FailEvents, request: get("/me/entries")},
		"requests without settings":            {fail: (*testsupport.Store).FailSettings, request: get("/me/requests")},
		"requests without requests":            {fail: (*testsupport.Store).FailRequests, request: get("/me/requests")},
		"a request made without writes":        {fail: (*testsupport.Store).FailRequestWrites, request: post("/me/requests"), fields: []string{"title", "Lab"}},
		"a request page without settings":      {fail: (*testsupport.Store).FailSettings, request: get("/me/requests/1")},
		"a request page without requests":      {fail: (*testsupport.Store).FailRequests, request: get("/me/requests/1")},
		"a request saved without writes":       {fail: (*testsupport.Store).FailRequestWrites, request: post("/me/requests/1"), fields: []string{"title", "Lab"}},
		"a request withdrawn without writes":   {fail: (*testsupport.Store).FailRequestWrites, request: post("/me/requests/1/delete")},
		"the code API without users":           {fail: (*testsupport.Store).FailUsers, request: get("/api/v1/me/code")},
		"choosing a code without users":        {fail: (*testsupport.Store).FailUsers, request: put("/api/v1/me/code"), body: `{"digits":"4821"}`},
		"choosing a password without settings": {fail: (*testsupport.Store).FailSettings, request: put("/api/v1/me/password"), body: `{"new_password":"a new and longer one"}`},
		"choosing a password without users":    {fail: (*testsupport.Store).FailUsers, request: put("/api/v1/me/password"), body: `{"new_password":"a new and longer one"}`},
		"the access API without settings":      {fail: (*testsupport.Store).FailSettings, request: get("/api/v1/me/access")},
		"the access API without users":         {fail: (*testsupport.Store).FailUsers, request: get("/api/v1/me/access")},
		"the access API without doors":         {fail: (*testsupport.Store).FailDoors, request: get("/api/v1/me/access")},
		"the entries API without events":       {fail: (*testsupport.Store).FailEvents, request: get("/api/v1/me/entries")},
		"the requests API without requests":    {fail: (*testsupport.Store).FailRequests, request: get("/api/v1/me/requests")},
		"making a request without writes":      {fail: (*testsupport.Store).FailRequestWrites, request: request{"POST", "/api/v1/me/requests"}, body: labAccess},
		"reading a request without requests":   {fail: (*testsupport.Store).FailRequests, request: get("/api/v1/me/requests/1")},
		"changing a request without writes":    {fail: (*testsupport.Store).FailRequestWrites, request: put("/api/v1/me/requests/1"), body: labAccess},
		"withdrawing a request without writes": {fail: (*testsupport.Store).FailRequestWrites, request: request{"DELETE", "/api/v1/me/requests/1"}},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			f := testsupport.NewFactory()
			_, ann := portalSite(t, f)
			send(t, ann, "POST", "/api/v1/me/requests", labAccess, 201)
			c.fail(f.StoreStub)

			var got testsupport.Response
			if c.method == "POST" && c.body == "" {
				got = ann.Post(c.target, c.fields...)
			} else {
				got = ann.Send(c.method, c.target, c.body)
			}

			if diff := cmp.Diff([]any{500, "request failed"}, []any{got.Status, lastLog(f).Message}); diff != "" {
				t.Errorf("status, logged (-want +got):\n%s", diff)
			}
		})
	}
}

func TestCalendarKeysNameWhoseCodeTheyShow(t *testing.T) {
	t.Parallel()
	admin, ann := portalSite(t, testsupport.NewFactory())

	got := []any{
		testsupport.ParsePage(ann.Get("/me/access/1").Body).Lists["Calendar key"],
		testsupport.ParsePage(ann.Get("/me/access").Body).Lists["Calendar key"],
		testsupport.ParsePage(admin.Get("/admin/users/2/doors/1/calendar").Body).Lists["Calendar key"],
	}

	want := []any{
		[]string{"Your code works", "Open mode: any keypress unlocks", "Locked: no code opens it"},
		[]string{"Open mode: any keypress unlocks", "Your code works", "Locked: no code opens it"},
		[]string{"Their code works", "Open mode: any keypress unlocks", "Locked: no code opens it"},
	}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("door calendar, access list, user's access calendar keys (-want +got):\n%s", diff)
	}
}
