# Phase 4: Gossip Event Schema - Implementation Plan

**Spec:** `docs/superpowers/specs/2026-06-26-config-backup-sync-design.md`
**Date:** 2026-06-26
**Status:** Ready for implementation
**Prerequisites:** Phase 3 (Dual-DB Router) complete

---

## Overview

This plan extends the existing gossip protocol with new event types for sessions and memories, enabling real-time bilateral sync in cluster deployments. Builds on the existing `internal/cluster/gossip.go` infrastructure.

### Scope

| In Scope | Out of Scope |
|----------|--------------|
| New event types (SESSION_TURN, MEMORY_STORED) | Config sync (Phase 5) |
| Payload schema definitions | Backup scheduler (Phase 1) |
| Idempotent merge handlers | Migration tool (Phase 3) |
| Vector clock support | |

---

## Phase 4 Checklist

### Task 4.1: Event Type Extensions

**File:** `pkg/models/cluster.go`

```go
// Add to existing ClusterEventType enum
const (
    // ... existing event types ...

    // Session events
    EventTypeSessionCreated ClusterEventType = "SESSION_CREATED"
    EventTypeSessionTurn    ClusterEventType = "SESSION_TURN"

    // Memory events
    EventTypeMemoryStored   ClusterEventType = "MEMORY_STORED"
    EventTypeMemoryExpired  ClusterEventType = "MEMORY_EXPIRED"
    EventTypeMemoryEdge     ClusterEventType = "MEMORY_EDGE"  // Epistemic links
)

// SessionTurnPayload defines the payload for SESSION_TURN events
type SessionTurnPayload struct {
    SessionID   string `json:"session_id"`
    TurnID      string `json:"turn_id"`
    Role        string `json:"role"`
    Content     string `json:"content"`
    Timestamp   int64  `json:"timestamp"`
    SessionMeta []byte `json:"session_meta,omitempty"`  // If session is new
}

// MemoryStoredPayload defines the payload for MEMORY_STORED events
type MemoryStoredPayload struct {
    ID        string            `json:"id"`
    Type      MemoryType        `json:"type"`
    Category  string            `json:"category"`
    Content   string            `json:"content"`
    CreatedAt int64             `json:"created_at"`
    AgentID   string            `json:"agent_id,omitempty"`
    SessionID string            `json:"session_id,omitempty"`
    Metadata  map[string]any    `json:"metadata,omitempty"`
}

// MemoryEdgePayload defines epistemic relationship between memories
type MemoryEdgePayload struct {
    FromID      string     `json:"from_id"`
    ToID        string     `json:"to_id"`
    EdgeType    EdgeType   `json:"edge_type"`  // contradicts, supports, supersedes
    Established int64      `json:"established"`
}
```

**Tests:** `pkg/models/cluster_test.go`

---

### Task 4.2: Gossip Publish Integration

**File:** `internal/memory/dual_store.go` (update)

Add gossip publisher hook:
```go
type DualStore struct {
    // ... existing fields ...
    gossipPub GossipPublisher  // Interface to publish events
}

// SetGossipPublisher sets the gossip publisher for cluster sync
func (s *DualStore) SetGossipPublisher(pub GossipPublisher) {
    s.gossipPub = pub
}

// GossipPublisher interface (avoids import cycle)
type GossipPublisher interface {
    Publish(eventType ClusterEventType, payload any) error
}
```

**Update StoreTurn to publish:**
```go
func (s *DualStore) StoreTurn(ctx context.Context, turn *Turn) error {
    s.mu.Lock()
    defer s.mu.Unlock()

    // ... existing write logic ...

    // Publish to gossip (non-blocking)
    if s.gossipPub != nil && sourceNode == s.localNodeID {
        payload := SessionTurnPayload{
            SessionID: turn.SessionID,
            TurnID:    turn.TurnID,
            Role:      turn.Role,
            Content:   turn.Content,
            Timestamp: turn.Timestamp,
        }
        go func() {
            err := s.gossipPub.Publish(EventTypeSessionTurn, payload)
            if err != nil {
                s.logger.Warn("failed to publish turn event", "error", err)
            }
        }()
    }

    return nil
}
```

**Tests:** `internal/memory/dual_store_gossip_test.go`

---

### Task 4.3: Gossip Receive Handlers

**File:** `internal/cluster/gossip_handler.go` (new)

