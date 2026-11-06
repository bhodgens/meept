package cluster

// peer_liveness_test.go — liveness-from-the-wire tests (multiuser live
// verification follow-up). Peers must be discovered from inbound traffic,
// refreshed on repeat observation, and pruned when stale.

import (
	"database/sql"
	"encoding/json"
	"testing"
	"time"

	"github.com/caimlas/meept/pkg/models"
)

func TestObservePeer_DisoversNewPeer(t *testing.T) {
	g := NewGossipEngine(nil, "node-a", nil, testLogger(t))
	g.observePeer("node-b", "10.0.0.2:9701")

	peers := g.Peers()
	if len(peers) != 1 {
		t.Fatalf("peers = %d, want 1", len(peers))
	}
	p := peers[0]
	if p.NodeID != "node-b" {
		t.Errorf("node = %s, want node-b", p.NodeID)
	}
	if p.Endpoint != "10.0.0.2:9701" {
		t.Errorf("endpoint = %q, want 10.0.0.2:9701", p.Endpoint)
	}
	if p.Status != "active" {
		t.Errorf("status = %q, want active", p.Status)
	}
}

func TestObservePeer_RefreshesExistingAndKeepsEndpoint(t *testing.T) {
	g := NewGossipEngine(nil, "node-a", nil, testLogger(t))
	g.observePeer("node-b", "10.0.0.2:9701")
	first := g.Peers()[0].LastSeen

	time.Sleep(2 * time.Millisecond)
	// Repeat observation WITHOUT endpoint must not clear the known one.
	g.observePeer("node-b", "")
	again := g.Peers()[0]

	if !again.LastSeen.After(first) {
		t.Error("LastSeen not refreshed on repeat observation")
	}
	if again.Endpoint != "10.0.0.2:9701" {
		t.Errorf("endpoint clobbered: %q", again.Endpoint)
	}
}

func TestObservePeer_IgnoresSelfAndEmpty(t *testing.T) {
	g := NewGossipEngine(nil, "node-a", nil, testLogger(t))
	g.observePeer("node-a", "")
	g.observePeer("", "x")
	if n := g.PeerCount(); n != 0 {
		t.Errorf("peers = %d, want 0 (self/empty must be ignored)", n)
	}
}

func TestHandleClusterEvent_HeartbeatUpsertsPeerAndSkipsHandlers(t *testing.T) {
	g := NewGossipEngine(nil, "node-a", nil, testLogger(t))
	handlerCalled := false
	g.RegisterHandler(stubGossipHandler{fn: func(_ *models.ClusterEvent) error {
		handlerCalled = true
		return nil
	}})

	hb := &models.ClusterEvent{
		EventID:   models.GenerateEventID(),
		NodeID:    "node-b",
		EventType: clusterHeartbeatEventType,
		Timestamp: time.Now().UTC(),
	}
	g.handleClusterEvent(busEnvelope(t, hb))

	peers := g.Peers()
	if len(peers) != 1 || peers[0].NodeID != "node-b" {
		t.Fatalf("peers after heartbeat = %+v, want [node-b]", peers)
	}
	if handlerCalled {
		t.Error("domain handler invoked for CLUSTER_HEARTBEAT; must short-circuit")
	}
}

func TestHandleClusterEvent_DomainEventUpsertsSender(t *testing.T) {
	g := NewGossipEngine(nil, "node-a", nil, testLogger(t))

	ev := &models.ClusterEvent{
		EventID:   models.GenerateEventID(),
		NodeID:    "node-c",
		EventType: models.ClusterEventType("SOME_DOMAIN_EVENT"),
		Timestamp: time.Now().UTC(),
		Payload:   []byte("{}"),
	}
	g.handleClusterEvent(busEnvelope(t, ev))

	found := false
	for _, p := range g.Peers() {
		if p.NodeID == "node-c" {
			found = true
		}
	}
	if !found {
		t.Fatal("sender not discovered as peer from domain event")
	}
}

func TestPruneStalePeers_RemovesStaleAfterObservation(t *testing.T) {
	g := NewGossipEngine(&Config{
		Gossip: GossipConfig{PeerTimeout: 50 * time.Millisecond},
	}, "node-a", nil, testLogger(t))
	g.observePeer("node-b", "")

	time.Sleep(80 * time.Millisecond)
	g.pruneStalePeers()

	if n := g.PeerCount(); n != 0 {
		t.Errorf("stale peer survived prune: count=%d", n)
	}
}

// busEnvelope wraps an event in the BusMessage shape handleClusterEvent
// expects from the transport.
func busEnvelope(t *testing.T, ev *models.ClusterEvent) *models.BusMessage {
	t.Helper()
	payload, err := json.Marshal(ev)
	if err != nil {
		t.Fatalf("marshal event: %v", err)
	}
	envelope, err := json.Marshal(map[string]json.RawMessage{"event": payload})
	if err != nil {
		t.Fatalf("marshal envelope: %v", err)
	}
	return &models.BusMessage{
		ID:      "test-" + ev.EventID,
		Type:    models.MessageTypeEvent,
		Source:  "test",
		Payload: envelope,
	}
}

