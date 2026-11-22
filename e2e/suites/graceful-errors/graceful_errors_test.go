//go:build e2e

// Package gracefulerrors is the leaf-06 gate suite of the
// 20261121-interactive-usability tree: no request fails outright.
//
//	graceful-01  empty plan on a non-artifact task degrades to ONE direct
//	             step (degraded plan-quality marker, no planner-sentinel
//	             task.failed) instead of hard-failing
//	graceful-02  a ContextTooSmall verdict renders the user-language
//	             sentence through UserFacingError, never the raw sentinel
//	graceful-03  a quota error with an UNKNOWN schedule still parks with a
//	             conservative synthesized unblockAt (queued note reachable)
package gracefulerrors

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/agent"
	"github.com/caimlas/meept/internal/llm"
	"github.com/caimlas/meept/internal/task"
)

// emptyPlanChatter answers decompose prompts with an EMPTY step list --
// the planner's empty-plan sentinel path.
type emptyPlanChatter struct {
	calls int
}

func (c *emptyPlanChatter) Chat(ctx context.Context, messages []llm.ChatMessage, opts ...llm.ChatOption) (*llm.Response, error) {
	c.calls++
	return &llm.Response{Content: `{"steps": []}`}, nil
}

func (c *emptyPlanChatter) ChatWithProgress(ctx context.Context, messages []llm.ChatMessage, progress llm.ProgressCallback, opts ...llm.ChatOption) (*llm.Response, error) {
	return c.Chat(ctx, messages, opts...)
}

func (c *emptyPlanChatter) Config() *llm.ModelConfig { return nil }

// TestGraceful01_EmptyPlanDegradesToDirectStep (graceful-01): the
// codeword-recall shape -- a trivial non-artifact question whose planner
// answer is an empty plan -- must NOT fail with the planner sentinel. The
// strategic planner degrades to one direct fallback step and the task
// proceeds (scheduled/executing), never failed.
func TestGraceful01_EmptyPlanDegradesToDirectStep(t *testing.T) {
	planner := &emptyPlanChatter{}
	sp, msgBus, failedSub := newGatePlanner(t, planner)
	defer msgBus.Unsubscribe(failedSub)

	tsk := task.NewTask("recall the codeword", "graceful-01 empty plan degrade")
	tsk.ID = "task-graceful-01"
	if err := gateTaskStore(sp).Create(tsk); err != nil {
		t.Fatalf("graceful-01: create task: %v", err)
	}

	err := sp.Plan(context.Background(), agent.PlanRequest{
		TaskID:    tsk.ID,
		SessionID: "sess-graceful-01",
		Input:     "what was the codeword",
		Intent:    string(agent.IntentQuickPlan),
		Mode:      "quick_plan",
	})
	if err != nil {
		t.Fatalf("graceful-01: Plan err = %v, want nil (degrade, not failure)", err)
	}

	// No task.failed carrying the planner sentinel may arrive.
	if reason := lastFailedReason(t, failedSub); reason != "" {
		if containsFold(reason, "planner produced no plan") {
			t.Fatalf("graceful-01: task.failed carried the planner sentinel: %q", reason)
		}
	}

	steps, stepErr := gateStepStore(sp).ListByTaskID(tsk.ID)
	if stepErr != nil {
		t.Fatalf("graceful-01: list steps: %v", stepErr)
	}
	if len(steps) != 1 {
		t.Fatalf("graceful-01: persisted steps = %d, want exactly 1 direct fallback step", len(steps))
	}
	if steps[0].Description != "what was the codeword" {
		t.Errorf("graceful-01: step description = %q, want the input verbatim", steps[0].Description)
	}
	if q := gatePlanQuality(sp, tsk.ID); !containsFold(q, "degraded") {
		t.Errorf("graceful-01: plan_quality = %q, want a degraded marker", q)
	}
	// Exactly one planner call: degrade never re-enters the planner.
	if planner.calls != 1 {
		t.Errorf("graceful-01: planner calls = %d, want 1", planner.calls)
	}

	// The task must not sit in the failed state.
	got, gerr := gateTaskStore(sp).GetByID(tsk.ID)
	if gerr != nil {
		t.Fatalf("graceful-01: get task: %v", gerr)
	}
	if got.State == task.StateFailed {
		t.Errorf("graceful-01: task state = %q, want a non-failed state (degrade)", got.State)
	}
}

// TestGraceful02_ContextTooSmallRendersUserSentence (graceful-02): a
// ContextTooSmall verdict maps to the user-language sentence, never the
// raw sentinel text with provider/model machine ids.
func TestGraceful02_ContextTooSmallRendersUserSentence(t *testing.T) {
	err := &llm.ContextTooSmallError{
		ProviderID:   "llama",
		ModelID:      "lfm-7b",
		EstInput:     40000,
		Reserve:      2000,
		ContextLimit: 32768,
	}
	got := agent.UserFacingError(err)
	if got != "conversation too long for this model; start a new session or ask me to summarize" {
		t.Fatalf("graceful-02: mapped error = %q, want the user-language sentence", got)
	}
	// Machine ids/vocab must not leak: provider name, model id, or the
	// admission verdict's field names. ("model" as a plain English word
	// in the sentence is fine; the ids "llama"/"lfm-7b" are not.)
	for _, vocab := range []string{"provider", "est_input", "context_limit", "llama", "lfm-7b"} {
		if containsFold(got, vocab) {
			t.Errorf("graceful-02: mapped copy leaks machine vocab %q: %q", vocab, got)
		}
	}
}

// TestGraceful03_QuotaUnknownScheduleStillParks (graceful-03): a quota
// reset error with NO ResetAt and NO RetryAfter (unknown schedule) still
// parks -- the conservative synthesized unblockAt (now +
// 3*DefaultQuotaResumePollInterval) keeps the queued-note lane reachable
// instead of dropping the turn on the floor.
func TestGraceful03_QuotaUnknownScheduleStillParks(t *testing.T) {
	var resumed int
	w := agent.NewQuotaResumeWatcher(nil, func(_ context.Context, _ agent.QuotaParkedTurn) {
		resumed++
	}, 24*time.Hour)
	w.SetPollInterval(50 * time.Millisecond)

	quotaErr := &llm.QuotaResetError{
		ProviderID: "p",
		ModelID:    "m",
		Code:       "quota",
		// ResetAt zero, RetryAfter zero: UNKNOWN schedule.
	}
	if !quotaErr.ResetAt.IsZero() {
		t.Fatal("graceful-03 fixture: ResetAt must be zero")
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	w.Start(ctx)

	// The handler branch synthesizes now + 3*DefaultQuotaResumePollInterval
	// when the schedule is unknown; replicate that computation here and
	// prove the watcher accepts the synthesized schedule (the old behavior
	// refused a zero UnblockAt, killing the queued-note lane).
	unblockAt := time.Now().Add(3 * agent.DefaultQuotaResumePollInterval)
	park := agent.QuotaParkedTurn{
		SessionID:      "sess-g3",
		ConversationID: "conv-g3",
		Message:        "hello",
		ProviderID:     "p",
		UnblockAt:      unblockAt,
		TurnID:         "turn-g3",
	}
	if !w.Park(park) {
		t.Fatal("graceful-03: synthesized unblockAt refused -- queued-note lane unreachable")
	}
	if w.Pending() != 1 {
		t.Fatalf("graceful-03: pending = %d, want 1", w.Pending())
	}
}

func containsFold(s, substr string) bool {
	return strings.Contains(strings.ToLower(s), strings.ToLower(substr))
}
