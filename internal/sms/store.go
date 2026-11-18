package sms

// Durable SMS store (leaf 03/01) — one SQLite database at
// $MEEPT_HOME/sms/sms.db holding two tables:
//
//   - sms_events_raw: append-only audit of every Telnyx messaging webhook
//     event, idempotent on event_id (INSERT OR IGNORE: a provider
//     redelivery is an ack-noop, never an error).
//   - sms_message_state: one row per provider message id, updated with
//     out-of-order protection — a write applies only when its provider
//     timestamp is >= the stored row's (monotonic; equal timestamps apply).
//
// Ported schema: mirrors strategist-usa go-web-hooks
// sms_events_raw + sms_message_state (001_initial_schema.sql:10-67 raw
// events, :75+ timestamp-based conflict resolution on the state table),
// adapted to the SQLite house convention — modernc driver, WAL,
// busy_timeout, synchronous(NORMAL), MaxOpenConns(1) — the same posture
// as internal/voice/callstore.go. Times are RFC3339 TEXT throughout (the
// same convention as the call store / effects ledger); the monotonic
// comparison and the RecentEvents window are string compares on those
// RFC3339 stamps, exactly as the contract pins them.
//
// Contract: docs/plans/20261110-telnyx-texting/master.md §4 (shapes frozen
// by the gate test 00T §T1 header; do not rename).

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	_ "modernc.org/sqlite" // sqlite driver registration (house convention)
)

// smsStoreSchema creates the two tables plus the reader index. Every
// column the contract pins is in the base CREATE, so a fresh database is
// fully migrated here; future column additions go through the guarded
// ALTER idiom (callstore migrationAddColumn / session store
// migrationAddColumn precedent: duplicate-column errors are the expected
// steady state on already-migrated databases, anything else is surfaced
// loudly).
const smsStoreSchema = `
CREATE TABLE IF NOT EXISTS sms_events_raw (
    event_id    TEXT PRIMARY KEY,
    event       TEXT NOT NULL DEFAULT '',
    direction   TEXT NOT NULL DEFAULT '',
    src         TEXT NOT NULL DEFAULT '',
    dst         TEXT NOT NULL DEFAULT '',
    body        TEXT NOT NULL DEFAULT '',
    payload     TEXT NOT NULL DEFAULT '',
    provider_ts TEXT NOT NULL DEFAULT '',
    received_at TEXT NOT NULL DEFAULT ''
);

CREATE TABLE IF NOT EXISTS sms_message_state (
    provider_message_id TEXT PRIMARY KEY,
    dst                 TEXT NOT NULL DEFAULT '',
    status              TEXT NOT NULL DEFAULT '',
    err                 TEXT NOT NULL DEFAULT '',
    updated_at          TEXT NOT NULL DEFAULT '',
    event_id            TEXT NOT NULL DEFAULT ''
);

CREATE INDEX IF NOT EXISTS idx_sms_events_raw_src_ts
    ON sms_events_raw(src, provider_ts);
`

// Event is one row of sms_events_raw (contract §4). All fields are the
// verbatim webhook-derived values the daemon arms with; ProviderTS and
// ReceivedAt are RFC3339 stamps.
type Event struct {
	EventID    string // column event_id  (TEXT PRIMARY KEY)
	Event      string // column event     (event_type, e.g. message.received)
	Direction  string // column direction ("inbound" | "outbound")
	Src        string // column src       (E.164 sender)
	Dst        string // column dst       (E.164 recipient)
	Body       string // column body      (message text)
	Payload    string // column payload   (raw JSON, audit copy)
	ProviderTS string // column provider_ts  (RFC3339, provider event time)
	ReceivedAt string // column received_at (RFC3339, webhook receipt time)
}

// State is one row of sms_message_state (contract §4). UpdatedAt carries
// the provider event time of the last applied event (the strategist
// last_event_ts semantics pinned by the gate): the monotonic guard
// compares this column.
type State struct {
	ProviderMessageID string // column provider_message_id (TEXT PRIMARY KEY)
	Dst               string // column dst
	Status            string // column status
	Err               string // column err
	UpdatedAt         string // column updated_at (RFC3339, provider event time)
	EventID           string // column event_id (last event that applied)
}

// Store is the SQLite SMS store. A single connection (SetMaxOpenConns(1))
// serializes writers in-process — the house WAL posture — so concurrent
// webhook appends queue on the busy_timeout instead of racing.
type Store struct {
	db     *sql.DB
	logger *slog.Logger
}

// Open opens (and migrates) the SMS store at dbPath. The daemon passes
// config.MeeptPath("sms", "sms.db"); logger nil falls back to
// slog.Default(). Idempotent on an existing database: schema creation is
// IF-NOT-EXISTS, data survives reopen.
func Open(dbPath string, logger *slog.Logger) (*Store, error) {
	if logger == nil {
		logger = slog.Default()
	}
	db, err := sql.Open("sqlite", dbPath+"?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)&_pragma=synchronous(NORMAL)")
	if err != nil {
		return nil, fmt.Errorf("sms: failed to open database: %w", err)
	}
	db.SetMaxOpenConns(1)
	if _, err := db.Exec(smsStoreSchema); err != nil {
		db.Close()
		return nil, fmt.Errorf("sms: failed to migrate sms store: %w", err)
	}
	return &Store{db: db, logger: logger}, nil
}

