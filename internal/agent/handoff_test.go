package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/bus"
	"github.com/caimlas/meept/internal/config"
	"github.com/caimlas/meept/internal/queue"
	"github.com/caimlas/meept/internal/task"
	"github.com/caimlas/meept/pkg/models"

	"database/sql"

	_ "modernc.org/sqlite"
)

// mockQueue is a no-op implementation of queue.Queue for testing.
type mockQueue struct{}

func (m *mockQueue) Enqueue(_ context.Context, _ *queue.Job) error  { return nil }
func (m *mockQueue) Claim(_ context.Context, _ string, _ []string) (*queue.Job, error) {
	return nil, nil
}
func (m *mockQueue) MarkProcessing(_ context.Context, _ string) error { return nil }
func (m *mockQueue) Complete(_ context.Context, _ string, _ any) error { return nil }
func (m *mockQueue) Fail(_ context.Context, _ string, _ error) error  { return nil }
func (m *mockQueue) Retry(_ context.Context, _ string) error           { return nil }
func (m *mockQueue) Get(_ context.Context, _ string) (*queue.Job, error) {
	return nil, nil
}
func (m *mockQueue) ListByState(_ context.Context, _ queue.JobState, _ int) ([]*queue.Job, error) {
	return nil, nil
}
func (m *mockQueue) ListByTaskID(_ context.Context, _ string) ([]*queue.Job, error) {
	return nil, nil
}
func (m *mockQueue) Stats(_ context.Context) (*queue.QueueStats, error) {
	return nil, nil
}
func (m *mockQueue) RecoverFromDeadLetter(_ context.Context, _ string) (*queue.Job, error) {
	return nil, nil
}
func (m *mockQueue) ListDeadLetter(_ context.Context, _ int) ([]*queue.Job, error) {
	return nil, nil
}
func (m *mockQueue) DeadLetterStats(_ context.Context) (int, error) { return 0, nil }
func (m *mockQueue) Close() error                                     { return nil }

// newTestTaskAndStepStore creates a task store (with step store) for testing.
// The task store creates both tables in the same DB.
func newTestTaskAndStepStore(t *testing.T) (*task.Store, *task.StepStore) {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "tasks.db")
	store, err := task.NewStore(dbPath, nil)
	if err != nil {
		t.Fatalf("failed to create task store: %v", err)
	}
	t.Cleanup(func() { store.Close() })

	// The store internally creates a StepStore; access it through a separate
	// connection to the same DB so both stores see each other's data.
	db, err := sql.Open("sqlite", dbPath+"?_journal_mode=WAL&_busy_timeout=5000")
	if err != nil {
		t.Fatalf("failed to open db for step store: %v", err)
	}
	t.Cleanup(func() { db.Close() })

	stepStore, err := task.NewStepStore(db, nil)
	if err != nil {
		t.Fatalf("failed to create step store: %v", err)
	}

	return store, stepStore
}

// handoffBusMsg creates a BusMessage from a HandoffRequest.
func handoffBusMsg(req HandoffRequest) *models.BusMessage {
	payload, _ := json.Marshal(req)
	return &models.BusMessage{
		Type:      models.MessageTypeEvent,
		Topic:     "orchestrator.handoff",
		Source:    "test",
		Timestamp: time.Now().UTC(),
		Payload:   payload,
	}
}

