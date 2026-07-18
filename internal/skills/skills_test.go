package skills

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/yourorg/gonexus/internal/graph"
)

func TestGenerate(t *testing.T) {
	g := graph.New()
	// a small clique so a community (module) is detected.
	for _, id := range []string{"a", "b", "c"} {
		g.AddNode(&graph.Node{ID: id, Name: id, Kind: graph.KindFunc, Package: "svc"})
	}
	g.AddEdge(graph.Edge{From: "a", To: "b", Kind: graph.EdgeCalls})
	g.AddEdge(graph.Edge{From: "b", To: "c", Kind: graph.EdgeCalls})
	g.AddEdge(graph.Edge{From: "c", To: "a", Kind: graph.EdgeCalls})

	dir := t.TempDir()
	files, err := Generate(g, "demo", dir)
	if err != nil {
		t.Fatal(err)
	}
	// 6 standard + at least 1 dynamic area skill.
	if len(files) < 7 {
		t.Fatalf("wrote %d skill files, want >= 7", len(files))
	}
	if _, err := os.Stat(filepath.Join(dir, "impact-analysis.md")); err != nil {
		t.Fatal("missing standard skill impact-analysis.md")
	}
	// a dynamic area-*.md exists and names the module.
	var area string
	for _, f := range files {
		if strings.Contains(filepath.Base(f), "area-") {
			area = f
		}
	}
	if area == "" {
		t.Fatal("no dynamic area skill generated")
	}
	b, _ := os.ReadFile(area)
	if !strings.Contains(string(b), "Module") {
		t.Fatalf("area skill missing module body: %s", b)
	}
}
