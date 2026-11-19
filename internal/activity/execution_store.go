package activity

// execution_store.go - durable execution-run history (issue #160 follow-up).
//
// The live WS relay (bot.result / employee.result) is fire-and-forget: a
// client that disconnects misses runs forever. This store is the rebuildable
// projection that makes runs queryable after reconnect AND after daemon
// restart. It subscribes to the same two typed topics the WS relay uses and
// appends one row per execution. Bounded retention keeps the table small.
//
// SQLite conventions match internal/effects/ledger_sqlite.go: modernc-style
// `_pragma=` DSN parameters (mattn keys are silently dropped by
// modernc.org/sqlite - audit-sqlite-dsn-pragmas.py), WAL enforced by an
// explicit PRAGMA, and MaxOpenConns(1) for single-writer serialization.

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

// ExecutionRun is one persisted bot/employee execution result. Field keys
// mirror agent.ExecutionResultEvent plus the tier discriminator derived from
// the source topic (the wire payload itself carries no kind field).
type ExecutionRun struct {
	ID         int64     `json:"id"`
	Tier       string    `json:"tier"` // "bot" | "employee"
	AgentID    string    `json:"agent_id"`
	Status     string    `json:"status"` // completed | failed | skipped
	Output     string    `json:"output,omitempty"`
	TokensUsed int       `json:"tokens_used"`
	Error      string    `json:"error,omitempty"`
	DurationMS int64     `json:"duration_ms"`
	StopReason string    `json:"stop_reason,omitempty"`
	TriggerRef string    `json:"trigger_ref,omitempty"`
	SessionID  string    `json:"session_id,omitempty"`
	CreatedAt  time.Time `json:"created_at"`
}

// ExecutionQuery filters a List call. Zero values mean unfiltered.
type ExecutionQuery struct {
	AgentID string    // exact match when non-empty
	Since   time.Time // only runs at/after this instant when non-zero
	Limit   int       // <=0 defaults to 50; capped at 1000
}

const executionRunsSchema = `
CREATE TABLE IF NOT EXISTS execution_runs (
	id          INTEGER PRIMARY KEY AUTOINCREMENT,
	tier        TEXT NOT NULL,
	agent_id    TEXT NOT NULL,
	status      TEXT NOT NULL,
	output      TEXT NOT NULL DEFAULT '',
	tokens_used INTEGER NOT NULL DEFAULT 0,
	error       TEXT NOT NULL DEFAULT '',
	duration_ms INTEGER NOT NULL DEFAULT 0,
	stop_reason TEXT NOT NULL DEFAULT '',
	trigger_ref TEXT NOT NULL DEFAULT '',
	session_id  TEXT NOT NULL DEFAULT '',
	created_at  TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_execution_runs_agent_time ON execution_runs(agent_id, created_at);
CREATE INDEX IF NOT EXISTS idx_execution_runs_time ON execution_runs(created_at);
`

const executionRunsPragma = `PRAGMA journal_mode=WAL;`

// executionRetention bounds the table: oldest rows beyond the cap are
// deleted on each append. 5000 rows is ~a week of heavy fleet activity and
// keeps the file small; the GUI/TUI only ever page the newest slice.
const executionRetention = 5000

// ExecutionStore is the durable execution-run projection.
type ExecutionStore struct {
	db     *sql.DB
	logger *slog.Logger
}

// OpenExecutionStore opens (and migrates) the execution_runs table at
// dbPath. A nil logger falls back to slog.Default().
func OpenExecutionStore(dbPath string, logger *slog.Logger) (*ExecutionStore, error) {
	if logger == nil {
		logger = slog.Default()
	}
	db, err := sql.Open("sqlite", dbPath+"?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)&_pragma=synchronous(NORMAL)")
	if err != nil {
		return nil, fmt.Errorf("execution store: open database: %w", err)
	}
	db.SetMaxOpenConns(1)
	if _, err := db.Exec(executionRunsSchema + executionRunsPragma); err != nil {
		if closeErr := db.Close(); closeErr != nil {
			logger.Warn("execution store: close after schema failure", "error", closeErr)
		}
		return nil, fmt.Errorf("execution store: migrate execution_runs: %w", err)
	}
	return &ExecutionStore{db: db, logger: logger}, nil
}

// Close releases the database handle.
func (s *ExecutionStore) Close() error {
	if s == nil || s.db == nil {
		return nil
	}
	return s.db.Close()
}

