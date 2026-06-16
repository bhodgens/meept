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
package mutexio

import (
	"go/ast"
	"go/token"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/analysis/passes/inspect"
	"golang.org/x/tools/go/ast/inspector"
)

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
	nodeFilter := []ast.Node{
		(*ast.FuncDecl)(nil),
		(*ast.FuncLit)(nil),
	}
	insp.Preorder(nodeFilter, func(n ast.Node) {
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
		checkBody(pass, body)
	})
	return nil, nil
}

// callInfo is a single selector-method call recorded during linear scan.
type callInfo struct {
	call      *ast.CallExpr
	method    string
	recvIdent *ast.Ident // receiver ident if it's a simple selector
	isLock    bool
	isUnlock  bool
	isDefer   bool // true if call is inside a DeferStmt
}

// checkBody scans a function body for Lock/Unlock pairs and flags any
// I/O-method calls that appear textually between them.
func checkBody(pass *analysis.Pass, body *ast.BlockStmt) {
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
		if id, ok := sel.X.(*ast.Ident); ok {
			ci.recvIdent = id
		}
		calls = append(calls, ci)
		return true
	})

	// Pair lock/unlock by receiver ident name using a stack.
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
		// find matching lock (innermost with same receiver ident name)
		for j := len(stack) - 1; j >= 0; j-- {
			lf := stack[j]
			if lf.ci.recvIdent == nil || ci.recvIdent == nil {
				continue
			}
			if lf.ci.recvIdent.Name != ci.recvIdent.Name {
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
			checkRange(pass, body, lf.ci.call, ci.call, startPos, endPos)
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
func checkRange(pass *analysis.Pass, body *ast.BlockStmt, lockCall, unlockCall *ast.CallExpr, startPos, endPos token.Pos) {
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
		// call or other operations on the mutex).
		if id, ok := sel.X.(*ast.Ident); ok {
			if lid, ok2 := lockCall.Fun.(*ast.SelectorExpr); ok2 {
				if lid2, ok3 := lid.X.(*ast.Ident); ok3 {
					if id.Name == lid2.Name {
						return true
					}
				}
			}
		}
		// Skip deferred calls — they execute at function return, not
		// while the lock is held (unless the lock itself is deferred,
		// which is unusual and out of scope).
		if deferCalls[ce] {
			return true
		}
		pass.Reportf(ce.Pos(), "mutexio: %s called while holding a mutex (CLAUDE.md mutex scope rule)", method)
		return true
	})
}
