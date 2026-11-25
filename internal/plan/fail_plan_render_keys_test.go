package plan

import (
	"encoding/json"
	"log/slog"
	"path/filepath"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/bus"
)

// TestFailPlanPublishesRenderKeys pins bughunt 2026-11-24 M7: the
// plan.failed payload carried only plan_id/session_id/reason, so the TUI
// failure card fell back to 'Plan: <id>' + 'document proposal - no
// executable steps' for a failed multi-phase plan. The payload must carry
// the render keys the cards read: title, phase_count, step_count.
func TestFailPlanPublishesRenderKeys(t *testing.T) {
	ctx := t.Context()
	logger := slog.Default()
	msgBus := bus.New(nil, logger)
	store, err := NewSQLiteStore(filepath.Join(t.TempDir(), "m7-keys.db"), logger)
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	defer store.Close()
	mgr := NewPlanManager(store, msgBus, gatedPlanConfig(), &noopCreator{}, logger)

	p := NewPlan("three-phase doomsday", "desc", "", "", "")
	p.State = StateExecuting
	p.FilePath = filepath.Join(t.TempDir(), "plan.md")
	if err := store.CreatePlan(ctx, p); err != nil {
		t.Fatalf("CreatePlan: %v", err)
	}
	for i := range 3 {
		ph := NewPlanPhase(p.ID, "Phase", i+1, 2)
		ph.TotalSteps = 2
		ph.CompletedSteps = 2
		if err := store.CreatePhase(ctx, ph); err != nil {
			t.Fatalf("CreatePhase: %v", err)
		}
	}

	sub := msgBus.Subscribe("m7-fail-plan-keys", "plan.failed")
	t.Cleanup(func() { msgBus.Unsubscribe(sub) })

	if err := mgr.failPlan(ctx, p.ID); err != nil {
		t.Fatalf("failPlan: %v", err)
	}

	deadline := time.After(5 * time.Second)
	for {
		select {
		case m, ok := <-sub.Channel:
			if !ok {
				t.Fatal("bus closed")
			}
			var payload map[string]any
			if err := json.Unmarshal(m.Payload, &payload); err != nil {
				t.Fatalf("payload not JSON: %v", err)
			}
			if payload["title"] == nil || payload["phase_count"] == nil || payload["step_count"] == nil {
				t.Fatalf("M7: plan.failed missing render keys: %v", payload)
			}
			if payload["title"] != "three-phase doomsday" {
				t.Fatalf("title = %v, want the plan title", payload["title"])
			}
			if nf, _ := payload["phase_count"].(float64); int(nf) != 3 {
				t.Fatalf("phase_count = %v, want 3", payload["phase_count"])
			}
			if nf, _ := payload["step_count"].(float64); int(nf) != 6 {
				t.Fatalf("step_count = %v, want 6", payload["step_count"])
			}
			return
		case <-deadline:
			t.Fatal("no plan.failed event within 5s")
		}
	}
}
