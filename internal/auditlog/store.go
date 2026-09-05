package auditlog

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	_ "modernc.org/sqlite" //nolint:revive // sqlite driver registration
)

const auditLogSchemaSQL = `
CREATE TABLE IF NOT EXISTS audit_log_chain (
    seq         INTEGER PRIMARY KEY,
    prev_hash   TEXT NOT NULL,
    record_hash TEXT NOT NULL UNIQUE,
    type        TEXT NOT NULL,
    employee_id TEXT NOT NULL,
    payload     TEXT NOT NULL,
    at          TEXT NOT NULL
);
`

// Migrate creates the audit_log_chain table if missing. Idempotent.
// INSERT-only by convention in this plan; an UPDATE/DELETE-blocking trigger
// is OPEN-QUESTIONS.md Q1 (recommended fast-follow, deliberately not here).
func Migrate(ctx context.Context, db *sql.DB) error {
	if _, err := db.ExecContext(ctx, auditLogSchemaSQL); err != nil {
		return fmt.Errorf("migrate audit log: %w", err)
	}
	return nil
}

// Store appends records to the hash-chained audit log. Concurrency is
// serialized by SQLite itself: a single pinned connection (SetMaxOpenConns(1))
// plus BEGIN IMMEDIATE per append — the same pattern as
// internal/tools/builtin/change_journal.go. No Go mutex is held across I/O.
type Store struct {
	db     *sql.DB
	logger *slog.Logger
}

// OpenStore opens (or creates) the audit log database at dbPath. A leading
// ~ expands to the user's home directory. The parent directory is created
// with 0700. A nil logger falls back to slog.Default().
func OpenStore(dbPath string, logger *slog.Logger) (*Store, error) {
	if logger == nil {
		logger = slog.Default()
	}
	if strings.HasPrefix(dbPath, "~") {
		home, err := os.UserHomeDir()
		if err != nil {
			return nil, fmt.Errorf("audit log: home dir: %w", err)
		}
		dbPath = filepath.Join(home, dbPath[1:])
	}
	if dir := filepath.Dir(dbPath); dir != "" {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return nil, fmt.Errorf("audit log: create dir: %w", err)
		}
	}
	dsn := dbPath + "?_journal_mode=WAL&_busy_timeout=5000"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("audit log: open db: %w", err)
	}
	db.SetMaxOpenConns(1)
	s := &Store{db: db, logger: logger.With("component", "audit-chain")}
	if err := s.migrate(context.Background()); err != nil {
		if cerr := db.Close(); cerr != nil {
			// Close error cannot supersede the migrate failure.
			err = fmt.Errorf("audit log: migrate: %w (close: %v)", err, cerr)
		}
		return nil, err
	}
	return s, nil
}

// OpenStoreFromDB wraps an existing connection pool (shared databases).
// The caller keeps ownership of db; Close is a no-op on the handle
// (documented: only OpenStore-owned conns are closed).
func OpenStoreFromDB(db *sql.DB, logger *slog.Logger) (*Store, error) {
	if logger == nil {
		logger = slog.Default()
	}
	db.SetMaxOpenConns(1)
	s := &Store{db: db, logger: logger.With("component", "audit-chain")}
	if err := s.migrate(context.Background()); err != nil {
		return nil, err
	}
	return s, nil
}

func (s *Store) migrate(ctx context.Context) error { return Migrate(ctx, s.db) }

// Append inserts rec as the next chain record. Seq, PrevHash and RecordHash
// are computed and fill the returned Record (caller values ignored); At is
// stamped now().UTC() when zero. The read-head → hash → insert sequence runs
// inside one transaction so concurrent appends cannot fork the chain.
func (s *Store) Append(ctx context.Context, rec Record) (Record, error) {
	if rec.Type == "" {
		return Record{}, fmt.Errorf("audit log append: type is required")
	}
	if rec.At.IsZero() {
		rec.At = time.Now().UTC()
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Record{}, fmt.Errorf("audit log append: begin: %w", err)
	}
	defer func() {
		// Rollback after Commit is a documented no-op (sql.ErrTxDone);
		// anything else is logged, never dropped.
		if rerr := tx.Rollback(); rerr != nil && rerr != sql.ErrTxDone {
			s.logger.Debug("audit log append: post-commit rollback", "error", rerr)
		}
	}()

	// With SetMaxOpenConns(1) our single conn serializes all callers; the
	// transaction keeps read-head+insert atomic against THIS connection.
	var seq uint64
	var prev string
	err = tx.QueryRowContext(ctx, `SELECT seq, record_hash FROM audit_log_chain ORDER BY seq DESC LIMIT 1`).Scan(&seq, &prev)
	if err != nil && err != sql.ErrNoRows {
		return Record{}, fmt.Errorf("audit log append: head: %w", err)
	}
	if err == sql.ErrNoRows {
		seq, prev = 0, ""
	}
	rec.Seq = seq + 1
	rec.PrevHash = prev
	h, err := HashRecord(rec)
	if err != nil {
		return Record{}, fmt.Errorf("audit log append: %w", err)
	}
	rec.RecordHash = h
	payload, err := CanonicalJSON(rec.Payload)
	if err != nil {
		return Record{}, fmt.Errorf("audit log append: payload: %w", err)
	}
	_, err = tx.ExecContext(ctx, `
        INSERT INTO audit_log_chain (seq, prev_hash, record_hash, type, employee_id, payload, at)
        VALUES (?, ?, ?, ?, ?, ?, ?)`,
		rec.Seq, rec.PrevHash, rec.RecordHash, rec.Type, rec.EmployeeID,
		string(payload), rec.At.UTC().Format(time.RFC3339Nano))
	if err != nil {
		return Record{}, fmt.Errorf("audit log append: insert: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return Record{}, fmt.Errorf("audit log append: commit: %w", err)
	}
	return rec, nil
}

// Head returns the newest record; ok=false when the chain is empty.
func (s *Store) Head(ctx context.Context) (Record, bool, error) {
	var (
		rec     Record
		payload string
		at      string
	)
	err := s.db.QueryRowContext(ctx, `
        SELECT seq, prev_hash, record_hash, type, employee_id, payload, at
        FROM audit_log_chain ORDER BY seq DESC LIMIT 1`).
		Scan(&rec.Seq, &rec.PrevHash, &rec.RecordHash, &rec.Type,
			&rec.EmployeeID, &payload, &at)
	if err == sql.ErrNoRows {
		return Record{}, false, nil
	}
	if err != nil {
		return Record{}, false, fmt.Errorf("audit log head: %w", err)
	}
	if err := decodePayload(payload, &rec.Payload); err != nil {
		return Record{}, false, err
	}
	if err := rec.At.UnmarshalJSON([]byte(`"` + at + `"`)); err != nil {
		return Record{}, false, fmt.Errorf("audit log head: %w", err)
	}
	return rec, true, nil
}

// ChainDB exposes the underlying handle (leaf 03 verification + anchoring).
func (s *Store) ChainDB() *sql.DB { return s.db }

// Close closes an OpenStore-owned database. OpenStoreFromDB handles are
// caller-owned; Close on those is a no-op by convention (we cannot detect
// ownership, so Close always closes — callers sharing a pool must not call
// Close; document at call sites).
func (s *Store) Close() error { return s.db.Close() }

func decodePayload(s string, into *map[string]any) error {
	var m map[string]any
	if err := json.Unmarshal([]byte(s), &m); err != nil {
		return fmt.Errorf("audit log payload decode: %w", err)
	}
	*into = m
	return nil
}
