package rpc

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/caimlas/meept/internal/agent"
	"github.com/caimlas/meept/internal/bus"
	"github.com/caimlas/meept/internal/config"
)

// newTestQueueHandler creates a QueueHandler with a real AgentRegistry
// that has a MessageQueue registered for the given conversationID.
func newTestQueueHandler(t *testing.T, conversationID string) (*QueueHandler, *agent.AgentRegistry) {
	t.Helper()

	msgBus := bus.New(nil, nil)
	reg := agent.NewAgentRegistry(agent.RegistryConfig{
		MessageBus: msgBus,
		Queues: config.AgentQueuesConfig{
			MaxFollowUp: 20,
		},
	})

	q := agent.NewMessageQueue(
		agent.WithQueueBus(msgBus),
		agent.WithQueueAgentID("test"),
	)
	reg.RegisterActiveQueue(conversationID, q)

	return NewQueueHandler(reg), reg
}

func TestQueueHandler_Steer_Success(t *testing.T) {
	t.Parallel()
	const convID = "conv-rpc-steer-ok"

	h, reg := newTestQueueHandler(t, convID)
	defer reg.Close()

	params := json.RawMessage(`{"message":"change approach","conversation_id":"` + convID + `","source":"cli"}`)
	result, err := h.handleSteer(context.Background(), params)
	if err != nil {
		t.Fatalf("handleSteer() error = %v", err)
	}

	resp, ok := result.(map[string]any)
	if !ok {
		t.Fatal("expected map[string]any result")
	}
	if resp["status"] != "queued" {
		t.Errorf("status = %v, want %q", resp["status"], "queued")
	}
	if resp["queue"] != "steer" {
		t.Errorf("queue = %v, want %q", resp["queue"], "steer")
	}

	// Verify the message was actually steered.
	q, _ := reg.GetActiveQueue(convID)
	if !q.HasSteering() {
		t.Error("expected steering message in queue")
	}
}

func TestQueueHandler_Steer_NoRegistry(t *testing.T) {
	t.Parallel()

	h := NewQueueHandler(nil)

	params := json.RawMessage(`{"message":"test","conversation_id":"conv-42"}`)
	_, err := h.handleSteer(context.Background(), params)
	if err == nil {
		t.Fatal("expected error when registry is nil")
	}
	// The reg() helper returns "queue feature not enabled".
	if err.Error() != "queue feature not enabled" {
		t.Errorf("expected 'queue feature not enabled', got %v", err)
	}
}

func TestQueueHandler_Steer_MissingParams(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		params json.RawMessage
	}{
		{"empty params", json.RawMessage(`{}`)},
		{"missing conversation_id", json.RawMessage(`{"message":"test"}`)},
		{"missing message", json.RawMessage(`{"conversation_id":"conv-42"}`)},
		{"invalid json", json.RawMessage(`not json`)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			h, reg := newTestQueueHandler(t, "conv-42")
			defer reg.Close()

			_, err := h.handleSteer(context.Background(), tt.params)
			if err == nil {
				t.Fatal("expected error for invalid params")
			}
		})
	}
}

func TestQueueHandler_Steer_QueueNotFound(t *testing.T) {
	t.Parallel()

	msgBus := bus.New(nil, nil)
	reg := agent.NewAgentRegistry(agent.RegistryConfig{MessageBus: msgBus})
	defer reg.Close()
	h := NewQueueHandler(reg)

	params := json.RawMessage(`{"message":"test","conversation_id":"nonexistent"}`)
	_, err := h.handleSteer(context.Background(), params)
	if err == nil {
		t.Fatal("expected error when queue not found")
	}
	if !errors.Is(err, agent.ErrQueueNotFound) {
		t.Errorf("expected ErrQueueNotFound, got %v", err)
	}
}

func TestQueueHandler_Steer_QueueClosed(t *testing.T) {
	t.Parallel()
	const convID = "conv-steer-closed"

	msgBus := bus.New(nil, nil)
	reg := agent.NewAgentRegistry(agent.RegistryConfig{MessageBus: msgBus})
	defer reg.Close()

	q := agent.NewMessageQueue()
	reg.RegisterActiveQueue(convID, q)
	// UnregisterActiveQueue closes the queue AND removes it from the map,
	// so the RPC handler sees ErrQueueNotFound (not ErrQueueClosed).
	reg.UnregisterActiveQueue(convID)

	h := NewQueueHandler(reg)
	params := json.RawMessage(`{"message":"after close","conversation_id":"` + convID + `"}`)
	_, err := h.handleSteer(context.Background(), params)
	if err == nil {
		t.Fatal("expected error when queue is closed/unregistered")
	}
	// The handler sees the queue as not found because UnregisterActiveQueue
	// removes it from the map. ErrQueueClosed is tested at the queue level.
	if !errors.Is(err, agent.ErrQueueNotFound) {
		t.Errorf("expected ErrQueueNotFound, got %v", err)
	}
}

