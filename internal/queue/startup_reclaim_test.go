// startup_reclaim_test.go covers Store.ResetStaleClaimsAtStartup: the
// single-node crash-recovery sweep that resets jobs stuck in claimed/processing
// from a previous daemon process back to pending.
package queue

import (
	"context"
	"encoding/json"
	"testing"
	"time"
)

// seedStartupJob inserts a job in an explicit state with explicit claim,
// retry and update-time fields so startup-reclaim assertions are deterministic.
func seedStartupJob(t *testing.T, store *Store, id string, state JobState, updatedAt time.Time) *Job {
	t.Helper()

	job, err := NewJob(JobTypeOneOff, map[string]string{"prompt": "startup-reclaim-test"})
	if err != nil {
		t.Fatalf("NewJob failed: %v", err)
	}
	job.ID = id
	job.State = state
	job.RetryCount = 2
	job.Result = json.RawMessage(`"stale-result"`)
	job.Error = "stale-error"
	job.CreatedAt = updatedAt.Add(-time.Hour)
	job.UpdatedAt = updatedAt
	if state == StateClaimed || state == StateProcessing {
		job.ClaimedBy = "worker-from-dead-process"
	}

	if err := store.Insert(job); err != nil {
		t.Fatalf("Insert(%s) failed: %v", id, err)
	}
	return job
}

// setNextRetryAt writes next_retry_at directly (Insert does not cover the
// column), mirroring the raw-SQL seeding used elsewhere in this package.
func setNextRetryAt(t *testing.T, store *Store, jobID string, when time.Time) {
	t.Helper()
	if _, err := store.DB().Exec(`UPDATE jobs SET next_retry_at = ? WHERE id = ?`,
		when.UTC().Format(time.RFC3339), jobID); err != nil {
		t.Fatalf("failed to set next_retry_at on %s: %v", jobID, err)
	}
}

// TestResetStaleClaimsAtStartup is table-driven over the crash-recovery
// semantics: claimed/processing rows from before the boot cutoff are reset to
// pending with claim/result/error cleared and retry fields preserved;
// every other state — and any claim written by THIS process (updated after
// the cutoff) — is untouched.
func TestResetStaleClaimsAtStartup(t *testing.T) {
	// Simulated boot: cutoff is "this process started 1s before now".
	// Orphan rows were last touched an hour before boot; this-run rows an
	// hour after. RFC3339 stores second precision, so keep >=1s separation
	// from the cutoff.
	boot := time.Now().UTC().Truncate(time.Second)
	cutoff := boot.Add(-time.Second)
	orphanUpdatedAt := boot.Add(-time.Hour)
	freshUpdatedAt := boot.Add(time.Hour)
	// Retry backoff seeded on orphans; must survive the sweep untouched.
	seededNextRetry := boot.Add(30 * time.Second)

	tests := []struct {
		name          string
		state         JobState
		updatedAt     time.Time
		wantReset     bool
		wantState     JobState
		wantClaimedBy string
	}{
		{
			name:          "claimed orphan is reset to pending",
			state:         StateClaimed,
			updatedAt:     orphanUpdatedAt,
			wantReset:     true,
			wantState:     StatePending,
			wantClaimedBy: "",
		},
		{
			name:          "processing orphan is reset to pending",
			state:         StateProcessing,
			updatedAt:     orphanUpdatedAt,
			wantReset:     true,
			wantState:     StatePending,
			wantClaimedBy: "",
		},
		{
			name:          "fresh claim from this run is untouched",
			state:         StateClaimed,
			updatedAt:     freshUpdatedAt,
			wantReset:     false,
			wantState:     StateClaimed,
			wantClaimedBy: "worker-from-dead-process",
		},
		{
			name:          "pending job untouched",
			state:         StatePending,
			updatedAt:     orphanUpdatedAt,
			wantReset:     false,
			wantState:     StatePending,
			wantClaimedBy: "",
		},
		{
			name:          "failed job untouched",
			state:         StateFailed,
			updatedAt:     orphanUpdatedAt,
			wantReset:     false,
			wantState:     StateFailed,
			wantClaimedBy: "",
		},
		{
			name:          "completed job untouched",
			state:         StateCompleted,
			updatedAt:     orphanUpdatedAt,
			wantReset:     false,
			wantState:     StateCompleted,
			wantClaimedBy: "",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			store := newTestStore(t, "")
			job := seedStartupJob(t, store, "job-"+tc.name, tc.state, tc.updatedAt)
			if tc.state == StateClaimed || tc.state == StateProcessing {
				setNextRetryAt(t, store, job.ID, seededNextRetry)
			}

			got, err := store.ResetStaleClaimsAtStartup(context.Background(), cutoff)
			if err != nil {
				t.Fatalf("ResetStaleClaimsAtStartup failed: %v", err)
			}

			wantCount := 0
			if tc.wantReset {
				wantCount = 1
			}
			if got != wantCount {
				t.Errorf("reset count = %d, want %d", got, wantCount)
			}

			after, err := store.GetByID(job.ID)
			if err != nil {
				t.Fatalf("GetByID failed: %v", err)
			}

			if after.State != tc.wantState {
				t.Errorf("state = %q, want %q", after.State, tc.wantState)
			}
			if after.ClaimedBy != tc.wantClaimedBy {
				t.Errorf("claimed_by = %q, want %q", after.ClaimedBy, tc.wantClaimedBy)
			}

			if !tc.wantReset {
				return
			}

			// Reset preserves the retry budget and backoff (consistent with
			// ResetToPending: a crash is not a job failure).
			if after.RetryCount != job.RetryCount {
				t.Errorf("retry_count = %d, want preserved %d", after.RetryCount, job.RetryCount)
			}
			if after.NextRetryAt == nil {
				t.Fatalf("next_retry_at = nil, want preserved %v", seededNextRetry)
			}
			if !after.NextRetryAt.Equal(seededNextRetry.Truncate(time.Second)) {
				t.Errorf("next_retry_at = %v, want preserved %v",
					after.NextRetryAt, seededNextRetry.Truncate(time.Second))
			}
			// Claim artifacts cleared.
			if after.Result != nil {
				t.Errorf("result = %s, want cleared", after.Result)
			}
			if after.Error != "" {
				t.Errorf("error = %q, want cleared", after.Error)
			}
		})
	}
}

