// Package codeindex indexes Go source trees into the knowledge store as
// CODE pages plus an intra-repo call-edge table (tree 04 leaf L3).
//
// Per *.go file it emits ONE page:
//
//	Title  = "code:<pkg-path>:<filename>"   (pkg-path = dir relpath of the
//	                                          file's directory, "." for root)
//	Body   = package clause + imports + top-level decl signatures
//	Source = "code:<relpath>"
//
// and records the resolved calls between top-level symbols in the knowledge
// store's code_edges table (see knowledge/codeedges.go).
//
// Skip rules: vendor/, testdata/, hidden dirs (leading "."), and *_test.go
// files are never indexed. Unchanged files (byte-identical content) are
// skipped by content hash. Call resolution is intra-repo only: an
// ast.CallExpr whose callee is not a symbol in the in-memory symbol table
// is treated as external and NOT stored as an edge.
//
// ImpactedBy(symbol) answers "what breaks if I change symbol": the one-hop
// callers from the edge table. Symbol lookup accepts bare ("Alpha") or
// package-qualified ("pkg.Alpha") names; a bare name matches any stored
// symbol ending in ".Alpha" (v1 heuristic — a bare name that is the suffix
// of an unrelated qualified symbol can over-match; use qualified names to
// disambiguate).
//
// Stdlib only (go/parser, go/ast). Intended to be driven from the periodic
// consolidation wiring (leaf T04-L4) alongside knowledge.SyncDir; this
// package takes no timers of its own.
package codeindex

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/caimlas/meept/internal/memory/knowledge"
)

// IndexReport summarizes one IndexGoDirs pass.
type IndexReport struct {
	// FilesIndexed counts *.go files that were new or changed and got
	// (re-)ingested.
	FilesIndexed int
	// FilesSkipped counts unchanged files (content-hash skip) plus files
	// that failed to parse (error recorded in Errors — one bad file
	// never aborts the pass).
	FilesSkipped int
	// FilesRemoved counts pages dropped because their file vanished from
	// the indexed dirs (pass 3 stale-source sweep).
	FilesRemoved int
	// SymbolsFound counts symbols in the in-memory symbol table after
	// the pass (all parsed files, changed or not).
	SymbolsFound int
	// EdgesFound counts call edges written for (re-)ingested files.
	EdgesFound int
	// Errors lists per-file failures, never fatal.
	Errors []string
}

// Ingester is the narrow store surface the indexer needs: page upsert,
// stored-page read (content-hash diff), call-edge replacement, and page
// listing + delete-by-source for stale-file cleanup.
type Ingester interface {
	Ingest(ctx context.Context, page knowledge.Page) (string, error)
	Read(ctx context.Context, id string) (knowledge.Page, error)
	ReplaceCallEdges(ctx context.Context, fromFile string, edges []knowledge.CallEdge) error
	// ListSources returns every page source with the given prefix (empty
	// prefix = all), used to find pages whose file vanished from the dirs.
	ListSources(ctx context.Context, prefix string) ([]string, error)
	// DeleteBySource removes the page (and its links) with this source.
	// Missing source → no-op.
	DeleteBySource(ctx context.Context, source string) error
}

// compile-time check: the concrete knowledge store satisfies the seam.
var _ Ingester = (*knowledge.Store)(nil)

// symbol is one entry of the in-memory symbol table.
type symbol struct {
	kind string // func | type | var | const
}

