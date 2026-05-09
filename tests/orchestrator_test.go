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
	"github.com/caimlas/meept/pkg/models"

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

// TestOrchestratorPlanFlow_EndToEnd verifies the full plan-request-to-scheduled-job
// chain: publishPlanRequest -> orchestrator.plan -> StrategicPlanner.Plan() ->
// task.planned + orchestrator.schedule -> TacticalScheduler.ScheduleReadySteps().
// The test uses the full testEnv with real SQLite stores, queue, and bus.
func TestOrchestratorPlanFlow_EndToEnd(t *testing.T) {
	env := setupTestEnv(t)
	defer env.cleanup()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Subscribe to key bus topics to observe the flow
	taskPlannedSub := env.bus.Subscribe("e2e-test", "task.planned")
	defer env.bus.Unsubscribe(taskPlannedSub)

	orchScheduleSub := env.bus.Subscribe("e2e-test", "orchestrator.schedule")
	defer env.bus.Unsubscribe(orchScheduleSub)

	taskProgressSub := env.bus.Subscribe("e2e-test", "task.progress")
	defer env.bus.Unsubscribe(taskProgressSub)

	// Start the orchestrator (subscribes to orchestrator.plan, orchestrator.schedule, etc.)
	if err := env.orch.Start(ctx); err != nil {
		t.Fatalf("Failed to start orchestrator: %v", err)
	}
	defer env.orch.Stop(ctx)

	// Give subscriptions time to register
	time.Sleep(50 * time.Millisecond)

	// Create a task in the store (this would normally be done by the Dispatcher)
	tsk := task.NewTask("e2e-task", "Implement user authentication")
	if err := env.taskStore.Create(tsk); err != nil {
		t.Fatalf("failed to create task: %v", err)
	}

	// Publish a plan request to the bus (simulating ChatHandler.publishPlanRequest)
	planReq := agent.PlanRequest{
		TaskID:    tsk.ID,
		SessionID: "session-e2e-test",
		Input:     "Implement user authentication",
		Intent:    "code",
	}
	planPayload, err := json.Marshal(planReq)
	if err != nil {
		t.Fatalf("failed to marshal plan request: %v", err)
	}

	planMsg := &models.BusMessage{
		ID:        "e2e-plan-msg-001",
		Type:      models.MessageTypeRequest,
		Topic:     "orchestrator.plan",
		Source:    "chat-handler",
		Timestamp: time.Now().UTC(),
		Payload:   planPayload,
	}
	env.bus.Publish("orchestrator.plan", planMsg)

	// Step 1: Verify task.planned event was published by StrategicPlanner
	select {
	case msg := <-taskPlannedSub.Channel:
		var event map[string]any
		if err := json.Unmarshal(msg.Payload, &event); err != nil {
			t.Fatalf("failed to unmarshal task.planned: %v", err)
		}
		if event["task_id"] != tsk.ID {
			t.Errorf("task.planned task_id = %v, want %s", event["task_id"], tsk.ID)
		}
		if event["session_id"] != "session-e2e-test" {
			t.Errorf("task.planned session_id = %v, want session-e2e-test", event["session_id"])
		}
		totalSteps, _ := event["total_steps"].(float64)
		if totalSteps < 1 {
			t.Errorf("task.planned total_steps = %v, want >= 1", event["total_steps"])
		}
		t.Logf("task.planned received: total_steps=%.0f, ready_steps=%v",
			totalSteps, event["ready_steps"])
	case <-time.After(5 * time.Second):
		t.Fatal("Timeout waiting for task.planned event")
	}

	// Step 2: Verify orchestrator.schedule event was published
	select {
	case msg := <-orchScheduleSub.Channel:
		var event map[string]any
		if err := json.Unmarshal(msg.Payload, &event); err != nil {
			t.Fatalf("failed to unmarshal orchestrator.schedule: %v", err)
		}
		if event["task_id"] != tsk.ID {
			t.Errorf("orchestrator.schedule task_id = %v, want %s", event["task_id"], tsk.ID)
		}
		t.Logf("orchestrator.schedule received for task %s", tsk.ID)
	case <-time.After(5 * time.Second):
		t.Fatal("Timeout waiting for orchestrator.schedule event")
	}

	// Step 3: Verify the tactical scheduler picked up the schedule request and
	// scheduled ready steps (task.progress is published after scheduling).
	select {
	case msg := <-taskProgressSub.Channel:
		var event map[string]any
		if err := json.Unmarshal(msg.Payload, &event); err != nil {
			t.Fatalf("failed to unmarshal task.progress: %v", err)
		}
		if event["task_id"] != tsk.ID {
			t.Errorf("task.progress task_id = %v, want %s", event["task_id"], tsk.ID)
		}
		scheduled, _ := event["scheduled_steps"].(float64)
		if scheduled < 1 {
			t.Errorf("task.progress scheduled_steps = %v, want >= 1", event["scheduled_steps"])
		}
		t.Logf("task.progress received: scheduled_steps=%.0f", scheduled)
	case <-time.After(5 * time.Second):
		t.Fatal("Timeout waiting for task.progress event from tactical scheduler")
	}

	// Step 4: Verify the task state was updated and steps were persisted
	updatedTask, err := env.taskStore.GetByID(tsk.ID)
	if err != nil {
		t.Fatalf("failed to get updated task: %v", err)
	}
	if updatedTask.State != task.StateExecuting {
		t.Errorf("task state = %q, want %q", updatedTask.State, task.StateExecuting)
	}
	if updatedTask.TotalJobs < 1 {
		t.Errorf("task TotalJobs = %d, want >= 1", updatedTask.TotalJobs)
	}

	// Verify steps were persisted and scheduled
	steps, err := env.stepStore.ListByTaskID(tsk.ID)
	if err != nil {
		t.Fatalf("failed to list steps: %v", err)
	}
	if len(steps) == 0 {
		t.Fatal("expected at least 1 step to be persisted")
	}

	// The fallback path creates a single root step with no dependencies.
	// It should be promoted to ready and then scheduled.
	foundScheduled := false
	for _, s := range steps {
		if s.State == task.StepScheduled || s.State == task.StepReady {
			foundScheduled = true
			if s.JobID == "" && s.State == task.StepScheduled {
				t.Errorf("scheduled step %s has no job ID", s.ID)
			}
			t.Logf("step %s: state=%s, agent=%s, job_id=%s",
				s.ID, s.State, s.AgentID, s.JobID)
		}
	}
	if !foundScheduled {
		t.Errorf("expected at least one step to be scheduled or ready, got states: %v",
			stepStates(steps))
	}
}

// stepStates returns a summary of step states for error messages.
func stepStates(steps []*task.TaskStep) []string {
	states := make([]string, len(steps))
	for i, s := range steps {
		states[i] = string(s.State)
	}
	return states
}
