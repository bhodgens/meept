package queue

// Gate tests for the 20261109 state-machine remediation, 01-queue leaf.
// These pin the TARGET behavior of audit findings M-1..M-4, F6..F9 before the
// implementation leaves dispatch. Several are expected RED:
//   - Store.MarkProcessing(jobID, claimToken)      (contract C-Q1) — method
//     does not exist yet; the compile failure IS the RED evidence.
//   - Store.SweepAbandonedTerminalStates(ctx, d)   (contract C-Q2) — same.
//   - Requeue/Retry cluster-column parity, dead-letter interactive restore,
//     operator Retry budget bound, jobs.state CHECK, duplicate-safe moveToDead
//     — compile today and fail on behavior.
// See docs/plans/20261109-statemachine-remediation/01-queue/00T-queue-tests.md.

import (
	"context"
	"database/sql"
	"testing"
	"time"
)

// gateOldTimestamp is an RFC3339 UTC stamp 2h in the past — older than any
// sweep cutoff these tests use (1h).
func gateOldTimestamp() string {
	return time.Now().UTC().Add(-2 * time.Hour).Format(time.RFC3339)
}

// mustExec fails the test if the statement errors.
func mustExec(t *testing.T, store *Store, query string, args ...any) {
	t.Helper()
	if _, err := store.db.Exec(query, args...); err != nil {
		t.Fatalf("exec %q: %v", query, err)
	}
}

// gateRowState reads jobs.state for a job id (direct SQL so the test does not
// depend on scan shapes).
func gateRowState(t *testing.T, store *Store, jobID string) string {
	t.Helper()
	var state string
	if err := store.db.QueryRow(`SELECT state FROM jobs WHERE id = ?`, jobID).Scan(&state); err != nil {
		t.Fatalf("query state for %s: %v", jobID, err)
	}
	return state
}

// gateClusterRow reads the claim + cluster bookkeeping columns for a job.
// Any non-null value after a pending reset is a leak (finding M-4 / leaf 02).
type gateClusterRow struct {
	claimedBy       sql.NullString
	claimToken      sql.NullString
	claimedByNode   sql.NullString
	timeoutAt       sql.NullString
	lastHeartbeatAt sql.NullString
}

func gateReadClusterRow(t *testing.T, store *Store, jobID string) gateClusterRow {
	t.Helper()
	var r gateClusterRow
	err := store.db.QueryRow(`
		SELECT claimed_by, claim_token, claimed_by_node, timeout_at, last_heartbeat_at
		FROM jobs WHERE id = ?`, jobID,
	).Scan(&r.claimedBy, &r.claimToken, &r.claimedByNode, &r.timeoutAt, &r.lastHeartbeatAt)
	if err != nil {
		t.Fatalf("read cluster columns for %s: %v", jobID, err)
	}
	return r
}

func (r gateClusterRow) leaks() []string {
	var out []string
	if r.claimedBy.Valid && r.claimedBy.String != "" {
		out = append(out, "claimed_by="+r.claimedBy.String)
	}
	if r.claimToken.Valid && r.claimToken.String != "" {
		out = append(out, "claim_token="+r.claimToken.String)
	}
	if r.claimedByNode.Valid && r.claimedByNode.String != "" {
		out = append(out, "claimed_by_node="+r.claimedByNode.String)
	}
	if r.timeoutAt.Valid && r.timeoutAt.String != "" {
		out = append(out, "timeout_at="+r.timeoutAt.String)
	}
	if r.lastHeartbeatAt.Valid && r.lastHeartbeatAt.String != "" {
		out = append(out, "last_heartbeat_at="+r.lastHeartbeatAt.String)
	}
	return out
}

// claimClustered inserts and cluster-claims one job, returning the store,
// the job, and its live claim token.
func claimClustered(t *testing.T, node string) (*Store, *Job) {
	t.Helper()
	store := newTestStore(t, "")
	job := mustNewJob(t, JobTypeOneOff, map[string]string{"prompt": "gate"})
	if err := store.Insert(job); err != nil {
		t.Fatalf("Insert failed: %v", err)
	}
	claimed, err := store.ClaimNextClustered("worker-"+node, node, nil, "", time.Minute)
	if err != nil {
		t.Fatalf("ClaimNextClustered failed: %v", err)
	}
	return store, claimed
}

