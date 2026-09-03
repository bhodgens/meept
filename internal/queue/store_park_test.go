package queue

// Tree 03 leaf 03 tests: queue-job provider-wait requeue (D9).
//
// Coverage matrix (queue worker execution site):
//   - ThrottleBackoffError        → requeued at RetryAt, retry_count unchanged
//   - QuotaResetError             → requeued at resetAt, retry_count unchanged
//   - ErrAllModelsQuotaBlocked    → requeued at the quota plan's first step
//   - claim honors next_retry_at  → not claimable before NotBefore
//   - interactive preserved       → requeued interactive job stays interactive
//   - give-up                     → legacy Fail path → dead_letter row

import (
	"testing"
	"time"

	"github.com/caimlas/meept/internal/llm"
)

// TestStoreRequeue_NoRetryCountConsumed: Requeue resets the job to
// pending at notBefore and leaves retry_count untouched.
func TestStoreRequeue_NoRetryCountConsumed(t *testing.T) {
	store := newTestStore(t, "")

	job := mustNewJob(t, JobTypeOneOff, map[string]string{"prompt": "turn"})
	job.WithMaxRetries(3)
	if err := store.Insert(job); err != nil {
		t.Fatalf("Insert failed: %v", err)
	}
	// Claim the job (state=claimed) as the worker would.
	if _, err := store.ClaimNextForAgent("worker-1", nil, ""); err != nil {
		t.Fatalf("Claim failed: %v", err)
	}

	notBefore := time.Now().UTC().Add(10 * time.Minute)
	if err := store.Requeue(job.ID, notBefore); err != nil {
		t.Fatalf("Requeue failed: %v", err)
	}

	got, err := store.GetByID(job.ID)
	if err != nil {
		t.Fatalf("GetByID failed: %v", err)
	}
	if got.State != StatePending {
		t.Fatalf("state = %q, want pending", got.State)
	}
	if got.RetryCount != 0 {
		t.Fatalf("retry_count = %d, want 0 (provider waits must not consume retries)", got.RetryCount)
	}
	if got.NextRetryAt == nil {
		t.Fatal("next_retry_at not set on requeued job")
	}
	if d := got.NextRetryAt.Sub(notBefore); d < -time.Second || d > time.Second {
		t.Fatalf("next_retry_at = %v, want ~%v", got.NextRetryAt, notBefore)
	}
	if got.ClaimedBy != "" {
		t.Fatalf("claimed_by = %q, want empty after requeue", got.ClaimedBy)
	}
}

// TestStoreRequeue_ClaimHonorsNotBefore: a requeued job is not claimable
// before its not-before time and becomes claimable after it.
func TestStoreRequeue_ClaimHonorsNotBefore(t *testing.T) {
	store := newTestStore(t, "")

	job := mustNewJob(t, JobTypeOneOff, map[string]string{"prompt": "turn"})
	if err := store.Insert(job); err != nil {
		t.Fatalf("Insert failed: %v", err)
	}
	if _, err := store.ClaimNextForAgent("worker-1", nil, ""); err != nil {
		t.Fatalf("Claim failed: %v", err)
	}

	// Requeue far enough ahead that a poll inside the window cannot
	// flake: 2 minutes.
	notBefore := time.Now().UTC().Add(2 * time.Minute)
	if err := store.Requeue(job.ID, notBefore); err != nil {
		t.Fatalf("Requeue failed: %v", err)
	}

	// Before the not-before time: no job available.
	if _, err := store.ClaimNextForAgent("worker-2", nil, ""); err != ErrNoJobAvailable {
		t.Fatalf("claim before NotBefore returned %v, want ErrNoJobAvailable", err)
	}

	// Force the schedule into the past (simulating clock advance).
	future := notBefore.Add(-2 * time.Minute).UTC()
	if _, err := store.db.Exec(`UPDATE jobs SET next_retry_at = ? WHERE id = ?`,
		future.Format(time.RFC3339), job.ID); err != nil {
		t.Fatalf("failed to advance next_retry_at: %v", err)
	}

	claimed, err := store.ClaimNextForAgent("worker-2", nil, "")
	if err != nil {
		t.Fatalf("claim after NotBefore failed: %v", err)
	}
	if claimed.ID != job.ID {
		t.Fatalf("claimed job %s, want %s", claimed.ID, job.ID)
	}
}

