package employee

// Tree 03 leaf 03 tests: goal-loop provider-wait parking (D9).
//
// Coverage matrix (goal_loop reflector site):
//   - QuotaResetError            → parked at resetAt, no error surfaces
//   - ErrAllModelsQuotaBlocked   → parked at the quota plan's first step
//   - ThrottleBackoffError       → parked at the plan schedule
//   - give-up (horizon exceeded) → ORIGINAL error propagates unchanged
//   - nil parker (not wired)     → legacy error behaviour unchanged

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/agent"
	"github.com/caimlas/meept/internal/bot"
	"github.com/caimlas/meept/internal/llm"
)

// parkTestPolicy is the failure-policy config used by park tests. The
// horizon (2h) is large enough for the park waits (<=45m) to schedule
// while the give-up tests (48h resets) still exceed it.
func parkTestPolicy() llm.FailurePolicyConfig {
	return llm.FailurePolicyConfig{
		Horizon:           2 * time.Hour,
		BaseThrottle:      30 * time.Second,
		BaseQuota402Extra: 5 * time.Minute,
		PollFloor:         time.Hour,
	}
}

// decodeParked is a test helper that pulls the single parked record out
// of the shared parker via ResumeGoalEpisode-observable state. Because
// TurnParker has no accessor for raw records, tests capture the record
// through a resume callback parker instead.
func newCapturingParker(t *testing.T, policy llm.FailurePolicyConfig) (*EpisodeParker, *[]agent.ParkedTurnRecord) {
	t.Helper()
	captured := &[]agent.ParkedTurnRecord{}
	turns := agent.NewTurnParker(nil, func(_ context.Context, rec agent.ParkedTurnRecord) {
		*captured = append(*captured, rec)
	}, time.Hour)
	parker := NewEpisodeParker(turns, policy, nil)
	return parker, captured
}

// TestAssess_QuotaWaitParksEpisode: a QuotaResetError from the reflector
// during ASSESS parks the episode (tier-2 phase "assess") at the server
// reset time and returns NO error.
func TestAssess_QuotaWaitParksEpisode(t *testing.T) {
	resetAt := time.Now().UTC().Add(45 * time.Minute)
	parker, captured := newCapturingParker(t, parkTestPolicy())
	turns := parker.turns
	reflector := newStubReflector()
	loop := NewGoalLoop("emp-park-q", testTier2Constitution(), nil, nil).
		WithReflector(reflector).
		WithEpisodeParker(parker)
	reflector.queueError(&llm.QuotaResetError{
		ProviderID: "p1",
		ModelID:    "m1",
		Code:       "usage_limit_reached",
		ResetAt:    resetAt,
		MaxWait:    24 * time.Hour,
	})

	candidates, err := loop.Assess(context.Background(), basicTrigger())
	if err != nil {
		t.Fatalf("Assess returned error on quota wait, want nil: %v", err)
	}
	if candidates != nil {
		t.Fatalf("Assess returned candidates on park, want nil: %v", candidates)
	}
	if got := turns.Pending(); got != 1 {
		t.Fatalf("parker pending = %d, want 1", got)
	}
	if len(*captured) != 0 {
		t.Fatalf("resume fired before resume time passed")
	}
	// The scheduled resume time is the server reset (capped by the
	// parker's maxWait, 1h here > 45m, so uncapped). Next() is per-class.
	next, ok := turns.Next(llm.FailureQuota)
	if !ok {
		t.Fatal("no quota-class parked record scheduled")
	}
	if d := next.Sub(resetAt); d < -2*time.Second || d > 2*time.Second {
		t.Fatalf("resume_at = %v, want ~%v (server reset)", next, resetAt)
	}
}

// TestAssess_AllModelsBlockedParksEpisode: ErrAllModelsQuotaBlocked
// parks at the quota plan's first step (base throttle + 402 extra).
func TestAssess_AllModelsBlockedParksEpisode(t *testing.T) {
	policy := parkTestPolicy()
	parker, _ := newCapturingParker(t, policy)
	turns := parker.turns
	reflector := newStubReflector()
	loop := NewGoalLoop("emp-park-b", testTier2Constitution(), nil, nil).
		WithReflector(reflector).
		WithEpisodeParker(parker)
	reflector.queueError(llm.ErrAllModelsQuotaBlocked)

	before := time.Now().UTC()
	_, err := loop.Assess(context.Background(), basicTrigger())
	if err != nil {
		t.Fatalf("Assess returned error on all-models-blocked, want nil: %v", err)
	}
	if got := turns.Pending(); got != 1 {
		t.Fatalf("parker pending = %d, want 1", got)
	}
	want := before.Add(policy.BaseThrottle + policy.BaseQuota402Extra)
	next, ok := turns.Next(llm.FailureQuota)
	if !ok {
		t.Fatal("no quota-class parked record scheduled")
	}
	if d := next.Sub(want); d < -2*time.Second || d > 2*time.Second {
		t.Fatalf("resume_at = %v, want ~%v (plan first step)", next, want)
	}
}

