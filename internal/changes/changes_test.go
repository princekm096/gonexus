package changes

import (
	"testing"

	"github.com/yourorg/gonexus/internal/graph"
)

func TestParseUnifiedDiff(t *testing.T) {
	diff := `diff --git a/foo.go b/foo.go
--- a/foo.go
+++ b/foo.go
@@ -10,2 +10,3 @@ func A() {
+	x := 1
@@ -40 +41,0 @@ func B() {
diff --git a/gone.go b/gone.go
--- a/gone.go
+++ /dev/null
@@ -1,5 +0,0 @@
`
	got := parseUnifiedDiff(diff)
	if len(got["foo.go"]) != 2 {
		t.Fatalf("foo.go ranges = %v, want 2", got["foo.go"])
	}
	// first hunk: +10,3 -> [10,12]
	if got["foo.go"][0] != (lineRange{10, 12}) {
		t.Fatalf("range[0] = %v, want {10 12}", got["foo.go"][0])
	}
	// second hunk: +41,0 (deletion) -> [41,41]
	if got["foo.go"][1] != (lineRange{41, 41}) {
		t.Fatalf("range[1] = %v, want {41 41}", got["foo.go"][1])
	}
	if _, ok := got["gone.go"]; ok {
		t.Fatal("deleted file should be skipped")
	}
}

func TestSymbolsForRanges(t *testing.T) {
	g := graph.New()
	// Three funcs in one file at lines 5, 20, 50.
	g.AddNode(&graph.Node{ID: "A", Name: "A", Kind: graph.KindFunc, File: "/r/foo.go", Line: 5})
	g.AddNode(&graph.Node{ID: "B", Name: "B", Kind: graph.KindFunc, File: "/r/foo.go", Line: 20})
	g.AddNode(&graph.Node{ID: "C", Name: "C", Kind: graph.KindFunc, File: "/r/foo.go", Line: 50})

	// A change at line 25 falls in B's span [20,49].
	got := symbolsForRanges(g, "/r/foo.go", []lineRange{{25, 25}})
	if len(got) != 1 || got[0] != "B" {
		t.Fatalf("line 25 -> %v, want [B]", got)
	}
	// A range spanning A and C's declarations hits A and C (and B between).
	got = symbolsForRanges(g, "/r/foo.go", []lineRange{{5, 55}})
	if len(got) != 3 {
		t.Fatalf("wide range -> %v, want A,B,C", got)
	}
}
