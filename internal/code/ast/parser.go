package ast

import (
	"context"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	sitter "github.com/smacker/go-tree-sitter"
)

// ParserManager manages tree-sitter parsers for multiple languages.
// It provides thread-safe parsing with an optional parse cache.
type ParserManager struct {
	mu      sync.RWMutex
	parsers map[Language]*sitter.Parser
	cache   *ParseCache
}

// ParserConfig configures the parser manager.
type ParserConfig struct {
	// CacheEnabled enables parse result caching.
	CacheEnabled bool
	// CacheMaxSize is the maximum number of cached parse results.
	CacheMaxSize int
	// CacheTTL is how long cached results remain valid.
	CacheTTL time.Duration
}

// DefaultParserConfig returns sensible defaults.
func DefaultParserConfig() ParserConfig {
	return ParserConfig{
		CacheEnabled: true,
		CacheMaxSize: 100,
		CacheTTL:     5 * time.Minute,
	}
}

// NewParserManager creates a new parser manager.
func NewParserManager(config ParserConfig) *ParserManager {
	pm := &ParserManager{
		parsers: make(map[Language]*sitter.Parser),
	}
	if config.CacheEnabled {
		pm.cache = NewParseCache(config.CacheMaxSize, config.CacheTTL)
	}
	return pm
}

// getParser returns a parser for the given language, creating one if needed.
func (pm *ParserManager) getParser(lang Language) (*sitter.Parser, error) {
	pm.mu.RLock()
	parser, exists := pm.parsers[lang]
	pm.mu.RUnlock()

	if exists {
		return parser, nil
	}

	// Need to create new parser
	pm.mu.Lock()
	defer pm.mu.Unlock()

	// Double-check after acquiring write lock
	if parser, exists = pm.parsers[lang]; exists {
		return parser, nil
	}

	grammar := GetLanguageGrammar(lang)
	if grammar == nil {
		return nil, fmt.Errorf("unsupported language: %s", lang)
	}

	parser = sitter.NewParser()
	parser.SetLanguage(grammar)
	pm.parsers[lang] = parser

	return parser, nil
}

// Parse parses source code and returns the AST.
func (pm *ParserManager) Parse(ctx context.Context, source []byte, lang Language) (*ParseResult, error) {
	if lang == LangUnknown {
		return nil, fmt.Errorf("unknown language")
	}

	parser, err := pm.getParser(lang)
	if err != nil {
		return nil, err
	}

	tree, err := parser.ParseCtx(ctx, nil, source)
	if err != nil {
		return nil, fmt.Errorf("parse error: %w", err)
	}

	result := &ParseResult{
		Language: lang,
		RootNode: convertNode(tree.RootNode(), source, 0), // depth limit 0 = full tree
	}

	// Collect errors from the tree
	if tree.RootNode().HasError() {
		result.Errors = collectErrors(tree.RootNode(), source)
	}

	return result, nil
}

// ParseFile parses a file and returns the AST.
func (pm *ParserManager) ParseFile(ctx context.Context, filePath string) (*ParseResult, error) {
	lang := DetectLanguage(filePath)
	if lang == LangUnknown {
		return nil, fmt.Errorf("could not detect language for: %s", filePath)
	}

	// Check cache first
	if pm.cache != nil {
		if cached := pm.cache.Get(filePath); cached != nil {
			return cached, nil
		}
	}

	source, err := os.ReadFile(filePath)
	if err != nil {
		return nil, fmt.Errorf("failed to read file: %w", err)
	}

	result, err := pm.Parse(ctx, source, lang)
	if err != nil {
		return nil, err
	}
	result.FilePath = filePath

	// Store in cache
	if pm.cache != nil {
		pm.cache.Put(filePath, result)
	}

	return result, nil
}

// ParseFileWithLanguage parses a file with an explicitly specified language.
func (pm *ParserManager) ParseFileWithLanguage(ctx context.Context, filePath string, lang Language) (*ParseResult, error) {
	if lang == LangUnknown {
		return nil, fmt.Errorf("unknown language")
	}

	source, err := os.ReadFile(filePath)
	if err != nil {
		return nil, fmt.Errorf("failed to read file: %w", err)
	}

	result, err := pm.Parse(ctx, source, lang)
	if err != nil {
		return nil, err
	}
	result.FilePath = filePath

	return result, nil
}

