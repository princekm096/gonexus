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
}
