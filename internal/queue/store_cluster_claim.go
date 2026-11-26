package queue

import (
	"context"
	"fmt"
	"time"
)

// Cluster claim store methods: the shared-store half of the multi-node
// claim path. ClusterQueue.Claim stamps the owning NODE onto the row
// (claimed_by_node + timeout_at), the worker heartbeat extends it via
// HeartbeatClaim, and every node's sweeper calls ReclaimRemoteStale to
// release claims whose owning node stopped renewing (crash, partition).
//
// These columns already existed in the jobs schema (migration added
// claimed_by_node/timeout_at/last_heartbeat_at) but nothing wrote them on
// the claim path - the gap that made cross-node reclaim a no-op.

// ClaimNextClustered claims the next job for workerID and stamps the
// cluster ownership columns: claimed_by_node, timeout_at (now+timeout),
// last_heartbeat_at. Same selection semantics as ClaimNextForAgent
// (soft agent preference, retry backoff, capability matching).
func (s *Store) ClaimNextClustered(workerID, nodeID string, caps []string, agentID string, claimTimeout time.Duration) (*Job, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return nil, fmt.Errorf("cluster claim: begin: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	now := time.Now().UTC().Format(time.RFC3339)

	// Mirrors ClaimNextForAgent's soft-preference query (see that method
	// for the semantics rationale; kept textually identical on purpose -
	// divergent claim ordering between clustered and non-clustered paths
	// would be undebuggable).
	var query string
	var args []any
	if agentID != "" {
		query = `
			SELECT id, task_id, agent_id, type, priority, state, payload, required_caps,
			       max_retries, retry_count, interactive, claimed_by, result, error, created_at, updated_at, due_at, next_retry_at, claim_token
			FROM jobs
			WHERE state = 'pending'
			  AND (due_at IS NULL OR due_at <= ?)
			  AND (next_retry_at IS NULL OR next_retry_at <= ?)
			  AND (agent_id IS NULL OR agent_id = '' OR agent_id = ?)
			ORDER BY
			  interactive DESC,
			  CASE WHEN agent_id = ? THEN 0 ELSE 1 END,
			  priority DESC, created_at ASC, id ASC
			LIMIT 10`
		args = []any{now, now, agentID, agentID}
	} else {
		query = `
			SELECT id, task_id, agent_id, type, priority, state, payload, required_caps,
			       max_retries, retry_count, interactive, claimed_by, result, error, created_at, updated_at, due_at, next_retry_at, claim_token
			FROM jobs
			WHERE state = 'pending'
			  AND (due_at IS NULL OR due_at <= ?)
			  AND (next_retry_at IS NULL OR next_retry_at <= ?)
			ORDER BY interactive DESC, priority DESC, created_at ASC, id ASC
			LIMIT 10`
		args = []any{now, now}
	}

	rows, err := tx.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("cluster claim: query: %w", err)
	}
	defer rows.Close()
	var claimableJob *Job
	for rows.Next() {
		job, err := s.scanJobRows(rows)
		if err != nil {
			continue
		}
		if job.CanBeClaimedBy(caps) {
			claimableJob = job
			break
		}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("cluster claim: iterate: %w", err)
	}

	if claimableJob == nil {
		return nil, ErrNoJobAvailable
	}

	// Fresh attempt token per claim (audit H4) - same contract as the
	// non-clustered claim path.
	claimToken := newClaimToken()
	timeoutAt := time.Now().UTC().Add(claimTimeout).Format(time.RFC3339Nano)
	claimResult, err := tx.Exec(`
		UPDATE jobs SET state = 'claimed', claimed_by = ?, claim_token = ?,
		       claimed_by_node = ?, timeout_at = ?, last_heartbeat_at = ?, updated_at = ?
		WHERE id = ? AND state = 'pending'`,
		workerID, claimToken, nodeID, timeoutAt, now, now, claimableJob.ID)
	if err != nil {
		return nil, fmt.Errorf("cluster claim: update: %w", err)
	}
	affected, _ := claimResult.RowsAffected()
	if affected == 0 {
		return nil, ErrJobAlreadyClaimed
	}

	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("cluster claim: commit: %w", err)
	}

	claimableJob.State = StateClaimed
	claimableJob.ClaimedBy = workerID
	claimableJob.ClaimToken = claimToken
	claimableJob.ClaimedByNode = nodeID
	claimableJob.TimeoutAt = timeoutAt
	s.logger.Info("Job claimed (clustered)", "id", claimableJob.ID, "worker", workerID,
		"node", nodeID, "agent", claimableJob.AgentID)
	return claimableJob, nil
}

