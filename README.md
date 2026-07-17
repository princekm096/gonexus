# GoNexus

A **code knowledge graph** for AI agents and humans. GoNexus indexes a
codebase into a graph of symbols (functions, types, components) and their
relationships (calls, imports, implements), then answers architectural
questions — blast radius, execution flows, modules, semantic search — over
gRPC, MCP, and a web UI.

Clean-room implementation (no upstream license constraints). Stack: **Go +
gRPC/Connect backend, Vue 3 + WebGL frontend, MCP for agents.**

- **Language-native indexing** — Go via `go/packages`+`go/types`, TS/JS/Vue via
  the TypeScript compiler + `@vue/compiler-sfc`. Real type resolution, not
  regex/heuristics, so the call graph is precise across files and languages.
- **One graph per repo**, many repos per server. Incremental re-index.
- **12 MCP tools** so Claude Code / Cursor / Codex get precomputed context in one
  call instead of ten exploratory reads.

See [STATUS.md](STATUS.md) for the full feature checklist.

---

## Prerequisites

| Need | For |
|------|-----|
| **Go 1.25+** | building GoNexus; indexing Go repos (their Go toolchain must be present) |
| **Node 18+** | indexing TS/JS/Vue; running the web UI |
| **git** | `detect_changes` (git-diff impact) |
| OpenAI-compatible endpoint (Ollama, LM Studio, OpenAI…) | *optional* semantic search + wiki prose |
| `buf` + protoc plugins | *optional* only to regenerate the gRPC contract |

> The Go module is `github.com/yourorg/gonexus`. For local use it works as-is;
> rename it in `go.mod` and imports if you publish under your own org.

## Install

```bash
git clone <this-repo> gonexus && cd gonexus

# 1. backend binary
go build -o gonexus ./cmd/gonexus

# 2. TS/Vue extractor deps (once) — skip if you only index Go
cd tools/ts-extractor && npm install && cd -

# 3. web UI deps (once) — skip if you only use the API/MCP
cd web && npm install && cd -
```

Put `gonexus` on your `PATH` if you like (`sudo mv gonexus /usr/local/bin`).

## Quick start

```bash
# Index a repo and register it. Name defaults to the directory name.
gonexus index /path/to/your/repo myrepo

# Option A — serve the API + open the web UI
gonexus serve :8080            # in one terminal
cd web && npm run dev           # in another → http://localhost:5173

# Option B — expose it to Claude Code as MCP tools
claude mcp add gonexus -- /absolute/path/to/gonexus mcp
```

