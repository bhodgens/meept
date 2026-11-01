package turnjournal

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"time"

	_ "modernc.org/sqlite" // sqlite3 driver registration
)

// turnJournalSchema is the frozen turn_journal schema (master contract
// C1): one row per step-boundary event, indexed for the resume path's
// two queries (open runs, completed steps of a turn). The CHECK keeps
// the result blob honest: a step_completed row must carry a payload.
// created_at is Unix nanos.
const turnJournalSchema = `
CREATE TABLE IF NOT EXISTS turn_journal (
	id             INTEGER PRIMARY KEY AUTOINCREMENT,
	turn_id        TEXT NOT NULL,
	session_id     TEXT NOT NULL,
	step_id        TEXT NOT NULL,
	step_index     INTEGER NOT NULL,
	kind           TEXT NOT NULL,
	result_payload BLOB,
	created_at     INTEGER NOT NULL,
	CHECK (kind <> 'step_completed' OR result_payload IS NOT NULL)
);

CREATE INDEX IF NOT EXISTS idx_turn_journal_turn_step_kind
	ON turn_journal(turn_id, step_id, kind);
`

// SQLiteJournal is the durable step ledger over the daemon's
// turnjournal.db. Safe for concurrent use via database/sql.
type SQLiteJournal struct {
	db     *sql.DB
	logger *slog.Logger
}

// NewSQLiteJournal opens (and migrates) the turn_journal table on the
// database at dbPath. The DSN matches the park store (modernc.org/sqlite
// honors only `_pragma=`-style DSN parameters - a `_journal_mode=`-style
// key would be silently ignored, leaving WAL off): WAL journal + 5s busy
// timeout + NORMAL synchronous.
func NewSQLiteJournal(dbPath string, logger *slog.Logger) (*SQLiteJournal, error) {
	if logger == nil {
		logger = slog.Default()
	}
	db, err := sql.Open("sqlite", dbPath+"?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)&_pragma=synchronous(NORMAL)")
	if err != nil {
		return nil, fmt.Errorf("turnjournal: failed to open database: %w", err)
	}
	if _, err := db.Exec(turnJournalSchema); err != nil {
		db.Close()
		return nil, fmt.Errorf("turnjournal: failed to migrate turn_journal: %w", err)
	}
	logger.Info("turn journal initialized", "path", dbPath)
	return &SQLiteJournal{db: db, logger: logger}, nil
}

// Close releases the database handle.
func (j *SQLiteJournal) Close() error {
	if j == nil || j.db == nil {
		return nil
	}
	return j.db.Close()
}

// opTimeout bounds every journal statement; journal writers run on the
// turn's request path and must never block past this (park-store
// opTimeout pattern).
const opTimeout = 5 * time.Second

// bound applies the op timeout to a caller context.
func bound(ctx context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(ctx, opTimeout)
}

// Append records one boundary event. A step_completed row must carry a
// non-nil ResultPayload (schema CHECK); every other kind must not.
func (j *SQLiteJournal) Append(ctx context.Context, rec Record) error {
	if j == nil || j.db == nil {
		return fmt.Errorf("turnjournal: not initialized")
	}
	if rec.TurnID == "" {
		return fmt.Errorf("turnjournal: append: turn_id is required")
	}
	if !rec.Kind.valid() {
		return fmt.Errorf("turnjournal: append: unknown kind %q", string(rec.Kind))
	}
	if rec.Kind == KindStepCompleted && rec.ResultPayload == nil {
		return fmt.Errorf("turnjournal: append: step_completed requires a result payload")
	}

	ctx, cancel := bound(ctx)
	defer cancel()

	ts := rec.Timestamp
	if ts.IsZero() {
		ts = time.Now()
	}
	_, err := j.db.ExecContext(ctx, `
		INSERT INTO turn_journal (
			turn_id, session_id, step_id, step_index, kind, result_payload, created_at
		) VALUES (?, ?, ?, ?, ?, ?, ?)`,
		rec.TurnID,
		rec.SessionID,
		rec.StepID,
		rec.StepIndex,
		string(rec.Kind),
		rec.ResultPayload,
		ts.UnixNano(),
	)
	if err != nil {
		return fmt.Errorf("turnjournal: append %s/%s: %w", rec.TurnID, string(rec.Kind), err)
	}
	return nil
}

