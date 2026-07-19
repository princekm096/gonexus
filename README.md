# GoNexus

**A code knowledge graph for AI agents and humans.** GoNexus indexes your
codebase into a graph of symbols (functions, types, components) and their
relationships (calls, imports, implements, constructs), then answers the
architectural questions that normally take a human — or an AI agent — many
exploratory reads to figure out: *what calls this? what breaks if I change it?
how does this request flow? what modules exist? does the frontend match the
backend?*

Stack: **Go + gRPC/Connect backend, Vue 3 + WebGL frontend, MCP for agents.**
Languages indexed: **Go** (via `go/packages` + `go/types`) and **TypeScript /
JavaScript / Vue** (via the TypeScript compiler + `@vue/compiler-sfc`) — real
type resolution, so the call graph is precise across files *and* languages.

> **Why it exists:** AI coding agents miss architectural context and make broken
> edits. GoNexus precomputes the relationships at index time, so an agent gets a
> complete answer in one call instead of ten grep/read round-trips — and humans
> get the same answers from a CLI, an API, or a web UI.

---

## Table of contents

- [Install](#install)
- [Quick start](#quick-start)
- [Core concept: the graph](#core-concept-the-graph)
- [Feature guide](#feature-guide) — what each feature is, how to use it, why it helps
  - [Search & navigation](#1-search--navigation)
  - [Change safety](#2-change-safety-impact--diffs)
  - [Architecture understanding](#3-architecture-understanding)
  - [Refactoring](#4-refactoring)
  - [Deep static analysis (Go)](#5-deep-static-analysis-go)
  - [API correctness](#6-api-correctness)
  - [Multi-repo](#7-multi-repo-groups)
  - [Agent integration](#8-agent-integration)
  - [Visualization](#9-web-ui--visualization)
- [Interfaces: CLI, API, MCP](#interfaces)
- [Configuration](#configuration)
- [Security](#security)
- [How it works](#how-it-works)
- [Development](#development)

---

## Install

```bash
git clone <this-repo> gonexus && cd gonexus
go build -o gonexus ./cmd/gonexus          # backend binary
cd tools/ts-extractor && npm install && cd -   # TS/Vue indexing (optional)
cd web && npm install && cd -                  # web UI (optional)
```

**Prerequisites:** Go 1.25+ (and the Go toolchain present for repos you index),
Node 18+ (for TS/Vue and the UI), git (for diff-impact). An OpenAI-compatible
endpoint is optional (semantic search, wiki prose, chat).

## Quick start

```bash
gonexus index /path/to/your/repo myrepo    # build + register the graph
gonexus serve                              # API on 127.0.0.1:8080
#   … or expose to Claude Code:
claude mcp add gonexus -- /abs/path/to/gonexus mcp
#   … or open the web UI:
cd web && npm run dev                       # http://localhost:5173
```

Then ask questions — from the CLI (`gonexus impact myrepo <symbol>`), the API
(`curl … /Impact`), an agent (the `impact` MCP tool), or the UI.

## Core concept: the graph

Everything is one graph per repo:

- **Nodes** — packages, files, functions, methods, types, interfaces, classes,
  Vue components. Each carries its signature, doc, location, and (for structs)
  field shape.
- **Edges** — `calls`, `imports`, `implements`, `defines`, `constructs`.
- Go and TS/Vue are indexed by their own compilers and **merged into one graph**,
  so a Go backend + Vue frontend is a single queryable model.

Every feature below is a query or transformation over this graph. Symbol **ids**
come from search/context results — Go: `pkg/path.Type.Method`; TS/Vue:
`relpath#Symbol`.

---

## Feature guide

Each feature: **what it is · how to use it · why it helps.** Tool names are the
MCP tool / the CLI verb (most also have a gRPC method).

### 1. Search & navigation

**Hybrid search** (`query`)
- *What:* find symbols by name or meaning. BM25 keyword ranking (with camelCase
  splitting + full Porter stemming) always on; fused via Reciprocal Rank Fusion
  with semantic embeddings when an embeddings endpoint is configured.
- *How:* `gonexus query myrepo "parse config"` · MCP `query` · results are
  grouped by the execution flow (process) they belong to.
- *Why:* one search that catches both exact names and conceptually-related code,
  even when you don't know the exact identifier.

**Context** (`context`)
- *What:* a 360° view of one symbol — signature, doc, location, and its incoming
  (callers) and outgoing (callees) edges.
- *How:* `gonexus context myrepo <id>` · MCP `context`.
- *Why:* understand a symbol and everything around it without opening five files.

**Cypher** (`cypher`)
- *What:* a graph pattern query, e.g. `(a:func)-[:calls]->(b:method)`.
- *How:* `gonexus cypher myrepo "(a:func)-[:calls]->(b:method)"` · MCP `cypher`.
- *Why:* ask structural questions ("which functions call methods?") directly.

### 2. Change safety (impact & diffs)

**Impact / blast radius** (`impact`)
- *What:* every symbol that transitively calls the target — grouped by call
  distance (depth) with a confidence (1/depth: direct callers 1.0, two hops 0.5).
- *How:* `gonexus impact myrepo <id>` · MCP `impact`.
- *Why:* know exactly what could break *before* you edit — the single most
  valuable pre-edit check.

**Git-diff impact** (`detect_changes`)
- *What:* maps your current `git diff` to the symbols it changes and their
  combined blast radius.
- *How:* `gonexus detect-changes myrepo [base]` · MCP `detect_changes`.
- *Why:* pre-commit / PR review — see what a change actually affects and what to
  re-test, no guessing.

**Trace** (`trace`)
- *What:* the shortest call path between two symbols.
- *How:* `gonexus trace myrepo <fromID> <toID>` · MCP `trace`.
- *Why:* answer "how does A reach B?" instantly.

### 3. Architecture understanding

**Entry points & process tracing** (`entrypoints`, `process`)
- *What:* `entrypoints` lists execution roots (main, HTTP handlers, CLI commands)
  ranked by reach; `process` returns the full call tree from a root.
- *How:* MCP `entrypoints` / `process`.
- *Why:* learn how an unfamiliar codebase actually runs, end to end.

**Clustering** (`clusters`)
- *What:* emergent modules found by **multi-level Louvain** community detection
  over the call graph, each with a **cohesion** score (how self-contained it is).
- *How:* MCP `clusters`.
- *Why:* discover the *real* functional structure — cross-package modules the
  directory layout doesn't show.

**Wiki** (`wiki`)
- *What:* architecture documentation generated from the graph — overview,
  frameworks detected, modules, entry points, key interfaces, most-called
  functions. Adds an LLM-authored narrative when a chat endpoint is configured.
- *How:* `gonexus wiki myrepo` (markdown to stdout) · MCP `wiki`.
- *Why:* onboarding docs that never go stale — regenerated from the code itself.

### 4. Refactoring

**Coordinated rename** (`rename`)
- *What:* a confidence-scored, multi-file rename plan (and optional apply). The
  graph finds the reference files; whole-word replace does the edits; confidence
  = how unique the name is (1.0 = safe to auto-apply).
- *How:* MCP `rename` (plan first, check confidence, then `apply:true`).
- *Why:* rename across files with a safety signal, without a full IDE.

### 5. Deep static analysis (Go)

Opt-in with `GONEXUS_PDG=1` at index time (heavier; Go only).

**Taint analysis** (`explain`)
- *What:* source→sink flows — untrusted input (env, HTTP request) reaching
  dangerous sinks (exec, SQL, file ops), via SSA def-use propagation.
- *How:* MCP `explain`.
- *Why:* catch injection-class bugs the call graph alone can't see.

**Program-dependence graph** (`pdg_query`)
- *What:* a function's control-flow graph (basic blocks + edges) and
  statement-level data dependence (SSA def-use count).
- *How:* MCP `pdg_query`.
- *Why:* precise intra-function reasoning for debugging and analysis.

**Always on (Go):** `constructs` edges (`NewX → X` constructor inference) and
**framework detection** (Gin, Echo, GORM, gRPC, Vue, React… from imports),
surfaced in the wiki.

### 6. API correctness

**Route map & API impact** (`route_map`, `api_impact`)
- *What:* `route_map` detects HTTP endpoint→handler mappings (net/http, gin,
  echo, chi); `api_impact` returns the blast radius of a route's handler.
- *How:* MCP `route_map` / `api_impact`.
- *Why:* see your API surface and what a route change affects.

**Shape check** (`shape_check`)
- *What:* cross-language validation — compares a consumer's property access
  (TS/Vue `user.email`) against the provider's struct fields (Go `User` JSON
  tags), flagging typos and stale/removed fields.
- *How:* `gonexus shape-check myrepo` · MCP `shape_check`.
- *Why:* catch frontend/backend contract drift that neither compiler sees alone.

**Structural check** (`check`)
- *What:* validates symbol ids exist and reports dangling edges.
- *How:* `gonexus check myrepo <ids…>` · MCP `check`.
- *Why:* a fast integrity check over the indexed graph.

### 7. Multi-repo (groups)

**Groups & cross-repo impact** (`group_list`, `group_sync`, `gonexus group …`)
- *What:* track several repos as a group; `group_sync` builds a cross-repo
  contract registry (shared exported symbols / routes) and links the repos;
  `group impact` shows which other repos a change may affect.
- *How:* `gonexus group create svc` → `group add svc api` → `group sync svc`.
- *Why:* blast radius that crosses service boundaries — essential for a
  microservice org.

### 8. Agent integration

**MCP server** (`gonexus mcp`) — exposes all the tools above over stdio to Claude
Code, Cursor, Codex, etc. Plus:

- **Prompts** — `detect_impact` (pre-commit change analysis) and `generate_map`
  (architecture doc with a mermaid diagram) as one-call workflows.
- **Skills** (`gonexus skills`) — generates ready-to-use agent skill files (6
  standard workflows + one per detected module) so agents know when to reach for
  each tool.
- **Editor hooks** (`gonexus hooks` / `gonexus enrich`) — a PreToolUse hook that
  injects the **blast radius of the file being edited** before the edit runs, and
  a PostToolUse hook that warns when the index is stale.
- **Chat / Ask** — a natural-language question answered by a **server-side ReAct
  agent** that drives the graph tools (query → context → impact …) to a
  conclusion; degrades to a relevant-symbols list without an LLM.
- *Why:* the whole point — agents get precomputed context and stop making broken,
  under-informed edits.

### 9. Web UI & visualization

- *What:* Vue 3 app with a repo selector, symbol search/detail, an **Ask** chat
  panel, and a **WebGL graph** (Sigma.js + ForceAtlas2) showing a symbol's
  neighborhood — click a node to re-focus. Auto-detects a local `gonexus serve`.
- *How:* `cd web && npm run dev`.
- *Why:* humans explore the same graph visually that agents query programmatically.

---

## Interfaces

Everything is available three ways:

| Interface | For | How |
|-----------|-----|-----|
| **CLI** | humans, scripts, CI | `gonexus <verb> <repo> …` |
| **API** | any service (Connect = plain HTTP POST JSON, also gRPC / gRPC-Web) | `POST /gonexus.v1.GoNexusService/<Method>` |
| **MCP** | AI agents | `claude mcp add gonexus -- /abs/gonexus mcp` |

**CLI commands:** `index`, `list`, `status`, `remove`, `clean`, `query`,
`context`, `impact`, `trace`, `cypher`, `check`, `shape-check`, `detect-changes`,
`group …`, `skills`, `hooks`, `enrich`, `wiki`, `serve`, `mcp`, `setup`,
`doctor`, `uninstall`.

**API example:**
```bash
curl -s localhost:8080/gonexus.v1.GoNexusService/Impact \
  -H 'Content-Type: application/json' \
  -d '{"repo":"myrepo","id":"github.com/acme/app/config.Load"}'
```

**Multi-repo:** repos live in `~/.gonexus/registry.json`; each repo's graph is in
its own `<repo>/.gonexus/graph.json`. One `serve`/`mcp` process handles them all
and hot-reloads a repo's graph when it changes on disk. `gonexus status` shows
which indexes are stale. Re-indexing is **incremental** (per-language
fingerprint cache): a no-op reindex is ~0.01s vs ~1.8s cold, and editing only Go
skips the TS toolchain entirely.

## Configuration

All optional. Unset → sensible defaults (BM25 search, structural wiki, no auth,
loopback bind).

| Variable | Effect |
|----------|--------|
| `GONEXUS_EMBED_URL` / `_MODEL` / `_KEY` | OpenAI-compatible embeddings → semantic search |
| `GONEXUS_EMBED_DEVICE` | device passthrough (cpu/cuda/mps/…) |
| `GONEXUS_EMBED_MAX_NODES` | cap embedded nodes (most-referenced prioritized) |
| `GONEXUS_LLM_URL` / `_MODEL` / `_KEY` | OpenAI-compatible chat → wiki prose, Ask agent |
| `GONEXUS_PDG` | `1` at index time to build SSA PDG + taint (Go) |
| `GONEXUS_AUTH_TOKEN` | require `Authorization: Bearer <token>` on the API |
| `GONEXUS_READ_ONLY` | `1` disables `Index`, forces `Rename` plan-only |
| `GONEXUS_CORS_ORIGINS` | extra allowed browser origins (localhost always allowed) |
| `GONEXUS_TS_EXTRACTOR` | path to `extract.mjs` if not at the default |

## Security

The server can read indexed source and (unless read-only) edit files, so it's
not meant to be openly exposed.

- **Binds `127.0.0.1` by default**; warns if bound to a public interface without
  a token.
- **Token auth** — `GONEXUS_AUTH_TOKEN` (constant-time compared) on every request.
- **Read-only mode** — `GONEXUS_READ_ONLY=1` for untrusted agents.
- **Origin-checked CORS**, never `*`.
- Docker image runs as a **non-root** user; `git` refs validated against option
  injection; rename targets validated as identifiers.

## How it works

```
                    ┌── Go: go/packages + go/types ──┐
  source repo ──────┤                                 ├──► knowledge graph ──► queries
                    └── TS/JS/Vue: tsc + compiler-sfc ┘     (nodes + edges)      │
                                                                                 │
   Vue UI ◄── Connect/gRPC ◄── server ◄─────────────────────────────────────────┤
   agents ◄── MCP stdio   ◄──── mcp    ◄─────────────────────────────────────────┘
```

Package layout:

```
proto/gonexus/v1/   gRPC/Connect contract (source of truth)
gen/                generated Go (buf)
internal/graph/     graph model + queries (search, impact, trace, process, cluster, shape)
internal/index/     Go indexer, TS bridge, incremental fingerprinting
internal/analysis/  Go SSA: PDG + taint
internal/registry/  ~/.gonexus registry, per-repo store, groups
internal/changes/   git-diff → symbols + blast radius
internal/rename/    coordinated multi-file rename
internal/wiki/      architecture doc generation
internal/embed/ llm/  optional embeddings + chat clients
internal/server/    Connect handlers (+ auth/CORS)
internal/mcp/       MCP stdio server (tools + prompts)
internal/skills/    agent skill generation
cmd/gonexus/        CLI
tools/ts-extractor/ Node sidecar: TS/JS/Vue → {nodes, edges, shapes}
web/                Vue 3 + Vite UI (Sigma.js WebGL graph)
```

## Development

```bash
go test ./...     # 39 tests
go build ./...
```

Regenerate the gRPC contract after editing `proto/` (needs `buf`):

```bash
go install github.com/bufbuild/buf/cmd/buf@latest \
  google.golang.org/protobuf/cmd/protoc-gen-go@latest \
  connectrpc.com/connect/cmd/protoc-gen-connect-go@latest
buf generate
```

See [STATUS.md](STATUS.md) for the full built-vs-remaining checklist. Deliberate
simplifications are marked with `ponytail:` comments in the code, each naming its
upgrade path.
