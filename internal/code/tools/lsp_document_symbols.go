package tools

import (
	"context"
	"fmt"
	"path/filepath"

	"github.com/caimlas/meept/internal/code/lsp"
	"github.com/caimlas/meept/internal/llm"
	"github.com/caimlas/meept/internal/tools"
)

// LSPDocumentSymbolsTool gets all symbols in a single file.
type LSPDocumentSymbolsTool struct {
	manager *lsp.Manager
}

// NewLSPDocumentSymbolsTool creates a new LSP document symbols tool.
func NewLSPDocumentSymbolsTool(manager *lsp.Manager) (*LSPDocumentSymbolsTool, error) {
	if manager == nil {
		return nil, fmt.Errorf("lsp.Manager cannot be nil")
	}
	return &LSPDocumentSymbolsTool{manager: manager}, nil
}

func (t *LSPDocumentSymbolsTool) Name() string { return "lsp_document_symbols" }

func (t *LSPDocumentSymbolsTool) Description() string {
	return `Get all symbols defined in a source file, organized hierarchically.
Returns functions, types, variables, and other symbols with their locations.
Requires an LSP server for the file's language to be configured and running.`
}

func (t *LSPDocumentSymbolsTool) Parameters() llm.FunctionParameters {
	return llm.FunctionParameters{
		Type: SchemaTypeObject,
		Properties: map[string]llm.ParameterProperty{
			SchemaPropFilePath: {
				Type:        SchemaTypeString,
				Description: "Path to the source file to analyze.",
			},
		},
		Required: []string{SchemaPropFilePath},
	}
}

func (t *LSPDocumentSymbolsTool) Execute(ctx context.Context, args map[string]any) (any, error) {
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

	// Get document symbols
	uri := lsp.PathToURI(absPath)
	symbols, err := srv.Client.DocumentSymbols(ctx, uri)
	if err != nil {
		return nil, fmt.Errorf("failed to get document symbols: %w", err)
	}

	if len(symbols) == 0 {
		return map[string]any{
			SchemaPropFound:    false,
			SchemaPropMessage:  "No symbols found in this file",
			SchemaPropFilePath: filePath,
		}, nil
	}

	// Convert to result format
	result := convertDocumentSymbols(symbols)

	return map[string]any{
		SchemaPropFound:    true,
		SchemaPropFilePath: filePath,
		"symbols":          result,
		SchemaPropCount:    len(result),
	}, nil
}

// Ensure tool implements the Tool interface
var _ tools.Tool = (*LSPDocumentSymbolsTool)(nil)
