// Package graph is the in-memory code knowledge graph plus the read queries
// that power GoNexus (context / impact / trace / search).
//
// ponytail: in-memory + JSON persist. Impact is reverse-edge BFS, trace is BFS,
// context is a map lookup — all cheap. Swap the store for an embedded graph DB
// (SQLite/Ladybug) when a repo no longer fits in RAM.
package graph

import (
	"encoding/json"
	"os"
	"sort"
	"sync"
)

// Kind enumerates node types. Keep flat; add as language coverage grows.
type Kind string

const (
	KindPackage   Kind = "package"
	KindFile      Kind = "file"
	KindFunc      Kind = "func"
	KindMethod    Kind = "method"
	KindType      Kind = "type"
	KindInterface Kind = "interface"
	KindVar       Kind = "var"
	KindConst     Kind = "const"
	KindClass     Kind = "class"     // TS/JS
	KindComponent Kind = "component" // Vue SFC
)

// EdgeKind enumerates relationships.
type EdgeKind string

const (
	EdgeDefines    EdgeKind = "defines"    // container -> member
	EdgeCalls      EdgeKind = "calls"      // func -> func
	EdgeImports    EdgeKind = "imports"    // package -> package
	EdgeImplements EdgeKind = "implements" // type -> interface
	EdgeReferences EdgeKind = "references" // symbol -> type used
)

type Node struct {
	ID        string `json:"id"` // stable: pkgpath.Recv.Name
	Kind      Kind   `json:"kind"`
	Name      string `json:"name"`
	Package   string `json:"package"`
	File      string `json:"file"`
	Line      int    `json:"line"`
	Signature string `json:"signature,omitempty"`
	Doc       string `json:"doc,omitempty"`
	// Vector is an optional normalized embedding of the node's text, set at
	// index time when an embedder is configured; powers semantic search.
	Vector []float32 `json:"vector,omitempty"`
}

type Edge struct {
	From string   `json:"from"`
	To   string   `json:"to"`
	Kind EdgeKind `json:"kind"`
}

type Graph struct {
	Nodes map[string]*Node `json:"nodes"`
	Edges []Edge           `json:"edges"`

	// adjacency, rebuilt on load; not serialized.
	out map[string][]Edge `json:"-"`
	in  map[string][]Edge `json:"-"`

	// BM25 index, built lazily on first search and reused.
	bm25     *bm25Index
	bm25Once sync.Once

	// Detected communities, built lazily on first call.
	clusters    []Community
	clusterOnce sync.Once
}

func New() *Graph {
	return &Graph{Nodes: map[string]*Node{}, out: map[string][]Edge{}, in: map[string][]Edge{}}
}

func (g *Graph) AddNode(n *Node) {
	if n.ID == "" {
		return
	}
	if _, ok := g.Nodes[n.ID]; !ok {
		g.Nodes[n.ID] = n
	}
}

func (g *Graph) AddEdge(e Edge) {
	if e.From == "" || e.To == "" || e.From == e.To {
		return
	}
	g.Edges = append(g.Edges, e)
	g.out[e.From] = append(g.out[e.From], e)
	g.in[e.To] = append(g.in[e.To], e)
}

// Merge folds other's nodes and edges into g (used to combine per-language
// extractors — e.g. a Go backend and a Vue frontend in one repo).
func (g *Graph) Merge(other *Graph) {
	for _, n := range other.Nodes {
		g.AddNode(n)
	}
	for _, e := range other.Edges {
		g.AddEdge(e)
	}
}

// index rebuilds adjacency maps (after JSON load).
func (g *Graph) index() {
	g.out = map[string][]Edge{}
	g.in = map[string][]Edge{}
	for _, e := range g.Edges {
		g.out[e.From] = append(g.out[e.From], e)
		g.in[e.To] = append(g.in[e.To], e)
	}
}

// ---- persistence ----

func (g *Graph) Save(path string) error {
	b, err := json.Marshal(g)
	if err != nil {
		return err
	}
	return os.WriteFile(path, b, 0o644)
}

func Load(path string) (*Graph, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	g := New()
	if err := json.Unmarshal(b, g); err != nil {
		return nil, err
	}
	g.index()
	return g, nil
}

// ---- queries ----

// Context returns a symbol with its incoming and outgoing edges (360° view).
func (g *Graph) Context(id string) (node *Node, incoming, outgoing []Edge) {
	return g.Nodes[id], g.in[id], g.out[id]
}

// Impact returns the transitive set of callers of id (blast radius): who breaks
// if id changes. BFS over reverse "calls" edges.
func (g *Graph) Impact(id string) []string {
	return g.reachable(id, g.in, EdgeCalls)
}

// Dependencies returns what id transitively calls (forward "calls" edges).
func (g *Graph) Dependencies(id string) []string {
	return g.reachable(id, g.out, EdgeCalls)
}

func (g *Graph) reachable(start string, adj map[string][]Edge, kind EdgeKind) []string {
	seen := map[string]bool{start: true}
	queue := []string{start}
	var out []string
	for len(queue) > 0 {
		cur := queue[0]
		queue = queue[1:]
		for _, e := range adj[cur] {
			if e.Kind != kind {
				continue
			}
			// in-map holds edges where e.To==cur (neighbor is e.From);
			// out-map holds edges where e.From==cur (neighbor is e.To).
			next := e.To
			if e.To == cur {
				next = e.From
			}
			if !seen[next] {
				seen[next] = true
				out = append(out, next)
				queue = append(queue, next)
			}
		}
	}
	sort.Strings(out)
	return out
}

// Trace returns the shortest call path from -> to (inclusive), or nil.
func (g *Graph) Trace(from, to string) []string {
	if from == to {
		return []string{from}
	}
	prev := map[string]string{}
	seen := map[string]bool{from: true}
	queue := []string{from}
	for len(queue) > 0 {
		cur := queue[0]
		queue = queue[1:]
		for _, e := range g.out[cur] {
			if e.Kind != EdgeCalls {
				continue
			}
			if !seen[e.To] {
				seen[e.To] = true
				prev[e.To] = cur
				if e.To == to {
					return rebuild(prev, from, to)
				}
				queue = append(queue, e.To)
			}
		}
	}
	return nil
}

func rebuild(prev map[string]string, from, to string) []string {
	var path []string
	for cur := to; cur != ""; cur = prev[cur] {
		path = append([]string{cur}, path...)
		if cur == from {
			break
		}
	}
	return path
}

// Search ranks nodes for q with BM25 (see search.go). SearchHybrid adds
// semantic reranking when a query vector is supplied.
func (g *Graph) Search(q string, limit int) []*Node {
	return g.SearchHybrid(q, limit, nil)
}
