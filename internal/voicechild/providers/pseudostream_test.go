package providers

// Gate tests (leaf 00T, features F2/F4, contracts C2/C5). The package must
// stay compilable before leaf 02 lands newPseudoStreamSTT / STTBaseFactory,
// so the missing-symbol gate (TestPseudoStreamEmitsPartialsThenFinal) probes
// the package source via go/parser and names each absent contract symbol;
// it turns GREEN with zero edits once the decorator lands. The guardrail
// (TestNewSTTDefaultNoDecorator) uses only today's symbols and passes now.

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"strings"
	"testing"
)

// TestNewSTTDefaultNoDecorator (F4 guardrail): with no partial interval
// configured (the zero value), NewSTT must return the BASE provider itself,
// never a decorator wrapper. This passes today and must stay green.
func TestNewSTTDefaultNoDecorator(t *testing.T) {
	stt, err := NewSTT(ProviderConfig{
		Vendor: "rumble", Model: "Reson8/Rumble-2-Flash",
		BaseURL: "http://127.0.0.1:1", SampleRate: 16000,
	}, "")
	if err != nil {
		t.Fatalf("NewSTT: %v", err)
	}
	if _, ok := stt.(*RumbleSTT); !ok {
		t.Fatalf("NewSTT returned %T, want *RumbleSTT (no decorator on the legacy/zero-interval path)", stt)
	}
}

// ---- source-level gate helpers (keep the package compiling pre-leaf-02) ----

func gateParseProviders(t *testing.T) map[string]*ast.File {
	t.Helper()
	fset := token.NewFileSet()
	pkgs, err := parser.ParseDir(fset, ".", func(fi fs.FileInfo) bool {
		return !fi.IsDir() && strings.HasSuffix(fi.Name(), ".go") && !strings.HasSuffix(fi.Name(), "_test.go")
	}, 0)
	if err != nil {
		t.Fatalf("parse providers package source: %v", err)
	}
	out := make(map[string]*ast.File)
	for _, pkg := range pkgs {
		for name, f := range pkg.Files {
			out[name] = f
		}
	}
	if len(out) == 0 {
		t.Fatal("no provider source files found to gate")
	}
	return out
}

func gateFindFunc(t *testing.T, files map[string]*ast.File, name string) *ast.FuncDecl {
	t.Helper()
	for _, f := range files {
		for _, d := range f.Decls {
			if fd, ok := d.(*ast.FuncDecl); ok && fd.Name.Name == name {
				return fd
			}
		}
	}
	return nil
}

func gateFindType(t *testing.T, files map[string]*ast.File, name string) *ast.TypeSpec {
	t.Helper()
	for _, f := range files {
		for _, d := range f.Decls {
			gd, ok := d.(*ast.GenDecl)
			if !ok || gd.Tok != token.TYPE {
				continue
			}
			for _, s := range gd.Specs {
				if ts, ok := s.(*ast.TypeSpec); ok && ts.Name.Name == name {
					return ts
				}
			}
		}
	}
	return nil
}

func gateExprText(e ast.Expr) string {
	switch x := e.(type) {
	case *ast.Ident:
		return x.Name
	case *ast.SelectorExpr:
		return gateExprText(x.X) + "." + x.Sel.Name
	case *ast.StarExpr:
		return "*" + gateExprText(x.X)
	case *ast.FuncType:
		return "func"
	case *ast.ArrayType:
		return "[]" + gateExprText(x.Elt)
	default:
		return fmt.Sprintf("%T", e)
	}
}

func gateCheckSignature(t *testing.T, label string, ft *ast.FuncType, wantParams, wantResults int) {
	t.Helper()
	if ft == nil {
		t.Fatalf("%s has no function signature", label)
	}
	if ft.Params == nil || len(ft.Params.List) != wantParams {
		t.Fatalf("%s has %d params, want %d (contract C5)", label, fieldCount(ft.Params), wantParams)
	}
	if ft.Results == nil || len(ft.Results.List) != wantResults {
		t.Fatalf("%s has %d results, want %d (contract C5)", label, resultCount(ft.Results), wantResults)
	}
}

func fieldCount(fl *ast.FieldList) int {
	if fl == nil {
		return 0
	}
	return len(fl.List)
}

func resultCount(fl *ast.FieldList) int {
	if fl == nil {
		return 0
	}
	return len(fl.List)
}

