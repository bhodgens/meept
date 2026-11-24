package agent

// Leaf 02 gate tests (20261121-interactive-usability, completion honesty):
//   - the trivial-task review heuristic must reject an errored step and
//     must not auto-approve a machine-shaped tool-JSON result,
//   - the unbacked-claim guard must fail the step (not ship a labelled
//     success) when the turn's deliverable is a file effect and zero file
//     tools executed.
//
// Each test is mutation-verified: neutering the guard line flips the test
// RED, restoring it returns GREEN (evidence in the leaf report).

import (
	"context"
	"strings"
	"testing"

	"github.com/caimlas/meept/internal/bus"
	"github.com/caimlas/meept/internal/llm"
	"github.com/caimlas/meept/internal/task"
	"github.com/caimlas/meept/pkg/security"
)

// ---------------------------------------------------------------------------
// Trivial-review heuristic: errored steps never pass
// ---------------------------------------------------------------------------

// TestTrivialReviewRejectsErroredStep pins the review-side half of the
// contract: a trivial (<3 steps) task whose step result carries an error
// must come back ReviewRejected from ReviewStep -- never the heuristic
// approval. (Heuristic-level pin: rm.heuristicReviewPasses alone must also
// refuse, so the gate holds even if the call ordering in ReviewStep ever
// changes.)
func TestTrivialReviewRejectsErroredStep(t *testing.T) {
	rm := newTestReviewManager(t)
	// task-...0007 shape: the guard nudged the unbacked-claim turn, the
	// model's reply was recorded, and the loop's give-up path stamped an
	// error into the result. One step = trivial task.
	step := &task.TaskStep{
		ID:       "step-l02-err",
		TaskID:   "task-l02-err",
		Sequence: 0,
		State:    task.StepCompleted,
		ToolHint: "code",
		Result:   "error: nudge budget exhausted: claimed file changes without executing tools",
	}
	if err := rm.stepStore.Create(step); err != nil {
		t.Fatalf("create step: %v", err)
	}

	// Heuristic-level gate: an errored result never passes.
	if rm.heuristicReviewPasses(step) {
		t.Fatal("heuristicReviewPasses = true for an errored step, want false")
	}

	// Review-level gate: the full ReviewStep path must reject, and the
	// persisted step must be failed -- the "errored steps never pass
	// review" contract at the trivial bypass.
	res, err := rm.ReviewStep(context.Background(), step, nil)
	if err != nil {
		t.Fatalf("ReviewStep: %v", err)
	}
	if res.Status != ReviewRejected {
		t.Fatalf("status = %v, want ReviewRejected", res.Status)
	}
	if !strings.Contains(res.Feedback, "execution error") {
		t.Fatalf("feedback = %q, want it to name the execution error", res.Feedback)
	}
	persisted, err := rm.stepStore.GetByID(step.ID)
	if err != nil {
		t.Fatalf("GetByID: %v", err)
	}
	if persisted.State != task.StepFailed {
		t.Fatalf("persisted state = %v, want StepFailed", persisted.State)
	}
}

// ---------------------------------------------------------------------------
// Trivial-review heuristic: machine-shaped tool-JSON never auto-approves
// ---------------------------------------------------------------------------

