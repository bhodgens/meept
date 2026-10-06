package queue

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"strings"
	"time"

	"crypto/ed25519"

	"github.com/caimlas/meept/pkg/id"

	_ "modernc.org/sqlite" //nolint:revive // blank import for side effects
)

// ErrNoJobAvailable is returned when no claimable job is found.
var ErrNoJobAvailable = errors.New("no job available")

// ErrJobAlreadyClaimed is returned when a job cannot be claimed because it is
// already claimed by another worker or does not exist.
var ErrJobAlreadyClaimed = errors.New("job not found or already claimed")

// ErrJobNotClaimable is returned by Complete when the job is not in a
// claimable (claimed/processing) state for the attempt that is completing
// it. Jobs keep the same ID across Retry/Requeue, so this fires for stale
// or duplicate completion events — e.g. a completion for attempt 1 arriving
// after the job was requeued for attempt 2, or after a cluster reclaim
// re-executed it under a newer claim token. A same-attempt retry of an
// ALREADY-completed job is NOT this error: that is idempotent success (see
// CompleteAttempt).
var ErrJobNotClaimable = errors.New("job not in a claimable state for completion")

// ErrJobStateLocked is returned when a generic state update (UpdateState —
// i.e. MarkProcessing) targets a job that can no longer change state: it is
// terminal (completed/dead) or does not exist. Symmetric with
// ErrJobNotClaimable: a late MarkProcessing from a worker whose attempt was
// superseded must not resurrect a finished job into 'processing'.
var ErrJobStateLocked = errors.New("job not in an updatable state")

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

	// modernc.org/sqlite honors `_pragma=...` DSN params only; the
	// mattn-style `_journal_mode`/`_busy_timeout` keys used here previously
	// were silently ignored (see internal/plan/store_sqlite.go for the same
	// rationale). WAL + busy_timeout keep parallel job claims/acks from
	// turning into a SQLITE_BUSY storm.
	db, err := sql.Open("sqlite", dbPath+"?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)&_pragma=synchronous(NORMAL)")
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}
	// Serialize writes at the connection-pool level: sqlite allows exactly
	// one writer at a time, so a pool wider than the writer count only adds
	// BUSY handoffs. Multiple *readers* remain allowed under WAL.
	db.SetMaxOpenConns(1)
	if err := db.Ping(); err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to ping database: %w", err)
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
	interactive   INTEGER DEFAULT 0,
	claimed_by    TEXT,
	result        TEXT,
	error         TEXT,
	created_at    TEXT NOT NULL,
	updated_at    TEXT NOT NULL,
	due_at        TEXT,
	-- claim_token identifies the CURRENT execution attempt (audit H4).
	-- ClaimNextForAgent/ClaimNextByID write a fresh token on every claim;
	-- CompleteAttempt matches it, so a completion carries attempt identity,
	-- not just job state. NULL on a never-claimed job.
	claim_token   TEXT
);

CREATE INDEX IF NOT EXISTS idx_jobs_state_priority ON jobs(state, priority DESC, created_at);
-- idx_jobs_claim (claim-covering, interactive-first) is created in migrate()
-- AFTER the legacy column migration: on pre-interactive databases baseSchema's
-- CREATE TABLE is a no-op, so the column does not exist yet at base-schema
-- time and the index there would fail with "no such column".
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

	// Legacy migrations: add columns if they don't exist (for older database
	// files). Each ALTER is checked against the live column list first, so
	// repeated boots are silent instead of warning on duplicate columns.
	legacyMigrations := []struct {
		table   string
		column  string
		declSQL string
	}{
		{"jobs", "agent_id", "ALTER TABLE jobs ADD COLUMN agent_id TEXT"},
		{"dead_letter", "agent_id", "ALTER TABLE dead_letter ADD COLUMN agent_id TEXT"},
		{"jobs", "next_retry_at", "ALTER TABLE jobs ADD COLUMN next_retry_at TEXT"},
		{"dead_letter", "due_at", "ALTER TABLE dead_letter ADD COLUMN due_at TEXT"},
		// Interactive column for pre-interactive databases (tree 04 leaf 02).
		// SQLite applies DEFAULT 0 to pre-existing rows on ADD COLUMN, which
		// is exactly the required backfill: old jobs are background.
		{"jobs", "interactive", "ALTER TABLE jobs ADD COLUMN interactive INTEGER DEFAULT 0"},
		// Completion epoch/token (audit H4). Existing rows get NULL, which
		// reads back as "never claimed under a token" — CompleteAttempt then
		// falls back to the legacy state-only predicate (see
		// tokenMatches), so an in-flight job on an upgraded database still
		// completes. No backfill write is needed or wanted: fabricating a
		// token for an attempt nobody holds would be a lie.
		{"jobs", "claim_token", "ALTER TABLE jobs ADD COLUMN claim_token TEXT"},
	}

	for _, mig := range legacyMigrations {
		exists, err := s.columnExists(mig.table, mig.column)
		if err != nil {
			s.logger.Warn("Legacy migration check failed; attempting ALTER anyway",
				"table", mig.table, "column", mig.column, "error", err)
		}
		if exists {
			continue
		}
		if _, err := s.db.Exec(mig.declSQL); err != nil {
			if strings.Contains(err.Error(), "duplicate column name") {
				continue // raced with a concurrent migration; harmless
			}
			s.logger.Warn("Legacy migration failed",
				"migration", mig.declSQL, "error", err)
		}
	}

	// Claim-covering index for interactive-first ordering (tree 04 leaf 02,
	// Contract 2). Runs after the interactive column migration above so
	// pre-interactive databases have the column before the index references
	// it. SQLite >= 3.3 supports DESC index columns; the modernc.org driver
	// bundles a current SQLite, matching store.go's feature baseline.
	if _, err := s.db.Exec(`CREATE INDEX IF NOT EXISTS idx_jobs_claim
		ON jobs(state, interactive DESC, priority DESC, created_at)`); err != nil {
		return fmt.Errorf("failed to create idx_jobs_claim: %w", err)
	}

	return nil
}

