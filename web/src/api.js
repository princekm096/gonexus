// Connect unary calls are plain HTTP POST with a JSON body — no grpc-web client,
// no codegen. One helper covers the whole service.
const BASE =
  import.meta.env.VITE_GONEXUS_URL || "http://localhost:8080";
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
};
