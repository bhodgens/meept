package services

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/caimlas/meept/internal/bus"
	"github.com/caimlas/meept/pkg/models"
)

// ChatService handles chat operations.
type ChatService struct {
	bus    *bus.MessageBus
	logger *slog.Logger
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
func NewChatService(msgBus *bus.MessageBus, logger *slog.Logger) *ChatService {
	if logger == nil {
		logger = slog.Default()
	}
	return &ChatService{
		bus:    msgBus,
		logger: logger,
	}
}

// Chat sends a message and waits for a response.
func (s *ChatService) Chat(ctx context.Context, req ChatRequest) (*ChatResponse, error) {
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
