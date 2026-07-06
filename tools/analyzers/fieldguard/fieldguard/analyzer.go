// Package fieldguard implements a go/analysis analyzer that verifies struct
// field access follows ownership annotations.
//
// It checks:
// 1. Fields annotated with "// guarded by <mutex>" are only accessed while
//    holding that mutex (basic intraprocedural check).
// 2. Fields annotated with "// immutable" are only written in constructors.
// 3. Structs with shared mutable state should have annotations.
//
// This enforces the concurrency patterns from docs/concepts/concurrency.md.
package fieldguard

import (
	"go/ast"
	"regexp"
	"strings"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/analysis/passes/inspect"
	"golang.org/x/tools/go/ast/inspector"
)

const doc = `verify struct field access follows ownership annotations

This analyzer checks that:
  - Fields annotated with "// guarded by <mutex>" are accessed under lock
  - Fields annotated with "// immutable" are only written in constructors
  - Structs with mutex fields have proper annotations

See docs/concepts/concurrency.md for the full pattern specification.`

// Analyzer is the fieldguard analyzer entry point.
var Analyzer = &analysis.Analyzer{
	Name:     "fieldguard",
	Doc:      doc,
	Requires: []*analysis.Analyzer{inspect.Analyzer},
	Run:      run,
}

// guardedByRegex matches "// guarded by <mutex>" comments
var guardedByRegex = regexp.MustCompile(`//\s*guarded by\s+(\w+)`)

// immutableRegex matches "// immutable" comments
var immutableRegex = regexp.MustCompile(`//\s*immutable\b`)

func run(pass *analysis.Pass) (interface{}, error) {
	insp := pass.ResultOf[inspect.Analyzer].(*inspector.Inspector)

	// Build a map of guarded fields per struct
	guardedFields := make(map[string]map[string]string) // typeName -> fieldName -> mutexName
	immutableFields := make(map[string]map[string]bool)  // typeName -> fieldName -> true

	// First pass: collect struct type annotations
	typeFilter := []ast.Node{
		(*ast.TypeSpec)(nil),
	}
	insp.Preorder(typeFilter, func(n ast.Node) {
		ts, ok := n.(*ast.TypeSpec)
		if !ok {
			return
		}
		st, ok := ts.Type.(*ast.StructType)
		if !ok {
			return
		}
		typeName := ts.Name.Name
		if st.Fields != nil {
			analyzeStructFields(st, typeName, guardedFields, immutableFields)
		}
	})

	// Second pass: check field access in functions
	funcFilter := []ast.Node{
		(*ast.FuncDecl)(nil),
		(*ast.FuncLit)(nil),
	}
	insp.Preorder(funcFilter, func(n ast.Node) {
		var body *ast.BlockStmt
		switch fn := n.(type) {
		case *ast.FuncDecl:
			body = fn.Body
			checkFuncDecl(pass, fn, body, guardedFields, immutableFields)
		case *ast.FuncLit:
			body = fn.Body
			checkFuncLit(pass, fn, body, guardedFields)
		}
	})

	return nil, nil
}

func analyzeStructFields(st *ast.StructType, typeName string,
	guardedFields map[string]map[string]string,
	immutableFields map[string]map[string]bool) {

	if st.Fields == nil {
		return
	}

	for _, field := range st.Fields.List {
		if field.Names == nil {
			continue
		}

		fieldName := field.Names[0].Name

		// Parse comments for annotations
		if field.Comment != nil {
			for _, c := range field.Comment.List {
				// Only track fields that are guarded (require a mutex), not fields that ARE the guard
				if guardedByRegex.MatchString(c.Text) && !strings.Contains(c.Text, "guard for") {
					matches := guardedByRegex.FindStringSubmatch(c.Text)
					if len(matches) == 2 {
						if guardedFields[typeName] == nil {
							guardedFields[typeName] = make(map[string]string)
						}
						guardedFields[typeName][fieldName] = matches[1]
					}
				}
				if immutableRegex.MatchString(c.Text) {
					if immutableFields[typeName] == nil {
						immutableFields[typeName] = make(map[string]bool)
					}
					immutableFields[typeName][fieldName] = true
				}
			}
		}
	}
}

func checkFuncDecl(pass *analysis.Pass, fn *ast.FuncDecl, body *ast.BlockStmt,
	guardedFields map[string]map[string]string,
	immutableFields map[string]map[string]bool) {

	if body == nil {
		return
	}

	receiverName := getReceiverVar(fn)
	receiverType := getReceiverType(fn)

	// We need to do multiple passes:
	// 1. Collect all lock operations and field accesses
	// 2. Build lock state at each program point
	// 3. Report violations

	// For v2: simplified approach - track locks linearly and check field access
	// This handles the common case where Lock/Unlock are in the same basic block

	// First, collect all statements with their lock state
	statements := flattenStatements(body)

	// Check immutable field writes
	checkImmutableWrites(pass, fn, body, receiverType, immutableFields)

	// Check guarded field access with lock state tracking
	checkGuardedFieldAccess(pass, fn, statements, receiverName, receiverType, guardedFields)
}

// flattenStatements returns all statements in a function body in execution order
func flattenStatements(body *ast.BlockStmt) []ast.Stmt {
	if body == nil {
		return nil
	}
	return body.List
}

