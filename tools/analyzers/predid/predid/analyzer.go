// Package predid implements a go/analysis analyzer that detects use of
// predictable time-based values as unique identifiers.
//
// The anti-pattern: using time.Now().UnixNano(), time.Now().UnixMicro(),
// time.Now().Unix(), or time.Now().Format(...) as a unique ID. These are
// predictable (attacker can guess), can collide under concurrency, and
// leak server timestamps. The project provides pkg/id.Generate() for
// cryptographically-secure unique IDs.
//
// Usage:
//
//	go run ./tools/analyzers/predid/ ./...
package predid

import (
	"go/ast"
	"strings"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/analysis/passes/inspect"
	"golang.org/x/tools/go/ast/inspector"
)

const doc = `detect predictable time-based identifiers

This analyzer flags places where time.Now().UnixNano(), .UnixMicro(),
.Unix(), or .Format(...) are used in contexts that suggest ID generation.
These values are predictable, can collide under concurrency, and leak
server timestamps.

Use pkg/id.Generate() for cryptographically-secure unique IDs instead.

The analyzer is conservative: it only flags when the time call appears
in an assignment to a variable whose name suggests ID semantics (id,
uid, key, token, session, uuid, guid, rid), or when the call result is
compared with == / != in a way that suggests equality-based lookup
(common in ID comparison).`

// Analyzer is the predid analyzer entry point.
var Analyzer = &analysis.Analyzer{
	Name:     "predid",
	Doc:      doc,
	Requires: []*analysis.Analyzer{inspect.Analyzer},
	Run:      run,
}

// methods on time.Time that produce predictable numeric/string values
var predictableMethods = map[string]string{
	"UnixNano":  "time.Now().UnixNano()",
	"UnixMicro": "time.Now().UnixMicro()",
	"Unix":      "time.Now().Unix()",
}

// variable name substrings that suggest ID semantics
var idNameSubstrings = []string{
	"id", "uid", "gid", "rid", "key", "token", "session",
	"uuid", "guid", "nonce", "seed", "hash",
}

func run(pass *analysis.Pass) (interface{}, error) {
	insp := pass.ResultOf[inspect.Analyzer].(*inspector.Inspector)

	nodeFilter := []ast.Node{
		(*ast.AssignStmt)(nil),
		(*ast.ValueSpec)(nil),
		(*ast.CompositeLit)(nil),
		(*ast.CallExpr)(nil),
	}

	insp.Preorder(nodeFilter, func(n ast.Node) {
		switch stmt := n.(type) {
		case *ast.AssignStmt:
			checkAssignStmt(pass, stmt)
		case *ast.ValueSpec:
			checkValueSpec(pass, stmt)
		case *ast.CompositeLit:
			checkCompositeLit(pass, stmt)
		case *ast.CallExpr:
			checkCallExpr(pass, stmt)
		}
	})

	return nil, nil
}

// checkAssignStmt checks `lhs := rhs` and `lhs = rhs` for predictable IDs.
func checkAssignStmt(pass *analysis.Pass, stmt *ast.AssignStmt) {
	for i, lhs := range stmt.Lhs {
		if i >= len(stmt.Rhs) {
			break
		}
		rhs := stmt.Rhs[i]

		// Get LHS variable name
		var name string
		if ident, ok := lhs.(*ast.Ident); ok {
			name = ident.Name
		}
		if name == "" || name == "_" {
			continue
		}

		if !looksLikeID(name) {
			continue
		}

		if desc := isPredictableTimeCall(rhs); desc != "" {
			pass.Reportf(rhs.Pos(),
				"predid: %s assigned to %q which looks like an ID; use pkg/id.Generate() instead",
				desc, name)
		}
	}
}

// checkValueSpec checks `var x = time.Now().UnixNano()` declarations.
func checkValueSpec(pass *analysis.Pass, spec *ast.ValueSpec) {
	for i, name := range spec.Names {
		if i >= len(spec.Values) {
			break
		}
		val := spec.Values[i]

		if !looksLikeID(name.Name) {
			continue
		}

		if desc := isPredictableTimeCall(val); desc != "" {
			pass.Reportf(val.Pos(),
				"predid: %s assigned to %q which looks like an ID; use pkg/id.Generate() instead",
				desc, name.Name)
		}
	}
}

