package index

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/yourorg/gonexus/internal/analysis"
	"github.com/yourorg/gonexus/internal/embed"
	"github.com/yourorg/gonexus/internal/graph"
	"github.com/yourorg/gonexus/internal/registry"
)

// IndexAndRegister builds a repo's graph (incrementally), persists it to the
// repo's own .gonexus/graph.json, and records it in the global registry.
// name defaults to the directory base name. Returns the resolved name + counts.
func IndexAndRegister(path, name string) (string, int, int, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", 0, 0, err
	}
	if name == "" {
		name = filepath.Base(abs)
	}
	if strings.ContainsAny(name, "/\\") {
		return "", 0, 0, fmt.Errorf("invalid repo name %q", name)
	}

	cacheDir := registry.CacheDir(abs)
	g, err := BuildRepo(abs, cacheDir)
	if err != nil {
		return "", 0, 0, err
	}
	embedNodes(g)
	if err := os.MkdirAll(cacheDir, 0o755); err != nil {
		return "", 0, 0, err
	}
	if err := g.Save(filepath.Join(cacheDir, "graph.json")); err != nil {
		return "", 0, 0, err
	}
	if err := registry.Add(name, abs); err != nil {
		return "", 0, 0, err
	}
	buildPDG(abs, cacheDir)
	return name, len(g.Nodes), len(g.Edges), nil
}

// buildPDG runs the heavy SSA analysis (CFG/data-dependence + taint) and
// persists it — only when GONEXUS_PDG=1 and the repo has Go. Failures are
// logged, never fatal.
func buildPDG(dir, cacheDir string) {
	if os.Getenv("GONEXUS_PDG") != "1" || !hasGo(dir) {
		return
	}
	res, err := analysis.BuildGoPDG(dir)
	if err != nil {
		log.Printf("gonexus: pdg skipped: %v", err)
		return
	}
	_ = res.Save(filepath.Join(cacheDir, "pdg.json"))
}

// embedNodes fills each node's Vector when an embedder is configured; a no-op
// (BM25-only) otherwise. Embedding failure is logged, not fatal — the index is
// still fully usable via BM25.
func embedNodes(g *graph.Graph) {
	emb := embed.FromEnv()
	if emb == nil {
		return
	}
	ids := make([]string, 0, len(g.Nodes))
	for id := range g.Nodes {
		ids = append(ids, id)
	}
	// GONEXUS_EMBED_MAX_NODES caps how many nodes get vectors (embedding cost);
	// the most-referenced symbols are prioritized. Uncapped nodes stay BM25-only.
	if cap := envInt("GONEXUS_EMBED_MAX_NODES"); cap > 0 && len(ids) > cap {
		indeg := map[string]int{}
		for _, e := range g.Edges {
			indeg[e.To]++
		}
		sort.Slice(ids, func(i, j int) bool { return indeg[ids[i]] > indeg[ids[j]] })
		ids = ids[:cap]
	}
	texts := make([]string, 0, len(ids))
	for _, id := range ids {
		texts = append(texts, graph.NodeText(g.Nodes[id]))
	}
	vecs, err := emb.Embed(context.Background(), texts)
	if err != nil {
		log.Printf("gonexus: embedding skipped: %v", err)
		return
	}
	for i, id := range ids {
		g.Nodes[id].Vector = graph.Normalize(vecs[i])
	}
}

func envInt(key string) int {
	n, _ := strconv.Atoi(os.Getenv(key))
	return n
}
