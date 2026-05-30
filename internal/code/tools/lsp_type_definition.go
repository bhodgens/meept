package tools

import (
	"context"
	"fmt"
	"path/filepath"

	"github.com/caimlas/meept/internal/code/lsp"
	"github.com/caimlas/meept/internal/llm"
	"github.com/caimlas/meept/internal/tools"
)

// LSPTypeDefinitionTool finds the type definition of a symbol at a given position.
type LSPTypeDefinitionTool struct {
	manager *lsp.Manager
}

// NewLSPTypeDefinitionTool creates a new LSP type definition tool.
func NewLSPTypeDefinitionTool(manager *lsp.Manager) (*LSPTypeDefinitionTool, error) {
	if manager == nil {
		return nil, fmt.Errorf("lsp.Manager cannot be nil")
	}
	return &LSPTypeDefinitionTool{manager: manager}, nil
}

func (t *LSPTypeDefinitionTool) Name() string { return "lsp_type_definition" }

func (t *LSPTypeDefinitionTool) Category() string { return "code" }

func (t *LSPTypeDefinitionTool) Description() string {
	return `Find the type definition of a symbol at a specific location in code.
Returns the file path, line, and column where the symbol's type is defined.
Requires an LSP server for the file's language to be configured and running.`
}

func (t *LSPTypeDefinitionTool) Parameters() llm.FunctionParameters {
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

func (t *LSPTypeDefinitionTool) Execute(ctx context.Context, args map[string]any) (any, error) {
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

	// Get type definition
	uri := lsp.PathToURI(absPath)
	locations, err := srv.Client.TypeDefinition(ctx, uri, line, char)
	if err != nil {
		return nil, fmt.Errorf("failed to get type definition: %w", err)
	}

	if len(locations) == 0 {
		return map[string]any{
			SchemaPropFound:     false,
			SchemaPropMessage:   "No type definition found at this location",
			SchemaPropFilePath:  filePath,
			SchemaPropLine:      line,
			SchemaPropCharacter: char,
		}, nil
	}

	// Convert locations to result format
	definitions := make([]map[string]any, len(locations))
	for i, loc := range locations {
		definitions[i] = map[string]any{
			SchemaPropFilePath:  lsp.URIToPath(loc.URI),
			SchemaPropStartLine: loc.Range.Start.Line,
			SchemaPropStartChar: loc.Range.Start.Character,
			SchemaPropEndLine:   loc.Range.End.Line,
			SchemaPropEndChar:   loc.Range.End.Character,
		}
	}

	return map[string]any{
		SchemaPropFound: true,
		"definitions":   definitions,
		SchemaPropCount: len(definitions),
	}, nil
}

// Ensure tool implements the Tool interface
var _ tools.Tool = (*LSPTypeDefinitionTool)(nil)
