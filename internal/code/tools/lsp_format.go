package tools

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/caimlas/meept/internal/code/lsp"
	"github.com/caimlas/meept/internal/llm"
	"github.com/caimlas/meept/internal/tools"
)

// LSPFormatTool formats a source file using the LSP server.
type LSPFormatTool struct {
	manager *lsp.Manager
}

// NewLSPFormatTool creates a new LSP format tool.
func NewLSPFormatTool(manager *lsp.Manager) (*LSPFormatTool, error) {
	if manager == nil {
		return nil, fmt.Errorf("lsp.Manager cannot be nil")
	}
	return &LSPFormatTool{manager: manager}, nil
}

func (t *LSPFormatTool) Name() string { return "lsp_format" }

func (t *LSPFormatTool) Category() string { return "code" }

func (t *LSPFormatTool) Description() string {
	return `Format a source file using the configured LSP server's formatter.
Applies formatting edits to the file and returns a summary of changes.
Requires an LSP server for the file's language to be configured and running.`
}

func (t *LSPFormatTool) Parameters() llm.FunctionParameters {
	return llm.FunctionParameters{
		Type: SchemaTypeObject,
		Properties: map[string]llm.ParameterProperty{
			SchemaPropFilePath: {
				Type:        SchemaTypeString,
				Description: "Path to the source file to format.",
			},
		},
		Required: []string{SchemaPropFilePath},
	}
}

func (t *LSPFormatTool) Execute(ctx context.Context, args map[string]any) (any, error) {
	filePath, _ := args[SchemaPropFilePath].(string)
	if filePath == "" {
		return nil, fmt.Errorf("file_path is required")
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

	// Get formatting edits
	uri := lsp.PathToURI(absPath)
	edits, err := srv.Client.Formatting(ctx, uri)
	if err != nil {
		return nil, fmt.Errorf("failed to get formatting edits: %w", err)
	}

	if edits == nil || len(edits) == 0 {
		return map[string]any{
			SchemaPropFound:    false,
			SchemaPropMessage:  "File is already formatted or formatter returned no changes",
			SchemaPropFilePath: filePath,
		}, nil
	}

	// Apply formatting edits
	if err := applyTextEdits(absPath, edits); err != nil {
		return nil, fmt.Errorf("failed to apply formatting edits: %w", err)
	}

	// Notify the LSP server about the change
	content, err := os.ReadFile(absPath)
	if err == nil {
		_ = srv.DocMgr.UpdateFile(ctx, absPath, string(content))
	}

	// Build summary of changes
	editSummary := make([]map[string]any, len(edits))
	for i, edit := range edits {
		editSummary[i] = map[string]any{
			SchemaPropStartLine: edit.Range.Start.Line,
			SchemaPropStartChar: edit.Range.Start.Character,
			SchemaPropEndLine:   edit.Range.End.Line,
			SchemaPropEndChar:   edit.Range.End.Character,
		}
	}

	// Count lines changed
	linesChanged := make(map[int]bool)
	for _, edit := range edits {
		for line := edit.Range.Start.Line; line <= edit.Range.End.Line; line++ {
			linesChanged[line] = true
		}
	}

	return map[string]any{
		SchemaPropFound:    true,
		SchemaPropFilePath: filePath,
		"applied":          true,
		"edit_count":       len(edits),
		"lines_changed":    len(linesChanged),
		"edits":            editSummary,
	}, nil
}


// Ensure tool implements the Tool interface
var _ tools.Tool = (*LSPFormatTool)(nil)
