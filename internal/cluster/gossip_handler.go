package cluster

import (
	"context"
	"encoding/json"
	"log/slog"
	"time"

	"github.com/caimlas/meept/internal/memory"
	"github.com/caimlas/meept/pkg/models"
)

// eventGossipHandler dispatches incoming cluster events to domain-specific
// handlers (sessions, memories). It is registered on cluster.GossipEngine
// via RegisterHandler and called by emitToHandlers after signature
// verification and persistence.
type eventGossipHandler struct {
	dualStore *memory.DualStore
	localNode string
	logger    *slog.Logger
}

// NewGossipHandler creates a handler that writes gossip-received events
// into the dual-store gossip path so merged reads can surface them.
func NewGossipHandler(dualStore *memory.DualStore, localNode string, logger *slog.Logger) *eventGossipHandler {
	return &eventGossipHandler{
		dualStore: dualStore,
		localNode: localNode,
		logger:    logger,
	}
}

// OnEvent routes a cluster event based on its type.
func (h *eventGossipHandler) OnEvent(event *models.ClusterEvent) error {
	if event == nil {
		return nil
	}

	switch event.EventType {
	case models.EventTypeSessionTurn:
		return h.handleSessionTurn(event)
	case models.EventTypeSessionCreated:
		return nil // no-op: session already created locally
	case models.EventTypeMemoryStored:
		return h.handleMemoryStored(event)
	case models.EventTypeMemoryExpired:
		return nil // no-op: expiration is eventually consistent
	case models.EventTypeMemoryEdge:
		return h.handleMemoryEdge(event)
	default:
		return nil // ignore unknown event types silently
	}
}

// handleSessionTurn converts a SESSION_TURN gossip event into a memory
// record stored in the gossip DB (via StoreRemoteMemory).
func (h *eventGossipHandler) handleSessionTurn(event *models.ClusterEvent) error {
	if event.NodeID == h.localNode {
		return nil // skip events from this node
	}

	if len(event.Payload) == 0 {
		event.Payload = json.RawMessage("{}")
	}

	var payload models.SessionTurnPayload
	if err := json.Unmarshal(event.Payload, &payload); err != nil {
		return &GossipHandlerError{EventID: event.EventID, EventType: string(event.EventType), Err: err}
	}

	turnContent := `[turn:role=` + payload.Role + `] ` + payload.Content

	mem := &memory.Memory{
		ID:        "gossip-turn-" + event.EventID,
		Type:      memory.MemoryType("episode"),
		Category:  "session_turn",
		Content:   turnContent,
		CreatedAt: time.Unix(0, payload.Timestamp).UTC(),
		AgentID:   event.NodeID,
		SessionID: payload.SessionID,
		Metadata: map[string]any{
			"turn_id": payload.TurnID,
			"source_node": event.NodeID,
			"gossip_event_id": event.EventID,
		},
	}

	if h.dualStore != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := h.dualStore.StoreRemoteMemory(ctx, mem, event.NodeID); err != nil {
			h.logger.Warn("gossip handler: store remote turn failed", "turn_id", payload.TurnID, "err", err)
		}
	}

	return nil
}

// handleMemoryStored re-stores a MEMORY_STORED gossip event via
// StoreRemoteMemory so the gossip DB accumulates peer memories.
func (h *eventGossipHandler) handleMemoryStored(event *models.ClusterEvent) error {
	if event.NodeID == h.localNode {
		return nil
	}

	if len(event.Payload) == 0 {
		event.Payload = json.RawMessage("{}")
	}

	var payload models.MemoryStoredPayload
	if err := json.Unmarshal(event.Payload, &payload); err != nil {
		return &GossipHandlerError{EventID: event.EventID, EventType: string(event.EventType), Err: err}
	}

	mem := &memory.Memory{
		ID:        payload.ID,
		Type:      memory.MemoryType(payload.Type),
		Category:  payload.Category,
		Content:   payload.Content,
		CreatedAt: time.Unix(0, payload.CreatedAt).UTC(),
		AgentID:   payload.AgentID,
		SessionID: payload.SessionID,
		Metadata:  payload.Metadata,
	}

	if h.dualStore != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := h.dualStore.StoreRemoteMemory(ctx, mem, event.NodeID); err != nil {
			h.logger.Warn("gossip handler: store remote memory failed", "mem_id", payload.ID, "err", err)
		}
	}

	return nil
}

// handleMemoryEdge is a no-op placeholder for MEMORY_EDGE events.
// Epistemic edge gossip can be propagated later when the memory package
// adds a dedicated edge table.
func (h *eventGossipHandler) handleMemoryEdge(event *models.ClusterEvent) error {
	if len(event.Payload) == 0 {
		event.Payload = json.RawMessage("{}")
	}
	h.logger.Debug("gossip handler: MEMORY_EDGE event received",
		"event_id", event.EventID)
	return nil
}

// GossipHandlerError wraps errors from GossipHandler methods.
type GossipHandlerError struct {
	EventID   string
	EventType string
	Err       error
}

func (e *GossipHandlerError) Error() string {
	return "gossip handler (" + e.EventType + "): " + e.Err.Error()
}

func (e *GossipHandlerError) Unwrap() error {
	return e.Err
}
