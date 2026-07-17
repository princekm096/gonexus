package registry

import (
	"os"
	"testing"
	"time"

	"github.com/yourorg/gonexus/internal/graph"
)

// isolate points HOME/USERPROFILE at a temp dir so the registry file and repo
// caches don't touch the real ~/.gonexus.
func isolate(t *testing.T) {
	t.Helper()
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	t.Setenv("USERPROFILE", tmp) // windows
}

func TestDefaultName(t *testing.T) {
	f := &File{Repos: map[string]Repo{}}
	if _, err := f.DefaultName(""); err == nil {
		t.Error("empty registry should error on default")
	}
	f.Repos["only"] = Repo{Name: "only"}
	if n, err := f.DefaultName(""); err != nil || n != "only" {
		t.Errorf("sole repo default = %q,%v; want only", n, err)
	}
	f.Repos["second"] = Repo{Name: "second"}
	if _, err := f.DefaultName(""); err == nil {
		t.Error("ambiguous registry should error on empty selector")
	}
	if _, err := f.DefaultName("nope"); err == nil {
		t.Error("unknown repo should error")
	}
}

func TestStoreLoadsAndReloads(t *testing.T) {
	isolate(t)

	// Fake a registered repo: a repo dir with a persisted graph.
	repoDir := t.TempDir()
	if err := os.MkdirAll(CacheDir(repoDir), 0o755); err != nil {
		t.Fatal(err)
	}
	g := graph.New()
	g.AddNode(&graph.Node{ID: "a", Name: "a", Kind: graph.KindFunc})
	if err := g.Save(GraphPath(Repo{Path: repoDir})); err != nil {
		t.Fatal(err)
	}
	if err := Add("demo", repoDir); err != nil {
		t.Fatal(err)
	}

	s := NewStore()
	got, name, err := s.Graph("") // empty -> sole repo
	if err != nil || name != "demo" {
		t.Fatalf("Graph(\"\") = %q,%v; want demo", name, err)
	}
	if got.Nodes["a"] == nil {
		t.Fatal("loaded graph missing node a")
	}

	// Rewrite the graph with a new node; store must pick it up on mtime change.
	g.AddNode(&graph.Node{ID: "b", Name: "b", Kind: graph.KindFunc})
	if err := g.Save(GraphPath(Repo{Path: repoDir})); err != nil {
		t.Fatal(err)
	}
	future := time.Now().Add(time.Second)
	if err := os.Chtimes(GraphPath(Repo{Path: repoDir}), future, future); err != nil {
		t.Fatal(err)
	}
	got2, _, _ := s.Graph("demo")
	if got2.Nodes["b"] == nil {
		t.Fatal("store did not reload after graph.json changed")
	}
}

func TestListCounts(t *testing.T) {
	isolate(t)
	repoDir := t.TempDir()
	_ = os.MkdirAll(CacheDir(repoDir), 0o755)
	g := graph.New()
	g.AddNode(&graph.Node{ID: "x", Name: "x", Kind: graph.KindFunc})
	_ = g.Save(GraphPath(Repo{Path: repoDir}))
	_ = Add("r", repoDir)

	repos, counts, err := NewStore().List()
	if err != nil || len(repos) != 1 || counts[0] != 1 {
		t.Fatalf("List = %v counts %v err %v; want 1 repo, 1 node", repos, counts, err)
	}
}