// IndexGoDirs walks each dir in dirs, indexes every *.go file (skipping
// vendor/, testdata/, hidden dirs, and *_test.go) into the knowledge store:
// one CODE page per file plus intra-repo call edges. Changed-file detection
// is by content hash — byte-identical files skip the page upsert but still
// contribute their symbols to the symbol table so cross-file call
// resolution stays complete, and their edge rows are rewritten (same
// content → same edges; idempotent upsert).
//
// Missing/unreadable dirs are errors wrapped with %w; per-file failures are
// recorded in IndexReport.Errors and never abort the pass. An empty dirs
// list is a silent no-op (code_dirs default = disabled).
func IndexGoDirs(ctx context.Context, store Ingester, dirs []string, logger *slog.Logger) (IndexReport, error) {
	if store == nil {
		return IndexReport{}, errors.New("codeindex: nil store")
	}
	if logger == nil {
		logger = slog.Default()
	}

	var rep IndexReport
	if len(dirs) == 0 {
		return rep, nil
	}

	// Pass 1: walk all dirs, parse every eligible file, build the
	// global symbol table. Parse failures are non-fatal. The FileSet is
	// returned with the parsed trees: positions are only resolvable on
	// the SAME fset the parser registered them on (a fresh one yields
	// line 0 for every position — bughunt 2026-10-14, from_line was
	// always 0).
	files, symbols, fset, err := parseTrees(ctx, dirs, &rep)
	if err != nil {
		return IndexReport{}, err
	}
	rep.SymbolsFound = len(symbols)

	// Pass 2: per file — content-hash diff against the stored page,
	// ingest when new/changed, rewrite call edges either way (an
	// unchanged file's edges may reference symbols in files that
	// changed this pass). Deterministic order: map iteration is not.
	rels := make([]string, 0, len(files))
	for rel := range files {
		rels = append(rels, rel)
	}
	sort.Strings(rels)

	for _, rel := range rels {
		f := files[rel]
		edges := callEdges(f.af, fset, f.pkgPath, rel, symbols)

		unchanged, err := fileUnchanged(ctx, store, rel, f.content)
		if err != nil {
			rep.Errors = append(rep.Errors, fmt.Sprintf("lookup %s: %v", rel, err))
			continue
		}
		if !unchanged {
			page := knowledge.Page{
				Title:  "code:" + f.pkgPath + ":" + filepath.Base(rel),
				Body:   renderPageBody(f.af),
				Source: "code:" + rel,
			}
			if _, err := store.Ingest(ctx, page); err != nil {
				rep.Errors = append(rep.Errors, fmt.Sprintf("ingest %s: %v", rel, err))
				continue
			}
			rep.FilesIndexed++
			rep.EdgesFound += len(edges)
		} else {
			rep.FilesSkipped++
		}

		if err := store.ReplaceCallEdges(ctx, rel, edges); err != nil {
			rep.Errors = append(rep.Errors, fmt.Sprintf("edges %s: %v", rel, err))
		}
	}

	// Pass 3: drop pages whose file vanished from the indexed dirs. A
	// deleted *.go previously kept its CODE page and its call edges
	// forever, so knowledge_impact kept reporting callers from files that
	// no longer exist (bughunt 2026-10-14 — IndexGoDirs only ingested).
	if existing, err := store.ListSources(ctx, "code:"); err != nil {
		rep.Errors = append(rep.Errors, fmt.Sprintf("list sources: %v", err))
	} else {
		for _, src := range existing {
			rel := strings.TrimPrefix(src, "code:")
			if _, ok := files[rel]; ok {
				continue
			}
			if err := store.DeleteBySource(ctx, src); err != nil {
				rep.Errors = append(rep.Errors, fmt.Sprintf("delete stale %s: %v", src, err))
				continue
			}
			if err := store.ReplaceCallEdges(ctx, rel, nil); err != nil {
				rep.Errors = append(rep.Errors, fmt.Sprintf("drop edges %s: %v", rel, err))
				continue
			}
			rep.FilesRemoved++
		}
	}

	logger.Debug("codeindex pass complete",
		"dirs", len(dirs),
		"files_indexed", rep.FilesIndexed,
		"files_skipped", rep.FilesSkipped,
		"symbols", rep.SymbolsFound,
		"edges", rep.EdgesFound,
		"errors", len(rep.Errors),
	)
	return rep, nil
}

// ImpactedBy returns the one-hop callers of symbol from the knowledge
// store's call-edge table. Bare names match package-qualified symbols by
// ".name" suffix (see package doc). No callers → empty slice, no error.
func ImpactedBy(ctx context.Context, store *knowledge.Store, symbol string) ([]string, error) {
	if store == nil {
		return nil, errors.New("codeindex: nil store")
	}
	if strings.TrimSpace(symbol) == "" {
		return nil, errors.New("codeindex: empty symbol")
	}
	return store.Callers(ctx, symbol)
}

// indexedFile is one parsed file carried between the two passes.
type indexedFile struct {
	af      *ast.File
	pkgPath string
	content []byte
}

