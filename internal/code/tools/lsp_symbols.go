package tools

import (
	"context"
	"fmt"
	"path/filepath"

	"github.com/caimlas/meept/internal/code/lsp"
	"github.com/caimlas/meept/internal/llm"
	"github.com/caimlas/meept/internal/tools"
)

// LSPSymbolsTool searches for symbols in the workspace or document.
type LSPSymbolsTool struct {
	manager *lsp.Manager
}

// NewLSPSymbolsTool creates a new LSP symbols tool.
func NewLSPSymbolsTool(manager *lsp.Manager) *LSPSymbolsTool {
	if manager == nil {
		panic("lsp.Manager cannot be nil")
	}
	return &LSPSymbolsTool{manager: manager}
}

func (t *LSPSymbolsTool) Name() string { return "lsp_workspace_symbols" }

func (t *LSPSymbolsTool) Description() string {
	return `Search for symbols across the workspace or in a specific document.
For workspace search, provide a query string to search globally.
For document symbols, provide a file path to get all symbols in that file.
Requires an LSP server for the language to be configured and running.`
}

func (t *LSPSymbolsTool) Parameters() llm.FunctionParameters {
	return llm.FunctionParameters{
		Type: "object",
		Properties: map[string]llm.ParameterProperty{
			"query": {
				Type:        "string",
				Description: "Search query for workspace symbols. Required if file_path is not provided.",
			},
			"file_path": {
				Type:        "string",
				Description: "Path to file for document symbols. If provided, returns all symbols in this file.",
			},
			"language": {
				Type:        "string",
				Description: "Language ID for workspace search (e.g., 'go', 'python', 'typescript'). Required for workspace search.",
			},
		},
		Required: []string{},
	}
}

func (t *LSPSymbolsTool) Execute(ctx context.Context, args map[string]any) (any, error) {
	query, _ := args["query"].(string)
	filePath, _ := args["file_path"].(string)
	language, _ := args["language"].(string)

	// Document symbols mode
	if filePath != "" {
		return t.documentSymbols(ctx, filePath)
	}

	// Workspace symbols mode
	if query == "" {
		return nil, fmt.Errorf("either file_path or query is required")
	}
	if language == "" {
		return nil, fmt.Errorf("language is required for workspace symbol search")
	}

	return t.workspaceSymbols(ctx, query, language)
}

func (t *LSPSymbolsTool) documentSymbols(ctx context.Context, filePath string) (any, error) {
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

	// Convert to result format
	result := convertDocumentSymbols(symbols)

	return map[string]any{
		"mode":      "document",
		"file_path": filePath,
		"symbols":   result,
		"count":     len(result),
	}, nil
}

func (t *LSPSymbolsTool) workspaceSymbols(ctx context.Context, query, language string) (any, error) {
	srv, err := t.manager.GetServerForLanguage(ctx, language)
	if err != nil {
		return nil, fmt.Errorf("no LSP server for language %s: %w", language, err)
	}
	if srv.Client == nil {
		return nil, fmt.Errorf("LSP server for %s is not fully initialized", language)
	}

	symbols, err := srv.Client.WorkspaceSymbols(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("failed to search workspace symbols: %w", err)
	}

	// Convert to result format
	result := make([]map[string]any, len(symbols))
	for i, sym := range symbols {
		result[i] = map[string]any{
			"name":       sym.Name,
			"kind":       symbolKindToString(sym.Kind),
			"file_path":  lsp.URIToPath(sym.Location.URI),
			"start_line": sym.Location.Range.Start.Line,
			"start_char": sym.Location.Range.Start.Character,
			"end_line":   sym.Location.Range.End.Line,
			"end_char":   sym.Location.Range.End.Character,
		}
		if sym.ContainerName != "" {
			result[i]["container"] = sym.ContainerName
		}
	}

	return map[string]any{
		"mode":     "workspace",
		"query":    query,
		"language": language,
		"symbols":  result,
		"count":    len(result),
	}, nil
}

func convertDocumentSymbols(symbols []lsp.DocumentSymbol) []map[string]any {
	result := make([]map[string]any, len(symbols))
	for i, sym := range symbols {
		item := map[string]any{
			"name":       sym.Name,
			"kind":       symbolKindToString(sym.Kind),
			"start_line": sym.Range.Start.Line,
			"start_char": sym.Range.Start.Character,
			"end_line":   sym.Range.End.Line,
			"end_char":   sym.Range.End.Character,
		}
		if sym.Detail != "" {
			item["detail"] = sym.Detail
		}
		if len(sym.Children) > 0 {
			item["children"] = convertDocumentSymbols(sym.Children)
		}
		result[i] = item
	}
	return result
}

func symbolKindToString(kind lsp.SymbolKind) string {
	switch kind {
	case lsp.SymbolKindFile:
		return "file"
	case lsp.SymbolKindModule:
		return "module"
	case lsp.SymbolKindNamespace:
		return "namespace"
	case lsp.SymbolKindPackage:
		return "package"
	case lsp.SymbolKindClass:
		return "class"
	case lsp.SymbolKindMethod:
		return "method"
	case lsp.SymbolKindProperty:
		return "property"
	case lsp.SymbolKindField:
		return "field"
	case lsp.SymbolKindConstructor:
		return "constructor"
	case lsp.SymbolKindEnum:
		return "enum"
	case lsp.SymbolKindInterface:
		return "interface"
	case lsp.SymbolKindFunction:
		return "function"
	case lsp.SymbolKindVariable:
		return "variable"
	case lsp.SymbolKindConstant:
		return "constant"
	case lsp.SymbolKindString:
		return "string"
	case lsp.SymbolKindNumber:
		return "number"
	case lsp.SymbolKindBoolean:
		return "boolean"
	case lsp.SymbolKindArray:
		return "array"
	case lsp.SymbolKindObject:
		return "object"
	case lsp.SymbolKindKey:
		return "key"
	case lsp.SymbolKindNull:
		return "null"
	case lsp.SymbolKindEnumMember:
		return "enum_member"
	case lsp.SymbolKindStruct:
		return "struct"
	case lsp.SymbolKindEvent:
		return "event"
	case lsp.SymbolKindOperator:
		return "operator"
	case lsp.SymbolKindTypeParameter:
		return "type_parameter"
	default:
		return "unknown"
	}
}

// Ensure tool implements the Tool interface
var _ tools.Tool = (*LSPSymbolsTool)(nil)
