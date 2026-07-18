package groups

import (
	"os"
	"testing"

	"github.com/yourorg/gonexus/internal/graph"
	"github.com/yourorg/gonexus/internal/registry"
)

// makeRepo writes a repo with one exported symbol named `sym` and registers it.
func makeRepo(t *testing.T, name, sym string) {
	t.Helper()
	dir := t.TempDir()
	_ = os.MkdirAll(registry.CacheDir(dir), 0o755)
	g := graph.New()
	g.AddNode(&graph.Node{ID: name + "." + sym, Name: sym, Kind: graph.KindType})
	if err := g.Save(registry.GraphPath(registry.Repo{Path: dir})); err != nil {
		t.Fatal(err)
	}
	if err := registry.Add(name, dir); err != nil {
		t.Fatal(err)
	}
}

func TestSyncAndCrossImpact(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)

	// Two repos share the exported type "User"; one is unique.
	makeRepo(t, "api", "User")
	makeRepo(t, "web", "User")
	makeRepo(t, "tool", "Widget")

	for _, r := range []string{"api", "web", "tool"} {
		if err := registry.GroupCreate("svc"); err != nil {
			t.Fatal(err)
		}
		if err := registry.GroupAddRepo("svc", r); err != nil {
			t.Fatal(err)
		}
	}

	store := registry.NewStore()
	res, err := Sync(store, "svc")
	if err != nil {
		t.Fatal(err)
	}
	// "User" is shared by api+web -> one link; "Widget" only in tool -> no link.
	if len(res.Links) != 1 || res.Links[0].Key != "User" {
		t.Fatalf("links = %+v, want one link on User", res.Links)
	}
	if len(res.Links[0].Repos) != 2 {
		t.Fatalf("User link repos = %v, want api+web", res.Links[0].Repos)
	}

	// Cross-impact: changing api's User should flag web.
	affected, key, err := CrossImpact(store, "svc", "api", "api.User")
	if err != nil {
		t.Fatal(err)
	}
	if key != "User" || len(affected) != 1 || affected[0] != "web" {
		t.Fatalf("cross-impact = %v (key %q), want [web]", affected, key)
	}
}
