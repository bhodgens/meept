package agent

import (
	"context"
	"strings"
	"testing"

	"github.com/caimlas/meept/internal/llm"
	"github.com/caimlas/meept/internal/task"
	"github.com/caimlas/meept/internal/tools"
)

// ---------------------------------------------------------------------------
// Test doubles
// ---------------------------------------------------------------------------

// critiqueStubTool implements tools.Tool for the critique-input tests.
type critiqueStubTool struct {
	tools.ToolDefaults
	name string
}

func (t *critiqueStubTool) Name() string                                             { return t.name }
func (t *critiqueStubTool) Description() string                                      { return t.name + " tool" }
func (t *critiqueStubTool) Parameters() llm.FunctionParameters                       { return llm.FunctionParameters{} }
func (t *critiqueStubTool) Execute(_ context.Context, _ map[string]any) (any, error) { return nil, nil }

// newCritiqueToolRegistry builds a real tools.Registry carrying the given
// tool names — the same production registry type the daemon wires into
// SetValidToolNames.
func newCritiqueToolRegistry(t *testing.T, names ...string) *tools.Registry {
	t.Helper()
	reg := tools.NewRegistry(nil)
	for _, n := range names {
		reg.Register(&critiqueStubTool{name: n})
	}
	return reg
}

// critiqueSessionFixture creates one task linked to sessionID with one step
// per description, persists the steps, and returns the task ID plus the
// step IDs in description order.
func critiqueSessionFixture(t *testing.T, store *task.Store, stepStore *task.StepStore, sessionID string, descriptions ...string) (string, []string) {
	t.Helper()
	st := task.NewTask("critique fixture", "")
	st.LinkSession(sessionID)
	if err := store.Create(st); err != nil {
		t.Fatalf("failed to create fixture task: %v", err)
	}
	var stepIDs []string
	for i, desc := range descriptions {
		step := task.NewTaskStep(st.ID, desc, i)
		if err := stepStore.Create(step); err != nil {
			t.Fatalf("failed to create fixture step: %v", err)
		}
		if err := stepStore.SetSessionID(step.ID, sessionID); err != nil {
			t.Fatalf("failed to set fixture step session: %v", err)
		}
		stepIDs = append(stepIDs, step.ID)
	}
	return st.ID, stepIDs
}

// ---------------------------------------------------------------------------
// Store-level pins: SetReviewVerdict / ReviewVerdictsForSession
// ---------------------------------------------------------------------------

// TestReviewVerdictsForSession_NilSafeStore pins the leaf's nil-safety pin:
// an unknown/empty session returns zero verdicts with no error, and a nil
// store surfaces an error rather than pretending there were no verdicts.
func TestReviewVerdictsForSession_NilSafeStore(t *testing.T) {
	_, stepStore := newTestTaskAndStepStore(t)

	// Unknown session: zero verdicts, nil error.
	verdicts, err := stepStore.ReviewVerdictsForSession("session-never-existed", 3)
	if err != nil {
		t.Fatalf("unknown session should not error: %v", err)
	}
	if len(verdicts) != 0 {
		t.Fatalf("unknown session should return zero verdicts, got %d", len(verdicts))
	}

	// Empty session ID: zero verdicts, nil error (session-scoped by
	// construction; session-less steps never leak).
	verdicts, err = stepStore.ReviewVerdictsForSession("", 3)
	if err != nil {
		t.Fatalf("empty session should not error: %v", err)
	}
	if len(verdicts) != 0 {
		t.Fatalf("empty session should return zero verdicts, got %d", len(verdicts))
	}

	// A nil receiver must not panic.
	var nilStore *task.StepStore
	if _, err := nilStore.ReviewVerdictsForSession("session-x", 3); err == nil {
		t.Fatalf("nil store should return an error, not silent zero")
	}
}

