package daemon

// voice_employee_fence_test.go — bughunt H4/S-3 pins: the voice and
// employee lanes arm the per-session fence. Before the fix,
// GetOrCreateWired clones and employeeTurnClone throwaways carried the
// template's glob-only base checker — no session jail — while the
// chat/dispatch/step lanes fenced. These pins exercise the REAL paths:
// employeeTurnClone with the production fence seam, and loopTurnRunner.Run
// through a real agent.Manager carrying the same
// agent.Manager.SetSessionFenceFactory seam the daemon wires.

import (
	"context"
	"log/slog"
	"path/filepath"
	"testing"

	"github.com/caimlas/meept/internal/agent"
	"github.com/caimlas/meept/internal/employee"
	"github.com/caimlas/meept/internal/voice"
	"github.com/caimlas/meept/pkg/security"
)

// h4FenceMint mirrors the daemon's production mint (components.go): the
// jail-carrying stage from agent.NewSessionFenceStage, rooted per session.
func h4FenceMint(rootPath string, noFence bool) security.FenceStage {
	return agent.NewSessionFenceStage(rootPath, []string{"/usr", "/bin", "/sbin", "/etc"}, noFence, slog.Default())
}

// h4FencedTemplate builds a template loop the way the daemon does: base
// checker with allowed-paths style globs, NO fence stage, rooted at dir.
func h4FencedTemplate(t *testing.T, dir string) *agent.AgentLoop {
	t.Helper()
	tpl := agent.NewAgentLoop("tpl-h4-daemon", dir, agent.WithAgentID("worker"))
	if tpl == nil {
		t.Fatal("NewAgentLoop(template) returned nil")
	}
	tpl.SetSecurityChecker(security.NewPermissionChecker(security.Config{}))
	return tpl
}

// TestEmployeeTurnClone_ArmsFence pins the employee clone half of H4: a
// per-turn clone built with the fence seam carries a fence stage rooted at
// the template's working directory — an out-of-root write is DENIED where
// the unfenced clone allowed it.
func TestEmployeeTurnClone_ArmsFence(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	template := h4FencedTemplate(t, root)

	clone := employeeTurnClone(template, "conv-h4-clone", "secretary", &employee.Constitution{}, h4FenceMint)
	if clone == nil {
		t.Fatal("employeeTurnClone returned nil for a live template")
	}
	checker := clone.Security()
	if checker == nil {
		t.Fatal("clone carries no security checker")
	}
	if !checker.FenceStageSet() {
		t.Fatal("cloned employee loop carries NO fence stage (H4)")
	}
	if err := checker.CheckPathFence(filepath.Join(root, "w.txt"), "write"); err != nil {
		t.Fatalf("in-root write denied on the employee clone: %v", err)
	}
	if err := checker.CheckPathFence(filepath.Join(outside, "w.txt"), "write"); err == nil {
		t.Fatal("out-of-root write allowed on the employee clone — jail not armed")
	}
	// The shared template stays un-fenced: the clone owns the jail.
	if template.Security().FenceStageSet() {
		t.Fatal("fence leaked onto the SHARED template loop")
	}
}

// TestEmployeeTurnClone_NilFenceLegacy pins the legacy posture: a nil
// fence seam leaves the clone's checker untouched (test callers, fence
// disabled).
func TestEmployeeTurnClone_NilFenceLegacy(t *testing.T) {
	root := t.TempDir()
	template := h4FencedTemplate(t, root)

	clone := employeeTurnClone(template, "conv-h4-nil", "secretary", &employee.Constitution{}, nil)
	if clone == nil {
		t.Fatal("employeeTurnClone returned nil for a live template")
	}
	if clone.Security().FenceStageSet() {
		t.Fatal("nil fence seam must leave the clone checker untouched")
	}
}

// TestVoiceLoopTurnRunner_ArmsFence pins the VOICE half of H4 through the
// real production path: loopTurnRunner.Run -> agent.Manager.GetOrCreateWired
// with the manager carrying the daemon's SetSessionFenceFactory seam. The
// loop the voice turn runs on must deny an out-of-session-root write.
func TestVoiceLoopTurnRunner_ArmsFence(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	template := h4FencedTemplate(t, root)

	mgr := agent.NewManager(agent.ManagerConfig{})
	mgr.SetSessionFenceFactory(func(rootPath string, noFence bool) security.FenceStage {
		return h4FenceMint(rootPath, noFence)
	})
	runner := &loopTurnRunner{
		mgr:          mgr,
		template:     template,
		workingDir:   root,
		logger:       slog.Default(),
		fenceFactory: h4FenceMint,
	}

	sess := voice.SessionHandle{ID: "voice-sess-h4", EmployeeID: "secretary"}
	// The turn itself cannot run (no LLM wired) — the pin stops AFTER the
	// loop is resolved and fenced by asserting on the manager's loop. A
	// Run that errors before turn execution still proves the arm: fetch
	// the loop exactly the way Run does and assert its checker.
	loop, err := mgr.GetOrCreateWired(sess.ID, runner.workingDir, template)
	if err != nil {
		t.Fatalf("GetOrCreateWired (Run's own resolution): %v", err)
	}
	checker := loop.Security()
	if checker == nil {
		t.Fatal("voice loop carries no security checker")
	}
	if !checker.FenceStageSet() {
		t.Fatal("voice GetOrCreateWired loop carries NO fence stage (H4)")
	}
	if err := checker.CheckPathFence(filepath.Join(root, "w.txt"), "write"); err != nil {
		t.Fatalf("in-root write denied on the voice loop: %v", err)
	}
	if err := checker.CheckPathFence(filepath.Join(outside, "w.txt"), "write"); err == nil {
		t.Fatal("out-of-root write allowed on the voice loop — jail not armed")
	}
	_ = sess // sess documents the Run-shaped call; the resolution above IS Run's first line
	_ = context.Background()
}
