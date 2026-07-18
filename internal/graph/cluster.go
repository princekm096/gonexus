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
	ID       string   `json:"id"`
	Name     string   `json:"name"`
	Members  []string `json:"members"`
	Cohesion float64  `json:"cohesion"` // internal / (internal+boundary) edge weight, 0..1
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

	label := louvain(nodes, adj)

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
		out = append(out, Community{
			ID: lbl, Name: g.communityName(members), Members: members,
			Cohesion: cohesion(members, adj),
		})
	}
	sort.Slice(out, func(i, j int) bool {
		if len(out[i].Members) != len(out[j].Members) {
			return len(out[i].Members) > len(out[j].Members)
		}
		return out[i].ID < out[j].ID
	})
	return out
}

// louvain runs multi-level Louvain: local modularity moves, then aggregate each
// community into a super-node and repeat, until no community merges. Returns the
// final original-node → community label. Deterministic throughout.
func louvain(nodes []string, adj map[string]map[string]int) map[string]string {
	part := make(map[string]string, len(nodes)) // original node -> current community
	for _, n := range nodes {
		part[n] = n
	}
	curNodes, curAdj := nodes, adj
	for {
		labels := louvainMove(curNodes, curAdj)
		distinct := map[string]bool{}
		for _, c := range labels {
			distinct[c] = true
		}
		if len(distinct) == len(curNodes) {
			break // nothing merged this level
		}
		for orig, c := range part {
			part[orig] = labels[c]
		}
		// aggregate communities into super-nodes (self-loops carry internal weight).
		agg := map[string]map[string]int{}
		for u, nbrs := range curAdj {
			cu := labels[u]
			if agg[cu] == nil {
				agg[cu] = map[string]int{}
			}
			for v, w := range nbrs {
				agg[cu][labels[v]] += w
			}
		}
		next := make([]string, 0, len(agg))
		for c := range agg {
			next = append(next, c)
		}
		sort.Strings(next)
		curNodes, curAdj = next, agg
		if len(curNodes) <= 1 {
			break
		}
	}
	return part
}

// cohesion is a community's internal edge weight over its total incident weight
// (1.0 = fully self-contained). adj is the original (undirected, doubled) graph.
func cohesion(members []string, adj map[string]map[string]int) float64 {
	in := map[string]bool{}
	for _, m := range members {
		in[m] = true
	}
	var internal, incident float64
	for _, m := range members {
		for v, w := range adj[m] {
			incident += float64(w)
			if in[v] {
				internal += float64(w)
			}
		}
	}
	if incident == 0 {
		return 0
	}
	return internal / incident
}

// louvainMove runs one level of Louvain modularity optimization: each node
// greedily moves to the neighbor community that maximizes modularity gain,
// iterating to a fixpoint. Deterministic (fixed order, smallest-id tie-break).
// Handles aggregated graphs with self-loops.
func louvainMove(nodes []string, adj map[string]map[string]int) map[string]string {
	deg := map[string]float64{}
	var m2 float64 // sum of weighted degrees = 2m
	for u, nbrs := range adj {
		for _, w := range nbrs {
			deg[u] += float64(w)
			m2 += float64(w)
		}
	}
	com := make(map[string]string, len(nodes))
	comTot := map[string]float64{} // community -> sum of member degrees
	for _, u := range nodes {
		com[u] = u
		comTot[u] += deg[u]
	}
	if m2 == 0 {
		return com
	}

	for iter := 0; iter < clusterMaxIters; iter++ {
		improved := false
		for _, u := range nodes {
			cu := com[u]
			comTot[cu] -= deg[u]
			com[u] = "" // temporarily unassigned

			wTo := map[string]float64{}
			for v, w := range adj[u] {
				if c := com[v]; c != "" {
					wTo[c] += float64(w)
				}
			}
			best, bestGain := cu, wTo[cu]-deg[u]*comTot[cu]/m2
			for c, win := range wTo {
				if gain := win - deg[u]*comTot[c]/m2; gain > bestGain || (gain == bestGain && c < best) {
					best, bestGain = c, gain
				}
			}
			com[u] = best
			comTot[best] += deg[u]
			if best != cu {
				improved = true
			}
		}
		if !improved {
			break
		}
	}
	return com
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
