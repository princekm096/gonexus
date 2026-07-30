// Package server wires the graph queries to the Connect/gRPC surface.
package server

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"connectrpc.com/connect"
	v1 "github.com/yourorg/gonexus/gen/gonexus/v1"
	"github.com/yourorg/gonexus/gen/gonexus/v1/gonexusv1connect"
	"github.com/yourorg/gonexus/internal/analysis"
	"github.com/yourorg/gonexus/internal/changes"
	"github.com/yourorg/gonexus/internal/embed"
	"github.com/yourorg/gonexus/internal/graph"
	"github.com/yourorg/gonexus/internal/index"
	"github.com/yourorg/gonexus/internal/llm"
	"github.com/yourorg/gonexus/internal/registry"
	"github.com/yourorg/gonexus/internal/rename"
	"github.com/yourorg/gonexus/internal/wiki"
)

// Server serves the code graphs of all registered repos. Each query names a
// repo (empty = the sole repo); the store loads and mtime-caches per-repo
// graphs from ~/.gonexus/registry.json.
type Server struct {
	store    *registry.Store
	emb      embed.Embedder // nil unless GONEXUS_EMBED_URL is set
	llm      llm.Client     // nil unless GONEXUS_LLM_URL is set
	readOnly bool           // GONEXUS_READ_ONLY=1: reject Index, downgrade Rename to plan-only
}

var _ gonexusv1connect.GoNexusServiceHandler = (*Server)(nil)

func New() *Server {
	return &Server{
		store: registry.NewStore(), emb: embed.FromEnv(), llm: llm.FromEnv(),
		readOnly: os.Getenv("GONEXUS_READ_ONLY") == "1",
	}
}

// queryVector embeds q for semantic reranking, or returns nil (BM25 only).
func (s *Server) queryVector(ctx context.Context, q string) []float32 {
	if s.emb == nil {
		return nil
	}
	vecs, err := s.emb.Embed(ctx, []string{q})
	if err != nil || len(vecs) == 0 {
		return nil // degrade to BM25 on embed failure
	}
	return vecs[0]
}

// graphFor resolves the repo's graph or returns a Connect error.
func (s *Server) graphFor(repo string) (*graph.Graph, error) {
	g, _, err := s.store.Graph(repo)
	if err != nil {
		return nil, connect.NewError(connect.CodeNotFound, err)
	}
	return g, nil
}

func (s *Server) Index(ctx context.Context, req *connect.Request[v1.IndexRequest]) (*connect.Response[v1.IndexResponse], error) {
	if s.readOnly {
		return nil, connect.NewError(connect.CodePermissionDenied,
			fmt.Errorf("read-only mode (GONEXUS_READ_ONLY): Index disabled"))
	}
	name, nodes, edges, err := index.IndexAndRegister(req.Msg.Path, req.Msg.Name)
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, err)
	}
	return connect.NewResponse(&v1.IndexResponse{
		Nodes: int32(nodes), Edges: int32(edges), Name: name,
	}), nil
}

func (s *Server) Query(ctx context.Context, req *connect.Request[v1.QueryRequest]) (*connect.Response[v1.QueryResponse], error) {
	g, err := s.graphFor(req.Msg.Repo)
	if err != nil {
		return nil, err
	}
	limit := int(req.Msg.Limit)
	if limit <= 0 {
		limit = 20
	}
	nodes := g.SearchHybrid(req.Msg.Q, limit, s.queryVector(ctx, req.Msg.Q))
	return connect.NewResponse(&v1.QueryResponse{Results: toNodes(nodes)}), nil
}

func (s *Server) Context(ctx context.Context, req *connect.Request[v1.ContextRequest]) (*connect.Response[v1.ContextResponse], error) {
	g, err := s.graphFor(req.Msg.Repo)
	if err != nil {
		return nil, err
	}
	n, in, out := g.Context(req.Msg.Id)
	if n == nil {
		return nil, connect.NewError(connect.CodeNotFound, nil)
	}
	return connect.NewResponse(&v1.ContextResponse{
		Node: toNode(n), Incoming: toEdges(in), Outgoing: toEdges(out),
	}), nil
}

