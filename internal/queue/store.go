package queue

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"crypto/ed25519"

	_ "modernc.org/sqlite" //nolint:revive // blank import for side effects
)

// ErrNoJobAvailable is returned when no claimable job is found.
var ErrNoJobAvailable = errors.New("no job available")

// ErrJobAlreadyClaimed is returned when a job cannot be claimed because it is
// not found or already claimed by another worker.
var ErrJobAlreadyClaimed = errors.New("job not found or already claimed")

// Store provides SQLite persistence for jobs.
type Store struct {
	db     *sql.DB
	logger *slog.Logger
}

// NewStore creates a new SQLite-backed job store.
func NewStore(dbPath string, logger *slog.Logger) (*Store, error) {
	if logger == nil {
		logger = slog.Default()
	}

	db, err := sql.Open("sqlite", dbPath+"?_journal_mode=WAL&_busy_timeout=5000")
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	store := &Store{
		db:     db,
		logger: logger,
	}

	if err := store.migrate(); err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to migrate database: %w", err)
	}

	logger.Info("Job queue store initialized", "path", dbPath)
	return store, nil
}

// baseSchema creates the core job queue tables.
const baseSchema = `
CREATE TABLE IF NOT EXISTS jobs (
	id            TEXT PRIMARY KEY,
	task_id       TEXT,
	agent_id      TEXT,
	type          TEXT NOT NULL,
	priority      INTEGER DEFAULT 2,
	state         TEXT DEFAULT 'pending',
	payload       TEXT NOT NULL,
	required_caps TEXT DEFAULT '[]',
	max_retries   INTEGER DEFAULT 3,
	retry_count   INTEGER DEFAULT 0,
	claimed_by    TEXT,
	result        TEXT,
	error         TEXT,
	created_at    TEXT NOT NULL,
	updated_at    TEXT NOT NULL,
	due_at        TEXT
);

CREATE INDEX IF NOT EXISTS idx_jobs_state_priority ON jobs(state, priority DESC, created_at);
CREATE INDEX IF NOT EXISTS idx_jobs_task_id ON jobs(task_id);
CREATE INDEX IF NOT EXISTS idx_jobs_claimed_by ON jobs(claimed_by);
CREATE INDEX IF NOT EXISTS idx_jobs_agent_id ON jobs(agent_id);

CREATE TABLE IF NOT EXISTS dead_letter (
	id            TEXT PRIMARY KEY,
	task_id       TEXT,
	agent_id      TEXT,
	type          TEXT NOT NULL,
	priority      INTEGER,
	payload       TEXT NOT NULL,
	required_caps TEXT,
	max_retries   INTEGER,
	retry_count   INTEGER,
	error         TEXT,
	created_at    TEXT NOT NULL,
	died_at       TEXT NOT NULL,
	due_at        TEXT
);

-- queued_followups table for persisted follow-up messages (Phase 4).
CREATE TABLE IF NOT EXISTS queued_followups (
	conversation_id TEXT NOT NULL,
	message_id      TEXT PRIMARY KEY,
	content         TEXT NOT NULL,
	queue_type      TEXT NOT NULL,
	source          TEXT NOT NULL,
	created_at      TEXT DEFAULT (datetime('now')),
	updated_at      TEXT DEFAULT (datetime('now'))
);

CREATE INDEX IF NOT EXISTS idx_queued_followups_conversation
	ON queued_followups(conversation_id);
`

// clusterSchema extends the base schema with cluster support.
// ALTER TABLE statements are wrapped to tolerate duplicate-column errors
// so that applying this schema multiple times is safe.
const clusterSchema = `
-- Add cluster fields to jobs table
ALTER TABLE jobs ADD COLUMN cluster_task_id TEXT;
ALTER TABLE jobs ADD COLUMN managing_node TEXT;
ALTER TABLE jobs ADD COLUMN claimed_by_node TEXT;
ALTER TABLE jobs ADD COLUMN timeout_at TIMESTAMP;
ALTER TABLE jobs ADD COLUMN last_heartbeat_at TIMESTAMP;
ALTER TABLE jobs ADD COLUMN payload_full BLOB;
ALTER TABLE jobs ADD COLUMN is_replica INTEGER DEFAULT 0;

-- Create cluster_events table for gossip replication
CREATE TABLE IF NOT EXISTS cluster_events (
	event_id TEXT PRIMARY KEY,
	node_id TEXT NOT NULL,
	event_type TEXT NOT NULL,
	timestamp INTEGER NOT NULL,
	vector_clock TEXT NOT NULL,
	payload BLOB NOT NULL,
	signature BLOB NOT NULL,
	received_at INTEGER NOT NULL,
	synced INTEGER DEFAULT 0
);

CREATE INDEX IF NOT EXISTS idx_events_type ON cluster_events(event_type);
CREATE INDEX IF NOT EXISTS idx_events_node ON cluster_events(node_id);
CREATE INDEX IF NOT EXISTS idx_events_time ON cluster_events(timestamp);

-- Create cluster_members cache table (populated from git sync)
CREATE TABLE IF NOT EXISTS cluster_members (
	node_id TEXT PRIMARY KEY,
	node_name TEXT,
	wireguard_pub TEXT NOT NULL,
	signing_pub BLOB NOT NULL,
	endpoint TEXT NOT NULL,
	capabilities TEXT,
	cluster_ip TEXT,
	joined_at INTEGER NOT NULL,
	last_heartbeat INTEGER NOT NULL,
	status TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_members_status ON cluster_members(status);
`

