package voice

// Call session store (leaf 12): one SQLite row per phone call, keyed by
// call_control_id, carrying the bound meept session id and the full call
// transcript. The db lives at $MEEPT_HOME/voice/calls.db (the daemon wiring
// passes config.MeeptPath("voice", "calls.db")). The store implements leaf
// 04's CallTranscriptSource: TodayTranscripts returns prior calls for a
// number filtered to the LOCAL calendar day (never a UTC-derived day key,
// per contract C7).

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	_ "modernc.org/sqlite" // sqlite driver registration (house convention)
)

// ErrCallNotFound is returned by AppendTranscript when the call id has no
// row (a transcript line for an unknown or already-ended call).
var ErrCallNotFound = errors.New("voice callstore: call not found")

// callStoreSchema creates the voice_calls table. Times are RFC3339 TEXT
// (same convention as the effects ledger / sessions.created_at). The
// outcome column is NOT in the base schema: it is added by the guarded
// ALTER below, exercising the house migration idiom on a fresh database
// the same way it will run on pre-existing ones.
const callStoreSchema = `
CREATE TABLE IF NOT EXISTS voice_calls (
    call_id     TEXT PRIMARY KEY,
    number      TEXT NOT NULL DEFAULT '',
    employee_id TEXT NOT NULL DEFAULT '',
    direction   TEXT NOT NULL DEFAULT '',
    session_id  TEXT NOT NULL DEFAULT '',
    state       TEXT NOT NULL DEFAULT 'live',
    started_at  TEXT NOT NULL,
    ended_at    TEXT,
    transcript  TEXT NOT NULL DEFAULT '[]'
);

CREATE INDEX IF NOT EXISTS idx_voice_calls_number_started
    ON voice_calls(number, started_at);

CREATE INDEX IF NOT EXISTS idx_voice_calls_employee_session
    ON voice_calls(employee_id, session_id);
`

// CallRecord is one new call row. StartedAt zero means "now" (store clock).
type CallRecord struct {
	CallID     string
	Number     string
	EmployeeID string
	Direction  string // "incoming" | "outgoing"
	SessionID  string
	StartedAt  time.Time
}

// TranscriptLine is one entry of the call transcript JSON array.
type TranscriptLine struct {
	Role string    `json:"role"` // "user" (caller) | "assistant" (employee)
	Text string    `json:"text"`
	At   time.Time `json:"at"`
}

// CallStore is the SQLite call-session store. A single connection
// (SetMaxOpenConns(1)) serializes writers in-process, the same convention
// as the effects ledger; a package mutex guards the read-modify-write
// transcript append.
type CallStore struct {
	db     *sql.DB
	logger *slog.Logger
	loc    *time.Location

	nowFn func() time.Time // test seam: defaults to time.Now
}

// NewCallStore opens (and migrates) the call store at dbPath. loc is the
// local zone for the day filter; nil falls back to time.Local.
func NewCallStore(dbPath string, logger *slog.Logger) (*CallStore, error) {
	if logger == nil {
		logger = slog.Default()
	}
	db, err := sql.Open("sqlite", dbPath+"?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)&_pragma=synchronous(NORMAL)")
	if err != nil {
		return nil, fmt.Errorf("voice callstore: failed to open database: %w", err)
	}
	db.SetMaxOpenConns(1)
	if _, err := db.Exec(callStoreSchema); err != nil {
		db.Close()
		return nil, fmt.Errorf("voice callstore: failed to migrate voice_calls: %w", err)
	}
	s := &CallStore{
		db:     db,
		logger: logger,
		loc:    time.Local,
		nowFn:  time.Now,
	}
	// Guarded-ALTER migration idiom (session store migrationAddColumn /
	// effects ledger abandon_reason precedent): duplicate-column errors are
	// the expected steady state on migrated databases; anything else is
	// fatal.
	s.migrationAddColumn(`ALTER TABLE voice_calls ADD COLUMN outcome TEXT DEFAULT ''`, "outcome")
	logger.Info("voice call store initialized", "path", dbPath)
	return s, nil
}

// migrationAddColumn runs one ALTER TABLE, tolerating only the
// duplicate-column error that steady-state reruns produce.
func (s *CallStore) migrationAddColumn(stmt, columnName string) {
	if _, err := s.db.Exec(stmt); err != nil {
		// Duplicate-column is the expected steady state on migrated
		// databases; any other ALTER failure means the column is missing
		// and later queries will fail on it, so surface it loudly.
		if !strings.Contains(err.Error(), "duplicate column name") {
			s.logger.Error("voice callstore: column migration failed", "column", columnName, "error", err)
		}
	}
}

// Close closes the underlying database.
func (s *CallStore) Close() error {
	if s == nil || s.db == nil {
		return nil
	}
	return s.db.Close()
}

// Start inserts the call row for a live call. A duplicate call_id is an
// error: one row per call_control_id, matching the manager's once-only
// release semantics.
func (s *CallStore) Start(ctx context.Context, rec CallRecord) error {
	if rec.CallID == "" {
		return errors.New("voice callstore: start: call_id is required")
	}
	startedAt := rec.StartedAt
	if startedAt.IsZero() {
		startedAt = s.nowFn()
	}
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO voice_calls (call_id, number, employee_id, direction, session_id, state, started_at, ended_at, transcript, outcome)
		 VALUES (?, ?, ?, ?, ?, 'live', ?, NULL, '[]', '')`,
		rec.CallID, rec.Number, rec.EmployeeID, rec.Direction, rec.SessionID,
		startedAt.UTC().Format(time.RFC3339Nano),
	)
	if err != nil {
		return fmt.Errorf("voice callstore: start call %s: %w", rec.CallID, err)
	}
	return nil
}

// End marks the call terminal: state 'ended', ended_at now, outcome set.
// Unknown call ids are a no-op (idempotent teardown, matching the manager).
func (s *CallStore) End(ctx context.Context, callID string, outcome string) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE voice_calls SET state = 'ended', ended_at = ?, outcome = ? WHERE call_id = ?`,
		s.nowFn().UTC().Format(time.RFC3339Nano), outcome, callID,
	)
	if err != nil {
		return fmt.Errorf("voice callstore: end call %s: %w", callID, err)
	}
	return nil
}