// columnExists reports whether the named column is present in the given
// table using PRAGMA table_info.
func (s *Store) columnExists(table, column string) (bool, error) {
	rows, err := s.db.Query(fmt.Sprintf(`PRAGMA table_info(%s)`, table))
	if err != nil {
		return false, fmt.Errorf("pragma table_info(%s): %w", table, err)
	}
	defer rows.Close()
	for rows.Next() {
		var (
			cid       int
			name      string
			ctype     string
			notnull   int
			dfltValue sql.NullString
			pk        int
		)
		if err := rows.Scan(&cid, &name, &ctype, &notnull, &dfltValue, &pk); err != nil {
			return false, fmt.Errorf("scan table_info row: %w", err)
		}
		if name == column {
			return true, nil
		}
	}
	return false, rows.Err()
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
		defer rows.Close()
		for rows.Next() {
			var (
				cid       int
				name      string
				ctype     string
				notnull   int
				dfltValue sql.NullString
				pk        int
			)
			if err2 := rows.Scan(&cid, &name, &ctype, &notnull, &dfltValue, &pk); err2 == nil {
				existingCols = append(existingCols, name)
			}
		}
		if err := rows.Err(); err != nil {
			s.logger.Warn("PRAGMA table_info iteration failed", "error", err)
		}
	}

	// Filter out existing columns from ALTER statements.
	altered := strings.Builder{}
	for _, col := range clusterColumnNames {
		if has(existingCols, col) {
			continue
		}
		fmt.Fprintf(&altered, "ALTER TABLE jobs ADD COLUMN %s;\n", col)
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
	return slices.Contains(slice, elem)
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
		                  max_retries, retry_count, interactive, claimed_by, result, error, created_at, updated_at, due_at, claim_token)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
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
		boolToInt(job.Interactive),
		nullableString(job.ClaimedBy),
		nullableRawJSON(job.Result),
		nullableString(job.Error),
		job.CreatedAt.Format(time.RFC3339),
		job.UpdatedAt.Format(time.RFC3339),
		dueAt,
		nullableString(job.ClaimToken),
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
		       max_retries, retry_count, interactive, claimed_by, result, error, created_at, updated_at, due_at, next_retry_at, claim_token
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

	// Claim semantics: job.AgentID is a SOFT PREFERENCE, not exclusivity.
	// A claiming worker with a non-empty agentID prefers its own jobs (the
	// ORDER BY pins them first) but can still claim unassigned jobs; a
	// worker with an EMPTY agentID has no agent filter at all and may claim
	// ANY pending job, including pinned ones. This is deliberate: pool
	// workers are generic executors, the agent persona (model, tools,
	// prompt) rides the job payload and is resolved by the job processor,
	// so any worker can run any job. Do not "fix" the empty-agentID query
	// to skip pinned jobs without also changing the pool's construction
	// (internal/worker/pool.go AddWorker starts every worker with an empty
	// agentID).

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

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterating claimable jobs: %w", err)
	}

	if claimableJob == nil {
		return nil, ErrNoJobAvailable
	}

	// Claim the job (reuse now from above since it's already in the same transaction).
	// A FRESH claim_token is minted for every claim: the token is the
	// execution attempt's identity (audit H4). A completion that presents an
	// older token belongs to a superseded attempt and is refused, while the
	// same token completing twice is idempotent.
	claimToken := newClaimToken()
	claimResult, err := tx.Exec(`
		UPDATE jobs SET state = 'claimed', claimed_by = ?, claim_token = ?, updated_at = ?
		WHERE id = ? AND state = 'pending'`,
		workerID, claimToken, now, claimableJob.ID)
	if err != nil {
		return nil, fmt.Errorf("failed to claim job: %w", err)
	}

	affected, _ := claimResult.RowsAffected()
	if affected == 0 {
		return nil, ErrJobAlreadyClaimed
	}

	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("failed to commit claim: %w", err)
	}

	claimableJob.State = StateClaimed
	claimableJob.ClaimedBy = workerID
	claimableJob.ClaimToken = claimToken
	s.logger.Info("Job claimed", "id", claimableJob.ID, "worker", workerID, "agent", claimableJob.AgentID)
	return claimableJob, nil
}

