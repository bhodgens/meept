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

func setupTestHandlers(t *testing.T) (*AmendmentHandlers, *AmendmentManager, func()) {
	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))
	msgBus := bus.New(nil, logger)

	// Create in-memory task registry
	tmpDir := t.TempDir()
	registry := setupTestRegistry(t, tmpDir, msgBus, logger)

	// Create queue
	q, err := queue.NewPersistentQueue(filepath.Join(tmpDir, "queue.db"), msgBus, logger)
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

	return handlers, amendmentMgr, cleanup
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

func TestAmendmentHandlers_HandleInjectContext(t *testing.T) {
	handlers, amendmentMgr, cleanup := setupTestHandlers(t)
	defer cleanup()

	ctx := context.Background()

	// Create a test task
	task, err := handlers.registry.Create(ctx, "test task", "test description")
	if err != nil {
		t.Fatalf("Failed to create task: %v", err)
	}
	task.ContextQuery = "initial context"
	if err := handlers.registry.Update(ctx, task); err != nil {
		t.Fatalf("Failed to update task: %v", err)
	}

	// Create amendment request and submit it
	req := NewAmendmentRequest(task.ID, AmendmentInjectContext, "additional context")
	if err := amendmentMgr.Submit(ctx, req); err != nil {
		t.Fatalf("Submit failed: %v", err)
	}

	// Process the amendment
	reply, err := amendmentMgr.Process(ctx, req.ID)
	if err != nil {
		t.Fatalf("Process failed: %v", err)
	}

	if !reply.Success {
		t.Errorf("Expected success, got false. Message: %s", reply.Message)
	}

	// Verify context was injected
	updatedTask, err := handlers.registry.Get(ctx, task.ID)
	if err != nil {
		t.Fatalf("Failed to get task: %v", err)
	}

	expectedContext := "initial context\nadditional context"
	if updatedTask.ContextQuery != expectedContext {
		t.Errorf("ContextQuery = %q, want %q", updatedTask.ContextQuery, expectedContext)
	}
}

func TestAmendmentHandlers_HandleInjectContext_EmptyInitial(t *testing.T) {
	handlers, _, cleanup := setupTestHandlers(t)
	defer cleanup()

	ctx := context.Background()

	// Create a test task with no initial context
	task, err := handlers.registry.Create(ctx, "test task", "test description")
	if err != nil {
		t.Fatalf("Failed to create task: %v", err)
	}

	// Create amendment request and submit it
	req := NewAmendmentRequest(task.ID, AmendmentInjectContext, "new context")

	// Manually call handler
	reply, err := handlers.handleInjectContext(ctx, req)
	if err != nil {
		t.Fatalf("handleInjectContext failed: %v", err)
	}

	if !reply.Success {
		t.Errorf("Expected success, got false. Message: %s", reply.Message)
	}

	// Verify context was injected
	updatedTask, err := handlers.registry.Get(ctx, task.ID)
	if err != nil {
		t.Fatalf("Failed to get task: %v", err)
	}

	if updatedTask.ContextQuery != "new context" {
		t.Errorf("ContextQuery = %q, want %q", updatedTask.ContextQuery, "new context")
	}
}

func TestAmendmentHandlers_HandleSkipStep(t *testing.T) {
	handlers, _, cleanup := setupTestHandlers(t)
	defer cleanup()

	ctx := context.Background()

	// Create a test task
	task, err := handlers.registry.Create(ctx, "test task", "test description")
	if err != nil {
		t.Fatalf("Failed to create task: %v", err)
	}

	// Create a step
	step := NewTaskStep(task.ID, "test step", 1)
	if err := handlers.stepStore.Create(step); err != nil {
		t.Fatalf("Failed to create step: %v", err)
	}

	// Create amendment request and submit it
	req := NewAmendmentRequest(task.ID, AmendmentSkipStep, "skip this step")
	req.StepID = step.ID

	// Manually call handler
	reply, err := handlers.handleSkipStep(ctx, req)
	if err != nil {
		t.Fatalf("handleSkipStep failed: %v", err)
	}

	if !reply.Success {
		t.Errorf("Expected success, got false. Message: %s", reply.Message)
	}

	// Verify step was skipped
	updatedStep, err := handlers.stepStore.GetByID(step.ID)
	if err != nil {
		t.Fatalf("Failed to get step: %v", err)
	}

	if updatedStep.State != StepSkipped {
		t.Errorf("Step state = %v, want %v", updatedStep.State, StepSkipped)
	}
}

func TestAmendmentHandlers_HandleSkipStep_MissingStepID(t *testing.T) {
	handlers, _, cleanup := setupTestHandlers(t)
	defer cleanup()

	ctx := context.Background()

	req := NewAmendmentRequest("task-1", AmendmentSkipStep, "skip")
	req.StepID = ""

	reply, err := handlers.handleSkipStep(ctx, req)
	if err != nil {
		t.Fatalf("handleSkipStep failed: %v", err)
	}

	if reply.Success {
		t.Error("Expected failure when step_id is missing")
	}
}

