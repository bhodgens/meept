package agent

// Parked-turn persistence (durability for D9 universal parking).
//
// The TurnParker is an in-memory scheduler: on its own, records parked when
// Stop runs are logged at warn and dropped, and a daemon crash/restart loses
// every quota/throttle-parked turn silently. SQLiteParkStore adds the
// missing write-behind: every accepted Park is mirrored to a parked_turns
// table, every resume deletes its row, and the next Start re-arms whatever
// survived shutdown. The store is additive — the parker keeps working
// byte-identically in memory when no persistence is wired (tests, embedded
// uses, NewTurnParker callers that never call SetParkPersistence).
//
// FILE SHARING: SQLiteParkStore opens its own handle on the daemon's
// parks.db (WAL mode + busy timeout, same DSN convention as the session
// and queue stores), so it never aliases the session store's or queue
// store's *sql.DB. One parker per process owns one store; sharing one
// store across parkers is supported at the table level (kind scoping)
// but only the daemon's construction order exercises it.

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/caimlas/meept/internal/llm"

	_ "modernc.org/sqlite" // sqlite3 driver registration
)

// ParkPersistence is the persistence seam for parked turns. Implemented by
// SQLiteParkStore; every method must be safe for concurrent use and must
// treat a cancelled context as a failed write (the parker logs and
// continues — persistence is best-effort for Park, authoritative for
// Delete).
//
// The interface lives in package agent (interface-at-consumer, the same
// pattern as parkEventBus) so the parker never imports database/sql, and
// tests can substitute an in-memory fake.
type ParkPersistence interface {
	// Save inserts (or replaces, on park_key collision) a parked record
	// with its ParkKind and dedup key. persistenceKey unique within the
	// kind; it is the identity the delete/re-arm paths resolve rows by.
	Save(ctx context.Context, kind ParkKind, persistenceKey string, rec ParkedTurnRecord) error
	// Delete removes a record on resume (or give-up). Deleting an
	// already-absent row is not an error (idempotent).
	Delete(ctx context.Context, kind ParkKind, persistenceKey string) error
	// Load returns all records of the given kind that have not yet
	// expired (resume_at > now) and REMOVES the expired ones (prune at
	// load). Records are ordered oldest-resume-first so re-arm drains in
	// the same order the memory queue would have.
	Load(ctx context.Context, kind ParkKind, now time.Time) ([]ParkedTurnRecord, []string, error)
}

// ParkKind scopes a parked record's turn type. Chat turns and goal-loop
// episodes share the parked_turns table but must never re-arm into each
// other's routers: chat quota records decode via quotaTurnPayload through
// the QuotaResumeWatcher's adapted callback, episodes via goalTurnPayload
// through the daemon's goal-loop resume router.
type ParkKind string

const (
	// ParkKindChat is the chat-side parked turn (QuotaResumeWatcher /
	// throttle parks riding the handler's chat parker).
	ParkKindChat ParkKind = "chat"
	// ParkKindEpisode is the goal-loop parked episode (internal/employee).
	ParkKindEpisode ParkKind = "episode"
)

// persistenceKeySuffix is appended to chat-side persistence keys (after
// the session ID) to keep distinct chat turns distinct: chat turns are
// intentionally NOT deduped in memory (two different user messages are
// two turns — internal/employee/park.go H4 note), so each park gets a
// wall-clock-nanosecond suffix. RFC3339Nano preserves that precision
// through the TEXT column.
//
// Episode keys instead use the H4 semantic identity (employee + phase +
// trigger identity, triggerKey-style normalization ignoring FiredAt) so a
// re-parked episode overwrites its predecessor row instead of
// accumulating duplicates across restarts — the table-level twin of the
// parker's in-memory dedup map. That key is derived on the employee-side
// wiring (daemon components) from goalTurnPayload and passed in via
// ParkPersistence.Save.
const chatPersistenceKeyTimeFormat = time.RFC3339Nano

