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
	"os/exec"

	"github.com/yourorg/gonexus/gen/gonexus/v1/gonexusv1connect"
	"github.com/yourorg/gonexus/internal/groups"
	"github.com/yourorg/gonexus/internal/index"
	"github.com/yourorg/gonexus/internal/llm"
	"github.com/yourorg/gonexus/internal/skills"
	"github.com/yourorg/gonexus/internal/mcp"
	"github.com/yourorg/gonexus/internal/registry"
	"github.com/yourorg/gonexus/internal/server"
	"github.com/yourorg/gonexus/internal/wiki"
	"golang.org/x/net/http2"
	"golang.org/x/net/http2/h2c"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Println("usage: gonexus [setup | index <path> [name] | list | status | remove <name> | clean <name> | group ... | skills [repo] | hooks | wiki [repo] | serve [addr] | mcp | doctor | uninstall]")
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
	case "group":
		cmdGroup(os.Args[2:])
	case "skills":
		repo := ""
		if len(os.Args) > 2 {
			repo = os.Args[2]
		}
		cmdSkills(repo)
	case "hooks":
		cmdHooks()
	case "setup":
		cmdSetup()
	case "clean":
		if len(os.Args) < 3 {
			log.Fatal("usage: gonexus clean <name>")
		}
		cmdClean(os.Args[2])
	case "doctor":
		cmdDoctor()
	case "uninstall":
		cmdUninstall()
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

func cmdGroup(args []string) {
	usage := "usage: gonexus group [create <name> | add <group> <repo> | remove <group> [repo] | list | sync <group> | impact <group> <repo> <symbolID>]"
	if len(args) == 0 {
		log.Fatal(usage)
	}
	switch args[0] {
	case "create":
		must(len(args) >= 2, usage)
		fatalIf(registry.GroupCreate(args[1]))
		fmt.Printf("created group %q\n", args[1])
	case "add":
		must(len(args) >= 3, usage)
		fatalIf(registry.GroupAddRepo(args[1], args[2]))
		fmt.Printf("added %q to group %q\n", args[2], args[1])
	case "remove":
		must(len(args) >= 2, usage)
		if len(args) >= 3 {
			fatalIf(registry.GroupRemoveRepo(args[1], args[2]))
		} else {
			fatalIf(registry.GroupRemove(args[1]))
		}
		fmt.Println("done")
	case "list":
		f, err := registry.LoadGroups()
		fatalIf(err)
		for _, n := range f.Names() {
			fmt.Printf("%-20s %v\n", n, f.Groups[n].Repos)
		}
	case "sync":
		must(len(args) >= 2, usage)
		res, err := groups.Sync(registry.NewStore(), args[1])
		fatalIf(err)
		fmt.Printf("group %q: %d cross-repo links\n", res.Group, len(res.Links))
		for _, l := range res.Links {
			fmt.Printf("  %-30s %v\n", l.Key, l.Repos)
		}
	case "impact":
		must(len(args) >= 4, usage)
		affected, key, err := groups.CrossImpact(registry.NewStore(), args[1], args[2], args[3])
		fatalIf(err)
		fmt.Printf("contract %q — repos possibly affected: %v\n", key, affected)
	default:
		log.Fatal(usage)
	}
}

func must(cond bool, msg string) {
	if !cond {
		log.Fatal(msg)
	}
}

func fatalIf(err error) {
	if err != nil {
		log.Fatal(err)
	}
}

// cmdSetup indexes the current directory and prints the MCP wiring command.
func cmdSetup() {
	cwd, err := os.Getwd()
	fatalIf(err)
	name, nodes, edges, err := index.IndexAndRegister(cwd, "")
	fatalIf(err)
	exe, _ := os.Executable()
	fmt.Printf("indexed %q: %d nodes, %d edges\n", name, nodes, edges)
	fmt.Printf("\nWire to Claude Code:\n  claude mcp add gonexus -- %s mcp\n", exe)
}

// cmdClean removes a repo's index cache and unregisters it.
func cmdClean(name string) {
	f, err := registry.Load()
	fatalIf(err)
	r, ok := f.Repos[name]
	if !ok {
		log.Fatalf("no such repo %q", name)
	}
	fatalIf(os.RemoveAll(registry.CacheDir(r.Path)))
	fatalIf(registry.Remove(name))
	fmt.Printf("cleaned %q (removed %s and unregistered)\n", name, registry.CacheDir(r.Path))
}

// cmdDoctor checks the environment GoNexus depends on.
func cmdDoctor() {
	check := func(label string, ok bool, note string) {
		mark := "ok"
		if !ok {
			mark = "MISSING"
		}
		fmt.Printf("  %-8s %-16s %s\n", mark, label, note)
	}
	check("go", inPath("go"), "required to index Go repos")
	check("node", inPath("node"), "required to index TS/JS/Vue")
	check("git", inPath("git"), "required for detect_changes")
	_, extErr := os.Stat("tools/ts-extractor/node_modules")
	check("ts-deps", extErr == nil, "run: cd tools/ts-extractor && npm install")
	if p, err := registry.Path(); err == nil {
		_, rerr := os.Stat(p)
		check("registry", rerr == nil || os.IsNotExist(rerr), p)
	}
	if emb := embedEnv(); emb {
		check("embeddings", true, "GONEXUS_EMBED_URL set (semantic search on)")
	}
}

func inPath(bin string) bool { _, err := exec.LookPath(bin); return err == nil }
func embedEnv() bool         { return os.Getenv("GONEXUS_EMBED_URL") != "" }

// cmdUninstall removes GoNexus's global state (~/.gonexus). Leaves your code.
func cmdUninstall() {
	home, err := os.UserHomeDir()
	fatalIf(err)
	dir := home + "/.gonexus"
	fatalIf(os.RemoveAll(dir))
	fmt.Printf("removed %s (registry, groups). Per-repo .gonexus/ caches are left in place.\n", dir)
}

func cmdSkills(repo string) {
	g, _, err := registry.NewStore().Graph(repo)
	fatalIf(err)
	r, err := registry.NewStore().Repo(repo)
	fatalIf(err)
	outDir := registry.CacheDir(r.Path) + "/skills"
	files, err := skills.Generate(g, r.Name, outDir)
	fatalIf(err)
	fmt.Printf("wrote %d skills to %s\n", len(files), outDir)
}

// cmdHooks prints a Claude Code hooks snippet: PreToolUse enriches with GoNexus
// context, PostToolUse warns when the index is stale.
func cmdHooks() {
	fmt.Print(`Add to your Claude Code settings.json (hooks):

{
  "hooks": {
    "PostToolUse": [
      {
        "matcher": "Edit|Write",
        "hooks": [
          { "type": "command", "command": "gonexus status | grep -q STALE && echo 'GoNexus index is stale; run: gonexus index <repo>' || true" }
        ]
      }
    ]
  }
}

The PostToolUse hook warns after edits when a repo's index no longer matches its
source. Re-run 'gonexus index <path>' (or the reindex MCP tool) to refresh.
`)
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
