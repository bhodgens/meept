package daemon

// 02-routing TEST-GATE T6 (W12; R5/A3/A4; master matrix F6).
//
// AgentJobProcessor.Process injects the session working dir ONLY inside
// the GetForTask success branch (components.go ~11182). When GetForTask
// errors (unknown agent) the processor falls back to p.agentLoop - the
// process-wide interactive loop - with NO dir binding: the step runs in
// whatever dir the shared loop happens to carry (daemon CWD in
// production), writing artifacts outside the user's project.
//
// Contract (02-routing/04 T3): the fallback binds the session dir (via
// the turn context / a scoped loop) or refuses with actionable
// ErrNoWorkingDir-style guidance. The shared loop is never mutated.
//
// RED today: the fallback turn observes the shared loop's stale dir and
// the shared loop itself is never bound to the session.

import (
	"context"
	"encoding/json"
	"log/slog"
	"os"
	"path/filepath"
	"testing"

	"github.com/caimlas/meept/internal/agent"
	"github.com/caimlas/meept/internal/queue"
	"github.com/caimlas/meept/internal/session"
	"github.com/caimlas/meept/internal/task"
	"github.com/caimlas/meept/internal/tools"
	"github.com/caimlas/meept/pkg/security"
)

// dirGateProcessor builds a processor with a registry that KNOWS coder
// but NOT the job's agent id, forcing the GetForTask error -> fallback
// branch. The shared loop carries a stale "daemon" dir.
func dirGateProcessor(t *testing.T) (*AgentJobProcessor, *agent.AgentLoop, *session.MemoryStore, *task.Store) {
	t.Helper()
	p, ss, ts := newTestAgentJobProcessor(t)

	shared := agent.NewAgentLoop("shared-interactive", "/daemon/stale-cwd")
	t.Cleanup(shared.Close)
	p.agentLoop = shared

	registry := agent.NewAgentRegistry(agent.RegistryConfig{Logger: testLogger(t)})
	if err := registry.RegisterSpec(&agent.AgentSpec{
		ID: "coder", Name: "coder", Role: agent.RoleExecutor, Enabled: true,
	}); err != nil {
		t.Fatalf("register coder: %v", err)
	}
	p.WithRegistry(registry)
	return p, shared, ss, ts
}

// dirGateStepJob builds a step job whose agent id is UNKNOWN to the
// registry (GetForTask error path) and whose payload carries the origin
// session id (the R4 provenance hint).
func dirGateStepJob(t *testing.T, sessionID string) *queue.Job {
	t.Helper()
	payload, err := json.Marshal(map[string]any{
		"step_id":     "step-dir-gate",
		"task_id":     "task-dir-gate",
		"description": "write the artifact into the project",
		"session_id":  sessionID,
	})
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}
	return &queue.Job{
		ID:      "job-dir-gate",
		TaskID:  "task-dir-gate",
		AgentID: "no-such-agent",
		Payload: payload,
	}
}

// TestStepJobDirBinding_FallbackBindsSessionDir pins the core contract:
// on the GetForTask-error fallback the step's turn must observe the
// SESSION's working dir (tools.WorkingDirFromContext at the loop
// boundary), never the shared loop's stale daemon dir.
func TestStepJobDirBinding_FallbackBindsSessionDir(t *testing.T) {
	p, shared, ss, ts := dirGateProcessor(t)

	sess, err := ss.Create("step-dir-gate")
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	sess.ProjectPath = "/user/project"
	seedTestTask(t, ts, "task-dir-gate", []string{sess.ConversationID})

	var seenDir string
	var observed bool
	p.turnContextObserver = func(ctx context.Context) {
		seenDir = tools.WorkingDirFromContext(ctx)
		observed = true
	}

	job := dirGateStepJob(t, sess.ID)
	// RunOnce fails (no LLM on the shared loop); the observer fires
	// immediately before the call, so the pin is about what the turn
	// WOULD have received.
	_, runErr := p.Process(context.Background(), job)
	if !observed {
		t.Fatalf("loop boundary never reached (run error: %v): the fallback branch must still hand the turn a dir-bound context", runErr)
	}
	if seenDir != sess.ProjectPath {
		t.Errorf("fallback step turn working dir = %q, want the session project %q: dir injection lives only in the GetForTask success branch, so the fallback runs in the daemon's stale dir (R5/A3)",
			seenDir, sess.ProjectPath)
	}
	if got := shared.GetWorkingDir(); got == sess.ProjectPath {
		t.Errorf("shared interactive loop mutated to %q: the fallback must bind the turn context or a scoped loop, never retarget the process-wide loop", got)
	}
}

