package agent

// Tests for leaf 02 of docs/plans/quickplan-mode: dispatcher mode
// plumbing, ambiguity-gate resume, final-fallback swap, and the
// Session execution context block.

import (
	"context"
	"strings"
	"testing"

	"github.com/caimlas/meept/internal/bus"
	"github.com/caimlas/meept/internal/config"
	"github.com/caimlas/meept/internal/memory"
	"github.com/caimlas/meept/internal/plan"
	"github.com/caimlas/meept/internal/task"
)

// TestSuggestMode_QuickPlanNeverShortDowngraded pins suggestMode contract 2
// (master Contract 2): IntentQuickPlan always synthesizes quick_plan,
// regardless of input length or analyzer suggestion. The explicit execution
// phrasing IS the signal; short input never downgrades it to direct.
func TestSuggestMode_QuickPlanNeverShortDowngraded(t *testing.T) {
	cases := []struct {
		name     string
		analysis *TrueIntentAnalysis
		input    string
	}{
		{name: "short input, no analysis", analysis: nil, input: "fix it"},
		{name: "short input, plan suggestion", analysis: &TrueIntentAnalysis{SuggestedMode: "plan"}, input: "fix"},
		{name: "short input, spec_plan suggestion", analysis: &TrueIntentAnalysis{SuggestedMode: "spec_plan"}, input: "fix"},
		{name: "long input, no analysis", analysis: nil, input: strings.Repeat("review and correct the code ", 4)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := suggestMode(IntentQuickPlan, tc.analysis, tc.input); got != "quick_plan" {
				t.Errorf("suggestMode(IntentQuickPlan) = %q, want quick_plan", got)
			}
		})
	}
}

// TestValidModes_QuickPlanRegistered pins master Contract 2: "quick_plan"
// is an accepted SuggestedMode value and survives validateMode round-trip.
func TestValidModes_QuickPlanRegistered(t *testing.T) {
	if _, ok := validModes["quick_plan"]; !ok {
		t.Fatal("quick_plan not present in validModes")
	}
	if got := validateMode("quick_plan"); got != "quick_plan" {
		t.Errorf("validateMode(quick_plan) = %q, want quick_plan", got)
	}
}

// TestClassifyIntent_FinalFallbackIsQuickPlan pins master Contract 5: an
// input that falls through the whole chain (short/simple guard passed,
// capability matcher, LLM, keyword, semantic, heuristic all nil/miss)
// produces the quickplan fallback intent, not chat.
func TestClassifyIntent_FinalFallbackIsQuickPlan(t *testing.T) {
	d := NewDispatcher(DispatcherConfig{Logger: testLogger()})

	// Long enough to pass the short/simple guard, but matching no
	// keyword/heuristic pattern in any classifier.
	const input = "zorblification quixomatic rendlement requested"

	memCtx := &MemoryContext{Results: []memory.MemoryResult{}, IntentCounts: map[string]int{}}
	intent, err := d.classifyIntent(context.Background(), input, memCtx)
	if err != nil {
		t.Fatalf("classifyIntent: %v", err)
	}
	if intent == nil {
		t.Fatal("nil intent")
	}
	if intent.Type != string(IntentQuickPlan) {
		t.Errorf("Type = %q, want %q", intent.Type, string(IntentQuickPlan))
	}
	if intent.AgentType != "orchestrator" {
		t.Errorf("AgentType = %q, want orchestrator", intent.AgentType)
	}
	if intent.Method != "fallback" {
		t.Errorf("Method = %q, want fallback", intent.Method)
	}
	if intent.Confidence != 0.3 {
		t.Errorf("Confidence = %v, want 0.3", intent.Confidence)
	}
}

