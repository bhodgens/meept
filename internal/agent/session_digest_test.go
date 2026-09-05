package agent

import (
	"io"
	"log/slog"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/bus"
	"github.com/caimlas/meept/internal/task"
)

func digestTestLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// newDigestTestDispatcher builds a Dispatcher backed by a real task
// Registry (which owns a real task.Store + StepStore on disk in a temp
// dir) and a real in-memory SessionTracker, mirroring the store setup in
// internal/daemon/agent_job_processor_test.go.
func newDigestTestDispatcher(t *testing.T) (*Dispatcher, *task.Registry) {
	t.Helper()

	dbPath := filepath.Join(t.TempDir(), "tasks.db")
	logger := digestTestLogger()
	reg, err := task.NewRegistry(dbPath, bus.New(nil, nil), logger)
	if err != nil {
		t.Fatalf("create task registry: %v", err)
	}
	t.Cleanup(func() {
		if err := reg.Close(); err != nil {
			t.Errorf("close task registry: %v", err)
		}
	})

	d := &Dispatcher{
		taskStore:      reg.Store(),
		taskRegistry:   reg,
		sessionTracker: NewSessionTracker(time.Hour),
		logger:         logger,
	}
	return d, reg
}

// seedDigestTask inserts a task with fixed identity/state/timestamps and
// links it to a session (sessionID "" skips linking).
func seedDigestTask(t *testing.T, d *Dispatcher, id, name, sessionID string, state task.TaskState, updatedAt time.Time, agent string) {
	t.Helper()

	tk := task.NewTask(name, "digest test task")
	tk.ID = id
	tk.State = state
	tk.UpdatedAt = updatedAt
	tk.AssignedAgent = agent
	if err := d.taskStore.Create(tk); err != nil {
		t.Fatalf("create task %s: %v", id, err)
	}
	if sessionID != "" {
		if err := d.taskStore.LinkSession(id, sessionID); err != nil {
			t.Fatalf("link task %s to session %s: %v", id, sessionID, err)
		}
	}
}

// seedDigestStep inserts a step for a task via the registry's step store.
func seedDigestStep(t *testing.T, reg *task.Registry, taskID string, seq int, state task.StepState, result string) {
	t.Helper()

	st := task.NewTaskStep(taskID, "digest test step", seq)
	st.State = state
	st.Result = result
	if err := reg.StepStore().Create(st); err != nil {
		t.Fatalf("create step for task %s: %v", taskID, err)
	}
}

// Task 1: zero-value digest is empty; populated digest is not.
func TestSessionContextDigest_IsEmpty(t *testing.T) {
	var zero SessionContextDigest
	if !zero.IsEmpty() {
		t.Errorf("zero-value digest IsEmpty() = false, want true")
	}

	populated := SessionContextDigest{LastIntentType: string(IntentClarify)}
	if populated.IsEmpty() {
		t.Errorf("populated digest IsEmpty() = true, want false")
	}
}

