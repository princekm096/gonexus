package graph

import "testing"

// a -> b -> c ; e -> b ; d isolated (no calls). Entry points: a, e.
func procGraph() *Graph {
	g := New()
	for _, id := range []string{"a", "b", "c", "d", "e"} {
		g.AddNode(&Node{ID: id, Name: id, Kind: KindFunc})
	}
	g.AddEdge(Edge{From: "a", To: "b", Kind: EdgeCalls})
	g.AddEdge(Edge{From: "b", To: "c", Kind: EdgeCalls})
	g.AddEdge(Edge{From: "e", To: "b", Kind: EdgeCalls})
	return g
}

func TestEntryPoints(t *testing.T) {
	got := ids(procGraph().EntryPoints())
	// a and e are call roots that call something; b,c are called; d calls nothing.
	if len(got) != 2 || got[0] != "a" || got[1] != "e" {
		t.Fatalf("entry points = %v, want [a e]", got)
	}
}

func TestProcess(t *testing.T) {
	g := procGraph()
	nodes, edges := g.Process("a")
	if got := ids(nodes); len(got) != 3 || got[0] != "a" || got[2] != "c" {
		t.Fatalf("process(a) nodes = %v, want [a b c]", got)
	}
	if len(edges) != 2 { // a->b, b->c (not e->b: e not in the flow)
		t.Fatalf("process(a) edges = %v, want 2", edges)
	}
	if g.ProcessSize("a") != 3 {
		t.Fatalf("ProcessSize(a) = %d, want 3", g.ProcessSize("a"))
	}
}

func TestProcessesOf(t *testing.T) {
	// c is reached by both entry points a and e (via b).
	got := ids(procGraph().ProcessesOf("c"))
	if len(got) != 2 || got[0] != "a" || got[1] != "e" {
		t.Fatalf("processesOf(c) = %v, want [a e]", got)
	}
}

func TestImpactGraded(t *testing.T) {
	g := procGraph() // a->b->c, e->b
	graded := g.ImpactGraded("c")
	// b is a direct caller (depth 1, conf 1.0); a and e are depth 2 (conf 0.5).
	byID := map[string]GradedImpact{}
	for _, gi := range graded {
		byID[gi.ID] = gi
	}
	if byID["b"].Depth != 1 || byID["b"].Confidence != 1.0 {
		t.Fatalf("b = %+v, want depth1 conf1.0", byID["b"])
	}
	if byID["a"].Depth != 2 || byID["e"].Depth != 2 {
		t.Fatalf("a/e should be depth 2: %+v", graded)
	}
}
