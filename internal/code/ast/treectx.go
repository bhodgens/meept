package ast

import (
	"context"
	"os"
	"sort"
	"strings"

	sitter "github.com/smacker/go-tree-sitter"
)

// TreeContextOptions configures tree context generation.
type TreeContextOptions struct {
	// ShowSignatures includes function/method signatures in output.
	ShowSignatures bool
	// ShowIndent includes indentation-based hierarchy.
	ShowIndent bool
	// MaxLines limits the maximum number of lines in output.
	MaxLines int
}

// DefaultTreeContextOptions returns sensible defaults.
func DefaultTreeContextOptions() TreeContextOptions {
	return TreeContextOptions{
		ShowSignatures: true,
		ShowIndent:     true,
		MaxLines:       100,
	}
}

// TreeContextResult contains the generated tree context.
type TreeContextResult struct {
	String      string
	MarkedLines []int
	Scopes      []TreeScope
	Language    Language
	FilePath    string
	TotalLines  int
}

// TreeScope represents a structural scope in the code.
type TreeScope struct {
	Line     int
	Indent   int
	Name     string
	Kind     string
	Type     string // "function", "class", "method", etc.
	FullName string // Full signature for functions
}

// TreeContextWithMarkersSimple generates tree-aware context for a file with error markers.
// This is a simpler version that returns just the context string.
// It shows code structure (functions, classes) surrounding the marked lines, and marks
// the error locations with a █ symbol for easy identification by LLMs.
// Padding specifies how many lines of context to show around each marked line.
func TreeContextWithMarkersSimple(filePath string, markedLines map[int]bool, padding int) string {
	result, err := TreeContextWithMarkers(filePath, markedLines, padding)
	if err != nil || result == nil {
		return ""
	}
	return result.String
}

// TreeContextWithMarkers generates tree-aware context for a file with error markers.
// It shows code structure (functions, classes) surrounding the marked lines, and marks
// the error locations with a special character for easy identification by LLMs.
// If opts is not provided, defaults are used.
func TreeContextWithMarkers(filePath string, markedLines map[int]bool, padding int, opts ...TreeContextOptions) (*TreeContextResult, error) {
	if padding < 1 {
		padding = 3
	}

	// Use defaults if opts not provided
	var cfg TreeContextOptions
	if len(opts) > 0 {
		cfg = opts[0]
	} else {
		cfg = DefaultTreeContextOptions()
	}

	// Detect language
	lang := DetectLanguage(filePath)
	if lang == LangUnknown {
		return nil, nil
	}

	// Read file content
	source, err := os.ReadFile(filePath)
	if err != nil {
		return nil, err
	}

	// Parse the file
	grammar := GetLanguageGrammar(lang)
	if grammar == nil {
		return nil, nil
	}

	parser := sitter.NewParser()
	parser.SetLanguage(grammar)

	tree, err := parser.ParseCtx(context.Background(), nil, source)
	if err != nil {
		return nil, err
	}
	defer tree.Close()

	// Calculate context lines from marked lines with padding
	ctxLines := collectContextLines(markedLines, padding, countLines(string(source)))

	// Build tree-aware context using the raw tree
	root := tree.RootNode()
	scopes := collectScopes(root, source, lang)

	// Build the context string with markers
	var sb strings.Builder
	addedScopes := make(map[int]bool)

	for _, line := range ctxLines {
		// Find and add structural context for this line
		for _, scope := range scopes {
			if scope.Line <= line && !addedScopes[scope.Line] {
				// Check if this scope should be shown (before the marked line)
				if scope.Line <= line && (len(ctxLines) < 10 || scope.Line >= ctxLines[0]-2) {
					addedScopes[scope.Line] = true

					if cfg.ShowIndent {
						indent := strings.Repeat("  ", scope.Indent)
						sb.WriteString(indent)
					}
					if cfg.ShowSignatures {
						sb.WriteString(scope.Name)
						if scope.Type == "function" || scope.Type == "method" {
							sb.WriteString(scope.FullName)
						}
					}
					sb.WriteString("\n")
				}
			}
		}

		// Mark error lines with █ symbol
		if markedLines[line] {
			srcLine := getLine(source, line)
			if cfg.ShowIndent {
				indent := strings.Repeat("  ", guessIndent(source, line))
				sb.WriteString(indent)
			}
			sb.WriteString("█ ")
			sb.WriteString(srcLine)
			sb.WriteString("\n")
		}
	}

	// Collect marked line numbers for result
	marked := make([]int, 0, len(markedLines))
	for line := range markedLines {
		marked = append(marked, line)
	}
	sort.Ints(marked)

	return &TreeContextResult{
		String:      sb.String(),
		MarkedLines: marked,
		Scopes:      scopes,
		Language:    lang,
		FilePath:    filePath,
		TotalLines:  countLines(string(source)),
	}, nil
}

// collectContextLines calculates which lines to include in context.
func collectContextLines(markedLines map[int]bool, padding, totalLines int) []int {
	lineSet := make(map[int]bool)

	for line := range markedLines {
		for i := line - padding; i <= line+padding; i++ {
			if i >= 0 && i < totalLines {
				lineSet[i] = true
			}
		}
	}

	lines := make([]int, 0, len(lineSet))
	for line := range lineSet {
		lines = append(lines, line)
	}
	sort.Ints(lines)
	return lines
}

