package entrypoint_test

import (
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/dakriy/doorcode/internal/app"
	"github.com/dakriy/doorcode/internal/testsupport"
)

// meshSite is a broker site with a mesh, the connection of each of its
// controllers by hardware id.
type meshSite struct {
	a     *app.App
	b     *testsupport.Browser
	conns map[string]*testsupport.Connection
}

// newMeshSite is a meshSite whose controllers A1 to E5 are enrolled as
// Main entrance (1), Staff entrance (2), Server room (3), Workshop (4),
// and Loading dock (5), connected, and have reported that Main and Staff
// entrance reach the access point, Server room hops through Staff
// entrance, Workshop through Main entrance, and Loading dock through
// Server room, but for the parents parents changes, by hardware id.
func newMeshSite(t *testing.T, parents map[string]string) meshSite {
	t.Helper()
	f := testsupport.NewFactory()
	a, b := brokerSite(t, f)
	s := meshSite{a: a, b: b, conns: map[string]*testsupport.Connection{}}
	reported := map[string]string{"A1": "", "B2": "", "C3": "B2", "D4": "A1", "E5": "C3"}
	for hwid, parent := range parents {
		reported[hwid] = parent
	}
	for i, door := range []string{"Main entrance", "Staff entrance", "Server room", "Workshop", "Loading dock"} {
		createDoor(t, b, `{"name":"`+door+`"}`)
		hwid := []string{"A1", "B2", "C3", "D4", "E5"}[i]
		s.conns[hwid] = enroll(t, f, a, b, hwid, testsupport.Itoa(i+1))
	}
	for i, hwid := range []string{"A1", "B2", "C3", "D4", "E5"} {
		f.BrokerStub.Inject(doorTopic(testsupport.Itoa(i+1), "telemetry"), reportOf(map[string]any{
			"mesh_layer": layerOf(hwid, reported), "mesh_parent": reported[hwid], "mesh_children": childrenOf(hwid, reported),
		}))
	}
	run(t, a)
	return s
}

// layerOf is how many controllers hwid hops through, as parents say,
// stopping at a parent no controller has or one met before.
func layerOf(hwid string, parents map[string]string) int {
	layer := 0
	for parent, seen := parents[hwid], map[string]bool{hwid: true}; parent != "" && !seen[parent]; parent = parents[parent] {
		seen[parent] = true
		layer++
	}
	return layer
}

// childrenOf is how many controllers hop through hwid, as parents say.
func childrenOf(hwid string, parents map[string]string) int {
	children := 0
	for _, parent := range parents {
		if parent == hwid {
			children++
		}
	}
	return children
}

// meshPage is the mesh page as the administrator sees it.
func (s meshSite) meshPage(t *testing.T) testsupport.Page {
	t.Helper()
	return doorPage(t, s.b, "/admin/mesh")
}

// titlesOf is the title of each mark chart draws, in order.
func titlesOf(chart testsupport.Chart) []string {
	out := []string{}
	for _, m := range chart.Marks {
		out = append(out, m.Title)
	}
	return out
}

// controllersHead heads the mesh page's table of controllers.
var controllersHead = []string{"Door", "Status", "Layer", "Hops through", "Relay for", "Path to the access point"}

func TestTheMeshPageDrawsEachControllerByLayerWithAnEdgeToWhatItHopsThrough(t *testing.T) {
	t.Parallel()
	s := newMeshSite(t, nil)

	page := s.meshPage(t)

	chart := page.Charts["Mesh by layer"]
	got := []any{testsupport.Chart{Lines: chart.Lines, Series: chart.Series, Labels: chart.Labels}, page.Drawings["Mesh by layer"]}
	want := []any{
		testsupport.Chart{
			Lines:  []string{"164,72 256,32", "348,72 256,32", "164,168 164,120", "348,168 348,120", "256,264 348,216"},
			Series: []string{"mesh-edge", "mesh-edge", "mesh-edge", "mesh-edge", "mesh-edge"},
			Labels: []string{
				"Layer 0", "Layer 1", "Layer 2", "Access point",
				"Main entrance", "Relay for 1", "Staff entrance", "Relay for 2", "Workshop", "Server room", "Relay for 1", "Loading dock",
			},
		},
		testsupport.Drawing{
			ViewBox: "0 0 440 320", Style: "max-width: 440px; min-width: 293px",
			Texts: []string{"0,100", "0,196", "0,292", "172,20", "12,20", "12,37", "12,20", "12,37", "12,29", "12,20", "12,37", "12,29"},
		},
	}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("drawing (-want +got):\n%s", diff)
	}
}