// TestTrivialReviewRejectsMachineShapedResult pins the second half of the
// probe-B defect: the model replied with literal tool-call JSON, and the
// trivial-task heuristic approved that dump as a completed answer. The
// heuristic must refuse to auto-approve a machine-shaped result.
func TestTrivialReviewRejectsMachineShapedResult(t *testing.T) {
	rm := newTestReviewManager(t)

	machineShaped := "{\n" +
		"  \"tool_calls\": [\n" +
		"    {\n" +
		"      \"function\": {\n" +
		"        \"name\": \"file_write\",\n" +
		"        \"arguments\": {\"path\": \"hello.txt\", \"content\": \"hi\"}\n" +
		"      }\n" +
		"    }\n" +
		"  ]\n" +
		"}"
	step := &task.TaskStep{
		ID:          "step-l02-json",
		TaskID:      "task-l02-json",
		Sequence:    0,
		State:       task.StepCompleted,
		ToolHint:    "code",
		Description: "create hello.txt with the greeting",
		Result:      machineShaped,
	}
	if err := rm.stepStore.Create(step); err != nil {
		t.Fatalf("create step: %v", err)
	}

	// Detector-level pin: the result classifies machine-shaped.
	if !stepResultIsMachineShaped(step.Result) {
		t.Fatal("stepResultIsMachineShaped = false for raw tool-call JSON, want true")
	}
	// A prose answer quoting a JSON snippet must NOT classify.
	prose := "I checked the plan. The payload {\"name\": \"x\"} is only an example; the actual work is described below in plain sentences."
	if stepResultIsMachineShaped(prose) {
		t.Fatal("stepResultIsMachineShaped = true for prose, want false")
	}
	// A structured step-job envelope (success=true + response prose) must
	// NOT classify -- that is the normal successful shape.
	envelope := `{"success": true, "response": "created hello.txt and verified the bytes on disk."}`
	if stepResultIsMachineShaped(envelope) {
		t.Fatal("stepResultIsMachineShaped = true for a success envelope, want false")
	}
	// Bughunt 2026-11-23: a legitimate SHORT structured answer carrying a
	// bare "name" key is NOT tool-call narration -- the bare-name key used
	// to over-fire and the <40-char machine-shaped result was rejected
	// outright by the ReviewStep escalation path.
	shortAnswer := `{"name": "svc", "version": "2"}`
	if stepResultIsMachineShaped(shortAnswer) {
		t.Fatal("stepResultIsMachineShaped = true for a bare-name structured answer, want false")
	}
	// The unambiguous tool-call narration shapes still classify.
	fnShape := `{"function": {"name": "x", "arguments": {}}}`
	if !stepResultIsMachineShaped(fnShape) {
		t.Fatal("stepResultIsMachineShaped = false for a function/name/arguments shape, want true")
	}
	argsShape := `{"name": "file_write", "arguments": {"path": "x"}}`
	if !stepResultIsMachineShaped(argsShape) {
		t.Fatal("stepResultIsMachineShaped = false for a name+arguments shape, want true")
	}

	// Heuristic-level pin: the machine-shaped result must not ride the
	// trivial bypass to an approval.
	if rm.heuristicReviewPasses(step) {
		t.Fatal("heuristicReviewPasses = true for a machine-shaped tool-JSON result, want false")
	}
}

// ---------------------------------------------------------------------------
// Unbacked-claim guard: bounded nudge, then honest failure
// ---------------------------------------------------------------------------

// nudgeCapChatter returns an unbacked file-effect claim exactly once, then
// claims again (a distinct text so the convergence detector stays quiet).
// With MaxIterations=1 the guard cannot nudge (no budget to retry), so the
// turn lands on the no-iteration-budget path -- the exact shape probe B
// task-...0007 terminated on.
type nudgeCapChatter struct {
	callCount int
}

func (m *nudgeCapChatter) Chat(_ context.Context, _ []llm.ChatMessage, _ ...llm.ChatOption) (*llm.Response, error) {
	m.callCount++
	return &llm.Response{
		Content: "claim: Created file report_" + string(rune('a'+m.callCount)) + ".txt with the full results",
		Usage:   llm.TokenUsage{TotalTokens: 1},
	}, nil
}

func (m *nudgeCapChatter) ChatWithProgress(ctx context.Context, messages []llm.ChatMessage, progress llm.ProgressCallback, opts ...llm.ChatOption) (*llm.Response, error) {
	return m.Chat(ctx, messages, opts...)
}

func (m *nudgeCapChatter) Config() *llm.ModelConfig {
	return &llm.ModelConfig{ModelID: "nudge-cap-chatter"}
}

// TestUnbackedClaimsFailAfterNudge pins the honest-failure posture: a
// file-deliverable turn whose final text claims created files with zero
// file-tool executions must FAIL (ErrNudgeBudgetExhausted with the
// actionable give-up wording), never return a labelled "unverified" reply
// that downstream review can approve as a completed step. The bounded-nudge
// half is pinned by TestNudgeCap_* (at most maxNudgesPerClassPerTurn nudges
// append); this test pins the failure half on the no-budget path.
func TestUnbackedClaimsFailAfterNudge(t *testing.T) {
	loop := NewAgentLoop("test-session", "/tmp",
		WithLLMChatter(&nudgeCapChatter{}),
		WithMessageBus(bus.New(nil, slogDiscardLogger())),
		WithAgentConfig(AgentConfig{
			MaxIterations: 1, // no budget for the nudge's retry iteration
			Guards:        DefaultGuardConfig(),
		}),
	)
	loop.security = security.NewPermissionChecker(security.Config{})

	reply, err := loop.RunOnce(context.Background(), "create the file summary.txt", "conv-l02-unbacked")
	if err == nil {
		t.Fatalf("RunOnce = (%q, nil), want the honest failure for unbacked file claims on a file-deliverable turn", reply)
	}
	if !containsError(err, ErrNudgeBudgetExhausted) {
		t.Fatalf("err = %v, want ErrNudgeBudgetExhausted", err)
	}
	if !strings.Contains(err.Error(), "claimed file changes without executing tools") {
		t.Fatalf("err = %v, want the actionable give-up wording naming the missing executions", err)
	}
}
