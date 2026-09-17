package handlers

import "testing"

// TestHandleTurnTerminal_TimeoutStatus covers the minimal TUI surface for
// turn.terminal (leaf 01-turn-terminal-event Task 5): a timeout-status event
// updates the pending indicator with the fixed lowercase text; other
// statuses produce no notification.
func TestHandleTurnTerminal_TimeoutStatus(t *testing.T) {
	h := NewTaskEventHandler()

	notif := h.HandleTurnTerminal(map[string]any{
		"conversation_id": "conv-1",
		"turn_id":         "turn-1",
		"status":          "timeout",
		"reply":           "Task task-1 is still running; results will arrive when it completes.",
	})
	if notif == nil {
		t.Fatal("timeout-status turn.terminal must produce a pending-indicator notification")
	}
	if notif.Type != "timeout" {
		t.Errorf("type = %q, want timeout", notif.Type)
	}
	if notif.Message != "task still running — result will arrive" {
		t.Errorf("message = %q, want the fixed pending-indicator text", notif.Message)
	}
}

func TestHandleTurnTerminal_NonTimeoutStatusesAreNoOp(t *testing.T) {
	h := NewTaskEventHandler()

	for _, status := range []string{"completed", "failed", "parked"} {
		if notif := h.HandleTurnTerminal(map[string]any{
			"conversation_id": "conv-1",
			"status":          status,
		}); notif != nil {
			t.Errorf("status %q: expected nil notification, got %+v", status, notif)
		}
	}
}
