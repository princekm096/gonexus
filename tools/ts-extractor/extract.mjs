// GoNexus TS/JS/Vue extractor. Emits {nodes, edges} JSON to stdout for a repo.
//
// Same thesis as the Go indexer: use the language's own toolchain. Builds a real
// ts.Program + type checker, so call edges resolve precisely across files
// (imported functions, class methods, same-name collisions) — not by guessing.
// .vue files are fed to the Program through a custom CompilerHost that serves
// each .vue as its extracted <script> block and resolves `./X.vue` imports.
//
// Node IDs are `relpath#Name` (Go IDs are pkgpath.Name), so the two languages
// never collide in the merged graph.
//
//
// Usage: node extract.mjs <repoDir>   ->  {"nodes":[...],"edges":[...]} on stdout

import { readFileSync, readdirSync, statSync, existsSync } from "node:fs";
import { join, relative, extname, dirname, resolve } from "node:path";
import ts from "typescript";
import { parse as parseVue } from "@vue/compiler-sfc";

const root = resolve(process.argv[2] || ".");
const CODE_EXTS = new Set([".ts", ".tsx", ".js", ".jsx", ".vue"]);
const SKIP_DIRS = new Set(["node_modules", ".git", "dist", "build", ".gonexus", ".nuxt", ".output"]);

function walk(dir, out = []) {
  for (const name of readdirSync(dir)) {
    if (SKIP_DIRS.has(name)) continue;
    const p = join(dir, name);
    let st;
    try { st = statSync(p); } catch { continue; }
    if (st.isDirectory()) walk(p, out);
    else if (CODE_EXTS.has(extname(p))) out.push(p);
  }
  return out;
}

// Extract the TS <script> text of a .vue file (cached).
const vueScriptCache = new Map();
function vueScript(file) {
  if (vueScriptCache.has(file)) return vueScriptCache.get(file);
  let content = "";
  try {
    const { descriptor } = parseVue(readFileSync(file, "utf8"), { filename: file });
    content = descriptor.scriptSetup?.content || descriptor.script?.content || "";
  } catch { /* malformed SFC -> empty script */ }
  vueScriptCache.set(file, content);
  return content;
}

const files = walk(root).map((f) => resolve(f));
const ourSet = new Set(files);
const rel = (f) => relative(root, resolve(f));

const options = {
  allowJs: true,
  checkJs: false,
  noEmit: true,
  target: ts.ScriptTarget.Latest,
  module: ts.ModuleKind.ESNext,
  moduleResolution: ts.ModuleResolutionKind.Bundler,
  allowNonTsExtensions: true,
  jsx: ts.JsxEmit.Preserve,
  skipLibCheck: true,
};

// CompilerHost that understands .vue as a TS module.
const host = ts.createCompilerHost(options, true);
const baseGetSourceFile = host.getSourceFile.bind(host);
host.getSourceFile = (fileName, langVersion, onError, shouldCreate) => {
  if (fileName.endsWith(".vue")) {
    return ts.createSourceFile(fileName, vueScript(fileName), langVersion, true, ts.ScriptKind.TS);
  }
  return baseGetSourceFile(fileName, langVersion, onError, shouldCreate);
};
host.fileExists = (f) => (f.endsWith(".vue") ? existsSync(f) : ts.sys.fileExists(f));
host.readFile = (f) => (f.endsWith(".vue") ? vueScript(f) : ts.sys.readFile(f));
host.resolveModuleNameLiterals = (literals, containingFile) =>
  literals.map((lit) => {
    const spec = lit.text;
    if (spec.startsWith(".") && spec.endsWith(".vue")) {
      const abs = resolve(dirname(containingFile), spec);
      return existsSync(abs)
        ? { resolvedModule: { resolvedFileName: abs, extension: ".ts", isExternalLibraryImport: false } }
        : { resolvedModule: undefined };
    }
    const r = ts.resolveModuleName(spec, containingFile, options, ts.sys);
    return { resolvedModule: r.resolvedModule };
  });

const program = ts.createProgram(files, options, host);
const checker = program.getTypeChecker();

const nodes = [];
const edges = [];
const idByDecl = new Map(); // declaration ts.Node -> node id (consistent IDs)
const callSites = [];       // {from, callee: expression}

// Resolve a relative import specifier to an indexed file (for import edges).
function resolveImport(fromFile, spec) {
  if (!spec.startsWith(".")) return null;
  const base = resolve(dirname(fromFile), spec);
  const cands = [
    base, base + ".ts", base + ".tsx", base + ".js", base + ".jsx", base + ".vue",
    join(base, "index.ts"), join(base, "index.tsx"), join(base, "index.js"), join(base, "index.vue"),
  ];
  for (const c of cands) if (ourSet.has(resolve(c))) return resolve(c);
  return null;
}

