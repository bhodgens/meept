package queue

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	_ "github.com/mattn/go-sqlite3"
)

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

	db, err := sql.Open("sqlite3", dbPath+"?_journal_mode=WAL&_busy_timeout=5000")
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

func (s *Store) migrate() error {
	schema := `
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
		died_at       TEXT NOT NULL
	);
	`

	_, err := s.db.Exec(schema)
	if err != nil {
		return err
	}

	// Add columns if they don't exist (for migrations from older schemas)
	migrations := []string{
		"ALTER TABLE jobs ADD COLUMN agent_id TEXT",
		"ALTER TABLE dead_letter ADD COLUMN agent_id TEXT",
		"ALTER TABLE jobs ADD COLUMN next_retry_at TEXT",
	}

	for _, m := range migrations {
		// Ignore errors - column may already exist
		s.db.Exec(m)
	}

	return nil
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
	defer tx.Rollback()

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
		return nil, nil // No jobs available
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
func (s *Store) Fail(jobID string, errMsg string) error {
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
	backoff := retryBackoffBase * time.Duration(backoffMultiplier)
	if backoff > 8*time.Second {
		backoff = 8 * time.Second
	}

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

	// Dead letter count
	row := s.db.QueryRow(`SELECT COUNT(*) FROM dead_letter`)
	row.Scan(&stats.DeadCount)

	return stats, nil
}

// QueueStats holds queue statistics.
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
	defer tx.Rollback()

	now := time.Now().UTC().Format(time.RFC3339)

	// Insert into dead_letter
	_, err = tx.Exec(`
		INSERT INTO dead_letter (id, task_id, agent_id, type, priority, payload, required_caps, max_retries, retry_count, error, created_at, died_at)
		SELECT id, task_id, agent_id, type, priority, payload, required_caps, max_retries, retry_count, error, created_at, ?
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

// Close closes the database connection.
func (s *Store) Close() error {
	return s.db.Close()
}

func (s *Store) scanJob(row *sql.Row) (*Job, error) {
	var (
		id, jobType, state, payload       string
		priority, maxRetries, retryCount  int
		taskID, agentID, claimedBy        sql.NullString
		result, errMsg                    sql.NullString
		capsJSON                          string
		createdAt, updatedAt              string
		dueAt, nextRetryAt                sql.NullString
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
		id, jobType, state, payload       string
		priority, maxRetries, retryCount  int
		taskID, agentID, claimedBy        sql.NullString
		result, errMsg                    sql.NullString
		capsJSON                          string
		createdAt, updatedAt              string
		dueAt, nextRetryAt                sql.NullString
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

	json.Unmarshal([]byte(capsJSON), &job.RequiredCaps)

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

func nullableString(s string) interface{} {
	if s == "" {
		return nil
	}
	return s
}

func nullableRawJSON(j json.RawMessage) interface{} {
	if len(j) == 0 {
		return nil
	}
	return string(j)
}
