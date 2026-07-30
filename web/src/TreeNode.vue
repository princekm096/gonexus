<script setup>
import { ref } from "vue";

defineOptions({ name: "TreeNode" });
defineProps({
  dirs: { type: Array, default: () => [] },
  files: { type: Array, default: () => [] },
  focusId: { type: String, default: "" },
  kindColor: { type: Object, default: () => ({}) },
  kindIcon: { type: Object, default: () => ({}) },
});
const emit = defineEmits(["select"]);

// Per-instance open state for the folders/files rendered at this level.
const openDirs = ref({});
const openFiles = ref({});
const toggleDir = (n) => (openDirs.value = { ...openDirs.value, [n]: !openDirs.value[n] });
const toggleFile = (n) => (openFiles.value = { ...openFiles.value, [n]: !openFiles.value[n] });
</script>

<template>
  <ul class="tree">
    <li v-for="d in dirs" :key="'d:' + d.name">
      <div class="row" @click="toggleDir(d.name)">
        <span class="caret">{{ openDirs[d.name] ? "▾" : "▸" }}</span>
        <span class="ficon">{{ d.repo ? "◈" : "📁" }}</span>
        <span class="label">{{ d.name }}</span>
      </div>
      <TreeNode
        v-if="openDirs[d.name]"
        :dirs="d.dirs"
        :files="d.files"
        :focus-id="focusId"
        :kind-color="kindColor"
        :kind-icon="kindIcon"
        @select="(id) => emit('select', id)"
      />
    </li>

    <li v-for="f in files" :key="'f:' + f.name">
      <div class="row" @click="toggleFile(f.name)">
        <span class="caret">{{ openFiles[f.name] ? "▾" : "▸" }}</span>
        <span class="ficon">📄</span>
        <span class="label">{{ f.name }}</span>
        <span class="count">{{ f.syms.length }}</span>
      </div>
      <ul v-if="openFiles[f.name]" class="syms">
        <li
          v-for="s in f.syms"
          :key="s.id"
          :class="{ active: s.id === focusId }"
          @click="emit('select', s.id)"
        >
          <span class="sicon" :style="{ color: kindColor[s.kind] || '#8b949e' }">{{ kindIcon[s.kind] || "•" }}</span>
          <span class="sname">{{ s.name }}</span>
        </li>
      </ul>
    </li>
  </ul>
</template>

<style scoped>
.tree { list-style: none; margin: 0; padding: 0 0 0 12px; }
.tree:first-child { padding-left: 0; }
.row { display: flex; align-items: center; gap: 5px; padding: 3px 4px; border-radius: 5px; cursor: pointer; font-size: 12.5px; }
.row:hover { background: #1f2630; }
.caret { width: 10px; color: #7d8590; font-size: 10px; }
.ficon { width: 16px; text-align: center; }
.label { flex: 1; white-space: nowrap; overflow: hidden; text-overflow: ellipsis; }
.count { font-size: 10px; color: #7d8590; }
.syms { list-style: none; margin: 0; padding: 0 0 0 26px; }
.syms li { display: flex; align-items: center; gap: 6px; padding: 3px 4px; border-radius: 5px; cursor: pointer; font-size: 12px; }
.syms li:hover { background: #1f2630; }
.syms li.active { background: #1f6feb33; }
.sicon { width: 14px; text-align: center; flex: none; }
.sname { white-space: nowrap; overflow: hidden; text-overflow: ellipsis; }
</style>
