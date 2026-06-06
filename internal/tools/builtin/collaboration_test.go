package builtin

import (
	"context"
	"errors"
	"testing"

	"github.com/caimlas/meept/internal/tools"
)

func TestInitiateCollaborationTool_Name(t *testing.T) {
	tool := NewInitiateCollaborationTool()
	if tool.Name() != "initiate_collaboration" {
		t.Errorf("Name() = %q, want initiate_collaboration", tool.Name())
	}
}

func TestInitiateCollaborationTool_Execute_Success(t *testing.T) {
	tool := NewInitiateCollaborationTool()
	tool.SetCallback(func(ctx context.Context, mode, taskDesc, reason string, preferredAgents []string) (string, error) {
		if mode != "pair_programming" {
			t.Errorf("mode = %q, want pair_programming", mode)
		}
		return "collab-abc-123", nil
	})

	result, err := tool.Execute(context.Background(), map[string]any{
		"mode":             "pair_programming",
		"task_description": "refactor the auth module",
		"reason":           "uncertain about best approach",
		"preferred_agents": []any{"planner"},
	})
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}
	r, ok := result.(InitiateCollaborationResult)
	if !ok {
		t.Fatalf("expected InitiateCollaborationResult, got %T", result)
	}
	if !r.Success {
		t.Errorf("Success = %v, want true", r.Success)
	}
	if r.SessionID != "collab-abc-123" {
		t.Errorf("SessionID = %q, want collab-abc-123", r.SessionID)
	}
}

func TestInitiateCollaborationTool_Execute_InvalidMode(t *testing.T) {
	tool := NewInitiateCollaborationTool()
	result, err := tool.Execute(context.Background(), map[string]any{
		"mode":             "invalid",
		"task_description": "test",
		"reason":           "test",
	})
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}
	if _, ok := result.(*tools.ToolResult); !ok {
		t.Fatalf("expected *ToolResult for error, got %T", result)
	}
}

func TestInitiateCollaborationTool_Execute_MissingDescription(t *testing.T) {
	tool := NewInitiateCollaborationTool()
	result, err := tool.Execute(context.Background(), map[string]any{
		"mode":   "pair_programming",
		"reason": "testing",
	})
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}
	if _, ok := result.(*tools.ToolResult); !ok {
		t.Fatalf("expected *ToolResult for error, got %T", result)
	}
}

func TestInitiateCollaborationTool_Execute_CallbackError(t *testing.T) {
	tool := NewInitiateCollaborationTool()
	tool.SetCallback(func(ctx context.Context, mode, taskDesc, reason string, preferredAgents []string) (string, error) {
		return "", errors.New("engine busy")
	})

	result, err := tool.Execute(context.Background(), map[string]any{
		"mode":             "pair_programming",
		"task_description": "test",
		"reason":           "test",
	})
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}
	r, ok := result.(InitiateCollaborationResult)
	if !ok {
		t.Fatalf("expected InitiateCollaborationResult, got %T", result)
	}
	if r.Success {
		t.Error("expected failure when callback returns error")
	}
}
