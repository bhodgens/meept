package session

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"time"
)

// SQLiteDesignationHistoryStore persists designation transitions in SQLite.
type SQLiteDesignationHistoryStore struct {
	db     *sql.DB
	logger *slog.Logger
}

// NewSQLiteDesignationHistoryStore creates a new SQLite-backed designation history store.
// The caller must ensure migrate() has been called on the database (it is called
// automatically by the session store's migration path).
func NewSQLiteDesignationHistoryStore(db *sql.DB, logger *slog.Logger) *SQLiteDesignationHistoryStore {
	if logger == nil {
		logger = slog.Default()
	}
	return &SQLiteDesignationHistoryStore{db: db, logger: logger}
}

// migrateDesignationHistory creates the designation_history table if it does not exist.
// This is called from the parent SQLiteStore.migrate() method.
func migrateDesignationHistory(db *sql.DB) error {
	schema := `
	CREATE TABLE IF NOT EXISTS designation_history (
		id           INTEGER PRIMARY KEY AUTOINCREMENT,
		session_id   TEXT NOT NULL,
		from_status  TEXT NOT NULL DEFAULT '',
		to_status    TEXT NOT NULL,
		reason       TEXT NOT NULL DEFAULT '',
		timestamp    TEXT NOT NULL
	);
	CREATE INDEX IF NOT EXISTS idx_designation_history_session ON designation_history(session_id);
	`
	if _, err := db.Exec(schema); err != nil {
		return fmt.Errorf("failed to create designation_history table: %w", err)
	}
	return nil
}

// Record persists a designation transition.
func (s *SQLiteDesignationHistoryStore) Record(ctx context.Context, sessionID string, from, to DesignationStatus, reason string) error {
	if sessionID == "" {
		return fmt.Errorf("sessionID is required")
	}
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO designation_history (session_id, from_status, to_status, reason, timestamp) VALUES (?, ?, ?, ?, ?)`,
		sessionID, string(from), string(to), reason, now)
	if err != nil {
		return fmt.Errorf("failed to record designation history: %w", err)
	}
	s.logger.Debug("designation transition recorded",
		"session_id", sessionID,
		"from", string(from),
		"to", string(to),
		"reason", reason)
	return nil
}

// List returns the designation history for a session, ordered oldest-first.
func (s *SQLiteDesignationHistoryStore) List(ctx context.Context, sessionID string) ([]DesignationHistoryEntry, error) {
	if sessionID == "" {
		return nil, fmt.Errorf("sessionID is required")
	}
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, session_id, from_status, to_status, reason, timestamp FROM designation_history WHERE session_id = ? ORDER BY id ASC`,
		sessionID)
	if err != nil {
		return nil, fmt.Errorf("failed to query designation history: %w", err)
	}
	defer rows.Close()

	var entries []DesignationHistoryEntry
	for rows.Next() {
		var entry DesignationHistoryEntry
		var fromStatus, toStatus, timestampStr string
		if err := rows.Scan(&entry.ID, &entry.SessionID, &fromStatus, &toStatus, &entry.Reason, &timestampStr); err != nil {
			return nil, fmt.Errorf("failed to scan designation history row: %w", err)
		}
		entry.FromStatus = DesignationStatus(fromStatus)
		entry.ToStatus = DesignationStatus(toStatus)
		if t, err := time.Parse(time.RFC3339, timestampStr); err == nil {
			entry.Timestamp = t
		}
		entries = append(entries, entry)
	}
	return entries, rows.Err()
}