func TestAmendmentHandlers_HandleAddStep(t *testing.T) {
	handlers, _, cleanup := setupTestHandlers(t)
	defer cleanup()

	ctx := context.Background()

	// Create a test task
	task, err := handlers.registry.Create(ctx, "test task", "test description")
	if err != nil {
		t.Fatalf("Failed to create task: %v", err)
	}

	// Create initial step
	step := NewTaskStep(task.ID, "initial step", 1)
	if err := handlers.stepStore.Create(step); err != nil {
		t.Fatalf("Failed to create step: %v", err)
	}

	// Create amendment request with metadata
	metadata, _ := json.Marshal(map[string]any{
		"description": "new step",
		"tool_hint":   "coder",
		"depends_on":  []string{step.ID},
	})

	req := NewAmendmentRequest(task.ID, AmendmentAddStep, "add new step")
	req.Metadata = metadata

	// Manually call handler
	reply, err := handlers.handleAddStep(ctx, req)
	if err != nil {
		t.Fatalf("handleAddStep failed: %v", err)
	}

	if !reply.Success {
		t.Errorf("Expected success, got false. Message: %s", reply.Message)
	}

	// Verify step was added
	steps, err := handlers.stepStore.ListByTaskID(task.ID)
	if err != nil {
		t.Fatalf("Failed to list steps: %v", err)
	}

	if len(steps) != 2 {
		t.Errorf("Expected 2 steps, got %d", len(steps))
	}

	// Verify task total jobs was incremented
	updatedTask, err := handlers.registry.Get(ctx, task.ID)
	if err != nil {
		t.Fatalf("Failed to get task: %v", err)
	}

	if updatedTask.TotalJobs != 1 {
		t.Errorf("Task.TotalJobs = %d, want %d", updatedTask.TotalJobs, 1)
	}
}

func TestAmendmentHandlers_HandleReprioritize(t *testing.T) {
	handlers, _, cleanup := setupTestHandlers(t)
	defer cleanup()

	ctx := context.Background()

	// Create a test task
	task, err := handlers.registry.Create(ctx, "test task", "test description")
	if err != nil {
		t.Fatalf("Failed to create task: %v", err)
	}

	// Create steps
	step1 := NewTaskStep(task.ID, "step 1", 1)
	step2 := NewTaskStep(task.ID, "step 2", 2)
	step3 := NewTaskStep(task.ID, "step 3", 3)
	handlers.stepStore.Create(step1)
	handlers.stepStore.Create(step2)
	handlers.stepStore.Create(step3)

	// Create amendment request to reorder
	metadata, _ := json.Marshal(map[string][]string{
		"step_ids": {step3.ID, step1.ID, step2.ID}, // New order
	})

	req := NewAmendmentRequest(task.ID, AmendmentReprioritize, "reorder steps")
	req.Metadata = metadata

	// Manually call handler
	reply, err := handlers.handleReprioritize(ctx, req)
	if err != nil {
		t.Fatalf("handleReprioritize failed: %v", err)
	}

	if !reply.Success {
		t.Errorf("Expected success, got false. Message: %s", reply.Message)
	}

	// Verify sequence was updated
	steps, _ := handlers.stepStore.ListByTaskID(task.ID)
	expectedOrder := map[string]int{
		step3.ID: 0,
		step1.ID: 1,
		step2.ID: 2,
	}

	for _, step := range steps {
		if expectedSeq, ok := expectedOrder[step.ID]; ok {
			if step.Sequence != expectedSeq {
				t.Errorf("Step %s sequence = %d, want %d", step.ID, step.Sequence, expectedSeq)
			}
		}
	}
}

func TestAmendmentHandlers_HandleChangeAgent(t *testing.T) {
	handlers, _, cleanup := setupTestHandlers(t)
	defer cleanup()

	ctx := context.Background()

	// Create a test task
	task, err := handlers.registry.Create(ctx, "test task", "test description")
	if err != nil {
		t.Fatalf("Failed to create task: %v", err)
	}

	// Create a step
	step := NewTaskStep(task.ID, "test step", 1)
	step.AgentID = "coder"
	handlers.stepStore.Create(step)

	// Create amendment request and submit it
	metadata, _ := json.Marshal(map[string]string{
		"step_id":  step.ID,
		"agent_id": "debugger",
	})

	req := NewAmendmentRequest(task.ID, AmendmentChangeAgent, "change agent")
	req.Metadata = metadata

	// Manually call handler
	reply, err := handlers.handleChangeAgent(ctx, req)
	if err != nil {
		t.Fatalf("handleChangeAgent failed: %v", err)
	}

	if !reply.Success {
		t.Errorf("Expected success, got false. Message: %s", reply.Message)
	}

	// Verify agent was changed
	updatedStep, err := handlers.stepStore.GetByID(step.ID)
	if err != nil {
		t.Fatalf("Failed to get step: %v", err)
	}

	if updatedStep.AgentID != "debugger" {
		t.Errorf("Step.AgentID = %s, want debugger", updatedStep.AgentID)
	}
}

func TestAmendmentHandlers_HandleChangeAgent_InvalidMetadata(t *testing.T) {
	handlers, _, cleanup := setupTestHandlers(t)
	defer cleanup()

	ctx := context.Background()

	req := NewAmendmentRequest("task-1", AmendmentChangeAgent, "change agent")
	req.Metadata = json.RawMessage(`invalid json`)

	reply, err := handlers.handleChangeAgent(ctx, req)
	if err != nil {
		t.Fatalf("handleChangeAgent failed: %v", err)
	}

	if reply.Success {
		t.Error("Expected failure with invalid metadata")
	}
}