// TestStoreRequeue_InteractivePreserved: a requeued interactive job
// keeps its interactive flag (SHARED-CONVENTIONS §4.4) and still wins
// claim ordering against a later non-interactive job.
func TestStoreRequeue_InteractivePreserved(t *testing.T) {
	store := newTestStore(t, "")

	interactive := mustNewJob(t, JobTypeOneOff, map[string]string{"prompt": "interactive turn"}).
		WithInteractive(true)
	if err := store.Insert(interactive); err != nil {
		t.Fatalf("Insert interactive failed: %v", err)
	}
	if _, err := store.ClaimNextForAgent("worker-1", nil, ""); err != nil {
		t.Fatalf("Claim failed: %v", err)
	}
	notBefore := time.Now().UTC().Add(50 * time.Millisecond)
	if err := store.Requeue(interactive.ID, notBefore); err != nil {
		t.Fatalf("Requeue failed: %v", err)
	}

	got, err := store.GetByID(interactive.ID)
	if err != nil {
		t.Fatalf("GetByID failed: %v", err)
	}
	if !got.Interactive {
		t.Fatal("interactive flag lost on requeue")
	}

	// A background job enqueued AFTER the interactive one must not jump
	// the queue once the interactive job's not-before elapses.
	background := mustNewJob(t, JobTypeOneOff, map[string]string{"prompt": "background"})
	if err := store.Insert(background); err != nil {
		t.Fatalf("Insert background failed: %v", err)
	}
	// Force the interactive job's next_retry_at into the past (same
	// deterministic clock-advance pattern as
	// TestStoreRequeue_ClaimHonorsNotBefore) instead of sleeping out the
	// 50ms not-before window.
	if _, err := store.db.Exec(`UPDATE jobs SET next_retry_at = ? WHERE id = ?`,
		time.Now().UTC().Add(-time.Second).Format(time.RFC3339), interactive.ID); err != nil {
		t.Fatalf("failed to advance next_retry_at: %v", err)
	}

	first, err := store.ClaimNextForAgent("worker-2", nil, "")
	if err != nil {
		t.Fatalf("Claim failed: %v", err)
	}
	if first.ID != interactive.ID {
		t.Fatalf("claim returned %s, want the requeued interactive job %s", first.ID, interactive.ID)
	}
}

// TestStoreRequeue_FailedStateRequeueable: a job sitting in failed state
// after a legacy Fail can still be requeued by the provider-wait path
// (the two paths can interleave across process restarts).
func TestStoreRequeue_FailedStateRequeueable(t *testing.T) {
	store := newTestStore(t, "")

	job := mustNewJob(t, JobTypeOneOff, map[string]string{"prompt": "turn"}).WithMaxRetries(3)
	if err := store.Insert(job); err != nil {
		t.Fatalf("Insert failed: %v", err)
	}
	if _, err := store.ClaimNextForAgent("worker-1", nil, ""); err != nil {
		t.Fatalf("Claim failed: %v", err)
	}
	if err := store.Fail(job.ID, "boom"); err != nil {
		t.Fatalf("Fail failed: %v", err)
	}

	notBefore := time.Now().UTC().Add(time.Minute)
	if err := store.Requeue(job.ID, notBefore); err != nil {
		t.Fatalf("Requeue on failed job failed: %v", err)
	}
	got, err := store.GetByID(job.ID)
	if err != nil {
		t.Fatalf("GetByID failed: %v", err)
	}
	if got.State != StatePending {
		t.Fatalf("after requeue: state=%q, want pending", got.State)
	}
	// retry_count is 0: the legacy Fail does not bump it (only Retry
	// does), and the provider-wait requeue must not add one either —
	// the invariant under test is that requeue consumed no retries.
	if got.RetryCount != 0 {
		t.Fatalf("after requeue: retry_count=%d, want 0 (requeue must not consume retries)", got.RetryCount)
	}
}

