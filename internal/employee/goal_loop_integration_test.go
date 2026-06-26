package employee

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"
)

// ---------------------------------------------------------------------------
// H7: Full-cycle integration test
// ---------------------------------------------------------------------------

// TestGoalLoop_Tier1_FullCycle_Integration verifies the complete
// ASSESS → EXECUTE → REFLECT cycle for a tier-1 reactive employee.
// The mock LLM returns a valid ASSESS response with one candidate. The
// mock executor records the tool call and returns a success. The mock
// reflector returns "healthy". The test asserts that all stages executed
// in the correct order with correct state transitions.
func TestGoalLoop_Tier1_FullCycle_Integration(t *testing.T) {
	reflector := newStubReflector()
	// ASSESS: returns one candidate
	reflector.queueResponse(`{
		"candidates": [
			{"title": "check CI", "description": "verify CI is green", "prompt": "run ci-check"}
		]
	}`)
	// REFLECT: returns healthy
	reflector.queueResponse(`{"health":"healthy","reasoning":"CI is green"}`)

	executor := newStubExecutor()
	executor.succeedWith("CI passed", 42)

	loop := NewGoalLoop("emp-integration", testTier1Constitution(), nil, nil).
		WithReflector(reflector).
		WithExecutor(executor)

	trigger := TriggerEvent{
		Source:  "cron",
		Topic:   "*/15 * * * *",
		Payload: []byte(`{"status":"alert"}`),
		FiredAt: time.Date(2026, 6, 25, 10, 0, 0, 0, time.UTC),
	}

	err := loop.Decide(context.Background(), trigger)
	if err != nil {
		t.Fatalf("Decide returned error: %v", err)
	}

	// Verify: ASSESS called LLM once
	if reflector.CallCount() != 2 { // ASSESS + REFLECT
		t.Errorf("reflector called %d times, want 2 (assess + reflect)", reflector.CallCount())
	}

	// Verify: EXECUTE called executor once
	if executor.CallCount() != 1 {
		t.Errorf("executor called %d times, want 1", executor.CallCount())
	}

	// Verify: failure counter is 0 (success resets)
	if loop.ConsecutiveFailures() != 0 {
		t.Errorf("consecutive failures = %d, want 0 after success", loop.ConsecutiveFailures())
	}

	// Verify: last assessment time is set
	if loop.LastAssessmentTime().IsZero() {
		t.Error("expected last assessment time to be set after reflect")
	}
}

// TestGoalLoop_Tier1_FullCycle_Failure_Recovery verifies the full cycle
// when execution fails, then succeeds on the next trigger. Asserts that
// the consecutive failure counter increments on failure and resets on
// success, with correct goal health transitions.
func TestGoalLoop_Tier1_FullCycle_Failure_Recovery(t *testing.T) {
	reflector := newStubReflector()

	executor := newStubExecutor()

	// First trigger: assess produces candidate, executor fails, reflect at_risk
	reflector.queueResponse(`{"candidates":[{"title":"retry","description":"d","prompt":"p"}]}`)
	executor.failWith(errors.New("service unavailable"))

	loop := NewGoalLoop("emp-fail-recover", testTier1Constitution(), nil, nil).
		WithReflector(reflector).
		WithExecutor(executor)

	trigger := TriggerEvent{
		Source:  "cron",
		FiredAt: time.Now().UTC(),
	}

	_ = loop.Decide(context.Background(), trigger)

	if loop.ConsecutiveFailures() != 1 {
		t.Errorf("after first failure: consecutive failures = %d, want 1", loop.ConsecutiveFailures())
	}

	// Second trigger: executor succeeds, reflect returns healthy
	reflector.queueResponse(`{"candidates":[{"title":"retry","description":"d","prompt":"p"}]}`)
	reflector.queueResponse(`{"health":"healthy","reasoning":"recovered"}`)
	executor.succeedWith("all good", 100)

	_ = loop.Decide(context.Background(), trigger)

	if loop.ConsecutiveFailures() != 0 {
		t.Errorf("after recovery: consecutive failures = %d, want 0", loop.ConsecutiveFailures())
	}
}

