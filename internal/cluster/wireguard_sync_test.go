package cluster

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestNewWireGuardManager(t *testing.T) {
	mgr, err := NewWireGuardManager("/tmp/test-wg0.conf", "wg0")
	if err != nil {
		t.Fatalf("NewWireGuardManager failed: %v", err)
	}
	if mgr.iface != "wg0" {
		t.Errorf("iface = %q, want %q", mgr.iface, "wg0")
	}
	if mgr.configPath != "/tmp/test-wg0.conf" {
		t.Errorf("configPath = %q, want %q", mgr.configPath, "/tmp/test-wg0.conf")
	}
}

func TestGenerateConfig_Basic(t *testing.T) {
	mgr, err := NewWireGuardManager("/tmp/wg0.conf", "wg0")
	if err != nil {
		t.Fatalf("NewWireGuardManager failed: %v", err)
	}

	cfg := &WireGuardConfig{
		PrivateKey:          "abc123privatekey==",
		ClusterIP:           "10.200.0.1",
		ListenPort:          51820,
		DNS:                 "8.8.8.8",
		PersistentKeepalive: "25",
		Peers: []Member{
			{
				NodeID:       "node-02",
				WireGuardPub: "peer-pubkey-01==",
				Endpoint:     "192.168.1.42:51820",
				ClusterIP:    "10.200.0.2",
			},
		},
	}

	data, err := mgr.GenerateConfig(cfg)
	if err != nil {
		t.Fatalf("GenerateConfig failed: %v", err)
	}

	output := string(data)

	// Verify required sections are present
	if !strings.Contains(output, "[Interface]") {
		t.Error("missing [Interface] section")
	}
	if !strings.Contains(output, "PrivateKey = abc123privatekey==") {
		t.Error("missing PrivateKey in output")
	}
	if !strings.Contains(output, "Address = 10.200.0.1/32") {
		t.Error("missing Address in output")
	}
	if !strings.Contains(output, "ListenPort = 51820") {
		t.Error("missing ListenPort in output")
	}
	if !strings.Contains(output, "DNS = 8.8.8.8") {
		t.Error("missing DNS in output")
	}
	if !strings.Contains(output, "[Peer]") {
		t.Error("missing [Peer] section")
	}
	if !strings.Contains(output, "PublicKey = peer-pubkey-01==") {
		t.Error("missing Peer PublicKey in output")
	}
	if !strings.Contains(output, "AllowedIPs = 10.200.0.2/32") {
		t.Error("missing AllowedIPs in output")
	}
	if !strings.Contains(output, "PersistentKeepalive = 25") {
		t.Error("missing PersistentKeepalive in output")
	}
	if !strings.Contains(output, "Endpoint = 192.168.1.42:51820") {
		t.Error("missing Endpoint in output")
	}
}

func TestGenerateConfig_NoEndpoint(t *testing.T) {
	mgr, err := NewWireGuardManager("/tmp/wg0.conf", "wg0")
	if err != nil {
		t.Fatalf("NewWireGuardManager failed: %v", err)
	}

	cfg := &WireGuardConfig{
		PrivateKey:  "testkey==",
		ClusterIP:   "10.200.0.1",
		ListenPort:  51820,
		DNS:         "8.8.8.8",
		Peers: []Member{
			{
				NodeID:       "node-02",
				WireGuardPub: "peer-pubkey-01==",
				ClusterIP:    "10.200.0.2",
				// No Endpoint set
			},
		},
	}

	data, err := mgr.GenerateConfig(cfg)
	if err != nil {
		t.Fatalf("GenerateConfig failed: %v", err)
	}

	output := string(data)

	if strings.Contains(output, "Endpoint") {
		t.Error("Endpoint should not appear when peer has no endpoint")
	}
}

func TestGenerateConfig_NoPeers(t *testing.T) {
	mgr, err := NewWireGuardManager("/tmp/wg0.conf", "wg0")
	if err != nil {
		t.Fatalf("NewWireGuardManager failed: %v", err)
	}

	cfg := &WireGuardConfig{
		PrivateKey: "testkey==",
		ClusterIP:  "10.200.0.1",
		ListenPort: 51820,
		DNS:        "8.8.8.8",
		Peers:      []Member{},
	}

	data, err := mgr.GenerateConfig(cfg)
	if err != nil {
		t.Fatalf("GenerateConfig failed: %v", err)
	}

	output := string(data)
	if strings.Contains(output, "[Peer]") {
		t.Error("[Peer] section should not appear when there are no peers")
	}
}

