package agent

// 01-safety TEST GATE T2 (00T-gate.md): rejected relay terminal.
//
// C2: task.rejected must relay to turn.terminal with status=failed,
// handler_case=task_rejected_relay, error=<reason>, under the ORIGINATING
// turn id (registry task->turn edge), and complete the turn after emit.
//
// RED today: ChatHandler.Start subscribes task.completed/task.failed but
// NO task.rejected subscription exists (handler.go Start block); the
// strategic planner's RejectPlan publishes task.rejected into the void
// (orphan topic). The rejected turn never terminalizes until the watchdog
// falsely reaps it.

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/caimlas/meept/pkg/models"
)

// TestRejectedRelayTerminal pins the full relay: a tracked turn attached
// to a task, a task.rejected{reason} publication, and the terminal event
// the awaiting client keys on.
func TestRejectedRelayTerminal(t *testing.T) {
	h := newTestChatHandlerWithBus(t)
	reg := NewTurnRegistry()
	h.SetTurnRegistry(reg)

	reg.Register("turn-rej-1", "conv-rej-1")
	reg.AttachTask("turn-rej-1", "task-rej-1")

	sub := h.bus.Subscribe("gate-rejected-relay", "turn.terminal")
	defer h.bus.Unsubscribe(sub)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := h.Start(ctx); err != nil {
		t.Fatalf("ChatHandler.Start: %v", err)
	}
	defer func() {
		if err := h.Stop(context.Background()); err != nil {
			t.Logf("handler stop: %v", err)
		}
	}()

	payload, err := json.Marshal(map[string]any{
		"task_id": "task-rej-1",
		"reason":  "user rejected the plan: touches production",
	})
	if err != nil {
		t.Fatalf("marshal task.rejected: %v", err)
	}
	h.bus.Publish("task.rejected", &models.BusMessage{
		Type:    models.MessageTypeEvent,
		Topic:   "task.rejected",
		Source:  "strategic-planner",
		Payload: payload,
	})

	select {
	case msg := <-sub.Channel:
		var ev TurnTerminalEvent
		if uerr := json.Unmarshal(msg.Payload, &ev); uerr != nil {
			t.Fatalf("decode terminal: %v", uerr)
		}
		if ev.TurnID != "turn-rej-1" {
			t.Errorf("terminal turn_id = %q, want the ORIGINATING turn-rej-1 (never a fresh id)", ev.TurnID)
		}
		if ev.Status != "failed" {
			t.Errorf("terminal status = %q, want failed (C2)", ev.Status)
		}
		if ev.HandlerCase != "task_rejected_relay" {
			t.Errorf("handler_case = %q, want task_rejected_relay (C1/C2)", ev.HandlerCase)
		}
		if ev.Error != "user rejected the plan: touches production" {
			t.Errorf("terminal error = %q, want the rejection reason verbatim", ev.Error)
		}
		if ev.TaskID != "task-rej-1" {
			t.Errorf("terminal task_id = %q, want task-rej-1", ev.TaskID)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("CURRENT DEFECT: no task.rejected subscription exists; the rejected turn never terminalizes (orphan topic)")
	}

	// The task->turn edge must be dropped after the relay so the watchdog
	// cannot later reap an already-answered turn (completeTurn after emit).
	if got := reg.TurnIDForTask("task-rej-1"); got != "" {
		t.Errorf("task->turn edge still resolves to %q after the rejected relay; completeTurn must run after emit", got)
	}
}