func (s *Server) Impact(ctx context.Context, req *connect.Request[v1.ImpactRequest]) (*connect.Response[v1.ImpactResponse], error) {
	g, err := s.graphFor(req.Msg.Repo)
	if err != nil {
		return nil, err
	}
	hits := make([]*v1.ImpactHit, 0)
	for _, h := range g.ImpactGraded(req.Msg.Id) {
		hits = append(hits, &v1.ImpactHit{Id: h.ID, Depth: int32(h.Depth), Confidence: h.Confidence})
	}
	return connect.NewResponse(&v1.ImpactResponse{Ids: g.Impact(req.Msg.Id), Hits: hits}), nil
}

func (s *Server) Trace(ctx context.Context, req *connect.Request[v1.TraceRequest]) (*connect.Response[v1.TraceResponse], error) {
	g, err := s.graphFor(req.Msg.Repo)
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(&v1.TraceResponse{Path: g.Trace(req.Msg.From, req.Msg.To)}), nil
}

func (s *Server) Subgraph(ctx context.Context, req *connect.Request[v1.SubgraphRequest]) (*connect.Response[v1.SubgraphResponse], error) {
	g, err := s.graphFor(req.Msg.Repo)
	if err != nil {
		return nil, err
	}
	depth := int(req.Msg.Depth)
	if depth <= 0 {
		depth = 1
	}
	nodes, edges := neighborhood(g, req.Msg.Id, depth)
	return connect.NewResponse(&v1.SubgraphResponse{Nodes: toNodes(nodes), Edges: toEdges(edges)}), nil
}

func (s *Server) EntryPoints(ctx context.Context, req *connect.Request[v1.EntryPointsRequest]) (*connect.Response[v1.EntryPointsResponse], error) {
	g, err := s.graphFor(req.Msg.Repo)
	if err != nil {
		return nil, err
	}
	entries := g.EntryPoints()
	out := make([]*v1.EntryPoint, 0, len(entries))
	for _, n := range entries {
		out = append(out, &v1.EntryPoint{Node: toNode(n), Size: int32(g.ProcessSize(n.ID))})
	}
	return connect.NewResponse(&v1.EntryPointsResponse{Entries: out}), nil
}

func (s *Server) Process(ctx context.Context, req *connect.Request[v1.ProcessRequest]) (*connect.Response[v1.ProcessResponse], error) {
	g, err := s.graphFor(req.Msg.Repo)
	if err != nil {
		return nil, err
	}
	nodes, edges := g.Process(req.Msg.Id)
	return connect.NewResponse(&v1.ProcessResponse{Nodes: toNodes(nodes), Edges: toEdges(edges)}), nil
}

func (s *Server) DetectChanges(ctx context.Context, req *connect.Request[v1.DetectChangesRequest]) (*connect.Response[v1.DetectChangesResponse], error) {
	g, err := s.graphFor(req.Msg.Repo)
	if err != nil {
		return nil, err
	}
	repo, err := s.store.Repo(req.Msg.Repo)
	if err != nil {
		return nil, connect.NewError(connect.CodeNotFound, err)
	}
	res, err := changes.Detect(g, repo.Path, req.Msg.Base)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	return connect.NewResponse(&v1.DetectChangesResponse{
		Changed:  toNodes(res.Changed),
		Impacted: res.Impacted,
	}), nil
}

func (s *Server) Rename(ctx context.Context, req *connect.Request[v1.RenameRequest]) (*connect.Response[v1.RenameResponse], error) {
	g, err := s.graphFor(req.Msg.Repo)
	if err != nil {
		return nil, err
	}
	apply := req.Msg.Apply && !s.readOnly // read-only: plan-only, never write
	res, err := rename.Plan(g, req.Msg.Id, req.Msg.NewName, apply)
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, err)
	}
	edits := make([]*v1.RenameEdit, 0, len(res.Edits))
	for _, e := range res.Edits {
		lines := make([]int32, len(e.Lines))
		for i, l := range e.Lines {
			lines[i] = int32(l)
		}
		edits = append(edits, &v1.RenameEdit{File: e.File, Lines: lines})
	}
	return connect.NewResponse(&v1.RenameResponse{
		OldName: res.OldName, NewName: res.NewName, Edits: edits,
		Occurrences: int32(res.Occurrences), Confidence: res.Confidence, Applied: res.Applied,
	}), nil
}