// UpdateState updates a job's state.
//
// State guard (audit L8): the write is refused when the row is TERMINAL
// (completed/dead) or absent. Without it, a late MarkProcessing from a worker
// whose attempt was superseded resurrects a finished job into 'processing',
// and Complete can then never settle it — the asymmetry was with Complete's
// own guard. Callers treat ErrJobStateLocked as "my attempt lost the race",
// which is exactly what it is.
//
// Only UpdateState's single production caller (MarkProcessing) touches a
// claimed/processing row, so the predicate stays general rather than pinning
// the exact old state: any non-terminal state is updatable.
func (s *Store) UpdateState(jobID string, state JobState) error {
	now := time.Now().UTC().Format(time.RFC3339)
	res, err := s.db.Exec(`
		UPDATE jobs SET state = ?, updated_at = ?
		WHERE id = ? AND state NOT IN ('completed', 'dead')`,
		string(state), now, jobID)
	if err != nil {
		return fmt.Errorf("failed to update job state: %w", err)
	}

	affected, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to read state update rows affected: %w", err)
	}
	if affected == 0 {
		s.logger.Warn("Refused state update for a terminal or missing job",
			"id", jobID, "requested_state", string(state))
		return ErrJobStateLocked
	}
	return nil
}

// Complete marks a job as completed with a result.
//
// LEGACY, TOKEN-LESS PATH — kept for callers that do not hold a claim token
// (bus/RPC/HTTP surfaces, tests). It applies the state-only guard: the job
// must be claimed/processing. A second completion of the same job fails with
// ErrJobNotClaimable, which is the pre-H4 contract those callers were built
// against. Workers and anything holding an attempt use CompleteAttempt, which
// additionally distinguishes a legitimate retry from a superseded attempt.
func (s *Store) Complete(jobID string, result any) error {
	_, err := s.CompleteAttempt(jobID, result, "")
	return err
}

// CompletionDisposition reports how a Complete* call resolved, so the queue
// layer (and a caller that must decide whether to publish) can tell a fresh
// completion from an idempotent retry and from a superseded attempt.
type CompletionDisposition int

const (
	// CompletionApplied means this call performed the state transition and the
	// completion is new — publish the completion event.
	CompletionApplied CompletionDisposition = iota
	// CompletionIdempotent means the job was already completed by the SAME
	// claim token: a retry of a request that already succeeded. Success (nil
	// error), but publish NO second event.
	CompletionIdempotent
)

// CompleteAttempt marks a job completed on behalf of the attempt identified by
// claimToken (audit H4). Three outcomes, distinguished by (state, token):
//
//   - state in (claimed, processing) AND token matches → CompletionApplied.
//   - state == completed AND token matches → CompletionIdempotent. The
//     IDENTICAL request retried by the same worker is idempotent success:
//     the first result stands, nothing is overwritten, and the caller
//     publishes no second event.
//   - anything else → ErrJobNotClaimable. This is the stale/double-completion
//     case: the job moved on (requeued, reclaimed, re-claimed as attempt 2)
//     so the presented token belongs to a SUPERSEDED attempt.
//
// An empty claimToken degrades to the legacy state-only predicate (see
// Complete), which keeps every token-less caller on its existing contract
// after the migration adds the column.
func (s *Store) CompleteAttempt(jobID string, result any, claimToken string) (CompletionDisposition, error) {
	resultJSON, err := json.Marshal(result)
	if err != nil {
		return CompletionApplied, fmt.Errorf("failed to marshal result: %w", err)
	}

	now := time.Now().UTC().Format(time.RFC3339)

	// Step 1 — the fresh transition. The token predicate is part of the WHERE
	// clause so the guard is enforced by the database, not by a read-then-write
	// race. Token-less callers keep the pre-H4 state-only predicate.
	// Args are appended in PLACEHOLDER ORDER: result, updated_at, id, then the
	// optional claim token (which the predicate appends last).
	claimPredicate := `state IN ('claimed', 'processing')`
	args := []any{string(resultJSON), now, jobID}
	if claimToken != "" {
		claimPredicate += ` AND claim_token = ?`
		args = append(args, claimToken)
	}

	res, err := s.db.Exec(fmt.Sprintf(`
		UPDATE jobs SET state = 'completed', result = ?, updated_at = ?
		WHERE id = ? AND %s`, claimPredicate), args...)
	if err != nil {
		return CompletionApplied, fmt.Errorf("failed to complete job: %w", err)
	}

	affected, err := res.RowsAffected()
	if err != nil {
		return CompletionApplied, fmt.Errorf("failed to read completion rows affected: %w", err)
	}
	if affected > 0 {
		s.logger.Info("Job completed", "id", jobID, "claim_token", claimToken)
		return CompletionApplied, nil
	}

	// Step 2 — the idempotent retry. Only reachable when this call presented a
	// token: the row is already 'completed' under the SAME attempt, i.e. the
	// identical request arriving twice. The first result is left untouched.
	if claimToken != "" {
		var (
			state       string
			storedToken sql.NullString
		)
		scanErr := s.db.QueryRow(`SELECT state, claim_token FROM jobs WHERE id = ?`, jobID).
			Scan(&state, &storedToken)
		if scanErr == nil &&
			state == string(StateCompleted) && storedToken.Valid && storedToken.String == claimToken {
			s.logger.Debug("Idempotent completion retry for the same claim token", "id", jobID)
			return CompletionIdempotent, nil
		}
		if errors.Is(scanErr, sql.ErrNoRows) {
			s.logger.Warn("Completion for an unknown job", "id", jobID)
		}
	}

	// Jobs keep the same ID across Retry/Requeue/Reclaim, so a refusal here
	// means a stale or double completion event from a SUPERSEDED attempt (the
	// token moved on) or a job that was never claimable. The earlier result must
	// not be overwritten and the caller must not publish a fresh completion.
	return CompletionApplied, ErrJobNotClaimable
}

