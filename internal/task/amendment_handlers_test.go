package task

import (
	"context"
	"encoding/json"
	"log/slog"
	"os"
	"path/filepath"
	"testing"

	"github.com/caimlas/meept/internal/bus"
	"github.com/caimlas/meept/internal/queue"
)

func setupTestHandlers(t *testing.T) (*AmendmentHandlers, *Registry, func()) {
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	msgBus := bus.New(nil, logger)

	tmpDir := t.TempDir()
	registry := setupTestRegistry(t, tmpDir, msgBus, logger)

	queuePath := filepath.Join(tmpDir, "queue.db")
	q, err := queue.NewPersistentQueue(queuePath, msgBus, logger)
	if err != nil {
		t.Fatalf("Failed to create queue: %v", err)
	}

	handlers := NewAmendmentHandlers(registry, q)
	amendmentMgr := NewAmendmentManager(msgBus, logger)
	handlers.RegisterAll(amendmentMgr)

	cleanup := func() {
		q.Close()
		registry.Close()
	}

	return handlers, registry, cleanup
}

func setupTestRegistry(t *testing.T, tmpDir string, msgBus *bus.MessageBus, logger *slog.Logger) *Registry {
	t.Helper()
	dbPath := filepath.Join(tmpDir, "tasks.db")
	registry, err := NewRegistry(dbPath, msgBus, logger)
	if err != nil {
		t.Fatalf("Failed to create registry: %v", err)
	}
	return registry
}

func TestHandleInjectContext(t *testing.T) {
	handlers, registry, cleanup := setupTestHandlers(t)
	defer cleanup()
	ctx := context.Background()

	// Create a task
	task, err := registry.Create(ctx, "test-task", "test task")
	if err != nil {
		t.Fatalf("Failed to create task: %v", err)
	}

	req := &AmendmentRequest{
		ID:      "inject-1",
		TaskID:  task.ID,
		Type:    AmendmentInjectContext,
		Content: "skip the tests and go straight to deployment",
	}

	reply, err := handlers.handleInjectContext(ctx, req)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	if !reply.Success {
		t.Errorf("Expected success, got: %s", reply.Message)
	}

	// Verify context was injected with [AMENDMENT] tag
	updatedTask, err := registry.Get(ctx, task.ID)
	if err != nil {
		t.Fatalf("Failed to get updated task: %v", err)
	}

	if !stringsContains(updatedTask.ContextQuery, "[AMENDMENT]") {
		t.Errorf("Expected [AMENDMENT] tag in context, got: %s", updatedTask.ContextQuery)
	}
	if !stringsContains(updatedTask.ContextQuery, "skip the tests") {
		t.Errorf("Expected context to be injected, got: %s", updatedTask.ContextQuery)
	}
}

func TestHandleInjectContext_WithExistingContext(t *testing.T) {
	handlers, registry, cleanup := setupTestHandlers(t)
	defer cleanup()
	ctx := context.Background()

	task, err := registry.Create(ctx, "test-task", "test task")
	if err != nil {
		t.Fatalf("Failed to create task: %v", err)
	}

	// Set initial context
	task.ContextQuery = "initial context"
	if err := registry.Update(ctx, task); err != nil {
		t.Fatalf("Failed to update task: %v", err)
	}

	req := &AmendmentRequest{
		ID:      "inject-2",
		TaskID:  task.ID,
		Type:    AmendmentInjectContext,
		Content: "additional context",
	}

	reply, err := handlers.handleInjectContext(ctx, req)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	if !reply.Success {
		t.Errorf("Expected success, got: %s", reply.Message)
	}

	updatedTask, err := registry.Get(ctx, task.ID)
	if err != nil {
		t.Fatalf("Failed to get task: %v", err)
	}

	if !stringsContains(updatedTask.ContextQuery, "initial context") {
		t.Errorf("Expected initial context preserved, got: %s", updatedTask.ContextQuery)
	}
	if !stringsContains(updatedTask.ContextQuery, "[AMENDMENT] additional context") {
		t.Errorf("Expected amendment context appended, got: %s", updatedTask.ContextQuery)
	}
}

