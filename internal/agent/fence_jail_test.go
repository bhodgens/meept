package agent

// fence_jail_test.go — regression pin for issue #157 (the #138 session path
// jail re-homed onto the production session FenceStage).
//
// The jail originally hung off a per-tool fenceChecker field that leaf 01 T3
// retired, so it never engaged in the daemon: the session stage ran a plain
// CheckPath and its AllowRead lane let any session read every configured
// system path, and — with no AllowRead — the central verdict was the ONLY
// boundary. This pin locks the re-homed posture on the REAL production
// constructor (newSessionFenceFactory / NewSessionFenceStage → composeLoopFence):
//
//  1. a path INSIDE the session jail passes with an empty allowlist (the
//     in-jail short-circuit — the jail, not the allowlist, admits it);
//  2. a path OUTSIDE the jail is DENIED even where the central rules would
//     allow it — both the fence's own AllowRead lane and the checker's
//     wildcard AllowedPaths glob (RuleSource "fence" still fires).
//
// NON-VACUITY: disabling the jail call in jailedFenceStage.CheckPath (route
// it to fc.CheckPath, the pre-fix stage) turns assertion 2 red: AllowRead
// admits the outside read and the glob admits it centrally.

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	intsecurity "github.com/caimlas/meept/internal/security"
	"github.com/caimlas/meept/pkg/security"
)

// TestSessionFenceStage_JailShortCircuitsEmptyAllowlist pins property 1:
// the production factory stage (nil AllowRead — the allowlist does not
// match anything) still allows reads and writes inside the session jail,
// including a not-yet-existing file (the write-dead-end resolution path)
// and a benign symlink whose target stays inside the jail.
func TestSessionFenceStage_JailShortCircuitsEmptyAllowlist(t *testing.T) {
	jail := t.TempDir()
	factory := newSessionFenceFactory(testLoggerForFence())
	stage := factory(jail, false, "")
	if stage == nil {
		t.Fatal("factory must mint a stage for a non-empty root")
	}

	inside := filepath.Join(jail, "src", "main.go")
	for _, op := range []string{"read", "write", "delete"} {
		if err := stage.CheckPath(inside, op); err != nil {
			t.Fatalf("in-jail %s denied with an empty allowlist (jail short-circuit dead): %v", op, err)
		}
	}
	// The jail root itself is inside the jail.
	if err := stage.CheckPath(jail, "write"); err != nil {
		t.Fatalf("jail root itself denied: %v", err)
	}
	// Benign in-jail symlink (target also inside) stays admitted — the
	// jail must not break legitimate in-tree links.
	target := filepath.Join(jail, "real.txt")
	if err := os.WriteFile(target, []byte("x"), 0o600); err != nil {
		t.Fatalf("seed target: %v", err)
	}
	link := filepath.Join(jail, "alias.txt")
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	if err := stage.CheckPath(link, "read"); err != nil {
		t.Fatalf("benign in-jail symlink refused: %v", err)
	}
}

// TestSessionFenceStage_JailDeniesWhereCentralRulesWouldAllow pins property
// 2 (THE #157 gap): with the jail engaged, a path outside the session root
// is refused even though BOTH central layers would allow it — the fence's
// own AllowRead lane lists the directory, and the PermissionChecker's
// AllowedPaths wildcard admits every glob match. Before the re-home the
// session stage ran plain CheckPath and this read passed (the bench
// symptom: file operations reported success into foreign trees).
func TestSessionFenceStage_JailDeniesWhereCentralRulesWouldAllow(t *testing.T) {
	jail := t.TempDir()
	foreign := t.TempDir()
	foreignFile := filepath.Join(foreign, "reference.md")
	if err := os.WriteFile(foreignFile, []byte("doc"), 0o600); err != nil {
		t.Fatalf("seed foreign file: %v", err)
	}
	// macOS /var -> /private/var: register the AllowRead entry under its
	// RESOLVED spelling so the only thing that can deny the read is the
	// jail, not a spelling mismatch inside the (disabled-by-jail) lane.
	foreignResolved := foreign
	if ev, err := filepath.EvalSymlinks(foreign); err == nil {
		foreignResolved = ev
	}

	// Real production construction: the daemon factory shape with an
	// AllowRead lane that CENTRALLY admits the foreign tree. (The
	// pre-re-home control — plain fc.CheckPath on the same config —
	// admits this read; that swap is what turns this pin red during the
	// non-vacuity check, see the file header.)
	stage := NewSessionFenceStage(jail, []string{foreignResolved}, false, "", testLoggerForFence())
	if stage == nil {
		t.Fatal("NewSessionFenceStage returned nil for a valid root")
	}

	// Fence-stage seam: outside path denied, jail sentinel named.
	err := stage.CheckPath(foreignFile, "read")
	if err == nil {
		t.Fatal("out-of-jail READ allowed despite AllowRead being consulted only AFTER the jail " +
			"— the #157 re-home is dead (jailedFenceStage.CheckPath not calling CheckPathJailed)")
	}
	if !errors.Is(err, intsecurity.ErrOutsideSessionRoot) {
		t.Fatalf("out-of-jail denial must carry ErrOutsideSessionRoot, got: %v", err)
	}
	if !strings.Contains(err.Error(), jail) {
		t.Fatalf("denial must name the session root so the model can self-correct: %v", err)
	}

	// Full central pipeline: base checker whose glob allows ANYTHING
	// (AllowedPaths "/" — matchGlobPattern admits every path), fenced by
	// the session stage through the REAL composeLoopFence arm point. The
	// verdict must be a fence denial (RuleSource "fence") even though the
	// glob stage passes.
	base := security.NewPermissionChecker(security.Config{AllowedPaths: []string{"/"}})
	loop := composeLoopFence(base, newSessionFenceFactory(testLoggerForFence()), jail, false, "", nil)
	if loop == nil || loop == base {
		t.Fatal("composeLoopFence must install a per-session fenced clone")
	}
	got := loop.CheckPermissionForAgent("file_read", map[string]string{"path": foreignFile}, "")
	if got.Allowed {
		t.Fatalf("file_read of an out-of-jail path allowed through the central pipeline "+
			"(glob admits, jail must still deny): %+v", got)
	}
	if got.RuleSource != "fence" {
		t.Fatalf("expected RuleSource fence, got %q (%s)", got.RuleSource, got.Reason)
	}
	// Control: the same call INSIDE the jail passes the whole pipeline.
	insideGot := loop.CheckPermissionForAgent("file_read", map[string]string{"path": filepath.Join(jail, "f.txt")}, "")
	if !insideGot.Allowed {
		t.Fatalf("in-jail file_read denied centrally: %+v", insideGot)
	}
}

