package rpc

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/cluster"
)

func newTestClusterHandler(cfg *cluster.Config) *ClusterHandler {
	return NewClusterHandler(nil, nil, cfg)
}

func TestHandleJoin_ValidKey(t *testing.T) {
	t.Parallel()

	cfg := &cluster.Config{
		ClusterID:   "cl-123",
		ClusterName: "Test Cluster",
		NodeID:      "node-01",
		JoinKey:     "secret-key-abc",
		Network: cluster.NetworkConfig{
			WireGuardSubnet: "10.200.0.0/24",
			WireGuardPort:   51820,
			Interface:       "wg0",
		},
		Gossip: cluster.GossipConfig{
			HeartbeatInterval: 30 * time.Second,
			PeerTimeout:       2 * time.Minute,
			EventRetention:    time.Hour,
			MaxRetryAttempts:  3,
		},
		Queue: cluster.QueueConfig{
			DefaultClaimTimeout:     5 * time.Minute,
			NodeReachabilityTimeout: 2 * time.Minute,
			FullPayloadReplication:  true,
		},
		Security: cluster.SecurityConfig{
			RequireNodeSignatures:  true,
			Ed25519KeyRotationDays: 90,
		},
	}

	h := newTestClusterHandler(cfg)

	params, _ := json.Marshal(map[string]string{"join_key": "secret-key-abc"})
	result, err := h.handleJoin(context.Background(), params)
	if err != nil {
		t.Fatalf("handleJoin with valid key returned error: %v", err)
	}

	m, ok := result.(map[string]any)
	if !ok {
		t.Fatal("expected map[string]any result")
	}

	if m["cluster_name"] != "Test Cluster" {
		t.Errorf("cluster_name = %v, want Test Cluster", m["cluster_name"])
	}
	if m["cluster_id"] != "cl-123" {
		t.Errorf("cluster_id = %v, want cl-123", m["cluster_id"])
	}
	if m["node_id"] != "node-01" {
		t.Errorf("node_id = %v, want node-01", m["node_id"])
	}

	// Verify config payload is a proper JSON object with the right fields.
	cfgRaw, ok := m["config"].(json.RawMessage)
	if !ok || len(cfgRaw) == 0 {
		t.Fatal("expected non-empty config json.RawMessage")
	}

	var joinResp JoinResponse
	if err := json.Unmarshal(cfgRaw, &joinResp); err != nil {
		t.Fatalf("failed to unmarshal config into JoinResponse: %v", err)
	}
	if joinResp.ClusterID != "cl-123" {
		t.Errorf("config.cluster_id = %q, want cl-123", joinResp.ClusterID)
	}
	if joinResp.Network.WireGuardSubnet != "10.200.0.0/24" {
		t.Errorf("config.network.wireguard_subnet = %q, want 10.200.0.0/24", joinResp.Network.WireGuardSubnet)
	}
	if joinResp.Queue.FullPayloadReplication != true {
		t.Errorf("config.queue.full_payload_replication = %v, want true", joinResp.Queue.FullPayloadReplication)
	}
	if joinResp.Security.RequireNodeSignatures != true {
		t.Errorf("config.security.require_node_signatures = %v, want true", joinResp.Security.RequireNodeSignatures)
	}
}

func TestHandleJoin_InvalidKey(t *testing.T) {
	t.Parallel()

	cfg := &cluster.Config{
		ClusterID:   "cl-123",
		ClusterName: "Test Cluster",
		NodeID:      "node-01",
		JoinKey:     "secret-key-abc",
	}

	h := newTestClusterHandler(cfg)

	params, _ := json.Marshal(map[string]string{"join_key": "wrong-key"})
	_, err := h.handleJoin(context.Background(), params)
	if err == nil {
		t.Fatal("expected error for invalid join key, got nil")
	}
	if !strings.Contains(err.Error(), "invalid join key") {
		t.Errorf("error = %q, want 'invalid join key'", err.Error())
	}
}

