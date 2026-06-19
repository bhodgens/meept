package queue

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/bus"
	"github.com/caimlas/meept/pkg/models"
)

func newTestQueue(t *testing.T) *PersistentQueue {
	t.Helper()
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "queue.db")

	msgBus := bus.New(nil, nil)
	q, err := NewPersistentQueue(dbPath, msgBus, nil)
	if err != nil {
		t.Fatalf("failed to create queue: %v", err)
	}
	t.Cleanup(func() { q.Close() })
	return q
}

func TestPersistentQueue_EnqueueAndGet(t *testing.T) {
	q := newTestQueue(t)
	ctx := context.Background()

	job, err := NewJob(JobTypeOneOff, map[string]string{"prompt": "hello"})
	if err != nil {
		t.Fatalf("NewJob failed: %v", err)
	}

	if err := q.Enqueue(ctx, job); err != nil {
		t.Fatalf("Enqueue failed: %v", err)
	}

	got, err := q.Get(ctx, job.ID)
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}
	if got == nil {
		t.Fatal("expected job, got nil")
	}
	if got.State != StatePending {
		t.Errorf("expected state %q, got %q", StatePending, got.State)
	}
}

func TestPersistentQueue_ClaimAndComplete(t *testing.T) {
	q := newTestQueue(t)
	ctx := context.Background()

	job, _ := NewJob(JobTypeOneOff, map[string]string{"prompt": "test"})
	_ = q.Enqueue(ctx, job)

	claimed, err := q.Claim(ctx, "worker-1", nil, "")
	if err != nil {
		t.Fatalf("Claim failed: %v", err)
	}
	if claimed == nil {
		t.Fatal("expected to claim a job")
	}
	if claimed.ID != job.ID {
		t.Errorf("expected job %q, got %q", job.ID, claimed.ID)
	}

	if err := q.Complete(ctx, job.ID, map[string]string{"result": "done"}); err != nil {
		t.Fatalf("Complete failed: %v", err)
	}

	got, _ := q.Get(ctx, job.ID)
	if got.State != StateCompleted {
		t.Errorf("expected state %q, got %q", StateCompleted, got.State)
	}
}

func TestPersistentQueue_ClaimEmpty(t *testing.T) {
	q := newTestQueue(t)
	ctx := context.Background()

	claimed, err := q.Claim(ctx, "worker-1", nil, "")
	if !errors.Is(err, ErrNoJobAvailable) {
		t.Fatalf("expected ErrNoJobAvailable, got: %v", err)
	}
	if claimed != nil {
		t.Error("expected nil from empty queue")
	}
}

func TestPersistentQueue_FailAndRetry(t *testing.T) {
	q := newTestQueue(t)
	ctx := context.Background()

	job, _ := NewJob(JobTypeOneOff, map[string]string{"prompt": "test"})
	_ = q.Enqueue(ctx, job)
	_, _ = q.Claim(ctx, "worker-1", nil, "")

	if err := q.Fail(ctx, job.ID, errForTestError("something broke")); err != nil {
		t.Fatalf("Fail failed: %v", err)
	}

	got, _ := q.Get(ctx, job.ID)
	if got.State != StateFailed {
		t.Errorf("expected state %q, got %q", StateFailed, got.State)
	}

	if err := q.Retry(ctx, job.ID); err != nil {
		t.Fatalf("Retry failed: %v", err)
	}

	got, _ = q.Get(ctx, job.ID)
	if got.State != StatePending {
		t.Errorf("expected state %q after retry, got %q", StatePending, got.State)
	}
}

func TestPersistentQueue_ListByState(t *testing.T) {
	q := newTestQueue(t)
	ctx := context.Background()

	j1, _ := NewJob(JobTypeOneOff, map[string]string{"prompt": "1"})
	j2, _ := NewJob(JobTypeOneOff, map[string]string{"prompt": "2"})
	j3, _ := NewJob(JobTypeOneOff, map[string]string{"prompt": "3"})

	_ = q.Enqueue(ctx, j1)
	_ = q.Enqueue(ctx, j2)
	_ = q.Enqueue(ctx, j3)

	// Complete one
	_, _ = q.Claim(ctx, "w1", nil, "")
	_ = q.Complete(ctx, j1.ID, nil)

	pending, err := q.ListByState(ctx, StatePending, 10)
	if err != nil {
		t.Fatalf("ListByState failed: %v", err)
	}
	if len(pending) != 2 {
		t.Errorf("expected 2 pending, got %d", len(pending))
	}
}

