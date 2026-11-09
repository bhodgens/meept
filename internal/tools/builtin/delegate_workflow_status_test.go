package builtin

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/agent"
	"github.com/caimlas/meept/internal/tools"
)

// newStatusTestController builds a controller wired to a real (unwired-
// registry) DelegateManager, so background workflow runs spawn + settle
// through the production seam. NOTE: an agent()-bearing script is REQUIRED
// for engine settlement in tests — a script that returns without calling
// agent() leaves the codemode promise unsettled and the engine waits out
// its full sync window.
func newStatusTestController(t *testing.T) (*DelegateController, *agent.DelegateManager) {
	t.Helper()
	mgr := agent.NewDelegateManager(nil, func(string, any) {}, func(string) error { return nil })
	return NewDelegateController(mgr, nil, nil), mgr
}

// settleAllChildren settles every child spawned so far as completed with a
// marker result.
func settleAllChildren(mgr *agent.DelegateManager, result string) {
	for _, kid := range mgr.Children("") {
		mgr.SettleForTest(kid.ChildID, agent.ChildStateCompleted, result, "")
	}
}

// runBackgroundTool drives the production tool path: workflow with
// run_in_background, returning (jobId, runId). A short sync window keeps a
// test's settle-the-child window inside the engine's per-agent() join
// budget (the settle must land before the join times out).
func runBackgroundTool(t *testing.T, c *DelegateController, script string) (string, string) {
	t.Helper()
	tool := NewWorkflowTool(c)
	res, err := tool.Execute(context.Background(), map[string]any{
		"script":            script,
		"agent_id":          "coder",
		"run_in_background": true,
		"sync_timeout_ms":   2000,
	})
	if err != nil {
		t.Fatalf("workflow background execute: %v", err)
	}
	m, ok := res.(map[string]any)
	if !ok {
		t.Fatalf("background result type = %T (%v), want the background map", res, res)
	}
	if m["kind"] != "background" {
		t.Fatalf("background kind = %v, want background", m["kind"])
	}
	jobID, _ := m["jobId"].(string)
	runID, _ := m["runId"].(string)
	if jobID == "" || runID == "" {
		t.Fatalf("background result missing jobId/runId: %v", m)
	}
	return jobID, runID
}

// TestWorkflowStatusToolRunningThenSettled: the full loop through the tool
// surface — background run -> status reports running (with run_id) -> after
// settlement completed with the run summary.
func TestWorkflowStatusToolRunningThenSettled(t *testing.T) {
	c, mgr := newStatusTestController(t)
	jobID, runID := runBackgroundTool(t, c, "const v = await agent('work'); return v;")

	tool := NewWorkflowStatusTool(c)

	// Pre-settlement (child still in flight): running, never blocking.
	res, err := tool.Execute(context.Background(), map[string]any{"job_id": jobID})
	if err != nil {
		t.Fatalf("status execute: %v", err)
	}
	running, ok := res.(jobStatusView)
	if !ok {
		t.Fatalf("status result type = %T, want jobStatusView", res)
	}
	if running.Status != workflowStatusRunning {
		t.Fatalf("pre-settlement status = %s, want running", running.Status)
	}
	if running.RunID != runID {
		t.Errorf("pre-settlement run_id = %q, want %q", running.RunID, runID)
	}

	// Settle the engine's child, then wait for the engine itself. The
	// settle must land INSIDE the agent() join window (sync_timeout_ms
	// 2000): give the background engine a beat to spawn the child first,
	// then settle as {"value": ...} — the workflow's childOutcome unwraps
	// the inner "value" key into the agent()'s result.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if len(mgr.Children("")) > 0 {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	for _, kid := range mgr.Children("") {
		mgr.SettleForTest(kid.ChildID, agent.ChildStateCompleted, `{"value":"status-ok"}`, "")
	}
	eng := c.jobReg[jobID]
	if eng == nil {
		t.Fatal("job not registered on the controller")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if _, err := eng.WaitCompletion(ctx); err != nil {
		t.Fatalf("background run did not settle: %v", err)
	}

	// Post-settlement: completed with the summary.
	res, err = tool.Execute(context.Background(), map[string]any{"job_id": jobID})
	if err != nil {
		t.Fatalf("status execute (settled): %v", err)
	}
	done := res.(jobStatusView)
	if done.Status != workflowStatusCompleted {
		t.Fatalf("settled status = %s, want completed (err=%s)", done.Status, done.Error)
	}
	if done.RunID != runID {
		t.Errorf("settled run_id = %q, want %q", done.RunID, runID)
	}
	if done.Result == "" || done.Result == "null" {
		t.Errorf("settled result = %q, want the run value", done.Result)
	}
	if done.AgentsStarted != 1 {
		t.Errorf("agents_started = %d, want 1 (script spawned one child)", done.AgentsStarted)
	}
}

// TestWorkflowStatusIdempotentAfterSettlement: the Completion seam is
// single-consumption, so the status surface must cache the settlement —
// repeated queries keep reporting completed, never flip back to running.
func TestWorkflowStatusIdempotentAfterSettlement(t *testing.T) {
	c, mgr := newStatusTestController(t)
	jobID, _ := runBackgroundTool(t, c, "const v = await agent('work'); return 7;")

	settleAllChildren(mgr, `"unused"`)
	eng := c.jobReg[jobID]
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	settled, err := eng.WaitCompletion(ctx)
	if err != nil {
		t.Fatalf("run did not settle: %v", err)
	}

	// First status read consumes the seam; the cache must retain it.
	for i := 0; i < 3; i++ {
		view, ok := c.workflowJobStatus(jobID)
		if !ok {
			t.Fatalf("query %d: job unknown", i+1)
		}
		if view.Status != workflowStatusCompleted {
			t.Fatalf("query %d after settlement: status = %s, want completed (cached settlement lost)", i+1, view.Status)
		}
		if view.StopReason != settled.StopReason {
			t.Errorf("query %d: stop_reason = %s, want %s", i+1, view.StopReason, settled.StopReason)
		}
	}
}

