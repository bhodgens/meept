package cluster

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
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

	// conflictResolver arbitrates between two MEMORY_STORED events that
	// target the same memory ID. When nil, the handler falls back to the
	// dual store's INSERT OR REPLACE semantics (last writer wins at the
	// SQL level, but with no event-metadata awareness).
	conflictResolver *ConflictResolver

	// metrics receives conflict-counter increments (Task 4.8). Nil-safe:
	// every IncX helper nil-guards dereferences.
	metrics *Metrics
}

// NewGossipHandler creates a handler that writes gossip-received events
// into the dual-store gossip path so merged reads can surface them.
// The optional resolver enables entity-level conflict resolution for
// MEMORY_STORED events (same memory ID, different payloads from different
// nodes) using last-write-wins by event timestamp + node ID tiebreak.
func NewGossipHandler(dualStore *memory.DualStore, localNode string, logger *slog.Logger, resolver *ConflictResolver) *eventGossipHandler {
	return &eventGossipHandler{
		dualStore:        dualStore,
		localNode:        localNode,
		logger:           logger,
		conflictResolver: resolver,
	}
}

// SetMetrics attaches a Metrics counters struct for conflict-resolution
// observability (Task 4.8). Nil values are ignored per CLAUDE.md
// nil-guard convention.
func (h *eventGossipHandler) SetMetrics(m *Metrics) {
	if m != nil {
		h.metrics = m
	}
}

// SetConflictResolver attaches a conflict resolver to an already-constructed
// handler. Nil values are ignored per CLAUDE.md nil-guard convention.
func (h *eventGossipHandler) SetConflictResolver(r *ConflictResolver) {
	if r != nil {
		h.conflictResolver = r
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
//
// When a ConflictResolver is attached and a memory with the same ID already
// exists in the gossip DB, the resolver decides whether the incoming event
// should replace the existing record (last-write-wins by event timestamp,
// node-ID tiebreak). If the existing record wins, the incoming event is
// dropped. Without a resolver the handler falls back to the dual store's
// INSERT OR REPLACE behavior.
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

	// Entity-level conflict resolution: if a resolver is wired and a memory
	// with the same ID already exists, decide which event wins. The existing
	// row's created_at (RFC3339Nano) and source_node form a synthetic event
	// for comparison against the incoming cluster event.
	if h.conflictResolver != nil && h.dualStore != nil {
		if existing := h.fetchExistingMemoryEvent(payload.ID); existing != nil {
			// Conflict observed — count before resolution.
			if h.metrics != nil {
				h.metrics.IncMergeConflict()
			}
			winner, err := h.conflictResolver.Resolve(event, existing)
			if err != nil {
				h.logger.Warn("gossip handler: conflict resolve failed, falling back to replace",
					"mem_id", payload.ID, "err", err)
			} else if winner != event {
				// Existing event wins — drop the incoming write. "local"
				// winner label means the already-applied record wins.
				if h.metrics != nil {
					h.metrics.IncConflictResolution("local")
				}
				// Existing event wins — drop the incoming write.
				h.logger.Debug("gossip handler: dropping incoming memory event, existing wins",
					"mem_id", payload.ID,
					"incoming_node", event.NodeID,
					"existing_node", existing.NodeID,
					"incoming_ts", event.Timestamp,
					"existing_ts", existing.Timestamp,
				)
				return nil
			}
			// Incoming event won — count remote victory.
			if h.metrics != nil {
				h.metrics.IncConflictResolution("remote")
			}
		}
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

// fetchExistingMemoryEvent reconstructs a synthetic ClusterEvent from a
// previously-applied gossip memory row, so the ConflictResolver can
// compare it against an incoming event. Returns nil if the memory does
// not exist or cannot be read (caller treats nil as "no conflict").
//
// The gossip memories table stores created_at as RFC3339Nano text and
// source_node as the originating node ID, which become the synthetic
// event's Timestamp and NodeID respectively.
func (h *eventGossipHandler) fetchExistingMemoryEvent(memoryID string) *models.ClusterEvent {
	if h.dualStore == nil || memoryID == "" {
		return nil
	}
	db := h.dualStore.GossipDB()
	if db == nil {
		return nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	var createdAtStr, sourceNode string
	err := db.QueryRowContext(ctx,
		`SELECT created_at, source_node FROM memories WHERE id = ?`,
		memoryID,
	).Scan(&createdAtStr, &sourceNode)
	if err != nil {
		if err != sql.ErrNoRows {
			h.logger.Debug("gossip handler: query existing memory failed",
				"mem_id", memoryID, "err", err)
		}
		return nil
	}

	ts, err := time.Parse(time.RFC3339Nano, createdAtStr)
	if err != nil {
		h.logger.Debug("gossip handler: parse existing memory timestamp failed",
			"mem_id", memoryID, "raw_ts", createdAtStr, "err", err)
		return nil
	}

	if sourceNode == "" {
		sourceNode = "unknown"
	}

	return &models.ClusterEvent{
		EventID:   fmt.Sprintf("existing:%s", memoryID),
		NodeID:    sourceNode,
		Timestamp: ts,
	}
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