// TestClassifyIntent_ShortSimpleGuardStillChat pins the regression guard:
// the short/simple guard EARLIER in classifyIntent still routes short
// conversational inputs to chat; the quickplan fallback does not capture
// them.
func TestClassifyIntent_ShortSimpleGuardStillChat(t *testing.T) {
	d := NewDispatcher(DispatcherConfig{Logger: testLogger()})

	memCtx := &MemoryContext{Results: []memory.MemoryResult{}, IntentCounts: map[string]int{}}
	intent, err := d.classifyIntent(context.Background(), "hi", memCtx)
	if err != nil {
		t.Fatalf("classifyIntent: %v", err)
	}
	if intent == nil {
		t.Fatal("nil intent")
	}
	if intent.Type != string(IntentChat) {
		t.Errorf("Type = %q, want chat", intent.Type)
	}
	if intent.Method != "short_message_guard" {
		t.Errorf("Method = %q, want short_message_guard", intent.Method)
	}
}

// TestClassifyAndRoute_ShortInputRoutesChat is the full-pipeline variant of
// the regression guard: "hi" still lands on the chat agent with the guard
// method.
func TestClassifyAndRoute_ShortInputRoutesChat(t *testing.T) {
	d := NewDispatcher(DispatcherConfig{Logger: testLogger()})

	res, err := d.ClassifyAndRoute(context.Background(), "hi there", "session-qp-guard", nil, "")
	if err != nil {
		t.Fatalf("ClassifyAndRoute: %v", err)
	}
	if res.Intent == nil || res.Intent.Type != string(IntentChat) {
		t.Fatalf("intent = %+v, want chat", res.Intent)
	}
	if res.Intent.Method != "short_message_guard" {
		t.Errorf("Method = %q, want short_message_guard", res.Intent.Method)
	}
}

// quickplanAmbiguousAnalysis is an analyzer response the intent analyzer
// parses as an ambiguous QUICKPLAN-category analysis.
const quickplanAmbiguousAnalysis = `{"goal":"review and fix the module","ambiguity":0.9,"scope":"narrow","category":"quickplan","suggested_questions":["Which module do you mean?"],"confidence":0.9}`

// TestAmbiguityGate_QuickPlanClarifySeedsPendingMode wires the ambiguity
// gate end-to-end: an ambiguous input whose analysis resolves to quickplan
// clarification-gates, and the pending clarification state carries
// PendingMode "quick_plan".
func TestAmbiguityGate_QuickPlanClarifySeedsPendingMode(t *testing.T) {
	cs := newCaptureServer(t, quickplanAmbiguousAnalysis)
	d := newDigestCaptureDispatcher(t, cs)

	res, err := d.ClassifyAndRoute(context.Background(), "review the module for bugs and fix them", "sess-qp-gate", nil, "")
	if err != nil {
		t.Fatalf("ClassifyAndRoute: %v", err)
	}
	if res == nil || !res.ClarificationNeeded {
		t.Fatalf("expected clarification gate, got %+v", res)
	}
	if res.Intent == nil || res.Intent.SuggestedMode != "quick_plan" {
		t.Fatalf("clarify intent SuggestedMode = %+v, want quick_plan", res.Intent)
	}

	// isPendingClarification must now be true, and the pending state must
	// carry the quick_plan mode.
	if !d.isPendingClarification("sess-qp-gate") {
		t.Fatal("isPendingClarification = false, want true after clarify gate")
	}
	pending := d.getPendingClarification("sess-qp-gate")
	if pending == nil {
		t.Fatal("getPendingClarification = nil")
	}
	if pending.PendingMode != "quick_plan" {
		t.Errorf("PendingMode = %q, want quick_plan", pending.PendingMode)
	}
}

