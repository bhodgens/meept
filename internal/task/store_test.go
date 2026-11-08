package task

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/agent/turnjournal"
	"github.com/caimlas/meept/internal/queue"
)

func newTestStore(t *testing.T) *Store {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "tasks.db")
	store, err := NewStore(dbPath, nil)
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}
	t.Cleanup(func() { store.Close() })
	return store
}

func makeTask(id, name string, state TaskState) *Task {
	now := time.Now().UTC()
	return &Task{
		ID:        id,
		Name:      name,
		State:     state,
		CreatedAt: now,
		UpdatedAt: now,
	}
}

// TestStore_List_ReturnsTasks verifies basic round-trip of List.
func TestStore_List_ReturnsTasks(t *testing.T) {
	s := newTestStore(t)

	if err := s.Create(makeTask("t1", "first", StatePending)); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if err := s.Create(makeTask("t2", "second", StateCompleted)); err != nil {
		t.Fatalf("Create: %v", err)
	}

	tasks, err := s.List(nil, 10)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(tasks) != 2 {
		t.Errorf("expected 2 tasks, got %d", len(tasks))
	}
}

// TestStore_ListPropagatesIterError verifies that if the DB connection is
// closed during iteration, the error is surfaced rather than silently
// returning a partial result.
func TestStore_ListPropagatesIterError(t *testing.T) {
	s := newTestStore(t)

	if err := s.Create(makeTask("t1", "first", StatePending)); err != nil {
		t.Fatalf("Create: %v", err)
	}

	// Close the DB. Subsequent queries should fail with a wrapped error
	// (either from Query or from rows.Err()), not return (nil, nil) or a
	// partial successful result.
	if err := s.db.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	_, err := s.List(nil, 10)
	if err == nil {
		t.Error("expected error from List after DB close, got nil")
	}
}

// TestListActive_LogsScanErrors is an indirect test: ListActive should still
// complete successfully when there is valid data, and propagate DB-level
// iteration errors. Scan errors on individual rows are logged and skipped.
func TestListActive_ReturnsActiveAndPropagatesError(t *testing.T) {
	s := newTestStore(t)

	if err := s.Create(makeTask("a1", "active", StatePending)); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if err := s.Create(makeTask("a2", "done", StateCompleted)); err != nil {
		t.Fatalf("Create: %v", err)
	}

	tasks, err := s.ListActive()
	if err != nil {
		t.Fatalf("ListActive: %v", err)
	}
	if len(tasks) != 1 {
		t.Errorf("expected 1 active task, got %d", len(tasks))
	}

	// After closing the DB, ListActive should return an error.
	if err := s.db.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	_, err = s.ListActive()
	if err == nil {
		t.Error("expected error from ListActive after DB close, got nil")
	}
}

// TestRecoverStaleTasks_SparesJournalReclaimableTurn pins issue 110: a
// journaled in-flight step the turn journal can reclaim is not force-failed.
// Abandoned rows (no open run, closed run, terminal job, nil journal, nil
// queue) still sweep to failed / daemon_shutdown.
func TestRecoverStaleTasks_SparesJournalReclaimableTurn(t *testing.T) {
	dir := t.TempDir()
	store, err := NewStore(filepath.Join(dir, "tasks.db"), nil)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	journal, err := turnjournal.NewSQLiteJournal(filepath.Join(dir, "turnjournal.db"), nil)
	if err != nil {
		t.Fatalf("NewSQLiteJournal: %v", err)
	}
	t.Cleanup(func() { _ = journal.Close() })

	jobs, err := queue.NewPersistentQueue(filepath.Join(dir, "queue.db"), nil, nil)
	if err != nil {
		t.Fatalf("NewPersistentQueue: %v", err)
	}
	t.Cleanup(func() { _ = jobs.Close() })

	const (
		reclaimable = "task-reclaim"
		abandoned   = "task-abandoned"
		closedRun   = "task-closed"
		terminalJob = "task-terminal-job"
	)
	seedExecuting(t, store, reclaimable)
	seedExecuting(t, store, abandoned)
	seedExecuting(t, store, closedRun)
	seedExecuting(t, store, terminalJob)

	ctx := t.Context()
	if err := journal.Append(ctx, turnjournal.Record{TurnID: reclaimable, SessionID: "s", Kind: turnjournal.KindRunStarted}); err != nil {
		t.Fatalf("append open run: %v", err)
	}
	if err := journal.Append(ctx, turnjournal.Record{TurnID: closedRun, SessionID: "s", Kind: turnjournal.KindRunStarted}); err != nil {
		t.Fatalf("append closed run start: %v", err)
	}
	if err := journal.CloseRun(ctx, closedRun); err != nil {
		t.Fatalf("close run: %v", err)
	}
	if err := journal.Append(ctx, turnjournal.Record{TurnID: terminalJob, SessionID: "s", Kind: turnjournal.KindRunStarted}); err != nil {
		t.Fatalf("append terminal-job run: %v", err)
	}

	enqueueJob(t, jobs, reclaimable, queue.StateClaimed)
	enqueueJob(t, jobs, closedRun, queue.StateClaimed)
	enqueueJob(t, jobs, terminalJob, queue.StateCompleted)

	n, err := store.RecoverStaleTasks(journal, jobs)
	if err != nil {
		t.Fatalf("RecoverStaleTasks: %v", err)
	}
	if n != 3 {
		t.Errorf("recovered = %d, want 3 (abandoned + closed run + terminal job)", n)
	}

	if got := taskState(t, store, reclaimable); got != StateExecuting {
		t.Errorf("reclaimable task state = %q, want executing", got)
	}
	if got := stepState(t, store, reclaimable); got != StepRunning {
		t.Errorf("reclaimable step state = %q, want running", got)
	}
	assertSwept(t, store, abandoned)
	assertSwept(t, store, closedRun)
	assertSwept(t, store, terminalJob)
}

