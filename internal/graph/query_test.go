package graph

import "testing"

func queryGraph() *Graph {
	g := New()
	g.AddNode(&Node{ID: "f", Name: "f", Kind: KindFunc})
	g.AddNode(&Node{ID: "m", Name: "m", Kind: KindMethod})
	g.AddNode(&Node{ID: "t", Name: "t", Kind: KindType})
	g.AddEdge(Edge{From: "f", To: "m", Kind: EdgeCalls})
	g.AddEdge(Edge{From: "f", To: "t", Kind: EdgeCalls}) // f calls t (type) - unusual but for filter test
	return g
}

func TestCheck(t *testing.T) {
	g := queryGraph()
	g.AddEdge(Edge{From: "f", To: "ghost", Kind: EdgeCalls}) // dangling: ghost missing
	missing, dangling := g.Check([]string{"f", "nope"})
	if len(missing) != 1 || missing[0] != "nope" {
		t.Fatalf("missing = %v, want [nope]", missing)
	}
	if len(dangling) != 1 || dangling[0].To != "ghost" {
		t.Fatalf("dangling = %v, want edge to ghost", dangling)
	}
}

func TestCypher(t *testing.T) {
	g := queryGraph()
	// funcs that call methods -> only f->m.
	edges, err := g.Cypher("(a:func)-[:calls]->(b:method)", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(edges) != 1 || edges[0].From != "f" || edges[0].To != "m" {
		t.Fatalf("cypher = %v, want f->m", edges)
	}
	// bad pattern errors.
	if _, err := g.Cypher("nonsense", 10); err == nil {
		t.Fatal("want error for bad pattern")
	}
}
