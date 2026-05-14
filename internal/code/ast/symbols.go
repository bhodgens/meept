package ast

import (
	"context"
	"os"
	"slices"
	"strings"

	sitter "github.com/smacker/go-tree-sitter"
)

// SymbolExtractor extracts code symbols from parsed source.
type SymbolExtractor struct {
	parser *ParserManager
}

// NewSymbolExtractor creates a new symbol extractor.
func NewSymbolExtractor(parser *ParserManager) *SymbolExtractor {
	return &SymbolExtractor{parser: parser}
}

// ExtractFromSource extracts symbols from source code.
func (e *SymbolExtractor) ExtractFromSource(ctx context.Context, source []byte, lang Language, filter SymbolFilter) ([]Symbol, error) {
	tree, err := e.parser.GetTree(ctx, source, lang)
	if err != nil {
		return nil, err
	}

	return e.extractSymbols(tree.RootNode(), source, lang, filter, 0), nil
}

// ExtractFromFile extracts symbols from a file.
func (e *SymbolExtractor) ExtractFromFile(ctx context.Context, filePath string) ([]Symbol, error) {
	return e.ExtractFromFileWithFilter(ctx, filePath, DefaultSymbolFilter())
}

// ExtractFromFileWithFilter extracts symbols from a file with a custom filter.
func (e *SymbolExtractor) ExtractFromFileWithFilter(ctx context.Context, filePath string, filter SymbolFilter) ([]Symbol, error) {
	lang := DetectLanguage(filePath)
	if lang == LangUnknown {
		return nil, nil
	}

	// Read file source
	source, err := os.ReadFile(filePath)
	if err != nil {
		return nil, err
	}

	tree, err := e.parser.GetTree(ctx, source, lang)
	if err != nil {
		return nil, err
	}

	symbols := e.extractSymbols(tree.RootNode(), source, lang, filter, 0)
	for i := range symbols {
		symbols[i].FilePath = filePath
		symbols[i].Language = lang
	}

	return symbols, nil
}

// extractSymbols recursively extracts symbols from a node.
func (e *SymbolExtractor) extractSymbols(node *sitter.Node, source []byte, lang Language, filter SymbolFilter, depth int) []Symbol {
	if node == nil {
		return nil
	}

	// Check depth limit
	if filter.MaxDepth > 0 && depth >= filter.MaxDepth {
		return nil
	}

	var symbols []Symbol

	// Language-specific symbol extraction
	switch lang {
	case LangGo:
		symbols = e.extractGoSymbols(node, source, filter, depth)
	case LangPython:
		symbols = e.extractPythonSymbols(node, source, filter, depth)
	case LangTypeScript, LangJavaScript:
		symbols = e.extractJSSymbols(node, source, filter, depth)
	case LangRust:
		symbols = e.extractRustSymbols(node, source, filter, depth)
	case LangJava:
		symbols = e.extractJavaSymbols(node, source, filter, depth)
	case LangC, LangCpp:
		symbols = e.extractCSymbols(node, source, filter, depth)
	case LangRuby:
		symbols = e.extractRubySymbols(node, source, filter, depth)
	default:
		// Generic extraction for other languages
		symbols = e.extractGenericSymbols(node, source, filter, depth)
	}

	return symbols
}

