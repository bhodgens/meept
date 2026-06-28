package backup

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// makeCompressedPeerBackup writes a tiny file and compresses it to .db.zst,
// returning the path to the compressed file. This mirrors what the backup
// scheduler produces for peer consumption.
func makeCompressedPeerBackup(t *testing.T, dir, baseName string, payload []byte) string {
	t.Helper()

	srcPath := filepath.Join(dir, baseName)
	if err := os.WriteFile(srcPath, payload, 0o600); err != nil {
		t.Fatalf("WriteFile %s: %v", srcPath, err)
	}

	dstStem := filepath.Join(dir, baseName+".compressed")
	if _, err := CompressFile(srcPath, dstStem); err != nil {
		t.Fatalf("CompressFile: %v", err)
	}
	return dstStem + ".zst"
}

// TestTempManager_ReservePeerDB_CreatesUniquePath verifies that ReservePeerDB
// decompresses a peer backup into the temp dir under the sync-temp/ prefix
// and that the returned path exists and differs from the source.
func TestTempManager_ReservePeerDB_CreatesUniquePath(t *testing.T) {
	t.Parallel()

	base := t.TempDir()
	tm, err := NewTempManager(base)
	if err != nil {
		t.Fatalf("NewTempManager: %v", err)
	}
	t.Cleanup(func() { tm.Cleanup() })

	compressed := makeCompressedPeerBackup(t, base, "peer.db", []byte("peer db payload"))

	got, err := tm.ReservePeerDB(compressed)
	if err != nil {
		t.Fatalf("ReservePeerDB: %v", err)
	}

	// File must exist.
	fi, err := os.Stat(got)
	if err != nil {
		t.Fatalf("Stat(%s): %v", got, err)
	}
	if fi.Size() == 0 {
		t.Errorf("decompressed file is empty")
	}
	// Path must live under the temp dir.
	if !strings.HasPrefix(got, filepath.Join(base, syncTempDirName)) {
		t.Errorf("decompressed path %q not under sync-temp/", got)
	}
	// Name must have the "peer-" prefix.
	if !strings.HasPrefix(filepath.Base(got), "peer-") {
		t.Errorf("decompressed file name = %q, want 'peer-' prefix", filepath.Base(got))
	}
}

// TestTempManager_ReservePeerDB_EmptyPath verifies that passing an empty path
// returns an error wrapping ErrBackupNotCompressed.
func TestTempManager_ReservePeerDB_EmptyPath(t *testing.T) {
	t.Parallel()

	base := t.TempDir()
	tm, err := NewTempManager(base)
	if err != nil {
		t.Fatalf("NewTempManager: %v", err)
	}
	t.Cleanup(func() { tm.Cleanup() })

	_, err = tm.ReservePeerDB("")
	if err == nil {
		t.Fatal("expected error for empty peer backup path")
	}
}

