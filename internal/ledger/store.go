// Package ledger is the append-only employee ledger: an immutable
// SQLite log (ledger) plus one current-truth row per (employee, subject)
// (ledger_state). Records are appended through Store.Append — which
// validates the closed Kind/Source sets, enforces the AllowedTransitions
// state machine for SetStatus appends, and upserts the state row in one
// transaction — and read back through QueryLog / QueryOpen / GetState or
// followed live through Tail. The store is a single physical file
// (<DataDir>/ledger/ledger.db) on modernc.org/sqlite in WAL mode with a
// one-connection pool, so writers serialize in-process; times are stored
// as RFC3339 TEXT per the house convention (internal/voice/callstore.go,
// internal/effects/ledger_sqlite.go).
package ledger

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	_ "modernc.org/sqlite" // driver registration (house convention)
)

// Kind classifies one ledger record. Closed set.
type Kind string

const (
	KindCommittal   Kind = "committal"   // what was said/promised on an interaction
	KindAction      Kind = "action"      // something was done
	KindObservation Kind = "observation" // noted fact, no state implication
	KindCommitment  Kind = "commitment"  // an obligation owed (opens work)
	KindResolution  Kind = "resolution"  // closes/answers a commitment
)

// Source names the writer system. Closed set.
type Source string

const (
	SourceVoice Source = "voice"
	SourceDesk  Source = "desk"
	SourceTool  Source = "tool"
	SourceCLI   Source = "cli"
	SourceTask  Source = "task"
)

// Status is the current-state column value. Closed set with the
// transition map below. Enforced by Append, never by readers.
type Status string

const (
	StatusOpen        Status = "open"
	StatusDone        Status = "done"
	StatusCancelled   Status = "cancelled"
	StatusWaitingMe   Status = "waiting_on_me"
	StatusWaitingThem Status = "waiting_on_them"
)

// AllowedTransitions: the legal state machine for subject status.
//
//	open            -> done | cancelled | waiting_on_me | waiting_on_them
//	waiting_on_me   -> open | done | cancelled
//	waiting_on_them -> open | done | cancelled
//	done            -> open          (reopen; store logs INFO "ledger reopen")
//	cancelled       -> open          (reopen; same log)
//	"" (new subject) -> any
//
// NOTE (leaf 01-store/01): the contract sketches this map in
// transitions.go; the test gate references it at compile time, so it is
// declared here until leaf 02 lands and relocates it.
var AllowedTransitions = map[Status][]Status{
	StatusOpen:        {StatusDone, StatusCancelled, StatusWaitingMe, StatusWaitingThem},
	StatusWaitingMe:   {StatusOpen, StatusDone, StatusCancelled},
	StatusWaitingThem: {StatusOpen, StatusDone, StatusCancelled},
	StatusDone:        {StatusOpen},
	StatusCancelled:   {StatusOpen},
}

// Record is one appended, immutable log row. ID is SQLite rowid = the
// monotonic per-store offset used by Tail.
type Record struct {
	ID         int64
	EmployeeID string // required; never client-supplied at tool boundary
	At         time.Time
	Kind       Kind
	Subject    string // namespace key, e.g. "acme-roof"
	Payload    []byte // opaque JSON (turnjournal.result_payload convention)
	Source     Source
	SourceRef  string // call ccid / turn id / task id ('' allowed)
	Resolves   int64  // ledger.id closed by this row; 0 = none;
	// MUST reference a row of the SAME
	// EmployeeID
}

// DraftRecord is the Append input (no ID).
type DraftRecord struct {
	EmployeeID string
	Kind       Kind
	Subject    string
	Payload    []byte
	Source     Source
	SourceRef  string
	Resolves   int64
	// SetStatus (optional): desired new subject status. Empty = log-only
	// append, state untouched. Non-empty triggers the transition check.
	SetStatus Status
	// Summary (optional, requires SetStatus): one-line current truth.
	Summary string
}