// TestStepJobDirBinding_ThreadScopedPayloadHintResolves pins the
// payload-hint thread unwrap: the payload session hint may be a
// THREAD-scoped conversation id ("<base>-thread-<topic>-<n>"); the
// fallback resolution must unwrap it to the owning session (the
// LinkedSessions chain already does; the payload-hint path does not).
func TestStepJobDirBinding_ThreadScopedPayloadHintResolves(t *testing.T) {
	p, _, ss, ts := dirGateProcessor(t)

	sess, err := ss.Create("step-dir-thread")
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	sess.ProjectPath = "/user/thread-project"
	// Task linked to the BASE conversation id; the payload hint carries
	// the THREAD-scoped form (the thread router's live output shape).
	seedTestTask(t, ts, "task-dir-gate", []string{sess.ConversationID})
	threadHint := sess.ConversationID + "-thread-work-0001"

	var seenDir string
	var observed bool
	p.turnContextObserver = func(ctx context.Context) {
		seenDir = tools.WorkingDirFromContext(ctx)
		observed = true
	}

	job := dirGateStepJob(t, threadHint)
	_, runErr := p.Process(context.Background(), job)
	if !observed {
		t.Fatalf("loop boundary never reached (run error: %v)", runErr)
	}
	if seenDir != sess.ProjectPath {
		t.Errorf("thread-scoped payload hint resolved dir = %q, want %q: the payload-hint fallback does not unwrap thread ids (R4/A4)",
			seenDir, sess.ProjectPath)
	}
}

// TestStepJobDirBinding_NoDirAnywhereRefusesActionably pins the refusal
// path: when NOTHING resolves a dir (no session binding, no link), the
// fallback must refuse with actionable guidance (ErrNoWorkingDir-style)
// instead of silently running unbound in the daemon dir.
func TestStepJobDirBinding_NoDirAnywhereRefusesActionably(t *testing.T) {
	p, shared, _, ts := dirGateProcessor(t)

	// Task with NO linked sessions; payload hint names a session that
	// does not exist. Nothing can resolve a working dir.
	seedTestTask(t, ts, "task-dir-gate", nil)

	var observed bool
	p.turnContextObserver = func(ctx context.Context) {
		observed = true
	}

	job := dirGateStepJob(t, "session-does-not-exist")
	_, runErr := p.Process(context.Background(), job)

	if runErr == nil {
		t.Fatal("Process succeeded with no resolvable working dir: an unbound step job must refuse actionably, never run silently in the daemon dir")
	}
	if !tools.IsNoWorkingDir(runErr) {
		t.Errorf("refusal error = %v, want an ErrNoWorkingDir-style actionable refusal (bind a project / pass an explicit path)", runErr)
	}
	if observed {
		t.Error("the loop was reached despite no resolvable dir: the refusal must happen BEFORE execution")
	}
	if got := shared.GetWorkingDir(); got != "/daemon/stale-cwd" {
		t.Errorf("shared loop dir mutated to %q on the refusal path", got)
	}
}

