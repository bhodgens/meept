package security

import (
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
	origWd, _ := os.Getwd()
	defer os.Chdir(origWd)
	os.Chdir(root)

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
	origWd, _ := os.Getwd()
	defer os.Chdir(origWd)
	os.Chdir(root)

	// Path traversal using ../ to escape root
	escapePath := filepath.Join(root, "..", "..", "etc", "passwd")
	err := fc.CheckPath(escapePath, "read")
	if err == nil {
		t.Errorf("CheckPath(%q, \"read\") = nil, want error (path traversal attempt)", escapePath)
	}
}