// parseTrees is pass 1: walk each dir, parse every eligible *.go file
// (skip rules applied), build the global symbol table keyed by
// package-qualified name. Per-file failures land in rep.Errors; only
// dir-level failures (missing dir, walk error, ctx cancel) return an error.
func parseTrees(ctx context.Context, dirs []string, rep *IndexReport) (map[string]*indexedFile, map[string]symbol, *token.FileSet, error) {
	files := map[string]*indexedFile{}
	fset := token.NewFileSet()
	symbols := map[string]symbol{}

	for _, dir := range dirs {
		info, err := os.Stat(dir)
		if err != nil {
			return nil, nil, nil, fmt.Errorf("codeindex: stat %s: %w", dir, err)
		}
		if !info.IsDir() {
			return nil, nil, nil, fmt.Errorf("codeindex: %s is not a directory", dir)
		}
		walkErr := filepath.WalkDir(dir, func(path string, d fs.DirEntry, walkErr error) error {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			if walkErr != nil {
				rep.Errors = append(rep.Errors, fmt.Sprintf("walk %s: %v", path, walkErr))
				return nil
			}
			if d.IsDir() {
				if path != dir && skipDirName(d.Name()) {
					return filepath.SkipDir
				}
				return nil
			}
			if !strings.HasSuffix(d.Name(), ".go") || strings.HasSuffix(d.Name(), "_test.go") {
				return nil
			}
			rel, relErr := filepath.Rel(dir, path)
			if relErr != nil {
				rep.Errors = append(rep.Errors, fmt.Sprintf("relpath %s: %v", path, relErr))
				return nil
			}
			rel = filepath.ToSlash(rel)
			pkgPath := pkgPathOf(rel)

			//nolint:gosec // path is rooted at the user-configured code dir
			src, readErr := os.ReadFile(path)
			if readErr != nil {
				rep.Errors = append(rep.Errors, fmt.Sprintf("read %s: %v", rel, readErr))
				return nil
			}
			af, parseErr := parser.ParseFile(fset, path, src, parser.SkipObjectResolution)
			if parseErr != nil {
				rep.FilesSkipped++
				rep.Errors = append(rep.Errors, fmt.Sprintf("parse %s: %v", rel, parseErr))
				return nil
			}
			files[rel] = &indexedFile{
				af:      af,
				pkgPath: pkgPath,
				content: src,
			}
			collectSymbols(af, fset, pkgPath, symbols)
			return nil
		})
		if walkErr != nil {
			return nil, nil, nil, fmt.Errorf("codeindex: walk %s: %w", dir, walkErr)
		}
	}
	return files, symbols, fset, nil
}

// skipDirName reports whether a directory is excluded from indexing.
func skipDirName(name string) bool {
	return name == "vendor" ||
		name == "testdata" ||
		strings.HasPrefix(name, ".")
}

// pkgPathOf converts a slash relpath to a package path: the dir portion,
// "." for a root-level file.
func pkgPathOf(rel string) string {
	dir := filepath.ToSlash(filepath.Dir(rel))
	return dir // filepath.Dir already yields "." for a root-level file
}

// collectSymbols fills sym with the top-level declarations of af, keyed by
// package-qualified name ("pkg.Name", "pkg.Recv.Method" for methods).
func collectSymbols(af *ast.File, fset *token.FileSet, pkgPath string, sym map[string]symbol) {
	qual := func(name string) string {
		if pkgPath == "." {
			return name
		}
		return pkgPath + "." + name
	}
	for _, decl := range af.Decls {
		switch d := decl.(type) {
		case *ast.FuncDecl:
			name := d.Name.Name
			if d.Recv != nil && len(d.Recv.List) > 0 {
				if recv := recvTypeName(d.Recv.List[0].Type); recv != "" {
					name = recv + "." + name
				}
			}
			sym[qual(name)] = symbol{kind: "func"}
		case *ast.GenDecl:
			for _, spec := range d.Specs {
				switch sp := spec.(type) {
				case *ast.TypeSpec:
					sym[qual(sp.Name.Name)] = symbol{kind: "type"}
				case *ast.ValueSpec:
					kind := "var"
					if d.Tok == token.CONST {
						kind = "const"
					}
					for _, name := range sp.Names {
						if name.Name == "_" {
							continue
						}
						sym[qual(name.Name)] = symbol{kind: kind}
					}
				}
			}
		}
	}
	_ = fset // positions retained in signature for future file:line reporting
}

