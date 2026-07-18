// Package mcp exposes the GoNexus graph queries as MCP tools over stdio, so
// coding agents (Claude Code, Cursor, Codex) get precomputed code context
// instead of making many exploratory calls.
//
// Thin adapter: tools resolve a repo's graph from the registry and call
// internal/graph queries directly, no gRPC round-trip.
package mcp

import (
	"context"
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/yourorg/gonexus/internal/changes"
	"github.com/yourorg/gonexus/internal/embed"
	"github.com/yourorg/gonexus/internal/graph"
	"path/filepath"

	"github.com/yourorg/gonexus/internal/analysis"
	"github.com/yourorg/gonexus/internal/groups"
	"github.com/yourorg/gonexus/internal/index"
	"github.com/yourorg/gonexus/internal/llm"
	"github.com/yourorg/gonexus/internal/registry"
	"github.com/yourorg/gonexus/internal/rename"
	"github.com/yourorg/gonexus/internal/wiki"
)

type store struct {
	repos *registry.Store
	emb   embed.Embedder // nil unless GONEXUS_EMBED_URL is set
	llm   llm.Client     // nil unless GONEXUS_LLM_URL is set
	guard guardConfig
}

// Serve runs the MCP server on stdio. Repos come from ~/.gonexus/registry.json
// and are loaded (and mtime-refreshed) on demand.
func Serve() error {
	st := &store{repos: registry.NewStore(), emb: embed.FromEnv(), llm: llm.FromEnv(), guard: loadGuardConfig()}
	srv := mcp.NewServer(&mcp.Implementation{Name: "gonexus", Version: "0.1.0"}, nil)
	st.register(srv)
	return srv.Run(context.Background(), &mcp.StdioTransport{})
}

func (st *store) graphFor(repo string) (*graph.Graph, error) {
	g, name, err := st.repos.Graph(repo)
	if err != nil {
		return nil, err
	}
	if !st.guard.repoAllowed(name) {
		return nil, fmt.Errorf("repo %q not in the allowed set", name)
	}
	return g, nil
}

// ---- tool I/O types (plain JSON structs; jsonschema tags document fields) ----

type nodeOut struct {
	ID        string `json:"id"`
	Kind      string `json:"kind"`
	Name      string `json:"name"`
	Package   string `json:"package"`
	File      string `json:"file"`
	Line      int    `json:"line"`
	Signature string `json:"signature,omitempty"`
	Doc       string `json:"doc,omitempty"`
	Process   string `json:"process,omitempty" jsonschema:"entry point whose flow reaches this symbol (search grouping)"`
}

type edgeOut struct {
	From string `json:"from"`
	To   string `json:"to"`
	Kind string `json:"kind"`
}

type queryIn struct {
	Q     string `json:"q" jsonschema:"symbol name or text to search for"`
	Limit int    `json:"limit,omitempty" jsonschema:"max results (default 20)"`
	Repo  string `json:"repo,omitempty" jsonschema:"repository name (from the repos tool); omit if only one repo is indexed"`
}
type queryOut struct {
	Results []nodeOut `json:"results"`
}

type contextIn struct {
	ID   string `json:"id" jsonschema:"symbol id (from a query result), e.g. pkg/path.Type.Method"`
	Repo string `json:"repo,omitempty" jsonschema:"repository name (from the repos tool); omit if only one repo is indexed"`
}
type contextOut struct {
	Node     *nodeOut  `json:"node"`
	Incoming []edgeOut `json:"incoming"`
	Outgoing []edgeOut `json:"outgoing"`
}

type impactIn struct {
	ID   string `json:"id" jsonschema:"symbol id to compute blast radius for"`
	Repo string `json:"repo,omitempty" jsonschema:"repository name (from the repos tool); omit if only one repo is indexed"`
}
type impactHitOut struct {
	ID         string  `json:"id"`
	Depth      int     `json:"depth"`
	Confidence float64 `json:"confidence"`
}
type impactOut struct {
	Callers []string       `json:"callers" jsonschema:"transitive callers that break if this symbol changes"`
	Hits    []impactHitOut `json:"hits" jsonschema:"callers grouped by depth with confidence (1/depth)"`
}

type traceIn struct {
	From string `json:"from" jsonschema:"source symbol id"`
	To   string `json:"to" jsonschema:"target symbol id"`
	Repo string `json:"repo,omitempty" jsonschema:"repository name (from the repos tool); omit if only one repo is indexed"`
}
type traceOut struct {
	Path []string `json:"path" jsonschema:"shortest call path from->to, empty if none"`
}

