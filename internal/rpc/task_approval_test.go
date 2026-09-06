package rpc

import (
	"context"
	"encoding/json"
	"log/slog"
	"path/filepath"
	"testing"
)

func dispatchTaskApproval(t *testing.T, h *TaskApprovalHandler, method string, params map[string]any) (any, error) {
	t.Helper()
	srv := New(&Config{SocketPath: filepath.Join(t.TempDir(), "test.sock")}, nil, slog.Default())
	h.RegisterTaskApprovalMethods(srv)
	raw, err := json.Marshal(params)
	if err != nil {
		t.Fatalf("marshal params: %v", err)
	}
	return srv.CallMethod(context.Background(), method, raw)
}

func TestTaskApprove_RPC(t *testing.T) {
	var approved []string
	h := NewTaskApprovalHandler(func(ctx context.Context, taskID string) error {
		approved = append(approved, taskID)
		return nil
	})

	res, err := dispatchTaskApproval(t, h, "task.approve", map[string]any{"task_id": "task-1"})
	if err != nil {
		t.Fatalf("task.approve: %v", err)
	}
	raw, _ := json.Marshal(res)
	if !jsonContains(raw, `"status":"approved"`) && !jsonContains(raw, `"status": "approved"`) {
		t.Errorf("unexpected response: %s", raw)
	}
	if len(approved) != 1 || approved[0] != "task-1" {
		t.Errorf("approved = %v, want [task-1]", approved)
	}

	// Missing task_id is an error.
	if _, err := dispatchTaskApproval(t, h, "task.approve", map[string]any{}); err == nil {
		t.Error("task.approve without task_id should error")
	}

	// Reject path.
	if _, err := dispatchTaskApproval(t, h, "task.reject", map[string]any{"task_id": "task-2", "reason": "no"}); err != nil {
		t.Fatalf("task.reject: %v", err)
	}
}

func TestTaskApprove_NilApprover(t *testing.T) {
	h := NewTaskApprovalHandler(nil)
	if _, err := dispatchTaskApproval(t, h, "task.approve", map[string]any{"task_id": "task-1"}); err == nil {
		t.Error("nil approver should error, got nil")
	}
}

func jsonContains(b []byte, sub string) bool {
	return len(b) > 0 && contains(string(b), sub)
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