// Task 2: buildSessionContextDigest population rules.
func TestSessionContextDigest_Build(t *testing.T) {
	base := time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)

	tests := []struct {
		name      string
		sessionID string
		setup     func(t *testing.T, d *Dispatcher, reg *task.Registry)
		check     func(t *testing.T, digest *SessionContextDigest)
	}{
		{
			name:      "nil stores yield empty digest without panic",
			sessionID: "sess-nil",
			setup: func(t *testing.T, d *Dispatcher, reg *task.Registry) {
				d.taskStore = nil
				d.taskRegistry = nil
				d.sessionTracker = nil
			},
			check: func(t *testing.T, digest *SessionContextDigest) {
				if digest == nil {
					t.Fatal("buildSessionContextDigest returned nil, want non-nil empty digest")
				}
				if !digest.IsEmpty() {
					t.Errorf("digest = %+v, want empty", *digest)
				}
			},
		},
		{
			name:      "empty session ID yields empty digest",
			sessionID: "",
			setup: func(t *testing.T, d *Dispatcher, reg *task.Registry) {
				seedDigestTask(t, d, "task-z", "linked task", "sess-z", task.StateCompleted, base, "coder")
				seedDigestStep(t, reg, "task-z", 0, task.StepCompleted, "did the thing")
			},
			check: func(t *testing.T, digest *SessionContextDigest) {
				if digest == nil {
					t.Fatal("buildSessionContextDigest returned nil, want non-nil empty digest")
				}
				if !digest.IsEmpty() {
					t.Errorf("digest = %+v, want empty", *digest)
				}
			},
		},
		{
			name:      "completed task with step result populates digest",
			sessionID: "sess-b",
			setup: func(t *testing.T, d *Dispatcher, reg *task.Registry) {
				seedDigestTask(t, d, "task-b", "Fix the login bug", "sess-b", task.StateCompleted, base, "coder")
				seedDigestStep(t, reg, "task-b", 0, task.StepCompleted, "Fixed the login bug.\nSee the diff for details.")
				d.sessionTracker.RecordIntent("sess-b", &Intent{Type: string(IntentInstruction)}, "coder")
			},
			check: func(t *testing.T, digest *SessionContextDigest) {
				if digest.IsEmpty() {
					t.Fatal("digest IsEmpty() = true, want populated")
				}
				if digest.LastTaskName != "Fix the login bug" {
					t.Errorf("LastTaskName = %q, want %q", digest.LastTaskName, "Fix the login bug")
				}
				if digest.LastTaskState != "completed" {
					t.Errorf("LastTaskState = %q, want %q", digest.LastTaskState, "completed")
				}
				if digest.LastTaskAgent != "coder" {
					t.Errorf("LastTaskAgent = %q, want %q", digest.LastTaskAgent, "coder")
				}
				if digest.LastResultSummary != "Fixed the login bug." {
					t.Errorf("LastResultSummary = %q, want first line %q", digest.LastResultSummary, "Fixed the login bug.")
				}
				if digest.LastIntentType != string(IntentInstruction) {
					t.Errorf("LastIntentType = %q, want %q", digest.LastIntentType, string(IntentInstruction))
				}
			},
		},
		{
			name:      "no steps with results leaves summary empty but task fields set",
			sessionID: "sess-c",
			setup: func(t *testing.T, d *Dispatcher, reg *task.Registry) {
				seedDigestTask(t, d, "task-c", "Pending work item", "sess-c", task.StateExecuting, base, "tester")
				seedDigestStep(t, reg, "task-c", 0, task.StepRunning, "")
			},
			check: func(t *testing.T, digest *SessionContextDigest) {
				if digest.IsEmpty() {
					t.Fatal("digest IsEmpty() = true, want task fields set")
				}
				if digest.LastTaskName != "Pending work item" {
					t.Errorf("LastTaskName = %q, want %q", digest.LastTaskName, "Pending work item")
				}
				if digest.LastTaskState != "executing" {
					t.Errorf("LastTaskState = %q, want %q", digest.LastTaskState, "executing")
				}
				if digest.LastTaskAgent != "tester" {
					t.Errorf("LastTaskAgent = %q, want %q", digest.LastTaskAgent, "tester")
				}
				if digest.LastResultSummary != "" {
					t.Errorf("LastResultSummary = %q, want empty", digest.LastResultSummary)
				}
			},
		},
		{
			name:      "two tasks: most recently updated wins",
			sessionID: "sess-d",
			setup: func(t *testing.T, d *Dispatcher, reg *task.Registry) {
				seedDigestTask(t, d, "task-d-old", "older task", "sess-d", task.StateCompleted, base, "coder")
				seedDigestTask(t, d, "task-d-new", "newer task", "sess-d", task.StateCompleted, base.Add(2*time.Hour), "reviewer")
				seedDigestStep(t, reg, "task-d-old", 0, task.StepCompleted, "old result")
				seedDigestStep(t, reg, "task-d-new", 0, task.StepCompleted, "new result")
			},
			check: func(t *testing.T, digest *SessionContextDigest) {
				if digest.LastTaskName != "newer task" {
					t.Errorf("LastTaskName = %q, want most recent %q", digest.LastTaskName, "newer task")
				}
				if digest.LastResultSummary != "new result" {
					t.Errorf("LastResultSummary = %q, want %q from most recent task", digest.LastResultSummary, "new result")
				}
			},
		},
		{
			name:      "task name over 200 chars is capped",
			sessionID: "sess-e",
			setup: func(t *testing.T, d *Dispatcher, reg *task.Registry) {
				longName := strings.Repeat("y", 250)
				seedDigestTask(t, d, "task-e", longName, "sess-e", task.StateCompleted, base, "coder")
			},
			check: func(t *testing.T, digest *SessionContextDigest) {
				want := truncateString(strings.Repeat("y", 250), 200)
				if digest.LastTaskName != want {
					t.Errorf("LastTaskName length = %d, want capped %d", len([]rune(digest.LastTaskName)), 200)
				}
				if len([]rune(digest.LastTaskName)) > 200 {
					t.Errorf("LastTaskName = %d runes, want <= 200", len([]rune(digest.LastTaskName)))
				}
			},
		},
		{
			name:      "multi-line step result keeps first line only",
			sessionID: "sess-f",
			setup: func(t *testing.T, d *Dispatcher, reg *task.Registry) {
				seedDigestTask(t, d, "task-f", "multi line", "sess-f", task.StateCompleted, base, "coder")
				seedDigestStep(t, reg, "task-f", 0, task.StepApproved, "first line here\nsecond line\nthird line")
			},
			check: func(t *testing.T, digest *SessionContextDigest) {
				if digest.LastResultSummary != "first line here" {
					t.Errorf("LastResultSummary = %q, want %q", digest.LastResultSummary, "first line here")
				}
			},
		},
		{
			name:      "first line over 400 chars is capped",
			sessionID: "sess-g",
			setup: func(t *testing.T, d *Dispatcher, reg *task.Registry) {
				seedDigestTask(t, d, "task-g", "long result", "sess-g", task.StateCompleted, base, "coder")
				seedDigestStep(t, reg, "task-g", 0, task.StepCompleted, strings.Repeat("x", 500))
			},
			check: func(t *testing.T, digest *SessionContextDigest) {
				want := truncateString(strings.Repeat("x", 500), 400)
				if digest.LastResultSummary != want {
					t.Errorf("LastResultSummary != capped 400 (got %d runes, want %d)", len([]rune(digest.LastResultSummary)), len([]rune(want)))
				}
				if len([]rune(digest.LastResultSummary)) > 400 {
					t.Errorf("LastResultSummary = %d runes, want <= 400", len([]rune(digest.LastResultSummary)))
				}
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			d, reg := newDigestTestDispatcher(t)
			tc.setup(t, d, reg)

			got := d.buildSessionContextDigest(tc.sessionID)
			if got == nil {
				t.Fatal("buildSessionContextDigest returned nil")
			}
			tc.check(t, got)
		})
	}
}