type reindexIn struct {
	Path string `json:"path" jsonschema:"repo directory to index"`
	Name string `json:"name,omitempty" jsonschema:"repo name (defaults to the directory name)"`
}
type reindexOut struct {
	Name  string `json:"name"`
	Nodes int    `json:"nodes"`
	Edges int    `json:"edges"`
}

type reposIn struct{}
type repoOut struct {
	Name  string `json:"name"`
	Path  string `json:"path"`
	Nodes int    `json:"nodes"`
	Stale bool   `json:"stale" jsonschema:"true if source changed since last index; call reindex"`
}
type reposOut struct {
	Repos []repoOut `json:"repos"`
}

type entryPointsIn struct {
	Repo string `json:"repo,omitempty" jsonschema:"repository name (from the repos tool); omit if only one repo is indexed"`
}
type entryPointOut struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Size int    `json:"size" jsonschema:"number of functions reachable from this entry point"`
}
type entryPointsOut struct {
	Entries []entryPointOut `json:"entries"`
}

type processIn struct {
	ID   string `json:"id" jsonschema:"entry-point (or any) symbol id to trace the flow from"`
	Repo string `json:"repo,omitempty" jsonschema:"repository name (from the repos tool); omit if only one repo is indexed"`
}
type processOut struct {
	Nodes []nodeOut `json:"nodes"`
	Edges []edgeOut `json:"edges"`
}

type detectChangesIn struct {
	Repo string `json:"repo,omitempty" jsonschema:"repository name (from the repos tool); omit if only one repo is indexed"`
	Base string `json:"base,omitempty" jsonschema:"git ref to diff against (e.g. main); empty = working tree vs HEAD"`
}
type detectChangesOut struct {
	Changed  []nodeOut `json:"changed" jsonschema:"symbols the diff touched"`
	Impacted []string  `json:"impacted" jsonschema:"transitive callers of the changed symbols (blast radius to review/test)"`
}

type renameIn struct {
	ID      string `json:"id" jsonschema:"symbol id to rename"`
	NewName string `json:"new_name" jsonschema:"the new identifier"`
	Apply   bool   `json:"apply,omitempty" jsonschema:"false = return the plan only; true = write the edits to disk"`
	Repo    string `json:"repo,omitempty" jsonschema:"repository name (from the repos tool); omit if only one repo is indexed"`
}
type renameEditOut struct {
	File  string `json:"file"`
	Lines []int  `json:"lines"`
}
type renameOut struct {
	OldName     string          `json:"oldName"`
	NewName     string          `json:"newName"`
	Edits       []renameEditOut `json:"edits"`
	Occurrences int             `json:"occurrences"`
	Confidence  float64         `json:"confidence" jsonschema:"1.0 when the name is unique in the repo; lower means review before applying"`
	Applied     bool            `json:"applied"`
}

type clustersIn struct {
	Repo string `json:"repo,omitempty" jsonschema:"repository name (from the repos tool); omit if only one repo is indexed"`
}

type wikiIn struct {
	Repo string `json:"repo,omitempty" jsonschema:"repository name (from the repos tool); omit if only one repo is indexed"`
}
type wikiOut struct {
	Markdown string `json:"markdown"`
}

type groupListIn struct{}
type groupOut struct {
	Name  string   `json:"name"`
	Repos []string `json:"repos"`
}
type groupListOut struct {
	Groups []groupOut `json:"groups"`
}

type groupSyncIn struct {
	Group string `json:"group" jsonschema:"group name"`
}
type linkOut struct {
	Key   string   `json:"key"`
	Repos []string `json:"repos"`
}
type groupSyncOut struct {
	Group          string         `json:"group"`
	Links          []linkOut      `json:"links" jsonschema:"contract keys shared across repos (cross-repo dependencies)"`
	ContractCounts map[string]int `json:"contractCounts"`
}

type toolMapIn struct{}
type toolInfo struct {
	Name        string `json:"name"`
	Description string `json:"description"`
}
type toolMapOut struct {
	Tools []toolInfo `json:"tools"`
}

type checkIn struct {
	IDs  []string `json:"ids" jsonschema:"symbol ids to validate"`
	Repo string   `json:"repo,omitempty" jsonschema:"repository name; omit if only one repo is indexed"`
}
type checkOut struct {
	Missing  []string  `json:"missing" jsonschema:"ids not present in the graph"`
	Dangling []edgeOut `json:"dangling" jsonschema:"edges with an endpoint missing from the graph"`
}