// TestReviewVerdictsForSession_BoundedAndSessionScoped pins the read-side
// bounds: only the LAST count verdicts come back (oldest-first), steps from
// other sessions never leak, and each string is bounded to
// task.ReviewReasonMaxChars runes.
func TestReviewVerdictsForSession_BoundedAndSessionScoped(t *testing.T) {
	store, stepStore := newTestTaskAndStepStore(t)

	taskIDA, _ := critiqueSessionFixture(t, store, stepStore, "sess-A",
		"step one", "step two", "step three", "step four", "step five", "step six")
	_, otherStepIDs := critiqueSessionFixture(t, store, stepStore, "sess-B", "other session step")

	stepsA, err := stepStore.ListByTaskID(taskIDA)
	if err != nil {
		t.Fatalf("failed to list session A steps: %v", err)
	}
	if len(stepsA) != 6 {
		t.Fatalf("fixture setup: want 6 session A steps, got %d", len(stepsA))
	}

	for i, s := range stepsA {
		v := ReviewApproved
		if i%2 == 1 {
			v = ReviewRejected
		}
		if err := stepStore.SetReviewVerdict(s.ID, string(v), "reason for "+s.Description); err != nil {
			t.Fatalf("failed to persist verdict: %v", err)
		}
	}
	if err := stepStore.SetReviewVerdict(otherStepIDs[0], string(ReviewRejected), "cross-session reason"); err != nil {
		t.Fatalf("failed to persist cross-session verdict: %v", err)
	}

	verdicts, err := stepStore.ReviewVerdictsForSession("sess-A", 3)
	if err != nil {
		t.Fatalf("failed to read verdicts: %v", err)
	}
	if len(verdicts) != 3 {
		t.Fatalf("want exactly 3 verdicts (bounded), got %d", len(verdicts))
	}
	// Oldest-first window over the last three reviewed steps.
	wantDescs := []string{"step four", "step five", "step six"}
	for i, w := range wantDescs {
		if verdicts[i].Description != w {
			t.Fatalf("verdict[%d] = %q, want %q", i, verdicts[i].Description, w)
		}
	}
	for _, v := range verdicts {
		if v.TaskID == "" {
			t.Fatalf("verdict for %q carries empty task id", v.Description)
		}
		if len([]rune(v.Reason)) > task.ReviewReasonMaxChars {
			t.Fatalf("reason bound violated: %d runes", len([]rune(v.Reason)))
		}
	}
	// The fixture mapped even indices (step one/three/five) to approved;
	// "step four" sits at index 3 → rejected.
	if got := verdicts[0].Verdict; got != string(ReviewRejected) {
		t.Fatalf("step four should be rejected (odd fixture index), got %q", got)
	}

	// Session B sees exactly its own verdict.
	bVerdicts, err := stepStore.ReviewVerdictsForSession("sess-B", 3)
	if err != nil {
		t.Fatalf("failed to read session B verdicts: %v", err)
	}
	if len(bVerdicts) != 1 || bVerdicts[0].Description != "other session step" {
		t.Fatalf("session B read leaked or lost verdicts: %+v", bVerdicts)
	}
}

// TestSetReviewVerdict_BoundsReason pins the write-side bound: a reason
// longer than task.ReviewReasonMaxChars is truncated to exactly that many
// runes.
func TestSetReviewVerdict_BoundsReason(t *testing.T) {
	store, stepStore := newTestTaskAndStepStore(t)
	_, stepIDs := critiqueSessionFixture(t, store, stepStore, "sess-bound", "bounded step")

	longReason := strings.Repeat("x", 5000)
	if err := stepStore.SetReviewVerdict(stepIDs[0], string(ReviewRejected), longReason); err != nil {
		t.Fatalf("failed to persist verdict: %v", err)
	}
	verdicts, err := stepStore.ReviewVerdictsForSession("sess-bound", 3)
	if err != nil {
		t.Fatalf("failed to read verdicts: %v", err)
	}
	if len(verdicts) != 1 {
		t.Fatalf("want 1 verdict, got %d", len(verdicts))
	}
	if got := len([]rune(verdicts[0].Reason)); got > task.ReviewReasonMaxChars {
		t.Fatalf("reason bound violated: %d runes > %d", got, task.ReviewReasonMaxChars)
	}
	// 5000 'x' truncated to 200 runes = 199 x's + "…".
	if !strings.HasSuffix(verdicts[0].Reason, "…") || len([]rune(verdicts[0].Reason)) != task.ReviewReasonMaxChars {
		t.Fatalf("want exactly %d runes ending in ellipsis, got %d runes %q",
			task.ReviewReasonMaxChars, len([]rune(verdicts[0].Reason)), verdicts[0].Reason)
	}
}

// ---------------------------------------------------------------------------
// Assembly pins: BuildPlanCritiqueInput
// ---------------------------------------------------------------------------

