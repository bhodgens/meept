package gatecapture

import (
	"os"
	"path/filepath"
	"testing"
)

// TestOpenGateDBFileModes pins the security F-1 fix (bughunt 2026-11-06):
// the capture DB can carry secret-bearing command argv, so the directory
// must be user-only (0700) and the DB file owner-only (0600) — sqlite's
// default create mode is 0644 (world-readable), which the old code shipped.
func TestOpenGateDBFileModes(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "sub", "gatecapture.db")

	gdb, err := openGateDB(dbPath)
	if err != nil {
		t.Fatalf("openGateDB: %v", err)
	}
	if err := gdb.close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	dInfo, err := os.Stat(filepath.Dir(dbPath))
	if err != nil {
		t.Fatalf("stat dir: %v", err)
	}
	if perm := dInfo.Mode().Perm(); perm&0o077 != 0 {
		t.Errorf("db dir mode = 0%o, want no group/other bits (0700 class)", perm)
	}

	fInfo, err := os.Stat(dbPath)
	if err != nil {
		t.Fatalf("stat db: %v", err)
	}
	if perm := fInfo.Mode().Perm(); perm&0o077 != 0 {
		t.Errorf("db file mode = 0%o, want owner-only (0600 class)", perm)
	}

	// Re-open over the existing file must not loosen the modes.
	gdb2, err := openGateDB(dbPath)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	_ = gdb2.close()
	if perm, err := os.Stat(dbPath); err == nil && perm.Mode().Perm()&0o077 != 0 {
		t.Errorf("after reopen db mode = 0%o, want owner-only", perm.Mode().Perm())
	}
}
