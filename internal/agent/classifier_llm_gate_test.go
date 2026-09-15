package agent

// Pins for bughunt-2026-09-12 C-0 (the LLM classifier could emit quickplan
// but the async gate stayed shut for that producer) and item 5 (every lane
// the classifier can emit must resolve to an agent, or to the documented
// orchestrator pipeline).
//
// The invariant these tests establish: an LLM-classified lane whose
// ShouldDispatchAsync is true actually reaches the handler's async path
// (handler.go:770: ShouldDispatchAsync(result) && result.Task != nil).

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/caimlas/meept/internal/bus"
	"github.com/caimlas/meept/internal/config"
	"github.com/caimlas/meept/internal/llm"
	"github.com/caimlas/meept/internal/task"
)

// orchestratorAgentAlias is the AgentType the quickplan/compound lanes carry.
// It is a PIPELINE label (the bus topic "orchestrator.plan" plus the
// strategic planner), not a directory-backed agent — see
// TestQuickPlanAsyncPathResolvesPlannerSpecNotOrchestratorDir.
const orchestratorAgentAlias = "orchestrator"

// newClassifierJSONServer returns a real *llm.Client pointed at an httptest
// server that answers every chat completion with content. Used to drive the
// LLM classifier's wire path without a live model.
func newClassifierJSONServer(t *testing.T, content string) *llm.Client {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":` +
			strconv.Quote(content) + `}}],"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}}`))
	}))
	t.Cleanup(srv.Close)
	return llm.NewClient(&llm.ModelConfig{BaseURL: srv.URL, ModelID: "capture"})
}

// laneDispatchExpectations is the INDEPENDENT per-lane table for the
// single-intent producer: the RequiresPlanning flag each lane's intent must
// carry, and the async-dispatch verdict that flag must yield. It is written
// out by hand — NOT derived from IntentType.RequiresPlanning() — because the
// old assertion compared the emitted intent against its own values
// (want := lane.RequiresPlanning() then gotAsync == want), which is a
// tautology for every lane whose RequiresPlanning() is false (24 of the 27
// lanes: the flag is unobservable when it does not feed the async list). A
// regression that dropped or inverted the flag for those lanes passed
// silently. Every lane in classifierLanes MUST appear here.
var laneDispatchExpectations = map[IntentType]struct {
	requiresPlanning bool
	async            bool
}{
	IntentGit:         {false, true},
	IntentSchedule:    {false, false},
	IntentCode:        {true, true},
	IntentDebug:       {false, true},
	IntentReview:      {false, false},
	IntentPlan:        {true, true},
	IntentQuickPlan:   {true, true},
	IntentPlatform:    {false, false},
	IntentReport:      {false, false},
	IntentRecall:      {false, false},
	IntentAnalyze:     {false, false},
	IntentSearch:      {false, false},
	IntentResearch:    {false, false},
	IntentExplore:     {false, false},
	IntentToolUse:     {false, false},
	IntentSecurity:    {false, false},
	IntentStatus:      {false, false},
	IntentWrite:       {false, true},
	IntentArchitect:   {false, true},
	IntentSkeptic:     {false, true},
	IntentLibrarian:   {false, true},
	IntentImageGen:    {false, true},
	IntentVideoGen:    {false, true},
	IntentImageID:     {false, true},
	IntentInstruction: {false, false},
	IntentClarify:     {false, false},
	IntentChat:        {false, false},
}

