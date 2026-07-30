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
  // blast-radius overlay: ids to spotlight red; empty = off.
  impactIds: { type: Array, default: () => [] },
  // cross-repo mode: color nodes by owning repo instead of kind.
  colorByRepo: { type: Boolean, default: false },
});
const emit = defineEmits(["select", "toggle-max"]);

const container = ref(null);
let renderer = null;
let graph = null;
let hovered = null; // transient hover spotlight
let pinned = null; // clicked node — spotlight persists until background click
let depthSet = null; // Set of ids within focusDepth of focusId, or null = all
let impactSet = new Set(); // blast-radius ids to spotlight

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
const IMPACT = "#ff7b72"; // blast-radius highlight
const CROSS = "#f778ba"; // cross-repo contract edge
const PILL_BORDER = "#39c5cf"; // cyan label pill border

function roundRect(ctx, x, y, w, h, r) {
  if (ctx.roundRect) {
    ctx.beginPath();
    ctx.roundRect(x, y, w, h, r);
    return;
  }
  ctx.beginPath();
  ctx.moveTo(x + r, y);
  ctx.arcTo(x + w, y, x + w, y + h, r);
  ctx.arcTo(x + w, y + h, x, y + h, r);
  ctx.arcTo(x, y + h, x, y, r);
  ctx.arcTo(x, y, x + w, y, r);
  ctx.closePath();
}

// Custom label: a dark rounded pill with a cyan border to the right of the node
// (GitNexus-style filename chips), instead of Sigma's plain floating text.
function drawNodeLabel(context, data, settings) {
  if (!data.label) return;
  const size = settings.labelSize;
  context.font = `${settings.labelWeight} ${size}px ${settings.labelFont}`;
  const w = context.measureText(data.label).width;
  const padX = 6,
    padY = 4;
  const x = data.x + data.size + 5;
  const top = data.y - size / 2 - padY;
  const h = size + padY * 2;
  context.fillStyle = "rgba(13,17,23,0.9)";
  context.strokeStyle = PILL_BORDER;
  context.lineWidth = 1;
  roundRect(context, x - padX, top, w + padX * 2, h, 5);
  context.fill();
  context.stroke();
  context.fillStyle = "#e6edf3";
  context.textAlign = "left";
  context.textBaseline = "middle";
  context.fillText(data.label, x, data.y);
}
// Stable per-repo palette for cross-repo coloring.
const REPO_PALETTE = ["#58a6ff", "#7ee787", "#f0883e", "#d2a8ff", "#f778ba", "#ffa657", "#79c0ff", "#56d364"];
const repoColors = {};
function repoColor(repo) {
  if (!repo) return "#8b949e";
  if (!(repo in repoColors)) {
    repoColors[repo] = REPO_PALETTE[Object.keys(repoColors).length % REPO_PALETTE.length];
  }
  return repoColors[repo];
}

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
      repo: n.repo || "",
      baseColor: props.colorByRepo ? repoColor(n.repo) : KIND_COLOR[n.kind] || "#8b949e",
      color: props.colorByRepo ? repoColor(n.repo) : KIND_COLOR[n.kind] || "#8b949e",
      x: Math.random(),
      y: Math.random(),
    });
  }
  for (const e of props.edges) {
    if (g.hasNode(e.from) && g.hasNode(e.to) && !g.hasEdge(e.from, e.to)) {
      g.addEdge(e.from, e.to, {
        color: e.cross ? CROSS : EDGE_COLOR[e.kind] || "#5b6b7d",
        size: e.cross ? 5 : 3,
        cross: !!e.cross,
      });
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
  pinned = null;
  recomputeDepth();
  recomputeImpact();
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
    defaultDrawNodeLabel: drawNodeLabel,
    nodeReducer,
    edgeReducer,
  });
  renderer.on("clickNode", ({ node }) => {
    pinned = node; // pin the spotlight so neighbors/edges/labels persist
    emit("select", node);
    renderer.refresh();
  });
  renderer.on("clickStage", () => {
    if (pinned) {
      pinned = null; // click empty space to clear the pinned spotlight
      renderer.refresh();
    }
  });
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
  const spot = hovered || pinned; // hover previews; a click pins it
  if (spot) {
    if (id === spot || graph.areNeighbors(spot, id)) {
      return { ...data, zIndex: 1, forceLabel: true, color: id === spot || isFocus ? "#ffffff" : data.color };
    }
    return { ...data, color: DIM, label: "", zIndex: 0 };
  }
  if (impactSet.size) {
    if (isFocus) return { ...data, color: "#ffffff", zIndex: 2, forceLabel: true, size: data.size + 4 };
    if (impactSet.has(id)) return { ...data, color: IMPACT, zIndex: 1, forceLabel: true };
    return { ...data, color: DIM, label: "", zIndex: 0 };
  }
  if (isFocus) return { ...data, color: "#ffffff", zIndex: 1, forceLabel: true, size: data.size + 4 };
  return { ...data, color: data.baseColor };
}
function edgeReducer(edge, data) {
  const [s, t] = graph.extremities(edge);
  const sd = graph.getNodeAttribute(s, "kind"),
    td = graph.getNodeAttribute(t, "kind");
  if (!visible(s, sd) || !visible(t, td)) return { ...data, hidden: true };
  const spot = hovered || pinned;
  if (spot) {
    if (s === spot || t === spot) return { ...data, color: "#788aa0", zIndex: 1 };
    return { ...data, hidden: true };
  }
  if (impactSet.size) {
    const inSet = (n) => n === props.focusId || impactSet.has(n);
    if (inSet(s) && inSet(t)) return { ...data, color: IMPACT, zIndex: 1 };
    return { ...data, hidden: true };
  }
  return data;
}

// recomputeImpact rebuilds the blast-radius spotlight set from the prop.
function recomputeImpact() {
  impactSet = new Set(props.impactIds || []);
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
watch(
  () => props.impactIds,
  () => {
    recomputeImpact();
    renderer && renderer.refresh();
  },
  { deep: true },
);
watch(() => props.maximized, reframe);
watch(() => props.colorByRepo, reframe);
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
