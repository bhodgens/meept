package services

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/caimlas/meept/internal/agent"
	"github.com/caimlas/meept/internal/bus"
	"github.com/caimlas/meept/internal/llm"
	"github.com/caimlas/meept/internal/session"
	"github.com/caimlas/meept/pkg/id"
	"github.com/caimlas/meept/pkg/models"
)

// ChatService handles chat operations.
type ChatService struct {
	bus           *bus.MessageBus
	agentRegistry *agent.AgentRegistry
	sessionStore  session.Store
	logger        *slog.Logger
	timeout       time.Duration
}

// ChatRequest contains chat input.
type ChatRequest struct {
	Message        string            `json:"message"`
	Parts          []llm.ContentPart `json:"parts,omitempty"`
	ConversationID string            `json:"conversation_id"`
	AgentID        string            `json:"agent_id,omitempty"`
}

// ChatResponse contains chat output.
type ChatResponse struct {
	Reply      string `json:"reply"`
	Model      string `json:"model,omitempty"`
	TokensUsed int    `json:"tokens_used,omitempty"`
	Error      string `json:"error,omitempty"`
}

// NewChatService creates a chat service.
func NewChatService(msgBus *bus.MessageBus, agentReg *agent.AgentRegistry, logger *slog.Logger, opts ...ChatServiceOption) *ChatService {
	if logger == nil {
		logger = slog.Default()
	}
	s := &ChatService{
		bus:           msgBus,
		agentRegistry: agentReg,
		logger:        logger,
	}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

// ChatServiceOption configures a ChatService.
type ChatServiceOption func(*ChatService)

// WithSessionStore sets the session store for conversation ID resolution.
func WithSessionStore(store session.Store) ChatServiceOption {
	return func(s *ChatService) {
		s.sessionStore = store
	}
}

// WithChatTimeout sets the response wait timeout for chat requests.
// If d is zero or negative, the default (2 minutes) is used.
func WithChatTimeout(d time.Duration) ChatServiceOption {
	return func(s *ChatService) {
		if d > 0 {
			s.timeout = d
		}
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

	conversationID := req.ConversationID

	// Resolve session ID to the session's internal conversation ID so that
	// agent-side persistence (persistConversation) writes to the same
	// session the client is viewing.
	if s.sessionStore != nil {
		if sess := s.sessionStore.Get(req.ConversationID); sess != nil && sess.ConversationID != "" {
			conversationID = sess.ConversationID
			s.logger.Debug("resolved conversation_id from session store",
				"session_id", req.ConversationID,
				"conversation_id", conversationID,
			)
		}
	}

	// Create request message
	// Log agent ID if provided by client
	if req.AgentID != "" {
		s.logger.Info("Chat request with agent override", "agent_id", req.AgentID, "conversation_id", conversationID)
	}

	msgID := id.Generate("svc-chat-")
	payload := map[string]any{
		"message":         req.Message,
		"conversation_id": conversationID,
		"agent_id":        req.AgentID,
	}
	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		// Marshal failures on a simple map literal are extremely unlikely,
		// but if it happens, fail loudly rather than publishing an empty
		// payload that hangs the request until the 2-minute timeout.
		return nil, wrapError("chat", "Chat", fmt.Errorf("marshal chat payload: %w", err))
	}
	msg := &models.BusMessage{
		ID:      msgID,
		Type:    models.MessageTypeRequest,
		Topic:   "chat.request",
		Source:  "svc.chat",
		Payload: payloadBytes,
		ReplyTo: "chat.response",
	}

	// Create response channel
	respChan := make(chan *models.BusMessage, 1)
	// FIX: subscribe to the same topic the ChatHandler publishes on
	replyTopic := "chat.response"
	sub := s.bus.Subscribe(msgID, replyTopic)
	defer s.bus.Unsubscribe(sub)

	// Watch for responses (context-aware, with deterministic cleanup)
	watcherCtx, cancelWatcher := context.WithCancel(ctx)
	var watcherWG sync.WaitGroup
	watcherWG.Add(1)

	go func() {
		defer watcherWG.Done()
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
			case <-watcherCtx.Done():
				return
			}
		}
	}()

	// Ensure the watcher goroutine exits before we return.
	defer func() {
		cancelWatcher()
		watcherWG.Wait()
	}()

	// Publish request
	s.bus.Publish("chat.request", msg)

	// Wait for response
	timeout := s.timeout
	if timeout <= 0 {
		timeout = 2 * time.Minute
	}
	timer := time.NewTimer(timeout)
	defer timer.Stop()

	select {
	case resp := <-respChan:
		var reply struct {
			Reply      string `json:"reply"`
			Model      string `json:"model,omitempty"`
			TokensUsed int    `json:"tokens_used,omitempty"`
			Error      string `json:"error,omitempty"`
		}
		if err := json.Unmarshal(resp.Payload, &reply); err != nil {
			return &ChatResponse{Reply: string(resp.Payload)}, nil
		}
		return &ChatResponse{
			Reply:      reply.Reply,
			Model:      reply.Model,
			TokensUsed: reply.TokensUsed,
			Error:      reply.Error,
		}, nil
	case <-timer.C:
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
