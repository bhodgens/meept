package backup

import (
	"os"
	"path/filepath"
	"testing"
)

func TestGetLocalDBPaths(t *testing.T) {
	dir := t.TempDir()

	// Create local.db
	localDB := filepath.Join(dir, "local.db")
	if err := os.WriteFile(localDB, []byte("test"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	paths, err := GetLocalDBPaths(dir)
	if err != nil {
		t.Fatalf("GetLocalDBPaths: %v", err)
	}
	if len(paths) != 1 {
		t.Fatalf("expected 1 path, got %d", len(paths))
	}
	if paths[0] != localDB {
		t.Errorf("path: got %q, want %q", paths[0], localDB)
	}
}

func TestGetLocalDBPaths_SessionsDB(t *testing.T) {
	dir := t.TempDir()

	// Create only sessions.db (legacy)
	sessionsDB := filepath.Join(dir, "sessions.db")
	if err := os.WriteFile(sessionsDB, []byte("legacy"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	paths, err := GetLocalDBPaths(dir)
	if err != nil {
		t.Fatalf("GetLocalDBPaths: %v", err)
	}
	if len(paths) != 1 {
		t.Fatalf("expected 1 path, got %d", len(paths))
	}
	if paths[0] != sessionsDB {
		t.Errorf("path: got %q, want %q", paths[0], sessionsDB)
	}
}

func TestGetLocalDBPaths_MultipleDBs(t *testing.T) {
	dir := t.TempDir()

	// Create local.db + memory.db
	localDB := filepath.Join(dir, "local.db")
	if err := os.WriteFile(localDB, []byte("local"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	memoryDB := filepath.Join(dir, "memory.db")
	if err := os.WriteFile(memoryDB, []byte("memory"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	paths, err := GetLocalDBPaths(dir)
	if err != nil {
		t.Fatalf("GetLocalDBPaths: %v", err)
	}
	if len(paths) != 2 {
		t.Fatalf("expected 2 paths, got %d", len(paths))
	}
}

func TestGetLocalDBPaths_NoDBs(t *testing.T) {
	dir := t.TempDir()
	_, err := GetLocalDBPaths(dir)
	if err != ErrNoDatabases {
		t.Errorf("expected ErrNoDatabases, got %v", err)
	}
}

func TestGetLocalDBPaths_EmptyDir(t *testing.T) {
	_, err := GetLocalDBPaths("")
	if err == nil {
		t.Fatal("expected error for empty dir")
	}
}

func TestGetLocalDBPath(t *testing.T) {
	dir := t.TempDir()
	localDB := filepath.Join(dir, "local.db")
	if err := os.WriteFile(localDB, []byte("data"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	path, additional, err := GetLocalDBPath(dir)
	if err != nil {
		t.Fatalf("GetLocalDBPath: %v", err)
	}
	if path != localDB {
		t.Errorf("path: got %q, want %q", path, localDB)
	}
	if additional != nil {
		t.Errorf("expected nil additional paths, got %v", additional)
	}
}