for (const sf of program.getSourceFiles()) {
  const abs = resolve(sf.fileName);
  if (!ourSet.has(abs)) continue; // skip lib.d.ts / node_modules
  const relPath = rel(abs);
  const isVue = extname(abs) === ".vue";
  const fileNodeID = relPath;
  const lineOf = (n) => sf.getLineAndCharacterOfPosition(n.getStart(sf)).line + 1;

  nodes.push({
    id: fileNodeID,
    kind: isVue ? "component" : "file",
    name: relPath.split("/").pop(),
    package: dirname(relPath),
    file: abs,
    line: 1,
  });

  const addNode = (id, kind, name, node) => {
    nodes.push({ id, kind, name, package: dirname(relPath), file: abs, line: lineOf(node) });
    idByDecl.set(node, id);
  };

  const visit = (node, enclosing) => {
    // function foo() {}
    if (ts.isFunctionDeclaration(node) && node.name) {
      const id = `${relPath}#${node.name.text}`;
      addNode(id, "func", node.name.text, node);
      edges.push({ from: fileNodeID, to: id, kind: "defines" });
      ts.forEachChild(node, (c) => visit(c, id));
      return;
    }
    // class C { method() {} }
    if (ts.isClassDeclaration(node) && node.name) {
      const cid = `${relPath}#${node.name.text}`;
      addNode(cid, "class", node.name.text, node);
      edges.push({ from: fileNodeID, to: cid, kind: "defines" });
      for (const m of node.members) {
        if (ts.isMethodDeclaration(m) && m.name && ts.isIdentifier(m.name)) {
          const mid = `${cid}.${m.name.text}`;
          addNode(mid, "method", m.name.text, m);
          edges.push({ from: cid, to: mid, kind: "defines" });
          if (m.body) visit(m.body, mid);
        }
      }
      return;
    }
    // const foo = () => {} / function expression
    if (ts.isVariableDeclaration(node) && node.name && ts.isIdentifier(node.name) &&
        node.initializer && (ts.isArrowFunction(node.initializer) || ts.isFunctionExpression(node.initializer))) {
      const id = `${relPath}#${node.name.text}`;
      addNode(id, "func", node.name.text, node); // key by VariableDeclaration (matches checker)
      edges.push({ from: fileNodeID, to: id, kind: "defines" });
      if (node.initializer.body) visit(node.initializer.body, id); // body may be an expression
      return;
    }
    // const api = { query() {}, context: () => {} } — object-literal methods.
    if (ts.isVariableDeclaration(node) && node.name && ts.isIdentifier(node.name) &&
        node.initializer && ts.isObjectLiteralExpression(node.initializer)) {
      const oid = `${relPath}#${node.name.text}`;
      addNode(oid, "var", node.name.text, node);
      edges.push({ from: fileNodeID, to: oid, kind: "defines" });
      for (const prop of node.initializer.properties) {
        let name = null, body = null;
        if (ts.isMethodDeclaration(prop) && prop.name && ts.isIdentifier(prop.name)) {
          name = prop.name.text;
          body = prop.body;
        } else if (ts.isPropertyAssignment(prop) && prop.name && ts.isIdentifier(prop.name) &&
                   prop.initializer && (ts.isArrowFunction(prop.initializer) || ts.isFunctionExpression(prop.initializer))) {
          name = prop.name.text;
          body = prop.initializer.body;
        }
        if (!name) continue;
        const mid = `${oid}.${name}`;
        addNode(mid, "method", name, prop); // key by property (matches checker's symbol decl)
        edges.push({ from: oid, to: mid, kind: "defines" });
        if (body) visit(body, mid); // body may be an expression (arrow)
      }
      return;
    }
    // import ... from "..."
    if (ts.isImportDeclaration(node) && ts.isStringLiteral(node.moduleSpecifier)) {
      const target = resolveImport(abs, node.moduleSpecifier.text);
      if (target) edges.push({ from: fileNodeID, to: rel(target), kind: "imports" });
      return;
    }
    // call site — record, then descend into arguments for nested calls
    if (ts.isCallExpression(node)) {
      callSites.push({ from: enclosing, callee: node.expression });
    }
    ts.forEachChild(node, (c) => visit(c, enclosing));
  };

  ts.forEachChild(sf, (c) => visit(c, fileNodeID));
}

// Resolve each call to a declaration node we indexed, via the checker.
for (const { from, callee } of callSites) {
  const ident = ts.isPropertyAccessExpression(callee)
    ? callee.name
    : ts.isIdentifier(callee)
      ? callee
      : null;
  if (!ident) continue;
  let sym = checker.getSymbolAtLocation(ident);
  if (!sym) continue;
  if (sym.flags & ts.SymbolFlags.Alias) sym = checker.getAliasedSymbol(sym);
  const decls = sym.getDeclarations?.() || [];
  for (const d of decls) {
    const to = idByDecl.get(d);
    if (to && to !== from) {
      edges.push({ from, to, kind: "calls" });
      break;
    }
  }
}

process.stdout.write(JSON.stringify({ nodes, edges }));