// CompletionIsFresh reports whether a completion event should be published for
// this disposition. An idempotent retry is NOT fresh: the first completion
// already published, and republishing would double-count the job downstream
// (task finalization, memory sync).
func (d CompletionDisposition) CompletionIsFresh() bool {
	return d == CompletionApplied
}

// CompletionFresh inspects a job's completion state from the CALLER's side
// (audit H4/H5): does this queue job still accept a completion for the attempt
// identified by claimToken?
//
// This is the primitive the stale-completion guard needs. Job state alone
// cannot answer it: a job that a cluster reclaim reset to 'pending' and then
// re-executed is 'claimed' under a NEWER token, while the in-flight worker
// still holds the OLD one — so state says "claimed" (accepting) and attempt
// identity says "superseded" (rejecting). Only the token decides.
//
// Returned verdict:
//   - true  → accept: state is completed, or claimed/processing under THIS
//     token (the event is fresh, or a retry of it).
//   - false → drop as stale: the attempt is superseded (a newer claim token is
//     live), or the job is not in a completable state.
//   - ok=false → the job could not be read; the caller must fail open exactly
//     as it did before this helper existed.
//
// An empty claimToken falls back to the state-only predicate, which is the
// pre-token contract for callers that hold no attempt (and for rows migrated
// from a database that predates the column).
func (s *Store) CompletionIsFresh(jobID, claimToken string) (fresh, ok bool) {
	row := s.db.QueryRow(`SELECT state, claim_token FROM jobs WHERE id = ?`, jobID)
	var (
		state       string
		storedToken sql.NullString
	)
	if err := row.Scan(&state, &storedToken); err != nil {
		return false, false
	}

	if state == string(StateCompleted) {
		// Completed: fresh unless a DIFFERENT (newer) attempt has already
		// taken the job over, which the state alone cannot express — a
		// completed row keeps the token of the attempt that completed it, and
		// a re-claim mints a new one.
		return claimToken == "" || !storedToken.Valid || storedToken.String == claimToken, true
	}

	if state != string(StateClaimed) && state != string(StateProcessing) {
		// pending/failed: the attempt was requeued; stale.
		return false, true
	}
	if claimToken == "" || !storedToken.Valid {
		// Token-less caller (or a pre-migration row): state-only predicate.
		return true, true
	}
	return storedToken.String == claimToken, true
}

// PendingGate returns the next_retry_at a pending job is parked at, or the zero
// time when the job is absent or immediately claimable (audit M2: the wake
// scheduler needs to know WHEN a closed claim gate opens, which is not
// derivable from the transition's own arguments alone for Retry).
func (s *Store) PendingGate(jobID string) time.Time {
	var next sql.NullString
	if err := s.db.QueryRow(`SELECT next_retry_at FROM jobs WHERE id = ?`, jobID).Scan(&next); err != nil || !next.Valid {
		return time.Time{}
	}
	t, err := time.Parse(time.RFC3339, next.String)
	if err != nil {
		return time.Time{}
	}
	return t
}

// newClaimToken mints a claim token for one execution attempt. crypto/rand
// via pkg/id: never time.Now().UnixNano() or math/rand (Predictable ID rule),
// and the zero-suffix fallback is the documented entropy-exhaustion signal.
func newClaimToken() string {
	return id.Generate("claim-")
}