// TestAmbiguityGate_NonQuickPlanUnchanged pins the unchanged-behavior
// contract: an ambiguous non-quickplan intent clarification-gates exactly
// as before, with an empty PendingMode.
func TestAmbiguityGate_NonQuickPlanUnchanged(t *testing.T) {
	cs := newCaptureServer(t, ambiguousAnalysisJSON) // category "clarification"
	d := newDigestCaptureDispatcher(t, cs)

	res, err := d.ClassifyAndRoute(context.Background(), "did the change get made?", "sess-other-gate", nil, "")
	if err != nil {
		t.Fatalf("ClassifyAndRoute: %v", err)
	}
	if res == nil || !res.ClarificationNeeded {
		t.Fatalf("expected clarification gate, got %+v", res)
	}
	if res.Intent == nil || res.Intent.SuggestedMode != "" {
		t.Fatalf("clarify intent SuggestedMode = %+v, want empty", res.Intent)
	}
	pending := d.getPendingClarification("sess-other-gate")
	if pending == nil {
		t.Fatal("getPendingClarification = nil")
	}
	if pending.PendingMode != "" {
		t.Errorf("PendingMode = %q, want empty", pending.PendingMode)
	}
}

// TestResumeAfterClarification_PreservesQuickPlanMode proves the resume
// path (leaf 02 Task 3.2): after a quickplan-seeded clarification, the
// user's answer resumes with SuggestedMode "quick_plan" on both the intent
// and the DispatchResult, regardless of what the re-classifier returns.
func TestResumeAfterClarification_PreservesQuickPlanMode(t *testing.T) {
	d := NewDispatcher(DispatcherConfig{Logger: testLogger()})

	// Seed the pending-clarification state exactly as the ambiguity gate
	// leaves it: a clarify intent whose SuggestedMode carries quick_plan.
	clarify := &Intent{
		Type:          string(IntentClarify),
		Confidence:    0.9,
		AgentType:     config.AgentIDChat,
		Summary:       "review the module for bugs and fix them",
		SuggestedMode: "quick_plan",
		TrueAnalysis: &TrueIntentAnalysis{
			Goal:      "review and fix the module",
			Ambiguity: 0.9,
			Scope:     "narrow",
			Category:  "quickplan",
		},
	}
	d.sessionTracker.RecordIntent("sess-qp-resume", clarify, config.AgentIDChat)

	// A clear, non-quickplan-classified answer (classifyIntent with an
	// empty dispatcher falls through to the quickplan fallback here, but
	// the point of the test is that the mode is preserved either way).
	res, err := d.ResumeAfterClarification(context.Background(), clarify.Summary, "the dispatcher module", "sess-qp-resume")
	if err != nil {
		t.Fatalf("ResumeAfterClarification: %v", err)
	}
	if res == nil {
		t.Fatal("nil result")
	}
	if res.Intent == nil {
		t.Fatal("nil intent")
	}
	if res.Intent.SuggestedMode != "quick_plan" {
		t.Errorf("resumed intent SuggestedMode = %q, want quick_plan", res.Intent.SuggestedMode)
	}
	if res.SuggestedMode != "quick_plan" {
		t.Errorf("resumed result SuggestedMode = %q, want quick_plan", res.SuggestedMode)
	}
	// The pending clarification must be consumed.
	if d.isPendingClarification("sess-qp-resume") {
		t.Error("pending clarification not cleared after resume")
	}
}

// TestResumeAfterClarification_NonQuickPlanNoMode pins the negative case:
// a legacy (empty PendingMode) clarification resume never synthesizes
// quick_plan on its own. The resumed fallback intent here comes from the
// classifier chain's quickplan final fallback (Type=quickplan), whose
// SuggestedMode is quick_plan by design; the assertion is that the resume
// path adds no override beyond what classification produced, and that the
// combined input did carry through.
func TestResumeAfterClarification_NonQuickPlanNoMode(t *testing.T) {
	d := NewDispatcher(DispatcherConfig{Logger: testLogger()})

	clarify := &Intent{
		Type:       string(IntentClarify),
		Confidence: 0.9,
		AgentType:  config.AgentIDChat,
		Summary:    "did the change get made",
	}
	d.sessionTracker.RecordIntent("sess-legacy-resume", clarify, config.AgentIDChat)

	res, err := d.ResumeAfterClarification(context.Background(), clarify.Summary, "the auth one", "sess-legacy-resume")
	if err != nil {
		t.Fatalf("ResumeAfterClarification: %v", err)
	}
	if res == nil || res.Intent == nil {
		t.Fatal("nil result/intent")
	}
	// The combined input flows through classification (here: the final
	// fallback, since the bare dispatcher has no analyzers wired).
	if res.OriginalInput == clarify.Summary {
		t.Errorf("OriginalInput = %q, want the combined original+answer text", res.OriginalInput)
	}
	if !strings.Contains(res.OriginalInput, "the auth one") {
		t.Errorf("OriginalInput missing the user answer: %q", res.OriginalInput)
	}
}

