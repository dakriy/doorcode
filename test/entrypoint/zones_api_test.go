package entrypoint_test

import (
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/dakriy/doorcode/internal/testsupport"
)

// zonedDoors is a browser signed in as the administrator of a site with
// the doors Front door (1) and Back door (2).
func zonedDoors(t *testing.T, f *testsupport.Factory) *testsupport.Browser {
	t.Helper()
	b := signedInAdmin(t, f)
	createDoor(t, b, frontDoor)
	createDoor(t, b, `{"name":"Back door"}`)
	return b
}

func TestCreatingAZoneAnswersWithItTidied(t *testing.T) {
	b := zonedDoors(t, testsupport.NewFactory())

	got := b.PostJSON("/api/v1/zones", `{"name":" Ground floor ","description":" Lobby level. ","door_ids":[1,2,1]}`)

	want := testsupport.Response{Status: 201, ContentType: jsonType, Body: `{"id":1,"name":"Ground floor","description":"Lobby level.",` +
		`"doors":[{"id":2,"name":"Back door"},{"id":1,"name":"Front door"}]}` + "\n"}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("response (-want +got):\n%s", diff)
	}
}

func TestZonesAreListedByNameAndNamedOnTheirDoors(t *testing.T) {
	b := zonedDoors(t, testsupport.NewFactory())
	createZone(t, b, `{"name":"west","door_ids":[1]}`)
	createZone(t, b, `{"name":"East","door_ids":[1,2]}`)

	zones := testsupport.JSON(b.Get("/api/v1/zones").Body)
	door := testsupport.JSONAt(b.Get("/api/v1/doors/1").Body, "/zones")

	want := []any{
		testsupport.JSON(`{"zones":[{"id":2,"name":"East","description":"","doors":[{"id":2,"name":"Back door"},{"id":1,"name":"Front door"}]},` +
			`{"id":1,"name":"west","description":"","doors":[{"id":1,"name":"Front door"}]}]}`),
		testsupport.JSON(`[{"id":2,"name":"East"},{"id":1,"name":"west"}]`),
	}
	if diff := cmp.Diff(want, []any{zones, door}); diff != "" {
		t.Errorf("zones, the front door's zones (-want +got):\n%s", diff)
	}
}

func TestUpdatingAZoneReplacesItsDoors(t *testing.T) {
	b := zonedDoors(t, testsupport.NewFactory())
	createZone(t, b, `{"name":"East","description":"Old","door_ids":[1]}`)

	updated := b.PutJSON("/api/v1/zones/1", `{"name":"east","door_ids":[2]}`)
	got := b.Get("/api/v1/zones/1")

	want := `{"id":1,"name":"east","description":"","doors":[{"id":2,"name":"Back door"}]}` + "\n"
	if diff := cmp.Diff([]any{200, want, want}, []any{updated.Status, updated.Body, got.Body}); diff != "" {
		t.Errorf("status, update, read back (-want +got):\n%s", diff)
	}
}

func TestDeletingAZoneKeepsItsDoors(t *testing.T) {
	b := zonedDoors(t, testsupport.NewFactory())
	createZone(t, b, `{"name":"East","door_ids":[1]}`)

	deleted := b.Delete("/api/v1/zones/1")
	zone := b.Get("/api/v1/zones/1")
	door := b.Get("/api/v1/doors/1")

	want := []any{204, 404, `{"error":"no zone has this id"}` + "\n", frontDoorJSON + "\n"}
	if diff := cmp.Diff(want, []any{deleted.Status, zone.Status, zone.Body, door.Body}); diff != "" {
		t.Errorf("delete, zone, door (-want +got):\n%s", diff)
	}
}

func TestZoneFieldsAreChecked(t *testing.T) {
	cases := map[string]struct {
		body string
		want string
	}{
		"not json":           {`{"door_ids":"all"}`, `body must be a JSON object with a string \"name\"`},
		"no name":            {`{"name":""}`, "Enter a name."},
		"a long name":        {`{"name":"` + repeat("x", 101) + `"}`, "Use at most 100 characters."},
		"a taken name":       {`{"name":"EAST"}`, "Another zone has this name."},
		"a long description": {`{"name":"A","description":"` + repeat("x", 501) + `"}`, "Use at most 500 characters."},
		"an unknown door":    {`{"name":"A","door_ids":[1,3]}`, "No door has id 3."},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			b := zonedDoors(t, testsupport.NewFactory())
			createZone(t, b, `{"name":"East"}`)

			got := b.PostJSON("/api/v1/zones", c.body)

			want := testsupport.Response{Status: 400, ContentType: jsonType, Body: `{"error":"` + c.want + `"}` + "\n"}
			if diff := cmp.Diff(want, got); diff != "" {
				t.Errorf("response (-want +got):\n%s", diff)
			}
		})
	}
}

