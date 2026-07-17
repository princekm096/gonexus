package index

import (
	"os/exec"
	"testing"

	"github.com/yourorg/gonexus/internal/graph"
)

// Exercises the Node extractor on a tiny .vue + .ts fixture. Skips when node or
// the extractor's deps aren't installed (not every CI has them).
func TestBuildTS(t *testing.T) {
	if _, err := exec.LookPath("node"); err != nil {
		t.Skip("node not installed")
	}
	t.Setenv("GONEXUS_TS_EXTRACTOR", "../../tools/ts-extractor/extract.mjs")

	g, err := BuildTS("testdata/tsfix")
	if err != nil {
		t.Skipf("extractor unavailable (run npm install in tools/ts-extractor): %v", err)
	}

	if n := g.Nodes["Widget.vue"]; n == nil || n.Kind != graph.KindComponent {
		t.Fatalf("Widget.vue node = %v, want kind=component", n)
	}
	if g.Nodes["util.ts#greet"] == nil {
		t.Fatal("missing util.ts#greet func node")
	}
	if !hasEdge(g, "Widget.vue", "util.ts", graph.EdgeImports) {
		t.Fatal("missing import edge Widget.vue -> util.ts")
	}
	// hello() calls greet(), greet is a unique name -> resolved call edge.
	if !hasEdge(g, "Widget.vue#hello", "util.ts#greet", graph.EdgeCalls) {
		t.Fatal("missing call edge hello -> greet")
	}
	// Object-literal method node, and its expression body's call resolves.
	if g.Nodes["util.ts#api.hi"] == nil {
		t.Fatal("missing object-literal method node util.ts#api.hi")
	}
	if !hasEdge(g, "util.ts#api.hi", "util.ts#greet", graph.EdgeCalls) {
		t.Fatal("missing call edge api.hi -> greet (expression-body arrow)")
	}
}

func hasEdge(g *graph.Graph, from, to string, kind graph.EdgeKind) bool {
	for _, e := range g.Edges {
		if e.From == from && e.To == to && e.Kind == kind {
			return true
		}
	}
	return false
}
