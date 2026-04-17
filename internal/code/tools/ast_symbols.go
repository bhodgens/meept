package tools

import (
	"context"
	"fmt"

	"github.com/caimlas/meept/internal/code/ast"
	"github.com/caimlas/meept/internal/llm"
	"github.com/caimlas/meept/internal/tools"
)

// ASTSymbolsTool extracts code symbols from source files.
type ASTSymbolsTool struct {
	extractor *ast.SymbolExtractor
}

// NewASTSymbolsTool creates a new AST symbols tool.
func NewASTSymbolsTool(parser *ast.ParserManager) (*ASTSymbolsTool, error) {
	if parser == nil {
		return nil, fmt.Errorf("ast.ParserManager cannot be nil")
	}
	return &ASTSymbolsTool{
		extractor: ast.NewSymbolExtractor(parser),
	}, nil
}

func (t *ASTSymbolsTool) Name() string { return "ast_symbols" }

func (t *ASTSymbolsTool) Description() string {
	return `Extract code symbols (functions, classes, methods, interfaces, etc.) from source files.
Returns symbol names, kinds, locations, and signatures. Useful for understanding code structure.`
}

func (t *ASTSymbolsTool) Parameters() llm.FunctionParameters {
	return llm.FunctionParameters{
		Type: "object",
		Properties: map[string]llm.ParameterProperty{
			"file_path": {
				Type:        "string",
				Description: "Path to the source file to analyze.",
			},
			"kinds": {
				Type:        "array",
				Description: "Filter by symbol kinds: function, method, class, interface, struct, enum, constant, variable, module. Empty means all.",
			},
			"include_private": {
				Type:        "boolean",
				Description: "Include private/unexported symbols (default: true).",
			},
			"max_depth": {
				Type:        "integer",
				Description: "Maximum nesting depth for child symbols (default: 0 = unlimited).",
			},
		},
		Required: []string{"file_path"},
	}
}

func (t *ASTSymbolsTool) Execute(ctx context.Context, args map[string]any) (any, error) {
	filePath, _ := args["file_path"].(string)
	if filePath == "" {
		return nil, fmt.Errorf("file_path is required")
	}

	filter := ast.DefaultSymbolFilter()

	// Parse kinds filter
	if kindsRaw, ok := args["kinds"].([]any); ok && len(kindsRaw) > 0 {
		kinds := make([]ast.SymbolKind, 0, len(kindsRaw))
		for _, k := range kindsRaw {
			if kindStr, ok := k.(string); ok {
				kind := parseSymbolKind(kindStr)
				if kind != 0 {
					kinds = append(kinds, kind)
				}
			}
		}
		if len(kinds) > 0 {
			filter.Kinds = kinds
		}
	}

	// Parse include_private
	if includePrivate, ok := args["include_private"].(bool); ok {
		filter.IncludePrivate = includePrivate
	}

	// Parse max_depth
	if maxDepth, ok := args["max_depth"].(float64); ok {
		filter.MaxDepth = int(maxDepth)
	}

	symbols, err := t.extractor.ExtractFromFileWithFilter(ctx, filePath, filter)
	if err != nil {
		return nil, fmt.Errorf("failed to extract symbols: %w", err)
	}

	return map[string]any{
		"file_path": filePath,
		"symbols":   symbols,
		"count":     len(symbols),
	}, nil
}

func parseSymbolKind(s string) ast.SymbolKind {
	switch s {
	case "function":
		return ast.SymbolKindFunction
	case "method":
		return ast.SymbolKindMethod
	case "class":
		return ast.SymbolKindClass
	case "interface":
		return ast.SymbolKindInterface
	case "struct":
		return ast.SymbolKindStruct
	case "enum":
		return ast.SymbolKindEnum
	case "constant":
		return ast.SymbolKindConstant
	case "variable":
		return ast.SymbolKindVariable
	case "module":
		return ast.SymbolKindModule
	case "field":
		return ast.SymbolKindField
	case "property":
		return ast.SymbolKindProperty
	default:
		return 0
	}
}

// Ensure tool implements the Tool interface
var _ tools.Tool = (*ASTSymbolsTool)(nil)
