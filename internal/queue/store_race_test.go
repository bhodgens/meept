package queue

// Regression test for audit M-1 (leaf 01-queue/01, contract C-Q1): the
// requeue-vs-MarkProcessing race. A worker holds claim token T for a job;
// concurrently the job is requeued (provider-wait / reclaim), which clears
// claim_token and parks the row back in 'pending'. The old attempt's late
// MarkProcessing(T) must be CAS-refused — it can neither strand the row in
// 'processing' under a dead claim nor block the next worker — and the row
// must remain claimable.

import (
	"errors"
	"testing"
	"time"
)

func TestMarkProcessingVsRequeueRace(t *testing.T) {
	store := newTestStore(t, "")
	job := mustNewJob(t, JobTypeOneOff, map[string]string{"prompt": "race"})
	if err := store.Insert(job); err != nil {
		t.Fatalf("Insert failed: %v", err)
	}

	claimed, err := store.ClaimNextForAgent("worker-race-a", nil, "")
	if err != nil {
		t.Fatalf("ClaimNextForAgent failed: %v", err)
	}
	if claimed.ClaimToken == "" {
		t.Fatal("claim minted no token; the race is unrepresentable")
	}

	// Concurrent actor requeues the job. notBefore is in the past so the
	// re-claim below is not gated by the retry window.
	if err := store.Requeue(claimed.ID, time.Now().UTC().Add(-time.Hour)); err != nil {
		t.Fatalf("Requeue failed: %v", err)
	}

	// The superseded attempt now tries to mark processing with its dead token.
	err = store.MarkProcessing(claimed.ID, claimed.ClaimToken)
	if !errors.Is(err, ErrJobStateLocked) {
		t.Fatalf("MarkProcessing with superseded token: err = %v, want ErrJobStateLocked", err)
	}

	// The refusal must not have touched the row: still pending, claimable.
	var state string
	if err := store.db.QueryRow(`SELECT state FROM jobs WHERE id = ?`, claimed.ID).Scan(&state); err != nil {
		t.Fatalf("read state: %v", err)
	}
	if state != string(StatePending) {
		t.Fatalf("after refused MarkProcessing: state = %s, want pending", state)
	}

	// A fresh worker claims it and its MarkProcessing lands normally.
	reclaimed, err := store.ClaimNextForAgent("worker-race-b", nil, "")
	if err != nil {
		t.Fatalf("re-claim after Requeue failed: %v", err)
	}
	if reclaimed.ID != claimed.ID {
		t.Fatalf("re-claim returned a different job: %s != %s", reclaimed.ID, claimed.ID)
	}
	if reclaimed.ClaimToken == claimed.ClaimToken {
		t.Fatal("re-claim reused the retired token")
	}
	if err := store.MarkProcessing(reclaimed.ID, reclaimed.ClaimToken); err != nil {
		t.Fatalf("MarkProcessing with live token: %v, want nil", err)
	}
	if err := store.db.QueryRow(`SELECT state FROM jobs WHERE id = ?`, claimed.ID).Scan(&state); err != nil {
		t.Fatalf("read state: %v", err)
	}
	if state != string(StateProcessing) {
		t.Errorf("final state = %s, want processing", state)
	}
}
