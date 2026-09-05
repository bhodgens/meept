package agent

// Regression tests for the 2026-09-05 daemon panic:
//
//	TacticalScheduler.OnJobCompleted refreshed step state via
//	`step, err = ts.stepStore.GetByID(step.ID)` and then logged
//	`step.ID` inside the error branch. GetByID returns (nil, err)
//	on any failure — a SQLITE_BUSY storm from parallel step jobs
//	made the refresh read fail, step became nil, and the daemon
//	panicked ("invalid memory address or nil pointer dereference"),
//	killing the socket mid-bench-run.
//
// Tests pin both halves of the fix:
//  1. the nil-deref guard in OnJobCompleted — deterministic fault
//     injection at the store seam (store closed mid-callback), no
//     timing race in the assertion,
//  2. sqlite hardening in the task/queue stores: WAL + busy_timeout via
//     the `_pragma=...` DSN form modernc.org/sqlite actually honors, plus
//     MaxOpenConns(1) write serialization; a 10-goroutine write storm
//     must not surface SQLITE_BUSY to callers.

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	_ "modernc.org/sqlite"

	"github.com/caimlas/meept/internal/queue"
	"github.com/caimlas/meept/internal/task"
)

// TestOnJobCompleted_BusyStormAtStepRefreshDoesNotPanic is the
// deterministic reproduction of the daemon-killing crash. GetByJobID
// (:515) succeeds, then the step-state refresh GetByID (:784) fails
// with the production failure shape — (nil, error) — exactly what the
// SQLITE_BUSY storm delivered (scanStep returns nil on any error).
// Before the fix, the error log at :786 dereferenced the nil step and
// the daemon died. The stepStoreReadHook injects the failure at the
// seam, so the test has no timing dependence.
func TestOnJobCompleted_BusyStormAtStepRefreshDoesNotPanic(t *testing.T) {
	ts, _, cleanup := newTacticalTestSetup(t)
	defer cleanup()

	parentTask := task.NewTask("busy-refresh-test", "busy storm at refresh")
	parentTask.TotalJobs = 1
	parentTask.SetState(task.StateExecuting)
	if err := ts.taskStore.Create(parentTask); err != nil {
		t.Fatalf("failed to create task: %v", err)
	}

	step := task.NewTaskStep(parentTask.ID, "do work", 0)
	if err := ts.stepStore.Create(step); err != nil {
		t.Fatalf("failed to create step: %v", err)
	}
	if err := ts.stepStore.SetJobID(step.ID, "job-busy-refresh-1"); err != nil {
		t.Fatalf("failed to set job ID: %v", err)
	}
	resultJSON, _ := json.Marshal(map[string]any{"success": true, "result": "ok"})

	// Inject the production failure shape: the refresh GetByID returns
	// (nil, err). GetByJobID passes through so the callback proceeds
	// past :515 into the crash window.
	calls := 0
	ts.stepStoreReadHook = func(id string, byJob bool) (*task.TaskStep, error) {
		if byJob {
			return nil, nil // pass through to the real store
		}
		calls++
		return nil, task.ErrStepNotFound // (nil, err) — the BUSY-storm shape
	}

	var gotErr error
	func() {
		defer func() {
			if r := recover(); r != nil {
				t.Fatalf("OnJobCompleted PANICKED under BUSY storm (nil step deref at refresh): %v", r)
			}
		}()
		gotErr = ts.OnJobCompleted(context.Background(), "job-busy-refresh-1", resultJSON)
	}()

	if calls == 0 {
		t.Fatal("injected refresh failure never fired; test would not have caught the nil-deref")
	}
	if gotErr != nil {
		t.Fatalf("OnJobCompleted returned error %v; want nil (refresh failure is logged and swallowed by the guard)", gotErr)
	}
}

// TestOnJobCompleted_StepEvictedBetweenReadsDoesNotPanic covers the
// second production trigger: the step row is missing by the time
// completion-processing reads fail — same nil-deref, different failure
// shape (ErrStepNotFound instead of SQLITE_BUSY). Sequential, so it
// pins the (nil, err) guard without any timing dependence.
func TestOnJobCompleted_StepEvictedBetweenReadsDoesNotPanic(t *testing.T) {
	ts, _, cleanup := newTacticalTestSetup(t)
	defer cleanup()

	parentTask := task.NewTask("nil-refresh-evict", "step evicted between reads")
	parentTask.TotalJobs = 1
	parentTask.SetState(task.StateExecuting)
	if err := ts.taskStore.Create(parentTask); err != nil {
		t.Fatalf("failed to create task: %v", err)
	}

	step := task.NewTaskStep(parentTask.ID, "do work", 0)
	if err := ts.stepStore.Create(step); err != nil {
		t.Fatalf("failed to create step: %v", err)
	}
	if err := ts.stepStore.SetJobID(step.ID, "job-evict-1"); err != nil {
		t.Fatalf("failed to set job ID: %v", err)
	}
	resultJSON, _ := json.Marshal(map[string]any{"success": true, "result": "ok"})

	func() {
		defer func() {
			if r := recover(); r != nil {
				t.Fatalf("OnJobCompleted PANICKED (nil step deref at refresh): %v", r)
			}
		}()
		//nolint:errcheck // panic guard above is the assertion
		_ = ts.OnJobCompleted(context.Background(), "job-evict-1", resultJSON)
		if err := ts.stepStore.DeleteByTaskID(parentTask.ID); err != nil {
			t.Fatalf("delete: %v", err)
		}
		// Second completion: GetByJobID misses (ErrStepNotFound) — must
		// surface as a plain error, never a panic.
		err := ts.OnJobCompleted(context.Background(), "job-evict-1", resultJSON)
		if err == nil {
			t.Log("second completion returned nil (job row already gone)")
		}
	}()
}

