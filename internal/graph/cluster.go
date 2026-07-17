package graph

import (
	"path"
	"sort"
	"strings"
)

// Community detection groups functions into emergent logical modules, using
// Label Propagation over the undirected call+implements graph (multi-edges act
// as weights). Unlike grouping by package, this finds cross-package functional
// clusters.
//
// ponytail: Label Propagation — deterministic (fixed node order, smallest-label
// tie-break), near-linear, no parameters. Swap for Louvain if modularity
// quality ever matters.

const clusterMaxIters = 20

// Community is an emergent module: a set of related nodes with a name derived
// from their dominant package.
type Community struct {
	ID      string   `json:"id"`
	Name    string   `json:"name"`
	Members []string `json:"members"`
}

// Communities returns detected communities of size >= 2, sorted largest first.
// Cached on first call.
func (g *Graph) Communities() []Community {
	g.clusterOnce.Do(func() { g.clusters = g.detectCommunities() })
	return g.clusters
}

func (g *Graph) detectCommunities() []Community {
	// Undirected weighted adjacency over calls + implements edges.
	adj := map[string]map[string]int{}
	link := func(a, b string) {
		if a == b {
			return
		}
		if adj[a] == nil {
			adj[a] = map[string]int{}
		}
		adj[a][b]++
	}
	for _, e := range g.Edges {
		if e.Kind != EdgeCalls && e.Kind != EdgeImplements {
			continue
		}
		if g.Nodes[e.From] == nil || g.Nodes[e.To] == nil {
			continue // skip edges to external (unindexed) targets
		}
		link(e.From, e.To)
		link(e.To, e.From)
	}

	nodes := make([]string, 0, len(adj))
	for id := range adj {
		nodes = append(nodes, id)
	}
	sort.Strings(nodes) // deterministic sweep order

	label := make(map[string]string, len(nodes))
	for _, id := range nodes {
		label[id] = id
	}

	// Asynchronous label propagation: each node adopts the highest-weighted
	// neighbor label; ties break to the smallest label for reproducibility.
	for iter := 0; iter < clusterMaxIters; iter++ {
		changed := false
		for _, u := range nodes {
			tally := map[string]int{}
			for v, w := range adj[u] {
				tally[label[v]] += w
			}
			best, bestW := label[u], -1
			for lbl, w := range tally {
				if w > bestW || (w == bestW && lbl < best) {
					best, bestW = lbl, w
				}
			}
			if best != label[u] {
				label[u] = best
				changed = true
			}
		}
		if !changed {
			break
		}
	}

	// Group by final label.
	groups := map[string][]string{}
	for _, id := range nodes {
		groups[label[id]] = append(groups[label[id]], id)
	}

	var out []Community
	for lbl, members := range groups {
		if len(members) < 2 {
			continue // singletons aren't modules
		}
		sort.Strings(members)
		out = append(out, Community{ID: lbl, Name: g.communityName(members), Members: members})
	}
	sort.Slice(out, func(i, j int) bool {
		if len(out[i].Members) != len(out[j].Members) {
			return len(out[i].Members) > len(out[j].Members)
		}
		return out[i].ID < out[j].ID
	})
	return out
}

// communityName is the most common package among members (base segment); ties
// break alphabetically. Falls back to the first member's name.
func (g *Graph) communityName(members []string) string {
	count := map[string]int{}
	for _, id := range members {
		if n := g.Nodes[id]; n != nil && n.Package != "" {
			count[n.Package]++
		}
	}
	best, bestN := "", 0
	for pkg, c := range count {
		if c > bestN || (c == bestN && pkg < best) {
			best, bestN = pkg, c
		}
	}
	if best != "" {
		if strings.Contains(best, "/") {
			return path.Base(best)
		}
		return best
	}
	if n := g.Nodes[members[0]]; n != nil {
		return n.Name
	}
	return members[0]
}