// HeartbeatClaim extends timeout_at + last_heartbeat_at on a job claimed by
// the given node. Returns false when the claim no longer belongs to that
// node (it was reclaimed while the worker was still running - the worker
// learns its attempt is superseded on the next heartbeat).
func (s *Store) HeartbeatClaim(ctx context.Context, nodeID, jobID string, claimTimeout time.Duration) (bool, error) {
	now := time.Now().UTC().Format(time.RFC3339)
	timeoutAt := time.Now().UTC().Add(claimTimeout).Format(time.RFC3339Nano)
	res, err := s.db.ExecContext(ctx, `
		UPDATE jobs SET timeout_at = ?, last_heartbeat_at = ?
		WHERE id = ? AND claimed_by_node = ? AND state IN ('claimed', 'processing')`,
		timeoutAt, now, jobID, nodeID)
	if err != nil {
		return false, fmt.Errorf("cluster heartbeat: %w", err)
	}
	n, _ := res.RowsAffected()
	return n > 0, nil
}

// parseLeaseStamp parses a claim-lease timestamp column written by ANY
// generation of this store: RFC3339Nano (heartbeat/claim writes, with or
// without sub-second digits — Go drops an all-zero nanosecond fraction
// on format) or RFC3339 (the older seconds-granularity writes). String
// comparison of the two vocabularies is WRONG — "12:00:05Z" sorts after
// "12:00:05.500Z" because 'Z'(0x5A) > '.'(0x2E) — so every lease check
// parses to time first (A3-12). ok=false means the column holds a value
// neither vocabulary can read (never treat it as expired: an
// unparseable lease must not silently free a live claim).
func parseLeaseStamp(s string) (time.Time, bool) {
	for _, layout := range []string{time.RFC3339Nano, time.RFC3339, "2006-01-02T15:04:05.999999999Z07:00", "2006-01-02 15:04:05.999999999-07:00"} {
		if t, err := time.Parse(layout, s); err == nil {
			return t, true
		}
	}
	return time.Time{}, false
}

// ReclaimRemoteStale resets to pending every job claimed by a node OTHER
// than localNodeID whose timeout_at has expired. Returns the reclaimed job
// IDs. The caller (ClusterQueue sweeper) records reclaim events + wakes
// waiters. Local claims are NOT touched here - ReclaimIfStale owns those
// (it has in-flight claim tokens and per-attempt bookkeeping).
//
// A3-12: the expiry test runs in Go on PARSED times, not as a SQL
// string compare. timeout_at is written RFC3339Nano while the sweep's
// now was RFC3339, and ISO-8601 string ordering only holds when both
// sides carry the SAME fraction syntax: a lease written this second
// with sub-second digits ("...:05.123Z") vs "...:05Z" compares lexically
// GREATER ('.' < 'Z'), so the sweeper would call it expired up to a
// full second early and steal a live claim; conversely a lease exactly
// on a whole second stored WITHOUT fraction is indistinguishable from
// an expired one. Parse-then-compare is vocabulary-independent.
func (s *Store) ReclaimRemoteStale(ctx context.Context, localNodeID string) ([]string, error) {
	now := time.Now().UTC()
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, claimed_by_node, timeout_at FROM jobs
		WHERE state IN ('claimed', 'processing')
		  AND claimed_by_node IS NOT NULL AND claimed_by_node != ''
		  AND claimed_by_node != ?
		  AND timeout_at IS NOT NULL`,
		localNodeID)
	if err != nil {
		return nil, fmt.Errorf("remote reclaim: query: %w", err)
	}
	defer rows.Close()
	var stale []struct {
		id, node string
	}
	for rows.Next() {
		var id, node, timeoutAt string
		if err := rows.Scan(&id, &node, &timeoutAt); err != nil {
			continue
		}
		expiry, ok := parseLeaseStamp(timeoutAt)
		if !ok {
			// Unparseable lease: fail closed (never reclaim a live
			// claim on a format we cannot read).
			s.logger.Warn("remote reclaim: unparseable timeout_at, skipping",
				"job_id", id, "timeout_at", timeoutAt)
			continue
		}
		if expiry.Before(now) {
			stale = append(stale, struct{ id, node string }{id, node})
		}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("remote reclaim: iterate: %w", err)
	}

	nowStr := now.Format(time.RFC3339)
	var reclaimed []string
	for _, st := range stale {
		res, err := s.db.ExecContext(ctx, `
			UPDATE jobs
			SET state = 'pending', claimed_by = NULL, claimed_by_node = NULL,
			    timeout_at = NULL, last_heartbeat_at = NULL, claim_token = NULL, updated_at = ?
			    WHERE id = ? AND claimed_by_node = ? AND state IN ('claimed', 'processing')`,
			nowStr, st.id, st.node)
		if err != nil {
			s.logger.Warn("remote reclaim: reset failed", "job_id", st.id, "error", err)
			continue
		}
		if n, _ := res.RowsAffected(); n > 0 {
			reclaimed = append(reclaimed, st.id)
			s.logger.Info("remote stale claim reclaimed",
				"job_id", st.id, "node", st.node)
		}
	}
	return reclaimed, nil
}
