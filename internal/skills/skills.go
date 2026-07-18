// Package skills generates agent skill files (markdown) for a repo: six
// standard workflow skills plus one dynamic skill per detected module.
package skills

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/yourorg/gonexus/internal/graph"
)

// standard skills: name -> markdown body (frontmatter added on write).
var standard = map[string]string{
	"exploring": "Use `repos` then `query` to find symbols, `context` for a 360° view, and " +
		"`clusters` to learn the module structure. Prefer these over grepping the tree.",
	"debugging": "Given a failing symbol, use `context` for its callers/callees, `trace` to find " +
		"the path from an entry point, and `explain` for taint findings if the repo was indexed with --pdg.",
	"impact-analysis": "Before editing a symbol, call `impact` for its blast radius. Before committing, " +
		"call `detect_changes`. For API routes, use `api_impact`.",
	"refactoring": "Use `impact` to scope the change, then `rename` (plan first, check confidence, then apply). " +
		"Re-run `detect_changes` afterwards to confirm the blast radius.",
	"guide": "GoNexus exposes a code knowledge graph. Discover tools with `tool_map`; every query tool takes " +
		"an optional `repo`. Start from `repos` and `entrypoints`.",
	"cli": "The `gonexus` CLI: `index <path>`, `list`, `status`, `wiki`, `serve`, `mcp`, and `group ...`. " +
		"Re-index when `status` shows STALE.",
}

// Generate writes skill files for repo into outDir. Returns the files written.
func Generate(g *graph.Graph, repo, outDir string) ([]string, error) {
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return nil, err
	}
	var written []string
	write := func(name, title, body string) error {
		path := filepath.Join(outDir, name+".md")
		content := fmt.Sprintf("---\nname: %s\nrepo: %s\n---\n\n# %s\n\n%s\n", name, repo, title, body)
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			return err
		}
		written = append(written, path)
		return nil
	}

	for name, body := range standard {
		title := strings.ToUpper(name[:1]) + name[1:]
		if err := write(name, title, body); err != nil {
			return nil, err
		}
	}

	// dynamic: one skill per module (community), listing representative members.
	for _, c := range g.Communities() {
		names := make([]string, 0, len(c.Members))
		for _, m := range c.Members {
			if n := g.Nodes[m]; n != nil {
				names = append(names, n.Name)
			}
			if len(names) >= 12 {
				break
			}
		}
		body := fmt.Sprintf("Module **%s** (%d symbols). Key members: %s.\n\n"+
			"Use `query` scoped to these names and `process`/`impact` to work within this module.",
			c.Name, len(c.Members), strings.Join(names, ", "))
		if err := write("area-"+safe(c.Name), "Area: "+c.Name, body); err != nil {
			return nil, err
		}
	}
	return written, nil
}

// safe makes a filename-safe slug.
func safe(s string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(s) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
		default:
			b.WriteRune('-')
		}
	}
	return strings.Trim(b.String(), "-")
}