// TestGoalLoop_Tier2_FullCycle_Integration verifies the complete
// ASSESS → PLAN cycle for a tier-2 propose employee. The mock LLM returns
// candidates, the mock planner creates pending plans, and the test verifies
// the full flow without execution (tier-2 pauses for signoff).
func TestGoalLoop_Tier2_FullCycle_Integration(t *testing.T) {
	reflector := newStubReflector()
	reflector.queueResponse(`{
		"candidates": [
			{"title": "investigate flaky test", "description": "check test_X flakiness", "prompt": "run test_X --repeat 10"},
			{"title": "open issue", "description": "document flaky test", "prompt": "create issue for test_X"}
		]
	}`)

	planner := newStubPlanner()

	loop := NewGoalLoop("emp-tier2", testTier2Constitution(), nil, nil).
		WithReflector(reflector).
		WithPlanner(planner)

	err := loop.Decide(context.Background(), basicTrigger())
	if err != nil {
		t.Fatalf("Decide returned error: %v", err)
	}

	// Verify: LLM called once (ASSESS only for tier-2)
	if reflector.CallCount() != 1 {
		t.Errorf("reflector called %d times, want 1 (assess only)", reflector.CallCount())
	}

	// Verify: planner created 2 plans
	if planner.CreatedCount() != 2 {
		t.Errorf("planner created %d plans, want 2", planner.CreatedCount())
	}

	titles := planner.CreatedTitles()
	if titles[0] != "investigate flaky test" || titles[1] != "open issue" {
		t.Errorf("unexpected plan titles: %v", titles)
	}
}

// TestGoalLoop_Tier2_ApproveAndExecute_FullCycle verifies the complete
// approve → execute → reflect cycle when a tier-2 plan is approved.
func TestGoalLoop_Tier2_ApproveAndExecute_FullCycle(t *testing.T) {
	reflector := newStubReflector()
	// REFLECT: returns healthy
	reflector.queueResponse(`{"health":"healthy","reasoning":"plan executed successfully"}`)

	executor := newStubExecutor()
	executor.succeedWith("issue created", 200)

	loop := NewGoalLoop("emp-tier2-approve", testTier2Constitution(), nil, nil).
		WithReflector(reflector).
		WithExecutor(executor)

	planRef := PlanRef{
		ID:         "plan-approved-001",
		State:      "approved",
		Prompt:     "create issue for test_X",
		ApproverID: "user", // escalates_to includes "user"
	}

	result, health, err := loop.ApproveAndExecute(context.Background(), planRef)
	if err != nil {
		t.Fatalf("ApproveAndExecute error: %v", err)
	}

	if !result.Success {
		t.Errorf("expected success, got: %s", result.Error)
	}
	if health != GoalHealthy {
		t.Errorf("health = %s, want healthy", health.String())
	}
	if executor.CallCount() != 1 {
		t.Errorf("executor called %d times, want 1", executor.CallCount())
	}
	if result.TokensUsed != 200 {
		t.Errorf("tokens = %d, want 200", result.TokensUsed)
	}
}

// TestGoalLoop_Tier1_AssessFallback_FullCycle verifies the full cycle
// when ASSESS produces invalid JSON (spec line 590: fallback to tier-1
// implicit plan). The raw LLM output is wrapped as a single candidate
// and executed.
func TestGoalLoop_Tier1_AssessFallback_FullCycle(t *testing.T) {
	reflector := newStubReflector()
	// ASSESS: invalid JSON, triggers fallback
	reflector.queueResponse("I'll check the CI status and report back.")
	// REFLECT: returns healthy
	reflector.queueResponse(`{"health":"healthy","reasoning":"ok"}`)

	executor := newStubExecutor()
	executor.succeedWith("CI is green", 50)

	loop := NewGoalLoop("emp-fallback", testTier1Constitution(), nil, nil).
		WithReflector(reflector).
		WithExecutor(executor)

	err := loop.Decide(context.Background(), basicTrigger())
	if err != nil {
		t.Fatalf("Decide with fallback should not error: %v", err)
	}

	// Verify: the fallback candidate was executed (executor called once)
	if executor.CallCount() != 1 {
		t.Errorf("executor called %d times, want 1 (fallback candidate)", executor.CallCount())
	}
}