// TestSessionFenceStage_NilPosturesUnchanged pins the byte-identical
// passthroughs the jail must NOT touch: --nofence sessions and unbound
// (empty root) sessions still mint no stage at all — unjailed, unfenced,
// exactly the pre-#138 posture.
func TestSessionFenceStage_NilPosturesUnchanged(t *testing.T) {
	jail := t.TempDir()
	factory := newSessionFenceFactory(testLoggerForFence())
	if got := factory(jail, true, ""); got != nil {
		t.Fatalf("--nofence must yield no stage (jail outranks nothing): %T", got)
	}
	if got := factory("", false, ""); got != nil {
		t.Fatalf("empty root must yield no stage: %T", got)
	}
	if got := NewSessionFenceStage("  ", nil, false, "", testLoggerForFence()); got != nil {
		t.Fatalf("blank root must yield no stage: %T", got)
	}
}

// TestSessionFenceStage_SymlinkEscapeRefused pins the jail's symlink
// posture (issue #138's absolute-path half): a link INSIDE the session
// root that lands OUTSIDE it is refused — the jail compares resolved
// paths, not spellings.
func TestSessionFenceStage_SymlinkEscapeRefused(t *testing.T) {
	jail := t.TempDir()
	foreign := t.TempDir()
	foreignFile := filepath.Join(foreign, "secret.txt")
	if err := os.WriteFile(foreignFile, []byte("x"), 0o600); err != nil {
		t.Fatalf("seed foreign file: %v", err)
	}
	link := filepath.Join(jail, "portal")
	if err := os.Symlink(foreign, link); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	stage := NewSessionFenceStage(jail, nil, false, "", testLoggerForFence())
	if err := stage.CheckPath(filepath.Join(link, "secret.txt"), "read"); err == nil {
		t.Fatal("symlink escape admitted — jail comparison is not symlink-resolved")
	}
}

// ---------------------------------------------------------------------------
// #137 follow-on — per-session SCRATCH root pins.
//
// The session fence, when armed with a valid session id, admits the
// session's scratch dir (<MEEPT_HOME>/scratch/<session id>) as a second
// WRITABLE jail root; everything outside BOTH roots keeps the existing
// jail error shape. These pins run against the real production
// constructor with MEEPT_HOME pinned to a temp dir, so the scratch root
// resolution (config.MeeptHome + "scratch" + sanitized id) is exercised
// end-to-end, not mocked.
//
// NON-VACUITY (disable-restore probes, removed after verification):
//   - pin (a)/(b): drop `s.scratchRoot` from the jailedFenceStage.CheckPath
//     call in fence_stage.go -> the scratch WRITE turns red (and (b) stays
//     green vacuously, which is exactly why (b) needs its own probe);
//   - pin (b) specifically: make sessionScratchRoot key on a constant
//     (or ignore the id) -> session B's stage admits session A's scratch
//     dir, turning (b) red while (a) stays green — the per-session
//     boundary is what the probe tests, not the scratch union alone;
//   - pin (c): have sessionScratchRoot fall back to the raw/unsanitized
//     id (or "" -> a shared default dir) -> the traversal/empty-id cases
//     admit a scratch root, turning (c) red.

