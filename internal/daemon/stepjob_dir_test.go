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
	"testing"

	"github.com/caimlas/meept/internal/agent"
	"github.com/caimlas/meept/internal/queue"
	"github.com/caimlas/meept/internal/session"
	"github.com/caimlas/meept/internal/task"
	"github.com/caimlas/meept/internal/tools"
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