// extractGoSymbols extracts symbols from Go code.
func (e *SymbolExtractor) extractGoSymbols(node *sitter.Node, source []byte, filter SymbolFilter, depth int) []Symbol {
	var symbols []Symbol

	nodeType := node.Type()

	switch nodeType {
	case NodeFunctionDeclaration:
		name := e.getChildByFieldName(node, "name", source)
		if name != "" && e.shouldInclude(name, SymbolKindFunction, filter) {
			sym := Symbol{
				Name:       name,
				Kind:       SymbolKindFunction,
				Range:      nodeToRange(node),
				Signature:  e.getGoFunctionSignature(node, source),
				DocComment: e.getPrecedingComment(node, source),
			}
			symbols = append(symbols, sym)
		}

	case NodeMethodDeclaration:
		name := e.getChildByFieldName(node, "name", source)
		if name != "" && e.shouldInclude(name, SymbolKindMethod, filter) {
			sym := Symbol{
				Name:       name,
				Kind:       SymbolKindMethod,
				Range:      nodeToRange(node),
				Signature:  e.getGoMethodSignature(node, source),
				DocComment: e.getPrecedingComment(node, source),
			}
			symbols = append(symbols, sym)
		}

	case "type_declaration":
		// Handle type spec children
		for i := range node.ChildCount() {
			child := node.Child(int(i))
			if child != nil && child.Type() == "type_spec" {
				name := e.getChildByFieldName(child, "name", source)
				typeNode := e.getChildNodeByFieldName(child, "type")
				kind := SymbolKindClass
				if typeNode != nil {
					switch typeNode.Type() {
					case "struct_type":
						kind = SymbolKindStruct
					case "interface_type":
						kind = SymbolKindInterface
					}
				}
				if name != "" && e.shouldInclude(name, kind, filter) {
					sym := Symbol{
						Name:       name,
						Kind:       kind,
						Range:      nodeToRange(child),
						DocComment: e.getPrecedingComment(node, source),
					}
					symbols = append(symbols, sym)
				}
			}
		}

	case "const_declaration", "var_declaration":
		kind := SymbolKindVariable
		if nodeType == "const_declaration" {
			kind = SymbolKindConstant
		}
		for i := range node.ChildCount() {
			child := node.Child(int(i))
			if child != nil && (child.Type() == "const_spec" || child.Type() == "var_spec") {
				name := e.getChildByFieldName(child, "name", source)
				if name != "" && e.shouldInclude(name, kind, filter) {
					sym := Symbol{
						Name:  name,
						Kind:  kind,
						Range: nodeToRange(child),
					}
					symbols = append(symbols, sym)
				}
			}
		}
	}

	// Recurse into children
	for i := range node.ChildCount() {
		child := node.Child(int(i))
		if child != nil {
			childSymbols := e.extractGoSymbols(child, source, filter, depth+1)
			symbols = append(symbols, childSymbols...)
		}
	}

	return symbols
}

// extractPythonSymbols extracts symbols from Python code.
func (e *SymbolExtractor) extractPythonSymbols(node *sitter.Node, source []byte, filter SymbolFilter, depth int) []Symbol {
	var symbols []Symbol

	switch node.Type() {
	case NodeFunctionDefinition:
		name := e.getChildByFieldName(node, "name", source)
		if name != "" && e.shouldInclude(name, SymbolKindFunction, filter) {
			sym := Symbol{
				Name:       name,
				Kind:       SymbolKindFunction,
				Range:      nodeToRange(node),
				Signature:  e.getPythonFunctionSignature(node, source),
				DocComment: e.getPythonDocstring(node, source),
			}
			symbols = append(symbols, sym)
		}

	case "class_definition":
		name := e.getChildByFieldName(node, "name", source)
		if name != "" && e.shouldInclude(name, SymbolKindClass, filter) {
			sym := Symbol{
				Name:       name,
				Kind:       SymbolKindClass,
				Range:      nodeToRange(node),
				DocComment: e.getPythonDocstring(node, source),
			}
			// Extract methods as children
			for i := range node.ChildCount() {
				child := node.Child(int(i))
				if child != nil && child.Type() == "block" {
					childSymbols := e.extractPythonSymbols(child, source, filter, depth+1)
					sym.Children = childSymbols
				}
			}
			symbols = append(symbols, sym)
			return symbols // Don't recurse again
		}
	}

	// Recurse into children
	for i := range node.ChildCount() {
		child := node.Child(int(i))
		if child != nil {
			childSymbols := e.extractPythonSymbols(child, source, filter, depth+1)
			symbols = append(symbols, childSymbols...)
		}
	}

	return symbols
}

// extractJSSymbols extracts symbols from JavaScript/TypeScript code.
func (e *SymbolExtractor) extractJSSymbols(node *sitter.Node, source []byte, filter SymbolFilter, depth int) []Symbol {
	var symbols []Symbol

	switch node.Type() {
	case NodeFunctionDeclaration:
		name := e.getChildByFieldName(node, "name", source)
		if name != "" && e.shouldInclude(name, SymbolKindFunction, filter) {
			symbols = append(symbols, Symbol{
				Name:  name,
				Kind:  SymbolKindFunction,
				Range: nodeToRange(node),
			})
		}

	case NodeClassDeclaration:
		name := e.getChildByFieldName(node, "name", source)
		if name != "" && e.shouldInclude(name, SymbolKindClass, filter) {
			symbols = append(symbols, Symbol{
				Name:  name,
				Kind:  SymbolKindClass,
				Range: nodeToRange(node),
			})
		}

	case "method_definition":
		name := e.getChildByFieldName(node, "name", source)
		if name != "" && e.shouldInclude(name, SymbolKindMethod, filter) {
			symbols = append(symbols, Symbol{
				Name:  name,
				Kind:  SymbolKindMethod,
				Range: nodeToRange(node),
			})
		}

	case "interface_declaration", "type_alias_declaration":
		name := e.getChildByFieldName(node, "name", source)
		if name != "" && e.shouldInclude(name, SymbolKindInterface, filter) {
			symbols = append(symbols, Symbol{
				Name:  name,
				Kind:  SymbolKindInterface,
				Range: nodeToRange(node),
			})
		}
	}

	// Recurse
	for i := range node.ChildCount() {
		child := node.Child(int(i))
		if child != nil {
			childSymbols := e.extractJSSymbols(child, source, filter, depth+1)
			symbols = append(symbols, childSymbols...)
		}
	}

	return symbols
}

