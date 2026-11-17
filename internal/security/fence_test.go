package security

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCheckPath_InsideRoot(t *testing.T) {
	root := t.TempDir()
	fc := NewFenceChecker(FenceConfig{
		Enabled:  true,
		RootPath: root,
	}, nil)

	// File inside root should be allowed for all ops
	for _, op := range []string{"read", "write", "exec"} {
		err := fc.CheckPath(filepath.Join(root, "some", "file.go"), op)
		if err != nil {
			t.Errorf("CheckPath(%q, %q) = %v, want nil", filepath.Join(root, "some", "file.go"), op, err)
		}
	}

	// Root itself should be allowed
	err := fc.CheckPath(root, "read")
	if err != nil {
		t.Errorf("CheckPath(%q, \"read\") = %v, want nil", root, err)
	}
}

func TestCheckPath_OutsideRoot_ReadAllowed(t *testing.T) {
	root := t.TempDir()
	allowedDir := t.TempDir()

	fc := NewFenceChecker(FenceConfig{
		Enabled:   true,
		RootPath:  root,
		AllowRead: []string{allowedDir},
	}, nil)

	outsidePath := filepath.Join(allowedDir, "system", "lib.h")
	err := fc.CheckPath(outsidePath, "read")
	if err != nil {
		t.Errorf("CheckPath(%q, \"read\") = %v, want nil (in AllowRead)", outsidePath, err)
	}
}

func TestCheckPath_OutsideRoot_WriteBlocked(t *testing.T) {
	root := t.TempDir()
	allowedDir := t.TempDir()

	fc := NewFenceChecker(FenceConfig{
		Enabled:   true,
		RootPath:  root,
		AllowRead: []string{allowedDir},
	}, nil)

	outsidePath := filepath.Join(allowedDir, "system", "lib.h")
	err := fc.CheckPath(outsidePath, "write")
	if err == nil {
		t.Errorf("CheckPath(%q, \"write\") = nil, want error (write outside root should be blocked)", outsidePath)
	}
}

func TestCheckPath_OutsideRoot_ReadOutsideAllowRead(t *testing.T) {
	root := t.TempDir()
	allowedDir := t.TempDir()
	otherDir := t.TempDir()

	fc := NewFenceChecker(FenceConfig{
		Enabled:   true,
		RootPath:  root,
		AllowRead: []string{allowedDir},
	}, nil)

	outsidePath := filepath.Join(otherDir, "secret.txt")
	err := fc.CheckPath(outsidePath, "read")
	if err == nil {
		t.Errorf("CheckPath(%q, \"read\") = nil, want error (read outside AllowRead)", outsidePath)
	}
}

func TestCheckPath_NoFence(t *testing.T) {
	root := t.TempDir()
	otherDir := t.TempDir()

	fc := NewFenceChecker(FenceConfig{
		Enabled:  true,
		RootPath: root,
		NoFence:  true,
	}, nil)

	outsidePath := filepath.Join(otherDir, "anywhere.txt")
	for _, op := range []string{"read", "write", "exec"} {
		err := fc.CheckPath(outsidePath, op)
		if err != nil {
			t.Errorf("CheckPath(%q, %q) = %v, want nil (NoFence mode)", outsidePath, op, err)
		}
	}
}

func TestCheckPath_NotEnabled(t *testing.T) {
	root := t.TempDir()
	otherDir := t.TempDir()

	fc := NewFenceChecker(FenceConfig{
		Enabled:  false,
		RootPath: root,
	}, nil)

	outsidePath := filepath.Join(otherDir, "anywhere.txt")
	for _, op := range []string{"read", "write", "exec"} {
		err := fc.CheckPath(outsidePath, op)
		if err != nil {
			t.Errorf("CheckPath(%q, %q) = %v, want nil (fencing not enabled)", outsidePath, op, err)
		}
	}
}

func TestCheckPath_EmptyRootPath(t *testing.T) {
	fc := NewFenceChecker(FenceConfig{
		Enabled:  true,
		RootPath: "",
	}, nil)

	// Empty root path is now detected as misconfiguration and blocked.
	err := fc.CheckPath(".", "read")
	if err == nil {
		t.Error("CheckPath(\".\", \"read\") = nil, want error (empty root is misconfigured)")
	}
	if !strings.Contains(err.Error(), "misconfigured") {
		t.Errorf("CheckPath error = %v, want 'misconfigured' error", err)
	}
}

