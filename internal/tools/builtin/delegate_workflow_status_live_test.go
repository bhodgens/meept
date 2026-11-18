package builtin

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/agent"
	"github.com/caimlas/meept/internal/tools"
)

// settlingSpawner is a workflowSpawner whose children settle quickly, so a
// real DelegateManager (unwired registry: children stay in flight until
// SettleForTest) is exercised end to end through the controller.
type settlingSpawner struct {
	mgr *agent.DelegateManager
}

func (s *settlingSpawner) Spawn(ctx context.Context, req agent.DelegateRequest, parent string) (agent.DelegateResult, error) {
	return s.mgr.Spawn(ctx, req, parent)
}

func (s *settlingSpawner) Join(ctx context.Context, parent, childID string, timeout time.Duration) (agent.DelegateResult, error) {
	return s.mgr.Join(ctx, parent, childID, timeout)
}

// newLiveStatusController builds a controller whose workflow engine fans out
// through a real (unwired-registry) DelegateManager: the controller holds
// the manager (so the TOOL path constructs engines against it) and tests
// also hand the same manager to a manually-built engine via the
// settlingSpawner.
func newLiveStatusController(t *testing.T) (*DelegateController, *agent.DelegateManager) {
	t.Helper()
	mgr := agent.NewDelegateManager(nil, func(string, any) {}, func(string) error { return nil })
	return NewDelegateController(mgr, nil, nil), mgr
}

// TestWorkflowStatusThroughRealDelegateManager: the DSH-8 completion loop
// exercised through the production DelegateManager — the child settles,
// the engine settles, and the tool surface answers completed. This catches
// wiring drift a fake spawner cannot.
func TestWorkflowStatusThroughRealDelegateManager(t *testing.T) {
	c, mgr := newLiveStatusController(t)
	engine := NewWorkflowEngine(WorkflowConfig{
		MaxConcurrent: 4, MaxTotal: 4, MaxItemsPerCall: 4,
		SyncTimeout: 2 * time.Second, EngineTimeout: 30 * time.Second,
		DefaultAgent: "coder",
	}, &settlingSpawner{mgr: mgr})
	engine.parentSession = "sess-live"

	jobID := engine.RunBackground("const v = await agent('work'); return v;", nil)
	c.registerWorkflowJob(jobID, engine)

	// Settle the child the engine's agent() bridge spawned (the bridge's
	// sync window is 5s; settle promptly, then wait for the engine).
	deadline := time.Now().Add(5 * time.Second)
	var childID string
	for time.Now().Before(deadline) {
		if kids := mgr.Children("sess-live"); len(kids) > 0 {
			childID = kids[0].ChildID
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if childID == "" {
		t.Fatal("workflow engine spawned no child against the real manager")
	}
	mgr.SettleForTest(childID, agent.ChildStateCompleted, `{"value":"live-ok"}`, "")

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if _, err := engine.WaitCompletion(ctx); err != nil {
		t.Fatalf("engine did not settle: %v", err)
	}

	view, ok := c.workflowJobStatus(jobID)
	if !ok {
		t.Fatal("job unknown on the controller")
	}
	if view.Status != workflowStatusCompleted {
		t.Fatalf("status = %s, want completed (err=%s)", view.Status, view.Error)
	}
	if view.RunID != engine.RunID() {
		t.Errorf("run_id = %q, want %q", view.RunID, engine.RunID())
	}
	if !strings.Contains(view.Result, "live-ok") {
		t.Errorf("result = %q, want the script value", view.Result)
	}
	if view.AgentsStarted != 1 {
		t.Errorf("agents_started = %d, want 1", view.AgentsStarted)
	}
}

// TestWorkflowToolBackgroundRegistersJob: the production tool path (workflow
// with run_in_background) records the job on the controller so a later
// workflow_status query finds it.
func TestWorkflowToolBackgroundRegistersJob(t *testing.T) {
	c, mgr := newLiveStatusController(t)
	tool := NewWorkflowTool(c)

	res, err := tool.Execute(context.Background(), map[string]any{
		"script":            "const v = await agent('work'); return 'reg-ok';",
		"agent_id":          "coder",
		"run_in_background": true,
	})
	if err != nil {
		t.Fatalf("background execute: %v", err)
	}
	m, ok := res.(map[string]any)
	if !ok {
		t.Fatalf("background result type = %T (%v), want the background map", res, res)
	}
	if m["kind"] != "background" {
		t.Fatalf("kind = %v, want background", m["kind"])
	}
	jobID, _ := m["jobId"].(string)
	if jobID == "" {
		t.Fatalf("missing jobId in %v", m)
	}
	if _, registered := c.jobReg[jobID]; !registered {
		t.Fatal("background job was not registered on the controller")
	}
	// Clean up the in-flight child (unwired registry never settles it).
	settleAllChildren(mgr, `"x"`)
}

// TestWorkflowStatusToolErrorEnvelopeShape pins the envelope contract the
// executor renders to the model: Success:false + the reason in Error.
func TestWorkflowStatusToolErrorEnvelopeShape(t *testing.T) {
	c := NewDelegateController(nil, nil, nil)
	tool := NewWorkflowStatusTool(c)
	res, err := tool.Execute(context.Background(), map[string]any{"job_id": "wf-job-gone"})
	if err != nil {
		t.Fatalf("unknown id must not be a Go error: %v", err)
	}
	tr, ok := res.(*tools.ToolResult)
	if !ok {
		t.Fatalf("result type = %T, want *tools.ToolResult", res)
	}
	if tr.Success {
		t.Error("unknown job must not be Success")
	}
	if !strings.Contains(tr.Error, "unknown job_id") {
		t.Errorf("error text = %q, want it to name the unknown job", tr.Error)
	}
}

// TestWorkflowStatusConcurrentQueries: status reads are race-free under
// concurrent query + registration (go test -race covers this file).
func TestWorkflowStatusConcurrentQueries(t *testing.T) {
	c, _ := newLiveStatusController(t)
	eng := newTestEngine(t, &fakeSpawner{})
	var wg sync.WaitGroup
	for i := range 8 {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			c.registerWorkflowJob("wf-job-cc-"+itoa(n), eng)
			_, _ = c.workflowJobStatus("wf-job-cc-" + itoa(n%4))
		}(i)
	}
	wg.Wait()
}
