package pii

import (
	"os"
	"path/filepath"
	"testing"
)

// TestNewSwapStoreFileModes pins owner-only modes on the PII swap store:
// rows map placeholder tokens to the secret values they stand for, so the
// DB must never be group/other-readable — same contract as gatecapture
// 39dfb0ca and the orchestrator audit DB.
func TestNewSwapStoreFileModes(t *testing.T) {
	tmp := t.TempDir()
	subdir := filepath.Join(tmp, "pii")

	s, err := NewSwapStore(subdir)
	if err != nil {
		t.Fatalf("NewSwapStore: %v", err)
	}
	// Force a real write so WAL sidecars materialize.
	if err := s.Put("sess-1", "<REDACTED-a1>", "hunter2", "secret"); err != nil {
		t.Fatalf("Put: %v", err)
	}

	dbPath := filepath.Join(subdir, "pii_swap.db")
	if fi, err := os.Stat(subdir); err != nil {
		t.Fatalf("stat dir: %v", err)
	} else if fi.Mode().Perm()&0o077 != 0 {
		t.Errorf("pii dir mode = %o, want no group/other bits", fi.Mode().Perm())
	}
	if fi, err := os.Stat(dbPath); err != nil {
		t.Fatalf("stat db: %v", err)
	} else if fi.Mode().Perm() != 0o600 {
		t.Errorf("pii db mode = %o, want 0600", fi.Mode().Perm())
	}
	for _, suffix := range []string{"-wal", "-shm"} {
		if fi, err := os.Stat(dbPath + suffix); err == nil && fi.Mode().Perm()&0o077 != 0 {
			t.Errorf("pii sidecar %s mode = %o, want owner-only", suffix, fi.Mode().Perm())
		}
	}

	// Pre-existing loose dir is repaired on reopen.
	loose := filepath.Join(tmp, "loose")
	if err := os.Mkdir(loose, 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := NewSwapStore(loose); err != nil {
		t.Fatalf("NewSwapStore(loose): %v", err)
	}
	if fi, err := os.Stat(loose); err != nil {
		t.Fatalf("stat loose: %v", err)
	} else if fi.Mode().Perm()&0o077 != 0 {
		t.Errorf("pre-existing pii dir not repaired: mode = %o", fi.Mode().Perm())
	}
	_ = s.Close()
}
