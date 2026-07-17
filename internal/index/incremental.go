package index

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/yourorg/gonexus/internal/graph"
)

// Incremental re-index at per-language granularity. Each language's subgraph is
// cached (.gonexus/{go,ts}.json) with a content fingerprint in manifest.json.
// A language is rebuilt only when its files changed; an unchanged language is
// reloaded from cache without touching its toolchain. So a no-op reindex is
// two file loads, and editing only Go skips the TS extractor (and vice versa).
//
// ponytail: fingerprint is path+size+mtime (make-style, stat-only, no reads).
// Switch to content hashing if mtime-preserving edits ever slip through.

var goExts = map[string]bool{".go": true}
var tsExts = map[string]bool{".ts": true, ".tsx": true, ".js": true, ".jsx": true, ".vue": true}

var skipDirs = map[string]bool{
	"node_modules": true, ".git": true, "dist": true, "build": true,
	".gonexus": true, ".nuxt": true, ".output": true, "vendor": true,
}

type manifest struct {
	Go string `json:"go"`
	TS string `json:"ts"`
}

// BuildRepo builds a combined Go + TS/JS/Vue graph, reusing cached per-language
// subgraphs under cacheDir when the language's files are unchanged. Either
// language absent or failing is tolerated. Errors only if nothing was indexed.
func BuildRepo(dir, cacheDir string) (*graph.Graph, error) {
	man := loadManifest(cacheDir)
	next := manifest{}
	g := graph.New()
	built := false
	var lastErr error

	if hasGo(dir) {
		next.Go, _ = fingerprint(dir, goExts)
		if gg, err := buildLang(dir, cacheDir, "go", next.Go, man.Go, BuildGo); err == nil {
			g.Merge(gg)
			built = true
		} else {
			lastErr = fmt.Errorf("go: %w", err)
		}
	}

	// TS extractor walks recursively and returns empty for Go-only repos.
	next.TS, _ = fingerprint(dir, tsExts)
	if tg, err := buildLang(dir, cacheDir, "ts", next.TS, man.TS, BuildTS); err == nil {
		if len(tg.Nodes) > 0 {
			g.Merge(tg)
			built = true
		}
	} else {
		lastErr = fmt.Errorf("ts: %w", err)
	}

	if !built {
		if lastErr != nil {
			return nil, lastErr
		}
		return nil, fmt.Errorf("no Go module or TS/JS/Vue files found in %s", dir)
	}
	_ = saveManifest(cacheDir, next)
	return g, nil
}

// IsStale reports whether repoPath's source no longer matches its cached index
// (or was never indexed). Mirrors the fingerprints BuildRepo would compute.
func IsStale(repoPath string) bool {
	cacheDir := filepath.Join(repoPath, ".gonexus")
	want := manifest{}
	if hasGo(repoPath) {
		want.Go, _ = fingerprint(repoPath, goExts)
	}
	want.TS, _ = fingerprint(repoPath, tsExts)
	return loadManifest(cacheDir) != want
}

// buildLang returns the cached subgraph if fp matches the previous fingerprint,
// otherwise rebuilds via build() and refreshes the cache.
func buildLang(dir, cacheDir, name, fp, prevFP string, build func(string) (*graph.Graph, error)) (*graph.Graph, error) {
	cache := filepath.Join(cacheDir, name+".json")
	if fp != "" && fp == prevFP {
		if g, err := graph.Load(cache); err == nil {
			return g, nil // unchanged: skip the toolchain entirely
		}
		// cache missing despite matching fp -> fall through and rebuild
	}
	g, err := build(dir)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(cacheDir, 0o755); err == nil {
		_ = g.Save(cache)
	}
	return g, nil
}

// fingerprint hashes path+size+mtime of every matching file under dir.
func fingerprint(dir string, exts map[string]bool) (string, error) {
	var files []string
	err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil // tolerate unreadable entries
		}
		if d.IsDir() {
			if skipDirs[d.Name()] {
				return fs.SkipDir
			}
			return nil
		}
		name := d.Name()
		// Go test files don't affect the graph (BuildGo uses Tests:false).
		if exts[".go"] && strings.HasSuffix(name, "_test.go") {
			return nil
		}
		if exts[filepath.Ext(name)] {
			files = append(files, p)
		}
		return nil
	})
	if err != nil {
		return "", err
	}
	sort.Strings(files)
	h := sha256.New()
	for _, p := range files {
		info, err := os.Stat(p)
		if err != nil {
			continue
		}
		fmt.Fprintf(h, "%s\x00%d\x00%d\n", p, info.Size(), info.ModTime().UnixNano())
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func loadManifest(cacheDir string) manifest {
	var m manifest
	b, err := os.ReadFile(filepath.Join(cacheDir, "manifest.json"))
	if err != nil {
		return m
	}
	_ = json.Unmarshal(b, &m)
	return m
}

func saveManifest(cacheDir string, m manifest) error {
	b, err := json.Marshal(m)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(cacheDir, 0o755); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(cacheDir, "manifest.json"), b, 0o644)
}
