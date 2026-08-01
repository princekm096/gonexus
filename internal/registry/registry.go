// Package registry tracks the set of indexed repositories in
// ~/.gonexus/registry.json, so one server/MCP process serves many repos.
// Each repo's graph + caches live centrally under ~/.gonexus/cache/<key>/ so
// indexing never writes into the repo itself.
package registry

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"
)

type Repo struct {
	Name      string    `json:"name"`
	Path      string    `json:"path"`
	IndexedAt time.Time `json:"indexedAt"`
}

type File struct {
	Repos map[string]Repo `json:"repos"`
}

// Path is ~/.gonexus/registry.json.
func Path() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".gonexus", "registry.json"), nil
}

// GraphPath is where a repo's persisted graph lives.
func GraphPath(r Repo) string { return filepath.Join(CacheDir(r.Path), "graph.json") }

// CacheDir is where a repo's graph + incremental caches live: a central,
// per-repo directory under ~/.gonexus/cache/ keyed by the repo's absolute path,
// so indexing never litters the repo with .gonexus files.
func CacheDir(repoPath string) string {
	abs, err := filepath.Abs(repoPath)
	if err != nil {
		abs = repoPath
	}
	sum := sha256.Sum256([]byte(abs))
	key := filepath.Base(abs) + "-" + hex.EncodeToString(sum[:])[:12]
	home, err := os.UserHomeDir()
	if err != nil {
		home = os.TempDir()
	}
	return filepath.Join(home, ".gonexus", "cache", key)
}

// Load reads the registry, returning an empty one if the file doesn't exist.
func Load() (*File, error) {
	p, err := Path()
	if err != nil {
		return nil, err
	}
	b, err := os.ReadFile(p)
	if os.IsNotExist(err) {
		return &File{Repos: map[string]Repo{}}, nil
	}
	if err != nil {
		return nil, err
	}
	var f File
	if err := json.Unmarshal(b, &f); err != nil {
		return nil, err
	}
	if f.Repos == nil {
		f.Repos = map[string]Repo{}
	}
	return &f, nil
}

func (f *File) Save() error {
	p, err := Path()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	b, err := json.MarshalIndent(f, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(p, b, 0o644)
}

// Names returns the registered repo names, sorted.
func (f *File) Names() []string {
	out := make([]string, 0, len(f.Repos))
	for n := range f.Repos {
		out = append(out, n)
	}
	sort.Strings(out)
	return out
}

// Add upserts a repo (name -> absolute path) and persists.
func Add(name, repoPath string) error {
	abs, err := filepath.Abs(repoPath)
	if err != nil {
		return err
	}
	f, err := Load()
	if err != nil {
		return err
	}
	f.Repos[name] = Repo{Name: name, Path: abs, IndexedAt: time.Now()}
	return f.Save()
}

// Remove deletes a repo from the registry.
func Remove(name string) error {
	f, err := Load()
	if err != nil {
		return err
	}
	if _, ok := f.Repos[name]; !ok {
		return fmt.Errorf("no such repo %q", name)
	}
	delete(f.Repos, name)
	return f.Save()
}

// DefaultName resolves an empty repo selector: the sole repo, or an error
// naming the choices when zero or many are registered.
func (f *File) DefaultName(name string) (string, error) {
	if name != "" {
		if _, ok := f.Repos[name]; !ok {
			return "", fmt.Errorf("unknown repo %q (have %v)", name, f.Names())
		}
		return name, nil
	}
	switch len(f.Repos) {
	case 0:
		return "", fmt.Errorf("no repos indexed; run `gonexus index <path>`")
	case 1:
		return f.Names()[0], nil
	default:
		return "", fmt.Errorf("multiple repos registered; specify repo (one of %v)", f.Names())
	}
}
