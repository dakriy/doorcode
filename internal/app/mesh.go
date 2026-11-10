package app

import (
	"net/http"

	"github.com/dakriy/doorcode/internal/auth"
	"github.com/dakriy/doorcode/internal/openapi"
	"github.com/dakriy/doorcode/internal/telemetry"
)

type meshNodeView struct {
	DoorID             int64    `json:"door_id" doc:"The id of the door the controller is enrolled as."`
	Name               string   `json:"name" doc:"The door's name."`
	HardwareID         string   `json:"hardware_id" doc:"The controller's hardware id."`
	Layer              int      `json:"layer" doc:"The controller's layer in its latest report: 0 when it reaches the building's access point itself, 1 when it hops through one other controller, and so on."`
	Parent             string   `json:"parent" doc:"The hardware id of the controller it hops through; empty when it reaches the access point itself."`
	Children           int      `json:"children" doc:"How many controllers hop through it, as its latest report said."`
	Online             bool     `json:"online" doc:"True while the controller is connected to the broker."`
	ParentUnknown      bool     `json:"parent_unknown" doc:"True when it hops through a controller no door has, or through one that is offline."`
	Dependents         int      `json:"dependents" doc:"How many controllers' paths to the access point pass through this one; above 0 for a relay."`
	Path               []string `json:"path" doc:"The hardware ids of the controllers its traffic passes through on the way to the access point, its parent first, chained through each one's parent."`
	ReachesAccessPoint bool     `json:"reaches_access_point" doc:"True when the path ends at a controller that reaches the access point itself; false when it ends at a controller no door has, or comes back on itself."`
}

type meshEdgeView struct {
	From int64 `json:"from" doc:"The id of the door whose controller hops."`
	To   int64 `json:"to" doc:"The id of the door whose controller it hops through."`
}

type meshView struct {
	Nodes []meshNodeView `json:"nodes" doc:"Each door with a controller enrolled that is not deleted, by name."`
	Edges []meshEdgeView `json:"edges" doc:"An edge from each controller to the controller it hops through, when a door has that one."`
}

func (a *App) meshRoutes() []route {
	return []route{
		pageRoute(http.MethodGet, "/admin/mesh", opensDoors, a.meshPage),
		apiRoute(http.MethodGet, "/mesh", opensDoors, a.getMesh, apiDoc{
			id: "getMesh", summary: "Get the site's Wi-Fi mesh", tag: "telemetry",
			responses: []openapi.Response{{Status: http.StatusOK, Description: "Where each door's controller sits in the mesh, as its latest report said.", Body: meshView{}}},
		}),
	}
}

func (a *App) getMesh(w http.ResponseWriter, r *http.Request, _ auth.User) {
	doors, err := a.doors.CurrentDoors(r.Context())
	if err != nil {
		a.internalError(w, r, err)
		return
	}
	view := meshView{Nodes: []meshNodeView{}, Edges: []meshEdgeView{}}
	for _, n := range telemetry.Graph(doors) {
		view.Nodes = append(view.Nodes, meshNodeView{
			DoorID: n.DoorID, Name: n.Name, HardwareID: n.HardwareID, Layer: n.Mesh.Layer, Parent: n.Mesh.Parent, Children: n.Children,
			Online: n.Online, ParentUnknown: n.ParentUnknown, Dependents: n.Dependents, Path: n.Path, ReachesAccessPoint: n.Reaches,
		})
		if n.ParentID != 0 {
			view.Edges = append(view.Edges, meshEdgeView{From: n.DoorID, To: n.ParentID})
		}
	}
	writeJSON(w, http.StatusOK, view)
}
