package agent

// Gate tests for master.md contract C3 (serving-model join, stamping half) —
// leaf 00T-capture, task T4 (TJ-STAMP-1, TJ-EXCLUDE-1).
//
// These tests intentionally fail RED until leaf 02-trace-stamping lands:
// there is no step-scoped trace stamp at the step-job boundary yet.
//
// TESTS ONLY: no implementation lives here. The stamp is a ctx channel
// (WithTraceContext / TraceIDFromContext, tracectx.go) — never a loop field.

import (
	"context"
	"testing"

	"github.com/caimlas/meept/internal/llm"
	"github.com/caimlas/meept/internal/task"
)

// traceSniffChatter records the trace id observed on each dispatched call via
// llm.EffectiveTraceID (the same option loop.go stamps from the ctx).
type traceSniffChatter struct {
	traces []string
	resp   *llm.Response
}

func (s *traceSniffChatter) Chat(_ context.Context, _ []llm.ChatMessage, opts ...llm.ChatOption) (*llm.Response, error) {
	s.traces = append(s.traces, llm.EffectiveTraceID(opts))
	if s.resp == nil {
		return &llm.Response{Content: "approved"}, nil
	}
	return s.resp, nil
}

func (s *traceSniffChatter) ChatWithProgress(ctx context.Context, messages []llm.ChatMessage, _ llm.ProgressCallback, opts ...llm.ChatOption) (*llm.Response, error) {
	return s.Chat(ctx, messages, opts...)
}

func (s *traceSniffChatter) Config() *llm.ModelConfig { return &llm.ModelConfig{} }

// TJ-STAMP-1 (00T-capture T4, stamping half): Given a step-job ctx; When the
// loop dispatches LLM calls; Then the call options carry trace "stp-<stepID>",
// derived from WithTraceContext on the step-job ctx (the same request-scoped
// channel the turn mint seam uses). An EMPTY step id must leave the ctx
// unchanged (mirror of the tracectx guard: no scope = no sentinel value).
//
// RED today: nothing stamps "stp-"+stepID anywhere; RunOnce on a step-scoped
// ctx dispatches with trace "" (pre-leaf posture).
func TestTJ_STAMP_1_StepScopedTraceStampsLLMCalls(t *testing.T) {
	chatter := &traceSniffChatter{}
	loop := NewAgentLoop("sess-tj-stamp", t.TempDir(),
		WithLLMChatter(chatter))
	t.Cleanup(loop.Close)

	// The step-job dispatch boundary wraps its ctx with the step-scoped
	// trace before handing the turn to the loop (leaf 02's stamp site).
	ctx := WithTraceContext(context.Background(), "stp-step-tj-1")

	if _, err := loop.RunOnce(ctx, "review nothing, just answer", "conv-tj-stamp"); err != nil {
		t.Fatalf("RunOnce: %v", err)
	}
	if len(chatter.traces) == 0 {
		t.Fatal("the loop dispatched zero LLM calls; the fixture chatter must see at least one call")
	}
	for i, got := range chatter.traces {
		if got != "stp-step-tj-1" {
			t.Errorf("call %d trace = %q, want \"stp-step-tj-1\" (the step-scoped stamp must reach every LLM call via the ctx)", i, got)
		}
	}
}

// TJ-STAMP-1 (empty-step-id guard): an empty trace id returns the ctx
// unchanged — no scope, never a sentinel. This half is a pure channel
// contract and is GREEN today (tracectx.go already guards); it pins the
// behavior leaf 02's stamp site must preserve when the step id is missing.
func TestTJ_STAMP_1_EmptyStepIDLeavesCtxUnchanged(t *testing.T) {
	ctx := context.Background()
	stamped := WithTraceContext(ctx, "")
	if got := TraceIDFromContext(stamped); got != "" {
		t.Errorf("TraceIDFromContext(WithTraceContext(ctx, \"\")) = %q, want \"\" (empty is a no-op)", got)
	}
	var nilCtx context.Context
	if got := TraceIDFromContext(nilCtx); got != "" {
		t.Errorf("TraceIDFromContext(nil) = %q, want \"\"", got)
	}
}

// TJ-EXCLUDE-1 (00T-capture T4, exclusion half): Given the judge loop for
// step S; When its calls dispatch; Then their trace is NOT "stp-S" — the
// reviewer lane runs under its own ctx (turn trace or none), so the judge's
// calls stay excluded from the blame join by agent_id structurally, before
// any post-filtering. The reviewerLoop ctx must never carry the step trace.
//
// RED today: the reviewer lane's ctx carries no step trace, so this half
// passes structurally once it compiles — but it is the regression fence that
// fails leaf 02 the moment the stamp leaks into the judge lane.
func TestTJ_EXCLUDE_1_JudgeLoopCtxCarriesNoStepTrace(t *testing.T) {
	reg := newReviewerWorkdirRegistry(t)
	rm := stepBackedReviewManager(t, reg, stepWorkingDirSessionStore{})

	step := &task.TaskStep{
		ID:       "step-tj-excl",
		TaskID:   "task-tj-excl",
		AgentID:  "coder",
		ToolHint: "code",
		Result:   "changed the thing",
	}
	// 3 steps: isTrivialTask (<3 steps) would skip the LLM reviewer lane.
	seedReviewSteps(t, rm, "task-tj-excl", step,
		&task.TaskStep{ID: "step-tj-excl-b", TaskID: "task-tj-excl"},
		&task.TaskStep{ID: "step-tj-excl-c", TaskID: "task-tj-excl"},
	)

	// The judge loop the review lane resolves — the same task-scoped
	// instance ReviewStep runs its turn on (reviewerLoop, review_manager.go).
	judgeLoop, err := reg.GetForTask("test-reviewer", "task-tj-excl")
	if err != nil {
		t.Fatalf("get reviewer loop: %v", err)
	}

	// The stamp site (leaf 02) wraps the STEP JOB's ctx; the judge lane's
	// turn ctx comes from the ReviewStep caller. Nothing in the judge lane
	// may derive "stp-"+stepID onto its own ctx. Drive the reviewer turn the
	// way ReviewStep does — a plain ctx (no turn trace, no step trace) — and
	// assert the judge's calls carry NO step-scoped trace.
	chatter := &traceSniffChatter{}
	judgeLoop.SetChatter(chatter)

	if _, err := judgeLoop.RunOnce(context.Background(), rm.buildReviewPrompt(step, nil), step.ID); err != nil {
		t.Fatalf("judge RunOnce: %v", err)
	}
	if len(chatter.traces) == 0 {
		t.Fatal("the judge loop dispatched zero LLM calls; the fixture chatter must see at least one call")
	}
	for i, got := range chatter.traces {
		if got == "stp-step-tj-excl" {
			t.Errorf("judge call %d carries the step trace %q: the judge loop's ctx must NOT be step-scoped (C3 exclusion; blame filters by agent_id structurally)", i, got)
		}
	}
}
