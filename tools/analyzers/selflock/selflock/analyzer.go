// Package selflock implements a go/analysis analyzer that detects
// self-deadlock patterns in Go code.
//
// This analyzer detects two related deadlock patterns:
//
// 1. Self-deadlock: External lock followed by method with internal locking
//    goal.Lock()                    // External lock
//    goal.AddActivePlan(planID)     // Has internal: defer g.mu.Unlock()
//    goal.Unlock()                  // Never reached - DEADLOCK
//
// 2. Callback deadlock: External lock held during store callback
//    goal.Lock()                    // External lock
//    store.Update(ctx, goal)        // Calls goal.snapshot() -> RLock()
//    goal.Unlock()                  // Never reached - DEADLOCK
//
// Both patterns violate the project's locking discipline: methods with
// internal locking should not be called while holding an external lock
// on the same object.
//
// # Suppression
//
// Findings can be suppressed with a trailing comment:
//
//	goal.Lock()
//	goal.AddActivePlan(id) //nolint:selflock // safe because X
//	goal.Unlock()
package selflock

import (
	"go/ast"
	"go/token"
	"strconv"
	"strings"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/analysis/passes/inspect"
	"golang.org/x/tools/go/ast/inspector"
)

const doc = `detect Go self-deadlock patterns

This analyzer flags places where a sync.Mutex or sync.RWMutex is locked and,
before the matching unlock, a method with internal locking is called on the
same receiver. This causes a self-deadlock since Go mutexes are non-reentrant.

Common patterns detected:
  - obj.Lock(); obj.MethodWithInternalLock()
  - obj.Lock(); store.Update(ctx, obj) where Update calls obj methods

Limited to intraprocedural analysis and known method names.`

// Analyzer is the selflock analyzer entry point.
var Analyzer = &analysis.Analyzer{
	Name:     "selflock",
	Doc:      doc,
	Requires: []*analysis.Analyzer{inspect.Analyzer},
	Run:      run,
}

// internalLockMethods are methods known to have internal locking.
// This list should be extended based on project conventions.
var internalLockMethods = map[string]bool{
	// Employee goal methods (internal/employee/goal.go)
	"AddActivePlan":   true,
	"RemoveActivePlan": true,
	"AppendHistory":   true,
	"Assess":          true,
	"snapshot":        true,
	// Common patterns in other projects
	"DoWithLock":      true,
	"UpdateSync":      true,
}

// storeUpdateMethods are store methods that may call back into object methods.
var storeUpdateMethods = map[string]bool{
	"Update":      true,
	"Save":        true,
	"Persist":     true,
	"Store":       true,
	"Write":       true,
	"Commit":      true,
}

func run(pass *analysis.Pass) (interface{}, error) {
	insp := pass.ResultOf[inspect.Analyzer].(*inspector.Inspector)

	// Collect all comment groups for nolint:selflock suppression
	var allComments []*ast.CommentGroup
	commentFilter := []ast.Node{
		(*ast.File)(nil),
	}
	insp.Preorder(commentFilter, func(n ast.Node) {
		if f, ok := n.(*ast.File); ok {
			allComments = append(allComments, f.Comments...)
		}
	})

	// Build nolint:selflock line set
	nolintLines := map[string]bool{}
	fset := pass.Fset
	for _, cg := range allComments {
		for _, c := range cg.List {
			if !strings.Contains(c.Text, "nolint:selflock") {
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

// callInfo tracks a method call with locking semantics
type callInfo struct {
	call    *ast.CallExpr
	method  string
	recvKey string // canonical dotted path of receiver
	isLock  bool
	isUnlock bool
}

// receiverKey extracts canonical receiver identifier
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

func checkBody(pass *analysis.Pass, body *ast.BlockStmt, nolintLines map[string]bool, fset *token.FileSet) {
	// Collect all selector method calls
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
		ci := callInfo{call: ce, method: method}
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

	// Track active locks by receiver
	type lockFrame struct {
		ci       callInfo
		varName  string // the variable being locked (e.g., "goal" from "goal.Lock()")
	}
	var stack []lockFrame

	for _, ci := range calls {
		if ci.isLock {
			// Extract receiver variable name for tracking
			var varName string
			if sel, ok := ci.call.Fun.(*ast.SelectorExpr); ok {
				if ident, ok := sel.X.(*ast.Ident); ok {
					varName = ident.Name
				}
			}
			stack = append(stack, lockFrame{ci: ci, varName: varName})
			continue
		}

		if ci.isUnlock {
			// Pop matching lock
			for j := len(stack) - 1; j >= 0; j-- {
				lf := stack[j]
				if lf.ci.recvKey == ci.recvKey && lf.ci.recvKey != "" {
					stack = append(stack[:j], stack[j+1:]...)
					break
				}
			}
			continue
		}

		// Check for internal-lock methods called while holding lock
		if internalLockMethods[ci.method] {
			for _, lf := range stack {
				// Same receiver = self-deadlock
				if lf.ci.recvKey == ci.recvKey && lf.ci.recvKey != "" {
					reportIfNotSuppressed(pass, ci.call, nolintLines, fset,
						"selflock: %s called while holding lock on same receiver %s (non-reentrant mutex)",
						ci.method, ci.recvKey)
					break
				}
			}
		}

		// Check for store.Update pattern
		if storeUpdateMethods[ci.method] {
			// Check if any argument references the locked object
			for _, lf := range stack {
				if lf.varName == "" {
					continue
				}
				for _, arg := range ci.call.Args {
					if ident, ok := arg.(*ast.Ident); ok {
						if ident.Name == lf.varName {
							reportIfNotSuppressed(pass, ci.call, nolintLines, fset,
								"selflock: %s may call back into locked object %s (callback deadlock)",
								ci.method, lf.varName)
							break
						}
					}
				}
			}
		}
	}
}

func reportIfNotSuppressed(pass *analysis.Pass, call *ast.CallExpr, nolintLines map[string]bool, fset *token.FileSet, format string, args ...interface{}) {
	// Check suppression
	if isNoLintSelfLock(call, nolintLines, fset) {
		return
	}
	pass.Reportf(call.Pos(), format, args...)
}

func isNoLintSelfLock(ce *ast.CallExpr, nolintLines map[string]bool, fset *token.FileSet) bool {
	if len(nolintLines) == 0 || fset == nil {
		return false
	}
	startPos := fset.Position(ce.Pos())
	endPos := fset.Position(ce.End())
	if startPos.Filename != endPos.Filename {
		return false
	}
	for line := startPos.Line; line <= endPos.Line; line++ {
		if nolintLines[startPos.Filename+":"+strconv.Itoa(line)] {
			return true
		}
	}
	return false
}
