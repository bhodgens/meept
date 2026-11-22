package agent

// 01-safety TEST GATE T11 (00T-gate.md): replan storm ceiling (F5c).
//
// F-NEW-10: a failed task replanned 113x over 30+ minutes. Root cause:
// the "forward progress" cap extension (ralph_loop.go TriggerReplan +
// CheckCompletion) resets iterations to MaxIterations-1 EVERY cycle the
// completed-step count grows -- and each replan adds completed steps, so
// the extension grants are UNBOUNDED. failTaskAtCap never latches: the
// replan-created steps keep the task non-terminal and the storm continues.
//
// 07-replan-storm contract:
//   - hard ceiling: total replans <= MaxIterations + max_replan_extensions
//     (RalphLoopConfig.MaxReplanExtensions, default 2);
//   - a task that hit failTaskAtCap NEVER replans again (terminal latch);
//   - ONE failure terminal: no re-relay under fresh turn ids.
//
// RED today: extensions are unbounded (the storm shape below replans
// far past any ceiling); no latch; repeated task.failed re-relays.

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/bus"
	"github.com/caimlas/meept/internal/task"
	"github.com/caimlas/meept/pkg/models"
)

// gateStormFixture builds a RalphLoop over a temp task+step store with a
// live bus, mirroring newChainStabilityRalphFixture.
func gateStormFixture(t *testing.T) (*RalphLoop, *bus.MessageBus, *task.Store, *task.StepStore) {
	t.Helper()
	rl, mb, ts, ss := newChainStabilityRalphFixture(t)
	return rl, mb, ts, ss
}

// gateAddCompletedStep appends one completed step to the task, growing
// taskProgress exactly like a replan cycle that completes new steps.
func gateAddCompletedStep(t *testing.T, ss *task.StepStore, taskID string, seq int) {
	t.Helper()
	step := task.NewTaskStep(taskID, fmt.Sprintf("storm step %d", seq), seq)
	step.State = task.StepCompleted
	if err := ss.Create(step); err != nil {
		t.Fatalf("create completed step: %v", err)
	}
}

// TestReplanHardCeiling pins the storm shape: a task failing evidence
// checks with a GROWING completed-step count must stop replanning at
// MaxIterations + MaxReplanExtensions, not extend forever.
func TestReplanHardCeiling(t *testing.T) {
	rl, mb, ts, ss := gateStormFixture(t)

	tk := task.NewTask("replan storm ceiling", "storm ceiling gate task with evidence requirements")
	if err := ts.Create(tk); err != nil {
		t.Fatalf("create task: %v", err)
	}

	replanSub := mb.Subscribe("gate-storm-replans", "orchestrator.replan")
	defer mb.Unsubscribe(replanSub)
	failSub := mb.Subscribe("gate-storm-fails", "task.failed")
	defer mb.Unsubscribe(failSub)

	// The contract ceiling: MaxIterations + the NEW bounded extension
	// budget (default 2). Today MaxReplanExtensions does not exist.
	ceiling := rl.config.MaxIterations + rl.config.MaxReplanExtensions

	ctx := context.Background()
	replans := 0
	// Drive far more cycles than any sane ceiling: the storm ran 113.
	for cycle := 0; cycle < 40; cycle++ {
		// Each cycle completes one more step (the growth that grants the
		// unbounded extension today).
		gateAddCompletedStep(t, ss, tk.ID, cycle)

		result, err := json.Marshal(map[string]any{
			"success": true,
			"result":  "done",
			// Must share NO word >3 chars with the task description
			// ("storm ceiling gate task with evidence requirements") or
			// validateEvidence's key-term heuristic marks the evidence
			// sufficient and the task completes instead of storming
			// (gate fixture fix: the original text leaked the word
			// "evidence", colliding with the task's own key terms).
			"evidence": []string{"unrelated gibberish matching nothing here"},
		})
		if err != nil {
			t.Fatalf("marshal result: %v", err)
		}
		isComplete, _, needsReplan := rl.CheckCompletion(ctx, tk.ID, result)
		if isComplete {
			t.Fatalf("cycle %d: task reported complete without valid evidence", cycle)
		}
		if needsReplan {
			if err := rl.TriggerReplan(ctx, tk.ID, nil); err != nil {
				t.Fatalf("cycle %d TriggerReplan: %v", cycle, err)
			}
		}
		// Count replan requests actually published.
		for {
			select {
			case <-replanSub.Channel:
				replans++
				continue
			default:
			}
			break
		}
		// Once the task terminalizes failed, the storm must be over.
		if got, gerr := ts.GetByID(tk.ID); gerr == nil && got.State == task.StateFailed {
			break
		}
	}

	if replans > ceiling {
		t.Errorf("CURRENT DEFECT: %d replans published, ceiling is MaxIterations(%d)+MaxReplanExtensions(%d)=%d; the progress extension grants are UNBOUNDED (F-NEW-10 storm: 113 replans)",
			replans, rl.config.MaxIterations, rl.config.MaxReplanExtensions, ceiling)
	}
	got, err := ts.GetByID(tk.ID)
	if err != nil {
		t.Fatalf("get task: %v", err)
	}
	if got.State != task.StateFailed {
		t.Errorf("task state after the storm = %q, want failed (the ceiling must terminalize)", got.State)
	}
	// Exactly ONE failure event.
	fails := 0
	for {
		select {
		case <-failSub.Channel:
			fails++
			continue
		default:
		}
		break
	}
	if fails != 1 {
		t.Errorf("task.failed published %d times, want exactly 1 (one failure terminal)", fails)
	}
}