// InvalidateCache removes a file from the cache.
func (pm *ParserManager) InvalidateCache(filePath string) {
	if pm.cache != nil {
		pm.cache.Invalidate(filePath)
	}
}

// ClearCache removes all entries from the cache.
func (pm *ParserManager) ClearCache() {
	if pm.cache != nil {
		pm.cache.Clear()
	}
}

// convertNode converts a tree-sitter node to our Node type.
// depthLimit of 0 means no limit.
func convertNode(n *sitter.Node, source []byte, depthLimit int) Node {
	node := Node{
		Type:    n.Type(),
		IsNamed: n.IsNamed(),
		Range: Range{
			StartLine:   int(n.StartPoint().Row),
			StartColumn: int(n.StartPoint().Column),
			EndLine:     int(n.EndPoint().Row),
			EndColumn:   int(n.EndPoint().Column),
		},
	}

	// Only include text for leaf nodes or small nodes
	if n.ChildCount() == 0 || (n.EndByte()-n.StartByte()) < 200 {
		node.Text = n.Content(source)
	}

	// Convert children if within depth limit
	if depthLimit != 1 && n.ChildCount() > 0 {
		newLimit := depthLimit
		if depthLimit > 0 {
			newLimit--
		}

		children := make([]Node, 0, n.ChildCount())
		for i := uint32(0); i < n.ChildCount(); i++ {
			child := n.Child(int(i))
			if child != nil {
				children = append(children, convertNode(child, source, newLimit))
			}
		}
		node.Children = children
	}

	return node
}

// collectErrors finds error nodes in the tree.
func collectErrors(n *sitter.Node, source []byte) []string {
	var errors []string

	var walk func(*sitter.Node)
	walk = func(node *sitter.Node) {
		if node == nil {
			return
		}

		if node.IsError() || node.IsMissing() {
			startLine := node.StartPoint().Row + 1
			startCol := node.StartPoint().Column + 1
			text := ""
			if node.EndByte()-node.StartByte() < 50 {
				text = fmt.Sprintf(": %q", node.Content(source))
			}
			errors = append(errors, fmt.Sprintf("line %d, col %d%s", startLine, startCol, text))
		}

		for i := uint32(0); i < node.ChildCount(); i++ {
			walk(node.Child(int(i)))
		}
	}

	walk(n)
	return errors
}

// GetTree parses source and returns the raw tree-sitter tree.
// This is useful for running queries against the tree.
func (pm *ParserManager) GetTree(ctx context.Context, source []byte, lang Language) (*sitter.Tree, error) {
	if lang == LangUnknown {
		return nil, fmt.Errorf("unknown language")
	}

	parser, err := pm.getParser(lang)
	if err != nil {
		return nil, err
	}

	return parser.ParseCtx(ctx, nil, source)
}

// CompressCodeAtBoundaries compresses source code by keeping structural elements
// (function/method signatures, type definitions, imports, package/module declarations)
// while truncating function bodies. It returns the compressed code string with a
// "...[compressed]" marker where bodies were removed.
//
// If the compressed output still exceeds maxChars, it falls back to simple truncation.
// If parsing fails, it returns the original source truncated to maxChars with a marker.
func CompressCodeAtBoundaries(source []byte, lang Language, maxChars int) string {
	if len(source) <= maxChars {
		return string(source)
	}
	if lang == LangUnknown || maxChars <= 0 {
		return truncateByteFallback(source, maxChars)
	}

	grammar := GetLanguageGrammar(lang)
	if grammar == nil {
		return truncateByteFallback(source, maxChars)
	}

	parser := sitter.NewParser()
	parser.SetLanguage(grammar)

	tree, err := parser.ParseCtx(context.Background(), nil, source)
	if err != nil || tree == nil {
		return truncateByteFallback(source, maxChars)
	}
	defer tree.Close()

	root := tree.RootNode()
	if root.HasError() && root.ChildCount() == 0 {
		return truncateByteFallback(source, maxChars)
	}

	// Collect ranges of "body" nodes to exclude (function bodies, method bodies)
	bodyRanges := collectBodyRanges(root, source, lang)

	// Reconstruct the source, skipping body ranges
	var buf strings.Builder
	prevEnd := 0
	removed := 0

	for _, br := range bodyRanges {
		// Write everything before this body range
		if br.start > prevEnd {
			buf.Write(source[prevEnd:br.start])
		}
		removed += br.end - br.start
		buf.WriteString(" { ...[compressed] }")
		prevEnd = br.end
	}

	// Write remaining content after last body range
	if prevEnd < len(source) {
		buf.Write(source[prevEnd:])
	}

	result := buf.String()

	// If still too large, fall back to truncation
	if len(result) > maxChars {
		return truncateStrFallback(result, maxChars)
	}

	return result
}

