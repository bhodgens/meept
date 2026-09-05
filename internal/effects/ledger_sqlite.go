package effects

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

// effectsLedgerSchema creates the effect-ledger table. Times are RFC3339
// TEXT (same convention as parked_turns.resume_at / sessions.created_at);
// payload and receipt are the raw JSON preserved byte-for-byte so
// downstream consumers may compare bytes.
const effectsLedgerSchema = `
CREATE TABLE IF NOT EXISTS effects_ledger (
    key                 TEXT PRIMARY KEY,
    state               TEXT NOT NULL CHECK (state IN
                          ('claimed','receipted','completed','abandoned')),
    task_id             TEXT NOT NULL DEFAULT '',
    step_id             TEXT NOT NULL DEFAULT '',
    session_id          TEXT NOT NULL DEFAULT '',
    tool                TEXT NOT NULL DEFAULT '',
    provider_idempotent INTEGER NOT NULL DEFAULT 0,
    claimed_at          TEXT NOT NULL,
    executed_at         TEXT,
    completed_at        TEXT,
    payload             TEXT,
    receipt             TEXT
);

CREATE INDEX IF NOT EXISTS idx_effects_ledger_state
    ON effects_ledger(state);
`

// sqliteLedgerPragma is executed once at construction, after the schema
// migration. WAL mode persists in the database file, so journal mode is
// set here rather than (only) via DSN parameters.
const sqliteLedgerPragma = `PRAGMA journal_mode=WAL;`

// SQLiteLedger is the durable Ledger over <data_dir>/effects.db.
type SQLiteLedger struct {
	db     *sql.DB
	logger *slog.Logger
}

// NewSQLiteLedger opens (and migrates) the effects_ledger table on the
// database at dbPath. The DSN matches the park store's convention; note
// that modernc.org/sqlite v1.50.1 only honors `_pragma=`-style DSN
// parameters, so WAL is additionally enforced by sqliteLedgerPragma and
// single-writer serialization (the busy_timeout equivalent) by capping the
// pool at one connection — concurrent Claims of one key then serialize on
// the single connection instead of failing with SQLITE_BUSY.
func NewSQLiteLedger(dbPath string, logger *slog.Logger) (*SQLiteLedger, error) {
	if logger == nil {
		logger = slog.Default()
	}
	db, err := sql.Open("sqlite", dbPath+"?_journal_mode=WAL&_busy_timeout=5000")
	if err != nil {
		return nil, fmt.Errorf("effects ledger: failed to open database: %w", err)
	}
	db.SetMaxOpenConns(1)
	if _, err := db.Exec(effectsLedgerSchema + sqliteLedgerPragma); err != nil {
		db.Close()
		return nil, fmt.Errorf("effects ledger: failed to migrate effects_ledger: %w", err)
	}
	logger.Info("effects ledger initialized", "path", dbPath)
	return &SQLiteLedger{db: db, logger: logger}, nil
}

// Close implements Ledger.
func (s *SQLiteLedger) Close() error {
	if s == nil || s.db == nil {
		return nil
	}
	return s.db.Close()
}

// Claim implements Ledger. Atomicity comes from the PRIMARY KEY: a plain
// INSERT; on unique-constraint failure the prior row is read and returned
// with granted=false. Never a check-then-insert.
func (s *SQLiteLedger) Claim(ctx context.Context, key string, meta EffectMeta) (bool, *EffectRecord, error) {
	const insert = `
INSERT INTO effects_ledger (
    key, state, task_id, step_id, session_id, tool,
    provider_idempotent, claimed_at, payload
) VALUES (?, 'claimed', ?, ?, ?, ?, ?, ?, ?)`
	now := time.Now().UTC().Format(time.RFC3339Nano)
	if _, err := s.db.ExecContext(ctx, insert,
		key, meta.TaskID, meta.StepID, meta.SessionID, meta.Tool,
		boolToInt(meta.ProviderIdempotent), now, rawToText(meta.Payload),
	); err != nil {
		if !strings.Contains(err.Error(), "UNIQUE constraint failed") {
			return false, nil, fmt.Errorf("effects ledger: insert claim for %s: %w", key, err)
		}
		prior, getErr := s.Get(ctx, key)
		if getErr != nil {
			return false, nil, fmt.Errorf("effects ledger: read prior after claim conflict for %s: %w", key, getErr)
		}
		return false, prior, nil
	}
	return true, nil, nil
}

