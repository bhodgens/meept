package backup

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

// SyncStatus holds the sync status for a single peer.
type SyncStatus struct {
	PeerID         string
	LastSync       time.Time
	LastMergeStats *MergeStats
	Error          string
}

// SyncMetadataStore tracks sync state in a local SQLite database.
type SyncMetadataStore struct {
	db *sql.DB
}

// NewSyncMetadataStore creates and initializes a sync metadata store.
func NewSyncMetadataStore(db *sql.DB) *SyncMetadataStore {
	return &SyncMetadataStore{db: db}
}

// EnsureTable ensures the sync_metadata table exists.
func (s *SyncMetadataStore) EnsureTable() error {
	_, err := s.db.Exec(`
		CREATE TABLE IF NOT EXISTS sync_metadata (
			key   TEXT PRIMARY KEY,
			value TEXT NOT NULL
		)
	`)
	return err
}

// key returns the store key for a peer's last sync time.
func (s *SyncMetadataStore) key(peerID string) string {
	return "last_sync:" + peerID
}

// keyMergeStats returns the store key for a peer's last merge stats.
func (s *SyncMetadataStore) keyMergeStats(peerID string) string {
	return "last_merge_stats:" + peerID
}

// keyLastError returns the store key for a peer's last error.
func (s *SyncMetadataStore) keyLastError(peerID string) string {
	return "last_error:" + peerID
}

// GetLastSync returns the time of the last successful sync with the peer.
func (s *SyncMetadataStore) GetLastSync(peerID string) (time.Time, error) {
	var ts int64
	key := s.key(peerID)
	err := s.db.QueryRow("SELECT value FROM sync_metadata WHERE key = ?", key).Scan(&ts)
	if err != nil {
		if err == sql.ErrNoRows {
			return time.Time{}, nil
		}
		return time.Time{}, fmt.Errorf("sync_metadata get last_sync: %w", err)
	}
	return time.UnixMilli(ts).UTC(), nil
}

// SetLastSync records the time of a successful sync with the peer.
func (s *SyncMetadataStore) SetLastSync(peerID string, t time.Time) error {
	key := s.key(peerID)
	_, err := s.db.Exec("INSERT OR REPLACE INTO sync_metadata (key, value) VALUES (?, ?)",
		key, t.UnixMilli())
	if err != nil {
		return fmt.Errorf("sync_metadata set last_sync: %w", err)
	}
	return nil
}

// SetLastMergeStats records the merge statistics from the last sync.
func (s *SyncMetadataStore) SetLastMergeStats(peerID string, stats *MergeStats) error {
	key := s.keyMergeStats(peerID)
	data := fmt.Sprintf("{\"sessions\":%d,\"turns\":%d,\"memories\":%d,\"skipped\":%d,\"errors\":%d}",
		stats.SessionsMerged, stats.TurnsMerged, stats.MemoriesMerged, stats.Skipped, stats.Errors)
	_, err := s.db.Exec("INSERT OR REPLACE INTO sync_metadata (key, value) VALUES (?, ?)",
		key, data)
	if err != nil {
		slog.Warn("sync: failed to persist merge stats", "peer_id", peerID, "error", err)
	}
	return err
}

// SetLastError records the last error encountered for a peer.
func (s *SyncMetadataStore) SetLastError(peerID string, errMsg string) error {
	key := s.keyLastError(peerID)
	_, err := s.db.Exec("INSERT OR REPLACE INTO sync_metadata (key, value) VALUES (?, ?)",
		key, errMsg)
	if err != nil {
		slog.Warn("sync: failed to persist last error", "peer_id", peerID, "error", err)
	}
	return err
}

// GetAllSyncStatus returns the sync status for all known peers.
func (s *SyncMetadataStore) GetAllSyncStatus() (map[string]SyncStatus, error) {
	rows, err := s.db.Query("SELECT key, value FROM sync_metadata")
	if err != nil {
		return nil, fmt.Errorf("sync_metadata query all: %w", err)
	}
	defer rows.Close()

	result := make(map[string]SyncStatus)

	for rows.Next() {
		var key, value string
		if err := rows.Scan(&key, &value); err != nil {
			return nil, fmt.Errorf("sync_metadata scan row: %w", err)
		}

		if strings.HasPrefix(key, "last_sync:") {
			peerID := strings.TrimPrefix(key, "last_sync:")
			ts, _ := strconv.ParseInt(value, 10, 64)
			if peerID == "" {
				continue
			}
			st, exists := result[peerID]
			if !exists {
				st.PeerID = peerID
			}
			st.LastSync = time.UnixMilli(ts).UTC()
			result[peerID] = st
		} else if strings.HasPrefix(key, "last_error:") {
			peerID := strings.TrimPrefix(key, "last_error:")
			if peerID == "" {
				continue
			}
			st, exists := result[peerID]
			if !exists {
				st.PeerID = peerID
			}
			st.Error = value
			result[peerID] = st
		} else if strings.HasPrefix(key, "last_merge_stats:") {
			peerID := strings.TrimPrefix(key, "last_merge_stats:")
			if peerID == "" {
				continue
			}
			st, exists := result[peerID]
			if !exists {
				st.PeerID = peerID
			}
			var stats MergeStats
			if err := json.Unmarshal([]byte(value), &stats); err == nil {
				st.LastMergeStats = &stats
			}
			result[peerID] = st
		}
	}

	return result, nil
}

// ClearPeerStatus removes all sync metadata for a peer.
func (s *SyncMetadataStore) ClearPeerStatus(peerID string) error {
	keys := []string{s.key(peerID), s.keyMergeStats(peerID), s.keyLastError(peerID)}
	for _, k := range keys {
		_, err := s.db.Exec("DELETE FROM sync_metadata WHERE key = ?", k)
		if err != nil {
			return fmt.Errorf("sync_metadata clear: %w", err)
		}
	}
	return nil
}