// State is the current-truth row (one per employee+subject).
type State struct {
	EmployeeID string
	Subject    string
	Status     Status
	Summary    string
	UpdatedAt  time.Time
	AsOf       int64 // ledger.id of the row that last set it
}

// Errors (frozen names).
var ErrIllegalTransition = errors.New("ledger: illegal status transition")
var ErrResolveNotFound = errors.New("ledger: resolves references unknown or foreign record")

// errNotImplemented backs the read-path methods whose full semantics
// land with leaves 02-store/02 (Append transition/state machine,
// Resolves validation) and 03-query-verify-tail (GetState/QueryOpen/
// VerifyState/RepairState/Tail). The methods exist so the package
// builds and §1-§2 run; they fail with a 'ledger:'-prefixed error.
var errNotImplemented = errors.New("ledger: not implemented; pending leaves 01-store/02-03")

// LogQuery filters for QueryLog.
type LogQuery struct {
	EmployeeID string // required
	Subject    string // '' = any
	Kinds      []Kind // nil = any
	SinceID    int64  // 0 = from start
	Limit      int    // 0 = default 200; max 1000
}

// VerifyReport summarizes a VerifyState fold; Diffs lists divergences.
type VerifyReport struct {
	Checked   int
	Divergent int
	Diffs     []StateDiff
}

// StateDiff names one divergent (employee, subject): Have is the stored
// ledger_state row, Want the recomputed fold.
type StateDiff struct {
	EmployeeID string
	Subject    string
	Have       State
	Want       State
}

// ledgerSchema is the frozen schema (plan 20261109-employee-ledger,
// 00T/01 Interface Contracts) — character-for-character from the
// contract block. CREATE IF NOT EXISTS makes Open idempotent on a live
// database.
const ledgerSchema = `
CREATE TABLE IF NOT EXISTS ledger (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    employee_id TEXT NOT NULL,
    at          TEXT NOT NULL,               -- RFC3339 UTC
    kind        TEXT NOT NULL CHECK (kind IN ('committal','action','observation','commitment','resolution')),
    subject     TEXT NOT NULL DEFAULT '',
    payload     BLOB,                        -- opaque JSON, NULL allowed
    source      TEXT NOT NULL CHECK (source IN ('voice','desk','tool','cli','task')),
    source_ref  TEXT NOT NULL DEFAULT '',
    resolves    INTEGER NOT NULL DEFAULT 0
);
CREATE INDEX IF NOT EXISTS idx_ledger_emp_subject ON ledger(employee_id, subject, id);
CREATE INDEX IF NOT EXISTS idx_ledger_resolves    ON ledger(resolves) WHERE resolves <> 0;

CREATE TABLE IF NOT EXISTS ledger_state (
    employee_id TEXT NOT NULL,
    subject     TEXT NOT NULL,
    status      TEXT NOT NULL CHECK (status IN ('open','done','cancelled','waiting_on_me','waiting_on_them')),
    summary     TEXT NOT NULL DEFAULT '',
    updated_at  TEXT NOT NULL,
    as_of       INTEGER NOT NULL,
    PRIMARY KEY (employee_id, subject)
);

CREATE VIRTUAL TABLE IF NOT EXISTS ledger_fts USING fts5(
    subject, payload_snippet, content='');   -- external-content-free: we
                                             -- insert (subject, first 512B
                                             -- of payload) at Append time
`

// Store is the SQLite employee-ledger store. A single connection
// (SetMaxOpenConns(1)) serializes writers in-process, the same
// convention as the effects ledger and voice call store; mu guards the
// afterAppend hook seam.
type Store struct {
	db     *sql.DB
	logger *slog.Logger

	nowFn func() time.Time // test seam: defaults to time.Now

	mu          sync.Mutex
	afterAppend func(rec Record, st *State) // seam used by 02-03 publish; set via WithAfterAppend
}