// clusterColumnNames lists the cluster-specific ALTER TABLE columns
// in the same order as they appear in clusterSchema, for idempotency checks.
var clusterColumnNames = []string{
	"cluster_task_id", "managing_node", "claimed_by_node",
	"timeout_at", "last_heartbeat_at", "payload_full", "is_replica",
}

func (s *Store) migrate() error {
	// Apply base schema
	if _, err := s.db.Exec(baseSchema); err != nil {
		return fmt.Errorf("failed to apply base schema: %w", err)
	}

	// Apply cluster schema: run individual ALTER statements and ignore
	// duplicate-column errors so repeated migrations are safe.
	if err := s.applyClusterSchema(); err != nil {
		s.logger.Warn("Cluster schema migration had errors (may be partial)", "error", err)
		// Don't fail the store -- non-cluster usage must still work.
	}

	// Legacy migrations: add columns if they don't exist (for older database files)
	migrations := []string{
		"ALTER TABLE jobs ADD COLUMN agent_id TEXT",
		"ALTER TABLE dead_letter ADD COLUMN agent_id TEXT",
		"ALTER TABLE jobs ADD COLUMN next_retry_at TEXT",
		"ALTER TABLE dead_letter ADD COLUMN due_at TEXT",
	}

	for _, m := range migrations {
		if _, err := s.db.Exec(m); err != nil {
			s.logger.Warn("Legacy migration step failed (column may already exist)",
				"migration", m, "error", err)
		}
	}

	return nil
}

// applyClusterSchema applies the cluster portion of clusterSchema one
// statement at a time, skipping duplicate-column errors and running
// the CREATE TABLE / CREATE INDEX statements via Exec as-is (they use
// IF NOT EXISTS so they are naturally idempotent).
func (s *Store) applyClusterSchema() error {
	// First, check which cluster columns already exist.
	var existingCols []string
	rows, err := s.db.Query(`PRAGMA table_info(jobs)`)
	if err == nil {
		for rows.Next() {
			var (
				cid        int
				name       string
				ctype      string
				notnull    int
				dfltValue  sql.NullString
				pk         int
			)
			if err2 := rows.Scan(&cid, &name, &ctype, &notnull, &dfltValue, &pk); err2 == nil {
				existingCols = append(existingCols, name)
			}
		}
		rows.Close()
	}

	// Filter out existing columns from ALTER statements.
	altered := strings.Builder{}
	for _, col := range clusterColumnNames {
		if has(existingCols, col) {
			continue
		}
		altered.WriteString(fmt.Sprintf("ALTER TABLE jobs ADD COLUMN %s;\n", col))
	}

	// Run the filtered ALTER statements.
	if altered.Len() > 0 {
		if _, err := s.db.Exec(altered.String()); err != nil {
			// Return the error for logging; callers can decide.
			return err
		}
	}

	// Run CREATE TABLE / CREATE INDEX statements (they use IF NOT EXISTS).
	createStmts := []string{
		`CREATE TABLE IF NOT EXISTS cluster_events (
			event_id TEXT PRIMARY KEY,
			node_id TEXT NOT NULL,
			event_type TEXT NOT NULL,
			timestamp INTEGER NOT NULL,
			vector_clock TEXT NOT NULL,
			payload BLOB NOT NULL,
			signature BLOB NOT NULL,
			received_at INTEGER NOT NULL,
			synced INTEGER DEFAULT 0
		)`,
		`CREATE INDEX IF NOT EXISTS idx_events_type ON cluster_events(event_type)`,
		`CREATE INDEX IF NOT EXISTS idx_events_node ON cluster_events(node_id)`,
		`CREATE INDEX IF NOT EXISTS idx_events_time ON cluster_events(timestamp)`,
		`CREATE TABLE IF NOT EXISTS cluster_members (
			node_id TEXT PRIMARY KEY,
			node_name TEXT,
			wireguard_pub TEXT NOT NULL,
			signing_pub BLOB NOT NULL,
			endpoint TEXT NOT NULL,
			capabilities TEXT,
			cluster_ip TEXT,
			joined_at INTEGER NOT NULL,
			last_heartbeat INTEGER NOT NULL,
			status TEXT NOT NULL
		)`,
		`CREATE INDEX IF NOT EXISTS idx_members_status ON cluster_members(status)`,
	}

	for _, stmt := range createStmts {
		if _, err := s.db.Exec(stmt); err != nil {
			return err
		}
	}

	return nil
}

