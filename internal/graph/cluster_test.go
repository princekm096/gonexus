package graph

import "testing"

func clique(g *Graph, ids ...string) {
	for _, a := range ids {
		g.AddNode(&Node{ID: a, Name: a, Kind: KindFunc})
	}
	for i, a := range ids {
		for _, b := range ids[i+1:] {
			g.AddEdge(Edge{From: a, To: b, Kind: EdgeCalls})
		}
	}
}

// Two disconnected triangles -> two communities; a lone node -> no community.
func TestCommunities(t *testing.T) {
	g := New()
	clique(g, "a", "b", "c")
	clique(g, "x", "y", "z")
	g.AddNode(&Node{ID: "lone", Name: "lone", Kind: KindFunc})

	coms := g.Communities()
	if len(coms) != 2 {
		t.Fatalf("got %d communities, want 2: %+v", len(coms), coms)
	}
	// Each triangle's members must land in the same community.
	label := map[string]int{}
	for i, c := range coms {
		for _, m := range c.Members {
			label[m] = i
		}
	}
	if label["a"] != label["b"] || label["b"] != label["c"] {
		t.Fatalf("triangle a,b,c split across communities: %v", label)
	}
	if label["x"] != label["y"] || label["y"] != label["z"] {
		t.Fatalf("triangle x,y,z split across communities: %v", label)
	}
	if label["a"] == label["x"] {
		t.Fatal("disconnected triangles merged into one community")
	}
	if _, ok := label["lone"]; ok {
		t.Fatal("singleton should not form a community")
	}
}
