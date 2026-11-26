package security

// groups_backstop_symlink_test.go — bughunt A3-4 pin: the C5 backstop
// must resolve symlinks BEFORE comparing, because the executor hands it
// the RAW path (the old comment's premise — "the caller already resolved
// symlinks" — was false). Trigger shape from the audit: a symlink inside
// the project pointing at <home>/meept.json5; a lexical-only comparison
// admits the write, letting a non-admin member rewrite its own groups
// policy. Non-vacuity: PRE-FIX protected() compared filepath.Clean(path)
// lexically, so "<project>/cfg" (the link) never matched "<home>/
// meept.json5" and check() returned nil (allow). If the resolution
// regressed back to lexical-only, the symlink case below flips to allowed
// and this test fails.

import (
	"os"
	"path/filepath"
	"testing"
)

func TestGroupsBackstopSymlinkEscape(t *testing.T) {
	home := t.TempDir()
	if err := os.WriteFile(filepath.Join(home, "meept.json5"), []byte("{}"), 0o600); err != nil {
		t.Fatalf("seed config: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(home, "agents"), 0o755); err != nil {
		t.Fatalf("seed agents dir: %v", err)
	}
	usersFile := filepath.Join(home, "users.json5")
	if err := os.WriteFile(usersFile, []byte("{}"), 0o600); err != nil {
		t.Fatalf("seed users file: %v", err)
	}
	project := t.TempDir()

	cfg := GroupsBackstopConfig{
		Enabled:     true,
		Home:        home,
		UsersFile:   usersFile,
		AdminAgents: map[string]struct{}{"agent-update": {}},
	}

	// Control: the direct path denies (never regressed).
	if err := cfg.check("aastub", filepath.Join(home, "meept.json5")); err == nil {
		t.Fatal("direct config write allowed — base backstop broken")
	}

	// The A3-4 escape: a symlink inside the project pointing at the
	// protected config file.
	link := filepath.Join(project, "cfg")
	if err := os.Symlink(filepath.Join(home, "meept.json5"), link); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	if err := cfg.check("aastub", link); err == nil {
		t.Fatalf("symlink %s -> %s ADMITTED (A3-4 regression: lexical-only compare)", link, filepath.Join(home, "meept.json5"))
	}

	// A symlinked directory component: <project>/h -> home, then
	// <project>/h/meept.json5 must also deny.
	dirLink := filepath.Join(project, "h")
	if err := os.Symlink(home, dirLink); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	if err := cfg.check("aastub", filepath.Join(dirLink, "meept.json5")); err == nil {
		t.Fatal("config write through a symlinked home dir admitted")
	}
	if err := cfg.check("aastub", filepath.Join(dirLink, "agents", "coder", "AGENT.md")); err == nil {
		t.Fatal("agents-dir write through a symlinked home dir admitted")
	}
	if err := cfg.check("aastub", filepath.Join(dirLink, "users.json5")); err == nil {
		t.Fatal("users-store write through a symlinked home dir admitted")
	}

	// Admin exemption survives resolution (the admin must not be locked
	// out of its own surfaces by the resolver).
	if err := cfg.check("agent-update", link); err != nil {
		t.Fatalf("admin write through the same path denied: %v", err)
	}

	// Non-vacuity control: a symlink to an UNprotected target stays
	// allowed (the resolver must not turn every link into a denial).
	benign := filepath.Join(project, "notes.txt")
	if err := os.WriteFile(benign, []byte("hi"), 0o600); err != nil {
		t.Fatalf("seed benign: %v", err)
	}
	benignLink := filepath.Join(project, "nlink")
	if err := os.Symlink(benign, benignLink); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	if err := cfg.check("aastub", benignLink); err != nil {
		t.Fatalf("benign symlink target denied (over-blocking): %v", err)
	}
}
