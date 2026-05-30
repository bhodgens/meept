package tools

import (
	"context"
	"fmt"
	"path/filepath"

	"github.com/caimlas/meept/internal/code/lsp"
	"github.com/caimlas/meept/internal/llm"
	"github.com/caimlas/meept/internal/tools"
)

// LSPImplementationTool finds all implementations of a symbol.
type LSPImplementationTool struct {
	manager *lsp.Manager
}

// NewLSPImplementationTool creates a new LSP implementation tool.
func NewLSPImplementationTool(manager *lsp.Manager) (*LSPImplementationTool, error) {
	if manager == nil {
		return nil, fmt.Errorf("lsp.Manager cannot be nil")
	}
	return &LSPImplementationTool{manager: manager}, nil
}

func (t *LSPImplementationTool) Name() string { return "lsp_implementation" }

func (t *LSPImplementationTool) Category() string { return "code" }

func (t *LSPImplementationTool) Description() string {
	return `Find all implementations of a symbol at a specific location in code.
Returns all locations where the symbol (interface, abstract type, etc.) is implemented.
Requires an LSP server for the file's language to be configured and running.`
}

func (t *LSPImplementationTool) Parameters() llm.FunctionParameters {
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

func (t *LSPImplementationTool) Execute(ctx context.Context, args map[string]any) (any, error) {
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

	// Find implementations
	uri := lsp.PathToURI(absPath)
	locations, err := srv.Client.Implementation(ctx, uri, line, char)
	if err != nil {
		return nil, fmt.Errorf("failed to find implementations: %w", err)
	}

	if len(locations) == 0 {
		return map[string]any{
			SchemaPropFound:     false,
			SchemaPropMessage:   "No implementations found for symbol at this location",
			SchemaPropFilePath:  filePath,
			SchemaPropLine:      line,
			SchemaPropCharacter: char,
		}, nil
	}

	// Convert locations to result format, grouped by file
	byFile := make(map[string][]map[string]any)
	for _, loc := range locations {
		path := lsp.URIToPath(loc.URI)
		impl := map[string]any{
			SchemaPropStartLine: loc.Range.Start.Line,
			SchemaPropStartChar: loc.Range.Start.Character,
			SchemaPropEndLine:   loc.Range.End.Line,
			SchemaPropEndChar:   loc.Range.End.Character,
		}
		byFile[path] = append(byFile[path], impl)
	}

	// Convert to list format
	implementations := make([]map[string]any, 0, len(byFile))
	for path, impls := range byFile {
		implementations = append(implementations, map[string]any{
			SchemaPropFilePath: path,
			"implementations":  impls,
			SchemaPropCount:    len(impls),
		})
	}

	return map[string]any{
		SchemaPropFound: true,
		"files":         implementations,
		"total_count":   len(locations),
		"file_count":    len(implementations),
	}, nil
}

// Ensure tool implements the Tool interface
var _ tools.Tool = (*LSPImplementationTool)(nil)