// has reports whether slice contains the given element.
func has(slice []string, elem string) bool {
	for _, s := range slice {
		if s == elem {
			return true
		}
	}
	return false
}

// Insert adds a new job to the queue.
func (s *Store) Insert(job *Job) error {
	capsJSON, _ := json.Marshal(job.RequiredCaps)

	var dueAt *string
	if job.DueAt != nil {
		t := job.DueAt.Format(time.RFC3339)
		dueAt = &t
	}

	_, err := s.db.Exec(`
		INSERT INTO jobs (id, task_id, agent_id, type, priority, state, payload, required_caps,
		                  max_retries, retry_count, claimed_by, result, error, created_at, updated_at, due_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		job.ID,
		nullableString(job.TaskID),
		nullableString(job.AgentID),
		string(job.Type),
		int(job.Priority),
		string(job.State),
		string(job.Payload),
		string(capsJSON),
		job.MaxRetries,
		job.RetryCount,
		nullableString(job.ClaimedBy),
		nullableRawJSON(job.Result),
		nullableString(job.Error),
		job.CreatedAt.Format(time.RFC3339),
		job.UpdatedAt.Format(time.RFC3339),
		dueAt,
	)

	if err != nil {
		s.logger.Error("Failed to insert job", "id", job.ID, "error", err)
		return fmt.Errorf("failed to insert job: %w", err)
	}

	s.logger.Debug("Job inserted", "id", job.ID, "type", job.Type, "priority", job.Priority)
	return nil
}

// GetByID retrieves a job by its ID.
func (s *Store) GetByID(id string) (*Job, error) {
	row := s.db.QueryRow(`
		SELECT id, task_id, agent_id, type, priority, state, payload, required_caps,
		       max_retries, retry_count, claimed_by, result, error, created_at, updated_at, due_at, next_retry_at
		FROM jobs WHERE id = ?`, id)

	return s.scanJob(row)
}

// ClaimNext claims the next available job matching the worker's capabilities.
// Uses SELECT FOR UPDATE semantics via immediate transaction.
func (s *Store) ClaimNext(workerID string, caps []string) (*Job, error) {
	return s.ClaimNextForAgent(workerID, caps, "")
}

// ClaimNextForAgent claims the next available job for a specific agent.
// If agentID is empty, claims any job matching capabilities.
// If agentID is specified, only claims jobs targeted to that agent OR unassigned jobs.
// Respects next_retry_at for jobs with retry backoff.
func (s *Store) ClaimNextForAgent(workerID string, caps []string, agentID string) (*Job, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return nil, fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	now := time.Now().UTC().Format(time.RFC3339)

	// Build query with optional agent filtering
	// Jobs can be claimed if:
	// - They have no agent_id (unassigned, any agent can claim)
	// - Their agent_id matches the claiming agent
	// - Retry backoff has elapsed (next_retry_at <= now)
	var query string
	var args []any

	if agentID != "" {
		query = `
			SELECT id, task_id, agent_id, type, priority, state, payload, required_caps,
			       max_retries, retry_count, claimed_by, result, error, created_at, updated_at, due_at, next_retry_at
			FROM jobs
			WHERE state = 'pending'
			  AND (due_at IS NULL OR due_at <= ?)
			  AND (next_retry_at IS NULL OR next_retry_at <= ?)
			  AND (agent_id IS NULL OR agent_id = '' OR agent_id = ?)
			ORDER BY
			  CASE WHEN agent_id = ? THEN 0 ELSE 1 END,
			  priority DESC, created_at ASC
			LIMIT 10`
		args = []any{now, now, agentID, agentID}
	} else {
		query = `
			SELECT id, task_id, agent_id, type, priority, state, payload, required_caps,
			       max_retries, retry_count, claimed_by, result, error, created_at, updated_at, due_at, next_retry_at
			FROM jobs
			WHERE state = 'pending'
			  AND (due_at IS NULL OR due_at <= ?)
			  AND (next_retry_at IS NULL OR next_retry_at <= ?)
			ORDER BY priority DESC, created_at ASC
			LIMIT 10`
		args = []any{now, now}
	}

	rows, err := tx.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to query jobs: %w", err)
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

	if claimableJob == nil {
		return nil, ErrNoJobAvailable
	}

	// Claim the job (reuse now from above since it's already in the same transaction)
	_, err = tx.Exec(`
		UPDATE jobs SET state = 'claimed', claimed_by = ?, updated_at = ?
		WHERE id = ? AND state = 'pending'`,
		workerID, now, claimableJob.ID)
	if err != nil {
		return nil, fmt.Errorf("failed to claim job: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("failed to commit claim: %w", err)
	}

	claimableJob.State = StateClaimed
	claimableJob.ClaimedBy = workerID
	s.logger.Info("Job claimed", "id", claimableJob.ID, "worker", workerID, "agent", claimableJob.AgentID)
	return claimableJob, nil
}

// UpdateState updates a job's state.
func (s *Store) UpdateState(jobID string, state JobState) error {
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := s.db.Exec(`UPDATE jobs SET state = ?, updated_at = ? WHERE id = ?`,
		string(state), now, jobID)
	return err
}

// Complete marks a job as completed with a result.
func (s *Store) Complete(jobID string, result any) error {
	resultJSON, err := json.Marshal(result)
	if err != nil {
		return fmt.Errorf("failed to marshal result: %w", err)
	}

	now := time.Now().UTC().Format(time.RFC3339)
	_, err = s.db.Exec(`
		UPDATE jobs SET state = 'completed', result = ?, updated_at = ?
		WHERE id = ?`,
		string(resultJSON), now, jobID)

	if err != nil {
		return fmt.Errorf("failed to complete job: %w", err)
	}

	s.logger.Info("Job completed", "id", jobID)
	return nil
}

// Fail marks a job as failed with an error message.
func (s *Store) Fail(jobID, errMsg string) error {
	now := time.Now().UTC().Format(time.RFC3339)

	// Get current retry count
	var retryCount, maxRetries int
	row := s.db.QueryRow(`SELECT retry_count, max_retries FROM jobs WHERE id = ?`, jobID)
	if err := row.Scan(&retryCount, &maxRetries); err != nil {
		return fmt.Errorf("failed to get retry count: %w", err)
	}

	newState := StateFailed
	if retryCount >= maxRetries {
		newState = StateDead
	}

	_, err := s.db.Exec(`
		UPDATE jobs SET state = ?, error = ?, updated_at = ?
		WHERE id = ?`,
		string(newState), errMsg, now, jobID)

	if err != nil {
		return fmt.Errorf("failed to update job failure: %w", err)
	}

	s.logger.Info("Job failed", "id", jobID, "state", newState, "error", errMsg)

	// Move to dead letter if too many retries
	if newState == StateDead {
		if err := s.moveToDead(jobID); err != nil {
			s.logger.Error("Failed to move job to dead letter", "id", jobID, "error", err)
		}
	}

	return nil
}

// retryBackoffBase is the base delay for exponential retry backoff.
const retryBackoffBase = 2 * time.Second

// Retry resets a failed job for retry with exponential backoff.
// Backoff follows: 2s, 4s, 8s (capped at 8s).
func (s *Store) Retry(jobID string) error {
	now := time.Now().UTC()

	// Get current retry count to calculate backoff
	var retryCount int
	row := s.db.QueryRow(`SELECT retry_count FROM jobs WHERE id = ?`, jobID)
	if err := row.Scan(&retryCount); err != nil {
		return fmt.Errorf("failed to get retry count: %w", err)
	}

	// Calculate exponential backoff: 2s * 2^retryCount, capped at 8s
	backoffMultiplier := 1 << retryCount // 2^retryCount: 1, 2, 4, 8, ...
	backoff := min(retryBackoffBase*time.Duration(backoffMultiplier), 8*time.Second)

	nextRetryAt := now.Add(backoff)

	result, err := s.db.Exec(`
		UPDATE jobs
		SET state = 'pending',
		    retry_count = retry_count + 1,
		    claimed_by = NULL,
		    error = NULL,
		    next_retry_at = ?,
		    updated_at = ?
		WHERE id = ? AND state IN ('failed', 'claimed')`,
		nextRetryAt.Format(time.RFC3339), now.Format(time.RFC3339), jobID)

	if err != nil {
		return fmt.Errorf("failed to retry job: %w", err)
	}

	rows, _ := result.RowsAffected()
	if rows == 0 {
		return fmt.Errorf("job not found or not in retryable state: %s", jobID)
	}

	s.logger.Info("Job queued for retry with backoff",
		"id", jobID,
		"retry_count", retryCount+1,
		"backoff", backoff,
		"next_retry_at", nextRetryAt,
	)
	return nil
}

// ResetToPending resets a claimed/processing job back to pending state,
// clearing the claimed_by, result, and error fields. This is used when
// a node is unreachable and its jobs need to be re-handled by another node.
func (s *Store) ResetToPending(ctx context.Context, jobID string) error {
	now := time.Now().UTC().Format(time.RFC3339)
	result, err := s.db.ExecContext(ctx, `
		UPDATE jobs
		SET state = 'pending',
		    claimed_by = NULL,
		    result = NULL,
		    error = NULL,
		    timeout_at = NULL,
		    last_heartbeat_at = NULL,
		    updated_at = ?
		WHERE id = ? AND state IN ('claimed', 'processing')`,
		now, jobID)
	if err != nil {
		return fmt.Errorf("failed to reset job to pending: %w", err)
	}

	rows, _ := result.RowsAffected()
	if rows == 0 {
		return fmt.Errorf("job not found or not in claimed/processing state: %s", jobID)
	}

	s.logger.Info("cluster_queue: job reset to pending", "job_id", jobID)
	return nil
}

// ListByState returns jobs in a given state.
func (s *Store) ListByState(state JobState, limit int) ([]*Job, error) {
	rows, err := s.db.Query(`
		SELECT id, task_id, agent_id, type, priority, state, payload, required_caps,
		       max_retries, retry_count, claimed_by, result, error, created_at, updated_at, due_at, next_retry_at
		FROM jobs
		WHERE state = ?
		ORDER BY priority DESC, created_at ASC
		LIMIT ?`,
		string(state), limit)
	if err != nil {
		return nil, fmt.Errorf("failed to query jobs: %w", err)
	}
	defer rows.Close()

	var jobs []*Job
	for rows.Next() {
		job, err := s.scanJobRows(rows)
		if err != nil {
			s.logger.Error("Failed to scan job", "error", err)
			continue
		}
		jobs = append(jobs, job)
	}

	return jobs, nil
}

// ListByTaskID returns all jobs associated with a task.
func (s *Store) ListByTaskID(taskID string) ([]*Job, error) {
	rows, err := s.db.Query(`
		SELECT id, task_id, agent_id, type, priority, state, payload, required_caps,
		       max_retries, retry_count, claimed_by, result, error, created_at, updated_at, due_at, next_retry_at
		FROM jobs
		WHERE task_id = ?
		ORDER BY created_at ASC`,
		taskID)
	if err != nil {
		return nil, fmt.Errorf("failed to query jobs: %w", err)
	}
	defer rows.Close()

	var jobs []*Job
	for rows.Next() {
		job, err := s.scanJobRows(rows)
		if err != nil {
			continue
		}
		jobs = append(jobs, job)
	}

	return jobs, nil
}

// ListByAgentID returns all pending jobs assigned to a specific agent.
func (s *Store) ListByAgentID(agentID string, limit int) ([]*Job, error) {
	rows, err := s.db.Query(`
		SELECT id, task_id, agent_id, type, priority, state, payload, required_caps,
		       max_retries, retry_count, claimed_by, result, error, created_at, updated_at, due_at, next_retry_at
		FROM jobs
		WHERE agent_id = ? AND state = 'pending'
		ORDER BY priority DESC, created_at ASC
		LIMIT ?`,
		agentID, limit)
	if err != nil {
		return nil, fmt.Errorf("failed to query jobs: %w", err)
	}
	defer rows.Close()

	var jobs []*Job
	for rows.Next() {
		job, err := s.scanJobRows(rows)
		if err != nil {
			continue
		}
		jobs = append(jobs, job)
	}

	return jobs, nil
}

// GetStats returns queue statistics.
func (s *Store) GetStats() (*QueueStats, error) {
	stats := &QueueStats{
		ByState:    make(map[JobState]int),
		ByPriority: make(map[Priority]int),
	}

	// Count by state
	rows, err := s.db.Query(`SELECT state, COUNT(*) FROM jobs GROUP BY state`)
	if err != nil {
		return nil, fmt.Errorf("failed to get state stats: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var state string
		var count int
		if err := rows.Scan(&state, &count); err != nil {
			continue
		}
		stats.ByState[JobState(state)] = count
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("failed iterating state stats: %w", err)
	}

	// Count by priority for pending jobs
	rows, err = s.db.Query(`SELECT priority, COUNT(*) FROM jobs WHERE state = 'pending' GROUP BY priority`)
	if err != nil {
		return nil, fmt.Errorf("failed to get priority stats: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var priority, count int
		if err := rows.Scan(&priority, &count); err != nil {
			continue
		}
		stats.ByPriority[Priority(priority)] = count
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("failed iterating priority stats: %w", err)
	}

	// Dead letter count
	row := s.db.QueryRow(`SELECT COUNT(*) FROM dead_letter`)
	_ = row.Scan(&stats.DeadCount)

	return stats, nil
}

// QueueStats holds queue statistics.
//
//nolint:revive // stutter with package name is intentional for API clarity
type QueueStats struct {
	ByState    map[JobState]int
	ByPriority map[Priority]int
	DeadCount  int
}

// moveToDead moves a job to the dead letter table.
func (s *Store) moveToDead(jobID string) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	now := time.Now().UTC().Format(time.RFC3339)

	// Insert into dead_letter, preserving due_at from the original job.
	_, err = tx.Exec(`
		INSERT INTO dead_letter (id, task_id, agent_id, type, priority, payload, required_caps, max_retries, retry_count, error, created_at, died_at, due_at)
		SELECT id, task_id, agent_id, type, priority, payload, required_caps, max_retries, retry_count, error, created_at, ?, due_at
		FROM jobs WHERE id = ?`,
		now, jobID)
	if err != nil {
		return err
	}

	// Delete from jobs
	_, err = tx.Exec(`DELETE FROM jobs WHERE id = ?`, jobID)
	if err != nil {
		return err
	}

	return tx.Commit()
}

// RecoverFromDeadLetter recovers a dead-lettered job by re-inserting it into the active queue.
// The job is reset to pending state with retry count cleared.
// Returns the recovered job or an error if recovery fails.
func (s *Store) RecoverFromDeadLetter(jobID string) (*Job, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return nil, fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	now := time.Now().UTC().Format(time.RFC3339)

	// Select from dead_letter
	row := tx.QueryRow(`
		SELECT id, task_id, agent_id, type, priority, payload, required_caps, max_retries, retry_count, error, created_at, due_at
		FROM dead_letter WHERE id = ?`, jobID)

	var (
		id, jobType, payload, capsJSON   string
		priority, maxRetries, retryCount int
		taskID, agentID                  sql.NullString
		errMsg                           sql.NullString
		createdAt                        string
		dueAt                            sql.NullString
	)

	err = row.Scan(&id, &taskID, &agentID, &jobType, &priority, &payload, &capsJSON,
		&maxRetries, &retryCount, &errMsg, &createdAt, &dueAt)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("dead letter job not found: %s", jobID)
		}
		return nil, fmt.Errorf("failed to read dead letter: %w", err)
	}

	// Re-insert into jobs with reset state, preserving due_at from dead_letter.
	_, err = tx.Exec(`
		INSERT INTO jobs (id, task_id, agent_id, type, priority, state, payload, required_caps,
		                  max_retries, retry_count, claimed_by, result, error, created_at, updated_at, due_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		id,
		taskID,
		agentID,
		jobType,
		priority,
		string(StatePending),
		payload,
		capsJSON,
		maxRetries,
		0, // Reset retry_count
		(*string)(nil), // claimed_by
		(*string)(nil), // result
		(*string)(nil), // Reset error
		createdAt,
		now,
		dueAt, // Preserve due_at from dead letter
	)
	if err != nil {
		return nil, fmt.Errorf("failed to re-insert job: %w", err)
	}

	// Delete from dead_letter
	_, err = tx.Exec(`DELETE FROM dead_letter WHERE id = ?`, jobID)
	if err != nil {
		return nil, fmt.Errorf("failed to remove from dead letter: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("failed to commit recovery: %w", err)
	}

	// Fetch and return the recovered job
	recovered, err := s.GetByID(jobID)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch recovered job: %w", err)
	}

	s.logger.Info("Dead letter job recovered",
		"job_id", jobID,
		"task_id", recovered.TaskID,
		"agent_id", recovered.AgentID,
	)

	return recovered, nil
}

// ListDeadLetter returns dead-lettered jobs with optional filtering.
func (s *Store) ListDeadLetter(limit int) ([]*Job, error) {
	rows, err := s.db.Query(`
		SELECT id, task_id, agent_id, type, priority, payload, required_caps,
		       max_retries, retry_count, error, created_at, died_at, due_at
		FROM dead_letter
		ORDER BY died_at ASC
		LIMIT ?`, limit)
	if err != nil {
		return nil, fmt.Errorf("failed to query dead letter: %w", err)
	}
	defer rows.Close()

	var jobs []*Job
	for rows.Next() {
		var (
			id, jobType, payload, capsJSON   string
			priority, maxRetries, retryCount int
			taskID, agentID                  sql.NullString
			errMsg                           sql.NullString
			createdAt, diedAt                string
			dueAt                            sql.NullString
		)

		err := rows.Scan(&id, &taskID, &agentID, &jobType, &priority, &payload, &capsJSON,
			&maxRetries, &retryCount, &errMsg, &createdAt, &diedAt, &dueAt)
		if err != nil {
			s.logger.Error("Failed to scan dead letter job", "error", err)
			continue
		}

		job := &Job{
			ID:         id,
			Type:       JobType(jobType),
			State:      StateDead,
			Payload:    json.RawMessage(payload),
			Priority:   Priority(priority),
			MaxRetries: maxRetries,
			RetryCount: retryCount,
		}

		if taskID.Valid {
			job.TaskID = taskID.String
		}
		if agentID.Valid {
			job.AgentID = agentID.String
		}
		if errMsg.Valid {
			job.Error = errMsg.String
		}

		_ = json.Unmarshal([]byte(capsJSON), &job.RequiredCaps)

		if t, err := time.Parse(time.RFC3339, createdAt); err == nil {
			job.CreatedAt = t
		}

		if dueAt.Valid {
			if t, err := time.Parse(time.RFC3339, dueAt.String); err == nil {
				job.DueAt = &t
			}
		}

		jobs = append(jobs, job)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("failed iterating dead letter jobs: %w", err)
	}

	return jobs, nil
}

// DeadLetterStats returns statistics about dead-lettered jobs.
func (s *Store) DeadLetterStats() (int, error) {
	var count int
	err := s.db.QueryRow(`SELECT COUNT(*) FROM dead_letter`).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("failed to count dead letter: %w", err)
	}
	return count, nil
}

