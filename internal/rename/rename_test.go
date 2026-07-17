package rename

import (
	"os"
	"path/filepath"
	"regexp"
	"testing"

	"github.com/yourorg/gonexus/internal/graph"
)

func TestMatchLinesWholeWord(t *testing.T) {
	re := regexp.MustCompile(`\bAdd\b`)
	src := "func Add() {}\n// Additional note\ncall Add()\n"
	got := matchLines(src, re)
	// line 1 (Add), line 3 (Add) — NOT line 2 (Additional is not whole-word).
	if len(got) != 2 || got[0] != 1 || got[1] != 3 {
		t.Fatalf("matchLines = %v, want [1 3]", got)
	}
}

func TestPlanAndApply(t *testing.T) {
	dir := t.TempDir()
	defFile := filepath.Join(dir, "def.go")
	useFile := filepath.Join(dir, "use.go")
	os.WriteFile(defFile, []byte("package p\nfunc Add(a, b int) int { return a + b }\n"), 0o644)
	os.WriteFile(useFile, []byte("package p\nfunc use() int { return Add(1, 2) }\n"), 0o644)

	g := graph.New()
	g.AddNode(&graph.Node{ID: "p.Add", Name: "Add", Kind: graph.KindFunc, Package: "p", File: defFile, Line: 2})
	g.AddNode(&graph.Node{ID: "p.use", Name: "use", Kind: graph.KindFunc, Package: "p", File: useFile, Line: 2})
	g.AddEdge(graph.Edge{From: "p.use", To: "p.Add", Kind: graph.EdgeCalls})

	// Plan only.
	res, err := Plan(g, "p.Add", "Sum", false)
	if err != nil {
		t.Fatal(err)
	}
	if res.Confidence != 1.0 { // "Add" is unique in the graph
		t.Fatalf("confidence = %v, want 1.0", res.Confidence)
	}
	if len(res.Edits) != 2 { // def.go and use.go both reference Add
		t.Fatalf("edits = %+v, want 2 files", res.Edits)
	}
	if res.Applied {
		t.Fatal("plan-only should not apply")
	}
	// Files untouched by a plan-only run.
	if b, _ := os.ReadFile(useFile); string(b) == "" || regexp.MustCompile(`\bSum\b`).Match(b) {
		t.Fatal("plan-only modified files")
	}

	// Apply.
	res, err = Plan(g, "p.Add", "Sum", true)
	if err != nil || !res.Applied {
		t.Fatalf("apply failed: %v", err)
	}
	b, _ := os.ReadFile(useFile)
	if !regexp.MustCompile(`\bSum\(1, 2\)`).Match(b) {
		t.Fatalf("use.go not updated: %s", b)
	}
	if regexp.MustCompile(`\bAdd\b`).Match(b) {
		t.Fatalf("old name remains in use.go: %s", b)
	}
}

func TestPlanRejectsBadName(t *testing.T) {
	g := graph.New()
	g.AddNode(&graph.Node{ID: "x", Name: "X", Kind: graph.KindFunc})
	if _, err := Plan(g, "x", "1bad", false); err == nil {
		t.Fatal("want error for invalid identifier")
	}
}