// TestTaskStoreConcurrentWritesNoBusy hammers one task DB from 10
// goroutines doing scheduler-shaped reads and writes (increment, create,
// set result, get, delete). Before the sqlite hardening this could
// surface SQLITE_BUSY ("database is locked") to callers because the
// driver ignored the mattn-style busy_timeout DSN key.
func TestTaskStoreConcurrentWritesNoBusy(t *testing.T) {
	store, err := task.NewStore(filepath.Join(t.TempDir(), "tasks-busy.db"), nil)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	defer store.Close()

	parent := task.NewTask("busy-storm", "concurrent write storm")
	parent.TotalJobs = 10
	if err := store.Create(parent); err != nil {
		t.Fatalf("create task: %v", err)
	}
	steps := store.StepStore()

	const goroutines = 10
	const iters = 20

	var wg sync.WaitGroup
	errCh := make(chan error, goroutines*iters)
	for g := 0; g < goroutines; g++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for i := 0; i < iters; i++ {
				if _, err := store.IncrementCompletedJobs(parent.ID); err != nil {
					errCh <- fmt.Errorf("increment: %w", err)
					continue
				}
				step := task.NewTaskStep(parent.ID, fmt.Sprintf("s-%d-%d", id, i), i)
				if err := steps.Create(step); err != nil {
					errCh <- fmt.Errorf("create step: %w", err)
					continue
				}
				if err := steps.SetResult(step.ID, fmt.Sprintf("r-%d-%d", id, i)); err != nil {
					errCh <- fmt.Errorf("set result: %w", err)
				}
				if _, err := store.GetByID(parent.ID); err != nil {
					errCh <- fmt.Errorf("get: %w", err)
				}
				if err := steps.DeleteByTaskID(parent.ID); err != nil {
					errCh <- fmt.Errorf("delete steps: %w", err)
				}
			}
		}(g)
	}
	wg.Wait()
	close(errCh)

	for err := range errCh {
		if strings.Contains(err.Error(), "database is locked") ||
			strings.Contains(err.Error(), "SQLITE_BUSY") ||
			strings.Contains(err.Error(), "database table is locked") {
			t.Errorf("SQLITE_BUSY surfaced to caller: %v", err)
			continue
		}
		t.Errorf("concurrent store op failed: %v", err)
	}

	got, err := store.GetByID(parent.ID)
	if err != nil {
		t.Fatalf("final get: %v", err)
	}
	if want := goroutines * iters; got.CompletedJobs != want {
		t.Errorf("completed_jobs = %d, want %d (lost updates under concurrency)", got.CompletedJobs, want)
	}
}

// TestQueueStoreConcurrentEnqueueNoBusy applies the same hammer to the
// job queue store, which parallel step jobs hit with enqueue from
// multiple workers.
func TestQueueStoreConcurrentEnqueueNoBusy(t *testing.T) {
	q, err := queue.NewPersistentQueue(filepath.Join(t.TempDir(), "queue-busy.db"), nil, nil)
	if err != nil {
		t.Fatalf("NewPersistentQueue: %v", err)
	}

	const goroutines = 10
	const iters = 10

	var wg sync.WaitGroup
	errCh := make(chan error, goroutines*iters)
	for g := 0; g < goroutines; g++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for i := 0; i < iters; i++ {
				payload, _ := json.Marshal(map[string]any{"goroutine": id, "iter": i})
				job, jerr := queue.NewJob(queue.JobTypeProjectTask, payload)
				if jerr != nil {
					errCh <- fmt.Errorf("new job: %w", jerr)
					continue
				}
				if jerr := q.Enqueue(context.Background(), job); jerr != nil {
					errCh <- fmt.Errorf("enqueue: %w", jerr)
				}
			}
		}(g)
	}
	wg.Wait()
	close(errCh)

	for err := range errCh {
		if strings.Contains(err.Error(), "database is locked") ||
			strings.Contains(err.Error(), "SQLITE_BUSY") {
			t.Errorf("SQLITE_BUSY surfaced from queue store: %v", err)
			continue
		}
		t.Errorf("concurrent queue op failed: %v", err)
	}
}

// TestTaskStorePragmasActive asserts the hardening actually took
// effect: WAL journal mode and a nonzero busy_timeout must be live on
// the open handle. This catches a silent regression back to
// mattn-style DSN keys that the driver ignores.
func TestTaskStorePragmasActive(t *testing.T) {
	store, err := task.NewStore(filepath.Join(t.TempDir(), "tasks-pragma.db"), nil)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	defer store.Close()

	var journalMode, busyTimeout string
	db := store.DB()
	if err := db.QueryRow("PRAGMA journal_mode").Scan(&journalMode); err != nil {
		t.Fatalf("read journal_mode: %v", err)
	}
	if err := db.QueryRow("PRAGMA busy_timeout").Scan(&busyTimeout); err != nil {
		t.Fatalf("read busy_timeout: %v", err)
	}
	if journalMode != "wal" {
		t.Errorf("journal_mode = %q, want \"wal\"", journalMode)
	}
	if busyTimeout == "0" || busyTimeout == "" {
		t.Errorf("busy_timeout = %q, want nonzero", busyTimeout)
	}
	t.Logf("journal_mode=%s busy_timeout=%s", journalMode, busyTimeout)
}