// Close closes the database connection.
func (s *Store) Close() error {
	return s.db.Close()
}

// DB returns the underlying database connection for recovery operations.
func (s *Store) DB() *sql.DB {
	return s.db
}

// GetClusterMembers reads active cluster members from the cluster_members table.
func (s *Store) GetClusterMembers() ([]*ClusterMember, error) {
	rows, err := s.db.Query(`
		SELECT node_id, node_name, wireguard_pub, signing_pub,
		       endpoint, capabilities, cluster_ip,
		       joined_at, last_heartbeat, status
		FROM cluster_members WHERE status = 'active'
		ORDER BY joined_at ASC
	`)
	if err != nil {
		return nil, fmt.Errorf("failed to query cluster_members: %w", err)
	}
	defer rows.Close()

	var members []*ClusterMember
	for rows.Next() {
		var m ClusterMember
		if err := s.scanClusterMember(rows, &m); err != nil {
			s.logger.Warn("failed to scan cluster member", "error", err)
			continue
		}
		members = append(members, &m)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterating cluster_members: %w", err)
	}
	return members, nil
}

// scanClusterMember scans a row into a ClusterMember struct.
func (s *Store) scanClusterMember(row Scanner, m *ClusterMember) error {
	var (
		joinedAt, lastHb        int64
		signingPubRaw           []byte
		capabilities, endpoint  string
		wireguardPub, nodeID    string
		nodeName, clusterIP     sql.NullString
		status                  string
	)
	if err := row.Scan(&nodeID, &nodeName, &wireguardPub, &signingPubRaw,
		&endpoint, &capabilities, &clusterIP,
		&joinedAt, &lastHb, &status); err != nil {
		return err
	}
	m.NodeID = nodeID
	m.NodeName = nodeName.String
	m.WireGuardPub = wireguardPub
	// Validate signing pubkey length: ed25519 public keys are exactly 32 bytes.
	// An empty slice indicates missing/uninitialized data (despite NOT NULL schema).
	if len(signingPubRaw) != 0 && len(signingPubRaw) != ed25519.PublicKeySize {
		return fmt.Errorf("invalid signing pubkey length: %d", len(signingPubRaw))
	}
	m.SigningPub = signingPubRaw
	m.Endpoint = endpoint
	m.ClusterIP = clusterIP.String
	m.Status = status
	if joinedAt > 0 {
		// joined_at is stored as UnixNano (see cluster_schema_test.go).
		m.JoinedAt = time.Unix(0, joinedAt)
	}
	if lastHb > 0 {
		// last_heartbeat is stored as UnixNano (see cluster_schema_test.go and
		// CheckNodeReachability in cluster_queue.go). Treat it as nanoseconds,
		// not seconds, so LastHeartbeat reflects the actual write time.
		m.LastHeartbeat = time.Unix(0, lastHb)
	}
	if capabilities != "" {
		_ = json.Unmarshal([]byte(capabilities), &m.Capabilities)
	}
	return nil
}