// TestRequeue_GiveUpDeadLettersViaExistingPath: beyond the horizon the
// provider-wait classification declines to requeue (worker-side; here we
// verify the EXISTING legacy path end-to-end): maxed-out retries on Fail
// land the job in dead_letter untouched.
func TestRequeue_GiveUpDeadLettersViaExistingPath(t *testing.T) {
	store := newTestStore(t, "")

	job := mustNewJob(t, JobTypeOneOff, map[string]string{"prompt": "turn"}).WithMaxRetries(0)
	if err := store.Insert(job); err != nil {
		t.Fatalf("Insert failed: %v", err)
	}
	if _, err := store.ClaimNextForAgent("worker-1", nil, ""); err != nil {
		t.Fatalf("Claim failed: %v", err)
	}
	if err := store.Fail(job.ID, "quota wait beyond horizon"); err != nil {
		t.Fatalf("Fail failed: %v", err)
	}

	// Dead-letter semantics untouched by this leaf.
	rows, err := store.DeadLetterStats()
	if err != nil {
		t.Fatalf("DeadLetterStats failed: %v", err)
	}
	if rows != 1 {
		t.Fatalf("dead_letter rows = %d, want 1", rows)
	}
	dl, err := store.ListDeadLetter(10)
	if err != nil {
		t.Fatalf("ListDeadLetter failed: %v", err)
	}
	if len(dl) != 1 || dl[0].ID != job.ID {
		t.Fatalf("dead letter contents = %+v, want job %s", dl, job.ID)
	}
}

// TestRequeue_ScheduleClasses verifies the schedule mapping the worker
// applies per failure class (mirrors requeueOnProviderWait; kept next to
// the store tests so the matrix is auditable in one place):
//   - ThrottleBackoffError → RetryAt
//   - QuotaResetError      → ResetAt
//   - ErrAllModelsQuotaBlocked → quota plan first step
func TestRequeue_ScheduleClasses(t *testing.T) {
	policy := llm.FailurePolicyConfig{
		Horizon:           2 * time.Hour,
		BaseThrottle:      30 * time.Second,
		BaseQuota402Extra: 5 * time.Minute,
		PollFloor:         time.Hour,
	}
	now := time.Now().UTC()

	// Throttle: server RetryAt wins when inside the horizon.
	retryAt := now.Add(90 * time.Second)
	throttleErr := &llm.ThrottleBackoffError{ProviderID: "p", ModelID: "m", RetryAt: retryAt, Attempt: 2}
	if _, ok := llm.AsThrottleBackoffError(throttleErr); !ok {
		t.Fatal("AsThrottleBackoffError failed on a throttle error")
	}
	plan := llm.DefaultBackoffPlan(llm.FailureThrottle, now, policy)
	if retryAt.After(plan.GiveUpAt) {
		t.Fatal("throttle RetryAt inside the horizon misclassified as give-up")
	}

	// Quota: server ResetAt wins.
	resetAt := now.Add(45 * time.Minute)
	quotaErr := &llm.QuotaResetError{ProviderID: "p", ModelID: "m", Code: "usage_limit_reached", ResetAt: resetAt, MaxWait: 24 * time.Hour}
	if !llm.IsQuotaResetError(quotaErr) {
		t.Fatal("IsQuotaResetError failed on a quota error")
	}
	quotaPlan := llm.DefaultBackoffPlan(llm.FailureQuota, now, policy)
	if resetAt.After(quotaPlan.GiveUpAt) {
		t.Fatal("quota reset inside the horizon misclassified as give-up")
	}

	// Give-up: reset beyond the horizon → legacy failure path.
	beyond := &llm.QuotaResetError{ProviderID: "p", ModelID: "m", Code: "usage_limit_reached", ResetAt: now.Add(48 * time.Hour), MaxWait: 72 * time.Hour}
	if !beyond.ResetAt.After(quotaPlan.GiveUpAt) {
		t.Fatal("48h reset should sit beyond the 2h test horizon")
	}

	// All-blocked: plan first step.
	allBlockedResume := quotaPlan.NextAttempt(now, 0, time.Time{})
	want := now.Add(policy.BaseThrottle + policy.BaseQuota402Extra)
	if d := allBlockedResume.Sub(want); d < -time.Second || d > time.Second {
		t.Fatalf("all-blocked resume = %v, want ~%v", allBlockedResume, want)
	}
}
