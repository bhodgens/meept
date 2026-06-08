package tools

import (
	"context"
	"fmt"
	"os"

	"github.com/caimlas/meept/internal/code/ast"
	"github.com/caimlas/meept/internal/llm"
	"github.com/caimlas/meept/internal/tools"
)

// ASTEditTool generates structural edit proposals from tree-sitter queries.
type ASTEditTool struct {
	parser *ast.ParserManager
}

// NewASTEditTool creates a new AST edit tool.
func NewASTEditTool(parser *ast.ParserManager) (*ASTEditTool, error) {
	if parser == nil {
		return nil, fmt.Errorf("ast.ParserManager cannot be nil")
	}
	return &ASTEditTool{parser: parser}, nil
}

func (t *ASTEditTool) Name() string { return "ast_edit" }

func (t *ASTEditTool) Category() string { return "code" }

func (t *ASTEditTool) Description() string {
	return `Run a tree-sitter query to find AST nodes and generate proposed edits WITHOUT applying them.
Use this for structural code changes like renaming variables, replacing function bodies, or inserting code.
Returns a preview of changes with edit_count, file_count, and affected ranges.
Use the "ast_resolve" tool with apply=true to apply the proposal afterward.`
}

func (t *ASTEditTool) Parameters() llm.FunctionParameters {
	return llm.FunctionParameters{
		Type: SchemaTypeObject,
		Properties: map[string]llm.ParameterProperty{
			SchemaPropFilePath: {
				Type:        SchemaTypeString,
				Description: "Path to the source file to edit.",
			},
			"query": {
				Type:        SchemaTypeString,
				Description: "Tree-sitter S-expression query pattern. Captured nodes (@name) will be rewritten.",
			},
			"operation": {
				Type:        SchemaTypeString,
				Description: "Operation type: replace (replace node with template), rename (rename identifier), insert_after (insert template after node).",
				Enum:        []string{"replace", "rename", "insert_after"},
			},
			"template": {
				Type:        SchemaTypeString,
				Description: "Template for the new text. For 'replace', use @capture_name to substitute captured text. For 'rename', this is the new name.",
			},
			SchemaPropLanguage: {
				Type:        SchemaTypeString,
				Description: "Override language detection (go, python, typescript, etc.).",
			},
		},
		Required: []string{SchemaPropFilePath, "query", "operation", "template"},
	}
}

func (t *ASTEditTool) Execute(ctx context.Context, args map[string]any) (any, error) {
	filePath, _ := args[SchemaPropFilePath].(string)
	if filePath == "" {
		return nil, fmt.Errorf("file_path is required")
	}

	query, _ := args["query"].(string)
	if query == "" {
		return nil, fmt.Errorf("query is required")
	}

	opStr, _ := args["operation"].(string)
	if opStr == "" {
		return nil, fmt.Errorf("operation is required")
	}

	template, _ := args["template"].(string)
	if template == "" {
		return nil, fmt.Errorf("template is required")
	}

	langStr, _ := args[SchemaPropLanguage].(string)

	var lang ast.Language
	if langStr != "" {
		lang = ast.LanguageFromString(langStr)
	} else {
		lang = ast.DetectLanguage(filePath)
	}
	if lang == ast.LangUnknown {
		return nil, fmt.Errorf("could not detect language for: %s", filePath)
	}

	operation := ast.OperationType(opStr)
	switch operation {
	case ast.OpReplace, ast.OpRename, ast.OpInsertAfter:
		// valid
	default:
		return nil, fmt.Errorf("unknown operation: %s (valid: replace, rename, insert_after)", opStr)
	}

	// Verify file exists
	if _, err := os.Stat(filePath); err != nil {
		return nil, fmt.Errorf("file not found: %w", err)
	}

	engine := ast.NewRewriteEngine(t.parser)
	proposal, err := engine.GenerateProposal(ctx, filePath, lang, query, operation, template)
	if err != nil {
		return nil, fmt.Errorf("failed to generate proposal: %w", err)
	}

	if proposal.MatchCount == 0 {
		return map[string]any{
			SchemaPropFound:   false,
			SchemaPropMessage: "No matches found for the given query",
			SchemaPropFilePath: filePath,
			"query":           query,
		}, nil
	}

	// Populate file paths in edits for resolve tool
	for i := range proposal.Edits {
		proposal.Edits[i].FilePath = filePath
	}

	// Build preview summary
	changeSummaries := make([]map[string]any, 0, len(proposal.Edits))
	for _, edit := range proposal.Edits {
		changeSummaries = append(changeSummaries, map[string]any{
			SchemaPropFilePath:  filePath,
			SchemaPropStartLine: edit.StartLine,
			SchemaPropStartChar: edit.StartCol,
			SchemaPropEndLine:   edit.EndLine,
			SchemaPropEndChar:   edit.EndCol,
			"old_text":          truncate(edit.OldText, 60),
			"new_text":          truncate(edit.NewText, 60),
		})
	}

	return map[string]any{
		SchemaPropFound:      true,
		SchemaPropFilePath:   filePath,
		"query":              query,
		"operation":          opStr,
		"template":           template,
		SchemaPropCount:      proposal.MatchCount,
		"preview":            proposal.PreviewText,
		"proposed_edits":     changeSummaries,
		"proposal_id":        fmt.Sprintf("ast_edit_%s_%d", filePath, len(proposal.Edits)),
		"requires_resolve":   true,
		"resolve_tool":       "ast_resolve",
		"resolve_params_hint": map[string]any{"proposal_id": fmt.Sprintf("ast_edit_%s_%d", filePath, len(proposal.Edits)), "apply": true},
	}, nil
}

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max] + "..."
}

// Ensure tool implements the Tool interface
var _ tools.Tool = (*ASTEditTool)(nil)
