package agent

// fence_union_test.go — regression pins for bughunt H6/A2-1 (the group
// fs_roots fence never armed on the chat/dispatch lanes) and A2-6/M12
// (GroupFenceStage was a purely lexical prefix check). Owner ruling D1:
// fs_roots are ADDITIVE — a member writes in the session root AND every
// listed fs_roots; everything outside both is denied.
//
// Non-vacuity notes are inline per test: each pin fails against the
// pre-fix code (dead else-branch / lexical-only comparison), so a future
// re-introduction of either shape turns these red.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/caimlas/meept/internal/groups"
	"github.com/caimlas/meept/pkg/security"
)

func managedRes(roots ...string) *groups.Resolved {
	return &groups.Resolved{Managed: true, FSRoots: roots}
}

// TestComposeLoopFence_GroupRootsArmAlongsideSessionFence is the primary
// H6/A2-1 pin: with the session fence factory WIRED (the hardened
// posture that made the group branch dead), the composed checker must
// allow BOTH the session root and the group fs_root, and deny outside.
// PRE-FIX BEHAVIOR (non-vacuity): injectSessionFence returned a
// session-only clone; the else-branch never ran; a write inside the
// group root was DENIED by the session fence.
func TestComposeLoopFence_GroupRootsArmAlongsideSessionFence(t *testing.T) {
	sessionRoot := t.TempDir()
	groupRoot := t.TempDir()
	outside := t.TempDir()

	factory := newSessionFenceFactory(testLoggerForFence())
	base := security.NewPermissionChecker(security.Config{})

	composed := composeLoopFence(base, factory, sessionRoot, false, "", managedRes(groupRoot))
	if composed == nil {
		t.Fatal("composeLoopFence returned nil with a factory + managed roots")
	}
	if composed == base {
		t.Fatal("composeLoopFence must clone when a fence applies")
	}

	// Session root stays writable (D1: additive, never narrows).
	if err := composed.CheckPathFence(filepath.Join(sessionRoot, "a.txt"), "write"); err != nil {
		t.Fatalf("session-root write denied (D1 violation): %v", err)
	}
	// Group root becomes writable (THE BUG: pre-fix this denied).
	if err := composed.CheckPathFence(filepath.Join(groupRoot, "b.txt"), "write"); err != nil {
		t.Fatalf("group fs_root write denied (H6/A2-1 regression): %v", err)
	}
	// Outside both stays denied (the boundary is real, not vacuous).
	if err := composed.CheckPathFence(filepath.Join(outside, "c.txt"), "write"); err == nil {
		t.Fatal("path outside session root AND group roots was allowed — fence is vacuous")
	}
	// The base checker is untouched (per-session clone discipline).
	if err := base.CheckPathFence(filepath.Join(groupRoot, "b.txt"), "write"); err != nil {
		t.Fatal("base checker must have no fence installed")
	}
}

// TestComposeLoopFence_GroupFenceArmsWithoutSessionFence pins the second
// dead branch: fence disabled daemon-wide (nil factory). Pre-fix,
// injectSessionFence returned base (non-nil) so the else-branch never
// ran and NOTHING was fenced. Post-fix the group fence arms alone and
// keeps the session root additive.
func TestComposeLoopFence_GroupFenceArmsWithoutSessionFence(t *testing.T) {
	sessionRoot := t.TempDir()
	groupRoot := t.TempDir()
	outside := t.TempDir()

	base := security.NewPermissionChecker(security.Config{})
	composed := composeLoopFence(base, nil, sessionRoot, false, "", managedRes(groupRoot))
	if composed == nil || composed == base {
		t.Fatal("managed roots must arm a fence clone even with no session factory")
	}
	if err := composed.CheckPathFence(filepath.Join(groupRoot, "x"), "write"); err != nil {
		t.Fatalf("group root denied without a session factory: %v", err)
	}
	if err := composed.CheckPathFence(filepath.Join(sessionRoot, "x"), "write"); err != nil {
		t.Fatalf("session root must stay additive (D1): %v", err)
	}
	if err := composed.CheckPathFence(filepath.Join(outside, "x"), "write"); err == nil {
		t.Fatal("outside path allowed with no session fence — group boundary vacuous")
	}
	// Pre-fix byte-identical check: nil factory + NO managed record must
	// keep returning the base pointer untouched (no fence appears).
	plain := composeLoopFence(base, nil, sessionRoot, false, "", nil)
	if plain != base {
		t.Fatal("unmanaged + no factory must pass base through byte-identically")
	}
	unmanaged := &groups.Resolved{Managed: false, FSRoots: []string{groupRoot}}
	if got := composeLoopFence(base, nil, sessionRoot, false, "", unmanaged); got != base {
		t.Fatal("Managed=false records must contribute no group fence")
	}
}

