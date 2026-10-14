package codeindex

import (
	"fmt"
	"go/ast"
	"go/token"
	"strings"
)

// renderPageBody renders one file's CODE page body: package clause,
// imports, and one line per top-level declaration as a readable Go-ish
// signature. Doc comments are omitted (body stays a compact index; the
// full source stays on disk). Example:
//
//	package alpha
//
//	import "fmt"
//
//	func Alpha() string
//	func (s *Server) Start(ctx context.Context) error
//	type Server struct
//	const MaxSize = 10
//	var defaultName string
func renderPageBody(af *ast.File) string {
	var b strings.Builder
	fmt.Fprintf(&b, "package %s\n", af.Name.Name)

	if len(af.Imports) > 0 {
		b.WriteString("\n")
		for _, imp := range af.Imports {
			path := strings.Trim(imp.Path.Value, `"`)
			if imp.Name != nil {
				fmt.Fprintf(&b, "import %s %q\n", imp.Name.Name, path)
			} else {
				fmt.Fprintf(&b, "import %q\n", path)
			}
		}
	}

	for _, decl := range af.Decls {
		switch d := decl.(type) {
		case *ast.FuncDecl:
			if sig := renderFuncSig(d); sig != "" {
				b.WriteString("\n")
				b.WriteString(sig)
				b.WriteString("\n")
			}
		case *ast.GenDecl:
			for _, spec := range d.Specs {
				if line := renderSpec(d.Tok, spec); line != "" {
					b.WriteString("\n")
					b.WriteString(line)
					b.WriteString("\n")
				}
			}
		}
	}
	return b.String()
}

// renderFuncSig renders a func/method declaration as a one-line signature.
func renderFuncSig(d *ast.FuncDecl) string {
	var b strings.Builder
	b.WriteString("func ")
	if d.Recv != nil && len(d.Recv.List) > 0 {
		fmt.Fprintf(&b, "(%s) ", renderFieldNames(d.Recv.List[0].Names, d.Recv.List[0].Type))
	}
	b.WriteString(d.Name.Name)
	b.WriteString(renderSignature(d.Type))
	return strings.TrimSpace(b.String())
}

// renderSignature renders params/results of a *ast.Signature (no "func"
// keyword, no name). Parameter names are included when present.
func renderSignature(sig *ast.FuncType) string {
	var parts []string
	if sig.Params != nil {
		for _, p := range sig.Params.List {
			parts = append(parts, renderFieldNames(p.Names, p.Type))
		}
	}
	paramStr := "(" + strings.Join(parts, ", ") + ")"

	if sig.Results == nil || len(sig.Results.List) == 0 {
		return paramStr
	}
	var res []string
	for _, r := range sig.Results.List {
		res = append(res, renderFieldNames(r.Names, r.Type))
	}
	if len(res) == 1 && sig.Results.List[0].Names == nil {
		return paramStr + " " + res[0]
	}
	return paramStr + " (" + strings.Join(res, ", ") + ")"
}

// renderFieldNames renders "name1, name2 Type" (or just "Type" when no
// names).
func renderFieldNames(names []*ast.Ident, typ ast.Expr) string {
	rendered := renderExpr(typ)
	if len(names) == 0 {
		return rendered
	}
	var ns []string
	for _, n := range names {
		ns = append(ns, n.Name)
	}
	return strings.Join(ns, ", ") + " " + rendered
}

// renderSpec renders one type/var/const spec as a one-liner.
func renderSpec(tok token.Token, spec ast.Spec) string {
	switch sp := spec.(type) {
	case *ast.TypeSpec:
		return "type " + sp.Name.Name + " " + strings.TrimSpace(renderExpr(sp.Type))
	case *ast.ValueSpec:
		kind := "var"
		if tok == token.CONST {
			kind = "const"
		}
		var b strings.Builder
		b.WriteString(kind + " ")
		for i, name := range sp.Names {
			if i > 0 {
				b.WriteString(", ")
			}
			b.WriteString(name.Name)
		}
		if sp.Type != nil {
			b.WriteString(" " + renderExpr(sp.Type))
		}
		return b.String()
	default:
		return ""
	}
}

// renderExpr renders a type expression back to readable Go source
// (qualified with the package's own identifier spellings). Falls back to
// the go/printer snippet, then "…" for exotic nodes.
func renderExpr(expr ast.Expr) string {
	switch t := expr.(type) {
	case nil:
		return ""
	case *ast.Ident:
		return t.Name
	case *ast.StarExpr:
		return "*" + renderExpr(t.X)
	case *ast.SelectorExpr:
		return renderExpr(t.X) + "." + t.Sel.Name
	case *ast.ArrayType:
		if t.Len == nil {
			return "[]" + renderExpr(t.Elt)
		}
		return "[…]" + renderExpr(t.Elt)
	case *ast.MapType:
		return "map[" + renderExpr(t.Key) + "]" + renderExpr(t.Value)
	case *ast.ChanType:
		switch t.Dir {
		case ast.RECV:
			return "<-chan " + renderExpr(t.Value)
		case ast.SEND:
			return "chan<- " + renderExpr(t.Value)
		default:
			return "chan " + renderExpr(t.Value)
		}
	case *ast.FuncType:
		return "func" + renderSignature(t)
	case *ast.Ellipsis:
		return "..." + renderExpr(t.Elt)
	case *ast.StructType:
		return "struct{" + fmt.Sprintf("%d field(s)", len(t.Fields.List)) + "}"
	case *ast.InterfaceType:
		return "interface{" + fmt.Sprintf("%d method(s)", len(t.Methods.List)) + "}"
	case *ast.IndexExpr:
		return renderExpr(t.X) + "[" + renderExpr(t.Index) + "]"
	case *ast.IndexListExpr:
		var idx []string
		for _, ix := range t.Indices {
			idx = append(idx, renderExpr(ix))
		}
		return renderExpr(t.X) + "[" + strings.Join(idx, ", ") + "]"
	case *ast.ParenExpr:
		return "(" + renderExpr(t.X) + ")"
	case *ast.BasicLit:
		return t.Value
	case *ast.BinaryExpr:
		return renderExpr(t.X) + " " + t.Op.String() + " " + renderExpr(t.Y)
	case *ast.CallExpr:
		var args []string
		for _, a := range t.Args {
			args = append(args, renderExpr(a))
		}
		return renderExpr(t.Fun) + "(" + strings.Join(args, ", ") + ")"
	case *ast.UnaryExpr:
		return t.Op.String() + renderExpr(t.X)
	default:
		return "…"
	}
}
