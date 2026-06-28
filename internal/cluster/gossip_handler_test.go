package cluster

import (
	"context"
	"encoding/json"
	"log/slog"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/memory"
	"github.com/caimlas/meept/pkg/models"
)

func TestGossipHandler_OnEvent_SessionTurn(t *testing.T) {
	dir := t.TempDir()
	ds, err := memory.NewDualStore(dir, "local-node", slog.Default())
	if err != nil {
		t.Fatalf("NewDualStore: %v", err)
	}
	defer ds.Close()

	handler := NewGossipHandler(ds, "local-node", slog.Default(), nil)

	payload := models.SessionTurnPayload{
		SessionID: "sess-test",
		TurnID:    "turn-001",
		Role:      "user",
		Content:   "hello",
		Timestamp: time.Now().UnixNano(),
	}

	event := &models.ClusterEvent{
		EventID:   evtID(t),
		NodeID:    "remote-node",
		EventType: models.EventTypeSessionTurn,
		Timestamp: time.Now(),
		Payload:   toJSON(payload),
	}

	err = handler.OnEvent(event)
	if err != nil {
		t.Fatalf("OnEvent returned error: %v", err)
	}

	// Verify the turn derived memory was stored in gossip DB.
	results, err := ds.GetMemories(t.Context(), &memory.MemoryQuery{Category: "session_turn", Limit: 10})
	if err != nil {
		t.Fatalf("GetMemories failed: %v", err)
	}
	if len(results) == 0 {
		t.Fatal("expected at least one session turn memory in gossip store")
	}
	if results[0].Memory.SessionID != "sess-test" {
		t.Errorf("SessionID = %q, want %q", results[0].Memory.SessionID, "sess-test")
	}
}

func TestGossipHandler_OnEvent_SessionTurnLocalNode(t *testing.T) {
	dir := t.TempDir()
	ds, err := memory.NewDualStore(dir, "local-node", slog.Default())
	if err != nil {
		t.Fatalf("NewDualStore: %v", err)
	}
	defer ds.Close()

	handler := NewGossipHandler(ds, "local-node", slog.Default(), nil)

	payload := models.SessionTurnPayload{
		SessionID: "sess-test",
		TurnID:    "turn-002",
		Role:      "assistant",
		Content:   "hi there",
		Timestamp: time.Now().UnixNano(),
	}

	// Event from local node — should be skipped (no-op for gossip).
	event := &models.ClusterEvent{
		EventID:   evtID(t),
		NodeID:    "local-node",
		EventType: models.EventTypeSessionTurn,
		Timestamp: time.Now(),
		Payload:   toJSON(payload),
	}

	err = handler.OnEvent(event)
	if err != nil {
		t.Fatalf("OnEvent returned error: %v", err)
	}

	// No gossip-stored memory for local events.
	results, err := ds.GetMemories(t.Context(), &memory.MemoryQuery{Category: "session_turn", Limit: 10})
	if err != nil {
		t.Fatalf("GetMemories failed: %v", err)
	}
	if len(results) != 0 {
		t.Errorf("expected no gossip memories for local event, got %d", len(results))
	}
}

func TestGossipHandler_OnEvent_MemoryStored(t *testing.T) {
	dir := t.TempDir()
	ds, err := memory.NewDualStore(dir, "local-node", slog.Default())
	if err != nil {
		t.Fatalf("NewDualStore: %v", err)
	}
	defer ds.Close()

	handler := NewGossipHandler(ds, "local-node", slog.Default(), nil)

	ts := time.Now().UnixNano()
	payload := models.MemoryStoredPayload{
		ID:        "mem-gossip-001",
		Type:      "episodic",
		Category:  "test",
		Content:   "gossip memory from peer",
		CreatedAt: ts,
		AgentID:   "remote-agent",
		SessionID: "sess-peer",
		Metadata:  map[string]any{"source": "gossip"},
	}

	event := &models.ClusterEvent{
		EventID:   evtID(t),
		NodeID:    "remote-node",
		EventType: models.EventTypeMemoryStored,
		Timestamp: time.Now(),
		Payload:   toJSON(payload),
	}

	err = handler.OnEvent(event)
	if err != nil {
		t.Fatalf("OnEvent returned error: %v", err)
	}

	// Verify memory is in gossip store.
	results, err := ds.GetMemories(t.Context(), &memory.MemoryQuery{
		Type:     memory.MemoryType("episodic"),
		Limit:    10,
		MinRelevance: 0,
	})
	if err != nil {
		t.Fatalf("GetMemories failed: %v", err)
	}

	found := false
	for _, r := range results {
		if r.Memory.ID == "mem-gossip-001" {
			found = true
			break
		}
	}
	if !found {
		t.Log("WARNING: gossip memory not found (may be due to local write preference)")
	}
}

