package agent

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/bus"
	"github.com/caimlas/meept/internal/task"
	"github.com/caimlas/meept/pkg/models"
)

// newCapTestRalphLoop builds a RalphLoop over a temp task store with a live
// bus, returning the bus so tests can assert on published task.failed events.
func newCapTestRalphLoop(t *testing.T) (*RalphLoop, *bus.MessageBus) {
	t.Helper()

	dbPath := filepath.Join(t.TempDir(), "tasks.db")
	taskStore, err := task.NewStore(dbPath, nil)
	if err != nil {
		t.Fatalf("failed to create task store: %v", err)
	}
	t.Cleanup(func() { taskStore.Close() })

	logger := slogDiscardLogger()
	messageBus := bus.New(nil, logger)

	cfg := DefaultRalphLoopConfig()
	rl := NewRalphLoop(cfg, nil, taskStore, nil, nil, messageBus, logger)
	return rl, messageBus
}

// TestRalphLoop_TriggerReplan_CapFailsTask pins the e2e run-3 fix: once
// TriggerReplan has been called MaxIterations times for one task, further
// calls must NOT re-enqueue a replan — the task is marked failed so the
// sync wait terminalizes. Run 3's daemon.log shows iteration=1 three times
// for one task because an eager Reset zeroed the counter mid-flight.
func TestRalphLoop_TriggerReplan_CapFailsTask(t *testing.T) {
	rl, messageBus := newCapTestRalphLoop(t)

	tk := task.NewTask("replan cap task", "pins TriggerReplan MaxIterations")
	if err := rl.taskStore.Create(tk); err != nil {
		t.Fatalf("create task: %v", err)
	}

	failSub := messageBus.Subscribe("cap-test", "task.failed")
	failures := make(chan *models.BusMessage, 1) //nolint:staticcheck // test-only channel sizing
	go func() {
		for msg := range failSub.Channel {
			failures <- msg
		}
	}()

	ctx := context.Background()
	for i := 0; i < rl.config.MaxIterations; i++ {
		// Result carries evidence that does NOT match the task's key terms,
		// mirroring run 3's validateEvidence failure.
		result, _ := json.Marshal(map[string]any{
			"success": true,
			"result":  "done",
			"evidence": []string{
				"unrelated evidence that matches none of the key terms",
			},
		})
		isComplete, _, needsReplan := rl.CheckCompletion(ctx, tk.ID, result)
		if !needsReplan || isComplete {
			t.Fatalf("iteration %d: expected needsReplan=true/isComplete=false", i+1)
		}
		if err := rl.TriggerReplan(ctx, tk.ID, nil); err != nil {
			t.Fatalf("iteration %d: TriggerReplan: %v", i+1, err)
		}
	}

	// The task is still non-terminal at this point (last replan enqueued).
	got, err := rl.taskStore.GetByID(tk.ID)
	if err != nil {
		t.Fatalf("get task: %v", err)
	}
	if got.State.IsTerminal() {
		t.Fatalf("task terminal after %d replans; want still running", rl.config.MaxIterations)
	}

	// The NEXT replan request must hit the cap: no new replan, task failed.
	if err := rl.TriggerReplan(ctx, tk.ID, nil); err != nil {
		t.Fatalf("post-cap TriggerReplan returned error: %v", err)
	}

	select {
	case msg := <-failures:
		var payload struct {
			TaskID string `json:"task_id"`
		}
		if err := json.Unmarshal(msg.Payload, &payload); err != nil {
			t.Fatalf("unmarshal task.failed payload: %v", err)
		}
		if payload.TaskID != tk.ID {
			t.Fatalf("task.failed for %s, want %s", payload.TaskID, tk.ID)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("expected task.failed within 2s of capped TriggerReplan")
	}

	got, err = rl.taskStore.GetByID(tk.ID)
	if err != nil {
		t.Fatalf("get task after cap: %v", err)
	}
	if got.State != task.StateFailed {
		t.Fatalf("task state = %q, want failed at replan cap", got.State)
	}
}

// TestRalphLoop_ResetOnlyOnCompletedTask pins the orchestrator contract that
// replaced the eager Reset: TaskOutcome must report completed=true only for
// StateCompleted, so capped tasks keep their iteration counter armed.
func TestRalphLoop_TaskOutcome_DistinguishesCompletedFromFailed(t *testing.T) {
	rl, _ := newCapTestRalphLoop(t)

	completed := task.NewTask("completed task", "outcome pin")
	completed.State = task.StateCompleted
	if err := rl.taskStore.Create(completed); err != nil {
		t.Fatalf("create: %v", err)
	}
	failed := task.NewTask("failed task", "outcome pin")
	failed.State = task.StateFailed
	if err := rl.taskStore.Create(failed); err != nil {
		t.Fatalf("create: %v", err)
	}
	pending := task.NewTask("pending task", "outcome pin")
	if err := rl.taskStore.Create(pending); err != nil {
		t.Fatalf("create: %v", err)
	}

	if done, term := rl.TaskOutcome(completed.ID); !done || !term {
		t.Fatalf("TaskOutcome(completed) = (%v,%v), want (true,true)", done, term)
	}
	if done, term := rl.TaskOutcome(failed.ID); done || !term {
		t.Fatalf("TaskOutcome(failed) = (%v,%v), want (false,true)", done, term)
	}
	if done, term := rl.TaskOutcome(pending.ID); done || term {
		t.Fatalf("TaskOutcome(pending) = (%v,%v), want (false,false)", done, term)
	}
}

// TestRalphLoop_TriggerReplan_NilBusDoesNotPanic guards failTaskAtCap's
// best-effort contract: no bus wired must not panic the capped path.
func TestRalphLoop_TriggerReplan_NilBusDoesNotPanic(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "tasks.db")
	taskStore, err := task.NewStore(dbPath, nil)
	if err != nil {
		t.Fatalf("failed to create task store: %v", err)
	}
	t.Cleanup(func() { taskStore.Close() })

	logger := slogDiscardLogger()
	cfg := DefaultRalphLoopConfig()
	rl := NewRalphLoop(cfg, nil, taskStore, nil, nil, nil, logger) // nil bus

	tk := task.NewTask("nil bus task", "cap pin")
	if err := taskStore.Create(tk); err != nil {
		t.Fatalf("create: %v", err)
	}

	ctx := context.Background()
	for i := 0; i <= rl.config.MaxIterations; i++ {
		if err := rl.TriggerReplan(ctx, tk.ID, nil); err != nil {
			t.Fatalf("TriggerReplan #%d: %v", i+1, err)
		}
	}

	got, err := rl.taskStore.GetByID(tk.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.State != task.StateFailed {
		t.Fatalf("state = %q, want failed (nil-bus path must still terminalize)", got.State)
	}
}
