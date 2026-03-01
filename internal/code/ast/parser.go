package ast

import (
	"context"
	"fmt"
	"os"
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
