package cluster

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/config"
)

func TestLoadClusterConfig_JSON5(t *testing.T) {
	tmpDir := t.TempDir()
	cfgPath := filepath.Join(tmpDir, "cluster.json5")

	cfgData := `
{
  "cluster_id":   "test-cluster",
  "cluster_name": "Test Cluster",
  "network": {
    "wireguard_subnet": "10.200.0.0/24",
    "wireguard_port":   51820,
    "mesh_interface":   "wg0",
  },
  "gossip": {
    "heartbeat_interval": 30000000000,
    "peer_timeout": 120000000000,
  },
  "queue": {
    "default_claim_timeout": 300000000000,
  },
  "git": {
    "sync_interval": 300000000000,
  },
  "security": {
    "require_node_signatures":   true,
    "ed25519_key_rotation_days": 90,
  },
}
`
	if err := os.WriteFile(cfgPath, []byte(cfgData), 0600); err != nil {
		t.Fatalf("failed to write test config: %v", err)
	}

	cfg, err := LoadClusterConfig(cfgPath)
	if err != nil {
		t.Fatalf("load failed: %v", err)
	}

	if cfg.ClusterID != "test-cluster" {
		t.Errorf("ClusterID = %q, want %q", cfg.ClusterID, "test-cluster")
	}
	if cfg.ClusterName != "Test Cluster" {
		t.Errorf("ClusterName = %q, want %q", cfg.ClusterName, "Test Cluster")
	}
	if cfg.Network.WireGuardSubnet != "10.200.0.0/24" {
		t.Errorf("WireGuardSubnet = %q, want %q", cfg.Network.WireGuardSubnet, "10.200.0.0/24")
	}
	if cfg.Network.WireGuardPort != 51820 {
		t.Errorf("WireGuardPort = %d, want %d", cfg.Network.WireGuardPort, 51820)
	}
}

func TestLoadClusterConfig_MissingFile(t *testing.T) {
	_, err := LoadClusterConfig("/no/such/path/cluster.json5")
	if err == nil {
		t.Fatal("expected error for missing file, got nil")
	}
}

func TestLoadClusterConfig_InvalidJSON5(t *testing.T) {
	tmpDir := t.TempDir()
	cfgPath := filepath.Join(tmpDir, "cluster.json5")

	invalidData := `{bad json}`
	if err := os.WriteFile(cfgPath, []byte(invalidData), 0600); err != nil {
		t.Fatalf("failed to write test config: %v", err)
	}

	_, err := LoadClusterConfig(cfgPath)
	if err == nil {
		t.Fatal("expected error for invalid JSON5, got nil")
	}
}

func TestLoadClusterConfig_Defaults(t *testing.T) {
	tmpDir := t.TempDir()
	cfgPath := filepath.Join(tmpDir, "cluster.json5")

	// Minimal config - only cluster_id
	cfgData := `{ "cluster_id": "minimal" }`
	if err := os.WriteFile(cfgPath, []byte(cfgData), 0600); err != nil {
		t.Fatalf("failed to write test config: %v", err)
	}

	cfg, err := LoadClusterConfig(cfgPath)
	if err != nil {
		t.Fatalf("load failed: %v", err)
	}

	if cfg.Gossip.HeartbeatInterval != 30*time.Second {
		t.Errorf("default HeartbeatInterval = %v, want 30s", cfg.Gossip.HeartbeatInterval)
	}
	if cfg.Gossip.PeerTimeout != 2*time.Minute {
		t.Errorf("default PeerTimeout = %v, want 2m", cfg.Gossip.PeerTimeout)
	}
	if cfg.Gossip.MaxRetryAttempts != 3 {
		t.Errorf("default MaxRetryAttempts = %d, want 3", cfg.Gossip.MaxRetryAttempts)
	}
	if cfg.Queue.DefaultClaimTimeout != 5*time.Minute {
		t.Errorf("default DefaultClaimTimeout = %v, want 5m", cfg.Queue.DefaultClaimTimeout)
	}
	if cfg.Queue.NodeReachabilityTimeout != 2*time.Minute {
		t.Errorf("default NodeReachabilityTimeout = %v, want 2m", cfg.Queue.NodeReachabilityTimeout)
	}
	if cfg.Git.SyncInterval != 5*time.Minute {
		t.Errorf("default SyncInterval = %v, want 5m", cfg.Git.SyncInterval)
	}
	if cfg.Security.Ed25519KeyRotationDays != 90 {
		t.Errorf("default Ed25519KeyRotationDays = %d, want 90", cfg.Security.Ed25519KeyRotationDays)
	}
}

func TestSaveClusterConfig(t *testing.T) {
	tmpDir := t.TempDir()
	cfgPath := filepath.Join(tmpDir, "cluster.json5")

	cfg := &Config{
		ClusterID:   "saved-cluster",
		ClusterName: "Saved Cluster",
		Network: NetworkConfig{
			WireGuardSubnet: "10.200.1.0/24",
			WireGuardPort:   51821,
			Interface:       "wg1",
		},
		Gossip: GossipConfig{
			HeartbeatInterval: 60 * time.Second,
		},
	}

	if err := SaveClusterConfig(cfgPath, cfg); err != nil {
		t.Fatalf("save failed: %v", err)
	}

	// Verify file was created
	if _, err := os.Stat(cfgPath); err != nil {
		t.Fatalf("config file not found after save: %v", err)
	}

	// Reload and verify
	loaded, err := LoadClusterConfig(cfgPath)
	if err != nil {
		t.Fatalf("reload failed: %v", err)
	}
	if loaded.ClusterID != cfg.ClusterID {
		t.Errorf("reloaded ClusterID = %q, want %q", loaded.ClusterID, cfg.ClusterID)
	}
	if loaded.Network.WireGuardPort != cfg.Network.WireGuardPort {
		t.Errorf("reloaded WireGuardPort = %d, want %d", loaded.Network.WireGuardPort, cfg.Network.WireGuardPort)
	}
}