type cypherIn struct {
	Pattern string `json:"pattern" jsonschema:"single-hop pattern, e.g. (a:func)-[:calls]->(b:method)"`
	Limit   int    `json:"limit,omitempty"`
	Repo    string `json:"repo,omitempty" jsonschema:"repository name; omit if only one repo is indexed"`
}
type cypherOut struct {
	Edges []edgeOut `json:"edges"`
}

type routeMapIn struct {
	Repo string `json:"repo,omitempty" jsonschema:"repository name; omit if only one repo is indexed"`
}
type routeOut struct {
	Method  string `json:"method"`
	Path    string `json:"path"`
	Handler string `json:"handler"`
}
type routeMapOut struct {
	Routes []routeOut `json:"routes"`
}

type apiImpactIn struct {
	Path string `json:"path,omitempty" jsonschema:"optional route path to filter to"`
	Repo string `json:"repo,omitempty" jsonschema:"repository name; omit if only one repo is indexed"`
}
type routeImpactOut struct {
	Route    routeOut `json:"route"`
	Impacted []string `json:"impacted"`
}
type apiImpactOut struct {
	Routes []routeImpactOut `json:"routes"`
}

type explainIn struct {
	ID   string `json:"id,omitempty" jsonschema:"optional function id to filter to"`
	Repo string `json:"repo,omitempty" jsonschema:"repository name; omit if only one repo is indexed"`
}
type taintOut struct {
	Func   string `json:"func"`
	Source string `json:"source"`
	Sink   string `json:"sink"`
	Line   int    `json:"line"`
}
type explainOut struct {
	Findings []taintOut `json:"findings" jsonschema:"source→sink taint flows (requires the repo indexed with GONEXUS_PDG=1)"`
}

type pdgIn struct {
	ID   string `json:"id" jsonschema:"function id to inspect"`
	Repo string `json:"repo,omitempty" jsonschema:"repository name; omit if only one repo is indexed"`
}
type pdgOut struct {
	ID        string   `json:"id"`
	Blocks    int      `json:"blocks"`
	CtrlEdges [][2]int `json:"ctrlEdges" jsonschema:"CFG block index -> successor block index"`
	DataEdges int      `json:"dataEdges" jsonschema:"SSA def-use (data dependence) edge count"`
	Params    []string `json:"params"`
}
type clusterOut struct {
	ID      string   `json:"id"`
	Name    string   `json:"name"`
	Members []string `json:"members"`
}
type clustersOut struct {
	Clusters []clusterOut `json:"clusters"`
}