func TestTacticalScheduler_HandleHandoff_CreatesNewStep(t *testing.T) {
	taskStore, stepStore := newTestTaskAndStepStore(t)
	msgBus := bus.New(nil, slogDiscardLogger())
	defer msgBus.Close()

	// Create a task
	tk := task.NewTask("task-handoff-1", "test handoff task")
	tk.TotalJobs = 2
	if err := taskStore.Create(tk); err != nil {
		t.Fatalf("failed to create task: %v", err)
	}

	// Create the originating step (in completed state so handoff step can be promoted)
	fromStep := task.NewTaskStep(tk.ID, "initial coding step", 1)
	fromStep.State = task.StepCompleted
	fromStep.ToolHint = "code"
	if err := stepStore.Create(fromStep); err != nil {
		t.Fatalf("failed to create from step: %v", err)
	}

	// Create scheduler with mock queue (step gets promoted since from step is completed)
	scheduler := NewTacticalScheduler(TacticalSchedulerConfig{
		StepStore: stepStore,
		TaskStore: taskStore,
		Queue:     &mockQueue{},
		Bus:       msgBus,
		Logger:    slogDiscardLogger(),
	})

	// Build handoff request
	req := HandoffRequest{
		TaskID:        tk.ID,
		FromStepID:    fromStep.ID,
		FromAgentID:   config.AgentIDCoder,
		ToAgentID:     config.AgentIDDebugger,
		Description:   "Debug the failing test",
		Reason:        "Tests are failing after implementation",
		PartialResult: "Implemented feature X",
		InjectAfter:    true,
	}

	msg := handoffBusMsg(req)

	// Execute
	err := scheduler.HandleHandoff(context.Background(), msg)
	if err != nil {
		t.Fatalf("HandleHandoff failed: %v", err)
	}

	// Verify new step was created
	steps, err := stepStore.ListByTaskID(tk.ID)
	if err != nil {
		t.Fatalf("failed to list steps: %v", err)
	}
	if len(steps) != 2 {
		t.Fatalf("expected 2 steps, got %d", len(steps))
	}

	// Find the new step (not the from step)
	var newStep *task.TaskStep
	for _, s := range steps {
		if s.ID != fromStep.ID {
			newStep = s
			break
		}
	}
	if newStep == nil {
		t.Fatal("new step not found")
	}

	// Verify properties
	if newStep.Description != "Debug the failing test" {
		t.Errorf("expected description %q, got %q", "Debug the failing test", newStep.Description)
	}
	if newStep.ToolHint != "debug" {
		t.Errorf("expected tool_hint %q, got %q", "debug", newStep.ToolHint)
	}
	if len(newStep.DependsOn) != 1 || newStep.DependsOn[0] != fromStep.ID {
		t.Errorf("expected depends_on [%s], got %v", fromStep.ID, newStep.DependsOn)
	}

	// Verify accumulated context
	if len(newStep.AccumulatedContext) == 0 {
		t.Error("expected accumulated_context to be set")
	}
	if newStep.AccumulatedContext == "" {
		t.Error("expected non-empty accumulated_context")
	}

	// Verify task TotalJobs incremented
	updated, err := taskStore.GetByID(tk.ID)
	if err != nil {
		t.Fatalf("failed to get updated task: %v", err)
	}
	if updated.TotalJobs != 3 {
		t.Errorf("expected TotalJobs 3, got %d", updated.TotalJobs)
	}
}

func TestTacticalScheduler_HandleHandoff_InvalidPayload(t *testing.T) {
	taskStore, stepStore := newTestTaskAndStepStore(t)
	msgBus := bus.New(nil, slogDiscardLogger())
	defer msgBus.Close()

	scheduler := NewTacticalScheduler(TacticalSchedulerConfig{
		StepStore: stepStore,
		TaskStore: taskStore,
		Bus:       msgBus,
		Logger:    slogDiscardLogger(),
	})

	msg := &models.BusMessage{
		Type:      models.MessageTypeEvent,
		Topic:     "orchestrator.handoff",
		Source:    "test",
		Timestamp: time.Now().UTC(),
		Payload:   []byte(`{invalid json garbage}`),
	}

	err := scheduler.HandleHandoff(context.Background(), msg)
	if err == nil {
		t.Fatal("expected error for invalid payload, got nil")
	}
}

func TestTacticalScheduler_HandleHandoff_MissingTask(t *testing.T) {
	taskStore, stepStore := newTestTaskAndStepStore(t)
	msgBus := bus.New(nil, slogDiscardLogger())
	defer msgBus.Close()

	scheduler := NewTacticalScheduler(TacticalSchedulerConfig{
		StepStore: stepStore,
		TaskStore: taskStore,
		Bus:       msgBus,
		Logger:    slogDiscardLogger(),
	})

	req := HandoffRequest{
		TaskID:        "nonexistent-task",
		FromStepID:    "step-nonexistent",
		FromAgentID:   config.AgentIDCoder,
		ToAgentID:     config.AgentIDDebugger,
		Description:   "Debug",
		Reason:        "tests fail",
		PartialResult: "partial",
		InjectAfter:   true,
	}

	msg := handoffBusMsg(req)

	err := scheduler.HandleHandoff(context.Background(), msg)
	if err == nil {
		t.Fatal("expected error for missing task, got nil")
	}
}

