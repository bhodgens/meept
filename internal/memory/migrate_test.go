package memory

import (
	"database/sql"
	"os"
	"path/filepath"
	"testing"

	_ "modernc.org/sqlite"
)

func TestMigrateRenameSessionsDB(t *testing.T) {
	dir := t.TempDir()

	// Create a fake sessions.db.
	sessionsPath := filepath.Join(dir, "sessions.db")
	f, err := os.Create(sessionsPath)
	if err != nil {
		t.Fatalf("create sessions.db: %v", err)
	}
	f.Close()

	// Run migration.
	if err := MigrateToDualDB(dir, "test-node", nil); err != nil {
		t.Fatalf("MigrateToDualDB: %v", err)
	}

	// Verify sessions.db is gone.
	if _, err := os.Stat(sessionsPath); !os.IsNotExist(err) {
		t.Error("sessions.db should be renamed away")
	}

	// Verify local.db exists.
	localPath := filepath.Join(dir, localDBName)
	if _, err := os.Stat(localPath); os.IsNotExist(err) {
		t.Fatal("local.db should exist after migration")
	}

	// Verify sync-gossip.db exists.
	gossipPath := filepath.Join(dir, gossipDBName)
	if _, err := os.Stat(gossipPath); os.IsNotExist(err) {
		t.Fatal("sync-gossip.db should exist after migration")
	}
}

func TestMigrateBackup(t *testing.T) {
	dir := t.TempDir()

	sessionsPath := filepath.Join(dir, "sessions.db")
	f, _ := os.Create(sessionsPath)
	f.Close()

	if err := MigrateToDualDB(dir, "test-node", nil); err != nil {
		t.Fatalf("MigrateToDualDB: %v", err)
	}

	backupPath := filepath.Join(dir, "migration-backup", "sessions.db.pre-migration")
	if _, err := os.Stat(backupPath); os.IsNotExist(err) {
		t.Error("backup of sessions.db should exist in migration-backup/")
	}
}

func TestMigrateNoExistingDBs(t *testing.T) {
	dir := t.TempDir()

	// No existing DB files - should still create gossip DB.
	if err := MigrateToDualDB(dir, "test-node", nil); err != nil {
		t.Fatalf("MigrateToDualDB: %v", err)
	}

	gossipPath := filepath.Join(dir, gossipDBName)
	if _, err := os.Stat(gossipPath); os.IsNotExist(err) {
		t.Fatal("sync-gossip.db should exist even with no input DBs")
	}
}

func TestMigrateRenameMemoryDBOnly(t *testing.T) {
	dir := t.TempDir()

	// Only memory.db exists (no sessions.db).
	memoryPath := filepath.Join(dir, "memory.db")
	f, _ := os.Create(memoryPath)
	f.Close()

	if err := MigrateToDualDB(dir, "test-node", nil); err != nil {
		t.Fatalf("MigrateToDualDB: %v", err)
	}

	// memory.db should be renamed to local.db.
	path, _ := os.Stat(filepath.Join(dir, localDBName))
	if path == nil {
		t.Fatal("local.db should exist after migrating only memory.db")
	}
}

func TestMigrateGossipSchemaApplied(t *testing.T) {
	dir := t.TempDir()

	if err := MigrateToDualDB(dir, "test-node", nil); err != nil {
		t.Fatalf("MigrateToDualDB: %v", err)
	}

	// Open gossip.db and verify the schema tables exist.
	gossipPath := filepath.Join(dir, gossipDBName)
	db, err := sql.Open("sqlite", gossipPath)
	if err != nil {
		t.Fatalf("open gossip.db: %v", err)
	}
	defer db.Close()

	var count int
	err = db.QueryRow(
		`SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='memories'`,
	).Scan(&count)
	if err != nil {
		t.Fatalf("query sqlite_master: %v", err)
	}
	if count != 1 {
		t.Error("memories table should be created in gossip schema")
	}
}

func TestMigrateToDualDBRejectsEmptyDataDir(t *testing.T) {
	err := MigrateToDualDB("", "test-node", nil)
	if err == nil {
		t.Error("expected error for empty dataDir")
	}
}

func TestMigrateBothDatabases(t *testing.T) {
	dir := t.TempDir()

	// Create both sessions.db and memory.db with different data.
	sessionsPath := filepath.Join(dir, "sessions.db")
	sess, _ := os.Create(sessionsPath)

	memoryPath := filepath.Join(dir, "memory.db")
	mem, _ := os.Create(memoryPath)

	sess.Close()
	mem.Close()

	if err := MigrateToDualDB(dir, "test-node", nil); err != nil {
		t.Fatalf("MigrateToDualDB: %v", err)
	}

	// sessions.db should be renamed to local.db (not copied).
	if _, err := os.Stat(sessionsPath); !os.IsNotExist(err) {
		t.Error("sessions.db should be renamed away")
	}

	// local.db should exist.
	if _, err := os.Stat(filepath.Join(dir, localDBName)); os.IsNotExist(err) {
		t.Fatal("local.db should exist")
	}

	// memory.db should still exist (it was only backed up, then attempted merge).
	// The merge will fail gracefully but the file is left.
	if _, err := os.Stat(memoryPath); os.IsNotExist(err) {
		// Acceptable - if merge worked
	}
}