// register wires each graph query as an MCP tool.
func (st *store) register(srv *mcp.Server) {
	mcp.AddTool(srv, &mcp.Tool{
		Name:        "repos",
		Description: "List the indexed repositories and their sizes. Call this first when you don't know the repo name.",
	}, st.reposList)

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "query",
		Description: "Search a repo's code graph for symbols by name/text.",
	}, st.query)

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "context",
		Description: "360° view of a symbol: its signature, doc, and incoming/outgoing edges (callers and callees).",
	}, st.context)

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "impact",
		Description: "Blast radius: the transitive set of callers that break if this symbol changes. Check this BEFORE editing a symbol.",
	}, st.impact)

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "trace",
		Description: "Shortest call path between two symbols.",
	}, st.trace)

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "entrypoints",
		Description: "List execution-flow roots (main, request handlers, CLI commands) with the size of each flow. Use to discover how a codebase runs.",
	}, st.entryPoints)

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "process",
		Description: "Trace the execution flow rooted at a symbol: every function it transitively calls, plus the call edges. Use to understand what an entry point does end to end.",
	}, st.process)

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "detect_changes",
		Description: "Map the current git diff to the symbols it changes and their blast radius (transitive callers). Use before committing or in review to see what a change affects.",
	}, st.detectChanges)

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "rename",
		Description: "Plan (and optionally apply) a coordinated multi-file rename of a symbol. Returns the files/lines to change with a confidence score; check confidence (1.0 = the name is unique) before applying.",
	}, st.rename)

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "clusters",
		Description: "List emergent modules (communities of related functions detected on the call graph). Use to learn a codebase's functional structure.",
	}, st.clusters)

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "wiki",
		Description: "Generate architecture documentation (markdown) for a repo from its graph: modules, entry points, key interfaces, most-called functions. Use to onboard onto an unfamiliar codebase.",
	}, st.wiki)

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "group_list",
		Description: "List configured repository groups and their member repos.",
	}, st.groupList)

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "group_sync",
		Description: "Build a group's cross-repo contract registry: shared contract keys (exported symbols / routes) that link repos together.",
	}, st.groupSync)

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "tool_map",
		Description: "List all GoNexus tools and what they do. Call this to discover capabilities.",
	}, st.toolMap)

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "check",
		Description: "Validate symbol ids against the graph and report dangling edges (read-only structural check).",
	}, st.check)

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "cypher",
		Description: "Run a single-hop graph pattern query, e.g. (a:func)-[:calls]->(b:method).",
	}, st.cypher)

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "route_map",
		Description: "List detected HTTP endpoint → handler mappings (Go routers: net/http, gin, echo, chi).",
	}, st.routeMap)

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "api_impact",
		Description: "Blast radius of the handler(s) behind an HTTP route — what a route change affects.",
	}, st.apiImpact)

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "explain",
		Description: "List source→sink taint flows (e.g. untrusted input reaching exec/SQL/file ops). Requires the repo indexed with GONEXUS_PDG=1.",
	}, st.explain)

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "pdg_query",
		Description: "Control/data dependence for a function: CFG blocks + edges and SSA def-use count. Requires GONEXUS_PDG=1 indexing.",
	}, st.pdgQuery)

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "reindex",
		Description: "Index (or refresh) a repo by path and register it. Use when a repo is missing or its index is stale.",
	}, st.reindex)

	registerPrompts(srv)
}

// registerPrompts adds workflow prompts agents can invoke by name.
func registerPrompts(srv *mcp.Server) {
	prompt := func(name, desc, text string) {
		srv.AddPrompt(&mcp.Prompt{Name: name, Description: desc},
			func(context.Context, *mcp.GetPromptRequest) (*mcp.GetPromptResult, error) {
				return &mcp.GetPromptResult{
					Description: desc,
					Messages: []*mcp.PromptMessage{{
						Role:    "user",
						Content: &mcp.TextContent{Text: text},
					}},
				}, nil
			})
	}
	prompt("detect_impact",
		"Analyze the impact of pending changes before committing.",
		"Call the `detect_changes` tool to map the current git diff to the symbols it touches "+
			"and their blast radius. Then summarize, as a pre-commit review: what changed, what "+
			"downstream code could break, and which tests to run.")
	prompt("generate_map",
		"Generate architecture documentation for a repo.",
		"Call the `wiki` tool for the repo, then present the architecture: a short overview, the "+
			"main modules, entry points, and key interfaces. Include a mermaid diagram of how the "+
			"top entry points connect to the main modules.")
}

func (st *store) pdgFor(repo string) (*analysis.Result, error) {
	r, err := st.repos.Repo(repo)
	if err != nil {
		return nil, err
	}
	return analysis.Load(filepath.Join(r.Path, ".gonexus", "pdg.json"))
}

// allTools is the static catalog surfaced by tool_map.
var allTools = []toolInfo{
	{"repos", "list indexed repositories"},
	{"query", "hybrid search for symbols"},
	{"context", "360° view of a symbol"},
	{"impact", "blast radius of a symbol"},
	{"trace", "shortest call path between symbols"},
	{"entrypoints", "execution-flow roots"},
	{"process", "call-tree flow from an entry point"},
	{"clusters", "emergent modules (community detection)"},
	{"detect_changes", "git diff → changed symbols + blast radius"},
	{"rename", "confidence-scored multi-file rename"},
	{"wiki", "architecture documentation"},
	{"explain", "source→sink taint findings (--pdg)"},
	{"pdg_query", "function control/data dependence (--pdg)"},
	{"check", "validate ids + dangling edges"},
	{"cypher", "single-hop graph pattern query"},
	{"route_map", "HTTP endpoint → handler mappings"},
	{"api_impact", "blast radius behind a route"},
	{"tool_map", "list all tools"},
	{"reindex", "index/refresh a repo"},
}

func (st *store) groupList(_ context.Context, _ *mcp.CallToolRequest, _ groupListIn) (*mcp.CallToolResult, groupListOut, error) {
	f, err := registry.LoadGroups()
	if err != nil {
		return nil, groupListOut{}, err
	}
	out := groupListOut{Groups: make([]groupOut, 0)}
	for _, name := range f.Names() {
		out.Groups = append(out.Groups, groupOut{Name: name, Repos: f.Groups[name].Repos})
	}
	return nil, out, nil
}