// AppendTranscript appends one line to the call's transcript JSON array.
// The read-modify-write is atomic via a BEGIN IMMEDIATE transaction: the
// write lock is taken at the SQL layer, so no in-process mutex is held
// across the database I/O (mutexio rule) and concurrent appenders from
// other goroutines serialize correctly.
func (s *CallStore) AppendTranscript(ctx context.Context, callID string, line TranscriptLine) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("voice callstore: begin transcript tx for %s: %w", callID, err)
	}
	defer func() {
		if rollErr := tx.Rollback(); rollErr != nil && !errors.Is(rollErr, sql.ErrTxDone) {
			s.logger.Debug("voice callstore: rollback", "err", rollErr)
		}
	}()

	var raw string
	err = tx.QueryRowContext(ctx,
		`SELECT transcript FROM voice_calls WHERE call_id = ?`, callID,
	).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrCallNotFound
	}
	if err != nil {
		return fmt.Errorf("voice callstore: read transcript for %s: %w", callID, err)
	}
	var lines []TranscriptLine
	if raw != "" {
		if err := json.Unmarshal([]byte(raw), &lines); err != nil {
			return fmt.Errorf("voice callstore: decode transcript for %s: %w", callID, err)
		}
	}
	lines = append(lines, line)
	encoded, err := json.Marshal(lines)
	if err != nil {
		return fmt.Errorf("voice callstore: encode transcript for %s: %w", callID, err)
	}
	if _, err := tx.ExecContext(ctx,
		`UPDATE voice_calls SET transcript = ? WHERE call_id = ?`, string(encoded), callID,
	); err != nil {
		return fmt.Errorf("voice callstore: write transcript for %s: %w", callID, err)
	}
	return tx.Commit()
}

// TodayTranscripts implements leaf 04's CallTranscriptSource: prior calls
// with the same number since LOCAL midnight of the store's zone, oldest
// first. The summary is the first caller line plus the outcome, capped.
func (s *CallStore) TodayTranscripts(ctx context.Context, number string) ([]CallTranscript, error) {
	now := s.nowFn().In(s.loc)
	midnight := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, s.loc)

	rows, err := s.db.QueryContext(ctx,
		`SELECT call_id, number, started_at, ended_at, outcome, transcript
		 FROM voice_calls
		 WHERE number = ? AND started_at >= ?
		 ORDER BY started_at ASC`,
		number, midnight.UTC().Format(time.RFC3339Nano),
	)
	if err != nil {
		return nil, fmt.Errorf("voice callstore: today transcripts for %s: %w", number, err)
	}
	defer rows.Close()

	var out []CallTranscript
	for rows.Next() {
		var (
			callID    string
			num       string
			startedAt string
			endedAt   sql.NullString
			outcome   sql.NullString
			raw       string
		)
		if err := rows.Scan(&callID, &num, &startedAt, &endedAt, &outcome, &raw); err != nil {
			return nil, fmt.Errorf("voice callstore: scan transcript row: %w", err)
		}
		at, err := time.Parse(time.RFC3339Nano, startedAt)
		if err != nil {
			return nil, fmt.Errorf("voice callstore: parse started_at %q: %w", startedAt, err)
		}
		out = append(out, CallTranscript{
			Number:  num,
			At:      at,
			Summary: transcriptSummary(outcome.String, raw),
		})
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("voice callstore: iterate transcript rows: %w", err)
	}
	return out, nil
}

// SessionIDFor returns the meept session id bound to the most recent call
// with this number and employee, or ok=false when none exists.
func (s *CallStore) SessionIDFor(number, employeeID string) (string, bool) {
	var sid string
	err := s.db.QueryRow(
		`SELECT session_id FROM voice_calls
		 WHERE number = ? AND employee_id = ? AND session_id != ''
		 ORDER BY started_at DESC LIMIT 1`,
		number, employeeID,
	).Scan(&sid)
	if err != nil {
		return "", false
	}
	return sid, true
}

// compile-time proof the store satisfies leaf 04's transcript source seam.
var _ CallTranscriptSource = (*CallStore)(nil)

// transcriptSummary builds the capped one-line summary: first caller line
// plus the outcome. maxSummaryRunes comes from daybrief.go (same package).
func transcriptSummary(outcome, transcriptJSON string) string {
	var lines []TranscriptLine
	if transcriptJSON != "" {
		if err := json.Unmarshal([]byte(transcriptJSON), &lines); err != nil {
			lines = nil
		}
	}
	first := ""
	for _, l := range lines {
		if l.Role == "user" && strings.TrimSpace(l.Text) != "" {
			first = strings.TrimSpace(l.Text)
			break
		}
	}
	parts := make([]string, 0, 2)
	if outcome != "" {
		parts = append(parts, outcome)
	}
	if first != "" {
		parts = append(parts, first)
	}
	return clampRunes(strings.Join(parts, ": "), maxSummaryRunes)
}
