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
// downstream consumers may compare bytes. claimant_* is the principal
// recorded at Claim. claimant_set=0 is a pre-claimant legacy row.
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
    receipt             TEXT,
    abandon_reason      TEXT,
    claimant_agent_id   TEXT NOT NULL DEFAULT '',
    claimant_session_id TEXT NOT NULL DEFAULT '',
    claimant_set        INTEGER NOT NULL DEFAULT 0
);

CREATE INDEX IF NOT EXISTS idx_effects_ledger_state
    ON effects_ledger(state);
`

// sqliteLedgerPragma is executed once at construction, after the schema
// migration. WAL mode persists in the database file, so journal mode is
// set here rather than (only) via DSN parameters.
const sqliteLedgerPragma = `PRAGMA journal_mode=WAL;`

const effectSelectCols = `key, state, task_id, step_id, session_id, tool,
       provider_idempotent, claimed_at, executed_at, completed_at,
       payload, receipt, abandon_reason,
       claimant_agent_id, claimant_session_id, claimant_set`

// SQLiteLedger is the durable Ledger over <data_dir>/effects.db.
type SQLiteLedger struct {
	db     *sql.DB
	logger *slog.Logger
}

// NewSQLiteLedger opens (and migrates) the effects_ledger table on the
// database at dbPath. The DSN uses `_pragma=`-style parameters —
// modernc.org/sqlite v1.50.1 only honors that form (same convention as
// the queue/task/park stores) — and WAL is additionally enforced by
// sqliteLedgerPragma, with single-writer serialization (the
// busy_timeout equivalent for in-process contention) by capping the
// pool at one connection: concurrent Claims of one key then serialize
// on the single connection instead of failing with SQLITE_BUSY.
func NewSQLiteLedger(dbPath string, logger *slog.Logger) (*SQLiteLedger, error) {
	if logger == nil {
		logger = slog.Default()
	}
	db, err := sql.Open("sqlite", dbPath+"?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)&_pragma=synchronous(NORMAL)")
	if err != nil {
		return nil, fmt.Errorf("effects ledger: failed to open database: %w", err)
	}
	db.SetMaxOpenConns(1)
	if _, err := db.Exec(effectsLedgerSchema + sqliteLedgerPragma); err != nil {
		db.Close()
		return nil, fmt.Errorf("effects ledger: failed to migrate effects_ledger: %w", err)
	}
	// In-place column addition for pre-existing databases (CREATE TABLE IF
	// NOT EXISTS does not alter an existing table). Duplicate-add errors
	// are the expected steady state on migrated DBs.
	for _, stmt := range []string{
		`ALTER TABLE effects_ledger ADD COLUMN abandon_reason TEXT`,
		`ALTER TABLE effects_ledger ADD COLUMN claimant_agent_id TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE effects_ledger ADD COLUMN claimant_session_id TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE effects_ledger ADD COLUMN claimant_set INTEGER NOT NULL DEFAULT 0`,
	} {
		if _, err := db.Exec(stmt); err != nil {
			s := fmt.Sprintf("%v", err)
			if !strings.Contains(s, "duplicate column name") {
				db.Close()
				return nil, fmt.Errorf("effects ledger: failed to migrate column: %w", err)
			}
		}
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
// INSERT; on unique-constraint failure a legacy row (claimant_set=0) is
// adopted by who (first claim wins) and the prior row is returned with
// granted=false. Never a check-then-insert. A zero claimant is rejected.
func (s *SQLiteLedger) Claim(ctx context.Context, key string, meta EffectMeta, who Claimant) (bool, *EffectRecord, error) {
	if who.Zero() {
		return false, nil, fmt.Errorf("effects ledger: claim %s: %w", key, ErrClaimantRequired)
	}
	const insert = `
INSERT INTO effects_ledger (
    key, state, task_id, step_id, session_id, tool,
    provider_idempotent, claimed_at, payload,
    claimant_agent_id, claimant_session_id, claimant_set
) VALUES (?, 'claimed', ?, ?, ?, ?, ?, ?, ?, ?, ?, 1)`
	now := time.Now().UTC().Format(time.RFC3339Nano)
	if _, err := s.db.ExecContext(ctx, insert,
		key, meta.TaskID, meta.StepID, meta.SessionID, meta.Tool,
		boolToInt(meta.ProviderIdempotent), now, rawToText(meta.Payload),
		who.AgentID, who.SessionID,
	); err != nil {
		if !strings.Contains(err.Error(), "UNIQUE constraint failed") {
			return false, nil, fmt.Errorf("effects ledger: insert claim for %s: %w", key, err)
		}
		// First claim of an unclaimed-legacy row wins ownership. The
		// conditional UPDATE is the race: exactly one adopter stamps
		// the claimant. granted stays false — the effect was already
		// claimed and must not be re-executed.
		const adopt = `
