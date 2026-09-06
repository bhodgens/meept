package agent

// Quota-aware job deferral tests (quota-reset-resilience): a quota-classified
// step-job failure must DEFER (requeue at the quota reset, no retry consumed)
// instead of failing, bounded by the deferral policy; non-quota failures keep
// the legacy retry/fail behavior.

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/llm"
	"github.com/caimlas/meept/internal/queue"
	"github.com/caimlas/meept/internal/task"
)

// requeueableQueue records Requeue calls and serves stored jobs.
type requeueableQueue struct {
	mockQueue
	enqueued   []*queue.Job
	jobs       map[string]*queue.Job
	requeues   map[string]time.Time
	retryCalls []string
	failCalls  []string
}

func newRequeueableQueue() *requeueableQueue {
	return &requeueableQueue{
		jobs:     make(map[string]*queue.Job),
		requeues: make(map[string]time.Time),
	}
}

func (q *requeueableQueue) Enqueue(_ context.Context, job *queue.Job) error {
	q.enqueued = append(q.enqueued, job)
	q.jobs[job.ID] = job
	return nil
}

// Get serves jobs registered via RegisterJob (the legacy retry path looks
// the job up to check CanRetry before calling Retry).
func (q *requeueableQueue) Get(_ context.Context, jobID string) (*queue.Job, error) {
	if job, ok := q.jobs[jobID]; ok {
		return job, nil
	}
	return nil, fmt.Errorf("job %s not found", jobID)
}

// RegisterJob seeds a job without enqueueing (OnJobFailed paths look jobs
// up by the jobID the failure event carried).
func (q *requeueableQueue) RegisterJob(job *queue.Job) {
	q.jobs[job.ID] = job
}

func (q *requeueableQueue) Requeue(_ context.Context, jobID string, notBefore time.Time) error {
	q.requeues[jobID] = notBefore
	return nil
}

func (q *requeueableQueue) Retry(_ context.Context, jobID string) error {
	q.retryCalls = append(q.retryCalls, jobID)
	return nil
}

func (q *requeueableQueue) Fail(_ context.Context, jobID string, err error) error {
	q.failCalls = append(q.failCalls, jobID)
	return nil
}

// newQuotaDeferralScheduler builds a scheduler wired to a real task/step
// store, a requeueable test queue, and a short deferral policy for tests.
func newQuotaDeferralScheduler(t *testing.T) (*TacticalScheduler, *requeueableQueue) {
	t.Helper()
	taskStore, stepStore := newTestTaskAndStepStore(t)
	q := newRequeueableQueue()
	ts := NewTacticalScheduler(TacticalSchedulerConfig{
		TaskStore: taskStore,
		StepStore: stepStore,
		Queue:     q,
		Logger:    slogDiscardLogger(),
		QuotaDeferral: &QuotaDeferralPolicy{
			MaxDeferrals:      2,
			MaxTotalDeferral:  6 * time.Hour,
			UnknownResetDelay: 5 * time.Minute,
		},
	})
	return ts, q
}

// quotaFailureMsg mirrors the production error text that survives the bus:
// QuotaResetError.Error() rendered by fmt.Errorf("%w", ...) plus the daemon's
// job-level quota stamp (components.go publishQuotaWait path).
func quotaFailureMsg(resetAt time.Time) string {
	qe := &llm.QuotaResetError{
		ProviderID: "agnes",
		ModelID:    "agnes-large",
		Code:       "usage_limit_reached",
		ResetAt:    resetAt,
		StatusCode: 429,
	}
	return fmt.Sprintf("agent execution failed: %v (quota wait: agnes/agnes-large is rate-limited until %s. your request is saved and will need a re-ask once the limit resets.)", qe, resetAt.UTC().Format(time.RFC3339))
}

// stepForJob creates a task + step in scheduled state bound to jobID.
func stepForJob(t *testing.T, ts *TacticalScheduler, jobID string) *task.TaskStep {
	t.Helper()
	step := task.NewTaskStep("task-quota-defer", "step under quota test", 1)
	step.JobID = jobID
	step.State = task.StepScheduled
	step.AgentID = "coder"
	if err := ts.stepStore.Create(step); err != nil {
		t.Fatalf("failed to create step: %v", err)
	}
	return step
}

