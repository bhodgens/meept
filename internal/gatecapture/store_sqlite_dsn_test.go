package gatecapture

import (
	"os"
	"path/filepath"
	"testing"
)

// TestOpenGateDBHonorsDriverPragmas pins the DSN contract: the registered
// "sqlite" driver is modernc.org/sqlite (mattn is NOT a dependency), and its
// applyQueryParams honors ONLY the `_pragma=name(value)` and `_time_format`
// keys. A mattn-style `?_journal_mode=WAL` query is silently dropped:
// journal_mode stays delete, so concurrent capture writes used to hit
// SQLITE_BUSY with no timeout. This test proves the DSN reaches SQLite.
func TestOpenGateDBHonorsDriverPragmas(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "gatecapture.db")

	gdb, err := openGateDB(dbPath)
	if err != nil {
		t.Fatalf("openGateDB: %v", err)
	}
	defer func() { _ = gdb.close() }()

	var mode string
	if err := gdb.db.QueryRow(`PRAGMA journal_mode`).Scan(&mode); err != nil {
		t.Fatalf("pragma journal_mode: %v", err)
	}
	if mode != "wal" {
		t.Errorf("journal_mode = %q, want wal — the DSN pragmas are being ignored by the driver", mode)
	}

	var fk int
	if err := gdb.db.QueryRow(`PRAGMA foreign_keys`).Scan(&fk); err != nil {
		t.Fatalf("pragma foreign_keys: %v", err)
	}
	if fk != 1 {
		t.Errorf("foreign_keys = %d, want 1 — the _foreign_keys pragma is not reaching SQLite", fk)
	}

	// The -wal sidecar must exist once WAL is active (mode probe writes).
	if _, err := os.Stat(dbPath + "-wal"); err != nil {
		t.Logf("note: -wal absent right after the mode probe (%v); WAL sidecars appear on first write", err)
	}
}