func TestCheckPath_RelativePathInsideRoot(t *testing.T) {
	root := t.TempDir()

	fc := NewFenceChecker(FenceConfig{
		Enabled:  true,
		RootPath: root,
	}, nil)

	// Save and restore working directory
	t.Chdir(root)

	// Relative path that resolves inside root
	err := fc.CheckPath("./subdir/file.go", "read")
	if err != nil {
		t.Errorf("CheckPath(\"./subdir/file.go\", \"read\") = %v, want nil", err)
	}
}

func TestCheckPath_SymlinkInsideRoot(t *testing.T) {
	root := t.TempDir()

	// Create a real file inside root
	realFile := filepath.Join(root, "real.txt")
	if err := os.WriteFile(realFile, []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Create a symlink inside root pointing to the real file
	linkFile := filepath.Join(root, "link.txt")
	if err := os.Symlink(realFile, linkFile); err != nil {
		t.Fatal(err)
	}

	fc := NewFenceChecker(FenceConfig{
		Enabled:  true,
		RootPath: root,
	}, nil)

	err := fc.CheckPath(linkFile, "read")
	if err != nil {
		t.Errorf("CheckPath(%q, \"read\") = %v, want nil (symlink inside root)", linkFile, err)
	}
}

func TestCheckCommand(t *testing.T) {
	root := t.TempDir()
	otherDir := t.TempDir()

	fc := NewFenceChecker(FenceConfig{
		Enabled:  true,
		RootPath: root,
	}, nil)

	// WorkDir inside root -> allowed
	err := fc.CheckCommand("ls", root)
	if err != nil {
		t.Errorf("CheckCommand(\"ls\", %q) = %v, want nil", root, err)
	}

	// WorkDir outside root -> blocked
	err = fc.CheckCommand("ls", otherDir)
	if err == nil {
		t.Errorf("CheckCommand(\"ls\", %q) = nil, want error (workdir outside root)", otherDir)
	}
}

func TestCheckCommand_PathValidation(t *testing.T) {
	root := t.TempDir()
	otherDir := t.TempDir()

	fc := NewFenceChecker(FenceConfig{
		Enabled:  true,
		RootPath: root,
	}, nil)

	// Command with absolute path inside root -> allowed
	err := fc.CheckCommand("cat "+filepath.Join(root, "file.txt"), root)
	if err != nil {
		t.Errorf("CheckCommand with path inside root = %v, want nil", err)
	}

	// Command with absolute path outside root -> blocked
	outsideFile := filepath.Join(otherDir, "secret.txt")
	err = fc.CheckCommand("cat "+outsideFile, root)
	if err == nil {
		t.Errorf("CheckCommand with path outside root = nil, want error")
	}

	// Command with /etc/passwd -> blocked
	err = fc.CheckCommand("cat /etc/passwd", root)
	if err == nil {
		t.Error("CheckCommand with /etc/passwd = nil, want error")
	}

	// Command with ~ path -> blocked (home dir outside root)
	err = fc.CheckCommand("cat ~/.ssh/id_rsa", root)
	if err == nil {
		t.Error("CheckCommand with ~/.ssh/id_rsa = nil, want error")
	}

	// Command with parent traversal -> blocked
	err = fc.CheckCommand("cat ../../etc/passwd", root)
	if err == nil {
		t.Error("CheckCommand with ../../etc/passwd = nil, want error")
	}

	// Simple command with no path args -> allowed (workdir is inside root)
	err = fc.CheckCommand("echo hello world", root)
	if err != nil {
		t.Errorf("CheckCommand(\"echo hello world\") = %v, want nil", err)
	}

	// Command with flags that look like paths but aren't -> allowed
	err = fc.CheckCommand("grep --color=auto pattern file", root)
	if err != nil {
		t.Errorf("CheckCommand with flags = %v, want nil", err)
	}

	// Relative path inside root (./ prefix) -> allowed when cwd is root
	t.Chdir(root)
	err = fc.CheckCommand("cat ./src/main.go", root)
	if err != nil {
		t.Errorf("CheckCommand with ./src/main.go = %v, want nil (inside root)", err)
	}
}

func TestIsNoFence(t *testing.T) {
	fc := NewFenceChecker(FenceConfig{NoFence: true}, nil)
	if !fc.IsNoFence() {
		t.Error("IsNoFence() = false, want true")
	}

	fc2 := NewFenceChecker(FenceConfig{NoFence: false}, nil)
	if fc2.IsNoFence() {
		t.Error("IsNoFence() = true, want false")
	}
}

func TestCheckPath_ExactAllowReadMatch(t *testing.T) {
	root := t.TempDir()
	allowedDir := t.TempDir()

	fc := NewFenceChecker(FenceConfig{
		Enabled:   true,
		RootPath:  root,
		AllowRead: []string{allowedDir},
	}, nil)

	// Exact match of the allowed directory itself should be permitted
	err := fc.CheckPath(allowedDir, "read")
	if err != nil {
		t.Errorf("CheckPath(%q, \"read\") = %v, want nil (exact AllowRead match)", allowedDir, err)
	}
}

func TestCheckPath_PathTraversalAttempt(t *testing.T) {
	root := t.TempDir()

	fc := NewFenceChecker(FenceConfig{
		Enabled:  true,
		RootPath: root,
	}, nil)

	// Save and restore working directory
	t.Chdir(root)

	// Path traversal using ../ to escape root
	escapePath := filepath.Join(root, "..", "..", "etc", "passwd")
	err := fc.CheckPath(escapePath, "read")
	if err == nil {
		t.Errorf("CheckPath(%q, \"read\") = nil, want error (path traversal attempt)", escapePath)
	}
}

// --- Per-session jail root (#138, second half) ---------------------------
//
// FenceConfig is process-wide and its RootPath is a static rig-wide
// allowlist, so a session bound to worktree A can still touch worktree B
// (or any other tree the allowlist admits) with an ABSOLUTE path. The jail
// closes that: CheckPathJailed refuses any path outside the caller's
// session root before the ordinary fence check runs. An empty jail root is
// the legacy posture and must stay byte-identical to CheckPath.

// jailFixture builds a fence whose rig-wide root admits BOTH the session
// worktree and a neighbouring tree — the shape that let the bench write
// into a foreign repo clone while file_write reported success.
func jailFixture(t *testing.T) (fc *FenceChecker, sessionRoot, otherTree, allowReadTree string) {
	t.Helper()
	rig := t.TempDir()
	sessionRoot = filepath.Join(rig, "worktrees", "a")
	otherTree = filepath.Join(rig, "clones", "rig")
	allowReadTree = filepath.Join(rig, "shared")
	for _, d := range []string{sessionRoot, otherTree, allowReadTree} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", d, err)
		}
	}
	return NewFenceChecker(FenceConfig{
		Enabled:   true,
		RootPath:  rig,
		AllowRead: []string{allowReadTree},
	}, nil), sessionRoot, otherTree, allowReadTree
}

