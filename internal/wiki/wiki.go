// Package wiki generates architecture documentation from the code graph. The
// structural digest (packages, modules, entry points, interfaces, hot symbols)
// is deterministic and always available; an optional LLM adds narrative prose.
package wiki

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/yourorg/gonexus/internal/graph"
	"github.com/yourorg/gonexus/internal/llm"
)

const (
	topModules = 12
	topEntries = 10
	topIfaces  = 10
	topHot     = 10
)

type entryInfo struct {
	name string
	size int
}
type ifaceInfo struct {
	name         string
	implementors []string
}
type hotInfo struct {
	name    string
	callers int
}

// Digest is the deterministic structural summary of a graph.
type Digest struct {
	Repo                      string
	Packages, Funcs, Types, N int
	Modules                   []graph.Community
	Entries                   []entryInfo
	Interfaces                []ifaceInfo
	Hot                       []hotInfo
}

// Build computes the digest from the graph.
func Build(g *graph.Graph, repo string) Digest {
	d := Digest{Repo: repo, N: len(g.Nodes)}
	for _, n := range g.Nodes {
		switch n.Kind {
		case graph.KindPackage:
			d.Packages++
		case graph.KindFunc, graph.KindMethod:
			d.Funcs++
		case graph.KindType, graph.KindInterface, graph.KindClass:
			d.Types++
		}
	}

	d.Modules = g.Communities()
	if len(d.Modules) > topModules {
		d.Modules = d.Modules[:topModules]
	}

	for _, n := range g.EntryPoints() {
		d.Entries = append(d.Entries, entryInfo{n.Name, g.ProcessSize(n.ID)})
	}
	sort.Slice(d.Entries, func(i, j int) bool { return d.Entries[i].size > d.Entries[j].size })
	if len(d.Entries) > topEntries {
		d.Entries = d.Entries[:topEntries]
	}

	// Interfaces + implementors (incoming implements edges); hot symbols by
	// direct caller count (incoming calls edges).
	for id, n := range g.Nodes {
		_, incoming, _ := g.Context(id)
		if n.Kind == graph.KindInterface {
			var impls []string
			for _, e := range incoming {
				if e.Kind == graph.EdgeImplements {
					if s := g.Nodes[e.From]; s != nil {
						impls = append(impls, s.Name)
					}
				}
			}
			if len(impls) > 0 {
				sort.Strings(impls)
				d.Interfaces = append(d.Interfaces, ifaceInfo{n.Name, impls})
			}
		}
		if n.Kind == graph.KindFunc || n.Kind == graph.KindMethod {
			callers := 0
			for _, e := range incoming {
				if e.Kind == graph.EdgeCalls {
					callers++
				}
			}
			if callers > 0 {
				d.Hot = append(d.Hot, hotInfo{n.Name, callers})
			}
		}
	}
	sort.Slice(d.Interfaces, func(i, j int) bool {
		if len(d.Interfaces[i].implementors) != len(d.Interfaces[j].implementors) {
			return len(d.Interfaces[i].implementors) > len(d.Interfaces[j].implementors)
		}
		return d.Interfaces[i].name < d.Interfaces[j].name
	})
	if len(d.Interfaces) > topIfaces {
		d.Interfaces = d.Interfaces[:topIfaces]
	}
	sort.Slice(d.Hot, func(i, j int) bool {
		if d.Hot[i].callers != d.Hot[j].callers {
			return d.Hot[i].callers > d.Hot[j].callers
		}
		return d.Hot[i].name < d.Hot[j].name
	})
	if len(d.Hot) > topHot {
		d.Hot = d.Hot[:topHot]
	}
	return d
}

// Markdown renders the deterministic wiki.
func (d Digest) Markdown() string {
	var b strings.Builder
	fmt.Fprintf(&b, "# %s — Architecture\n\n", d.Repo)
	fmt.Fprintf(&b, "%d symbols across %d packages: %d functions/methods, %d types.\n\n",
		d.N, d.Packages, d.Funcs, d.Types)

	b.WriteString("## Modules\n\n")
	b.WriteString("Emergent functional clusters (community detection over the call graph):\n\n")
	for _, m := range d.Modules {
		fmt.Fprintf(&b, "- **%s** — %d symbols\n", m.Name, len(m.Members))
	}
	b.WriteString("\n## Entry Points\n\n")
	b.WriteString("Execution-flow roots, by reach:\n\n")
	for _, e := range d.Entries {
		fmt.Fprintf(&b, "- `%s` — reaches %d functions\n", e.name, e.size)
	}
	if len(d.Interfaces) > 0 {
		b.WriteString("\n## Key Interfaces\n\n")
		for _, i := range d.Interfaces {
			fmt.Fprintf(&b, "- `%s` — implemented by %s\n", i.name, strings.Join(i.implementors, ", "))
		}
	}
	if len(d.Hot) > 0 {
		b.WriteString("\n## Most-Called\n\n")
		for _, h := range d.Hot {
			fmt.Fprintf(&b, "- `%s` — %d callers\n", h.name, h.callers)
		}
	}
	return b.String()
}

// Generate returns the wiki markdown. If client is non-nil, an LLM narrative
// overview is prepended to the deterministic structure.
func Generate(ctx context.Context, g *graph.Graph, repo string, client llm.Client) (string, error) {
	d := Build(g, repo)
	structural := d.Markdown()
	if client == nil {
		return structural, nil
	}
	system := "You are a staff engineer writing concise architecture documentation. " +
		"Given a structural digest of a codebase, write a 2-3 paragraph overview of what the " +
		"system does and how it's organized, then a one-line description per module. Output markdown."
	prose, err := client.Complete(ctx, system, structural)
	if err != nil {
		return structural, nil // degrade to structural on LLM failure
	}
	return "# " + repo + " — Overview\n\n" + strings.TrimSpace(prose) + "\n\n---\n\n" + structural, nil
}