// RecordReceipt implements Ledger: claimed -> receipted, receipt and
// executed_at stamped. RowsAffected==0 distinguishes unknown key from
// invalid transition by re-reading the row.
func (s *SQLiteLedger) RecordReceipt(ctx context.Context, key string, receipt json.RawMessage) error {
	const update = `
UPDATE effects_ledger
SET state = 'receipted', receipt = ?, executed_at = ?
WHERE key = ? AND state = 'claimed'`
	now := time.Now().UTC().Format(time.RFC3339Nano)
	res, err := s.db.ExecContext(ctx, update, rawToText(receipt), now, key)
	if err != nil {
		return fmt.Errorf("effects ledger: record receipt for %s: %w", key, err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("effects ledger: rows affected for %s: %w", key, err)
	}
	if n == 0 {
		return s.transitionErrFor(ctx, key, "record_receipt")
	}
	return nil
}

// Complete implements Ledger: claimed/receipted -> completed, completed_at
// stamped. Re-Complete of an already-completed record is nil (idempotent).
func (s *SQLiteLedger) Complete(ctx context.Context, key string) error {
	const update = `
UPDATE effects_ledger
SET state = 'completed', completed_at = ?
WHERE key = ? AND state IN ('claimed','receipted')`
	now := time.Now().UTC().Format(time.RFC3339Nano)
	res, err := s.db.ExecContext(ctx, update, now, key)
	if err != nil {
		return fmt.Errorf("effects ledger: complete %s: %w", key, err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("effects ledger: rows affected for %s: %w", key, err)
	}
	if n == 0 {
		rec, getErr := s.Get(ctx, key)
		if getErr != nil {
			if errors.Is(getErr, ErrUnknownKey) {
				return fmt.Errorf("effects ledger: complete %s: %w", key, ErrUnknownKey)
			}
			return fmt.Errorf("effects ledger: read record for complete %s: %w", key, getErr)
		}
		if rec.State == StateCompleted {
			return nil // idempotent re-complete
		}
		return fmt.Errorf("effects ledger: complete %s: %w", key, validateTransition(rec.State, "complete"))
	}
	return nil
}

// Abandon implements Ledger: claimed/receipted -> abandoned.
func (s *SQLiteLedger) Abandon(ctx context.Context, key string, reason string) error {
	// reason is accepted for the pinned surface; the abandoned row itself
	// is terminal, so there is nowhere durable to fold it yet.
	const update = `
UPDATE effects_ledger
SET state = 'abandoned'
WHERE key = ? AND state IN ('claimed','receipted')`
	res, err := s.db.ExecContext(ctx, update, key)
	if err != nil {
		return fmt.Errorf("effects ledger: abandon %s: %w", key, err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("effects ledger: rows affected for %s: %w", key, err)
	}
	if n == 0 {
		return s.transitionErrFor(ctx, key, "abandon")
	}
	return nil
}

// ReconcilePending implements Ledger: every record in state claimed or
// receipted, oldest claimed_at first.
func (s *SQLiteLedger) ReconcilePending(ctx context.Context) ([]EffectRecord, error) {
	const query = `
SELECT key, state, task_id, step_id, session_id, tool,
       provider_idempotent, claimed_at, executed_at, completed_at,
       payload, receipt
FROM effects_ledger
WHERE state IN ('claimed','receipted')
ORDER BY claimed_at ASC`
	rows, err := s.db.QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("effects ledger: reconcile query: %w", err)
	}
	defer rows.Close()
	var pending []EffectRecord
	for rows.Next() {
		rec, err := scanEffectRecord(rows)
		if err != nil {
			return nil, fmt.Errorf("effects ledger: scan pending record: %w", err)
		}
		pending = append(pending, *rec)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("effects ledger: iterate pending records: %w", err)
	}
	return pending, nil
}

// Get implements Ledger.
func (s *SQLiteLedger) Get(ctx context.Context, key string) (*EffectRecord, error) {
	const query = `
SELECT key, state, task_id, step_id, session_id, tool,
       provider_idempotent, claimed_at, executed_at, completed_at,
       payload, receipt
FROM effects_ledger
WHERE key = ?`
	var rec EffectRecord
	var claimedAt string
	var executedAt, completedAt, payload, receipt sql.NullString
	var providerIdempotent int
	err := s.db.QueryRowContext(ctx, query, key).Scan(
		&rec.Key, &rec.State, &rec.TaskID, &rec.StepID, &rec.SessionID, &rec.Tool,
		&providerIdempotent, &claimedAt, &executedAt, &completedAt,
		&payload, &receipt,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("effects ledger: key %s: %w", key, ErrUnknownKey)
		}
		return nil, fmt.Errorf("effects ledger: read record %s: %w", key, err)
	}
	out, err := buildRecord(rec, providerIdempotent, claimedAt, executedAt, completedAt, payload, receipt)
	if err != nil {
		return nil, fmt.Errorf("effects ledger: record %s: %w", key, err)
	}
	return out, nil
}

// transitionErrFor maps a zero-rows conditional UPDATE to the pinned
// errors by re-reading the row (unknown key vs invalid transition).
func (s *SQLiteLedger) transitionErrFor(ctx context.Context, key, action string) error {
	rec, getErr := s.Get(ctx, key)
	if getErr != nil {
		if errors.Is(getErr, ErrUnknownKey) {
			return fmt.Errorf("effects ledger: %s %s: %w", action, key, ErrUnknownKey)
		}
		return fmt.Errorf("effects ledger: read record for %s %s: %w", action, key, getErr)
	}
	return fmt.Errorf("effects ledger: %s %s: %w", action, key, validateTransition(rec.State, action))
}

// scanEffectRecord scans one row of the shared 12-column SELECT.
func scanEffectRecord(rows *sql.Rows) (*EffectRecord, error) {
	var rec EffectRecord
	var claimedAt string
	var executedAt, completedAt, payload, receipt sql.NullString
	var providerIdempotent int
	if err := rows.Scan(
		&rec.Key, &rec.State, &rec.TaskID, &rec.StepID, &rec.SessionID, &rec.Tool,
		&providerIdempotent, &claimedAt, &executedAt, &completedAt,
		&payload, &receipt,
	); err != nil {
		return nil, err
	}
	return buildRecord(rec, providerIdempotent, claimedAt, executedAt, completedAt, payload, receipt)
}

// buildRecord assembles the EffectRecord from raw scanned columns.
func buildRecord(rec EffectRecord, providerIdempotent int, claimedAt string,
	executedAt, completedAt, payload, receipt sql.NullString) (*EffectRecord, error) {
	claimed, err := time.Parse(time.RFC3339Nano, claimedAt)
	if err != nil {
		return nil, fmt.Errorf("parse claimed_at %q: %w", claimedAt, err)
	}
	rec.ClaimedAt = claimed
	rec.ProviderIdempotent = providerIdempotent != 0
	if executedAt.Valid && executedAt.String != "" {
		t, err := time.Parse(time.RFC3339Nano, executedAt.String)
		if err != nil {
			return nil, fmt.Errorf("parse executed_at %q: %w", executedAt.String, err)
		}
		rec.ExecutedAt = &t
	}
	if completedAt.Valid && completedAt.String != "" {
		t, err := time.Parse(time.RFC3339Nano, completedAt.String)
		if err != nil {
			return nil, fmt.Errorf("parse completed_at %q: %w", completedAt.String, err)
		}
		rec.CompletedAt = &t
	}
	rec.Payload = textToRaw(payload)
	rec.Receipt = textToRaw(receipt)
	return &rec, nil
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

// rawToText preserves the JSON bytes verbatim (no re-encoding).
func rawToText(raw json.RawMessage) any {
	if raw == nil {
		return nil
	}
	return string(raw)
}

func textToRaw(ns sql.NullString) json.RawMessage {
	if !ns.Valid || ns.String == "" {
		return nil
	}
	return json.RawMessage(ns.String)
}
