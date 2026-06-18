package tools

import (
	"testing"

	"github.com/caimlas/meept/internal/tools/builtin"
)

// TestAllSetters_NilSafe verifies that every Set* method on code/tools-package
// structs that accepts a pointer, interface, slice, map, or func argument is
// nil-safe. See CLAUDE.md "Setter methods" coding practice.
func TestAllSetters_NilSafe(t *testing.T) {
	// Setters under test only assign a field (with a nil guard), so a
	// zero-value instance is sufficient to exercise nil-safety.
	astEdit := &ASTEditTool{}
	lspRename := &LSPRenameTool{}
	resolveEdit := &ResolveASTEditTool{}

	tests := []struct {
		name    string
		setFunc func()
	}{
		// ASTEditTool setters (internal/code/tools/ast_edit.go)
		{"ASTEditTool.SetPendingChangesRegistry", func() {
			astEdit.SetPendingChangesRegistry((*builtin.PendingChangesRegistry)(nil))
		}},
		{"ASTEditTool.SetFenceChecker", func() { astEdit.SetFenceChecker(nil) }},

		// LSPRenameTool setters (internal/code/tools/lsp_rename.go)
		{"LSPRenameTool.SetPendingChangesRegistry", func() {
			lspRename.SetPendingChangesRegistry((*builtin.PendingChangesRegistry)(nil))
		}},

		// ResolveASTEditTool setters (internal/code/tools/resolve_ast_edit.go)
		{"ResolveASTEditTool.SetFenceChecker", func() { resolveEdit.SetFenceChecker(nil) }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			defer func() {
				if r := recover(); r != nil {
					t.Errorf("Set method panicked on nil: %v", r)
				}
			}()
			tt.setFunc()
		})
	}
}
