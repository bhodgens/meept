package session

// Session projection watermark cache (contract S2, leaf 03-session-storage/02,
// task T3). Persisted rows are (session_id, key, ver, seq, val):
//   - ver is the fold unit's StateVersion at write time; a read whose live
//     unit carries a different ver DISCARDS the row (never migrates it),
//   - seq is the watermark: exactly how many messages the row has folded,
//   - val is the WHOLE post-change state serialized as JSON (never a delta).
//
// Disposition of invalid records is backup-and-skip (dsh projection-cache
// spec.ts): a corrupt or unreadable row costs a longer tail replay, never a
// wrong value — LoadSession skips rows whose val is not valid JSON after
// logging, and never fails the read.
//
// The table is created additively (CREATE TABLE IF NOT EXISTS) in the same
// migrate() path as the rest of the store schema.

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
)

// ProjectionCheckpointRow is one persisted watermark row.
type ProjectionCheckpointRow struct {
	Key string `json:"key"`
	Ver int    `json:"ver"`
	Seq int64  `json:"seq"`
	Val string `json:"val"`
}

// SessionProjectionCache persists projection checkpoints in sqlite.
type SessionProjectionCache struct {
	db *sql.DB
}

// projectionCacheSchema is the additive table DDL. Kept separate so both
// NewSessionProjectionCache (standalone/test use) and the store's migrate()
// create the identical shape.
const projectionCacheSchema = `
CREATE TABLE IF NOT EXISTS session_projection_cache (
	session_id TEXT NOT NULL,
	key        TEXT NOT NULL,
	ver        INTEGER NOT NULL,
	seq        INTEGER NOT NULL,
	val        TEXT NOT NULL,
	PRIMARY KEY (session_id, key)
);
`

// NewSessionProjectionCache opens (creating the table if needed) the
// watermark cache over an existing db handle. The caller owns the handle's
// lifecycle; the cache never closes it.
func NewSessionProjectionCache(db *sql.DB) (*SessionProjectionCache, error) {
	if db == nil {
		return nil, fmt.Errorf("session: projection cache requires a non-nil db")
	}
	if _, err := db.Exec(projectionCacheSchema); err != nil {
		return nil, fmt.Errorf("session: create projection cache table: %w", err)
	}
	return &SessionProjectionCache{db: db}, nil
}

// Checkpoint upserts the given rows for one session in a single
// transaction. A nil/empty map is a no-op.
func (c *SessionProjectionCache) Checkpoint(sessionID string, rows map[string]ProjectionCheckpointRow) error {
	if c == nil || len(rows) == 0 {
		return nil
	}
	tx, err := c.db.Begin()
	if err != nil {
		return fmt.Errorf("session: projection checkpoint begin: %w", err)
	}
	defer func() {
		if rErr := tx.Rollback(); rErr != nil && !errors.Is(rErr, sql.ErrTxDone) {
			slog.Debug("session projection cache: rollback note", "error", rErr)
		}
	}()
	stmt, err := tx.Prepare(`
		INSERT INTO session_projection_cache (session_id, key, ver, seq, val)
		VALUES (?, ?, ?, ?, ?)
		ON CONFLICT (session_id, key)
		DO UPDATE SET ver = excluded.ver, seq = excluded.seq, val = excluded.val`)
	if err != nil {
		return fmt.Errorf("session: projection checkpoint prepare: %w", err)
	}
	defer stmt.Close()
	for _, row := range rows {
		if _, err := stmt.Exec(sessionID, row.Key, row.Ver, row.Seq, row.Val); err != nil {
			return fmt.Errorf("session: projection checkpoint upsert %q: %w", row.Key, err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("session: projection checkpoint commit: %w", err)
	}
	return nil
}

// LoadSession returns every usable cached row for one session. Invalid
// records are skipped (backup-and-skip): a row whose val is not valid JSON
// is logged and omitted rather than failing the whole read.
func (c *SessionProjectionCache) LoadSession(sessionID string) (map[string]ProjectionCheckpointRow, error) {
	if c == nil {
		return nil, nil //nolint:nilnil // nil cache is "no cached rows" (empty read), not an error; caller ranges the map
	}
	rows, err := c.db.Query(
		"SELECT key, ver, seq, val FROM session_projection_cache WHERE session_id = ?",
		sessionID)
	if err != nil {
		return nil, fmt.Errorf("session: projection cache load %s: %w", sessionID, err)
	}
	defer rows.Close()

	out := make(map[string]ProjectionCheckpointRow)
	for rows.Next() {
		var row ProjectionCheckpointRow
		if err := rows.Scan(&row.Key, &row.Ver, &row.Seq, &row.Val); err != nil {
			slog.Debug("session projection cache: skipping unreadable row",
				"session_id", sessionID, "error", err)
			continue
		}
		if !json.Valid([]byte(row.Val)) {
			slog.Debug("session projection cache: skipping row with invalid JSON val",
				"session_id", sessionID, "key", row.Key)
			continue
		}
		out[row.Key] = row
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("session: projection cache iterate %s: %w", sessionID, err)
	}
	return out, nil
}

// RestoreFloor returns the lowest usable watermark among the given rows —
// the one-below anchor a cold restore replays from (dsh index.ts:579-589).
// An empty set returns 0 (replay from the beginning).
func (c *SessionProjectionCache) RestoreFloor(rows map[string]ProjectionCheckpointRow) (int64, error) {
	if len(rows) == 0 {
		return 0, nil
	}
	floor := int64(-1)
	for _, row := range rows {
		if floor < 0 || row.Seq < floor {
			floor = row.Seq
		}
	}
	if floor < 0 {
		return 0, nil
	}
	return floor, nil
}

// CachedSnapshot serves the ver-matching cached rows for one session as a
// projection snapshot. Rows whose val is not valid JSON are skipped. The
// registry consults the cache directly for ver-discard semantics; this
// helper exists for consumers that want the raw persisted surface.
func (c *SessionProjectionCache) CachedSnapshot(sessionID string, keys ...string) SessionProjectionSnapshot {
	snap := SessionProjectionSnapshot{
		Values: make(map[string]any),
		Seqs:   make(map[string]int64),
	}
	rows, err := c.LoadSession(sessionID)
	if err != nil {
		slog.Debug("session projection cache: cached snapshot load failed",
			"session_id", sessionID, "error", err)
		return snap
	}
	wanted := keys
	if len(wanted) == 0 {
		wanted = make([]string, 0, len(rows))
		for k := range rows {
			wanted = append(wanted, k)
		}
	}
	for _, key := range wanted {
		row, ok := rows[key]
		if !ok {
			continue
		}
		snap.Values[key] = json.RawMessage(row.Val)
		snap.Seqs[key] = row.Seq
	}
	return snap
}
