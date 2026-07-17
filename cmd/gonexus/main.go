// Command gonexus: index repos into a knowledge graph and serve it over
// Connect/gRPC or MCP. Repos are tracked in ~/.gonexus/registry.json; each
// repo's graph lives in its own <repo>/.gonexus/graph.json.
//
//	gonexus index <path> [name]  # index a repo and register it
//	gonexus list                 # list registered repos
//	gonexus serve [addr]         # serve all registered repos (default :8080)
//	gonexus mcp                  # serve over MCP stdio
package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"

	"github.com/yourorg/gonexus/gen/gonexus/v1/gonexusv1connect"
	"github.com/yourorg/gonexus/internal/index"
	"github.com/yourorg/gonexus/internal/llm"
	"github.com/yourorg/gonexus/internal/mcp"
	"github.com/yourorg/gonexus/internal/registry"
	"github.com/yourorg/gonexus/internal/server"
	"github.com/yourorg/gonexus/internal/wiki"
	"golang.org/x/net/http2"
	"golang.org/x/net/http2/h2c"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Println("usage: gonexus [index <path> [name] | list | status | remove <name> | wiki [repo] | serve [addr] | mcp]")
		os.Exit(2)
	}
	switch os.Args[1] {
	case "index":
		if len(os.Args) < 3 {
			log.Fatal("usage: gonexus index <path> [name]")
		}
		name := ""
		if len(os.Args) > 3 {
			name = os.Args[3]
		}
		cmdIndex(os.Args[2], name)
	case "list":
		cmdList()
	case "status":
		cmdStatus()
	case "remove":
		if len(os.Args) < 3 {
			log.Fatal("usage: gonexus remove <name>")
		}
		if err := registry.Remove(os.Args[2]); err != nil {
			log.Fatal(err)
		}
		fmt.Printf("removed %q\n", os.Args[2])
	case "wiki":
		repo := ""
		if len(os.Args) > 2 {
			repo = os.Args[2]
		}
		cmdWiki(repo)
	case "serve":
		addr := ":8080"
		if len(os.Args) > 2 {
			addr = os.Args[2]
		}
		cmdServe(addr)
	case "mcp":
		// stdout is the JSON-RPC transport; keep it clean (log goes to stderr).
		if err := mcp.Serve(); err != nil {
			log.Fatal(err)
		}
	default:
		log.Fatalf("unknown command %q", os.Args[1])
	}
}

func cmdIndex(path, name string) {
	name, nodes, edges, err := index.IndexAndRegister(path, name)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("indexed %q: %d nodes, %d edges (registered)\n", name, nodes, edges)
}

func cmdList() {
	f, err := registry.Load()
	if err != nil {
		log.Fatal(err)
	}
	names := f.Names()
	if len(names) == 0 {
		fmt.Println("no repos indexed; run `gonexus index <path>`")
		return
	}
	for _, n := range names {
		r := f.Repos[n]
		fmt.Printf("%-20s %s\n", n, r.Path)
	}
}

func cmdStatus() {
	f, err := registry.Load()
	if err != nil {
		log.Fatal(err)
	}
	names := f.Names()
	if len(names) == 0 {
		fmt.Println("no repos indexed; run `gonexus index <path>`")
		return
	}
	for _, n := range names {
		state := "fresh"
		if index.IsStale(f.Repos[n].Path) {
			state = "STALE"
		}
		fmt.Printf("%-20s %-6s %s\n", n, state, f.Repos[n].Path)
	}
}

func cmdWiki(repo string) {
	g, _, err := registry.NewStore().Graph(repo)
	if err != nil {
		log.Fatal(err)
	}
	r, err := registry.NewStore().Repo(repo)
	if err != nil {
		log.Fatal(err)
	}
	md, err := wiki.Generate(context.Background(), g, r.Name, llm.FromEnv())
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println(md)
}

func cmdServe(addr string) {
	s := server.New()
	mux := http.NewServeMux()
	mux.Handle(gonexusv1connect.NewGoNexusServiceHandler(s))

	// h2c so plaintext gRPC works in dev; CORS so the Vue dev server can call.
	handler := withCORS(h2c.NewHandler(mux, &http2.Server{}))
	log.Printf("gonexus serving on %s", addr)
	log.Fatal(http.ListenAndServe(addr, handler))
}

func withCORS(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "POST, GET, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Connect-Protocol-Version, Connect-Timeout-Ms")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		h.ServeHTTP(w, r)
	})
}
