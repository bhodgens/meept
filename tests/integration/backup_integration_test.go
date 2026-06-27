package integration

import (
	"context"
	"database/sql"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/backup"
	"github.com/caimlas/meept/internal/config"
	"github.com/caimlas/meept/internal/memory"
	"github.com/caimlas/meept/pkg/id"
)

// TestBackupSchedulerIntegration tests the backup scheduler lifecycle.
// TestBackupSchedulerIntegration tests the backup scheduler lifecycle.
func TestBackupSchedulerIntegration(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()

	// Create backup config with local repo
	backupCfg := config.BackupConfig{
		Enabled:       true,
		RepoURL:       filepath.Join(tmpDir, "backup-repo"),
		Schedule:      time.Hour,
		RetentionDays: 7,
		NodeID:        "test-node",
		CheckoutDir:   filepath.Join(tmpDir, "checkout"),
	}

	// Create scheduler
	logger := newTestLogger()
	sched, err := backup.NewGitBackupScheduler(backupCfg, logger)
	if err != nil {
		t.Fatalf("NewGitBackupScheduler failed: %v", err)
	}

	// Test: Stop scheduler without starting (should be no-op)
	sched.Stop()
	t.Log("Backup scheduler stopped successfully (was not started)")
}

// TestSyncPullerIntegration tests the sync puller lifecycle.
func TestSyncPullerIntegration(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()

	// Create destination database
	destDir := filepath.Join(tmpDir, "dest")
	destStore, err := memory.NewDualStore(destDir, "dest-node", logger)
	if err != nil {
		t.Fatalf("NewDualStore failed: %v", err)
	}
	defer destStore.Close()

	// Create sync config
	syncCfg := config.PeerSyncConfig{
		Enabled:      true,
		Peers:        []string{"peer-node"},
		RepoURL:      filepath.Join(tmpDir, "backup-repo"),
		PullSchedule: time.Minute,
	}

	// Create puller
	puller, err := backup.NewSyncPuller(syncCfg, nil, destStore.GossipDB())
	if err != nil {
		t.Fatalf("NewSyncPuller failed: %v", err)
	}
	defer puller.Stop()

	t.Log("Sync puller created successfully")
}

// TestTempManagerIntegration tests temp file creation and cleanup.
func TestTempManagerIntegration(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()

	// Create temp manager
	tempMgr, err := backup.NewTempManager(tmpDir)
	if err != nil {
		t.Fatalf("NewTempManager failed: %v", err)
	}

	// Create a test file and compress it
	testData := []byte("test sqlite database content")
	tempFile := filepath.Join(tmpDir, "test.db")
	if err := os.WriteFile(tempFile, testData, 0644); err != nil {
		t.Fatalf("WriteFile failed: %v", err)
	}

	compressedFile := filepath.Join(tmpDir, "compressed.db")
	if _, err := backup.CompressFile(tempFile, compressedFile); err != nil {
		t.Fatalf("CompressFile failed: %v", err)
	}
	compressedFile += ".zst" // CompressFile adds .zst suffix

	// Test: ReservePeerDB (decompress)
	tempPath, err := tempMgr.ReservePeerDB(compressedFile)
	if err != nil {
		t.Fatalf("ReservePeerDB failed: %v", err)
	}
	defer tempMgr.Remove(tempPath)

	// Verify temp file exists
	if _, err := os.Stat(tempPath); os.IsNotExist(err) {
		t.Errorf("Temp file %s not created", tempPath)
	}

	// Test: Cleanup
	err = tempMgr.Cleanup()
	if err != nil {
		t.Fatalf("Cleanup failed: %v", err)
	}

	// Verify temp file removed
	if _, err := os.Stat(tempPath); err == nil {
		t.Errorf("Temp file %s not cleaned up", tempPath)
	}

	t.Log("Temp manager integration test completed")
}

// TestSyncMetadataStoreIntegration tests metadata persistence.
func TestSyncMetadataStoreIntegration(t *testing.T) {
	t.Parallel()

	_ = context.Background() // kept for API consistency
	tmpDir := t.TempDir()

	// Create in-memory SQLite DB for metadata store
	dbPath := filepath.Join(tmpDir, "metadata.db")
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("sql.Open failed: %v", err)
	}
	defer db.Close()

	// Create metadata store
	store := backup.NewSyncMetadataStore(db)

	// Ensure table exists
	if err := store.EnsureTable(); err != nil {
		t.Fatalf("EnsureTable failed: %v", err)
	}

	peerID := "test-peer"
	testTime := time.Now().Add(-time.Hour)

	// Test: Set last sync time
	err = store.SetLastSync(peerID, testTime)
	if err != nil {
		t.Fatalf("SetLastSync failed: %v", err)
	}

	// Test: Get last sync time
	lastSync, err := store.GetLastSync(peerID)
	if err != nil {
		t.Fatalf("GetLastSync failed: %v", err)
	}

	if lastSync.IsZero() {
		t.Error("LastSync should not be zero")
	}

	// Test: Set merge stats
	stats := &backup.MergeStats{
		SessionsMerged: 5,
		TurnsMerged:    50,
		MemoriesMerged: 10,
	}
	err = store.SetLastMergeStats(peerID, stats)
	if err != nil {
		t.Fatalf("SetLastMergeStats failed: %v", err)
	}

	// Test: Set error
	err = store.SetLastError(peerID, "test error")
	if err != nil {
		t.Fatalf("SetLastError failed: %v", err)
	}

	// Test: Get all sync status
	statusMap, err := store.GetAllSyncStatus()
	if err != nil {
		t.Fatalf("GetAllSyncStatus failed: %v", err)
	}

	if _, ok := statusMap[peerID]; !ok {
		t.Error("Expected peer status in map")
	}

	t.Log("Sync metadata store integration test completed")
}


// Helper functions

var logger = slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))

func newTestLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func createTestSchemaAndData(t *testing.T, db *sql.DB) {
	t.Helper()

	// Create schema
	schema := `
		CREATE TABLE IF NOT EXISTS sessions (
			id TEXT PRIMARY KEY,
			created_at INTEGER,
			updated_at INTEGER,
			metadata BLOB
		);
		CREATE TABLE IF NOT EXISTS turns (
			turn_id TEXT PRIMARY KEY,
			session_id TEXT,
			role TEXT,
			content TEXT,
			timestamp INTEGER,
			source_node TEXT
		);
		CREATE TABLE IF NOT EXISTS memories (
			id TEXT PRIMARY KEY,
			type TEXT,
			category TEXT,
			content TEXT,
			created_at INTEGER,
			agent_id TEXT,
			session_id TEXT,
			source_node TEXT
		);
	`
	_, err := db.Exec(schema)
	if err != nil {
		t.Fatalf("Create schema failed: %v", err)
	}

	// Insert test data
	for i := 0; i < 5; i++ {
		sessionID := id.Generate("sess-")
		_, err = db.Exec(`
			INSERT INTO sessions (id, created_at, updated_at, metadata)
			VALUES (?, ?, ?, ?)
		`, sessionID, time.Now().UnixNano(), time.Now().UnixNano(), []byte(`{}`))
		if err != nil {
			t.Fatalf("Insert session failed: %v", err)
		}

		for j := 0; j < 10; j++ {
			_, err = db.Exec(`
				INSERT INTO turns (turn_id, session_id, role, content, timestamp)
				VALUES (?, ?, ?, ?, ?)
			`, id.Generate("turn-"), sessionID, "user", "test", time.Now().UnixNano())
			if err != nil {
				t.Fatalf("Insert turn failed: %v", err)
			}
		}
	}
}