// bodyRange represents a byte range in the source to be compressed.
type bodyRange struct {
	start int
	end   int
}

// collectBodyRanges finds function/method body ranges in the AST that should
// be compressed. It prioritizes bodies from the end of the file so that
// early code (signatures, types, imports) is preserved.
func collectBodyRanges(root *sitter.Node, source []byte, lang Language) []bodyRange {
	var ranges []bodyRange

	var walk func(*sitter.Node)
	walk = func(node *sitter.Node) {
		if node == nil {
			return
		}

		if isBodyHolder(node, lang) {
			body := node.ChildByFieldName("body")
			if body != nil && body.EndByte()-body.StartByte() > 0 {
				// Include the braces/brackets of the body
				start := body.StartByte()
				end := body.EndByte()
				// Extend to include closing brace on same line
				if end <= uint32(len(source)) {
					ranges = append(ranges, bodyRange{start: int(start), end: int(end)})
				}
				// Don't recurse into the body itself
				return
			}
			// For Go specifically, check for "block" child (function body)
			if lang == LangGo {
				block := findChildByType(node, "block")
				if block != nil && block.EndByte()-block.StartByte() > 2 {
					ranges = append(ranges, bodyRange{start: int(block.StartByte()), end: int(block.EndByte())})
					return
				}
			}
		}

		for i := uint32(0); i < node.ChildCount(); i++ {
			child := node.Child(int(i))
			if child != nil {
				walk(child)
			}
		}
	}

	walk(root)

	return ranges
}

// isBodyHolder checks if a node type typically contains a body that can be compressed.
func isBodyHolder(node *sitter.Node, lang Language) bool {
	t := node.Type()
	switch lang {
	case LangGo:
		return t == "function_declaration" || t == "method_declaration" || t == "func_literal"
	case LangPython:
		return t == "function_definition" || t == "class_definition"
	case LangTypeScript, LangJavaScript:
		return t == "function_declaration" || t == "class_declaration" || t == "method_definition" || t == "arrow_function" || t == "function_expression"
	case LangRust:
		return t == "function_item" || t == "impl_item"
	case LangJava:
		return t == "method_declaration" || t == "class_declaration" || t == "constructor_declaration"
	case LangC, LangCpp:
		return t == "function_definition" || t == "class_specifier" || t == "struct_specifier"
	case LangRuby:
		return t == "method" || t == "class" || t == "module"
	default:
		return false
	}
}

// findChildByType finds the first direct child with the given type.
func findChildByType(node *sitter.Node, childType string) *sitter.Node {
	for i := uint32(0); i < node.ChildCount(); i++ {
		child := node.Child(int(i))
		if child != nil && child.Type() == childType {
			return child
		}
	}
	return nil
}

// truncateByteFallback truncates byte content to maxChars with a marker.
func truncateByteFallback(source []byte, maxChars int) string {
	if maxChars <= 0 {
		return ""
	}
	if len(source) <= maxChars {
		return string(source)
	}
	const marker = "\n...[truncated]"
	if maxChars <= len(marker) {
		return string(source[:maxChars])
	}
	return string(source[:maxChars-len(marker)]) + marker
}

// truncateStrFallback truncates a string to maxChars with a marker.
func truncateStrFallback(s string, maxChars int) string {
	if maxChars <= 0 {
		return ""
	}
	if len(s) <= maxChars {
		return s
	}
	const marker = "\n...[truncated]"
	if maxChars <= len(marker) {
		return s[:maxChars]
	}
	return s[:maxChars-len(marker)] + marker
}