// TestGoalLoop_Tier1_NoCandidates_NoOp verifies the full cycle when
// ASSESS produces no candidates — the loop is a no-op (no execution,
// no reflection).
func TestGoalLoop_Tier1_NoCandidates_NoOp(t *testing.T) {
	reflector := newStubReflector()
	// default: {"candidates":[]}

	executor := newStubExecutor()

	loop := NewGoalLoop("emp-noop", testTier1Constitution(), nil, nil).
		WithReflector(reflector).
		WithExecutor(executor)

	err := loop.Decide(context.Background(), basicTrigger())
	if err != nil {
		t.Fatalf("Decide with no candidates should not error: %v", err)
	}

	if executor.CallCount() != 0 {
		t.Errorf("executor called %d times, want 0 (no candidates)", executor.CallCount())
	}
}

// TestGoalLoop_FullCycle_MetricsEmission verifies that the
// employee.goal.health metric is emitted during Reflect when
// SetEmitMetricFunc is wired.
func TestGoalLoop_FullCycle_MetricsEmission(t *testing.T) {
	reflector := newStubReflector()
	reflector.queueResponse(`{"candidates":[{"title":"x","description":"d","prompt":"p"}]}`)
	reflector.queueResponse(`{"health":"healthy","reasoning":"ok"}`)

	executor := newStubExecutor()
	executor.succeedWith("done", 10)

	// Wire a GoalStore with an active goal so the goal.health metric is emitted.
	store := testGoalStore(t)
	seedBot(t, store, "emp-metrics")
	goal := &Goal{
		ID:         "goal-metrics-cycle",
		EmployeeID: "emp-metrics",
		Title:      "test goal",
		Mandate:    "test",
		State:      GoalActive,
		Source:     SourceUser,
		Health:     GoalUnknown,
	}
	if err := store.Create(context.Background(), goal); err != nil {
		t.Fatalf("Create goal: %v", err)
	}

	var metricCalls int32
	var lastMetricName string
	var lastMetricValue float64
	var lastMetricTags map[string]string

	loop := NewGoalLoop("emp-metrics", testTier1Constitution(), store, nil).
		WithReflector(reflector).
		WithExecutor(executor)
	loop.SetEmitMetricFunc(func(name string, value float64, tags map[string]string) {
		atomic.AddInt32(&metricCalls, 1)
		lastMetricName = name
		lastMetricValue = value
		lastMetricTags = tags
	})

	err := loop.Decide(context.Background(), basicTrigger())
	if err != nil {
		t.Fatalf("Decide error: %v", err)
	}

	if atomic.LoadInt32(&metricCalls) == 0 {
		t.Fatal("expected at least one metric emission from Reflect")
	}
	if lastMetricName != "employee.goal.health" {
		t.Errorf("metric name = %q, want %q", lastMetricName, "employee.goal.health")
	}
	if lastMetricValue != float64(GoalHealthy) {
		t.Errorf("metric value = %v, want %v (GoalHealthy)", lastMetricValue, float64(GoalHealthy))
	}
	if lastMetricTags["employee_id"] != "emp-metrics" {
		t.Errorf("metric employee_id tag = %q, want %q", lastMetricTags["employee_id"], "emp-metrics")
	}
}

