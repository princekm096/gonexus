# GoNexus build status

Live checklist of what's built vs left. Roadmap detail in [README](README.md).

## Parity push (excl. language support)
- [x] **P5 — MCP guards**: read-only mode (`GONEXUS_MCP_READ_ONLY`), repo
      allowlist (`GONEXUS_MCP_ALLOWED_REPOS`), response budget
      (`GONEXUS_MCP_DEFAULT_MAX_TOKENS`). (`internal/mcp/guards.go`)
- [x] **P6 — inspection/query tools**: `tool_map`, `check` (id/dangling
      validation), `cypher` (single-hop pattern), `route_map` (HTTP routes),
      `api_impact` (route handler blast radius), **`shape_check`** (cross-language:
      Go struct JSON fields vs TS/Vue property access; flags typos/stale fields).
      gRPC + MCP + CLI.
- [x] P7 — repo groups: registry (`~/.gonexus/groups.json`), contract
      extraction (exported symbols + routes), cross-repo linking + impact.
      MCP `group_list`/`group_sync`, CLI `group create/add/remove/list/sync/impact`.
      (`internal/groups`)
- [x] P8 — agent surface: MCP prompts (`detect_impact`, `generate_map`), skills
      generation (`gonexus skills` → 6 standard + per-module), editor hook
      snippet (`gonexus hooks`). (`internal/skills`)
- [x] P9 — depth: **multi-level Louvain + cohesion scoring** replaces LPA;
      impact depth-grouping + confidence (`ImpactGraded`); process-grouped
      search (`process` on results); **full Porter stemmer** (go-porterstemmer).
- [x] P10 — web extras: bridge auto-detect banner + agent chat panel; `Ask` is a
      **server-side ReAct agent** (LLM drives query/context/impact/trace/
      entrypoints tools to a fixpoint), degrades to RAG list without an LLM.
- [x] P11 — deploy/CLI: Dockerfile + `.github/workflows/ci.yml`;
      `setup`/`clean`/`doctor`/`uninstall` + read verbs `query`/`context`/
      `impact`/`trace`/`cypher`/`check`/`detect-changes` as CLI subcommands.
- [x] Editor hooks — **PreToolUse `gonexus enrich`** (injects blast-radius of the
      file being edited) + PostToolUse stale warning; `gonexus hooks` prints both.
- [x] Embeddings — device passthrough (`GONEXUS_EMBED_DEVICE`) + node cap
      (`GONEXUS_EMBED_MAX_NODES`, most-referenced symbols prioritized).
- Excluded (impractical here): client-side WASM rebuild, Cosign/SBOM/K8s
      signing, hosted SaaS, OCaml. **No functional gaps remain** in scope.

## Web UI parity (GitNexus-style workspace) ✅
Single-page graph explorer served by `gonexus serve` at `:8080`; `web/dist` is
committed so a pull runs it with no frontend build.
- [x] Top bar: repo pill + live status dot, **Single / Cross-repo** toggle,
      `⌘K` node search with results dropdown.
- [x] Left sidebar tabs: **Explorer** (folder→file→symbol tree, repo at top in
      cross-repo mode) and **Filters** (per-kind visibility toggles w/ icons,
      focus-depth All/1/2/3/5 hops, color legend). (`web/src/TreeNode.vue`)
- [x] **Code Inspector**: syntax-highlighted source (highlight.js) via the
      `Source` RPC, "✨ N references" badge, Blast radius button, clickable
      callers/callees.
- [x] Whole-repo WebGL graph: kind colors + cyan filename pills + arrows,
      hover-isolate, click-to-pin, blast-radius red overlay, Web-Worker layout,
      maximize, no node cap.
- [x] **Cross-repo graph**: `GroupGraph` merges a group's repos, colored by
      repo, with cross-repo contract edges.
- [x] **Docked Ask AI panel** (`web/src/ChatPanel.vue`): message history +
      clickable source chips that jump to the inspector.

## Phase 1 — spine ✅
- [x] Go indexer via `go/packages` + `go/types` (`internal/index`)
- [x] In-memory graph + JSON persist (`internal/graph`)
- [x] Queries: `context`, `impact`, `trace`, `query`, `subgraph`
- [x] gRPC/Connect API (connect-go, `internal/server`)
- [x] Vue 3 + Vite UI (`web/`)
- [x] CLI: `index`, `serve`

## Phase 2 — coverage & correctness ✅
- [x] Vue/TS/JS extractor (`tools/ts-extractor`, TypeScript compiler +
      @vue/compiler-sfc) → same graph model, merged via `index.BuildRepo`.
- [x] Precise cross-file call edges via `ts.Program` + type checker (real
      `ts.createProgram`, custom `.vue`-aware CompilerHost). Resolves imported
      functions, class methods, and same-name collisions.
- [x] Object-literal method nodes (`const api = {query(){}}`) + expression-body
      arrow scanning — calls into/out of them now resolve (e.g. Vue `App.vue`
      → `api.query`).
- [x] `implements` edges (`types.Implements`) — heritage tracking. Concrete
      type → every module-local interface it satisfies (value + pointer
      receiver), cross-package. `addImplementsEdges` in `internal/index/index.go`.
- [x] Incremental re-index — per-language fingerprint cache
      (`{go,ts}.json` + `manifest.json`). Unchanged language reloaded from cache,
      its toolchain skipped. No-op reindex ~0.01s vs ~1.8s full; single-language
      edit skips the other toolchain. (`internal/index/incremental.go`)
      _Granularity is per-language; per-package Go / per-file TS is a later step._