// parkedTurnsSchema creates the parked-turn table. Times are RFC3339 TEXT
// (same convention as sessions.created_at); turn_payload is the raw
// ParkedTurnRecord.TurnPayload JSON preserved byte-for-byte so the
// class-specific decoders (quotaTurnPayload / goalTurnPayload) see
// exactly what the pre-restart parker held.
const parkedTurnsSchema = `
CREATE TABLE IF NOT EXISTS parked_turns (
	id            INTEGER PRIMARY KEY AUTOINCREMENT,
	kind          TEXT NOT NULL,
	park_key      TEXT NOT NULL,
	session_id    TEXT NOT NULL DEFAULT '',
	conversation_id TEXT NOT NULL DEFAULT '',
	agent_id      TEXT NOT NULL DEFAULT '',
	class         TEXT NOT NULL DEFAULT '',
	resume_at     TEXT NOT NULL,
	attempt       INTEGER NOT NULL DEFAULT 0,
	max_attempts  INTEGER NOT NULL DEFAULT 0,
	turn_payload  TEXT NOT NULL DEFAULT '',
	created_at    TEXT NOT NULL,
	UNIQUE(kind, park_key)
);

CREATE INDEX IF NOT EXISTS idx_parked_turns_kind_resume
	ON parked_turns(kind, resume_at);
`

// SQLiteParkStore is the default ParkPersistence over the daemon's
// parks.db.
type SQLiteParkStore struct {
	db     *sql.DB
	logger *slog.Logger
}

// NewSQLiteParkStore opens (and migrates) the parked-turn table on the
// database at dbPath. The DSN matches the session/queue stores: WAL
// journal + 5s busy timeout. modernc.org/sqlite honors only
// `_pragma=`-style DSN parameters (the queue/task stores use the same
// form), so `_journal_mode=`-style keys would be silently ignored.
func NewSQLiteParkStore(dbPath string, logger *slog.Logger) (*SQLiteParkStore, error) {
	if logger == nil {
		logger = slog.Default()
	}
	db, err := sql.Open("sqlite", dbPath+"?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)&_pragma=synchronous(NORMAL)")
	if err != nil {
		return nil, fmt.Errorf("park store: failed to open database: %w", err)
	}
	if _, err := db.Exec(parkedTurnsSchema); err != nil {
		db.Close()
		return nil, fmt.Errorf("park store: failed to migrate parked_turns: %w", err)
	}
	logger.Info("park store initialized", "path", dbPath)
	return &SQLiteParkStore{db: db, logger: logger}, nil
}

// Close releases the database handle.
func (s *SQLiteParkStore) Close() error {
	if s == nil || s.db == nil {
		return nil
	}
	return s.db.Close()
}

// opTimeout bounds every store statement; park-path callers run on the
// turn's request path and must never block past this.
const opTimeout = 5 * time.Second

