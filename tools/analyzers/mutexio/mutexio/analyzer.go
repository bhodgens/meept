// Package mutexio implements a go/analysis analyzer that detects
// sync.Mutex / sync.RWMutex locks held across I/O operations.
//
// This enforces the "Mutex scope" rule from the project's CLAUDE.md:
// "Never hold a mutex across I/O operations (network calls, disk
// reads/writes, LLM calls, channel sends)."
//
// The analyzer is intraprocedural and uses a textual range check: if an
// I/O-like method call appears between a Lock and its matching Unlock
// (by receiver ident), it reports. Defer-based unlocks are handled by
// treating the end of the function body as the effective unlock position
// when a `defer X.Unlock()` is seen without a later non-deferred unlock.
//
// # Suppression
//
// Findings can be suppressed on a per-call basis with a trailing comment
// on the same line as the I/O call:
//
//	s.db.Exec(...) //nolint:mutexio // mutex serializes sqlite connection access
//
// The comment must contain the literal "nolint:mutexio". Any text after
// is treated as rationale for human readers and is ignored by the
// analyzer. This mirrors the golangci-lint nolint directive convention
// so the same directive works whether running under golangci-lint or
// the standalone runner (make mutexio).
package mutexio

import (
	"go/ast"
	"go/token"
	"go/types"
	"strconv"
	"strings"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/analysis/passes/inspect"
	"golang.org/x/tools/go/ast/inspector"
)

// atomicMethods are method names that are part of the sync/atomic API.
// When called on an atomic type (atomic.Bool, atomic.Int64, etc.) they
// are in-memory operations and must not be flagged as I/O.
var atomicMethods = map[string]bool{
	"Load":           true,
	"Store":          true,
	"Add":            true,
	"Swap":           true,
	"CompareAndSwap": true,
}

// mapLikeMethods are method names that read or mutate in-memory maps.
// When called on a sync.Map or a project-local map-like type (one whose
// receiver struct contains a sync.Mutex / sync.RWMutex field) these are
// not I/O and must not be flagged.
var mapLikeMethods = map[string]bool{
	"Load":           true,
	"LoadOrStore":    true,
	"LoadAndDelete":  true,
	"Delete":         true,
	"Range":          true,
	"Get":            true,
	"Len":            true,
	"Size":           true,
	"Has":            true,
	"Contains":       true,
	"Swap":           true,
	"CompareAndSwap": true,
	"Put":            true,
	"Invalidate":     true,
	"Set":            true,
}

const doc = `detect mutex held across I/O operations

This analyzer flags places where a sync.Mutex or sync.RWMutex is locked and,
before the matching unlock, an I/O operation (disk, network, LLM, bus, DB)
is performed in the same function. Holding a mutex across I/O violates the
project's CLAUDE.md "Mutex scope" rule and can cause latency and deadlocks.

Limited to intraprocedural analysis; will not catch I/O via callbacks.`

// Analyzer is the mutexio analyzer entry point.
var Analyzer = &analysis.Analyzer{
	Name:     "mutexio",
	Doc:      doc,
	Requires: []*analysis.Analyzer{inspect.Analyzer},
	Run:      run,
}

// ioMethods are method names that indicate an I/O operation when called
// between a Lock and its matching Unlock. The check is intentionally
// conservative: it flags the call by method name regardless of receiver
// type (except the mutex itself).
var ioMethods = map[string]bool{
	"WriteFile":      true,
	"ReadFile":       true,
	"MarshalIndent":  true,
	"Unmarshal":      true,
	"Do":             true,
	"Post":           true,
	"Get":            true,
	"Chat":           true,
	"ChatStream":     true,
	"ChatWithProgress": true,
	"Call":           true,
	"Publish":        true,
	"Close":          true,
	"Exec":           true,
	"Query":          true,
	"QueryRow":       true,
	"Connect":        true,
	"Dial":           true,
	"Send":           true,
	"Receive":        true,
	"Persist":        true,
	"PersistSync":    true,
	"Save":           true,
	"Load":           true,
}