- [x] Multi-repo registry (`~/.gonexus/registry.json`), one server/MCP → many
      repos. Each repo's graph + caches live **centrally** under
      `~/.gonexus/cache/<key>/` (keyed by absolute path) — indexing never writes
      into the repo; a legacy in-repo `.gonexus/` is auto-removed on re-index.
      Store mtime-reloads so a CLI reindex shows up live. Requests carry `repo`
      (empty = sole repo). CLI `index <path> [name]` / `list`; `Repos` RPC +
      `repos` MCP tool; Vue repo selector. (`internal/registry`)

## Deep static analysis (Go)
- [x] Constructor inference — `NewX(...) (*X|X)` → `constructs` edge (always on,
      `internal/index/index.go`).
- [x] Framework/library detection — from import edges (`graph.Frameworks`);
      surfaced in the wiki.
- [x] PDG / control-flow graphs via Go SSA — per-function CFG (blocks + edges)
      + SSA def-use data dependence. Opt-in `GONEXUS_PDG=1`; gRPC `PdgQuery`,
      MCP `pdg_query`. (`internal/analysis`)
- [x] Taint analysis (source→sink) — intra-procedural value-flow over SSA
      def-use, default rule set (env/http input → exec/SQL/file ops). gRPC
      `Explain`, MCP `explain`.
      _Go-only (SSA); intra-procedural, no memory/alias; block-level control
      dependence. Upgrade path: pointer analysis + IFDS._

## Phase 3 — search & intelligence
- [x] Hybrid search: BM25 (always, pure Go — `internal/graph/search.go`) fused
      with optional embeddings via RRF. Camel/snake-aware tokenizer. Embeddings
      are opt-in through an OpenAI-compatible endpoint (`internal/embed`,
      `GONEXUS_EMBED_URL`); node vectors stored at index time, query embedded at
      search time. No embedder → clean BM25-only.
- [x] Community detection / clustering into logical modules — **multi-level
      Louvain** over the call+implements graph, each cluster with a **cohesion**
      score; finds cross-package functional clusters. gRPC `Clusters`, MCP
      `clusters`. (`internal/graph/cluster.go`)
- [x] Process tracing from entry points — entry points = call roots (main,
      handlers, commands) from graph topology; `Process(id)` returns the
      forward call-tree flow through indexed code; `ProcessesOf(id)` = which
      flows reach a symbol. gRPC `EntryPoints`/`Process`, MCP `entrypoints`/
      `process`. (`internal/graph/process.go`)

## Phase 4 — agent & product surface
- [x] MCP server over stdio (`gonexus mcp`, `internal/mcp`) — 22 tools: repos,
      query, context, impact, trace, entrypoints, process, clusters,
      detect_changes, rename, wiki, reindex, tool_map, check, cypher, route_map,
      api_impact, shape_check, explain, pdg_query, group_list, group_sync.
- [x] Additional Connect/gRPC methods for the web explorer: `Graph` (whole-repo
      node/edge set), `Source` (a symbol's source, path-confined to the repo),
      `Groups` + `GroupGraph` (merged cross-repo graph with contract edges).
- [x] Stale-index detection — `index.IsStale` (source fingerprint vs manifest);
      surfaced on `Repos`/`repos` (`stale` flag) + CLI `gonexus status`. A
      PreToolUse hook can shell `gonexus status` to warn agents.
- [x] WebGL graph viz (Sigma.js + graphology + ForceAtlas2) — now the
      **whole-repo** graph (`Graph`/`GroupGraph`), not a depth-2 neighborhood.
      Nodes colored by kind with cyan filename pills + directional arrows;
      hover-isolate, **click-to-pin** spotlight, **blast-radius** red highlight,
      node-type + focus-depth filters, maximize. Layout runs in a **Web Worker**
      so large graphs never freeze the page; no node cap. (`web/src/GraphView.vue`)
      See "Web UI parity" below.
- [x] Git-diff impact (`detect_changes`) — `git diff -U0` → changed line ranges
      → enclosing symbols → union blast radius (`Impact`). gRPC `DetectChanges`,
      MCP `detect_changes`. Optional `base` ref; default working tree vs HEAD.
      (`internal/changes`)
- [x] Coordinated multi-file `rename` with confidence scoring — graph finds the
      reference files (def + callers/impl + same-package for types), whole-word
      scan/replace, confidence = name uniqueness in the repo. Plan-only by
      default; `apply` writes. gRPC `Rename`, MCP `rename`. (`internal/rename`)
      _Heuristic (whole-word); gopls for compiler-exact Go renames = ceiling._
- [x] Wiki generation — deterministic architecture digest (overview, modules,
      entry points, key interfaces, most-called) always available; optional LLM
      narrative prose via an OpenAI-compatible chat endpoint (`internal/llm`,
      `GONEXUS_LLM_URL`). gRPC `Wiki`, MCP `wiki`, CLI `gonexus wiki [repo]`.
      (`internal/wiki`)
- [ ] Storage swap: in-memory → embedded graph DB — deferred (YAGNI until a repo
      exceeds RAM; upgrade path marked at `internal/graph/graph.go`)

## Known shortcuts (search `ponytail:` in code)
- In-memory graph + JSON persist, not a graph DB
- Per-language incremental granularity (not per-package/per-file)
- Rename is whole-word heuristic (not compiler-exact); confidence flags ambiguity
- Cross-repo contract links are name-key based (shared exported symbol/route
  names), not precise `.proto`/import resolution