func TestTheMeshPageOutlinesRelaysAndPlacesEachNodeUnderWhatItHopsThrough(t *testing.T) {
	t.Parallel()
	s := newMeshSite(t, nil)

	got := s.meshPage(t).Charts["Mesh by layer"].Marks

	node := func(x, y, class, title string) testsupport.Mark {
		return testsupport.Mark{X: x, Y: y, Width: "168", Height: "48", Class: class, Title: title}
	}
	want := []testsupport.Mark{
		{X: "172", Y: "0", Width: "168", Height: "32", Class: "mesh-point", Title: "The building's access point"},
		node("80", "72", "mesh-node mesh-relay", "Main entrance: layer 0, via Access point, relay for 1 door"),
		node("264", "72", "mesh-node mesh-relay", "Staff entrance: layer 0, via Access point, relay for 2 doors"),
		node("80", "168", "mesh-node", "Workshop: layer 1, via Main entrance"),
		node("264", "168", "mesh-node mesh-relay", "Server room: layer 1, via Staff entrance, relay for 1 door"),
		node("172", "264", "mesh-node", "Loading dock: layer 2, via Server room"),
	}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("nodes (-want +got):\n%s", diff)
	}
}

func TestTheMeshPageListsEachControllersPathToTheAccessPoint(t *testing.T) {
	t.Parallel()
	s := newMeshSite(t, nil)

	got := s.meshPage(t).Tables["Controllers"]

	want := [][]string{
		controllersHead,
		{"Main entrance", "Online", "0", "Access point", "1 door", "Main entrance → Access point"},
		{"Staff entrance", "Online", "0", "Access point", "2 doors", "Staff entrance → Access point"},
		{"Workshop", "Online", "1", "Main entrance", "None", "Workshop → Main entrance → Access point"},
		{"Server room", "Online", "1", "Staff entrance", "1 door", "Server room → Staff entrance → Access point"},
		{"Loading dock", "Online", "2", "Server room", "None", "Loading dock → Server room → Staff entrance → Access point"},
	}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("controllers (-want +got):\n%s", diff)
	}
}

func TestTheMeshPageMarksAControllerThatHopsThroughOneNoDoorHas(t *testing.T) {
	t.Parallel()
	s := newMeshSite(t, map[string]string{"E5": "FF09"})

	page := s.meshPage(t)

	chart := page.Charts["Mesh by layer"]
	got := []any{chart.Lines, chart.Marks[5], chart.Labels[8:], page.Tables["Controllers"][5]}
	want := []any{
		[]string{"256,72 348,32", "440,72 348,32", "164,168 256,120", "348,168 440,120"},
		testsupport.Mark{X: "448", Y: "168", Width: "168", Height: "48", Class: "mesh-node mesh-lost", Title: "Loading dock: layer 1, via FF09 (no door)"},
		[]string{"Server room", "Loading dock", "Parent unknown"},
		[]string{"Loading dock", "Online", "1", "FF09 (no door)", "None", "Loading dock → FF09 → Unknown"},
	}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("edges, node, labels, row (-want +got):\n%s", diff)
	}
}

func TestTheMeshPageMarksAControllerWhoseParentIsOfflineAndDimsTheParent(t *testing.T) {
	t.Parallel()
	s := newMeshSite(t, nil)
	s.conns["C3"].Drop()
	run(t, s.a)

	page := s.meshPage(t)

	chart := page.Charts["Mesh by layer"]
	got := []any{chart.Series[4], titlesOf(chart)[4:], chart.Marks[5].Class, chart.Labels[9:], page.Tables["Controllers"][4:]}
	want := []any{
		"mesh-edge mesh-lost",
		[]string{"Server room: layer 1, via Staff entrance, offline, relay for 1 door", "Loading dock: layer 2, via Server room (offline)"},
		"mesh-node mesh-lost",
		[]string{"Server room", "Offline · Relay for 1", "Loading dock", "Parent unknown"},
		[][]string{
			{"Server room", "Offline", "1", "Staff entrance", "1 door", "Server room → Staff entrance → Access point"},
			{"Loading dock", "Online", "2", "Server room (offline)", "None", "Loading dock → Server room → Staff entrance → Access point"},
		},
	}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("edge, nodes, class, labels, rows (-want +got):\n%s", diff)
	}
}