// TestRecoverStaleTasks_NilJournalSweepsJournaledTurn pins the flag-off
// path: a nil journal (rehydrate will not run) still force-fails an open run.
func TestRecoverStaleTasks_NilJournalSweepsJournaledTurn(t *testing.T) {
	dir := t.TempDir()
	store, err := NewStore(filepath.Join(dir, "tasks.db"), nil)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	jobs, err := queue.NewPersistentQueue(filepath.Join(dir, "queue.db"), nil, nil)
	if err != nil {
		t.Fatalf("NewPersistentQueue: %v", err)
	}
	t.Cleanup(func() { _ = jobs.Close() })

	const id = "task-flag-off"
	seedExecuting(t, store, id)
	enqueueJob(t, jobs, id, queue.StateProcessing)

	n, err := store.RecoverStaleTasks(nil, jobs)
	if err != nil {
		t.Fatalf("RecoverStaleTasks: %v", err)
	}
	if n != 1 {
		t.Errorf("recovered = %d, want 1", n)
	}
	assertSwept(t, store, id)
}

// TestRecoverStaleTasks_NilQueueSweepsOpenRun pins a journal with no queue:
// rehydrate skips, so the open run is abandoned and still sweeps.
func TestRecoverStaleTasks_NilQueueSweepsOpenRun(t *testing.T) {
	dir := t.TempDir()
	store, err := NewStore(filepath.Join(dir, "tasks.db"), nil)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	journal, err := turnjournal.NewSQLiteJournal(filepath.Join(dir, "turnjournal.db"), nil)
	if err != nil {
		t.Fatalf("NewSQLiteJournal: %v", err)
	}
	t.Cleanup(func() { _ = journal.Close() })

	const id = "task-no-queue"
	seedExecuting(t, store, id)
	if err := journal.Append(t.Context(), turnjournal.Record{TurnID: id, SessionID: "s", Kind: turnjournal.KindRunStarted}); err != nil {
		t.Fatalf("append open run: %v", err)
	}

	n, err := store.RecoverStaleTasks(journal, nil)
	if err != nil {
		t.Fatalf("RecoverStaleTasks: %v", err)
	}
	if n != 1 {
		t.Errorf("recovered = %d, want 1", n)
	}
	assertSwept(t, store, id)
}

func seedExecuting(t *testing.T, store *Store, id string) {
	t.Helper()
	if err := store.Create(makeTask(id, id, StateExecuting)); err != nil {
		t.Fatalf("Create %s: %v", id, err)
	}
	now := time.Now().UTC()
	step := &TaskStep{
		ID:          "step-" + id,
		TaskID:      id,
		Description: "in flight",
		State:       StepRunning,
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	if err := store.StepStore().Create(step); err != nil {
		t.Fatalf("Create step %s: %v", id, err)
	}
}

func enqueueJob(t *testing.T, jobs *queue.PersistentQueue, taskID string, state queue.JobState) {
	t.Helper()
	job, err := queue.NewJob(queue.JobTypeProjectTask, map[string]string{"task": taskID})
	if err != nil {
		t.Fatalf("NewJob: %v", err)
	}
	job.TaskID = taskID
	job.State = state
	if err := jobs.Enqueue(t.Context(), job); err != nil {
		t.Fatalf("Enqueue %s: %v", taskID, err)
	}
}

func taskState(t *testing.T, store *Store, id string) TaskState {
	t.Helper()
	got, err := store.GetByID(id)
	if err != nil {
		t.Fatalf("GetByID %s: %v", id, err)
	}
	return got.State
}

func stepState(t *testing.T, store *Store, taskID string) StepState {
	t.Helper()
	steps, err := store.StepStore().ListByTaskID(taskID)
	if err != nil {
		t.Fatalf("ListByTaskID %s: %v", taskID, err)
	}
	if len(steps) != 1 {
		t.Fatalf("steps for %s = %d, want 1", taskID, len(steps))
	}
	return steps[0].State
}

func assertSwept(t *testing.T, store *Store, id string) {
	t.Helper()
	if got := taskState(t, store, id); got != StateFailed {
		t.Errorf("task %s state = %q, want failed", id, got)
	}
	steps, err := store.StepStore().ListByTaskID(id)
	if err != nil {
		t.Fatalf("ListByTaskID %s: %v", id, err)
	}
	if len(steps) != 1 {
		t.Fatalf("steps for %s = %d, want 1", id, len(steps))
	}
	if steps[0].State != StepFailed {
		t.Errorf("step %s state = %q, want failed", id, steps[0].State)
	}
	if steps[0].Result != "daemon_shutdown" {
		t.Errorf("step %s result = %q, want daemon_shutdown", id, steps[0].Result)
	}
}