func TestHandleSkipStep(t *testing.T) {
	handlers, _, cleanup := setupTestHandlers(t)
	defer cleanup()
	ctx := context.Background()

	// Create a task with two steps (step 2 depends on step 1)
	task, err := handlers.registry.Create(ctx, "test-task", "test task")
	if err != nil {
		t.Fatalf("Failed to create task: %v", err)
	}

	step1 := NewTaskStep(task.ID, "step 1", 0)
	step2 := NewTaskStep(task.ID, "step 2", 1)
	step2.DependsOn = []string{step1.ID}

	if err := handlers.stepStore.Create(step1); err != nil {
		t.Fatalf("Failed to create step 1: %v", err)
	}
	if err := handlers.stepStore.Create(step2); err != nil {
		t.Fatalf("Failed to create step 2: %v", err)
	}

	req := &AmendmentRequest{
		ID:      "skip-1",
		TaskID:  task.ID,
		Type:    AmendmentSkipStep,
		StepID:  step1.ID,
		Content: "skipped",
	}

	reply, err := handlers.handleSkipStep(ctx, req)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	if !reply.Success {
		t.Errorf("Expected success, got: %s", reply.Message)
	}

	// Verify step was skipped
	updatedStep1, err := handlers.stepStore.GetByID(step1.ID)
	if err != nil {
		t.Fatalf("Failed to get updated step: %v", err)
	}

	if updatedStep1.State != StepSkipped {
		t.Errorf("Expected step to be skipped, got: %s", updatedStep1.State)
	}
}

func TestHandleSkipStep_MissingStepID(t *testing.T) {
	handlers, _, cleanup := setupTestHandlers(t)
	defer cleanup()
	ctx := context.Background()

	req := &AmendmentRequest{
		ID:      "skip-missing",
		TaskID:  "nonexistent",
		Type:    AmendmentSkipStep,
		Content: "test",
	}

	reply, err := handlers.handleSkipStep(ctx, req)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	if reply.Success {
		t.Error("Expected failure when step_id is missing")
	}
}

func TestHandleAddStep(t *testing.T) {
	handlers, registry, cleanup := setupTestHandlers(t)
	defer cleanup()
	ctx := context.Background()

	// Create a task
	task, err := registry.Create(ctx, "test-task", "test task")
	if err != nil {
		t.Fatalf("Failed to create task: %v", err)
	}

	metadata, _ := json.Marshal(map[string]string{
		"description": "new step",
		"tool_hint":   "coder",
	})

	req := &AmendmentRequest{
		ID:       "add-1",
		TaskID:   task.ID,
		Type:     AmendmentAddStep,
		Content:  "add new step",
		Metadata: metadata,
	}

	reply, err := handlers.handleAddStep(ctx, req)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	if !reply.Success {
		t.Errorf("Expected success, got: %s", reply.Message)
	}

	// Verify task total jobs was updated
	updatedTask, err := registry.Get(ctx, task.ID)
	if err != nil {
		t.Fatalf("Failed to get updated task: %v", err)
	}

	if updatedTask.TotalJobs != 1 {
		t.Errorf("Expected TotalJobs to be 1, got: %d", updatedTask.TotalJobs)
	}
}

func TestHandleAddStep_WithAgentID(t *testing.T) {
	handlers, registry, cleanup := setupTestHandlers(t)
	defer cleanup()
	ctx := context.Background()

	task, err := registry.Create(ctx, "test-task", "test task")
	if err != nil {
		t.Fatalf("Failed to create task: %v", err)
	}

	metadata, _ := json.Marshal(map[string]string{
		"description": "new step",
		"agent_id":    "debugger",
	})

	req := &AmendmentRequest{
		ID:       "add-agent",
		TaskID:   task.ID,
		Type:     AmendmentAddStep,
		Content:  "add step with agent",
		Metadata: metadata,
	}

	reply, err := handlers.handleAddStep(ctx, req)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	if !reply.Success {
		t.Errorf("Expected success, got: %s", reply.Message)
	}

	// Verify the step was created with the agent ID
	steps, _ := handlers.stepStore.ListByTaskID(task.ID)
	found := false
	for _, s := range steps {
		if s.AgentID == "debugger" {
			found = true
			break
		}
	}
	if !found {
		t.Error("Expected to find step with agent_id=debugger")
	}
}

func TestHandleAddStep_MissingDescription(t *testing.T) {
	handlers, _, cleanup := setupTestHandlers(t)
	defer cleanup()
	ctx := context.Background()

	task, err := handlers.registry.Create(ctx, "test-task", "test task")
	if err != nil {
		t.Fatalf("Failed to create task: %v", err)
	}

	metadata, _ := json.Marshal(map[string]string{
		"description": "",
	})
	req := &AmendmentRequest{
		ID:       "add-empty",
		TaskID:   task.ID,
		Type:     AmendmentAddStep,
		Metadata: metadata,
	}

	reply, err := handlers.handleAddStep(ctx, req)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	if reply.Success {
		t.Error("Expected failure for missing description")
	}
}