// extractRustSymbols extracts symbols from Rust code.
func (e *SymbolExtractor) extractRustSymbols(node *sitter.Node, source []byte, filter SymbolFilter, depth int) []Symbol {
	var symbols []Symbol

	switch node.Type() {
	case "function_item":
		name := e.getChildByFieldName(node, "name", source)
		if name != "" && e.shouldInclude(name, SymbolKindFunction, filter) {
			symbols = append(symbols, Symbol{
				Name:  name,
				Kind:  SymbolKindFunction,
				Range: nodeToRange(node),
			})
		}

	case "struct_item":
		name := e.getChildByFieldName(node, "name", source)
		if name != "" && e.shouldInclude(name, SymbolKindStruct, filter) {
			symbols = append(symbols, Symbol{
				Name:  name,
				Kind:  SymbolKindStruct,
				Range: nodeToRange(node),
			})
		}

	case "enum_item":
		name := e.getChildByFieldName(node, "name", source)
		if name != "" && e.shouldInclude(name, SymbolKindEnum, filter) {
			symbols = append(symbols, Symbol{
				Name:  name,
				Kind:  SymbolKindEnum,
				Range: nodeToRange(node),
			})
		}

	case "trait_item":
		name := e.getChildByFieldName(node, "name", source)
		if name != "" && e.shouldInclude(name, SymbolKindInterface, filter) {
			symbols = append(symbols, Symbol{
				Name:  name,
				Kind:  SymbolKindInterface,
				Range: nodeToRange(node),
			})
		}

	case "impl_item":
		// For impl blocks, we want to extract the methods inside
		// Skip the impl itself but recurse
	}

	// Recurse
	for i := range node.ChildCount() {
		child := node.Child(int(i))
		if child != nil {
			childSymbols := e.extractRustSymbols(child, source, filter, depth+1)
			symbols = append(symbols, childSymbols...)
		}
	}

	return symbols
}

// extractJavaSymbols extracts symbols from Java code.
func (e *SymbolExtractor) extractJavaSymbols(node *sitter.Node, source []byte, filter SymbolFilter, depth int) []Symbol {
	var symbols []Symbol

	switch node.Type() {
	case NodeMethodDeclaration:
		name := e.getChildByFieldName(node, "name", source)
		if name != "" && e.shouldInclude(name, SymbolKindMethod, filter) {
			symbols = append(symbols, Symbol{
				Name:  name,
				Kind:  SymbolKindMethod,
				Range: nodeToRange(node),
			})
		}

	case NodeClassDeclaration:
		name := e.getChildByFieldName(node, "name", source)
		if name != "" && e.shouldInclude(name, SymbolKindClass, filter) {
			symbols = append(symbols, Symbol{
				Name:  name,
				Kind:  SymbolKindClass,
				Range: nodeToRange(node),
			})
		}

	case "interface_declaration":
		name := e.getChildByFieldName(node, "name", source)
		if name != "" && e.shouldInclude(name, SymbolKindInterface, filter) {
			symbols = append(symbols, Symbol{
				Name:  name,
				Kind:  SymbolKindInterface,
				Range: nodeToRange(node),
			})
		}
	}

	// Recurse
	for i := range node.ChildCount() {
		child := node.Child(int(i))
		if child != nil {
			childSymbols := e.extractJavaSymbols(child, source, filter, depth+1)
			symbols = append(symbols, childSymbols...)
		}
	}

	return symbols
}

