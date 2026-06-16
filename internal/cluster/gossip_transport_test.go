package cluster

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net"
	"strconv"
	"testing"
	"time"

	"github.com/caimlas/meept/pkg/models"
)

func itoa(i int) string { return strconv.Itoa(i) }

func TestGossipTransport_SendReceiveEvent(t *testing.T) {
	// Find an available port for the receiver
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("find port: %v", err)
	}
	receiverPort := ln.Addr().(*net.TCPAddr).Port
	ln.Close()

	// Configure gossip port = receiver port, WG port = gossip port - 1
	cfg := &Config{
		Network: NetworkConfig{
			WireGuardPort:   receiverPort - 1,
			WireGuardSubnet: "10.200.0.0/24",
		},
		Gossip: GossipConfig{
			HeartbeatInterval: 30 * time.Second,
			PeerTimeout:       2 * time.Minute,
			EventRetention:    1 * time.Hour,
			MaxRetryAttempts:  3,
		},
		Security: SecurityConfig{RequireNodeSignatures: false},
	}

	// Members provider that knows about the receiver at 127.0.0.1
	senderMembers := &mockMembersProvider{
		members: map[string]*Member{
			"node-receiver": {
				NodeID:    "node-receiver",
				ClusterIP: "127.0.0.1",
				Status:    "active",
				Endpoint:  "127.0.0.1:" + itoa(receiverPort-1),
			},
		},
	}

	logger := slog.Default()

	// Channel to capture received event
	receiverGot := make(chan *models.ClusterEvent, 1)

	// Start a raw TCP listener that acts as the receiver
	receiverLn, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", receiverPort))
	if err != nil {
		t.Fatalf("receiver listen: %v", err)
	}
	defer receiverLn.Close()

	go func() {
		conn, err := receiverLn.Accept()
		if err != nil {
			return
		}
		defer conn.Close()

		conn.SetReadDeadline(time.Now().Add(5 * time.Second))
		buf := make([]byte, 8192)
		n, err := conn.Read(buf)
		if err != nil {
			return
		}

		data := buf[:n]
		if len(data) > 0 && data[len(data)-1] == '\n' {
			data = data[:len(data)-1]
		}

		var received models.ClusterEvent
		if err := json.Unmarshal(data, &received); err != nil {
			t.Logf("receiver unmarshal: %v", err)
			return
		}

		conn.Write([]byte("ACK\n"))
		receiverGot <- &received
	}()

	// Create sender transport (no need to start its own listener for sending)
	senderCfg := *cfg
	senderTransport := NewGossipTransport(&senderCfg, "node-sender", nil, senderMembers, logger)

	// Send event
	event := &models.ClusterEvent{
		EventID:   models.GenerateEventID(),
		NodeID:    "node-sender",
		EventType: models.EventTaskCreate,
		Timestamp: time.Now().UTC(),
		Payload:   json.RawMessage(`{"task_id":"test-task-001"}`),
	}

	senderTransport.SendEvent(event)

	// Wait for receiver
	select {
	case received := <-receiverGot:
		if received.EventID != event.EventID {
			t.Errorf("EventID mismatch: got %s, want %s", received.EventID, event.EventID)
		}
		if received.EventType != event.EventType {
			t.Errorf("EventType mismatch: got %s, want %s", received.EventType, event.EventType)
		}
		if received.NodeID != event.NodeID {
			t.Errorf("NodeID mismatch: got %s, want %s", received.NodeID, event.NodeID)
		}
	case <-time.After(5 * time.Second):
		t.Error("timed out waiting for event delivery")
	}
}

func TestGossipTransport_PeerGossipAddr(t *testing.T) {
	cfg := &Config{
		Network: NetworkConfig{
			WireGuardPort: 51820,
		},
	}

	transport := &GossipTransport{cfg: cfg}

	// Test with cluster IP
	member := &Member{
		ClusterIP: "10.200.0.5",
		Endpoint:  "192.168.1.42:51820",
	}
	addr := transport.peerGossipAddr(member)
	if addr != "10.200.0.5:51821" {
		t.Errorf("expected 10.200.0.5:51821, got %s", addr)
	}

	// Test fallback to endpoint when no cluster IP
	memberNoIP := &Member{
		ClusterIP: "",
		Endpoint:  "192.168.1.42:51820",
	}
	addr = transport.peerGossipAddr(memberNoIP)
	if addr != "192.168.1.42:51821" {
		t.Errorf("expected 192.168.1.42:51821, got %s", addr)
	}
}

func TestGossipTransport_SentTracking(t *testing.T) {
	transport := &GossipTransport{
		sentEvents: make(map[string]map[string]time.Time),
	}

	// Initially not sent
	if transport.hasSentToPeer("node-1", "event-1") {
		t.Error("should not be marked as sent initially")
	}

	// Mark as sent
	transport.markSentToPeer("node-1", "event-1")
	if !transport.hasSentToPeer("node-1", "event-1") {
		t.Error("should be marked as sent after marking")
	}

	// Different event should not be marked
	if transport.hasSentToPeer("node-1", "event-2") {
		t.Error("different event should not be marked")
	}

	// Different peer should not be marked
	if transport.hasSentToPeer("node-2", "event-1") {
		t.Error("different peer should not be marked")
	}
}

func TestGossipTransport_SkipsSelf(t *testing.T) {
	cfg := &Config{
		Network: NetworkConfig{WireGuardPort: 51820},
		Gossip:  GossipConfig{EventRetention: time.Hour},
	}

	members := &mockMembersProvider{
		members: map[string]*Member{
			"self":       {NodeID: "self", ClusterIP: "10.0.0.1", Status: "active"},
			"other-node": {NodeID: "other-node", ClusterIP: "10.0.0.2", Status: "active"},
		},
	}

	transport := NewGossipTransport(cfg, "self", nil, members, slog.Default())

	event := &models.ClusterEvent{
		EventID:   models.GenerateEventID(),
		NodeID:    "self",
		EventType: models.EventNodeHeartbeat,
	}

	// SendEvent should skip self node
	transport.SendEvent(event)
	time.Sleep(100 * time.Millisecond)

	// self should never be in the sent tracking for this event
	if transport.hasSentToPeer("self", event.EventID) {
		t.Error("should not track self as sent")
	}
}

func TestGossipTransport_StartStop(t *testing.T) {
	cfg := &Config{
		Network: NetworkConfig{WireGuardPort: 51820},
		Gossip:  GossipConfig{EventRetention: time.Hour},
	}

	transport := NewGossipTransport(cfg, "test-node", nil, nil, slog.Default())

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := transport.Start(ctx); err != nil {
		t.Fatalf("start failed: %v", err)
	}

	if !transport.IsRunning() {
		t.Error("should be running after start")
	}

	if err := transport.Stop(); err != nil {
		t.Fatalf("stop failed: %v", err)
	}

	if transport.IsRunning() {
		t.Error("should not be running after stop")
	}
}

type mockMembersProvider struct {
	members map[string]*Member
}

func (m *mockMembersProvider) GetActiveMembers() (map[string]*Member, error) {
	return m.members, nil
}
