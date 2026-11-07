package security

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/caimlas/meept/internal/config"
)

// TestNewEngineDBFileModes pins owner-only modes on the security engine DB
// lane: rows carry permission overrides and decision logs, so dir/DB/sidecars
// must never be group/other-readable — same contract as gatecapture 39dfb0ca
// and the orchestrator audit DB (c3d857e6).
func TestNewEngineDBFileModes(t *testing.T) {
	tmp := t.TempDir()
	subdir := filepath.Join(tmp, "sec")
	dbPath := filepath.Join(subdir, "engine.db")

	engine, err := NewEngine(dbPath, &config.SecurityConfig{}, nil)
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}
	defer engine.Close()

	if fi, err := os.Stat(subdir); err != nil {
		t.Fatalf("stat dir: %v", err)
	} else if fi.Mode().Perm()&0o077 != 0 {
		t.Errorf("engine dir mode = %o, want no group/other bits", fi.Mode().Perm())
	}
	if fi, err := os.Stat(dbPath); err != nil {
		t.Fatalf("stat db: %v", err)
	} else if fi.Mode().Perm() != 0o600 {
		t.Errorf("engine db mode = %o, want 0600", fi.Mode().Perm())
	}
	// WAL sidecars must be owner-only while the engine is live (seed
	// writes activate WAL; a raw sqlite_mode default would leave 0644).
	wal := dbPath + "-wal"
	if _, err := os.Stat(wal); err == nil {
		if fi, err := os.Stat(wal); err == nil && fi.Mode().Perm()&0o077 != 0 {
			t.Errorf("engine -wal mode = %o, want owner-only", fi.Mode().Perm())
		}
	}

	// Pre-existing loose dir is repaired on reopen.
	loose := filepath.Join(tmp, "loose")
	if err := os.Mkdir(loose, 0o755); err != nil {
		t.Fatal(err)
	}
	looseDB := filepath.Join(loose, "engine.db")
	e2, err := NewEngine(looseDB, &config.SecurityConfig{}, nil)
	if err != nil {
		t.Fatalf("NewEngine(loose): %v", err)
	}
	defer e2.Close()
	if fi, err := os.Stat(loose); err != nil {
		t.Fatalf("stat loose: %v", err)
	} else if fi.Mode().Perm()&0o077 != 0 {
		t.Errorf("pre-existing engine dir not repaired: mode = %o", fi.Mode().Perm())
	}
}