```go
package cluster

// GossipHandler handles incoming gossip events
type GossipHandler struct {
    dualStore *memory.DualStore
    logger    *slog.Logger
}

func NewGossipHandler(dualStore *memory.DualStore, logger *slog.Logger) *GossipHandler

// OnEvent handles incoming cluster events
func (h *GossipHandler) OnEvent(event *ClusterEvent) error {
    switch event.EventType {
    case "SESSION_TURN":
        return h.handleSessionTurn(event)
    case "MEMORY_STORED":
        return h.handleMemoryStored(event)
    case "MEMORY_EXPIRED":
        return h.handleMemoryExpired(event)
    case "MEMORY_EDGE":
        return h.handleMemoryEdge(event)
    default:
        return nil  // Let other handlers process
    }
}

func (h *GossipHandler) handleSessionTurn(event *ClusterEvent) error {
    var payload SessionTurnPayload
    if err := json.Unmarshal(event.Payload, &payload); err != nil {
        return fmt.Errorf("failed to unmarshal session turn payload: %w", err)
    }

    // Verify signature (existing gossip handles this)
    if !verifyEventSignature(event) {
        return errors.New("invalid event signature")
    }

    // Insert into gossip.db (idempotent)
    turn := &Turn{
        TurnID:    payload.TurnID,
        SessionID: payload.SessionID,
        Role:      payload.Role,
        Content:   payload.Content,
        Timestamp: payload.Timestamp,
    }

    return h.dualStore.StoreRemoteTurn(context.Background(), turn, event.NodeID)
}

func (h *GossipHandler) handleMemoryStored(event *ClusterEvent) error {
    var payload MemoryStoredPayload
    if err := json.Unmarshal(event.Payload, &payload); err != nil {
        return fmt.Errorf("failed to unmarshal memory payload: %w", err)
    }

    memory := &memory.Memory{
        ID:        payload.ID,
        Type:      payload.Type,
        Category:  payload.Category,
        Content:   payload.Content,
        CreatedAt: time.Unix(0, payload.CreatedAt),
        AgentID:   payload.AgentID,
        SessionID: payload.SessionID,
        Metadata:  payload.Metadata,
    }

    return h.dualStore.StoreRemoteMemory(context.Background(), memory, event.NodeID)
}
```

**Tests:** `internal/cluster/gossip_handler_test.go`

---

### Task 4.4: Event Deduplication

**File:** `internal/cluster/engine.go` (update)

Add event dedup check before processing:
```go
// processEvent handles a single event from a peer
func (e *Engine) processEvent(event *ClusterEvent) error {
    // Check for duplicate (event_id already in local DB)
    exists, err := e.eventExists(event.EventID)
    if err != nil {
        return fmt.Errorf("failed to check event existence: %w", err)
    }
    if exists {
        return nil  // Already processed, skip
    }

    // Call registered handlers
    for _, handler := range e.handlers {
        if err := handler.OnEvent(event); err != nil {
            return err
        }
    }

    // Mark as processed
    return e.markEventProcessed(event)
}

func (e *Engine) eventExists(eventID string) (bool, error) {
    var exists bool
    err := e.db.QueryRow("SELECT EXISTS(SELECT 1 FROM cluster_events WHERE event_id = ?)", eventID).Scan(&exists)
    return exists, err
}

func (e *Engine) markEventProcessed(event *ClusterEvent) error {
    _, err := e.db.Exec(`
        INSERT OR REPLACE INTO cluster_events
        (event_id, node_id, event_type, timestamp, received_at)
        VALUES (?, ?, ?, ?, ?)
    `, event.EventID, event.NodeID, event.EventType, event.Timestamp, time.Now().UnixNano())
    return err
}
```

---

### Task 4.5: Vector Clock Support

**File:** `pkg/models/cluster.go` (update)

```go
// ClusterEvent already has VectorClock field, ensure it's populated
type ClusterEvent struct {
    EventID     string            `json:"event_id"`
    NodeID      string            `json:"node_id"`
    EventType   ClusterEventType  `json:"event_type"`
    Timestamp   int64             `json:"timestamp"`
    VectorClock map[string]int64  `json:"vector_clock"`  // node_id -> counter
    Payload     json.RawMessage   `json:"payload"`
    Signature   []byte            `json:"signature"`
}

// VectorClock manages causal ordering
type VectorClock struct {
    clocks map[string]int64
    nodeID string
}

func NewVectorClock(nodeID string) *VectorClock
func (vc *VectorClock) Increment()
func (vc *VectorClock) Update(other map[string]int64)
func (vc *VectorClock) ToMap() map[string]int64
func (vc *VectorClock) Compare(other map[string]int64) ClockComparison
```