func (s *Server) Wiki(ctx context.Context, req *connect.Request[v1.WikiRequest]) (*connect.Response[v1.WikiResponse], error) {
	g, err := s.graphFor(req.Msg.Repo)
	if err != nil {
		return nil, err
	}
	repo, err := s.store.Repo(req.Msg.Repo)
	if err != nil {
		return nil, connect.NewError(connect.CodeNotFound, err)
	}
	md, err := wiki.Generate(ctx, g, repo.Name, s.llm)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	return connect.NewResponse(&v1.WikiResponse{Markdown: md}), nil
}

func (s *Server) pdgFor(repo string) (*analysis.Result, error) {
	r, err := s.store.Repo(repo)
	if err != nil {
		return nil, connect.NewError(connect.CodeNotFound, err)
	}
	res, err := analysis.Load(filepath.Join(r.Path, ".gonexus", "pdg.json"))
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	return res, nil
}

func (s *Server) Explain(ctx context.Context, req *connect.Request[v1.ExplainRequest]) (*connect.Response[v1.ExplainResponse], error) {
	res, err := s.pdgFor(req.Msg.Repo)
	if err != nil {
		return nil, err
	}
	out := make([]*v1.TaintFinding, 0)
	for _, t := range res.TaintForFunc(req.Msg.Id) {
		out = append(out, &v1.TaintFinding{Func: t.Func, Source: t.Source, Sink: t.Sink, Line: int32(t.Line)})
	}
	return connect.NewResponse(&v1.ExplainResponse{Findings: out}), nil
}

func (s *Server) PdgQuery(ctx context.Context, req *connect.Request[v1.PdgQueryRequest]) (*connect.Response[v1.PdgQueryResponse], error) {
	res, err := s.pdgFor(req.Msg.Repo)
	if err != nil {
		return nil, err
	}
	fp := res.FindByFunc(req.Msg.Id)
	if fp == nil {
		return nil, connect.NewError(connect.CodeNotFound, nil)
	}
	edges := make([]*v1.ControlEdge, 0, len(fp.CtrlEdges))
	for _, e := range fp.CtrlEdges {
		edges = append(edges, &v1.ControlEdge{From: int32(e[0]), To: int32(e[1])})
	}
	return connect.NewResponse(&v1.PdgQueryResponse{
		Id: fp.ID, Blocks: int32(fp.Blocks), CtrlEdges: edges,
		DataEdges: int32(fp.DataEdges), Params: fp.Params,
	}), nil
}

func (s *Server) Check(ctx context.Context, req *connect.Request[v1.CheckRequest]) (*connect.Response[v1.CheckResponse], error) {
	g, err := s.graphFor(req.Msg.Repo)
	if err != nil {
		return nil, err
	}
	missing, dangling := g.Check(req.Msg.Ids)
	return connect.NewResponse(&v1.CheckResponse{Missing: missing, Dangling: toEdges(dangling)}), nil
}

func (s *Server) Cypher(ctx context.Context, req *connect.Request[v1.CypherRequest]) (*connect.Response[v1.CypherResponse], error) {
	g, err := s.graphFor(req.Msg.Repo)
	if err != nil {
		return nil, err
	}
	edges, err := g.Cypher(req.Msg.Pattern, int(req.Msg.Limit))
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, err)
	}
	return connect.NewResponse(&v1.CypherResponse{Edges: toEdges(edges)}), nil
}

func (s *Server) RouteMap(ctx context.Context, req *connect.Request[v1.RouteMapRequest]) (*connect.Response[v1.RouteMapResponse], error) {
	g, err := s.graphFor(req.Msg.Repo)
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(&v1.RouteMapResponse{Routes: toRoutes(g.Routes)}), nil
}

func (s *Server) ApiImpact(ctx context.Context, req *connect.Request[v1.ApiImpactRequest]) (*connect.Response[v1.ApiImpactResponse], error) {
	g, err := s.graphFor(req.Msg.Repo)
	if err != nil {
		return nil, err
	}
	out := make([]*v1.RouteImpact, 0)
	for _, r := range g.Routes {
		if req.Msg.Path != "" && r.Path != req.Msg.Path {
			continue
		}
		var impacted []string
		if r.Handler != "" {
			impacted = g.Impact(r.Handler)
		}
		out = append(out, &v1.RouteImpact{
			Route:    &v1.Route{Method: r.Method, Path: r.Path, Handler: r.Handler},
			Impacted: impacted,
		})
	}
	return connect.NewResponse(&v1.ApiImpactResponse{Routes: out}), nil
}