// TestComposeLoopFence_SessionFenceOnlyByteIdentical pins the
// Managed=false contract: with a factory and NO group roots the result is
// exactly what injectSessionFence produces (same allow/deny shape) —
// the fix never widens the unmanaged posture.
func TestComposeLoopFence_SessionFenceOnlyByteIdentical(t *testing.T) {
	sessionRoot := t.TempDir()
	other := t.TempDir()
	factory := newSessionFenceFactory(testLoggerForFence())
	base := security.NewPermissionChecker(security.Config{})

	viaCompose := composeLoopFence(base, factory, sessionRoot, false, "", &groups.Resolved{Managed: true})
	viaLegacy := composeLoopFence(base, factory, sessionRoot, false, "", nil)
	for _, path := range []string{filepath.Join(sessionRoot, "f"), filepath.Join(other, "f")} {
		e1 := viaCompose.CheckPathFence(path, "write")
		e2 := viaLegacy.CheckPathFence(path, "write")
		if (e1 == nil) != (e2 == nil) {
			t.Fatalf("managed-but-rootless composition diverged from legacy session fence for %s: %v vs %v", path, e1, e2)
		}
	}
}

// TestComposeLoopFence_NoFenceSessionKeepsGroupFence pins the per-session
// --nofence posture: the session root is NOT fenced, but the group
// fs_roots policy boundary still binds (outside is denied) and the
// session dir stays writable per D1 (additive).
func TestComposeLoopFence_NoFenceSessionKeepsGroupFence(t *testing.T) {
	sessionRoot := t.TempDir()
	groupRoot := t.TempDir()
	outside := t.TempDir()
	factory := newSessionFenceFactory(testLoggerForFence())
	base := security.NewPermissionChecker(security.Config{})

	composed := composeLoopFence(base, factory, sessionRoot, true, "", managedRes(groupRoot))
	if composed == nil || composed == base {
		t.Fatal("--nofence session must still arm the group fence")
	}
	if err := composed.CheckPathFence(filepath.Join(groupRoot, "w"), "write"); err != nil {
		t.Fatalf("group root denied under --nofence: %v", err)
	}
	if err := composed.CheckPathFence(filepath.Join(sessionRoot, "w"), "write"); err != nil {
		t.Fatalf("session dir must stay writable under --nofence (D1 additive): %v", err)
	}
	if err := composed.CheckPathFence(filepath.Join(outside, "w"), "write"); err == nil {
		t.Fatal("outside path allowed under --nofence — group fence vacuous")
	}
}

