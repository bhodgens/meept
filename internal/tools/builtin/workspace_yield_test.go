package builtin

import (
	"context"
	"testing"

	"github.com/caimlas/meept/internal/tools"
)

func TestWorkspaceYieldTool_Name(t *testing.T) {
	tool := NewWorkspaceYieldTool()
	if tool.Name() != "workspace_yield" {
		t.Errorf("Name() = %q, want workspace_yield", tool.Name())
	}
}

func TestWorkspaceYieldTool_Execute_Approve(t *testing.T) {
	tool := NewWorkspaceYieldTool()
	called := false
	tool.SetCallback(func(ctx context.Context, action, feedback string) error {
		called = true
		if action != "approve" {
			t.Errorf("action = %q, want approve", action)
		}
		return nil
	})

	result, err := tool.Execute(context.Background(), map[string]any{
		"action": "approve",
	})
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}
	if !called {
		t.Error("callback should have been called")
	}
	r, ok := result.(WorkspaceYieldResult)
	if !ok {
		t.Fatalf("expected WorkspaceYieldResult, got %T", result)
	}
	if !r.Success {
		t.Errorf("Success = %v, want true", r.Success)
	}
}

func TestWorkspaceYieldTool_Execute_InvalidAction(t *testing.T) {
	tool := NewWorkspaceYieldTool()
	result, err := tool.Execute(context.Background(), map[string]any{
		"action": "invalid",
	})
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}
	if _, ok := result.(*tools.ToolResult); !ok {
		t.Fatalf("expected *ToolResult for error, got %T", result)
	}
}
