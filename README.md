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
go install ./cmd/gonexus                       # backend binary → $GOPATH/bin/gonexus
cd tools/ts-extractor && npm install && cd -   # TS/Vue indexing (optional)
```

The **web UI is prebuilt and committed** (`web/dist/`), so `gonexus serve`
serves it with no frontend build step — a `git pull` always brings the current
UI. You only need `cd web && npm install && npm run build` if you're changing
the frontend yourself.

**Prerequisites:** Go 1.25+ (and the Go toolchain present for repos you index),
Node 18+ (only for TS/Vue indexing or rebuilding the UI), git (for diff-impact).
An OpenAI-compatible endpoint is optional (semantic search, wiki prose, chat).

## Quick start

```bash
gonexus index /path/to/your/repo myrepo    # build + register the graph
gonexus serve                              # API + web UI on http://127.0.0.1:8080
#   … or expose to Claude Code:
claude mcp add gonexus -- $(which gonexus) mcp
```

`gonexus serve` hosts **both** the JSON/gRPC API and the web UI on
`127.0.0.1:8080` — open that URL in a browser to explore the graph. Then ask
questions from any surface: the CLI (`gonexus impact myrepo <symbol>`), the API
(`curl … /Impact`), an agent (the `impact` MCP tool), or the UI.

> `gonexus setup` (run from inside a repo) does index + register and prints the
> exact `claude mcp add …` line for you.

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
- *How:* `gonexus group create svc` → `group add svc api` → `group add svc web`
  → `group sync svc`.
- *Why:* blast radius that crosses service boundaries — essential for a
  microservice org.

**Cross-repo graph** (web UI · `GroupGraph` API)
- *What:* the merged graph of every repo in a group, nodes colored by repo, with
  the cross-repo **contract edges** (shared symbol/route names) drawn between
  them — e.g. a Go backend and its Vue frontend as one picture.
- *How:* in the web UI toggle **Cross-repo** and pick a group; or call the
  `GroupGraph` API.
- *Why:* see how services actually connect, not just that they do.

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

A single-page **graph-explorer workspace** (Vue 3), served by `gonexus serve` at
`http://127.0.0.1:8080`. Layout:

- **Top bar** — repo pill with a live status dot, a **Single / Cross-repo**
  toggle, and a `⌘K` node search.
- **Left sidebar** — two tabs: **Explorer** (a folder → file → symbol tree; in
  cross-repo mode the repo is the top level) and **Filters** (per-kind
  visibility toggles with icons, **focus-depth** hops All/1/2/3/5, color legend).
- **Code Inspector** (center) — the selected symbol's **syntax-highlighted
  source** (highlight.js), an "✨ N references" badge, **Blast radius** button,
  and clickable callers / callees.
- **Graph** (right) — an always-on **WebGL graph of the whole repo** (Sigma.js +
  ForceAtlas2), nodes colored by kind with cyan filename pills and directional
  arrows. **Hover** to isolate a node's connections, **click** to pin that
  spotlight (click again / empty space to release), **Blast radius** lights the
  impacted set red, **Maximize** for full-screen. Layout runs in a **Web Worker**
  so even large (cross-repo) graphs never freeze the page.
- **💬 Ask AI** — a docked chat panel; answers cite graph symbols as clickable
  source chips that jump to the inspector.

- *How:* `gonexus serve`, then open `http://127.0.0.1:8080`. (For frontend
  development with hot reload: `cd web && npm run dev` on `:5173`, pointing at
  the API via `VITE_GONEXUS_URL`.)
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

**Multi-repo:** repos are registered in `~/.gonexus/registry.json`, and each
repo's graph + caches live **centrally** under `~/.gonexus/cache/<key>/`
(keyed by the repo's absolute path) — indexing never writes into the repo
itself. One `serve`/`mcp` process handles all registered repos and hot-reloads a
repo's graph when it changes on disk. `gonexus status` shows which indexes are
stale. Re-indexing is **incremental** (per-language fingerprint cache): a no-op
reindex is ~0.01s vs ~1.8s cold, and editing only Go skips the TS toolchain
entirely.

**New in the API:** `Graph` (whole-repo node/edge set for the explorer),
`Source` (a symbol's source, resolved from its own node and confined to the repo
root), `Groups` / `GroupGraph` (the merged cross-repo graph). See the full method
list in `proto/gonexus/v1/gonexus.proto`.

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

The server can index arbitrary paths (which executes the Go/Node toolchain),
serve indexed source (the `Source` endpoint), and — unless read-only — edit
files, so it's not meant to be openly exposed.

- **Binds `127.0.0.1` by default.** **Fails closed** on a non-loopback bind
  without a token: `serve` refuses to start unless `GONEXUS_AUTH_TOKEN` is set.
- **Token auth** — `GONEXUS_AUTH_TOKEN` (constant-time compared) on every
  request; `/healthz` stays open for health checks.
- **Read-only mode** — `GONEXUS_READ_ONLY=1` disables `Index` and downgrades
  `Rename` to plan-only, for untrusted agents.
- **`Source` is path-safe** — it reads the file recorded on a symbol's own node
  and refuses anything outside the repo root, so it can't be used to read
  arbitrary host files.
- **Origin-checked CORS**, never `*`; a `ReadHeaderTimeout` guards against
  Slowloris.
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
internal/registry/  ~/.gonexus registry, central per-repo cache, per-repo store, groups
internal/changes/   git-diff → symbols + blast radius
internal/rename/    coordinated multi-file rename
internal/wiki/      architecture doc generation
internal/embed/ llm/  optional embeddings + chat clients
internal/server/    Connect handlers (+ auth/CORS)
internal/mcp/       MCP stdio server (tools + prompts)
internal/skills/    agent skill generation
cmd/gonexus/        CLI
tools/ts-extractor/ Node sidecar: TS/JS/Vue → {nodes, edges, shapes}
web/                Vue 3 + Vite UI (Sigma.js WebGL graph); web/dist/ is the
                    committed prebuilt bundle that `serve` hosts
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
