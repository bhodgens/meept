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
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
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
	// ParkKindGate is a checkpointed human gate (hitl-notifications leaf 02,
	// C5): a plan/goal approval pause persisted as a GateParkData payload.
	// Gate parks schedule at a far-future resume time (gateParkHorizon,
	// gate_park.go) so the time-based drain never auto-approves a gate —
	// only an explicit ResumeByKey (the approval path) resumes them. Same
	// kind-scoped persistence rows, same re-arm-on-Start semantics.
	ParkKindGate ParkKind = "gate"
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
// Park-owner identifiers (C-K1, statemachine-remediation tree 04 leaf 01):
// every persisted parked turn carries exactly ONE owning parker, stamped at
// Save and enforced at Load. Kind scoping alone was the C1 CRITICAL: the
// quota watcher's inner parker and the daemon's throttle parker BOTH persist
// under ParkKindChat, so a kind-only Load re-armed each parker's Start with
// the OTHER's rows — and because quota/throttle payload keys overlap, a
// throttle row decoded "successfully" into quotaTurnPayload and the message
// executed a second time. Owners make the re-arm partition disjoint: a row
// can only ever be loaded (and resumed) by its owner.
const (
	ParkOwnerQuotaWatcher = "quota-watcher" // QuotaResumeWatcher's inner parker
	ParkOwnerThrottle     = "throttle"      // daemon chat throttle TurnParker
	ParkOwnerGate         = "gate"          // dedicated ParkKindGate parker
	ParkOwnerEpisode      = "episode"       // goal-loop ParkKindEpisode parker
)

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
	owner         TEXT NOT NULL DEFAULT '',
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

	// ownerFilterMu guards ownerFilter, the store-level Load filter
	// (C-K1): when non-empty, Load returns only rows whose owner column
	// matches. SetOwnerFilter is a test/observability seam; production
	// parkers pass their owner explicitly via LoadOwned (the
	// OwnerScopedPersistence capability), which never reads this field.
	ownerFilterMu sync.Mutex
	ownerFilter   string
}

// SetOwnerFilter installs a store-level owner filter on Load (C-K1). Empty
// string restores the unfiltered, kind-only view (the pre-migration
// contract — park_store tests and any parker without an owner rely on it).
func (s *SQLiteParkStore) SetOwnerFilter(owner string) {
	if s == nil {
		return
	}
	s.ownerFilterMu.Lock()
	s.ownerFilter = owner
	s.ownerFilterMu.Unlock()
}

// currentOwnerFilter reads the sticky filter.
func (s *SQLiteParkStore) currentOwnerFilter() string {
	s.ownerFilterMu.Lock()
	defer s.ownerFilterMu.Unlock()
	return s.ownerFilter
}

