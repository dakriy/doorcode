package entrypoint_test

import (
	"testing"

	"github.com/dakriy/doorcode/internal/testsupport"
)

// send sends a request with a JSON body through c and fails the test unless
// it answers with want.
func send(t *testing.T, c *testsupport.Browser, method, target, body string, want int) testsupport.Response {
	t.Helper()
	got := c.Send(method, target, body)
	if got.Status != want {
		t.Fatalf("%s %s answered %d, want %d:\n%s", method, target, got.Status, want, got.Body)
	}
	return got
}

// createUser creates a user through the API as c's user. The first user
// created after startup has id 2.
func createUser(t *testing.T, c *testsupport.Browser, body string) {
	t.Helper()
	send(t, c, "POST", "/api/v1/users", body, 201)
}

// createGroup creates a group through the API as c's user. The first group
// created after startup has id 2.
func createGroup(t *testing.T, c *testsupport.Browser, body string) {
	t.Helper()
	send(t, c, "POST", "/api/v1/groups", body, 201)
}

// join makes the user with id user a member of the group with id group
// through the API as c's user.
func join(t *testing.T, c *testsupport.Browser, group, user string) {
	t.Helper()
	send(t, c, "PUT", "/api/v1/groups/"+group+"/members/"+user, "", 204)
}

// adminHolding returns a browser signed in as the first administrator,
// whose only group now carries roles, a JSON array.
func adminHolding(t *testing.T, f *testsupport.Factory, roles string) (testsupport.Handler, *testsupport.Browser) {
	t.Helper()
	h, b := adminSession(t, f)
	createGroup(t, b, `{"slug":"staff","name":"Staff","roles":`+roles+`}`)
	join(t, b, "2", "1")
	f.StoreStub.LeaveGroup(adminEmail, "administrators")
	return h, b
}

// ann is the body of a user created for a test.
const ann = `{"email":"ann@example.com","first_name":"Ann","last_name":"Smith","user_id":"100234"}`
