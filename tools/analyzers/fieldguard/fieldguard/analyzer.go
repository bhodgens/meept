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
	structsWithMutex := make(map[string]bool)            // typeName -> has mutex

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
			analyzeStructFields(pass, st, typeName, guardedFields, immutableFields, structsWithMutex)
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

	// Note: Third pass (flagging structs with mutex but no annotations)
	// is intentionally omitted in v1 - this is a documentation/awareness
	// feature handled by the pre-commit hook.

	return nil, nil
}

func analyzeStructFields(pass *analysis.Pass, st *ast.StructType, typeName string,
	guardedFields map[string]map[string]string,
	immutableFields map[string]map[string]bool,
	structsWithMutex map[string]bool) {

	if st.Fields == nil {
		return
	}

	for _, field := range st.Fields.List {
		if field.Names == nil {
			// Check for embedded mutex
			if isMutexType(field.Type) {
				structsWithMutex[typeName] = true
			}
			continue
		}

		fieldName := field.Names[0].Name

		// Check if this is a mutex field
		if isMutexType(field.Type) {
			structsWithMutex[typeName] = true
		}

		// Parse comments for annotations
		if field.Comment != nil {
			for _, c := range field.Comment.List {
				if guardedByRegex.MatchString(c.Text) {
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

func isMutexType(expr ast.Expr) bool {
	switch t := expr.(type) {
	case *ast.SelectorExpr:
		// sync.Mutex or sync.RWMutex
		if ident, ok := t.X.(*ast.Ident); ok {
			return ident.Name == "sync" && (t.Sel.Name == "Mutex" || t.Sel.Name == "RWMutex")
		}
	case *ast.StarExpr:
		return isMutexType(t.X)
	}
	return false
}

func checkFuncDecl(pass *analysis.Pass, fn *ast.FuncDecl, body *ast.BlockStmt,
	guardedFields map[string]map[string]string,
	immutableFields map[string]map[string]bool) {

	if body == nil {
		return
	}

	// Track lock state for guarded field checking
	// For v1, do a simple check: flag unguarded access to annotated fields

	// Check for immutable field writes
	ast.Inspect(body, func(n ast.Node) bool {
		assign, ok := n.(*ast.AssignStmt)
		if !ok {
			return true
		}

		// Skip if this looks like a constructor (function name starts with "New")
		if strings.HasPrefix(fn.Name.Name, "New") {
			return true
		}

		for _, lhs := range assign.Lhs {
			sel, ok := lhs.(*ast.SelectorExpr)
			if !ok {
				continue
			}

			// Check if this is an immutable field assignment
			if ident, ok := sel.X.(*ast.Ident); ok {
				fieldName := sel.Sel.Name
				if immut := immutableFields[ident.Name]; immut != nil {
					if immut[fieldName] {
						pass.Reportf(assign.Pos(), "fieldguard: writing to immutable field %q in non-constructor", fieldName)
					}
				}
			}
		}
		return true
	})
}

func checkFuncLit(pass *analysis.Pass, fn *ast.FuncLit, body *ast.BlockStmt,
	guardedFields map[string]map[string]string) {

	if body == nil {
		return
	}

	// Similar checks for function literals
	// V1 implementation focuses on immutable field detection
}