func TestPersistentQueue_Stats(t *testing.T) {
	q := newTestQueue(t)
	ctx := context.Background()

	j1, _ := NewJob(JobTypeOneOff, map[string]string{"prompt": "1"})
	j2, _ := NewJob(JobTypeOneOff, map[string]string{"prompt": "2"})
	_ = q.Enqueue(ctx, j1)
	_ = q.Enqueue(ctx, j2)

	stats, err := q.Stats(ctx)
	if err != nil {
		t.Fatalf("Stats failed: %v", err)
	}
	if stats.ByState[StatePending] != 2 {
		t.Errorf("expected 2 pending in stats, got %d", stats.ByState[StatePending])
	}
}

func TestPersistentQueue_ClosedOperations(t *testing.T) {
	q := newTestQueue(t)
	ctx := context.Background()

	q.Close()

	job, _ := NewJob(JobTypeOneOff, map[string]string{"prompt": "test"})
	if err := q.Enqueue(ctx, job); err == nil {
		t.Error("expected error on closed queue")
	}

	_, err := q.Claim(ctx, "w1", nil, "")
	if err == nil {
		t.Error("expected error on closed queue claim")
	}
}

func TestPersistentQueue_ListByTaskID(t *testing.T) {
	q := newTestQueue(t)
	ctx := context.Background()

	j1, _ := NewJob(JobTypeProjectTask, map[string]string{"prompt": "1"})
	j1.WithTaskID("task-123")
	j2, _ := NewJob(JobTypeProjectTask, map[string]string{"prompt": "2"})
	j2.WithTaskID("task-123")
	j3, _ := NewJob(JobTypeOneOff, map[string]string{"prompt": "3"})

	_ = q.Enqueue(ctx, j1)
	_ = q.Enqueue(ctx, j2)
	_ = q.Enqueue(ctx, j3)

	jobs, err := q.ListByTaskID(ctx, "task-123")
	if err != nil {
		t.Fatalf("ListByTaskID failed: %v", err)
	}
	if len(jobs) != 2 {
		t.Errorf("expected 2 jobs for task, got %d", len(jobs))
	}
}

func TestJob_Predicates(t *testing.T) {
	job, _ := NewJob(JobTypeOneOff, map[string]string{"prompt": "test"})

	if !job.IsPending() {
		t.Error("new job should be pending")
	}
	if job.IsComplete() {
		t.Error("new job should not be complete")
	}
	if !job.CanRetry() {
		t.Error("new job should be retryable")
	}

	job.RetryCount = job.MaxRetries
	if job.CanRetry() {
		t.Error("job at max retries should not be retryable")
	}
}

func TestJob_CanBeClaimedBy(t *testing.T) {
	job, _ := NewJob(JobTypeOneOff, map[string]string{"prompt": "test"})

	// No required caps: any worker can claim
	if !job.CanBeClaimedBy(nil) {
		t.Error("job with no caps should be claimable by any worker")
	}

	job.WithRequiredCaps([]string{"code", "reasoning"})

	if job.CanBeClaimedBy([]string{"code"}) {
		t.Error("worker missing 'reasoning' should not be able to claim")
	}
	if !job.CanBeClaimedBy([]string{"code", "reasoning", "extra"}) {
		t.Error("worker with all required caps should be able to claim")
	}
}

func TestJob_Priority(t *testing.T) {
	tests := []struct {
		priority Priority
		expected string
	}{
		{PriorityLow, "low"},
		{PriorityNormal, "normal"},
		{PriorityHigh, "high"},
		{PriorityUrgent, "urgent"},
		{Priority(99), "unknown"},
	}

	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			if got := tt.priority.String(); got != tt.expected {
				t.Errorf("Priority(%d).String() = %q, want %q", tt.priority, got, tt.expected)
			}
		})
	}
}

// TestHandler_StatsViaBus tests the queue handler's stats endpoint via the bus.
func TestHandler_StatsViaBus(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "queue.db")

	msgBus := bus.New(nil, nil)
	q, err := NewPersistentQueue(dbPath, msgBus, nil)
	if err != nil {
		t.Fatalf("failed to create queue: %v", err)
	}
	defer q.Close()

	// Pre-enqueue a job
	ctx := context.Background()
	job, _ := NewJob(JobTypeOneOff, map[string]string{"prompt": "test"})
	_ = q.Enqueue(ctx, job)

	handler := NewHandler(q, msgBus, nil)
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	_ = handler.Start(ctx)
	defer func() { _ = handler.Stop(ctx) }()

	respSub := msgBus.Subscribe("test-stats", "queue.result")

	statsPayload, _ := json.Marshal(map[string]any{})
	statsMsg := &models.BusMessage{
		ID:        "test-stats-1",
		Type:      models.MessageTypeRequest,
		Topic:     "queue.stats",
		Source:    "test",
		Timestamp: time.Now().UTC(),
		Payload:   statsPayload,
	}

	msgBus.Publish("queue.stats", statsMsg)

	select {
	case resp := <-respSub.Channel:
		var result map[string]any
		_ = json.Unmarshal(resp.Payload, &result)
		if _, hasErr := result["error"]; hasErr {
			t.Errorf("got error: %s", string(resp.Payload))
		}
		byState, ok := result["by_state"].(map[string]any)
		if !ok {
			t.Fatalf("expected by_state map, got %T", result["by_state"])
		}
		pending, _ := byState["pending"].(float64)
		if pending < 1 {
			t.Errorf("expected at least 1 pending, got %.0f", pending)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for stats response")
	}
}