// Fail marks a job as failed with an error message.
func (s *Store) Fail(jobID, errMsg string) error {
	now := time.Now().UTC().Format(time.RFC3339)

	// Use BEGIN IMMEDIATE so the retry_count read and the state update are
	// atomic across concurrent Fail/Retry callers. Without this, two Fail
	// calls could both observe retryCount<maxRetries, both transition to
	// StateFailed, and neither trigger dead-lettering.
	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("begin fail tx: %w", err)
	}
	defer func() {
		if rErr := tx.Rollback(); rErr != nil && !errors.Is(rErr, sql.ErrTxDone) {
			s.logger.Debug("fail tx rollback (no-op after commit)", "error", rErr)
		}
	}()

	var retryCount, maxRetries int
	row := tx.QueryRow(`SELECT retry_count, max_retries FROM jobs WHERE id = ?`, jobID)
	if err := row.Scan(&retryCount, &maxRetries); err != nil {
		return fmt.Errorf("failed to get retry count: %w", err)
	}

	newState := StateFailed
	if retryCount >= maxRetries {
		newState = StateDead
	}

	if _, err := tx.Exec(`
		UPDATE jobs SET state = ?, error = ?, updated_at = ?
		WHERE id = ?`,
		string(newState), errMsg, now, jobID); err != nil {
		return fmt.Errorf("failed to update job failure: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit fail tx: %w", err)
	}

	s.logger.Info("Job failed", "id", jobID, "state", newState, "error", errMsg)

	// Move to dead letter if too many retries. Done after commit so a
	// dead-letter move failure does not roll back the state transition.
	if newState == StateDead {
		if err := s.moveToDead(jobID); err != nil {
			s.logger.Error("Failed to move job to dead letter", "id", jobID, "error", err)
		}
	}

	return nil
}

// retryBackoffBase is the base delay for exponential retry backoff.
const retryBackoffBase = 2 * time.Second

// retryBackoffCap bounds the exponential retry backoff. Rate-limit failures
// that miss the quota-class regex take this generic retry path and would
// otherwise re-hit the provider every few seconds until max_retries;
// failures WITH a parseable reset take the quota-deferral path
// (Store.Requeue) instead.
const retryBackoffCap = 30 * time.Second

// Requeue resets a job that hit a PROVIDER WAIT (llm.ThrottleBackoffError,
// QuotaResetError / ErrAllModelsQuotaBlocked — tree 03 leaf 03, D9) to
// pending for a future claim at notBefore, WITHOUT incrementing
// retry_count: a provider wait is the machine being patient, not a job
// failure, so the job's own retry budget must not be consumed. The claim
// query's existing `next_retry_at <= now` gate (ClaimNextForAgent) is
// the not-before mechanism — the leaf contract's "NotBefore" IS
// next_retry_at.
//
// interactive is preserved in the UPDATE column list (SHARED-CONVENTIONS
// §4.4): a requeued interactive job stays interactive so claim ordering
// is undisturbed. The job is left in a claimable state (claimed_by
// cleared, error cleared) so the next eligible Claim wins it.
func (s *Store) Requeue(jobID string, notBefore time.Time) error {
	now := time.Now().UTC()

	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("begin requeue tx: %w", err)
	}
	defer func() {
		if rErr := tx.Rollback(); rErr != nil && !errors.Is(rErr, sql.ErrTxDone) {
			s.logger.Debug("requeue tx rollback (no-op after commit)", "error", rErr)
		}
	}()

	result, err := tx.Exec(`
		UPDATE jobs
		SET state = 'pending',
		    claimed_by = NULL,
		    result = NULL,
		    error = NULL,
		    interactive = interactive,
		    next_retry_at = ?,
		    claim_token = NULL,
		    updated_at = ?
		WHERE id = ? AND state IN ('failed', 'claimed', 'processing')`,
		notBefore.UTC().Format(time.RFC3339), now.Format(time.RFC3339), jobID)

	if err != nil {
		return fmt.Errorf("failed to requeue job: %w", err)
	}

	rows, _ := result.RowsAffected()
	if rows == 0 {
		return fmt.Errorf("job not found or not in requeueable state: %s", jobID)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit requeue tx: %w", err)
	}

	s.logger.Info("Job requeued on provider wait (retry count unchanged)",
		"id", jobID,
		"not_before", notBefore.UTC(),
	)
	return nil
}

