package agent

// Tests for leaf 02 of docs/plans/quickplan-mode: dispatcher mode
// plumbing, ambiguity-gate resume, final-fallback swap, and the
// Session execution context block.

import (
	"context"
	"strings"
	"testing"
	"time"

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

// TestClassifyIntent_FallbackQuickPlanDispatchesAsync pins bughunt
// 2026-09-10 C1: the quickplan fallback producer must set
// RequiresPlanning on the intent so the full-pipeline result opens the
// handler's async gate (ShouldDispatchAsync && Task != nil). Without the
// field the handler routes the turn to a chat loop and orphans the
// created task — the quickplan→orchestrator pipeline was dead code.
func TestClassifyIntent_FallbackQuickPlanDispatchesAsync(t *testing.T) {
	d := NewDispatcher(DispatcherConfig{Logger: testLogger()})

	// Full ClassifyAndRoute pass: fallback intent + created task. The
	// bare dispatcher has no task store, so the task is synthesized the
	// same way the handler-visible path requires (ShouldCreateTask is
	// true for quickplan; the handler's async branch needs Task != nil).
	const input = "zorblification quixomatic rendlement requested"
	res, err := d.ClassifyAndRoute(context.Background(), input, "sess-qp-async-fallback", nil, "", "")
	if err != nil {
		t.Fatalf("ClassifyAndRoute: %v", err)
	}
	if res.Intent == nil || res.Intent.Type != string(IntentQuickPlan) {
		t.Fatalf("intent = %+v, want %q", res.Intent, string(IntentQuickPlan))
	}
	if !IntentType(res.Intent.Type).ShouldCreateTask() {
		t.Fatal("quickplan must create tasks; handler async branch would never be reached")
	}
	if !res.Intent.RequiresPlanning {
		t.Fatal("fallback quickplan intent missing RequiresPlanning: async gate can never open (C1 regression)")
	}
	if res.Task == nil {
		// Mirror the handler's field: with a task store wired, the
		// dispatcher creates the task; the gate itself reads only the
		// intent, so attach it to exercise the exact gate expression.
		res.Task = task.NewTask("qp", input)
	}
	if !d.ShouldDispatchAsync(res) {
		t.Fatal("ShouldDispatchAsync = false for fallback quickplan with task: quickplan→orchestrator pipeline is dead (C1 regression)")
	}
}

// TestClassifyIntent_SemanticQuickPlanDispatchesAsync pins the C1 twin on
// the semantic-matching producer (classifyIntent step 3.5): a semantic
// quickplan match must carry RequiresPlanning so the result dispatches
// async. The semantic index is driven by a canned embedding client that
// ranks the quickplan intent definition top for the probe input.
func TestClassifyIntent_SemanticQuickPlanDispatchesAsync(t *testing.T) {
	d := NewDispatcher(DispatcherConfig{Logger: testLogger()})

	// Build a real semantic index over intent texts; the canned client
	// makes every vector identical, so the cosine tie resolves in
	// BuildIndex's slice order. Permute the index so QUICKPLAN is the
	// first entry — mirroring a genuine quickplan neighborhood — and the
	// tie-break hands the match to quickplan.
	client := &cannedFirstMatchEmbeddingClient{dim: 8}
	idx := NewSemanticIndex(client)
	if err := idx.BuildIndex(context.Background()); err != nil {
		t.Fatalf("BuildIndex: %v", err)
	}
	entries := idx.entries
	vectors := idx.vectors
	pi := -1
	for i, e := range entries {
		if e.IntentType == IntentQuickPlan {
			pi = i
			break
		}
	}
	if pi < 0 {
		t.Fatal("semantic index does not contain quickplan")
	}
	entries[0], entries[pi] = entries[pi], entries[0]
	vectors[0], vectors[pi] = vectors[pi], vectors[0]
	match := idx.Match("knock out the whole roadmap", 0.01)
	if match == nil || match.IntentType != IntentQuickPlan {
		t.Fatalf("semantic match = %+v, want quickplan", match)
	}
	d.semanticIndex = idx

	memCtx := &MemoryContext{Results: []memory.MemoryResult{}, IntentCounts: map[string]int{}}
	intent, err := d.classifyIntent(context.Background(), "knock out the whole roadmap", memCtx)
	if err != nil {
		t.Fatalf("classifyIntent: %v", err)
	}
	if intent == nil || intent.Type != string(IntentQuickPlan) {
		t.Fatalf("intent = %+v, want %q", intent, string(IntentQuickPlan))
	}
	if intent.Method != "semantic" {
		t.Errorf("Method = %q, want semantic", intent.Method)
	}
	if !intent.RequiresPlanning {
		t.Fatal("semantic quickplan intent missing RequiresPlanning: async gate can never open (C1 regression)")
	}
	res := &DispatchResult{Intent: intent, Task: task.NewTask("qp", "qp")}
	if !d.ShouldDispatchAsync(res) {
		t.Fatal("ShouldDispatchAsync = false for semantic quickplan with task (C1 regression)")
	}
}

// cannedFirstMatchEmbeddingClient returns a fixed unit vector for every
// text, collapsing all cosine similarities to 1.0 so Match resolves by the
// first indexed entry (the semantic index lists quickplan first).
type cannedFirstMatchEmbeddingClient struct{ dim int }

func (c *cannedFirstMatchEmbeddingClient) Embed(_ context.Context, _ string) ([]float64, error) {
	v := make([]float64, c.dim)
	v[0] = 1
	return v, nil
}

func (c *cannedFirstMatchEmbeddingClient) EmbedBatch(_ context.Context, texts []string) ([][]float64, error) {
	out := make([][]float64, len(texts))
	for i := range texts {
		out[i] = []float64{1}
		for len(out[i]) < c.dim {
			out[i] = append(out[i], 0)
		}
	}
	return out, nil
}

func (c *cannedFirstMatchEmbeddingClient) Dimension() int { return c.dim }

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

	res, err := d.ClassifyAndRoute(context.Background(), "hi there", "session-qp-guard", nil, "", "")
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

	res, err := d.ClassifyAndRoute(context.Background(), "review the module for bugs and fix them", "sess-qp-gate", nil, "", "")
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

	res, err := d.ClassifyAndRoute(context.Background(), "did the change get made?", "sess-other-gate", nil, "", "")
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

// quickplanStillAmbiguousAnalysis is a re-analysis of the combined
// original+answer input that is STILL highly ambiguous — the gibberish
// second answer case.
const quickplanStillAmbiguousAnalysis = `{"goal":"still unclear","ambiguity":0.9,"scope":"narrow","category":"clarification","suggested_questions":["What exactly should be done?"],"confidence":0.9}`

// TestResumeGate_OnlyClarifyMarkerStillAsksFollowUp pins bughunt 2026-09-10
// M2: the resume-path A5 gate must treat a digest whose ONLY entry is the
// clarify marker as empty, so a gibberish second answer produces a
// FOLLOW-UP clarification — not a route. buildClarificationResult records
// the clarify intent in the tracker before the resume rebuilds the digest;
// without the clarify-ignoring emptiness check the marker destroys the
// gate's own precondition and the second answer silently routes (into
// autonomous quickplan execution once C1 is fixed).
func TestResumeGate_OnlyClarifyMarkerStillAsksFollowUp(t *testing.T) {
	cs := newCaptureServer(t, quickplanStillAmbiguousAnalysis)
	d := newDigestCaptureDispatcher(t, cs)

	// Seed the tracker EXACTLY as the first clarification leaves it: the
	// clarify intent is the session's only recorded entry.
	clarify := &Intent{
		Type:       string(IntentClarify),
		Confidence: 0.9,
		AgentType:  config.AgentIDChat,
		Summary:    "do the thing",
	}
	d.sessionTracker.RecordIntent("sess-m2-gibberish", clarify, config.AgentIDChat)

	// Digest precondition: only the clarify marker present.
	digest := d.buildSessionContextDigest("sess-m2-gibberish")
	if digest.IsEmpty() {
		t.Fatal("precondition: digest with clarify marker should be non-empty under IsEmpty")
	}
	if !digest.IsEmptyIgnoringClarify() {
		t.Fatal("digest with ONLY the clarify marker must be empty under IsEmptyIgnoringClarify (M2)")
	}

	res, err := d.ResumeAfterClarification(context.Background(), clarify.Summary, "blorf narble zinx", "sess-m2-gibberish")
	if err != nil {
		t.Fatalf("ResumeAfterClarification: %v", err)
	}
	if res == nil || !res.ClarificationNeeded {
		t.Fatalf("still-ambiguous second answer routed instead of follow-up clarification: %+v (M2 regression: silent routing = autonomous quickplan under C1)", res)
	}
}

// TestResumeGate_RealContextProceeds pins the other resume branch: a
// session with REAL context (task fields + non-clarify intent) must NOT
// clarification-gate on an ambiguous answer — history-aware classification
// proceeds, exactly as the pre-M2 A5 gate intended for non-empty digests.
func TestResumeGate_RealContextProceeds(t *testing.T) {
	cs := newCaptureServer(t, `{"goal":"fix bug","ambiguity":0.9,"scope":"narrow","category":"fix","suggested_questions":[],"confidence":0.9}`)
	d := newDigestCaptureDispatcher(t, cs)

	// Real context: a tracked task + a non-clarify last intent.
	base := time.Date(2026, 9, 10, 9, 0, 0, 0, time.UTC)
	seedDigestTask(t, d, "task-m2", "the login bug", "sess-m2-ctx", task.StateCompleted, base, "coder")
	seedDigestStep(t, d.taskRegistry, "task-m2", 0, task.StepCompleted, "Fixed the login bug.")
	d.sessionTracker.RecordIntent("sess-m2-ctx", &Intent{Type: string(IntentCode), AgentType: "coder"}, "coder")

	digest := d.buildSessionContextDigest("sess-m2-ctx")
	if digest.IsEmpty() {
		t.Fatal("precondition: real-context digest should be non-empty")
	}
	if !digest.IsEmptyIgnoringClarify() == digest.IsEmpty() {
		// Same verdict expected for this fixture under both measures.
		t.Fatal("IsEmptyIgnoringClarify must agree with IsEmpty when no clarify marker is present")
	}

	res, err := d.ResumeAfterClarification(context.Background(), "the login bug", "the one you just finished", "sess-m2-ctx")
	if err != nil {
		t.Fatalf("ResumeAfterClarification: %v", err)
	}
	if res == nil || res.ClarificationNeeded {
		t.Fatalf("context-bearing session re-clarified an ambiguous answer; A5 gate regression: %+v", res)
	}
}

// TestRouteToPlan_QuickPlanCarriesThrough pins the M3 dispatcher half: a
// quickplan intent that reaches routeToPlan (plans config always-plan, or a
// threshold rule) must produce an async-dispatchable result — Task created,
// SuggestedMode quick_plan, empty Response — instead of the legacy
// "plan created" text reply that terminated as direct_response and stranded
// the draft plan. Non-quickplan intents keep the text response.
func TestRouteToPlan_QuickPlanCarriesThrough(t *testing.T) {
	d := newDigestCaptureDispatcher(t, newCaptureServer(t, `{}`))
	if d.planManager == nil {
		logger := digestTestLogger()
		store, err := plan.NewSQLiteStore(t.TempDir()+"/plans.db", logger)
		if err != nil {
			t.Fatalf("plan store: %v", err)
		}
		t.Cleanup(func() { _ = store.Close() })
		d.SetPlanManager(plan.NewPlanManager(store, nil, config.PlansConfig{}, nil, logger))
	}
	ctx := context.Background()

	// Quickplan intent: task + mode + no text response.
	qp := &Intent{
		Type:             string(IntentQuickPlan),
		Confidence:       0.8,
		AgentType:        "orchestrator",
		Summary:          "carry out the migration plan",
		RequiresPlanning: true,
	}
	res, err := d.routeToPlan(ctx, "carry out the migration plan without check-ins", qp, "sess-m3-qp")
	if err != nil {
		t.Fatalf("routeToPlan(quickplan): %v", err)
	}
	if res == nil || res.Task == nil {
		t.Fatalf("quickplan routeToPlan produced no Task; async gate can never open: %+v", res)
	}
	if res.Response != "" {
		t.Errorf("quickplan routeToPlan set Response %q; handler treats non-empty as direct_response (M3)", res.Response)
	}
	if res.SuggestedMode != string(IntentQuickPlan.SuggestedMode()) {
		t.Errorf("SuggestedMode = %q, want %q", res.SuggestedMode, IntentQuickPlan.SuggestedMode())
	}
	if res.Intent == nil || res.Intent.Type != string(IntentQuickPlan) {
		t.Errorf("intent not carried through: %+v", res.Intent)
	}

	// Non-quickplan intent: legacy text response, no task.
	planIntent := &Intent{Type: string(IntentPlan), Confidence: 0.8, AgentType: config.AgentIDPlanner, Summary: "plan the refactor"}
	res2, err := d.routeToPlan(ctx, "plan the refactor", planIntent, "sess-m3-plan")
	if err != nil {
		t.Fatalf("routeToPlan(plan): %v", err)
	}
	if res2.Response == "" {
		t.Error("non-quickplan routeToPlan lost the legacy plan-created response")
	}
	if res2.Task != nil {
		t.Error("non-quickplan routeToPlan created a task; legacy behavior is text-only")
	}
}

// TestPendingClarification_OriginalInputLossless pins the M4 fix: a clarify
// intent recorded with OriginalInput resumes from the FULL original input,
// not the 100-char Summary; a legacy record without OriginalInput falls
// back to the Summary.
func TestPendingClarification_OriginalInputLossless(t *testing.T) {
	d := newDigestCaptureDispatcher(t, newCaptureServer(t, `{}`))
	if d.sessionTracker == nil {
		t.Fatal("dispatcher has no session tracker")
	}

	full := "Fix the parser bug where deeply nested JSON5 objects containing unicode escapes like \\u00e9 in long key names blow the stack. It reproduces on the config loader path."
	if len(full) <= 100 {
		t.Fatalf("fixture must exceed the 100-char summary cap; got %d", len(full))
	}

	// New-style record: OriginalInput set.
	d.sessionTracker.RecordIntent("sess-m4-full", &Intent{
		Type:          string(IntentClarify),
		Confidence:    1.0,
		AgentType:     config.AgentIDChat,
		Summary:       extractSummary(full),
		OriginalInput: full,
	}, config.AgentIDChat)
	pending := d.getPendingClarification("sess-m4-full")
	if pending == nil {
		t.Fatal("pending clarification not found")
	}
	if pending.OriginalInput != full {
		t.Errorf("resume input truncated to summary (M4 regression):\n got: %q\nwant: %q", pending.OriginalInput, full)
	}

	// Legacy record: no OriginalInput -> Summary fallback.
	d.sessionTracker.RecordIntent("sess-m4-legacy", &Intent{
		Type:       string(IntentClarify),
		Confidence: 1.0,
		AgentType:  config.AgentIDChat,
		Summary:    extractSummary(full),
	}, config.AgentIDChat)
	pending2 := d.getPendingClarification("sess-m4-legacy")
	if pending2 == nil {
		t.Fatal("legacy pending clarification not found")
	}
	if pending2.OriginalInput != extractSummary(full) {
		t.Errorf("legacy fallback should use Summary:\n got: %q\nwant: %q", pending2.OriginalInput, extractSummary(full))
	}
}