// stubGossipHandler records whether the handler pipeline dispatched to it.
type stubGossipHandler struct {
	fn func(*models.ClusterEvent) error
}

func (s stubGossipHandler) OnEvent(ev *models.ClusterEvent) error {
	return s.fn(ev)
}

// TestHandleClusterEvent_DuplicateEventSkipsHandlerWithDB tests that when a
// duplicate event_id is persisted (INSERT OR IGNORE returns 0 rows affected),
// the handler is NOT dispatched. This requires a real sqlite DB.
func TestHandleClusterEvent_DuplicateEventSkipsHandlerWithDB(t *testing.T) {
	// Create an in-memory sqlite DB for testing
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	// Create the cluster_events table
	_, err = db.Exec(`CREATE TABLE cluster_events (
		event_id TEXT PRIMARY KEY,
		node_id TEXT NOT NULL,
		event_type TEXT NOT NULL,
		timestamp INTEGER NOT NULL,
		vector_clock TEXT NOT NULL,
		payload BLOB NOT NULL,
		signature BLOB NOT NULL,
		received_at INTEGER NOT NULL
	)`)
	if err != nil {
		t.Fatalf("create table: %v", err)
	}

	g := NewGossipEngine(nil, "node-a", nil, testLogger(t), WithDatabase(db))
	handlerCalled := false
	g.RegisterHandler(stubGossipHandler{fn: func(_ *models.ClusterEvent) error {
		handlerCalled = true
		return nil
	}})

	// First event: should be inserted and handler called
	ev1 := &models.ClusterEvent{
		EventID:   "evt-1",
		NodeID:    "node-b",
		EventType: models.ClusterEventType("TEST_EVENT"),
		Timestamp: time.Now().UTC(),
		Payload:   []byte(`{}`),
	}
	g.handleClusterEvent(busEnvelope(t, ev1))
	if !handlerCalled {
		t.Error("first event: handler not called")
	}
	handlerCalled = false

	// Second event with same ID: should be ignored (RowsAffected=0) and handler NOT called
	ev2 := &models.ClusterEvent{
		EventID:   "evt-1", // same ID as ev1
		NodeID:    "node-c",
		EventType: models.ClusterEventType("TEST_EVENT"),
		Timestamp: time.Now().UTC(),
		Payload:   []byte(`{}`),
	}
	g.handleClusterEvent(busEnvelope(t, ev2))
	if handlerCalled {
		t.Error("duplicate event: handler SHOULD NOT have been called")
	}
}

// TestSweepClusterEventsRetention Deletes old rows and keeps fresh ones.
func TestSweepClusterEventsRetention(t *testing.T) {
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	_, err = db.Exec(`CREATE TABLE cluster_events (
		event_id TEXT PRIMARY KEY,
		node_id TEXT NOT NULL,
		event_type TEXT NOT NULL,
		timestamp INTEGER NOT NULL,
		vector_clock TEXT NOT NULL,
		payload BLOB NOT NULL,
		signature BLOB NOT NULL,
		received_at INTEGER NOT NULL
	)`)
	if err != nil {
		t.Fatalf("create table: %v", err)
	}

	g := NewGossipEngine(&Config{
		Gossip: GossipConfig{EventTableRetention: 1 * time.Hour},
	}, "node-a", nil, testLogger(t), WithDatabase(db))

	// Insert a "fresh" row (received_at = now)
	now := time.Now().UnixNano()
	_, err = db.Exec(`INSERT INTO cluster_events (event_id, node_id, event_type, timestamp, vector_clock, payload, signature, received_at)
		VALUES ('fresh-evt', 'node-b', 'TEST', ?, '{}', '{}', '{}', ?)`, now, now)
	if err != nil {
		t.Fatalf("insert fresh: %v", err)
	}

	// Insert an "old" row (received_at = now - 2 hours, which is > 1 hour retention)
	older := now - int64(2*time.Hour)
	_, err = db.Exec(`INSERT INTO cluster_events (event_id, node_id, event_type, timestamp, vector_clock, payload, signature, received_at)
		VALUES ('old-evt', 'node-b', 'TEST', ?, '{}', '{}', '{}', ?)`, now, older)
	if err != nil {
		t.Fatalf("insert old: %v", err)
	}

	// Verify both rows exist before sweep
	var count int
	err = db.QueryRow(`SELECT COUNT(*) FROM cluster_events`).Scan(&count)
	if err != nil || count != 2 {
		t.Fatalf("before sweep: count=%d, want 2", count)
	}

	// Run the retention sweep
	g.sweepClusterEventsRetention()

	// Verify only the fresh row remains
	err = db.QueryRow(`SELECT COUNT(*) FROM cluster_events`).Scan(&count)
	if err != nil {
		t.Fatalf("after sweep count query: %v", err)
	}
	if count != 1 {
		t.Errorf("after sweep: count=%d, want 1 (old row deleted)", count)
	}

	// Verify the fresh row still exists
	var eventID string
	err = db.QueryRow(`SELECT event_id FROM cluster_events`).Scan(&eventID)
	if err != nil {
		t.Fatalf("select fresh event: %v", err)
	}
	if eventID != "fresh-evt" {
		t.Errorf("event_id = %q, want 'fresh-evt'", eventID)
	}
}