// TestSessionFenceStage_ScratchRootPins is the combined (a)+(b)+(c) pin:
// (a) a scratch write is ALLOWED while an out-of-jail write stays DENIED
// with the existing jail error shape; (b) a DIFFERENT session's scratch
// dir is DENIED — the per-session boundary bites; (c) an invalid session
// id (slash / dot-dot runes) arms no scratch root at all — worktree-only
// posture.
func TestSessionFenceStage_ScratchRootPins(t *testing.T) {
	home := t.TempDir()
	t.Setenv("MEEPT_HOME", home)
	jail := t.TempDir()

	const sidA = "session-a1b2c3d4"
	const sidB = "session-e5f6g7h8"
	scratchA := filepath.Join(home, "scratch", sidA)
	scratchB := filepath.Join(home, "scratch", sidB)
	outside := t.TempDir()

	stage := NewSessionFenceStage(jail, nil, false, sidA, testLoggerForFence())
	if stage == nil {
		t.Fatal("mint must succeed for a valid root")
	}

	// (a) scratch write/read/delete under the session's OWN scratch dir is
	// permitted; the dir need not pre-exist (lazy creation — the file
	// tools MkdirAll parents when a write actually lands).
	for _, op := range []string{"write", "read", "delete"} {
		if err := stage.CheckPath(filepath.Join(scratchA, "artifact.bin"), op); err != nil {
			t.Fatalf("scratch %s denied for the owning session (a): %v", op, err)
		}
	}
	// (a, second half) an out-of-jail write is STILL denied, existing jail
	// error shape intact (ErrOutsideSessionRoot sentinel).
	foreignWrite := filepath.Join(outside, "leak.txt")
	err := stage.CheckPath(foreignWrite, "write")
	if err == nil {
		t.Fatal("out-of-jail AND out-of-scratch write allowed — scratch union widened the jail past its two roots")
	}
	if !errors.Is(err, intsecurity.ErrOutsideSessionRoot) {
		t.Fatalf("out-of-jail denial must keep ErrOutsideSessionRoot shape, got: %v", err)
	}

	// (b) THE security-critical pin: a DIFFERENT session's scratch dir is
	// denied. session B's stage must not admit A's lane and vice versa.
	stageB := NewSessionFenceStage(jail, nil, false, sidB, testLoggerForFence())
	if err := stageB.CheckPath(filepath.Join(scratchA, "other.bin"), "write"); err == nil {
		t.Fatal("session B's jail admitted session A's scratch dir — the per-session boundary does not bite")
	} else if !errors.Is(err, intsecurity.ErrOutsideSessionRoot) {
		t.Fatalf("cross-session scratch denial must keep the jail error shape, got: %v", err)
	}
	// Symmetry: A's stage refuses B's lane, and B's own lane is allowed.
	if err := stage.CheckPath(filepath.Join(scratchB, "mine.bin"), "write"); err == nil {
		t.Fatal("session A's jail admitted session B's scratch dir")
	}
	if err := stageB.CheckPath(filepath.Join(scratchB, "mine.bin"), "write"); err != nil {
		t.Fatalf("session B's own scratch lane denied: %v", err)
	}

	// (c) invalid / empty session ids arm NO scratch root: the jail stays
	// worktree-only, so the scratch lane (even the real directory below)
	// is DENIED while in-jail writes keep working.
	for _, bad := range []string{"", "   ", "sess/../../escape", "sess/../x", "weird id!", "sess.dot", "x/../../scratch/session-a1b2c3d4"} {
		s := NewSessionFenceStage(jail, nil, false, bad, testLoggerForFence())
		if s == nil {
			t.Fatalf("stage must still mint for a valid rootPath with session id %q", bad)
		}
		if err := s.CheckPath(filepath.Join(scratchA, "x.bin"), "write"); err == nil {
			t.Fatalf("invalid/empty session id %q armed a scratch root — traversal-unsafe or absent id must stay worktree-only", bad)
		}
		if err := s.CheckPath(filepath.Join(jail, "f.go"), "write"); err != nil {
			t.Fatalf("worktree-only posture broke in-jail write for id %q: %v", bad, err)
		}
	}
}

// TestSessionFenceStage_NofenceUnchangedByScratch pins (d): the scratch
// follow-on must not disturb the --nofence posture — a no-fence session
// still mints NO stage at all, byte-identical to the legacy passthrough,
// whatever the session id says.
func TestSessionFenceStage_NofenceUnchangedByScratch(t *testing.T) {
	home := t.TempDir()
	t.Setenv("MEEPT_HOME", home)
	jail := t.TempDir()
	factory := newSessionFenceFactory(testLoggerForFence())
	if got := factory(jail, true, "session-a1b2c3d4"); got != nil {
		t.Fatalf("--nofence with a valid session id must still yield no stage: %T", got)
	}
	if got := NewSessionFenceStage(jail, nil, true, "session-a1b2c3d4", testLoggerForFence()); got != nil {
		t.Fatalf("--nofence mint must yield no stage regardless of scratch arming: %T", got)
	}
	// Empty-rootPath stays nil too (requirement 3, other half).
	if got := NewSessionFenceStage("", nil, false, "session-a1b2c3d4", testLoggerForFence()); got != nil {
		t.Fatalf("empty rootPath must yield no stage even with a valid id: %T", got)
	}
}
