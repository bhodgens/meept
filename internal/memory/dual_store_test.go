package memory

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
)

func TestNewDualStore(t *testing.T) {
	dir := t.TempDir()
	logger := slog.Default()

	ds, err := NewDualStore(dir, "test-node-1", logger)
	if err != nil {
		t.Fatalf("NewDualStore: %v", err)
	}
	defer ds.Close()

	if ds.localNodeID != "test-node-1" {
		t.Errorf("localNodeID = %q, want %q", ds.localNodeID, "test-node-1")
	}
	if ds.localDB == nil {
		t.Fatal("localDB is nil")
	}
	if ds.gossipDB == nil {
		t.Fatal("gossipDB is nil")
	}
}

func TestNewDualStoreRejectsEmptyNodeID(t *testing.T) {
	dir := t.TempDir()
	_, err := NewDualStore(dir, "", slog.Default())
	if err == nil {
		t.Fatal("expected error for empty nodeID")
	}
}

func TestDualStoreCreatesDBFiles(t *testing.T) {
	dir := t.TempDir()
	ds, err := NewDualStore(dir, "test-node", slog.Default())
	if err != nil {
		t.Fatalf("NewDualStore: %v", err)
	}
	defer ds.Close()

	localPath := filepath.Join(dir, localDBName)
	gossipPath := filepath.Join(dir, gossipDBName)

	if _, err := os.Stat(localPath); os.IsNotExist(err) {
		t.Fatal("local.db was not created")
	}
	if _, err := os.Stat(gossipPath); os.IsNotExist(err) {
		t.Fatal("sync-gossip.db was not created")
	}
}

func TestDualStoreSyncMetadata(t *testing.T) {
	dir := t.TempDir()
	ds, err := NewDualStore(dir, "node-1", slog.Default())
	if err != nil {
		t.Fatalf("NewDualStore: %v", err)
	}
	defer ds.Close()

	// Check that sync_metadata tables exist and have version markers.
	var val string
	err = ds.localDB.QueryRow("SELECT value FROM sync_metadata WHERE key='schema_version'").Scan(&val)
	if err != nil {
		t.Fatalf("local sync_metadata schema_version not found: %v", err)
	}
	if val != syncMetaVersion {
		t.Errorf("local schema_version = %q, want %q", val, syncMetaVersion)
	}

	err = ds.gossipDB.QueryRow("SELECT value FROM sync_metadata WHERE key='schema_version'").Scan(&val)
	if err != nil {
		t.Fatalf("gossip sync_metadata schema_version not found: %v", err)
	}
}

func TestDualStoreClose(t *testing.T) {
	dir := t.TempDir()
	ds, err := NewDualStore(dir, "test-node", slog.Default())
	if err != nil {
		t.Fatalf("NewDualStore: %v", err)
	}

	if err := ds.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	// Second close should be idempotent (no panic).
	if err := ds.Close(); err != nil {
		t.Fatalf("second Close: %v", err)
	}
}

func TestIsLocal(t *testing.T) {
	dir := t.TempDir()
	ds, err := NewDualStore(dir, "node-alpha", slog.Default())
	if err != nil {
		t.Fatalf("NewDualStore: %v", err)
	}
	defer ds.Close()

	if !ds.IsLocal("node-alpha") {
		t.Error("IsLocal(node-alpha) should be true")
	}
	if ds.IsLocal("node-beta") {
		t.Error("IsLocal(node-beta) should be false")
	}
}

func TestDualStoreEmptyReadReturnsNil(t *testing.T) {
	dir := t.TempDir()
	ds, err := NewDualStore(dir, "test-node", slog.Default())
	if err != nil {
		t.Fatalf("NewDualStore: %v", err)
	}
	defer ds.Close()

	results, err := ds.GetMemories(context.Background(), &MemoryQuery{Limit: 10})
	if err != nil {
		t.Fatalf("GetMemories: %v", err)
	}
	if results != nil {
		t.Errorf("expected nil results for empty store, got %d results", len(results))
	}
}