// TestBuildCritiqueInput_AllSourcesRendered pins: with every source present,
// the rendered prompt carries all four sections.
func TestBuildCritiqueInput_AllSourcesRendered(t *testing.T) {
	store, stepStore := newTestTaskAndStepStore(t)
	_, stepIDs := critiqueSessionFixture(t, store, stepStore, "sess-all", "implement the parser")
	if err := stepStore.SetReviewVerdict(stepIDs[0], string(ReviewRejected), "missed the edge cases"); err != nil {
		t.Fatalf("failed to persist verdict: %v", err)
	}

	reg := newCritiqueToolRegistry(t, "shell", "file_read", "file_write")
	input, err := BuildPlanCritiqueInput(CritiqueInputSources{
		StepStore: stepStore,
		Registry:  reg,
		SessionID: "sess-all",
		Failures: []stepFailure{
			{Description: "compile the package", Agent: "coder", Error: "undefined: foo"},
		},
		PriorDraft: &PlanDraft{Markdown: "# Plan: demo\n\n## Phases\n", Version: 2},
	})
	if err != nil {
		t.Fatalf("BuildPlanCritiqueInput failed: %v", err)
	}

	if len(input.PriorReviewVerdicts) != 1 {
		t.Fatalf("want 1 prior verdict, got %d", len(input.PriorReviewVerdicts))
	}
	if input.ValidTools == nil || !input.ValidTools["shell"] || !input.ValidTools["file_read"] {
		t.Fatalf("ValidTools not populated from registry: %+v", input.ValidTools)
	}
	if input.FailureBlock == "" || !strings.Contains(input.FailureBlock, "## Previous attempt failed") {
		t.Fatalf("FailureBlock missing or not the 2af298b1 format: %q", input.FailureBlock)
	}
	if input.PriorDraft == nil {
		t.Fatalf("PriorDraft lost in assembly")
	}

	rendered := input.Render()
	for _, want := range []string{
		"## Prior review verdicts",
		"implement the parser",
		"missed the edge cases",
		"## Previous attempt failed",
		"undefined: foo",
		"## Valid tools",
		"file_read, file_write, shell",
		"## Prior draft",
		"# Plan: demo",
	} {
		if !strings.Contains(rendered, want) {
			t.Fatalf("rendered prompt missing %q:\n%s", want, rendered)
		}
	}
}

// TestBuildCritiqueInput_EmptySourcesOmitSections pins: with no sources at
// all the render is empty — no section headers, no "null", no empty-list
// noise — and assembly still succeeds (nil-safe stores).
func TestBuildCritiqueInput_EmptySourcesOmitSections(t *testing.T) {
	_, stepStore := newTestTaskAndStepStore(t)

	input, err := BuildPlanCritiqueInput(CritiqueInputSources{
		StepStore: stepStore, // live store, but empty session
		SessionID: "sess-nothing",
	})
	if err != nil {
		t.Fatalf("BuildPlanCritiqueInput with empty sources must not error: %v", err)
	}
	rendered := input.Render()
	if rendered != "" {
		t.Fatalf("empty input should render nothing, got:\n%s", rendered)
	}
	for _, noise := range []string{"null", "[]", "none", "## "} {
		if strings.Contains(rendered, noise) {
			t.Fatalf("rendered prompt contains %q noise:\n%s", noise, rendered)
		}
	}

	// All-nil sources degrade the same way.
	input2, err := BuildPlanCritiqueInput(CritiqueInputSources{})
	if err != nil {
		t.Fatalf("all-nil sources must not error: %v", err)
	}
	if input2.Render() != "" {
		t.Fatalf("all-nil input should render nothing, got:\n%s", input2.Render())
	}
}

// TestBuildCritiqueInput_NilStepStoreZeroVerdicts pins the leaf's exact
// nil-safe shape: no review store → zero verdicts, no error.
func TestBuildCritiqueInput_NilStepStoreZeroVerdicts(t *testing.T) {
	input, err := BuildPlanCritiqueInput(CritiqueInputSources{
		StepStore: nil,
		SessionID: "sess-x",
	})
	if err != nil {
		t.Fatalf("nil step store must not error: %v", err)
	}
	if len(input.PriorReviewVerdicts) != 0 {
		t.Fatalf("nil store must yield zero verdicts, got %d", len(input.PriorReviewVerdicts))
	}
}

