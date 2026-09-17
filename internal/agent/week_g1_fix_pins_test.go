package agent

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/bus"
	"github.com/caimlas/meept/internal/llm"
	"github.com/caimlas/meept/internal/task"
	"github.com/caimlas/meept/pkg/models"
)

// ---------------------------------------------------------------------------
// Week bughunt 2026-09-17 Group 1 pins (findings 1, 2, 15, 16, 18 + finding 11)
// ---------------------------------------------------------------------------

// TestWeekG1DispatchAttachment is the repo pin for finding 1 (adapted from the
// parent probe TestWeekParentDispatchAttachment): BOTH dispatch paths (sync
// and async) must attach the created task to the SUBMITTED turn id
// (req.TurnID), so the task-end relay re-broadcasts the real result under the
// id the client's chat.submit ack returned. Pre-fix, async passed the task id
// as the turn key and sync passed an empty turn id, so TurnIDForTask never
// resolved.
func TestWeekG1DispatchAttachment(t *testing.T) {
	for _, syncMode := range []bool{false, true} {
		name := "async"
		if syncMode {
			name = "sync"
		}
		t.Run(name, func(t *testing.T) {
			msgBus := bus.New(nil, slogDiscardLogger())
			d, store := asyncTurnTestDispatcher(t)
			h := NewChatHandler(nil, d, msgBus, slogDiscardLogger())
			h.SetTaskStore(store)
			h.SetSyncMode(syncMode)
			h.syncWaitCeiling = 10 * time.Millisecond
			reg := NewTurnRegistry()
			h.SetTurnRegistry(reg)

			sub := msgBus.Subscribe("week-g1", "turn.terminal")
			defer msgBus.Unsubscribe(sub)

			payload, err := json.Marshal(ChatRequest{
				Message:        asyncTurnTestInput,
				ConversationID: "conv-week-g1",
				TurnID:         "turn-week-g1",
			})
			if err != nil {
				t.Fatal(err)
			}
			h.handleRequest(context.Background(), &models.BusMessage{
				ID:      "week-g1-request",
				Type:    models.MessageTypeRequest,
				Payload: payload,
			})

			// Consume the turn's own terminal event (parked ack / timeout).
			ev := waitTurnTerminal(t, sub)
			if ev.TaskID == "" {
				t.Fatalf("dispatch produced no task: %+v", ev)
			}
			if got := reg.TurnIDForTask(ev.TaskID); got != "turn-week-g1" {
				t.Errorf("task correlation = %q; want turn-week-g1 (task %s) — "+
					"attachTask must record (req.TurnID, result.Task.ID) before the branch",
					got, ev.TaskID)
			}
		})
	}
}

// TestWeekG1FailedCompletionStatus pins finding 2: a task.completed payload
// carrying status=failed must relay as a turn.terminal event with status
// "failed" (the failure reason rides the reply), never the blanket
// "completed" the pre-fix consumer emitted.
func TestWeekG1FailedCompletionStatus(t *testing.T) {
	h := newTestChatHandlerWithBus(t)
	sub := h.bus.Subscribe("week-g1-failed", "turn.terminal")
	defer h.bus.Unsubscribe(sub)

	payload, err := json.Marshal(map[string]any{
		"task_id":         "task-week-g1-failed",
		"name":            "test failure",
		"status":          "failed",
		"result":          "validation failed",
		"linked_sessions": []string{"session-week"},
	})
	if err != nil {
		t.Fatal(err)
	}
	h.handleTaskCompleted(&models.BusMessage{Payload: payload})

	ev := waitTurnTerminal(t, sub)
	if ev.Status != "failed" {
		t.Errorf("failed task relay status = %q; want failed", ev.Status)
	}
	if ev.Error == "" {
		t.Error("failed task relay must carry a non-empty error (the failure reason)")
	}
	if !strings.Contains(ev.Reply, "validation failed") {
		t.Errorf("reply = %q; want it to carry the failure reason", ev.Reply)
	}
}

