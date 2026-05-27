package services

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/caimlas/meept/internal/agent"
	"github.com/caimlas/meept/internal/bus"
	"github.com/caimlas/meept/pkg/models"
)

// ChatService handles chat operations.
type ChatService struct {
	bus           *bus.MessageBus
	agentRegistry *agent.AgentRegistry
	logger        *slog.Logger
}

// ChatRequest contains chat input.
type ChatRequest struct {
	Message        string `json:"message"`
	ConversationID string `json:"conversation_id"`
}

// ChatResponse contains chat output.
type ChatResponse struct {
	Reply      string `json:"reply"`
	Model      string `json:"model,omitempty"`
	TokensUsed int    `json:"tokens_used,omitempty"`
}

// NewChatService creates a chat service.
func NewChatService(msgBus *bus.MessageBus, agentReg *agent.AgentRegistry, logger *slog.Logger) *ChatService {
	if logger == nil {
		logger = slog.Default()
	}
	return &ChatService{
		bus:           msgBus,
		agentRegistry: agentReg,
		logger:        logger,
	}
}

// Chat sends a message and waits for a response.
func (s *ChatService) Chat(ctx context.Context, req ChatRequest) (*ChatResponse, error) {
	if s.bus == nil {
		return nil, wrapError("chat", "Chat", ErrUnavailable)
	}
	if req.Message == "" {
		return nil, wrapError("chat", "Chat", ErrInvalidInput)
	}
	if req.ConversationID == "" {
		return nil, wrapError("chat", "Chat", ErrInvalidInput)
	}

	// Create request message
	msgID := fmt.Sprintf("svc-chat-%d", time.Now().UnixNano())
	msg := &models.BusMessage{
		ID:      msgID,
		Type:    models.MessageTypeRequest,
		Topic:   "chat.request",
		Source:  "svc.chat",
		Payload: []byte(req.Message),
		ReplyTo: "chat.response",
	}

	// Create response channel
	respChan := make(chan *models.BusMessage, 1)
	replyTopic := "chat.res." + msgID
	sub := s.bus.Subscribe(msgID, replyTopic)
	defer s.bus.Unsubscribe(sub)

	// Watch for responses (context-aware)
	go func() {
		for {
			select {
			case resp, ok := <-sub.Channel:
				if !ok {
					return
				}
				if resp.ReplyTo == msgID {
					select {
					case respChan <- resp:
					default:
					}
					return
				}
			case <-ctx.Done():
				return
			}
		}
	}()

	// Publish request
	s.bus.Publish("chat.request", msg)

	// Wait for response
	select {
	case resp := <-respChan:
		var reply struct {
			Reply      string `json:"reply"`
			Model      string `json:"model,omitempty"`
			TokensUsed int    `json:"tokens_used,omitempty"`
		}
		if err := json.Unmarshal(resp.Payload, &reply); err != nil {
			return &ChatResponse{Reply: string(resp.Payload)}, nil
		}
		return &ChatResponse{
			Reply:      reply.Reply,
			Model:      reply.Model,
			TokensUsed: reply.TokensUsed,
		}, nil
	case <-time.After(2 * time.Minute):
		return nil, wrapError("chat", "Chat", ErrTimeout)
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// SteerRequest contains a steering message.
type SteerRequest struct {
	Message        string `json:"message"`
	ConversationID string `json:"conversation_id"`
	Source         string `json:"source,omitempty"`
}

// FollowUpRequest contains a follow-up message.
type FollowUpRequest struct {
	Message        string `json:"message"`
	ConversationID string `json:"conversation_id"`
	Source         string `json:"source,omitempty"`
}

// QueueStatusRequest queries the current queue state.
type QueueStatusRequest struct {
	ConversationID string `json:"conversation_id"`
}

// QueueStatusResponse is the response from GetQueueStatus.
type QueueStatusResponse struct {
	SteeringDepth int    `json:"steering_depth"`
	FollowUpDepth int    `json:"followup_depth"`
	IsActive      bool   `json:"is_active"`
	Generation    uint64 `json:"generation"`
}

// Steer injects a message into the steering queue of an active agent loop.
func (s *ChatService) Steer(ctx context.Context, req SteerRequest) error {
	if req.Message == "" {
		return wrapError("steer", "Steer", ErrInvalidInput)
	}
	if s.agentRegistry == nil {
		return wrapError("steer", "Steer", ErrUnavailable)
	}
	queue, _ := s.agentRegistry.GetActiveQueue(req.ConversationID)
	if queue == nil {
		return fmt.Errorf("no active agent for conversation %s: %w", req.ConversationID, agent.ErrQueueNotFound)
	}
	if err := queue.Steer(ctx, req.Message, req.Source); err != nil {
		return fmt.Errorf("failed to steer: %w", err)
	}
	return nil
}

// FollowUp injects a message into the follow-up queue of an active agent loop.
func (s *ChatService) FollowUp(ctx context.Context, req FollowUpRequest) error {
	if req.Message == "" {
		return wrapError("followup", "FollowUp", ErrInvalidInput)
	}
	if s.agentRegistry == nil {
		return wrapError("followup", "FollowUp", ErrUnavailable)
	}
	queue, _ := s.agentRegistry.GetActiveQueue(req.ConversationID)
	if queue == nil {
		return fmt.Errorf("no active agent for conversation %s: %w", req.ConversationID, agent.ErrQueueNotFound)
	}
	if err := queue.FollowUp(ctx, req.Message, req.Source); err != nil {
		return fmt.Errorf("failed to enqueue follow-up: %w", err)
	}
	return nil
}

// GetQueueStatus returns the current queue state for a conversation.
func (s *ChatService) GetQueueStatus(ctx context.Context, req QueueStatusRequest) (*QueueStatusResponse, error) {
	if s.agentRegistry == nil {
		return &QueueStatusResponse{
			SteeringDepth: 0,
			FollowUpDepth: 0,
			IsActive:      false,
			Generation:    0,
		}, nil
	}
	queue, _ := s.agentRegistry.GetActiveQueue(req.ConversationID)
	if queue == nil {
		return &QueueStatusResponse{
			SteeringDepth: 0,
			FollowUpDepth: 0,
			IsActive:      false,
			Generation:    0,
		}, nil
	}

	status := queue.Status()
	return &QueueStatusResponse{
		SteeringDepth: status.SteeringDepth,
		FollowUpDepth: status.FollowUpDepth,
		IsActive:      status.IsActive,
		Generation:    status.Generation,
	}, nil
}
