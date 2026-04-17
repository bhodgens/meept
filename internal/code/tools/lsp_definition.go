package tools

import (
	"context"
	"fmt"
	"path/filepath"

	"github.com/caimlas/meept/internal/code/lsp"
	"github.com/caimlas/meept/internal/llm"
	"github.com/caimlas/meept/internal/tools"
)

// LSPDefinitionTool finds the definition of a symbol at a given position.
type LSPDefinitionTool struct {
	manager *lsp.Manager
}

// NewLSPDefinitionTool creates a new LSP definition tool.
func NewLSPDefinitionTool(manager *lsp.Manager) (*LSPDefinitionTool, error) {
	if manager == nil {
		return nil, fmt.Errorf("lsp.Manager cannot be nil")
	}
	return &LSPDefinitionTool{manager: manager}, nil
}

func (t *LSPDefinitionTool) Name() string { return "lsp_goto_definition" }

func (t *LSPDefinitionTool) Description() string {
	return `Find the definition of a symbol at a specific location in code.
Returns the file path, line, and column where the symbol is defined.
Requires an LSP server for the file's language to be configured and running.`
}

func (t *LSPDefinitionTool) Parameters() llm.FunctionParameters {
	return llm.FunctionParameters{
		Type: "object",
		Properties: map[string]llm.ParameterProperty{
			"file_path": {
				Type:        "string",
				Description: "Path to the source file containing the symbol.",
			},
			"line": {
				Type:        "integer",
				Description: "Line number (0-indexed) of the symbol.",
			},
			"character": {
				Type:        "integer",
				Description: "Column/character offset (0-indexed) within the line.",
			},
		},
		Required: []string{"file_path", "line", "character"},
	}
}

func (t *LSPDefinitionTool) Execute(ctx context.Context, args map[string]any) (any, error) {
	filePath, _ := args["file_path"].(string)
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

	// Get definition
	uri := lsp.PathToURI(absPath)
	locations, err := srv.Client.GotoDefinition(ctx, uri, line, char)
	if err != nil {
		return nil, fmt.Errorf("failed to get definition: %w", err)
	}

	if len(locations) == 0 {
		return map[string]any{
			"found":     false,
			"message":   "No definition found at this location",
			"file_path": filePath,
			"line":      line,
			"character": char,
		}, nil
	}

	// Convert locations to result format
	definitions := make([]map[string]any, len(locations))
	for i, loc := range locations {
		definitions[i] = map[string]any{
			"file_path":  lsp.URIToPath(loc.URI),
			"start_line": loc.Range.Start.Line,
			"start_char": loc.Range.Start.Character,
			"end_line":   loc.Range.End.Line,
			"end_char":   loc.Range.End.Character,
		}
	}

	return map[string]any{
		"found":       true,
		"definitions": definitions,
		"count":       len(definitions),
	}, nil
}

// Ensure tool implements the Tool interface
var _ tools.Tool = (*LSPDefinitionTool)(nil)