// TestWeekG1ParkResumeEmitsTurnTerminal pins finding 15: a turn parked with a
// TurnID (a chat.submit turn) must emit a FINAL turn.terminal event on
// resume — completed after a successful resume, failed when the resume
// errors. Pre-fix the resume only sendResponse'd and the submitted turn was
// deleted from the registry with awaiters never resolving. Legacy parked
// turns (empty TurnID) stay silent.
func TestWeekG1ParkResumeEmitsTurnTerminal(t *testing.T) {
	t.Run("budget resume emits completed", func(t *testing.T) {
		msgBus := bus.New(nil, slogDiscardLogger())
		loop := NewAgentLoop("g1-budget", "/tmp",
			WithLLMChatter(&stubChatter{resp: &llm.Response{Content: "resumed reply"}}))
		h := NewChatHandler(loop, nil, msgBus, slogDiscardLogger())

		sub := msgBus.Subscribe("week-g1-budget", "turn.terminal")
		defer msgBus.Unsubscribe(sub)

		h.resumeParkedTurn(context.Background(), ParkedTurn{
			SessionID:      "sess-g1-budget",
			ConversationID: "conv-g1-budget",
			TurnID:         "turn-g1-budget",
			Message:        "finish it",
		})

		ev := waitTurnTerminal(t, sub)
		if ev.TurnID != "turn-g1-budget" {
			t.Errorf("turn_id = %q, want turn-g1-budget", ev.TurnID)
		}
		if ev.Status != "completed" {
			t.Errorf("status = %q, want completed", ev.Status)
		}
	})

	t.Run("budget resume emits failed on error", func(t *testing.T) {
		msgBus := bus.New(nil, slogDiscardLogger())
		// No LLM client: RunOnceWithParts fails with ErrNoLLMClient.
		loop := NewAgentLoop("g1-budget-fail", "/tmp")
		h := NewChatHandler(loop, nil, msgBus, slogDiscardLogger())

		sub := msgBus.Subscribe("week-g1-budget-fail", "turn.terminal")
		defer msgBus.Unsubscribe(sub)

		h.resumeParkedTurn(context.Background(), ParkedTurn{
			SessionID:      "sess-g1-budget-fail",
			ConversationID: "conv-g1-budget-fail",
			TurnID:         "turn-g1-budget-fail",
			Message:        "finish it",
		})

		ev := waitTurnTerminal(t, sub)
		if ev.Status != "failed" {
			t.Errorf("status = %q, want failed", ev.Status)
		}
		if ev.Error == "" {
			t.Error("failed resume must carry the error text")
		}
	})

	t.Run("quota resume emits completed", func(t *testing.T) {
		msgBus := bus.New(nil, slogDiscardLogger())
		loop := NewAgentLoop("g1-quota", "/tmp",
			WithLLMChatter(&stubChatter{resp: &llm.Response{Content: "quota resumed reply"}}))
		h := NewChatHandler(loop, nil, msgBus, slogDiscardLogger())

		sub := msgBus.Subscribe("week-g1-quota", "turn.terminal")
		defer msgBus.Unsubscribe(sub)

		h.resumeQuotaParkedTurn(context.Background(), QuotaParkedTurn{
			SessionID:      "sess-g1-quota",
			ConversationID: "conv-g1-quota",
			TurnID:         "turn-g1-quota",
			Message:        "finish it",
		})

		ev := waitTurnTerminal(t, sub)
		if ev.TurnID != "turn-g1-quota" {
			t.Errorf("turn_id = %q, want turn-g1-quota", ev.TurnID)
		}
		if ev.Status != "completed" {
			t.Errorf("status = %q, want completed", ev.Status)
		}
	})

	t.Run("legacy parked turn without TurnID stays silent", func(t *testing.T) {
		msgBus := bus.New(nil, slogDiscardLogger())
		loop := NewAgentLoop("g1-legacy", "/tmp",
			WithLLMChatter(&stubChatter{resp: &llm.Response{Content: "legacy reply"}}))
		h := NewChatHandler(loop, nil, msgBus, slogDiscardLogger())

		sub := msgBus.Subscribe("week-g1-legacy", "turn.terminal")
		defer msgBus.Unsubscribe(sub)

		h.resumeParkedTurn(context.Background(), ParkedTurn{
			SessionID:      "sess-g1-legacy",
			ConversationID: "conv-g1-legacy",
			// TurnID empty: legacy park — no terminal event.
			Message: "finish it",
		})

		assertNoSecondTurnTerminal(t, sub)
	})
}

// TestTurnRegistry_CompletedTombstoneSuppressesRetry pins finding 16: after
// Complete, a re-Register of the SAME turn id must be treated as existing
// (suppressed) — a same-turn-id retry after the turn answered must not
// re-execute. The tombstone set is bounded (cap + oldest-eviction) so a long
// run cannot grow it without bound.
func TestTurnRegistry_CompletedTombstoneSuppressesRetry(t *testing.T) {
	reg := NewTurnRegistry()

	if existing := reg.Register("turn-tomb", "conv-tomb"); existing {
		t.Fatal("fresh Register returned existing=true")
	}
	reg.Complete("turn-tomb")

	// Retry after completion: suppressed (existing=true), not re-registered.
	if existing := reg.Register("turn-tomb", "conv-tomb"); !existing {
		t.Error("Register after Complete returned existing=false; a same-turn-id retry would re-execute completed work")
	}
	if _, ok := reg.turns["turn-tomb"]; ok {
		t.Error("tombstoned retry re-created the live record")
	}
}

