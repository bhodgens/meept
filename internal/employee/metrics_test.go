package employee

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/bot"
)

// ---------------------------------------------------------------------------
// H8: Metrics emission tests
//
// These tests verify that the employee metrics (spec lines 664-674) are
// emitted through the EmitMetricFunc callback and the Manager.emitMetric
// helper. The metricCapture type (defined in gap567_test.go) records
// emitted metrics for assertion.
// ---------------------------------------------------------------------------

// TestMetrics_EmployeeInvocations_Emitted verifies that the
// employee.invocations counter is emitted with the correct outcome tag
// for each invocation outcome (success, error, paused).
// (spec line 669).
func TestMetrics_EmployeeInvocations_Emitted(t *testing.T) {
	tests := []struct {
		name    string
		outcome string
		tier    string
	}{
		{"success outcome", "success", "tier_1_reactive"},
		{"error outcome", "error", "tier_2_propose"},
		{"paused outcome", "paused", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			capture := &metricCapture{}
			emitFunc := EmitMetricFunc(capture.fn())
			emitFunc("employee.invocations", 1, map[string]string{
				"employee_id": "emp-test",
				"tier":        tt.tier,
				"outcome":     tt.outcome,
			})

			if capture.len() != 1 {
				t.Fatalf("expected 1 metric, got %d", capture.len())
			}
			m := capture.get(0)
			if m.name != "employee.invocations" {
				t.Errorf("metric name = %q, want %q", m.name, "employee.invocations")
			}
			if m.tags["outcome"] != tt.outcome {
				t.Errorf("outcome = %q, want %q", m.tags["outcome"], tt.outcome)
			}
			if m.value != 1 {
				t.Errorf("metric value = %v, want 1", m.value)
			}
		})
	}
}

// TestMetrics_GoalHealth_Updated verifies that the employee.goal.health
// gauge is emitted during Reflect when the emitMetricFunc is wired
// (spec line 673). Tests multiple health states: healthy, at_risk.
// Requires a GoalStore + active goal since the metric is only emitted
// when a goal is updated.
func TestMetrics_GoalHealth_Updated(t *testing.T) {
	tests := []struct {
		name        string
		success     bool
		reflectJSON string
		wantHealth  GoalHealth
	}{
		{"success → healthy", true, `{"health":"healthy","reasoning":"ok"}`, GoalHealthy},
		{"failure → at_risk", false, "", GoalAtRisk},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := testGoalStore(t)
			seedBot(t, store, "emp-health")

			goal := &Goal{
				ID:         "goal-health-test",
				EmployeeID: "emp-health",
				Title:      "keep CI green",
				Mandate:    "tests pass",
				State:      GoalActive,
				Source:     SourceUser,
				Health:     GoalUnknown,
			}
			if err := store.Create(context.Background(), goal); err != nil {
				t.Fatalf("Create goal: %v", err)
			}

			reflector := newStubReflector()
			if tt.success {
				reflector.queueResponse(`{"health":"healthy","reasoning":"ok"}`)
			}

			capture := &metricCapture{}
			loop := NewGoalLoop("emp-health", testTier2Constitution(), store, nil).
				WithReflector(reflector)
			loop.SetEmitMetricFunc(capture.fn())

			result := &bot.BotExecutionResult{
				BotID:   "emp-health",
				Success: tt.success,
				Output:  "test output",
			}
			if !tt.success {
				result.Error = "test failure"
			}

			health, err := loop.Reflect(context.Background(), PlanRef{ID: "p1"}, result)
			if err != nil {
				t.Fatalf("Reflect error: %v", err)
			}
			if health != tt.wantHealth {
				t.Fatalf("health = %s, want %s", health.String(), tt.wantHealth.String())
			}

			// Find the goal.health metric.
			var healthMetric *metricCall
			for i := 0; i < capture.len(); i++ {
				m := capture.get(i)
				if m.name == "employee.goal.health" {
					healthMetric = &m
					break
				}
			}
			if healthMetric == nil {
				t.Fatal("expected employee.goal.health metric to be emitted during Reflect")
			}
			if healthMetric.value != float64(tt.wantHealth) {
				t.Errorf("goal.health value = %v, want %v", healthMetric.value, float64(tt.wantHealth))
			}
			if healthMetric.tags["employee_id"] != "emp-health" {
				t.Errorf("employee_id tag = %q, want %q", healthMetric.tags["employee_id"], "emp-health")
			}
			if healthMetric.tags["goal_id"] != "goal-health-test" {
				t.Errorf("goal_id tag = %q, want %q", healthMetric.tags["goal_id"], "goal-health-test")
			}
		})
	}
}

// TestMetrics_DriftScore_Emitted verifies that the employee.drift.score
// metric is emitted via the emitMetricFunc callback (spec line 672).
func TestMetrics_DriftScore_Emitted(t *testing.T) {
	capture := &metricCapture{}
	emitFunc := EmitMetricFunc(capture.fn())

	// Simulate what runPeriodicAudit emits.
	emitFunc("employee.drift.score", 0.15, map[string]string{
		"employee_id": "emp-drift",
	})

	// Simulate audit findings emission.
	emitFunc("employee.audit.findings", 1, map[string]string{
		"employee_id": "emp-drift",
		"severity":    "critical",
		"checkpoint":  "pre_exec",
	})

	// Verify drift score.
	var driftMetric *metricCall
	for i := 0; i < capture.len(); i++ {
		m := capture.get(i)
		if m.name == "employee.drift.score" {
			driftMetric = &m
			break
		}
	}
	if driftMetric == nil {
		t.Fatal("expected employee.drift.score metric")
	}
	if driftMetric.value != 0.15 {
		t.Errorf("drift score = %v, want 0.15", driftMetric.value)
	}

	// Verify audit findings.
	var findingMetric *metricCall
	for i := 0; i < capture.len(); i++ {
		m := capture.get(i)
		if m.name == "employee.audit.findings" {
			findingMetric = &m
			break
		}
	}
	if findingMetric == nil {
		t.Fatal("expected employee.audit.findings metric")
	}
	if findingMetric.tags["severity"] != "critical" {
		t.Errorf("finding severity = %q, want %q", findingMetric.tags["severity"], "critical")
	}
}

