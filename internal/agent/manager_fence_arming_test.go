package agent

// manager_fence_arming_test.go — bughunt H4/S-3 pins: the MANAGER-level
// fence arm. The voice lane (internal/daemon loopTurnRunner.Run) and the
// employee clone path take their loops from GetOrCreateWired; before the
// fix those loops carried the template's UN-fenced base checker while the
// chat/dispatch/step lanes fenced (the e3a8a3e8 "every lane" claim missed
// them). The arm lives in GetOrCreateWired behind SetSessionFenceFactory.

import (
	"log/slog"
	"path/filepath"
	"testing"

	"github.com/caimlas/meept/internal/session"
	"github.com/caimlas/meept/pkg/security"
)

// TestManager_GetOrCreateWired_ArmsSessionFence pins the arm: with the
// factory wired, a freshly built wired loop carries a fence stage rooted
// at the loop's working directory — out-of-root writes are denied,
// in-root writes pass. Pre-fix (factory nil) this is the live voice-lane
// hazard, so the assertion on the stage's presence is the regression pin.
func TestManager_GetOrCreateWired_ArmsSessionFence(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()

	template := NewAgentLoop("tpl-h4", root,
		WithAgentID("worker"),
		WithSecurityChecker(security.NewPermissionChecker(security.Config{})),
	)
	mgr := NewManager(ManagerConfig{})
	mgr.SetSessionFenceFactory(newSessionFenceFactory(testLoggerForFence()))

	loop, err := mgr.GetOrCreateWired("sess-h4", root, template)
	if err != nil {
		t.Fatalf("GetOrCreateWired: %v", err)
	}
	checker := loop.Security()
	if checker == nil {
		t.Fatal("wired loop carries no security checker")
	}
	if !checker.FenceStageSet() {
		t.Fatal("wired loop carries NO fence stage — voice/employee lane unfenced (H4)")
	}
	if err := checker.CheckPathFence(filepath.Join(root, "w.txt"), "write"); err != nil {
		t.Fatalf("in-root write denied: %v", err)
	}
	if err := checker.CheckPathFence(filepath.Join(outside, "w.txt"), "write"); err == nil {
		t.Fatal("out-of-root write allowed on the wired loop — fence vacuous")
	}
}

// TestManager_GetOrCreateWired_NoFactoryLegacy pins the legacy posture:
// a manager without a factory never installs a fence (byte-identical
// pre-H4 behavior for test Managers and fence-disabled deployments).
func TestManager_GetOrCreateWired_NoFactoryLegacy(t *testing.T) {
	root := t.TempDir()
	template := NewAgentLoop("tpl-h4-legacy", root, WithAgentID("worker"))
	mgr := NewManager(ManagerConfig{})

	loop, err := mgr.GetOrCreateWired("sess-h4-legacy", root, template)
	if err != nil {
		t.Fatalf("GetOrCreateWired: %v", err)
	}
	if loop.Security().FenceStageSet() {
		t.Fatal("nil factory must leave the loop checker untouched (legacy posture)")
	}
}

// TestManager_GetOrCreateWired_NoFenceOverride pins the per-session
// --nofence override: the session row's NoFence flag disables the arm
// exactly like the chat lane's sess.NoFence input.
func TestManager_GetOrCreateWired_NoFenceOverride(t *testing.T) {
	root := t.TempDir()
	const sid = "sess-h4-nofence"

	template := NewAgentLoop("tpl-h4-nf", root, WithAgentID("worker"))
	store := session.NewMemoryStore(slog.Default())
	if _, err := store.Ensure(sid, "h4"); err != nil {
		t.Fatalf("Ensure: %v", err)
	}
	if err := store.SetNoFence(sid, true); err != nil {
		t.Fatalf("SetNoFence: %v", err)
	}
	mgr := NewManager(ManagerConfig{SessionStore: store})
	mgr.SetSessionFenceFactory(newSessionFenceFactory(testLoggerForFence()))

	loop, err := mgr.GetOrCreateWired(sid, root, template)
	if err != nil {
		t.Fatalf("GetOrCreateWired: %v", err)
	}
	if loop.Security().FenceStageSet() {
		t.Fatal("--nofence session must not carry a fence stage")
	}
}

// TestManager_GetOrCreateWired_CachedLoopUnchanged pins cache identity:
// a second GetOrCreateWired for the same (session, template) returns the
// SAME armed loop — the arm runs at creation, never re-composes per call.
func TestManager_GetOrCreateWired_CachedLoopUnchanged(t *testing.T) {
	root := t.TempDir()
	template := NewAgentLoop("tpl-h4-cache", root, WithAgentID("worker"))
	mgr := NewManager(ManagerConfig{})
	mgr.SetSessionFenceFactory(newSessionFenceFactory(testLoggerForFence()))

	first, err := mgr.GetOrCreateWired("sess-h4-cache", root, template)
	if err != nil {
		t.Fatalf("first GetOrCreateWired: %v", err)
	}
	second, err := mgr.GetOrCreateWired("sess-h4-cache", root, template)
	if err != nil {
		t.Fatalf("second GetOrCreateWired: %v", err)
	}
	if first != second {
		t.Fatal("cached lookup must return the identical loop pointer")
	}
}
