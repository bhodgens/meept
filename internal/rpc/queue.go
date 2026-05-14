package rpc

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/caimlas/meept/internal/agent"
)

// QueueHandler provides native RPC methods for steering and follow-up queue
// operations. It calls MessageQueue directly so that CLI and TUI commands
// reach active agent loops even when no bus subscriber is running.
type QueueHandler struct {
	registry *agent.AgentRegistry
}

// NewQueueHandler creates a new handler. If registry is nil the registered
// methods return "queue feature not enabled" errors.
func NewQueueHandler(reg *agent.AgentRegistry) *QueueHandler {
	return &QueueHandler{registry: reg}
}

// RegisterQueueMethods registers queue RPC methods on the server.
func (h *QueueHandler) RegisterQueueMethods(server *Server) {
	server.RegisterHandler("chat.steer", h.handleSteer)
	server.RegisterHandler("chat.followup", h.handleFollowUp)
	server.RegisterHandler("chat.queue_status", h.handleStatus)
	server.RegisterHandler("chat.queue.restore", h.handleRestore)
}

func (h *QueueHandler) reg() (*agent.AgentRegistry, error) {
	if h.registry == nil {
		return nil, fmt.Errorf("queue feature not enabled")
	}
	return h.registry, nil
}

// handleSteer handles chat.steer RPC calls.
func (h *QueueHandler) handleSteer(ctx context.Context, params json.RawMessage) (any, error) {
	reg, err := h.reg()
	if err != nil {
		return nil, err
	}

	var req struct {
		Message        string `json:"message"`
		ConversationID string `json:"conversation_id"`
		Source         string `json:"source,omitempty"`
	}
	if err := json.Unmarshal(params, &req); err != nil {
		return nil, fmt.Errorf("invalid params: %w", err)
	}
	if req.Message == "" {
		return nil, fmt.Errorf("message is required")
	}
	if req.ConversationID == "" {
		return nil, fmt.Errorf("conversation_id is required")
	}

	q, _ := reg.GetActiveQueue(req.ConversationID)
	if q == nil {
		return nil, agent.ErrQueueNotFound
	}

	if err := q.Steer(ctx, req.Message, req.Source); err != nil {
		return nil, fmt.Errorf("steer failed: %w", err)
	}

	return map[string]any{
		RPCKeyStatus: "queued",
		"queue":  "steer",
	}, nil
}

// handleFollowUp handles chat.followup RPC calls.
func (h *QueueHandler) handleFollowUp(ctx context.Context, params json.RawMessage) (any, error) {
	reg, err := h.reg()
	if err != nil {
		return nil, err
	}

	var req struct {
		Message        string `json:"message"`
		ConversationID string `json:"conversation_id"`
		Source         string `json:"source,omitempty"`
	}
	if err := json.Unmarshal(params, &req); err != nil {
		return nil, fmt.Errorf("invalid params: %w", err)
	}
	if req.Message == "" {
		return nil, fmt.Errorf("message is required")
	}
	if req.ConversationID == "" {
		return nil, fmt.Errorf("conversation_id is required")
	}

	q, _ := reg.GetActiveQueue(req.ConversationID)
	if q == nil {
		return nil, agent.ErrQueueNotFound
	}

	if err := q.FollowUp(ctx, req.Message, req.Source); err != nil {
		return nil, fmt.Errorf("follow-up failed: %w", err)
	}

	return map[string]any{
		RPCKeyStatus: "queued",
		"queue":  "followup",
	}, nil
}

// handleStatus handles chat.queue_status RPC calls.
func (h *QueueHandler) handleStatus(ctx context.Context, params json.RawMessage) (any, error) {
	reg, err := h.reg()
	if err != nil {
		return nil, err
	}

	var req struct {
		ConversationID string `json:"conversation_id"`
	}
	if err := json.Unmarshal(params, &req); err != nil {
		return nil, fmt.Errorf("invalid params: %w", err)
	}
	if req.ConversationID == "" {
		return nil, fmt.Errorf("conversation_id is required")
	}

	q, _ := reg.GetActiveQueue(req.ConversationID)
	if q == nil {
		return map[string]any{
			"steering_depth":  0,
			"followup_depth":  0,
			"is_active":       false,
			"generation":      uint64(0),
		}, nil
	}

	status := q.Status()
	return map[string]any{
		"steering_depth": status.SteeringDepth,
		"followup_depth": status.FollowUpDepth,
		"is_active":      status.IsActive,
		"generation":     status.Generation,
	}, nil
}

// handleRestore handles chat.queue.restore RPC calls.
// It returns the current status of the queue for a conversation, which is
// used by the TUI to check whether pending follow-ups were restored on
// daemon startup.
func (h *QueueHandler) handleRestore(ctx context.Context, params json.RawMessage) (any, error) {
	reg, err := h.reg()
	if err != nil {
		return nil, err
	}

	var req struct {
		ConversationID string `json:"conversation_id"`
	}
	if err := json.Unmarshal(params, &req); err != nil {
		return nil, fmt.Errorf("invalid params: %w", err)
	}
	if req.ConversationID == "" {
		return nil, fmt.Errorf("conversation_id is required")
	}

	q, _ := reg.GetActiveQueue(req.ConversationID)
	if q == nil {
		return map[string]any{
			"restored": 0,
			"active":   false,
		}, nil
	}

	status := q.Status()
	return map[string]any{
		"restored": status.FollowUpDepth,
		"active":   status.IsActive,
	}, nil
}