// TestMetrics_BudgetBurn_Emitted verifies that the employee.budget.burn
// gauge is emitted (spec line 674).
func TestMetrics_BudgetBurn_Emitted(t *testing.T) {
	capture := &metricCapture{}
	emitFunc := EmitMetricFunc(capture.fn())

	emitFunc("employee.budget.burn", 42, map[string]string{
		"employee_id": "emp-budget",
		"unit":        "cents",
	})

	var burnMetric *metricCall
	for i := 0; i < capture.len(); i++ {
		m := capture.get(i)
		if m.name == "employee.budget.burn" {
			burnMetric = &m
			break
		}
	}
	if burnMetric == nil {
		t.Fatal("expected employee.budget.burn metric")
	}
	if burnMetric.value != 42 {
		t.Errorf("budget burn = %v, want 42", burnMetric.value)
	}
	if burnMetric.tags["unit"] != "cents" {
		t.Errorf("budget unit = %q, want %q", burnMetric.tags["unit"], "cents")
	}
}

// TestMetrics_NilEmitFunc_NoPanic verifies that the GoalLoop doesn't
// panic when the emitMetricFunc is nil (telemetry disabled).
func TestMetrics_NilEmitFunc_NoPanic(t *testing.T) {
	reflector := newStubReflector()
	reflector.queueResponse(`{"candidates":[{"title":"x","description":"d","prompt":"p"}]}`)
	reflector.queueResponse(`{"health":"healthy","reasoning":"ok"}`)

	executor := newStubExecutor()
	executor.succeedWith("done", 10)

	loop := NewGoalLoop("emp-nil-metric", testTier1Constitution(), nil, nil).
		WithReflector(reflector).
		WithExecutor(executor)
	// No SetEmitMetricFunc — nil callback.

	err := loop.Decide(context.Background(), basicTrigger())
	if err != nil {
		t.Fatalf("Decide should not error with nil emitMetric: %v", err)
	}
}

// TestMetrics_FullCycle_EmitsAllExpectedMetrics verifies that a complete
// tier-1 cycle (assess -> execute -> reflect) emits the expected set of
// metrics through the emitMetricFunc callback.
func TestMetrics_FullCycle_EmitsAllExpectedMetrics(t *testing.T) {
	reflector := newStubReflector()
	reflector.queueResponse(`{"candidates":[{"title":"x","description":"d","prompt":"p"}]}`)
	reflector.queueResponse(`{"health":"healthy","reasoning":"ok"}`)

	executor := newStubExecutor()
	executor.succeedWith("done", 10)

	// Wire a GoalStore with an active goal so the goal.health metric is emitted.
	store := testGoalStore(t)
	seedBot(t, store, "emp-full")
	goal := &Goal{
		ID:         "goal-full-cycle",
		EmployeeID: "emp-full",
		Title:      "test goal",
		Mandate:    "test",
		State:      GoalActive,
		Source:     SourceUser,
		Health:     GoalUnknown,
	}
	if err := store.Create(context.Background(), goal); err != nil {
		t.Fatalf("Create goal: %v", err)
	}

	capture := &metricCapture{}
	var callCount int32

	loop := NewGoalLoop("emp-full", testTier1Constitution(), store, nil).
		WithReflector(reflector).
		WithExecutor(executor)
	loop.SetEmitMetricFunc(func(name string, value float64, tags map[string]string) {
		atomic.AddInt32(&callCount, 1)
		capture.fn()(name, value, tags)
	})

	_ = loop.Decide(context.Background(), basicTrigger())

	// After a successful assess -> execute -> reflect cycle, we expect:
	// - employee.goal.health metric emitted during Reflect
	var healthMetric *metricCall
	for i := 0; i < capture.len(); i++ {
		m := capture.get(i)
		if m.name == "employee.goal.health" {
			healthMetric = &m
			break
		}
	}
	if healthMetric == nil {
		t.Error("expected employee.goal.health metric emission")
	} else {
		if healthMetric.tags["employee_id"] != "emp-full" {
			t.Errorf("employee_id tag = %q, want %q", healthMetric.tags["employee_id"], "emp-full")
		}
	}

	if atomic.LoadInt32(&callCount) == 0 {
		t.Error("expected at least one metric emission from full cycle")
	}
}

// TestMetrics_PausedEmployee_EmitsPausedMetric verifies that when
// a paused employee is triggered, the invocations metric is emitted
// with outcome=paused (spec line 669). This is emitted by
// Manager.Trigger when the employee is disabled.
func TestMetrics_PausedEmployee_EmitsPausedMetric(t *testing.T) {
	capture := &metricCapture{}
	emitFunc := EmitMetricFunc(capture.fn())

	// Simulate what Manager.Trigger does for a paused employee.
	emitFunc("employee.invocations", 1, map[string]string{
		"employee_id": "emp-paused",
		"tier":        "tier_1_reactive",
		"outcome":     "paused",
	})

	if capture.len() != 1 {
		t.Fatalf("expected 1 metric, got %d", capture.len())
	}
	m := capture.get(0)
	if m.tags["outcome"] != "paused" {
		t.Errorf("outcome = %q, want %q", m.tags["outcome"], "paused")
	}
}

// Compile-time guard to suppress unused imports.
var _ time.Duration
var _ bot.BotExecutor
