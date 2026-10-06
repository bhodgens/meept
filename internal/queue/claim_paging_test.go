package queue

// B1: claim slow-path 50-row window starvation.
//
// The Claim slow path (installed whenever a task-cancel callback is
// present — which production always does) used to scan only the FIRST 50
// pending rows and filter in memory. With 50+ head-of-line parked jobs
// (quota-deferred jobs carry a future next_retry_at), a claimable job at
// row 51+ was never seen and every worker got ErrNoJobAvailable forever.
//
// Coverage:
//   - claimable job behind 60 parked jobs must be found (keyset paging)
//   - 500+ all-parked jobs → bounded scan → ErrNoJobAvailable (no livelock)
//   - small table → same job a plain scan picks (interactive DESC order)

import (
	"context"
	"errors"
	"testing"
	"time"
)

// parkPending inserts a pending job and back-fills its next_retry_at with a
// future timestamp (Insert does not persist NextRetryAt; Requeue is the
// production writer of that column, but a direct UPDATE keeps the fixture
// focused on claim paging rather than the requeue path).
func parkPending(t *testing.T, store *Store, job *Job, retryAt time.Time) {
	t.Helper()
	if err := store.Insert(job); err != nil {
		t.Fatalf("Insert failed for job %s: %v", job.ID, err)
	}
	if _, err := store.DB().Exec(
		`UPDATE jobs SET next_retry_at = ? WHERE id = ?`,
		retryAt.UTC().Format(time.RFC3339), job.ID,
	); err != nil {
		t.Fatalf("failed to park job %s: %v", job.ID, err)
	}
}

// TestClaimSlowPath_PagesBeyondHeadOfLineParkedJobs: 60 pending jobs parked
// on future next_retry_at plus 1 claimable job that sorts at the very tail.
// Claim must page past the parked head-of-line block and return the tail job.
func TestClaimSlowPath_PagesBeyondHeadOfLineParkedJobs(t *testing.T) {
	q := newTestQueue(t)
	// Force the slow path (production always installs a cancel callback).
	q.SetTaskCancelledCallback(func(string) (bool, string) { return false, "" })

	store := q.Store()
	base := time.Now().UTC().Add(-time.Hour)
	parkedRetryAt := base.Add(2 * time.Hour)

	// 60 parked jobs, staggered created_at so ordering is deterministic.
	for i := range 60 {
		job := mustNewJob(t, JobTypeOneOff, map[string]string{"prompt": "parked"})
		job.CreatedAt = base.Add(time.Duration(i) * time.Second)
		job.UpdatedAt = job.CreatedAt
		parkPending(t, store, job, parkedRetryAt)
	}

	// The claimable job sorts LAST (created_at farthest in the future).
	claimable := mustNewJob(t, JobTypeOneOff, map[string]string{"prompt": "claimable"})
	claimable.CreatedAt = base.Add(2 * time.Minute)
	claimable.UpdatedAt = claimable.CreatedAt
	if err := store.Insert(claimable); err != nil {
		t.Fatalf("Insert failed for claimable job: %v", err)
	}

	got, err := q.Claim(context.Background(), "worker-1", nil, "")
	if err != nil {
		t.Fatalf("Claim failed: %v (claimable job starved behind parked head-of-line block)", err)
	}
	if got == nil {
		t.Fatal("Claim returned nil job with no error")
	}
	if got.ID != claimable.ID {
		t.Fatalf("claimed job = %s, want tail job %s", got.ID, claimable.ID)
	}
}

// TestClaimSlowPath_BoundedScan: 520 all-parked pending jobs and no
// claimable work at all. Claim must give up with ErrNoJobAvailable after a
// bounded scan instead of paging forever.
func TestClaimSlowPath_BoundedScan(t *testing.T) {
	q := newTestQueue(t)
	q.SetTaskCancelledCallback(func(string) (bool, string) { return false, "" })

	store := q.Store()
	base := time.Now().UTC().Add(-time.Hour)
	parkedRetryAt := base.Add(2 * time.Hour)

	for i := range 520 {
		job := mustNewJob(t, JobTypeOneOff, map[string]string{"prompt": "parked"})
		job.CreatedAt = base.Add(time.Duration(i) * time.Second)
		job.UpdatedAt = job.CreatedAt
		parkPending(t, store, job, parkedRetryAt)
	}

	start := time.Now()
	_, err := q.Claim(context.Background(), "worker-1", nil, "")
	elapsed := time.Since(start)
	if !errors.Is(err, ErrNoJobAvailable) {
		t.Fatalf("Claim error = %v, want ErrNoJobAvailable", err)
	}
	if elapsed > 30*time.Second {
		t.Fatalf("Claim took %v, want a bounded scan well under 30s", elapsed)
	}
}

// TestClaimSlowPath_SinglePageUnchanged: with a small table the slow path
// must still pick the same job a plain in-order scan would: the interactive
// job sorts first (interactive DESC) and is claimable.
func TestClaimSlowPath_SinglePageUnchanged(t *testing.T) {
	q := newTestQueue(t)
	q.SetTaskCancelledCallback(func(string) (bool, string) { return false, "" })

	store := q.Store()
	base := time.Now().UTC().Add(-time.Hour)

	normal := mustNewJob(t, JobTypeOneOff, map[string]string{"prompt": "normal"})
	normal.CreatedAt = base.Add(1 * time.Second)
	normal.UpdatedAt = normal.CreatedAt
	if err := store.Insert(normal); err != nil {
		t.Fatalf("Insert failed for normal job: %v", err)
	}

	interactive := mustNewJob(t, JobTypeOneOff, map[string]string{"prompt": "interactive"})
	interactive.CreatedAt = base.Add(2 * time.Second)
	interactive.UpdatedAt = interactive.CreatedAt
	interactive.WithInteractive(true)
	if err := store.Insert(interactive); err != nil {
		t.Fatalf("Insert failed for interactive job: %v", err)
	}

	got, err := q.Claim(context.Background(), "worker-1", nil, "")
	if err != nil {
		t.Fatalf("Claim failed: %v", err)
	}
	if got.ID != interactive.ID {
		t.Fatalf("claimed job = %s, want interactive job %s (interactive DESC order)", got.ID, interactive.ID)
	}
}
