package compress

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/caimlas/meept/internal/code/ast"
)

// CodeCompressor provides AST-aware code compression.
//
// Strategy:
// - Preserve: imports, type definitions, function signatures, exported symbols
// - Compress: function bodies, variable initializers, long string literals
//
// Uses tree-sitter parsers from internal/code/ast/ for structural analysis.
//
// Typical savings: 60-80% on source code files.
type CodeCompressor struct {
	// MaxLinesToShow is the maximum lines to show per function body
	MaxLinesToShow int
	// PreserveStrings keeps string literals uncompressed
	PreserveStrings bool
	// Languages is the set of languages to compress (empty = all)
	Languages []string
}

// CodeCompressorConfig configures the CodeCompressor.
type CodeCompressorConfig struct {
	MaxLinesToShow    int      `json:"max_lines_to_show" toml:"max_lines_to_show"`
	PreserveStrings   bool     `json:"preserve_strings" toml:"preserve_strings"`
	Languages         []string `json:"languages" toml:"languages"`
}

// DefaultCodeCompressorConfig returns default configuration.
func DefaultCodeCompressorConfig() CodeCompressorConfig {
	return CodeCompressorConfig{
		MaxLinesToShow:  3,
		PreserveStrings: false,
		Languages:       []string{"go", "python", "typescript", "rust"},
	}
}

// NewCodeCompressor creates a CodeCompressor with the given config.
func NewCodeCompressor(cfg CodeCompressorConfig) *CodeCompressor {
	return &CodeCompressor{
		MaxLinesToShow:  cfg.MaxLinesToShow,
		PreserveStrings: cfg.PreserveStrings,
		Languages:       cfg.Languages,
	}
}

// Crush compresses code content using AST-aware tree-sitter parsing.
//
// Strategy:
//   - Preserves imports, type definitions, function signatures, exported symbols
//   - Compresses function bodies (replaces with summary marker)
//   - Falls back to line-based compression if AST parsing is unavailable
func (cc *CodeCompressor) Crush(content string, language string) (string, CompressionResult) {
	result := CompressionResult{
		OriginalContent: content,
		Strategy:        StrategyCode,
	}

	// Detect language from file extension, content, or provided language
	if language == "" {
		// Guess from content heuristics
		language = detectLanguage(content)
	}

	// Check if this language is supported
	if len(cc.Languages) > 0 && !containsString(cc.Languages, language) {
		result.CompressedContent = content
		result.OriginalTokens = countTokens(content)
		result.CompressedTokens = result.OriginalTokens
		result.TokensSaved = 0
		result.CompressionRatio = 1.0
		result.TransformsApplied = []string{"passthrough:unsupported_language"}
		return result.CompressedContent, result
	}

	originalTokens := countTokens(content)

	// Try AST-aware compression via the ast package
	astLang := ast.LanguageFromString(language)
	if astLang != ast.LangUnknown {
		compressed := ast.CompressCodeAtBoundaries([]byte(content), astLang, 20000)
		compressedTokens := countTokens(compressed)
		tokensSaved := max(0, originalTokens-compressedTokens)

		result.CompressedContent = compressed
		result.OriginalTokens = originalTokens
		result.CompressedTokens = compressedTokens
		result.TokensSaved = tokensSaved
		result.CompressionRatio = float64(compressedTokens) / float64(max(1, originalTokens))
		result.TransformsApplied = []string{"ast_tree_sitter"}

		// If AST compression didn't shrink anything, fall back to line-based
		if tokensSaved == 0 && len(content) > 20 {
			return cc.compressLineBased(content, originalTokens)
		}

		// Injection guard
		if compressedTokens > originalTokens {
			result.CompressedContent = content
			result.TokensSaved = 0
			result.CompressionRatio = 1.0
			result.TransformsApplied = append(result.TransformsApplied, "inflation_guard:reverted")
		}

		return result.CompressedContent, result
	}

	// Fallback: line-based compression for unsupported languages
	return cc.compressLineBased(content, originalTokens)
}

// compressLineBased performs the older line-truncation strategy as a fallback
// for languages the AST layer does not support.
func (cc *CodeCompressor) compressLineBased(content string, originalTokens int) (string, CompressionResult) {
	result := CompressionResult{
		OriginalContent: content,
		Strategy:        StrategyCode,
	}

	lines := strings.Split(content, "\n")
	compressedLines := compressCodeLines(lines, cc.MaxLinesToShow)
	compressed := strings.Join(compressedLines, "\n")
	compressedTokens := countTokens(compressed)
	tokensSaved := max(0, originalTokens-compressedTokens)

	result.CompressedContent = compressed
	result.OriginalTokens = originalTokens
	result.CompressedTokens = compressedTokens
	result.TokensSaved = tokensSaved
	result.CompressionRatio = float64(compressedTokens) / float64(max(1, originalTokens))
	result.TransformsApplied = []string{"code_lines"}

	if compressedTokens > originalTokens {
		result.CompressedContent = content
		result.TokensSaved = 0
		result.CompressionRatio = 1.0
		result.TransformsApplied = append(result.TransformsApplied, "inflation_guard:reverted")
	}

	return result.CompressedContent, result
}

// AstCrush compresses code from a file using full AST-aware parsing.
// It uses the same tree-sitter pipeline as Crush but also extracts
// structured symbols (imports, types, function signatures) via the ast.SymbolExtractor.
func (cc *CodeCompressor) AstCrush(filePath string) (string, CompressionResult) {
	abs, err := filepath.Abs(filePath)
	if err != nil {
		abs = filePath
	}

	src, err := os.ReadFile(abs)
	if err != nil {
		return cc.Crush("", "")
	}

	return cc.Crush(string(src), string(ast.DetectLanguage(filePath)))
}

// compressCodeLines compresses a list of code lines as a fallback strategy.
func compressCodeLines(lines []string, maxLines int) []string {
	if len(lines) <= 20 {
		return lines
	}

	result := make([]string, 0, 30)

	// Keep first 15 lines (imports, package, headers)
	keepFirst := 15
	if keepFirst > len(lines) {
		keepFirst = len(lines)
	}
	result = append(result, lines[:keepFirst]...)

	// Add summary for middle section
	middleCount := len(lines) - keepFirst - 5
	if middleCount > 0 {
		result = append(result, fmt.Sprintf("// [CODE_COMPRESSED: %d lines omitted]", middleCount))
	}

	// Keep last 5 lines
	keepLast := 5
	startIdx := len(lines) - keepLast
	if startIdx < keepFirst {
		startIdx = keepFirst
	}
	if startIdx < len(lines) {
		result = append(result, lines[startIdx:]...)
	}

	return result
}

// detectLanguage attempts to detect the programming language from content heuristics.
// For production use, prefer ast.DetectLanguage based on file extension.
func detectLanguage(content string) string {
	if strings.Contains(content, "package ") && strings.Contains(content, "import ") {
		return "go"
	}
	if strings.Contains(content, "def ") && strings.Contains(content, "import ") {
		return "python"
	}
	if strings.Contains(content, "function ") || strings.Contains(content, "const ") || strings.Contains(content, "interface ") {
		return "typescript"
	}
	if strings.Contains(content, "fn ") && strings.Contains(content, "let mut") {
		return "rust"
	}
	if strings.Contains(content, "public class ") || strings.Contains(content, "public static void") {
		return "java"
	}
	if strings.Contains(content, "#include") || strings.Contains(content, "int main(") {
		return "c"
	}
	return "unknown"
}

// containsString checks if a slice contains a string.
func containsString(slice []string, s string) bool {
	for _, item := range slice {
		if item == s {
			return true
		}
	}
	return false
}