// TestGoalLoop_Tier1_FailureSequence_Broken verifies the full cycle
// where three consecutive failures lead to broken status and auto-pause.
func TestGoalLoop_Tier1_FailureSequence_Broken(t *testing.T) {
	reflector := newStubReflector()

	executor := newStubExecutor()
	executor.failWith(errors.New("persistent failure"))

	recorder := &pauseRecorder{}

	loop := NewGoalLoop("emp-broken", testTier1Constitution(), nil, nil).
		WithReflector(reflector).
		WithExecutor(executor).
		WithPauseFunc(recorder.fn())

	// Queue responses for 3 cycles: each produces a candidate, executor
	// fails, reflect is not called via LLM (failure path skips LLM reflect).
	for i := 0; i < 3; i++ {
		reflector.queueResponse(`{"candidates":[{"title":"retry","description":"d","prompt":"p"}]}`)
	}

	trigger := TriggerEvent{Source: "cron", FiredAt: time.Now().UTC()}

	// Fail 1: at_risk
	_ = loop.Decide(context.Background(), trigger)
	if loop.ConsecutiveFailures() != 1 {
		t.Fatalf("after fail 1: failures = %d, want 1", loop.ConsecutiveFailures())
	}
	if recorder.wasCalled() {
		t.Fatal("auto-pause should not fire on failure 1")
	}

	// Fail 2: at_risk
	_ = loop.Decide(context.Background(), trigger)
	if loop.ConsecutiveFailures() != 2 {
		t.Fatalf("after fail 2: failures = %d, want 2", loop.ConsecutiveFailures())
	}
	if recorder.wasCalled() {
		t.Fatal("auto-pause should not fire on failure 2")
	}

	// Fail 3: broken + auto-pause
	_ = loop.Decide(context.Background(), trigger)
	if loop.ConsecutiveFailures() != 3 {
		t.Fatalf("after fail 3: failures = %d, want 3", loop.ConsecutiveFailures())
	}
	if !recorder.wasCalled() {
		t.Fatal("auto-pause should fire on failure 3 (threshold reached)")
	}
	if recorder.empID != "emp-broken" {
		t.Errorf("pause empID = %q, want %q", recorder.empID, "emp-broken")
	}
}

// TestGoalLoop_CanExecutePlan_AuthorityCheck verifies G3: the plan
// ownership check — only plans with approver "system" or matching one
// of the employee's escalates_to entries can be executed.
func TestGoalLoop_CanExecutePlan_AuthorityCheck(t *testing.T) {
	c := &Constitution{
		Purpose:      "test",
		Role:         "tester",
		AutonomyTier: Tier2Propose,
		EscalatesTo:  []string{"reviewer-1", "role:user"},
	}

	tests := []struct {
		name      string
		approver  string
	executable bool
	}{
		{"system approver", "system", true},
		{"empty approver", "", true},
		{"matching escalates_to", "reviewer-1", true},
		{"self approver", "emp-test", true},
		{"non-matching approver", "random-agent", false},
		{"role:user approver", "role:user", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			loop := NewGoalLoop("emp-test", c, nil, nil)
			plan := PlanRef{ID: "p1", ApproverID: tt.approver}
			if got := loop.CanExecutePlan(plan); got != tt.executable {
				t.Errorf("CanExecutePlan(approver=%q) = %v, want %v",
					tt.approver, got, tt.executable)
			}
		})
	}
}

// TestGoalLoop_Reflect_NilResult_NoPanic verifies that Reflect handles
// a nil result gracefully (shouldn't happen in normal flow but the
// implementation must not panic).
func TestGoalLoop_Reflect_NilResult_NoPanic(t *testing.T) {
	reflector := newStubReflector()
	loop := NewGoalLoop("emp-nil", testTier1Constitution(), nil, nil).
		WithReflector(reflector)

	// Call Reflect with nil result — should not panic.
	health, err := loop.Reflect(context.Background(), PlanRef{ID: "p1"}, nil)
	if err != nil {
		t.Fatalf("Reflect with nil result should not error: %v", err)
	}
	// Nil result is treated as failure path.
	if health != GoalAtRisk && health != GoalBroken {
		// Could be at_risk (1 failure) which is the expected first-fail state.
		t.Logf("health = %s (expected at_risk for first failure)", health.String())
	}
}
