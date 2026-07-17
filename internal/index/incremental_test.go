package index

import (
	"os"
	"path/filepath"
	"testing"
)

func TestFingerprintStableAndSensitive(t *testing.T) {
	dir := t.TempDir()
	write := func(name, body string) {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("a.go", "package a\n")

	fp1, err := fingerprint(dir, goExts)
	if err != nil {
		t.Fatal(err)
	}
	// Same tree, no changes -> identical fingerprint (drives cache reuse).
	fp2, _ := fingerprint(dir, goExts)
	if fp1 != fp2 {
		t.Fatalf("fingerprint unstable: %s != %s", fp1, fp2)
	}
	// New file -> different fingerprint (drives rebuild).
	write("b.go", "package a\n")
	fp3, _ := fingerprint(dir, goExts)
	if fp3 == fp1 {
		t.Fatal("fingerprint unchanged after adding a file")
	}
	// Test files are excluded, so adding one must NOT change the fingerprint.
	write("a_test.go", "package a\n")
	fp4, _ := fingerprint(dir, goExts)
	if fp4 != fp3 {
		t.Fatal("fingerprint changed on a _test.go file (should be ignored)")
	}
}

func TestIsStale(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "a.go"), []byte("package a\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// Never indexed -> stale.
	if !IsStale(dir) {
		t.Fatal("un-indexed repo should be stale")
	}
	// Persist a manifest matching the current fingerprints -> fresh.
	cacheDir := filepath.Join(dir, ".gonexus")
	want := manifest{}
	want.TS, _ = fingerprint(dir, tsExts)
	// no go.mod -> hasGo false -> want.Go stays ""
	if err := saveManifest(cacheDir, want); err != nil {
		t.Fatal(err)
	}
	if IsStale(dir) {
		t.Fatal("repo matching its manifest should be fresh")
	}
	// Add a file -> stale again.
	if err := os.WriteFile(filepath.Join(dir, "b.ts"), []byte("export const x = 1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if !IsStale(dir) {
		t.Fatal("repo with new file should be stale")
	}
}

func TestManifestRoundTrip(t *testing.T) {
	dir := t.TempDir()
	if err := saveManifest(dir, manifest{Go: "x", TS: "y"}); err != nil {
		t.Fatal(err)
	}
	m := loadManifest(dir)
	if m.Go != "x" || m.TS != "y" {
		t.Fatalf("round-trip = %+v, want {x y}", m)
	}
	// Missing manifest -> zero value, no error.
	if got := loadManifest(t.TempDir()); got.Go != "" || got.TS != "" {
		t.Fatalf("missing manifest = %+v, want empty", got)
	}
}
