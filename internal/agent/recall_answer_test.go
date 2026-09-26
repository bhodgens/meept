package agent

import (
	"context"
	"log/slog"
	"path/filepath"
	"strings"
	"testing"

	"github.com/caimlas/meept/internal/bus"
	"github.com/caimlas/meept/internal/task"
)

// recallTestDispatcher builds a dispatcher wired to a real task store,
// mirroring the recall-answer integration shape.
func recallTestDispatcher(t *testing.T) (*Dispatcher, *task.Registry) {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "tasks.db")
	logger := slog.New(slog.DiscardHandler)
	reg, err := task.NewRegistry(dbPath, bus.New(nil, nil), logger)
	if err != nil {
		t.Fatalf("create task registry: %v", err)
	}
	t.Cleanup(func() {
		if err := reg.Close(); err != nil {
			t.Errorf("close task registry: %v", err)
		}
	})
	d := NewDispatcher(DispatcherConfig{
		TaskStore:    reg.Store(),
		TaskRegistry: reg,
		Logger:       logger,
	})
	return d, reg
}

// TestRecallAnswer_TerminalTaskNamesResult pins option 3 branch 1: a
// completed prior task yields the stored result prose with task + status —
// no LLM involved (2026-09-25 run 21/22 A5 failures).
func TestRecallAnswer_TerminalTaskNamesResult(t *testing.T) {
	d, reg := recallTestDispatcher(t)
	ctx := context.Background()

	tsk, err := reg.Create(ctx, "create a file named hello.txt", "create hello.txt")
	if err != nil {
		t.Fatalf("create task: %v", err)
	}
	if err := reg.LinkSession(ctx, tsk.ID, "session-recall"); err != nil {
		t.Fatalf("link session: %v", err)
	}
	if err := reg.UpdateState(ctx, tsk.ID, task.StateCompleted); err != nil {
		t.Fatalf("complete task: %v", err)
	}
	// give the task a step result with the answer
	step := task.NewTaskStep(tsk.ID, "write hello.txt", 0)
	step.State = task.StepApproved
	step.Result = "The file hello.txt has been created in the current directory. Full path: /w/project/hello.txt"
	if err := reg.StepStore().Create(step); err != nil {
		t.Fatalf("create step: %v", err)
	}

	result := &DispatchResult{
		Intent:        &Intent{Type: string(IntentRecall), Confidence: 0.9},
		OriginalInput: "did the change get made? where is the file?",
	}
	answer, handled := d.RecallAnswer(ctx, result, "session-recall", false)
	if !handled {
		t.Fatal("recall answer not handled for a session with a prior task")
	}
	if !strings.Contains(answer, "completed") {
		t.Errorf("answer does not state task status: %q", answer)
	}
	if strings.Contains(answer, "still in progress") {
		t.Errorf("terminal task misreported as in-progress: %q", answer)
	}
}

// TestRecallAnswer_InProgressNamesTask pins branch 3: a non-terminal prior
// task yields an explicit in-progress answer (never a misleading snapshot).
func TestRecallAnswer_InProgressNamesTask(t *testing.T) {
	d, reg := recallTestDispatcher(t)
	ctx := context.Background()

	tsk, err := reg.Create(ctx, "create a file named hello.txt", "create hello.txt")
	if err != nil {
		t.Fatalf("create task: %v", err)
	}
	if err := reg.LinkSession(ctx, tsk.ID, "session-inprog"); err != nil {
		t.Fatalf("link session: %v", err)
	}

	result := &DispatchResult{
		Intent:        &Intent{Type: string(IntentRecall)},
		OriginalInput: "did the change get made?",
	}
	answer, handled := d.RecallAnswer(ctx, result, "session-inprog", false)
	if !handled {
		t.Fatal("recall answer not handled")
	}
	if !strings.Contains(answer, "still in progress") {
		t.Errorf("in-progress answer missing: %q", answer)
	}
}

// TestRecallAnswer_NoPriorTaskFallsThrough pins the fall-through: an empty
// session (no tasks) is not handled — the normal LLM path proceeds.
func TestRecallAnswer_NoPriorTaskFallsThrough(t *testing.T) {
	d, _ := recallTestDispatcher(t)
	result := &DispatchResult{
		Intent:        &Intent{Type: string(IntentRecall)},
		OriginalInput: "did the change get made?",
	}
	if _, handled := d.RecallAnswer(context.Background(), result, "session-empty", false); handled {
		t.Error("empty session must fall through to the normal path")
	}
}

// TestRecallAnswer_NonRecallNotHandled guards the intent gate.
func TestRecallAnswer_NonRecallNotHandled(t *testing.T) {
	d, _ := recallTestDispatcher(t)
	result := &DispatchResult{Intent: &Intent{Type: string(IntentChat)}}
	if _, handled := d.RecallAnswer(context.Background(), result, "s", false); handled {
		t.Error("non-recall intent must not be handled")
	}
}
