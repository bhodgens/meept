package queue

// A5-7 (bughunt 2026-11-25): Store.ClaimNextByID must carry the due_at /
// next_retry_at predicates atomically. The slow claim path selected the
// job as due in Go and then claimed it by ID without re-checking the
// deadline columns in the UPDATE — a row requeued into backoff (or a
// queue-backend parked row with the far-future due_at sentinel) between
// the two statements was claimed anyway.

import (
	"path/filepath"
	"testing"
	"time"
)

func TestClaimNextByIDRespectsDueAndBackoff(t *testing.T) {
	store := newTestStore(t, filepath.Join(t.TempDir(), "queue.db"))

	// 1. A pending job with a FUTURE due_at (the parked-row sentinel shape)
	// must not be claimable by ID.
	parked := mustNewJob(t, JobTypeOneOff, map[string]string{"prompt": "parked"})
	parked.WithDueAt(time.Now().UTC().Add(2 * time.Hour))
	if err := store.Insert(parked); err != nil {
		t.Fatalf("Insert parked: %v", err)
	}
	if _, err := store.ClaimNextByID(parked.ID, "w-1"); err != ErrJobAlreadyClaimed {
		t.Fatalf("claim of a future-due job = %v, want ErrJobAlreadyClaimed (claim refused atomically)", err)
	}

	// 2. A pending job inside retry backoff (next_retry_at in the future —
	// the TOCTOU shape: selected as due, then requeued into backoff) must
	// not be claimable by ID.
	backingOff := mustNewJob(t, JobTypeOneOff, map[string]string{"prompt": "retry"})
	if err := store.Insert(backingOff); err != nil {
		t.Fatalf("Insert backingOff: %v", err)
	}
	future := time.Now().UTC().Add(10 * time.Minute).Format(time.RFC3339)
	if _, err := store.db.Exec(`UPDATE jobs SET next_retry_at = ? WHERE id = ?`, future, backingOff.ID); err != nil {
		t.Fatalf("seed next_retry_at: %v", err)
	}
	if _, err := store.ClaimNextByID(backingOff.ID, "w-1"); err != ErrJobAlreadyClaimed {
		t.Fatalf("claim of a backoff job = %v, want ErrJobAlreadyClaimed (slow-path TOCTOU bypass)", err)
	}

	// 3. Non-vacuity: a plain due pending job claims fine, and once the
	// backoff elapses (rewritten into the past) the SAME row claims.
	claimable := mustNewJob(t, JobTypeOneOff, map[string]string{"prompt": "due"})
	if err := store.Insert(claimable); err != nil {
		t.Fatalf("Insert claimable: %v", err)
	}
	got, err := store.ClaimNextByID(claimable.ID, "w-1")
	if err != nil || got == nil || got.ID != claimable.ID {
		t.Fatalf("claim of a due job = (%v, %v), want the job", got, err)
	}

	past := time.Now().UTC().Add(-time.Second).Format(time.RFC3339)
	if _, err := store.db.Exec(`UPDATE jobs SET next_retry_at = ? WHERE id = ?`, past, backingOff.ID); err != nil {
		t.Fatalf("expire backoff: %v", err)
	}
	got2, err := store.ClaimNextByID(backingOff.ID, "w-2")
	if err != nil || got2 == nil || got2.ID != backingOff.ID {
		t.Fatalf("claim after backoff elapsed = (%v, %v), want the job (guard is a deadline, not a ban)", got2, err)
	}
}
