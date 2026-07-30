<script setup>
import { ref, computed, onMounted, onBeforeUnmount } from "vue";
import { api } from "./api.js";
import GraphView from "./GraphView.vue";

// Node fill by kind — kept in sync with GraphView's palette for legend/toggles.
const KIND_COLOR = {
  package: "#6e7681",
  file: "#8b949e",
  type: "#f0883e",
  class: "#f0883e",
  interface: "#d2a8ff",
  func: "#58a6ff",
  method: "#79c0ff",
  component: "#7ee787",
  var: "#a5d6ff",
  const: "#a5d6ff",
};
const KIND_ORDER = ["package", "file", "type", "class", "interface", "func", "method", "component", "var", "const"];

const repos = ref([]);
const repo = ref("");
const connected = ref(true);
const error = ref("");

const allNodes = ref([]);
const allEdges = ref([]);
const truncated = ref(false);

const q = ref("");
const results = ref([]);
const showResults = ref(false);
const searchBox = ref(null);

const focusId = ref("");
const selected = ref(null); // { node, incoming, outgoing }
const impact = ref([]);
const source = ref(null); // { file, startLine, code, lang }

const hiddenKinds = ref({}); // kind -> true = hidden in the graph
const focusDepth = ref(0); // 0 = all
const graphMax = ref(false);

const ask = ref("");
const answer = ref("");
const asking = ref(false);

// Node kinds actually present, in a stable order, for the sidebar toggles.
const presentKinds = computed(() => {
  const set = new Set(allNodes.value.map((n) => n.kind));
  const ordered = KIND_ORDER.filter((k) => set.has(k));
  for (const k of set) if (!ordered.includes(k)) ordered.push(k);
  return ordered;
});

onMounted(async () => {
  window.addEventListener("keydown", onKey);
  try {
    const r = await api.repos();
    repos.value = r.repos || [];
    connected.value = true;
    if (repos.value.length) {
      // Default to the repo with the most symbols (avoids empty testdata repos).
      repo.value = repos.value.reduce((a, b) => ((b.nodes || 0) > (a.nodes || 0) ? b : a)).name;
      await loadGraph();
    }
  } catch (e) {
    connected.value = false;
  }
});
onBeforeUnmount(() => window.removeEventListener("keydown", onKey));

function onKey(e) {
  if ((e.metaKey || e.ctrlKey) && e.key.toLowerCase() === "k") {
    e.preventDefault();
    searchBox.value?.focus();
  }
}

async function loadGraph() {
  error.value = "";
  focusId.value = "";
  selected.value = null;
  source.value = null;
  try {
    const g = await api.graph(repo.value);
    allNodes.value = g.nodes || [];
    allEdges.value = g.edges || [];
    truncated.value = !!g.truncated;
  } catch (e) {
    error.value = String(e);
  }
}

async function search() {
  if (!q.value.trim()) {
    results.value = [];
    showResults.value = false;
    return;
  }
  try {
    const r = await api.query(q.value, repo.value, 20);
    results.value = r.results || [];
    showResults.value = true;
  } catch (e) {
    error.value = String(e);
  }
}

async function select(id) {
  error.value = "";
  impact.value = [];
  focusId.value = id;
  showResults.value = false;
  try {
    selected.value = await api.context(id, repo.value);
    source.value = null;
    source.value = await api.source(id, repo.value);
  } catch (e) {
    error.value = String(e);
  }
}

async function showImpact() {
  if (!selected.value?.node) return;
  const r = await api.impact(selected.value.node.id, repo.value);
  impact.value = r.ids || [];
}

async function runAsk() {
  if (!ask.value.trim()) return;
  asking.value = true;
  answer.value = "";
  try {
    const r = await api.ask(ask.value, repo.value);
    answer.value = r.answer || "";
  } catch (e) {
    answer.value = "Error: " + e;
  } finally {
    asking.value = false;
  }
}

function toggleKind(k) {
  hiddenKinds.value = { ...hiddenKinds.value, [k]: !hiddenKinds.value[k] };
}
const shortId = (id) => id.split("/").pop();
const codeLines = computed(() => (source.value ? source.value.code.split("\n") : []));
</script>