func (st *store) groupSync(_ context.Context, _ *mcp.CallToolRequest, in groupSyncIn) (*mcp.CallToolResult, groupSyncOut, error) {
	res, err := groups.Sync(st.repos, in.Group)
	if err != nil {
		return nil, groupSyncOut{}, err
	}
	out := groupSyncOut{Group: res.Group, Links: make([]linkOut, 0, len(res.Links)), ContractCounts: map[string]int{}}
	for _, l := range res.Links {
		out.Links = append(out.Links, linkOut{Key: l.Key, Repos: l.Repos})
	}
	for repo, cs := range res.Contracts {
		out.ContractCounts[repo] = len(cs)
	}
	return nil, out, nil
}

func (st *store) toolMap(_ context.Context, _ *mcp.CallToolRequest, _ toolMapIn) (*mcp.CallToolResult, toolMapOut, error) {
	return nil, toolMapOut{Tools: allTools}, nil
}

func (st *store) check(_ context.Context, _ *mcp.CallToolRequest, in checkIn) (*mcp.CallToolResult, checkOut, error) {
	g, err := st.graphFor(in.Repo)
	if err != nil {
		return nil, checkOut{}, err
	}
	missing, dangling := g.Check(in.IDs)
	return nil, checkOut{Missing: missing, Dangling: toEdgeOut(dangling)}, nil
}

func (st *store) cypher(_ context.Context, _ *mcp.CallToolRequest, in cypherIn) (*mcp.CallToolResult, cypherOut, error) {
	g, err := st.graphFor(in.Repo)
	if err != nil {
		return nil, cypherOut{}, err
	}
	edges, err := g.Cypher(in.Pattern, in.Limit)
	if err != nil {
		return nil, cypherOut{}, err
	}
	return nil, cypherOut{Edges: toEdgeOut(edges)}, nil
}

func (st *store) routeMap(_ context.Context, _ *mcp.CallToolRequest, in routeMapIn) (*mcp.CallToolResult, routeMapOut, error) {
	g, err := st.graphFor(in.Repo)
	if err != nil {
		return nil, routeMapOut{}, err
	}
	out := routeMapOut{Routes: make([]routeOut, 0, len(g.Routes))}
	for _, r := range g.Routes {
		out.Routes = append(out.Routes, routeOut{Method: r.Method, Path: r.Path, Handler: r.Handler})
	}
	return nil, out, nil
}

func (st *store) apiImpact(_ context.Context, _ *mcp.CallToolRequest, in apiImpactIn) (*mcp.CallToolResult, apiImpactOut, error) {
	g, err := st.graphFor(in.Repo)
	if err != nil {
		return nil, apiImpactOut{}, err
	}
	out := apiImpactOut{Routes: make([]routeImpactOut, 0)}
	for _, r := range g.Routes {
		if in.Path != "" && r.Path != in.Path {
			continue
		}
		var impacted []string
		if r.Handler != "" {
			impacted = g.Impact(r.Handler)
		}
		out.Routes = append(out.Routes, routeImpactOut{
			Route:    routeOut{Method: r.Method, Path: r.Path, Handler: r.Handler},
			Impacted: impacted,
		})
	}
	return nil, out, nil
}

func (st *store) explain(_ context.Context, _ *mcp.CallToolRequest, in explainIn) (*mcp.CallToolResult, explainOut, error) {
	res, err := st.pdgFor(in.Repo)
	if err != nil {
		return nil, explainOut{}, err
	}
	out := explainOut{Findings: make([]taintOut, 0)}
	for _, t := range res.TaintForFunc(in.ID) {
		out.Findings = append(out.Findings, taintOut{Func: t.Func, Source: t.Source, Sink: t.Sink, Line: t.Line})
	}
	return nil, out, nil
}

func (st *store) pdgQuery(_ context.Context, _ *mcp.CallToolRequest, in pdgIn) (*mcp.CallToolResult, pdgOut, error) {
	res, err := st.pdgFor(in.Repo)
	if err != nil {
		return nil, pdgOut{}, err
	}
	fp := res.FindByFunc(in.ID)
	if fp == nil {
		return nil, pdgOut{}, fmt.Errorf("no PDG for %q (index with GONEXUS_PDG=1)", in.ID)
	}
	return nil, pdgOut{
		ID: fp.ID, Blocks: fp.Blocks, CtrlEdges: fp.CtrlEdges,
		DataEdges: fp.DataEdges, Params: fp.Params,
	}, nil
}

