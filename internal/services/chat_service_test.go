package services

import (
	"context"
	"errors"
	"log/slog"
	"testing"

	"github.com/caimlas/meept/internal/agent"
	"github.com/caimlas/meept/internal/bus"
	"github.com/caimlas/meept/internal/config"
)

// newTestChatServiceWithRegistry creates a ChatService backed by a real
// AgentRegistry with a MessageQueue registered for the given conversationID.
// The registry is minimally configured (no LLM client, no task store, etc.)
// so that only queue operations are exercised.
func newTestChatServiceWithRegistry(t *testing.T, conversationID string) (*ChatService, *agent.AgentRegistry) {
	t.Helper()

	msgBus := bus.New(nil, nil)
	var logger *slog.Logger // let NewChatService use slog.Default

	reg := agent.NewAgentRegistry(agent.RegistryConfig{
		MessageBus: msgBus,
		Queues: config.AgentQueuesConfig{
			MaxFollowUp: 20,
		},
	})

	// Create a real MessageQueue and register it as active.
	q := agent.NewMessageQueue(
		agent.WithQueueBus(msgBus),
		agent.WithQueueAgentID("test"),
	)
	reg.RegisterActiveQueue(conversationID, q)

	svc := NewChatService(msgBus, reg, logger)
	return svc, reg
}

func TestChatService_Steer_Success(t *testing.T) {
	t.Parallel()
	const convID = "conv-steer-ok"

	svc, reg := newTestChatServiceWithRegistry(t, convID)
	defer reg.Close()

	err := svc.Steer(context.Background(), SteerRequest{
		Message:        "change direction",
		ConversationID: convID,
		Source:         "tui",
	})
	if err != nil {
		t.Fatalf("Steer() error = %v", err)
	}

	// Verify the steering message landed in the queue.
	q, _ := reg.GetActiveQueue(convID)
	if q == nil {
		t.Fatal("expected active queue")
	}
	if !q.HasSteering() {
		t.Error("expected steering message in queue")
	}
}

func TestChatService_Steer_NoRegistry(t *testing.T) {
	t.Parallel()

	svc := NewChatService(bus.New(nil, nil), nil, nil)

	err := svc.Steer(context.Background(), SteerRequest{
		Message:        "no registry",
		ConversationID: "conv-42",
	})
	if err == nil {
		t.Fatal("expected error when registry is nil")
	}
	if !errors.Is(err, ErrUnavailable) {
		t.Errorf("expected ErrUnavailable, got %v", err)
	}
}

func TestChatService_Steer_QueueNotFound(t *testing.T) {
	t.Parallel()

	msgBus := bus.New(nil, nil)
	reg := agent.NewAgentRegistry(agent.RegistryConfig{
		MessageBus: msgBus,
	})
	defer reg.Close()

	svc := NewChatService(msgBus, reg, nil)

	err := svc.Steer(context.Background(), SteerRequest{
		Message:        "no queue",
		ConversationID: "nonexistent-conv",
	})
	if err == nil {
		t.Fatal("expected error when no active queue")
	}
	if !errors.Is(err, agent.ErrQueueNotFound) {
		t.Errorf("expected ErrQueueNotFound, got %v", err)
	}
}

func TestChatService_Steer_QueueClosed(t *testing.T) {
	t.Parallel()
	const convID = "conv-steer-closed"

	msgBus := bus.New(nil, nil)
	reg := agent.NewAgentRegistry(agent.RegistryConfig{
		MessageBus: msgBus,
	})
	defer reg.Close()

	q := agent.NewMessageQueue()
	reg.RegisterActiveQueue(convID, q)
	// UnregisterActiveQueue closes the queue AND removes it from the map,
	// so the service layer sees ErrQueueNotFound (not ErrQueueClosed).
	reg.UnregisterActiveQueue(convID)

	svc := NewChatService(msgBus, reg, nil)

	err := svc.Steer(context.Background(), SteerRequest{
		Message:        "after close",
		ConversationID: convID,
	})
	if err == nil {
		t.Fatal("expected error when queue is closed/unregistered")
	}
	// The service sees the queue as not found because UnregisterActiveQueue
	// removes it from the map. ErrQueueClosed is tested at the queue level.
	if !errors.Is(err, agent.ErrQueueNotFound) {
		t.Errorf("expected ErrQueueNotFound, got %v", err)
	}
}

func TestChatService_Steer_EmptyMessage(t *testing.T) {
	t.Parallel()

	svc := NewChatService(bus.New(nil, nil), nil, nil)

	err := svc.Steer(context.Background(), SteerRequest{
		Message:        "",
		ConversationID: "conv-42",
	})
	if err == nil {
		t.Fatal("expected error for empty message")
	}
	if !errors.Is(err, ErrInvalidInput) {
		t.Errorf("expected ErrInvalidInput, got %v", err)
	}
}

func TestChatService_FollowUp_Success(t *testing.T) {
	t.Parallel()
	const convID = "conv-followup-ok"

	svc, reg := newTestChatServiceWithRegistry(t, convID)
	defer reg.Close()

	err := svc.FollowUp(context.Background(), FollowUpRequest{
		Message:        "also do this",
		ConversationID: convID,
		Source:         "tui",
	})
	if err != nil {
		t.Fatalf("FollowUp() error = %v", err)
	}

	q, _ := reg.GetActiveQueue(convID)
	if q == nil {
		t.Fatal("expected active queue")
	}
	if !q.HasFollowUp() {
		t.Error("expected follow-up message in queue")
	}
}