func TestTheMeshPageSaysWhenNothingMeshes(t *testing.T) {
	t.Parallel()
	k := keypadSite(t, testsupport.NewFactory())
	k.report(map[string]any{"mesh_children": 2})

	page := doorPage(t, k.b, "/admin/mesh")

	want := []any{
		"Mesh · doorcode", mainNav("Doors: Mesh", ""),
		[]string{"Nothing meshes Every enrolled controller reaches the building's access point itself. A door appears here once its controller reports hopping through another."},
	}
	if diff := cmp.Diff(want, []any{page.Title, page.Nav, page.Notes}); diff != "" {
		t.Errorf("title, navigation, notes (-want +got):\n%s", diff)
	}
}

func TestTheMeshAPIGivesEachControllerAndAnEdgeToWhatItHopsThrough(t *testing.T) {
	t.Parallel()
	s := newMeshSite(t, map[string]string{"E5": "FF09"})
	s.conns["B2"].Drop()
	run(t, s.a)

	got := testsupport.JSON(send(t, s.b, "GET", "/api/v1/mesh", "", 200).Body)

	want := testsupport.JSON(`{"nodes":[
		{"door_id":5,"name":"Loading dock","hardware_id":"E5","layer":1,"parent":"FF09","children":0,"online":true,
			"parent_unknown":true,"dependents":0,"path":["FF09"],"reaches_access_point":false},
		{"door_id":1,"name":"Main entrance","hardware_id":"A1","layer":0,"parent":"","children":1,"online":true,
			"parent_unknown":false,"dependents":1,"path":[],"reaches_access_point":true},
		{"door_id":3,"name":"Server room","hardware_id":"C3","layer":1,"parent":"B2","children":0,"online":true,
			"parent_unknown":true,"dependents":0,"path":["B2"],"reaches_access_point":true},
		{"door_id":2,"name":"Staff entrance","hardware_id":"B2","layer":0,"parent":"","children":1,"online":false,
			"parent_unknown":false,"dependents":1,"path":[],"reaches_access_point":true},
		{"door_id":4,"name":"Workshop","hardware_id":"D4","layer":1,"parent":"A1","children":0,"online":true,
			"parent_unknown":false,"dependents":0,"path":["A1"],"reaches_access_point":true}
	],"edges":[{"from":3,"to":2},{"from":4,"to":1}]}`)
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("mesh (-want +got):\n%s", diff)
	}
}

func TestTheMeshEndsAPathThatComesBackOnItself(t *testing.T) {
	t.Parallel()
	s := newMeshSite(t, map[string]string{"A1": "D4"})

	got := []any{
		testsupport.JSONAt(send(t, s.b, "GET", "/api/v1/mesh", "", 200).Body, "/nodes/4"),
		s.meshPage(t).Tables["Controllers"][4],
	}

	want := []any{
		testsupport.JSON(`{"door_id":4,"name":"Workshop","hardware_id":"D4","layer":1,"parent":"A1","children":1,"online":true,
			"parent_unknown":false,"dependents":1,"path":["A1"],"reaches_access_point":false}`),
		[]string{"Workshop", "Online", "1", "Main entrance", "1 door", "Workshop → Main entrance → Unknown"},
	}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("node, row (-want +got):\n%s", diff)
	}
}

func TestTheMeshLeavesOutDeletedDoors(t *testing.T) {
	t.Parallel()
	s := newMeshSite(t, nil)
	send(t, s.b, "DELETE", "/api/v1/doors/4", "", 204)

	got := []any{}
	for _, node := range testsupport.JSONAt(send(t, s.b, "GET", "/api/v1/mesh", "", 200).Body, "/nodes").([]any) {
		got = append(got, node.(map[string]any)["name"])
	}

	want := []any{"Loading dock", "Main entrance", "Server room", "Staff entrance"}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("nodes (-want +got):\n%s", diff)
	}
}
