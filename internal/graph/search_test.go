package graph

import "testing"

func TestTokenizeCamel(t *testing.T) {
	got := tokenize("BuildRepo")
	want := map[string]bool{"buildrepo": true, "build": true, "repo": true}
	for _, tok := range got {
		delete(want, tok)
	}
	if len(want) != 0 {
		t.Fatalf("tokenize(BuildRepo)=%v, missing %v", got, want)
	}
}

func TestBM25Ranking(t *testing.T) {
	g := New()
	g.AddNode(&Node{ID: "1", Name: "BuildRepo", Kind: KindFunc, Doc: "build the repository graph"})
	g.AddNode(&Node{ID: "2", Name: "BuildGo", Kind: KindFunc, Doc: "build go packages"})
	g.AddNode(&Node{ID: "3", Name: "Serve", Kind: KindFunc, Doc: "serve http"})

	res := g.Search("build repo", 3)
	if len(res) == 0 || res[0].ID != "1" {
		t.Fatalf("top for 'build repo' = %v, want BuildRepo(1)", ids(res))
	}
	// "serve" must not surface for a build query.
	for _, n := range res {
		if n.ID == "3" {
			t.Fatalf("unrelated Serve matched 'build repo': %v", ids(res))
		}
	}
}

func TestSearchHybridRecall(t *testing.T) {
	g := New()
	// "a" matches the query lexically; "b" does not, but is the semantic match.
	g.AddNode(&Node{ID: "a", Name: "handler", Kind: KindFunc, Vector: Normalize([]float32{1, 0, 0})})
	g.AddNode(&Node{ID: "b", Name: "renderer", Kind: KindFunc, Vector: Normalize([]float32{0, 1, 0})})

	// BM25 alone can't find "b" from the term "handler".
	if got := ids(g.Search("handler", 5)); contains(got, "b") {
		t.Fatalf("BM25 unexpectedly returned b: %v", got)
	}
	// Hybrid, with a query vector pointing at b, recalls it.
	q := []float32{0, 1, 0}
	if got := ids(g.SearchHybrid("handler", 5, q)); !contains(got, "b") {
		t.Fatalf("hybrid failed to recall semantic match b: %v", got)
	}
}

func contains(ss []string, s string) bool {
	for _, x := range ss {
		if x == s {
			return true
		}
	}
	return false
}

func TestRRF(t *testing.T) {
	// x is 2nd in list1 but 1st in list2 -> should win over y (1st,then absent).
	fused := rrf([]string{"y", "x"}, []string{"x", "z"})
	if fused[0] != "x" {
		t.Fatalf("rrf top = %q, want x", fused[0])
	}
}

func ids(ns []*Node) []string {
	out := make([]string, len(ns))
	for i, n := range ns {
		out[i] = n.ID
	}
	return out
}

func TestStem(t *testing.T) {
	cases := map[string]string{"parsing": "pars", "parsed": "pars", "parses": "pars", "queries": "query"}
	for in, want := range cases {
		if got := stem(in); got != want {
			t.Errorf("stem(%q)=%q, want %q", in, got, want)
		}
	}
	// short words unchanged
	if stem("go") != "go" {
		t.Error("short word should not stem")
	}
}