func run(pass *analysis.Pass) (interface{}, error) {
	insp := pass.ResultOf[inspect.Analyzer].(*inspector.Inspector)

	// Collect all comment groups so checkRange can honor //nolint:mutexio
	// directives. We collect them once per package, not per function.
	var allComments []*ast.CommentGroup
	commentFilter := []ast.Node{
		(*ast.File)(nil),
	}
	insp.Preorder(commentFilter, func(n ast.Node) {
		if f, ok := n.(*ast.File); ok {
			allComments = append(allComments, f.Comments...)
		}
	})

	// Build a per-line nolint:mutexio set keyed by "filename:line".
	// Any comment starting on that line containing "nolint:mutexio"
	// marks the line suppressed.
	nolintLines := map[string]bool{}
	fset := pass.Fset
	for _, cg := range allComments {
		for _, c := range cg.List {
			if !strings.Contains(c.Text, "nolint:mutexio") {
				continue
			}
			p := fset.Position(c.Pos())
			nolintLines[p.Filename+":"+strconv.Itoa(p.Line)] = true
		}
	}

	funcFilter := []ast.Node{
		(*ast.FuncDecl)(nil),
		(*ast.FuncLit)(nil),
	}
	insp.Preorder(funcFilter, func(n ast.Node) {
		var body *ast.BlockStmt
		switch fn := n.(type) {
		case *ast.FuncDecl:
			body = fn.Body
		case *ast.FuncLit:
			body = fn.Body
		}
		if body == nil {
			return
		}
		checkBody(pass, body, nolintLines, fset)
	})
	return nil, nil
}

// callInfo is a single selector-method call recorded during linear scan.
type callInfo struct {
	call    *ast.CallExpr
	method  string
	recvKey string // canonical dotted path of the receiver (e.g. "s.mu", "p.mu")
	isLock  bool
	isUnlock bool
	isDefer bool // true if call is inside a DeferStmt
}

// receiverKey extracts a canonical string key identifying the receiver
// expression. Supports nested selector expressions (p.mu.Lock(),
// a.b.c.mu.Lock()) and simple idents (mu.Lock()). Returns "" if the
// receiver is something more complex (e.g., a function call result),
// in which case pairing is skipped (callInfo.recvKey stays "").
func receiverKey(expr ast.Expr) string {
	switch e := expr.(type) {
	case *ast.Ident:
		return e.Name
	case *ast.SelectorExpr:
		parent := receiverKey(e.X)
		if parent == "" {
			return ""
		}
		return parent + "." + e.Sel.Name
	default:
		return ""
	}
}

// checkBody scans a function body for Lock/Unlock pairs and flags any
// I/O-method calls that appear textually between them.
func checkBody(pass *analysis.Pass, body *ast.BlockStmt, nolintLines map[string]bool, fset *token.FileSet) {
	deferCalls := collectDeferCalls(body)
	var calls []callInfo
	ast.Inspect(body, func(n ast.Node) bool {
		ce, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		sel, ok := ce.Fun.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		method := sel.Sel.Name
		ci := callInfo{call: ce, method: method, isDefer: deferCalls[ce]}
		switch method {
		case "Lock", "RLock":
			ci.isLock = true
		case "Unlock", "RUnlock":
			ci.isUnlock = true
		}
		ci.recvKey = receiverKey(sel.X)
		calls = append(calls, ci)
		return true
	})

	// Pair lock/unlock by receiver key using a stack.
	// When the unlock is deferred, use the function-body end as the
	// effective unlock position so I/O between the Lock and the end of
	// the function is flagged (matches the CLAUDE.md rule intent).
	type frame struct {
		ci callInfo
	}
	var stack []frame
	for _, ci := range calls {
		if ci.isLock {
			stack = append(stack, frame{ci: ci})
			continue
		}
		if !ci.isUnlock {
			continue
		}
		// find matching lock (innermost with same receiver key)
		for j := len(stack) - 1; j >= 0; j-- {
			lf := stack[j]
			if lf.ci.recvKey == "" || ci.recvKey == "" {
				continue
			}
			if lf.ci.recvKey != ci.recvKey {
				continue
			}
			// Matching pair: scan between lock and unlock positions.
			startPos := lf.ci.call.End()
			var endPos token.Pos
			if ci.isDefer {
				// Deferred unlock effectively releases at function return.
				endPos = body.End()
			} else {
				endPos = ci.call.Pos()
			}
			checkRange(pass, body, lf.ci.call, ci.call, startPos, endPos, nolintLines, fset)
			stack = append(stack[:j], stack[j+1:]...)
			break
		}
	}
}