// TestLLMClassifier_ParseResponseAsyncGateMatchesLaneRule pins the single-
// intent producer (parseResponse): for EVERY lane the classifier can emit,
// the intent it produces must carry the RequiresPlanning flag the explicit
// per-lane table declares, and that flag must yield the table's async verdict.
// Pre-fix only IntentPlan set the flag, so quickplan yielded false and the
// gate shut.
func TestLLMClassifier_ParseResponseAsyncGateMatchesLaneRule(t *testing.T) {
	c := &LLMClassifier{logger: testLogger()}
	if len(classifierLanes) == 0 {
		t.Fatal("classifierLanes is empty")
	}
	for _, lane := range classifierLanes {
		exp, ok := laneDispatchExpectations[lane]
		if !ok {
			t.Errorf("lane %q has no entry in laneDispatchExpectations; the table must cover every emittable lane", lane)
			continue
		}
		content := `{"intent":"` + string(lane) + `","confidence":0.9}`
		got, err := c.parseResponse(content, "carry out the remaining work")
		if err != nil {
			t.Fatalf("parseResponse(%s): %v", lane, err)
		}
		if got.Type != string(lane) {
			t.Fatalf("lane %q: parseResponse emitted %q", lane, got.Type)
		}
		if got.RequiresPlanning != exp.requiresPlanning {
			t.Errorf("lane %q: emitted RequiresPlanning=%v, want %v (explicit lane table)",
				lane, got.RequiresPlanning, exp.requiresPlanning)
		}
		if gotAsync := IntentType(got.Type).ShouldDispatchAsync(got.RequiresPlanning); gotAsync != exp.async {
			t.Errorf("lane %q: emitted RequiresPlanning=%v -> ShouldDispatchAsync=%v, want %v",
				lane, got.RequiresPlanning, gotAsync, exp.async)
		}
	}
	// Every table entry must name a real lane, so the table cannot rot into a
	// superset that hides a missing lane.
	for lane := range laneDispatchExpectations {
		if !isValidIntent(string(lane)) {
			t.Errorf("laneDispatchExpectations names %q, which is not a valid lane", lane)
		}
	}
	// The exact C-0 shape: an LLM quickplan must be async-dispatchable.
	qp, err := c.parseResponse(`{"intent":"quickplan","confidence":0.9}`, "just do it")
	if err != nil {
		t.Fatalf("parseResponse(quickplan): %v", err)
	}
	if !qp.RequiresPlanning {
		t.Fatal("LLM quickplan missing RequiresPlanning: async gate can never open (C-0)")
	}
	if !IntentType(qp.Type).ShouldDispatchAsync(qp.RequiresPlanning) {
		t.Fatal("ShouldDispatchAsync = false for LLM quickplan: quickplan -> orchestrator pipeline is dead (C-0)")
	}
}

// TestLLMClassifier_ClassifyMultiQuickPlanSetsRequiresPlanning pins the
// multi-intent producer (ClassifyMulti): its requiresPlanning derivation was
// the second plan-only equality the finding named.
func TestLLMClassifier_ClassifyMultiQuickPlanSetsRequiresPlanning(t *testing.T) {
	client := newClassifierJSONServer(t, `[{"intent":"quickplan","confidence":0.9,"summary":"carry it out"}]`)
	c := NewLLMClassifier(LLMClassifierConfig{Client: client}, testLogger())

	intents := c.ClassifyMulti(context.Background(), "knock out the whole roadmap", nil)
	if len(intents) != 1 {
		t.Fatalf("ClassifyMulti returned %d intents, want 1", len(intents))
	}
	got := intents[0]
	if got.Type != string(IntentQuickPlan) {
		t.Fatalf("intent type = %q, want quickplan", got.Type)
	}
	if !got.RequiresPlanning {
		t.Fatal("ClassifyMulti quickplan missing RequiresPlanning: async gate can never open (C-0)")
	}
	if !IntentType(got.Type).ShouldDispatchAsync(got.RequiresPlanning) {
		t.Fatal("ShouldDispatchAsync = false for ClassifyMulti quickplan (C-0)")
	}
}