func TestHandleReprioritize(t *testing.T) {
	handlers, _, cleanup := setupTestHandlers(t)
	defer cleanup()
	ctx := context.Background()

	// Create a task with steps
	task, err := handlers.registry.Create(ctx, "test-task", "test task")
	if err != nil {
		t.Fatalf("Failed to create task: %v", err)
	}

	step1 := NewTaskStep(task.ID, "step 1", 0)
	step2 := NewTaskStep(task.ID, "step 2", 1)
	step3 := NewTaskStep(task.ID, "step 3", 2)

	handlers.stepStore.Create(step1)
	handlers.stepStore.Create(step2)
	handlers.stepStore.Create(step3)

	metadata, _ := json.Marshal(map[string][]string{
		"step_ids": []string{step3.ID, step1.ID, step2.ID},
	})

	req := &AmendmentRequest{
		ID:       "reprio-1",
		TaskID:   task.ID,
		Type:     AmendmentReprioritize,
		Content:  "reorder steps",
		Metadata: metadata,
	}

	reply, err := handlers.handleReprioritize(ctx, req)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	if !reply.Success {
		t.Errorf("Expected success, got: %s", reply.Message)
	}

	// Verify sequence was updated
	s1, _ := handlers.stepStore.GetByID(step1.ID)
	s2, _ := handlers.stepStore.GetByID(step2.ID)
	s3, _ := handlers.stepStore.GetByID(step3.ID)

	if s3.Sequence != 0 || s1.Sequence != 1 || s2.Sequence != 2 {
		t.Errorf("Expected reprioritization, got sequences: %d, %d, %d", s3.Sequence, s1.Sequence, s2.Sequence)
	}
}

func TestHandleReprioritize_EmptyStepIDs(t *testing.T) {
	handlers, _, cleanup := setupTestHandlers(t)
	defer cleanup()
	ctx := context.Background()

	req := &AmendmentRequest{
		ID:      "reprio-empty",
		TaskID:  "task-1",
		Type:    AmendmentReprioritize,
		Content: "reorder",
	}

	reply, err := handlers.handleReprioritize(ctx, req)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	if reply.Success {
		t.Error("Expected failure for empty step_ids")
	}
}

func TestHandleChangeAgent(t *testing.T) {
	handlers, _, cleanup := setupTestHandlers(t)
	defer cleanup()
	ctx := context.Background()

	// Create a task with step
	task, err := handlers.registry.Create(ctx, "test-task", "test task")
	if err != nil {
		t.Fatalf("Failed to create task: %v", err)
	}

	step := NewTaskStep(task.ID, "step 1", 0)
	step.AgentID = "coder"
	handlers.stepStore.Create(step)

	metadata, _ := json.Marshal(map[string]string{
		"step_id":  step.ID,
		"agent_id": "debugger",
	})

	req := &AmendmentRequest{
		ID:       "agent-1",
		TaskID:   task.ID,
		Type:     AmendmentChangeAgent,
		Content:  "change agent",
		Metadata: metadata,
	}

	reply, err := handlers.handleChangeAgent(ctx, req)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	if !reply.Success {
		t.Errorf("Expected success, got: %s", reply.Message)
	}

	// Verify agent was changed
	updatedStep, err := handlers.stepStore.GetByID(step.ID)
	if err != nil {
		t.Fatalf("Failed to get updated step: %v", err)
	}

	if updatedStep.AgentID != "debugger" {
		t.Errorf("Expected agent to be debugger, got: %s", updatedStep.AgentID)
	}
}

func TestHandleChangeAgent_MissingFields(t *testing.T) {
	handlers, _, cleanup := setupTestHandlers(t)
	defer cleanup()
	ctx := context.Background()

	// Test missing step_id
	req := &AmendmentRequest{
		ID:       "agent-missing-step",
		TaskID:   "task-1",
		Type:     AmendmentChangeAgent,
		Metadata: json.RawMessage(`{"agent_id":"debugger"}`),
	}

	reply, err := handlers.handleChangeAgent(ctx, req)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	if reply.Success {
		t.Error("Expected failure for missing step_id")
	}

	// Test missing agent_id
	req = &AmendmentRequest{
		ID:       "agent-missing-agent",
		TaskID:   "task-1",
		Type:     AmendmentChangeAgent,
		Metadata: json.RawMessage(`{"step_id":"step-1"}`),
	}

	reply, err = handlers.handleChangeAgent(ctx, req)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	if reply.Success {
		t.Error("Expected failure for missing agent_id")
	}
}

func TestHandleAmendmentErrors(t *testing.T) {
	handlers, _, cleanup := setupTestHandlers(t)
	defer cleanup()
	ctx := context.Background()

	// Test skip with missing step_id
	req := &AmendmentRequest{
		ID:      "error-1",
		TaskID:  "nonexistent",
		Type:    AmendmentSkipStep,
		Content: "test",
	}

	reply, err := handlers.handleSkipStep(ctx, req)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	// This should fail because step doesn't exist
	if reply.Success {
		t.Error("Expected failure for nonexistent step")
	}
}

// stringsContains is a simple helper since we can't import strings
func stringsContains(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
