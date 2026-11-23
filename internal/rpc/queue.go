package rpc

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/caimlas/meept/internal/agent"
	"github.com/caimlas/meept/internal/errcls"
	"github.com/caimlas/meept/internal/session"
)

// errAmbiguousQueueParam marks an ErrAmbiguousQueue resolution failure as a
// caller-correctable parameter error so the JSON-RPC boundary answers
// -32602 InvalidParams (the RPC analogue of HTTP 400) with the resolver's
// actionable text. Registered once with errcls; true not-found keeps its
// ErrQueueNotFound sentinel so clients still see a not-found outcome.
var errAmbiguousQueueParam = errors.New("ambiguous queue target")

func init() {
	errcls.RegisterParameterSentinels(errAmbiguousQueueParam)
}

// queueResolutionError maps a resolveQueue failure to a client-actionable
// error. An ambiguous multi-thread match is NOT a missing queue: it means the
// caller addressed the session-level id while several live thread queues
// match, so it must surface the resolver's "use the full thread id" guidance
// (as a parameter error) rather than being flattened into ErrQueueNotFound.
// Any true not-found (or a nil queue with no error) keeps ErrQueueNotFound.
func queueResolutionError(err error) error {
	if errors.Is(err, agent.ErrAmbiguousQueue) {
		return fmt.Errorf("%w: %w", errAmbiguousQueueParam, err)
	}
	return agent.ErrQueueNotFound
}

// queueSessionStore is the slice of session.Store behavior queue
// resolution needs (identity canonicalization, 20261121-interactive-usability
// leaf 01): any of {primary session id, conversation id, thread-scoped
// conversation id} must find the live queue. Defined locally so the handler
// keeps no hard dependency on session wiring details; *session.Store
// satisfies it structurally.
type queueSessionStore interface {
	Get(id string) *session.Session
	GetByConversationID(conversationID string) *session.Session
}

// QueueHandler provides native RPC methods for steering and follow-up queue
// operations. It calls MessageQueue directly so that CLI and TUI commands
// reach active agent loops even when no bus subscriber is running.
type QueueHandler struct {
	registry     *agent.AgentRegistry
	sessionStore queueSessionStore // optional; nil disables session-id resolution
}

// NewQueueHandler creates a new handler. If registry is nil the registered
// methods return "queue feature not enabled" errors.
func NewQueueHandler(reg *agent.AgentRegistry) *QueueHandler {
	return &QueueHandler{registry: reg}
}

// SetSessionStore wires the session store used to resolve a raw session id
// (or a thread-scoped conversation id) to the session's owning conversation
// id before the queue lookup. Nil is a no-op.
func (h *QueueHandler) SetSessionStore(s queueSessionStore) {
	if s != nil {
		h.sessionStore = s
	}
}

// resolveQueue resolves the live queue for the request's conversation id
// through the canonical identity chain: exact queue key -> session/conversation
// resolution -> thread-key remap (C3). Nil store keeps the queue-key-only
// resolution.
func (h *QueueHandler) resolveQueue(reg *agent.AgentRegistry, conversationID string) (*agent.MessageQueue, error) {
	if h.sessionStore != nil {
		return reg.ResolveActiveQueueWithSession(
			conversationID,
			h.sessionStore.GetByConversationID,
			h.sessionStore.Get,
		)
	}
	return reg.ResolveActiveQueue(conversationID)
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

	q, err := h.resolveQueue(reg, req.ConversationID)
	if err != nil || q == nil {
		return nil, queueResolutionError(err)
	}

	if err := q.Steer(ctx, req.Message, req.Source); err != nil {
		return nil, fmt.Errorf("steer failed: %w", err)
	}

	return map[string]any{
		RPCKeyStatus: "queued",
		"queue":      "steer",
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

	q, err := h.resolveQueue(reg, req.ConversationID)
	if err != nil || q == nil {
		return nil, queueResolutionError(err)
	}

	if err := q.FollowUp(ctx, req.Message, req.Source); err != nil {
		return nil, fmt.Errorf("follow-up failed: %w", err)
	}

	return map[string]any{
		RPCKeyStatus: "queued",
		"queue":      "followup",
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

	// Identity canonicalization: resolve through the session-aware chain
	// (raw session id / thread-scoped id -> owning conversation id).
	// A queue found under a thread-scoped key reports that queue's own
	// generation via Status() below; no fabricated generation is needed.
	q, resolveErr := h.resolveQueue(reg, req.ConversationID)
	if errors.Is(resolveErr, agent.ErrAmbiguousQueue) {
		// Ambiguous is not "no queue": tell the caller which id shape to
		// use instead of silently reporting an inactive/zero queue.
		return nil, queueResolutionError(resolveErr)
	}

	// B5 / leaf 05-navigation/03 T2: parked work must be visible even
	// with no active loop. task_states lists the conversation's
	// non-terminal tasks (session-scoped via the task store's
	// session_tasks join; a thread-scoped conversation id is unwrapped
	// to its session-level base first). The lookup is bounded to the
	// session's linked tasks - never a full task-table scan.
	taskStates := sessionParkedTaskStates(reg, req.ConversationID)

	if q == nil {
		return map[string]any{
			"steering_depth": 0,
			"followup_depth": 0,
			"is_active":      false,
			"generation":     uint64(0),
			"task_states":    taskStates,
		}, nil
	}

	status := q.Status()
	return map[string]any{
		"steering_depth": status.SteeringDepth,
		"followup_depth": status.FollowUpDepth,
		"is_active":      status.IsActive,
		"generation":     status.Generation,
		"task_states":    taskStates,
	}, nil
}

// sessionParkedTaskStates answers queue_status's task_states rows:
// {task_id,state,name} for every non-terminal task linked to the
// session behind conversationID. Returns an empty (non-nil) slice when
// no task store is wired, the session has no linked tasks, or the
// lookup fails - parked visibility is additive and must never turn a
// queue_status poll into an error.
func sessionParkedTaskStates(reg *agent.AgentRegistry, conversationID string) []map[string]any {
	states := []map[string]any{}
	store := reg.TaskStore()
	if store == nil {
		return states
	}
	base := agent.UnwrapThreadConversationID(conversationID)
	tasks, err := store.GetTasksForSession(base)
	if err != nil {
		return states
	}
	for _, t := range tasks {
		if t == nil || t.State.IsTerminal() {
			continue
		}
		states = append(states, map[string]any{
			"task_id": t.ID,
			"state":   string(t.State),
			"name":    t.Name,
		})
	}
	return states
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

	q, err := h.resolveQueue(reg, req.ConversationID)
	if err != nil || q == nil {
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