// TestPseudoStreamEmitsPartialsThenFinal (F2/C5): the provider-agnostic
// decorator must exist with the exact C5 shape, NewSTT must opt into it only
// when PartialIntervalMs > 0, and the decorator must emit BOTH a
// Transcript{Final:false} partial and exactly one Transcript{Final:true}.
// RED today: newPseudoStreamSTT / STTBaseFactory / PartialIntervalMs are
// absent. The runtime partial/final timing itself is covered by the
// companion e2e gate (00Te) and the implementing leaf's own tests.
func TestPseudoStreamEmitsPartialsThenFinal(t *testing.T) {
	files := gateParseProviders(t)

	// 1. STTBaseFactory type: func(ProviderConfig, string) (STTProvider, error).
	ts := gateFindType(t, files, "STTBaseFactory")
	if ts == nil {
		t.Fatalf("missing symbol type STTBaseFactory (contract C5 F2): generic pseudo-stream cannot be built")
	}
	ft, ok := ts.Type.(*ast.FuncType)
	if !ok {
		t.Fatalf("STTBaseFactory is %T, want a func type (contract C5)", ts.Type)
	}
	gateCheckSignature(t, "STTBaseFactory", ft, 2, 2)
	if got := gateExprText(ft.Params.List[0].Type); got != "ProviderConfig" {
		t.Errorf("STTBaseFactory param 0 = %s, want ProviderConfig", got)
	}
	if got := gateExprText(ft.Params.List[1].Type); got != "string" {
		t.Errorf("STTBaseFactory param 1 = %s, want string (key)", got)
	}
	if got := gateExprText(ft.Results.List[0].Type); !strings.Contains(got, "STTProvider") {
		t.Errorf("STTBaseFactory result 0 = %s, want STTProvider", got)
	}
	if got := gateExprText(ft.Results.List[1].Type); got != "error" {
		t.Errorf("STTBaseFactory result 1 = %s, want error", got)
	}

	// 2. newPseudoStreamSTT(cfg, key, base) (STTProvider, error).
	fd := gateFindFunc(t, files, "newPseudoStreamSTT")
	if fd == nil {
		t.Fatalf("missing symbol func newPseudoStreamSTT (contract C5 F2): no client-side pseudo-stream")
	}
	gateCheckSignature(t, "newPseudoStreamSTT", fd.Type, 3, 2)
	if got := gateExprText(fd.Type.Params.List[0].Type); got != "ProviderConfig" {
		t.Errorf("newPseudoStreamSTT param 0 = %s, want ProviderConfig", got)
	}
	if got := gateExprText(fd.Type.Params.List[1].Type); got != "string" {
		t.Errorf("newPseudoStreamSTT param 1 = %s, want string (key)", got)
	}
	switch p2 := fd.Type.Params.List[2].Type.(type) {
	case *ast.FuncType: // inline func literal
	case *ast.Ident:
		if p2.Name != "STTBaseFactory" {
			t.Errorf("newPseudoStreamSTT param 2 = %s, want STTBaseFactory/base func", p2.Name)
		}
	default:
		t.Errorf("newPseudoStreamSTT param 2 = %s, want STTBaseFactory/base func", gateExprText(p2))
	}
	if got := gateExprText(fd.Type.Results.List[0].Type); !strings.Contains(got, "STTProvider") {
		t.Errorf("newPseudoStreamSTT result 0 = %s, want STTProvider", got)
	}
	if got := gateExprText(fd.Type.Results.List[1].Type); got != "error" {
		t.Errorf("newPseudoStreamSTT result 1 = %s, want error", got)
	}

	// 3. NewSTT opts in only when PartialIntervalMs > 0 and wraps via the decorator.
	nf := gateFindFunc(t, files, "NewSTT")
	if nf == nil || nf.Body == nil {
		t.Fatalf("missing func NewSTT body (contract C5)")
	}
	seen := make(map[string]bool)
	ast.Inspect(nf.Body, func(n ast.Node) bool {
		if id, ok := n.(*ast.Ident); ok {
			seen[id.Name] = true
		}
		return true
	})
	if !seen["PartialIntervalMs"] {
		t.Errorf("NewSTT does not consult PartialIntervalMs - the decorator is not opt-in (contract C5)")
	}
	if !seen["newPseudoStreamSTT"] {
		t.Errorf("NewSTT does not call newPseudoStreamSTT - decorator is not wired (contract C5)")
	}

	// 4. Both a Final:false partial and a Final:true final must be emitted.
	var hasFalse, hasTrue bool
	for _, f := range files {
		ast.Inspect(f, func(n ast.Node) bool {
			kv, ok := n.(*ast.KeyValueExpr)
			if !ok {
				return true
			}
			key, ok := kv.Key.(*ast.Ident)
			if !ok || key.Name != "Final" {
				return true
			}
			if id, ok := kv.Value.(*ast.Ident); ok {
				switch id.Name {
				case "false":
					hasFalse = true
				case "true":
					hasTrue = true
				}
			}
			return true
		})
	}
	if !hasFalse {
		t.Errorf("no Transcript{Final:false} partial emission found (contract C5: intermediate partials)")
	}
	if !hasTrue {
		t.Errorf("no Transcript{Final:true} final emission found (contract C5: exactly one final)")
	}
}
