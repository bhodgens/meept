package backup

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"
)

// BackupManifest describes a single backup set.
type BackupManifest struct {
	NodeID        string         `json:"node_id"`
	Timestamp     time.Time      `json:"timestamp"`
	Databases     []DatabaseInfo `json:"databases"`
	SyncMetadata  SyncMetadata   `json:"sync_metadata"`
}

// DatabaseInfo holds metadata for a single backed-up database file.
type DatabaseInfo struct {
	Name                 string `json:"name"`
	CompressedSize       int64  `json:"compressed_size"`
	UncompressedSize     int64  `json:"uncompressed_size"`
	SHA256               string `json:"sha256"`
	OriginalPath         string `json:"-"` // not serialized, used internally
	CompressedPath       string `json:"compressed_path"`
}

// SyncMetadata tracks sync state for future phases (config sync, gossip).
type SyncMetadata struct {
	LastPeerPull        string   `json:"last_peer_pull,omitempty"`
	PeersSynced         []string `json:"peers_synced,omitempty"`
	GossipEventsSent24h int      `json:"gossip_events_sent_24h"`
	GossipEventsRecv24h int      `json:"gossip_events_recv_24h"`
}

// GenerateManifest creates a manifest from a list of database paths.
func GenerateManifest(nodeID string, dbPaths []string) (*BackupManifest, error) {
	if len(dbPaths) == 0 {
		return nil, ErrNoDatabases
	}

	m := &BackupManifest{
		NodeID:    nodeID,
		Timestamp: time.Now().UTC(),
		Databases: make([]DatabaseInfo, 0, len(dbPaths)),
		SyncMetadata: SyncMetadata{
			PeersSynced: []string{},
		},
	}

	for _, dbPath := range dbPaths {
		name := filepath.Base(dbPath)

		// Compute uncompressed size
		info, err := os.Stat(dbPath)
		if err != nil {
			return nil, Wrap("manifest_stat", err)
		}

		// Compute SHA256
		sha, err := ComputeSHA256(dbPath)
		if err != nil {
			return nil, Wrap("manifest_sha256", err)
		}

		// Compress to temp path in same directory
		backupDir := filepath.Join(filepath.Dir(dbPath), "backups", time.Now().UTC().Format("2006-01-02"), nodeID)
		compressedPath := filepath.Join(backupDir, name+".zst")

		compressedSize, err := CompressFile(dbPath, compressedPath)
		if err != nil {
			return nil, Wrap("manifest_compress", err)
		}

		m.Databases = append(m.Databases, DatabaseInfo{
			Name:                 name,
			UncompressedSize:     info.Size(),
			CompressedSize:       compressedSize,
			SHA256:               sha,
			OriginalPath:         dbPath,
			CompressedPath:       compressedPath,
		})
	}

	return m, nil
}

// Save writes the manifest to the given path as JSON.
func (m *BackupManifest) Save(path string) error {
	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return Wrap("manifest_save_marshal", err)
	}

	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return Wrap("manifest_save_mkdir", err)
	}

	if err := os.WriteFile(path, data, 0o600); err != nil {
		return Wrap("manifest_save_write", err)
	}

	return nil
}

// LoadManifest reads a manifest from the given path.
func LoadManifest(path string) (*BackupManifest, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, ErrManifestMissing
		}
		return nil, Wrap("manifest_load_read", err)
	}

	var m BackupManifest
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, Wrap("manifest_load_unmarshal", err)
	}

	return &m, nil
}

// BackupPath returns the directory where this manifest's backup files are stored.
func (m *BackupManifest) BackupPath(basePath string) string {
	return filepath.Join(basePath, m.Timestamp.Format("2006-01-02"), m.NodeID)
}

// TotalCompressedSize returns the sum of compressed sizes across all databases.
func (m *BackupManifest) TotalCompressedSize() int64 {
	var total int64
	for _, db := range m.Databases {
		total += db.CompressedSize
	}
	return total
}

// TotalUncompressedSize returns the sum of uncompressed sizes across all databases.
func (m *BackupManifest) TotalUncompressedSize() int64 {
	var total int64
	for _, db := range m.Databases {
		total += db.UncompressedSize
	}
	return total
}
