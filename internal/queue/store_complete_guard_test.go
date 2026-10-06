package queue

// B2 stale-completion guard tests: Store.Complete must only transition a job
// from 'claimed'/'processing' to 'completed'. Jobs keep the same ID across
// Retry/Requeue, so a stale or duplicate completion event for a requeued job
// (back in 'pending') must fail with the ErrJobNotClaimable sentinel instead
// of overwriting the earlier result or pretending a second success.
//
// H4 (claim token): the guard above is job-state only, so it cannot tell a
// legitimate RETRY of a request that already succeeded from a completion whose
// attempt was superseded. CompleteAttempt adds the attempt token: the same
// token completing twice is idempotent SUCCESS (no second event), a
// SUPERSEDED token is still ErrJobNotClaimable.
//
// H5 (reclaim safety): ResetToPending retires the token, so a reclaimed-then-
// re-executed job's stale completion cannot clobber the new attempt.

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/bus"
)

// completeableJob inserts a job and moves it into the requested pre-complete
// state, mirroring the real lifecycle paths:
//   - claimed/processing: Claim (+ MarkProcessing)
//   - pending: fresh insert (or post-Requeue shape)
//   - failed: Fail
func completeableJob(t *testing.T, s *Store, state JobState) *Job {
	t.Helper()

	job := mustNewJob(t, JobTypeOneOff, map[string]string{"prompt": "b2"})
	if err := s.Insert(job); err != nil {
		t.Fatalf("Insert failed: %v", err)
	}

	switch state {
	case StateClaimed:
		if _, err := s.ClaimNextForAgent("worker-b2", nil, ""); err != nil {
			t.Fatalf("Claim failed: %v", err)
		}
	case StateProcessing:
		if _, err := s.ClaimNextForAgent("worker-b2", nil, ""); err != nil {
			t.Fatalf("Claim failed: %v", err)
		}
		if err := s.UpdateState(job.ID, StateProcessing); err != nil {
			t.Fatalf("MarkProcessing failed: %v", err)
		}
	case StateFailed:
		if _, err := s.ClaimNextForAgent("worker-b2", nil, ""); err != nil {
			t.Fatalf("Claim failed: %v", err)
		}
		if err := s.Fail(job.ID, "boom"); err != nil {
			t.Fatalf("Fail failed: %v", err)
		}
	case StatePending:
		// fresh insert (or post-Requeue shape)
	default:
		t.Fatalf("unsupported setup state: %s", state)
	}
	return job
}

