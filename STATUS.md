# GoNexus build status

Live checklist of what's built vs left. Roadmap detail in [README](README.md).

## Parity push (excl. language support)
- [x] **P5 — MCP guards**: read-only mode (`GONEXUS_MCP_READ_ONLY`), repo
      allowlist (`GONEXUS_MCP_ALLOWED_REPOS`), response budget
      (`GONEXUS_MCP_DEFAULT_MAX_TOKENS`). (`internal/mcp/guards.go`)
- [x] **P6 — inspection/query tools**: `tool_map`, `check` (id/dangling
      validation), `cypher` (single-hop pattern), `route_map` (HTTP routes),
      `api_impact` (route handler blast radius). gRPC + MCP.
      _`shape_check` (cross-language API response-shape validation) deferred._
- [x] P7 — repo groups: registry (`~/.gonexus/groups.json`), contract
      extraction (exported symbols + routes), cross-repo linking + impact.
      MCP `group_list`/`group_sync`, CLI `group create/add/remove/list/sync/impact`.
      (`internal/groups`)
- [x] P8 — agent surface: MCP prompts (`detect_impact`, `generate_map`), skills
      generation (`gonexus skills` → 6 standard + per-module), editor hook
      snippet (`gonexus hooks`). (`internal/skills`)
- [x] P9 — depth: Louvain (one-level modularity) replaces LPA; impact
      depth-grouping + confidence (`ImpactGraded`); process-grouped search
      (`process` on results); Porter-lite stemmer in the tokenizer.
- [x] P10 — web extras: bridge auto-detect banner + agent chat panel
      (`Ask` = RAG over the graph; LLM-optional). WebGL/WASM client-side deferred.
- [x] P11 — deploy/CLI: Dockerfile + `.github/workflows/ci.yml`;
      `setup`/`clean`/`doctor`/`uninstall` CLI.
- Excluded (impractical here): client-side WASM rebuild, ReAct browser agent,
      Cosign/SBOM/K8s signing, hosted SaaS, OCaml; `shape_check` deferred.

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
      (`.gonexus/{go,ts}.json` + `manifest.json`). Unchanged language reloaded
      from cache, its toolchain skipped. No-op reindex ~0.01s vs ~1.8s full;
      single-language edit skips the other toolchain. (`internal/index/incremental.go`)
      _Granularity is per-language; per-package Go / per-file TS is a later step._
- [x] Multi-repo registry (`~/.gonexus/registry.json`), one server/MCP → many
      repos. Each repo's graph in its own `<repo>/.gonexus/graph.json`; store
      mtime-reloads so a CLI reindex shows up live. Requests carry `repo`
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
- [x] Community detection / clustering into logical modules — Label Propagation
      over the call+implements graph; finds cross-package functional clusters.
      gRPC `Clusters`, MCP `clusters`. (`internal/graph/cluster.go`)
      _LPA can form one giant community; Louvain would split finer._
- [x] Process tracing from entry points — entry points = call roots (main,
      handlers, commands) from graph topology; `Process(id)` returns the
      forward call-tree flow through indexed code; `ProcessesOf(id)` = which
      flows reach a symbol. gRPC `EntryPoints`/`Process`, MCP `entrypoints`/
      `process`. (`internal/graph/process.go`)

## Phase 4 — agent & product surface
- [x] MCP server over stdio (`gonexus mcp`, `internal/mcp`) — 12 tools: repos,
      query, context, impact, trace, entrypoints, process, clusters,
      detect_changes, rename, wiki, reindex
- [x] Stale-index detection — `index.IsStale` (source fingerprint vs manifest);
      surfaced on `Repos`/`repos` (`stale` flag) + CLI `gonexus status`. A
      PreToolUse hook can shell `gonexus status` to warn agents.
- [x] WebGL graph viz (Sigma.js + graphology + ForceAtlas2) over `Subgraph` —
      `web/src/GraphView.vue`. Depth-2 neighborhood of the selected symbol,
      nodes colored by kind, directed edges, click a node to re-focus.
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
- LPA clustering can form one giant community (Louvain would split finer)