func TestTacticalScheduler_HandleHandoff_RewiresDownstreamDependencies(t *testing.T) {
	taskStore, stepStore := newTestTaskAndStepStore(t)
	msgBus := bus.New(nil, slogDiscardLogger())
	defer msgBus.Close()

	// Create task
	tk := task.NewTask("task-rewire-1", "rewire test task")
	tk.TotalJobs = 3
	if err := taskStore.Create(tk); err != nil {
		t.Fatalf("failed to create task: %v", err)
	}

	// Create 3-step DAG: A -> B -> C
	stepA := task.NewTaskStep(tk.ID, "Step A", 1)
	stepA.State = task.StepCompleted

	stepB := task.NewTaskStep(tk.ID, "Step B", 2)
	stepB.DependsOn = []string{stepA.ID}

	stepC := task.NewTaskStep(tk.ID, "Step C", 3)
	stepC.DependsOn = []string{stepB.ID}

	if err := stepStore.Create(stepA); err != nil {
		t.Fatalf("failed to create step A: %v", err)
	}
	if err := stepStore.Create(stepB); err != nil {
		t.Fatalf("failed to create step B: %v", err)
	}
	if err := stepStore.Create(stepC); err != nil {
		t.Fatalf("failed to create step C: %v", err)
	}

	scheduler := NewTacticalScheduler(TacticalSchedulerConfig{
		StepStore: stepStore,
		TaskStore: taskStore,
		Queue:     &mockQueue{},
		Bus:       msgBus,
		Logger:    slogDiscardLogger(),
	})

	// Handoff after A: inject step, should rewire B's dependency from A to injected
	req := HandoffRequest{
		TaskID:        tk.ID,
		FromStepID:    stepA.ID,
		FromAgentID:   config.AgentIDCoder,
		ToAgentID:     config.AgentIDDebugger,
		Description:   "Debug step A output",
		Reason:        "Found potential issues",
		PartialResult: "Implemented initial version",
		InjectAfter:    true,
	}

	msg := handoffBusMsg(req)
	err := scheduler.HandleHandoff(context.Background(), msg)
	if err != nil {
		t.Fatalf("HandleHandoff failed: %v", err)
	}

	// Verify B now depends on the injected step instead of A
	steps, err := stepStore.ListByTaskID(tk.ID)
	if err != nil {
		t.Fatalf("failed to list steps: %v", err)
	}
	if len(steps) != 4 {
		t.Fatalf("expected 4 steps, got %d", len(steps))
	}

	// Find step B
	var bAfter *task.TaskStep
	for _, s := range steps {
		if s.Description == "Step B" {
			bAfter = s
			break
		}
	}
	if bAfter == nil {
		t.Fatal("step B not found after handoff")
	}

	// B should no longer depend on A
	for _, dep := range bAfter.DependsOn {
		if dep == stepA.ID {
			t.Errorf("step B should not depend on step A after rewiring, but still does")
		}
	}

	// B should depend on the injected step
	// Find injected step
	var injected *task.TaskStep
	for _, s := range steps {
		if s.Description == "Debug step A output" {
			injected = s
			break
		}
	}
	if injected == nil {
		t.Fatal("injected step not found")
	}

	found := false
	for _, dep := range bAfter.DependsOn {
		if dep == injected.ID {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("step B should depend on injected step, but depends on %v", bAfter.DependsOn)
	}
}

func TestTacticalScheduler_HandleHandoff_ToolHintDerivedFromAgent(t *testing.T) {
	taskStore, stepStore := newTestTaskAndStepStore(t)
	msgBus := bus.New(nil, slogDiscardLogger())
	defer msgBus.Close()

	tk := task.NewTask("task-hint-test", "tool hint test")
	tk.TotalJobs = 1
	if err := taskStore.Create(tk); err != nil {
		t.Fatalf("failed to create task: %v", err)
	}

	fromStep := task.NewTaskStep(tk.ID, "Step A", 1)
	fromStep.State = task.StepCompleted
	if err := stepStore.Create(fromStep); err != nil {
		t.Fatalf("failed to create from step: %v", err)
	}

	scheduler := NewTacticalScheduler(TacticalSchedulerConfig{
		StepStore:        stepStore,
		TaskStore:        taskStore,
		Queue:            &mockQueue{},
		Bus:              msgBus,
		Logger:           slogDiscardLogger(),
		MaxHandoffSteps:  0, // disable rate limiting for this test
		HandoffUseAmendment: false,
	})

	tests := []struct {
		name     string
		agentID  string
		wantHint string
		explicit string // if non-empty, use this instead of deriving
	}{
		{"coder", config.AgentIDCoder, "code", ""},
		{"debugger", config.AgentIDDebugger, "debug", ""},
		{"analyst", config.AgentIDAnalyst, "analyze", ""},
		{"committer", config.AgentIDCommitter, "git", ""},
		{"planner", config.AgentIDPlanner, "plan", ""},
		{"explicit override", config.AgentIDCoder, "custom", "custom"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := HandoffRequest{
				TaskID:        tk.ID,
				FromStepID:    fromStep.ID,
				FromAgentID:   config.AgentIDChat,
				ToAgentID:     tt.agentID,
				Description:   "test step " + tt.name,
				Reason:        "testing",
				PartialResult: "partial",
				InjectAfter:    true,
			}
			if tt.explicit != "" {
				req.ToolHint = tt.explicit
			}

			msg := handoffBusMsg(req)
			err := scheduler.HandleHandoff(context.Background(), msg)
			if err != nil {
				t.Fatalf("HandleHandoff failed: %v", err)
			}

			steps, err := stepStore.ListByTaskID(tk.ID)
			if err != nil {
				t.Fatalf("failed to list steps: %v", err)
			}

			// Find the latest step
			var latest *task.TaskStep
			for _, s := range steps {
				if s.Description == "test step "+tt.name {
					latest = s
					break
				}
			}
			if latest == nil {
				t.Fatal("step not found")
			}
			if latest.ToolHint != tt.wantHint {
				t.Errorf("expected tool_hint %q, got %q", tt.wantHint, latest.ToolHint)
			}
		})
	}
}