// ---- handlers ----

func (st *store) reposList(_ context.Context, _ *mcp.CallToolRequest, _ reposIn) (*mcp.CallToolResult, reposOut, error) {
	repos, counts, err := st.repos.List()
	if err != nil {
		return nil, reposOut{}, err
	}
	out := reposOut{Repos: make([]repoOut, 0, len(repos))}
	for i, r := range repos {
		out.Repos = append(out.Repos, repoOut{
			Name: r.Name, Path: r.Path, Nodes: counts[i], Stale: index.IsStale(r.Path),
		})
	}
	return nil, out, nil
}

func (st *store) query(ctx context.Context, _ *mcp.CallToolRequest, in queryIn) (*mcp.CallToolResult, queryOut, error) {
	g, err := st.graphFor(in.Repo)
	if err != nil {
		return nil, queryOut{}, err
	}
	limit := in.Limit
	if limit <= 0 {
		limit = 20
	}
	limit = min(limit, st.guard.listCap(limit)) // enforce response budget
	out := queryOut{Results: make([]nodeOut, 0, limit)}
	for _, n := range g.SearchHybrid(in.Q, limit, st.queryVector(ctx, in.Q)) {
		no := toNodeOut(n)
		no.Process = g.ProcessOf(n.ID) // group result by its execution flow
		out.Results = append(out.Results, no)
	}
	return nil, out, nil
}

// queryVector embeds q for semantic reranking, or nil (BM25 only).
func (st *store) queryVector(ctx context.Context, q string) []float32 {
	if st.emb == nil {
		return nil
	}
	vecs, err := st.emb.Embed(ctx, []string{q})
	if err != nil || len(vecs) == 0 {
		return nil
	}
	return vecs[0]
}

func (st *store) context(_ context.Context, _ *mcp.CallToolRequest, in contextIn) (*mcp.CallToolResult, contextOut, error) {
	g, err := st.graphFor(in.Repo)
	if err != nil {
		return nil, contextOut{}, err
	}
	n, incoming, outgoing := g.Context(in.ID)
	if n == nil {
		return nil, contextOut{}, fmt.Errorf("symbol not found: %s", in.ID)
	}
	no := toNodeOut(n)
	return nil, contextOut{Node: &no, Incoming: toEdgeOut(incoming), Outgoing: toEdgeOut(outgoing)}, nil
}

func (st *store) impact(_ context.Context, _ *mcp.CallToolRequest, in impactIn) (*mcp.CallToolResult, impactOut, error) {
	g, err := st.graphFor(in.Repo)
	if err != nil {
		return nil, impactOut{}, err
	}
	out := impactOut{Callers: g.Impact(in.ID)}
	for _, h := range g.ImpactGraded(in.ID) {
		out.Hits = append(out.Hits, impactHitOut{ID: h.ID, Depth: h.Depth, Confidence: h.Confidence})
	}
	return nil, out, nil
}

func (st *store) trace(_ context.Context, _ *mcp.CallToolRequest, in traceIn) (*mcp.CallToolResult, traceOut, error) {
	g, err := st.graphFor(in.Repo)
	if err != nil {
		return nil, traceOut{}, err
	}
	return nil, traceOut{Path: g.Trace(in.From, in.To)}, nil
}

func (st *store) entryPoints(_ context.Context, _ *mcp.CallToolRequest, in entryPointsIn) (*mcp.CallToolResult, entryPointsOut, error) {
	g, err := st.graphFor(in.Repo)
	if err != nil {
		return nil, entryPointsOut{}, err
	}
	entries := g.EntryPoints()
	out := entryPointsOut{Entries: make([]entryPointOut, 0, len(entries))}
	for _, n := range entries {
		out.Entries = append(out.Entries, entryPointOut{ID: n.ID, Name: n.Name, Size: g.ProcessSize(n.ID)})
	}
	return nil, out, nil
}