// migrate adds the C-K1 owner column to pre-owner databases and backfills
// it. Schema creates the column for fresh databases (owner in CREATE TABLE
// IF NOT EXISTS is a no-op on legacy files), so the ALTER + backfill only
// runs on first open of a pre-migration parks.db. Backfill is kind/class
// aware (the class column IS stored per row, so rows are NOT undifferentiated):
//   - kind=episode rows -> 'episode' (the goal-loop parker wrote them)
//   - kind=gate rows -> 'gate' (the gate parker wrote them)
//   - kind=chat class=throttle rows -> 'throttle'
//   - everything else (chat quota rows + unknown-class rows) ->
//     'quota-watcher' — the dominant historical writer. Unknown-class chat
//     rows are counted and the count is logged as a migrate warning.
//
// Idempotent: guarded by columnExists, so repeated boots are silent.
func (s *SQLiteParkStore) migrate(ctx context.Context) error {
	exists, err := s.columnExists("parked_turns", "owner")
	if err != nil {
		return err
	}
	if !exists {
		if _, err := s.db.ExecContext(ctx,
			`ALTER TABLE parked_turns ADD COLUMN owner TEXT NOT NULL DEFAULT ''`); err != nil {
			if !strings.Contains(err.Error(), "duplicate column name") {
				return fmt.Errorf("park store: add owner column: %w", err)
			}
		} else {
			var moved, unknown int64
			if _, err := s.db.ExecContext(ctx,
				`UPDATE parked_turns SET owner = 'episode' WHERE kind = 'episode' AND owner = ''`); err != nil {
				return fmt.Errorf("park store: backfill episode owners: %w", err)
			}
			if _, err := s.db.ExecContext(ctx,
				`UPDATE parked_turns SET owner = 'gate' WHERE kind = 'gate' AND owner = ''`); err != nil {
				return fmt.Errorf("park store: backfill gate owners: %w", err)
			}
			if _, err := s.db.ExecContext(ctx,
				`UPDATE parked_turns SET owner = 'throttle' WHERE kind = 'chat' AND class = 'throttle' AND owner = ''`); err != nil {
				return fmt.Errorf("park store: backfill throttle owners: %w", err)
			}
			// Count chat rows whose class is neither quota nor throttle
			// before the catch-all backfill (these land on 'quota-watcher'
			// and then DROP at first Load via the strict class parser).
			row := s.db.QueryRowContext(ctx,
				`SELECT COUNT(*) FROM parked_turns WHERE kind = 'chat' AND class NOT IN ('quota','throttle')`)
			_ = row.Scan(&unknown)
			res, err := s.db.ExecContext(ctx,
				`UPDATE parked_turns SET owner = 'quota-watcher' WHERE owner = ''`)
			if err != nil {
				return fmt.Errorf("park store: backfill default owners: %w", err)
			}
			moved, _ = res.RowsAffected()
			s.logger.Info("park store: migrated owner column — legacy rows backfilled",
				"backfilled", moved)
			if unknown > 0 {
				s.logger.Warn("park store: legacy chat rows with unknown class backfilled to 'quota-watcher' — they drop at first load",
					"unknown_class_rows", unknown)
			}
		}
	}
	// Kind+owner covering index (owner-filtered Load path). IF NOT EXISTS
	// makes this idempotent on fresh and migrated databases alike.
	if _, err := s.db.ExecContext(ctx,
		`CREATE INDEX IF NOT EXISTS idx_parked_turns_kind_owner ON parked_turns(kind, owner)`); err != nil {
		return fmt.Errorf("park store: create owner index: %w", err)
	}
	return nil
}

