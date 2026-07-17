// Package rename produces a confidence-scored, multi-file rename plan for a
// symbol and can apply it. It's graph-guided: the graph says which files
// reference the symbol; a whole-word scan finds the occurrences.
//
// ponytail: heuristic (whole-word), not compiler-exact. Confidence = how unique
// the name is in the graph, so an agent can review before applying. For
// compiler-verified Go renames, delegate to gopls — that's the upgrade path.
package rename

import (
	"fmt"
	"os"
	"regexp"
	"sort"

	"github.com/yourorg/gonexus/internal/graph"
)

// Edit is one file the rename touches.
type Edit struct {
	File  string `json:"file"`
	Lines []int  `json:"lines"` // 1-based lines with a whole-word match
}

// Result is a rename plan (and whether it was applied).
type Result struct {
	OldName    string  `json:"oldName"`
	NewName    string  `json:"newName"`
	Edits      []Edit  `json:"edits"`
	Occurrences int    `json:"occurrences"`
	Confidence float64 `json:"confidence"` // 1.0 when the name is unique in the repo
	Applied    bool    `json:"applied"`
}

var identRe = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

// Plan builds the rename plan for symbol id -> newName. If apply is true, the
// edits are written to disk.
func Plan(g *graph.Graph, id, newName string, apply bool) (*Result, error) {
	n := g.Nodes[id]
	if n == nil {
		return nil, fmt.Errorf("unknown symbol: %s", id)
	}
	old := n.Name
	if old == "" {
		return nil, fmt.Errorf("symbol %s has no name to rename", id)
	}
	if !identRe.MatchString(newName) {
		return nil, fmt.Errorf("invalid new name: %q", newName)
	}
	if newName == old {
		return nil, fmt.Errorf("new name equals old name")
	}

	// Confidence: unique name -> whole-word matches are safe (1.0); otherwise
	// downweight by how many symbols share the name.
	nameCount := 0
	for _, m := range g.Nodes {
		if m.Name == old {
			nameCount++
		}
	}
	conf := 1.0
	if nameCount > 1 {
		conf = 1.0 / float64(nameCount)
	}

	files := referenceFiles(g, n)
	word := regexp.MustCompile(`\b` + regexp.QuoteMeta(old) + `\b`)

	var edits []Edit
	total := 0
	for _, f := range files {
		src, err := os.ReadFile(f)
		if err != nil {
			continue // file gone/unreadable; skip
		}
		lines := matchLines(string(src), word)
		if len(lines) == 0 {
			continue
		}
		total += len(lines)
		edits = append(edits, Edit{File: f, Lines: lines})
		if apply {
			if err := os.WriteFile(f, word.ReplaceAll(src, []byte(newName)), 0o644); err != nil {
				return nil, fmt.Errorf("apply %s: %w", f, err)
			}
		}
	}
	sort.Slice(edits, func(i, j int) bool { return edits[i].File < edits[j].File })

	return &Result{
		OldName: old, NewName: newName, Edits: edits,
		Occurrences: total, Confidence: conf, Applied: apply,
	}, nil
}

// referenceFiles is the set of files that may reference the symbol: its own
// file, the files of everything with an edge into it, and (for types) every
// file in its package, since intra-package type uses aren't all edges.
func referenceFiles(g *graph.Graph, n *graph.Node) []string {
	set := map[string]bool{}
	if n.File != "" {
		set[n.File] = true
	}
	_, incoming, _ := g.Context(n.ID)
	for _, e := range incoming {
		if src := g.Nodes[e.From]; src != nil && src.File != "" {
			set[src.File] = true
		}
	}
	if n.Kind == graph.KindType || n.Kind == graph.KindInterface || n.Kind == graph.KindClass {
		for _, m := range g.Nodes {
			if m.Package == n.Package && m.File != "" {
				set[m.File] = true
			}
		}
	}
	out := make([]string, 0, len(set))
	for f := range set {
		out = append(out, f)
	}
	sort.Strings(out)
	return out
}

// matchLines returns the 1-based line numbers of src that contain a match.
func matchLines(src string, re *regexp.Regexp) []int {
	var out []int
	line := 1
	start := 0
	for i := 0; i < len(src); i++ {
		if src[i] == '\n' {
			if re.MatchString(src[start:i]) {
				out = append(out, line)
			}
			line++
			start = i + 1
		}
	}
	if start < len(src) && re.MatchString(src[start:]) {
		out = append(out, line)
	}
	return out
}