// newPromptAwareClassifierServer answers the analyzer, the multi-intent
// detector, and the single-intent classifier with distinguishable responses
// so one server can drive a full ClassifyAndRoute turn.
func newPromptAwareClassifierServer(t *testing.T) *llm.Client {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var req struct {
			Messages []llm.ChatMessage `json:"messages"`
		}
		_ = json.Unmarshal(body, &req)
		var prompt strings.Builder
		for _, m := range req.Messages {
			prompt.WriteString(m.Content)
			prompt.WriteString("\n")
		}
		var content string
		switch {
		case strings.Contains(prompt.String(), "identify ALL distinct intents"):
			content = `[]` // multi-intent: no compound
		case strings.Contains(prompt.String(), "Classify this user input"):
			content = `{"intent":"quickplan","confidence":0.92,"reasoning":"direct execution"}`
		default:
			// Intent analyzer: a clear, non-ambiguous analysis.
			content = `{"goal":"carry out the roadmap","ambiguity":0.1,"scope":"broad","category":"quickplan","suggested_questions":[],"confidence":0.9}`
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":` +
			strconv.Quote(content) + `}}],"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}}`))
	}))
	t.Cleanup(srv.Close)
	return llm.NewClient(&llm.ModelConfig{BaseURL: srv.URL, ModelID: "capture"})
}

// TestClassifyAndRoute_LLMQuickPlanOpensAsyncGate is the end-to-end pin: an
// LLM-classified quickplan, routed through the real ClassifyAndRoute chain,
// must satisfy the exact handler async-gate expression
// (handler.go:770: ShouldDispatchAsync(result) && result.Task != nil).
// Pre-fix ShouldDispatchAsync was false and the createTask below produced a
// task the handler then orphaned.
func TestClassifyAndRoute_LLMQuickPlanOpensAsyncGate(t *testing.T) {
	logger := digestTestLogger()
	reg, err := task.NewRegistry(filepath.Join(t.TempDir(), "tasks.db"), bus.New(nil, nil), logger)
	if err != nil {
		t.Fatalf("task registry: %v", err)
	}
	t.Cleanup(func() { _ = reg.Close() })

	d := NewDispatcher(DispatcherConfig{
		Registry:         NewAgentRegistry(RegistryConfig{Logger: logger}),
		TaskStore:        reg.Store(),
		TaskRegistry:     reg,
		ClassifierClient: newPromptAwareClassifierServer(t),
		Logger:           logger,
	})

	// Long enough not to trip the short/simple guard, and carrying no
	// compound-signal phrase, so ClassifyAndRoute reaches the single-intent
	// classifier with the canned quickplan verdict.
	const input = "knock out the whole roadmap of remaining migration work without checking in with me at every step"

	res, err := d.ClassifyAndRoute(context.Background(), input, "sess-llm-qp", nil, "", "")
	if err != nil {
		t.Fatalf("ClassifyAndRoute: %v", err)
	}
	if res.Intent == nil {
		t.Fatal("nil intent")
	}
	if res.Intent.Type != string(IntentQuickPlan) {
		t.Fatalf("intent = %q, want quickplan (method=%q)", res.Intent.Type, res.Intent.Method)
	}
	if res.Intent.Method != "llm" {
		t.Fatalf("classification method = %q, want llm (the LLM producer is what this pin exercises)", res.Intent.Method)
	}
	if !res.Intent.RequiresPlanning {
		t.Fatal("LLM-classified quickplan missing RequiresPlanning (C-0 regression)")
	}
	if res.Task == nil {
		t.Fatal("quickplan produced no task; the handler gate needs Task != nil")
	}
	if !d.ShouldDispatchAsync(res) {
		t.Fatal("ShouldDispatchAsync = false for LLM quickplan: handler async branch (handler.go:770) unreachable (C-0)")
	}
	// The gate expression verbatim.
	if !(d.ShouldDispatchAsync(res) && res.Task != nil) {
		t.Fatal("handler async gate (ShouldDispatchAsync && Task != nil) is shut for an LLM-classified quickplan")
	}
}

