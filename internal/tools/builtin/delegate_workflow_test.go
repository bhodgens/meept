package builtin

import (
	"context"
	"encoding/json"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/agent"
	"github.com/caimlas/meept/internal/tools"
)

// fakeSpawner implements the workflowSpawner seam against a controllable
// canned child settlement, so engine tests exercise Spawn+Join + caps without
// a live agent registry. The drained results are consumed in whatever order
// concurrent Join calls arrive; tests assert set-wise, not positionally.
type fakeSpawner struct {
	mu      sync.Mutex
	reqs    []agent.DelegateRequest
	results []agent.DelegateResult // drained per Join; last repeats
	err     error                  // returned by Spawn when non-nil
}

func (f *fakeSpawner) Spawn(ctx context.Context, req agent.DelegateRequest, parent string) (agent.DelegateResult, error) {
	if f.err != nil {
		return agent.DelegateResult{}, f.err
	}
	f.mu.Lock()
	f.reqs = append(f.reqs, req)
	n := len(f.reqs)
	f.mu.Unlock()
	return agent.DelegateResult{
		AgentID: req.AgentID, ChildID: "delegate-test-" + itoa(n),
		Status: "running", Success: true,
	}, nil
}

func (f *fakeSpawner) Join(ctx context.Context, parent, childID string, timeout time.Duration) (agent.DelegateResult, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.results) > 1 {
		r := f.results[0]
		f.results = f.results[1:]
		return r, nil
	}
	if len(f.results) == 1 {
		return f.results[0], nil
	}
	return agent.DelegateResult{Success: true, Status: "completed", Response: "done"}, nil
}

func itoa(n int) string { return strconv.Itoa(n) }

// newTestEngine builds a workflow engine with a fake spawner and bounded caps.
func newTestEngine(t *testing.T, s *fakeSpawner) *WorkflowEngine {
	t.Helper()
	cfg := WorkflowConfig{
		MaxConcurrent: 8, MaxTotal: 8, MaxItemsPerCall: 8,
		SyncTimeout: time.Second, EngineTimeout: 30 * time.Second,
		DefaultAgent: "coder",
	}
	return NewWorkflowEngine(cfg, s)
}

func wfRunValue(t *testing.T, r WorkflowRun) any {
	t.Helper()
	if r.StopReason != workflowStopCompleted {
		t.Fatalf("run not completed: reason=%s msg=%s", r.StopReason, r.StopMessage)
	}
	var v any
	if err := json.Unmarshal([]byte(r.Value), &v); err != nil {
		t.Fatalf("result value is not JSON: %v (%q)", err, r.Value)
	}
	return v
}

func TestWorkflowValidateScriptRejectsMetaStatement(t *testing.T) {
	err := validateScript("export const meta = {name: 'x'}")
	if err == nil || !strings.Contains(err.Error(), workflowClassScriptParse) {
		t.Fatalf("want SCRIPT_PARSE rejection, got %v", err)
	}
}

func TestWorkflowValidateScriptRejectsMalformedSyntax(t *testing.T) {
	err := validateScript("const x = ;  return 1;")
	if err == nil || !strings.Contains(err.Error(), workflowClassScriptParse) {
		t.Fatalf("want SCRIPT_PARSE rejection, got %v", err)
	}
}

func TestWorkflowValidateScriptAcceptsAsyncBody(t *testing.T) {
	if err := validateScript("const v = await agent('x');\nreturn {v: v}"); err != nil {
		t.Fatalf("valid script rejected: %v", err)
	}
	if err := validateScript(""); err == nil {
		t.Fatal("empty script should be rejected")
	}
}

func TestWorkflowValidateMeta(t *testing.T) {
	ok := &workflowMeta{Name: "translate-docs", Description: "translate the corpus"}
	if err := validateMeta(ok); err != nil {
		t.Fatalf("valid meta rejected: %v", err)
	}
	bad := &workflowMeta{Name: "Bad_Name", Description: "x"}
	if err := validateMeta(bad); err == nil || !strings.Contains(err.Error(), workflowClassMetaInvalid) {
		t.Fatalf("want META_INVALID on bad kebab name, got %v", err)
	}
	noDesc := &workflowMeta{Name: "ok"}
	if err := validateMeta(noDesc); err == nil {
		t.Fatal("want META_INVALID on missing description")
	}
}