// Retry resets a failed job for retry with exponential backoff.
// Backoff follows: 2s, 4s, 8s, 16s, 30s (capped at 30s).
func (s *Store) Retry(jobID string) error {
	now := time.Now().UTC()

	// Wrap the read-modify-write in a transaction so retry_count cannot
	// change between the SELECT and the UPDATE. BEGIN IMMEDIATE acquires
	// a write lock up front, preventing a concurrent Fail() from
	// observing an inconsistent state.
	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("begin retry tx: %w", err)
	}
	defer func() {
		if rErr := tx.Rollback(); rErr != nil && !errors.Is(rErr, sql.ErrTxDone) {
			s.logger.Debug("requeue tx rollback (no-op after commit)", "error", rErr)
		}
	}()

	// Get current retry count to calculate backoff
	var retryCount int
	row := tx.QueryRow(`SELECT retry_count FROM jobs WHERE id = ?`, jobID)
	if err := row.Scan(&retryCount); err != nil {
		return fmt.Errorf("failed to get retry count: %w", err)
	}

	// Calculate exponential backoff: 2s * 2^retryCount, capped at 30s
	backoffMultiplier := 1 << retryCount // 2^retryCount: 1, 2, 4, 8, ...
	backoff := min(retryBackoffBase*time.Duration(backoffMultiplier), retryBackoffCap)

	nextRetryAt := now.Add(backoff)

	result, err := tx.Exec(`
		UPDATE jobs
		SET state = 'pending',
		    retry_count = retry_count + 1,
		    claimed_by = NULL,
		    error = NULL,
		    next_retry_at = ?,
		    claim_token = NULL,
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

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit retry tx: %w", err)
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
//
// claim_token is RETIRED (set to NULL), not carried over (audit H5). A reclaim
// declares "the attempt holding this token is abandoned": every worker still
// executing that attempt is, by definition, on a token nobody owns any more.
// Clearing it is what makes the reclaim SAFE for an in-flight worker — the
// moment the job is re-claimed, the new claim mints a fresh token, and the
// abandoned attempt's completion presents a stale one. Store.CompleteAttempt
// and Store.CompletionIsFresh both key on that token, so the stale completion
// is refused (ErrJobNotClaimable) instead of overwriting the re-executed
// attempt's result, and the downstream stale-completion guard can SEE the
// supersession rather than inferring it from job state.
//
// The claim_token = NULL is also why the "already retired" case is
// indistinguishable from a pre-migration row: both mean "no live attempt", and
// both resolve to the state-only predicate for a token-less caller. That is the
// backward-compatible fallback, not a hole.
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
		    claim_token = NULL,
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

// ResetStaleClaimsAtStartup resets all jobs left in claimed/processing state
// by a previous daemon process back to pending so they re-execute after a
// crash. It is the single-node counterpart of the cluster reclaim path
// (ResetToPending via ClusterQueue.reclaimJobUnlocked): on startup the process
// that held the claims is provably dead, so every pre-boot claim qualifies.
//
// claimsBefore filters the sweep to jobs whose last update happened strictly
// before that timestamp. Daemon startup passes its process start time because
// the worker pool starts (inside Components.Start) before the recovery block
// runs — claims written by THIS process must never be reset. RFC3339 truncates
// to seconds, so callers pass claimsBefore.Add(-time.Second) to stay safe.
//
// The reset mirrors ResetToPending exactly: claimed_by/result/error, the
// cluster claim columns AND claim_token are cleared (a retired attempt must
// not be able to complete a re-executed job — audit H5), while retry_count
// and next_retry_at are PRESERVED so a re-claimed orphan keeps its original
// retry budget/backoff (a crash is not a job failure). Updates run in a single
// transaction so a crash mid-sweep cannot leave a half-reset mix of states.
// Returns the number of jobs that were reset.
func (s *Store) ResetStaleClaimsAtStartup(ctx context.Context, claimsBefore time.Time) (int, error) {
	// Snapshot: how many jobs qualify right now, for the return value.
	// The UPDATE below re-checks the same predicates under the transaction,
	// so a concurrent claim cannot be reset by this sweep.
	var staleIDs []string
	rows, err := s.db.QueryContext(ctx, `
		SELECT id FROM jobs
		WHERE state IN ('claimed', 'processing')
		  AND updated_at < ?`,
		claimsBefore.UTC().Format(time.RFC3339))
	if err != nil {
		return 0, fmt.Errorf("failed to query stale job claims: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			s.logger.Error("Failed to scan stale claim job ID", "error", err)
			continue
		}
		staleIDs = append(staleIDs, id)
	}
	if err := rows.Err(); err != nil {
		return 0, fmt.Errorf("failed to iterate stale job claims: %w", err)
	}

	if len(staleIDs) == 0 {
		return 0, nil
	}

	now := time.Now().UTC().Format(time.RFC3339)
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, fmt.Errorf("failed to begin startup reclaim transaction: %w", err)
	}
	// Safe to call after Commit; a no-op on a committed transaction.
	defer func() {
		_ = tx.Rollback()
	}()

	result, err := tx.ExecContext(ctx, `
		UPDATE jobs
		SET state = 'pending',
		    claimed_by = NULL,
		    result = NULL,
		    error = NULL,
		    timeout_at = NULL,
		    last_heartbeat_at = NULL,
		    claim_token = NULL,
		    updated_at = ?
		WHERE state IN ('claimed', 'processing')
		  AND updated_at < ?`,
		now, claimsBefore.UTC().Format(time.RFC3339))
	if err != nil {
		return 0, fmt.Errorf("failed to reset stale job claims: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("failed to commit startup reclaim: %w", err)
	}

	reset, _ := result.RowsAffected()
	if reset != int64(len(staleIDs)) {
		// A concurrent claim/complete raced the sweep between snapshot and
		// UPDATE. The predicates re-checked under the transaction, so the
		// lower count is correct — log it rather than overstate.
		s.logger.Warn("startup reclaim raced with concurrent job state change",
			"snapshot_count", len(staleIDs), "reset_count", reset)
	}
	s.logger.Info("startup reclaim: reset crash-orphaned job claims to pending",
		"count", reset)
	return int(reset), nil
}

// ListByState returns jobs in a given state.
func (s *Store) ListByState(state JobState, limit int) ([]*Job, error) {
	rows, err := s.db.Query(`
		SELECT id, task_id, agent_id, type, priority, state, payload, required_caps,
		       max_retries, retry_count, interactive, claimed_by, result, error, created_at, updated_at, due_at, next_retry_at, claim_token
		FROM jobs
		WHERE state = ?
		ORDER BY interactive DESC, priority DESC, created_at ASC, id ASC
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
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("failed to iterate jobs: %w", err)
	}

	return jobs, nil
}