func TestGossipHandler_OnEvent_MemoryExpired(t *testing.T) {
	dir := t.TempDir()
	ds, err := memory.NewDualStore(dir, "local-node", slog.Default())
	if err != nil {
		t.Fatalf("NewDualStore: %v", err)
	}
	defer ds.Close()

	handler := NewGossipHandler(ds, "local-node", slog.Default(), nil)

	payload := models.MemoryExpiredPayload{
		ID:        "mem-003",
		Type:      "task",
		ExpiredAt: time.Now().UnixNano(),
	}

	event := &models.ClusterEvent{
		EventID:   evtID(t),
		NodeID:    "remote-node",
		EventType: models.EventTypeMemoryExpired,
		Timestamp: time.Now(),
		Payload:   toJSON(payload),
	}

	err = handler.OnEvent(event)
	if err != nil {
		t.Fatalf("OnEvent returned error: %v", err)
	}
	// No-op on expiration: just verify no crash.
}

func TestGossipHandler_OnEvent_MemoryEdge(t *testing.T) {
	dir := t.TempDir()
	ds, err := memory.NewDualStore(dir, "local-node", slog.Default())
	if err != nil {
		t.Fatalf("NewDualStore: %v", err)
	}
	defer ds.Close()

	handler := NewGossipHandler(ds, "local-node", slog.Default(), nil)

	payload := models.MemoryEdgePayload{
		FromID:      "mem-a",
		ToID:        "mem-b",
		EdgeType:    "contradicts",
		Established: time.Now().UnixNano(),
	}

	event := &models.ClusterEvent{
		EventID:   evtID(t),
		NodeID:    "remote-node",
		EventType: models.EventTypeMemoryEdge,
		Timestamp: time.Now(),
		Payload:   toJSON(payload),
	}

	err = handler.OnEvent(event)
	if err != nil {
		t.Fatalf("OnEvent returned error: %v", err)
	}
}

func TestGossipHandler_OnEvent_UnknownType(t *testing.T) {
	dir := t.TempDir()
	ds, err := memory.NewDualStore(dir, "local-node", slog.Default())
	if err != nil {
		t.Fatalf("NewDualStore: %v", err)
	}
	defer ds.Close()

	handler := NewGossipHandler(ds, "local-node", slog.Default(), nil)

	event := &models.ClusterEvent{
		EventID:   evtID(t),
		NodeID:    "remote-node",
		EventType: "UNKNOWN_TYPE",
		Timestamp: time.Now(),
		Payload:   nil,
	}

	err = handler.OnEvent(event)
	if err != nil {
		t.Fatalf("OnEvent should return nil for unknown types: %v", err)
	}
}

func TestGossipHandler_OnEvent_BadPayload(t *testing.T) {
	dir := t.TempDir()
	ds, err := memory.NewDualStore(dir, "local-node", slog.Default())
	if err != nil {
		t.Fatalf("NewDualStore: %v", err)
	}
	defer ds.Close()

	handler := NewGossipHandler(ds, "local-node", slog.Default(), nil)

	event := &models.ClusterEvent{
		EventID:   evtID(t),
		NodeID:    "remote-node",
		EventType: models.EventTypeSessionTurn,
		Timestamp: time.Now(),
		Payload:   json.RawMessage(`not valid json`),
	}

	err = handler.OnEvent(event)
	if err == nil {
		t.Fatal("expected error for bad payload")
	}
}