// Open opens (and migrates) the ledger store at dbPath. The migration is
// CREATE IF NOT EXISTS, so calling Open twice on one path — or after a
// prior process — is safe and preserves rows.
func Open(dbPath string, logger *slog.Logger) (*Store, error) {
	if logger == nil {
		logger = slog.Default()
	}
	db, err := sql.Open("sqlite", dbPath+"?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)&_pragma=synchronous(NORMAL)")
	if err != nil {
		return nil, fmt.Errorf("ledger: failed to open database: %w", err)
	}
	db.SetMaxOpenConns(1)
	if _, err := db.Exec(ledgerSchema); err != nil {
		db.Close()
		return nil, fmt.Errorf("ledger: failed to migrate ledger: %w", err)
	}
	s := &Store{db: db, logger: logger, nowFn: time.Now}
	// Guarded-ALTER migration idiom (internal/voice/callstore.go:118-130):
	// reserved for future columns — duplicate-column errors are the
	// expected steady state on migrated databases; anything else is fatal.
	logger.Info("ledger store initialized", "path", dbPath)
	return s, nil
}

// Close closes the underlying database.
func (s *Store) Close() error {
	if s == nil || s.db == nil {
		return nil
	}
	return s.db.Close()
}

// WithAfterAppend registers a hook fired once per successful Append,
// after commit, with the stored Record and the resulting *State (nil for
// log-only appends). The wiring that calls it with a real state lands
// with leaf 02-03 (publish); the field and setter are declared now per
// the frozen gate seam.
func (s *Store) WithAfterAppend(fn func(rec Record, st *State)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.afterAppend = fn
}

