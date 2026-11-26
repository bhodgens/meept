package agent

// dispatcher_fence_arm_test.go — bughunt H6/A2-1 end-to-end pin: the
// dispatcher resolveAgent arm sites must install a checker carrying BOTH
// the session fence and the group fs_roots fence. Pre-fix this was dead
// code: injectSessionFence always returned non-nil base, so the else-if
// group branch never ran and a group member's fs_root write was denied
// (fencing on) or the boundary simply did not exist (fencing off). The
// test asserts the group root is WRITABLE and an outside path is DENIED
// on the loop resolveAgent actually returns — the exact shape the audit
// says never happened.

import (
	"log/slog"
	"path/filepath"
	"testing"

	"github.com/caimlas/meept/internal/config"
	"github.com/caimlas/meept/internal/groups"
	"github.com/caimlas/meept/internal/session"
	"github.com/caimlas/meept/pkg/security"
)

func TestDispatcher_ResolveAgent_ArmsGroupFenceWithSessionFence(t *testing.T) {
	sessionRoot := t.TempDir()
	groupRoot := t.TempDir()
	outside := t.TempDir()

	reg := newTestRegistryWithChat(t)
	tpl, err := reg.Get(config.AgentIDChat)
	if err != nil || tpl == nil {
		t.Fatalf("registry chat template: %v", err)
	}
	// Template posture mirrors the daemon: a non-nil un-fenced checker
	// (base) plus the group resolution record ConfigSnapshot carries to
	// every session clone (three-site rule site 2).
	tpl.SetSecurityChecker(security.NewPermissionChecker(security.Config{}))
	tpl.SetGroupResolution(&groups.Resolved{Managed: true, FSRoots: []string{groupRoot}})

	const sessionID = "sess-fence-arm"
	store := &stubSessionStore{
		sessions: map[string]*session.Session{
			sessionID: {ID: sessionID, ConversationID: sessionID, ProjectPath: sessionRoot},
		},
	}
	d := &Dispatcher{
		registry:     reg,
		loopManager:  NewManager(ManagerConfig{}),
		sessionStore: store,
		// Daemon-hardened posture: session fence factory WIRED. Pre-fix,
		// this is exactly what killed the group branch.
		fenceFactory: newSessionFenceFactory(testLoggerForFence()),
		logger:       slog.Default(),
	}

	got := d.resolveAgent(config.AgentIDChat, sessionID)
	if got == nil {
		t.Fatal("resolveAgent returned nil")
	}
	if got == tpl {
		t.Fatal("expected the session-scoped loop, got the registry template")
	}
	checker := got.Security()
	if checker == nil {
		t.Fatal("armed loop carries no security checker")
	}

	// Group root writable (THE bug: pre-fix denied by the session fence).
	if err := checker.CheckPathFence(filepath.Join(groupRoot, "w.txt"), "write"); err != nil {
		t.Fatalf("group fs_root write denied on the dispatched loop (H6/A2-1): %v", err)
	}
	// Session root writable.
	if err := checker.CheckPathFence(filepath.Join(sessionRoot, "w.txt"), "write"); err != nil {
		t.Fatalf("session-root write denied: %v", err)
	}
	// Outside both: denied.
	if err := checker.CheckPathFence(filepath.Join(outside, "w.txt"), "write"); err == nil {
		t.Fatal("outside-group/session write allowed — fence vacuous")
	}
	// The session fence itself must still bind its command surface: a
	// substitution command in an outside workdir is refused by the
	// session stage even though the group stage would allow "." forms.
	if err := checker.CheckPathFence(filepath.Join(sessionRoot, "ok"), "read"); err != nil {
		t.Fatalf("session read denied unexpectedly: %v", err)
	}
}
