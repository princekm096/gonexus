package graph

import "sort"

// Process tracing: entry points and the execution flows rooted at them, derived
// purely from the call graph.
//
// ponytail: an entry point is a call root (no incoming calls edge) that calls
// something — main, request handlers, CLI commands. No framework-specific
// allowlist; topology names them.

func (g *Graph) hasCallEdge(adj map[string][]Edge, id string) bool {
	for _, e := range adj[id] {
		if e.Kind == EdgeCalls {
			return true
		}
	}
	return false
}

// EntryPoints returns func/method nodes that root an execution flow, sorted by
// id. A root is called by nothing in the graph but calls something.
func (g *Graph) EntryPoints() []*Node {
	var out []*Node
	for id, n := range g.Nodes {
		if n.Kind != KindFunc && n.Kind != KindMethod {
			continue
		}
		if g.hasCallEdge(g.in, id) { // something calls it -> not a root
			continue
		}
		if !g.hasCallEdge(g.out, id) { // calls nothing -> not a flow
			continue
		}
		out = append(out, n)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

// reachableNodes is the set of indexed nodes transitively called from entryID
// (call targets in external packages aren't indexed, so they're excluded — the
// flow is through this codebase).
func (g *Graph) reachableNodes(entryID string) map[string]bool {
	set := map[string]bool{}
	if _, ok := g.Nodes[entryID]; ok {
		set[entryID] = true
	}
	for _, id := range g.Dependencies(entryID) {
		if _, ok := g.Nodes[id]; ok {
			set[id] = true
		}
	}
	return set
}

// Process returns the execution flow rooted at entryID: the transitively-called
// indexed nodes plus the calls edges among them (the call tree, for a flow view).
func (g *Graph) Process(entryID string) ([]*Node, []Edge) {
	set := g.reachableNodes(entryID)
	if len(set) == 0 {
		return nil, nil
	}
	nodes := make([]*Node, 0, len(set))
	for id := range set {
		nodes = append(nodes, g.Nodes[id])
	}
	sort.Slice(nodes, func(i, j int) bool { return nodes[i].ID < nodes[j].ID })

	// Dedup: the graph keeps one calls edge per call site, so f->g can repeat.
	// A flow view wants each call relationship once.
	var edges []Edge
	seen := map[Edge]bool{}
	for _, e := range g.Edges {
		if e.Kind == EdgeCalls && set[e.From] && set[e.To] && !seen[e] {
			seen[e] = true
			edges = append(edges, e)
		}
	}
	return nodes, edges
}

// ProcessSize is the number of indexed nodes in the flow rooted at entryID
// (including the entry). Used to rank entry points by reach.
func (g *Graph) ProcessSize(entryID string) int {
	return len(g.reachableNodes(entryID))
}

// ProcessesOf returns the entry points whose flow reaches id — the processes a
// symbol participates in (e.g. "is this function on a request path?").
func (g *Graph) ProcessesOf(id string) []*Node {
	callers := map[string]bool{}
	for _, c := range g.Impact(id) {
		callers[c] = true
	}
	var out []*Node
	for _, e := range g.EntryPoints() {
		if callers[e.ID] || e.ID == id {
			out = append(out, e)
		}
	}
	return out
}
