package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/pelletier/go-toml/v2"
)

// TestDefaultConfig_RPCPeerCredDefaults verifies the peer-credential defaults:
// empty allowlist (no rejection, log-only) and logging enabled.
func TestDefaultConfig_RPCPeerCredDefaults(t *testing.T) {
	cfg := DefaultConfig()
	if len(cfg.Transport.RPC.AllowedUIDs) != 0 {
		t.Errorf("AllowedUIDs = %v, want empty (log-only default)", cfg.Transport.RPC.AllowedUIDs)
	}
	if !cfg.Transport.RPC.PeerCredLog {
		t.Error("PeerCredLog should be true by default")
	}
}

// TestRPCTransportConfig_TOMLRoundTrip verifies both new fields survive a
// TOML marshal/unmarshal round trip with the documented toml keys.
func TestRPCTransportConfig_TOMLRoundTrip(t *testing.T) {
	in := RPCTransportConfig{
		Enabled:     true,
		SocketPath:  "~/.meept/meept.sock",
		AllowedUIDs: []int{501, 502, 503},
		PeerCredLog: false,
	}

	data, err := toml.Marshal(in)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var out RPCTransportConfig
	if err := toml.Unmarshal(data, &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if !out.Enabled || out.SocketPath != in.SocketPath {
		t.Errorf("pre-existing fields did not survive round trip: %+v", out)
	}
	if len(out.AllowedUIDs) != 3 || out.AllowedUIDs[0] != 501 || out.AllowedUIDs[2] != 503 {
		t.Errorf("AllowedUIDs round trip = %v", out.AllowedUIDs)
	}
	if out.PeerCredLog {
		t.Error("PeerCredLog=false did not survive round trip")
	}
}

// TestLoad_RPCPeerCredKeys verifies the documented TOML keys are honored by
// the real loader, including merge-over-defaults behavior for explicit
// peer_cred_log = false.
func TestLoad_RPCPeerCredKeys(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "meept.toml")

	content := `
[transport.rpc]
allowed_uids = [501, 502]
peer_cred_log = false
`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}

	got := cfg.Transport.RPC
	if len(got.AllowedUIDs) != 2 || got.AllowedUIDs[0] != 501 || got.AllowedUIDs[1] != 502 {
		t.Errorf("AllowedUIDs = %v, want [501 502]", got.AllowedUIDs)
	}
	if got.PeerCredLog {
		t.Error("PeerCredLog = true, want false (explicitly disabled)")
	}

	// Defaults must be preserved for unspecified fields.
	if !got.Enabled {
		t.Error("RPC Enabled should stay true from defaults")
	}
	if got.SocketPath != "~/.meept/meept.sock" {
		t.Errorf("SocketPath = %q, want default ~/.meept/meept.sock", got.SocketPath)
	}
}