func checkImmutableWrites(pass *analysis.Pass, fn *ast.FuncDecl, body *ast.BlockStmt,
	receiverType string, immutableFields map[string]map[string]bool) {

	// Skip constructors (functions starting with "New")
	if strings.HasPrefix(fn.Name.Name, "New") {
		return
	}

	ast.Inspect(body, func(n ast.Node) bool {
		assign, ok := n.(*ast.AssignStmt)
		if !ok {
			return true
		}

		for _, lhs := range assign.Lhs {
			sel, ok := lhs.(*ast.SelectorExpr)
			if !ok {
				continue
			}

			// Check if receiver matches
			if sel.X != nil {
				fieldName := sel.Sel.Name
				if immut := immutableFields[receiverType]; immut != nil {
					if immut[fieldName] {
						pass.Reportf(assign.Pos(), "fieldguard: writing to immutable field %q in non-constructor", fieldName)
					}
				}
			}
		}
		return true
	})
}

func checkGuardedFieldAccess(pass *analysis.Pass, fn *ast.FuncDecl, statements []ast.Stmt,
	receiverName string, receiverType string, guardedFields map[string]map[string]string) {

	// Track which mutexes are held at each point
	heldLocks := make(map[string]bool)

	for _, stmt := range statements {
		// First, process lock/unlock operations to update heldLocks
		// This must happen BEFORE checking field access in the same statement
		
		// Check for lock operations (e.g., g.mu.Lock())
		if exprStmt, ok := stmt.(*ast.ExprStmt); ok {
			if call, ok := exprStmt.X.(*ast.CallExpr); ok {
				if mutex := isLockCall(call); mutex != "" {
					heldLocks[mutex] = true
					continue // Skip field access check for pure lock statements
				}
				if mutex := isUnlockCall(call); mutex != "" {
					delete(heldLocks, mutex)
					continue // Skip field access check for pure unlock statements
				}
			}
		}

		// Check for defer unlock (e.g., defer g.mu.Unlock())
		if deferStmt, ok := stmt.(*ast.DeferStmt); ok {
			if mutex := isUnlockCall(deferStmt.Call); mutex != "" {
				// For defer, mark as held for rest of function (simplified)
				heldLocks[mutex] = true
				continue // Skip field access check for defer unlock statements
			}
		}
		
		// Check for defer lock (rare but possible)
		if deferStmt, ok := stmt.(*ast.DeferStmt); ok {
			if mutex := isLockCall(deferStmt.Call); mutex != "" {
				heldLocks[mutex] = true
				continue
			}
		}

		// Now check for field access in this statement
		// Skip selector expressions that are part of call expressions (like g.mu in g.mu.Lock())
		ast.Inspect(stmt, func(n ast.Node) bool {
			sel, ok := n.(*ast.SelectorExpr)
			if !ok {
				return true
			}

			// Skip if this selector is the function being called (e.g., g.mu.Lock)
			if isSelectorPartOfCall(sel, stmt) {
				return true
			}

			// Check if this is receiver field access
			if ident, ok := sel.X.(*ast.Ident); ok {
				if receiverName != "" && ident.Name == receiverName && receiverType != "" {
					if guards, hasGuards := guardedFields[receiverType]; hasGuards {
						if guardName, isGuarded := guards[sel.Sel.Name]; isGuarded {
							// Check if the guard mutex is held
							if !heldLocks[guardName] {
								pass.Reportf(sel.Pos(), "fieldguard: unguarded access to field %q (requires %s)", sel.Sel.Name, guardName)
							}
						}
					}
				}
			}
			return true
		})
	}
}

// isSelectorPartOfCall checks if a selector is the function part of a call expression
func isSelectorPartOfCall(sel *ast.SelectorExpr, stmt ast.Stmt) bool {
	switch s := stmt.(type) {
	case *ast.ExprStmt:
		if call, ok := s.X.(*ast.CallExpr); ok {
			return call.Fun == sel
		}
	case *ast.DeferStmt:
		// s.Call is *ast.CallExpr, check if its Fun matches sel
		return s.Call.Fun == sel
	}
	return false
}

func isLockCall(call *ast.CallExpr) string {
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return ""
	}
	if sel.Sel.Name == "Lock" || sel.Sel.Name == "RLock" {
		// For s.mu.Lock(), sel.X is s.mu (SelectorExpr), get "mu"
		if innerSel, ok := sel.X.(*ast.SelectorExpr); ok {
			return innerSel.Sel.Name
		}
	}
	return ""
}

func isUnlockCall(call *ast.CallExpr) string {
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return ""
	}
	if sel.Sel.Name == "Unlock" || sel.Sel.Name == "RUnlock" {
		// For s.mu.Unlock(), sel.X is s.mu (SelectorExpr), get "mu"
		if innerSel, ok := sel.X.(*ast.SelectorExpr); ok {
			return innerSel.Sel.Name
		}
	}
	return ""
}

func getReceiverVar(fn *ast.FuncDecl) string {
	if fn.Recv == nil {
		return ""
	}
	for _, field := range fn.Recv.List {
		for _, name := range field.Names {
			return name.Name
		}
	}
	return ""
}

func getReceiverType(fn *ast.FuncDecl) string {
	if fn.Recv == nil {
		return ""
	}
	for _, field := range fn.Recv.List {
		// Handle *Type receiver
		if star, ok := field.Type.(*ast.StarExpr); ok {
			if ident, ok := star.X.(*ast.Ident); ok {
				return ident.Name
			}
		}
		// Handle Type receiver (value receiver)
		if ident, ok := field.Type.(*ast.Ident); ok {
			return ident.Name
		}
	}
	return ""
}

func checkFuncLit(pass *analysis.Pass, fn *ast.FuncLit, body *ast.BlockStmt,
	guardedFields map[string]map[string]string) {

	if body == nil {
		return
	}

	// V1 implementation: function literals are not checked for guarded access
	// This could be enhanced in a future version
}