// extractCSymbols extracts symbols from C/C++ code.
func (e *SymbolExtractor) extractCSymbols(node *sitter.Node, source []byte, filter SymbolFilter, depth int) []Symbol {
	var symbols []Symbol

	switch node.Type() {
	case NodeFunctionDefinition:
		declarator := e.getChildNodeByFieldName(node, "declarator")
		if declarator != nil {
			name := e.getIdentifierFromDeclarator(declarator, source)
			if name != "" && e.shouldInclude(name, SymbolKindFunction, filter) {
				symbols = append(symbols, Symbol{
					Name:  name,
					Kind:  SymbolKindFunction,
					Range: nodeToRange(node),
				})
			}
		}

	case "struct_specifier":
		name := e.getChildByFieldName(node, "name", source)
		if name != "" && e.shouldInclude(name, SymbolKindStruct, filter) {
			symbols = append(symbols, Symbol{
				Name:  name,
				Kind:  SymbolKindStruct,
				Range: nodeToRange(node),
			})
		}

	case "class_specifier":
		name := e.getChildByFieldName(node, "name", source)
		if name != "" && e.shouldInclude(name, SymbolKindClass, filter) {
			symbols = append(symbols, Symbol{
				Name:  name,
				Kind:  SymbolKindClass,
				Range: nodeToRange(node),
			})
		}

	case "enum_specifier":
		name := e.getChildByFieldName(node, "name", source)
		if name != "" && e.shouldInclude(name, SymbolKindEnum, filter) {
			symbols = append(symbols, Symbol{
				Name:  name,
				Kind:  SymbolKindEnum,
				Range: nodeToRange(node),
			})
		}
	}

	// Recurse
	for i := range node.ChildCount() {
		child := node.Child(int(i))
		if child != nil {
			childSymbols := e.extractCSymbols(child, source, filter, depth+1)
			symbols = append(symbols, childSymbols...)
		}
	}

	return symbols
}

// extractRubySymbols extracts symbols from Ruby code.
func (e *SymbolExtractor) extractRubySymbols(node *sitter.Node, source []byte, filter SymbolFilter, depth int) []Symbol {
	var symbols []Symbol

	switch node.Type() {
	case "method":
		name := e.getChildByFieldName(node, "name", source)
		if name != "" && e.shouldInclude(name, SymbolKindMethod, filter) {
			symbols = append(symbols, Symbol{
				Name:  name,
				Kind:  SymbolKindMethod,
				Range: nodeToRange(node),
			})
		}

	case "class":
		name := e.getChildByFieldName(node, "name", source)
		if name != "" && e.shouldInclude(name, SymbolKindClass, filter) {
			symbols = append(symbols, Symbol{
				Name:  name,
				Kind:  SymbolKindClass,
				Range: nodeToRange(node),
			})
		}

	case "module":
		name := e.getChildByFieldName(node, "name", source)
		if name != "" && e.shouldInclude(name, SymbolKindModule, filter) {
			symbols = append(symbols, Symbol{
				Name:  name,
				Kind:  SymbolKindModule,
				Range: nodeToRange(node),
			})
		}
	}

	// Recurse
	for i := range node.ChildCount() {
		child := node.Child(int(i))
		if child != nil {
			childSymbols := e.extractRubySymbols(child, source, filter, depth+1)
			symbols = append(symbols, childSymbols...)
		}
	}

	return symbols
}

// extractGenericSymbols provides basic symbol extraction for unsupported languages.
func (e *SymbolExtractor) extractGenericSymbols(node *sitter.Node, source []byte, filter SymbolFilter, depth int) []Symbol {
	var symbols []Symbol

	nodeType := node.Type()

	// Common patterns across languages
	if strings.Contains(nodeType, "function") || strings.Contains(nodeType, "method") {
		name := e.getChildByFieldName(node, "name", source)
		if name == "" {
			// Try identifier child
			for i := range node.ChildCount() {
				child := node.Child(int(i))
				if child != nil && child.Type() == "identifier" {
					name = child.Content(source)
					break
				}
			}
		}
		kind := SymbolKindFunction
		if strings.Contains(nodeType, "method") {
			kind = SymbolKindMethod
		}
		if name != "" && e.shouldInclude(name, kind, filter) {
			symbols = append(symbols, Symbol{
				Name:  name,
				Kind:  kind,
				Range: nodeToRange(node),
			})
		}
	} else if strings.Contains(nodeType, "class") {
		name := e.getChildByFieldName(node, "name", source)
		if name != "" && e.shouldInclude(name, SymbolKindClass, filter) {
			symbols = append(symbols, Symbol{
				Name:  name,
				Kind:  SymbolKindClass,
				Range: nodeToRange(node),
			})
		}
	}

	// Recurse
	for i := range node.ChildCount() {
		child := node.Child(int(i))
		if child != nil {
			childSymbols := e.extractGenericSymbols(child, source, filter, depth+1)
			symbols = append(symbols, childSymbols...)
		}
	}

	return symbols
}

// Helper methods

//nolint:unparam // fieldName is intentionally generic for reuse across different AST node types
func (e *SymbolExtractor) getChildByFieldName(node *sitter.Node, fieldName string, source []byte) string {
	child := node.ChildByFieldName(fieldName)
	if child != nil {
		return child.Content(source)
	}
	return ""
}