// ListByStateAfter returns jobs in a given state, ordered exactly like
// ListByState (interactive DESC, priority DESC, created_at ASC, id ASC),
// but starting strictly AFTER the keyset position identified by
// (afterInteractive, afterPriority, afterCreatedAt, afterID). This supports
// deterministic keyset pagination over large pending tables so the Claim
// slow path can scan past head-of-line parked jobs without re-reading
// earlier rows.
func (s *Store) ListByStateAfter(state JobState, limit int, afterInteractive bool, afterPriority int, afterCreatedAt time.Time, afterID string) ([]*Job, error) {
	const selectBase = `SELECT id, task_id, agent_id, type, priority, state, payload, required_caps,
		       max_retries, retry_count, interactive, claimed_by, result, error, created_at, updated_at, due_at, next_retry_at, claim_token
		FROM jobs
		WHERE state = ?`
	const orderBy = ` ORDER BY interactive DESC, priority DESC, created_at ASC, id ASC
		LIMIT ?`

	query := selectBase + orderBy
	args := []any{string(state), limit}

	// A zero cursor (zero time AND empty id) selects the first page with no
	// keyset filter — no real row can carry either marker (created_at is
	// NOT NULL and RFC3339-formatted; id is the primary key).
	if !afterCreatedAt.IsZero() || afterID != "" {
		createdAfter := afterCreatedAt.UTC().Format(time.RFC3339)
		query = selectBase + `
		  AND (interactive < ? OR
		       (interactive = ? AND priority < ?) OR
		       (interactive = ? AND priority = ? AND created_at > ?) OR
		       (interactive = ? AND priority = ? AND created_at = ? AND id > ?))` + orderBy
		args = []any{
			string(state),
			boolToInt(afterInteractive),
			boolToInt(afterInteractive), afterPriority,
			boolToInt(afterInteractive), afterPriority, createdAfter,
			boolToInt(afterInteractive), afterPriority, createdAfter, afterID,
			limit,
		}
	}

	rows, err := s.db.Query(query, args...)
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
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("failed to iterate jobs: %w", err)
	}

	return jobs, nil
}

// ListByTaskID returns all jobs associated with a task.
func (s *Store) ListByTaskID(taskID string) ([]*Job, error) {
	rows, err := s.db.Query(`
		SELECT id, task_id, agent_id, type, priority, state, payload, required_caps,
		       max_retries, retry_count, interactive, claimed_by, result, error, created_at, updated_at, due_at, next_retry_at, claim_token
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
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("failed to iterate jobs by task: %w", err)
	}

	return jobs, nil
}

// ListByAgentID returns all pending jobs assigned to a specific agent.
func (s *Store) ListByAgentID(agentID string, limit int) ([]*Job, error) {
	rows, err := s.db.Query(`
		SELECT id, task_id, agent_id, type, priority, state, payload, required_caps,
		       max_retries, retry_count, interactive, claimed_by, result, error, created_at, updated_at, due_at, next_retry_at, claim_token
		FROM jobs
		WHERE agent_id = ? AND state = 'pending'
		ORDER BY interactive DESC, priority DESC, created_at ASC
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
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("failed to iterate jobs by agent: %w", err)
	}

	return jobs, nil
}

