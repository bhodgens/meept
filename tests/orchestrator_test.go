package tests

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/agent"
	"github.com/caimlas/meept/internal/bus"
	"github.com/caimlas/meept/internal/queue"
	"github.com/caimlas/meept/internal/task"

	_ "github.com/mattn/go-sqlite3"
)

// testEnv holds the test environment components.
type testEnv struct {
	tmpDir    string
	bus       *bus.MessageBus
	taskStore *task.Store
	stepStore *task.StepStore
	queue     *queue.PersistentQueue
	strategic *agent.StrategicPlanner
	tactical  *agent.TacticalScheduler
	orch      *agent.Orchestrator
}

func setupTestEnv(t *testing.T) *testEnv {
	t.Helper()

	// Create temp directory for test databases
	tmpDir, err := os.MkdirTemp("", "meept-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}

	msgBus := bus.New(nil, nil)

	taskDBPath := filepath.Join(tmpDir, "tasks.db")
	taskStore, err := task.NewStore(taskDBPath, nil)
	if err != nil {
		os.RemoveAll(tmpDir)
		t.Fatalf("failed to create task store: %v", err)
	}

	stepStore := taskStore.StepStore()

	queueDBPath := filepath.Join(tmpDir, "queue.db")
	q, err := queue.NewPersistentQueue(queueDBPath, msgBus, nil)
	if err != nil {
		taskStore.Close()
		os.RemoveAll(tmpDir)
		t.Fatalf("failed to create queue: %v", err)
	}

	// Note: We create strategic without a real agent registry
	// This test focuses on the step/scheduling flow, not actual LLM calls
	strategic := agent.NewStrategicPlanner(agent.StrategicPlannerConfig{
		Registry:       nil, // Will test fallback path
		TaskStore:      taskStore,
		StepStore:      stepStore,
		Bus:            msgBus,
		MaxPlanSteps:   5,
		PlannerTimeout: 10 * time.Second,
	})

	tactical := agent.NewTacticalScheduler(agent.TacticalSchedulerConfig{
		StepStore: stepStore,
		TaskStore: taskStore,
		Queue:     q,
		Registry:  nil,
		Bus:       msgBus,
	})

	orch := agent.NewOrchestrator(agent.OrchestratorDeps{
		Strategic: strategic,
		Tactical:  tactical,
		Bus:       msgBus,
	})

	return &testEnv{
		tmpDir:    tmpDir,
		bus:       msgBus,
		taskStore: taskStore,
		stepStore: stepStore,
		queue:     q,
		strategic: strategic,
		tactical:  tactical,
		orch:      orch,
	}
}

func (e *testEnv) cleanup() {
	e.bus.Close()
	e.queue.Close()
	e.taskStore.Close()
	os.RemoveAll(e.tmpDir)
}

func TestOrchestrator_StartStop(t *testing.T) {
	env := setupTestEnv(t)
	defer env.cleanup()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Start orchestrator
	if err := env.orch.Start(ctx); err != nil {
		t.Fatalf("Start failed: %v", err)
	}

	// Give it time to subscribe
	time.Sleep(50 * time.Millisecond)

	// Stop orchestrator
	if err := env.orch.Stop(ctx); err != nil {
		t.Fatalf("Stop failed: %v", err)
	}
}

func TestTacticalScheduler_ScheduleReadySteps(t *testing.T) {
	env := setupTestEnv(t)
	defer env.cleanup()

	ctx := context.Background()

	// Create a task
	tsk := task.NewTask("test-task", "Test task description")
	if err := env.taskStore.Create(tsk); err != nil {
		t.Fatalf("failed to create task: %v", err)
	}

	// Create a ready step
	step := task.NewTaskStep(tsk.ID, "Write code", 0)
	step.ToolHint = "code"
	if err := env.stepStore.Create(step); err != nil {
		t.Fatalf("failed to create step: %v", err)
	}
	if err := env.stepStore.SetState(step.ID, task.StepReady); err != nil {
		t.Fatalf("failed to set step ready: %v", err)
	}

	// Schedule ready steps
	if err := env.tactical.ScheduleReadySteps(ctx, tsk.ID); err != nil {
		t.Fatalf("ScheduleReadySteps failed: %v", err)
	}

	// Verify step is now scheduled
	updatedStep, err := env.stepStore.GetByID(step.ID)
	if err != nil {
		t.Fatalf("GetByID failed: %v", err)
	}
	if updatedStep.State != task.StepScheduled {
		t.Errorf("expected step state %q, got %q", task.StepScheduled, updatedStep.State)
	}
	if updatedStep.JobID == "" {
		t.Error("expected step to have a job ID")
	}
	if updatedStep.AgentID != "coder" {
		t.Errorf("expected agent ID 'coder' for tool_hint 'code', got %q", updatedStep.AgentID)
	}

	// Verify job was created
	job, err := env.queue.Get(ctx, updatedStep.JobID)
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}
	if job == nil {
		t.Fatal("expected job to exist")
	}
	if job.AgentID != "coder" {
		t.Errorf("job agent ID = %q, want 'coder'", job.AgentID)
	}
}