// Save implements ParkStore.
func (s *SQLiteParkStore) Save(ctx context.Context, kind ParkKind, persistenceKey string, rec ParkedTurnRecord) error {
	if s == nil || s.db == nil {
		return fmt.Errorf("park store: not initialized")
	}
	if kind == "" || persistenceKey == "" {
		return fmt.Errorf("park store: kind and persistence key are required")
	}
	ctx, cancel := context.WithTimeout(ctx, opTimeout)
	defer cancel()
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO parked_turns (
			kind, park_key, session_id, conversation_id, agent_id, class,
			resume_at, attempt, max_attempts, turn_payload, created_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(kind, park_key) DO UPDATE SET
			session_id      = excluded.session_id,
			conversation_id = excluded.conversation_id,
			agent_id        = excluded.agent_id,
			class           = excluded.class,
			resume_at       = excluded.resume_at,
			attempt         = excluded.attempt,
			max_attempts    = excluded.max_attempts,
			turn_payload    = excluded.turn_payload,
			created_at      = excluded.created_at
	`,
		string(kind),
		persistenceKey,
		rec.SessionID,
		rec.ConversationID,
		rec.AgentID,
		parkClassString(rec.Class),
		formatParkTime(rec.ResumeAt),
		rec.Attempt,
		rec.MaxAttempts,
		string(rec.TurnPayload),
		formatParkTime(time.Now()),
	)
	if err != nil {
		return fmt.Errorf("park store: save %s/%s: %w", kind, persistenceKey, err)
	}
	return nil
}

// Delete implements ParkStore. Idempotent: deleting an absent row affects
// 0 rows and is not an error (resume may race a give-up, or the row may
// already be gone from a prune).
func (s *SQLiteParkStore) Delete(ctx context.Context, kind ParkKind, persistenceKey string) error {
	if s == nil || s.db == nil {
		return fmt.Errorf("park store: not initialized")
	}
	if kind == "" || persistenceKey == "" {
		return fmt.Errorf("park store: kind and persistence key are required")
	}
	ctx, cancel := context.WithTimeout(ctx, opTimeout)
	defer cancel()
	if _, err := s.db.ExecContext(ctx,
		`DELETE FROM parked_turns WHERE kind = ? AND park_key = ?`,
		string(kind), persistenceKey,
	); err != nil {
		return fmt.Errorf("park store: delete %s/%s: %w", kind, persistenceKey, err)
	}
	return nil
}

// Load implements ParkStore: one transaction reads the kind's rows,
// deletes expired ones, and returns the survivors oldest-resume-first
// with their park keys (so the caller can delete them by identity on
// resume). A transaction is required so a concurrent Load (second parker
// sharing the file) can never see a row both prune and re-arm.
func (s *SQLiteParkStore) Load(ctx context.Context, kind ParkKind, now time.Time) ([]ParkedTurnRecord, []string, error) {
	if s == nil || s.db == nil {
		return nil, nil, fmt.Errorf("park store: not initialized")
	}
	ctx, cancel := context.WithTimeout(ctx, opTimeout)
	defer cancel()

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, nil, fmt.Errorf("park store: load begin: %w", err)
	}
	defer func() {
		// Rollback after Commit is a documented no-op; the error carries
		// no information worth logging, so it is explicitly discarded.
		if rerr := tx.Rollback(); rerr != nil && rerr != sql.ErrTxDone {
			s.logger.Debug("park store: post-commit rollback", "error", rerr)
		}
	}()

	rows, err := tx.QueryContext(ctx, `
		SELECT park_key, session_id, conversation_id, agent_id, class,
		       resume_at, attempt, max_attempts, turn_payload
		FROM parked_turns
		WHERE kind = ?
		ORDER BY resume_at ASC, id ASC
	`, string(kind))
	if err != nil {
		return nil, nil, fmt.Errorf("park store: load query: %w", err)
	}

	var records []ParkedTurnRecord
	var keys []string
	var expired []string
	for rows.Next() {
		var (
			key, sessID, convID, agentID, classRaw, resumeAtRaw, payload string
			attempt, maxAttempts                                         int
		)
		if err := rows.Scan(&key, &sessID, &convID, &agentID, &classRaw,
			&resumeAtRaw, &attempt, &maxAttempts, &payload); err != nil {
			rows.Close()
			return nil, nil, fmt.Errorf("park store: load scan: %w", err)
		}
		resumeAt := parseParkTime(resumeAtRaw)
		if resumeAt.IsZero() || !resumeAt.After(now) {
			expired = append(expired, key)
			continue
		}
		records = append(records, ParkedTurnRecord{
			ConversationID: convID,
			SessionID:      sessID,
			AgentID:        agentID,
			Class:          parseParkClass(classRaw),
			ResumeAt:       resumeAt,
			Attempt:        attempt,
			MaxAttempts:    maxAttempts,
			TurnPayload:    json.RawMessage(payload),
		})
		keys = append(keys, key)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, nil, fmt.Errorf("park store: load iterate: %w", err)
	}
	rows.Close()

	for _, key := range expired {
		if _, err := tx.ExecContext(ctx,
			`DELETE FROM parked_turns WHERE kind = ? AND park_key = ?`,
			string(kind), key,
		); err != nil {
			return nil, nil, fmt.Errorf("park store: prune expired: %w", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, nil, fmt.Errorf("park store: load commit: %w", err)
	}
	return records, keys, nil
}

// parseParkClass inverts parkClassString; unknown/empty classes fall back
// to FailureQuota (the dominant historical class) so a re-armed record
// still routes somewhere sane instead of being dropped.
func parseParkClass(class string) llm.FailureClass {
	switch class {
	case "throttle":
		return llm.FailureThrottle
	case "quota":
		return llm.FailureQuota
	default:
		return llm.FailureQuota
	}
}

// formatParkTime renders a time for the resume_at/created_at columns
// (RFC3339 with nanoseconds so round-trips preserve scheduling precision).
func formatParkTime(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.UTC().Format(time.RFC3339Nano)
}

// parseParkTime reads RFC3339Nano (preferred) or RFC3339 (rows written by
// other tooling); an empty or unparsable value returns the zero time,
// which Load treats as expired.
func parseParkTime(raw string) time.Time {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return time.Time{}
	}
	if t, err := time.Parse(time.RFC3339Nano, raw); err == nil {
		return t
	}
	if t, err := time.Parse(time.RFC3339, raw); err == nil {
		return t
	}
	return time.Time{}
}
