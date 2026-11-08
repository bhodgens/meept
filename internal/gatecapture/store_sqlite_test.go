package gatecapture

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// sampleGateRecord builds a vocabulary-valid row for the file-mode tests.
// insert() is the raw row writer (Store.Record mints ID/Ts upstream), so the
// helper stamps them here.
func sampleGateRecord() *CommandGateRecord {
	return &CommandGateRecord{
		ID:            "rec-modes-1",
		Ts:            time.Now().UTC(),
		Chokepoint:    ChokepointDecisionGateDeny,
		Command:       "rm -rf /tmp/x",
		ContextBlob:   "{}",
		HumanDecision: "denied",
	}
}

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

	// bughunt 2026-11-07 infra-fixes F-1: with WAL active, argv rows live in
	// the -wal sidecar until checkpoint. SQLite creates it at 0644; the
	// tighten must land AFTER the first open+migrate (pre-open chmods no-op
	// because the sidecar does not exist yet). Insert first so the WAL is
	// definitely materialized, then stat WHILE the handle is open (a clean
	// close checkpoints and removes it).
	if err := gdb.insert(context.Background(), sampleGateRecord()); err != nil {
		t.Fatalf("insert: %v", err)
	}
	if perm := statMode(t, dbPath+"-wal"); perm&0o077 != 0 {
		t.Errorf("live -wal mode = 0%o, want owner-only (bughunt 2026-11-07 F-1)", perm)
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

// TestOpenGateDBRepairsPreexistingDir pins bughunt 2026-11-07 infra-fixes
// F-2: os.MkdirAll applies the mode only to dirs it CREATES, so an install
// from the pre-fix build (0755 dir) never got repaired at reopen. The
// commit-message claim "installs from the old build are repaired at next
// boot" must actually hold: the reopen tightens the dir, the db file, AND
// the WAL sidecars.
func TestOpenGateDBRepairsPreexistingDir(t *testing.T) {
	tmp := t.TempDir()
	dbDir := filepath.Join(tmp, "gate")
	dbPath := filepath.Join(dbDir, "gatecapture.db")

	// Simulate the old build: world-readable dir + world-readable db file,
	// then a fresh open (the "next boot").
	if err := os.MkdirAll(dbDir, 0o755); err != nil {
		t.Fatalf("mkdir pre-fix style: %v", err)
	}
	if err := os.WriteFile(dbPath, nil, 0o644); err != nil {
		t.Fatalf("create pre-fix style db: %v", err)
	}

	gdb, err := openGateDB(dbPath)
	if err != nil {
		t.Fatalf("openGateDB repair boot: %v", err)
	}
	if err := gdb.insert(context.Background(), sampleGateRecord()); err != nil {
		t.Fatalf("insert: %v", err)
	}
	if perm := statMode(t, dbPath+"-wal"); perm&0o077 != 0 {
		t.Errorf("repaired -wal mode = 0%o, want owner-only", perm)
	}
	_ = gdb.close()

	if perm := statMode(t, dbDir); perm&0o077 != 0 {
		t.Errorf("repaired pre-existing dir mode = 0%o, want no group/other bits (bughunt 2026-11-07 F-2)", perm)
	}
	if perm := statMode(t, dbPath); perm&0o077 != 0 {
		t.Errorf("repaired pre-existing db mode = 0%o, want owner-only", perm)
	}
}

func statMode(t *testing.T, path string) os.FileMode {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat %s: %v", path, err)
	}
	return info.Mode().Perm()
}