// TestMarkProcessingRequiresToken pins contract C-Q1:
//
//	Store.MarkProcessing(jobID, claimToken) error
//	UPDATE jobs SET state='processing' WHERE id=? AND claim_token=? AND state IN ('claimed','processing')
//
// A wrong-or-empty token must be refused (error or no-op) with the row still
// 'claimed'; the owning token flips it to 'processing'.
//
// RED today: Store has NO MarkProcessing method (only UpdateState, which is
// token-less), so this file fails to compile with
// "store.MarkProcessing undefined" — the missing-implementation RED evidence
// for finding M-1.
func TestMarkProcessingRequiresToken(t *testing.T) {
	store, claimed := claimClustered(t, "node-mp")

	// Foreign token must not be able to move the attempt forward.
	if err := store.MarkProcessing(claimed.ID, "not-my-token"); err == nil {
		t.Errorf("MarkProcessing with wrong token returned nil error; expected refusal")
	}
	if state := gateRowState(t, store, claimed.ID); state != string(StateClaimed) {
		t.Errorf("after wrong-token MarkProcessing: state=%s, want claimed", state)
	}

	// Empty token likewise refused.
	if err := store.MarkProcessing(claimed.ID, ""); err == nil {
		t.Errorf("MarkProcessing with empty token returned nil error; expected refusal")
	}
	if state := gateRowState(t, store, claimed.ID); state != string(StateClaimed) {
		t.Errorf("after empty-token MarkProcessing: state=%s, want claimed", state)
	}

	// The live claim token succeeds.
	if err := store.MarkProcessing(claimed.ID, claimed.ClaimToken); err != nil {
		t.Errorf("MarkProcessing with live token: %v, want nil", err)
	}
	if state := gateRowState(t, store, claimed.ID); state != string(StateProcessing) {
		t.Errorf("after live-token MarkProcessing: state=%s, want processing", state)
	}
}

// TestRequeueClearsClusterColumns pins finding M-4 / leaf 02: the Requeue
// reset must clear the FULL claim+cluster column set (claimed_by,
// claim_token, claimed_by_node, timeout_at, last_heartbeat_at), not just the
// legacy subset. RED today: Requeue leaves timeout_at, last_heartbeat_at and
// claimed_by_node set.
func TestRequeueClearsClusterColumns(t *testing.T) {
	store, claimed := claimClustered(t, "node-rq")

	if err := store.Requeue(claimed.ID, time.Now().UTC().Add(time.Minute)); err != nil {
		t.Fatalf("Requeue failed: %v", err)
	}

	if state := gateRowState(t, store, claimed.ID); state != string(StatePending) {
		t.Fatalf("after Requeue: state=%s, want pending", state)
	}
	if leaks := gateReadClusterRow(t, store, claimed.ID).leaks(); len(leaks) > 0 {
		t.Errorf("Requeue left cluster columns set: %v", leaks)
	}
}

// TestRetryClearsClusterColumns is the same parity requirement on the
// operator/automatic Retry path. RED today: Retry leaves timeout_at,
// last_heartbeat_at and claimed_by_node set.
func TestRetryClearsClusterColumns(t *testing.T) {
	store, claimed := claimClustered(t, "node-rt")

	if err := store.Retry(claimed.ID); err != nil {
		t.Fatalf("Retry failed: %v", err)
	}

	if state := gateRowState(t, store, claimed.ID); state != string(StatePending) {
		t.Fatalf("after Retry: state=%s, want pending", state)
	}
	if leaks := gateReadClusterRow(t, store, claimed.ID).leaks(); len(leaks) > 0 {
		t.Errorf("Retry left cluster columns set: %v", leaks)
	}
}

// TestDeadLetterRecoveryPreservesInteractive pins leaf 04/T4: a job marked
// interactive (D11 claim ordering) that dies and is recovered must come back
// with interactive=true. RED today: the dead_letter round-trip drops the
// column, so the recovered row is interactive=false.
func TestDeadLetterRecoveryPreservesInteractive(t *testing.T) {
	store := newTestStore(t, "")
	job := mustNewJob(t, JobTypeOneOff, map[string]string{"prompt": "interactive gate"})
	job.WithInteractive(true).WithMaxRetries(0) // first Fail -> dead letter
	if err := store.Insert(job); err != nil {
		t.Fatalf("Insert failed: %v", err)
	}
	if err := store.Fail(job.ID, "exhausted on first failure"); err != nil {
		t.Fatalf("Fail failed: %v", err)
	}

	recovered, err := store.RecoverFromDeadLetter(job.ID)
	if err != nil {
		t.Fatalf("RecoverFromDeadLetter failed: %v", err)
	}
	if !recovered.Interactive {
		t.Error("recovered job lost interactive flag; want interactive=true")
	}
	if recovered.State != StatePending {
		t.Errorf("recovered job state=%s, want pending", recovered.State)
	}
}

