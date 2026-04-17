// Package tools provides agent tools for code intelligence.
package tools

import (
	"context"
	"fmt"

	"github.com/caimlas/meept/internal/code/ast"
	"github.com/caimlas/meept/internal/llm"
	"github.com/caimlas/meept/internal/tools"
)

// ASTParseTool parses source code into an AST.
type ASTParseTool struct {
	parser *ast.ParserManager
}

// NewASTParseTool creates a new AST parse tool.
func NewASTParseTool(parser *ast.ParserManager) (*ASTParseTool, error) {
	if parser == nil {
		return nil, fmt.Errorf("ast.ParserManager cannot be nil")
	}
	return &ASTParseTool{parser: parser}, nil
}

func (t *ASTParseTool) Name() string { return "ast_parse" }

func (t *ASTParseTool) Description() string {
	return `Parse source code into an abstract syntax tree. Can parse from a file path or inline source code.
Supports: Go, Python, TypeScript, JavaScript, Rust, C, C++, Java, Ruby, YAML, TOML, Bash, and more.`
}

func (t *ASTParseTool) Parameters() llm.FunctionParameters {
	return llm.FunctionParameters{
		Type: "object",
		Properties: map[string]llm.ParameterProperty{
			"file_path": {
				Type:        "string",
				Description: "Path to the source file to parse. Either file_path or source+language is required.",
			},
			"source": {
				Type:        "string",
				Description: "Inline source code to parse (use with 'language' parameter).",
			},
			"language": {
				Type:        "string",
				Description: "Language of inline source: go, python, typescript, javascript, rust, c, cpp, java, ruby, yaml, toml, bash.",
			},
			"max_depth": {
				Type:        "integer",
				Description: "Maximum depth of AST nodes to return (default: 5, 0 for unlimited).",
			},
		},
		Required: []string{},
	}
}

func (t *ASTParseTool) Execute(ctx context.Context, args map[string]any) (any, error) {
	filePath, _ := args["file_path"].(string)
	source, _ := args["source"].(string)
	language, _ := args["language"].(string)
	maxDepth := 5
	if d, ok := args["max_depth"].(float64); ok {
		maxDepth = int(d)
	}

	var result *ast.ParseResult
	var err error

	if filePath != "" {
		result, err = t.parser.ParseFile(ctx, filePath)
		if err != nil {
			return nil, fmt.Errorf("failed to parse file: %w", err)
		}
	} else if source != "" && language != "" {
		lang := ast.LanguageFromString(language)
		if lang == ast.LangUnknown {
			return nil, fmt.Errorf("unsupported language: %s", language)
		}
		result, err = t.parser.Parse(ctx, []byte(source), lang)
		if err != nil {
			return nil, fmt.Errorf("failed to parse source: %w", err)
		}
	} else {
		return nil, fmt.Errorf("either file_path or source+language must be provided")
	}

	// Apply depth limit if result has a root node
	if maxDepth > 0 && result != nil && result.RootNode.Type != "" {
		result.RootNode = truncateNode(result.RootNode, maxDepth)
	}

	return result, nil
}

// truncateNode limits the depth of a node tree.
func truncateNode(node ast.Node, depth int) ast.Node {
	if depth <= 0 {
		node.Children = nil
		return node
	}

	if len(node.Children) > 0 {
		truncated := make([]ast.Node, len(node.Children))
		for i, child := range node.Children {
			truncated[i] = truncateNode(child, depth-1)
		}
		node.Children = truncated
	}

	return node
}

// Ensure tool implements the Tool interface
var _ tools.Tool = (*ASTParseTool)(nil)