func TestOnJobFailed_QuotaClassFailureDefersNotFails(t *testing.T) {
	ts, q := newQuotaDeferralScheduler(t)
	ctx := context.Background()

	jobID := "job-quota-1"
	stepForJob(t, ts, jobID)
	resetAt := time.Now().UTC().Add(3 * time.Hour)

	err := ts.OnJobFailed(ctx, jobID, quotaFailureMsg(resetAt))
	if err != nil {
		t.Fatalf("OnJobFailed returned error on quota deferral: %v", err)
	}

	// Job requeued, never failed or retried.
	if _, ok := q.requeues[jobID]; !ok {
		t.Fatal("expected job to be requeued on quota failure")
	}
	if len(q.failCalls) != 0 {
		t.Errorf("Fail called %d times on quota deferral, want 0", len(q.failCalls))
	}
	if len(q.retryCalls) != 0 {
		t.Errorf("Retry called %d times on quota deferral, want 0", len(q.retryCalls))
	}

	// Requeue gate lands at the quota reset (±1s for store formatting).
	got := q.requeues[jobID]
	if diff := got.Sub(resetAt); diff > time.Second || diff < -time.Second {
		t.Errorf("requeue notBefore = %v, want ~%v (reset)", got, resetAt)
	}

	// Step back to scheduled with a cleared result — not failed.
	fresh, err := ts.stepStore.GetByJobID(jobID)
	if err != nil || fresh == nil {
		t.Fatalf("failed to re-fetch step: %v", err)
	}
	if fresh.State != task.StepScheduled {
		t.Errorf("step state = %q, want scheduled", fresh.State)
	}
	if fresh.Result != "" {
		t.Errorf("step result = %q, want empty (deferred, not failed)", fresh.Result)
	}

	// Deferral counter recorded.
	step, _ := ts.stepStore.GetByJobID(jobID)
	if n := ts.quotaDeferrals[step.ID]; n != 1 {
		t.Errorf("deferral count = %d, want 1", n)
	}
}

func TestOnJobFailed_QuotaDeferralExhaustionFails(t *testing.T) {
	ts, q := newQuotaDeferralScheduler(t)
	ctx := context.Background()

	jobID := "job-quota-exhaust"
	stepForJob(t, ts, jobID)

	// Seed the counter at the cap (MaxDeferrals = 2 in the test policy).
	fresh, _ := ts.stepStore.GetByJobID(jobID)
	ts.quotaDeferrals[fresh.ID] = 2
	ts.quotaDeferralFirst[fresh.ID] = time.Now().UTC()

	err := ts.OnJobFailed(ctx, jobID, quotaFailureMsg(time.Now().UTC().Add(1*time.Hour)))
	if err != nil {
		t.Fatalf("OnJobFailed returned error on exhaustion: %v", err)
	}

	if len(q.requeues) != 0 {
		t.Errorf("job requeued %d times past the deferral cap, want 0", len(q.requeues))
	}

	// Step FAILED with the quota-deferred-exhausted stamp on the result.
	fresh, err = ts.stepStore.GetByJobID(jobID)
	if err != nil || fresh == nil {
		t.Fatalf("failed to re-fetch step: %v", err)
	}
	if fresh.State != task.StepFailed {
		t.Errorf("step state = %q, want failed after exhaustion", fresh.State)
	}
	if got := fresh.Result; !strings.Contains(got, "quota-deferred exhausted") {
		t.Errorf("step result = %q, want quota-deferred-exhausted stamp", got)
	}
}

func TestOnJobFailed_QuotaResetBeyondMaxTotalDeferralFails(t *testing.T) {
	ts, q := newQuotaDeferralScheduler(t)
	ctx := context.Background()

	jobID := "job-quota-far"
	stepForJob(t, ts, jobID)

	// Reset lands 8h out — beyond the 6h policy span → give up immediately.
	err := ts.OnJobFailed(ctx, jobID, quotaFailureMsg(time.Now().UTC().Add(8*time.Hour)))
	if err != nil {
		t.Fatalf("OnJobFailed returned error: %v", err)
	}

	if len(q.requeues) != 0 {
		t.Errorf("job requeued for a reset beyond the policy span, want 0 requeues")
	}
	fresh, _ := ts.stepStore.GetByJobID(jobID)
	if fresh.State != task.StepFailed {
		t.Errorf("step state = %q, want failed", fresh.State)
	}
}

