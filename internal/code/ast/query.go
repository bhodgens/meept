package ast

import (
	"context"
	"fmt"
	"os"

	sitter "github.com/smacker/go-tree-sitter"
)

// QueryExecutor runs tree-sitter queries against parsed source.
type QueryExecutor struct {
	parser *ParserManager
}

// NewQueryExecutor creates a new query executor.
func NewQueryExecutor(parser *ParserManager) *QueryExecutor {
	return &QueryExecutor{parser: parser}
}

// RunQuery executes a tree-sitter S-expression query on source code.
func (q *QueryExecutor) RunQuery(ctx context.Context, source []byte, lang Language, queryPattern string) (*QueryResult, error) {
	if lang == LangUnknown {
		return nil, fmt.Errorf("unknown language")
	}

	grammar := GetLanguageGrammar(lang)
	if grammar == nil {
		return nil, fmt.Errorf("unsupported language: %s", lang)
	}

	tree, err := q.parser.GetTree(ctx, source, lang)
	if err != nil {
		return nil, fmt.Errorf("parse error: %w", err)
	}

	query, err := sitter.NewQuery([]byte(queryPattern), grammar)
	if err != nil {
		return nil, fmt.Errorf("invalid query: %w", err)
	}

	cursor := sitter.NewQueryCursor()
	cursor.Exec(query, tree.RootNode())

	result := &QueryResult{
		Query:   queryPattern,
		Matches: make([]QueryMatch, 0),
	}

	for {
		match, ok := cursor.NextMatch()
		if !ok {
			break
		}

		// Filter predicates (e.g., #match?, #eq?)
		match = cursor.FilterPredicates(match, source)

		qm := QueryMatch{
			PatternIndex: int(match.PatternIndex),
			Captures:     make([]QueryCapture, 0, len(match.Captures)),
		}

		for _, capture := range match.Captures {
			captureName := query.CaptureNameForId(capture.Index)
			qc := QueryCapture{
				Name: captureName,
				Node: convertNode(capture.Node, source, 2), // Limit depth for captures
			}
			qm.Captures = append(qm.Captures, qc)
		}

		result.Matches = append(result.Matches, qm)
		result.Count++
	}

	return result, nil
}

// RunQueryOnFile executes a tree-sitter query on a file.
func (q *QueryExecutor) RunQueryOnFile(ctx context.Context, filePath string, queryPattern string) (*QueryResult, error) {
	lang := DetectLanguage(filePath)
	if lang == LangUnknown {
		return nil, fmt.Errorf("could not detect language for: %s", filePath)
	}

	source, err := os.ReadFile(filePath)
	if err != nil {
		return nil, fmt.Errorf("failed to read file: %w", err)
	}

	return q.RunQuery(ctx, source, lang, queryPattern)
}

// RunQueryWithLanguage executes a query with an explicitly specified language.
func (q *QueryExecutor) RunQueryWithLanguage(ctx context.Context, filePath string, lang Language, queryPattern string) (*QueryResult, error) {
	source, err := os.ReadFile(filePath)
	if err != nil {
		return nil, fmt.Errorf("failed to read file: %w", err)
	}

	return q.RunQuery(ctx, source, lang, queryPattern)
}

// CommonQueries provides pre-built queries for common use cases.
var CommonQueries = map[string]map[Language]string{
	// Find all function definitions
	"functions": {
		LangGo:         "(function_declaration name: (identifier) @name)",
		LangPython:     "(function_definition name: (identifier) @name)",
		LangTypeScript: "(function_declaration name: (identifier) @name)",
		LangJavaScript: "(function_declaration name: (identifier) @name)",
		LangRust:       "(function_item name: (identifier) @name)",
		LangJava:       "(method_declaration name: (identifier) @name)",
		LangC:          "(function_definition declarator: (function_declarator declarator: (identifier) @name))",
		LangCpp:        "(function_definition declarator: (function_declarator declarator: (identifier) @name))",
		LangRuby:       "(method name: (identifier) @name)",
	},

	// Find all class definitions
	"classes": {
		LangGo:         "(type_declaration (type_spec name: (type_identifier) @name type: (struct_type)))",
		LangPython:     "(class_definition name: (identifier) @name)",
		LangTypeScript: "(class_declaration name: (type_identifier) @name)",
		LangJavaScript: "(class_declaration name: (identifier) @name)",
		LangRust:       "(struct_item name: (type_identifier) @name)",
		LangJava:       "(class_declaration name: (identifier) @name)",
		LangCpp:        "(class_specifier name: (type_identifier) @name)",
		LangRuby:       "(class name: (constant) @name)",
	},

	// Find all imports
	"imports": {
		LangGo:         "(import_declaration (import_spec path: (interpreted_string_literal) @path))",
		LangPython:     "[(import_statement name: (dotted_name) @name) (import_from_statement module_name: (dotted_name) @name)]",
		LangTypeScript: "(import_statement source: (string) @source)",
		LangJavaScript: "(import_statement source: (string) @source)",
		LangRust:       "(use_declaration argument: (_) @path)",
		LangJava:       "(import_declaration (scoped_identifier) @name)",
	},

	// Find all string literals
	"strings": {
		LangGo:         "[(interpreted_string_literal) @str (raw_string_literal) @str]",
		LangPython:     "(string) @str",
		LangTypeScript: "[(string) @str (template_string) @str]",
		LangJavaScript: "[(string) @str (template_string) @str]",
		LangRust:       "(string_literal) @str",
		LangJava:       "(string_literal) @str",
	},

	// Find all comments
	"comments": {
		LangGo:         "(comment) @comment",
		LangPython:     "(comment) @comment",
		LangTypeScript: "[(comment) @comment (hash_bang_line) @comment]",
		LangJavaScript: "[(comment) @comment (hash_bang_line) @comment]",
		LangRust:       "[(line_comment) @comment (block_comment) @comment]",
		LangJava:       "[(line_comment) @comment (block_comment) @comment]",
		LangC:          "(comment) @comment",
		LangCpp:        "(comment) @comment",
	},
}

// GetCommonQuery returns a pre-built query for a language.
func GetCommonQuery(queryName string, lang Language) (string, bool) {
	queries, ok := CommonQueries[queryName]
	if !ok {
		return "", false
	}
	query, ok := queries[lang]
	return query, ok
}
