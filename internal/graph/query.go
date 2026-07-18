package graph

import (
	"fmt"
	"regexp"
	"strings"
)

// Check validates ids against the graph: which are missing, and which edges
// dangle (endpoint not a node). Read-only structural validation.
func (g *Graph) Check(ids []string) (missing []string, dangling []Edge) {
	for _, id := range ids {
		if g.Nodes[id] == nil {
			missing = append(missing, id)
		}
	}
	for _, e := range g.Edges {
		// imports/constructs may point at external ids by design; only calls,
		// implements, defines are expected to resolve within the graph.
		if e.Kind == EdgeImports || e.Kind == EdgeConstructs {
			continue
		}
		if g.Nodes[e.From] == nil || g.Nodes[e.To] == nil {
			dangling = append(dangling, e)
		}
	}
	return
}

// single-hop pattern: (a:Kind)-[:EDGE]->(b:Kind); kinds optional.
var cypherRe = regexp.MustCompile(
	`^\(\s*\w+\s*(?::\s*(\w+))?\s*\)\s*-\s*\[\s*:\s*(\w+)\s*\]\s*->\s*\(\s*\w+\s*(?::\s*(\w+))?\s*\)$`)

// Cypher runs a minimal single-hop pattern query and returns matching edges.
// ponytail: a Cypher subset (one hop, kind filters), not a full parser — covers
// "which funcs call which methods" style questions; extend if multi-hop needed.
func (g *Graph) Cypher(pattern string, limit int) ([]Edge, error) {
	m := cypherRe.FindStringSubmatch(strings.TrimSpace(pattern))
	if m == nil {
		return nil, fmt.Errorf("unsupported pattern; expected (a:Kind)-[:EDGE]->(b:Kind)")
	}
	fromKind, edgeKind, toKind := Kind(m[1]), EdgeKind(strings.ToLower(m[2])), Kind(m[3])
	if limit <= 0 {
		limit = 100
	}
	var out []Edge
	for _, e := range g.Edges {
		if e.Kind != edgeKind {
			continue
		}
		if fromKind != "" && (g.Nodes[e.From] == nil || g.Nodes[e.From].Kind != fromKind) {
			continue
		}
		if toKind != "" && (g.Nodes[e.To] == nil || g.Nodes[e.To].Kind != toKind) {
			continue
		}
		out = append(out, e)
		if len(out) >= limit {
			break
		}
	}
	return out, nil
}
