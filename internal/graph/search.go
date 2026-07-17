package graph

import (
	"math"
	"sort"
	"strings"
	"unicode"
)

// Hybrid search: BM25 lexical ranking (always) fused with semantic cosine
// ranking (when node/query vectors exist) via Reciprocal Rank Fusion.
//
// ponytail: BM25 is pure Go, no deps, no model — it replaces the old substring
// ranker outright. Embeddings are optional; without them SearchHybrid degrades
// cleanly to BM25.

const (
	bm25K1 = 1.2
	bm25B  = 0.75
	rrfK   = 60.0 // RRF damping; standard default
	fuseN  = 200  // candidates pulled from each ranker before fusion
)

// SearchHybrid ranks nodes for q. If qvec is non-nil and nodes carry vectors,
// BM25 and cosine rankings are fused with RRF; otherwise it's BM25 alone.
func (g *Graph) SearchHybrid(q string, limit int, qvec []float32) []*Node {
	if limit <= 0 {
		limit = 20
	}
	lexical := g.bm25Search(q, fuseN)
	if qvec == nil {
		return g.take(lexical, limit)
	}
	semantic := g.vectorSearch(qvec, fuseN)
	return g.take(rrf(lexical, semantic), limit)
}

func (g *Graph) take(ids []string, limit int) []*Node {
	out := make([]*Node, 0, limit)
	for _, id := range ids {
		if n := g.Nodes[id]; n != nil {
			out = append(out, n)
			if len(out) >= limit {
				break
			}
		}
	}
	return out
}

// ---- BM25 ----

type bm25Index struct {
	ids    []string           // doc index -> node id
	tf     []map[string]int   // per-doc term frequencies
	length []int              // per-doc token count
	df     map[string]int     // term -> #docs containing it
	avgLen float64
	n      int
}

// nodeText is the searchable text for a node.
func nodeText(n *Node) string {
	return strings.Join([]string{n.Name, n.Package, n.Signature, n.Doc, n.ID}, " ")
}

// NodeText is the text an embedder should encode for a node.
func NodeText(n *Node) string { return nodeText(n) }

func (g *Graph) bmIndex() *bm25Index {
	g.bm25Once.Do(func() {
		idx := &bm25Index{df: map[string]int{}}
		var totalLen int
		for id, n := range g.Nodes {
			toks := tokenize(nodeText(n))
			tf := map[string]int{}
			for _, t := range toks {
				tf[t]++
			}
			for t := range tf {
				idx.df[t]++
			}
			idx.ids = append(idx.ids, id)
			idx.tf = append(idx.tf, tf)
			idx.length = append(idx.length, len(toks))
			totalLen += len(toks)
		}
		idx.n = len(idx.ids)
		if idx.n > 0 {
			idx.avgLen = float64(totalLen) / float64(idx.n)
		}
		g.bm25 = idx
	})
	return g.bm25
}

// bm25Search returns up to k node ids ranked by BM25 score (desc).
func (g *Graph) bm25Search(q string, k int) []string {
	idx := g.bmIndex()
	terms := tokenize(q)
	if len(terms) == 0 || idx.n == 0 {
		return nil
	}
	type scored struct {
		id string
		s  float64
	}
	var hits []scored
	for d := 0; d < idx.n; d++ {
		var score float64
		dl := float64(idx.length[d])
		for _, t := range terms {
			f := idx.tf[d][t]
			if f == 0 {
				continue
			}
			df := idx.df[t]
			idf := math.Log(1 + (float64(idx.n)-float64(df)+0.5)/(float64(df)+0.5))
			num := float64(f) * (bm25K1 + 1)
			den := float64(f) + bm25K1*(1-bm25B+bm25B*dl/idx.avgLen)
			score += idf * num / den
		}
		if score > 0 {
			hits = append(hits, scored{idx.ids[d], score})
		}
	}
	sort.Slice(hits, func(i, j int) bool {
		if hits[i].s != hits[j].s {
			return hits[i].s > hits[j].s
		}
		return hits[i].id < hits[j].id // stable tie-break
	})
	out := make([]string, 0, k)
	for i, h := range hits {
		if i >= k {
			break
		}
		out = append(out, h.id)
	}
	return out
}

// tokenize lowercases, splits on non-alphanumerics, and also splits camelCase /
// PascalCase identifiers, keeping the whole identifier too. So "BuildRepo"
// yields "buildrepo", "build", "repo".
func tokenize(s string) []string {
	var out []string
	for _, field := range strings.FieldsFunc(s, func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsNumber(r)
	}) {
		lower := strings.ToLower(field)
		out = append(out, lower)
		if parts := splitCamel(field); len(parts) > 1 {
			for _, p := range parts {
				out = append(out, strings.ToLower(p))
			}
		}
	}
	return out
}

func splitCamel(s string) []string {
	var parts []string
	var cur strings.Builder
	rs := []rune(s)
	for i, r := range rs {
		if i > 0 && unicode.IsUpper(r) && (unicode.IsLower(rs[i-1]) ||
			(i+1 < len(rs) && unicode.IsLower(rs[i+1]))) {
			if cur.Len() > 0 {
				parts = append(parts, cur.String())
				cur.Reset()
			}
		}
		cur.WriteRune(r)
	}
	if cur.Len() > 0 {
		parts = append(parts, cur.String())
	}
	return parts
}

// ---- semantic ----

// vectorSearch returns up to k node ids ranked by cosine similarity to qvec.
// Node vectors are stored normalized, so cosine is a dot product; qvec is
// normalized here defensively.
func (g *Graph) vectorSearch(qvec []float32, k int) []string {
	q := normalize(qvec)
	type scored struct {
		id string
		s  float64
	}
	var hits []scored
	for id, n := range g.Nodes {
		if len(n.Vector) != len(q) || len(q) == 0 {
			continue
		}
		hits = append(hits, scored{id, dot(q, n.Vector)})
	}
	sort.Slice(hits, func(i, j int) bool {
		if hits[i].s != hits[j].s {
			return hits[i].s > hits[j].s
		}
		return hits[i].id < hits[j].id
	})
	out := make([]string, 0, k)
	for i, h := range hits {
		if i >= k {
			break
		}
		out = append(out, h.id)
	}
	return out
}

// Normalize returns a unit-length copy of v (exported for the indexer to store
// normalized node vectors).
func Normalize(v []float32) []float32 { return normalize(v) }

func normalize(v []float32) []float32 {
	var sum float64
	for _, x := range v {
		sum += float64(x) * float64(x)
	}
	if sum == 0 {
		return v
	}
	inv := float32(1 / math.Sqrt(sum))
	out := make([]float32, len(v))
	for i, x := range v {
		out[i] = x * inv
	}
	return out
}

func dot(a, b []float32) float64 {
	var s float64
	for i := range a {
		s += float64(a[i]) * float64(b[i])
	}
	return s
}

// ---- fusion ----

// rrf fuses ranked id lists by Reciprocal Rank Fusion: an id's score is the sum
// over lists of 1/(k + rank). Higher is better.
func rrf(lists ...[]string) []string {
	score := map[string]float64{}
	for _, list := range lists {
		for rank, id := range list {
			score[id] += 1.0 / (rrfK + float64(rank))
		}
	}
	ids := make([]string, 0, len(score))
	for id := range score {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool {
		if score[ids[i]] != score[ids[j]] {
			return score[ids[i]] > score[ids[j]]
		}
		return ids[i] < ids[j]
	})
	return ids
}
