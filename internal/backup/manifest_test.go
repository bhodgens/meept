package backup

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestGenerateManifest(t *testing.T) {
	// Create a test database file
	testDir := t.TempDir()
	testDB := filepath.Join(testDir, "test.db")
	data := []byte("fake database content for manifest testing")
	if err := os.WriteFile(testDB, data, 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	manifest, err := GenerateManifest("test-node", []string{testDB})
	if err != nil {
		t.Fatalf("GenerateManifest: %v", err)
	}
	if manifest == nil {
		t.Fatal("expected non-nil manifest")
	}
	if manifest.NodeID != "test-node" {
		t.Errorf("NodeID: got %q, want %q", manifest.NodeID, "test-node")
	}
	if manifest.Timestamp.IsZero() {
		t.Error("expected non-zero timestamp")
	}
	if len(manifest.Databases) != 1 {
		t.Fatalf("expected 1 database, got %d", len(manifest.Databases))
	}

	db := manifest.Databases[0]
	if db.Name != "test.db" {
		t.Errorf("Name: got %q, want %q", db.Name, "test.db")
	}
	if db.CompressedSize <= 0 {
		t.Errorf("expected positive compressed size, got %d", db.CompressedSize)
	}
	if db.UncompressedSize != int64(len(data)) {
		t.Errorf("UncompressedSize: got %d, want %d", db.UncompressedSize, len(data))
	}
	if db.SHA256 == "" {
		t.Error("expected non-empty SHA256")
	}
	if db.CompressedPath == "" {
		t.Error("expected non-empty compressed path")
	}
}

func TestGenerateManifest_NoPaths(t *testing.T) {
	_, err := GenerateManifest("test-node", []string{})
	if err != ErrNoDatabases {
		t.Errorf("expected ErrNoDatabases, got %v", err)
	}
}

func TestGenerateManifest_NonExistentDB(t *testing.T) {
	_, err := GenerateManifest("test-node", []string{"/nonexistent/file.db"})
	if err == nil {
		t.Fatal("expected error for nonexistent DB")
	}
}

func TestManifestSave(t *testing.T) {
	manifest := &BackupManifest{
		NodeID:    "test-node",
		Timestamp: time.Date(2026, 6, 25, 12, 0, 0, 0, time.UTC),
		Databases: []DatabaseInfo{
			{
				Name:             "test.db",
				CompressedSize:   1024,
				UncompressedSize: 4096,
				SHA256:           "abc123def456",
				CompressedPath:   "/backups/test.db.zst",
			},
		},
		SyncMetadata: SyncMetadata{
			PeersSynced: []string{"peer-a", "peer-b"},
		},
	}

	manifestPath := filepath.Join(t.TempDir(), "manifest.json")
	if err := manifest.Save(manifestPath); err != nil {
		t.Fatalf("Save: %v", err)
	}

	// Verify file exists and is valid JSON
	data, err := os.ReadFile(manifestPath)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}

	var loaded map[string]interface{}
	if err := json.Unmarshal(data, &loaded); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}

	if loaded["node_id"] != "test-node" {
		t.Errorf("node_id: got %v, want test-node", loaded["node_id"])
	}
}

func TestLoadManifest(t *testing.T) {
	manifest := &BackupManifest{
		NodeID:    "load-test",
		Timestamp: time.Date(2026, 1, 15, 8, 30, 0, 0, time.UTC),
		Databases: []DatabaseInfo{
			{Name: "local.db", CompressedSize: 512, UncompressedSize: 2048, SHA256: "deadbeef"},
		},
	}

	manifestPath := filepath.Join(t.TempDir(), "manifest.json")
	if err := manifest.Save(manifestPath); err != nil {
		t.Fatalf("Save: %v", err)
	}

	loaded, err := LoadManifest(manifestPath)
	if err != nil {
		t.Fatalf("LoadManifest: %v", err)
	}

	if loaded.NodeID != "load-test" {
		t.Errorf("NodeID: got %q, want %q", loaded.NodeID, "load-test")
	}
	if loaded.Timestamp != manifest.Timestamp {
		t.Errorf("Timestamp: got %v, want %v", loaded.Timestamp, manifest.Timestamp)
	}
	if len(loaded.Databases) != 1 {
		t.Fatalf("Databases: got %d, want 1", len(loaded.Databases))
	}
}

func TestLoadManifest_NonExistent(t *testing.T) {
	_, err := LoadManifest("/nonexistent/manifest.json")
	if err != ErrManifestMissing {
		t.Errorf("expected ErrManifestMissing, got %v", err)
	}
}

func TestBackupPath(t *testing.T) {
	manifest := &BackupManifest{
		NodeID:    "my-node",
		Timestamp: time.Date(2026, 6, 25, 12, 0, 0, 0, time.UTC),
	}

	expected := filepath.Join("base", "2026-06-25", "my-node")
	if manifest.BackupPath("base") != expected {
		t.Errorf("BackupPath: got %q, want %q", manifest.BackupPath("base"), expected)
	}
}

func TestTotalCompressedSize(t *testing.T) {
	manifest := &BackupManifest{
		Databases: []DatabaseInfo{
			{CompressedSize: 100},
			{CompressedSize: 200},
			{CompressedSize: 300},
		},
	}

	if manifest.TotalCompressedSize() != 600 {
		t.Errorf("TotalCompressedSize: got %d, want 600", manifest.TotalCompressedSize())
	}
}

func TestTotalUncompressedSize(t *testing.T) {
	manifest := &BackupManifest{
		Databases: []DatabaseInfo{
			{UncompressedSize: 1024},
			{UncompressedSize: 2048},
		},
	}

	if manifest.TotalUncompressedSize() != 3072 {
		t.Errorf("TotalUncompressedSize: got %d, want 3072", manifest.TotalUncompressedSize())
	}
}
