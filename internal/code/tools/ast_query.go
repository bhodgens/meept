package tools

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/caimlas/meept/internal/code/ast"
	"github.com/caimlas/meept/internal/llm"
	"github.com/caimlas/meept/internal/tools"
)

// ASTQueryTool runs tree-sitter queries against source code.
type ASTQueryTool struct {
	executor     *ast.QueryExecutor
	ruleExecutor *ast.RuleExecutor
	parser       *ast.ParserManager
}

// NewASTQueryTool creates a new AST query tool.
func NewASTQueryTool(parser *ast.ParserManager) (*ASTQueryTool, error) {
	if parser == nil {
		return nil, fmt.Errorf("ast.ParserManager cannot be nil")
	}
	return &ASTQueryTool{
		executor:     ast.NewQueryExecutor(parser),
		ruleExecutor: ast.NewRuleExecutor(parser),
		parser:       parser,
	}, nil
}

func (t *ASTQueryTool) Name() string { return "ast_query" }

func (t *ASTQueryTool) Category() string { return "code" }

func (t *ASTQueryTool) Description() string {
	return `Run tree-sitter S-expression queries or YAML rules to find specific patterns in source code.
Use for advanced code analysis like finding all function calls, matching specific patterns, etc.

Two modes:
1. Query mode: Use 'query' or 'query_name' for S-expression patterns
2. Rule mode: Use 'rule' or 'rule_name' for YAML-based rules with constraints

Example queries:
- Functions: "(function_declaration name: (identifier) @name)"
- Method calls: "(call_expression function: (identifier) @func)"
- Imports: "(import_statement) @import"

Common query names: functions, classes, imports, strings, comments
Common rule names: go_test_functions, todo_comments, empty_if_blocks

YAML rule format:
  id: my-rule
  language: go
  pattern: $(FUNC)
  constraints:
    - regex:
        node: name
        pattern: "^Test"
`
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
			"rule": {
				Type:        SchemaTypeString,
				Description: "YAML rule string for complex patterns with constraints.",
			},
			"rule_name": {
				Type:        SchemaTypeString,
				Description: "Use a predefined rule: go_test_functions, todo_comments, empty_if_blocks.",
			},
			"rule_file": {
				Type:        SchemaTypeString,
				Description: "Path to a YAML rule file.",
			},
			SchemaPropLanguage: {
				Type:        SchemaTypeString,
				Description: "Override language detection (go, python, typescript, etc.).",
			},
			"max_matches": {
				Type:        SchemaTypeInteger,
				Description: "Maximum number of matches to return (default: 100).",
			},
			"include_source": {
				Type:        SchemaTypeBoolean,
				Description: "Include source code snippet for each match (default: false).",
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

	// Get optional parameters
	query, _ := args["query"].(string)
	queryName, _ := args["query_name"].(string)
	ruleStr, _ := args["rule"].(string)
	ruleName, _ := args["rule_name"].(string)
	ruleFile, _ := args["rule_file"].(string)
	langStr, _ := args["language"].(string)
	maxMatches := 100
	if m, ok := args["max_matches"].(float64); ok {
		maxMatches = int(m)
	}
	includeSource := false
	if s, ok := args["include_source"].(bool); ok {
		includeSource = s
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

	// Check if using rule mode or query mode
	isRuleMode := ruleStr != "" || ruleName != "" || ruleFile != ""
	isQueryMode := query != "" || queryName != ""

	if isRuleMode && isQueryMode {
		return nil, fmt.Errorf("cannot use both rule and query modes - choose one")
	}

	if !isRuleMode && !isQueryMode {
		return nil, fmt.Errorf("either query/query_name or rule/rule_name/rule_file must be provided")
	}

	var result *ast.RuleResult
	var queryResult *ast.QueryResult

	if isRuleMode {
		// Rule mode: Load and execute YAML rule
		var rule string
		if ruleStr != "" {
			rule = ruleStr
		} else if ruleName != "" {
			var ok bool
			rule, ok = ast.GetCommonRule(ruleName)
			if !ok {
				return nil, fmt.Errorf("no predefined rule '%s'", ruleName)
			}
		} else if ruleFile != "" {
			return t.executeRuleFile(ctx, filePath, lang, ruleFile, maxMatches, includeSource)
		}

		if rule != "" {
			parsedRule, err := ast.ParseRule(rule)
			if err != nil {
				return nil, fmt.Errorf("invalid YAML rule: %w", err)
			}
			result, err = t.ruleExecutor.ExecuteRuleOnFile(filePath, parsedRule)
			if err != nil {
				return nil, fmt.Errorf("rule execution failed: %w", err)
			}
		}
	} else {
		// Query mode: Execute S-expression query
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

		var err error
		queryResult, err = t.executor.RunQueryWithLanguage(ctx, filePath, lang, query)
		if err != nil {
			return nil, fmt.Errorf("query failed: %w", err)
		}
	}

	// Build response
	if result != nil {
		// Rule mode response
		matches := make([]map[string]any, len(result.Matches))
		for i, match := range result.Matches {
			matchData := map[string]any{
				"start_line":  match.StartLine,
				"start_char":  match.StartChar,
				"end_line":    match.EndLine,
				"end_char":    match.EndChar,
				"node_kind":   match.NodeKind,
				"captures":    match.Captures,
				"transformed": match.Transformed,
			}
			if includeSource {
				matchData["source"] = getSourceForMatch(filePath, match.StartLine, match.EndLine)
			}
			matches[i] = matchData
		}

		if len(matches) > maxMatches {
			matches = matches[:maxMatches]
		}

		return map[string]any{
			SchemaPropFound: len(matches) > 0,
			"match_count":   len(matches),
			"rule_id":       result.Rule.ID,
			"file_path":     filePath,
			"matches":       matches,
			"mode":          "rule",
		}, nil
	} else {
		// Query mode response (existing format)
		if queryResult.Count > maxMatches {
			queryResult.Matches = queryResult.Matches[:maxMatches]
			queryResult.Count = maxMatches
		}
		return queryResult, nil
	}
}

// executeRuleFile executes a rule from a YAML file
func (t *ASTQueryTool) executeRuleFile(ctx context.Context, filePath string, lang ast.Language, ruleFile string, maxMatches int, includeSource bool) (any, error) {
	rule, err := ast.LoadRuleFile(ruleFile)
	if err != nil {
		return nil, fmt.Errorf("failed to load rule file: %w", err)
	}

	result, err := t.ruleExecutor.ExecuteRuleOnFile(filePath, rule)
	if err != nil {
		return nil, fmt.Errorf("rule execution failed: %w", err)
	}

	matches := make([]map[string]any, len(result.Matches))
	for i, match := range result.Matches {
		matchData := map[string]any{
			"start_line":  match.StartLine,
			"start_char":  match.StartChar,
			"end_line":    match.EndLine,
			"end_char":    match.EndChar,
			"node_kind":   match.NodeKind,
			"captures":    match.Captures,
			"transformed": match.Transformed,
		}
		if includeSource {
			matchData["source"] = getSourceForMatch(filePath, match.StartLine, match.EndLine)
		}
		matches[i] = matchData
	}

	if len(matches) > maxMatches {
		matches = matches[:maxMatches]
	}

	return map[string]any{
		SchemaPropFound: len(matches) > 0,
		"match_count":   len(matches),
		"rule_id":       result.Rule.ID,
		"file_path":     filePath,
		"matches":       matches,
		"mode":          "rule",
	}, nil
}

// getSourceForMatch extracts source code for a match
func getSourceForMatch(filePath string, startLine, endLine int) string {
	data, err := os.ReadFile(filePath)
	if err != nil {
		return ""
	}

	lines := strings.Split(string(data), "\n")
	if startLine < 0 || startLine >= len(lines) {
		return ""
	}
	if endLine >= len(lines) {
		endLine = len(lines) - 1
	}

	var sb strings.Builder
	for i := startLine; i <= endLine && i < len(lines); i++ {
		if i > startLine {
			sb.WriteString("\n")
		}
		sb.WriteString(lines[i])
	}
	return sb.String()
}

// Ensure tool implements the Tool interface
var _ tools.Tool = (*ASTQueryTool)(nil)
