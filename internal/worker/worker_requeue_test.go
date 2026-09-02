package worker

// Tree 03 leaf 03 tests: the worker execution error path requeues jobs
// on provider waits (D9) without consuming the job's retry budget, and
// keeps the legacy Fail/dead-letter path for genuine failures + give-up.
//
// tryProcessJob performs the claim internally, so each test enqueues a
// job and calls tryProcessJob exactly once (ok=true means the job was
// claimed AND processed).

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/llm"
	"github.com/caimlas/meept/internal/queue"
)

// requeueProcessor is a JobProcessor stub that returns a canned error.
type requeueProcessor struct {
	err error
}

func (p *requeueProcessor) Process(_ context.Context, _ *queue.Job) (any, error) {
	return nil, p.err
}

// newRequeueTestWorker wires a Worker over a real PersistentQueue
// (temp SQLite) with the given processor error and failure policy.
func newRequeueTestWorker(t *testing.T, proc *requeueProcessor, policy *llm.FailurePolicyConfig) (*Worker, *queue.PersistentQueue) {
	t.Helper()
	q, err := queue.NewPersistentQueue(t.TempDir()+"/queue.db", nil, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatalf("NewPersistentQueue: %v", err)
	}
	t.Cleanup(func() {
		if cErr := q.Close(); cErr != nil {
			t.Logf("queue close: %v", cErr)
		}
	})
	w, err := NewWorker(Config{
		ID:        "w-test",
		Queue:     q,
		Processor: proc,
		Logger:    slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	if err != nil {
		t.Fatalf("NewWorker: %v", err)
	}
	w.SetProviderPolicy(policy)
	// tryProcessJob requires a claimable state; Start() sets Idle but a
	// full run loop is overkill for these tests, so transition directly
	// (StateStopped → Idle is a valid transition).
	w.setState(StateIdle)
	return w, q
}

// requeuePolicy matches the client defaults with a 2h horizon so park
// waits (<=45m) schedule while give-up cases (48h) do not.
func requeuePolicy() *llm.FailurePolicyConfig {
	return &llm.FailurePolicyConfig{
		Horizon:           2 * time.Hour,
		BaseThrottle:      30 * time.Second,
		BaseQuota402Extra: 5 * time.Minute,
		PollFloor:         time.Hour,
	}
}

// enqueueOne enqueues a fresh job and returns it.
func enqueueOne(t *testing.T, q *queue.PersistentQueue) *queue.Job {
	t.Helper()
	job, err := queue.NewJob(queue.JobTypeOneOff, map[string]string{"prompt": "turn"})
	if err != nil {
		t.Fatalf("NewJob: %v", err)
	}
	if err := q.Enqueue(context.Background(), job); err != nil {
		t.Fatalf("Enqueue: %v", err)
	}
	return job
}

// TestWorker_ThrottleWaitRequeuesWithoutRetryCount: the processor
// surfaces a ThrottleBackoffError; the job is requeued at RetryAt with
// retry_count unchanged and the worker slot released (no error surfaces
// to the run loop).
func TestWorker_ThrottleWaitRequeuesWithoutRetryCount(t *testing.T) {
	retryAt := time.Now().UTC().Add(5 * time.Minute)
	w, q := newRequeueTestWorker(t, &requeueProcessor{err: &llm.ThrottleBackoffError{
		ProviderID: "p", ModelID: "m", RetryAt: retryAt, Attempt: 3,
	}}, requeuePolicy())

	ctx := context.Background()
	job := enqueueOne(t, q)

	ok, procErr := w.tryProcessJob(ctx)
	if procErr != nil {
		t.Fatalf("tryProcessJob surfaced the provider wait as an error: %v", procErr)
	}
	if !ok {
		t.Fatal("tryProcessJob reported no work done on requeue")
	}

	got, err := q.Get(ctx, job.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.State != queue.StatePending {
		t.Fatalf("state = %q, want pending after provider-wait requeue", got.State)
	}
	if got.RetryCount != 0 {
		t.Fatalf("retry_count = %d, want 0 (a provider wait is not a job failure)", got.RetryCount)
	}
	if got.NextRetryAt == nil || got.NextRetryAt.Before(retryAt.Add(-2*time.Second)) || got.NextRetryAt.After(retryAt.Add(2*time.Second)) {
		t.Fatalf("next_retry_at = %v, want ~%v (plan schedule)", got.NextRetryAt, retryAt)
	}
	if got.ClaimedBy != "" {
		t.Fatalf("claimed_by = %q, want empty (worker slot released)", got.ClaimedBy)
	}
}

// TestWorker_QuotaWaitRequeuesAtReset: same for a QuotaResetError — the
// job returns at the server reset time, retries untouched.
func TestWorker_QuotaWaitRequeuesAtReset(t *testing.T) {
	resetAt := time.Now().UTC().Add(30 * time.Minute)
	w, q := newRequeueTestWorker(t, &requeueProcessor{err: &llm.QuotaResetError{
		ProviderID: "p", ModelID: "m", Code: "usage_limit_reached", ResetAt: resetAt, MaxWait: 24 * time.Hour,
	}}, requeuePolicy())

	ctx := context.Background()
	job := enqueueOne(t, q)

	if _, procErr := w.tryProcessJob(ctx); procErr != nil {
		t.Fatalf("tryProcessJob surfaced the quota wait as an error: %v", procErr)
	}

	got, err := q.Get(ctx, job.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.State != queue.StatePending {
		t.Fatalf("state = %q, want pending after quota-wait requeue", got.State)
	}
	if got.RetryCount != 0 {
		t.Fatalf("retry_count = %d, want 0", got.RetryCount)
	}
	if got.NextRetryAt == nil || got.NextRetryAt.Before(resetAt.Add(-2*time.Second)) || got.NextRetryAt.After(resetAt.Add(2*time.Second)) {
		t.Fatalf("next_retry_at = %v, want ~%v (reset time)", got.NextRetryAt, resetAt)
	}
}

// TestWorker_AllModelsBlockedRequeuesAtPlanStep: ErrAllModelsQuotaBlocked
// has no attached schedule; the job returns at the quota plan's first
// step (base throttle + 402 extra, D5).
func TestWorker_AllModelsBlockedRequeuesAtPlanStep(t *testing.T) {
	w, q := newRequeueTestWorker(t, &requeueProcessor{err: llm.ErrAllModelsQuotaBlocked}, requeuePolicy())

	ctx := context.Background()
	job := enqueueOne(t, q)

	if _, procErr := w.tryProcessJob(ctx); procErr != nil {
		t.Fatalf("tryProcessJob surfaced the all-blocked wait as an error: %v", procErr)
	}

	got, err := q.Get(ctx, job.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.RetryCount != 0 {
		t.Fatalf("retry_count = %d, want 0", got.RetryCount)
	}
	if got.NextRetryAt == nil {
		t.Fatal("next_retry_at not set on all-blocked requeue")
	}
	want := time.Now().UTC().Add(requeuePolicy().BaseThrottle + requeuePolicy().BaseQuota402Extra)
	if got.NextRetryAt.Before(want.Add(-2*time.Second)) || got.NextRetryAt.After(want.Add(2*time.Second)) {
		t.Fatalf("next_retry_at = %v, want ~%v (plan first step)", got.NextRetryAt, want)
	}
}

// TestWorker_GenuineFailureKeepsLegacyPath: a non-provider error keeps
// the legacy behaviour — Fail (+ retry/dead-letter) and a surfaced error.
func TestWorker_GenuineFailureKeepsLegacyPath(t *testing.T) {
	w, q := newRequeueTestWorker(t, &requeueProcessor{err: errors.New("disk on fire")}, requeuePolicy())

	ctx := context.Background()
	job := enqueueOne(t, q)
	// The first enqueued job keeps NewJob's default MaxRetries=3, so the
	// legacy Fail path RETRIES it (state failed → retried) rather than
	// dead-lettering; the surfaced error is the assertion that matters
	// here. (A MaxRetries=0 variant is covered by the give-up test
	// below, which exercises the dead-letter branch.)
	if job.State != queue.StatePending {
		t.Fatalf("seed job state = %v, want pending", job.State)
	}
	job2, err := queue.NewJob(queue.JobTypeOneOff, map[string]string{"prompt": "turn"})
	if err != nil {
		t.Fatalf("NewJob: %v", err)
	}
	job2 = job2.WithMaxRetries(0)
	if err := q.Enqueue(ctx, job2); err != nil {
		t.Fatalf("Enqueue: %v", err)
	}

	_, procErr := w.tryProcessJob(ctx) // claims job (first)
	if procErr == nil || procErr.Error() != "disk on fire" {
		t.Fatalf("legacy failure error = %v, want the original process error", procErr)
	}
	// job2 remains pending (only one processed per call).
	got2, err := q.Get(ctx, job2.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got2.State != queue.StatePending {
		t.Fatalf("job2 state = %q, want pending", got2.State)
	}
}

// TestWorker_GiveUpQuotaKeepsLegacyPath: a quota reset beyond the policy
// horizon is a give-up — legacy Fail path, no requeue.
func TestWorker_GiveUpQuotaKeepsLegacyPath(t *testing.T) {
	w, q := newRequeueTestWorker(t, &requeueProcessor{err: &llm.QuotaResetError{
		ProviderID: "p", ModelID: "m", Code: "usage_limit_reached",
		ResetAt: time.Now().UTC().Add(48 * time.Hour), MaxWait: 72 * time.Hour,
	}}, requeuePolicy()) // horizon 2h < 48h reset

	ctx := context.Background()
	job, err := queue.NewJob(queue.JobTypeOneOff, map[string]string{"prompt": "turn"})
	if err != nil {
		t.Fatalf("NewJob: %v", err)
	}
	job = job.WithMaxRetries(0) // Fail → dead via the existing path
	if err := q.Enqueue(ctx, job); err != nil {
		t.Fatalf("Enqueue: %v", err)
	}

	_, procErr := w.tryProcessJob(ctx)
	if procErr == nil {
		t.Fatal("give-up must surface the original error to the legacy path")
	}
	var qe *llm.QuotaResetError
	if !errors.As(procErr, &qe) {
		t.Fatalf("give-up error lost the quota error: %v", procErr)
	}
	// MaxRetries=0 → the legacy Fail path dead-lettered the job (moved
	// out of jobs into dead_letter — untouched semantics).
	dl, err := q.ListDeadLetter(ctx, 10)
	if err != nil {
		t.Fatalf("ListDeadLetter: %v", err)
	}
	if len(dl) != 1 || dl[0].ID != job.ID {
		t.Fatalf("dead letter = %+v, want job %s", dl, job.ID)
	}
}

// TestWorker_NilPolicyUsesDefaults: no injected policy → nil-safe
// defaults keep requeue working (24h horizon).
func TestWorker_NilPolicyUsesDefaults(t *testing.T) {
	resetAt := time.Now().UTC().Add(30 * time.Minute)
	w, q := newRequeueTestWorker(t, &requeueProcessor{err: &llm.QuotaResetError{
		ProviderID: "p", ModelID: "m", Code: "usage_limit_reached", ResetAt: resetAt, MaxWait: 24 * time.Hour,
	}}, nil)

	ctx := context.Background()
	job := enqueueOne(t, q)
	if _, procErr := w.tryProcessJob(ctx); procErr != nil {
		t.Fatalf("tryProcessJob surfaced the quota wait: %v", procErr)
	}
	got, err := q.Get(ctx, job.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.State != queue.StatePending || got.RetryCount != 0 {
		t.Fatalf("state=%q retry_count=%d, want pending/0 under nil policy", got.State, got.RetryCount)
	}
}
