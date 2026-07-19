// Package changes maps a git diff to the symbols it touches and their blast
// radius, for pre-commit / PR review ("what does this change affect?").
package changes

import (
	"bufio"
	"fmt"
	"math"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/yourorg/gonexus/internal/graph"
)

// Result is the outcome of Detect.
type Result struct {
	Changed  []*graph.Node // symbols whose definition/body changed
	Impacted []string      // transitive callers of changed symbols (blast radius)
}

type lineRange struct{ start, end int }

// Detect diffs repoPath (working tree vs HEAD, or vs base if given), maps the
// changed lines to symbols, and computes their blast radius.
func Detect(g *graph.Graph, repoPath, base string) (*Result, error) {
	diff, err := gitDiff(repoPath, base)
	if err != nil {
		return nil, err
	}
	ranges := parseUnifiedDiff(diff) // relpath -> changed new-side ranges

	changed := map[string]bool{}
	for rel, rs := range ranges {
		abs := filepath.Join(repoPath, rel)
		for _, id := range symbolsForRanges(g, abs, rs) {
			changed[id] = true
		}
	}

	impacted := map[string]bool{}
	for id := range changed {
		for _, c := range g.Impact(id) {
			if !changed[c] {
				impacted[c] = true
			}
		}
	}

	res := &Result{}
	for id := range changed {
		if n := g.Nodes[id]; n != nil {
			res.Changed = append(res.Changed, n)
		}
	}
	sort.Slice(res.Changed, func(i, j int) bool { return res.Changed[i].ID < res.Changed[j].ID })
	for id := range impacted {
		res.Impacted = append(res.Impacted, id)
	}
	sort.Strings(res.Impacted)
	return res, nil
}

func gitDiff(repoPath, base string) (string, error) {
	ref := base
	if ref == "" {
		ref = "HEAD"
	}
	// Reject option-injection: `base` must be a ref name, not a git flag.
	if strings.HasPrefix(ref, "-") {
		return "", fmt.Errorf("invalid base ref %q", ref)
	}
	// `--` terminates options so the ref can't be reinterpreted as one.
	out, err := exec.Command("git", "-C", repoPath, "diff", "--unified=0", ref, "--").Output()
	if err != nil {
		// git diff HEAD fails on a repo with no commits; treat as no diff.
		return "", nil
	}
	return string(out), nil
}

// parseUnifiedDiff extracts changed new-side line ranges per file from
// `git diff --unified=0` output. Deleted files are skipped.
func parseUnifiedDiff(diff string) map[string][]lineRange {
	out := map[string][]lineRange{}
	var cur string
	sc := bufio.NewScanner(strings.NewReader(diff))
	sc.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	for sc.Scan() {
		line := sc.Text()
		switch {
		case strings.HasPrefix(line, "+++ "):
			p := strings.TrimPrefix(line, "+++ ")
			if p == "/dev/null" {
				cur = ""
				continue
			}
			cur = strings.TrimPrefix(p, "b/")
		case strings.HasPrefix(line, "@@ ") && cur != "":
			if r, ok := parseHunkNewRange(line); ok {
				out[cur] = append(out[cur], r)
			}
		}
	}
	return out
}

// parseHunkNewRange reads the "+start,len" part of a hunk header
// "@@ -a,b +start,len @@". len defaults to 1 when omitted.
func parseHunkNewRange(h string) (lineRange, bool) {
	i := strings.Index(h, "+")
	if i < 0 {
		return lineRange{}, false
	}
	rest := h[i+1:]
	if j := strings.IndexByte(rest, ' '); j >= 0 {
		rest = rest[:j]
	}
	start, length := rest, "1"
	if c := strings.IndexByte(rest, ','); c >= 0 {
		start, length = rest[:c], rest[c+1:]
	}
	s, err1 := strconv.Atoi(start)
	l, err2 := strconv.Atoi(length)
	if err1 != nil || err2 != nil {
		return lineRange{}, false
	}
	if l == 0 { // pure deletion at position s
		return lineRange{s, s}, true
	}
	return lineRange{s, s + l - 1}, true
}

// symbolsForRanges returns the ids of symbol nodes in absFile whose span
// (declaration line up to the next symbol's line) intersects a changed range.
func symbolsForRanges(g *graph.Graph, absFile string, ranges []lineRange) []string {
	var nodes []*graph.Node
	for _, n := range g.Nodes {
		if n.File == absFile && n.Line > 0 && isSymbol(n.Kind) {
			nodes = append(nodes, n)
		}
	}
	sort.Slice(nodes, func(i, j int) bool { return nodes[i].Line < nodes[j].Line })

	var out []string
	for i, n := range nodes {
		lo := n.Line
		hi := math.MaxInt
		if i+1 < len(nodes) {
			hi = nodes[i+1].Line - 1
		}
		for _, r := range ranges {
			if r.start <= hi && r.end >= lo {
				out = append(out, n.ID)
				break
			}
		}
	}
	return out
}

func isSymbol(k graph.Kind) bool {
	switch k {
	case graph.KindFunc, graph.KindMethod, graph.KindType, graph.KindInterface,
		graph.KindClass, graph.KindComponent, graph.KindVar, graph.KindConst:
		return true
	}
	return false
}
