<script setup>
import { ref, onMounted, onBeforeUnmount, watch } from "vue";
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
let renderer = null;

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

function build() {
  if (renderer) {
    renderer.kill();
    renderer = null;
  }
  if (!container.value || props.nodes.length === 0) return;

  const g = new Graph({ multi: false });
  for (const n of props.nodes) {
    g.mergeNode(n.id, {
      label: n.name || n.id,
      size: n.id === props.focusId ? 12 : 6,
      color: n.id === props.focusId ? "#ffffff" : KIND_COLOR[n.kind] || "#8b949e",
      x: Math.random(),
      y: Math.random(),
    });
  }
  for (const e of props.edges) {
    if (g.hasNode(e.from) && g.hasNode(e.to) && !g.hasEdge(e.from, e.to)) {
      g.addEdge(e.from, e.to, { color: EDGE_COLOR[e.kind] || "#30414d", size: 1.2 });
    }
  }
  forceAtlas2.assign(g, {
    iterations: 200,
    settings: { gravity: 1, scalingRatio: 12, slowDown: 2, barnesHutOptimize: g.order > 200 },
  });

  renderer = new Sigma(g, container.value, {
    renderEdgeLabels: false,
    defaultEdgeType: "arrow",
    labelColor: { color: "#c9d1d9" },
    labelDensity: 0.5,
    labelRenderedSizeThreshold: 5,
  });
  renderer.on("clickNode", ({ node }) => emit("select", node));
}

onMounted(build);
onBeforeUnmount(() => renderer && renderer.kill());
watch(() => [props.nodes, props.focusId], build, { deep: false });
</script>

<template>
  <div class="graphwrap">
    <div ref="container" class="canvas"></div>
    <p v-if="!nodes.length" class="hint">select a symbol to see its neighborhood</p>
  </div>
</template>

<style scoped>
.graphwrap { position: relative; height: 420px; border: 1px solid #30363d; border-radius: 6px; background: #0d1117; overflow: hidden; }
.canvas { position: absolute; inset: 0; }
.hint { position: absolute; inset: 0; display: flex; align-items: center; justify-content: center; color: #7d8590; margin: 0; }
</style>