// collectDeferCalls returns CallExprs that are inside DeferStmt nodes.
func collectDeferCalls(body *ast.BlockStmt) map[*ast.CallExpr]bool {
	out := map[*ast.CallExpr]bool{}
	ast.Inspect(body, func(n ast.Node) bool {
		ds, ok := n.(*ast.DeferStmt)
		if !ok {
			return true
		}
		if ds.Call != nil {
			out[ds.Call] = true
		}
		return true
	})
	return out
}

// checkRange walks body and flags I/O method calls whose position is
// strictly after startPos and strictly before endPos.
// lockCall and unlockCall are passed so we can skip the unlock's own
// CallExpr (which would otherwise match for some methods) and so we
// can avoid treating the lock's receiver as I/O.
func checkRange(pass *analysis.Pass, body *ast.BlockStmt, lockCall, unlockCall *ast.CallExpr, startPos, endPos token.Pos, nolintLines map[string]bool, fset *token.FileSet) {
	deferCalls := collectDeferCalls(body)
	ast.Inspect(body, func(n ast.Node) bool {
		ce, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		// Skip the lock and unlock calls themselves.
		if ce == lockCall || ce == unlockCall {
			return true
		}
		// Strict textual range check.
		if ce.Pos() <= startPos || ce.Pos() >= endPos {
			return true
		}
		sel, ok := ce.Fun.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		method := sel.Sel.Name
		if !ioMethods[method] {
			return true
		}
		// Skip if the receiver is the mutex itself (e.g., the unlock
		// call or other operations on the mutex). Compare full receiver
		// keys so p.mu.Lock() / p.mu.Unlock() / p.mu.SomethingElse() all
		// share the same key.
		if lid, ok2 := lockCall.Fun.(*ast.SelectorExpr); ok2 {
			lockKey := receiverKey(lid.X)
			if lockKey != "" && lockKey == receiverKey(sel.X) {
				return true
			}
		}
		// Skip false positives: in-memory atomic loads/stores and map
		// lookups on sync.Map or project-local map-like types. These
		// share method names with real I/O (Load, Get, etc.) but perform
		// no I/O. Be conservative — when the receiver type can't be
		// resolved (e.g., interface) fall through to the existing logic.
		if isInMemoryMethod(pass, sel) {
			return true
		}
		// Skip deferred calls — they execute at function return, not
		// while the lock is held (unless the lock itself is deferred,
		// which is unusual and out of scope).
		if deferCalls[ce] {
			return true
		}
		// Honor //nolint:mutexio suppression directives on any line
		// spanned by the call expression. This allows legitimate patterns
		// (SQLite connection serialization, one-time init/teardown) to be
		// annotated even when the call spans multiple lines.
		if isNoLintMutexIO(ce, nolintLines, fset) {
			return true
		}
		pass.Reportf(ce.Pos(), "mutexio: %s called while holding a mutex (CLAUDE.md mutex scope rule)", method)
		return true
	})
}

// isNoLintMutexIO reports whether any line spanned by the given call
// expression is marked with a //nolint:mutexio suppression directive.
// Checking the full line range (not just the starting line) allows
// nolint directives on the closing line of multi-line calls, e.g.:
//
//	_, err := s.db.Exec(`
//		SELECT ...`,
//		arg1) //nolint:mutexio // mutex serializes sqlite connection access
func isNoLintMutexIO(ce *ast.CallExpr, nolintLines map[string]bool, fset *token.FileSet) bool {
	if len(nolintLines) == 0 || fset == nil {
		return false
	}
	startPos := fset.Position(ce.Pos())
	endPos := fset.Position(ce.End())
	if startPos.Filename != endPos.Filename {
		// Defensive: shouldn't happen for a single call expression.
		return false
	}
	for line := startPos.Line; line <= endPos.Line; line++ {
		if nolintLines[startPos.Filename+":"+strconv.Itoa(line)] {
			return true
		}
	}
	return false
}

