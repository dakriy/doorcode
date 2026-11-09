package entrypoint_test

import (
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/dakriy/doorcode/internal/testsupport"
)

// worksAt reports whether the user access API, over the stub clock's
// Monday, says the code of the user with id user opens Lobby (1) at at, an
// RFC 3339 time in UTC.
func worksAt(t *testing.T, b *testsupport.Browser, user, at string) bool {
	t.Helper()
	body := send(t, b, "GET", "/api/v1/users/"+user+"/access?from=2026-01-05&to=2026-01-05", "", 200).Body
	doors, _ := testsupport.JSONAt(body, "/doors").([]any)
	for _, d := range doors {
		access, _ := d.(map[string]any)
		door, _ := access["door"].(map[string]any)
		if door["id"] != float64(1) {
			continue
		}
		works, _ := access["works"].([]any)
		for _, w := range works {
			in, _ := w.(map[string]any)
			start, _ := in["start"].(string)
			end, _ := in["end"].(string)
			if start <= at && at < end {
				return true
			}
		}
	}
	return false
}

func TestTheKeypadAndEveryViewOfWhenACodeWorksAgreeAtEachInstant(t *testing.T) {
	t.Parallel()
	f := testsupport.NewFactory()
	k := keypadSite(t, f)
	f.StoreStub.ExpireUser("eve@example.com", "2026-01-05T10:00:00Z")
	createSchedule(t, k.b, `{"name":"Evenings","windows":[{"days":["mon"],"start":"20:00","end":"21:00"}]}`)
	send(t, k.b, "PUT", "/api/v1/doors/1", `{"name":"Lobby","public":true,"zone_ids":[1]}`, 200)
	send(t, k.b, "PUT", "/api/v1/zones/1", `{"name":"North","door_ids":[1],"open_schedule_ids":[4]}`, 200)
	createRule(t, k.b, `{"group_id":2,"schedule_id":2,"zone_ids":[1]}`)
	createOverride(t, k.b, "1", `{"kind":"locked","start":"2026-01-05T12:00:00Z","end":"2026-01-05T13:00:00Z"}`)
	createOverride(t, k.b, "1", `{"kind":"open","start":"2026-01-05T19:00:00Z","end":"2026-01-05T20:00:00Z"}`)
	portal := map[string]*testsupport.Browser{"Ann": signedInAs(t, k.b, "2", annEmail), "Cid": signedInAs(t, k.b, "3", "cid@example.com")}
	people := []struct{ name, id, code string }{{"Ann", "2", "111111"}, {"Cid", "3", "222222"}, {"Eve", "4", "333333"}}

	got := map[string][]string{}
	for _, at := range []string{"07:00", "09:00", "12:30", "19:30", "20:30"} {
		instant := "2026-01-05T" + at + ":00Z"
		k.at(instant)
		for _, p := range people {
			k.press(p.code)
			checked := send(t, k.b, "GET", "/api/v1/events", "", 200).Body
			said := at + " keypad " + testsupport.JSONString(checked, "/events/0/outcome") + " " + testsupport.JSONString(checked, "/events/0/reason")
			if worksAt(t, k.b, p.id, instant) {
				said += ", code works"
			} else {
				said += ", code does not work"
			}
			if b, signedIn := portal[p.name]; signedIn {
				said += ", portal: " + pageOf(b.Get("/me/access")).Tables["Your doors"][1][1]
			}
			got[p.name] = append(got[p.name], said)
		}
	}

	want := map[string][]string{
		"Ann": {
			"07:00 keypad denied no rule, code does not work, portal: Your code works at set times",
			"09:00 keypad allowed access rule, code works, portal: Your code works now",
			"12:30 keypad denied locked, code does not work, portal: Your code works at set times",
			"19:30 keypad allowed open override, code works, portal: Open now",
			"20:30 keypad allowed open mode, code works, portal: Open now",
		},
		"Cid": {
			"07:00 keypad allowed door commander, code works, portal: Your code works now",
			"09:00 keypad allowed door commander, code works, portal: Your code works now",
			"12:30 keypad allowed door commander, code works, portal: Your code works now",
			"19:30 keypad allowed door commander, code works, portal: Open now",
			"20:30 keypad allowed door commander, code works, portal: Open now",
		},
		"Eve": {
			"07:00 keypad denied no rule, code does not work",
			"09:00 keypad allowed access rule, code works",
			"12:30 keypad denied expired, code does not work",
			"19:30 keypad denied expired, code does not work",
			"20:30 keypad denied expired, code does not work",
		},
	}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("what the keypad, the user access API, and the portal say (-want +got):\n%s", diff)
	}
}
