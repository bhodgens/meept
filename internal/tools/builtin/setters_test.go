package builtin

import (
	"context"
	"testing"
)

// TestAllSetters_NilSafe verifies that all Set* methods on tool structs
// are nil-safe and do not panic when called with nil arguments.
func TestAllSetters_NilSafe(t *testing.T) {
	tests := []struct {
		name    string
		setFunc func()
	}{
		{"ReadFileTool.SetFenceChecker", func() { (&ReadFileTool{}).SetFenceChecker(nil) }},
		{"WriteFileTool.SetFenceChecker", func() { (&WriteFileTool{}).SetFenceChecker(nil) }},
		{"DeleteFileTool.SetFenceChecker", func() { (&DeleteFileTool{}).SetFenceChecker(nil) }},
		{"ListDirectoryTool.SetFenceChecker", func() { (&ListDirectoryTool{}).SetFenceChecker(nil) }},
		{"FileFindTool.SetFenceChecker", func() { (&FileFindTool{}).SetFenceChecker(nil) }},
		{"FileGrepTool.SetFenceChecker", func() { (&FileGrepTool{}).SetFenceChecker(nil) }},
		{"WebFetchTool.SetSecurityOrchestrator", func() { (&WebFetchTool{}).SetSecurityOrchestrator(nil) }},
		{"WorkspaceYieldTool.SetCallback", func() { (&WorkspaceYieldTool{}).SetCallback(nil) }},
		{"InitiateCollaborationTool.SetCallback", func() { (&InitiateCollaborationTool{}).SetCallback(nil) }},
		{"TeamCreateTool.SetCallback", func() { (&TeamCreateTool{}).SetCallback(nil) }},
		{"TeamAssignTool.SetCallback", func() { (&TeamAssignTool{}).SetCallback(nil) }},
		{"TeamStatusTool.SetCallback", func() { (&TeamStatusTool{}).SetCallback(nil) }},
		{"TeamMessageTool.SetCallback", func() { (&TeamMessageTool{}).SetCallback(nil) }},
		{"TeamResultTool.SetCallback", func() { (&TeamResultTool{}).SetCallback(nil) }},
		{"TeamPresetCreateTool.SetCallback", func() { (&TeamPresetCreateTool{}).SetCallback(nil) }},
		{"ShellExecuteTool.SetFenceChecker", func() { (&ShellExecuteTool{}).SetFenceChecker(nil) }},
		{"ShellExecuteTool.SetSecurityOrchestrator", func() { (&ShellExecuteTool{}).SetSecurityOrchestrator(nil) }},
		{"FileEditTool.SetFenceChecker", func() { (&FileEditTool{}).SetFenceChecker(nil) }},
		{"FileEditTool.SetLSPNotifier", func() { (&FileEditTool{}).SetLSPNotifier(nil) }},
		{"ResolveTool.SetFenceChecker", func() { (&ResolveTool{}).SetFenceChecker(nil) }},
		{"AskTool.SetResponseFunc", func() { (&AskTool{}).SetResponseFunc(nil) }},
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

// TestSetters_WithValues verifies that setters work correctly with non-nil values.
func TestSetters_WithValues(t *testing.T) {
	t.Run("WorkspaceYieldTool.SetCallback", func(t *testing.T) {
		tool := &WorkspaceYieldTool{}
		cb := func(ctx context.Context, action, feedback string) error { return nil }
		tool.SetCallback(cb)
		if tool.callback == nil {
			t.Error("SetCallback did not set the value")
		}
	})
}
