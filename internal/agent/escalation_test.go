package agent

import (
	"context"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/task"
)

func TestNewEscalationManager(t *testing.T) {
	cfg := EscalationManagerConfig{
		Config:  DefaultEscalationConfig(),
		Planner: nil, // No planner for basic test
		Bus:     nil,
	}
	em := NewEscalationManager(cfg)
	if em == nil {
		t.Fatal("expected non-nil escalation manager")
	}
}

func TestEscalationManager_Disabled(t *testing.T) {
	cfg := EscalationManagerConfig{
		Config: EscalationConfig{Enabled: false},
	}
	em := NewEscalationManager(cfg)

	failure := FailureContext{
		TaskID:    "task-1",
		StepID:    "step-1",
		AgentID:   "coder",
		Error:     "something failed",
		Timestamp: time.Now(),
	}

	err := em.Escalate(context.Background(), failure)
	if err != nil {
		t.Errorf("expected no error when disabled, got %v", err)
	}
}

func TestEscalationManager_EscalationLevel(t *testing.T) {
	cfg := EscalationManagerConfig{
		Config: EscalationConfig{
			Enabled:             true,
			MaxEscalationLevels: 3,
		},
	}
	em := NewEscalationManager(cfg)

	if level := em.GetEscalationLevel("task-1"); level != 0 {
		t.Errorf("expected initial level 0, got %d", level)
	}

	// Simulate escalation without planner (should reach human intervention)
	failure := FailureContext{
		TaskID:    "task-1",
		StepID:    "step-1",
		AgentID:   "coder",
		Error:     "test failure",
		Timestamp: time.Now(),
	}

	// First escalation
	_ = em.Escalate(context.Background(), failure)
	// Will fail because no planner, but escalation level should increase
	level := em.GetEscalationLevel("task-1")
	if level != 1 {
		t.Errorf("expected level 1 after first escalation, got %d", level)
	}

	// Second escalation
	em.Escalate(context.Background(), failure)
	level = em.GetEscalationLevel("task-1")
	if level != 2 {
		t.Errorf("expected level 2 after second escalation, got %d", level)
	}
}

func TestEscalationManager_ClearEscalation(t *testing.T) {
	cfg := EscalationManagerConfig{
		Config: EscalationConfig{
			Enabled:             true,
			MaxEscalationLevels: 3,
		},
	}
	em := NewEscalationManager(cfg)

	failure := FailureContext{
		TaskID:    "task-1",
		StepID:    "step-1",
		AgentID:   "coder",
		Error:     "test failure",
		Timestamp: time.Now(),
	}

	em.Escalate(context.Background(), failure)
	if level := em.GetEscalationLevel("task-1"); level != 1 {
		t.Errorf("expected level 1, got %d", level)
	}

	em.ClearEscalation("task-1")
	if level := em.GetEscalationLevel("task-1"); level != 0 {
		t.Errorf("expected level 0 after clear, got %d", level)
	}
}

func TestEscalationManager_EscalateForValidation(t *testing.T) {
	cfg := EscalationManagerConfig{
		Config: EscalationConfig{
			Enabled:             true,
			MaxEscalationLevels: 3,
		},
	}
	em := NewEscalationManager(cfg)

	failure := FailureContext{
		TaskID:    "task-1",
		StepID:    "step-1",
		AgentID:   "coder",
		Error:     "validation failed: missing evidence",
		Timestamp: time.Now(),
	}

	em.EscalateForValidation(context.Background(), failure, 3)

	level := em.GetEscalationLevel("task-1")
	if level != 1 {
		t.Errorf("expected level 1 after validation escalation, got %d", level)
	}
}

func TestDefaultEscalationConfig(t *testing.T) {
	cfg := DefaultEscalationConfig()
	if !cfg.Enabled {
		t.Error("expected enabled by default")
	}
	if cfg.MaxEscalationLevels != 3 {
		t.Errorf("expected max escalation levels 3, got %d", cfg.MaxEscalationLevels)
	}
	if cfg.NotificationTopic != "escalation.human_intervention" {
		t.Errorf("expected notification topic, got %s", cfg.NotificationTopic)
	}
}

func TestEscalationManager_MaxLevelsReached(t *testing.T) {
	cfg := EscalationManagerConfig{
		Config: EscalationConfig{
			Enabled:             true,
			MaxEscalationLevels: 2,
		},
	}
	em := NewEscalationManager(cfg)

	failure := FailureContext{
		TaskID:    "task-1",
		StepID:    "step-1",
		AgentID:   "coder",
		Error:     "persistent failure",
		Timestamp: time.Now(),
	}

	// Escalate twice to reach max
	em.Escalate(context.Background(), failure)
	em.Escalate(context.Background(), failure)

	level := em.GetEscalationLevel("task-1")
	if level != 2 {
		t.Errorf("expected level 2, got %d", level)
	}

	// Third escalation should trigger human intervention path
	// (no planner available, so it goes to notifyHumanIntervention)
	em.Escalate(context.Background(), failure)
	// Level should be 3 (max reached)
	level = em.GetEscalationLevel("task-1")
	if level != 3 {
		t.Errorf("expected level 3 at max, got %d", level)
	}
}

func TestReplanFailedTask(t *testing.T) {
	// This tests StrategicPlanner.ReplanFailedTask
	// Since it requires a full planner setup with stores, we test the method exists
	// and handles missing tasks gracefully
	sp := NewStrategicPlanner(StrategicPlannerConfig{
		MaxPlanSteps:   5,
		PlannerTimeout: 30 * time.Second,
	})

	err := sp.ReplanFailedTask(context.Background(), "nonexistent-task", "test error")
	if err == nil {
		t.Error("expected error for nonexistent task")
	}
}

func TestFailureContext(t *testing.T) {
	now := time.Now()
	fc := FailureContext{
		TaskID:       "task-1",
		StepID:       "step-1",
		AgentID:      "coder",
		Error:        "file not found",
		Stage:        "executing",
		RetryCount:   2,
		PartialState: "halfway through",
		Timestamp:    now,
	}

	if fc.TaskID != "task-1" {
		t.Errorf("expected task-1, got %s", fc.TaskID)
	}
	if fc.RetryCount != 2 {
		t.Errorf("expected retry count 2, got %d", fc.RetryCount)
	}
}

func TestEscalationLevel_Tracking(t *testing.T) {
	el := EscalationLevel{
		TaskID:       "task-1",
		Level:        2,
		Reason:       "timeout",
		OriginalTask: "fix the bug",
		Timestamp:    time.Now(),
	}

	if el.Level != 2 {
		t.Errorf("expected level 2, got %d", el.Level)
	}
}

// Verify the task package types are available
func TestEscalationTypesInterop(t *testing.T) {
	// Verify FailureContext has compatible types with task package
	_ = task.StepPending
	_ = task.StatePlanning
}