type errForTestError string

func (e errForTestError) Error() string { return string(e) }

// TestPersistentQueue_ClaimWithCancelledTaskCallback tests that jobs from cancelled tasks are skipped.
func TestPersistentQueue_ClaimWithCancelledTaskCallback(t *testing.T) {
	q := newTestQueue(t)
	ctx := context.Background()

	// Create two jobs: one for a "cancelled" task, one for a normal task
	job1, _ := NewJob(JobTypeProjectTask, map[string]string{"prompt": "cancelled task"})
	job1.WithTaskID("task-cancelled")
	job2, _ := NewJob(JobTypeProjectTask, map[string]string{"prompt": "normal task"})
	job2.WithTaskID("task-active")

	_ = q.Enqueue(ctx, job1)
	_ = q.Enqueue(ctx, job2)

	// Set up callback that marks task-cancelled as cancelled
	q.SetTaskCancelledCallback(func(taskID string) (bool, string) {
		if taskID == "task-cancelled" {
			return true, "test cancellation"
		}
		return false, ""
	})

	// Claim should skip the cancelled task and return the active one
	claimed, err := q.Claim(ctx, "worker-1", nil, "")
	if err != nil {
		t.Fatalf("Claim failed: %v", err)
	}
	if claimed == nil {
		t.Fatal("expected to claim a job")
	}
	if claimed.ID != job2.ID {
		t.Errorf("expected job %q (active task), got %q", job2.ID, claimed.ID)
	}

	// Verify the cancelled job is still in pending state
	cancelledJob, err := q.Get(ctx, job1.ID)
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}
	if cancelledJob.State != StatePending {
		t.Errorf("expected cancelled job to remain pending, got %q", cancelledJob.State)
	}
}

// TestPersistentQueue_ClaimAllTasksCancelled tests that claiming returns nil when all tasks are cancelled.
func TestPersistentQueue_ClaimAllTasksCancelled(t *testing.T) {
	q := newTestQueue(t)
	ctx := context.Background()

	job, _ := NewJob(JobTypeProjectTask, map[string]string{"prompt": "cancelled"})
	job.WithTaskID("task-cancelled")
	_ = q.Enqueue(ctx, job)
	// Set up callback that marks all tasks as cancelled
	q.SetTaskCancelledCallback(func(taskID string) (bool, string) {
		return true, "test cancellation"
	})

	// Claim should return ErrNoJobAvailable when all tasks are cancelled
	claimed, err := q.Claim(ctx, "worker-1", nil, "")
	if !errors.Is(err, ErrNoJobAvailable) {
		t.Fatalf("expected ErrNoJobAvailable, got: %v", err)
	}
	if claimed != nil {
		t.Errorf("expected nil when all tasks cancelled, got job %q", claimed.ID)
	}
}

// TestPersistentQueue_ClaimNoTaskID tests that jobs without task_id are claimable.
func TestPersistentQueue_ClaimNoTaskID(t *testing.T) {
	q := newTestQueue(t)
	ctx := context.Background()

	// Job without task_id (one-off job)
	job, _ := NewJob(JobTypeOneOff, map[string]string{"prompt": "one-off"})
	_ = q.Enqueue(ctx, job)
	// Set up callback (should not affect jobs without task_id)
	q.SetTaskCancelledCallback(func(taskID string) (bool, string) {
		return true, "test cancellation"
	})

	// Claim should still work for jobs without task_id
	claimed, err := q.Claim(ctx, "worker-1", nil, "")
	if err != nil {
		t.Fatalf("Claim failed: %v", err)
	}
	if claimed == nil {
		t.Fatal("expected to claim job without task_id")
	}
	if claimed.ID != job.ID {
		t.Errorf("expected job %q, got %q", job.ID, claimed.ID)
	}
}