// TestAssess_ThrottleWaitParksEpisode: ThrottleBackoffError parks at the
// carried RetryAt (plan-scheduled by the client's short loop).
func TestAssess_ThrottleWaitParksEpisode(t *testing.T) {
	parker, _ := newCapturingParker(t, parkTestPolicy())
	turns := parker.turns
	reflector := newStubReflector()
	loop := NewGoalLoop("emp-park-t", testTier2Constitution(), nil, nil).
		WithReflector(reflector).
		WithEpisodeParker(parker)
	retryAt := time.Now().UTC().Add(90 * time.Second)
	reflector.queueError(&llm.ThrottleBackoffError{
		ProviderID: "p1",
		ModelID:    "m1",
		RetryAt:    retryAt,
		Attempt:    3,
	})

	_, err := loop.Assess(context.Background(), basicTrigger())
	if err != nil {
		t.Fatalf("Assess returned error on throttle wait, want nil: %v", err)
	}
	if got := turns.Pending(); got != 1 {
		t.Fatalf("parker pending = %d, want 1", got)
	}
	next, ok := turns.Next(llm.FailureThrottle)
	if !ok {
		t.Fatal("no throttle-class parked record scheduled")
	}
	if d := next.Sub(retryAt); d < -2*time.Second || d > 2*time.Second {
		t.Fatalf("resume_at = %v, want ~%v (server RetryAt)", next, retryAt)
	}
}

// TestReflect_QuotaWaitParksEpisode: the REFLECT LLM call hitting a
// quota wall parks instead of failing the reflect step. Reflect returns
// GoalUnknown with no error; the goal store is untouched.
func TestReflect_QuotaWaitParksEpisode(t *testing.T) {
	resetAt := time.Now().UTC().Add(30 * time.Minute)
	parker, _ := newCapturingParker(t, parkTestPolicy())
	turns := parker.turns
	reflector := newStubReflector()
	executor := newStubExecutor()
	loop := NewGoalLoop("emp-park-r", testTier2Constitution(), nil, nil).
		WithReflector(reflector).
		WithExecutor(executor).
		WithEpisodeParker(parker)
	reflector.queueError(&llm.QuotaResetError{
		ProviderID: "p1",
		ModelID:    "m1",
		Code:       "usage_limit_reached",
		ResetAt:    resetAt,
		MaxWait:    24 * time.Hour,
	})

	plan := PlanRef{ID: "plan-1", State: "executing", Prompt: "do the thing", ApproverID: "system"}
	result := okExecResult()
	health, err := loop.Reflect(context.Background(), plan, result)
	if err != nil {
		t.Fatalf("Reflect returned error on quota wait, want nil: %v", err)
	}
	if health != GoalUnknown {
		t.Fatalf("health = %v, want GoalUnknown on park", health)
	}
	if got := turns.Pending(); got != 1 {
		t.Fatalf("parker pending = %d, want 1", got)
	}
	if _, ok := turns.Next(llm.FailureQuota); !ok {
		t.Fatal("no quota-class parked record scheduled")
	}
}

// TestAssess_QuotaWaitGiveUpPropagatesOriginalError: a reset beyond the
// plan horizon is a give-up — the ORIGINAL QuotaResetError propagates
// unchanged (same message and chain, no wrapping).
func TestAssess_QuotaWaitGiveUpPropagatesOriginalError(t *testing.T) {
	policy := parkTestPolicy() // horizon 10m
	parker, _ := newCapturingParker(t, policy)
	turns := parker.turns
	reflector := newStubReflector()
	loop := NewGoalLoop("emp-park-g", testTier2Constitution(), nil, nil).
		WithReflector(reflector).
		WithEpisodeParker(parker)
	quotaErr := &llm.QuotaResetError{
		ProviderID: "p1",
		ModelID:    "m1",
		Code:       "usage_limit_reached",
		ResetAt:    time.Now().UTC().Add(48 * time.Hour), // far beyond horizon
		MaxWait:    72 * time.Hour,
	}
	reflector.queueError(quotaErr)

	_, err := loop.Assess(context.Background(), basicTrigger())
	if err == nil {
		t.Fatal("Assess returned nil on give-up, want the original quota error")
	}
	// Byte-identical give-up: the surfaced error is EXACTLY the legacy
	// Assess wrapping of the original error — same prefix, same cause
	// message, nothing added or reworded.
	wantWrapped := "assess LLM call failed: " + quotaErr.Error()
	if err.Error() != wantWrapped {
		t.Fatalf("give-up error = %q, want legacy byte-identical %q", err.Error(), wantWrapped)
	}
	// Assess wraps non-park errors with the "assess LLM call failed: "
	// prefix; identity of the cause is asserted through the chain.
	var got *llm.QuotaResetError
	if !errors.As(err, &got) {
		t.Fatalf("give-up error chain lost the QuotaResetError: %v", err)
	}
	if got.ResetAt != quotaErr.ResetAt {
		t.Fatalf("reset time changed across give-up: got %v want %v", got.ResetAt, quotaErr.ResetAt)
	}
	if turns.Pending() != 0 {
		t.Fatalf("parker pending = %d, want 0 on give-up", turns.Pending())
	}
}