func TestQueueHandler_FollowUp_Success(t *testing.T) {
	t.Parallel()
	const convID = "conv-rpc-followup-ok"

	h, reg := newTestQueueHandler(t, convID)
	defer reg.Close()

	params := json.RawMessage(`{"message":"also do this","conversation_id":"` + convID + `","source":"tui"}`)
	result, err := h.handleFollowUp(context.Background(), params)
	if err != nil {
		t.Fatalf("handleFollowUp() error = %v", err)
	}

	resp, ok := result.(map[string]any)
	if !ok {
		t.Fatal("expected map[string]any result")
	}
	if resp["status"] != "queued" {
		t.Errorf("status = %v, want %q", resp["status"], "queued")
	}
	if resp["queue"] != "followup" {
		t.Errorf("queue = %v, want %q", resp["queue"], "followup")
	}

	q, _ := reg.GetActiveQueue(convID)
	if !q.HasFollowUp() {
		t.Error("expected follow-up message in queue")
	}
}

func TestQueueHandler_FollowUp_MissingParams(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		params json.RawMessage
	}{
		{"empty params", json.RawMessage(`{}`)},
		{"missing conversation_id", json.RawMessage(`{"message":"test"}`)},
		{"missing message", json.RawMessage(`{"conversation_id":"conv-42"}`)},
		{"invalid json", json.RawMessage(`not json`)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			h, reg := newTestQueueHandler(t, "conv-42")
			defer reg.Close()

			_, err := h.handleFollowUp(context.Background(), tt.params)
			if err == nil {
				t.Fatal("expected error for invalid params")
			}
		})
	}
}

func TestQueueHandler_FollowUp_NoRegistry(t *testing.T) {
	t.Parallel()

	h := NewQueueHandler(nil)

	params := json.RawMessage(`{"message":"test","conversation_id":"conv-42"}`)
	_, err := h.handleFollowUp(context.Background(), params)
	if err == nil {
		t.Fatal("expected error when registry is nil")
	}
	if err.Error() != "queue feature not enabled" {
		t.Errorf("expected 'queue feature not enabled', got %v", err)
	}
}

func TestQueueHandler_FollowUp_QueueNotFound(t *testing.T) {
	t.Parallel()

	msgBus := bus.New(nil, nil)
	reg := agent.NewAgentRegistry(agent.RegistryConfig{MessageBus: msgBus})
	defer reg.Close()
	h := NewQueueHandler(reg)

	params := json.RawMessage(`{"message":"test","conversation_id":"nonexistent"}`)
	_, err := h.handleFollowUp(context.Background(), params)
	if err == nil {
		t.Fatal("expected error when queue not found")
	}
	if !errors.Is(err, agent.ErrQueueNotFound) {
		t.Errorf("expected ErrQueueNotFound, got %v", err)
	}
}

func TestQueueHandler_FollowUp_QueueFull(t *testing.T) {
	t.Parallel()
	const convID = "conv-followup-full"

	msgBus := bus.New(nil, nil)
	reg := agent.NewAgentRegistry(agent.RegistryConfig{MessageBus: msgBus})
	defer reg.Close()

	q := agent.NewMessageQueue(agent.WithQueueConfig(agent.QueueConfig{
		MaxFollowUp:     2,
		PersistFollowUp: false,
	}))
	reg.RegisterActiveQueue(convID, q)

	h := NewQueueHandler(reg)

	// Fill the queue.
	for i := range 2 {
		params := json.RawMessage(`{"message":"msg","conversation_id":"` + convID + `"}`)
		if _, err := h.handleFollowUp(context.Background(), params); err != nil {
			t.Fatalf("FollowUp %d unexpected error: %v", i, err)
		}
	}

	// Third should fail.
	params := json.RawMessage(`{"message":"overflow","conversation_id":"` + convID + `"}`)
	_, err := h.handleFollowUp(context.Background(), params)
	if err == nil {
		t.Fatal("expected error when queue is full")
	}
	if !errors.Is(err, agent.ErrQueueFull) {
		t.Errorf("expected ErrQueueFull, got %v", err)
	}
}

func TestQueueHandler_Status_Active(t *testing.T) {
	t.Parallel()
	const convID = "conv-rpc-status-active"

	h, reg := newTestQueueHandler(t, convID)
	defer reg.Close()

	// Add messages to both queues.
	q, _ := reg.GetActiveQueue(convID)
	_ = q.Steer(context.Background(), "steer", "test")
	_ = q.FollowUp(context.Background(), "follow", "test")

	params := json.RawMessage(`{"conversation_id":"` + convID + `"}`)
	result, err := h.handleStatus(context.Background(), params)
	if err != nil {
		t.Fatalf("handleStatus() error = %v", err)
	}

	resp, ok := result.(map[string]any)
	if !ok {
		t.Fatal("expected map[string]any result")
	}
	if resp["steering_depth"] != 1 {
		t.Errorf("steering_depth = %v, want 1", resp["steering_depth"])
	}
	if resp["followup_depth"] != 1 {
		t.Errorf("followup_depth = %v, want 1", resp["followup_depth"])
	}
	if resp["is_active"] != true {
		t.Errorf("is_active = %v, want true", resp["is_active"])
	}
	if resp["generation"] == uint64(0) {
		t.Error("generation should be non-zero after modifications")
	}
}

