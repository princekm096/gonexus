package graph

import "testing"

// a -> b -> c, plus d -> b. Checks impact (reverse), trace (forward), search.
func build() *Graph {
	g := New()
	for _, id := range []string{"a", "b", "c", "d"} {
		g.AddNode(&Node{ID: id, Name: id, Kind: KindFunc})
	}
	g.AddEdge(Edge{From: "a", To: "b", Kind: EdgeCalls})
	g.AddEdge(Edge{From: "b", To: "c", Kind: EdgeCalls})
	g.AddEdge(Edge{From: "d", To: "b", Kind: EdgeCalls})
	return g
}

func TestImpact(t *testing.T) {
	g := build()
	// who transitively calls c? b (direct), a and d (through b).
	got := g.Impact("c")
	want := map[string]bool{"a": true, "b": true, "d": true}
	if len(got) != 3 {
		t.Fatalf("impact(c) = %v, want a,b,d", got)
	}
	for _, id := range got {
		if !want[id] {
			t.Fatalf("unexpected caller %q in %v", id, got)
		}
	}
}

func TestTrace(t *testing.T) {
	g := build()
	path := g.Trace("a", "c")
	if len(path) != 3 || path[0] != "a" || path[2] != "c" {
		t.Fatalf("trace(a,c) = %v, want [a b c]", path)
	}
	if p := g.Trace("c", "a"); p != nil {
		t.Fatalf("trace(c,a) = %v, want nil (no reverse path)", p)
	}
}

func TestSearch(t *testing.T) {
	g := build()
	if r := g.Search("b", 10); len(r) == 0 || r[0].ID != "b" {
		t.Fatalf("search(b) top = %v, want b", r)
	}
}