// TestStepJobFence_JailsTaskLoopToWorktree pins issue #137's step-job arm:
// the bench regression tasks run on the STEP-JOB lane, and its
// task-scoped loop previously armed only the additive GROUP fence — with
// no group record (the rig's posture) NOTHING narrowed the daemon-wide
// allowed_paths glob, so the observed wrong-root write
// ($RIG/repo/transcript-check.txt instead of the per-task worktree)
// sailed through with success=true. The fix composes the #138 session
// jail rooted at the resolved step worktree onto the same loop via
// agent.ComposeSessionFence (in-jail short-circuits, outside refused even
// where the glob admits).
//
// Fixture mirrors the rig: base checker whose glob admits BOTH the task
// worktree and a sibling "repo" clone (the hallucinated target); the
// jail must deny the clone write and allow the worktree write.
func TestStepJobFence_JailsTaskLoopToWorktree(t *testing.T) {
	p, ss, ts := newTestAgentJobProcessor(t)

	base := security.NewPermissionChecker(security.Config{AllowedPaths: []string{"/**"}})
	registry := agent.NewAgentRegistry(agent.RegistryConfig{
		Logger:          testLogger(t),
		SecurityChecker: base,
	})
	if err := registry.RegisterSpec(&agent.AgentSpec{
		ID: "coder", Name: "coder", Role: agent.RoleExecutor, Enabled: true,
	}); err != nil {
		t.Fatalf("register coder: %v", err)
	}
	p.WithRegistry(registry)

	// Rig shape: $RIG/worktrees/<task> (the jail) beside $RIG/repo (the
	// clone the model hallucinated writes into). Both under one parent so
	// the glob admits both — only the jail can deny.
	root := t.TempDir()
	worktree := filepath.Join(root, "worktrees", "task-a1")
	clone := filepath.Join(root, "repo")
	for _, d := range []string{worktree, clone} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", d, err)
		}
	}

	sess, err := ss.Create("step-fence-session")
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	sess.WorktreePath = worktree
	seedTestTask(t, ts, "task-dir-gate", []string{sess.ConversationID})

	p.WithSessionFenceFactory(func(rootPath string, noFence bool) security.FenceStage {
		return agent.NewSessionFenceStage(rootPath, nil, noFence, slog.New(slog.DiscardHandler))
	})

	job := &queue.Job{
		ID: "job-fence-gate", TaskID: "task-dir-gate", AgentID: "coder",
		Payload: fenceStepJobPayload(t, "task-dir-gate", "step-fence", sess.ID),
	}
	// RunOnce fails (no LLM); the pin reads the ARMED loop afterwards.
	_, _ = p.Process(context.Background(), job)

	loop, err := registry.GetForTask("coder", "task-dir-gate")
	if err != nil {
		t.Fatalf("get task loop: %v", err)
	}
	secured := loop.Security()
	if secured == nil || secured == base {
		t.Fatal("step-job task loop still carries the UNFENCED base checker: " +
			"the #137 ComposeSessionFence arm point never fired on the GetForTask branch")
	}
	if got := secured.CheckPermissionForAgent("file_write", map[string]string{"path": filepath.Join(clone, "transcript-check.txt")}, ""); got.Allowed {
		t.Errorf("write into the rig CLONE dir allowed inside the task worktree jail — the exact #137 wrong-root write: %+v", got)
	} else if got.RuleSource != "fence" {
		t.Errorf("clone-dir denial must come from the fence stage, got rule_source=%q reason=%q", got.RuleSource, got.Reason)
	}
	if got := secured.CheckPermissionForAgent("file_write", map[string]string{"path": filepath.Join(worktree, "transcript-check.txt")}, ""); !got.Allowed {
		t.Errorf("write inside the task worktree (the jail root) must pass: %+v", got)
	}
}

// TestStepJobFence_NilFactoryKeepsLegacyPosture pins the byte-identical
// fallback: with no fence factory wired (every pre-#137 caller and test)
// the step-job loop must NOT gain a jail — the base checker passes
// through untouched unless a managed group record arms the group fence.
func TestStepJobFence_NilFactoryKeepsLegacyPosture(t *testing.T) {
	p, ss, ts := newTestAgentJobProcessor(t)

	base := security.NewPermissionChecker(security.Config{AllowedPaths: []string{"/**"}})
	registry := agent.NewAgentRegistry(agent.RegistryConfig{Logger: testLogger(t), SecurityChecker: base})
	if err := registry.RegisterSpec(&agent.AgentSpec{
		ID: "coder", Name: "coder", Role: agent.RoleExecutor, Enabled: true,
	}); err != nil {
		t.Fatalf("register coder: %v", err)
	}
	p.WithRegistry(registry)

	sess, err := ss.Create("step-fence-legacy")
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	sess.WorktreePath = t.TempDir()
	seedTestTask(t, ts, "task-dir-gate", []string{sess.ConversationID})

	job := &queue.Job{
		ID: "job-fence-legacy", TaskID: "task-dir-gate", AgentID: "coder",
		Payload: fenceStepJobPayload(t, "task-dir-gate", "step-legacy", sess.ID),
	}
	_, _ = p.Process(context.Background(), job)

	loop, err := registry.GetForTask("coder", "task-dir-gate")
	if err != nil {
		t.Fatalf("get task loop: %v", err)
	}
	if loop.Security() != base {
		t.Fatal("nil fence factory must leave the base checker unswapped (legacy posture byte-identical)")
	}
}

func fenceStepJobPayload(t *testing.T, taskID, stepID, sessionID string) []byte {
	t.Helper()
	payload, err := json.Marshal(map[string]any{
		"step_id":     stepID,
		"task_id":     taskID,
		"description": "write the artifact into the task worktree",
		"session_id":  sessionID,
	})
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}
	return payload
}