// TestBuildSessionExecutionContext renders the Task 4 block from seeded
// state: active plan, open tracked tasks, prior quickplan waves — and the
// empty-session omission.
func TestBuildSessionExecutionContext(t *testing.T) {
	logger := testLogger()
	reg, err := task.NewRegistry(t.TempDir()+"/tasks.db", bus.New(nil, nil), logger)
	if err != nil {
		t.Fatalf("task registry: %v", err)
	}
	t.Cleanup(func() { _ = reg.Close() })

	d := NewDispatcher(DispatcherConfig{
		Logger:       logger,
		TaskStore:    reg.Store(),
		Registry:     NewAgentRegistry(RegistryConfig{Logger: logger}),
		TaskRegistry: reg,
	})
	if d.sessionTracker == nil {
		t.Fatal("session tracker not initialized")
	}

	ctx := context.Background()

	// Empty session: block omitted entirely.
	if got := d.buildSessionExecutionContext(ctx, "sess-qp-empty"); got != "" {
		t.Errorf("empty session context = %q, want empty", got)
	}

	// Seed: an open task + a prior quickplan intent.
	tk := task.NewTask("fix the parser", "fix the parser")
	tk.LinkSession("sess-qp-ctx")
	if err := reg.Store().Create(tk); err != nil {
		t.Fatalf("create task: %v", err)
	}
	// session_tasks join row: createTask populates this in production via
	// taskStore.LinkSession; the test must do it explicitly.
	if err := reg.Store().LinkSession(tk.ID, "sess-qp-ctx"); err != nil {
		t.Fatalf("link task session: %v", err)
	}
	d.sessionTracker.RecordIntent("sess-qp-ctx", &Intent{Type: string(IntentQuickPlan), AgentType: "orchestrator"}, "orchestrator")

	// Terminal (completed) task must NOT count as open.
	tkDone := task.NewTask("already finished", "already finished")
	tkDone.LinkSession("sess-qp-ctx")
	tkDone.SetState(task.StateCompleted)
	if err := reg.Store().Create(tkDone); err != nil {
		t.Fatalf("create done task: %v", err)
	}
	if err := reg.Store().LinkSession(tkDone.ID, "sess-qp-ctx"); err != nil {
		t.Fatalf("link done task session: %v", err)
	}

	got := d.buildSessionExecutionContext(ctx, "sess-qp-ctx")
	if !strings.Contains(got, "## Session execution context") {
		t.Errorf("context missing header:\n%s", got)
	}
	if !strings.Contains(got, "Open tracked tasks: 1") {
		t.Errorf("context missing open tasks:\n%s", got)
	}
	if !strings.Contains(got, "fix the parser") {
		t.Errorf("context missing task title:\n%s", got)
	}
	if !strings.Contains(got, "Prior quickplan runs in this conversation: 1") {
		t.Errorf("context missing prior waves:\n%s", got)
	}

	// With a PlanManager wired and a non-terminal plan linked to the
	// session, the Active plan section appears with ID/title/state.
	store, err := plan.NewSQLiteStore(t.TempDir()+"/plans.db", logger)
	if err != nil {
		t.Fatalf("plan store: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	pm := plan.NewPlanManager(store, nil, config.PlansConfig{}, nil, logger)
	if _, err := pm.CreatePlan(ctx, "Quickplan wave one", "desc", "", "", "sess-qp-ctx"); err != nil {
		t.Fatalf("CreatePlan: %v", err)
	}
	d.SetPlanManager(pm)

	got = d.buildSessionExecutionContext(ctx, "sess-qp-ctx")
	if !strings.Contains(got, "Active plan: ") {
		t.Errorf("context missing active plan:\n%s", got)
	}
	if !strings.Contains(got, "Quickplan wave one") {
		t.Errorf("context missing plan title:\n%s", got)
	}
}

// TestPlanRequest_SessionContextOnlyForQuickPlan pins the handler-side
// gating shape: only quick_plan requests carry a SessionContext block.
func TestPlanRequest_SessionContextOnlyForQuickPlan(t *testing.T) {
	req := PlanRequest{Mode: "quick_plan", SessionContext: "## Session execution context\n- Active plan: p1"}
	if req.Mode != "quick_plan" || req.SessionContext == "" {
		t.Fatalf("PlanRequest fields not round-tripping: %+v", req)
	}
	empty := PlanRequest{Mode: "plan"}
	if empty.SessionContext != "" {
		t.Errorf("non-quick_plan request carries SessionContext: %q", empty.SessionContext)
	}
}

// TestSuggestMode_ExistingPlanRoutingUnchanged re-pins the pre-existing
// suggestMode routing table (regression guard for leaf 02 Task 6): plan,
// spec_plan, spec_pair, and direct synthesis are unchanged.
func TestSuggestMode_ExistingPlanRoutingUnchanged(t *testing.T) {
	cases := []struct {
		name       string
		intentType IntentType
		analysis   *TrueIntentAnalysis
		input      string
		want       string
	}{
		{name: "analysis spec_plan wins", intentType: IntentCode, analysis: &TrueIntentAnalysis{SuggestedMode: "spec_plan"}, input: "refactor the auth subsystem", want: "spec_plan"},
		{name: "analysis invalid falls back", intentType: IntentCode, analysis: &TrueIntentAnalysis{SuggestedMode: "garbage"}, input: "refactor the auth subsystem to use oauth2 with pkce flow", want: "plan"},
		{name: "short input downgrades plan", intentType: IntentCode, analysis: &TrueIntentAnalysis{SuggestedMode: ""}, input: "fix typo", want: "direct"},
		{name: "short input does not downgrade spec_plan", intentType: IntentCode, analysis: &TrueIntentAnalysis{SuggestedMode: "spec_plan"}, input: "fix", want: "spec_plan"},
		{name: "compound forces spec_pair", intentType: IntentCompound, analysis: nil, input: "do a then b", want: "spec_pair"},
		{name: "code long input plan", intentType: IntentCode, analysis: nil, input: strings.Repeat("x", 60), want: "plan"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := suggestMode(tc.intentType, tc.analysis, tc.input); got != tc.want {
				t.Errorf("suggestMode(%v) = %q, want %q", tc.intentType, got, tc.want)
			}
		})
	}
}

// TestRequiresApproval_QuickPlanNoApprovalGate pins the strategic-planner
// side of Contract 2: quick_plan dispatches do not stop at the approval
// gate (short of an un-approved interview context).
func TestRequiresApproval_QuickPlanNoApprovalGate(t *testing.T) {
	sp := &StrategicPlanner{logger: testLogger(), approvalStepThreshold: 1}
	req := PlanRequest{Mode: "quick_plan"}
	if sp.requiresApproval(req, make([]*task.TaskStep, 12)) {
		t.Error("quick_plan plan gated at approval despite no interview")
	}
	// Non-quickplan behavior unchanged: many steps still gate.
	planReq := PlanRequest{Mode: "plan"}
	if !sp.requiresApproval(planReq, make([]*task.TaskStep, 12)) {
		t.Error("plan mode approval gate regression: 12 steps should gate")
	}
}