// recvTypeName extracts the receiver type name from a method receiver type
// expression (T and *T forms, including generic T[P] / T[P, Q]).
func recvTypeName(expr ast.Expr) string {
	switch t := expr.(type) {
	case *ast.StarExpr:
		return recvTypeName(t.X)
	case *ast.Ident:
		return t.Name
	case *ast.IndexExpr:
		return recvTypeName(t.X)
	case *ast.IndexListExpr:
		return recvTypeName(t.X)
	default:
		return ""
	}
}

// callEdges resolves the ast.CallExpr call sites of af against sym, minus
// self-recursion. Unresolved callees are external and produce no edge.
// Calls inside function literals attribute to the enclosing named symbol.
func callEdges(af *ast.File, fset *token.FileSet, pkgPath, rel string, sym map[string]symbol) []knowledge.CallEdge {
	qual := func(name string) string {
		if pkgPath == "." {
			return name
		}
		return pkgPath + "." + name
	}

	var edges []knowledge.CallEdge
	seen := map[string]bool{}
	addEdge := func(fromSym, toSym string, line int) {
		if fromSym == "" || toSym == "" || fromSym == toSym {
			return
		}
		key := fromSym + "->" + toSym
		if seen[key] {
			return
		}
		seen[key] = true
		edges = append(edges, knowledge.CallEdge{
			FromFile:   rel,
			FromSymbol: fromSym,
			ToSymbol:   toSym,
			FromLine:   line,
		})
	}

	// currentFunc tracks the enclosing named symbol; "" outside any.
	// localTypes maps local variable/param names to their package-
	// qualified type so `c := pkgtwo.Counter{}; c.Next()` resolves.
	currentFunc := ""
	localTypes := map[string]string{}
	collectLocalTypes(af, pkgPath, sym, localTypes)
	ast.Inspect(af, func(n ast.Node) bool {
		switch node := n.(type) {
		case *ast.FuncDecl:
			name := node.Name.Name
			if node.Recv != nil && len(node.Recv.List) > 0 {
				if recv := recvTypeName(node.Recv.List[0].Type); recv != "" {
					name = recv + "." + name
				}
			}
			currentFunc = qual(name)
			return true
		case *ast.GenDecl:
			// Var/const initializers can call functions; attribute
			// the calls to the first declared symbol in the group.
			for _, spec := range node.Specs {
				if vs, ok := spec.(*ast.ValueSpec); ok && len(vs.Names) > 0 {
					if name := vs.Names[0].Name; name != "_" {
						currentFunc = qual(name)
					}
				}
			}
			return true
		case *ast.AssignStmt:
			// Track `v := pkg.Type{...}` / `v := &pkg.Type{...}` so
			// later `v.Method()` calls resolve intra-repo. We do NOT
			// reset currentFunc here — assignments inside a func body
			// leave the enclosing symbol unchanged.
			for i, rhs := range node.Rhs {
				cl, ok := rhs.(*ast.CompositeLit)
				if !ok {
					continue
				}
				tn := lastIdent(cl.Type)
				if tn == "" {
					continue
				}
				cand := qualifyCallee(cl.Type, pkgPath, sym)
				if cand == "" {
					continue
				}
				// cand is the type name; record it for the lhs var.
				for j := range node.Lhs {
					if id, ok := node.Lhs[j].(*ast.Ident); ok && id.Name != "_" {
						_ = i
						localTypes[id.Name] = cand
					}
				}
			}
			return true
		case *ast.CallExpr:
			if currentFunc != "" {
				if callee := qualifyCallee(node.Fun, pkgPath, sym); callee != "" {
					if _, ok := sym[callee]; ok {
						addEdge(currentFunc, callee, fset.Position(node.Pos()).Line)
					}
				} else if sel, ok := node.Fun.(*ast.SelectorExpr); ok {
					// v.Method() where v is a tracked local var.
					if id, ok := sel.X.(*ast.Ident); ok {
						if typ, ok := localTypes[id.Name]; ok {
							cand := typ + "." + sel.Sel.Name
							if _, ok := sym[cand]; ok {
								addEdge(currentFunc, cand, fset.Position(node.Pos()).Line)
							}
						}
					}
				}
			}
			return true
		}
		return true
	})
	return edges
}

