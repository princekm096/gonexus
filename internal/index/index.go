// Package index builds a code knowledge graph from a Go module using the Go
// toolchain itself (go/packages + go/types), not a Tree-sitter reimplementation.
// go/types already resolves imports, methods, and call targets across files, so
// the call graph falls out of TypesInfo.Uses for free.
//
// ponytail: Go-only for now. Vue/TS is a separate extractor (Phase 2) — the Go
// toolchain can't parse it, so that path uses tree-sitter-typescript. Graph
// model is shared, so extractors just emit Nodes/Edges.
package index

import (
	"fmt"
	"go/ast"
	"go/token"
	"go/types"
	"strings"

	"github.com/yourorg/gonexus/internal/graph"
	"golang.org/x/tools/go/packages"
)

// BuildGo indexes the Go module rooted at dir into a graph.
func BuildGo(dir string) (*graph.Graph, error) {
	cfg := &packages.Config{
		Mode: packages.NeedName | packages.NeedFiles | packages.NeedSyntax |
			packages.NeedTypes | packages.NeedTypesInfo | packages.NeedImports |
			packages.NeedDeps | packages.NeedModule,
		Dir:   dir,
		Tests: false,
	}
	pkgs, err := packages.Load(cfg, "./...")
	if err != nil {
		return nil, fmt.Errorf("load: %w", err)
	}
	g := graph.New()
	for _, pkg := range pkgs {
		indexPkg(g, pkg)
	}
	addImplementsEdges(g, pkgs)
	return g, nil
}

// addImplementsEdges links each concrete type to every interface it satisfies
// (heritage tracking). Runs after all packages are indexed so a type in one
// module package can implement an interface in another. Only module-local
// types/interfaces get edges (external ones like io.Writer aren't graph nodes).
//
// ponytail: O(concretes × interfaces) with types.Implements. Fine for a module;
// bucket by method-set size if it ever dominates. Generic (parameterized) types
// are skipped — instantiation-specific satisfaction isn't a single edge.
func addImplementsEdges(g *graph.Graph, pkgs []*packages.Package) {
	type tref struct {
		id    string
		named *types.Named
	}
	var ifaces, concretes []tref
	for _, pkg := range pkgs {
		if pkg.Types == nil {
			continue
		}
		scope := pkg.Types.Scope()
		for _, name := range scope.Names() {
			tn, ok := scope.Lookup(name).(*types.TypeName)
			if !ok {
				continue
			}
			named, ok := tn.Type().(*types.Named)
			if !ok || named.TypeParams().Len() > 0 {
				continue // skip aliases and generic types
			}
			ref := tref{objID(tn), named}
			if _, isIface := named.Underlying().(*types.Interface); isIface {
				ifaces = append(ifaces, ref)
			} else {
				concretes = append(concretes, ref)
			}
		}
	}
	for _, i := range ifaces {
		iface := i.named.Underlying().(*types.Interface)
		if iface.Empty() {
			continue // everything satisfies interface{}/any — noise
		}
		for _, c := range concretes {
			if types.Implements(c.named, iface) || types.Implements(types.NewPointer(c.named), iface) {
				g.AddEdge(graph.Edge{From: c.id, To: i.id, Kind: graph.EdgeImplements})
			}
		}
	}
}

// objID is the stable node ID for a types.Object. Methods include the receiver
// so Stringer.String and Buffer.String don't collide.
func objID(obj types.Object) string {
	if obj == nil {
		return ""
	}
	pkgPath := ""
	if obj.Pkg() != nil {
		pkgPath = obj.Pkg().Path()
	}
	if fn, ok := obj.(*types.Func); ok {
		sig, _ := fn.Type().(*types.Signature)
		if sig != nil && sig.Recv() != nil {
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

func indexPkg(g *graph.Graph, pkg *packages.Package) {
	if pkg.Types == nil || pkg.TypesInfo == nil {
		return // skip packages that failed to type-check
	}
	pkgID := pkg.PkgPath
	g.AddNode(&graph.Node{ID: pkgID, Kind: graph.KindPackage, Name: pkg.Name, Package: pkgID})

	// imports
	for _, imp := range pkg.Imports {
		g.AddEdge(graph.Edge{From: pkgID, To: imp.PkgPath, Kind: graph.EdgeImports})
	}

	fset := pkg.Fset
	for _, file := range pkg.Syntax {
		for _, decl := range file.Decls {
			switch d := decl.(type) {
			case *ast.FuncDecl:
				indexFunc(g, pkg, fset, d)
			case *ast.GenDecl:
				indexGenDecl(g, pkg, fset, d)
			}
		}
	}
}

func indexFunc(g *graph.Graph, pkg *packages.Package, fset *token.FileSet, d *ast.FuncDecl) {
	obj := pkg.TypesInfo.Defs[d.Name]
	if obj == nil {
		return
	}
	id := objID(obj)
	kind := graph.KindFunc
	if d.Recv != nil {
		kind = graph.KindMethod
	}
	pos := fset.Position(d.Pos())
	g.AddNode(&graph.Node{
		ID: id, Kind: kind, Name: d.Name.Name, Package: pkg.PkgPath,
		File: pos.Filename, Line: pos.Line,
		Signature: types.ObjectString(obj, qualifier(pkg)),
		Doc:       docText(d.Doc),
	})
	g.AddEdge(graph.Edge{From: pkg.PkgPath, To: id, Kind: graph.EdgeDefines})

	// call edges: walk the body, resolve each callee via TypesInfo.Uses.
	if d.Body == nil {
		return
	}
	ast.Inspect(d.Body, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		var ident *ast.Ident
		switch fn := call.Fun.(type) {
		case *ast.Ident:
			ident = fn
		case *ast.SelectorExpr:
			ident = fn.Sel
		default:
			return true
		}
		callee := pkg.TypesInfo.Uses[ident]
		if fn, ok := callee.(*types.Func); ok {
			g.AddEdge(graph.Edge{From: id, To: objID(fn), Kind: graph.EdgeCalls})
		}
		return true
	})
}

func indexGenDecl(g *graph.Graph, pkg *packages.Package, fset *token.FileSet, d *ast.GenDecl) {
	for _, spec := range d.Specs {
		ts, ok := spec.(*ast.TypeSpec)
		if !ok {
			continue
		}
		obj := pkg.TypesInfo.Defs[ts.Name]
		if obj == nil {
			continue
		}
		kind := graph.KindType
		if _, isIface := ts.Type.(*ast.InterfaceType); isIface {
			kind = graph.KindInterface
		}
		pos := fset.Position(ts.Pos())
		id := objID(obj)
		g.AddNode(&graph.Node{
			ID: id, Kind: kind, Name: ts.Name.Name, Package: pkg.PkgPath,
			File: pos.Filename, Line: pos.Line,
			Signature: types.ObjectString(obj, qualifier(pkg)),
			Doc:       docText(d.Doc),
		})
		g.AddEdge(graph.Edge{From: pkg.PkgPath, To: id, Kind: graph.EdgeDefines})
		// implements edges are added in addImplementsEdges after all packages
		// are indexed (needs cross-package type info).
	}
}

func qualifier(pkg *packages.Package) types.Qualifier {
	return func(other *types.Package) string {
		if other == pkg.Types {
			return ""
		}
		return other.Name()
	}
}

func docText(g *ast.CommentGroup) string {
	if g == nil {
		return ""
	}
	return strings.TrimSpace(g.Text())
}