func TestOrchestrator_HandoffSubscription(t *testing.T) {
	msgBus := bus.New(nil, slogDiscardLogger())
	defer msgBus.Close()

	orchestrator := &Orchestrator{
		bus:    msgBus,
		logger: slogDiscardLogger(),
	}

	ctx := t.Context()
	if err := orchestrator.Start(ctx); err != nil {
		t.Fatalf("Failed to start orchestrator: %v", err)
	}
	defer func() { _ = orchestrator.Stop(context.Background()) }()

	time.Sleep(10 * time.Millisecond)

	stats := msgBus.Stats()
	count, ok := stats["orchestrator.handoff"]
	if !ok {
		t.Error("expected subscriber for topic orchestrator.handoff, not found")
	}
	if count < 1 {
		t.Errorf("expected at least 1 subscriber for topic orchestrator.handoff, got %d", count)
	}
}

func TestOrchestrator_HandoffSubscription_FullFlow(t *testing.T) {
	// 3-step DAG A -> B -> C, handoff after A, verify chain becomes A -> injected -> B -> C
	taskStore, stepStore := newTestTaskAndStepStore(t)
	msgBus := bus.New(nil, slogDiscardLogger())
	defer msgBus.Close()

	// Create task
	tk := task.NewTask("task-fullflow-1", "full flow test")
	tk.TotalJobs = 3
	if err := taskStore.Create(tk); err != nil {
		t.Fatalf("failed to create task: %v", err)
	}

	// Create 3-step chain: A (completed) -> B -> C
	stepA := task.NewTaskStep(tk.ID, "Step A", 1)
	stepA.State = task.StepCompleted

	stepB := task.NewTaskStep(tk.ID, "Step B", 2)
	stepB.DependsOn = []string{stepA.ID}

	stepC := task.NewTaskStep(tk.ID, "Step C", 3)
	stepC.DependsOn = []string{stepB.ID}

	if err := stepStore.Create(stepA); err != nil {
		t.Fatalf("failed to create step A: %v", err)
	}
	if err := stepStore.Create(stepB); err != nil {
		t.Fatalf("failed to create step B: %v", err)
	}
	if err := stepStore.Create(stepC); err != nil {
		t.Fatalf("failed to create step C: %v", err)
	}

	// Create tactical scheduler with mock queue
	scheduler := NewTacticalScheduler(TacticalSchedulerConfig{
		StepStore: stepStore,
		TaskStore: taskStore,
		Queue:     &mockQueue{},
		Bus:       msgBus,
		Logger:    slogDiscardLogger(),
	})

	// Create orchestrator with tactical
	orchestrator := NewOrchestrator(OrchestratorDeps{
		Tactical: scheduler,
		Bus:      msgBus,
		Logger:   slogDiscardLogger(),
	})

	ctx := t.Context()
	if err := orchestrator.Start(ctx); err != nil {
		t.Fatalf("Failed to start orchestrator: %v", err)
	}
	defer func() { _ = orchestrator.Stop(context.Background()) }()

	time.Sleep(10 * time.Millisecond)

	// Publish handoff event
	req := HandoffRequest{
		TaskID:        tk.ID,
		FromStepID:    stepA.ID,
		FromAgentID:   config.AgentIDCoder,
		ToAgentID:     config.AgentIDDebugger,
		Description:   "Review and debug step A",
		Reason:        "Need validation of approach",
		PartialResult: "Implemented feature X with approach Y",
		InjectAfter:    true,
	}

	msg := handoffBusMsg(req)
	msgBus.Publish("orchestrator.handoff", msg)

	// Wait for async handler to process
	time.Sleep(200 * time.Millisecond)

	// Verify the chain: A -> injected -> B -> C
	steps, err := stepStore.ListByTaskID(tk.ID)
	if err != nil {
		t.Fatalf("failed to list steps: %v", err)
	}
	if len(steps) != 4 {
		t.Fatalf("expected 4 steps, got %d", len(steps))
	}

	// Build step map for easy lookup
	stepMap := make(map[string]*task.TaskStep)
	for _, s := range steps {
		stepMap[s.ID] = s
	}

	// Find injected step
	var injected *task.TaskStep
	for _, s := range steps {
		if s.Description == "Review and debug step A" {
			injected = s
			break
		}
	}
	if injected == nil {
		t.Fatal("injected step not found")
	}

	// Verify injected depends on A
	if len(injected.DependsOn) != 1 || injected.DependsOn[0] != stepA.ID {
		t.Errorf("injected step should depend on A, got %v", injected.DependsOn)
	}

	// Verify B now depends on injected (not A)
	bAfter := stepMap[stepB.ID]
	for _, dep := range bAfter.DependsOn {
		if dep == stepA.ID {
			t.Error("step B should not depend on step A after rewiring")
		}
	}
	found := false
	for _, dep := range bAfter.DependsOn {
		if dep == injected.ID {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("step B should depend on injected step, got %v", bAfter.DependsOn)
	}

	// Verify C still depends on B
	cAfter := stepMap[stepC.ID]
	if len(cAfter.DependsOn) != 1 || cAfter.DependsOn[0] != stepB.ID {
		t.Errorf("step C should still depend on B, got %v", cAfter.DependsOn)
	}

	// Verify task TotalJobs incremented
	updated, err := taskStore.GetByID(tk.ID)
	if err != nil {
		t.Fatalf("failed to get updated task: %v", err)
	}
	if updated.TotalJobs != 4 {
		t.Errorf("expected TotalJobs 4, got %d", updated.TotalJobs)
	}
}

func TestTacticalScheduler_HandleHandoff_NoRewireWhenNotInjectAfter(t *testing.T) {
	taskStore, stepStore := newTestTaskAndStepStore(t)
	msgBus := bus.New(nil, slogDiscardLogger())
	defer msgBus.Close()

	tk := task.NewTask("task-norewire", "no rewire test")
	tk.TotalJobs = 3
	if err := taskStore.Create(tk); err != nil {
		t.Fatalf("failed to create task: %v", err)
	}

	stepA := task.NewTaskStep(tk.ID, "Step A", 1)
	stepA.State = task.StepCompleted

	stepB := task.NewTaskStep(tk.ID, "Step B", 2)
	stepB.DependsOn = []string{stepA.ID}

	if err := stepStore.Create(stepA); err != nil {
		t.Fatalf("failed to create step A: %v", err)
	}
	if err := stepStore.Create(stepB); err != nil {
		t.Fatalf("failed to create step B: %v", err)
	}

	scheduler := NewTacticalScheduler(TacticalSchedulerConfig{
		StepStore: stepStore,
		TaskStore: taskStore,
		Queue:     &mockQueue{},
		Bus:       msgBus,
		Logger:    slogDiscardLogger(),
	})

	req := HandoffRequest{
		TaskID:        tk.ID,
		FromStepID:    stepA.ID,
		FromAgentID:   config.AgentIDCoder,
		ToAgentID:     config.AgentIDDebugger,
		Description:   "Debug independently",
		Reason:        "parallel debug",
		PartialResult: "partial result",
		InjectAfter:   false, // no inject_after = no rewiring
	}

	msg := handoffBusMsg(req)
	err := scheduler.HandleHandoff(context.Background(), msg)
	if err != nil {
		t.Fatalf("HandleHandoff failed: %v", err)
	}

	// Verify B still depends on A (not rewired)
	steps, err := stepStore.ListByTaskID(tk.ID)
	if err != nil {
		t.Fatalf("failed to list steps: %v", err)
	}

	for _, s := range steps {
		if s.Description == "Step B" {
			if len(s.DependsOn) != 1 || s.DependsOn[0] != stepA.ID {
				t.Errorf("step B should still depend on A (no rewire), got %v", s.DependsOn)
			}
			break
		}
	}
}

// mockAmendmentManager implements AmendmentSubmitter for testing.
type mockAmendmentManager struct {
	submitFn  func(ctx context.Context, req *task.AmendmentRequest) error
	processFn func(ctx context.Context, requestID string) (*task.AmendmentReply, error)
}

func (m *mockAmendmentManager) Submit(ctx context.Context, req *task.AmendmentRequest) error {
	if m.submitFn != nil {
		return m.submitFn(ctx, req)
	}
	return nil
}

func (m *mockAmendmentManager) Process(ctx context.Context, requestID string) (*task.AmendmentReply, error) {
	if m.processFn != nil {
		return m.processFn(ctx, requestID)
	}
	return &task.AmendmentReply{
		Success:  true,
		Metadata: json.RawMessage(`{"step_id":"step-mock-1"}`),
	}, nil
}

func TestTacticalScheduler_HandleHandoff_RateLimitReached(t *testing.T) {
	taskStore, stepStore := newTestTaskAndStepStore(t)
	msgBus := bus.New(nil, slogDiscardLogger())
	defer msgBus.Close()

	// Create a task
	tk := task.NewTask("task-ratelimit-1", "rate limit test")
	tk.TotalJobs = 3
	if err := taskStore.Create(tk); err != nil {
		t.Fatalf("failed to create task: %v", err)
	}

	// Create the from step (completed so handoff steps can be promoted)
	fromStep := task.NewTaskStep(tk.ID, "initial step", 1)
	fromStep.State = task.StepCompleted
	if err := stepStore.Create(fromStep); err != nil {
		t.Fatalf("failed to create from step: %v", err)
	}

	// Create 5 handoff steps (max) to hit the rate limit
	for i := 0; i < 5; i++ {
		handoffStep := task.NewTaskStep(tk.ID, fmt.Sprintf("handoff step %d", i), 100+i)
		handoffStep.State = task.StepPending
		handoffStep.AccumulatedContext = fmt.Sprintf("[Handoff from coder]: partial result %d", i)
		if err := stepStore.Create(handoffStep); err != nil {
			t.Fatalf("failed to create handoff step %d: %v", i, err)
		}
	}

	scheduler := NewTacticalScheduler(TacticalSchedulerConfig{
		StepStore:        stepStore,
		TaskStore:        taskStore,
		Queue:            &mockQueue{},
		Bus:              msgBus,
		Logger:           slogDiscardLogger(),
		MaxHandoffSteps:  5,
		HandoffUseAmendment: false, // direct path for simplicity
	})

	req := HandoffRequest{
		TaskID:        tk.ID,
		FromStepID:    fromStep.ID,
		FromAgentID:   config.AgentIDCoder,
		ToAgentID:     config.AgentIDDebugger,
		Description:   "This should be rejected",
		Reason:        "exceeds rate limit",
		PartialResult: "too many handoffs",
		InjectAfter:    true,
	}

	msg := handoffBusMsg(req)
	err := scheduler.HandleHandoff(context.Background(), msg)
	if err == nil {
		t.Fatal("expected error for rate limit, got nil")
	}
	if !strings.Contains(err.Error(), "handoff rate limit reached") {
		t.Errorf("expected rate limit error, got: %v", err)
	}
}

func TestTacticalScheduler_HandleHandoff_AmendmentPath(t *testing.T) {
	taskStore, stepStore := newTestTaskAndStepStore(t)
	msgBus := bus.New(nil, slogDiscardLogger())
	defer msgBus.Close()

	// Create a task
	tk := task.NewTask("task-amend-1", "amendment path test")
	tk.TotalJobs = 1
	if err := taskStore.Create(tk); err != nil {
		t.Fatalf("failed to create task: %v", err)
	}

	fromStep := task.NewTaskStep(tk.ID, "initial step", 1)
	fromStep.State = task.StepCompleted
	if err := stepStore.Create(fromStep); err != nil {
		t.Fatalf("failed to create from step: %v", err)
	}

	var submittedReq *task.AmendmentRequest
	var processedID string
	mockAmend := &mockAmendmentManager{
		submitFn: func(ctx context.Context, req *task.AmendmentRequest) error {
			submittedReq = req
			return nil
		},
		processFn: func(ctx context.Context, requestID string) (*task.AmendmentReply, error) {
			processedID = requestID

			// Create the step that the amendment handler would create
			steps, _ := stepStore.ListByTaskID(tk.ID)
			sequence := len(steps) + 1
			newStep := task.NewTaskStep(tk.ID, "Debug the failing test", sequence)
			newStep.ToolHint = "debug"
			newStep.DependsOn = []string{fromStep.ID}
			if err := stepStore.Create(newStep); err != nil {
				t.Logf("failed to create step in mock: %v", err)
			}

			meta, _ := json.Marshal(map[string]string{"step_id": newStep.ID})
			return &task.AmendmentReply{
				Success:  true,
				Message:  fmt.Sprintf("Step %s added", newStep.ID),
				Metadata: meta,
			}, nil
		},
	}

	scheduler := NewTacticalScheduler(TacticalSchedulerConfig{
		StepStore:           stepStore,
		TaskStore:           taskStore,
		Queue:               &mockQueue{},
		Bus:                 msgBus,
		Logger:              slogDiscardLogger(),
		MaxHandoffSteps:     5,
		HandoffUseAmendment: true,
		AmendmentManager:    mockAmend,
	})

	req := HandoffRequest{
		TaskID:        tk.ID,
		FromStepID:    fromStep.ID,
		FromAgentID:   config.AgentIDCoder,
		ToAgentID:     config.AgentIDDebugger,
		Description:   "Debug the failing test",
		Reason:        "Tests are failing",
		PartialResult: "Implemented feature X",
		InjectAfter:    true,
	}

	msg := handoffBusMsg(req)
	err := scheduler.HandleHandoff(context.Background(), msg)
	if err != nil {
		t.Fatalf("HandleHandoff failed: %v", err)
	}

	// Verify Submit was called with correct metadata
	if submittedReq == nil {
		t.Fatal("expected amendment to be submitted")
	}
	if submittedReq.Type != task.AmendmentAddStep {
		t.Errorf("expected amendment type %s, got %s", task.AmendmentAddStep, submittedReq.Type)
	}
	if submittedReq.TaskID != tk.ID {
		t.Errorf("expected task ID %s, got %s", tk.ID, submittedReq.TaskID)
	}

	// Verify metadata contains expected fields
	var meta map[string]any
	if err := json.Unmarshal(submittedReq.Metadata, &meta); err != nil {
		t.Fatalf("failed to unmarshal amendment metadata: %v", err)
	}
	if meta["description"] != "Debug the failing test" {
		t.Errorf("expected description in metadata, got: %v", meta["description"])
	}
	if meta["tool_hint"] != "debug" {
		t.Errorf("expected tool_hint in metadata, got: %v", meta["tool_hint"])
	}
	if meta["agent_id"] != config.AgentIDDebugger {
		t.Errorf("expected agent_id %s in metadata, got: %v", config.AgentIDDebugger, meta["agent_id"])
	}

	// Verify Process was called
	if processedID == "" {
		t.Fatal("expected amendment to be processed")
	}
	if processedID != submittedReq.ID {
		t.Errorf("expected Process to be called with submit ID %s, got %s", submittedReq.ID, processedID)
	}

	// Verify accumulated context was set on the created step
	steps, err := stepStore.ListByTaskID(tk.ID)
	if err != nil {
		t.Fatalf("failed to list steps: %v", err)
	}

	var createdStep *task.TaskStep
	for _, s := range steps {
		if s.Description == "Debug the failing test" {
			createdStep = s
			break
		}
	}
	if createdStep == nil {
		t.Fatal("created step not found")
	}
	if createdStep.AccumulatedContext == "" {
		t.Error("expected accumulated context to be set on amendment-created step")
	}
	if !strings.Contains(createdStep.AccumulatedContext, "[Handoff from") {
		t.Errorf("expected accumulated context to contain handoff marker, got: %s", createdStep.AccumulatedContext)
	}
}

func TestTacticalScheduler_HandleHandoff_AmendmentPathRejected(t *testing.T) {
	taskStore, stepStore := newTestTaskAndStepStore(t)
	msgBus := bus.New(nil, slogDiscardLogger())
	defer msgBus.Close()

	tk := task.NewTask("task-amend-rej-1", "amendment rejection test")
	tk.TotalJobs = 1
	if err := taskStore.Create(tk); err != nil {
		t.Fatalf("failed to create task: %v", err)
	}

	fromStep := task.NewTaskStep(tk.ID, "initial step", 1)
	fromStep.State = task.StepCompleted
	if err := stepStore.Create(fromStep); err != nil {
		t.Fatalf("failed to create from step: %v", err)
	}

	mockAmend := &mockAmendmentManager{
		submitFn: func(ctx context.Context, req *task.AmendmentRequest) error {
			return nil
		},
		processFn: func(ctx context.Context, requestID string) (*task.AmendmentReply, error) {
			return &task.AmendmentReply{
				Success: false,
				Message: "handoff exceeds task capacity",
			}, nil
		},
	}

	scheduler := NewTacticalScheduler(TacticalSchedulerConfig{
		StepStore:           stepStore,
		TaskStore:           taskStore,
		Queue:               &mockQueue{},
		Bus:                 msgBus,
		Logger:              slogDiscardLogger(),
		MaxHandoffSteps:     5,
		HandoffUseAmendment: true,
		AmendmentManager:    mockAmend,
	})

	req := HandoffRequest{
		TaskID:        tk.ID,
		FromStepID:    fromStep.ID,
		FromAgentID:   config.AgentIDCoder,
		ToAgentID:     config.AgentIDDebugger,
		Description:   "This should be rejected",
		Reason:        "exceeds capacity",
		PartialResult: "partial",
		InjectAfter:    true,
	}

	msg := handoffBusMsg(req)
	err := scheduler.HandleHandoff(context.Background(), msg)
	if err == nil {
		t.Fatal("expected error for rejected amendment, got nil")
	}
	if !strings.Contains(err.Error(), "handoff amendment rejected") {
		t.Errorf("expected amendment rejection error, got: %v", err)
	}
	if !strings.Contains(err.Error(), "exceeds task capacity") {
		t.Errorf("expected rejection message in error, got: %v", err)
	}

	// Verify no new step was created
	steps, err := stepStore.ListByTaskID(tk.ID)
	if err != nil {
		t.Fatalf("failed to list steps: %v", err)
	}
	if len(steps) != 1 {
		t.Errorf("expected 1 step (from step only), got %d", len(steps))
	}
}

func TestTacticalScheduler_HandleHandoff_AmendmentFallbackToDirect(t *testing.T) {
	// When handoffUseAmendment is true but amendmentMgr is nil,
	// it should fall through to the direct creation path without error.
	taskStore, stepStore := newTestTaskAndStepStore(t)
	msgBus := bus.New(nil, slogDiscardLogger())
	defer msgBus.Close()

	tk := task.NewTask("task-fallback-1", "amendment fallback test")
	tk.TotalJobs = 1
	if err := taskStore.Create(tk); err != nil {
		t.Fatalf("failed to create task: %v", err)
	}

	fromStep := task.NewTaskStep(tk.ID, "initial step", 1)
	fromStep.State = task.StepCompleted
	if err := stepStore.Create(fromStep); err != nil {
		t.Fatalf("failed to create from step: %v", err)
	}

	// Amendment enabled but no manager — should fall through to direct path
	scheduler := NewTacticalScheduler(TacticalSchedulerConfig{
		StepStore:           stepStore,
		TaskStore:           taskStore,
		Queue:               &mockQueue{},
		Bus:                 msgBus,
		Logger:              slogDiscardLogger(),
		MaxHandoffSteps:     5,
		HandoffUseAmendment: true,
		AmendmentManager:    nil, // nil manager triggers fallback
	})

	req := HandoffRequest{
		TaskID:       tk.ID,
		FromStepID:   fromStep.ID,
		FromAgentID:  config.AgentIDCoder,
		ToAgentID:    config.AgentIDDebugger,
		Description:  "Fallback to direct creation",
		Reason:       "amendment manager unavailable",
		InjectAfter:   true,
	}

	msg := handoffBusMsg(req)
	err := scheduler.HandleHandoff(context.Background(), msg)
	if err != nil {
		t.Fatalf("expected fallback to direct path, got error: %v", err)
	}

	// Verify step was created via direct path
	steps, err := stepStore.ListByTaskID(tk.ID)
	if err != nil {
		t.Fatalf("failed to list steps: %v", err)
	}
	if len(steps) != 2 {
		t.Fatalf("expected 2 steps (from + created), got %d", len(steps))
	}

	// Find the new step
	var created *task.TaskStep
	for _, s := range steps {
		if s.ID != fromStep.ID {
			created = s
			break
		}
	}
	if created == nil {
		t.Fatal("created step not found")
	}
	if created.Description != "Fallback to direct creation" {
		t.Errorf("unexpected description: %q", created.Description)
	}
	if created.AccumulatedContext == "" {
		t.Error("expected accumulated context to be set")
	}
}