// OpenRuns returns every turn whose journal has no run_closed row, with
// the class payload from its run_started row. A run with multiple
// run_started rows (a legacy of an append retry) yields its newest
// payload.
func (j *SQLiteJournal) OpenRuns(ctx context.Context) ([]OpenRun, error) {
	if j == nil || j.db == nil {
		return nil, fmt.Errorf("turnjournal: not initialized")
	}

	ctx, cancel := bound(ctx)
	defer cancel()

	rows, err := j.db.QueryContext(ctx, `
		SELECT r.turn_id, MAX(r.session_id), r.result_payload
		FROM turn_journal r
		WHERE r.kind = 'run_started'
		  AND NOT EXISTS (
			SELECT 1 FROM turn_journal c
			WHERE c.turn_id = r.turn_id AND c.kind = 'run_closed'
		  )
		GROUP BY r.turn_id
	`)
	if err != nil {
		return nil, fmt.Errorf("turnjournal: open runs: %w", err)
	}
	defer rows.Close()

	var runs []OpenRun
	for rows.Next() {
		var (
			run     OpenRun
			payload []byte
		)
		if err := rows.Scan(&run.TurnID, &run.SessionID, &payload); err != nil {
			return nil, fmt.Errorf("turnjournal: open runs scan: %w", err)
		}
		run.TurnPayload = payload
		runs = append(runs, run)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("turnjournal: open runs rows: %w", err)
	}
	return runs, nil
}

// CompletedSteps maps step_id -> result payload for a turn's
// step_completed rows. The resume path consults this before each step
// (finding F3: a journaled result is returned, never re-invoked).
func (j *SQLiteJournal) CompletedSteps(ctx context.Context, turnID string) (map[string][]byte, error) {
	if j == nil || j.db == nil {
		return nil, fmt.Errorf("turnjournal: not initialized")
	}

	ctx, cancel := bound(ctx)
	defer cancel()

	rows, err := j.db.QueryContext(ctx, `
		SELECT step_id, result_payload
		FROM turn_journal
		WHERE turn_id = ? AND kind = 'step_completed'`,
		turnID,
	)
	if err != nil {
		return nil, fmt.Errorf("turnjournal: completed steps %s: %w", turnID, err)
	}
	defer rows.Close()

	steps := make(map[string][]byte)
	for rows.Next() {
		var (
			stepID  string
			payload []byte
		)
		if err := rows.Scan(&stepID, &payload); err != nil {
			return nil, fmt.Errorf("turnjournal: completed steps scan: %w", err)
		}
		steps[stepID] = payload
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("turnjournal: completed steps rows: %w", err)
	}
	return steps, nil
}

// CloseRun writes the run_closed row (terminal write of a completed
// job), removing the turn from OpenRuns.
func (j *SQLiteJournal) CloseRun(ctx context.Context, turnID string) error {
	if j == nil || j.db == nil {
		return fmt.Errorf("turnjournal: not initialized")
	}
	if turnID == "" {
		return fmt.Errorf("turnjournal: close run: turn_id is required")
	}

	ctx, cancel := bound(ctx)
	defer cancel()

	_, err := j.db.ExecContext(ctx, `
		INSERT INTO turn_journal (turn_id, session_id, step_id, step_index, kind, result_payload, created_at)
		VALUES (?, '', '', 0, 'run_closed', NULL, ?)`,
		turnID,
		time.Now().UnixNano(),
	)
	if err != nil {
		return fmt.Errorf("turnjournal: close run %s: %w", turnID, err)
	}
	return nil
}

// PruneClosed deletes every row of runs closed before the cutoff
// (finding F6: an append-only journal without retention is a slow
// leak). Open runs are never touched. Wired call sites are leaf 03's
// concern.
func (j *SQLiteJournal) PruneClosed(ctx context.Context, olderThan time.Duration) error {
	if j == nil || j.db == nil {
		return fmt.Errorf("turnjournal: not initialized")
	}

	ctx, cancel := bound(ctx)
	defer cancel()

	cutoff := time.Now().Add(-olderThan).UnixNano()
	res, err := j.db.ExecContext(ctx, `
		DELETE FROM turn_journal
		WHERE turn_id IN (
			SELECT turn_id FROM turn_journal
			WHERE kind = 'run_closed' AND created_at < ?
		)`,
		cutoff,
	)
	if err != nil {
		return fmt.Errorf("turnjournal: prune closed: %w", err)
	}
	if n, err := res.RowsAffected(); err == nil && n > 0 {
		j.logger.Debug("turn journal pruned closed runs", "rows", n, "older_than", olderThan.String())
	}
	return nil
}
