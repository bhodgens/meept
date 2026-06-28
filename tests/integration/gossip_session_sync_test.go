package integration

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"path/filepath"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/cluster"
	"github.com/caimlas/meept/internal/memory"
	"github.com/caimlas/meept/pkg/id"
	"github.com/caimlas/meept/pkg/models"
)

// makeGossipEvent constructs a ClusterEvent suitable for GossipHandler.OnEvent.
// It marshals the provided payload into the event's Payload field.
func makeGossipEvent(t *testing.T, eventType models.ClusterEventType, nodeID string, ts time.Time, payload any) *models.ClusterEvent {
	t.Helper()
	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}
	return &models.ClusterEvent{
		EventID:   id.Generate("evt-"),
		NodeID:    nodeID,
		EventType: eventType,
		Timestamp: ts,
		Payload:   payloadBytes,
	}
}

// TestGossipHandler_SessionTurn_PersistsToGossipDB verifies that a SESSION_TURN
// gossip event from a peer node is persisted as a memory record in the gossip DB
// via the dual store.
func TestGossipHandler_SessionTurn_PersistsToGossipDB(t *testing.T) {
	t.Parallel()

	tmp := t.TempDir()
	store, err := memory.NewDualStore(tmp, "node-local", newTestLogger())
	if err != nil {
		t.Fatalf("NewDualStore: %v", err)
	}
	defer store.Close()

	handler := cluster.NewGossipHandler(store, "node-local", newTestLogger(), nil)

	now := time.Now().UTC()
	payload := models.SessionTurnPayload{
		SessionID: id.Generate("sess-"),
		TurnID:    id.Generate("turn-"),
		Role:      "user",
		Content:   "hello from peer",
		Timestamp: now.UnixNano(),
	}
	event := makeGossipEvent(t, models.EventTypeSessionTurn, "node-peer", now, payload)

	if err := handler.OnEvent(event); err != nil {
		t.Fatalf("OnEvent: %v", err)
	}

	// Verify the gossip DB has one row in memories with source_node = node-peer.
	// The handler stores SESSION_TURN as an episodic memory via StoreRemoteMemory.
	ctx := context.Background()
	expectedID := "gossip-turn-" + event.EventID
	var count int
	if err := store.GossipDB().QueryRowContext(ctx,
		`SELECT COUNT(*) FROM memories WHERE id = ? AND source_node = ?`,
		expectedID, "node-peer",
	).Scan(&count); err != nil {
		t.Fatalf("query gossip memories: %v", err)
	}
	if count != 1 {
		t.Errorf("gossip memories count = %d, want 1 (eventID=%s)", count, expectedID)
	}
}

// TestGossipHandler_SessionTurn_SkipsLocalOrigin verifies that SESSION_TURN
// events originating from this node are dropped (no-op) by the handler.
func TestGossipHandler_SessionTurn_SkipsLocalOrigin(t *testing.T) {
	t.Parallel()

	localNode := "node-local"
	tmp := t.TempDir()
	store, err := memory.NewDualStore(tmp, localNode, newTestLogger())
	if err != nil {
		t.Fatalf("NewDualStore: %v", err)
	}
	defer store.Close()

	handler := cluster.NewGossipHandler(store, localNode, newTestLogger(), nil)

	now := time.Now().UTC()
	payload := models.SessionTurnPayload{
		SessionID: id.Generate("sess-"),
		TurnID:    id.Generate("turn-"),
		Role:      "user",
		Content:   "self-echo",
		Timestamp: now.UnixNano(),
	}
	event := makeGossipEvent(t, models.EventTypeSessionTurn, localNode, now, payload)

	if err := handler.OnEvent(event); err != nil {
		t.Fatalf("OnEvent: %v", err)
	}

	// No memory should have been written.
	ctx := context.Background()
	expectedID := "gossip-turn-" + event.EventID
	var count int
	if err := store.GossipDB().QueryRowContext(ctx,
		`SELECT COUNT(*) FROM memories WHERE id = ?`, expectedID,
	).Scan(&count); err != nil {
		t.Fatalf("query gossip: %v", err)
	}
	if count != 0 {
		t.Errorf("gossip memories count = %d, want 0 (local-origin events must be skipped)", count)
	}
}