func TestOnJobFailed_NonQuotaFailureKeepsLegacyPath(t *testing.T) {
	ts, q := newQuotaDeferralScheduler(t)
	ctx := context.Background()

	// A transient non-quota failure must take the legacy retry path
	// (queue.Retry with backoff), NOT the quota requeue.
	jobID := "job-transient"
	stepForJob(t, ts, jobID)
	q.RegisterJob((&queue.Job{ID: jobID, MaxRetries: 3}).WithAgentID("coder"))

	err := ts.OnJobFailed(ctx, jobID, "agent execution failed: HTTP 503: connection reset by peer")
	if err != nil {
		t.Fatalf("OnJobFailed returned error: %v", err)
	}

	if len(q.requeues) != 0 {
		t.Errorf("non-quota failure requeued %d times, want 0", len(q.requeues))
	}
	if len(q.retryCalls) != 1 {
		t.Fatalf("legacy retry path not taken: Retry calls = %d, want 1", len(q.retryCalls))
	}

	// A non-retryable non-quota failure (budget) fails the step outright.
	jobID2 := "job-budget"
	stepForJob(t, ts, jobID2)
	q.RegisterJob((&queue.Job{ID: jobID2, MaxRetries: 0}))
	if err := ts.OnJobFailed(ctx, jobID2, "token budget exceeded for task"); err != nil {
		t.Fatalf("OnJobFailed returned error: %v", err)
	}
	if len(q.requeues) != 0 {
		t.Errorf("budget failure requeued, want 0")
	}
	fresh, _ := ts.stepStore.GetByJobID(jobID2)
	if fresh.State != task.StepFailed {
		t.Errorf("budget-failure step state = %q, want failed", fresh.State)
	}
}

func TestQuotaResetAtFromMessage(t *testing.T) {
	reset := time.Now().UTC().Add(2 * time.Hour).Truncate(time.Second)
	msg := quotaFailureMsg(reset)
	got := quotaResetAtFromMessage(msg)
	if got.IsZero() {
		t.Fatalf("no reset time recovered from %q", msg)
	}
	if diff := got.Sub(reset); diff > time.Second || diff < -time.Second {
		t.Errorf("recovered reset = %v, want ~%v", got, reset)
	}
	// Bare QuotaResetError text (no daemon stamp) also parses.
	bare := (&llm.QuotaResetError{ProviderID: "p", ModelID: "m", ResetAt: reset}).Error()
	if got := quotaResetAtFromMessage(bare); got.IsZero() {
		t.Fatalf("bare QuotaResetError text not parsed: %q", bare)
	}
	// Past timestamps yield zero.
	if got := quotaResetAtFromMessage("resets_at=" + time.Now().UTC().Add(-time.Hour).Format(time.RFC3339)); !got.IsZero() {
		t.Errorf("past reset returned %v, want zero", got)
	}
	// Garbage yields zero.
	if got := quotaResetAtFromMessage("some totally unrelated error"); !got.IsZero() {
		t.Errorf("garbage message returned %v, want zero", got)
	}
}

func TestIsQuotaClassFailure(t *testing.T) {
	reset := time.Now().UTC().Add(time.Hour)
	cases := []struct {
		msg  string
		want bool
	}{
		{quotaFailureMsg(reset), true},
		{(&llm.QuotaResetError{ProviderID: "p", ModelID: "m", ResetAt: reset}).Error(), true},
		{"quota limit exceeded: provider=p model=m code=usage_limit_reached", true},
		{"resolve failed: all models in alias are quota-blocked: alias \"thinkhard\" (all 2 model(s) quota-blocked)", true},
		{"agent execution failed: HTTP 503: connection reset", false},
		{"rate limit exceeded: provider=p model=m, retry-after=30s", false},
		{"context deadline exceeded", false},
		{"token budget exceeded", false},
	}
	for _, tc := range cases {
		if got := isQuotaClassFailure(tc.msg); got != tc.want {
			t.Errorf("isQuotaClassFailure(%q) = %v, want %v", tc.msg, got, tc.want)
		}
	}
}

func TestOnJobCompleted_ClearsQuotaDeferralCounters(t *testing.T) {
	ts, _ := newQuotaDeferralScheduler(t)

	jobID := "job-quota-done"
	step := stepForJob(t, ts, jobID)
	ts.quotaDeferrals[step.ID] = 3
	ts.quotaDeferralFirst[step.ID] = time.Now().UTC()

	if err := ts.OnJobCompleted(context.Background(), jobID, []byte(`{"success":true}`)); err != nil {
		t.Fatalf("OnJobCompleted returned error: %v", err)
	}
	if _, ok := ts.quotaDeferrals[step.ID]; ok {
		t.Error("quota deferral counter not cleared on completion")
	}
	if _, ok := ts.quotaDeferralFirst[step.ID]; ok {
		t.Error("quota deferral first-time not cleared on completion")
	}
}