// TestStoreComplete_StateGuard is table-driven over the legal and illegal
// pre-complete states.
func TestStoreComplete_StateGuard(t *testing.T) {
	tests := []struct {
		name    string
		state   JobState
		wantErr bool
	}{
		{"claimed is completable", StateClaimed, false},
		{"processing is completable", StateProcessing, false},
		{"pending is not completable", StatePending, true},
		{"failed is not completable", StateFailed, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := newTestStore(t, "")
			job := completeableJob(t, store, tt.state)

			err := store.Complete(job.ID, map[string]string{"result": "done"})
			if tt.wantErr {
				if !errors.Is(err, ErrJobNotClaimable) {
					t.Fatalf("Complete from %s: err = %v, want ErrJobNotClaimable", tt.state, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("Complete from %s failed: %v", tt.state, err)
			}
		})
	}
}

// TestStoreComplete_DuplicateCompletionFails: completing twice must fail the
// second call with the sentinel AND must not overwrite the first result.
//
// This is the TOKEN-LESS caller contract (bus/RPC/HTTP surfaces, which hold no
// attempt): the state-only guard, unchanged by H4. The token-aware equivalent
// for a worker holding a claim is TestStoreCompleteAttempt_SameTokenIsIdempotent.
func TestStoreComplete_DuplicateCompletionFails(t *testing.T) {
	store := newTestStore(t, "")
	job := completeableJob(t, store, StateClaimed)

	if err := store.Complete(job.ID, map[string]string{"result": "first"}); err != nil {
		t.Fatalf("first Complete failed: %v", err)
	}

	err := store.Complete(job.ID, map[string]string{"result": "STALE"})
	if !errors.Is(err, ErrJobNotClaimable) {
		t.Fatalf("second Complete: err = %v, want ErrJobNotClaimable", err)
	}

	got, err := store.GetByID(job.ID)
	if err != nil {
		t.Fatalf("GetByID failed: %v", err)
	}
	if got.State != StateCompleted {
		t.Errorf("state = %q, want %q", got.State, StateCompleted)
	}
	if string(got.Result) != `{"result":"first"}` {
		t.Errorf("result = %s, want the first result preserved verbatim", got.Result)
	}
}

// claimAndToken inserts a job, claims it, and returns the job plus the claim
// token the claim minted — the attempt identity a completion must present.
func claimAndToken(t *testing.T, store *Store, workerID string) (*Job, string) {
	t.Helper()

	job := mustNewJob(t, JobTypeOneOff, map[string]string{"prompt": "token"})
	if err := store.Insert(job); err != nil {
		t.Fatalf("Insert failed: %v", err)
	}
	claimed, err := store.ClaimNextForAgent(workerID, nil, "")
	if err != nil {
		t.Fatalf("Claim failed: %v", err)
	}
	if claimed.ClaimToken == "" {
		t.Fatal("Claim returned an empty claim token; the attempt would be unidentifiable")
	}
	return claimed, claimed.ClaimToken
}

// TestStoreCompleteAttempt_SameTokenIsIdempotent (H4 pin 1): the SAME worker
// retrying the identical completion must get success — not a 409 — while the
// first result stands and the disposition says "not fresh" so no second event
// is published. This is the case the pre-H4 guard could not express.
func TestStoreCompleteAttempt_SameTokenIsIdempotent(t *testing.T) {
	store := newTestStore(t, "")
	job, token := claimAndToken(t, store, "worker-h4")

	disp, err := store.CompleteAttempt(job.ID, map[string]string{"result": "first"}, token)
	if err != nil {
		t.Fatalf("first CompleteAttempt failed: %v", err)
	}
	if !disp.CompletionIsFresh() {
		t.Error("first completion must be fresh (publishable)")
	}

	// The identical request, retried by the same worker.
	disp, err = store.CompleteAttempt(job.ID, map[string]string{"result": "first"}, token)
	if err != nil {
		t.Fatalf("retried CompleteAttempt: err = %v, want idempotent success", err)
	}
	if disp.CompletionIsFresh() {
		t.Error("retried completion must NOT be fresh (it would republish the event)")
	}

	got, err := store.GetByID(job.ID)
	if err != nil {
		t.Fatalf("GetByID failed: %v", err)
	}
	if string(got.Result) != `{"result":"first"}` {
		t.Errorf("result = %s, want the first result preserved verbatim", got.Result)
	}
}

// TestStoreCompleteAttempt_SupersededAttemptRejected (H4 pin 2): a completion
// carrying a token from a SUPERSEDED attempt must still be refused with
// ErrJobNotClaimable. Retry moves the job back to pending and retires the token;
// the worker that was mid-flight on the old attempt is exactly the stale
// completion the guard exists to drop.
func TestStoreCompleteAttempt_SupersededAttemptRejected(t *testing.T) {
	store := newTestStore(t, "")
	job, staleToken := claimAndToken(t, store, "worker-h4")

	if err := store.Retry(job.ID); err != nil {
		t.Fatalf("Retry failed: %v", err)
	}
	clearRetryBackoff(t, store, job.ID)

	// Attempt 2 claims the requeued job and mints its own token.
	attempt2, err := store.ClaimNextForAgent("worker-h4b", nil, "")
	if err != nil {
		t.Fatalf("second Claim failed: %v", err)
	}
	freshToken := attempt2.ClaimToken
	if attempt2.ID != job.ID {
		t.Fatalf("re-claimed job = %s, want %s", attempt2.ID, job.ID)
	}
	if freshToken == staleToken {
		t.Fatal("re-claim reused the previous attempt's token; attempts would be indistinguishable")
	}

	// The attempt-1 worker finishes late and completes.
	_, err = store.CompleteAttempt(job.ID, map[string]string{"result": "attempt 1"}, staleToken)
	if !errors.Is(err, ErrJobNotClaimable) {
		t.Fatalf("superseded CompleteAttempt: err = %v, want ErrJobNotClaimable", err)
	}

	got, err := store.GetByID(job.ID)
	if err != nil {
		t.Fatalf("GetByID failed: %v", err)
	}
	if got.Result != nil {
		t.Errorf("result = %s, want nil (a superseded attempt must not write a result)", got.Result)
	}
	if got.State == StateCompleted {
		t.Error("state = completed, want claimed: the live attempt must stay in flight")
	}
}

// TestStoreCompleteAttempt_ReclaimedReexecutionRejectsStaleCompletion (H5
// pin): the cluster reclaim window. A worker is mid-flight on attempt 1; the
// claim goes stale, ResetToPending flips the job to pending AND retires the
// token; another worker re-executes it under a new token. Attempt 1's late
// success must NOT clobber attempt 2 — and CompletionIsFresh must report it as
// stale, which is what the internal/agent stale-completion guard consumes.
func TestStoreCompleteAttempt_ReclaimedReexecutionRejectsStaleCompletion(t *testing.T) {
	store := newTestStore(t, "")
	job, staleToken := claimAndToken(t, store, "worker-h5")
	if err := store.UpdateState(job.ID, StateProcessing); err != nil {
		t.Fatalf("MarkProcessing failed: %v", err)
	}

	// Reclaim: the cluster sweep considers attempt 1 dead.
	if err := store.ResetToPending(t.Context(), job.ID); err != nil {
		t.Fatalf("ResetToPending failed: %v", err)
	}

	// The job is re-executed by a second worker.
	attempt2, err := store.ClaimNextForAgent("worker-h5b", nil, "")
	if err != nil {
		t.Fatalf("re-execution claim failed: %v", err)
	}
	freshToken := attempt2.ClaimToken
	if attempt2.ID != job.ID || freshToken == staleToken {
		t.Fatalf("re-execution did not produce a new attempt: id=%s token=%q", attempt2.ID, freshToken)
	}

	// The guard's view: attempt 1's completion is stale, attempt 2's is fresh.
	if fresh, ok := store.CompletionIsFresh(job.ID, staleToken); !ok || fresh {
		t.Errorf("CompletionIsFresh(stale token) = (%v, %v), want (false, true)", fresh, ok)
	}
	if fresh, ok := store.CompletionIsFresh(job.ID, freshToken); !ok || !fresh {
		t.Errorf("CompletionIsFresh(live token) = (%v, %v), want (true, true)", fresh, ok)
	}

	// Attempt 1's late success is refused and writes nothing.
	_, err = store.CompleteAttempt(job.ID, map[string]string{"result": "attempt 1 result"}, staleToken)
	if !errors.Is(err, ErrJobNotClaimable) {
		t.Fatalf("stale completion after reclaim: err = %v, want ErrJobNotClaimable", err)
	}

	// Attempt 2's completion still lands — the reclaim did not poison the job.
	if _, err := store.CompleteAttempt(job.ID, map[string]string{"result": "attempt 2 result"}, freshToken); err != nil {
		t.Fatalf("live attempt completion after reclaim failed: %v", err)
	}

	got, err := store.GetByID(job.ID)
	if err != nil {
		t.Fatalf("GetByID failed: %v", err)
	}
	if string(got.Result) != `{"result":"attempt 2 result"}` {
		t.Errorf("result = %s, want attempt 2's result", got.Result)
	}
}

// TestStoreUpdateState_RefusesTerminalTransition (L8 pin): MarkProcessing must
// not resurrect a finished job. Symmetric with the Complete guard.
func TestStoreUpdateState_RefusesTerminalTransition(t *testing.T) {
	store := newTestStore(t, "")
	job, _ := claimAndToken(t, store, "worker-l8")

	if _, err := store.CompleteAttempt(job.ID, map[string]string{"result": "done"}, mustClaimToken(t, store, job.ID)); err != nil {
		t.Fatalf("CompleteAttempt failed: %v", err)
	}

	err := store.UpdateState(job.ID, StateProcessing)
	if !errors.Is(err, ErrJobStateLocked) {
		t.Fatalf("MarkProcessing on a completed job: err = %v, want ErrJobStateLocked", err)
	}

	got, err := store.GetByID(job.ID)
	if err != nil {
		t.Fatalf("GetByID failed: %v", err)
	}
	if got.State != StateCompleted {
		t.Errorf("state = %q, want %q (a completed job must not be resurrected)", got.State, StateCompleted)
	}
}

// mustClaimToken reads the current attempt token for a job.
func mustClaimToken(t *testing.T, store *Store, jobID string) string {
	t.Helper()
	var token string
	if err := store.DB().QueryRow(`SELECT claim_token FROM jobs WHERE id = ?`, jobID).Scan(&token); err != nil {
		t.Fatalf("failed to read claim token for %s: %v", jobID, err)
	}
	return token
}

// clearRetryBackoff drops the next_retry_at gate Retry/Requeue set, so the
// re-claim happens immediately instead of after the backoff window. These pins
// are about attempt identity, not backoff timing — waiting out the real 2s
// would just make them slower without testing anything extra.
func clearRetryBackoff(t *testing.T, store *Store, jobID string) {
	t.Helper()
	if _, err := store.DB().Exec(`UPDATE jobs SET next_retry_at = NULL WHERE id = ?`, jobID); err != nil {
		t.Fatalf("failed to clear next_retry_at for %s: %v", jobID, err)
	}
}

// TestStoreComplete_RequeuedJobRejectsStaleCompletion: the B2 scenario — a
// claimed job is requeued (same ID, back to pending); a late completion for
// it must be rejected.
func TestStoreComplete_RequeuedJobRejectsStaleCompletion(t *testing.T) {
	store := newTestStore(t, "")
	job := completeableJob(t, store, StateClaimed)

	notBefore := time.Now().UTC().Add(time.Minute)
	if err := store.Requeue(job.ID, notBefore); err != nil {
		t.Fatalf("Requeue failed: %v", err)
	}

	err := store.Complete(job.ID, map[string]string{"result": "stale attempt 1"})
	if !errors.Is(err, ErrJobNotClaimable) {
		t.Fatalf("Complete on requeued job: err = %v, want ErrJobNotClaimable", err)
	}

	got, err := store.GetByID(job.ID)
	if err != nil {
		t.Fatalf("GetByID failed: %v", err)
	}
	if got.State != StatePending {
		t.Errorf("state = %q, want %q (requeued job must stay pending)", got.State, StatePending)
	}
	if got.Result != nil {
		t.Errorf("result = %s, want nil (stale result must not be stored)", got.Result)
	}
}

// TestPersistentQueue_DuplicateCompletePublishesOnce: the queue-level Complete
// must not publish queue.job.completed when store.Complete rejects the
// transition, so exactly one event reaches subscribers.
func TestPersistentQueue_DuplicateCompletePublishesOnce(t *testing.T) {
	msgBus := bus.New(nil, nil)
	q, err := NewPersistentQueue(filepath.Join(t.TempDir(), "b2.db"), msgBus, nil)
	if err != nil {
		t.Fatalf("NewPersistentQueue failed: %v", err)
	}
	t.Cleanup(func() { q.Close() })

	ctx := context.Background()
	job := mustNewJob(t, JobTypeOneOff, map[string]string{"prompt": "once"})
	if err := q.store.Insert(job); err != nil {
		t.Fatalf("Insert failed: %v", err)
	}
	if _, err := q.Claim(ctx, "worker-b2", nil, ""); err != nil {
		t.Fatalf("Claim failed: %v", err)
	}

	sub := msgBus.Subscribe("test-b2-once", "queue.job.completed")
	defer msgBus.Unsubscribe(sub)

	if err := q.Complete(ctx, job.ID, map[string]string{"result": "done"}); err != nil {
		t.Fatalf("first Complete failed: %v", err)
	}
	if err := q.Complete(ctx, job.ID, map[string]string{"result": "STALE"}); !errors.Is(err, ErrJobNotClaimable) {
		t.Fatalf("second Complete: err = %v, want ErrJobNotClaimable", err)
	}

	// Exactly one event.
	select {
	case <-sub.Channel:
		// first (correct) event received
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for the first queue.job.completed event")
	}
	select {
	case msg := <-sub.Channel:
		t.Fatalf("unexpected second queue.job.completed event: payload keys %+v", msg.Payload)
	case <-time.After(300 * time.Millisecond):
		// expected: no duplicate event
	}
}

// TestPersistentQueue_SameTokenRetryIsIdempotentAndPublishesOnce (H4 pin, queue
// level): the retrying-client case end to end. A worker holding its claim token
// retries the identical completion — it must get SUCCESS (no 409) and exactly
// ONE queue.job.completed event, because republishing would double-count the
// job downstream (task finalization, memory sync).
func TestPersistentQueue_SameTokenRetryIsIdempotentAndPublishesOnce(t *testing.T) {
	msgBus := bus.New(nil, nil)
	q, err := NewPersistentQueue(filepath.Join(t.TempDir(), "h4.db"), msgBus, nil)
	if err != nil {
		t.Fatalf("NewPersistentQueue failed: %v", err)
	}
	t.Cleanup(func() { q.Close() })

	ctx := context.Background()
	job := mustNewJob(t, JobTypeOneOff, map[string]string{"prompt": "retry"})
	if err := q.store.Insert(job); err != nil {
		t.Fatalf("Insert failed: %v", err)
	}
	claimed, err := q.Claim(ctx, "worker-h4", nil, "")
	if err != nil {
		t.Fatalf("Claim failed: %v", err)
	}
	if claimed.ClaimToken == "" {
		t.Fatal("Claim returned no claim token")
	}

	sub := msgBus.Subscribe("test-h4-retry", "queue.job.completed")
	defer msgBus.Unsubscribe(sub)

	if err := q.CompleteAttempt(ctx, claimed.ID, map[string]string{"result": "done"}, claimed.ClaimToken); err != nil {
		t.Fatalf("first CompleteAttempt failed: %v", err)
	}
	if err := q.CompleteAttempt(ctx, claimed.ID, map[string]string{"result": "done"}, claimed.ClaimToken); err != nil {
		t.Fatalf("retried CompleteAttempt: err = %v, want idempotent success", err)
	}

	select {
	case <-sub.Channel:
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for the queue.job.completed event")
	}
	select {
	case msg := <-sub.Channel:
		t.Fatalf("idempotent retry republished the completion event: payload keys %+v", msg.Payload)
	case <-time.After(300 * time.Millisecond):
		// expected: exactly one event
	}
}

// TestPersistentQueue_SupersededTokenRefusedAndUnpublished (H4 pin, queue
// level): a completion carrying a superseded attempt's token is refused and
// publishes nothing — the attempt-1 result must never reach subscribers as if
// it were the live attempt's.
func TestPersistentQueue_SupersededTokenRefusedAndUnpublished(t *testing.T) {
	msgBus := bus.New(nil, nil)
	q, err := NewPersistentQueue(filepath.Join(t.TempDir(), "h4b.db"), msgBus, nil)
	if err != nil {
		t.Fatalf("NewPersistentQueue failed: %v", err)
	}
	t.Cleanup(func() { q.Close() })

	ctx := context.Background()
	job := mustNewJob(t, JobTypeOneOff, map[string]string{"prompt": "superseded"})
	if err := q.store.Insert(job); err != nil {
		t.Fatalf("Insert failed: %v", err)
	}
	attempt1, err := q.Claim(ctx, "worker-a", nil, "")
	if err != nil {
		t.Fatalf("Claim failed: %v", err)
	}

	// The job is requeued and re-claimed under a new token.
	if err := q.Retry(ctx, job.ID); err != nil {
		t.Fatalf("Retry failed: %v", err)
	}
	clearRetryBackoff(t, q.store, job.ID)
	attempt2, err := q.Claim(ctx, "worker-b", nil, "")
	if err != nil {
		t.Fatalf("re-claim failed: %v", err)
	}
	if attempt2.ClaimToken == attempt1.ClaimToken {
		t.Fatal("re-claim reused the attempt token")
	}

	sub := msgBus.Subscribe("test-h4-superseded", "queue.job.completed")
	defer msgBus.Unsubscribe(sub)

	if err := q.CompleteAttempt(ctx, job.ID, map[string]string{"result": "attempt 1"}, attempt1.ClaimToken); !errors.Is(err, ErrJobNotClaimable) {
		t.Fatalf("superseded completion: err = %v, want ErrJobNotClaimable", err)
	}

	select {
	case msg := <-sub.Channel:
		t.Fatalf("superseded attempt published a completion event: payload keys %+v", msg.Payload)
	case <-time.After(300 * time.Millisecond):
		// expected: nothing published
	}
}

// TestPersistentQueue_ReclaimedReexecutionPublishesOnlyTheLiveAttempt (H5
// pin, queue level): the cluster-reclaim window end to end. The reclaim retires
// the abandoned attempt's token; only the re-execution's completion may publish.
func TestPersistentQueue_ReclaimedReexecutionPublishesOnlyTheLiveAttempt(t *testing.T) {
	msgBus := bus.New(nil, nil)
	q, err := NewPersistentQueue(filepath.Join(t.TempDir(), "h5.db"), msgBus, nil)
	if err != nil {
		t.Fatalf("NewPersistentQueue failed: %v", err)
	}
	t.Cleanup(func() { q.Close() })

	ctx := context.Background()
	job := mustNewJob(t, JobTypeOneOff, map[string]string{"prompt": "reclaim"})
	if err := q.store.Insert(job); err != nil {
		t.Fatalf("Insert failed: %v", err)
	}
	abandoned, err := q.Claim(ctx, "worker-h5-a", nil, "")
	if err != nil {
		t.Fatalf("Claim failed: %v", err)
	}
	if err := q.MarkProcessing(ctx, job.ID); err != nil {
		t.Fatalf("MarkProcessing failed: %v", err)
	}

	// The cluster reclaim considers that attempt dead.
	if err := q.Store().ResetToPending(ctx, job.ID); err != nil {
		t.Fatalf("ResetToPending failed: %v", err)
	}
	live, err := q.Claim(ctx, "worker-h5-b", nil, "")
	if err != nil {
		t.Fatalf("re-execution claim failed: %v", err)
	}

	sub := msgBus.Subscribe("test-h5-reclaim", "queue.job.completed")
	defer msgBus.Unsubscribe(sub)

	// The abandoned attempt finishes late.
	if err := q.CompleteAttempt(ctx, job.ID, map[string]string{"result": "abandoned"}, abandoned.ClaimToken); !errors.Is(err, ErrJobNotClaimable) {
		t.Fatalf("stale completion after reclaim: err = %v, want ErrJobNotClaimable", err)
	}
	select {
	case msg := <-sub.Channel:
		t.Fatalf("abandoned attempt published a completion event: payload keys %+v", msg.Payload)
	case <-time.After(300 * time.Millisecond):
		// expected: the stale completion publishes nothing
	}

	// The live attempt's completion lands and does publish.
	if err := q.CompleteAttempt(ctx, job.ID, map[string]string{"result": "live"}, live.ClaimToken); err != nil {
		t.Fatalf("live attempt completion failed: %v", err)
	}
	select {
	case <-sub.Channel:
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for the live attempt's completion event")
	}

	got, err := q.Get(ctx, job.ID)
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}
	if string(got.Result) != `{"result":"live"}` {
		t.Errorf("result = %s, want the live attempt's result (the stale one must not clobber it)", got.Result)
	}
}
