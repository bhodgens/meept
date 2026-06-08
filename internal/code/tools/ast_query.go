package tools

import (
	"context"
	"fmt"
	"os"

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

func (t *ASTQueryTool) Category() string { return "code" }

func (t *ASTQueryTool) Description() string {
	return `Run tree-sitter S-expression queries or YAML rules to find specific patterns in source code.
Use for advanced code analysis like finding all function calls, matching specific patterns, etc.

Query modes:
1. "query": Raw S-expression like "(function_declaration name: (identifier) @name)"
2. "query_name": Predefined query: functions, classes, imports, strings, comments
3. "yaml_rule": ast-grep style YAML rule with pattern, constraints, and transforms

All modes support:
- "context_lines": Number of lines before/after to include (default: 0)
- "max_matches": Maximum matches to return (default: 100)`
}

func (t *ASTQueryTool) Parameters() llm.FunctionParameters {
	return llm.FunctionParameters{
		Type: SchemaTypeObject,
		Properties: map[string]llm.ParameterProperty{
			SchemaPropFilePath: {
				Type:        SchemaTypeString,
				Description: "Path to the source file to query.",
			},
			"query": {
				Type:        SchemaTypeString,
				Description: "Tree-sitter S-expression query pattern. Use @name to capture nodes.",
			},
			"query_name": {
				Type:        SchemaTypeString,
				Description: "Use a predefined query: functions, classes, imports, strings, comments.",
			},
			"yaml_rule": {
				Type:        SchemaTypeString,
				Description: "ast-grep style YAML rule. Must contain 'pattern'. Optional: id, constraints, transform, fix.",
			},
			SchemaPropLanguage: {
				Type:        SchemaTypeString,
				Description: "Override language detection (go, python, typescript, etc.).",
			},
			"max_matches": {
				Type:        SchemaTypeInteger,
				Description: "Maximum number of matches to return (default: 100).",
			},
			"context_lines": {
				Type:        SchemaTypeInteger,
				Description: "Lines of context to include before/after each match (default: 0).",
			},
		},
		Required: []string{SchemaPropFilePath},
	}
}

func (t *ASTQueryTool) Execute(ctx context.Context, args map[string]any) (any, error) {
	filePath, _ := args[SchemaPropFilePath].(string)
	if filePath == "" {
		return nil, fmt.Errorf("file_path is required")
	}

	query, _ := args["query"].(string)
	queryName, _ := args["query_name"].(string)
	yamlRule, _ := args["yaml_rule"].(string)
	langStr, _ := args["language"].(string)
	maxMatches := 100
	if m, ok := args["max_matches"].(float64); ok {
		maxMatches = int(m)
	}
	contextLines := 0
	if c, ok := args["context_lines"].(float64); ok {
		contextLines = int(c)
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
	var result *ast.QueryResult
	var err error

	if yamlRule != "" {
		// YAML rule mode
		rule, parseErr := ast.ParseQueryRule(yamlRule)
		if parseErr != nil {
			return nil, fmt.Errorf("invalid yaml_rule: %w", parseErr)
		}
		// Override language from rule if provided
		if rule.Language != "" {
			lang = ast.LanguageFromString(rule.Language)
		}
		source, readErr := os.ReadFile(filePath)
		if readErr != nil {
			return nil, fmt.Errorf("failed to read file: %w", readErr)
		}
		result, err = t.executor.RunQueryWithRule(ctx, source, lang, rule)
	} else {
		// S-expression query mode
		if query == "" && queryName != "" {
			var ok bool
			query, ok = ast.GetCommonQuery(queryName, lang)
			if !ok {
				return nil, fmt.Errorf("no predefined query '%s' for language '%s'", queryName, lang)
			}
		}
		if query == "" {
			return nil, fmt.Errorf("either query, query_name, or yaml_rule must be provided")
		}
		result, err = t.executor.RunQueryWithLanguage(ctx, filePath, lang, query)
	}

	if err != nil {
		return nil, fmt.Errorf("query failed: %w", err)
	}

	// Limit matches
	if result.Count > maxMatches {
		limit := min(maxMatches, len(result.Matches))
		result.Matches = result.Matches[:limit]
		result.Count = limit
	}

	// Add context if requested
	if contextLines > 0 {
		source, readErr := os.ReadFile(filePath)
		if readErr == nil {
			ctxMatches, ctxErr := t.executor.RunQueryWithContext(ctx, source, lang, result.Query, contextLines)
			if ctxErr == nil {
				// Limit context matches
				if len(ctxMatches) > maxMatches {
					ctxMatches = ctxMatches[:maxMatches]
				}
				return struct {
					*ast.QueryResult
					ContextMatches []ast.ContextMatch `json:"context_matches"`
				}{
					QueryResult:    result,
					ContextMatches: ctxMatches,
				}, nil
			}
		}
	}

	return result, nil
}

// Ensure tool implements the Tool interface
var _ tools.Tool = (*ASTQueryTool)(nil)