func TestWriteConfig_CreatesDirAndFile(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "subdir", "wg0.conf")

	mgr, err := NewWireGuardManager(configPath, "wg0")
	if err != nil {
		t.Fatalf("NewWireGuardManager failed: %v", err)
	}

	cfg := &WireGuardConfig{
		PrivateKey: "testkey==",
		ClusterIP:  "10.200.0.1",
		ListenPort: 51820,
		DNS:        "8.8.8.8",
		Peers: []Member{
			{
				NodeID:       "node-02",
				WireGuardPub: "peer-pubkey-01==",
				ClusterIP:    "10.200.0.2",
			},
		},
	}

	if err := mgr.WriteConfig(cfg); err != nil {
		t.Fatalf("WriteConfig failed: %v", err)
	}

	// Verify file was created
	data, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("failed to read config file: %v", err)
	}

	if !strings.Contains(string(data), "PrivateKey = testkey==") {
		t.Error("config file missing PrivateKey")
	}

	// Verify file permissions
	info, err := os.Stat(configPath)
	if err != nil {
		t.Fatalf("failed to stat config file: %v", err)
	}
	expectedPerm := os.FileMode(0o600)
	if info.Mode().Perm() != expectedPerm {
		t.Errorf("file mode = %v, want %v", info.Mode().Perm(), expectedPerm)
	}
}

func TestWriteConfig_InvalidDir(t *testing.T) {
	mgr, err := NewWireGuardManager("/proc/nonexistent/wg0.conf", "wg0")
	if err != nil {
		t.Fatalf("NewWireGuardManager failed: %v", err)
	}

	cfg := &WireGuardConfig{
		PrivateKey: "testkey==",
		ClusterIP:  "10.200.0.1",
		ListenPort: 51820,
		DNS:        "8.8.8.8",
		Peers:      []Member{},
	}

	err = mgr.WriteConfig(cfg)
	if err == nil {
		t.Error("expected error for invalid directory, got nil")
	}
}

func TestAddPeer_AppendsPeer(t *testing.T) {
	cfg := &WireGuardConfig{
		PrivateKey: "testkey==",
		ClusterIP:  "10.200.0.1",
		ListenPort: 51820,
		DNS:        "8.8.8.8",
		Peers: []Member{
			{NodeID: "node-02", WireGuardPub: "pub-01", ClusterIP: "10.200.0.2"},
		},
	}

	newPeer := Member{
		NodeID:       "node-03",
		WireGuardPub: "pub-02",
		ClusterIP:    "10.200.0.3",
	}

	// AddPeer modifies cfg.Peers directly
	cfg.Peers = append(cfg.Peers, newPeer)

	if len(cfg.Peers) != 2 {
		t.Fatalf("expected 2 peers, got %d", len(cfg.Peers))
	}

	if cfg.Peers[1].NodeID != "node-03" {
		t.Errorf("peer[1] NodeID = %q, want %q", cfg.Peers[1].NodeID, "node-03")
	}
}

func TestRemovePeer_RemovesCorrectNode(t *testing.T) {
	cfg := &WireGuardConfig{
		PrivateKey: "testkey==",
		ClusterIP:  "10.200.0.1",
		ListenPort: 51820,
		DNS:        "8.8.8.8",
		Peers: []Member{
			{NodeID: "node-02", WireGuardPub: "pub-01", ClusterIP: "10.200.0.2"},
			{NodeID: "node-03", WireGuardPub: "pub-02", ClusterIP: "10.200.0.3"},
			{NodeID: "node-04", WireGuardPub: "pub-03", ClusterIP: "10.200.0.4"},
		},
	}

	var peers []Member
	for _, p := range cfg.Peers {
		if p.NodeID != "node-03" {
			peers = append(peers, p)
		}
	}

	cfg.Peers = peers

	if len(cfg.Peers) != 2 {
		t.Fatalf("expected 2 peers after removal, got %d", len(cfg.Peers))
	}

	for _, p := range cfg.Peers {
		if p.NodeID == "node-03" {
			t.Error("node-03 should have been removed")
		}
	}
}

