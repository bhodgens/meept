package tools

import (
	"context"
	"fmt"
	"os"

	"github.com/caimlas/meept/internal/code/ast"
	"github.com/caimlas/meept/internal/llm"
	"github.com/caimlas/meept/internal/tools"
	"github.com/caimlas/meept/internal/tools/builtin"
)

// ResolveASTEditTool applies or discards pending ast_edit proposals.
// This tool provides an explicit resolve pattern for the preview-before-apply workflow.
type ResolveASTEditTool struct {
	parser       *ast.ParserManager
	fenceChecker builtin.FenceChecker
}

// NewResolveASTEditTool creates a new resolve AST edit tool.
func NewResolveASTEditTool(parser *ast.ParserManager) (*ResolveASTEditTool, error) {
	if parser == nil {
		return nil, fmt.Errorf("ast.ParserManager cannot be nil")
	}
	return &ResolveASTEditTool{parser: parser}, nil
}

// SetFenceChecker sets the fence checker for path-based sandboxing.
// Nil guard is required per CLAUDE.md "Setter methods" coding practice.
func (t *ResolveASTEditTool) SetFenceChecker(fc builtin.FenceChecker) {
	if fc != nil {
		t.fenceChecker = fc
	}
}

func (t *ResolveASTEditTool) Name() string { return "resolve_ast_edit" }

func (t *ResolveASTEditTool) Category() string { return "code" }

func (t *ResolveASTEditTool) Description() string {
	return `Apply or discard proposed AST edits from a previous ast_edit call.
Use this tool after reviewing ast_edit output to explicitly apply the changes.

Parameters:
- file_path: Source file to edit
- query: Tree-sitter S-expression query (must match original ast_edit call)
- rewrite_template: Template with {{capture}} placeholders (must match original)
- action: "apply" to write changes, "discard" to show diff without modifying

The apply action re-runs the query and applies edits atomically.
This ensures the edits are still valid at application time.
`
}

func (t *ResolveASTEditTool) Parameters() llm.FunctionParameters {
	return llm.FunctionParameters{
		Type: SchemaTypeObject,
		Properties: map[string]llm.ParameterProperty{
			SchemaPropFilePath: {
				Type:        SchemaTypeString,
				Description: "Path to the source file to edit.",
			},
			"query": {
				Type:        SchemaTypeString,
				Description: "Tree-sitter S-expression query pattern (must match original ast_edit call).",
			},
			"query_name": {
				Type:        SchemaTypeString,
				Description: "Predefined query name (functions, classes, imports, etc.).",
			},
			"rewrite_template": {
				Type:        SchemaTypeString,
				Description: "Template for replacement with {{capture}} placeholders.",
			},
			SchemaPropLanguage: {
				Type:        SchemaTypeString,
				Description: "Override language detection.",
			},
			"action": {
				Type:        SchemaTypeString,
				Description: "Action to take: 'apply' to write changes, 'discard' to show preview only.",
				Enum:        []string{"apply", "discard"},
			},
		},
		Required: []string{SchemaPropFilePath, "rewrite_template", "action"},
	}
}

func (t *ResolveASTEditTool) Execute(ctx context.Context, args map[string]any) (any, error) {
	filePath, _ := args[SchemaPropFilePath].(string)
	if filePath == "" {
		return nil, fmt.Errorf("file_path is required")
	}

	action, _ := args["action"].(string)
	if action == "" {
		action = "discard" // default to safe preview
	}
	if action != "apply" && action != "discard" {
		return nil, fmt.Errorf("action must be 'apply' or 'discard'")
	}

	query, _ := args["query"].(string)
	queryName, _ := args["query_name"].(string)
	rewriteTemplate, _ := args["rewrite_template"].(string)
	langStr, _ := args["language"].(string)

	if rewriteTemplate == "" {
		return nil, fmt.Errorf("rewrite_template is required")
	}

	// Resolve query
	if query == "" && queryName != "" {
		var lang ast.Language
		if langStr != "" {
			lang = ast.LanguageFromString(langStr)
		} else {
			lang = ast.DetectLanguage(filePath)
		}
		var ok bool
		query, ok = ast.GetCommonQuery(queryName, lang)
		if !ok {
			return nil, fmt.Errorf("no predefined query '%s'", queryName)
		}
	}

	if query == "" {
		return nil, fmt.Errorf("either query or query_name must be provided")
	}

	// Read file
	source, err := os.ReadFile(filePath)
	if err != nil {
		return nil, fmt.Errorf("failed to read file: %w", err)
	}

	// Detect language
	var lang ast.Language
	if langStr != "" {
		lang = ast.LanguageFromString(langStr)
	} else {
		lang = ast.DetectLanguage(filePath)
	}
	if lang == ast.LangUnknown {
		return nil, fmt.Errorf("could not detect language for: %s", filePath)
	}

	// Run rewrite
	rewriter := ast.NewASTRewriter(t.parser)
	result, err := rewriter.RunRewrite(source, lang, query, rewriteTemplate)
	if err != nil {
		return nil, fmt.Errorf("rewrite failed: %w", err)
	}

	// Build edits
	edits := make([]map[string]any, len(result.Rewrite.ProposedEdits))
	for i, edit := range result.Rewrite.ProposedEdits {
		edits[i] = map[string]any{
			"start_line": edit.StartLine,
			"start_char": edit.StartChar,
			"end_line":   edit.EndLine,
			"end_char":   edit.EndChar,
			"old_text":   edit.OldText,
			"new_text":   edit.NewText,
			"node_kind":  edit.NodeKind,
			"captures":   edit.Captures,
		}
	}

	response := map[string]any{
		SchemaPropFound: result.Rewrite.MatchCount > 0,
		"match_count":   result.Rewrite.MatchCount,
		"file_path":     filePath,
		"query":         result.Rewrite.Query,
		"action":        action,
		"edits":         edits,
	}

	// Apply edits if action is "apply"
	if action == "apply" {
		modifiedSource := ast.ApplyEdits(source, result.Rewrite.ProposedEdits)
		response["applied"] = true

		// Fence check: validate the resolved file path against the workspace
		// boundary before writing. Mirrors the ASTEditTool write guard.
		if t.fenceChecker != nil {
			if err := t.fenceChecker.CheckPath(filePath, "write"); err != nil {
				return nil, fmt.Errorf("fence check failed: %w", err)
			}
		}

		// Write to file
		if err := os.WriteFile(filePath, modifiedSource, 0o644); err != nil {
			return nil, fmt.Errorf("failed to write modified file: %w", err)
		}
		response["message"] = fmt.Sprintf("Applied %d edits to %s", len(edits), filePath)
	} else {
		response["applied"] = false
		response["modified_source_preview"] = string(ast.ApplyEdits(source, result.Rewrite.ProposedEdits))
		response["message"] = fmt.Sprintf("Preview of %d edits to %s (use action='apply' to write)", len(edits), filePath)
	}

	return response, nil
}

// Ensure tool implements the Tool interface
var _ tools.Tool = (*ResolveASTEditTool)(nil)