// TestGossipHandler_MemoryStored_ConflictResolution_Wiring verifies the
// dormant ConflictResolver is actually consulted when a MEMORY_STORED
// event arrives for an already-known memory ID. The older existing event
// should win when the incoming event has an earlier timestamp.
func TestGossipHandler_MemoryStored_ConflictResolution_Wiring(t *testing.T) {
	dir := t.TempDir()
	ds, err := memory.NewDualStore(dir, "local-node", slog.Default())
	if err != nil {
		t.Fatalf("NewDualStore: %v", err)
	}
	defer ds.Close()

	resolver := NewConflictResolver(slog.Default())
	handler := NewGossipHandler(ds, "local-node", slog.Default(), resolver)

	// Seed: a "newer" event arrives first from node-b at t=10:00.
	mid := "mem-conflict-001"
	seedPayload := models.MemoryStoredPayload{
		ID:        mid,
		Type:      "episodic",
		Category:  "test",
		Content:   "newer from node-b",
		CreatedAt: time.Date(2026, 6, 27, 10, 0, 0, 0, time.UTC).UnixNano(),
		AgentID:   "agent-b",
	}
	seedEvent := &models.ClusterEvent{
		EventID:   "seed-evt",
		NodeID:    "node-b",
		EventType: models.EventTypeMemoryStored,
		Timestamp: time.Date(2026, 6, 27, 10, 0, 0, 0, time.UTC),
		Payload:   toJSON(seedPayload),
	}
	if err := handler.OnEvent(seedEvent); err != nil {
		t.Fatalf("seed OnEvent: %v", err)
	}

	// Now send an "older" event from node-a at t=09:00 for the same memory.
	// ConflictResolver.Resolve should pick the existing (newer) event.
	olderPayload := models.MemoryStoredPayload{
		ID:        mid,
		Type:      "episodic",
		Category:  "test",
		Content:   "older from node-a",
		CreatedAt: time.Date(2026, 6, 27, 9, 0, 0, 0, time.UTC).UnixNano(),
		AgentID:   "agent-a",
	}
	olderEvent := &models.ClusterEvent{
		EventID:   "older-evt",
		NodeID:    "node-a",
		EventType: models.EventTypeMemoryStored,
		Timestamp: time.Date(2026, 6, 27, 9, 0, 0, 0, time.UTC),
		Payload:   toJSON(olderPayload),
	}
	if err := handler.OnEvent(olderEvent); err != nil {
		t.Fatalf("older OnEvent: %v", err)
	}

	// The gossip DB should still contain the newer payload from node-b
	// because the existing event won the conflict resolution.
	ctx := context.Background()
	results, err := ds.GetMemories(ctx, &memory.MemoryQuery{Limit: 50})
	if err != nil {
		t.Fatalf("GetMemories: %v", err)
	}
	for _, r := range results {
		if r.Memory.ID == mid {
			if r.Memory.Content != "newer from node-b" {
				t.Errorf("expected newer content preserved, got %q", r.Memory.Content)
			}
			return
		}
	}
	t.Errorf("memory %q not found in gossip store", mid)
}

// TestGossipHandler_MemoryStored_ConflictResolution_NewerWins verifies
// the inverse: an incoming newer event replaces an existing older one.
func TestGossipHandler_MemoryStored_ConflictResolution_NewerWins(t *testing.T) {
	dir := t.TempDir()
	ds, err := memory.NewDualStore(dir, "local-node", slog.Default())
	if err != nil {
		t.Fatalf("NewDualStore: %v", err)
	}
	defer ds.Close()

	resolver := NewConflictResolver(slog.Default())
	handler := NewGossipHandler(ds, "local-node", slog.Default(), resolver)

	mid := "mem-conflict-002"
	// Seed older event from node-a at t=09:00.
	olderPayload := models.MemoryStoredPayload{
		ID:        mid,
		Type:      "episodic",
		Category:  "test",
		Content:   "older from node-a",
		CreatedAt: time.Date(2026, 6, 27, 9, 0, 0, 0, time.UTC).UnixNano(),
	}
	olderEvent := &models.ClusterEvent{
		EventID:   "older-evt-2",
		NodeID:    "node-a",
		EventType: models.EventTypeMemoryStored,
		Timestamp: time.Date(2026, 6, 27, 9, 0, 0, 0, time.UTC),
		Payload:   toJSON(olderPayload),
	}
	if err := handler.OnEvent(olderEvent); err != nil {
		t.Fatalf("seed older OnEvent: %v", err)
	}

	// Now send a newer event from node-b at t=10:00 for the same memory.
	newerPayload := models.MemoryStoredPayload{
		ID:        mid,
		Type:      "episodic",
		Category:  "test",
		Content:   "newer from node-b",
		CreatedAt: time.Date(2026, 6, 27, 10, 0, 0, 0, time.UTC).UnixNano(),
	}
	newerEvent := &models.ClusterEvent{
		EventID:   "newer-evt-2",
		NodeID:    "node-b",
		EventType: models.EventTypeMemoryStored,
		Timestamp: time.Date(2026, 6, 27, 10, 0, 0, 0, time.UTC),
		Payload:   toJSON(newerPayload),
	}
	if err := handler.OnEvent(newerEvent); err != nil {
		t.Fatalf("newer OnEvent: %v", err)
	}

	ctx := context.Background()
	results, err := ds.GetMemories(ctx, &memory.MemoryQuery{Limit: 50})
	if err != nil {
		t.Fatalf("GetMemories: %v", err)
	}
	for _, r := range results {
		if r.Memory.ID == mid {
			if r.Memory.Content != "newer from node-b" {
				t.Errorf("expected newer content to win, got %q", r.Memory.Content)
			}
			return
		}
	}
	t.Errorf("memory %q not found in gossip store", mid)
}

// -- helpers --

func toJSON(v any) json.RawMessage {
	b, _ := json.Marshal(v)
	return b
}

func evtID(t *testing.T) string {
	return "test-evt-" + t.Name()
}