func TestChatService_FollowUp_NoRegistry(t *testing.T) {
	t.Parallel()

	svc := NewChatService(bus.New(nil, nil), nil, nil)

	err := svc.FollowUp(context.Background(), FollowUpRequest{
		Message:        "no registry",
		ConversationID: "conv-42",
	})
	if err == nil {
		t.Fatal("expected error when registry is nil")
	}
	if !errors.Is(err, ErrUnavailable) {
		t.Errorf("expected ErrUnavailable, got %v", err)
	}
}

func TestChatService_FollowUp_QueueFull(t *testing.T) {
	t.Parallel()
	const convID = "conv-followup-full"

	msgBus := bus.New(nil, nil)
	reg := agent.NewAgentRegistry(agent.RegistryConfig{
		MessageBus: msgBus,
		Queues: config.AgentQueuesConfig{
			MaxFollowUp: 2,
		},
	})
	defer reg.Close()

	q := agent.NewMessageQueue(agent.WithQueueConfig(agent.QueueConfig{
		MaxFollowUp:   2,
		PersistFollowUp: false,
	}))
	reg.RegisterActiveQueue(convID, q)

	svc := NewChatService(msgBus, reg, nil)

	// Fill the queue.
	for i := range 2 {
		err := svc.FollowUp(context.Background(), FollowUpRequest{
			Message:        "msg",
			ConversationID: convID,
		})
		if err != nil {
			t.Fatalf("FollowUp %d unexpected error: %v", i, err)
		}
	}

	// Third should fail.
	err := svc.FollowUp(context.Background(), FollowUpRequest{
		Message:        "overflow",
		ConversationID: convID,
	})
	if err == nil {
		t.Fatal("expected error when queue is full")
	}
	if !errors.Is(err, agent.ErrQueueFull) {
		t.Errorf("expected ErrQueueFull, got %v", err)
	}
}

func TestChatService_FollowUp_EmptyMessage(t *testing.T) {
	t.Parallel()

	svc := NewChatService(bus.New(nil, nil), nil, nil)

	err := svc.FollowUp(context.Background(), FollowUpRequest{
		Message:        "",
		ConversationID: "conv-42",
	})
	if err == nil {
		t.Fatal("expected error for empty message")
	}
	if !errors.Is(err, ErrInvalidInput) {
		t.Errorf("expected ErrInvalidInput, got %v", err)
	}
}

func TestChatService_GetQueueStatus_Active(t *testing.T) {
	t.Parallel()
	const convID = "conv-status-active"

	svc, reg := newTestChatServiceWithRegistry(t, convID)
	defer reg.Close()

	// Add messages to both queues.
	_ = svc.Steer(context.Background(), SteerRequest{
		Message:        "steer",
		ConversationID: convID,
	})
	_ = svc.FollowUp(context.Background(), FollowUpRequest{
		Message:        "follow",
		ConversationID: convID,
	})

	resp, err := svc.GetQueueStatus(context.Background(), QueueStatusRequest{
		ConversationID: convID,
	})
	if err != nil {
		t.Fatalf("GetQueueStatus() error = %v", err)
	}

	if resp.SteeringDepth != 1 {
		t.Errorf("SteeringDepth = %d, want 1", resp.SteeringDepth)
	}
	if resp.FollowUpDepth != 1 {
		t.Errorf("FollowUpDepth = %d, want 1", resp.FollowUpDepth)
	}
	if !resp.IsActive {
		t.Error("IsActive = false, want true")
	}
	if resp.Generation == 0 {
		t.Error("Generation = 0, want non-zero (queue was modified)")
	}
}

func TestChatService_GetQueueStatus_NoQueue(t *testing.T) {
	t.Parallel()

	msgBus := bus.New(nil, nil)
	reg := agent.NewAgentRegistry(agent.RegistryConfig{
		MessageBus: msgBus,
	})
	defer reg.Close()

	svc := NewChatService(msgBus, reg, nil)

	resp, err := svc.GetQueueStatus(context.Background(), QueueStatusRequest{
		ConversationID: "nonexistent",
	})
	if err != nil {
		t.Fatalf("GetQueueStatus() error = %v", err)
	}
	if resp.SteeringDepth != 0 || resp.FollowUpDepth != 0 {
		t.Errorf("expected zero depths, got steering=%d followup=%d", resp.SteeringDepth, resp.FollowUpDepth)
	}
	if resp.IsActive {
		t.Error("IsActive should be false when no queue exists")
	}
}

func TestChatService_GetQueueStatus_NoRegistry(t *testing.T) {
	t.Parallel()

	svc := NewChatService(bus.New(nil, nil), nil, nil)

	resp, err := svc.GetQueueStatus(context.Background(), QueueStatusRequest{
		ConversationID: "conv-42",
	})
	if err != nil {
		t.Fatalf("GetQueueStatus() error = %v", err)
	}
	if resp.SteeringDepth != 0 || resp.FollowUpDepth != 0 {
		t.Errorf("expected zero depths with nil registry, got steering=%d followup=%d", resp.SteeringDepth, resp.FollowUpDepth)
	}
	if resp.IsActive {
		t.Error("IsActive should be false with nil registry")
	}
}