// ClusterMember is a simplified representation of a cluster peer.
type ClusterMember struct {
	NodeID       string        `json:"node_id"`
	NodeName     string        `json:"node_name"`
	WireGuardPub string        `json:"wireguard_pubkey"`
	SigningPub   ed25519.PublicKey `json:"signing_pubkey"`
	WireGuardKey []byte        `json:"-"`
	Capabilities []string      `json:"capabilities"`
	Endpoint     string        `json:"endpoint"`
	ClusterIP    string        `json:"cluster_ip"`
	JoinedAt     time.Time     `json:"joined_at"`
	LastHeartbeat time.Time    `json:"last_heartbeat"`
	Status       string        `json:"status"`
}

// Scanner is a minimal interface for rows/row.
type Scanner interface {
	Scan(dest ...any) error
}

func (s *Store) scanJob(row *sql.Row) (*Job, error) {
	var (
		id, jobType, state, payload      string
		priority, maxRetries, retryCount int
		taskID, agentID, claimedBy       sql.NullString
		result, errMsg                   sql.NullString
		capsJSON                         string
		createdAt, updatedAt             string
		dueAt, nextRetryAt               sql.NullString
	)

	err := row.Scan(&id, &taskID, &agentID, &jobType, &priority, &state, &payload, &capsJSON,
		&maxRetries, &retryCount, &claimedBy, &result, &errMsg, &createdAt, &updatedAt, &dueAt, &nextRetryAt)
	if err != nil {
		return nil, err
	}

	return s.buildJob(id, taskID, agentID, jobType, state, payload, capsJSON, priority, maxRetries, retryCount, claimedBy, result, errMsg, createdAt, updatedAt, dueAt, nextRetryAt)
}