// collectScopes walks the tree and collects structural scopes (functions, classes, etc.)
func collectScopes(root *sitter.Node, source []byte, lang Language) []TreeScope {
	var scopes []TreeScope

	var walk func(node *sitter.Node, indent int)
	walk = func(node *sitter.Node, indent int) {
		if node == nil || !node.IsNamed() {
			return
		}

		nodeType := node.Type()
		startLine := int(node.StartPoint().Row)

		// Check if this is a scope-defining node
		if scopeInfo := getScopeInfo(nodeType, lang); scopeInfo != nil {
			name := getScopeName(node, source, scopeInfo.Kind)
			fullName := getScopeFullName(node, source, lang, scopeInfo.Kind)

			scopes = append(scopes, TreeScope{
				Line:     startLine,
				Indent:   indent,
				Name:     name,
				Kind:     scopeInfo.Kind,
				Type:     scopeInfo.NodeType,
				FullName: fullName,
			})
			indent++
		}

		// Walk children
		for i := range node.ChildCount() {
			child := node.Child(int(i))
			if child != nil {
				walk(child, indent)
			}
		}
	}

	walk(root, 0)
	return scopes
}

// scopeInfo holds information about scope-defining nodes.
type scopeInfo struct {
	NodeType string // tree-sitter node type
	Kind     string // "function", "class", "method", "block"
}

var scopeKinds = map[string]map[string]*scopeInfo{
	"go": {
		"function_declaration":  {NodeType: "function_declaration", Kind: "function"},
		"method_declaration":    {NodeType: "method_declaration", Kind: "method"},
		"type_declaration":      {NodeType: "type_declaration", Kind: "type"},
		"interface_declaration": {NodeType: "interface_declaration", Kind: "interface"},
		"struct_declaration":    {NodeType: "struct_declaration", Kind: "struct"},
		"block":                 {NodeType: "block", Kind: "block"},
	},
	"python": {
		"function_definition": {NodeType: "function_definition", Kind: "function"},
		"class_definition":    {NodeType: "class_definition", Kind: "class"},
	},
	"typescript": {
		"function_declaration":  {NodeType: "function_declaration", Kind: "function"},
		"method_definition":     {NodeType: "method_definition", Kind: "method"},
		"class_declaration":     {NodeType: "class_declaration", Kind: "class"},
		"interface_declaration": {NodeType: "interface_declaration", Kind: "interface"},
	},
	"rust": {
		"function_item": {NodeType: "function_item", Kind: "function"},
		"impl_item":     {NodeType: "impl_item", Kind: "impl"},
		"struct_item":   {NodeType: "struct_item", Kind: "struct"},
		"enum_item":     {NodeType: "enum_item", Kind: "enum"},
	},
}

// getScopeInfo returns scope info for a node type and language.
func getScopeInfo(nodeType string, lang Language) *scopeInfo {
	if langScopeKinds, ok := scopeKinds[string(lang)]; ok {
		if info, ok := langScopeKinds[nodeType]; ok {
			return info
		}
	}
	return nil
}

// getScopeName extracts the name from a scope-defining node.
func getScopeName(node *sitter.Node, source []byte, kind string) string {
	switch kind {
	case "function", "method":
		// Try to get identifier
		for i := range node.ChildCount() {
			child := node.Child(int(i))
			if child != nil && (child.Type() == "identifier" || child.Type() == "field_identifier") {
				return "fn " + child.Content(source) + "()"
			}
		}
		return "fn <anonymous>"
	case "class", "struct", "type", "interface", "impl":
		for i := range node.ChildCount() {
			child := node.Child(int(i))
			if child != nil && (child.Type() == "type_identifier" || child.Type() == "identifier") {
				return kind + " " + child.Content(source)
			}
		}
		return kind + " <anonymous>"
	case "block":
		return "{...}"
	default:
		return node.Type()
	}
}

// getScopeFullName extracts the full signature for functions/methods.
func getScopeFullName(node *sitter.Node, source []byte, lang Language, kind string) string {
	if kind != "function" && kind != "method" {
		return ""
	}

	// Get the text content of the node (which includes signature)
	content := node.Content(source)

	// Truncate to reasonable signature length
	if len(content) > 60 {
		// Find the opening brace
		braceIdx := strings.Index(content, "{")
		if braceIdx > 0 && braceIdx < 60 {
			content = content[:braceIdx]
		} else {
			content = content[:60] + "..."
		}
	}

	return ": " + content
}

// countLines counts the number of lines in a string.
func countLines(s string) int {
	if s == "" {
		return 0
	}
	lines := 0
	for _, c := range s {
		if c == '\n' {
			lines++
		}
	}
	// Handle last line without newline
	if !strings.HasSuffix(s, "\n") && len(s) > 0 {
		lines++
	}
	return lines
}

// getLine returns a specific line (0-indexed) from source.
func getLine(source []byte, lineNum int) string {
	lines := strings.Split(string(source), "\n")
	if lineNum < 0 || lineNum >= len(lines) {
		return ""
	}
	return lines[lineNum]
}

// guessIndent attempts to guess the indentation level for a line.
func guessIndent(source []byte, lineNum int) int {
	line := getLine(source, lineNum)
	indent := 0
	for _, c := range line {
		if c == ' ' {
			indent++
		} else if c == '\t' {
			indent += 4
		} else {
			break
		}
	}
	// Return indent in "levels" (assuming 2 spaces per level)
	return indent / 2
}