<template>
  <div class="app" :class="{ max: graphMax }">
    <!-- Top bar -->
    <header class="topbar">
      <div class="brand"><span class="logo">◈</span> GoNexus</div>
      <div class="repo-pill" v-if="repos.length">
        <span class="dot" :class="{ on: connected }"></span>
        <select v-model="repo" @change="loadGraph">
          <option v-for="r in repos" :key="r.name" :value="r.name">{{ r.name }} ({{ r.nodes }})</option>
        </select>
      </div>
      <span v-if="!connected" class="badge warn">no backend — run <code>gonexus serve</code></span>
      <div class="spacer"></div>
      <div class="searchwrap">
        <input
          ref="searchBox"
          v-model="q"
          class="search"
          placeholder="Search nodes…   ⌘K"
          @input="search"
          @focus="showResults = results.length > 0"
          @keyup.enter="search"
        />
        <ul v-if="showResults && results.length" class="results">
          <li v-for="n in results" :key="n.id" @click="select(n.id)">
            <span class="dotk" :style="{ background: KIND_COLOR[n.kind] || '#8b949e' }"></span>
            <span class="rname">{{ n.name }}</span>
            <span class="rkind">{{ n.kind }}</span>
            <span class="rpkg">{{ n.package }}</span>
          </li>
        </ul>
      </div>
    </header>

    <!-- Body: sidebar | inspector | graph -->
    <div class="body">
      <aside class="sidebar" v-if="!graphMax">
        <h3>Filters</h3>
        <p class="hint">Toggle node types in the graph</p>
        <ul class="kinds">
          <li v-for="k in presentKinds" :key="k" @click="toggleKind(k)" :class="{ off: hiddenKinds[k] }">
            <span class="dotk" :style="{ background: KIND_COLOR[k] || '#8b949e' }"></span>
            <span class="kname">{{ k }}</span>
            <span class="eye">{{ hiddenKinds[k] ? "🚫" : "👁" }}</span>
          </li>
        </ul>

        <h3>Focus depth</h3>
        <p class="hint">Show nodes within N hops of the selected node</p>
        <div class="depths">
          <button
            v-for="d in [0, 1, 2, 3]"
            :key="d"
            :class="{ active: focusDepth === d }"
            @click="focusDepth = d"
          >
            {{ d === 0 ? "All" : d + " hop" }}
          </button>
        </div>

        <h3>Legend</h3>
        <ul class="legend">
          <li v-for="k in presentKinds" :key="k">
            <span class="dotk" :style="{ background: KIND_COLOR[k] || '#8b949e' }"></span> {{ k }}
          </li>
        </ul>
        <p v-if="truncated" class="hint warn">Graph capped to top nodes by degree.</p>
      </aside>

      <section class="inspector" v-if="!graphMax">
        <!-- Ask -->
        <form class="ask" @submit.prevent="runAsk">
          <input v-model="ask" placeholder="Ask about this codebase…" />
          <button type="submit" :disabled="asking">{{ asking ? "…" : "Ask" }}</button>
        </form>
        <pre v-if="answer" class="answer">{{ answer }}</pre>

        <!-- Code Inspector -->
        <div class="inspector-head">
          <span class="ii">&lt;/&gt;</span> Code Inspector
        </div>
        <div v-if="selected?.node" class="detail">
          <div class="node-title">
            <span class="dotk" :style="{ background: KIND_COLOR[selected.node.kind] || '#8b949e' }"></span>
            <span class="nname">{{ selected.node.name }}</span>
            <span class="nkind">{{ selected.node.kind }}</span>
          </div>
          <code class="sig" v-if="selected.node.signature">{{ selected.node.signature }}</code>
          <p class="loc">{{ selected.node.file }}:{{ selected.node.line }}</p>
          <button class="impact-btn" @click="showImpact">Blast radius →</button>
          <ul v-if="impact.length" class="impact">
            <li v-for="id in impact" :key="id">{{ shortId(id) }}</li>
          </ul>

          <div v-if="source" class="code">
            <div class="code-head">{{ source.lang || "source" }} · from line {{ source.startLine }}</div>
            <div class="code-body">
              <div v-for="(ln, i) in codeLines" :key="i" class="cl">
                <span class="ln">{{ source.startLine + i }}</span><span class="lc">{{ ln }}</span>
              </div>
            </div>
          </div>

          <div class="refs">
            <div class="ref-col">
              <h4>Callers / incoming ({{ selected.incoming?.length || 0 }})</h4>
              <ul>
                <li v-for="(e, i) in selected.incoming" :key="i" @click="select(e.from)">
                  <span class="ek">{{ e.kind }}</span> {{ shortId(e.from) }}
                </li>
              </ul>
            </div>
            <div class="ref-col">
              <h4>Callees / outgoing ({{ selected.outgoing?.length || 0 }})</h4>
              <ul>
                <li v-for="(e, i) in selected.outgoing" :key="i" @click="select(e.to)">
                  <span class="ek">{{ e.kind }}</span> {{ shortId(e.to) }}
                </li>
              </ul>
            </div>
          </div>
        </div>
        <p v-else class="empty">Select a node in the graph or search to inspect it.</p>
      </section>

      <section class="graph">
        <GraphView
          :nodes="allNodes"
          :edges="allEdges"
          :focus-id="focusId"
          :hidden-kinds="hiddenKinds"
          :focus-depth="focusDepth"
          :maximized="graphMax"
          @select="select"
          @toggle-max="graphMax = !graphMax"
        />
      </section>
    </div>

    <p v-if="error" class="error">{{ error }}</p>
  </div>