func TestCheckPathJailed_InsideSessionRootAllowed(t *testing.T) {
	fc, sessionRoot, _, _ := jailFixture(t)

	inside := filepath.Join(sessionRoot, "src", "main.go")
	for _, op := range []string{"read", "write", "exec"} {
		if err := fc.CheckPathJailed(inside, op, sessionRoot); err != nil {
			t.Errorf("CheckPathJailed(%q, %q) = %v, want nil", inside, op, err)
		}
	}
	// The session root itself is inside the session root.
	if err := fc.CheckPathJailed(sessionRoot, "write", sessionRoot); err != nil {
		t.Errorf("CheckPathJailed(%q, \"write\") = %v, want nil", sessionRoot, err)
	}
	// A not-yet-existing file directly under the root (the write case).
	if err := fc.CheckPathJailed(filepath.Join(sessionRoot, "new.txt"), "write", sessionRoot); err != nil {
		t.Errorf("CheckPathJailed(new file, \"write\") = %v, want nil", err)
	}
}

func TestCheckPathJailed_OutsideSessionRootRefused(t *testing.T) {
	fc, sessionRoot, otherTree, _ := jailFixture(t)

	outside := filepath.Join(otherTree, "README.md")
	if err := os.WriteFile(outside, []byte("rig\n"), 0o644); err != nil {
		t.Fatalf("seed outside file: %v", err)
	}
	for _, op := range []string{"read", "write"} {
		err := fc.CheckPathJailed(outside, op, sessionRoot)
		if err == nil {
			t.Fatalf("CheckPathJailed(%q, %q) = nil, want refusal (fence allowlist admits it, session root does not)", outside, op)
		}
		if !errors.Is(err, ErrOutsideSessionRoot) {
			t.Errorf("CheckPathJailed(%q, %q) error = %v, want errors.Is(ErrOutsideSessionRoot)", outside, op, err)
		}
		// The refusal must name BOTH the jail root and the offending path so
		// the model can correct itself instead of retrying blindly.
		if !strings.Contains(err.Error(), sessionRoot) {
			t.Errorf("refusal %q does not name the session root %q", err, sessionRoot)
		}
		if !strings.Contains(err.Error(), outside) {
			t.Errorf("refusal %q does not name the offending path %q", err, outside)
		}
	}
	// Sanity: the ordinary, un-jailed check still admits it (that is the
	// bug the jail exists to close).
	if err := fc.CheckPath(outside, "write"); err != nil {
		t.Fatalf("fixture precondition failed: plain CheckPath refused %q: %v", outside, err)
	}
}