// TestGossipHandler_MemoryStored_PersistsWithSourceNode verifies that a
// MEMORY_STORED event from a peer is persisted in the gossip DB with the
// correct source_node attribution.
func TestGossipHandler_MemoryStored_PersistsWithSourceNode(t *testing.T) {
	t.Parallel()

	tmp := t.TempDir()
	store, err := memory.NewDualStore(tmp, "node-local", newTestLogger())
	if err != nil {
		t.Fatalf("NewDualStore: %v", err)
	}
	defer store.Close()

	handler := cluster.NewGossipHandler(store, "node-local", newTestLogger(), nil)

	memID := id.Generate("mem-")
	now := time.Now().UTC()
	payload := models.MemoryStoredPayload{
		ID:        memID,
		Type:      "episodic",
		Category:  "peer-episode",
		Content:   "memory from peer",
		CreatedAt: now.UnixNano(),
		AgentID:   "agent-peer",
		SessionID: id.Generate("sess-"),
	}
	event := makeGossipEvent(t, models.EventTypeMemoryStored, "node-peer", now, payload)

	if err := handler.OnEvent(event); err != nil {
		t.Fatalf("OnEvent: %v", err)
	}

	ctx := context.Background()
	var (
		gotContent   string
		gotSource    string
		gotAgent     string
	)
	err = store.GossipDB().QueryRowContext(ctx,
		`SELECT content, source_node, agent_id FROM memories WHERE id = ?`,
		memID,
	).Scan(&gotContent, &gotSource, &gotAgent)
	if err != nil {
		t.Fatalf("query gossip memory: %v", err)
	}
	if gotContent != "memory from peer" {
		t.Errorf("content = %q, want %q", gotContent, "memory from peer")
	}
	if gotSource != "node-peer" {
		t.Errorf("source_node = %q, want %q", gotSource, "node-peer")
	}
	if gotAgent != "agent-peer" {
		t.Errorf("agent_id = %q, want %q", gotAgent, "agent-peer")
	}
}

// TestGossipHandler_ConflictResolution_NewerTimestampWins verifies that when
// two MEMORY_STORED events from different nodes target the same memory ID,
// the one with the newer timestamp wins (when a ConflictResolver is wired).
func TestGossipHandler_ConflictResolution_NewerTimestampWins(t *testing.T) {
	t.Parallel()

	tmp := t.TempDir()
	store, err := memory.NewDualStore(tmp, "node-local", newTestLogger())
	if err != nil {
		t.Fatalf("NewDualStore: %v", err)
	}
	defer store.Close()

	resolver := cluster.NewConflictResolver(slog.New(slog.NewTextHandler(io.Discard, nil)))
	handler := cluster.NewGossipHandler(store, "node-local", newTestLogger(), resolver)

	memID := id.Generate("conflict-")

	// Older event from node-A.
	older := time.Now().Add(-10 * time.Second).UTC()
	payloadOld := models.MemoryStoredPayload{
		ID:        memID,
		Type:      "episodic",
		Category:  "first",
		Content:   "OLDER",
		CreatedAt: older.UnixNano(),
	}
	eventOld := makeGossipEvent(t, models.EventTypeMemoryStored, "node-A", older, payloadOld)
	if err := handler.OnEvent(eventOld); err != nil {
		t.Fatalf("OnEvent older: %v", err)
	}

	// Newer event from node-B.
	newer := time.Now().UTC()
	payloadNew := models.MemoryStoredPayload{
		ID:        memID,
		Type:      "episodic",
		Category:  "second",
		Content:   "NEWER",
		CreatedAt: newer.UnixNano(),
	}
	eventNew := makeGossipEvent(t, models.EventTypeMemoryStored, "node-B", newer, payloadNew)
	if err := handler.OnEvent(eventNew); err != nil {
		t.Fatalf("OnEvent newer: %v", err)
	}

	ctx := context.Background()
	var gotContent string
	err = store.GossipDB().QueryRowContext(ctx,
		`SELECT content FROM memories WHERE id = ?`, memID,
	).Scan(&gotContent)
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	// The newer event should have won (its content replaces the older).
	if gotContent != "NEWER" {
		t.Errorf("content = %q, want %q (newer should win)", gotContent, "NEWER")
	}
}

