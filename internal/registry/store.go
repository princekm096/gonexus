package registry

import (
	"os"
	"sync"
	"time"

	"github.com/yourorg/gonexus/internal/graph"
)

// Store loads and caches per-repo graphs, reloading from disk when a repo's
// graph.json changes (so a CLI reindex is picked up by a running server).
// Reads the registry file fresh each call, so newly indexed repos appear too.
//
// ponytail: graphs held in memory; evict LRU only if the repo set outgrows RAM.
type Store struct {
	mu    sync.Mutex
	cache map[string]cached // repo name -> loaded graph
}

type cached struct {
	g     *graph.Graph
	mtime time.Time
}

func NewStore() *Store { return &Store{cache: map[string]cached{}} }

// Graph returns the graph for a repo (name may be empty to select the sole
// repo). The resolved name is returned so callers can echo it.
func (s *Store) Graph(name string) (*graph.Graph, string, error) {
	f, err := Load()
	if err != nil {
		return nil, "", err
	}
	name, err = f.DefaultName(name)
	if err != nil {
		return nil, "", err
	}
	gp := GraphPath(f.Repos[name])
	info, err := os.Stat(gp)
	if err != nil {
		return nil, name, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if c, ok := s.cache[name]; ok && !info.ModTime().After(c.mtime) {
		return c.g, name, nil
	}
	g, err := graph.Load(gp)
	if err != nil {
		return nil, name, err
	}
	s.cache[name] = cached{g: g, mtime: info.ModTime()}
	return g, name, nil
}

// Repo resolves a repo name (empty = sole repo) to its registry entry.
func (s *Store) Repo(name string) (Repo, error) {
	f, err := Load()
	if err != nil {
		return Repo{}, err
	}
	name, err = f.DefaultName(name)
	if err != nil {
		return Repo{}, err
	}
	return f.Repos[name], nil
}

// List returns registered repos with their node counts (0 if not yet loadable).
func (s *Store) List() ([]Repo, []int, error) {
	f, err := Load()
	if err != nil {
		return nil, nil, err
	}
	names := f.Names()
	repos := make([]Repo, 0, len(names))
	counts := make([]int, 0, len(names))
	for _, n := range names {
		repos = append(repos, f.Repos[n])
		g, _, err := s.Graph(n)
		if err != nil {
			counts = append(counts, 0)
		} else {
			counts = append(counts, len(g.Nodes))
		}
	}
	return repos, counts, nil
}