That's it. Query from the UI, from an agent, or with `curl` (see
[HTTP API](#http-api)).

## CLI

```
gonexus index <path> [name]   Index a repo and register it (incremental).
gonexus list                  List registered repos.
gonexus status                Show which repo indexes are stale.
gonexus remove <name>         Drop a repo from the registry.
gonexus wiki [repo]           Print architecture docs (markdown) to stdout.
gonexus serve [addr]          Serve all registered repos (default :8080).
gonexus mcp                   Serve over MCP stdio (for agents).
```

Repos are tracked in `~/.gonexus/registry.json`; each repo's graph lives in its
own `<repo>/.gonexus/graph.json` (add `.gonexus/` to that repo's `.gitignore`).
A running `serve`/`mcp` process reloads a repo's graph when it changes on disk,
so a `gonexus index` from the CLI shows up live.

## MCP (agent integration)

```bash
gonexus index /path/to/repo          # index first
claude mcp add gonexus -- /abs/path/to/gonexus mcp
```

Tools (all take an optional `repo` arg; omit it when only one repo is indexed):

| Tool | What it answers |
|------|-----------------|
| `repos` | which repos are indexed (and if any is `stale`) |
| `query` | hybrid search for symbols by name/text |
| `context` | a symbol's signature, doc, callers, and callees |
| `impact` | blast radius — everything that breaks if this symbol changes |
| `trace` | shortest call path between two symbols |
| `entrypoints` | execution-flow roots (main, handlers, commands) |
| `process` | the full call-tree flow rooted at an entry point |
| `clusters` | emergent modules (community detection) |
| `detect_changes` | git diff → changed symbols + their blast radius |
| `rename` | confidence-scored multi-file rename (plan or apply) |
| `wiki` | generated architecture documentation |
| `reindex` | (re)index a repo by path |

Typical agent loop: `impact` before editing a symbol; `detect_changes` before
committing; `context`/`trace` while navigating.

## HTTP API

The server speaks the [Connect protocol](https://connectrpc.com): a unary call
is a plain HTTP POST with a JSON body, so any HTTP client works — no gRPC
codegen needed. It also serves gRPC and gRPC-Web on the same port.

```
POST http://localhost:8080/gonexus.v1.GoNexusService/<Method>
Content-Type: application/json
```

Examples:

```bash
# list repos
curl -s localhost:8080/gonexus.v1.GoNexusService/Repos \
  -H 'Content-Type: application/json' -d '{}'

# search
curl -s localhost:8080/gonexus.v1.GoNexusService/Query \
  -H 'Content-Type: application/json' \
  -d '{"repo":"myrepo","q":"parse config","limit":10}'

# blast radius of a symbol
curl -s localhost:8080/gonexus.v1.GoNexusService/Impact \
  -H 'Content-Type: application/json' \
  -d '{"repo":"myrepo","id":"github.com/acme/app/config.Load"}'

# what does my current git diff affect?
curl -s localhost:8080/gonexus.v1.GoNexusService/DetectChanges \
  -H 'Content-Type: application/json' -d '{"repo":"myrepo"}'

# plan a rename (apply:true to write the edits)
curl -s localhost:8080/gonexus.v1.GoNexusService/Rename \
  -H 'Content-Type: application/json' \
  -d '{"repo":"myrepo","id":"github.com/acme/app/config.Load","new_name":"Read","apply":false}'
```

Methods: `Index`, `Query`, `Context`, `Impact`, `Trace`, `Subgraph`, `Repos`,
`EntryPoints`, `Process`, `Clusters`, `DetectChanges`, `Rename`, `Wiki`. Full
message shapes are in [`proto/gonexus/v1/gonexus.proto`](proto/gonexus/v1/gonexus.proto).
Symbol ids come from `Query`/`Context` results (Go: `pkg/path.Type.Method`;
TS/Vue: `relpath#Symbol`).

## Configuration (environment)

All optional. Unset → sensible defaults (BM25 search, structural wiki).

| Variable | Effect |
|----------|--------|
| `GONEXUS_EMBED_URL` | OpenAI-compatible embeddings endpoint → adds semantic reranking to `query` |
| `GONEXUS_EMBED_MODEL` | embedding model name |
| `GONEXUS_EMBED_KEY` | Bearer token for the embeddings endpoint |
| `GONEXUS_LLM_URL` | OpenAI-compatible chat endpoint → adds LLM prose to `wiki` |
| `GONEXUS_LLM_MODEL` | chat model name |
| `GONEXUS_LLM_KEY` | Bearer token for the chat endpoint |
| `GONEXUS_TS_EXTRACTOR` | path to `extract.mjs` if not at `tools/ts-extractor/extract.mjs` |

Semantic search needs the embed vars set **at index time** (to store node
vectors) *and* at query time (to embed the query). Example with a local Ollama:

```bash
export GONEXUS_EMBED_URL=http://localhost:11434/v1/embeddings
export GONEXUS_EMBED_MODEL=nomic-embed-text
gonexus index /path/to/repo myrepo   # stores vectors
gonexus serve :8080                   # embeds queries
```

## How it works

```
                    ┌── Go: go/packages + go/types ──┐
  source repo ──────┤                                 ├──► knowledge graph ──► queries
                    └── TS/JS/Vue: tsc + compiler-sfc ┘     (nodes + edges)      │
                                                                                 │
   Vue UI ◄── Connect/gRPC ◄── server ◄─────────────────────────────────────────┤
   agents ◄── MCP stdio   ◄──── mcp    ◄─────────────────────────────────────────┘
```

- **Nodes**: packages, files, functions, methods, types, interfaces, classes,
  Vue components. **Edges**: `calls`, `imports`, `implements`, `defines`.
- Go and TS/Vue are indexed by their own compilers, then **merged** into one
  graph — a repo with a Go backend and a Vue frontend is a single graph.
- The graph is held in memory and persisted as JSON. Queries (impact, trace,
  clustering, search) run over it; BM25 search is built-in, embeddings optional.

Package layout:

```
proto/gonexus/v1/   gRPC/Connect contract (source of truth)
gen/                 generated Go (buf)
internal/graph/      graph model + queries (search, impact, trace, process, cluster)
internal/index/      Go indexer, TS bridge, incremental fingerprinting
internal/registry/   ~/.gonexus registry + per-repo graph store
internal/embed/      optional embeddings client
internal/llm/        optional chat client (wiki prose)
internal/changes/    git-diff → symbols + blast radius
internal/rename/     coordinated multi-file rename
internal/wiki/       architecture doc generation
internal/server/     Connect handlers
internal/mcp/        MCP stdio server (agent tools)
cmd/gonexus/        CLI entrypoint
tools/ts-extractor/  Node sidecar: TS/JS/Vue → {nodes, edges}
web/                 Vue 3 + Vite UI (Sigma.js WebGL graph)
```

## Development

```bash
go test ./...          # run the test suite
go build ./...         # build everything
```

Regenerate the gRPC contract after editing `proto/` (needs `buf`):

```bash
go install github.com/bufbuild/buf/cmd/buf@latest \
  google.golang.org/protobuf/cmd/protoc-gen-go@latest \
  connectrpc.com/connect/cmd/protoc-gen-connect-go@latest
buf generate
```

The TS/Vue extractor is a standalone Node script
([`tools/ts-extractor/extract.mjs`](tools/ts-extractor/extract.mjs)); run it
directly for debugging:

```bash
node tools/ts-extractor/extract.mjs /path/to/js-or-vue/project | jq .
```

## Design notes & limitations

Deliberate, marked with `ponytail:` comments in the code (each names its upgrade
path):

- **In-memory graph + JSON persist**, not a graph DB — swap when a repo exceeds RAM.
- **Incremental is per-language**, not per-file — editing any Go file rebuilds
  the Go subgraph (but skips TS, and vice versa).
- **`rename` is whole-word + graph-guided**, not compiler-exact — confidence
  scores flag ambiguous names; use gopls when you need a verified Go rename.
- **Clustering uses Label Propagation** — fast and dependency-free, but can form
  one large community; Louvain would partition more finely.
- **Semantic search / wiki prose need an external model** — without one you get
  BM25 search and a structural (non-narrative) wiki, both fully functional.
