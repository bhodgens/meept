package tools

import (
	"context"
	"fmt"

	"github.com/caimlas/meept/internal/code/lsp"
	"github.com/caimlas/meept/internal/llm"
	"github.com/caimlas/meept/internal/tools"
)

// LSPRenameFilesTool handles workspace file rename operations.
// This tool triggers the LSP server's willRenameFiles capability to update
// barrel files, re-exports, and aliased imports when files are renamed.
type LSPRenameFilesTool struct {
	manager *lsp.Manager
}

// NewLSPRenameFilesTool creates a new LSP rename files tool.
func NewLSPRenameFilesTool(manager *lsp.Manager) (*LSPRenameFilesTool, error) {
	if manager == nil {
		return nil, fmt.Errorf("lsp.Manager cannot be nil")
	}
	return &LSPRenameFilesTool{manager: manager}, nil
}

func (t *LSPRenameFilesTool) Name() string { return "lsp_rename_files" }

func (t *LSPRenameFilesTool) Category() string { return "code" }

func (t *LSPRenameFilesTool) Description() string {
	return `Handle workspace file rename operations by notifying LSP servers.
This triggers the willRenameFiles capability to update barrel files, re-exports,
and aliased imports when files are renamed or moved.

Use this tool when:
- Renaming a file that is imported elsewhere
- Moving a file to a different directory
- Updating barrel files (index.ts, mod.rs, __init__.py, etc.)

The tool returns proposed edits without modifying files. Apply the edits manually
or use a follow-up tool to write the changes.

Parameters:
- old_path: The current path of the file being renamed
- new_path: The new path for the file
`
}

func (t *LSPRenameFilesTool) Parameters() llm.FunctionParameters {
	return llm.FunctionParameters{
		Type: SchemaTypeObject,
		Properties: map[string]llm.ParameterProperty{
			"old_path": {
				Type:        SchemaTypeString,
				Description: "The current absolute or relative path of the file being renamed.",
			},
			"new_path": {
				Type:        SchemaTypeString,
				Description: "The new absolute or relative path for the file.",
			},
		},
		Required: []string{"old_path", "new_path"},
	}
}

func (t *LSPRenameFilesTool) Execute(ctx context.Context, args map[string]any) (any, error) {
	oldPath, _ := args["old_path"].(string)
	newPath, _ := args["new_path"].(string)

	if oldPath == "" {
		return nil, fmt.Errorf("old_path is required")
	}
	if newPath == "" {
		return nil, fmt.Errorf("new_path is required")
	}

	// Convert to URIs
	oldURI := lsp.PathToURI(oldPath)
	newURI := lsp.PathToURI(newPath)

	// Call the manager's WillRenameFiles method
	edit, err := t.manager.WillRenameFiles(ctx, oldURI, newURI)
	if err != nil {
		return nil, fmt.Errorf("failed to call willRenameFiles: %w", err)
	}

	if edit == nil {
		return map[string]any{
			SchemaPropFound:   false,
			SchemaPropMessage: "LSP server does not support willRenameFiles or returned no edits",
			"old_path":        oldPath,
			"new_path":        newPath,
		}, nil
	}

	// Build response with proposed changes
	result := map[string]any{
		SchemaPropFound: true,
		"old_path":      oldPath,
		"new_path":      newPath,
	}

	// Include text changes if any
	if edit.Changes != nil && len(edit.Changes) > 0 {
		changes := make([]map[string]any, 0)
		for fileURI, edits := range edit.Changes {
			path := lsp.URIToPath(fileURI)
			editList := make([]map[string]any, len(edits))
			for i, e := range edits {
				editList[i] = map[string]any{
					SchemaPropStartLine: e.Range.Start.Line,
					SchemaPropStartChar: e.Range.Start.Character,
					SchemaPropEndLine:   e.Range.End.Line,
					SchemaPropEndChar:   e.Range.End.Character,
					"new_text":          e.NewText,
				}
			}
			changes = append(changes, map[string]any{
				SchemaPropFilePath: path,
				"edits":            editList,
			})
		}
		result["text_changes"] = changes
		result["change_count"] = len(changes)
	}

	// Include file operations if any
	if edit.FileOperations != nil && len(edit.FileOperations) > 0 {
		ops := make([]map[string]any, len(edit.FileOperations))
		for i, op := range edit.FileOperations {
			ops[i] = map[string]any{
				"kind":   op.Kind,
				"target": op.Target,
			}
			if op.OldURI != "" {
				ops[i]["old_uri"] = op.OldURI
			}
			if op.NewURI != "" {
				ops[i]["new_uri"] = op.NewURI
			}
		}
		result["file_operations"] = ops
		result["operation_count"] = len(ops)
	}

	// Include document changes if any
	if edit.DocumentChanges != nil && len(edit.DocumentChanges) > 0 {
		docChanges := make([]map[string]any, len(edit.DocumentChanges))
		for i, dc := range edit.DocumentChanges {
			docChanges[i] = map[string]any{
				SchemaPropFilePath: lsp.URIToPath(dc.TextDocument.URI),
				"version":          dc.TextDocument.Version,
				"edit_count":       len(dc.Edits),
			}
		}
		result["document_changes"] = docChanges
	}

	return result, nil
}

// Ensure tool implements the Tool interface
var _ tools.Tool = (*LSPRenameFilesTool)(nil)