UPDATE effects_ledger
SET claimant_agent_id = ?, claimant_session_id = ?, claimant_set = 1
WHERE key = ? AND claimant_set = 0`
		if _, adoptErr := s.db.ExecContext(ctx, adopt, who.AgentID, who.SessionID, key); adoptErr != nil {
			return false, nil, fmt.Errorf("effects ledger: adopt legacy claim for %s: %w", key, adoptErr)
		}
		prior, getErr := s.Get(ctx, key)
		if getErr != nil {
			return false, nil, fmt.Errorf("effects ledger: read prior after claim conflict for %s: %w", key, getErr)
		}
		return false, prior, nil
	}
	return true, nil, nil
}

// RecordReceipt implements Ledger: claimed -> receipted, claimant must match.
func (s *SQLiteLedger) RecordReceipt(ctx context.Context, key string, who Claimant, receipt json.RawMessage) error {
	return s.recordReceipt(ctx, key, who, false, receipt)
}

// RecordReceiptAdmin implements Ledger. Operator override; skips the claimant check.
func (s *SQLiteLedger) RecordReceiptAdmin(ctx context.Context, key string, receipt json.RawMessage) error {
	return s.recordReceipt(ctx, key, Claimant{}, true, receipt)
}

func (s *SQLiteLedger) recordReceipt(ctx context.Context, key string, who Claimant, admin bool, receipt json.RawMessage) error {
	const update = `
UPDATE effects_ledger
SET state = 'receipted', receipt = ?, executed_at = ?
WHERE key = ? AND state = 'claimed'`
	now := time.Now().UTC().Format(time.RFC3339Nano)
	// Claimant is checked before the write so a mismatch cannot stamp a
	// receipt. The conditional UPDATE still owns the state transition.
	if err := s.requireResolve(ctx, key, who, admin, "record_receipt"); err != nil {
		return err
	}
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
// stamped. Re-Complete by the owning claimant of an already-completed
// record is nil (idempotent). A different claimant is a mismatch.
func (s *SQLiteLedger) Complete(ctx context.Context, key string, who Claimant) error {
	return s.complete(ctx, key, who, false)
}

// CompleteAdmin implements Ledger. Operator override; skips the claimant check.
func (s *SQLiteLedger) CompleteAdmin(ctx context.Context, key string) error {
	return s.complete(ctx, key, Claimant{}, true)
}

func (s *SQLiteLedger) complete(ctx context.Context, key string, who Claimant, admin bool) error {
	const update = `
UPDATE effects_ledger
SET state = 'completed', completed_at = ?
WHERE key = ? AND state IN ('claimed','receipted')`
	if err := s.requireResolve(ctx, key, who, admin, "complete"); err != nil {
		// Idempotent re-complete is nil only for the owning claimant
		// (or admin). requireResolve already returns nil for that case
		// when the row is completed; any other error is final.
		return err
	}
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
		return s.transitionErrFor(ctx, key, "complete")
	}
	return nil
}

// Abandon implements Ledger: claimed/receipted -> abandoned. The reason is
// persisted on the row (abandon_reason) for CLI/human reconciliation.
func (s *SQLiteLedger) Abandon(ctx context.Context, key string, who Claimant, reason string) error {
	return s.abandon(ctx, key, who, false, reason)
}

// AbandonAdmin implements Ledger. Operator override; skips the claimant check.
func (s *SQLiteLedger) AbandonAdmin(ctx context.Context, key string, reason string) error {
	return s.abandon(ctx, key, Claimant{}, true, reason)
}

func (s *SQLiteLedger) abandon(ctx context.Context, key string, who Claimant, admin bool, reason string) error {
	const update = `
