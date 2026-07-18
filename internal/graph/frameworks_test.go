package graph

import "testing"

func TestFrameworks(t *testing.T) {
	g := New()
	g.AddNode(&Node{ID: "app", Kind: KindPackage})
	g.AddEdge(Edge{From: "app", To: "github.com/gin-gonic/gin", Kind: EdgeImports})
	g.AddEdge(Edge{From: "app", To: "gorm.io/gorm", Kind: EdgeImports})
	g.AddEdge(Edge{From: "app", To: "fmt", Kind: EdgeImports}) // stdlib, not a framework

	got := Frameworks(g)
	if len(got) != 2 || got[0] != "GORM" || got[1] != "Gin" {
		t.Fatalf("frameworks = %v, want [GORM Gin]", got)
	}
}
