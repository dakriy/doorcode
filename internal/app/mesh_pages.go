package app

import (
	"cmp"
	"math"
	"net/http"
	"slices"
	"strconv"
	"strings"

	"github.com/dakriy/doorcode/internal/auth"
	"github.com/dakriy/doorcode/internal/telemetry"
)

// The geometry of the mesh drawing, in the units of its view box: the
// gutter that names the layers, each column's width, a node's size, the
// access point's height, where the first layer starts, and how far apart
// layers sit.
const (
	meshGutter      = 72
	meshColumn      = 184
	meshNodeWidth   = 168
	meshNodeHeight  = 48
	meshPointHeight = 32
	meshFirstLayer  = 72
	meshLayerHeight = 96
)

// meshContent is the mesh page's content: the drawing and the table of
// controllers, or what the page says when nothing meshes.
type meshContent struct {
	Drawing *meshDrawing
	Rows    []meshRow
}

// meshDrawing is the mesh drawn by layer: its size, the access point,
// each layer's label, each controller, and an edge from each to what it
// hops through.
type meshDrawing struct {
	Width, Height int
	// MinWidth is the narrowest the drawing is shown, so its text stays
	// readable on a phone, which scrolls it instead.
	MinWidth    int
	AccessPoint meshBox
	Layers      []meshLabel
	Nodes       []meshBox
	Edges       []meshEdge
}

// meshLabel is a layer's label and where it sits.
type meshLabel struct {
	Y    int
	Text string
}

// meshBox is a node of the drawing: where it sits, the door page it
// links to, its name, what else it says (offline, parent unknown, a
// relay), the title a pointer over it shows, the class that outlines it
// as a relay or as hopping through what is unknown or offline, and
// whether it is dimmed for being offline.
type meshBox struct {
	X, Y       int
	Href       string
	Name, Note string
	Title      string
	Class      string
	Offline    bool
}

// meshEdge is a line from a node up to what it hops through, and the
// class that dashes it when that is offline.
type meshEdge struct {
	Points string
	Class  string
}

// meshRow is one controller as the mesh page's table lists it.
type meshRow struct {
	Href     string
	Name     string
	Status   string
	Layer    int
	Through  string
	RelayFor string
	Path     string
}

func (a *App) meshPage(w http.ResponseWriter, r *http.Request, viewer auth.User) {
	doors, err := a.doors.Doors(r.Context())
	if err != nil {
		a.pageError(w, r, err)
		return
	}
	nodes := telemetry.Graph(doors)
	content := meshContent{}
	if slices.ContainsFunc(nodes, func(n telemetry.Node) bool { return n.Mesh.Layer > 0 }) {
		content.Drawing, content.Rows = drawMesh(nodes)
	}
	a.render(w, r, http.StatusOK, meshTemplate, "Mesh", &viewer, content)
}

// drawMesh lays nodes out by layer, the access point at the top and each
// layer's nodes in the order of what they hop through, and lists them in
// that order.
func drawMesh(nodes []telemetry.Node) (*meshDrawing, []meshRow) {
	names := map[string]string{}
	deepest, widest := 0, 1
	for _, n := range nodes {
		names[n.HardwareID] = n.Name
		deepest = max(deepest, n.Mesh.Layer)
	}
	layers := make([][]telemetry.Node, deepest+1)
	for _, n := range nodes {
		layers[n.Mesh.Layer] = append(layers[n.Mesh.Layer], n)
	}
	for _, layer := range layers {
		widest = max(widest, len(layer))
	}
	d := &meshDrawing{Width: meshGutter + widest*meshColumn, Height: meshFirstLayer + deepest*meshLayerHeight + meshNodeHeight + 8}
	d.MinWidth = d.Width * 2 / 3
	d.AccessPoint = meshBox{X: meshGutter + (widest*meshColumn-meshNodeWidth)/2, Name: "Access point", Title: "The building's access point", Class: "mesh-point"}
	placed := map[int64]meshBox{}
	drawn := []telemetry.Node{}
	rows := []meshRow{}
	for depth, layer := range layers {
		y := meshFirstLayer + depth*meshLayerHeight
		d.Layers = append(d.Layers, meshLabel{Y: y + meshNodeHeight/2 + 4, Text: "Layer " + strconv.Itoa(depth)})
		slices.SortStableFunc(layer, func(p, q telemetry.Node) int { return cmp.Compare(parentCenter(p, placed), parentCenter(q, placed)) })
		left := meshGutter + (widest-len(layer))*meshColumn/2 + (meshColumn-meshNodeWidth)/2
		for i, n := range layer {
			box := boxOf(n, left+i*meshColumn, y, names)
			placed[n.DoorID] = box
			d.Nodes, drawn, rows = append(d.Nodes, box), append(drawn, n), append(rows, rowOfNode(n, names))
		}
	}
	for _, n := range drawn {
		if edge, ok := d.edgeOf(n, placed); ok {
			d.Edges = append(d.Edges, edge)
		}
	}
	return d, rows
}