func (e *SymbolExtractor) getChildNodeByFieldName(node *sitter.Node, fieldName string) *sitter.Node {
	return node.ChildByFieldName(fieldName)
}

func (e *SymbolExtractor) getIdentifierFromDeclarator(node *sitter.Node, source []byte) string {
	// C/C++ declarators can be nested (e.g., function_declarator -> identifier)
	if node.Type() == "identifier" {
		return node.Content(source)
	}
	for i := range node.ChildCount() {
		child := node.Child(int(i))
		if child != nil {
			if child.Type() == "identifier" {
				return child.Content(source)
			}
			// Recurse into declarator children
			if strings.Contains(child.Type(), "declarator") {
				if name := e.getIdentifierFromDeclarator(child, source); name != "" {
					return name
				}
			}
		}
	}
	return ""
}

func (e *SymbolExtractor) shouldInclude(name string, kind SymbolKind, filter SymbolFilter) bool {
	// Check if kind is in filter
	if len(filter.Kinds) > 0 {
		found := slices.Contains(filter.Kinds, kind)
		if !found {
			return false
		}
	}

	// Check private/unexported
	if !filter.IncludePrivate {
		// Go convention: lowercase first letter is unexported
		// Python convention: leading underscore is private
		if name != "" {
			first := rune(name[0])
			if first >= 'a' && first <= 'z' {
				return false
			}
			if name[0] == '_' {
				return false
			}
		}
	}

	return true
}

func (e *SymbolExtractor) getGoFunctionSignature(node *sitter.Node, source []byte) string {
	// Build signature from parameters and result
	var sig strings.Builder
	sig.WriteString("func ")

	name := e.getChildByFieldName(node, "name", source)
	sig.WriteString(name)

	params := node.ChildByFieldName("parameters")
	if params != nil {
		sig.WriteString(params.Content(source))
	}

	result := node.ChildByFieldName("result")
	if result != nil {
		sig.WriteString(" ")
		sig.WriteString(result.Content(source))
	}

	return sig.String()
}

func (e *SymbolExtractor) getGoMethodSignature(node *sitter.Node, source []byte) string {
	var sig strings.Builder
	sig.WriteString("func ")

	receiver := node.ChildByFieldName("receiver")
	if receiver != nil {
		sig.WriteString(receiver.Content(source))
		sig.WriteString(" ")
	}

	name := e.getChildByFieldName(node, "name", source)
	sig.WriteString(name)

	params := node.ChildByFieldName("parameters")
	if params != nil {
		sig.WriteString(params.Content(source))
	}

	result := node.ChildByFieldName("result")
	if result != nil {
		sig.WriteString(" ")
		sig.WriteString(result.Content(source))
	}

	return sig.String()
}

func (e *SymbolExtractor) getPythonFunctionSignature(node *sitter.Node, source []byte) string {
	var sig strings.Builder
	sig.WriteString("def ")

	name := e.getChildByFieldName(node, "name", source)
	sig.WriteString(name)

	params := node.ChildByFieldName("parameters")
	if params != nil {
		sig.WriteString(params.Content(source))
	}

	return sig.String()
}

func (e *SymbolExtractor) getPrecedingComment(node *sitter.Node, source []byte) string {
	// Look for comment sibling before this node
	// This is a simplified approach; proper implementation would check actual positions
	prev := node.PrevSibling()
	if prev != nil && prev.Type() == "comment" {
		return strings.TrimSpace(prev.Content(source))
	}
	return ""
}

func (e *SymbolExtractor) getPythonDocstring(node *sitter.Node, source []byte) string {
	// Python docstrings are the first statement in a function/class body
	body := node.ChildByFieldName("body")
	if body == nil {
		return ""
	}

	if body.ChildCount() > 0 {
		first := body.Child(0)
		if first != nil && first.Type() == "expression_statement" {
			if first.ChildCount() > 0 {
				expr := first.Child(0)
				if expr != nil && expr.Type() == "string" {
					docstring := expr.Content(source)
					// Remove quotes
					docstring = strings.Trim(docstring, "\"'")
					return strings.TrimSpace(docstring)
				}
			}
		}
	}
	return ""
}

func nodeToRange(node *sitter.Node) Range {
	return Range{
		StartLine:   int(node.StartPoint().Row),
		StartColumn: int(node.StartPoint().Column),
		EndLine:     int(node.EndPoint().Row),
		EndColumn:   int(node.EndPoint().Column),
	}
}