func (s *Store) scanJobRows(rows *sql.Rows) (*Job, error) {
	var (
		id, jobType, state, payload      string
		priority, maxRetries, retryCount int
		taskID, agentID, claimedBy       sql.NullString
		result, errMsg                   sql.NullString
		capsJSON                         string
		createdAt, updatedAt             string
		dueAt, nextRetryAt               sql.NullString
	)

	err := rows.Scan(&id, &taskID, &agentID, &jobType, &priority, &state, &payload, &capsJSON,
		&maxRetries, &retryCount, &claimedBy, &result, &errMsg, &createdAt, &updatedAt, &dueAt, &nextRetryAt)
	if err != nil {
		return nil, err
	}

	return s.buildJob(id, taskID, agentID, jobType, state, payload, capsJSON, priority, maxRetries, retryCount, claimedBy, result, errMsg, createdAt, updatedAt, dueAt, nextRetryAt)
}

func (s *Store) buildJob(id string, taskID, agentID sql.NullString, jobType, state, payload, capsJSON string,
	priority, maxRetries, retryCount int, claimedBy, result, errMsg sql.NullString,
	createdAt, updatedAt string, dueAt, nextRetryAt sql.NullString) (*Job, error) {

	job := &Job{
		ID:         id,
		Type:       JobType(jobType),
		State:      JobState(state),
		Payload:    json.RawMessage(payload),
		Priority:   Priority(priority),
		MaxRetries: maxRetries,
		RetryCount: retryCount,
	}

	if taskID.Valid {
		job.TaskID = taskID.String
	}
	if agentID.Valid {
		job.AgentID = agentID.String
	}
	if claimedBy.Valid {
		job.ClaimedBy = claimedBy.String
	}
	if result.Valid {
		job.Result = json.RawMessage(result.String)
	}
	if errMsg.Valid {
		job.Error = errMsg.String
	}

	_ = json.Unmarshal([]byte(capsJSON), &job.RequiredCaps)

	if t, err := time.Parse(time.RFC3339, createdAt); err == nil {
		job.CreatedAt = t
	}
	if t, err := time.Parse(time.RFC3339, updatedAt); err == nil {
		job.UpdatedAt = t
	}
	if dueAt.Valid {
		if t, err := time.Parse(time.RFC3339, dueAt.String); err == nil {
			job.DueAt = &t
		}
	}
	if nextRetryAt.Valid {
		if t, err := time.Parse(time.RFC3339, nextRetryAt.String); err == nil {
			job.NextRetryAt = &t
		}
	}

	return job, nil
}

func nullableString(s string) any {
	if s == "" {
		return nil
	}
	return s
}

func nullableRawJSON(j json.RawMessage) any {
	if len(j) == 0 {
		return nil
	}
	return string(j)
}

// ClaimNextByID attempts to claim a specific job by ID.
// Returns the job if successfully claimed, nil if already claimed or not found.
func (s *Store) ClaimNextByID(jobID, workerID string) (*Job, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return nil, fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	now := time.Now().UTC().Format(time.RFC3339)

	// Try to claim the specific job
	result, err := tx.Exec(`
		UPDATE jobs SET state = 'claimed', claimed_by = ?, updated_at = ?
		WHERE id = ? AND state = 'pending'`,
		workerID, now, jobID)
	if err != nil {
		return nil, fmt.Errorf("failed to claim job: %w", err)
	}

	rows, _ := result.RowsAffected()
	if rows == 0 {
		return nil, ErrJobAlreadyClaimed
	}

	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("failed to commit claim: %w", err)
	}

	// Fetch and return the claimed job
	return s.GetByID(jobID)
}