// TestAssess_ThrottleGiveUpPropagatesOriginalError: RetryAt zero means
// no schedulable wait — give-up with the original error.
func TestAssess_ThrottleGiveUpPropagatesOriginalError(t *testing.T) {
	parker, _ := newCapturingParker(t, parkTestPolicy())
	reflector := newStubReflector()
	loop := NewGoalLoop("emp-park-gt", testTier2Constitution(), nil, nil).
		WithReflector(reflector).
		WithEpisodeParker(parker)
	throttleErr := &llm.ThrottleBackoffError{ProviderID: "p1", ModelID: "m1", Attempt: 5}
	reflector.queueError(throttleErr)

	_, err := loop.Assess(context.Background(), basicTrigger())
	if err == nil {
		t.Fatal("Assess returned nil on throttle give-up, want the original error")
	}
	var got *llm.ThrottleBackoffError
	if !errors.As(err, &got) {
		t.Fatalf("give-up error chain lost the ThrottleBackoffError: %v", err)
	}
	if got.Attempt != 5 {
		t.Fatalf("throttle error mutated across give-up: attempt %d", got.Attempt)
	}
}

// TestAssess_NonProviderErrorUnchanged: ordinary errors (parse failures,
// network junk) are NOT provider waits — they propagate exactly as
// before and nothing parks.
func TestAssess_NonProviderErrorUnchanged(t *testing.T) {
	parker, _ := newCapturingParker(t, parkTestPolicy())
	turns := parker.turns
	reflector := newStubReflector()
	loop := NewGoalLoop("emp-park-n", testTier2Constitution(), nil, nil).
		WithReflector(reflector).
		WithEpisodeParker(parker)
	reflector.queueError(errors.New("connection reset by peer"))

	_, err := loop.Assess(context.Background(), basicTrigger())
	if err == nil {
		t.Fatal("Assess returned nil on a non-provider error")
	}
	if !strings.Contains(err.Error(), "assess LLM call failed") {
		t.Fatalf("non-provider error lost its legacy wrapping: %q", err.Error())
	}
	if turns.Pending() != 0 {
		t.Fatalf("parker pending = %d, want 0 (non-provider error must not park)", turns.Pending())
	}
}

// TestAssess_NilParkerLegacyBehaviour: no parker wired → provider waits
// keep the exact legacy behaviour (wrapped error, nothing parked).
func TestAssess_NilParkerLegacyBehaviour(t *testing.T) {
	reflector := newStubReflector()
	loop := NewGoalLoop("emp-park-nil", testTier2Constitution(), nil, nil).WithReflector(reflector)
	quotaErr := &llm.QuotaResetError{
		ProviderID: "p1", ModelID: "m1", Code: "usage_limit_reached",
		ResetAt: time.Now().UTC().Add(10 * time.Minute), MaxWait: 24 * time.Hour,
	}
	reflector.queueError(quotaErr)

	_, err := loop.Assess(context.Background(), basicTrigger())
	if err == nil || !strings.Contains(err.Error(), "assess LLM call failed") {
		t.Fatalf("nil-parker quota error = %v, want legacy wrapped error", err)
	}
}