// Append persists one run and trims the table to the retention cap.
// CreatedAt is stamped server-side (UTC, second precision) so ordering is
// stable regardless of client clocks.
func (s *ExecutionStore) Append(ctx context.Context, run ExecutionRun) error {
	if s == nil || s.db == nil {
		return errors.New("execution store: not configured")
	}
	if run.CreatedAt.IsZero() {
		run.CreatedAt = time.Now().UTC()
	}
	run.CreatedAt = run.CreatedAt.UTC().Truncate(time.Second)
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO execution_runs
		 (tier, agent_id, status, output, tokens_used, error, duration_ms, stop_reason, trigger_ref, session_id, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		run.Tier, run.AgentID, run.Status, run.Output, run.TokensUsed, run.Error,
		run.DurationMS, run.StopReason, run.TriggerRef, run.SessionID,
		run.CreatedAt.Format(time.RFC3339))
	if err != nil {
		return fmt.Errorf("execution store: insert run: %w", err)
	}
	// Retention trim: delete everything older than the newest N rows.
	if _, err := s.db.ExecContext(ctx,
		`DELETE FROM execution_runs WHERE id <= (SELECT MAX(id) FROM execution_runs) - ?`,
		executionRetention); err != nil {
		s.logger.Warn("execution store: retention trim failed", "error", err)
	}
	return nil
}

// List returns the newest runs matching q, newest first, plus the total
// count of matching rows BEFORE the limit is applied (paging parity with
// activity.query).
func (s *ExecutionStore) List(ctx context.Context, q ExecutionQuery) ([]ExecutionRun, int, error) {
	if s == nil || s.db == nil {
		return nil, 0, errors.New("execution store: not configured")
	}
	limit := q.Limit
	if limit <= 0 {
		limit = 50
	}
	if limit > 1000 {
		limit = 1000
	}

	var where []string
	var args []any
	if q.AgentID != "" {
		where = append(where, "agent_id = ?")
		args = append(args, q.AgentID)
	}
	if !q.Since.IsZero() {
		where = append(where, "created_at >= ?")
		args = append(args, q.Since.UTC().Truncate(time.Second).Format(time.RFC3339))
	}
	clause := ""
	if len(where) > 0 {
		clause = " WHERE " + strings.Join(where, " AND ")
	}

	var total int
	if err := s.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM execution_runs"+clause, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("execution store: count runs: %w", err)
	}

	listArgs := append(append([]any{}, args...), limit)
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, tier, agent_id, status, output, tokens_used, error, duration_ms, stop_reason, trigger_ref, session_id, created_at
		 FROM execution_runs`+clause+` ORDER BY id DESC LIMIT ?`, listArgs...)
	if err != nil {
		return nil, 0, fmt.Errorf("execution store: query runs: %w", err)
	}
	defer rows.Close()

	out := make([]ExecutionRun, 0)
	for rows.Next() {
		var r ExecutionRun
		var created string
		if err := rows.Scan(&r.ID, &r.Tier, &r.AgentID, &r.Status, &r.Output,
			&r.TokensUsed, &r.Error, &r.DurationMS, &r.StopReason, &r.TriggerRef,
			&r.SessionID, &created); err != nil {
			return nil, 0, fmt.Errorf("execution store: scan run: %w", err)
		}
		at, parseErr := time.Parse(time.RFC3339, created)
		if parseErr != nil {
			s.logger.Warn("execution store: unparseable created_at; using zero time", "value", created, "error", parseErr)
		}
		r.CreatedAt = at
		out = append(out, r)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("execution store: iterate runs: %w", err)
	}
	return out, total, nil
}

// Histogram buckets the last hour of runs for one agent into 8 cells
// (7.5 minutes each), oldest cell first. Each cell is the summed tokens of
// runs whose created_at falls in that window. Used by the GUI's 8-bit
// token-activity bars; runs outside the hour are ignored.
func (s *ExecutionStore) Histogram(ctx context.Context, agentID string, now time.Time) ([8]int, error) {
	var cells [8]int
	if s == nil || s.db == nil {
		return cells, errors.New("execution store: not configured")
	}
	if now.IsZero() {
		now = time.Now()
	}
	now = now.UTC()
	windowStart := now.Add(-time.Hour)
	rows, err := s.db.QueryContext(ctx,
		`SELECT created_at, tokens_used FROM execution_runs
		 WHERE agent_id = ? AND created_at >= ?`,
		agentID, windowStart.Truncate(time.Second).Format(time.RFC3339))
	if err != nil {
		return cells, fmt.Errorf("execution store: histogram query: %w", err)
	}
	defer rows.Close()
	const cellSpan = time.Hour / 8
	for rows.Next() {
		var created string
		var tokens int
		if err := rows.Scan(&created, &tokens); err != nil {
			return cells, fmt.Errorf("execution store: histogram scan: %w", err)
		}
		at, err := time.Parse(time.RFC3339, created)
		if err != nil {
			continue
		}
		idx := int(at.Sub(windowStart) / cellSpan)
		if idx < 0 {
			idx = 0
		}
		if idx > 7 {
			idx = 7
		}
		cells[idx] += tokens
	}
	if err := rows.Err(); err != nil {
		return cells, fmt.Errorf("execution store: histogram iterate: %w", err)
	}
	return cells, nil
}
