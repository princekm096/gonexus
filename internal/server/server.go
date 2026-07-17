// Package server wires the graph queries to the Connect/gRPC surface.
package server

import (
	"context"

	"connectrpc.com/connect"
	v1 "github.com/yourorg/gonexus/gen/gonexus/v1"
	"github.com/yourorg/gonexus/gen/gonexus/v1/gonexusv1connect"
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
	store *registry.Store
	emb   embed.Embedder // nil unless GONEXUS_EMBED_URL is set
	llm   llm.Client     // nil unless GONEXUS_LLM_URL is set
}

var _ gonexusv1connect.GoNexusServiceHandler = (*Server)(nil)

func New() *Server {
	return &Server{store: registry.NewStore(), emb: embed.FromEnv(), llm: llm.FromEnv()}
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
	return connect.NewResponse(&v1.ImpactResponse{Ids: g.Impact(req.Msg.Id)}), nil
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
	res, err := rename.Plan(g, req.Msg.Id, req.Msg.NewName, req.Msg.Apply)
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

func (s *Server) Clusters(ctx context.Context, req *connect.Request[v1.ClustersRequest]) (*connect.Response[v1.ClustersResponse], error) {
	g, err := s.graphFor(req.Msg.Repo)
	if err != nil {
		return nil, err
	}
	coms := g.Communities()
	out := make([]*v1.Cluster, 0, len(coms))
	for _, c := range coms {
		out = append(out, &v1.Cluster{Id: c.ID, Name: c.Name, Members: c.Members})
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