// parentCenter is how far right what n hops through is drawn, as far
// as placed has drawn: the access point, at 0, when it hops through
// nothing, and past every node when what it hops through is not drawn
// yet.
func parentCenter(n telemetry.Node, placed map[int64]meshBox) int {
	if n.Mesh.Parent == "" {
		return 0
	}
	if box, found := placed[n.ParentID]; found {
		return box.X
	}
	return math.MaxInt
}

// edgeOf is the line from the top of n's node to the bottom of what it
// hops through, the access point or its parent's node, dashed when the
// parent is offline; placed holds every node. A node whose parent no
// door has has none.
func (d *meshDrawing) edgeOf(n telemetry.Node, placed map[int64]meshBox) (meshEdge, bool) {
	from := placed[n.DoorID]
	direct := n.Mesh.Parent == ""
	if direct {
		return meshEdge{Points: topOf(from) + " " + position(d.AccessPoint.X+meshNodeWidth/2, meshPointHeight), Class: "mesh-edge"}, true
	}
	to, found := placed[n.ParentID]
	if !found {
		return meshEdge{}, false
	}
	return meshEdge{Points: topOf(from) + " " + position(to.X+meshNodeWidth/2, to.Y+meshNodeHeight), Class: "mesh-edge" + lostClass(n)}, true
}

// topOf is the middle of the top of box.
func topOf(box meshBox) string {
	return position(box.X+meshNodeWidth/2, box.Y)
}

// position is x, y in the drawing's view box.
func position(x, y int) string {
	return strconv.Itoa(x) + "," + strconv.Itoa(y)
}

// boxOf is n's node, drawn at x and y, naming controllers from names.
func boxOf(n telemetry.Node, x, y int, names map[string]string) meshBox {
	notes := []string{}
	class := "mesh-node"
	if !n.Online {
		notes = append(notes, "Offline")
	}
	if n.ParentUnknown {
		notes = append(notes, "Parent unknown")
	}
	if n.Dependents > 0 {
		notes, class = append(notes, "Relay for "+strconv.Itoa(n.Dependents)), class+" mesh-relay"
	}
	return meshBox{
		X: x, Y: y, Href: doorPath(n.DoorID), Name: n.Name, Note: strings.Join(notes, " · "),
		Title: n.Name + ": layer " + strconv.Itoa(n.Mesh.Layer) + ", via " + hopsThrough(n, names) + titleFlags(n),
		Class: class + lostClass(n), Offline: !n.Online,
	}
}

// lostClass is the class that marks n's node and edge when what it hops
// through is unknown or offline.
func lostClass(n telemetry.Node) string {
	if n.ParentUnknown {
		return " mesh-lost"
	}
	return ""
}

// rowOfNode is n as the mesh page's table lists it, naming controllers
// from names.
func rowOfNode(n telemetry.Node, names map[string]string) meshRow {
	relayFor := "None"
	if n.Dependents > 0 {
		relayFor = doorsText(n.Dependents)
	}
	hops := []string{n.Name}
	for _, hop := range n.Path {
		hops = append(hops, hopName(hop, names))
	}
	end := "Unknown"
	if n.Reaches {
		end = "Access point"
	}
	return meshRow{
		Href: doorPath(n.DoorID), Name: n.Name, Status: onlineText(n.Online), Layer: n.Mesh.Layer,
		Through: hopsThrough(n, names), RelayFor: relayFor, Path: strings.Join(append(hops, end), " → "),
	}
}

// hopsThrough is what n hops through: the access point, or its parent's
// door, said to be offline, or the parent's hardware id when no door has
// it.
func hopsThrough(n telemetry.Node, names map[string]string) string {
	direct, doorless := n.Mesh.Parent == "", n.ParentID == 0
	if direct {
		return "Access point"
	}
	if doorless {
		return n.Mesh.Parent + " (no door)"
	}
	if n.ParentUnknown {
		return names[n.Mesh.Parent] + " (offline)"
	}
	return names[n.Mesh.Parent]
}

// titleFlags is what a node's title adds when n is offline or a relay.
func titleFlags(n telemetry.Node) string {
	text := ""
	if !n.Online {
		text = ", offline"
	}
	if n.Dependents > 0 {
		text += ", relay for " + doorsText(n.Dependents)
	}
	return text
}

// hopName names the controller whose hardware id is hardwareID: its
// door's name, or the hardware id when no door has it.
func hopName(hardwareID string, names map[string]string) string {
	if name, found := names[hardwareID]; found {
		return name
	}
	return hardwareID
}

// onlineText is the status of a door whose controller is enrolled, as
// door lists show it, while the controller is online or not.
func onlineText(online bool) string {
	if online {
		return "Online"
	}
	return "Offline"
}

// doorsText is n doors, in words.
func doorsText(n int) string {
	if n == 1 {
		return "1 door"
	}
	return strconv.Itoa(n) + " doors"
}