func TestZoneAtTheLimitsIsAcceptedAndKeepsItsOwnName(t *testing.T) {
	b := signedInAdmin(t, testsupport.NewFactory())
	createZone(t, b, `{"name":"`+repeat("x", 100)+`","description":"`+repeat("y", 500)+`"}`)

	got := b.PutJSON("/api/v1/zones/1", `{"name":"`+repeat("X", 100)+`"}`)

	if diff := cmp.Diff(200, got.Status); diff != "" {
		t.Errorf("status (-want +got):\n%s\n%s", diff, got.Body)
	}
}

func TestChangingAZoneThatIsNotThereIsRefused(t *testing.T) {
	cases := map[string]func(b *testsupport.Browser) testsupport.Response{
		"read":   func(b *testsupport.Browser) testsupport.Response { return b.Get("/api/v1/zones/9") },
		"update": func(b *testsupport.Browser) testsupport.Response { return b.PutJSON("/api/v1/zones/9", `{"name":""}`) },
		"delete": func(b *testsupport.Browser) testsupport.Response { return b.Delete("/api/v1/zones/9") },
	}
	for name, request := range cases {
		t.Run(name, func(t *testing.T) {
			got := request(signedInAdmin(t, testsupport.NewFactory()))

			want := testsupport.Response{Status: 404, ContentType: jsonType, Body: `{"error":"no zone has this id"}` + "\n"}
			if diff := cmp.Diff(want, got); diff != "" {
				t.Errorf("response (-want +got):\n%s", diff)
			}
		})
	}
}

func TestUpdatingAZoneChecksItsFields(t *testing.T) {
	cases := map[string]struct {
		body string
		want string
	}{
		"not json":     {`{`, `body must be a JSON object with a string \"name\"`},
		"a taken name": {`{"name":"west"}`, "Another zone has this name."},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			b := signedInAdmin(t, testsupport.NewFactory())
			createZone(t, b, `{"name":"East"}`)
			createZone(t, b, `{"name":"West"}`)

			got := b.PutJSON("/api/v1/zones/1", c.body)

			want := testsupport.Response{Status: 400, ContentType: jsonType, Body: `{"error":"` + c.want + `"}` + "\n"}
			if diff := cmp.Diff(want, got); diff != "" {
				t.Errorf("response (-want +got):\n%s", diff)
			}
		})
	}
}

func TestZonesAnswerWithARequestIDWhenTheStoreFails(t *testing.T) {
	cases := map[string]struct {
		fail   func(s *testsupport.Store)
		method string
		target string
		body   string
	}{
		"listing zones":           {(*testsupport.Store).FailZones, "GET", "/api/v1/zones", ""},
		"naming listed doors":     {(*testsupport.Store).FailDoors, "GET", "/api/v1/zones", ""},
		"naming a zone's doors":   {(*testsupport.Store).FailDoors, "GET", "/api/v1/zones/1", ""},
		"checking a zone's doors": {(*testsupport.Store).FailDoors, "POST", "/api/v1/zones", `{"name":"West"}`},
		"creating a zone":         {(*testsupport.Store).FailZoneWrites, "POST", "/api/v1/zones", `{"name":"West"}`},
		"reading a zone":          {(*testsupport.Store).FailZones, "PUT", "/api/v1/zones/1", `{"name":"West"}`},
		"updating a zone":         {(*testsupport.Store).FailZoneWrites, "PUT", "/api/v1/zones/1", `{"name":"West"}`},
		"deleting a zone":         {(*testsupport.Store).FailZoneWrites, "DELETE", "/api/v1/zones/1", ""},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			f := testsupport.NewFactory()
			b := signedInAdmin(t, f)
			createZone(t, b, `{"name":"East"}`)
			c.fail(f.StoreStub)

			got := b.Send(c.method, c.target, c.body)

			want := testsupport.Response{Status: 500, ContentType: jsonType, Body: `{"error":"internal error","request_id":"4243444546474849"}` + "\n"}
			if diff := cmp.Diff(want, got); diff != "" {
				t.Errorf("response (-want +got):\n%s", diff)
			}
		})
	}
}