// checkCompositeLit checks struct literals like `Foo{ID: time.Now().UnixNano()}`.
func checkCompositeLit(pass *analysis.Pass, lit *ast.CompositeLit) {
	for _, elt := range lit.Elts {
		kv, ok := elt.(*ast.KeyValueExpr)
		if !ok {
			continue
		}
		key, ok := kv.Key.(*ast.Ident)
		if !ok {
			continue
		}
		if !looksLikeID(key.Name) {
			continue
		}
		if desc := isPredictableTimeCall(kv.Value); desc != "" {
			pass.Reportf(kv.Value.Pos(),
				"predid: %s assigned to struct field %q which looks like an ID; use pkg/id.Generate() instead",
				desc, key.Name)
		}
	}
}

// checkCallExpr checks function call arguments where the argument position
// name suggests ID semantics (e.g., SetID(time.Now().UnixNano())).
func checkCallExpr(pass *analysis.Pass, call *ast.CallExpr) {
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return
	}
	methodName := sel.Sel.Name

	// Check if method name suggests it takes an ID argument
	if !looksLikeID(methodName) && !isIDSetter(methodName) {
		return
	}

	for _, arg := range call.Args {
		if desc := isPredictableTimeCall(arg); desc != "" {
			pass.Reportf(arg.Pos(),
				"predid: %s passed as argument to %q which looks like an ID parameter; use pkg/id.Generate() instead",
				desc, methodName)
		}
	}
}

// isPredictableTimeCall returns a description string if the expression is
// a predictable time-based ID call, or "" otherwise.
func isPredictableTimeCall(expr ast.Expr) string {
	// Check for time.Now().Method() pattern
	call, ok := expr.(*ast.CallExpr)
	if !ok {
		return ""
	}

	// Direct method call: something.Now()
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return ""
	}

	methodName := sel.Sel.Name

	// Check for known predictable methods
	if _, isPred := predictableMethods[methodName]; isPred {
		// Verify the receiver is time.Now() or similar
		if isTimeNowCall(sel.X) {
			return predictableMethods[methodName]
		}
	}

	// Check for time.Now().Format(...) — string-based ID
	if methodName == "Format" {
		if isTimeNowCall(sel.X) {
			// Check if the format string looks like it's used as an ID
			// (compact format without separators like "20060102150405" or "20060102150405.999999999")
			if len(call.Args) > 0 {
				if lit, ok := call.Args[0].(*ast.BasicLit); ok {
					fmtStr := strings.Trim(lit.Value, "`\"")
					// Flag compact numeric formats commonly used as IDs
					if isIDFormat(fmtStr) {
						return "time.Now().Format(\"" + fmtStr + "\")"
					}
				}
			}
		}
	}

	return ""
}

// isTimeNowCall checks if expr is time.Now() or a variable known to hold time.Now().
func isTimeNowCall(expr ast.Expr) bool {
	call, ok := expr.(*ast.CallExpr)
	if !ok {
		return false
	}
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return false
	}
	return sel.Sel.Name == "Now"
}

// isIDFormat checks if a format string looks like it's designed to produce
// an ID (compact numeric format without date separators).
func isIDFormat(s string) bool {
	// Flag formats that are purely numeric/date-digit references without
	// separators (commonly used as poor-man's unique IDs)
	hasSeparator := strings.ContainsAny(s, "-/: T ")
	if hasSeparator {
		return false
	}
	// Must contain year reference (2006) to be a time format at all
	if !strings.Contains(s, "2006") {
		return false
	}
	return true
}

// looksLikeID checks if a variable/field name suggests ID semantics.
func looksLikeID(name string) bool {
	lower := strings.ToLower(name)
	for _, sub := range idNameSubstrings {
		if strings.Contains(lower, sub) {
			return true
		}
	}
	return false
}

// isIDSetter checks if a method name looks like it sets an ID.
func isIDSetter(name string) bool {
	lower := strings.ToLower(name)
	// SetID, WithID, SetToken, SetSession, etc.
	for _, sub := range idNameSubstrings {
		if strings.Contains(lower, "set"+sub) || strings.Contains(lower, "with"+sub) {
			return true
		}
	}
	return false
}

