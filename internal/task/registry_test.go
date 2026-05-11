package task

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/bus"
	"github.com/caimlas/meept/pkg/models"
)

func newTestRegistry(t *testing.T) (*Registry, *bus.MessageBus) {
	t.Helper()
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "tasks.db")

	msgBus := bus.New(nil, nil)
	reg, err := NewRegistry(dbPath, msgBus, nil)
	if err != nil {
		t.Fatalf("failed to create registry: %v", err)
	}
	t.Cleanup(func() { reg.Close() })
	return reg, msgBus
}

func TestRegistry_CreateAndGet(t *testing.T) {
	reg, _ := newTestRegistry(t)
	ctx := context.Background()

	task, err := reg.Create(ctx, "test-task", "description")
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}
	if task.ID == "" {
		t.Error("expected task ID to be set")
	}
	if task.Name != "test-task" {
		t.Errorf("expected name %q, got %q", "test-task", task.Name)
	}

	// Get it back
	got, err := reg.Get(ctx, task.ID)
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}
	if got == nil {
		t.Fatal("expected task, got nil")
	}
	if got.Name != "test-task" {
		t.Errorf("expected name %q, got %q", "test-task", got.Name)
	}
}

func TestRegistry_Update(t *testing.T) {
	reg, _ := newTestRegistry(t)
	ctx := context.Background()

	task, _ := reg.Create(ctx, "test", "desc")
	task.Name = "updated"
	task.SetState(StateExecuting)

	if err := reg.Update(ctx, task); err != nil {
		t.Fatalf("Update failed: %v", err)
	}

	got, _ := reg.Get(ctx, task.ID)
	if got.Name != "updated" {
		t.Errorf("expected name %q, got %q", "updated", got.Name)
	}
	if got.State != StateExecuting {
		t.Errorf("expected state %q, got %q", StateExecuting, got.State)
	}
}

func TestRegistry_Delete(t *testing.T) {
	reg, _ := newTestRegistry(t)
	ctx := context.Background()

	task, _ := reg.Create(ctx, "test", "desc")

	if err := reg.Delete(ctx, task.ID); err != nil {
		t.Fatalf("Delete failed: %v", err)
	}

	got, err := reg.Get(ctx, task.ID)
	if err == nil {
		t.Fatal("expected error after delete")
	}
	if !errors.Is(err, ErrTaskNotFound) {
		t.Fatalf("expected ErrTaskNotFound, got: %v", err)
	}
	if got != nil {
		t.Error("expected nil after delete")
	}
}

func TestRegistry_List(t *testing.T) {
	reg, _ := newTestRegistry(t)
	ctx := context.Background()

	reg.Create(ctx, "task-1", "desc")
	reg.Create(ctx, "task-2", "desc")
	reg.Create(ctx, "task-3", "desc")

	tasks, err := reg.List(ctx, nil, 10)
	if err != nil {
		t.Fatalf("List failed: %v", err)
	}
	if len(tasks) != 3 {
		t.Errorf("expected 3 tasks, got %d", len(tasks))
	}
}

func TestRegistry_ListWithStateFilter(t *testing.T) {
	reg, _ := newTestRegistry(t)
	ctx := context.Background()

	task1, _ := reg.Create(ctx, "pending-task", "desc")
	task2, _ := reg.Create(ctx, "executing-task", "desc")

	_ = task1
	task2.SetState(StateExecuting)
	reg.Update(ctx, task2)

	pendingState := StatePending
	tasks, err := reg.List(ctx, &pendingState, 10)
	if err != nil {
		t.Fatalf("List with filter failed: %v", err)
	}
	if len(tasks) != 1 {
		t.Errorf("expected 1 pending task, got %d", len(tasks))
	}
}

func TestRegistry_UpdateState(t *testing.T) {
	reg, _ := newTestRegistry(t)
	ctx := context.Background()

	task, _ := reg.Create(ctx, "test", "desc")

	if err := reg.UpdateState(ctx, task.ID, StateExecuting); err != nil {
		t.Fatalf("UpdateState failed: %v", err)
	}

	got, _ := reg.Get(ctx, task.ID)
	if got.State != StateExecuting {
		t.Errorf("expected state %q, got %q", StateExecuting, got.State)
	}
}

func TestRegistry_LinkSession(t *testing.T) {
	reg, _ := newTestRegistry(t)
	ctx := context.Background()

	task, _ := reg.Create(ctx, "test", "desc")

	if err := reg.LinkSession(ctx, task.ID, "session-1"); err != nil {
		t.Fatalf("LinkSession failed: %v", err)
	}

	sessions, err := reg.GetLinkedSessions(ctx, task.ID)
	if err != nil {
		t.Fatalf("GetLinkedSessions failed: %v", err)
	}
	if len(sessions) != 1 || sessions[0] != "session-1" {
		t.Errorf("expected [session-1], got %v", sessions)
	}
}

func TestRegistry_UnlinkSession(t *testing.T) {
	reg, _ := newTestRegistry(t)
	ctx := context.Background()

	task, _ := reg.Create(ctx, "test", "desc")
	reg.LinkSession(ctx, task.ID, "session-1")
	reg.LinkSession(ctx, task.ID, "session-2")

	if err := reg.UnlinkSession(ctx, task.ID, "session-1"); err != nil {
		t.Fatalf("UnlinkSession failed: %v", err)
	}

	sessions, _ := reg.GetLinkedSessions(ctx, task.ID)
	if len(sessions) != 1 || sessions[0] != "session-2" {
		t.Errorf("expected [session-2], got %v", sessions)
	}
}