// columnExists reports whether the named column is present in the given
// table using PRAGMA table_info (same idempotency-check pattern as the
// queue store's migrate).
func (s *SQLiteParkStore) columnExists(table, column string) (bool, error) {
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
	store := &SQLiteParkStore{db: db, logger: logger}
	if err := store.migrate(context.Background()); err != nil {
		db.Close()
		return nil, fmt.Errorf("park store: owner migration failed: %w", err)
	}
	logger.Info("park store initialized", "path", dbPath)
	return store, nil
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
			resume_at, attempt, max_attempts, turn_payload, created_at, owner
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(kind, park_key) DO UPDATE SET
			session_id      = excluded.session_id,
			conversation_id = excluded.conversation_id,
			agent_id        = excluded.agent_id,
			class           = excluded.class,
			resume_at       = excluded.resume_at,
			attempt         = excluded.attempt,
			max_attempts    = excluded.max_attempts,
			turn_payload    = excluded.turn_payload,
			created_at      = excluded.created_at,
			owner           = excluded.owner
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
		rec.Owner,
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
//
// Owner filtering (C-K1): a sticky SetOwnerFilter (test/observability
// seam) narrows the visible rows; parkers wanting their OWN rows should
// use LoadOwned (the OwnerScopedPersistence capability), which never
// touches the sticky filter. An unfiltered Load keeps the pre-owner
// kind-only contract byte-identical for unwired callers.
func (s *SQLiteParkStore) Load(ctx context.Context, kind ParkKind, now time.Time) ([]ParkedTurnRecord, []string, error) {
	return s.load(ctx, kind, s.currentOwnerFilter(), now)
}

// LoadOwned is the C-K1 owner-scoped Load: same semantics as Load but
// restricted to rows whose owner column equals owner. Rows stamped by a
// different parker are invisible — the partition that closes the C1
// double-execution window (quota watcher and throttle parker sharing
// ParkKindChat can no longer re-arm each other's rows).
func (s *SQLiteParkStore) LoadOwned(ctx context.Context, kind ParkKind, owner string, now time.Time) ([]ParkedTurnRecord, []string, error) {
	return s.load(ctx, kind, owner, now)
}

// load is the shared transactional reader behind Load/LoadOwned. A
// non-empty ownerFilter adds the owner predicate to every query (SELECT
// and the prune DELETEs, so one parker's Load can never delete another
// owner's expired row).
func (s *SQLiteParkStore) load(ctx context.Context, kind ParkKind, ownerFilter string, now time.Time) ([]ParkedTurnRecord, []string, error) {
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
		if rerr := tx.Rollback(); rerr != nil && !errors.Is(rerr, sql.ErrTxDone) {
			s.logger.Debug("park store: post-commit rollback", "error", rerr)
		}
	}()

	query := `
		SELECT park_key, session_id, conversation_id, agent_id, class,
		       resume_at, attempt, max_attempts, turn_payload, owner
		FROM parked_turns
		WHERE kind = ?`
	args := []any{string(kind)}
	if ownerFilter != "" {
		query += ` AND owner = ?`
		args = append(args, ownerFilter)
	}
	query += `
		ORDER BY resume_at ASC, id ASC
	`
	rows, err := tx.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, nil, fmt.Errorf("park store: load query: %w", err)
	}

	var records []ParkedTurnRecord
	var keys []string
	var expired []string
	var unknown []string
	for rows.Next() {
		var (
			key, sessID, convID, agentID, classRaw, resumeAtRaw, payload, owner string
			attempt, maxAttempts                                                int
		)
		if err := rows.Scan(&key, &sessID, &convID, &agentID, &classRaw,
			&resumeAtRaw, &attempt, &maxAttempts, &payload, &owner); err != nil {
			defer rows.Close()
			return nil, nil, fmt.Errorf("park store: load scan: %w", err)
		}
		class, ok := parseParkClass(classRaw)
		if !ok {
			// C-K1 defense in depth: an unknown class value cannot be
			// routed to ANY resume callback safely (the old silent
			// FailureQuota fallback is how a throttle row became a quota
			// re-execution). Drop the row with an error log.
			unknown = append(unknown, key)
			s.logger.Error("park store: parked row carries an unknown class — dropping",
				"kind", string(kind),
				"park_key", key,
				"class", classRaw,
			)
			continue
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
			Class:          class,
			ResumeAt:       resumeAt,
			Attempt:        attempt,
			MaxAttempts:    maxAttempts,
			TurnPayload:    json.RawMessage(payload),
			Owner:          owner,
		})
		keys = append(keys, key)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, nil, fmt.Errorf("park store: load iterate: %w", err)
	}
	rows.Close()

	deleteStmt := `DELETE FROM parked_turns WHERE kind = ? AND park_key = ?`
	deleteArgs := func(key string) []any { return []any{string(kind), key} }
	if ownerFilter != "" {
		deleteStmt += ` AND owner = ?`
	}
	for _, key := range expired {
		argsList := deleteArgs(key)
		if ownerFilter != "" {
			argsList = append(argsList, ownerFilter)
		}
		if _, err := tx.ExecContext(ctx, deleteStmt, argsList...); err != nil {
			return nil, nil, fmt.Errorf("park store: prune expired: %w", err)
		}
	}
	for _, key := range unknown {
		argsList := deleteArgs(key)
		if ownerFilter != "" {
			argsList = append(argsList, ownerFilter)
		}
		if _, err := tx.ExecContext(ctx, deleteStmt, argsList...); err != nil {
			return nil, nil, fmt.Errorf("park store: drop unknown-class row: %w", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, nil, fmt.Errorf("park store: load commit: %w", err)
	}
	return records, keys, nil
}

// parseParkClass inverts parkClassString. Since C-K1 (tree 04 leaf 01) an
// unknown/empty class NO LONGER silently falls back to FailureQuota — that
// fallback is the misroute half of the C1 double-execution: an
// undifferentiated row must not be re-dispatched to the quota router.
// The caller (store Load) drops the row with an error log when ok=false.
func parseParkClass(class string) (llm.FailureClass, bool) {
	switch class {
	case "throttle":
		return llm.FailureThrottle, true
	case "quota":
		return llm.FailureQuota, true
	case "none":
		return llm.FailureNone, true
	default:
		return llm.FailureNone, false
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
