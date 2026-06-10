// Package repomap provides repository mapping with graph-based symbol ranking.
// It extracts symbol definitions and references via tree-sitter, builds a dependency
// graph, and applies Personalized PageRank to identify the most relevant symbols
// for the current conversation.
package repomap

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"sync"

	"github.com/caimlas/meept/internal/code/ast"
	"github.com/caimlas/meept/internal/repomap/queries"
)

// Tag represents a code symbol (definition or reference)
type Tag struct {
	RelFname string // Relative file path
	FName    string // Absolute file path
	Line     int    // Line number (0-based)
	Name     string // Symbol name
	Kind     string // "function", "class", "variable", "type", "method", "constant", "property"
	IsDef    bool   // true=definition, false=reference
}

// TagCacheEntry represents a cached tag entry
type cacheEntry struct {
	Mtime int64  // File modification time
	Data  []byte // JSON-encoded tags
}

// TagExtractor handles symbol extraction from source files using tree-sitter.
type TagExtractor struct {
	parser *ast.ParserManager
	logger *slog.Logger
}

// NewTagExtractor creates a new tag extractor.
func NewTagExtractor(logger *slog.Logger) *TagExtractor {
	return &TagExtractor{
		parser: ast.NewParserManager(ast.DefaultParserConfig()),
		logger: logger,
	}
}

// extractDefinitions extracts symbol definitions from a parsed tree using the
// language-specific query file.
func (e *TagExtractor) extractDefinitions(ctx context.Context, source []byte, lang ast.Language, filePath string) ([]Tag, error) {
	query, err := queries.LoadQuery(string(lang), "definitions.scm")
	if err != nil {
		return nil, err
	}

	executor := ast.NewQueryExecutor(e.parser)
	result, err := executor.RunQuery(ctx, source, lang, query)
	if err != nil {
		return nil, fmt.Errorf("query error for definitions: %w", err)
	}

	var tags []Tag
	for _, match := range result.Matches {
		for _, capture := range match.Captures {
			// Only capture nodes marked as definitions
			if strings.Contains(capture.Name, ".definition") || strings.HasPrefix(capture.Name, "name.") {
				kind := extractKind(capture.Name)
				tags = append(tags, Tag{
					FName:    filePath,
					RelFname: filepath.Base(filePath),
					Line:     capture.Node.Range.StartLine,
					Name:     capture.Node.Text,
					Kind:     kind,
					IsDef:    true,
				})
			}
		}
	}

	return tags, nil
}

// extractReferences extracts symbol references from a parsed tree.
func (e *TagExtractor) extractReferences(ctx context.Context, source []byte, lang ast.Language, filePath string) ([]Tag, error) {
	query, err := queries.LoadQuery(string(lang), "references.scm")
	if err != nil {
		// If no reference query exists, fall back to extracting all identifiers
		return e.extractReferencesFallback(ctx, source, lang, filePath)
	}

	executor := ast.NewQueryExecutor(e.parser)
	result, err := executor.RunQuery(ctx, source, lang, query)
	if err != nil {
		return e.extractReferencesFallback(ctx, source, lang, filePath)
	}

	var tags []Tag
	seen := make(map[string]bool)

	for _, match := range result.Matches {
		for _, capture := range match.Captures {
			if strings.Contains(capture.Name, ".reference") || strings.HasPrefix(capture.Name, "ref.") {
				key := fmt.Sprintf("%s:%d:%s", filePath, capture.Node.Range.StartLine, capture.Node.Text)
				if seen[key] {
					continue
				}
				seen[key] = true

				kind := extractKind(capture.Name)
				tags = append(tags, Tag{
					FName:    filePath,
					RelFname: filepath.Base(filePath),
					Line:     capture.Node.Range.StartLine,
					Name:     capture.Node.Text,
					Kind:     kind,
					IsDef:    false,
				})
			}
		}
	}

	return tags, nil
}

// extractReferencesFallback extracts references using a simple identifier scan
// when tree-sitter queries are unavailable.
func (e *TagExtractor) extractReferencesFallback(ctx context.Context, source []byte, lang ast.Language, filePath string) ([]Tag, error) {
	// Use simple regex-based fallback for extracting identifiers
	// This is less accurate but works across all languages
	identifierPattern := `[a-zA-Z_][a-zA-Z0-9_]*`
	re := regexp.MustCompile(identifierPattern)

	// Get excluded keywords for the language
	excluded := getExcludedKeywords(lang)

	lines := strings.Split(string(source), "\n")
	var tags []Tag
	seen := make(map[string]bool)

	for lineNum, line := range lines {
		matches := re.FindAllStringIndex(line, -1)
		for _, match := range matches {
			name := line[match[0]:match[1]]
			// Skip excluded keywords and single-character identifiers
			if len(name) < 2 {
				continue
			}
			if slices.Contains(excluded, name) {
				continue
			}

			key := fmt.Sprintf("%s:%d:%s", filePath, lineNum, name)
			if seen[key] {
				continue
			}
			seen[key] = true

			tags = append(tags, Tag{
				FName:    filePath,
				RelFname: filepath.Base(filePath),
				Line:     lineNum,
				Name:     name,
				Kind:     "variable",
				IsDef:    false,
			})
		}
	}

	return tags, nil
}