func TestHandleJoin_OpenMode(t *testing.T) {
	t.Parallel()

	cfg := &cluster.Config{
		ClusterID:   "cl-open",
		ClusterName: "Open Cluster",
		NodeID:      "node-01",
		JoinKey:     "", // empty join key = open mode
	}

	h := newTestClusterHandler(cfg)

	// Any key should be accepted in open mode.
	params, _ := json.Marshal(map[string]string{"join_key": "anything-goes"})
	result, err := h.handleJoin(context.Background(), params)
	if err != nil {
		t.Fatalf("handleJoin in open mode returned error: %v", err)
	}

	m, ok := result.(map[string]any)
	if !ok {
		t.Fatal("expected map[string]any result")
	}
	if m["cluster_name"] != "Open Cluster" {
		t.Errorf("cluster_name = %v, want Open Cluster", m["cluster_name"])
	}
}

func TestHandleJoin_EmptyKey(t *testing.T) {
	t.Parallel()

	cfg := &cluster.Config{
		ClusterID:   "cl-123",
		ClusterName: "Test Cluster",
		NodeID:      "node-01",
	}

	h := newTestClusterHandler(cfg)

	params, _ := json.Marshal(map[string]string{"join_key": ""})
	_, err := h.handleJoin(context.Background(), params)
	if err == nil {
		t.Fatal("expected error for empty join key, got nil")
	}
	if !strings.Contains(err.Error(), "join_key is required") {
		t.Errorf("error = %q, want 'join_key is required'", err.Error())
	}
}

func TestHandleJoin_NilConfig(t *testing.T) {
	t.Parallel()

	h := &ClusterHandler{cfg: nil}

	params, _ := json.Marshal(map[string]string{"join_key": "some-key"})
	_, err := h.handleJoin(context.Background(), params)
	if err == nil {
		t.Fatal("expected error for nil config, got nil")
	}
	if !strings.Contains(err.Error(), "cluster not configured") {
		t.Errorf("error = %q, want 'cluster not configured'", err.Error())
	}
}

func TestHandleJoin_InvalidParams(t *testing.T) {
	t.Parallel()

	cfg := &cluster.Config{
		ClusterID: "cl-123",
	}
	h := newTestClusterHandler(cfg)

	// Invalid JSON should be rejected.
	_, err := h.handleJoin(context.Background(), json.RawMessage(`{bad json}`))
	if err == nil {
		t.Fatal("expected error for invalid params, got nil")
	}
	if !strings.Contains(err.Error(), "invalid join request") {
		t.Errorf("error = %q, want 'invalid join request'", err.Error())
	}
}

func TestJoinResponse_OmitsJoinKey(t *testing.T) {
	t.Parallel()

	cfg := &cluster.Config{
		ClusterID:   "cl-123",
		ClusterName: "Test Cluster",
		NodeID:      "node-01",
		JoinKey:     "secret-key-abc",
		Network: cluster.NetworkConfig{
			WireGuardSubnet: "10.200.0.0/24",
			WireGuardPort:   51820,
			Interface:       "wg0",
		},
		Gossip: cluster.GossipConfig{
			HeartbeatInterval: 30 * time.Second,
			PeerTimeout:       2 * time.Minute,
			EventRetention:    time.Hour,
			MaxRetryAttempts:  3,
		},
		Queue: cluster.QueueConfig{
			DefaultClaimTimeout:     5 * time.Minute,
			NodeReachabilityTimeout: 2 * time.Minute,
			FullPayloadReplication:  true,
		},
		Security: cluster.SecurityConfig{
			RequireNodeSignatures:  true,
			Ed25519KeyRotationDays: 90,
		},
	}

	h := newTestClusterHandler(cfg)

	params, _ := json.Marshal(map[string]string{"join_key": "secret-key-abc"})
	result, err := h.handleJoin(context.Background(), params)
	if err != nil {
		t.Fatalf("handleJoin returned error: %v", err)
	}

	m := result.(map[string]any)
	cfgRaw := m["config"].(json.RawMessage)

	// Verify the response does NOT contain the join_key in the config payload.
	if strings.Contains(string(cfgRaw), "join_key") {
		t.Errorf("config payload should not contain join_key for security, got: %s", string(cfgRaw))
	}
}
