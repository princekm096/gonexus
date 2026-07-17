<script setup>
import { ref, onMounted } from "vue";
import { api } from "./api.js";
import GraphView from "./GraphView.vue";

const repos = ref([]);
const repo = ref("");
const q = ref("");
const results = ref([]);
const selected = ref(null); // { node, incoming, outgoing }
const impact = ref([]);
const graphNodes = ref([]);
const graphEdges = ref([]);
const focusId = ref("");
const error = ref("");

onMounted(async () => {
  try {
    const r = await api.repos();
    repos.value = r.repos || [];
    if (repos.value.length) repo.value = repos.value[0].name;
  } catch (e) {
    error.value = String(e);
  }
});

async function search() {
  error.value = "";
  try {
    const r = await api.query(q.value, repo.value, 30);
    results.value = r.results || [];
  } catch (e) {
    error.value = String(e);
  }
}

async function select(id) {
  error.value = "";
  impact.value = [];
  focusId.value = id;
  try {
    selected.value = await api.context(id, repo.value);
    const sg = await api.subgraph(id, repo.value, 2);
    graphNodes.value = sg.nodes || [];
    graphEdges.value = sg.edges || [];
  } catch (e) {
    error.value = String(e);
  }
}

async function showImpact() {
  if (!selected.value?.node) return;
  const r = await api.impact(selected.value.node.id, repo.value);
  impact.value = r.ids || [];
}

const shortId = (id) => id.split("/").pop();
</script>

<template>
  <div class="wrap">
    <header>
      <h1>GoNexus</h1>
      <span class="sub">code knowledge graph</span>
    </header>

    <form class="search" @submit.prevent="search">
      <select v-model="repo" class="repo">
        <option v-for="r in repos" :key="r.name" :value="r.name">
          {{ r.name }} ({{ r.nodes }})
        </option>
      </select>
      <input v-model="q" placeholder="search symbols (e.g. Impact, Graph, Index)" />
      <button type="submit">Search</button>
    </form>

    <p v-if="error" class="error">{{ error }}</p>

    <div class="cols">
      <ul class="results">
        <li
          v-for="n in results"
          :key="n.id"
          :class="{ active: selected?.node?.id === n.id }"
          @click="select(n.id)"
        >
          <span class="kind">{{ n.kind }}</span>
          <span class="name">{{ n.name }}</span>
          <span class="pkg">{{ n.package }}</span>
        </li>
        <li v-if="!results.length" class="empty">no results</li>
      </ul>

      <section v-if="selected?.node" class="detail">
        <h2>{{ selected.node.name }}</h2>
        <code class="sig">{{ selected.node.signature }}</code>
        <p class="doc" v-if="selected.node.doc">{{ selected.node.doc }}</p>
        <p class="loc">{{ selected.node.file }}:{{ selected.node.line }}</p>

        <button class="impact-btn" @click="showImpact">Blast radius →</button>
        <ul v-if="impact.length" class="impact">
          <li v-for="id in impact" :key="id">{{ shortId(id) }}</li>
        </ul>

        <div class="edges">
          <div>
            <h3>Callers / incoming ({{ selected.incoming?.length || 0 }})</h3>
            <ul>
              <li v-for="(e, i) in selected.incoming" :key="i" @click="select(e.from)">
                <span class="ek">{{ e.kind }}</span> {{ shortId(e.from) }}
              </li>
            </ul>
          </div>
          <div>
            <h3>Callees / outgoing ({{ selected.outgoing?.length || 0 }})</h3>
            <ul>
              <li v-for="(e, i) in selected.outgoing" :key="i" @click="select(e.to)">
                <span class="ek">{{ e.kind }}</span> {{ shortId(e.to) }}
              </li>
            </ul>
          </div>
        </div>
      </section>
      <section v-else class="detail empty">select a symbol</section>
    </div>

    <GraphView
      class="graph"
      :nodes="graphNodes"
      :edges="graphEdges"
      :focus-id="focusId"
      @select="select"
    />
  </div>
</template>

<style>
body { margin: 0; font-family: ui-sans-serif, system-ui, sans-serif; background: #0d1117; color: #e6edf3; }
.wrap { max-width: 1100px; margin: 0 auto; padding: 24px; }
header { display: flex; align-items: baseline; gap: 10px; }
h1 { margin: 0; font-size: 22px; }
.sub { color: #7d8590; font-size: 13px; }
.search { display: flex; gap: 8px; margin: 16px 0; }
.search input { flex: 1; padding: 8px 10px; background: #161b22; border: 1px solid #30363d; border-radius: 6px; color: #e6edf3; }
.search .repo { padding: 8px 10px; background: #161b22; border: 1px solid #30363d; border-radius: 6px; color: #e6edf3; }
.search button, .impact-btn { padding: 8px 14px; background: #238636; color: #fff; border: 0; border-radius: 6px; cursor: pointer; }
.error { color: #f85149; }
.cols { display: grid; grid-template-columns: 340px 1fr; gap: 16px; }
.graph { margin-top: 16px; }
.results { list-style: none; margin: 0; padding: 0; max-height: 70vh; overflow: auto; }
.results li { padding: 8px 10px; border: 1px solid #30363d; border-radius: 6px; margin-bottom: 6px; cursor: pointer; display: flex; flex-direction: column; }
.results li.active { border-color: #238636; }
.results li.empty, .detail.empty { color: #7d8590; }
.kind { font-size: 11px; color: #7d8590; text-transform: uppercase; }
.name { font-weight: 600; }
.pkg { font-size: 11px; color: #58a6ff; word-break: break-all; }
.detail { border: 1px solid #30363d; border-radius: 6px; padding: 16px; }
.sig { display: block; background: #161b22; padding: 8px; border-radius: 6px; white-space: pre-wrap; font-size: 13px; }
.doc { color: #c9d1d9; }
.loc { color: #7d8590; font-size: 12px; }
.impact { list-style: none; padding: 0; }
.impact li { color: #f0883e; font-size: 13px; padding: 2px 0; }
.edges { display: grid; grid-template-columns: 1fr 1fr; gap: 16px; margin-top: 16px; }
.edges h3 { font-size: 13px; color: #7d8590; }
.edges ul { list-style: none; padding: 0; margin: 0; }
.edges li { padding: 4px 0; cursor: pointer; font-size: 13px; }
.edges li:hover { color: #58a6ff; }
.ek { font-size: 10px; color: #7d8590; }
</style>