// TestSweepAbandonedTerminalStates pins contract C-Q2 (findings M-2/M-3):
//
//	Store.SweepAbandonedTerminalStates(ctx context.Context, olderThan time.Duration) (int, error)
//
// It re-applies the dead-letter move to rows the Fail path stranded: a
// budget-exhausted 'failed' row and a 'dead' row still sitting in jobs (its
// moveToDead insert failed). Rows with retry budget remaining are untouched.
//
// RED today: the method does not exist, so this file fails to compile with
// "store.SweepAbandonedTerminalStates undefined" — the missing-implementation
// evidence for the sweeper leaf (03).
func TestSweepAbandonedTerminalStates(t *testing.T) {
	store := newTestStore(t, "")
	old := gateOldTimestamp()

	// Fixture 1: failed, budget EXHAUSTED (retry_count >= max_retries), stale.
	exhausted := mustNewJob(t, JobTypeOneOff, map[string]string{"prompt": "exhausted"})
	exhausted.WithMaxRetries(2)
	if err := store.Insert(exhausted); err != nil {
		t.Fatalf("Insert exhausted failed: %v", err)
	}
	mustExec(t, store,
		`UPDATE jobs SET state='failed', retry_count=2, updated_at=? WHERE id=?`,
		old, exhausted.ID)

	// Fixture 2: failed, budget REMAINING, stale — must be left alone.
	retryable := mustNewJob(t, JobTypeOneOff, map[string]string{"prompt": "retryable"})
	retryable.WithMaxRetries(3)
	if err := store.Insert(retryable); err != nil {
		t.Fatalf("Insert retryable failed: %v", err)
	}
	mustExec(t, store,
		`UPDATE jobs SET state='failed', retry_count=1, updated_at=? WHERE id=?`,
		old, retryable.ID)

	// Fixture 3: 'dead' stranded in jobs (moveToDead never completed).
	stranded := mustNewJob(t, JobTypeOneOff, map[string]string{"prompt": "stranded"})
	if err := store.Insert(stranded); err != nil {
		t.Fatalf("Insert stranded failed: %v", err)
	}
	mustExec(t, store,
		`UPDATE jobs SET state='dead', updated_at=? WHERE id=?`,
		old, stranded.ID)

	// A fresh failed-but-exhausted row newer than the cutoff must also survive
	// the sweep window (not yet abandoned).
	fresh := mustNewJob(t, JobTypeOneOff, map[string]string{"prompt": "fresh"})
	fresh.WithMaxRetries(1)
	if err := store.Insert(fresh); err != nil {
		t.Fatalf("Insert fresh failed: %v", err)
	}
	mustExec(t, store,
		`UPDATE jobs SET state='failed', retry_count=1 WHERE id=?`, fresh.ID)

	n, err := store.SweepAbandonedTerminalStates(context.Background(), time.Hour)
	if err != nil {
		t.Fatalf("SweepAbandonedTerminalStates error: %v", err)
	}
	if n != 2 {
		t.Errorf("sweeper moved %d rows, want 2 (exhausted-failed + stranded-dead)", n)
	}

	for _, id := range []string{exhausted.ID, stranded.ID} {
		var count int
		if err := store.db.QueryRow(`SELECT count(*) FROM dead_letter WHERE id=?`, id).Scan(&count); err != nil {
			t.Fatalf("query dead_letter for %s: %v", id, err)
		}
		if count != 1 {
			t.Errorf("id %s: dead_letter rows=%d, want 1 (row stranded in jobs)", id, count)
		}
	}

	// Retryable failed row and the fresh row must remain in jobs, untouched.
	for _, tc := range []struct {
		id   string
		want JobState
	}{
		{retryable.ID, StateFailed},
		{fresh.ID, StateFailed},
	} {
		if got := gateRowState(t, store, tc.id); got != string(tc.want) {
			t.Errorf("job %s: state=%s, want %s (must not be swept)", tc.id, got, tc.want)
		}
	}
}

