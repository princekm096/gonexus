// Connect unary calls are plain HTTP POST with a JSON body — no grpc-web client,
// no codegen. One helper covers the whole service.
// Empty base = same origin the page was served from, so `gonexus serve` works
// whether opened via localhost or 127.0.0.1, on any port. Override with
// VITE_GONEXUS_URL when the UI and API live apart (e.g. the vite dev server).
const BASE = import.meta.env.VITE_GONEXUS_URL || "";
const SVC = "gonexus.v1.GoNexusService";

async function call(method, body) {
  const res = await fetch(`${BASE}/${SVC}/${method}`, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(body),
  });
  if (!res.ok) throw new Error(`${method}: ${res.status} ${await res.text()}`);
  return res.json();
}

export const api = {
  repos: () => call("Repos", {}),
  ask: (question, repo) => call("Ask", { question, repo }),
  query: (q, repo, limit = 20) => call("Query", { q, repo, limit }),
  context: (id, repo) => call("Context", { id, repo }),
  impact: (id, repo) => call("Impact", { id, repo }),
  trace: (from, to, repo) => call("Trace", { from, to, repo }),
  subgraph: (id, repo, depth = 1) => call("Subgraph", { id, repo, depth }),
  graph: (repo, limit = 1500) => call("Graph", { repo, limit }),
  source: (id, repo, before = 3, after = 120) => call("Source", { id, repo, before, after }),
  groups: () => call("Groups", {}),
  groupGraph: (group, limit = 800) => call("GroupGraph", { group, limit }),
};
