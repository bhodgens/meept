package tools

import (
	"context"
	"fmt"
	"path/filepath"

	"github.com/caimlas/meept/internal/code/lsp"
	"github.com/caimlas/meept/internal/llm"
	"github.com/caimlas/meept/internal/tools"
)

// LSPHoverTool gets hover information (type, documentation) for a symbol.
type LSPHoverTool struct {
	manager *lsp.Manager
}

// NewLSPHoverTool creates a new LSP hover tool.
func NewLSPHoverTool(manager *lsp.Manager) (*LSPHoverTool, error) {
	if manager == nil {
		return nil, fmt.Errorf("lsp.Manager cannot be nil")
	}
	return &LSPHoverTool{manager: manager}, nil
}

func (t *LSPHoverTool) Name() string { return "lsp_hover" }

func (t *LSPHoverTool) Category() string { return "code" }

func (t *LSPHoverTool) Description() string {
	return `Get type information and documentation for a symbol at a specific location.
Returns the symbol's type signature and any associated documentation.
Requires an LSP server for the file's language to be configured and running.`
}

func (t *LSPHoverTool) Parameters() llm.FunctionParameters {
	return llm.FunctionParameters{
		Type: SchemaTypeObject,
		Properties: map[string]llm.ParameterProperty{
			SchemaPropFilePath: {
				Type:        SchemaTypeString,
				Description: "Path to the source file containing the symbol.",
			},
			SchemaPropLine: {
				Type:        SchemaTypeInteger,
				Description: "Line number (0-indexed) of the symbol.",
			},
			SchemaPropCharacter: {
				Type:        SchemaTypeInteger,
				Description: "Column/character offset (0-indexed) within the line.",
			},
		},
		Required: []string{SchemaPropFilePath, "line", "character"},
	}
}

func (t *LSPHoverTool) Execute(ctx context.Context, args map[string]any) (any, error) {
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

	// Get hover information
	uri := lsp.PathToURI(absPath)
	hover, err := srv.Client.Hover(ctx, uri, line, char)
	if err != nil {
		return nil, fmt.Errorf("failed to get hover info: %w", err)
	}

	if hover == nil {
		return map[string]any{
			SchemaPropFound:     false,
			SchemaPropMessage:   "No hover information available at this location",
			SchemaPropFilePath:  filePath,
			SchemaPropLine:      line,
			SchemaPropCharacter: char,
		}, nil
	}

	// Extract content from hover
	content := extractHoverContent(hover)

	result := map[string]any{
		SchemaPropFound: true,
		"content":       content,
	}

	// Add range if available
	if hover.Range != nil {
		result["range"] = map[string]any{
			SchemaPropStartLine: hover.Range.Start.Line,
			SchemaPropStartChar: hover.Range.Start.Character,
			SchemaPropEndLine:   hover.Range.End.Line,
			SchemaPropEndChar:   hover.Range.End.Character,
		}
	}

	return result, nil
}

// extractHoverContent extracts readable content from hover response.
func extractHoverContent(hover *lsp.Hover) string {
	return hover.Contents.Value
}

// Ensure tool implements the Tool interface
var _ tools.Tool = (*LSPHoverTool)(nil)