// GetStats returns queue statistics.
func (s *Store) GetStats() (*QueueStats, error) {
	stats := &QueueStats{
		ByState:       make(map[JobState]int),
		ByPriority:    make(map[Priority]int),
		ByInteractive: make(map[bool]int),
	}

	// Count by state
	stateRows, err := s.db.Query(`SELECT state, COUNT(*) FROM jobs GROUP BY state`)
	if err != nil {
		return nil, fmt.Errorf("failed to get state stats: %w", err)
	}
	defer stateRows.Close()

	for stateRows.Next() {
		var state string
		var count int
		if err := stateRows.Scan(&state, &count); err != nil {
			continue
		}
		stats.ByState[JobState(state)] = count
	}
	if err := stateRows.Err(); err != nil {
		return nil, fmt.Errorf("failed iterating state stats: %w", err)
	}

	// Count by priority for pending jobs
	priorityRows, err := s.db.Query(`SELECT priority, COUNT(*) FROM jobs WHERE state = 'pending' GROUP BY priority`)
	if err != nil {
		return nil, fmt.Errorf("failed to get priority stats: %w", err)
	}
	defer priorityRows.Close()

	for priorityRows.Next() {
		var priority, count int
		if err := priorityRows.Scan(&priority, &count); err != nil {
			continue
		}
		stats.ByPriority[Priority(priority)] = count
	}
	if err := priorityRows.Err(); err != nil {
		return nil, fmt.Errorf("failed iterating priority stats: %w", err)
	}

	// Interactive split for pending jobs (tree 04 leaf 02): lets operators
	// see how much of the backlog is user-adjacent work.
	interactiveRows, err := s.db.Query(`SELECT interactive, COUNT(*) FROM jobs WHERE state = 'pending' GROUP BY interactive`)
	if err != nil {
		return nil, fmt.Errorf("failed to get interactive stats: %w", err)
	}
	defer interactiveRows.Close()

	for interactiveRows.Next() {
		var interactive, count int
		if err := interactiveRows.Scan(&interactive, &count); err != nil {
			continue
		}
		stats.ByInteractive[interactive != 0] = count
	}
	if err := interactiveRows.Err(); err != nil {
		return nil, fmt.Errorf("failed iterating interactive stats: %w", err)
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
	// ByInteractive splits pending jobs by the interactive flag (D11).
	ByInteractive map[bool]int
	DeadCount     int
}

// boolToInt maps a boolean to its 0/1 storage encoding in the jobs table.
func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
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
		0,              // Reset retry_count
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
		joinedAt, lastHb       int64
		signingPubRaw          []byte
		capabilities, endpoint string
		wireguardPub, nodeID   string
		nodeName, clusterIP    sql.NullString
		status                 string
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
	NodeID        string            `json:"node_id"`
	NodeName      string            `json:"node_name"`
	WireGuardPub  string            `json:"wireguard_pubkey"`
	SigningPub    ed25519.PublicKey `json:"signing_pubkey"`
	WireGuardKey  []byte            `json:"-"`
	Capabilities  []string          `json:"capabilities"`
	Endpoint      string            `json:"endpoint"`
	ClusterIP     string            `json:"cluster_ip"`
	JoinedAt      time.Time         `json:"joined_at"`
	LastHeartbeat time.Time         `json:"last_heartbeat"`
	Status        string            `json:"status"`
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
		interactive                      int
		createdAt, updatedAt             string
		dueAt, nextRetryAt, claimToken   sql.NullString
	)

	err := row.Scan(&id, &taskID, &agentID, &jobType, &priority, &state, &payload, &capsJSON,
		&maxRetries, &retryCount, &interactive, &claimedBy, &result, &errMsg, &createdAt, &updatedAt, &dueAt, &nextRetryAt, &claimToken)
	if err != nil {
		return nil, err
	}

	return s.buildJob(id, taskID, agentID, jobType, state, payload, capsJSON, priority, maxRetries, retryCount, interactive, claimedBy, result, errMsg, createdAt, updatedAt, dueAt, nextRetryAt, claimToken)
}

func (s *Store) scanJobRows(rows *sql.Rows) (*Job, error) {
	var (
		id, jobType, state, payload      string
		priority, maxRetries, retryCount int
		taskID, agentID, claimedBy       sql.NullString
		result, errMsg                   sql.NullString
		capsJSON                         string
		interactive                      int
		createdAt, updatedAt             string
		dueAt, nextRetryAt, claimToken   sql.NullString
	)

	err := rows.Scan(&id, &taskID, &agentID, &jobType, &priority, &state, &payload, &capsJSON,
		&maxRetries, &retryCount, &interactive, &claimedBy, &result, &errMsg, &createdAt, &updatedAt, &dueAt, &nextRetryAt, &claimToken)
	if err != nil {
		return nil, err
	}

	return s.buildJob(id, taskID, agentID, jobType, state, payload, capsJSON, priority, maxRetries, retryCount, interactive, claimedBy, result, errMsg, createdAt, updatedAt, dueAt, nextRetryAt, claimToken)
}

func (s *Store) buildJob(id string, taskID, agentID sql.NullString, jobType, state, payload, capsJSON string,
	priority, maxRetries, retryCount, interactive int, claimedBy, result, errMsg sql.NullString,
	createdAt, updatedAt string, dueAt, nextRetryAt, claimToken sql.NullString) (*Job, error) {

	job := &Job{
		ID:          id,
		Type:        JobType(jobType),
		State:       JobState(state),
		Payload:     json.RawMessage(payload),
		Priority:    Priority(priority),
		MaxRetries:  maxRetries,
		RetryCount:  retryCount,
		Interactive: interactive != 0,
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
	if claimToken.Valid {
		job.ClaimToken = claimToken.String
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

	// Fresh claim token for this attempt (audit H4) — same contract as
	// ClaimNextForAgent: the token is the attempt identity a completion
	// must present.
	claimToken := newClaimToken()

	// Try to claim the specific job
	result, err := tx.Exec(`
		UPDATE jobs SET state = 'claimed', claimed_by = ?, claim_token = ?, updated_at = ?
		WHERE id = ? AND state = 'pending'`,
		workerID, claimToken, now, jobID)
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