// TestTurnRegistry_TombstoneBounded pins the eviction bound: completing more
// turns than the tombstone cap evicts the OLDEST tombstones and never grows
// the set beyond the cap.
func TestTurnRegistry_TombstoneBounded(t *testing.T) {
	reg := NewTurnRegistry()

	for i := 0; i < turnRegistryTombstoneCap+25; i++ {
		turnID := "turn-bound-" + idFromInt(i)
		reg.Register(turnID, "conv-bound")
		reg.Complete(turnID)
	}

	if got := len(reg.tombstones); got > turnRegistryTombstoneCap {
		t.Errorf("tombstone set size = %d, want <= %d (cap + eviction)", got, turnRegistryTombstoneCap)
	}
	// The oldest tombstones were evicted; the newest survive.
	if existing := reg.Register("turn-bound-"+idFromInt(turnRegistryTombstoneCap+24), "conv-bound"); !existing {
		t.Error("newest tombstone evicted early (wrong eviction order)")
	}
	if existing := reg.Register("turn-bound-"+idFromInt(0), "conv-bound"); existing {
		t.Error("oldest tombstone still present; eviction never ran")
	}
}

func idFromInt(i int) string {
	return "id-" + string(rune('a'+i%26)) + time.Duration(i).String()
}

// TestWeekG1RequestModelReachesPlanRequest pins finding 18: a chat.request
// carrying a per-request model that dispatches a task must propagate that ref
// through publishPlanRequest into PlanRequest.RequestModel, so the
// orchestrator's specialist execution serves the turn on the requested model.
func TestWeekG1RequestModelReachesPlanRequest(t *testing.T) {
	msgBus := bus.New(nil, slogDiscardLogger())
	d, _ := asyncTurnTestDispatcher(t)
	h := NewChatHandler(nil, d, msgBus, slogDiscardLogger())

	planSub := msgBus.Subscribe("week-g1-model", "orchestrator.plan")
	defer msgBus.Unsubscribe(planSub)

	payload, err := json.Marshal(ChatRequest{
		Message:        asyncTurnTestInput,
		ConversationID: "conv-week-g1-model",
		TurnID:         "turn-week-g1-model",
		Model:          "local/user-requested-model",
	})
	if err != nil {
		t.Fatal(err)
	}
	h.handleRequest(context.Background(), &models.BusMessage{
		ID:      "week-g1-model-request",
		Type:    models.MessageTypeRequest,
		Payload: payload,
	})

	select {
	case msg := <-planSub.Channel:
		var preq PlanRequest
		if err := json.Unmarshal(msg.Payload, &preq); err != nil {
			t.Fatalf("unmarshal plan request: %v", err)
		}
		if preq.RequestModel != "local/user-requested-model" {
			t.Errorf("PlanRequest.RequestModel = %q; want local/user-requested-model (per-request model must ride async task dispatch)", preq.RequestModel)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("no orchestrator.plan event received")
	}
}

// TestWeekG1SpecPairSetupFailureFailsTask pins finding 11: a spec_pair setup
// failure in Plan must mark the task StateFailed, persist it, and publish a
// task.failed event so clients get an immediate failure instead of waiting
// for the watchdog.
func TestWeekG1SpecPairSetupFailureFailsTask(t *testing.T) {
	msgBus := bus.New(nil, slogDiscardLogger())
	defer msgBus.Close()

	taskStore, err := newTestTaskStore(t.TempDir())
	if err != nil {
		t.Fatalf("task store: %v", err)
	}
	defer taskStore.Close()

	// No PairManager wired: spec_pair setup fails with "pair manager not
	// configured" — the exact infrastructure-failure shape from the audit.
	sp := NewStrategicPlanner(StrategicPlannerConfig{
		Registry:  NewAgentRegistry(RegistryConfig{Logger: slogDiscardLogger()}),
		TaskStore: taskStore,
		StepStore: taskStore.StepStore(),
		Bus:       msgBus,
		Logger:    slogDiscardLogger(),
	})

	tsk := newTestTask("task-week-g1-pairfail", "compound spec_pair setup failure")
	if err := taskStore.Create(tsk); err != nil {
		t.Fatalf("create task: %v", err)
	}

	failSub := msgBus.Subscribe("week-g1-pairfail", "task.failed")
	defer msgBus.Unsubscribe(failSub)

	planErr := sp.Plan(context.Background(), PlanRequest{
		TaskID:    tsk.ID,
		SessionID: "sess-week-g1-pairfail",
		Input:     "spec the whole system and pair on it",
		Mode:      "spec_pair",
	})
	if planErr == nil {
		t.Fatal("Plan returned nil for a spec_pair setup failure")
	}

	// Immediate task.failed event — not watchdog-only.
	select {
	case msg := <-failSub.Channel:
		var payload map[string]any
		if err := json.Unmarshal(msg.Payload, &payload); err != nil {
			t.Fatalf("unmarshal task.failed payload: %v", err)
		}
		if payload["task_id"] != tsk.ID {
			t.Errorf("task.failed task_id = %v, want %v", payload["task_id"], tsk.ID)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("no task.failed event published on spec_pair setup failure")
	}

	// Persisted honest state.
	got, err := taskStore.GetByID(tsk.ID)
	if err != nil || got == nil {
		t.Fatalf("reload task: %v", err)
	}
	if got.State != task.StateFailed {
		t.Errorf("task state = %q, want failed (task left in planning state pre-fix)", got.State)
	}
}