// isInMemoryMethod reports whether the given selector call is an in-memory
// operation (sync/atomic, sync.Map, or a project-local map-like type) that
// happens to share its method name with an I/O method and therefore should
// NOT be flagged by mutexio.
//
// Returns false (do not skip) when:
//   - the receiver type cannot be resolved (e.g., interface, generic type
//     parameter) — conservative: let the existing logic apply
//   - the method name is not part of the atomic or map-like vocabulary
//   - the receiver is a real I/O type (http.Client, os.File, *sql.DB, etc.)
//
// Returns true (skip, do not flag) when:
//   - receiver is a sync/atomic type and method is an atomic op (Load/Store/...)
//   - receiver is sync.Map and method is a map operation
//   - receiver is a project-local struct that embeds or declares a
//     sync.Mutex / sync.RWMutex field, and the method is a map-like operation
//     (Get/Put/Lookup/Len/Has/etc.). This is how the codebase's concurrent
//     map wrappers (L1Cache, scheduler.Store, SkillIndex, etc.) are shaped.
func isInMemoryMethod(pass *analysis.Pass, sel *ast.SelectorExpr) bool {
	method := sel.Sel.Name
	if !atomicMethods[method] && !mapLikeMethods[method] {
		return false
	}
	t := pass.TypesInfo.TypeOf(sel.X)
	if t == nil {
		return false
	}
	// Named type? Check package path + name first — this catches
	// sync/atomic types and sync.Map regardless of underlying struct.
	if named, ok := t.(*types.Named); ok {
		obj := named.Obj()
		if obj != nil && obj.Pkg() != nil {
			pkgPath := obj.Pkg().Path()
			pkgName := obj.Pkg().Name()
			// sync/atomic.Bool, .Int64, .Int32, .Uint64, .Uint32,
			// .Uintptr, .Pointer[T], .Value — all in-memory atomics.
			if pkgPath == "sync/atomic" || pkgName == "atomic" {
				return atomicMethods[method]
			}
			// sync.Map and sync.Map look-alikes in the standard library.
			if pkgPath == "sync" && obj.Name() == "Map" {
				return mapLikeMethods[method]
			}
		}
	}
	// Underlying struct: check for a sync.Mutex / sync.RWMutex field or
	// embed. This catches project-local concurrent-map types
	// (L1Cache, scheduler.Store, SkillIndex, etc.) whose Get/Load/Len
	// methods are in-memory map reads guarded by an internal lock.
	var structType *types.Struct
	switch u := t.Underlying().(type) {
	case *types.Struct:
		structType = u
	case *types.Pointer:
		if s, ok := u.Elem().Underlying().(*types.Struct); ok {
			structType = s
		}
	}
	if structType == nil {
		return false
	}
	if structHasMutexField(structType) && mapLikeMethods[method] {
		return true
	}
	return false
}

// structHasMutexField reports whether the given struct type declares or
// embeds a sync.Mutex, sync.RWMutex, or another struct that does. The check
// is bounded by a depth counter to prevent infinite recursion through
// self-referential types.
func structHasMutexField(s *types.Struct) bool {
	return structHasMutexFieldDepth(s, 0)
}

const maxMutexFieldDepth = 4

func structHasMutexFieldDepth(s *types.Struct, depth int) bool {
	if s == nil || depth > maxMutexFieldDepth {
		return false
	}
	for i := 0; i < s.NumFields(); i++ {
		f := s.Field(i)
		if f == nil {
			continue
		}
		if isMutexType(f.Type()) {
			return true
		}
		// Embedded anonymous fields: recurse into them. This also
		// catches types that embed sync.RWMutex directly.
		if f.Embedded() {
			if inner, ok := f.Type().Underlying().(*types.Struct); ok {
				if structHasMutexFieldDepth(inner, depth+1) {
					return true
				}
			}
		}
	}
	return false
}

// isMutexType reports whether the given type is sync.Mutex or sync.RWMutex
// (by package path + type name, not by structural match).
func isMutexType(t types.Type) bool {
	ptr, ok := t.(*types.Pointer)
	if ok {
		t = ptr.Elem()
	}
	named, ok := t.(*types.Named)
	if !ok {
		return false
	}
	obj := named.Obj()
	if obj == nil || obj.Pkg() == nil {
		return false
	}
	if obj.Pkg().Path() != "sync" {
		return false
	}
	switch obj.Name() {
	case "Mutex", "RWMutex":
		return true
	}
	return false
}