**Update gossip engine to maintain vector clocks:**
```go
// On event publish
event.VectorClock = e.vectorClock.ToMap()

// On event receive
e.vectorClock.Update(event.VectorClock)
```

---

### Task 4.6: Gossip Engine Wiring

**File:** `internal/daemon/components.go`

Wire gossip handler:
```go
func (c *Components) wireGossip() error {
    // ... existing gossip engine init ...

    // Create and register session/memory handler
    handler := cluster.NewGossipHandler(c.dualStore, c.logger)
    c.clusterEngine.RegisterHandler(handler)

    // Set gossip publisher on dual store (for bidirectional flow)
    c.dualStore.SetGossipPublisher(c.clusterEngine)

    return nil
}
```

**Tests:** `internal/daemon/gossip_wiring_test.go`

---

### Task 4.7: Conflict Resolution

**File:** `internal/cluster/conflict.go` (new)

```go
package cluster

// ConflictResolver handles conflicting events
type ConflictResolver struct {
    logger *slog.Logger
}

// Resolve handles conflicts based on event type
func (r *ConflictResolver) Resolve(event1, event2 *ClusterEvent) (*ClusterEvent, error) {
    // Last-write-wins by timestamp for most events
    if event1.Timestamp > event2.Timestamp {
        return event1, nil
    }
    return event2, nil
}

// For vector clock conflicts, use causal ordering
func resolveByVectorClock(event1, event2 *ClusterEvent) (*ClusterEvent, error) {
    // If event1 happened-before event2, use event2
    // If concurrent, use node ID as tiebreaker
    // Implementation depends on vector clock comparison
}
```

---

### Task 4.8: Observability

**File:** `internal/cluster/metrics.go` (update)

Add gossip-specific metrics:
```go
// Existing metrics
var (
    gossipEventsSent    = prometheus.NewCounter(...)
    gossipEventsRecv    = prometheus.NewCounter(...)
    gossipEventsDropped = prometheus.NewCounter(...)
)

// New metrics for session/memory events
var (
    sessionTurnsPublished = prometheus.NewCounterVec(
        prometheus.CounterOpts{
            Name: "gossip_session_turns_published_total",
            Help: "Total number of session turn events published",
        },
        []string{"node_id"},
    )

    memoriesPublished = prometheus.NewCounterVec(
        prometheus.CounterOpts{
            Name: "gossip_memories_published_total",
            Help: "Total number of memory events published",
        },
        []string{"memory_type"},
    )

    mergeConflicts = prometheus.NewCounter(
        prometheus.CounterOpts{
            Name: "gossip_merge_conflicts_total",
            Help: "Total number of merge conflicts resolved",
        },
    )
)
```

---

### Task 4.9: Unit Tests

**Files:**
- `pkg/models/cluster_test.go` (new event types)
- `internal/cluster/gossip_handler_test.go`
- `internal/cluster/conflict_test.go`
- `internal/memory/dual_store_gossip_test.go`

**Coverage targets:**
- Event handlers: 95%+
- Conflict resolution: 90%+
- Vector clock: 95%+

---

## Acceptance Criteria

- [ ] SESSION_TURN and MEMORY_STORED event types defined
- [ ] Payload schemas implemented with JSON marshaling
- [ ] Gossip publish integration working (dual store publishes on write)
- [ ] Gossip receive handlers merge events into gossip.db
- [ ] Event deduplication prevents duplicate processing
- [ ] Vector clocks maintained for causal ordering
- [ ] Gossip handler wired in daemon
- [ ] Conflict resolution implemented (last-write-wins)
- [ ] Metrics published for session/memory events
- [ ] All unit tests pass with >90% coverage

---

## Configuration Example

```json5
// ~/.meept/meept.json5
{
  cluster: {
    enabled: true,
    // ... existing cluster config ...
  },
  // Gossip enabled automatically when cluster enabled
}
```

---

## Dependencies

| Package | Purpose |
|---------|---------|
| `internal/cluster` | Existing gossip engine |
| `pkg/models` | Cluster event types |
| `internal/memory` | Dual-store routing |

---

## Risk Mitigation

| Risk | Mitigation |
|------|------------|
| Gossip floods network with events | Batch events, rate-limit publishes |
| Merge creates duplicates | INSERT OR IGNORE with unique constraints |
| Vector clock drift | Regular vector clock sync with peers |
| Handler panics crash gossip | Recover in handler wrapper, log errors |

---

## Estimated Effort

**Total tasks:** 9
**Estimated time:** 10-14 hours
**Complexity:** High (distributed systems, causal ordering)

---

*This plan implements Phase 4 of 7 from the backup/sync design spec.*