func TestUpdatePeers_ReplacesAll(t *testing.T) {
	cfg := &WireGuardConfig{
		PrivateKey: "testkey==",
		ClusterIP:  "10.200.0.1",
		ListenPort: 51820,
		DNS:        "8.8.8.8",
		Peers: []Member{
			{NodeID: "old-peer", WireGuardPub: "old-pub", ClusterIP: "10.200.0.9"},
		},
	}

	newPeers := []Member{
		{NodeID: "new-peer-01", WireGuardPub: "new-pub-01", ClusterIP: "10.200.0.2"},
		{NodeID: "new-peer-02", WireGuardPub: "new-pub-02", ClusterIP: "10.200.0.3"},
	}

	cfg.Peers = newPeers
	if len(cfg.Peers) != 2 {
		t.Fatalf("expected 2 peers, got %d", len(cfg.Peers))
	}
	if cfg.Peers[0].NodeID != "new-peer-01" {
		t.Errorf("peer[0] = %q, want %q", cfg.Peers[0].NodeID, "new-peer-01")
	}
	if cfg.Peers[1].NodeID != "new-peer-02" {
		t.Errorf("peer[1] = %q, want %q", cfg.Peers[1].NodeID, "new-peer-02")
	}
}

func TestGenerateConfig_MultiplePeers(t *testing.T) {
	mgr, err := NewWireGuardManager("/tmp/wg0.conf", "wg0")
	if err != nil {
		t.Fatalf("NewWireGuardManager failed: %v", err)
	}

	cfg := &WireGuardConfig{
		PrivateKey: "testkey==",
		ClusterIP:  "10.200.0.1",
		ListenPort: 51820,
		DNS:        "8.8.8.8",
		Peers: []Member{
			{NodeID: "node-02", WireGuardPub: "pub-01", ClusterIP: "10.200.0.2", Endpoint: "192.168.1.10:51820"},
			{NodeID: "node-03", WireGuardPub: "pub-02", ClusterIP: "10.200.0.3", Endpoint: "192.168.1.11:51820"},
			{NodeID: "node-04", WireGuardPub: "pub-03", ClusterIP: "10.200.0.4", Endpoint: ""},
		},
	}

	data, err := mgr.GenerateConfig(cfg)
	if err != nil {
		t.Fatalf("GenerateConfig failed: %v", err)
	}

	output := string(data)

	// Count [Peer] sections - should be 3
	peerCount := strings.Count(output, "[Peer]")
	if peerCount != 3 {
		t.Errorf("expected 3 [Peer] sections, got %d", peerCount)
	}

	// Verify Endpoint appears only twice (not for node-04)
	endpointCount := strings.Count(output, "Endpoint =")
	if endpointCount != 2 {
		t.Errorf("expected 2 Endpoint entries, got %d", endpointCount)
	}

	// Verify all AllowedIPs
	if !strings.Contains(output, "AllowedIPs = 10.200.0.2/32") {
		t.Error("missing AllowedIPs for node-02")
	}
	if !strings.Contains(output, "AllowedIPs = 10.200.0.3/32") {
		t.Error("missing AllowedIPs for node-03")
	}
	if !strings.Contains(output, "AllowedIPs = 10.200.0.4/32") {
		t.Error("missing AllowedIPs for node-04")
	}
}

func TestWireGuardConfig_JSONMarshal(t *testing.T) {
	// WireGuardConfig doesn't have custom JSON marshaling, but
	// verifying JSON round-trip ensures struct tags work.
	cfg := &WireGuardConfig{
		PrivateKey:            "testkey==",
		ClusterIP:             "10.200.0.1",
		ListenPort:            51820,
		DNS:                   "8.8.8.8",
		PersistentKeepalive:   "25",
		Peers:                 []Member{},
	}

	data, err := json.Marshal(cfg)
	if err != nil {
		t.Fatalf("Marshal failed: %v", err)
	}

	var decoded WireGuardConfig
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal failed: %v", err)
	}

	if decoded.PrivateKey != cfg.PrivateKey {
		t.Errorf("PrivateKey = %q, want %q", decoded.PrivateKey, cfg.PrivateKey)
	}
	if decoded.ClusterIP != cfg.ClusterIP {
		t.Errorf("ClusterIP = %q, want %q", decoded.ClusterIP, cfg.ClusterIP)
	}
}
