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

// transcriptAppendTimeout bounds one transcript append once it is
// detached from the caller's context (AppendTranscript): the write must
// still be able to fail fast against a wedged database, but it must NOT
// die because the voice turn's context was cancelled at teardown —
// defect B's lost late-final rows (see AppendTranscript).
const transcriptAppendTimeout = 5 * time.Second

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
// as the effects ledger; the transcript append is a single atomic UPDATE
// (json_insert), so no in-process read-modify-write guard exists or is
// needed (see AppendTranscript).
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
	// Boot sweep (audit 2026-11-09 voice finding 7, C-V3): reconcile any
	// row a previous process stranded at 'live' BEFORE this store can
	// serve a new call. The store opens in WireVoiceEmployee, which runs
	// before the webhook routes, the plain listener, and every leg are
	// armed — no live call can exist yet on this instance (single-
	// instance posture documented on SweepStaleLive). A sweep failure
	// does not fail the boot (the store still serves); it lands loud in
	// the log because stranded rows mislead every later reader.
	if n, sweepErr := s.SweepStaleLive(context.Background()); sweepErr != nil {
		logger.Error("voice callstore: boot sweep of stale live rows failed", "error", sweepErr)
	} else if n > 0 {
		logger.Info("voice callstore: boot sweep ended stale live rows", "count", n)
	}
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

// SweepStaleLive ends every voice_calls row still marked 'live' (audit
// 2026-11-09 voice finding 7, C-V3). A row only reaches 'ended' through
// the release fan-out; a process that died mid-call strands its row at
// 'live' forever, and later readers (day brief, TodayTranscripts,
// SessionIDFor) would treat a dead call as ongoing. NewCallStore calls
// this ONCE at store open — the voice boot point: WireVoiceEmployee
// builds the employee stack (and therefore opens this store) before
// voice_wiring arms the HTTP routes, the plain listener, or any leg, so
// no call can be in flight when the sweep runs.
//
// Returns the number of rows reconciled. Idempotent: the WHERE clause
// only matches 'live' rows, so a re-run over already-ended rows is a
// zero-row no-op that never re-stamps ended_at.
//
// SINGLE-INSTANCE POSTURE (documented per the audit and the branch
// review checklist): the sweep assumes one daemon owns this calls.db.
// A second instance sharing the same file would have its genuinely-live
// calls ended here. Meept's voice sidecar is a per-install single
// instance by construction (the child proc + registry + supervisor all
// assume sole ownership of the dial state), so the posture holds; a
// future multi-instance mode must gate the sweep on a lease instead.
func (s *CallStore) SweepStaleLive(ctx context.Context) (int, error) {
	res, err := s.db.ExecContext(ctx,
		`UPDATE voice_calls SET state = 'ended', outcome = 'failed', ended_at = ? WHERE state = 'live'`,
		s.nowFn().UTC().Format(time.RFC3339Nano),
	)
	if err != nil {
		return 0, fmt.Errorf("voice callstore: sweep stale live: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("voice callstore: sweep stale live count: %w", err)
	}
	return int(n), nil
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
//
// Two properties defect B (voice link status lagging spoken audio) needs:
//
//  1. ATOMIC: the append is a single UPDATE whose value is SQLite's own
//     json_insert on the column — no SELECT/decode/UPDATE read-modify-
//     write, so no lost-update window exists at any layer (in-process
//     goroutines AND any future second writer), and the row stays valid
//     JSON under every interleaving. The write lock is taken at the SQL
//     layer for the one statement (mutexio rule: nothing in-process is
//     held across the I/O).
//  2. CANCELLATION-RESILIENT: the binding calls this AFTER a turn's
//     reply returns, and that turn's context is routinely cancelled by
//     then — a barge-in supersede or a hangup racing a late final (live
//     defect link-393ebd86996a0dfd side b seq=5: the caller's audio had
//     already been heard, the reply landed after teardown, and the
//     append died with "context canceled", so the rows were LOST, not
//     late, and voice.link.status undercounted the spoken turns
//     forever). Recording a line the caller already heard is a
//     bookkeeping write, not a live-call action: detach from the
//     caller's cancellation (context.WithoutCancel) and bound the commit
//     with its own timeout so a wedged database can never hang the
//     writer goroutine.
func (s *CallStore) AppendTranscript(ctx context.Context, callID string, line TranscriptLine) error {
	encoded, err := json.Marshal(line)
	if err != nil {
		return fmt.Errorf("voice callstore: encode transcript for %s: %w", callID, err)
	}
	writeCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), transcriptAppendTimeout)
	defer cancel()
	res, err := s.db.ExecContext(writeCtx,
		`UPDATE voice_calls SET transcript = json_insert(COALESCE(NULLIF(transcript, ''), '[]'), '$[#]', json(?)) WHERE call_id = ?`,
		string(encoded), callID,
	)
	if err != nil {
		return fmt.Errorf("voice callstore: append transcript for %s: %w", callID, err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("voice callstore: append transcript rows for %s: %w", callID, err)
	}
	if n == 0 {
		return ErrCallNotFound
	}
	return nil
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

// EndedCall returns one terminal call's record plus its decoded
// transcript, ok=false for an unknown call_id. Read-only: it exists for
// the daemon's call-end notification relay
// (internal/daemon/voice_employee.go notifyCallRelay) and mutates
// nothing.
func (s *CallStore) EndedCall(ctx context.Context, callID string) (CallRecord, []TranscriptLine, bool, error) {
	var (
		number, employeeID, direction, sessionID string
		startedAt                                string
		raw                                      string
	)
	err := s.db.QueryRowContext(ctx,
		`SELECT number, employee_id, direction, session_id, started_at, transcript
		 FROM voice_calls WHERE call_id = ?`, callID,
	).Scan(&number, &employeeID, &direction, &sessionID, &startedAt, &raw)
	if errors.Is(err, sql.ErrNoRows) {
		return CallRecord{}, nil, false, nil
	}
	if err != nil {
		return CallRecord{}, nil, false, fmt.Errorf("voice callstore: read call %s: %w", callID, err)
	}
	var lines []TranscriptLine
	if raw != "" {
		if err := json.Unmarshal([]byte(raw), &lines); err != nil {
			return CallRecord{}, nil, false, fmt.Errorf("voice callstore: decode transcript for %s: %w", callID, err)
		}
	}
	return CallRecord{
		CallID:     callID,
		Number:     number,
		EmployeeID: employeeID,
		Direction:  direction,
		SessionID:  sessionID,
	}, lines, true, nil
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
