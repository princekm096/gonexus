<script setup>
import { ref, nextTick } from "vue";
import { api } from "./api.js";

const props = defineProps({
  repo: { type: String, default: "" },
  kindColor: { type: Object, default: () => ({}) },
});
const emit = defineEmits(["select"]);

const open = ref(false);
const input = ref("");
const sending = ref(false);
const messages = ref([]); // { role: "user"|"assistant", text, sources? }
const listEl = ref(null);

async function send() {
  const q = input.value.trim();
  if (!q || sending.value) return;
  messages.value.push({ role: "user", text: q });
  input.value = "";
  sending.value = true;
  scrollDown();
  try {
    const r = await api.ask(q, props.repo);
    messages.value.push({ role: "assistant", text: r.answer || "(no answer)", sources: r.sources || [] });
  } catch (e) {
    messages.value.push({ role: "assistant", text: "Error: " + e, sources: [] });
  } finally {
    sending.value = false;
    scrollDown();
  }
}
function scrollDown() {
  nextTick(() => listEl.value && (listEl.value.scrollTop = listEl.value.scrollHeight));
}
const shortId = (id) => id.split("/").pop();
</script>

<template>
  <div class="chat" :class="{ open }">
    <button v-if="!open" class="fab" @click="open = true">💬 Ask AI</button>

    <div v-else class="panel">
      <div class="head">
        <span>✨ Ask GoNexus</span>
        <span class="repo">{{ repo }}</span>
        <button class="x" @click="open = false">✕</button>
      </div>

      <div ref="listEl" class="msgs">
        <p v-if="!messages.length" class="empty">
          Ask about this codebase — architecture, where something is handled, how a flow works.
          Answers are grounded in the graph.
        </p>
        <div v-for="(m, i) in messages" :key="i" class="msg" :class="m.role">
          <div class="bubble">{{ m.text }}</div>
          <div v-if="m.sources && m.sources.length" class="sources">
            <button
              v-for="s in m.sources"
              :key="s.id"
              class="src"
              :title="s.id"
              @click="emit('select', s.id)"
            >
              <span class="d" :style="{ background: kindColor[s.kind] || '#8b949e' }"></span>{{ s.name }}
            </button>
          </div>
        </div>
        <div v-if="sending" class="msg assistant"><div class="bubble typing">thinking…</div></div>
      </div>

      <form class="input" @submit.prevent="send">
        <input v-model="input" placeholder="Ask about this codebase…" :disabled="sending" />
        <button type="submit" :disabled="sending || !input.trim()">Send</button>
      </form>
    </div>
  </div>
</template>

<style scoped>
.chat { position: fixed; right: 18px; bottom: 18px; z-index: 50; }
.fab { padding: 10px 16px; background: #a371f7; color: #fff; border: 0; border-radius: 22px; font-size: 14px; cursor: pointer; box-shadow: 0 4px 16px rgba(0, 0, 0, 0.4); }
.fab:hover { background: #b385ff; }
.panel { width: 380px; max-width: 90vw; height: 520px; max-height: 80vh; display: flex; flex-direction: column; background: #0d1117; border: 1px solid #30363d; border-radius: 12px; box-shadow: 0 12px 40px rgba(0, 0, 0, 0.5); overflow: hidden; }
.head { display: flex; align-items: center; gap: 8px; padding: 10px 12px; border-bottom: 1px solid #21262d; font-weight: 600; }
.head .repo { font-size: 11px; color: #7d8590; font-weight: 400; }
.head .x { margin-left: auto; background: transparent; border: 0; color: #7d8590; cursor: pointer; font-size: 14px; }
.msgs { flex: 1; overflow: auto; padding: 12px; display: flex; flex-direction: column; gap: 10px; }
.empty { color: #7d8590; font-size: 13px; margin: 0; }
.msg { display: flex; flex-direction: column; gap: 5px; }
.msg.user { align-items: flex-end; }
.bubble { max-width: 90%; padding: 8px 11px; border-radius: 12px; font-size: 13px; white-space: pre-wrap; line-height: 1.4; }
.msg.user .bubble { background: #1f6feb; color: #fff; border-bottom-right-radius: 4px; }
.msg.assistant .bubble { background: #161b22; border: 1px solid #30363d; color: #c9d1d9; border-bottom-left-radius: 4px; }
.typing { color: #7d8590; font-style: italic; }
.sources { display: flex; flex-wrap: wrap; gap: 5px; }
.src { display: flex; align-items: center; gap: 5px; padding: 3px 8px; background: #161b22; border: 1px solid #30363d; border-radius: 12px; color: #9da7b3; font-size: 11px; cursor: pointer; }
.src:hover { border-color: #58a6ff; color: #e6edf3; }
.src .d { width: 8px; height: 8px; border-radius: 50%; }
.input { display: flex; gap: 8px; padding: 10px; border-top: 1px solid #21262d; }
.input input { flex: 1; padding: 8px 10px; background: #161b22; border: 1px solid #30363d; border-radius: 8px; color: #e6edf3; }
.input button { padding: 8px 14px; background: #1f6feb; color: #fff; border: 0; border-radius: 8px; cursor: pointer; }
.input button:disabled { opacity: 0.5; cursor: not-allowed; }
</style>