UPDATE effects_ledger
SET state = 'abandoned', abandon_reason = ?
WHERE key = ? AND state IN ('claimed','receipted')`
	if err := s.requireResolve(ctx, key, who, admin, "abandon"); err != nil {
		return err
	}
	res, err := s.db.ExecContext(ctx, update, reason, key)
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

// requireResolve reads the row and rejects a mismatched claimant before
// any write. A completed row and action "complete" is the idempotent
// success path for the owner (and for admin). Unknown key stays
// ErrUnknownKey even when who is zero.
func (s *SQLiteLedger) requireResolve(ctx context.Context, key string, who Claimant, admin bool, action string) error {
	rec, getErr := s.Get(ctx, key)
	if getErr != nil {
		if errors.Is(getErr, ErrUnknownKey) {
			return fmt.Errorf("effects ledger: %s %s: %w", action, key, ErrUnknownKey)
		}
		return fmt.Errorf("effects ledger: read record for %s %s: %w", action, key, getErr)
	}
	if !admin {
		if err := authorizeResolve(rec, who); err != nil {
			return fmt.Errorf("effects ledger: %s %s: %w", action, key, err)
		}
	}
	if action == "complete" && rec.State == StateCompleted {
		return nil
	}
	if err := validateTransition(rec.State, action); err != nil {
		return fmt.Errorf("effects ledger: %s %s: %w", action, key, err)
	}
	return nil
}

// ReconcilePending implements Ledger: every record in state claimed or
// receipted, oldest claimed_at first.
func (s *SQLiteLedger) ReconcilePending(ctx context.Context) ([]EffectRecord, error) {
	query := `
SELECT ` + effectSelectCols + `
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
	query := `
SELECT ` + effectSelectCols + `
FROM effects_ledger
WHERE key = ?`
	row := s.db.QueryRowContext(ctx, query, key)
	rec, err := scanEffectRow(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("effects ledger: key %s: %w", key, ErrUnknownKey)
		}
		return nil, fmt.Errorf("effects ledger: read record %s: %w", key, err)
	}
	return rec, nil
}

// transitionErrFor maps a zero-rows conditional UPDATE to the pinned
// errors by re-reading the row (unknown key vs invalid transition).
// Claimant was already checked by requireResolve; a lost race that
// changed state surfaces the state error.
func (s *SQLiteLedger) transitionErrFor(ctx context.Context, key, action string) error {
	rec, getErr := s.Get(ctx, key)
	if getErr != nil {
		if errors.Is(getErr, ErrUnknownKey) {
			return fmt.Errorf("effects ledger: %s %s: %w", action, key, ErrUnknownKey)
		}
		return fmt.Errorf("effects ledger: read record for %s %s: %w", action, key, getErr)
	}
	if action == "complete" && rec.State == StateCompleted {
		return nil
	}
	return fmt.Errorf("effects ledger: %s %s: %w", action, key, validateTransition(rec.State, action))
}

// rowScanner is the subset of *sql.Row and *sql.Rows used by the shared scan.
type rowScanner interface {
	Scan(dest ...any) error
}

// scanEffectRecord scans one row of the shared SELECT.
func scanEffectRecord(rows *sql.Rows) (*EffectRecord, error) {
	return scanEffectRow(rows)
}

func scanEffectRow(row rowScanner) (*EffectRecord, error) {
	var rec EffectRecord
	var claimedAt string
	var executedAt, completedAt, payload, receipt, abandonReason sql.NullString
	var claimantAgent, claimantSession sql.NullString
	var providerIdempotent, claimantSet int
	if err := row.Scan(
		&rec.Key, &rec.State, &rec.TaskID, &rec.StepID, &rec.SessionID, &rec.Tool,
		&providerIdempotent, &claimedAt, &executedAt, &completedAt,
		&payload, &receipt, &abandonReason,
		&claimantAgent, &claimantSession, &claimantSet,
	); err != nil {
		return nil, err
	}
	out, err := buildRecord(rec, providerIdempotent, claimedAt, executedAt, completedAt, payload, receipt, abandonReason)
	if err != nil {
		return nil, err
	}
	out.ClaimantAgentID = claimantAgent.String
	out.ClaimantSessionID = claimantSession.String
	out.ClaimantSet = claimantSet != 0
	return out, nil
}

// buildRecord assembles the EffectRecord from raw scanned columns.
func buildRecord(rec EffectRecord, providerIdempotent int, claimedAt string,
	executedAt, completedAt, payload, receipt, abandonReason sql.NullString) (*EffectRecord, error) {
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
	rec.AbandonReason = abandonReason.String
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