// TestResumeGoalEpisode_AssessReenters: the resume callback re-enters
// the parked tier-2 assess phase and this time the reflector succeeds.
func TestResumeGoalEpisode_AssessReenters(t *testing.T) {
	parker, _ := newCapturingParker(t, parkTestPolicy())
	turns := parker.turns
	reflector := newStubReflector()
	loop := NewGoalLoop("emp-park-res", testTier2Constitution(), nil, nil).
		WithReflector(reflector).
		WithEpisodeParker(parker)
	reflector.queueError(&llm.QuotaResetError{
		ProviderID: "p1", ModelID: "m1", Code: "usage_limit_reached",
		ResetAt: time.Now().UTC().Add(30 * time.Minute), MaxWait: 24 * time.Hour,
	})
	if _, err := loop.Assess(context.Background(), basicTrigger()); err != nil {
		t.Fatalf("Assess errored on parkable quota wait: %v", err)
	}
	if turns.Pending() != 1 {
		t.Fatalf("parker pending = %d, want 1", turns.Pending())
	}

	// Resume succeeds: reflector now answers with no candidates.
	reflector.queueResponse(`{"candidates": []}`)
	// The daemon resume callback hands back the exact record the park
	// stored; reconstruct it with the same payload shape Assess uses.
	rec := agent.ParkedTurnRecord{
		ConversationID: loop.employeeID,
		SessionID:      loop.employeeID,
		AgentID:        loop.employeeID,
		Class:          llm.FailureQuota,
		ResumeAt:       time.Now().UTC().Add(30 * time.Minute),
		TurnPayload: mustMarshalGoalPayload(t, goalTurnPayload{
			Phase:   "assess",
			Trigger: triggerPtr(basicTrigger()),
		}),
	}
	loop.ResumeGoalEpisode(context.Background(), rec)
	if reflector.CallCount() < 2 {
		t.Fatalf("resume did not re-enter Assess: reflector calls = %d", reflector.CallCount())
	}
}

// TestResumeGoalEpisode_UnknownPhaseDrops: a corrupted payload phase is
// dropped with an error log, never panics.
func TestResumeGoalEpisode_UnknownPhaseDrops(t *testing.T) {
	parker, _ := newCapturingParker(t, parkTestPolicy())
	reflector := newStubReflector()
	loop := NewGoalLoop("emp-park-x", testTier2Constitution(), nil, nil).
		WithReflector(reflector).
		WithEpisodeParker(parker)
	rec := agent.ParkedTurnRecord{
		AgentID:     loop.employeeID,
		Class:       llm.FailureQuota,
		TurnPayload: mustMarshalGoalPayload(t, goalTurnPayload{Phase: "bogus"}),
	}
	loop.ResumeGoalEpisode(context.Background(), rec) // must not panic
	if reflector.CallCount() != 0 {
		t.Fatalf("unknown phase drove the reflector %d times", reflector.CallCount())
	}
}

// TestGiveUpQuotaResetReflectDefaultsHealthy: a give-up during REFLECT
// keeps the legacy warn-and-default-to-healthy behaviour.
func TestGiveUpQuotaResetReflectDefaultsHealthy(t *testing.T) {
	policy := parkTestPolicy() // horizon 10m
	parker, _ := newCapturingParker(t, policy)
	reflector := newStubReflector()
	executor := newStubExecutor()
	loop := NewGoalLoop("emp-park-gr", testTier2Constitution(), nil, nil).
		WithReflector(reflector).
		WithExecutor(executor).
		WithEpisodeParker(parker)
	reflector.queueError(&llm.QuotaResetError{
		ProviderID: "p1", ModelID: "m1", Code: "usage_limit_reached",
		ResetAt: time.Now().UTC().Add(48 * time.Hour), MaxWait: 72 * time.Hour,
	})
	plan := PlanRef{ID: "plan-2", State: "executing", Prompt: "p", ApproverID: "system"}
	health, err := loop.Reflect(context.Background(), plan, okExecResult())
	if err != nil {
		t.Fatalf("Reflect must not error on reflect-LLM failure: %v", err)
	}
	if health != GoalHealthy {
		t.Fatalf("give-up reflect health = %v, want legacy GoalHealthy", health)
	}
}

// --- tiny helpers ---------------------------------------------------------

func triggerPtr(t TriggerEvent) *TriggerEvent { return &t }

// mustMarshalGoalPayload marshals a payload or fails the test.
func mustMarshalGoalPayload(t *testing.T, p goalTurnPayload) []byte {
	t.Helper()
	raw, err := json.Marshal(p)
	if err != nil {
		t.Fatalf("marshal goalTurnPayload: %v", err)
	}
	return raw
}

// okExecResult returns a successful execution result for Reflect tests.
func okExecResult() *bot.BotExecutionResult {
	return &bot.BotExecutionResult{
		BotID:      "b1",
		Output:     "done",
		TokensUsed: 10,
		Success:    true,
		Duration:   time.Second,
	}
}