func toRoutes(rs []graph.Route) []*v1.Route {
	out := make([]*v1.Route, 0, len(rs))
	for _, r := range rs {
		out = append(out, &v1.Route{Method: r.Method, Path: r.Path, Handler: r.Handler})
	}
	return out
}

func (s *Server) Ask(ctx context.Context, req *connect.Request[v1.AskRequest]) (*connect.Response[v1.AskResponse], error) {
	g, err := s.graphFor(req.Msg.Repo)
	if err != nil {
		return nil, err
	}
	if s.llm == nil {
		// No LLM: return the most relevant symbols as the answer.
		nodes := g.Search(req.Msg.Question, 8)
		var b strings.Builder
		b.WriteString("No LLM configured (set GONEXUS_LLM_URL). Most relevant symbols:\n")
		for _, n := range nodes {
			fmt.Fprintf(&b, "- %s (%s) %s\n", n.Name, n.Kind, n.Signature)
		}
		return connect.NewResponse(&v1.AskResponse{Answer: b.String(), Sources: toNodes(nodes)}), nil
	}
	answer, sources := s.reactAgent(ctx, g, req.Msg.Question)
	return connect.NewResponse(&v1.AskResponse{Answer: answer, Sources: toNodes(sources)}), nil
}

const reactSystem = `You are a code-analysis agent working over a code knowledge graph.
Available tools:
- query(text): search for symbols
- context(id): a symbol's signature, callers and callees
- impact(id): transitive callers (blast radius)
- trace(input): shortest call path; input is "fromID||toID"
- entrypoints(): execution-flow roots
Reply with EXACTLY one JSON object per turn and nothing else:
{"action":"<tool>","input":"<arg>"} to call a tool, or
{"final":"<answer>"} when you can answer.
Base your answer only on tool observations. Cite symbol ids.`

// reactAgent runs a bounded ReAct loop: the LLM chooses graph tools, the server
// executes them and feeds back observations, until the LLM returns a final
// answer or the step budget is exhausted.
func (s *Server) reactAgent(ctx context.Context, g *graph.Graph, question string) (string, []*graph.Node) {
	const maxSteps = 6
	transcript := "Question: " + question
	var sources []*graph.Node
	seen := map[string]bool{}
	addSrc := func(ns []*graph.Node) {
		for _, n := range ns {
			if !seen[n.ID] {
				seen[n.ID] = true
				sources = append(sources, n)
			}
		}
	}

	for step := 0; step < maxSteps; step++ {
		resp, err := s.llm.Complete(ctx, reactSystem, transcript)
		if err != nil {
			return "LLM error: " + err.Error(), sources
		}
		act := parseAction(resp)
		if act.Final != "" || act.Action == "" {
			if act.Final != "" {
				return act.Final, sources
			}
			return resp, sources // model answered in prose
		}
		obs := runAction(g, act, addSrc)
		transcript += fmt.Sprintf("\nAction: %s(%s)\nObservation: %s", act.Action, act.Input, obs)
	}
	// budget exhausted: ask for a final answer with what we have.
	final, _ := s.llm.Complete(ctx, reactSystem+"\nYou are out of steps; give {\"final\":...} now.", transcript)
	if a := parseAction(final); a.Final != "" {
		return a.Final, sources
	}
	return final, sources
}

type reactAct struct {
	Action string `json:"action"`
	Input  string `json:"input"`
	Final  string `json:"final"`
}

// parseAction extracts the first {...} JSON object from an LLM reply.
func parseAction(s string) reactAct {
	i, j := strings.IndexByte(s, '{'), strings.LastIndexByte(s, '}')
	var a reactAct
	if i >= 0 && j > i {
		_ = json.Unmarshal([]byte(s[i:j+1]), &a)
	}
	return a
}

