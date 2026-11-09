package telemetry

import (
	"slices"

	"github.com/dakriy/doorcode/internal/connector"
)

// Node is a door's controller in the site's mesh, as its latest report
// placed it. ParentID is the door whose controller has the Parent's
// hardware id, 0 when no door has it; ParentUnknown marks a controller
// that hops through a controller no door has or one that is offline.
// Path is the hardware ids of the controllers its traffic crosses on
// the way to the access point, its parent first, chained through each
// one's parent; Reaches says whether the chain ends at a controller that
// reaches the access point itself, rather than at one no door has or one
// already in the chain. Dependents is how many controllers' paths cross
// this one.
type Node struct {
	DoorID        int64
	Name          string
	HardwareID    string
	Mesh          Mesh
	Children      int
	Online        bool
	ParentID      int64
	ParentUnknown bool
	Path          []string
	Reaches       bool
	Dependents    int
}

// Graph is the site's mesh drawn from doors: a node for each door that is
// not deleted and has a controller enrolled, in the order of doors.
func Graph(doors []connector.StoredDoor) []Node {
	nodes := []Node{}
	for _, d := range doors {
		if d.DeletedAt == nil && d.Controller != nil {
			nodes = append(nodes, nodeOf(d))
		}
	}
	byHardware := map[string]int{}
	for i, n := range nodes {
		byHardware[n.HardwareID] = i
	}
	out := []Node{}
	for _, n := range nodes {
		parent, found := byHardware[n.Mesh.Parent]
		if found {
			n.ParentID = nodes[parent].DoorID
		}
		n.ParentUnknown = n.Mesh.Parent != "" && (!found || !nodes[parent].Online)
		n.Path, n.Reaches = pathOf(n, nodes, byHardware)
		out = append(out, n)
	}
	return withDependents(out, byHardware)
}

// withDependents is nodes, found by hardware id, each with how many of
// their paths cross it.
func withDependents(nodes []Node, byHardware map[string]int) []Node {
	out := slices.Clone(nodes)
	for _, n := range nodes {
		for _, hop := range n.Path {
			if i, found := byHardware[hop]; found {
				out[i].Dependents++
			}
		}
	}
	return out
}

// nodeOf is d's controller as a node, before the rest of the mesh is
// known.
func nodeOf(d connector.StoredDoor) Node {
	n := Node{DoorID: d.ID, Name: d.Name, HardwareID: d.Controller.HardwareID, Mesh: MeshOf(*d.Controller), Online: d.Controller.Online}
	if t := d.Controller.Telemetry; t != nil {
		n.Children = t.MeshChildren
	}
	return n
}

// pathOf chains n's parents through nodes, found by hardware id, and
// says whether the chain reaches the access point.
func pathOf(n Node, nodes []Node, byHardware map[string]int) ([]string, bool) {
	path := []string{}
	for parent := n.Mesh.Parent; parent != ""; {
		if parent == n.HardwareID || slices.Contains(path, parent) {
			return path, false
		}
		path = append(path, parent)
		i, found := byHardware[parent]
		if !found {
			return path, false
		}
		parent = nodes[i].Mesh.Parent
	}
	return path, true
}