func TestWorkflowConfigCapsFailClose(t *testing.T) {
	p := workflowParams{}
	// Over-ceiling values must be INVALID_ARGUMENT, never clamped.
	p.MaxConcurrent = workflowMaxConcurrentCeiling + 1
	if _, err := validateConfig(p, ""); err == nil || !strings.Contains(err.Error(), workflowClassInvalidArg) {
		t.Fatalf("over-ceiling max_concurrent not rejected: %v", err)
	}
	p.MaxConcurrent = 0
	p.MaxTotal = workflowMaxTotalCeiling + 1
	if _, err := validateConfig(p, ""); err == nil {
		t.Fatal("over-ceiling max_total not rejected")
	}
	p.MaxTotal = 0
	p.MaxItemsPerCall = workflowMaxItemsCeiling + 1
	if _, err := validateConfig(p, ""); err == nil {
		t.Fatal("over-ceiling max_items not rejected")
	}
	// Valid explicit values are accepted.
	p = workflowParams{MaxConcurrent: 2, MaxTotal: 5, MaxItemsPerCall: 3, SyncTimeoutMS: 200, AgentID: "analyst"}
	cfg, err := validateConfig(p, "")
	if err != nil || cfg.MaxConcurrent != 2 || cfg.MaxTotal != 5 || cfg.SyncTimeout != 200*time.Millisecond || cfg.DefaultAgent != "analyst" {
		t.Fatalf("valid config rejected or misapplied: cfg=%+v err=%v", cfg, err)
	}
}

func TestWorkflowRegisterAndToolIdentity(t *testing.T) {
	ResetDelegateProviderForTest()
	SetDelegateProvider(NewDelegateController(nil, nil, nil))
	defer ResetDelegateProviderForTest()

	prov := delegateProviderFactory()
	if prov == nil {
		t.Fatal("bound delegate provider must not be nil")
	}
	found := false
	for _, tool := range prov.Discover() {
		if tool.Name() == "workflow" {
			found = true
			if tools.GetCategory(tool) != "delegation" {
				t.Errorf("workflow category = %q, want delegation", tools.GetCategory(tool))
			}
		}
	}
	if !found {
		t.Fatal("workflow tool not discovered through the delegate provider seam")
	}
	var _ tools.Tool = (*WorkflowTool)(nil)
}