// TestGossipHandler_ConflictResolution_OlderEventDropped verifies that when
// an older event arrives after a newer one has already been applied, the
// ConflictResolver drops the older incoming event.
func TestGossipHandler_ConflictResolution_OlderEventDropped(t *testing.T) {
	t.Parallel()

	tmp := t.TempDir()
	store, err := memory.NewDualStore(tmp, "node-local", newTestLogger())
	if err != nil {
		t.Fatalf("NewDualStore: %v", err)
	}
	defer store.Close()

	resolver := cluster.NewConflictResolver(newTestLogger())
	handler := cluster.NewGossipHandler(store, "node-local", newTestLogger(), resolver)

	memID := id.Generate("conflict-")

	// Apply newer event first.
	newer := time.Now().UTC()
	payloadNew := models.MemoryStoredPayload{
		ID:        memID,
		Type:      "episodic",
		Category:  "newer",
		Content:   "NEWER-FIRST",
		CreatedAt: newer.UnixNano(),
	}
	eventNew := makeGossipEvent(t, models.EventTypeMemoryStored, "node-B", newer, payloadNew)
	if err := handler.OnEvent(eventNew); err != nil {
		t.Fatalf("OnEvent newer: %v", err)
	}

	// Older event arrives second — should be dropped.
	older := time.Now().Add(-30 * time.Second).UTC()
	payloadOld := models.MemoryStoredPayload{
		ID:        memID,
		Type:      "episodic",
		Category:  "older",
		Content:   "OLDER-SECOND",
		CreatedAt: older.UnixNano(),
	}
	eventOld := makeGossipEvent(t, models.EventTypeMemoryStored, "node-A", older, payloadOld)
	if err := handler.OnEvent(eventOld); err != nil {
		t.Fatalf("OnEvent older: %v", err)
	}

	ctx := context.Background()
	var gotContent string
	if err := store.GossipDB().QueryRowContext(ctx,
		`SELECT content FROM memories WHERE id = ?`, memID,
	).Scan(&gotContent); err != nil {
		t.Fatalf("query: %v", err)
	}
	if gotContent != "NEWER-FIRST" {
		t.Errorf("content = %q, want %q (older event should have been dropped)", gotContent, "NEWER-FIRST")
	}
}

// TestGossipHandler_MemoryEdge_Persists verifies the handler accepts a
// MEMORY_EDGE event without error. The current handler implementation is a
// no-op (logs debug), but this test pins that contract so a future regression
// (e.g., panicking on edge events) is caught.
func TestGossipHandler_MemoryEdge_Persists(t *testing.T) {
	t.Parallel()

	tmp := t.TempDir()
	store, err := memory.NewDualStore(tmp, "node-local", newTestLogger())
	if err != nil {
		t.Fatalf("NewDualStore: %v", err)
	}
	defer store.Close()

	handler := cluster.NewGossipHandler(store, "node-local", newTestLogger(), nil)

	now := time.Now().UTC()
	payload := models.MemoryEdgePayload{
		FromID:      id.Generate("from-"),
		ToID:        id.Generate("to-"),
		EdgeType:    "supports",
		Established: now.UnixNano(),
	}
	event := makeGossipEvent(t, models.EventTypeMemoryEdge, "node-peer", now, payload)

	if err := handler.OnEvent(event); err != nil {
		t.Fatalf("OnEvent MEMORY_EDGE: %v", err)
	}
	// Current behavior is a no-op (no persistence). The test asserts the
	// handler does not error or panic. If a future change adds edge
	// persistence, this test can be extended.
}

// guard against unused import warnings.
var _ = filepath.Join
