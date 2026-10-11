package employee

import (
	"context"
	"testing"
)

// ---------------------------------------------------------------------------
// Finding #3: tier-3 MaxActivePlans cap exhaustion must not be a silent
// no-op. TriggerResult carries status="skipped" + skip_reason so callers
// (RPC agents.trigger, CLI) can tell a skipped round from a completed one.
// ---------------------------------------------------------------------------

// TestDecide_Tier3_AtCap_ReportsSkipReason pins the GoalLoop-side signal:
// after a Decide round that was short-circuited by the cap, LastDecideSkip
// reports max_active_plans; a normal round clears it.
func TestDecide_Tier3_AtCap_ReportsSkipReason(t *testing.T) {
	store := testGoalStore(t)
	seedBot(t, store, "emp-t3-skip")

	// Pre-seed an active goal already at cap=1.
	g := &Goal{
		ID:         "goal_t3_skip",
		EmployeeID: "emp-t3-skip",
		Title:      "at cap",
		Mandate:    "test tier-3 cap skip signal",
		State:      GoalActive,
		Source:     SourceUser,
	}
	g.SetActivePlan("plan-active")
	g.AddActivePlan("plan-active")
	if err := store.Create(context.Background(), g); err != nil {
		t.Fatalf("Create goal: %v", err)
	}

	c := testTier3Constitution()
	c.MaxActivePlans = 1

	reflector := newStubReflector()
	executor := newStubExecutor()
	loop := NewGoalLoop("emp-t3-skip", c, store, nil).
		WithReflector(reflector).
		WithExecutor(executor)

	if err := loop.Decide(context.Background(), basicTrigger()); err != nil {
		t.Fatalf("Decide should not error when at cap: %v", err)
	}

	got := loop.LastDecideSkip()
	if got == nil {
		t.Fatal("expected a skip signal after a cap-blocked Decide, got nil")
	}
	if got.Reason != DecideSkipMaxActivePlans {
		t.Errorf("skip reason = %q, want %q", got.Reason, DecideSkipMaxActivePlans)
	}
	if got.ActiveCount != 1 || got.Max != 1 {
		t.Errorf("skip detail: active_count=%d max=%d, want 1/1", got.ActiveCount, got.Max)
	}

	// The blocked round must not have assessed (reflector untouched).
	if reflector.CallCount() != 0 {
		t.Errorf("reflector called %d times, want 0 (ASSESS blocked at cap)", reflector.CallCount())
	}
}

// TestDecide_Tier3_NormalRound_ClearsSkipSignal pins the clearing side: a
// round that runs normally leaves no skip signal behind.
func TestDecide_Tier3_NormalRound_ClearsSkipSignal(t *testing.T) {
	reflector := newStubReflector()
	reflector.queueResponse(`{"candidates":[{"title":"auto fix","description":"d","prompt":"fix it"}]}`)
	reflector.queueResponse(`{"health":"healthy","reasoning":"ok"}`)
	executor := newStubExecutor()
	executor.succeedWith("done", 10)

	loop := NewGoalLoop("emp-t3-clear", testTier3Constitution(), nil, nil).
		WithReflector(reflector).
		WithExecutor(executor)

	if err := loop.Decide(context.Background(), basicTrigger()); err != nil {
		t.Fatalf("Decide error: %v", err)
	}
	if got := loop.LastDecideSkip(); got != nil {
		t.Errorf("expected no skip signal on a normal round, got %+v", got)
	}
}

// TestTriggerViaGoalLoop_SkippedAtCap pins the Manager-side contract: when
// the round was cap-blocked, TriggerResult.Status is "skipped" with
// skip_reason="max_active_plans" — NOT "completed" (the old silent no-op).
func TestTriggerViaGoalLoop_SkippedAtCap(t *testing.T) {
	store := testGoalStore(t)
	seedBot(t, store, "emp-trig-skip")

	g := &Goal{
		ID:         "goal_trig_skip",
		EmployeeID: "emp-trig-skip",
		Title:      "at cap",
		Mandate:    "trigger-level skip signal",
		State:      GoalActive,
		Source:     SourceUser,
	}
	g.SetActivePlan("plan-active")
	g.AddActivePlan("plan-active")
	if err := store.Create(context.Background(), g); err != nil {
		t.Fatalf("Create goal: %v", err)
	}

	c := testTier3Constitution()
	c.MaxActivePlans = 1

	loop := NewGoalLoop("emp-trig-skip", c, store, nil).
		WithReflector(newStubReflector()).
		WithExecutor(newStubExecutor())

	m := NewManager(nil)
	result, err := m.triggerViaGoalLoop(
		context.Background(),
		"emp-trig-skip",
		map[string]any{"source": "manual"},
		"tier_3_autonomous",
		loop,
	)
	if err != nil {
		t.Fatalf("triggerViaGoalLoop error: %v", err)
	}
	if result.Status != "skipped" {
		t.Errorf("status = %q, want %q (cap exhaustion is not a completed round)", result.Status, "skipped")
	}
	if result.SkipReason != "max_active_plans" {
		t.Errorf("skip_reason = %q, want %q", result.SkipReason, "max_active_plans")
	}
}

// TestTriggerViaGoalLoop_CompletedUnchanged guards backward compatibility:
// a normal round still returns status "completed" with no skip reason.
func TestTriggerViaGoalLoop_CompletedUnchanged(t *testing.T) {
	reflector := newStubReflector()
	// no candidates → valid completed round
	loop := NewGoalLoop("emp-trig-ok", testTier1Constitution(), nil, nil).
		WithReflector(reflector)

	m := NewManager(nil)
	result, err := m.triggerViaGoalLoop(
		context.Background(),
		"emp-trig-ok",
		map[string]any{"source": "manual"},
		"tier_1_reactive",
		loop,
	)
	if err != nil {
		t.Fatalf("triggerViaGoalLoop error: %v", err)
	}
	if result.Status != "completed" {
		t.Errorf("status = %q, want %q", result.Status, "completed")
	}
	if result.SkipReason != "" {
		t.Errorf("skip_reason = %q, want empty", result.SkipReason)
	}
}
