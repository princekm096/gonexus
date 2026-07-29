<script setup>
import { ref, onMounted, onBeforeUnmount, watch, nextTick } from "vue";
import Graph from "graphology";
import Sigma from "sigma";
import forceAtlas2 from "graphology-layout-forceatlas2";

const props = defineProps({
  nodes: { type: Array, default: () => [] },
  edges: { type: Array, default: () => [] },
  focusId: { type: String, default: "" },
});
const emit = defineEmits(["select"]);

const container = ref(null);
const maximized = ref(false);
let renderer = null;
let graph = null;
let hovered = null; // currently hovered node id (drives the highlight reducers)

// Node fill by kind; edges tinted by relationship.
const KIND_COLOR = {
  func: "#58a6ff",
  method: "#79c0ff",
  type: "#f0883e",
  interface: "#d2a8ff",
  class: "#f0883e",
  component: "#7ee787",
  file: "#8b949e",
  package: "#6e7681",
  var: "#a5d6ff",
  const: "#a5d6ff",
};
const EDGE_COLOR = {
  calls: "#30414d",
  implements: "#8957e5",
  imports: "#3d3d3d",
  defines: "#21262d",
};
const DIM = "#22272e"; // faded color for nodes/edges outside the hovered neighborhood

function build() {
  if (renderer) {
    renderer.kill();
    renderer = null;
  }
  hovered = null;
  if (!container.value || props.nodes.length === 0) return;

  const g = new Graph({ multi: false });
  for (const n of props.nodes) {
    g.mergeNode(n.id, {
      label: n.name || n.id,
      kind: n.kind,
      color: n.id === props.focusId ? "#ffffff" : KIND_COLOR[n.kind] || "#8b949e",
      x: Math.random(),
      y: Math.random(),
    });
  }
  for (const e of props.edges) {
    if (g.hasNode(e.from) && g.hasNode(e.to) && !g.hasEdge(e.from, e.to)) {
      g.addEdge(e.from, e.to, { color: EDGE_COLOR[e.kind] || "#30414d", size: 1 });
    }
  }

  // Size by degree so hubs read as hubs; the focus node stays largest. Labels
  // are gated on rendered size, so only the important nodes label by default —
  // that's what keeps a dense graph from turning into a wall of text.
  let maxDeg = 1;
  g.forEachNode((id) => (maxDeg = Math.max(maxDeg, g.degree(id))));
  g.forEachNode((id, attr) => {
    const deg = g.degree(id);
    const base = 4 + 10 * Math.sqrt(deg / maxDeg);
    g.setNodeAttribute(id, "size", id === props.focusId ? base + 6 : base);
  });

  // inferSettings tunes gravity/scaling to the graph's size; more iterations for
  // bigger graphs, Barnes-Hut so it stays fast when dense.
  const settings = forceAtlas2.inferSettings(g);
  forceAtlas2.assign(g, {
    iterations: Math.min(600, 150 + g.order),
    settings: { ...settings, barnesHutOptimize: g.order > 150, adjustSizes: true },
  });

  graph = g;
  renderer = new Sigma(g, container.value, {
    renderEdgeLabels: false,
    defaultEdgeType: "arrow",
    labelColor: { color: "#c9d1d9" },
    labelDensity: 0.35,
    labelGridCellSize: 80,
    labelRenderedSizeThreshold: 11, // only bigger (higher-degree) nodes label
    zIndex: true,
    nodeReducer,
    edgeReducer,
  });
  renderer.on("clickNode", ({ node }) => emit("select", node));
  renderer.on("enterNode", ({ node }) => {
    hovered = node;
    renderer.refresh();
  });
  renderer.on("leaveNode", () => {
    hovered = null;
    renderer.refresh();
  });
}

// When a node is hovered, spotlight it + its direct neighbors and fade the rest.
// This is the single biggest readability win on a dense graph: you see one
// symbol's actual connections instead of the whole hairball at once.
function nodeReducer(id, data) {
  if (!hovered) return data;
  if (id === hovered || graph.areNeighbors(hovered, id)) {
    return { ...data, zIndex: 1, forceLabel: true };
  }
  return { ...data, color: DIM, label: "", zIndex: 0 };
}
function edgeReducer(edge, data) {
  if (!hovered) return data;
  const [s, t] = graph.extremities(edge);
  if (s === hovered || t === hovered) return { ...data, color: "#4b5563", zIndex: 1 };
  return { ...data, hidden: true };
}

function fit() {
  if (renderer) renderer.getCamera().animatedReset();
}
function toggleMax() {
  maximized.value = !maximized.value;
  // The container resized — let Sigma re-measure, then reframe.
  nextTick(() => renderer && (renderer.refresh(), fit()));
}
function onKey(e) {
  if (e.key === "Escape" && maximized.value) toggleMax();
}

onMounted(() => {
  build();
  window.addEventListener("keydown", onKey);
});
onBeforeUnmount(() => {
  window.removeEventListener("keydown", onKey);
  renderer && renderer.kill();
});
watch(() => [props.nodes, props.focusId], build, { deep: false });
</script>

<template>
  <div class="graphwrap" :class="{ max: maximized }">
    <div ref="container" class="canvas"></div>

    <div v-if="nodes.length" class="controls">
      <button title="Fit graph to view" @click="fit">⤾ Fit</button>
      <button :title="maximized ? 'Exit fullscreen (Esc)' : 'Fullscreen'" @click="toggleMax">
        {{ maximized ? "✕ Close" : "⤢ Fullscreen" }}
      </button>
    </div>

    <p v-if="nodes.length" class="tip">hover a node to isolate its connections · scroll to zoom · drag to pan</p>
    <p v-if="!nodes.length" class="hint">select a symbol to see its neighborhood</p>
  </div>
</template>

<style scoped>
.graphwrap {
  position: relative;
  height: 72vh;
  border: 1px solid #30363d;
  border-radius: 6px;
  background: #0d1117;
  overflow: hidden;
}
/* Fullscreen: cover the viewport, above everything, no rounded corners. */
.graphwrap.max {
  position: fixed;
  inset: 0;
  z-index: 1000;
  height: 100vh;
  border: 0;
  border-radius: 0;
}
.canvas { position: absolute; inset: 0; }
.controls { position: absolute; top: 10px; right: 10px; display: flex; gap: 6px; z-index: 2; }
.controls button {
  padding: 6px 10px;
  font-size: 12px;
  background: rgba(22, 27, 34, 0.85);
  color: #e6edf3;
  border: 1px solid #30363d;
  border-radius: 6px;
  cursor: pointer;
  backdrop-filter: blur(2px);
}
.controls button:hover { border-color: #58a6ff; }
.tip {
  position: absolute;
  left: 12px;
  bottom: 10px;
  margin: 0;
  font-size: 11px;
  color: #7d8590;
  pointer-events: none;
}
.hint {
  position: absolute;
  inset: 0;
  display: flex;
  align-items: center;
  justify-content: center;
  color: #7d8590;
  margin: 0;
}
</style>
