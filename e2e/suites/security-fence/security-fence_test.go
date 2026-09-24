//go:build e2e

// Suite security-fence: the session path fence over a real sandbox
// project root — outside paths rejected, inside allowed, no_fence
// bypassing, symlink escapes caught, and the misconfigured-root posture.
package securityfence

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/caimlas/meept/internal/security"
)

// sandboxFence builds a FenceChecker over a real sandbox project dir
// with a sibling "outside" directory for escape attempts.
func sandboxFence(t *testing.T, opts func(*security.FenceConfig)) (*security.FenceChecker, string, string) {
	t.Helper()
	root := t.TempDir()
	outside := filepath.Join(t.TempDir(), "outside")
	if err := os.MkdirAll(outside, 0o755); err != nil {
		t.Fatalf("mkdir outside: %v", err)
	}
	cfg := security.FenceConfig{
		Enabled:  true,
		RootPath: root,
	}
	if opts != nil {
		opts(&cfg)
	}
	return security.NewFenceChecker(cfg, nil), root, outside
}

// TestSecurityFence_RootRejectsOutsideSymlinkEscapesNoFence covers
// security-fence-01: fence roots reject outside paths (read and write),
// no_fence bypasses, and a symlink pointing outside the root is caught —
// with the inside path allowed throughout.
func TestSecurityFence_RootRejectsOutsideSymlinkEscapesNoFence(t *testing.T) {
	fc, root, outside := sandboxFence(t, nil)
	if !fc.Valid() {
		t.Fatal("fence over a real root must be valid")
	}

	// Inside the root: read and write both allowed.
	inside := filepath.Join(root, "src", "main.go")
	if err := fc.CheckPath(inside, "read"); err != nil {
		t.Fatalf("inside read refused: %v", err)
	}
	if err := fc.CheckPath(inside, "write"); err != nil {
		t.Fatalf("inside write refused: %v", err)
	}
	if err := fc.CheckPath(root, "read"); err != nil {
		t.Fatalf("root itself refused: %v", err)
	}

	// Outside the root: rejected for both ops, and the error names the fence.
	outsideFile := filepath.Join(outside, "secret.txt")
	for _, op := range []string{"read", "write"} {
		err := fc.CheckPath(outsideFile, op)
		if err == nil {
			t.Fatalf("outside %s allowed past the fence", op)
		}
		if !strings.Contains(err.Error(), "fence") {
			t.Fatalf("outside %s error not fence-shaped: %v", op, err)
		}
	}

	// Traversal-shaped path escaping the root: rejected.
	escape := filepath.Join(root, "..", "elsewhere", "x")
	if err := fc.CheckPath(escape, "write"); err == nil {
		t.Fatal("dot-dot escape allowed past the fence")
	}

	// Symlink escape: a link INSIDE the root pointing OUTSIDE must be
	// caught by the symlink resolution (resolved, not lexical, check).
	link := filepath.Join(root, "portal")
	if err := os.Symlink(outside, link); err != nil {
		t.Fatalf("symlink: %v", err)
	}
	if err := fc.CheckPath(filepath.Join(link, "secret.txt"), "read"); err == nil {
		t.Fatal("symlink escape into an outside dir allowed past the fence")
	}

	// A benign symlink INSIDE the root (target also inside) is fine.
	benign := filepath.Join(root, "alias.go")
	if err := os.Symlink(inside, benign); err != nil {
		t.Fatalf("benign symlink: %v", err)
	}
	if err := fc.CheckPath(benign, "read"); err != nil {
		t.Fatalf("benign in-root symlink refused: %v", err)
	}

	// no_fence bypasses everything, including the escape link.
	nf, _, _ := sandboxFence(t, func(c *security.FenceConfig) { c.NoFence = true })
	if !nf.IsNoFence() {
		t.Fatal("IsNoFence must report the override")
	}
	if err := nf.CheckPath(outsideFile, "write"); err != nil {
		t.Fatalf("no_fence still fences: %v", err)
	}
	if err := nf.CheckPath(filepath.Join(link, "secret.txt"), "read"); err != nil {
		t.Fatalf("no_fence still catches symlinks: %v", err)
	}

	// allow_read: an outside path explicitly allowed for READ is readable
	// but still not writable. CheckPath resolves BOTH the target path and
	// each AllowRead entry through symlinks, so the RESOLVED spelling of
	// the outside dir is the correct entry (macOS /var -> /private/var):
	// both sides normalize to the same spelling before the prefix compare.
	// This needs its own fence (the AllowRead entry must be THIS fixture's
	// resolved outside dir, not the earlier one).
	ar, _, arOutside := sandboxFence(t, nil)
	arFile := filepath.Join(arOutside, "reference.md")
	arOutsideResolved, err := filepath.EvalSymlinks(arOutside)
	if err != nil {
		t.Fatalf("resolve outside: %v", err)
	}
	arWithAllow := security.NewFenceChecker(security.FenceConfig{
		Enabled:   true,
		RootPath:  root, // same root as fc: the allow entry is what matters
		AllowRead: []string{arOutsideResolved},
	}, nil)
	if err := os.WriteFile(arFile, []byte("doc"), 0o600); err != nil {
		t.Fatalf("seed allowed read file: %v", err)
	}
	if err := arWithAllow.CheckPath(arFile, "read"); err != nil {
		t.Fatalf("allow_read read refused: %v", err)
	}
	if err := arWithAllow.CheckPath(arFile, "write"); err == nil {
		t.Fatal("allow_read must not grant writes")
	}
	if !arWithAllow.Valid() {
		t.Fatal("allow_read config must not invalidate the fence")
	}
	_ = ar

	// Misconfigured root (empty): fail CLOSED — every operation blocked.
	bad := security.NewFenceChecker(security.FenceConfig{Enabled: true, RootPath: ""}, nil)
	if bad.Valid() {
		t.Fatal("empty root must not be valid")
	}
	if err := bad.CheckPath(inside, "read"); err == nil {
		t.Fatal("misconfigured fence must block, not pass")
	}
}
