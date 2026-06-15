package tools

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/caimlas/meept/internal/code/ast"
	"github.com/caimlas/meept/internal/llm"
	"github.com/caimlas/meept/internal/tools"
	"github.com/caimlas/meept/internal/tools/builtin"
)

// ASTEditTool performs structural AST-based edits with preview/apply pattern.
type ASTEditTool struct {
	rewriter               *ast.ASTRewriter
	parser                 *ast.ParserManager
	pendingChangesRegistry *builtin.PendingChangesRegistry
}

// NewASTEditTool creates a new AST edit tool.
func NewASTEditTool(parser *ast.ParserManager) (*ASTEditTool, error) {
	if parser == nil {
		return nil, fmt.Errorf("ast.ParserManager cannot be nil")
	}
	return &ASTEditTool{
		rewriter: ast.NewASTRewriter(parser),
		parser:   parser,
	}, nil
}

// SetPendingChangesRegistry sets the pending changes registry for preview/accept workflow.
// Nil guard is required per CLAUDE.md "Setter methods" coding practice.
func (t *ASTEditTool) SetPendingChangesRegistry(registry *builtin.PendingChangesRegistry) {
	if registry != nil {
		t.pendingChangesRegistry = registry
	}
}

func (t *ASTEditTool) Name() string { return "ast_edit" }

func (t *ASTEditTool) Category() string { return "code" }

func (t *ASTEditTool) Description() string {
	return `Perform structural AST-based code edits using tree-sitter queries.
Use this tool for batch modifications like:
- Replace all function bodies matching a pattern
- Rename all variable declarations of a type
- Insert code before/after specific AST node types

The tool returns proposed edits without modifying files. Use preview_only=false to apply.

Parameters:
- file_path: Source file to edit
- query: Tree-sitter S-expression query to match nodes
- query_name: Predefined query (functions, classes, imports, etc.)
- rewrite_template: Template with {{capture}} placeholders for replacement
- language: Override language detection
- preview_only: If true, only show preview (default: true)

Example rewrite templates:
- "{{visibility}} func {{name}}({{params}}) {{return_type}} { /* refactored */ }"
- "const {{name}} = {{value}} // deprecated: use {{newName}} instead"
`
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
				Description: "Tree-sitter S-expression query pattern. Use @name to capture nodes.",
			},
			"query_name": {
				Type:        SchemaTypeString,
				Description: "Use a predefined query: functions, classes, imports, strings, comments.",
			},
			"rewrite_template": {
				Type:        SchemaTypeString,
				Description: "Template for replacement with {{capture}} placeholders.",
			},
			SchemaPropLanguage: {
				Type:        SchemaTypeString,
				Description: "Override language detection (go, python, typescript, etc.).",
			},
			"preview_only": {
				Type:        SchemaTypeBoolean,
				Description: "If true, only return preview without applying (default: true).",
			},
		},
		Required: []string{SchemaPropFilePath, "rewrite_template"},
	}
}

func (t *ASTEditTool) Execute(ctx context.Context, args map[string]any) (any, error) {
	filePath, _ := args[SchemaPropFilePath].(string)
	if filePath == "" {
		return nil, fmt.Errorf("file_path is required")
	}

	query, _ := args["query"].(string)
	queryName, _ := args["query_name"].(string)
	rewriteTemplate, _ := args["rewrite_template"].(string)
	langStr, _ := args["language"].(string)
	previewOnly := true
	if p, ok := args["preview_only"].(bool); ok {
		previewOnly = p
	}

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
			return nil, fmt.Errorf("no predefined query '%s' for language '%s'", queryName, langStr)
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
	result, err := t.rewriter.RunRewrite(source, lang, query, rewriteTemplate)
	if err != nil {
		return nil, fmt.Errorf("rewrite failed: %w", err)
	}

	// Build response
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
		SchemaPropFound:   result.Rewrite.MatchCount > 0,
		"match_count":     result.Rewrite.MatchCount,
		"file_path":       filePath,
		"query":           result.Rewrite.Query,
		"edits":           edits,
		"preview_only":    previewOnly,
		"modified_source": "",
	}

	// Generate modified source
	modifiedSource := ast.ApplyEdits(source, result.Rewrite.ProposedEdits)

	// Handle preview/accept workflow when registry is available and preview_only=true
	if t.pendingChangesRegistry != nil && previewOnly {
		sessionID := fmt.Sprintf("%d", time.Now().UnixNano())
		if sid, ok := ctx.Value("session_id").(string); ok && sid != "" {
			sessionID = sid
		}

		now := time.Now()
		expiresAt := now.Add(30 * time.Minute)

		// Generate simple diff
		diff := generateSimpleDiff(string(source), string(modifiedSource), filePath)

		change := &builtin.PendingChange{
			ID:        fmt.Sprintf("ast_%s_%d", filePath, now.UnixNano()),
			SessionID: sessionID,
			FilePath:  filePath,
			Original:  string(source),
			Modified:  string(modifiedSource),
			Diff:      diff,
			CreatedAt: now,
			ExpiresAt: &expiresAt,
			Metadata: map[string]any{
				"tool":        "ast_edit",
				"match_count": result.Rewrite.MatchCount,
				"edit_count":  len(edits),
			},
		}

		t.pendingChangesRegistry.Add(change)

		response["pending_change_id"] = change.ID
		response["message"] = fmt.Sprintf("AST edit preview created (%d matches). Use 'resolve' tool to accept/reject change %s", result.Rewrite.MatchCount, change.ID)
		return response, nil
	}

	// Apply edits immediately when preview_only=false or no registry
	response["modified_source"] = string(modifiedSource)

	// Write to file
	if err := os.WriteFile(filePath, modifiedSource, 0o644); err != nil {
		return nil, fmt.Errorf("failed to write modified file: %w", err)
	}

	return response, nil
}

// Ensure tool implements the Tool interface
var _ tools.Tool = (*ASTEditTool)(nil)