// collectLocalTypes pre-populates the local var → qualified type map from
// every assignment/composite-literal in the file (coarse: variable scope is
// not tracked, v1 heuristic — type names are unique per package so a
// shadowed name can at worst add a spurious resolution candidate that must
// still exist in the symbol table).
func collectLocalTypes(af *ast.File, pkgPath string, sym map[string]symbol, out map[string]string) {
	ast.Inspect(af, func(n ast.Node) bool {
		as, ok := n.(*ast.AssignStmt)
		if !ok {
			return true
		}
		for _, rhs := range as.Rhs {
			cl, ok := rhs.(*ast.CompositeLit)
			if !ok {
				continue
			}
			cand := qualifyCallee(cl.Type, pkgPath, sym)
			if cand == "" {
				continue
			}
			for _, lhs := range as.Lhs {
				if id, ok := lhs.(*ast.Ident); ok && id.Name != "_" {
					out[id.Name] = cand
				}
			}
		}
		return true
	})
}

// qualifyCallee renders a call-target expression to its package-qualified
// symbol name when it could be intra-repo:
//
//	Ident          → pkg.Name (same-package reference)
//	pkg.Sel        → pkg.Sel when pkg is this file's package or the
//	                 qualified name exists in the table (intra-repo
//	                 cross-package); anything else is external
//	recv.Sel       → recvType.Sel when recv's trailing identifier names
//	                 a local type with that method (v1 heuristic)
//
// Unresolvable → "".
func qualifyCallee(expr ast.Expr, pkgPath string, sym map[string]symbol) string {
	switch e := expr.(type) {
	case *ast.Ident:
		return pkgQualify(e.Name, pkgPath)
	case *ast.SelectorExpr:
		if pkgIdent, ok := e.X.(*ast.Ident); ok {
			// pkg.Sel form: either this file's own package (bare
			// ident — only matches when pkgPath is ".") or a
			// qualified cross-package reference.
			cand := pkgIdent.Name + "." + e.Sel.Name
			if _, ok := sym[cand]; ok {
				return cand
			}
			return ""
		}
		if id := lastIdent(e.X); id != "" {
			cand := id + "." + e.Sel.Name
			if _, ok := sym[cand]; ok {
				return cand
			}
		}
		return ""
	default:
		return ""
	}
}

// pkgQualify prefixes a bare name with the package path unless already at
// the repo root.
func pkgQualify(name, pkgPath string) string {
	if pkgPath == "." {
		return name
	}
	return pkgPath + "." + name
}

// lastIdent returns the identifier name of an expression's trailing
// segment, or "" for anything else.
func lastIdent(expr ast.Expr) string {
	switch e := expr.(type) {
	case *ast.Ident:
		return e.Name
	case *ast.SelectorExpr:
		return e.Sel.Name
	case *ast.StarExpr:
		return lastIdent(e.X)
	case *ast.CallExpr:
		return lastIdent(e.Fun)
	default:
		return ""
	}
}

// fileUnchanged reports whether the stored page for rel is byte-identical
// to what a fresh ingest would write (mirrors SyncDir's hash-diff, applied
// to the rendered page body rather than the raw file bytes — the page IS
// the rendered body).
func fileUnchanged(ctx context.Context, store Ingester, rel string, content []byte) (bool, error) {
	id := knowledge.PageID("code:"+pkgPathOf(rel)+":"+filepath.Base(rel), "code:"+rel)
	stored, err := store.Read(ctx, id)
	switch {
	case err == nil:
		fresh := renderBody(rel, content)
		return sha256.Sum256([]byte(fresh)) == sha256.Sum256([]byte(stored.Body)), nil
	case errors.Is(err, knowledge.ErrNotFound):
		return false, nil // new file
	default:
		return false, err
	}
}

// renderBody parses content and renders its page body; on a parse error it
// returns "" (the file is reported as changed and the parse failure is
// handled by the main pass).
func renderBody(rel string, content []byte) string {
	fset := token.NewFileSet()
	af, err := parser.ParseFile(fset, rel, content, parser.SkipObjectResolution)
	if err != nil {
		return ""
	}
	return renderPageBody(af)
}