// extractKind extracts the symbol kind from the capture name.
func extractKind(captureName string) string {
	if strings.Contains(captureName, "function") {
		return "function"
	}
	if strings.Contains(captureName, "method") {
		return "method"
	}
	if strings.Contains(captureName, "class") {
		return "class"
	}
	if strings.Contains(captureName, "struct") || strings.Contains(captureName, "type") {
		return "type"
	}
	if strings.Contains(captureName, "constant") {
		return "constant"
	}
	if strings.Contains(captureName, "variable") || strings.Contains(captureName, "identifier") {
		return "variable"
	}
	if strings.Contains(captureName, "property") || strings.Contains(captureName, "field") {
		return "property"
	}
	return "variable"
}

// ExtractTags parses a single file and returns its tags (both definitions and references).
func (e *TagExtractor) ExtractTags(filePath string) ([]Tag, error) {
	return e.ExtractTagsWithContext(context.Background(), filePath)
}

// ExtractTagsWithContext parses a single file and returns its tags with context.
func (e *TagExtractor) ExtractTagsWithContext(ctx context.Context, filePath string) ([]Tag, error) {
	lang := ast.DetectLanguage(filePath)
	if lang == ast.LangUnknown {
		return nil, fmt.Errorf("could not detect language for: %s", filePath)
	}

	source, err := os.ReadFile(filePath)
	if err != nil {
		return nil, fmt.Errorf("failed to read file: %w", err)
	}

	// Run definitions and references extraction in parallel
	var defs, refs []Tag
	var defErr, refErr error
	var wg sync.WaitGroup

	wg.Add(2)
	go func() {
		defer wg.Done()
		defs, defErr = e.extractDefinitions(ctx, source, lang, filePath)
	}()

	go func() {
		defer wg.Done()
		refs, refErr = e.extractReferences(ctx, source, lang, filePath)
	}()

	wg.Wait()

	if defErr != nil && e.logger != nil {
		e.logger.Debug("definitions extraction failed", "file", filePath, "error", defErr)
	}
	if refErr != nil && e.logger != nil {
		e.logger.Debug("references extraction failed", "file", filePath, "error", refErr)
	}

	// Combine and deduplicate tags
	allTags := append(defs, refs...)
	allTags = deduplicateTags(allTags)

	return allTags, nil
}

// ExtractTagsRaw is the main entry point for batch extraction.
// It extracts all tags from the given list of files.
func (e *TagExtractor) ExtractTagsRaw(files []string) ([]Tag, error) {
	return e.ExtractTagsRawWithContext(context.Background(), files)
}

// ExtractTagsRawWithContext extracts tags from multiple files in parallel.
func (e *TagExtractor) ExtractTagsRawWithContext(ctx context.Context, files []string) ([]Tag, error) {
	if len(files) == 0 {
		return nil, nil
	}

	type result struct {
		tags []Tag
		err  error
	}

	results := make([]result, len(files))
	var wg sync.WaitGroup

	for i, file := range files {
		wg.Add(1)
		go func(idx int, path string) {
			defer wg.Done()
			tags, err := e.ExtractTagsWithContext(ctx, path)
			results[idx] = result{tags: tags, err: err}
		}(i, file)
	}

	wg.Wait()

	var allTags []Tag
	var errs []error

	for _, res := range results {
		if res.err != nil {
			errs = append(errs, res.err)
			continue
		}
		allTags = append(allTags, res.tags...)
	}

	if len(errs) > 0 && e.logger != nil {
		e.logger.Debug("some files failed to extract", "errors", len(errs))
	}

	// Deduplicate the combined tags
	allTags = deduplicateTags(allTags)

	return allTags, nil
}

// deduplicateTags removes duplicate tags based on file, line, and name.
func deduplicateTags(tags []Tag) []Tag {
	seen := make(map[string]bool)
	var unique []Tag

	for _, tag := range tags {
		key := fmt.Sprintf("%s:%d:%s:%v", tag.FName, tag.Line, tag.Name, tag.IsDef)
		if !seen[key] {
			seen[key] = true
			unique = append(unique, tag)
		}
	}

	return unique
}