func runAction(g *graph.Graph, a reactAct, addSrc func([]*graph.Node)) string {
	switch a.Action {
	case "query":
		nodes := g.Search(a.Input, 8)
		addSrc(nodes)
		var b strings.Builder
		for _, n := range nodes {
			fmt.Fprintf(&b, "%s [%s] %s; ", n.ID, n.Kind, n.Signature)
		}
		return b.String()
	case "context":
		n, in, out := g.Context(a.Input)
		if n == nil {
			return "not found"
		}
		addSrc([]*graph.Node{n})
		return fmt.Sprintf("%s %s; %d callers, %d callees", n.Name, n.Signature, len(in), len(out))
	case "impact":
		ids := g.Impact(a.Input)
		return fmt.Sprintf("%d transitive callers: %s", len(ids), strings.Join(ids, ", "))
	case "trace":
		parts := strings.SplitN(a.Input, "||", 2)
		if len(parts) != 2 {
			return "trace needs input 'fromID||toID'"
		}
		return "path: " + strings.Join(g.Trace(parts[0], parts[1]), " -> ")
	case "entrypoints":
		var names []string
		for _, n := range g.EntryPoints() {
			names = append(names, n.ID)
		}
		return "entry points: " + strings.Join(names, ", ")
	default:
		return "unknown tool " + a.Action
	}
}

func (s *Server) ShapeCheck(ctx context.Context, req *connect.Request[v1.ShapeCheckRequest]) (*connect.Response[v1.ShapeCheckResponse], error) {
	g, err := s.graphFor(req.Msg.Repo)
	if err != nil {
		return nil, err
	}
	out := make([]*v1.ShapeFinding, 0)
	for _, f := range g.ShapeCheck() {
		out = append(out, &v1.ShapeFinding{Object: f.Object, Type: f.Type, File: f.File, Unknown: f.Unknown})
	}
	return connect.NewResponse(&v1.ShapeCheckResponse{Findings: out}), nil
}

func (s *Server) Clusters(ctx context.Context, req *connect.Request[v1.ClustersRequest]) (*connect.Response[v1.ClustersResponse], error) {
	g, err := s.graphFor(req.Msg.Repo)
	if err != nil {
		return nil, err
	}
	coms := g.Communities()
	out := make([]*v1.Cluster, 0, len(coms))
	for _, c := range coms {
		out = append(out, &v1.Cluster{Id: c.ID, Name: c.Name, Members: c.Members, Cohesion: c.Cohesion})
	}
	return connect.NewResponse(&v1.ClustersResponse{Clusters: out}), nil
}

func (s *Server) Repos(ctx context.Context, req *connect.Request[v1.ReposRequest]) (*connect.Response[v1.ReposResponse], error) {
	repos, counts, err := s.store.List()
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	out := make([]*v1.Repo, 0, len(repos))
	for i, r := range repos {
		out = append(out, &v1.Repo{
			Name: r.Name, Path: r.Path, Nodes: int32(counts[i]), Stale: index.IsStale(r.Path),
		})
	}
	return connect.NewResponse(&v1.ReposResponse{Repos: out}), nil
}

// neighborhood collects nodes/edges within depth hops of id (any edge kind).
func neighborhood(g *graph.Graph, id string, depth int) ([]*graph.Node, []graph.Edge) {
	seen := map[string]bool{id: true}
	frontier := []string{id}
	var edges []graph.Edge
	edgeSeen := map[graph.Edge]bool{}
	for d := 0; d < depth; d++ {
		var next []string
		for _, cur := range frontier {
			_, in, out := g.Context(cur)
			for _, e := range append(append([]graph.Edge{}, in...), out...) {
				if !edgeSeen[e] {
					edgeSeen[e] = true
					edges = append(edges, e)
				}
				for _, nb := range []string{e.From, e.To} {
					if !seen[nb] {
						seen[nb] = true
						next = append(next, nb)
					}
				}
			}
		}
		frontier = next
	}
	var nodes []*graph.Node
	for nid := range seen {
		if n := g.Nodes[nid]; n != nil {
			nodes = append(nodes, n)
		}
	}
	return nodes, edges
}

