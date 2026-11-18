package plan

// Producer-seam pin for the task.step_failed wiring (statemachine-remediation
// follow-on to a94995c8, audit M-6): tactical publishes task.step_failed on a
// step's terminal failure; PlanHandler subscribes and routes it to
// PlanManager.OnStepFailed, the PhaseFailed writer. This test drives the full
// chain on a REAL bus: publish -> subscription -> handler -> manager ->
// failed_steps accounting -> PhaseFailed when the failures reach total_steps.

import (
	"log/slog"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/bus"
	"github.com/caimlas/meept/internal/config"
	"github.com/caimlas/meept/pkg/models"
)

// setupTestManagerWithBus is setupTestManager with a live bus wired onto the
// manager (the nil-bus fixture cannot publish plan.phase_state).
func setupTestManagerWithBus(t *testing.T) (*PlanManager, *bus.MessageBus) {
	t.Helper()
	logger := slog.Default()
	dir := t.TempDir()
	store, err := NewSQLiteStore(filepath.Join(dir, "test.db"), logger)
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}
	t.Cleanup(func() { store.Close() })

	msgBus := bus.New(nil, logger)
	cfg := config.PlansConfig{
		Mode: "threshold",
		Storage: config.PlansStorageConfig{
			DefaultPath:      "docs/plans",
			FilenameTemplate: "{{slug}}.md",
		},
	}
	return NewPlanManager(store, msgBus, cfg, nil, logger), msgBus
}

func TestPlanHandlerStepFailedBusSeam(t *testing.T) {
	ctx := t.Context()
	mgr, msgBus := setupTestManagerWithBus(t)

	// One phase, 3 steps, in_progress, bound to a task id the tactical
	// scheduler would name in its step events.
	p := NewPlan("step-failed-seam", "d", "", "", "")
	p.State = StateExecuting
	if err := mgr.store.CreatePlan(ctx, p); err != nil {
		t.Fatalf("CreatePlan: %v", err)
	}
	ph := NewPlanPhase(p.ID, "Phase A", 1, 3)
	ph.State = PhaseInProgress
	ph.TaskID = "task-seam"
	if err := mgr.store.CreatePhase(ctx, ph); err != nil {
		t.Fatalf("CreatePhase: %v", err)
	}
	mgr.mu.Lock()
	mgr.phaseTaskMap["task-seam"] = ph.ID
	mgr.taskPlanMap["task-seam"] = p.ID
	mgr.mu.Unlock()

	handler := NewPlanHandler(mgr, msgBus, slog.Default())
	if err := handler.Start(ctx); err != nil {
		t.Fatalf("handler Start: %v", err)
	}
	defer handler.Stop()

	// Watch plan.phase_state so the PhaseFailed transition is observable
	// through the event surface as well as the persisted row.
	phaseStateSub := msgBus.Subscribe("step-failed-phase-watch", "plan.phase_state")
	defer msgBus.Unsubscribe(phaseStateSub)

	// Publish the exact tactical payload shape (task.step_failed carries
	// task_id/step_id/state/result; the handler decodes task_id/step_id).
	for i := 0; i < 3; i++ {
		msg, err := models.NewBusMessage(models.MessageTypeEvent, "tactical-scheduler", map[string]any{
			"task_id": "task-seam",
			"step_id": "step-seam",
			"state":   "failed",
			"result":  "step blew up",
		})
		if err != nil {
			t.Fatalf("NewBusMessage: %v", err)
		}
		if delivered := msgBus.Publish("task.step_failed", msg); delivered == 0 {
			t.Fatalf("publish delivered to 0 subscribers — handler subscription for task.step_failed missing")
		}
	}

	// The handler runs on its subscription goroutine; poll the persisted
	// row until BOTH the accounting lands AND the PhaseFailed flip lands.
	// OnStepFailed increments failed_steps first, then flips the phase in a
	// second store write; observing the counter alone is an intermediate
	// state (the pin flaked under full-suite load).
	deadline := time.Now().Add(5 * time.Second)
	var phases []*PlanPhase
	for {
		var err error
		phases, err = mgr.store.GetPhases(ctx, p.ID)
		if err != nil {
			t.Fatalf("GetPhases: %v", err)
		}
		if len(phases) == 1 && phases[0].FailedSteps == 3 &&
			phases[0].State == PhaseFailed {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("phase never reached failed with 3 failed steps via the bus seam (last: %+v)", phases)
		}
		time.Sleep(20 * time.Millisecond)
	}
	if phases[0].State != PhaseFailed {
		t.Errorf("phase state = %s after all steps failed via bus, want %s", phases[0].State, PhaseFailed)
	}

	// The PhaseFailed writer published plan.phase_state — one event per
	// flip, carrying the failed state.
	select {
	case msg, ok := <-phaseStateSub.Channel:
		if !ok {
			t.Fatalf("phase_state subscription closed")
		}
		if msg.Topic != "plan.phase_state" {
			t.Errorf("topic = %q, want plan.phase_state", msg.Topic)
		}
		want := `"state":"failed"`
		if !strings.Contains(string(msg.Payload), want) {
			t.Errorf("phase_state payload %s missing %s", msg.Payload, want)
		}
	case <-time.After(2 * time.Second):
		t.Errorf("no plan.phase_state event after PhaseFailed flip")
	}
}