// getExcludedKeywords returns language-specific keywords to exclude from reference extraction.
func getExcludedKeywords(lang ast.Language) []string {
	switch lang {
	case ast.LangGo:
		return []string{"break", "case", "chan", "const", "continue", "default", "defer",
			"else", "fallthrough", "for", "func", "go", "goto", "if", "import",
			"interface", "map", "package", "range", "return", "select", "struct",
			"switch", "type", "var", "true", "false", "nil", "iota", "append",
			"cap", "close", "complex", "copy", "delete", "imag", "len", "make",
			"new", "panic", "print", "println", "real", "recover"}
	case ast.LangPython:
		return []string{"False", "None", "True", "and", "as", "assert", "async", "await",
			"break", "class", "continue", "def", "del", "elif", "else", "except",
			"finally", "for", "from", "global", "if", "import", "in", "is",
			"lambda", "nonlocal", "not", "or", "pass", "raise", "return", "try",
			"while", "with", "yield", "self", "print"}
	case ast.LangTypeScript, ast.LangJavaScript:
		return []string{"break", "case", "catch", "class", "const", "continue", "debugger",
			"default", "delete", "do", "else", "export", "extends", "finally",
			"for", "function", "if", "import", "in", "instanceof", "new", "return",
			"super", "switch", "this", "throw", "try", "typeof", "var", "void",
			"while", "with", "yield", "async", "await", "static", "get", "set",
			"true", "false", "null", "undefined", "NaN", "Infinity"}
	case ast.LangRust:
		return []string{"as", "async", "await", "break", "const", "continue", "crate", "dyn",
			"else", "enum", "extern", "false", "fn", "for", "if", "impl", "in",
			"let", "loop", "match", "mod", "move", "mut", "pub", "ref", "return",
			"self", "Self", "static", "struct", "super", "trait", "true", "type",
			"unsafe", "use", "where", "while"}
	case ast.LangJava:
		return []string{"abstract", "assert", "boolean", "break", "byte", "case", "catch",
			"char", "class", "const", "continue", "default", "do", "double", "else",
			"enum", "extends", "final", "finally", "float", "for", "goto", "if",
			"implements", "import", "instanceof", "int", "interface", "long", "native",
			"new", "package", "private", "protected", "public", "return", "short",
			"static", "strictfp", "super", "switch", "synchronized", "this", "throw",
			"throws", "transient", "try", "void", "volatile", "while", "true", "false", "null"}
	default:
		return []string{}
	}
}

// FindDefinitions finds all definition tags matching a given name across all tags.
func FindDefinitions(tags []Tag, name string) []Tag {
	var defs []Tag
	for _, tag := range tags {
		if tag.IsDef && tag.Name == name {
			defs = append(defs, tag)
		}
	}
	return defs
}

// FindReferences finds all reference tags matching a given name across all tags.
func FindReferences(tags []Tag, name string) []Tag {
	var refs []Tag
	for _, tag := range tags {
		if !tag.IsDef && tag.Name == name {
			refs = append(refs, tag)
		}
	}
	return refs
}

// GetDefinitionsByFile groups definitions by file.
func GetDefinitionsByFile(tags []Tag) map[string][]Tag {
	result := make(map[string][]Tag)
	for _, tag := range tags {
		if tag.IsDef {
			result[tag.RelFname] = append(result[tag.RelFname], tag)
		}
	}
	return result
}

// GetReferencesByFile groups references by file.
func GetReferencesByFile(tags []Tag) map[string][]Tag {
	result := make(map[string][]Tag)
	for _, tag := range tags {
		if !tag.IsDef {
			result[tag.RelFname] = append(result[tag.RelFname], tag)
		}
	}
	return result
}

// FilterTagsByKind filters tags by their kind.
func FilterTagsByKind(tags []Tag, kinds ...string) []Tag {
	var filtered []Tag
	for _, tag := range tags {
		for _, kind := range kinds {
			if tag.Kind == kind {
				filtered = append(filtered, tag)
				break
			}
		}
	}
	return filtered
}

// FilterDefinitions filters to only return definition tags.
func FilterDefinitions(tags []Tag) []Tag {
	var defs []Tag
	for _, tag := range tags {
		if tag.IsDef {
			defs = append(defs, tag)
		}
	}
	return defs
}

// FilterReferences filters to only return reference tags.
func FilterReferences(tags []Tag) []Tag {
	var refs []Tag
	for _, tag := range tags {
		if !tag.IsDef {
			refs = append(refs, tag)
		}
	}
	return refs
}
