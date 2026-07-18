package graph

import "testing"

func TestShapeCheck(t *testing.T) {
	g := New()
	// provider: Go type User with JSON fields name, email.
	g.AddNode(&Node{ID: "app.User", Name: "User", Kind: KindType, Fields: []string{"name", "email"}})
	// consumer: accesses name (ok), emial (typo), email (ok).
	g.AddShape(AccessShape{Object: "user", Props: []string{"name", "emial", "email"}, File: "web/App.vue"})
	// unrelated object with no matching type -> ignored.
	g.AddShape(AccessShape{Object: "widget", Props: []string{"whatever"}, File: "web/X.vue"})

	f := g.ShapeCheck()
	if len(f) != 1 {
		t.Fatalf("findings = %+v, want 1", f)
	}
	if f[0].Type != "user" || len(f[0].Unknown) != 1 || f[0].Unknown[0] != "emial" {
		t.Fatalf("finding = %+v, want unknown [emial] on user", f[0])
	}
}