func (st *store) process(_ context.Context, _ *mcp.CallToolRequest, in processIn) (*mcp.CallToolResult, processOut, error) {
	g, err := st.graphFor(in.Repo)
	if err != nil {
		return nil, processOut{}, err
	}
	nodes, edges := g.Process(in.ID)
	out := processOut{
		Nodes: make([]nodeOut, 0, len(nodes)),
		Edges: toEdgeOut(edges),
	}
	for _, n := range nodes {
		out.Nodes = append(out.Nodes, toNodeOut(n))
	}
	return nil, out, nil
}

func (st *store) detectChanges(_ context.Context, _ *mcp.CallToolRequest, in detectChangesIn) (*mcp.CallToolResult, detectChangesOut, error) {
	g, err := st.graphFor(in.Repo)
	if err != nil {
		return nil, detectChangesOut{}, err
	}
	repo, err := st.repos.Repo(in.Repo)
	if err != nil {
		return nil, detectChangesOut{}, err
	}
	res, err := changes.Detect(g, repo.Path, in.Base)
	if err != nil {
		return nil, detectChangesOut{}, err
	}
	out := detectChangesOut{
		Changed:  make([]nodeOut, 0, len(res.Changed)),
		Impacted: res.Impacted,
	}
	for _, n := range res.Changed {
		out.Changed = append(out.Changed, toNodeOut(n))
	}
	return nil, out, nil
}

func (st *store) rename(_ context.Context, _ *mcp.CallToolRequest, in renameIn) (*mcp.CallToolResult, renameOut, error) {
	g, err := st.graphFor(in.Repo)
	if err != nil {
		return nil, renameOut{}, err
	}
	apply := in.Apply
	if st.guard.readOnly && apply {
		apply = false // read-only: downgrade to plan-only, never write
	}
	res, err := rename.Plan(g, in.ID, in.NewName, apply)
	if err != nil {
		return nil, renameOut{}, err
	}
	out := renameOut{
		OldName: res.OldName, NewName: res.NewName,
		Occurrences: res.Occurrences, Confidence: res.Confidence, Applied: res.Applied,
		Edits: make([]renameEditOut, 0, len(res.Edits)),
	}
	for _, e := range res.Edits {
		out.Edits = append(out.Edits, renameEditOut{File: e.File, Lines: e.Lines})
	}
	return nil, out, nil
}

func (st *store) wiki(ctx context.Context, _ *mcp.CallToolRequest, in wikiIn) (*mcp.CallToolResult, wikiOut, error) {
	g, err := st.graphFor(in.Repo)
	if err != nil {
		return nil, wikiOut{}, err
	}
	repo, err := st.repos.Repo(in.Repo)
	if err != nil {
		return nil, wikiOut{}, err
	}
	md, err := wiki.Generate(ctx, g, repo.Name, st.llm)
	if err != nil {
		return nil, wikiOut{}, err
	}
	return nil, wikiOut{Markdown: md}, nil
}

func (st *store) clusters(_ context.Context, _ *mcp.CallToolRequest, in clustersIn) (*mcp.CallToolResult, clustersOut, error) {
	g, err := st.graphFor(in.Repo)
	if err != nil {
		return nil, clustersOut{}, err
	}
	coms := g.Communities()
	out := clustersOut{Clusters: make([]clusterOut, 0, len(coms))}
	for _, c := range coms {
		out.Clusters = append(out.Clusters, clusterOut{ID: c.ID, Name: c.Name, Members: c.Members})
	}
	return nil, out, nil
}

func (st *store) reindex(_ context.Context, _ *mcp.CallToolRequest, in reindexIn) (*mcp.CallToolResult, reindexOut, error) {
	if st.guard.readOnly {
		return nil, reindexOut{}, fmt.Errorf("read-only mode: reindex disabled (GONEXUS_MCP_READ_ONLY)")
	}
	name, nodes, edges, err := index.IndexAndRegister(in.Path, in.Name)
	if err != nil {
		return nil, reindexOut{}, err
	}
	return nil, reindexOut{Name: name, Nodes: nodes, Edges: edges}, nil
}

// ---- mappers ----

func toNodeOut(n *graph.Node) nodeOut {
	return nodeOut{
		ID: n.ID, Kind: string(n.Kind), Name: n.Name, Package: n.Package,
		File: n.File, Line: n.Line, Signature: n.Signature, Doc: n.Doc,
	}
}

func toEdgeOut(es []graph.Edge) []edgeOut {
	out := make([]edgeOut, 0, len(es))
	for _, e := range es {
		out = append(out, edgeOut{From: e.From, To: e.To, Kind: string(e.Kind)})
	}
	return out
}
