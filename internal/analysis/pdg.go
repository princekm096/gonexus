// Package analysis builds program-dependence information for Go using SSA:
// per-function control-flow graphs, statement-level data dependence (SSA
// def-use), and an intra-procedural source→sink taint analysis.
//
// Go-only: SSA is a Go-toolchain construct (same thesis as the rest of the
// indexer — use the language's own tools). It's heavy, so it's opt-in
// (`GONEXUS_PDG=1` at index time) and persisted separately from the graph.
//
// ponytail: taint is intra-procedural value-flow (no memory/alias tracking,
// no inter-procedural propagation); control dependence is reported at CFG-block
// granularity, not full post-dominance frontier. Both are honest
// over-approximations — the upgrade path is pointer analysis + IFDS.
package analysis

import (
	"go/types"
	"strconv"

	"golang.org/x/tools/go/packages"
	"golang.org/x/tools/go/ssa"
	"golang.org/x/tools/go/ssa/ssautil"
)

// FuncPDG summarizes one function's dependence structure.
type FuncPDG struct {
	ID        string   `json:"id"`        // matches the graph node id
	Blocks    int      `json:"blocks"`    // CFG basic blocks
	CtrlEdges [][2]int `json:"ctrlEdges"` // block index -> successor block index
	DataEdges int      `json:"dataEdges"` // SSA def-use edges (data dependence)
	Params    []string `json:"params"`    // parameter names
}

// TaintFinding is one source→sink flow.
type TaintFinding struct {
	Func   string `json:"func"`   // enclosing function id
	Source string `json:"source"` // tainting call, e.g. os.Getenv
	Sink   string `json:"sink"`   // dangerous call, e.g. os/exec.Command
	Line   int    `json:"line"`   // sink location
}

// Result is the persisted PDG payload.
type Result struct {
	Funcs []FuncPDG      `json:"funcs"`
	Taint []TaintFinding `json:"taint"`
}

// taint sources: functions whose *results* are untrusted.
var taintSources = map[string]bool{
	"os.Getenv":                         true,
	"(*net/http.Request).FormValue":     true,
	"(*net/http.Request).PostFormValue": true,
	"(*net/http.Request).URL":           true,
	"(*bufio.Reader).ReadString":        true,
	"io.ReadAll":                        true,
}

// taint sinks: functions whose *arguments* are dangerous if untrusted.
var taintSinks = map[string]bool{
	"os/exec.Command":             true,
	"os/exec.CommandContext":      true,
	"(*database/sql.DB).Query":    true,
	"(*database/sql.DB).Exec":     true,
	"(*database/sql.DB).QueryRow": true,
	"os.Open":                     true,
	"os.OpenFile":                 true,
	"os.ReadFile":                 true,
	"os.Remove":                   true,
	"os.RemoveAll":                true,
}

// BuildGoPDG runs SSA analysis over the Go module at dir.
func BuildGoPDG(dir string) (*Result, error) {
	cfg := &packages.Config{
		Mode: packages.NeedName | packages.NeedFiles | packages.NeedSyntax |
			packages.NeedTypes | packages.NeedTypesInfo | packages.NeedImports |
			packages.NeedDeps,
		Dir: dir,
	}
	pkgs, err := packages.Load(cfg, "./...")
	if err != nil {
		return nil, err
	}
	prog, _ := ssautil.AllPackages(pkgs, ssa.BuilderMode(0))
	prog.Build()

	res := &Result{}
	for fn := range ssautil.AllFunctions(prog) {
		if fn.Object() == nil || len(fn.Blocks) == 0 {
			continue // synthetic/external/no body
		}
		res.Funcs = append(res.Funcs, funcPDG(fn))
		res.Taint = append(res.Taint, taintOf(fn)...)
	}
	return res, nil
}

func funcPDG(fn *ssa.Function) FuncPDG {
	p := FuncPDG{ID: funcID(fn.Object()), Blocks: len(fn.Blocks)}
	for _, param := range fn.Params {
		p.Params = append(p.Params, param.Name())
	}
	for bi, b := range fn.Blocks {
		for _, s := range b.Succs {
			p.CtrlEdges = append(p.CtrlEdges, [2]int{bi, s.Index})
		}
		for _, instr := range b.Instrs {
			if v, ok := instr.(ssa.Value); ok {
				if refs := v.Referrers(); refs != nil {
					p.DataEdges += len(*refs)
				}
			}
		}
	}
	return p
}

// taintOf finds source→sink flows within fn by propagating taint over SSA
// def-use edges to a fixpoint, then checking sink-call arguments.
func taintOf(fn *ssa.Function) []TaintFinding {
	tainted := map[ssa.Value]bool{}
	var work []ssa.Value

	// seed: results of source calls are tainted.
	forEachCall(fn, func(c *ssa.Call, name string) {
		if taintSources[name] {
			tainted[c] = true
			work = append(work, c)
		}
	})

	// propagate: any value using a tainted value becomes tainted (over-approx).
	for len(work) > 0 {
		v := work[len(work)-1]
		work = work[:len(work)-1]
		refs := v.Referrers()
		if refs == nil {
			continue
		}
		for _, instr := range *refs {
			if used, ok := instr.(ssa.Value); ok && !tainted[used] {
				tainted[used] = true
				work = append(work, used)
			}
		}
	}

	var out []TaintFinding
	seen := map[string]bool{}
	forEachCall(fn, func(c *ssa.Call, name string) {
		if !taintSinks[name] {
			return
		}
		for _, arg := range c.Call.Args {
			if tainted[arg] {
				line := 0
				if pos := c.Pos(); pos.IsValid() {
					line = fn.Prog.Fset.Position(pos).Line
				}
				key := name + ":" + strconv.Itoa(line)
				if !seen[key] {
					seen[key] = true
					out = append(out, TaintFinding{
						Func: funcID(fn.Object()), Source: "tainted input",
						Sink: name, Line: line,
					})
				}
				break
			}
		}
	})
	return out
}

func forEachCall(fn *ssa.Function, visit func(*ssa.Call, string)) {
	for _, b := range fn.Blocks {
		for _, instr := range b.Instrs {
			c, ok := instr.(*ssa.Call)
			if !ok {
				continue
			}
			if callee := c.Call.StaticCallee(); callee != nil {
				visit(c, callee.String())
			}
		}
	}
}

// funcID mirrors internal/index.objID so PDG ids line up with graph nodes.
func funcID(obj types.Object) string {
	if obj == nil {
		return ""
	}
	pkgPath := ""
	if obj.Pkg() != nil {
		pkgPath = obj.Pkg().Path()
	}
	if fn, ok := obj.(*types.Func); ok {
		if sig, _ := fn.Type().(*types.Signature); sig != nil && sig.Recv() != nil {
			return pkgPath + "." + recvName(sig.Recv().Type()) + "." + obj.Name()
		}
	}
	if pkgPath == "" {
		return obj.Name()
	}
	return pkgPath + "." + obj.Name()
}

func recvName(t types.Type) string {
	if p, ok := t.(*types.Pointer); ok {
		t = p.Elem()
	}
	if named, ok := t.(*types.Named); ok {
		return named.Obj().Name()
	}
	return types.TypeString(t, nil)
}
