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

// LSPRenameTool renames a symbol across the workspace.
type LSPRenameTool struct {
	manager *lsp.Manager
}

// NewLSPRenameTool creates a new LSP rename tool.
func NewLSPRenameTool(manager *lsp.Manager) (*LSPRenameTool, error) {
	if manager == nil {
		return nil, fmt.Errorf("lsp.Manager cannot be nil")
	}
	return &LSPRenameTool{manager: manager}, nil
}

func (t *LSPRenameTool) Name() string { return "lsp_rename" }

func (t *LSPRenameTool) Description() string {
	return `Rename a symbol at a specific location and update all references across the workspace.
If apply is true, the changes are written to disk. If false, returns the planned changes without modifying files.
Requires an LSP server for the file's language to be configured and running.`
}

func (t *LSPRenameTool) Parameters() llm.FunctionParameters {
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
			"new_name": {
				Type:        SchemaTypeString,
				Description: "The new name for the symbol.",
			},
			"apply": {
				Type:        SchemaTypeBoolean,
				Description: "Whether to apply the rename to disk (default: true). If false, returns the planned changes.",
			},
		},
		Required: []string{SchemaPropFilePath, "line", "character", "new_name"},
	}
}

func (t *LSPRenameTool) Execute(ctx context.Context, args map[string]any) (any, error) {
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

	newName, _ := args["new_name"].(string)
	if newName == "" {
		return nil, fmt.Errorf("new_name is required")
	}

	apply := true
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

	// Get rename edits
	uri := lsp.PathToURI(absPath)
	workspaceEdit, err := srv.Client.Rename(ctx, uri, line, char, newName)
	if err != nil {
		return nil, fmt.Errorf("failed to get rename edits: %w", err)
	}

	if workspaceEdit == nil || len(workspaceEdit.Changes) == 0 {
		return map[string]any{
			SchemaPropFound:     false,
			SchemaPropMessage:   "No rename edits returned for this location",
			SchemaPropFilePath:  filePath,
			SchemaPropLine:      line,
			SchemaPropCharacter: char,
		}, nil
	}

	// Build changes summary
	changes := make([]map[string]any, 0)
	totalEdits := 0
	for fileURI, edits := range workspaceEdit.Changes {
		path := lsp.URIToPath(fileURI)
		editList := make([]map[string]any, len(edits))
		for i, edit := range edits {
			editList[i] = map[string]any{
				SchemaPropStartLine: edit.Range.Start.Line,
				SchemaPropStartChar: edit.Range.Start.Character,
				SchemaPropEndLine:   edit.Range.End.Line,
				SchemaPropEndChar:   edit.Range.End.Character,
				"new_text":          edit.NewText,
			}
		}
		changes = append(changes, map[string]any{
			SchemaPropFilePath: path,
			"edits":            editList,
			SchemaPropCount:    len(editList),
		})
		totalEdits += len(edits)
	}

	// Apply edits if requested
	if apply {
		for fileURI, edits := range workspaceEdit.Changes {
			path := lsp.URIToPath(fileURI)
			if err := applyTextEdits(path, edits); err != nil {
				return nil, fmt.Errorf("failed to apply edits to %s: %w", path, err)
			}
		}
	}

	return map[string]any{
		SchemaPropFound: true,
		"applied":       apply,
		"new_name":      newName,
		"changes":       changes,
		"file_count":    len(workspaceEdit.Changes),
		"edit_count":    totalEdits,
	}, nil
}

// applyTextEdits applies text edits to a file on disk.
func applyTextEdits(filePath string, edits []lsp.TextEdit) error {
	content, err := os.ReadFile(filePath)
	if err != nil {
		return fmt.Errorf("failed to read file: %w", err)
	}

	lines := strings.Split(string(content), "\n")

	// Apply edits in reverse order to preserve positions
	for i := len(edits) - 1; i >= 0; i-- {
		edit := edits[i]
		startLine := edit.Range.Start.Line
		startChar := edit.Range.Start.Character
		endLine := edit.Range.End.Line
		endChar := edit.Range.End.Character

		if startLine >= len(lines) || endLine >= len(lines) {
			return fmt.Errorf("edit range out of bounds: line %d-%d, file has %d lines", startLine, endLine, len(lines))
		}

		// Build new content
		var before, after strings.Builder
		for l := 0; l < startLine; l++ {
			if l > 0 {
				before.WriteString("\n")
			}
			before.WriteString(lines[l])
		}
		if startLine < len(lines) {
			before.WriteString(lines[startLine][:min(startChar, len(lines[startLine]))])
		}

		if endLine < len(lines) {
			after.WriteString(lines[endLine][min(endChar, len(lines[endLine])):])
		}
		for l := endLine + 1; l < len(lines); l++ {
			after.WriteString("\n")
			after.WriteString(lines[l])
		}

		newContent := before.String() + edit.NewText + after.String()
		lines = strings.Split(newContent, "\n")
	}

	return os.WriteFile(filePath, []byte(strings.Join(lines, "\n")), 0o644)
}

// Ensure tool implements the Tool interface
var _ tools.Tool = (*LSPRenameTool)(nil)