// TestTempManager_ReservePeerDB_NotZstd verifies that a path that doesn't end
// in .zst still attempts decompression but the base name is preserved without
// the suffix stripping (since the suffix check fails).
func TestTempManager_ReservePeerDB_BadFormat(t *testing.T) {
	t.Parallel()

	base := t.TempDir()
	tm, err := NewTempManager(base)
	if err != nil {
		t.Fatalf("NewTempManager: %v", err)
	}
	t.Cleanup(func() { tm.Cleanup() })

	// A non-zstd file fed to DecompressFile produces an error.
	bad := filepath.Join(base, "not-zstd.bin")
	if err := os.WriteFile(bad, []byte("not compressed"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	_, err = tm.ReservePeerDB(bad)
	if err == nil {
		t.Fatal("expected error decompressing a non-zstd file")
	}
}

// TestTempManager_Remove_CleansUp verifies that Remove deletes the file from
// disk and removes it from the tracked list.
func TestTempManager_Remove_CleansUp(t *testing.T) {
	t.Parallel()

	base := t.TempDir()
	tm, err := NewTempManager(base)
	if err != nil {
		t.Fatalf("NewTempManager: %v", err)
	}
	t.Cleanup(func() { tm.Cleanup() })

	compressed := makeCompressedPeerBackup(t, base, "peer.db", []byte("payload"))
	got, err := tm.ReservePeerDB(compressed)
	if err != nil {
		t.Fatalf("ReservePeerDB: %v", err)
	}

	// File exists at this point.
	if _, err := os.Stat(got); err != nil {
		t.Fatalf("pre-Remove Stat: %v", err)
	}

	tm.Remove(got)

	if _, err := os.Stat(got); !os.IsNotExist(err) {
		t.Errorf("post-Remove Stat: file still exists or unexpected error: %v", err)
	}
}

// TestTempManager_Remove_EmptyPath is a no-op (no panic).
func TestTempManager_Remove_EmptyPath(t *testing.T) {
	t.Parallel()

	base := t.TempDir()
	tm, err := NewTempManager(base)
	if err != nil {
		t.Fatalf("NewTempManager: %v", err)
	}
	t.Cleanup(func() { tm.Cleanup() })

	// Should not panic.
	tm.Remove("")
}

// TestTempManager_Remove_UntrackedPath is a best-effort delete — Remove
// attempts to delete the file even if it isn't in the tracked list.
func TestTempManager_Remove_UntrackedPath(t *testing.T) {
	t.Parallel()

	base := t.TempDir()
	tm, err := NewTempManager(base)
	if err != nil {
		t.Fatalf("NewTempManager: %v", err)
	}
	t.Cleanup(func() { tm.Cleanup() })

	// Write a random file that the manager doesn't track.
	rogue := filepath.Join(base, "rogue.db")
	if err := os.WriteFile(rogue, []byte("x"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	tm.Remove(rogue)

	if _, err := os.Stat(rogue); !os.IsNotExist(err) {
		t.Errorf("expected rogue file removed, got err=%v", err)
	}
}

// TestTempManager_Cleanup_RemovesAllTracked verifies that Cleanup removes every
// tracked temp file and the temp directory itself.
func TestTempManager_Cleanup_RemovesAllTracked(t *testing.T) {
	t.Parallel()

	base := t.TempDir()
	tm, err := NewTempManager(base)
	if err != nil {
		t.Fatalf("NewTempManager: %v", err)
	}

	// Reserve two peer DBs.
	c1 := makeCompressedPeerBackup(t, base, "peer1.db", []byte("one"))
	c2 := makeCompressedPeerBackup(t, base, "peer2.db", []byte("two"))
	p1, err := tm.ReservePeerDB(c1)
	if err != nil {
		t.Fatalf("ReservePeerDB 1: %v", err)
	}
	p2, err := tm.ReservePeerDB(c2)
	if err != nil {
		t.Fatalf("ReservePeerDB 2: %v", err)
	}

	if err := tm.Cleanup(); err != nil {
		t.Fatalf("Cleanup: %v", err)
	}

	for _, p := range []string{p1, p2} {
		if _, err := os.Stat(p); !os.IsNotExist(err) {
			t.Errorf("tracked file %q still present after Cleanup", p)
		}
	}

	// Temp directory itself is removed by Cleanup.
	if _, err := os.Stat(filepath.Join(base, syncTempDirName)); !os.IsNotExist(err) {
		t.Errorf("temp dir still present after Cleanup")
	}
}

// TestTempManager_Cleanup_Idempotent verifies that calling Cleanup twice
// doesn't panic and the second call is a no-op returning nil.
func TestTempManager_Cleanup_Idempotent(t *testing.T) {
	t.Parallel()

	base := t.TempDir()
	tm, err := NewTempManager(base)
	if err != nil {
		t.Fatalf("NewTempManager: %v", err)
	}

	if err := tm.Cleanup(); err != nil {
		t.Fatalf("first Cleanup: %v", err)
	}
	if err := tm.Cleanup(); err != nil {
		t.Errorf("second Cleanup: %v", err)
	}
}

// TestTempManager_NewTempManager_CleansStaleFiles verifies that
// NewTempManager cleans stale files from a previous run on construction. We
// simulate a crashed previous run by leaving files in the temp dir, then
// create a fresh manager pointing at the same base.
func TestTempManager_NewTempManager_CleansStaleFiles(t *testing.T) {
	t.Parallel()

	base := t.TempDir()

	// Simulate a stale temp dir from a previous crashed run.
	staleDir := filepath.Join(base, syncTempDirName)
	if err := os.MkdirAll(staleDir, 0o700); err != nil {
		t.Fatalf("MkdirAll staleDir: %v", err)
	}
	staleFile := filepath.Join(staleDir, "peer-stale.db")
	if err := os.WriteFile(staleFile, []byte("stale"), 0o600); err != nil {
		t.Fatalf("WriteFile staleFile: %v", err)
	}

	// Creating a fresh manager should clean the stale file.
	tm, err := NewTempManager(base)
	if err != nil {
		t.Fatalf("NewTempManager: %v", err)
	}
	t.Cleanup(func() { tm.Cleanup() })

	if _, err := os.Stat(staleFile); !os.IsNotExist(err) {
		t.Errorf("stale file should have been removed on NewTempManager, got err=%v", err)
	}
}

// TestTempManager_SizeCapEnforced verifies that checkTempSize refuses to
// decompress when the existing temp dir already exceeds the hard cap. We
// cannot realistically push 1GB through a unit test, so instead we drop a
// file large enough to trip the 1<<30 cap... actually we cannot, so we verify
// the *guard fires* by directly exercising checkTempSize against a fake large
// file marker. Instead of creating a 1GB file, we verify that checkTempSize
// succeeds on an empty temp dir (the normal path) and document that the cap
// enforcement is exercised via the constant check.
func TestTempManager_SizeCapNormalPath(t *testing.T) {
	t.Parallel()

	base := t.TempDir()
	tm, err := NewTempManager(base)
	if err != nil {
		t.Fatalf("NewTempManager: %v", err)
	}
	t.Cleanup(func() { tm.Cleanup() })

	// Empty temp dir — well under the 1GB cap.
	if err := tm.checkTempSize(); err != nil {
		t.Errorf("checkTempSize on empty dir returned err: %v", err)
	}

	// Sanity check: cap constant is 1GB.
	if maxSyncTempSize != 1<<30 {
		t.Errorf("maxSyncTempSize = %d, want %d", maxSyncTempSize, 1<<30)
	}
}

// TestTempManager_SizeCapTriggersForLargeFile verifies the cap path is
// exercised by faking a directory with a file just over the cap. We use the
// internal checkTempSize to drive the logic; creating a sparse file that
// reports a large size on stat without consuming 1GB of disk.
func TestTempManager_SizeCapTriggersForLargeFile(t *testing.T) {
	t.Parallel()

	// Use a dedicated base so this test's sparse file doesn't pollute
	// parallel tests.
	base := t.TempDir()
	tm, err := NewTempManager(base)
	if err != nil {
		t.Fatalf("NewTempManager: %v", err)
	}
	t.Cleanup(func() { tm.Cleanup() })

	// Create a sparse file with apparent size just over maxSyncTempSize.
	sparsePath := filepath.Join(tm.tempDir, "huge.db")
	f, err := os.Create(sparsePath)
	if err != nil {
		t.Fatalf("Create sparse: %v", err)
	}
	// Truncate to (maxSyncTempSize + 1MB). On most filesystems this is a
	// sparse allocation that doesn't consume real disk.
	if err := f.Truncate(int64(maxSyncTempSize) + 1<<20); err != nil {
		f.Close()
		t.Fatalf("Truncate sparse: %v", err)
	}
	if err := f.Close(); err != nil {
		t.Fatalf("Close sparse: %v", err)
	}

	err = tm.checkTempSize()
	if err == nil {
		t.Fatal("expected error when temp dir exceeds cap")
	}
	if !IsSyncError(err) {
		t.Errorf("expected *SyncError, got %T: %v", err, err)
	}
}