func TestSaveClusterConfig_CreatesParentDir(t *testing.T) {
	tmpDir := t.TempDir()
	cfgPath := filepath.Join(tmpDir, "subdir", "cluster.json5")

	cfg := &Config{
		ClusterID: "mkdir-test",
	}

	if err := SaveClusterConfig(cfgPath, cfg); err != nil {
		t.Fatalf("save failed: %v", err)
	}

	loaded, err := LoadClusterConfig(cfgPath)
	if err != nil {
		t.Fatalf("load failed: %v", err)
	}
	if loaded.ClusterID != cfg.ClusterID {
		t.Errorf("ClusterID mismatch after mkdir")
	}
}

func TestMember_MarshalUnmarshal(t *testing.T) {
	member := &Member{
		NodeID:       "node-01",
		NodeName:     "Test Node",
		WireGuardPub: "XyZabc123",
		SigningPub:   []byte{0x01, 0x02, 0x03},
		Endpoint:     "192.168.1.42:51820",
		Capabilities: []string{"coder", "analyst"},
		ClusterIP:    "10.200.0.1",
		Status:       "active",
		JoinedAt:     time.Date(2026, 6, 6, 12, 0, 0, 0, time.UTC),
	}

	data, err := json.Marshal(member)
	if err != nil {
		t.Fatalf("marshal failed: %v", err)
	}

	var loaded Member
	if err := config.UnmarshalJSON5(data, &loaded); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}

	if loaded.NodeID != member.NodeID {
		t.Error("NodeID mismatch")
	}
	if loaded.NodeName != member.NodeName {
		t.Errorf("NodeName = %q, want %q", loaded.NodeName, member.NodeName)
	}
	if string(loaded.SigningPub) != string(member.SigningPub) {
		t.Error("SigningPub mismatch")
	}
	if len(loaded.Capabilities) != len(member.Capabilities) {
		t.Errorf("Capabilities length = %d, want %d", len(loaded.Capabilities), len(member.Capabilities))
	}
	if loaded.ClusterIP != member.ClusterIP {
		t.Errorf("ClusterIP = %q, want %q", loaded.ClusterIP, member.ClusterIP)
	}
}

func TestLoadSaveMember(t *testing.T) {
	tmpDir := t.TempDir()

	member := &Member{
		NodeID:       "node-02",
		NodeName:     "Another Node",
		WireGuardPub: "abcXYZ789",
		Endpoint:     "10.0.0.5:51820",
		SigningPub:   []byte{0xAB, 0xCD},
		Capabilities: []string{"debugger", "planner"},
		ClusterIP:    "10.200.0.2",
		Status:       "active",
		LastHeartbeat: time.Date(2026, 6, 6, 14, 30, 0, 0, time.UTC),
	}

	if err := SaveMember(tmpDir, member); err != nil {
		t.Fatalf("SaveMember failed: %v", err)
	}

	loaded, err := LoadMember(tmpDir, "node-02")
	if err != nil {
		t.Fatalf("LoadMember failed: %v", err)
	}

	if loaded.NodeID != member.NodeID {
		t.Error("NodeID mismatch after load/save")
	}
	if loaded.NodeName != member.NodeName {
		t.Error("NodeName mismatch")
	}
	if loaded.Endpoint != member.Endpoint {
		t.Error("Endpoint mismatch")
	}
}

func TestLoadMember_NotFound(t *testing.T) {
	_, err := LoadMember("/tmp/nonexistent", "node-999")
	if err == nil {
		t.Fatal("expected error for non-existent member, got nil")
	}
}

func TestMemberPath(t *testing.T) {
	path := MemberPath("/var/cluster", "node-42")
	expected := "/var/cluster/nodes/node-42.json5"
	if path != expected {
		t.Errorf("MemberPath = %q, want %q", path, expected)
	}
}

func TestClusterConfig_JSON5WithComments(t *testing.T) {
	tmpDir := t.TempDir()
	cfgPath := filepath.Join(tmpDir, "cluster.json5")

	// JSON5 with comments and trailing commas
	cfgData := `{
  // Cluster identity
  "cluster_id": "commented-cluster",   // unique identifier
  "cluster_name": "With Comments",

  "network": {
    "wireguard_subnet": "10.200.0.0/24",
    "wireguard_port":   51820,
    "mesh_interface":   "wg0",
  },
}`
	if err := os.WriteFile(cfgPath, []byte(cfgData), 0600); err != nil {
		t.Fatalf("failed to write test config: %v", err)
	}

	cfg, err := LoadClusterConfig(cfgPath)
	if err != nil {
		t.Fatalf("load failed: %v", err)
	}
	if cfg.ClusterID != "commented-cluster" {
		t.Errorf("ClusterID = %q, want %q", cfg.ClusterID, "commented-cluster")
	}
}
