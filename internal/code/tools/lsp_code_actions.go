package tools

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/caimlas/meept/internal/code/lsp"
	"github.com/caimlas/meept/internal/llm"
	"github.com/caimlas/meept/internal/tools"
)

// LSPCodeActionsTool retrieves available code actions for a position in a file.
type LSPCodeActionsTool struct {
	manager *lsp.Manager
}

// NewLSPCodeActionsTool creates a new LSP code actions tool.
func NewLSPCodeActionsTool(manager *lsp.Manager) (*LSPCodeActionsTool, error) {
	if manager == nil {
		return nil, fmt.Errorf("lsp.Manager cannot be nil")
	}
	return &LSPCodeActionsTool{manager: manager}, nil
}

func (t *LSPCodeActionsTool) Name() string { return "lsp_code_actions" }

func (t *LSPCodeActionsTool) Description() string {
	return `Get available code actions (quick fixes, refactors, source actions) for a specific location.
Returns a list of actions with titles. If apply is true, applies the first matching action.
Optionally filter actions by title using the query parameter.
Requires an LSP server for the file's language to be configured and running.`
}

func (t *LSPCodeActionsTool) Parameters() llm.FunctionParameters {
	return llm.FunctionParameters{
		Type: SchemaTypeObject,
		Properties: map[string]llm.ParameterProperty{
			SchemaPropFilePath: {
				Type:        SchemaTypeString,
				Description: "Path to the source file.",
			},
			SchemaPropLine: {
				Type:        SchemaTypeInteger,
				Description: "Line number (0-indexed) for the code action request.",
			},
			SchemaPropCharacter: {
				Type:        SchemaTypeInteger,
				Description: "Column/character offset (0-indexed) within the line.",
			},
			"query": {
				Type:        SchemaTypeString,
				Description: "Optional filter: only return actions whose title contains this string.",
			},
			"apply": {
				Type:        SchemaTypeBoolean,
				Description: "If true, apply the first matching code action's edits to disk.",
			},
		},
		Required: []string{SchemaPropFilePath, "line", "character"},
	}
}

func (t *LSPCodeActionsTool) Execute(ctx context.Context, args map[string]any) (any, error) {
	filePath, _ := args[SchemaPropFilePath].(string)
	if filePath == "" {
		return nil, fmt.Errorf("file_path is required")
	}

	lineRaw, ok := args["line"].(float64)
	if !ok {
		return nil, fmt.Errorf("line is required")
	}
	line := int(lineRaw)

	charRaw, ok := args["character"].(float64)
	if !ok {
		return nil, fmt.Errorf("character is required")
	}
	char := int(charRaw)

	query, _ := args["query"].(string)
	apply := false
	if a, ok := args["apply"].(bool); ok {
		apply = a
	}

	// Get absolute path
	absPath, err := filepath.Abs(filePath)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve path: %w", err)
	}

	// Detect language and get server
	languageID := lsp.DetectLanguageID(absPath)
	srv, err := t.manager.GetServerForLanguage(ctx, languageID)
	if err != nil {
		return nil, fmt.Errorf("no LSP server for language %s: %w", languageID, err)
	}
	if srv.DocMgr == nil || srv.Client == nil {
		return nil, fmt.Errorf("LSP server for %s is not fully initialized", languageID)
	}

	// Open the document if not already open
	if _, err := srv.DocMgr.OpenFile(ctx, absPath); err != nil {
		return nil, fmt.Errorf("failed to open document: %w", err)
	}

	// Get code actions
	uri := lsp.PathToURI(absPath)
	actions, err := srv.Client.CodeActions(ctx, uri, line, char)
	if err != nil {
		return nil, fmt.Errorf("failed to get code actions: %w", err)
	}

	if len(actions) == 0 {
		return map[string]any{
			SchemaPropFound:     false,
			SchemaPropMessage:   "No code actions available at this location",
			SchemaPropFilePath:  filePath,
			SchemaPropLine:      line,
			SchemaPropCharacter: char,
		}, nil
	}

	// Filter by query if provided
	filtered := actions
	if query != "" {
		filtered = nil
		for _, action := range actions {
			if strings.Contains(strings.ToLower(action.Title), strings.ToLower(query)) {
				filtered = append(filtered, action)
			}
		}
		if len(filtered) == 0 {
			return map[string]any{
				SchemaPropFound:     false,
				SchemaPropMessage:   fmt.Sprintf("No code actions matching '%s'", query),
				SchemaPropFilePath:  filePath,
				SchemaPropLine:      line,
				SchemaPropCharacter: char,
				"available_count":   len(actions),
			}, nil
		}
	}

	// Convert to result format
	actionResults := make([]map[string]any, len(filtered))
	for i, action := range filtered {
		item := map[string]any{
			"title": action.Title,
		}
		if action.Kind != "" {
			item["kind"] = action.Kind
		}
		if action.IsPreferred {
			item["is_preferred"] = true
		}
		if action.Disabled != nil {
			item["disabled_reason"] = action.Disabled.Reason
		}
		actionResults[i] = item
	}

	result := map[string]any{
		SchemaPropFound:     true,
		SchemaPropFilePath:  filePath,
		SchemaPropLine:      line,
		SchemaPropCharacter: char,
		"actions":           actionResults,
		SchemaPropCount:     len(actionResults),
	}

	// Apply first matching action if requested
	if apply && len(filtered) > 0 {
		action := filtered[0]
		if action.Edit != nil && len(action.Edit.Changes) > 0 {
			for fileURI, edits := range action.Edit.Changes {
				editPath := lsp.URIToPath(fileURI)
				if err := applyTextEdits(editPath, edits); err != nil {
					return nil, fmt.Errorf("failed to apply code action edits to %s: %w", editPath, err)
				}
			}
			result["applied"] = true
			result["applied_title"] = action.Title

			// Notify the LSP server about the change
			content, err := os.ReadFile(absPath)
			if err == nil {
				_ = srv.DocMgr.UpdateFile(ctx, absPath, string(content))
			}
		} else {
			result["applied"] = false
			result["message"] = "Selected action has no workspace edits"
		}
	}

	return result, nil
}

// Ensure tool implements the Tool interface
var _ tools.Tool = (*LSPCodeActionsTool)(nil)
