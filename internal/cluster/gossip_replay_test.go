package cluster

import (
	"database/sql"
	"encoding/json"
	"sync"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/bus"
	"github.com/caimlas/meept/pkg/id"
	"github.com/caimlas/meept/pkg/models"
	_ "github.com/mattn/go-sqlite3"
)

// TestReplayDispatchGuard verifies that a duplicate event_id (INSERT OR IGNORE
// ignored) does NOT reach registered handlers. This is the fix for audit
// finding 4: the in-memory dedup cache alone is insufficient; durable
// dedup via persistEvent's RowsAffected must gate emitToHandlers.
func TestReplayDispatchGuard(t *testing.T) {
	dir := t.TempDir()
	dbPath := dir + "/test.db"
	db, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		t.Fatalf("sqlite open: %v", err)
	}
	defer db.Close()

	// Create cluster_events table
	_, err = db.Exec(`
		CREATE TABLE IF NOT EXISTS cluster_events (
			event_id TEXT PRIMARY KEY,
			node_id TEXT,
			event_type TEXT,
			timestamp INTEGER,
			vector_clock TEXT,
			payload TEXT,
			signature TEXT,
			received_at INTEGER
		)
	`)
	if err != nil {
		t.Fatalf("create table: %v", err)
	}

	b := bus.New(nil, discardLogger())
	cfg := &Config{
		NodeID: "node-replay",
		Gossip: GossipConfig{
			HeartbeatInterval: time.Hour,
			PeerTimeout:       time.Hour,
			EventRetention:    time.Hour,
		},
		Security: SecurityConfig{RequireNodeSignatures: false},
	}

	engine := NewGossipEngine(cfg, "node-replay", b, discardLogger(), WithDatabase(db))

	// Register a handler that counts how many times it's called
	handler := &countingHandler{}
	engine.RegisterHandler(handler)

	event := &models.ClusterEvent{
		EventID:   "dup-event-001",
		NodeID:    "remote-node",
		EventType: models.EventTypeSessionTurn,
		Timestamp: time.Now().UTC(),
		Payload:   json.RawMessage(`{"session_id":"sess-1","turn_id":"turn-1"}`),
	}

	// First call: should persist and dispatch to handler
	engine.handleClusterEvent(buildBusMessage(event))
	if handler.count != 1 {
		t.Fatalf("first dispatch: handler called %d times, want 1", handler.count)
	}

	// Second call with same event_id: INSERT OR IGNORE will be ignored (RowsAffected==0)
	// handler should NOT be called again
	engine.handleClusterEvent(buildBusMessage(event))
	if handler.count != 1 {
		t.Fatalf("second dispatch (duplicate): handler called %d times, want 1", handler.count)
	}
}

// countingHandler is a simple GossipHandler that counts OnEvent calls.
type countingHandler struct {
	mu    sync.Mutex
	count int
}

func (h *countingHandler) OnEvent(event *models.ClusterEvent) error {
	h.mu.Lock()
	h.count++
	h.mu.Unlock()
	return nil
}

func buildBusMessage(event *models.ClusterEvent) *models.BusMessage {
	payloadMap := map[string]json.RawMessage{"event": toJSON(event)}
	payload, _ := json.Marshal(payloadMap)
	return &models.BusMessage{
		ID:        id.Generate("test-"),
		Type:      models.MessageTypeEvent,
		Source:    "test",
		Timestamp: time.Now().UTC(),
		Payload:   payload,
	}
}
