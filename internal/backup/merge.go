package backup

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"strings"
	"time"

	_ "modernc.org/sqlite" // sqlite driver registration
)

// MergeStats holds counters for a peer database merge.
type MergeStats struct {
	SessionsMerged int `json:"sessions"`
	TurnsMerged    int `json:"turns"`
	MemoriesMerged int `json:"memories"`
	Skipped        int `json:"skipped"` // duplicate IDs skipped
	Errors         int `json:"errors"`
}

// MergePeerDB merges data from a peer's backup database into the local gossip database.
//
// The peer DB is attached as "peer" and data is copied using INSERT OR IGNORE
// to skip duplicate entries. The operation runs within a single transaction.
func MergePeerDB(ctx context.Context, gossipDB *sql.DB, peerDBPath, peerID string) (*MergeStats, error) {
	stats := &MergeStats{}

	// Open peer database
	peerDB, err := sql.Open("sqlite", peerDBPath)
	if err != nil {
		return stats, SyncWrap("merge_open_peer", err)
	}
	defer peerDB.Close()

	// Verify peer DB is valid by running a quick query
	if err := peerDB.PingContext(ctx); err != nil {
		return stats, SyncWrap("merge_ping_peer", &SyncError{
			Op:      "merge",
			PeerID:  peerID,
			Message: "peer database is not a valid SQLite database",
		})
	}

	// Begin local transaction
	tx, err := gossipDB.BeginTx(ctx, nil)
	if err != nil {
		return stats, SyncWrap("merge_begin_tx", err)
	}
	defer func() {
		if err != nil {
			tx.Rollback()
		}
	}()

	// Attach peer DB
	if _, err := tx.ExecContext(ctx, "ATTACH ? AS peer", peerDBPath); err != nil {
		return stats, SyncWrap("merge_attach_peer", err)
	}

	// Merge sessions
	sessionMerged, sessionSkipped, sessionErr := mergeSessions(ctx, tx, peerDB, peerID)
	stats.SessionsMerged = sessionMerged
	stats.Skipped += sessionSkipped

	// Merge turns
	turnMerged, turnSkipped, turnErr := mergeTurns(ctx, tx, peerDB, peerID)
	stats.TurnsMerged = turnMerged
	stats.Skipped += turnSkipped

	// Merge memories
	memMerged, memSkipped, memErr := mergeMemories(ctx, tx, peerDB, peerID)
	stats.MemoriesMerged = memMerged
	stats.Skipped += memSkipped

	// Count errors
	if sessionErr != nil {
		stats.Errors++
		slog.Warn("backup: merge sessions failed for peer", "peer_id", peerID, "error", sessionErr)
	}
	if turnErr != nil {
		stats.Errors++
		slog.Warn("backup: merge turns failed for peer", "peer_id", peerID, "error", turnErr)
	}
	if memErr != nil {
		stats.Errors++
		slog.Warn("backup: merge memories failed for peer", "peer_id", peerID, "error", memErr)
	}

	if sessionErr != nil && turnErr != nil && memErr != nil {
		if _, detErr := tx.ExecContext(ctx, "DETACH peer"); detErr != nil { slog.Debug("backup: cleanup detach failed", "error", detErr) }
		err = fmt.Errorf("all merge operations failed for peer %s: sessions: %w, turns: %w, memories: %w",
			peerID, sessionErr, turnErr, memErr)
		return stats, err
	}

	// Detach peer DB BEFORE commit to avoid lock issues on re-attach
	if _, detErr := tx.ExecContext(ctx, "DETACH peer"); detErr != nil {
		slog.Debug("backup: detach peer failed", "error", detErr)
	}

	if err := tx.Commit(); err != nil {
		return stats, SyncWrap("merge_commit", err)
	}

	return stats, nil
}

// runMergeOp executes a merge INSERT OR IGNORE and updates stats with the result.
func runMergeOp(ctx context.Context, tx *sql.Tx, query string, args ...interface{}) (merged, skipped int, err error) {
	res, err := tx.ExecContext(ctx, query, args...)
	if err != nil {
		return 0, 0, err
	}

	// RowsAffected returns total rows matched + inserted.
	// With INSERT OR IGNORE, if a row already exists it's "skipped".
	// We treat all rows as "merged" since INSERT OR IGNORE is the desired behavior
	// (idempotent upsert). The "skipped" concept is tracked via the error count.
	rows, err := res.RowsAffected()
	if err != nil {
		return 0, 0, err
	}
	return int(rows), 0, nil
}