func TestWorkflowToolRejectsInvalidScript(t *testing.T) {
	ctrl := NewDelegateController(nil, nil, nil)
	tool := NewWorkflowTool(ctrl)
	res, err := tool.Execute(context.Background(), map[string]any{
		"script": "export const meta = {name:'x'}; return 1;",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	tr, ok := res.(*tools.ToolResult)
	if !ok || tr.Success || !strings.Contains(tr.Error, workflowClassScriptParse) {
		t.Fatalf("want SCRIPT_PARSE isError, got %#v", res)
	}
}

func TestWorkflowToolRejectsOverCeilingCap(t *testing.T) {
	ctrl := NewDelegateController(nil, nil, nil)
	tool := NewWorkflowTool(ctrl)
	res, _ := tool.Execute(context.Background(), map[string]any{
		"script":                "return 1;",
		"max_concurrent_agents": float64(workflowMaxConcurrentCeiling + 1),
	})
	tr, ok := res.(*tools.ToolResult)
	if !ok || tr.Success || !strings.Contains(tr.Error, workflowClassInvalidArg) {
		t.Fatalf("want INVALID_ARGUMENT isError, got %#v", res)
	}
}

func TestWorkflowToolNilControllerFailsClosed(t *testing.T) {
	tool := NewWorkflowTool(nil)
	res, _ := tool.Execute(context.Background(), map[string]any{"script": "return 1;"})
	tr, ok := res.(*tools.ToolResult)
	if !ok || tr.Success || !strings.Contains(tr.Error, "controller not bound") {
		t.Fatalf("want controller-not-bound isError, got %#v", res)
	}
}

// -- engine / sandbox-level tests (real codemode VM) --

func TestWorkflowAgentFanoutJoin(t *testing.T) {
	s := &fakeSpawner{results: []agent.DelegateResult{{
		Success: true, Status: "completed", Response: `{"ok":true,"count":3}`,
	}}}
	e := newTestEngine(t, s)
	run := e.Run(context.Background(), "const v = await agent('do work');\nreturn {answer: v};", nil)
	val := wfRunValue(t, run)
	m := val.(map[string]any)
	ans := m["answer"].(map[string]any)
	if ans["count"] != float64(3) {
		t.Fatalf("answer = %v, want count 3", m["answer"])
	}
	if len(s.reqs) != 1 || s.reqs[0].AgentID != "coder" || s.reqs[0].Message != "do work" {
		t.Fatalf("spawn request = %+v", s.reqs)
	}
	if run.AgentsStarted != 1 {
		t.Fatalf("agentsStarted = %d, want 1", run.AgentsStarted)
	}
}

func TestWorkflowAgentChildFailureResolvesNull(t *testing.T) {
	s := &fakeSpawner{results: []agent.DelegateResult{{
		Success: false, Status: "failed", Error: "agent exploded",
	}}}
	e := newTestEngine(t, s)
	run := e.Run(context.Background(), "const v = await agent('x');\nreturn {v: v};", nil)
	val := wfRunValue(t, run)
	// A child failure resolves null, not a script abort.
	if v := val.(map[string]any)["v"]; v != nil {
		t.Fatalf("child failure should resolve null, got %#v", v)
	}
}

func TestWorkflowParallelFansOutAndNullsFailedThunks(t *testing.T) {
	s := &fakeSpawner{results: []agent.DelegateResult{
		{Success: true, Status: "completed", Response: `"a"`},
		{Success: false, Status: "failed"},
		{Success: true, Status: "completed", Response: `"c"`},
	}}
	e := newTestEngine(t, s)
	run := e.Run(context.Background(),
		"const rs = await parallel([()=>agent('one'), ()=>agent('two'), ()=>agent('three')]);\n"+
			"return {rs: rs};", nil)
	val := wfRunValue(t, run)
	out := val.(map[string]any)["rs"].([]any)
	// Three concurrent Join consumers drain the canned results in an
	// unspecified order; assert set-wise: one null, the other two are "a"/"c".
	var nulls int
	vals := map[any]bool{}
	for _, r := range out {
		if r == nil {
			nulls++
		} else {
			vals[r] = true
		}
	}
	if nulls != 1 || !vals["a"] || !vals["c"] || len(vals) != 2 {
		t.Fatalf("parallel results = %#v, want {a, c, null} set", out)
	}
	if e.AgentsStarted() != 3 {
		t.Fatalf("agentsStarted = %d, want 3", e.AgentsStarted())
	}
}

func TestWorkflowPipelineNullsThrowingItem(t *testing.T) {
	e := newTestEngine(t, &fakeSpawner{})
	run := e.Run(context.Background(),
		"const out = await pipeline([1,2], (x)=>x, (x)=>{ if(x===2) throw new Error('boom'); return x; });\n"+
			"return {out: out};", nil)
	val := wfRunValue(t, run)
	out := val.(map[string]any)["out"].([]any)
	if out[0] != float64(1) || out[1] != nil {
		t.Fatalf("pipeline out = %#v, want [1 null]", out)
	}
}

func TestWorkflowTotalAgentCapTripsFatal(t *testing.T) {
	s := &fakeSpawner{}
	cfg := WorkflowConfig{
		MaxConcurrent: 3, MaxTotal: 2, MaxItemsPerCall: 8,
		SyncTimeout: time.Second, EngineTimeout: 30 * time.Second, DefaultAgent: "coder",
	}
	e := NewWorkflowEngine(cfg, s)
	run := e.Run(context.Background(),
		"await parallel([()=>agent('a'), ()=>agent('b'), ()=>agent('c')]);\nreturn 1;", nil)
	if run.StopReason == workflowStopCompleted {
		t.Fatalf("cap trip must fail the run, got %+v", run)
	}
	if !strings.Contains(run.StopMessage, workflowClassAgentCap) {
		t.Fatalf("want AGENT_CAP fatal, got: %s", run.StopMessage)
	}
}

func TestWorkflowItemCapTripsFatal(t *testing.T) {
	e := newTestEngine(t, &fakeSpawner{})
	cfg := e.cfg
	cfg.MaxItemsPerCall = 2
	e = NewWorkflowEngine(cfg, &fakeSpawner{})
	run := e.Run(context.Background(),
		"await pipeline([1,2,3], (x)=>x);\nreturn 1;", nil)
	if run.StopReason == workflowStopCompleted || !strings.Contains(run.StopMessage, workflowClassItemCap) {
		t.Fatalf("want ITEM_CAP fatal, got reason=%s msg=%s", run.StopReason, run.StopMessage)
	}
}

func TestWorkflowConfinementNoNodeLeaks(t *testing.T) {
	e := newTestEngine(t, &fakeSpawner{})
	// process/fetch/XMLHttpRequest are undefined in the codemode VM.
	run := e.Run(context.Background(), "return {process: typeof process, fetch: typeof fetch, require: typeof require};", nil)
	if run.StopReason != workflowStopCompleted {
		t.Fatalf("confinement probe failed: %s", run.StopMessage)
	}
	val := wfRunValue(t, run)
	m := val.(map[string]any)
	if m["process"] != "undefined" || m["fetch"] != "undefined" {
		t.Fatalf("Node APIs leaked into the sandbox: %+v", m)
	}
	// Calling require("fs") must fail closed, never touch the filesystem.
	run2 := e.Run(context.Background(), "require('fs');\nreturn 1;", nil)
	if run2.StopReason == workflowStopCompleted {
		t.Fatalf("require('fs') must fail closed, got completed")
	}
}

// TestWorkflowMemoryCapInterruptsRunaway pins the FAIL-CLOSE allocation
// accounting: a runaway instrumented-allocation loop (Array.push of a long
// string) trips the codemode memory interrupt instead of hanging or OOMing.
// NOTE: uses countAlloc's proven Array.push path (estBytes); this build's goja
// exports int literals as int64, so hookArgBytes on memory.alloc() reads 0.
func TestWorkflowMemoryCapInterruptsRunaway(t *testing.T) {
	cfg := WorkflowConfig{
		MaxConcurrent: 8, MaxTotal: 8, MaxItemsPerCall: 8,
		SyncTimeout: time.Second, EngineTimeout: 10 * time.Second,
		DefaultAgent: "coder", MemoryLimit: 1 << 20, // 1 MiB counted allocations
	}
	e := NewWorkflowEngine(cfg, &fakeSpawner{})
	run := e.Run(context.Background(),
		"const s = 'y'.repeat(64);\nconst arr = [];\nwhile (true) { arr.push(s); }\nreturn arr.length;", nil)
	if run.StopReason == workflowStopCompleted {
		t.Fatalf("allocation runaway must be interrupted, got completed")
	}
	if !strings.Contains(run.StopMessage, "memory cap") {
		t.Fatalf("want memory-cap interruption, got: %s", run.StopMessage)
	}
}

// TestWorkflowTimeoutInterruptsRunaway pins that a busy (non-allocating)
// infinite loop is hard-killed by the engine wall-clock budget, not a hang.
func TestWorkflowTimeoutInterruptsRunaway(t *testing.T) {
	cfg := WorkflowConfig{
		MaxConcurrent: 8, MaxTotal: 8, MaxItemsPerCall: 8,
		SyncTimeout: time.Second, EngineTimeout: time.Second,
		DefaultAgent: "coder",
	}
	e := NewWorkflowEngine(cfg, &fakeSpawner{})
	run := e.Run(context.Background(), "while (true) {}\nreturn 1;", nil)
	if run.StopReason == workflowStopCompleted {
		t.Fatalf("infinite loop must be interrupted, got completed")
	}
	if run.StopReason != workflowStopError {
		t.Fatalf("infinite loop timeout should map to error, got %q (%s)", run.StopReason, run.StopMessage)
	}
}

func TestWorkflowArgsInjection(t *testing.T) {
	e := newTestEngine(t, &fakeSpawner{})
	args := map[string]any{"script": "ignored", "phase": "alpha", "agent_id": "researcher"}
	run := e.Run(context.Background(), "return {phase: args.phase, agentId: args.agent_id};", args)
	val := wfRunValue(t, run)
	m := val.(map[string]any)
	if m["phase"] != "alpha" || m["agentId"] != "researcher" {
		t.Fatalf("args injection wrong: %+v", m)
	}
}

func TestWorkflowResultRenderCap(t *testing.T) {
	long := strings.Repeat("x", workflowRenderCap+10)
	rendered := renderWorkflowResult(long)
	// The notice is appended AFTER the truncated payload, so the total length
	// exceeds the cap; the payload prefix must be exactly the cap's worth.
	if !strings.HasPrefix(rendered, long[:workflowRenderCap]) {
		t.Fatalf("render cap: payload not truncated to %d", workflowRenderCap)
	}
	if len(rendered) <= workflowRenderCap || !strings.Contains(rendered, "[result truncated") {
		t.Fatalf("render cap: truncation notice missing (len=%d)", len(rendered))
	}
	short := renderWorkflowResult("ok")
	if short != "ok" {
		t.Fatalf("short result mutated: %q", short)
	}
}