// TestClassifierLanes_EveryLaneResolvesToAnAgentOnDisk answers item 5 with
// the SHIPPED routing table: build the lane index the daemon publishes from
// config/agents frontmatter, then require every emittable lane to resolve to
// an agent definition that exists on disk. Exactly one exception is
// permitted - the orchestrator pipeline alias - and it is asserted to be the
// only one.
func TestClassifierLanes_EveryLaneResolvesToAnAgentOnDisk(t *testing.T) {
	idx, err := BuildLaneAgentIndexFromDir("../../config/agents")
	if err != nil {
		t.Fatalf("BuildLaneAgentIndexFromDir(config/agents): %v", err)
	}
	if len(idx) == 0 {
		t.Fatal("config/agents declares no intents; the frontmatter routing path is not primary")
	}
	prev := CurrentLaneAgentIndex()
	PublishLaneAgentIndex(idx)
	t.Cleanup(func() { PublishLaneAgentIndex(prev) })

	onDisk := 0
	var aliased []string
	for _, lane := range classifierLanes {
		name := string(lane)
		agent := agentForIntent(name)
		if agent == "" {
			t.Errorf("lane %q resolves to no agent", name)
			continue
		}
		if _, err := os.Stat(filepath.Join("../../config/agents", agent, "AGENT.md")); err == nil {
			onDisk++
			continue
		}
		aliased = append(aliased, name+"->"+agent)
		if agent != orchestratorAgentAlias {
			t.Errorf("lane %q resolves to agent %q, which has no ../../config/agents/%s/AGENT.md",
				name, agent, agent)
		}
	}
	// Every lane except the single orchestrator pipeline alias must land on
	// a directory-backed agent. If a new alias appears, this fails and the
	// author must either add the spec or document the pipeline.
	if onDisk != len(classifierLanes)-1 {
		t.Errorf("lanes resolving to an on-disk agent = %d, want %d; aliased lanes = %v",
			onDisk, len(classifierLanes)-1, aliased)
	}
	if len(aliased) != 1 || !strings.HasPrefix(aliased[0], "quickplan->") {
		t.Errorf("expected exactly one aliased lane (quickplan->orchestrator), got %v", aliased)
	}
}

// TestQuickPlanAsyncPathResolvesPlannerSpecNotOrchestratorDir pins the
// source-proven answer to item 5, so a future reader does not re-derive it:
//
//	handler.go:770  async branch on ShouldDispatchAsync && Task != nil
//	handler.go:1154 publishPlanRequest -> bus topic "orchestrator.plan"
//	orchestrator.go:193 handlePlanRequest is the subscriber
//	strategic.go:292/763/944 strategic.Plan resolves the PLANNER spec via
//	                        sp.registry.Get(config.AgentIDPlanner)
//
// The AgentType "orchestrator" is a pipeline label stamped on the intents
// and DispatchResult; the only place it would be resolved as an agent is
// RouteToAgent (dispatcher.go), which the async branch skips whenever a Task
// exists (and quickplan always creates one). So the absent
// config/agents/orchestrator is irrelevant and needs no spec directory.
func TestQuickPlanAsyncPathResolvesPlannerSpecNotOrchestratorDir(t *testing.T) {
	if got := agentForIntent(string(IntentQuickPlan)); got != orchestratorAgentAlias {
		t.Fatalf("agentForIntent(quickplan) = %q, want %q", got, orchestratorAgentAlias)
	}
	if _, err := os.Stat(filepath.Join("../../config/agents", orchestratorAgentAlias, "AGENT.md")); err == nil {
		t.Error("config/agents/orchestrator now exists; this test's premise (pipeline alias, not an agent dir) is stale")
	}
	// The spec the async path ACTUALLY resolves must exist on disk.
	if _, err := os.Stat(filepath.Join("../../config/agents", config.AgentIDPlanner, "AGENT.md")); err != nil {
		t.Fatalf("planner spec missing: the orchestrator.plan path resolves config.AgentIDPlanner (%q) and it must exist: %v",
			config.AgentIDPlanner, err)
	}
	// quickplan meets both handler-gate conditions, so RouteToAgent (the
	// only AgentType -> registry resolution) is never reached.
	if !IntentQuickPlan.ShouldCreateTask() {
		t.Fatal("quickplan must create tasks or the handler async branch is unreachable")
	}
	if !IntentQuickPlan.ShouldDispatchAsync(IntentQuickPlan.RequiresPlanning()) {
		t.Fatal("quickplan must dispatch async")
	}
}
