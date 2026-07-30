<script setup>
import { ref, onMounted, onBeforeUnmount, watch, nextTick } from "vue";
import Graph from "graphology";
import Sigma from "sigma";
import { EdgeArrowProgram } from "sigma/rendering";
import forceAtlas2 from "graphology-layout-forceatlas2";

const props = defineProps({
  nodes: { type: Array, default: () => [] },
  edges: { type: Array, default: () => [] },
  focusId: { type: String, default: "" },
  // kind -> true means "hidden". Absent/false = visible.
  hiddenKinds: { type: Object, default: () => ({}) },
  // 0 = show all; N = show only nodes within N hops of the focused node.
  focusDepth: { type: Number, default: 0 },
  // parent-controlled: graph fills the whole workspace (side panels hidden).
  maximized: { type: Boolean, default: false },
});
const emit = defineEmits(["select", "toggle-max"]);

const container = ref(null);
let renderer = null;
let graph = null;
let hovered = null; // hovered node id (drives the highlight reducers)
let depthSet = null; // Set of ids within focusDepth of focusId, or null = all

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
  calls: "#5b6b7d",
  implements: "#a371f7",
  imports: "#6e7681",
  defines: "#4b5563",
};
const DIM = "#22272e";

// build lays out the whole graph (graphology + ForceAtlas2) once per data change.
function build() {
  if (renderer) {
    renderer.kill();
    renderer = null;
  }
  graph = null;
  hovered = null;
  if (!container.value || props.nodes.length === 0) return;

  const g = new Graph({ multi: false });
  for (const n of props.nodes) {
    g.mergeNode(n.id, {
      label: n.name || n.id,
      kind: n.kind,
      color: KIND_COLOR[n.kind] || "#8b949e",
      x: Math.random(),
      y: Math.random(),
    });
  }
  for (const e of props.edges) {
    if (g.hasNode(e.from) && g.hasNode(e.to) && !g.hasEdge(e.from, e.to)) {
      g.addEdge(e.from, e.to, { color: EDGE_COLOR[e.kind] || "#5b6b7d", size: 3 });
    }
  }

  // Size by degree so hubs read as hubs; labels are gated on rendered size so a
  // dense graph doesn't turn into a wall of text.
  let maxDeg = 1;
  g.forEachNode((id) => (maxDeg = Math.max(maxDeg, g.degree(id))));
  g.forEachNode((id) => {
    const deg = g.degree(id);
    g.setNodeAttribute(id, "size", 3 + 9 * Math.sqrt(deg / maxDeg));
  });

  const settings = forceAtlas2.inferSettings(g);
  forceAtlas2.assign(g, {
    iterations: Math.min(600, 120 + g.order),
    settings: { ...settings, barnesHutOptimize: g.order > 150, adjustSizes: true },
  });

  graph = g;
  recomputeDepth();
  mount();
}

// mount (re)creates the Sigma renderer. Sigma fits the graph to the container at
// construction, so remounting after the container resizes reframes reliably —
// resize() alone keeps Sigma's stale fit and jams the graph into a corner.
function mount() {
  if (renderer) {
    renderer.kill();
    renderer = null;
  }
  if (!graph || !container.value) return;
  hovered = null;
  renderer = new Sigma(graph, container.value, {
    allowInvalidContainer: true, // tolerate a not-yet-laid-out grid cell
    renderEdgeLabels: false,
    defaultEdgeType: "arrow",
    edgeProgramClasses: { arrow: EdgeArrowProgram }, // v3 doesn't register it by default
    labelColor: { color: "#c9d1d9" },
    labelDensity: 0.35,
    labelGridCellSize: 80,
    labelRenderedSizeThreshold: 10,
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

// recomputeDepth builds the set of ids within focusDepth hops of the focused
// node (undirected BFS). Null when no focus or depth 0 = everything visible.
function recomputeDepth() {
  depthSet = null;
  if (!graph || !props.focusDepth || !props.focusId || !graph.hasNode(props.focusId)) return;
  const seen = new Set([props.focusId]);
  let frontier = [props.focusId];
  for (let d = 0; d < props.focusDepth; d++) {
    const next = [];
    for (const cur of frontier) {
      graph.forEachNeighbor(cur, (nb) => {
        if (!seen.has(nb)) {
          seen.add(nb);
          next.push(nb);
        }
      });
    }
    frontier = next;
  }
  depthSet = seen;
}

// visible applies the sidebar filters: node-type toggles + focus-depth.
function visible(id, kind) {
  if (props.hiddenKinds[kind]) return false;
  if (depthSet && !depthSet.has(id)) return false;
  return true;
}

function nodeReducer(id, data) {
  if (!visible(id, data.kind)) return { ...data, hidden: true };
  const isFocus = id === props.focusId;
  if (hovered) {
    if (id === hovered || graph.areNeighbors(hovered, id)) {
      return { ...data, zIndex: 1, forceLabel: true, color: isFocus ? "#ffffff" : data.color };
    }
    return { ...data, color: DIM, label: "", zIndex: 0 };
  }
  if (isFocus) return { ...data, color: "#ffffff", zIndex: 1, forceLabel: true, size: data.size + 4 };
  return data;
}
function edgeReducer(edge, data) {
  const [s, t] = graph.extremities(edge);
  const sd = graph.getNodeAttribute(s, "kind"),
    td = graph.getNodeAttribute(t, "kind");
  if (!visible(s, sd) || !visible(t, td)) return { ...data, hidden: true };
  if (hovered) {
    if (s === hovered || t === hovered) return { ...data, color: "#788aa0", zIndex: 1 };
    return { ...data, hidden: true };
  }
  return data;
}

// Container resized (maximize toggle): rebuild so a fresh Sigma fits the new size.
function reframe() {
  nextTick(() => requestAnimationFrame(build));
}
function fit() {
  if (renderer) renderer.getCamera().animatedReset();
}
function onKey(e) {
  if (e.key === "Escape" && props.maximized) emit("toggle-max");
}

onMounted(() => {
  reframe(); // defer to after layout so the grid cell has a real width
  window.addEventListener("keydown", onKey);
});
onBeforeUnmount(() => {
  window.removeEventListener("keydown", onKey);
  renderer && renderer.kill();
});
// Rebuild on data change; just refresh reducers on filter change; reframe on resize.
watch(() => props.nodes, reframe, { deep: false });
watch(
  () => [props.hiddenKinds, props.focusId, props.focusDepth],
  () => {
    recomputeDepth();
    renderer && renderer.refresh();
  },
  { deep: true },
);
watch(() => props.maximized, reframe);
</script>

<template>
  <div class="graphwrap">
    <div ref="container" class="canvas"></div>

    <div v-if="nodes.length" class="controls">
      <button title="Fit graph to view" @click="fit">⤾ Fit</button>
      <button :title="maximized ? 'Restore panels (Esc)' : 'Maximize graph'" @click="emit('toggle-max')">
        {{ maximized ? "⤡ Restore" : "⤢ Maximize" }}
      </button>
    </div>

    <p v-if="nodes.length" class="tip">click a node to inspect · hover to isolate · scroll to zoom · drag to pan</p>
    <p v-else class="hint">indexing… or pick a repo to load its graph</p>
  </div>
</template>

<style scoped>
.graphwrap {
  position: relative;
  height: 100%;
  width: 100%;
  background: #0b0e13;
  overflow: hidden;
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
