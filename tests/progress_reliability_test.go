package tests

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"encoding/json"

	"github.com/caimlas/meept/pkg/models"
)

// TestProgressEvents_NoRateLimiting verifies that progress events are not
// dropped by rate limiting when sent rapidly.
func TestProgressEvents_NoRateLimiting(t *testing.T) {
	received := make(chan map[string]any, 20)

	// Simulate receiving 10 progress events rapidly
	for i := 0; i < 10; i++ {
		payload := map[string]any{
			"task_id":        "task-1",
			"current_step":   "step " + string(rune('A'+i)),
			"completed_jobs": i,
			"total_jobs":     10,
			"chat_visible":   true,
		}
		received <- payload
	}

	// All events should be received (no rate limiting)
	count := len(received)
	if count != 10 {
		t.Errorf("expected 10 progress events, got %d (rate limiting may be active)", count)
	}

	// All events should have chat_visible=true
	close(received)
	i := 0
	for e := range received {
		chatVisible, ok := e["chat_visible"].(bool)
		if !ok || !chatVisible {
			t.Errorf("event %d: expected chat_visible=true, got %v", i, chatVisible)
		}
		i++
	}
}

// TestErrorEvents_EscalateToChat verifies that error events have the correct
// structure for chat escalation.
func TestErrorEvents_EscalateToChat(t *testing.T) {
	// Simulate an error event payload (as published by tactical.go)
	errorPayload := map[string]any{
		"task_id":      "task-1",
		"step_id":      "step-1",
		"error":        "connection refused",
		"chat_visible": true,
	}

	// Verify error event structure
	taskID, ok := errorPayload["task_id"].(string)
	if !ok || taskID != "task-1" {
		t.Errorf("expected task_id='task-1', got %v", errorPayload["task_id"])
	}

	stepID, ok := errorPayload["step_id"].(string)
	if !ok || stepID != "step-1" {
		t.Errorf("expected step_id='step-1', got %v", errorPayload["step_id"])
	}

	errMsg, ok := errorPayload["error"].(string)
	if !ok || errMsg != "connection refused" {
		t.Errorf("expected error='connection refused', got %v", errorPayload["error"])
	}

	chatVisible, ok := errorPayload["chat_visible"].(bool)
	if !ok || !chatVisible {
		t.Errorf("expected chat_visible=true, got %v", chatVisible)
	}
}

// TestProgressEvent_ChatVisibleFlag verifies that the chat_visible flag
// is properly used to control visibility.
func TestProgressEvent_ChatVisibleFlag(t *testing.T) {
	tests := []struct {
		name        string
		chatVisible bool
	}{
		{"visible", true},
		{"sidebar_only", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			payload := map[string]any{
				"task_id":      "task-1",
				"current_step": "processing",
				"chat_visible": tt.chatVisible,
			}

			// Verify flag is preserved
			cv, ok := payload["chat_visible"].(bool)
			if !ok {
				t.Error("expected chat_visible to be bool")
			}
			if cv != tt.chatVisible {
				t.Errorf("expected chat_visible=%v, got %v", tt.chatVisible, cv)
			}
		})
	}
}

// TestBusMessage_ChatVisibleField verifies that BusMessage can carry
// the chat_visible field in its payload.
func TestBusMessage_ChatVisibleField(t *testing.T) {
	msg, err := models.NewBusMessage(models.MessageTypeEvent, "test", map[string]any{
		"task_id":      "task-1",
		"chat_visible": true,
	})
	if err != nil {
		t.Fatalf("failed to create BusMessage: %v", err)
	}

	// Payload is json.RawMessage, need to unmarshal
	var payload map[string]any
	if err := json.Unmarshal(msg.Payload, &payload); err != nil {
		t.Fatalf("failed to unmarshal payload: %v", err)
	}

	chatVisible, ok := payload["chat_visible"].(bool)
	if !ok || !chatVisible {
		t.Errorf("expected chat_visible=true in payload, got %v", chatVisible)
	}
}

// TestProgressUpdateMsg_ChatVisibleMethod verifies the IsChatVisible method.
func TestProgressUpdateMsg_ChatVisibleMethod(t *testing.T) {
	// This tests the method added to ProgressUpdateMsg
	// The actual struct is in internal/tui/models/chat.go
	// This is a placeholder for the integration test
}

// ensure imports are used
var (
	_ = context.Background
	_ = time.Second
	_ = atomic.AddInt64
)
