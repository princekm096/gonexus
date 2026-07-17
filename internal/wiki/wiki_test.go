package wiki

import (
	"context"
	"strings"
	"testing"

	"github.com/yourorg/gonexus/internal/graph"
)

func sample() *graph.Graph {
	g := graph.New()
	g.AddNode(&graph.Node{ID: "main", Name: "main", Kind: graph.KindFunc})
	g.AddNode(&graph.Node{ID: "helper", Name: "helper", Kind: graph.KindFunc})
	g.AddNode(&graph.Node{ID: "I", Name: "Store", Kind: graph.KindInterface})
	g.AddNode(&graph.Node{ID: "A", Name: "MemStore", Kind: graph.KindType})
	g.AddNode(&graph.Node{ID: "B", Name: "FileStore", Kind: graph.KindType})
	g.AddEdge(graph.Edge{From: "main", To: "helper", Kind: graph.EdgeCalls})
	g.AddEdge(graph.Edge{From: "A", To: "I", Kind: graph.EdgeImplements})
	g.AddEdge(graph.Edge{From: "B", To: "I", Kind: graph.EdgeImplements})
	return g
}

func TestWikiMarkdown(t *testing.T) {
	md := Build(sample(), "demo").Markdown()
	for _, want := range []string{
		"# demo — Architecture",
		"## Entry Points",
		"`main`",
		"## Key Interfaces",
		"`Store` — implemented by FileStore, MemStore",
		"## Most-Called",
		"`helper` — 1 callers",
	} {
		if !strings.Contains(md, want) {
			t.Fatalf("wiki markdown missing %q\n---\n%s", want, md)
		}
	}
}

// nilClient path: no LLM -> structural only, no error.
func TestGenerateNoLLM(t *testing.T) {
	md, err := Generate(context.Background(), sample(), "demo", nil)
	if err != nil || !strings.Contains(md, "## Modules") {
		t.Fatalf("structural generate failed: %v", err)
	}
}
