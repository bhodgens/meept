package cluster

// peer_liveness_test.go — liveness-from-the-wire tests (multiuser live
// verification follow-up). Peers must be discovered from inbound traffic,
// refreshed on repeat observation, and pruned when stale.

import (
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