// TestGroupFenceStage_SymlinkEscape pins A2-6/M12: a symlink INSIDE the
// group root pointing OUTSIDE it must not admit writes through the link.
// PRE-FIX (non-vacuity): pathInRoot was a lexical prefix match, so the
// link path <root>/link/target was admitted (string prefix) even though
// it resolves to /outside.
func TestGroupFenceStage_SymlinkEscape(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	link := filepath.Join(root, "link")
	if err := os.Symlink(outside, link); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	// macOS /var -> /private/var makes TempDir resolution asymmetrical;
	// resolve both sides the same way the fence does before asserting.
	outsideReal := outside
	if ev, err := filepath.EvalSymlinks(outside); err == nil {
		outsideReal = ev
	}

	stage := NewGroupFenceStage([]string{root})
	if stage == nil {
		t.Fatal("stage must build over a non-empty root")
	}
	// Control: a real subdirectory inside the root is admitted.
	if err := stage.CheckPath(filepath.Join(root, "real", "f.txt"), "write"); err != nil {
		t.Fatalf("inside-root write denied: %v", err)
	}
	// The escape: through the symlink it lands outside.
	if err := stage.CheckPath(filepath.Join(link, "escape.txt"), "write"); err == nil {
		t.Fatalf("symlink escape admitted (A2-6/M12 regression): link resolves under %s, root is %s", outsideReal, root)
	}
	// And the link path itself (the directory) is outside once resolved.
	if err := stage.CheckPath(link, "write"); err == nil {
		t.Fatal("symlink node itself admitted as inside the root")
	}
}

// TestGroupFenceStage_WorkDirPosture pins the A2-6/M12 workdir contract:
// "" and "." mean the loop's effective working directory (the executor
// injects the real dir), so they are INSIDE; an explicit outside workdir
// is denied; shell substitutions the path checks cannot see through are
// refused; the old roots[0]-only admission is gone (a SECOND root must be
// honored).
func TestGroupFenceStage_WorkDirPosture(t *testing.T) {
	rootA := t.TempDir()
	rootB := t.TempDir()
	outside := t.TempDir()

	stage := NewGroupFenceStage([]string{rootA, rootB})
	if err := stage.CheckCommand("ls", ""); err != nil {
		t.Fatalf("empty workdir must be treated as inside (executor injects the real dir): %v", err)
	}
	if err := stage.CheckCommand("ls", "."); err != nil {
		t.Fatalf("\".\" workdir must be treated as inside: %v", err)
	}
	// roots[0] quirk gone: rootB is the SECOND entry — it must be admitted.
	if err := stage.CheckCommand("ls", filepath.Join(rootB, "sub")); err != nil {
		t.Fatalf("second group root workdir denied: %v", err)
	}
	if err := stage.CheckCommand("ls", outside); err == nil {
		t.Fatal("outside workdir admitted — fence vacuous")
	}
	// Substitution refusals (same patterns the session FenceChecker
	// refuses; the group lane cannot inspect through them).
	for _, cmd := range []string{"cat $(ls /etc)", "echo `whoami`", "diff <(a) <(b)"} {
		if err := stage.CheckCommand(cmd, rootA); err == nil {
			t.Fatalf("substitution command admitted: %q", cmd)
		}
	}
	// Substitution with empty/"." workdir is refused too (was admitted
	// wholesale by the old early-return quirk).
	if err := stage.CheckCommand("cat $(ls /etc)", ""); err == nil {
		t.Fatal("substitution admitted with empty workdir")
	}
}

// TestPathInRoot_NonVacuity sanity-pins the resolved-comparison helpers:
// ".." traversal stays blocked (Clean) and a plain outside path is denied.
func TestPathInRoot_NonVacuity(t *testing.T) {
	root := t.TempDir()
	if !pathInRoot(filepath.Join(root, "a", "b"), root) {
		t.Fatal("inside path must be in root")
	}
	if pathInRoot(filepath.Join(root, "..", "escape"), root) {
		t.Fatal(".. traversal admitted")
	}
	if pathInRoot(root, filepath.Join(root, "child")) {
		t.Fatal("parent admitted as inside child")
	}
	// Prefix trap: /tmp/root vs /tmp/root-evil must not match lexically.
	if pathInRoot(root+"-evil", root) {
		t.Fatal("sibling-prefix path admitted by separatorless prefix match")
	}
	if strings.Contains(root, "-evil") {
		t.Skip("tempdir naming collision")
	}
}