// TestFailedTaskNeverReplans pins the terminal latch: once failTaskAtCap
// has failed a task, TriggerReplan must refuse -- even though replan
// cycles keep adding completed steps (the shape that bypassed the
// TaskIsTerminal guard in the live storm).
func TestFailedTaskNeverReplans(t *testing.T) {
	rl, mb, ts, ss := gateStormFixture(t)

	tk := task.NewTask("failed task latch", "latch gate task")
	if err := ts.Create(tk); err != nil {
		t.Fatalf("create task: %v", err)
	}

	replanSub := mb.Subscribe("gate-latch-replans", "orchestrator.replan")
	defer mb.Unsubscribe(replanSub)

	ctx := context.Background()
	// Exhaust the base budget without progress: the cap fires and
	// failTaskAtCap marks the task failed.
	for i := 0; i < rl.config.MaxIterations+1; i++ {
		if err := rl.TriggerReplan(ctx, tk.ID, nil); err != nil {
			t.Fatalf("TriggerReplan %d: %v", i, err)
		}
	}
	got, err := ts.GetByID(tk.ID)
	if err != nil {
		t.Fatalf("get task: %v", err)
	}
	if got.State != task.StateFailed {
		t.Fatalf("task state = %q, want failed after the cap", got.State)
	}

	// Drain the replans published so far.
	for {
		select {
		case <-replanSub.Channel:
			continue
		default:
		}
		break
	}

	// The storm shape: new completed steps appear under the FAILED task
	// (replan-created work finishing late). The latch must hold.
	for i := 0; i < 10; i++ {
		gateAddCompletedStep(t, ss, tk.ID, 100+i)
		if err := rl.TriggerReplan(ctx, tk.ID, nil); err != nil {
			t.Fatalf("post-failure TriggerReplan returned error: %v", err)
		}
	}
	select {
	case msg := <-replanSub.Channel:
		t.Fatalf("CURRENT DEFECT: a terminal-FAILED task replanned again (%s); failTaskAtCap must latch -- no replan ever fires for a failed task", msg.Payload)
	case <-time.After(300 * time.Millisecond):
	}
}

// TestFailureRelayedOnce pins the relay dedupe: a task that failed and
// terminalized must not re-push task.failed under fresh turn ids when
// internal cycles re-report the same failure (43 re-relays in the rerun).
func TestFailureRelayedOnce(t *testing.T) {
	msgBus := bus.New(nil, slogDiscardLogger())
	h := NewChatHandler(nil, nil, msgBus, slogDiscardLogger())
	reg := NewTurnRegistry()
	h.SetTurnRegistry(reg)

	reg.Register("turn-fail-once", "conv-fail-once")
	reg.AttachTask("turn-fail-once", "task-fail-once")

	terminalSub := msgBus.Subscribe("gate-fail-once", "turn.terminal")
	defer msgBus.Unsubscribe(terminalSub)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := h.Start(ctx); err != nil {
		t.Fatalf("handler start: %v", err)
	}
	defer func() {
		if serr := h.Stop(context.Background()); serr != nil {
			t.Logf("handler stop: %v", serr)
		}
	}()

	payload, err := json.Marshal(map[string]any{
		"task_id":         "task-fail-once",
		"name":            "fail once gate",
		"error":           "the evidence check failed",
		"failed_jobs":     1,
		"completed_jobs":  0,
		"total_jobs":      1,
		"linked_sessions": []string{"conv-fail-once"},
	})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	// The first failure relays under the originating turn id.
	msgBus.Publish("task.failed", &models.BusMessage{Type: models.MessageTypeEvent, Topic: "task.failed", Payload: payload})
	first := gateWaitEvent(terminalSub)
	if first == nil {
		t.Fatal("no terminal for the first task.failed")
	}
	var ev TurnTerminalEvent
	if err := json.Unmarshal(first.Payload, &ev); err != nil {
		t.Fatalf("decode terminal: %v", err)
	}
	if ev.Status != "failed" || ev.TurnID != "turn-fail-once" {
		t.Fatalf("first relay = %q under %q, want failed under turn-fail-once", ev.Status, ev.TurnID)
	}

	// Internal cycles re-report the SAME task failure (the storm's
	// re-relay shape). The user must see the failure ONCE.
	for i := 0; i < 3; i++ {
		msgBus.Publish("task.failed", &models.BusMessage{Type: models.MessageTypeEvent, Topic: "task.failed", Payload: payload})
	}
	select {
	case extra := <-terminalSub.Channel:
		var ev2 TurnTerminalEvent
		if err := json.Unmarshal(extra.Payload, &ev2); err != nil {
			t.Fatalf("decode extra terminal: %v", err)
		}
		t.Errorf("CURRENT DEFECT: re-failure of an already-terminal task re-relayed a terminal (turn_id=%q status=%q); the failure must be relayed ONCE, never under fresh turn ids", ev2.TurnID, ev2.Status)
	case <-time.After(500 * time.Millisecond):
	}
}