func TestTacticalScheduler_OnJobCompleted(t *testing.T) {
	env := setupTestEnv(t)
	defer env.cleanup()

	ctx := context.Background()

	// Create a task with 2 steps
	tsk := task.NewTask("test-task", "Test")
	tsk.TotalJobs = 2
	if err := env.taskStore.Create(tsk); err != nil {
		t.Fatalf("failed to create task: %v", err)
	}

	// Step 0: no deps (root)
	step0 := task.NewTaskStep(tsk.ID, "Step 0", 0)
	step0.ID = "step-0"
	if err := env.stepStore.Create(step0); err != nil {
		t.Fatalf("failed to create step0: %v", err)
	}

	// Step 1: depends on step 0
	step1 := task.NewTaskStep(tsk.ID, "Step 1", 1)
	step1.ID = "step-1"
	step1.DependsOn = []string{"step-0"}
	if err := env.stepStore.Create(step1); err != nil {
		t.Fatalf("failed to create step1: %v", err)
	}

	// Set step 0 as scheduled with a job ID
	env.stepStore.SetState(step0.ID, task.StepScheduled)
	env.stepStore.SetJobID(step0.ID, "job-0")

	// Complete job-0
	if err := env.tactical.OnJobCompleted(ctx, "job-0", json.RawMessage(`"done"`)); err != nil {
		t.Fatalf("OnJobCompleted failed: %v", err)
	}

	// Verify step 0 is completed
	s0, _ := env.stepStore.GetByID(step0.ID)
	if s0.State != task.StepCompleted {
		t.Errorf("step0 state = %q, want %q", s0.State, task.StepCompleted)
	}

	// Verify step 1 was promoted to ready (dependency satisfied)
	s1, _ := env.stepStore.GetByID(step1.ID)
	if s1.State != task.StepReady && s1.State != task.StepScheduled {
		t.Errorf("step1 state = %q, want ready or scheduled", s1.State)
	}

	// Verify task's completed jobs counter was incremented
	updatedTask, _ := env.taskStore.GetByID(tsk.ID)
	if updatedTask.CompletedJobs != 1 {
		t.Errorf("task CompletedJobs = %d, want 1", updatedTask.CompletedJobs)
	}
}

func TestTacticalScheduler_OnJobFailed(t *testing.T) {
	env := setupTestEnv(t)
	defer env.cleanup()

	ctx := context.Background()

	// Create a task with 1 step
	tsk := task.NewTask("test-task", "Test")
	tsk.TotalJobs = 1
	if err := env.taskStore.Create(tsk); err != nil {
		t.Fatalf("failed to create task: %v", err)
	}

	step := task.NewTaskStep(tsk.ID, "Failing step", 0)
	if err := env.stepStore.Create(step); err != nil {
		t.Fatalf("failed to create step: %v", err)
	}
	env.stepStore.SetState(step.ID, task.StepScheduled)
	env.stepStore.SetJobID(step.ID, "job-fail")

	// Fail the job
	if err := env.tactical.OnJobFailed(ctx, "job-fail", "simulated error"); err != nil {
		t.Fatalf("OnJobFailed failed: %v", err)
	}

	// Verify step is failed
	s, _ := env.stepStore.GetByID(step.ID)
	if s.State != task.StepFailed {
		t.Errorf("step state = %q, want %q", s.State, task.StepFailed)
	}

	// Verify task's failed jobs counter was incremented
	updatedTask, _ := env.taskStore.GetByID(tsk.ID)
	if updatedTask.FailedJobs != 1 {
		t.Errorf("task FailedJobs = %d, want 1", updatedTask.FailedJobs)
	}

	// Task should be failed since there are no remaining live steps
	if updatedTask.State != task.StateFailed {
		t.Errorf("task State = %q, want %q", updatedTask.State, task.StateFailed)
	}
}

func TestStepJobPayload_Marshal(t *testing.T) {
	payload := agent.StepJobPayload{
		StepID:      "step-123",
		TaskID:      "task-456",
		Description: "Do something",
		ToolHint:    "code",
	}

	data, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("Marshal failed: %v", err)
	}

	var decoded agent.StepJobPayload
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal failed: %v", err)
	}

	if decoded.StepID != payload.StepID {
		t.Errorf("StepID = %q, want %q", decoded.StepID, payload.StepID)
	}
	if decoded.TaskID != payload.TaskID {
		t.Errorf("TaskID = %q, want %q", decoded.TaskID, payload.TaskID)
	}
}
