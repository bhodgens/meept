package agent

import (
	"encoding/json"
	"sync"
	"testing"

	_ "modernc.org/sqlite"
	"time"

	"github.com/caimlas/meept/internal/bus"
	"github.com/caimlas/meept/internal/task"
)

// Note: slogDiscardLogger is defined in handler_test.go (same package).

func TestSelectAgent(t *testing.T) {
	ts := &TacticalScheduler{}

	tests := []struct {
		toolHint  string
		wantAgent string
	}{
		{"code", "coder"},
		{"refactor", "coder"},
		{"debug", "debugger"},
		{"fix", "debugger"},
		{"analyze", "analyst"},
		{"research", "analyst"},
		{"git", "committer"},
		{"commit", "committer"},
		{"schedule", "scheduler"},
		{"plan", "planner"},
		{"chat", "chat"},
		{"", "chat"},
		{"unknown", "chat"},
	}

	for _, tt := range tests {
		step := &task.TaskStep{ToolHint: tt.toolHint}
		got := ts.selectAgent(step)
		if got != tt.wantAgent {
			t.Errorf("selectAgent(%q) = %q, want %q", tt.toolHint, got, tt.wantAgent)
		}
	}
}

func TestTacticalScheduler_IsRateLimitError(t *testing.T) {
	ts := &TacticalScheduler{}

	cases := []struct {
		msg  string
		want bool
	}{
		{"HTTP 429: Too Many Requests", true},
		{"anthropic: rate limit exceeded", true},
		{"quota exceeded for model", true},
		{"rate_limit_error on provider X", true},
		{"", false},
		{"context deadline exceeded", false},
		{"permission denied", false},
	}
	for _, tc := range cases {
		if got := ts.isRateLimitError(tc.msg); got != tc.want {
			t.Errorf("isRateLimitError(%q) = %v, want %v", tc.msg, got, tc.want)
		}
	}
}

func TestTacticalScheduler_Semaphore(t *testing.T) {
	// Test that semaphores are initialized correctly
	ts := NewTacticalScheduler(TacticalSchedulerConfig{
		MaxConcurrentJobs:     5,
		MaxConcurrentPerAgent: 2,
	})

	if ts.globalSemaphore == nil {
		t.Fatal("globalSemaphore should be initialized")
	}
	if cap(ts.globalSemaphore) != 5 {
		t.Errorf("globalSemaphore cap = %d, want 5", cap(ts.globalSemaphore))
	}

	if ts.agentSemaphore == nil {
		t.Fatal("agentSemaphore should be initialized")
	}

	// Test acquireSlots and releaseSlots
	t.Run("AcquireAndRelease", func(t *testing.T) {
		// Should be able to acquire slots for known agents
		if !ts.acquireSlots("coder") {
			t.Error("should be able to acquire slots for coder")
		}

		// Acquire again (up to limit)
		if !ts.acquireSlots("coder") {
			t.Error("should be able to acquire second slot for coder")
		}

		// Third acquire should fail (limit is 2)
		if ts.acquireSlots("coder") {
			t.Error("should not be able to acquire third slot for coder")
		}

		// Release one slot
		ts.releaseSlots("coder")

		// Should be able to acquire again
		if !ts.acquireSlots("coder") {
			t.Error("should be able to acquire slot after release")
		}
	})

	t.Run("GlobalSemaphoreLimit", func(t *testing.T) {
		// Create a new scheduler with small limits for testing
		ts2 := NewTacticalScheduler(TacticalSchedulerConfig{
			MaxConcurrentJobs:     3,
			MaxConcurrentPerAgent: 10,
		})

		// Acquire all global slots
		for i := range 3 {
			if !ts2.acquireSlots("coder") {
				t.Errorf("should acquire global slot %d", i)
			}
		}

		// Next acquire should fail
		if ts2.acquireSlots("coder") {
			t.Error("should not acquire when global semaphore full")
		}

		// Release all
		for range 3 {
			ts2.releaseSlots("coder")
		}
	})

	t.Run("ReleaseUnknownAgent", func(t *testing.T) {
		// Should not panic when releasing unknown agent
		ts.releaseSlots("unknown-agent")
	})

	t.Run("ConcurrentAccess", func(t *testing.T) {
		ts3 := NewTacticalScheduler(TacticalSchedulerConfig{
			MaxConcurrentJobs:     10,
			MaxConcurrentPerAgent: 5,
		})

		var wg sync.WaitGroup
		acquired := make(chan bool, 20)
		done := make(chan struct{})

		// Try to acquire from multiple goroutines, holding slots until done
		for range 20 {
			wg.Go(func() {
				if ts3.acquireSlots("coder") {
					acquired <- true
					<-done // Hold slot until signaled
					ts3.releaseSlots("coder")
				} else {
					acquired <- false
				}
			})
		}

		// Give goroutines time to acquire
		time.Sleep(100 * time.Millisecond)
		close(done) // Release all goroutines

		wg.Wait()
		close(acquired)

		// Count successful acquisitions
		count := 0
		for ok := range acquired {
			if ok {
				count++
			}
		}

		// Should have at most 10 successful (global limit)
		if count > 10 {
			t.Errorf("too many successful acquisitions: got %d, want <= 10", count)
		}
	})
}

