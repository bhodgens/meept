package handlers

import (
	"fmt"
	"testing"
)

func TestTaskEventHandler_NoRateLimiting(t *testing.T) {
	h := NewTaskEventHandler()

	// Send 10 progress events rapidly
	payloads := make([]map[string]any, 10)
	for i := range 10 {
		payloads[i] = map[string]any{
			"task_id":        "task-1",
			"current_step":   fmt.Sprintf("step %d", i),
			"completed_jobs": i,
			"total_jobs":     10,
		}
	}

	// All should produce notifications (no rate limiting)
	produced := 0
	for _, p := range payloads {
		if notif := h.HandleTaskProgress(p); notif != nil {
			produced++
		}
	}

	if produced != 10 {
		t.Errorf("expected 10 notifications, got %d (rate limiting active)", produced)
	}
}

func TestTaskEventHandler_HandleTaskProgress(t *testing.T) {
	h := NewTaskEventHandler()

	payload := map[string]any{
		"task_id":        "task-1",
		"current_step":   "building project",
		"completed_jobs": 5,
		"total_jobs":     10,
	}

	notif := h.HandleTaskProgress(payload)
	if notif == nil {
		t.Fatal("expected notification, got nil")
	}

	if notif.Type != "progress" {
		t.Errorf("expected type 'progress', got %q", notif.Type)
	}

	if notif.TaskID != "task-1" {
		t.Errorf("expected task_id 'task-1', got %q", notif.TaskID)
	}

	expectedMsg := "task progress [5/10]: building project"
	if notif.Message != expectedMsg {
		t.Errorf("expected message %q, got %q", expectedMsg, notif.Message)
	}
}

func TestTaskEventHandler_HandleTaskProgress_NoTotal(t *testing.T) {
	h := NewTaskEventHandler()

	payload := map[string]any{
		"task_id":      "task-1",
		"current_step": "thinking",
	}

	notif := h.HandleTaskProgress(payload)
	if notif == nil {
		t.Fatal("expected notification, got nil")
	}

	expectedMsg := "task progress: thinking"
	if notif.Message != expectedMsg {
		t.Errorf("expected message %q, got %q", expectedMsg, notif.Message)
	}
}

func TestTaskEventHandler_HandleTaskProgress_EmptyStep(t *testing.T) {
	h := NewTaskEventHandler()

	payload := map[string]any{
		"task_id":      "task-1",
		"current_step": "",
	}

	notif := h.HandleTaskProgress(payload)
	if notif != nil {
		t.Error("expected nil notification for empty step")
	}
}

func TestTaskEventHandler_HandleTaskCompleted(t *testing.T) {
	h := NewTaskEventHandler()

	payload := map[string]any{
		"task_id":        "task-1",
		"name":           "My Task",
		"result":         "Task completed successfully",
		"execution_time": "5s",
	}

	notif := h.HandleTaskCompleted(payload)
	if notif == nil {
		t.Fatal("expected notification, got nil")
	}

	if notif.Type != "completed" {
		t.Errorf("expected type 'completed', got %q", notif.Type)
	}
}

func TestTaskEventHandler_HandleTaskFailed(t *testing.T) {
	h := NewTaskEventHandler()

	payload := map[string]any{
		"task_id":        "task-1",
		"name":           "My Task",
		"error":          "connection refused",
		"failed_jobs":    2,
		"completed_jobs": 3,
		"total_jobs":     5,
	}

	notif := h.HandleTaskFailed(payload)
	if notif == nil {
		t.Fatal("expected notification, got nil")
	}

	if notif.Type != "failed" {
		t.Errorf("expected type 'failed', got %q", notif.Type)
	}
}

func TestTaskEventHandler_HandleStepCompleted(t *testing.T) {
	h := NewTaskEventHandler()

	payload := map[string]any{
		"task_id":     "task-1",
		"description": "Write unit tests",
		"result":      "Created 5 test files",
	}

	notif := h.HandleStepCompleted(payload)
	if notif == nil {
		t.Fatal("expected notification, got nil")
	}

	if notif.Type != "step_completed" {
		t.Errorf("expected type 'step_completed', got %q", notif.Type)
	}
}

func TestTaskEventHandler_HandleStepCompleted_EmptyDescription(t *testing.T) {
	h := NewTaskEventHandler()

	payload := map[string]any{
		"task_id": "task-1",
	}

	notif := h.HandleStepCompleted(payload)
	if notif != nil {
		t.Error("expected nil notification for empty description")
	}
}
