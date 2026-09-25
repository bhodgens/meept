package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// TestDefaultConfig_SyncWaitStall pins the stall-based sync-wait defaults:
// sync_wait_stall zero (= stall detection ENABLED, see the schema comment)
// and sync_wait_max 30m (the hard elapsed cap).
func TestDefaultConfig_SyncWaitStall(t *testing.T) {
	c := DefaultConfig()
	if c.Orchestrator.SyncWaitStall != 0 {
		t.Fatalf("orchestrator.sync_wait_stall = %v, want 0 (stall-based ceiling enabled by default)", c.Orchestrator.SyncWaitStall)
	}
	if c.Orchestrator.SyncWaitMax != 30*time.Minute {
		t.Fatalf("orchestrator.sync_wait_max = %v, want 30m", c.Orchestrator.SyncWaitMax)
	}
}

// TestLoadJSON5_SyncWaitStallRoundTrip verifies both keys round-trip
// through the real json5 loader, including the negative escape hatch.
func TestLoadJSON5_SyncWaitStallRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "meept.json5")
	content := `{
  "orchestrator": {
    "sync_wait_stall": -1,
    "sync_wait_max": "45m",
  },
}`
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	cfg := DefaultConfig()
	if err := LoadJSON5(path, cfg); err != nil {
		t.Fatalf("LoadJSON5: %v", err)
	}
	if cfg.Orchestrator.SyncWaitStall >= 0 {
		t.Fatalf("sync_wait_stall = %v, want the negative escape hatch", cfg.Orchestrator.SyncWaitStall)
	}
	if cfg.Orchestrator.SyncWaitMax != 45*time.Minute {
		t.Fatalf("sync_wait_max = %v, want 45m", cfg.Orchestrator.SyncWaitMax)
	}
}