func TestCheckPathJailed_SymlinkEscapeRefused(t *testing.T) {
	fc, sessionRoot, otherTree, _ := jailFixture(t)

	outsideFile := filepath.Join(otherTree, "secret.txt")
	if err := os.WriteFile(outsideFile, []byte("s\n"), 0o600); err != nil {
		t.Fatalf("seed outside file: %v", err)
	}
	link := filepath.Join(sessionRoot, "portal")
	if err := os.Symlink(otherTree, link); err != nil {
		t.Fatalf("symlink: %v", err)
	}

	err := fc.CheckPathJailed(filepath.Join(link, "secret.txt"), "read", sessionRoot)
	if err == nil {
		t.Fatal("symlink escape out of the session root allowed")
	}
	if !errors.Is(err, ErrOutsideSessionRoot) {
		t.Errorf("symlink escape error = %v, want errors.Is(ErrOutsideSessionRoot)", err)
	}
	// A link that stays inside the session root is fine.
	insideFile := filepath.Join(sessionRoot, "real.txt")
	if err := os.WriteFile(insideFile, []byte("x\n"), 0o600); err != nil {
		t.Fatalf("seed inside file: %v", err)
	}
	benign := filepath.Join(sessionRoot, "alias.txt")
	if err := os.Symlink(insideFile, benign); err != nil {
		t.Fatalf("benign symlink: %v", err)
	}
	if err := fc.CheckPathJailed(benign, "read", sessionRoot); err != nil {
		t.Errorf("benign in-root symlink refused: %v", err)
	}
}

func TestCheckPathJailed_JailNarrowsAllowRead(t *testing.T) {
	fc, sessionRoot, _, allowReadTree := jailFixture(t)

	// AllowRead admits this tree; the session jail does not. The jail is
	// the narrower, per-session contract and must win.
	inAllowRead := filepath.Join(allowReadTree, "ref.md")
	if err := os.WriteFile(inAllowRead, []byte("doc\n"), 0o600); err != nil {
		t.Fatalf("seed allow-read file: %v", err)
	}
	if err := fc.CheckPath(inAllowRead, "read"); err != nil {
		t.Fatalf("fixture precondition failed: plain CheckPath refused the AllowRead path: %v", err)
	}
	err := fc.CheckPathJailed(inAllowRead, "read", sessionRoot)
	if err == nil {
		t.Fatal("jailed read of an AllowRead path outside the session root allowed")
	}
	if !errors.Is(err, ErrOutsideSessionRoot) {
		t.Errorf("error = %v, want errors.Is(ErrOutsideSessionRoot)", err)
	}
}

func TestCheckPathJailed_EmptyRootIsLegacyCheckPath(t *testing.T) {
	fc, sessionRoot, otherTree, allowReadTree := jailFixture(t)

	probes := []struct {
		path string
		op   string
	}{
		{filepath.Join(sessionRoot, "src", "main.go"), "read"},
		{filepath.Join(sessionRoot, "src", "main.go"), "write"},
		{sessionRoot, "exec"},
		{filepath.Join(otherTree, "README.md"), "read"},
		{filepath.Join(otherTree, "README.md"), "write"},
		{filepath.Join(allowReadTree, "ref.md"), "read"},
		{filepath.Join(allowReadTree, "ref.md"), "write"},
	}
	for _, p := range probes {
		want := fc.CheckPath(p.path, p.op)
		got := fc.CheckPathJailed(p.path, p.op, "")
		if (want == nil) != (got == nil) {
			t.Errorf("CheckPathJailed(%q, %q, \"\") = %v, CheckPath = %v; empty jail root must be identical", p.path, p.op, got, want)
		}
	}
}

func TestCheckPathJailed_NoFenceBypasses(t *testing.T) {
	fc, sessionRoot, otherTree, _ := jailFixture(t)
	fc.SetNoFence(true)

	if err := fc.CheckPathJailed(filepath.Join(otherTree, "README.md"), "write", sessionRoot); err != nil {
		t.Errorf("NoFence override must bypass the jail too, got %v", err)
	}
}

func TestCheckPathJailed_EmptyJailRootValueIsUnjailed(t *testing.T) {
	fc, _, otherTree, _ := jailFixture(t)

	// A whitespace-only jail root is "unset", never a jail at "/".
	if err := fc.CheckPathJailed(filepath.Join(otherTree, "x"), "write", "   "); err != nil {
		t.Errorf("blank jail root must be treated as unset, got %v", err)
	}
}