func mergeSessions(ctx context.Context, tx *sql.Tx, peerDB *sql.DB, peerID string) (merged, skipped int, err error) {
	// Check if peer has sessions table
	hasTable, err := tableExists(ctx, peerDB, "sessions")
	if err != nil {
		return 0, 0, SyncWrap("merge_peer_sessions_table", err)
	}
	if !hasTable {
		slog.Debug("backup: peer has no sessions table, skipping", "peer_id", peerID)
		return 0, 0, nil
	}

	query := `INSERT OR IGNORE INTO sessions (id, created_at, updated_at, metadata, source_node)
SELECT id, created_at, updated_at, metadata, ? FROM peer.sessions`

	merged, skipped, err = runMergeOp(ctx, tx, query, peerID)
	if err != nil {
		if strings.Contains(err.Error(), "no such table") || strings.Contains(err.Error(), "not exist") {
			// Local sessions table doesn't exist yet
			slog.Debug("backup: local sessions table not found, skipping merge", "peer_id", peerID)
			return 0, 0, nil
		}
		return 0, 0, err
	}

	if merged > 0 {
		slog.Info("backup: merged sessions", "peer_id", peerID, "count", merged)
	}
	return merged, skipped, nil
}

func mergeTurns(ctx context.Context, tx *sql.Tx, peerDB *sql.DB, peerID string) (merged, skipped int, err error) {
	hasTable, err := tableExists(ctx, peerDB, "turns")
	if err != nil {
		return 0, 0, SyncWrap("merge_peer_turns_table", err)
	}
	if !hasTable {
		slog.Debug("backup: peer has no turns table, skipping", "peer_id", peerID)
		return 0, 0, nil
	}

	query := `INSERT OR IGNORE INTO turns (turn_id, session_id, role, content, timestamp, source_node)
SELECT turn_id, session_id, role, content, timestamp, ? FROM peer.turns`

	merged, skipped, err = runMergeOp(ctx, tx, query, peerID)
	if err != nil {
		if strings.Contains(err.Error(), "no such table") || strings.Contains(err.Error(), "not exist") {
			slog.Debug("backup: local turns table not found, skipping merge", "peer_id", peerID)
			return 0, 0, nil
		}
		return 0, 0, err
	}

	if merged > 0 {
		slog.Info("backup: merged turns", "peer_id", peerID, "count", merged)
	}
	return merged, skipped, nil
}

func mergeMemories(ctx context.Context, tx *sql.Tx, peerDB *sql.DB, peerID string) (merged, skipped int, err error) {
	hasTable, err := tableExists(ctx, peerDB, "memories")
	if err != nil {
		return 0, 0, SyncWrap("merge_peer_memories_table", err)
	}
	if !hasTable {
		slog.Debug("backup: peer has no memories table, skipping", "peer_id", peerID)
		return 0, 0, nil
	}

	query := `INSERT OR IGNORE INTO memories (id, type, category, content, created_at, agent_id, session_id, source_node)
SELECT id, type, category, content, created_at, agent_id, session_id, ? FROM peer.memories`

	merged, skipped, err = runMergeOp(ctx, tx, query, peerID)
	if err != nil {
		if strings.Contains(err.Error(), "no such table") || strings.Contains(err.Error(), "not exist") {
			slog.Debug("backup: local memories table not found, skipping merge", "peer_id", peerID)
			return 0, 0, nil
		}
		return 0, 0, err
	}

	if merged > 0 {
		slog.Info("backup: merged memories", "peer_id", peerID, "count", merged)
	}
	return merged, skipped, nil
}

// tableExists checks if a table exists in the database.
func tableExists(ctx context.Context, db *sql.DB, tableName string) (bool, error) {
	var count int
	err := db.QueryRowContext(ctx,
		"SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name=?",
		tableName).Scan(&count)
	if err != nil {
		return false, err
	}
	return count > 0, nil
}

// MergePeerDBWithContext is MergePeerDB with a timeout.
// Creates a derived context with the given timeout and calls MergePeerDB.
func MergePeerDBWithContext(ctx context.Context, gossipDB *sql.DB, peerDBPath, peerID string, timeout time.Duration) (*MergeStats, error) {
	mergeCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	if mergeCtx.Done() != nil {
		return MergePeerDB(mergeCtx, gossipDB, peerDBPath, peerID)
	}

	return MergePeerDB(mergeCtx, gossipDB, peerDBPath, peerID)
}