func TestRegistry_JobTracking(t *testing.T) {
	reg, _ := newTestRegistry(t)
	ctx := context.Background()

	task, _ := reg.Create(ctx, "test", "desc")

	reg.IncrementJobCount(ctx, task.ID)
	reg.IncrementJobCount(ctx, task.ID)
	reg.IncrementJobCount(ctx, task.ID)

	reg.CompleteJob(ctx, task.ID)
	reg.CompleteJob(ctx, task.ID)

	got, _ := reg.Get(ctx, task.ID)
	if got.TotalJobs != 3 {
		t.Errorf("expected 3 total jobs, got %d", got.TotalJobs)
	}
	if got.CompletedJobs != 2 {
		t.Errorf("expected 2 completed jobs, got %d", got.CompletedJobs)
	}
}

func TestRegistry_AutoComplete(t *testing.T) {
	reg, _ := newTestRegistry(t)
	ctx := context.Background()

	task, _ := reg.Create(ctx, "test", "desc")
	reg.IncrementJobCount(ctx, task.ID)
	reg.CompleteJob(ctx, task.ID)

	got, _ := reg.Get(ctx, task.ID)
	if got.State != StateCompleted {
		t.Errorf("expected task to auto-complete, got state %q", got.State)
	}
}

func TestRegistry_ClosedOperations(t *testing.T) {
	reg, _ := newTestRegistry(t)
	ctx := context.Background()

	reg.Close()

	_, err := reg.Create(ctx, "test", "desc")
	if err == nil {
		t.Error("expected error on closed registry")
	}

	err = reg.Update(ctx, NewTask("test", "desc"))
	if err == nil {
		t.Error("expected error on closed registry")
	}

	err = reg.Delete(ctx, "test-id")
	if err == nil {
		t.Error("expected error on closed registry")
	}
}

func TestRegistry_ListSummaries(t *testing.T) {
	reg, _ := newTestRegistry(t)
	ctx := context.Background()

	reg.Create(ctx, "task-1", "desc1")
	reg.Create(ctx, "task-2", "desc2")

	summaries, err := reg.ListSummaries(ctx, 10)
	if err != nil {
		t.Fatalf("ListSummaries failed: %v", err)
	}
	if len(summaries) != 2 {
		t.Errorf("expected 2 summaries, got %d", len(summaries))
	}
}

// TestHandler_MessageRouting tests that the Handler correctly routes messages.
func TestHandler_MessageRouting(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "tasks.db")

	msgBus := bus.New(nil, nil)
	reg, err := NewRegistry(dbPath, msgBus, nil)
	if err != nil {
		t.Fatalf("failed to create registry: %v", err)
	}
	defer reg.Close()

	handler := NewHandler(reg, msgBus, nil)

	ctx := t.Context()

	if err := handler.Start(ctx); err != nil {
		t.Fatalf("failed to start handler: %v", err)
	}
	defer handler.Stop(ctx)

	// Subscribe to responses
	respSub := msgBus.Subscribe("test-resp", "task.result")

	// Create a task via bus message
	createPayload, _ := json.Marshal(map[string]string{
		"name":        "bus-task",
		"description": "created via bus",
	})

	createMsg := &models.BusMessage{
		ID:        "test-create-1",
		Type:      models.MessageTypeRequest,
		Topic:     "task.create",
		Source:    "test",
		Timestamp: time.Now().UTC(),
		Payload:   createPayload,
	}

	msgBus.Publish("task.create", createMsg)

	// Wait for response
	select {
	case resp := <-respSub.Channel:
		if resp.ReplyTo != "test-create-1" {
			t.Errorf("expected reply_to %q, got %q", "test-create-1", resp.ReplyTo)
		}
		// Verify it's not an error
		var result map[string]any
		json.Unmarshal(resp.Payload, &result)
		if _, hasErr := result["error"]; hasErr {
			t.Errorf("got error response: %s", string(resp.Payload))
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for response")
	}
}

// TestHandler_ListViabus tests listing tasks via the message bus.
func TestHandler_ListViaBus(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "tasks.db")

	_ = os.MkdirAll(tmpDir, 0o755)

	msgBus := bus.New(nil, nil)
	reg, err := NewRegistry(dbPath, msgBus, nil)
	if err != nil {
		t.Fatalf("failed to create registry: %v", err)
	}
	defer reg.Close()

	// Pre-create some tasks
	ctx := context.Background()
	reg.Create(ctx, "task-a", "desc")
	reg.Create(ctx, "task-b", "desc")

	handler := NewHandler(reg, msgBus, nil)
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	handler.Start(ctx)
	defer handler.Stop(ctx)

	respSub := msgBus.Subscribe("test-list", "task.result")

	listPayload, _ := json.Marshal(map[string]any{"limit": 10})
	listMsg := &models.BusMessage{
		ID:        "test-list-1",
		Type:      models.MessageTypeRequest,
		Topic:     "task.list",
		Source:    "test",
		Timestamp: time.Now().UTC(),
		Payload:   listPayload,
	}

	msgBus.Publish("task.list", listMsg)

	select {
	case resp := <-respSub.Channel:
		var tasks []any
		json.Unmarshal(resp.Payload, &tasks)
		if len(tasks) < 2 {
			t.Errorf("expected at least 2 tasks, got %d (payload: %s)", len(tasks), string(resp.Payload))
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for list response")
	}
}
