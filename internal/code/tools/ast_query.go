package tools

import (
	"context"
	"fmt"

	"github.com/caimlas/meept/internal/code/ast"
	"github.com/caimlas/meept/internal/llm"
	"github.com/caimlas/meept/internal/tools"
)

// ASTQueryTool runs tree-sitter queries against source code.
type ASTQueryTool struct {
	executor *ast.QueryExecutor
}

// NewASTQueryTool creates a new AST query tool.
func NewASTQueryTool(parser *ast.ParserManager) (*ASTQueryTool, error) {
	if parser == nil {
		return nil, fmt.Errorf("ast.ParserManager cannot be nil")
	}
	return &ASTQueryTool{
		executor: ast.NewQueryExecutor(parser),
	}, nil
}

func (t *ASTQueryTool) Name() string { return "ast_query" }

func (t *ASTQueryTool) Description() string {
	return `Run tree-sitter S-expression queries to find specific patterns in source code.
Use for advanced code analysis like finding all function calls, matching specific patterns, etc.

Example queries:
- Functions: "(function_declaration name: (identifier) @name)"
- Method calls: "(call_expression function: (identifier) @func)"
- Imports: "(import_statement) @import"

Common query names available: functions, classes, imports, strings, comments`
}

func (t *ASTQueryTool) Parameters() llm.FunctionParameters {
	return llm.FunctionParameters{
		Type: SchemaTypeObject,
		Properties: map[string]llm.ParameterProperty{
			SchemaPropFilePath: {
				Type:        "string",
				Description: "Path to the source file to query.",
			},
			"query": {
				Type:        "string",
				Description: "Tree-sitter S-expression query pattern. Use @name to capture nodes.",
			},
			"query_name": {
				Type:        "string",
				Description: "Use a predefined query: functions, classes, imports, strings, comments.",
			},
			SchemaPropLanguage: {
				Type:        "string",
				Description: "Override language detection (go, python, typescript, etc.).",
			},
			"max_matches": {
				Type:        "integer",
				Description: "Maximum number of matches to return (default: 100).",
			},
		},
		Required: []string{"file_path"},
	}
}

func (t *ASTQueryTool) Execute(ctx context.Context, args map[string]any) (any, error) {
	filePath, _ := args["file_path"].(string)
	if filePath == "" {
		return nil, fmt.Errorf("file_path is required")
	}

	query, _ := args["query"].(string)
	queryName, _ := args["query_name"].(string)
	langStr, _ := args["language"].(string)
	maxMatches := 100
	if m, ok := args["max_matches"].(float64); ok {
		maxMatches = int(m)
	}

	// Determine language
	var lang ast.Language
	if langStr != "" {
		lang = ast.LanguageFromString(langStr)
	} else {
		lang = ast.DetectLanguage(filePath)
	}
	if lang == ast.LangUnknown {
		return nil, fmt.Errorf("could not detect language for: %s", filePath)
	}

	// Resolve query
	if query == "" && queryName != "" {
		var ok bool
		query, ok = ast.GetCommonQuery(queryName, lang)
		if !ok {
			return nil, fmt.Errorf("no predefined query '%s' for language '%s'", queryName, lang)
		}
	}

	if query == "" {
		return nil, fmt.Errorf("either query or query_name must be provided")
	}

	result, err := t.executor.RunQueryWithLanguage(ctx, filePath, lang, query)
	if err != nil {
		return nil, fmt.Errorf("query failed: %w", err)
	}

	// Limit matches
	if result.Count > maxMatches {
		limit := min(maxMatches, len(result.Matches))
		result.Matches = result.Matches[:limit]
		result.Count = limit
	}

	return result, nil
}

// Ensure tool implements the Tool interface
var _ tools.Tool = (*ASTQueryTool)(nil)