// Close closes the underlying database.
func (s *Store) Close() error {
	if s == nil || s.db == nil {
		return nil
	}
	return s.db.Close()
}

// AppendEvent writes one raw event. INSERT OR IGNORE on the event_id
// PRIMARY KEY makes redelivery idempotent: the return is
// (applied, err) with false,nil for a duplicate event id — an ack-noop
// the webhook handler answers 200 with, never an error that would make
// Telnyx retry forever.
func (s *Store) AppendEvent(ctx context.Context, ev Event) (bool, error) {
	res, err := s.db.ExecContext(ctx, `
INSERT OR IGNORE INTO sms_events_raw
    (event_id, event, direction, src, dst, body, payload, provider_ts, received_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		ev.EventID, ev.Event, ev.Direction, ev.Src, ev.Dst,
		ev.Body, ev.Payload, ev.ProviderTS, ev.ReceivedAt)
	if err != nil {
		return false, fmt.Errorf("sms: append event %q: %w", ev.EventID, err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("sms: append event %q rows affected: %w", ev.EventID, err)
	}
	return n > 0, nil
}

// UpsertState applies one state write with the monotonic guard ported
// from strategist 001_initial_schema.sql:75+ timestamp-based conflict
// resolution: the write lands only when its provider timestamp
// (State.UpdatedAt, the row's updated_at column) is >= the existing
// row's — equal timestamps apply (contract wording "provider_ts >=
// existing"), older writes are ignored and leave the row untouched. The
// read-compare-write runs in ONE transaction (deferred tx on the
// serialized single connection) so the guard cannot interleave with a
// concurrent writer. Return is (applied, err): false = ignored
// out-of-order write.
func (s *Store) UpsertState(ctx context.Context, st State) (bool, error) {
	if st.ProviderMessageID == "" {
		return false, fmt.Errorf("sms: upsert state: provider_message_id is required")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return false, fmt.Errorf("sms: upsert state: begin tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }() // no-op after commit; guards early returns

	var existing string
	err = tx.QueryRowContext(ctx,
		"SELECT updated_at FROM sms_message_state WHERE provider_message_id = ?",
		st.ProviderMessageID).Scan(&existing)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		// Fresh row: always applies.
	case err != nil:
		return false, fmt.Errorf("sms: upsert state %q read guard: %w", st.ProviderMessageID, err)
	default:
		// RFC3339 string compare (contract-pinned): older-than existing is
		// ignored, equal-or-newer applies.
		if strings.Compare(st.UpdatedAt, existing) < 0 {
			return false, nil
		}
	}

	_, err = tx.ExecContext(ctx, `
INSERT INTO sms_message_state
    (provider_message_id, dst, status, err, updated_at, event_id)
VALUES (?, ?, ?, ?, ?, ?)
ON CONFLICT(provider_message_id) DO UPDATE SET
    dst        = excluded.dst,
    status     = excluded.status,
    err        = excluded.err,
    updated_at = excluded.updated_at,
    event_id   = excluded.event_id`,
		st.ProviderMessageID, st.Dst, st.Status, st.Err, st.UpdatedAt, st.EventID)
	if err != nil {
		return false, fmt.Errorf("sms: upsert state %q write: %w", st.ProviderMessageID, err)
	}
	if err := tx.Commit(); err != nil {
		return false, fmt.Errorf("sms: upsert state %q commit: %w", st.ProviderMessageID, err)
	}
	return true, nil
}

// RecentEvents reads the window for one sender: src == src AND
// provider_ts >= since (RFC3339 string compare), newest-first by
// provider_ts, capped at limit. limit <= 0 yields no rows (defensive:
// LIMIT 0 is legal SQL, negative limits are not).
func (s *Store) RecentEvents(ctx context.Context, src string, since time.Time, limit int) ([]Event, error) {
	if limit <= 0 {
		return nil, nil
	}
	stamp := since.UTC().Format(time.RFC3339Nano)
	rows, err := s.db.QueryContext(ctx, `
SELECT event_id, event, direction, src, dst, body, payload, provider_ts, received_at
FROM sms_events_raw
WHERE src = ? AND provider_ts >= ?
ORDER BY provider_ts DESC
LIMIT ?`, src, stamp, limit)
	if err != nil {
		return nil, fmt.Errorf("sms: recent events for %q: %w", src, err)
	}
	defer rows.Close()

	var out []Event
	for rows.Next() {
		var ev Event
		if err := rows.Scan(&ev.EventID, &ev.Event, &ev.Direction, &ev.Src,
			&ev.Dst, &ev.Body, &ev.Payload, &ev.ProviderTS, &ev.ReceivedAt); err != nil {
			return nil, fmt.Errorf("sms: recent events scan: %w", err)
		}
		out = append(out, ev)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("sms: recent events iterate: %w", err)
	}
	return out, nil
}
