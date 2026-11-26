package agent

// handler_fence_arming_test.go — bughunt H6 chat-lane pin (2026-11-25
// fix-wave): ChatHandler.sessionLoop must arm a checker carrying BOTH the
// session fence AND the group fs_roots fence, mirroring
// dispatcher_fence_arm_test.go for the DISPATCH lane. Pre-fix the chat
// lane kept the dead shape: injectSessionFence returns non-nil base
// whenever it has nothing to add, so wireSessionLoop's `if fence != nil
// {...} else if group` else-branch never ran and a managed member's
// fs_root write on a chat turn was DENIED by the session fence alone.
// The fix composes at the caller via composeLoopFence (same posture as the
// three dispatcher sites) and wireSessionLoop installs the one composed
// checker.

import (
	"log/slog"
	"path/filepath"
	"testing"

	"github.com/caimlas/meept/internal/config"
	"github.com/caimlas/meept/internal/groups"
	"github.com/caimlas/meept/internal/session"
	"github.com/caimlas/meept/pkg/security"
)

func TestChatHandler_SessionLoop_ArmsGroupFenceWithSessionFence(t *testing.T) {
	sessionRoot := t.TempDir()
	groupRoot := t.TempDir()
	outside := t.TempDir()

	reg := newTestRegistryWithChat(t)
	tpl, err := reg.Get(config.AgentIDChat)
	if err != nil || tpl == nil {
		t.Fatalf("registry chat template: %v", err)
	}
	// Daemon posture: a non-nil UN-fenced base checker + the group record
	// the template carries (three-site rule site 2 — ConfigSnapshot hands
	// it to every manager clone).
	tpl.SetSecurityChecker(security.NewPermissionChecker(security.Config{}))
	tpl.SetGroupResolution(&groups.Resolved{Managed: true, FSRoots: []string{groupRoot}})

	const sessionID = "sess-chat-fence-arm"
	store := &stubSessionStore{
		sessions: map[string]*session.Session{
			sessionID: {ID: sessionID, ConversationID: sessionID, ProjectPath: sessionRoot},
		},
	}
	h := NewChatHandler(tpl, nil, nil, slog.Default())
	h.SetSessionStore(store)
	h.SetAgentLoopManager(NewManager(ManagerConfig{}))
	// Daemon-hardened posture: session fence factory WIRED — pre-fix this
	// is exactly what made the group branch dead on the chat lane.
	h.SetSessionFenceFactory(newSessionFenceFactory(testLoggerForFence()))

	got := h.sessionLoop(sessionID)
	if got == nil {
		t.Fatal("sessionLoop returned nil")
	}
	if got == tpl {
		t.Fatal("expected the session-scoped loop, got the template singleton")
	}
	checker := got.Security()
	if checker == nil {
		t.Fatal("armed loop carries no security checker")
	}

	// THE bug: group fs_root writable on the CHAT-lane loop (pre-fix
	// denied — only the session fence was armed).
	if err := checker.CheckPathFence(filepath.Join(groupRoot, "w.txt"), "write"); err != nil {
		t.Fatalf("group fs_root write denied on the chat-lane loop (H6): %v", err)
	}
	// Session root writable (the session fence still binds).
	if err := checker.CheckPathFence(filepath.Join(sessionRoot, "w.txt"), "write"); err != nil {
		t.Fatalf("session-root write denied: %v", err)
	}
	// Outside both: denied — the union boundary is real, not vacuous.
	if err := checker.CheckPathFence(filepath.Join(outside, "w.txt"), "write"); err == nil {
		t.Fatal("outside-group/session write allowed — chat-lane fence vacuous")
	}
}

// TestChatHandler_SingletonFallback_ArmsGroupFenceWithSessionFence pins the
// SECOND chat-lane path: the last-resort singleton fallback (empty
// conversation id → GetOrCreateWired errors → scopedChatLoop nil →
// fenceSet(h.loop, fence)). The fix composes ONCE at the caller, so every
// fenceSet/wireSessionLoop lane arms the same composed checker carrying the
// group roots.
func TestChatHandler_SingletonFallback_ArmsGroupFenceWithSessionFence(t *testing.T) {
	sessionRoot := t.TempDir()
	groupRoot := t.TempDir()
	outside := t.TempDir()

	reg := newTestRegistryWithChat(t)
	tpl, err := reg.Get(config.AgentIDChat)
	if err != nil || tpl == nil {
		t.Fatalf("registry chat template: %v", err)
	}
	tpl.SetSecurityChecker(security.NewPermissionChecker(security.Config{}))
	tpl.SetGroupResolution(&groups.Resolved{Managed: true, FSRoots: []string{groupRoot}})

	h := NewChatHandler(tpl, nil, nil, slog.Default())
	h.SetSessionStore(&stubSessionStore{sessions: map[string]*session.Session{}})
	h.SetAgentLoopManager(NewManager(ManagerConfig{}))
	// workingPath resolves through the default (no session for ""), so
	// the empty-conversation call walks GetOrCreateWired's sessionID error
	// → scopedChatLoop("") nil → singleton fenceSet fallback.
	h.SetDefaultWorkingDir(sessionRoot)
	h.SetSessionFenceFactory(newSessionFenceFactory(testLoggerForFence()))

	got := h.sessionLoop("")
	if got == nil {
		t.Fatal("sessionLoop returned nil")
	}
	if got != tpl {
		t.Fatal("expected the singleton fallback loop")
	}
	checker := got.Security()
	if checker == nil {
		t.Fatal("armed singleton carries no security checker")
	}
	if err := checker.CheckPathFence(filepath.Join(groupRoot, "w.txt"), "write"); err != nil {
		t.Fatalf("group fs_root write denied on the singleton-fallback chat lane (H6): %v", err)
	}
	if err := checker.CheckPathFence(filepath.Join(sessionRoot, "w.txt"), "write"); err != nil {
		t.Fatalf("session-root write denied: %v", err)
	}
	if err := checker.CheckPathFence(filepath.Join(outside, "w.txt"), "write"); err == nil {
		t.Fatal("outside-group/session write allowed on singleton fallback — fence vacuous")
	}
}