// Append validates kinds/sources, then INSERTs the log row and returns
// the stored Record.
//
// NOTE (leaf 01-store/01 minimal body): transition enforcement
// (AllowedTransitions + PayloadEnvelope wrapping), Resolves validation,
// the state upsert, and the afterAppend publish are leaf 02's scope;
// this body intentionally ignores SetStatus/Summary/Resolves so §1-§2
// of the gate run while §3+ stay RED until 02 lands.
func (s *Store) Append(ctx context.Context, d DraftRecord) (*Record, error) {
	if !validKind(d.Kind) {
		return nil, fmt.Errorf("ledger: invalid kind %q", d.Kind)
	}
	if !validSource(d.Source) {
		return nil, fmt.Errorf("ledger: invalid source %q", d.Source)
	}
	if d.EmployeeID == "" {
		return nil, errors.New("ledger: append: employee_id is required")
	}

	at := s.nowFn().UTC()
	res, err := s.db.ExecContext(ctx,
		`INSERT INTO ledger (employee_id, at, kind, subject, payload, source, source_ref, resolves)
         VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		d.EmployeeID, at.Format(time.RFC3339), string(d.Kind), d.Subject, d.Payload,
		string(d.Source), d.SourceRef, d.Resolves)
	if err != nil {
		return nil, fmt.Errorf("ledger: append: %w", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return nil, fmt.Errorf("ledger: append: last insert id: %w", err)
	}

	rec := Record{
		ID:         id,
		EmployeeID: d.EmployeeID,
		At:         at,
		Kind:       d.Kind,
		Subject:    d.Subject,
		Payload:    d.Payload,
		Source:     d.Source,
		SourceRef:  d.SourceRef,
		Resolves:   d.Resolves,
	}

	if fn := s.hook(); fn != nil {
		fn(rec, nil)
	}
	return &rec, nil
}

func (s *Store) hook() func(rec Record, st *State) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.afterAppend
}

func validKind(k Kind) bool {
	switch k {
	case KindCommittal, KindAction, KindObservation, KindCommitment, KindResolution:
		return true
	}
	return false
}

func validSource(src Source) bool {
	switch src {
	case SourceVoice, SourceDesk, SourceTool, SourceCLI, SourceTask:
		return true
	}
	return false
}

// QueryLog returns matching log rows in ascending id order.
func (s *Store) QueryLog(ctx context.Context, q LogQuery) ([]Record, error) {
	if q.EmployeeID == "" {
		return nil, errors.New("ledger: query log: employee_id is required")
	}
	limit := q.Limit
	if limit <= 0 {
		limit = 200
	}
	if limit > 1000 {
		limit = 1000
	}

	var sb strings.Builder
	sb.WriteString(`SELECT id, employee_id, at, kind, subject, payload, source, source_ref, resolves FROM ledger WHERE employee_id = ?`)
	args := []any{q.EmployeeID}
	if q.Subject != "" {
		sb.WriteString(` AND subject = ?`)
		args = append(args, q.Subject)
	}
	if len(q.Kinds) > 0 {
		placeholders := strings.TrimSuffix(strings.Repeat("?,", len(q.Kinds)), ",")
		fmt.Fprintf(&sb, ` AND kind IN (%s)`, placeholders)
		for _, k := range q.Kinds {
			args = append(args, string(k))
		}
	}
	if q.SinceID > 0 {
		sb.WriteString(` AND id > ?`)
		args = append(args, q.SinceID)
	}
	fmt.Fprintf(&sb, ` ORDER BY id ASC LIMIT %d`, limit)

	rows, err := s.db.QueryContext(ctx, sb.String(), args...)
	if err != nil {
		return nil, fmt.Errorf("ledger: query log: %w", err)
	}
	defer rows.Close()

	var out []Record
	for rows.Next() {
		rec, err := scanRecord(rows)
		if err != nil {
			return nil, fmt.Errorf("ledger: query log: scan: %w", err)
		}
		out = append(out, rec)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("ledger: query log: iterate: %w", err)
	}
	return out, nil
}

type rowScanner interface {
	Scan(dest ...any) error
}

func scanRecord(rows rowScanner) (Record, error) {
	var (
		rec     Record
		at      string
		kind    string
		source  string
		payload []byte
	)
	if err := rows.Scan(&rec.ID, &rec.EmployeeID, &at, &kind, &rec.Subject, &payload,
		&source, &rec.SourceRef, &rec.Resolves); err != nil {
		return Record{}, err
	}
	parsedAt, err := time.Parse(time.RFC3339, at)
	if err != nil {
		// tolerate fractional-second RFC3339Nano rows (raw writers)
		parsedAt, err = time.Parse(time.RFC3339Nano, at)
		if err != nil {
			return Record{}, fmt.Errorf("bad at timestamp %q: %w", at, err)
		}
	}
	rec.At = parsedAt
	rec.Kind = Kind(kind)
	rec.Source = Source(source)
	rec.Payload = payload
	return rec, nil
}

// QueryOpen returns state rows with Status not in {done, cancelled} for
// the employee, newest-updated first. Full implementation is leaf
// 03-store/03 (query-verify-tail).
func (s *Store) QueryOpen(ctx context.Context, employeeID string) ([]State, error) {
	return nil, errNotImplemented
}

// GetState returns the current-truth row for (employeeID, subject);
// ok=false with the zero State when none exists. Full implementation is
// leaf 03-store/03 (query-verify-tail).
func (s *Store) GetState(ctx context.Context, employeeID, subject string) (State, bool, error) {
	return State{}, false, errNotImplemented
}

// Tail yields records with ID > sinceID: catch-up from SQLite, then live
// from the in-process fan-out registered at Append commit. Full
// implementation is leaf 03-store/03 (query-verify-tail).
func (s *Store) Tail(ctx context.Context, sinceID int64) (<-chan Record, error) {
	return nil, errNotImplemented
}

// VerifyState folds the ledger and diffs ledger_state; report lists
// divergences. Full implementation is leaf 03-store/03
// (query-verify-tail).
func (s *Store) VerifyState(ctx context.Context) (*VerifyReport, error) {
	return nil, errNotImplemented
}

// RepairState rebuilds divergent state rows from the fold and returns
// the repaired count. Full implementation is leaf 03-store/03
// (query-verify-tail).
func (s *Store) RepairState(ctx context.Context) (int, error) {
	return 0, errNotImplemented
}