// Graph returns the whole repo graph, capped to keep the payload sane. Over the
// cap, the highest-degree nodes are kept (the ones worth seeing) and only edges
// between kept nodes are returned.
func (s *Server) Graph(ctx context.Context, req *connect.Request[v1.GraphRequest]) (*connect.Response[v1.GraphResponse], error) {
	g, err := s.graphFor(req.Msg.Repo)
	if err != nil {
		return nil, err
	}
	limit := int(req.Msg.Limit)
	if limit <= 0 {
		limit = 1500
	}
	deg := make(map[string]int, len(g.Nodes))
	for _, e := range g.Edges {
		deg[e.From]++
		deg[e.To]++
	}
	ids := make([]string, 0, len(g.Nodes))
	for id := range g.Nodes {
		ids = append(ids, id)
	}
	truncated := false
	if len(ids) > limit {
		sort.Slice(ids, func(i, j int) bool { return deg[ids[i]] > deg[ids[j]] })
		ids = ids[:limit]
		truncated = true
	}
	keep := make(map[string]bool, len(ids))
	nodes := make([]*graph.Node, 0, len(ids))
	for _, id := range ids {
		keep[id] = true
		nodes = append(nodes, g.Nodes[id])
	}
	var edges []graph.Edge
	for _, e := range g.Edges {
		if keep[e.From] && keep[e.To] {
			edges = append(edges, e)
		}
	}
	return connect.NewResponse(&v1.GraphResponse{
		Nodes: toNodes(nodes), Edges: toEdges(edges), Truncated: truncated,
	}), nil
}

// Source returns source code around a symbol. The file comes from the symbol's
// own node (never a client-supplied path) and must resolve within the repo
// root, so this can't read arbitrary files off the host.
func (s *Server) Source(ctx context.Context, req *connect.Request[v1.SourceRequest]) (*connect.Response[v1.SourceResponse], error) {
	g, err := s.graphFor(req.Msg.Repo)
	if err != nil {
		return nil, err
	}
	n := g.Nodes[req.Msg.Id]
	if n == nil || n.File == "" {
		return nil, connect.NewError(connect.CodeNotFound, fmt.Errorf("no source for %q", req.Msg.Id))
	}
	repo, err := s.store.Repo(req.Msg.Repo)
	if err != nil {
		return nil, connect.NewError(connect.CodeNotFound, err)
	}
	root, _ := filepath.Abs(repo.Path)
	file, _ := filepath.Abs(n.File)
	if !within(root, file) {
		return nil, connect.NewError(connect.CodePermissionDenied, fmt.Errorf("file outside repo root"))
	}
	data, err := os.ReadFile(file)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	lines := strings.Split(string(data), "\n")
	before, after := int(req.Msg.Before), int(req.Msg.After)
	if before < 0 || before > 50 {
		before = 3
	}
	if after <= 0 || after > 400 {
		after = 120
	}
	start := n.Line - before
	if n.Line == 0 { // no line info: show the file head
		start = 1
	}
	if start < 1 {
		start = 1
	}
	end := n.Line + after
	if n.Line == 0 {
		end = after
	}
	if end > len(lines) {
		end = len(lines)
	}
	code := strings.Join(lines[start-1:end], "\n")
	return connect.NewResponse(&v1.SourceResponse{
		File: file, StartLine: int32(start), Code: code, Lang: langOf(file),
	}), nil
}

// within reports whether path is inside root (after cleaning), used to keep
// Source from escaping the repo directory.
func within(root, path string) bool {
	rel, err := filepath.Rel(root, path)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

func langOf(file string) string {
	switch strings.ToLower(filepath.Ext(file)) {
	case ".go":
		return "go"
	case ".ts", ".tsx":
		return "typescript"
	case ".js", ".jsx", ".mjs":
		return "javascript"
	case ".vue":
		return "vue"
	case ".py":
		return "python"
	case ".md":
		return "markdown"
	case ".json":
		return "json"
	default:
		return ""
	}
}

// ---- mappers ----

func toNode(n *graph.Node) *v1.Node {
	if n == nil {
		return nil
	}
	return &v1.Node{
		Id: n.ID, Kind: string(n.Kind), Name: n.Name, Package: n.Package,
		File: n.File, Line: int32(n.Line), Signature: n.Signature, Doc: n.Doc,
	}
}

func toNodes(ns []*graph.Node) []*v1.Node {
	out := make([]*v1.Node, 0, len(ns))
	for _, n := range ns {
		out = append(out, toNode(n))
	}
	return out
}

func toEdges(es []graph.Edge) []*v1.Edge {
	out := make([]*v1.Edge, 0, len(es))
	for _, e := range es {
		out = append(out, &v1.Edge{From: e.From, To: e.To, Kind: string(e.Kind)})
	}
	return out
}