// TestResetStaleClaimsAtStartup_EmptyQueue verifies the sweep on an empty
// queue is a clean no-op.
func TestResetStaleClaimsAtStartup_EmptyQueue(t *testing.T) {
	store := newTestStore(t, "")

	got, err := store.ResetStaleClaimsAtStartup(context.Background(), time.Now().UTC())
	if err != nil {
		t.Fatalf("ResetStaleClaimsAtStartup failed: %v", err)
	}
	if got != 0 {
		t.Errorf("reset count = %d, want 0 on empty queue", got)
	}
}

// TestResetStaleClaimsAtStartup_ThenClaimable verifies the recovery outcome
// end-to-end: after the sweep, an orphaned job is claimable again by a
// worker, proving it re-executes after the crash.
func TestResetStaleClaimsAtStartup_ThenClaimable(t *testing.T) {
	store := newTestStore(t, "")

	boot := time.Now().UTC().Truncate(time.Second)
	orphan := seedStartupJob(t, store, "orphan-reclaimable", StateClaimed, boot.Add(-time.Hour))

	cutoff := boot.Add(-time.Second)
	got, err := store.ResetStaleClaimsAtStartup(context.Background(), cutoff)
	if err != nil {
		t.Fatalf("ResetStaleClaimsAtStartup failed: %v", err)
	}
	if got != 1 {
		t.Fatalf("reset count = %d, want 1", got)
	}

	reclaimed, err := store.ClaimNextForAgent("new-worker", nil, "")
	if err != nil {
		t.Fatalf("ClaimNextForAgent after sweep failed: %v", err)
	}
	if reclaimed.ID != orphan.ID {
		t.Errorf("claimed job %s, want recovered orphan %s", reclaimed.ID, orphan.ID)
	}
	if reclaimed.State != StateClaimed {
		t.Errorf("reclaimed job state = %q, want %q", reclaimed.State, StateClaimed)
	}
	if reclaimed.ClaimedBy != "new-worker" {
		t.Errorf("reclaimed job claimed_by = %q, want new-worker", reclaimed.ClaimedBy)
	}
	// Retry budget still rides along after the crash + reclaim.
	if reclaimed.RetryCount != 2 {
		t.Errorf("reclaimed job retry_count = %d, want preserved 2", reclaimed.RetryCount)
	}
}

// TestPersistentQueue_ResetStaleClaimsAtStartup covers the PersistentQueue
// wrapper: delegation on an open queue and the closed-queue guard.
func TestPersistentQueue_ResetStaleClaimsAtStartup(t *testing.T) {
	q := newTestQueue(t)
	ctx := context.Background()

	boot := time.Now().UTC().Truncate(time.Second)
	seedStartupJob(t, q.store, "wrapped-orphan", StateProcessing, boot.Add(-time.Hour))

	got, err := q.ResetStaleClaimsAtStartup(ctx, boot.Add(-time.Second))
	if err != nil {
		t.Fatalf("ResetStaleClaimsAtStartup failed: %v", err)
	}
	if got != 1 {
		t.Errorf("reset count = %d, want 1", got)
	}

	if err := q.Close(); err != nil {
		t.Fatalf("Close failed: %v", err)
	}
	if _, err := q.ResetStaleClaimsAtStartup(ctx, boot.Add(-time.Second)); err == nil {
		t.Error("expected error on closed queue, got nil")
	}
}