// TestBuildCritiqueInput_FailureBlockBounded pins the ≤2000 bound on the
// assembled failure block, even for many oversized failures.
func TestBuildCritiqueInput_FailureBlockBounded(t *testing.T) {
	failures := make([]stepFailure, 0, 20)
	for i := 0; i < 20; i++ {
		failures = append(failures, stepFailure{
			Description: "step that failed catastrophically",
			Agent:       "coder",
			Error:       strings.Repeat("e", failureStepErrorMaxChars),
		})
	}
	input, err := BuildPlanCritiqueInput(CritiqueInputSources{Failures: failures})
	if err != nil {
		t.Fatalf("assembly failed: %v", err)
	}
	if got := len([]rune(input.FailureBlock)); got > critiqueFailureBlockMaxChars {
		t.Fatalf("failure block %d chars > bound %d", got, critiqueFailureBlockMaxChars)
	}
	if input.FailureBlock == "" {
		t.Fatalf("failure block should be non-empty")
	}
	// The block must end with the ellipsis marker when clamped.
	if !strings.HasSuffix(input.FailureBlock, "…") {
		t.Fatalf("clamped failure block should end with ellipsis, got %q", tailOf(input.FailureBlock, 40))
	}
}

// tailOf returns the last n runes of s (test helper).
func tailOf(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[len(r)-n:])
}

// ---------------------------------------------------------------------------
// Tool-coverage pre-check pins
// ---------------------------------------------------------------------------

// TestValidateToolHints_UnknownHintBlocksValidPasses pins the synthetic
// blocking objection: unknown draft hints produce one objection each; known
// hints (and conversational role hints) produce none.
func TestValidateToolHints_UnknownHintBlocksValidPasses(t *testing.T) {
	input := &PlanCritiqueInput{
		ValidTools: map[string]bool{"shell": true, "file_read": true},
	}

	// Unknown hint → blocking objection.
	objections := input.ValidateToolHints([]string{"web3_blockchain_mint"})
	if len(objections) != 1 {
		t.Fatalf("want 1 objection for unknown hint, got %d", len(objections))
	}
	if objections[0].Hint != "web3_blockchain_mint" {
		t.Fatalf("objection names wrong hint: %q", objections[0].Hint)
	}
	if !strings.Contains(objections[0].String(), "BLOCKING") {
		t.Fatalf("objection must render as blocking: %q", objections[0].String())
	}

	// Valid hint → no objection.
	if got := input.ValidateToolHints([]string{"shell"}); got != nil {
		t.Fatalf("valid hint must not object, got %+v", got)
	}

	// Mixed batch: only the unknown ones object. "Chat" is a
	// conversational role hint, not a registry tool — exempt.
	got := input.ValidateToolHints([]string{"shell", "hallucinated_tool", "file_read", "Chat"})
	if len(got) != 1 || got[0].Hint != "hallucinated_tool" {
		t.Fatalf("mixed batch objections wrong: %+v", got)
	}

	// Empty ValidTools disables the check (nil registry → no coverage
	// information → never block everything).
	empty := &PlanCritiqueInput{}
	if got := empty.ValidateToolHints([]string{"anything"}); got != nil {
		t.Fatalf("empty ValidTools must disable coverage check, got %+v", got)
	}
}

// TestValidateToolHints_MatchesRegistryWiring pins the leaf's "same source"
// rule end to end: ValidTools built from tools.Registry.Names() through
// BuildPlanCritiqueInput accepts exactly the registered names.
func TestValidateToolHints_MatchesRegistryWiring(t *testing.T) {
	reg := newCritiqueToolRegistry(t, "shell", "file_read", "file_write")
	input, err := BuildPlanCritiqueInput(CritiqueInputSources{Registry: reg})
	if err != nil {
		t.Fatalf("assembly failed: %v", err)
	}
	if got := input.ValidateToolHints([]string{"shell", "file_read", "file_write"}); got != nil {
		t.Fatalf("registered names must all pass: %+v", got)
	}
	if got := input.ValidateToolHints([]string{"file_delete_forever"}); len(got) != 1 {
		t.Fatalf("unregistered name must object, got %+v", got)
	}
}

// TestCritiqueInput_RenderNilSafe pins receiver-level nil safety of Render
// and ValidateToolHints.
func TestCritiqueInput_RenderNilSafe(t *testing.T) {
	var nilInput *PlanCritiqueInput
	if got := nilInput.Render(); got != "" {
		t.Fatalf("nil Render must be empty, got %q", got)
	}
	if got := nilInput.ValidateToolHints([]string{"x"}); got != nil {
		t.Fatalf("nil ValidateToolHints must be silent, got %+v", got)
	}
	// A PriorDraft with empty markdown must not emit an empty section.
	in := &PlanCritiqueInput{PriorDraft: &PlanDraft{Markdown: "   "}}
	if got := in.Render(); got != "" {
		t.Fatalf("blank prior draft must omit its section, got %q", got)
	}
}