// TestWorkflowStatusFailedRunMapsToFailed: an agent() bridge fatal settles
// the run as failed; the status view carries the error.
func TestWorkflowStatusFailedRunMapsToFailed(t *testing.T) {
	c, mgr := newStatusTestController(t)
	jobID, _ := runBackgroundTool(t, c, "throw new Error('boom');")

	settleAllChildren(mgr, `"unused"`)
	eng := c.jobReg[jobID]
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	run, err := eng.WaitCompletion(ctx)
	if err != nil {
		t.Fatalf("run did not settle: %v", err)
	}
	if run.StopReason != workflowStopError {
		t.Fatalf("stop reason = %s, want error", run.StopReason)
	}
	view, ok := c.workflowJobStatus(jobID)
	if !ok {
		t.Fatal("job unknown after settlement")
	}
	if view.Status != workflowStatusFailed {
		t.Fatalf("status = %s, want failed", view.Status)
	}
	if view.Error == "" {
		t.Error("failed run must carry the error message")
	}
}

// TestWorkflowStatusInterruptedChildCompletesAsFailure: a child interrupted
// mid-join resolves the agent() call as a child failure (null), so the
// SCRIPT completes — the status view then reports completed with a
// agents_started count. Pin the semantics so a change is conscious.
func TestWorkflowStatusInterruptedChildCompletesAsFailure(t *testing.T) {
	c, mgr := newStatusTestController(t)
	jobID, _ := runBackgroundTool(t, c, "const v = await agent('work'); return {got: v};")

	for _, kid := range mgr.Children("") {
		_ = mgr.Cancel(kid.ChildID) // interrupt -> child settles interrupted
	}
	eng := c.jobReg[jobID]
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if _, err := eng.WaitCompletion(ctx); err != nil {
		t.Fatalf("run did not settle: %v", err)
	}
	view, ok := c.workflowJobStatus(jobID)
	if !ok {
		t.Fatal("job unknown")
	}
	if view.Status == workflowStatusRunning {
		t.Error("status must not stay running after the run settled")
	}
	if view.Status != workflowStatusCompleted {
		t.Errorf("interrupted child resolves a child failure (null), so the script completes: status = %s", view.Status)
	}
	if view.AgentsStarted != 1 {
		t.Errorf("agents_started = %d, want 1", view.AgentsStarted)
	}
}

// TestWorkflowStatusUnknownIDCleanError: an unknown id is a clean error
// envelope (Success:false), never a panic, never a Go error return.
func TestWorkflowStatusUnknownIDCleanError(t *testing.T) {
	c, _ := newStatusTestController(t)
	tool := NewWorkflowStatusTool(c)
	res, err := tool.Execute(context.Background(), map[string]any{"job_id": "wf-job-nope"})
	if err != nil {
		t.Fatalf("unknown id must not be a Go error: %v", err)
	}
	er, ok := res.(*tools.ToolResult)
	if !ok {
		t.Fatalf("unknown-id result type = %T, want error envelope", res)
	}
	if !strings.Contains(er.Error, "unknown job_id") {
		t.Errorf("error text = %q, want it to name the unknown job", er.Error)
	}

	// Empty id: required-field refusal.
	res, err = tool.Execute(context.Background(), map[string]any{"job_id": ""})
	if err != nil {
		t.Fatalf("empty id must not be a Go error: %v", err)
	}
	if _, ok := res.(*tools.ToolResult); !ok {
		t.Fatalf("empty-id result type = %T, want error envelope", res)
	}
}

// TestWorkflowStatusEvictedJobIsUnknown: registering over the cap evicts the
// oldest job, which then reports the clean unknown-id error — the registry
// stays bounded.
func TestWorkflowStatusEvictedJobIsUnknown(t *testing.T) {
	c, _ := newStatusTestController(t)
	eng := newTestEngine(t, &fakeSpawner{})
	oldest := ""
	for i := 0; i <= workflowJobRegistryCap; i++ {
		jid := eng.RunID() + "-" + itoa(i)
		if i == 0 {
			oldest = jid
		}
		c.registerWorkflowJob(jid, eng)
	}
	if got := len(c.jobReg); got > workflowJobRegistryCap {
		t.Fatalf("registry size = %d, want <= %d", got, workflowJobRegistryCap)
	}
	if _, ok := c.workflowJobStatus(oldest); ok {
		t.Error("oldest evicted job should report unknown")
	}
	// The most recent job is still answerable.
	recent := eng.RunID() + "-" + itoa(workflowJobRegistryCap)
	if _, ok := c.workflowJobStatus(recent); !ok {
		t.Error("most recent job should still be answerable")
	}
}

// TestWorkflowStatusToolRegisteredInProvider: workflow_status joins the
// delegate provider's Discover list (registration seam).
func TestWorkflowStatusToolRegisteredInProvider(t *testing.T) {
	SetDelegateProvider(NewDelegateController(nil, nil, nil))
	defer ResetDelegateProviderForTest()
	p := delegateProviderFactory()
	if p == nil {
		t.Fatal("delegate provider factory unbound")
	}
	found := false
	for _, tool := range p.Discover() {
		if tool.Name() == "workflow_status" {
			found = true
		}
	}
	if !found {
		t.Fatal("workflow_status not in delegate provider Discover()")
	}
}
