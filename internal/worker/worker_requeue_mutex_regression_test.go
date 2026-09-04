package worker

// Auditor E regression (mutex leak on the provider-wait requeue return):
// tryProcessJob used to `return true, nil` from the requeueOnProviderWait
// branch while still holding w.mu and with the FSM parked in
// StateProcessing — every subsequent Lock acquisition (next run-loop
// poll, Stop, GetState/GetStats/GetCurrentJob) deadlocked, and the
// worker starved forever. Assert both invariants are now held.

import (
	"context"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/llm"
	"github.com/caimlas/meept/internal/queue"
)

func TestWorker_RequeueReleasesLockAndState(t *testing.T) {
	retryAt := time.Now().UTC().Add(5 * time.Minute)
	proc := &requeueProcessor{err: &llm.ThrottleBackoffError{
		ProviderID: "p", ModelID: "m", RetryAt: retryAt, Attempt: 1,
	}}
	w, q := newRequeueTestWorker(t, proc, requeuePolicy())

	ctx := context.Background()
	job, err := queue.NewJob(queue.JobTypeOneOff, map[string]string{"prompt": "turn"})
	if err != nil {
		t.Fatalf("NewJob: %v", err)
	}
	if err := q.Enqueue(ctx, job); err != nil {
		t.Fatalf("Enqueue: %v", err)
	}

	if _, procErr := w.tryProcessJob(ctx); procErr != nil {
		t.Fatalf("tryProcessJob surfaced the provider wait: %v", procErr)
	}

	// (a) Lock released: TryLock is deterministic (no hang on failure).
	if !w.mu.TryLock() {
		t.Fatal("w.mu still held after requeue return — next poll/Stop/GetState deadlocks")
	}
	w.mu.Unlock()

	// (b) FSM left in a claimable-again resting state (Complete, like the
	// success path; the next tryProcessJob resets Complete → Idle).
	w.mu.RLock()
	state := w.State
	w.mu.RUnlock()
	if state == StateProcessing || state == StateClaiming {
		t.Fatalf("state after requeue = %q, want a claimable-again resting state", state)
	}
}