// TestPersistentQueue_ClaimThroughputUnderContention enqueues N jobs and then
// has M workers claim them concurrently. The test verifies:
//   - Every claim returns a distinct job (no double-claims).
//   - The total number of claimed jobs equals the number enqueued.
//   - No errors occur during contention.
//
// This exercises the fast-path atomic Claim via store.ClaimNextForAgent
// and serves as a throughput stress test under the -race detector.
func TestPersistentQueue_ClaimThroughputUnderContention(t *testing.T) {
	q := newTestQueue(t)
	ctx := context.Background()

	const numJobs = 50
	const numWorkers = 8

	// Enqueue N jobs.
	enqueuedIDs := make(map[string]struct{}, numJobs)
	for i := 0; i < numJobs; i++ {
		job, err := NewJob(JobTypeOneOff, map[string]string{
			"prompt": "throughput-test",
			"idx":    string(rune('a' + i%26)),
		})
		if err != nil {
			t.Fatalf("NewJob %d failed: %v", i, err)
		}
		if err := q.Enqueue(ctx, job); err != nil {
			t.Fatalf("Enqueue %d failed: %v", i, err)
		}
		enqueuedIDs[job.ID] = struct{}{}
	}

	// Workers claim concurrently.
	var (
		wg        sync.WaitGroup
		claimed   int32
		errCount  int32
		seenIDsMu sync.Mutex
		seenIDs   = make(map[string]int, numJobs)
	)
	start := make(chan struct{})
	wg.Add(numWorkers)

	for w := 0; w < numWorkers; w++ {
		go func(workerID int) {
			defer wg.Done()
			<-start
			for {
				job, err := q.Claim(ctx, "worker-test", nil, "")
				if err != nil {
					if !errors.Is(err, ErrNoJobAvailable) {
						atomic.AddInt32(&errCount, 1)
					}
					return
				}
				if job == nil {
					return
				}
				atomic.AddInt32(&claimed, 1)
				seenIDsMu.Lock()
				seenIDs[job.ID]++
				seenIDsMu.Unlock()
			}
		}(w)
	}
	close(start)
	wg.Wait()

	if errCount > 0 {
		t.Errorf("expected zero errors during contention, got %d", errCount)
	}

	// Every enqueued job should have been claimed exactly once.
	if int(claimed) != numJobs {
		t.Errorf("expected %d claims, got %d", numJobs, claimed)
	}
	for id := range enqueuedIDs {
		count := seenIDs[id]
		if count != 1 {
			t.Errorf("job %q: expected exactly 1 claim, got %d (double-claim race)", id, count)
		}
	}
}

// TestPersistentQueue_ClaimAgentIDTargeting verifies that when a job has
// agent_id set, only a worker with a matching agentID can claim it.
// A worker with a different agentID must NOT claim the targeted job.
func TestPersistentQueue_ClaimAgentIDTargeting(t *testing.T) {
	q := newTestQueue(t)
	ctx := context.Background()

	// Enqueue a job targeted to "coder"
	coderJob, _ := NewJob(JobTypeOneOff, map[string]string{"prompt": "code task"})
	coderJob.WithAgentID("coder")
	if err := q.Enqueue(ctx, coderJob); err != nil {
		t.Fatalf("Enqueue failed: %v", err)
	}

	// A planner worker must not claim the coder-targeted job
	claimed, err := q.Claim(ctx, "planner-worker", nil, "planner")
	if !errors.Is(err, ErrNoJobAvailable) {
		t.Fatalf("expected ErrNoJobAvailable for mismatched agent, got: %v (job=%v)", err, claimed)
	}
	if claimed != nil {
		t.Fatalf("planner should not claim coder job, got job %q", claimed.ID)
	}

	// A coder worker MUST claim the coder-targeted job
	claimed, err = q.Claim(ctx, "coder-worker", nil, "coder")
	if err != nil {
		t.Fatalf("Claim failed for matching agent: %v", err)
	}
	if claimed == nil {
		t.Fatal("expected coder to claim the targeted job")
	}
	if claimed.ID != coderJob.ID {
		t.Errorf("expected job %q, got %q", coderJob.ID, claimed.ID)
	}
}

// TestPersistentQueue_ClaimAgentIDUnassigned verifies that a worker with
// a specific agentID can still claim unassigned (agent_id="") jobs.
func TestPersistentQueue_ClaimAgentIDUnassigned(t *testing.T) {
	q := newTestQueue(t)
	ctx := context.Background()

	// Enqueue an unassigned job
	job, _ := NewJob(JobTypeOneOff, map[string]string{"prompt": "any agent task"})
	if err := q.Enqueue(ctx, job); err != nil {
		t.Fatalf("Enqueue failed: %v", err)
	}

	// A coder worker should be able to claim the unassigned job
	claimed, err := q.Claim(ctx, "coder-worker", nil, "coder")
	if err != nil {
		t.Fatalf("Claim failed for unassigned job: %v", err)
	}
	if claimed == nil {
		t.Fatal("expected to claim unassigned job")
	}
	if claimed.ID != job.ID {
		t.Errorf("expected job %q, got %q", job.ID, claimed.ID)
	}
}