</template>

<style>
* { box-sizing: border-box; }
body { margin: 0; font-family: ui-sans-serif, system-ui, sans-serif; background: #0d1117; color: #e6edf3; }
.app { display: grid; grid-template-rows: 52px 1fr; height: 100vh; }

/* Top bar */
.topbar { display: flex; align-items: center; gap: 14px; padding: 0 16px; border-bottom: 1px solid #21262d; background: #0d1117; }
.brand { font-weight: 700; font-size: 16px; display: flex; align-items: center; gap: 6px; }
.logo { color: #a371f7; }
.repo-pill { display: flex; align-items: center; gap: 6px; background: #161b22; border: 1px solid #30363d; border-radius: 20px; padding: 3px 10px; }
.repo-pill select { background: transparent; border: 0; color: #e6edf3; font-size: 13px; outline: none; cursor: pointer; }
.dot { width: 8px; height: 8px; border-radius: 50%; background: #6e7681; }
.dot.on { background: #3fb950; }
.spacer { flex: 1; }
.searchwrap { position: relative; width: 420px; max-width: 40vw; }
.search { width: 100%; padding: 8px 12px; background: #161b22; border: 1px solid #30363d; border-radius: 8px; color: #e6edf3; }
.searchwrap .results { position: absolute; top: 40px; left: 0; right: 0; list-style: none; margin: 0; padding: 4px; background: #161b22; border: 1px solid #30363d; border-radius: 8px; max-height: 60vh; overflow: auto; z-index: 20; }
.searchwrap .results li { display: flex; align-items: center; gap: 8px; padding: 7px 8px; border-radius: 6px; cursor: pointer; }
.searchwrap .results li:hover { background: #1f2630; }
.rname { font-weight: 600; }
.rkind { font-size: 11px; color: #7d8590; text-transform: uppercase; }
.rpkg { font-size: 11px; color: #58a6ff; margin-left: auto; max-width: 45%; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.badge.warn { background: #3d2b16; color: #f0d9a8; border: 1px solid #9e6a03; padding: 3px 8px; border-radius: 6px; font-size: 12px; }

/* Body */
.body { display: grid; grid-template-columns: 250px minmax(340px, 42%) 1fr; min-height: 0; }
.app.max .body { grid-template-columns: 1fr; }
.sidebar, .inspector { overflow: auto; min-height: 0; padding: 14px; border-right: 1px solid #21262d; }
.sidebar h3 { font-size: 12px; text-transform: uppercase; letter-spacing: 0.05em; color: #7d8590; margin: 16px 0 6px; }
.sidebar h3:first-child { margin-top: 0; }
.hint { font-size: 12px; color: #7d8590; margin: 0 0 8px; }
.hint.warn { color: #f0d9a8; }
.kinds, .legend { list-style: none; margin: 0; padding: 0; }
.kinds li { display: flex; align-items: center; gap: 8px; padding: 6px 8px; border-radius: 6px; cursor: pointer; }
.kinds li:hover { background: #1f2630; }
.kinds li.off { opacity: 0.4; }
.kinds .kname { flex: 1; }
.kinds .eye { font-size: 11px; }
.legend li { display: flex; align-items: center; gap: 8px; font-size: 12px; color: #9da7b3; padding: 3px 0; }
.dotk { width: 10px; height: 10px; border-radius: 50%; flex: none; }
.depths { display: flex; flex-wrap: wrap; gap: 6px; }
.depths button { padding: 5px 10px; font-size: 12px; background: #161b22; border: 1px solid #30363d; border-radius: 6px; color: #e6edf3; cursor: pointer; }
.depths button.active { background: #1f6feb; border-color: #1f6feb; }

/* Inspector */
.ask { display: flex; gap: 8px; margin-bottom: 10px; }
.ask input { flex: 1; padding: 8px 10px; background: #161b22; border: 1px solid #30363d; border-radius: 6px; color: #e6edf3; }
.ask button { padding: 8px 14px; background: #1f6feb; color: #fff; border: 0; border-radius: 6px; cursor: pointer; }
.answer { background: #161b22; border: 1px solid #30363d; border-radius: 6px; padding: 12px; white-space: pre-wrap; color: #c9d1d9; font-size: 13px; }
.inspector-head { display: flex; align-items: center; gap: 8px; font-weight: 600; margin: 12px 0; padding-bottom: 8px; border-bottom: 1px solid #21262d; }
.ii { color: #58a6ff; font-family: monospace; }
.node-title { display: flex; align-items: center; gap: 8px; }
.nname { font-weight: 700; font-size: 16px; }
.nkind { font-size: 11px; color: #7d8590; text-transform: uppercase; }
.sig { display: block; background: #161b22; padding: 8px; border-radius: 6px; white-space: pre-wrap; font-size: 12px; margin: 8px 0; }
.loc { font-size: 12px; color: #7d8590; margin: 4px 0; word-break: break-all; }
.impact-btn { padding: 6px 12px; background: #238636; color: #fff; border: 0; border-radius: 6px; cursor: pointer; }
.impact { list-style: none; padding: 8px 0 0; margin: 0; max-height: 140px; overflow: auto; font-size: 12px; color: #9da7b3; }
.code { margin: 12px 0; border: 1px solid #30363d; border-radius: 6px; overflow: hidden; }
.code-head { background: #161b22; padding: 6px 10px; font-size: 11px; color: #7d8590; border-bottom: 1px solid #30363d; }
.code-body { overflow: auto; max-height: 42vh; background: #0d1117; font-family: ui-monospace, Menlo, monospace; font-size: 12.5px; }
.cl { display: flex; white-space: pre; }
.ln { color: #4d5763; text-align: right; padding: 0 10px; user-select: none; min-width: 44px; }
.lc { color: #c9d1d9; padding-right: 12px; }
.refs { display: grid; grid-template-columns: 1fr 1fr; gap: 12px; margin-top: 12px; }
.ref-col h4 { font-size: 12px; color: #7d8590; margin: 0 0 6px; }
.ref-col ul { list-style: none; margin: 0; padding: 0; max-height: 220px; overflow: auto; }
.ref-col li { padding: 4px 6px; border-radius: 5px; cursor: pointer; font-size: 12px; }
.ref-col li:hover { background: #1f2630; }
.ek { color: #7d8590; font-size: 10px; }
.empty { color: #7d8590; }

/* Graph column */
.graph { min-height: 0; }
.error { position: fixed; bottom: 10px; left: 270px; color: #f85149; font-size: 13px; }
</style>