// TestMoveToDeadDuplicateSafe pins finding M-2's move-side half: transitioning
// a job to dead letter when a dead_letter row for the SAME id already exists
// (e.g. a previous sweep/Fail crashed between insert and delete, leaving the
// row in jobs AND a shadow in dead_letter) must be duplicate-safe: no error,
// and the jobs row removed. RED today: moveToDead uses a bare INSERT
// ..SELECT, so the PRIMARY KEY collision returns an error and the jobs row
// stays stranded.
func TestMoveToDeadDuplicateSafe(t *testing.T) {
	store := newTestStore(t, "")
	job := mustNewJob(t, JobTypeOneOff, map[string]string{"prompt": "dup dead"})
	if err := store.Insert(job); err != nil {
		t.Fatalf("Insert failed: %v", err)
	}

	// Pre-seed a dead_letter shadow row with the same id (the crash-residue
	// shape M-2 describes).
	now := time.Now().UTC().Format(time.RFC3339)
	mustExec(t, store, `
		INSERT INTO dead_letter (id, task_id, agent_id, type, priority, payload,
			required_caps, max_retries, retry_count, error, created_at, died_at, due_at)
		VALUES (?, NULL, NULL, ?, 2, ?, '[]', 3, 3, 'earlier death', ?, ?, NULL)`,
		job.ID, string(job.Type), string(job.Payload), now, now)

	// The dead-letter move (FailAttempt -> moveToDead) must tolerate the
	// existing row. Reach the same seam the Fail path uses.
	if err := store.moveToDead(job.ID); err != nil {
		t.Errorf("moveToDead with pre-existing dead_letter row: %v, want nil (duplicate-safe upsert)", err)
	}

	var jobsCount int
	if err := store.db.QueryRow(`SELECT count(*) FROM jobs WHERE id=?`, job.ID).Scan(&jobsCount); err != nil {
		t.Fatalf("count jobs row: %v", err)
	}
	if jobsCount != 0 {
		t.Errorf("jobs row still present after moveToDead; the row is stranded")
	}

	var dlCount int
	if err := store.db.QueryRow(`SELECT count(*) FROM dead_letter WHERE id=?`, job.ID).Scan(&dlCount); err != nil {
		t.Fatalf("count dead_letter row: %v", err)
	}
	if dlCount != 1 {
		t.Errorf("dead_letter rows=%d, want exactly 1 for id %s", dlCount, job.ID)
	}
}

// TestRetryOperatorRespectsMaxRetries pins finding F9: the operator/bus
// Retry path must refuse a job whose retry budget is already exhausted
// (retry_count >= max_retries) instead of resetting it to pending forever.
// RED today: Store.Retry has no budget check — it succeeds (nil error) and
// flips the row to pending.
func TestRetryOperatorRespectsMaxRetries(t *testing.T) {
	store := newTestStore(t, "")
	job := mustNewJob(t, JobTypeOneOff, map[string]string{"prompt": "budget out"})
	job.WithMaxRetries(2)
	if err := store.Insert(job); err != nil {
		t.Fatalf("Insert failed: %v", err)
	}
	mustExec(t, store,
		`UPDATE jobs SET state='failed', retry_count=2 WHERE id=?`, job.ID)

	err := store.Retry(job.ID)
	if err == nil {
		t.Error("Retry past exhausted budget returned nil error; want refusal (retry budget exhausted)")
	}

	// Whatever the refusal's side effects, the exhausted row must not be
	// resurrected to pending.
	if state := gateRowState(t, store, job.ID); state == string(StatePending) {
		t.Errorf("exhausted job was reset to pending by Retry; state=%s", state)
	}
}

// TestJobsStateCheckRejectsGarbage pins finding F7 / leaf 04: the jobs table
// must constrain state to the enum. A direct INSERT (or UPDATE) with a
// garbage state must be refused by a CHECK constraint. RED today: the schema
// has no CHECK, so the insert succeeds and the test fails with "expected
// constraint error, got none".
func TestJobsStateCheckRejectsGarbage(t *testing.T) {
	store := newTestStore(t, "")
	now := time.Now().UTC().Format(time.RFC3339)

	_, err := store.db.Exec(`
		INSERT INTO jobs (id, type, priority, state, payload, required_caps,
			max_retries, retry_count, created_at, updated_at)
		VALUES (?, ?, 2, ?, ?, '[]', 3, 0, ?, ?)`,
		"garbage-row-1", string(JobTypeOneOff), "garbage_bogus", `{"prompt":"x"}`, now, now)
	if err == nil {
		t.Fatal("expected constraint error, got none: jobs.state CHECK rejected nothing for state='garbage_bogus'")
	}

	// The valid enum values must still be insertable (CHECK is the whitelist,
	// not a block on the table).
	_, err = store.db.Exec(`
		INSERT INTO jobs (id, type, priority, state, payload, required_caps,
			max_retries, retry_count, created_at, updated_at)
		VALUES (?, ?, 2, ?, ?, '[]', 3, 0, ?, ?)`,
		"valid-row-1", string(JobTypeOneOff), string(StatePending), `{"prompt":"y"}`, now, now)
	if err != nil {
		t.Errorf("insert of valid state 'pending' failed: %v; want CHECK to allow enum values", err)
	}
}
