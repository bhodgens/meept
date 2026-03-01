package tools

import (
	"context"
	"fmt"
	"path/filepath"

	"github.com/caimlas/meept/internal/code/lsp"
	"github.com/caimlas/meept/internal/llm"
	"github.com/caimlas/meept/internal/tools"
)

// LSPReferencesTool finds all references to a symbol.
type LSPReferencesTool struct {
	manager *lsp.Manager
}

// NewLSPReferencesTool creates a new LSP references tool.
func NewLSPReferencesTool(manager *lsp.Manager) *LSPReferencesTool {
	if manager == nil {
		panic("lsp.Manager cannot be nil")
	}
	return &LSPReferencesTool{manager: manager}
}

func (t *LSPReferencesTool) Name() string { return "lsp_find_references" }

func (t *LSPReferencesTool) Description() string {
	return `Find all references to a symbol at a specific location in code.
Returns all locations where the symbol is used throughout the codebase.
Requires an LSP server for the file's language to be configured and running.`
}

func (t *LSPReferencesTool) Parameters() llm.FunctionParameters {
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
			"include_declaration": {
				Type:        "boolean",
				Description: "Include the declaration in the results (default: true).",
			},
		},
		Required: []string{"file_path", "line", "character"},
	}
}

func (t *LSPReferencesTool) Execute(ctx context.Context, args map[string]any) (any, error) {
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

	includeDecl := true
	if incl, ok := args["include_declaration"].(bool); ok {
		includeDecl = incl
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

	// Find references
	uri := lsp.PathToURI(absPath)
	locations, err := srv.Client.FindReferences(ctx, uri, line, char, includeDecl)
	if err != nil {
		return nil, fmt.Errorf("failed to find references: %w", err)
	}

	if len(locations) == 0 {
		return map[string]any{
			"found":     false,
			"message":   "No references found for symbol at this location",
			"file_path": filePath,
			"line":      line,
			"character": char,
		}, nil
	}

	// Convert locations to result format, grouped by file
	byFile := make(map[string][]map[string]any)
	for _, loc := range locations {
		path := lsp.URIToPath(loc.URI)
		ref := map[string]any{
			"start_line": loc.Range.Start.Line,
			"start_char": loc.Range.Start.Character,
			"end_line":   loc.Range.End.Line,
			"end_char":   loc.Range.End.Character,
		}
		byFile[path] = append(byFile[path], ref)
	}

	// Convert to list format
	references := make([]map[string]any, 0, len(byFile))
	for path, refs := range byFile {
		references = append(references, map[string]any{
			"file_path":  path,
			"references": refs,
			"count":      len(refs),
		})
	}

	return map[string]any{
		"found":       true,
		"files":       references,
		"total_count": len(locations),
		"file_count":  len(references),
	}, nil
}

// Ensure tool implements the Tool interface
var _ tools.Tool = (*LSPReferencesTool)(nil)
