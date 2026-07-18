package index

import (
	"testing"

	"github.com/yourorg/gonexus/internal/graph"
)

// Circle (value receiver) and Rect (pointer receiver) both satisfy Shape.
func TestImplementsEdges(t *testing.T) {
	g, err := BuildGo("testdata/gofix")
	if err != nil {
		t.Fatal(err)
	}
	if !hasEdge(g, "gofix.Circle", "gofix.Shape", graph.EdgeImplements) {
		t.Error("missing implements edge Circle -> Shape")
	}
	if !hasEdge(g, "gofix.Rect", "gofix.Shape", graph.EdgeImplements) {
		t.Error("missing implements edge Rect -> Shape (pointer receiver)")
	}
	// Constructor inference: NewCircle -> Circle.
	if !hasEdge(g, "gofix.NewCircle", "gofix.Circle", graph.EdgeConstructs) {
		t.Error("missing constructs edge NewCircle -> Circle")
	}
	// Route detection: mux.HandleFunc("/health", health).
	var route *graph.Route
	for i := range g.Routes {
		if g.Routes[i].Path == "/health" {
			route = &g.Routes[i]
		}
	}
	if route == nil || route.Handler != "gofix.health" {
		t.Fatalf("route /health not detected with handler gofix.health: %+v", g.Routes)
	}
}