func TestQueueHandler_Status_NoQueue(t *testing.T) {
	t.Parallel()

	msgBus := bus.New(nil, nil)
	reg := agent.NewAgentRegistry(agent.RegistryConfig{MessageBus: msgBus})
	defer reg.Close()
	h := NewQueueHandler(reg)

	params := json.RawMessage(`{"conversation_id":"nonexistent"}`)
	result, err := h.handleStatus(context.Background(), params)
	if err != nil {
		t.Fatalf("handleStatus() error = %v", err)
	}

	resp, ok := result.(map[string]any)
	if !ok {
		t.Fatal("expected map[string]any result")
	}
	if resp["steering_depth"] != 0 {
		t.Errorf("steering_depth = %v, want 0", resp["steering_depth"])
	}
	if resp["followup_depth"] != 0 {
		t.Errorf("followup_depth = %v, want 0", resp["followup_depth"])
	}
	if resp["is_active"] != false {
		t.Errorf("is_active = %v, want false", resp["is_active"])
	}
}

func TestQueueHandler_Status_MissingConversationID(t *testing.T) {
	t.Parallel()

	h, reg := newTestQueueHandler(t, "conv-42")
	defer reg.Close()

	params := json.RawMessage(`{}`)
	_, err := h.handleStatus(context.Background(), params)
	if err == nil {
		t.Fatal("expected error for missing conversation_id")
	}
}

func TestQueueHandler_Restore_Active(t *testing.T) {
	t.Parallel()
	const convID = "conv-rpc-restore-active"

	h, reg := newTestQueueHandler(t, convID)
	defer reg.Close()

	// Add follow-ups to the queue.
	q, _ := reg.GetActiveQueue(convID)
	_ = q.FollowUp(context.Background(), "restored msg 1", "test")
	_ = q.FollowUp(context.Background(), "restored msg 2", "test")

	params := json.RawMessage(`{"conversation_id":"` + convID + `"}`)
	result, err := h.handleRestore(context.Background(), params)
	if err != nil {
		t.Fatalf("handleRestore() error = %v", err)
	}

	resp, ok := result.(map[string]any)
	if !ok {
		t.Fatal("expected map[string]any result")
	}
	if resp["restored"] != 2 {
		t.Errorf("restored = %v, want 2", resp["restored"])
	}
	if resp["active"] != true {
		t.Errorf("active = %v, want true", resp["active"])
	}
}

func TestQueueHandler_Restore_NoQueue(t *testing.T) {
	t.Parallel()

	msgBus := bus.New(nil, nil)
	reg := agent.NewAgentRegistry(agent.RegistryConfig{MessageBus: msgBus})
	defer reg.Close()
	h := NewQueueHandler(reg)

	params := json.RawMessage(`{"conversation_id":"nonexistent"}`)
	result, err := h.handleRestore(context.Background(), params)
	if err != nil {
		t.Fatalf("handleRestore() error = %v", err)
	}

	resp, ok := result.(map[string]any)
	if !ok {
		t.Fatal("expected map[string]any result")
	}
	if resp["restored"] != 0 {
		t.Errorf("restored = %v, want 0", resp["restored"])
	}
	if resp["active"] != false {
		t.Errorf("active = %v, want false", resp["active"])
	}
}

func TestQueueHandler_Restore_NoRegistry(t *testing.T) {
	t.Parallel()

	h := NewQueueHandler(nil)

	params := json.RawMessage(`{"conversation_id":"conv-42"}`)
	_, err := h.handleRestore(context.Background(), params)
	if err == nil {
		t.Fatal("expected error when registry is nil")
	}
	if err.Error() != "queue feature not enabled" {
		t.Errorf("expected 'queue feature not enabled', got %v", err)
	}
}

func TestQueueHandler_Restore_MissingConversationID(t *testing.T) {
	t.Parallel()

	h, reg := newTestQueueHandler(t, "conv-42")
	defer reg.Close()

	params := json.RawMessage(`{}`)
	_, err := h.handleRestore(context.Background(), params)
	if err == nil {
		t.Fatal("expected error for missing conversation_id")
	}
}

func TestQueueHandler_ServerRegistration(t *testing.T) {
	h, reg := newTestQueueHandler(t, "conv-reg")
	defer reg.Close()

	server := New(&Config{}, nil, nil)
	h.RegisterQueueMethods(server)

	expected := []string{
		"chat.steer",
		"chat.followup",
		"chat.queue_status",
		"chat.queue.restore",
	}
	for _, method := range expected {
		handler, ok := server.handlers[method]
		if !ok {
			t.Errorf("handler for %s not registered", method)
		}
		if handler == nil {
			t.Errorf("handler for %s is nil", method)
		}
	}
}