// newTacticalTestSetup creates a TacticalScheduler backed by real stores and a message bus.
func newTacticalTestSetup(t *testing.T) (*TacticalScheduler, *bus.MessageBus, func()) {
	t.Helper()

	taskStore, err := task.NewStore(t.TempDir()+"/tasks.db", nil)
	if err != nil {
		t.Fatalf("failed to create task store: %v", err)
	}

	msgBus := bus.New(nil, slogDiscardLogger())

	ts := NewTacticalScheduler(TacticalSchedulerConfig{
		StepStore: taskStore.StepStore(),
		TaskStore: taskStore,
		Bus:       msgBus,
		Logger:    slogDiscardLogger(),
	})

	cleanup := func() {
		taskStore.Close()
	}

	return ts, msgBus, cleanup
}

// TestTacticalScheduler_AggregateTokens verifies that OnJobCompleted aggregates
// step token usage to the parent task and publishes a task.progress event
// with token_usage data.
func TestTacticalScheduler_AggregateTokens(t *testing.T) {
	ts, msgBus, cleanup := newTacticalTestSetup(t)
	defer cleanup()

	// Subscribe to task.progress events
	progressSub := msgBus.Subscribe("test-aggregate", "task.progress")
	defer msgBus.Unsubscribe(progressSub)

	// Create a task with 2 total jobs
	parentTask := task.NewTask("aggregate-test", "token aggregation test")
	parentTask.TotalJobs = 2
	parentTask.SetState(task.StateExecuting)
	if err := ts.taskStore.Create(parentTask); err != nil {
		t.Fatalf("failed to create task: %v", err)
	}

	// Create a step and link it to the task
	step1 := task.NewTaskStep(parentTask.ID, "first step", 0)
	step1.State = task.StepCompleted
	step1.AgentID = "coder"
	step1.TokenUsage = 500
	if err := ts.stepStore.Create(step1); err != nil {
		t.Fatalf("failed to create step: %v", err)
	}

	// Build a job result payload with token_usage
	resultPayload := map[string]any{
		"success":     true,
		"result":      "step completed successfully",
		"token_usage": 500,
	}
	resultJSON, _ := json.Marshal(resultPayload)

	// We need to call OnJobCompleted, but it looks up the step by job ID.
	// Set a job ID on the step so GetByJobID can find it.
	if err := ts.stepStore.SetJobID(step1.ID, "job-test-1"); err != nil {
		t.Fatalf("failed to set job ID: %v", err)
	}

	// Also need to make sure the step state is running so the flow proceeds.
	// OnJobCompleted reads the step, so we need it to be in a non-terminal state
	// initially (it will set completed/approved). Reset to scheduled.
	if err := ts.stepStore.SetState(step1.ID, task.StepScheduled); err != nil {
		t.Fatalf("failed to set step state: %v", err)
	}
	// Re-set token usage since SetState doesn't touch it
	step1.TokenUsage = 500

	// Create a second step so AreAllCompleted returns false (task stays executing).
	// Give it a dependency on a non-existent step so PromoteReadySteps won't
	// promote it, avoiding the need for a mock queue.
	step2 := task.NewTaskStep(parentTask.ID, "second step", 1)
	step2.State = task.StepPending
	step2.DependsOn = []string{"nonexistent-step-dep"}
	if err := ts.stepStore.Create(step2); err != nil {
		t.Fatalf("failed to create step 2: %v", err)
	}

	// Call OnJobCompleted
	err := ts.OnJobCompleted(t.Context(), "job-test-1", resultJSON)
	if err != nil {
		t.Fatalf("OnJobCompleted failed: %v", err)
	}

	// Verify the task's token usage was aggregated
	updatedTask, err := ts.taskStore.GetByID(parentTask.ID)
	if err != nil {
		t.Fatalf("failed to get task after completion: %v", err)
	}
	if updatedTask.TokenUsage != 500 {
		t.Errorf("expected task token_usage 500, got %d", updatedTask.TokenUsage)
	}
	if updatedTask.CompletedJobs != 1 {
		t.Errorf("expected completed_jobs 1, got %d", updatedTask.CompletedJobs)
	}

	// Verify a task.progress event was published with token_usage
	select {
	case msg := <-progressSub.Channel:
		var payload map[string]any
		if err := json.Unmarshal(msg.Payload, &payload); err != nil {
			t.Fatalf("failed to unmarshal progress event: %v", err)
		}
		if payload["task_id"] != parentTask.ID {
			t.Errorf("progress event task_id = %v, want %s", payload["task_id"], parentTask.ID)
		}
		if tokens, ok := payload["token_usage"].(float64); !ok || int(tokens) != 500 {
			t.Errorf("progress event token_usage = %v, want 500", payload["token_usage"])
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for task.progress event")
	}
}

// TestTacticalScheduler_TokenProgressIncludesUsage verifies that all three
// task.progress publish sites in OnJobCompleted and OnJobFailed include
// the token_usage field.
func TestTacticalScheduler_TokenProgressIncludesUsage(t *testing.T) {
	ts, msgBus, cleanup := newTacticalTestSetup(t)
	defer cleanup()

	progressSub := msgBus.Subscribe("test-includes", "task.progress")
	defer msgBus.Unsubscribe(progressSub)

	// Create a task
	parentTask := task.NewTask("progress-tokens", "progress token test")
	parentTask.TotalJobs = 1
	parentTask.SetState(task.StateExecuting)
	parentTask.TokenUsage = 1200 // Pre-existing tokens
	if err := ts.taskStore.Create(parentTask); err != nil {
		t.Fatalf("failed to create task: %v", err)
	}

	// Create and schedule a step (mark as validated so task-level validation passes)
	step1 := task.NewTaskStep(parentTask.ID, "step with tokens", 0)
	step1.AgentID = "coder"
	step1.Validated = true
	step1.TokenUsage = 300
	if err := ts.stepStore.Create(step1); err != nil {
		t.Fatalf("failed to create step: %v", err)
	}
	if err := ts.stepStore.SetJobID(step1.ID, "job-progress-1"); err != nil {
		t.Fatalf("failed to set job ID: %v", err)
	}
	if err := ts.stepStore.SetState(step1.ID, task.StepScheduled); err != nil {
		t.Fatalf("failed to set step state: %v", err)
	}

	resultPayload := map[string]any{
		"success":     true,
		"result":      "done",
		"token_usage": 300,
	}
	resultJSON, _ := json.Marshal(resultPayload)

	err := ts.OnJobCompleted(t.Context(), "job-progress-1", resultJSON)
	if err != nil {
		t.Fatalf("OnJobCompleted failed: %v", err)
	}

	// Drain events - there should be at least one task.progress with token_usage
	foundTokenUsage := false
	deadline := time.After(2 * time.Second)
	for !foundTokenUsage {
		select {
		case msg := <-progressSub.Channel:
			var payload map[string]any
			if err := json.Unmarshal(msg.Payload, &payload); err != nil {
				continue
			}
			if tokens, ok := payload["token_usage"]; ok {
				tokenVal := int(tokens.(float64))
				if tokenVal > 0 {
					foundTokenUsage = true
					// Task should have 1200 (pre-existing) + 300 (step) = 1500
					if tokenVal != 1500 {
						t.Errorf("expected token_usage 1500, got %d", tokenVal)
					}
				}
			}
		case <-deadline:
			t.Fatal("timeout waiting for task.progress event with token_usage")
		}
	}
}

// TestTacticalScheduler_PublishTokenProgress verifies the publishTokenProgress method.
func TestTacticalScheduler_PublishTokenProgress(t *testing.T) {
	msgBus := bus.New(nil, slogDiscardLogger())
	defer msgBus.Close()

	sub := msgBus.Subscribe("test-pub", "task.progress")
	defer msgBus.Unsubscribe(sub)

	ts := &TacticalScheduler{
		bus:    msgBus,
		logger: slogDiscardLogger(),
	}

	testTask := &task.Task{
		ID:            "task-pub-test",
		CompletedJobs: 3,
		TotalJobs:     5,
		TokenUsage:    2500,
	}

	ts.publishTokenProgress(testTask)

	select {
	case msg := <-sub.Channel:
		var payload map[string]any
		if err := json.Unmarshal(msg.Payload, &payload); err != nil {
			t.Fatalf("failed to unmarshal: %v", err)
		}
		if payload["task_id"] != "task-pub-test" {
			t.Errorf("task_id = %v, want task-pub-test", payload["task_id"])
		}
		if tokens, ok := payload["token_usage"].(float64); !ok || int(tokens) != 2500 {
			t.Errorf("token_usage = %v, want 2500", payload["token_usage"])
		}
		if completed, ok := payload["completed_jobs"].(float64); !ok || int(completed) != 3 {
			t.Errorf("completed_jobs = %v, want 3", payload["completed_jobs"])
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for publishTokenProgress event")
	}
}
