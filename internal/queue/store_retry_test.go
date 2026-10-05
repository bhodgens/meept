package queue

// Tests for Store.Retry exponential backoff capping (B4).
//
// The backoff is 2s * 2^retry_count with a 30s cap. The cap exists so
// rate-limit failures that miss the quota-class regex (and therefore take
// this generic retry path instead of the quota-deferral path) do not
// re-hit the provider every few seconds until max_retries.

import (
	"testing"
	"time"
)

// seedRetryCount forces retry_count on an already-claimed job so the next
// Retry call computes the backoff for a high attempt number.
func seedRetryCount(t *testing.T, store *Store, jobID string, count int) {
	t.Helper()
	if _, err := store.db.Exec(`UPDATE jobs SET retry_count = ? WHERE id = ?`, count, jobID); err != nil {
		t.Fatalf("failed to seed retry_count: %v", err)
	}
}

// TestStoreRetry_BackoffCapAt30s: a job with a high retry_count gets
// next_retry_at ~now+30s (the cap), NOT ~now+8s (the old cap).
func TestStoreRetry_BackoffCapAt30s(t *testing.T) {
	store := newTestStore(t, "")

	job := mustNewJob(t, JobTypeOneOff, map[string]string{"prompt": "turn"})
	job.WithMaxRetries(10)
	if err := store.Insert(job); err != nil {
		t.Fatalf("Insert failed: %v", err)
	}
	if _, err := store.ClaimNextForAgent("worker-1", nil, ""); err != nil {
		t.Fatalf("Claim failed: %v", err)
	}
	// retry_count >= 4 would exceed even the old 8s cap, so pin well past it.
	seedRetryCount(t, store, job.ID, 6)

	before := time.Now().UTC()
	if err := store.Retry(job.ID); err != nil {
		t.Fatalf("Retry failed: %v", err)
	}

	got, err := store.GetByID(job.ID)
	if err != nil {
		t.Fatalf("GetByID failed: %v", err)
	}
	if got.NextRetryAt == nil {
		t.Fatal("next_retry_at not set on retried job")
	}
	d := got.NextRetryAt.Sub(before)
	// ±2s tolerance for clock skew / test scheduling; old-cap behavior (8s)
	// is far outside this window.
	if d < 28*time.Second || d > 32*time.Second {
		t.Fatalf("next_retry_at = +%v, want ~30s (cap); old cap was 8s", d)
	}
}

// TestStoreRetry_BackoffLowRetryCounts: below the cap the exponential
// schedule is unchanged (2s, 4s).
func TestStoreRetry_BackoffLowRetryCounts(t *testing.T) {
	store := newTestStore(t, "")

	job := mustNewJob(t, JobTypeOneOff, map[string]string{"prompt": "turn"})
	job.WithMaxRetries(10)
	if err := store.Insert(job); err != nil {
		t.Fatalf("Insert failed: %v", err)
	}
	if _, err := store.ClaimNextForAgent("worker-1", nil, ""); err != nil {
		t.Fatalf("Claim failed: %v", err)
	}
	seedRetryCount(t, store, job.ID, 0)

	before := time.Now().UTC()
	if err := store.Retry(job.ID); err != nil {
		t.Fatalf("Retry failed: %v", err)
	}

	got, err := store.GetByID(job.ID)
	if err != nil {
		t.Fatalf("GetByID failed: %v", err)
	}
	if got.NextRetryAt == nil {
		t.Fatal("next_retry_at not set on retried job")
	}
	d := got.NextRetryAt.Sub(before)
	if d < time.Second || d > 3*time.Second {
		t.Fatalf("next_retry_at = +%v, want ~2s (base backoff)", d)
	}
}
