package config

import (
	"os"
	"path/filepath"
	"testing"
)

// TestDefaultConfig_SyncChatDisabled pins the frozen default for the
// legacy sync-chat opt-in (async-turn-migration leaf 07): the default
// install is async-everywhere — chat.submit acks immediately and results
// arrive via turn.terminal. The blocking chat RPC (110s sync wait, the
// "still running" stub) exists ONLY behind an explicit
// orchestrator.sync_chat_enabled=true during the migration tail.
func TestDefaultConfig_SyncChatDisabled(t *testing.T) {
	c := DefaultConfig()
	if c.Orchestrator.SyncChatEnabled {
		t.Fatal("orchestrator.sync_chat_enabled must default to false: async is the default chat contract")
	}
}

// TestLoadJSON5_SyncChatEnabledRoundTrip verifies the json5 key
// "sync_chat_enabled" parses into OrchestratorConfig.SyncChatEnabled on
// the real Config type (daemon -c path) — the legacy opt-in must actually
// be reachable from a config file.
func TestLoadJSON5_SyncChatEnabledRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "meept.json5")
	content := `{
  "orchestrator": {
    "sync_chat_enabled": true,
  },
}`
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	cfg := DefaultConfig()
	if err := LoadJSON5(path, cfg); err != nil {
		t.Fatalf("LoadJSON5: %v", err)
	}
	if !cfg.Orchestrator.SyncChatEnabled {
		t.Fatal("sync_chat_enabled=true lost in json5 round-trip")
	}
}

// TestTemplateMeeptJSON5_SyncChatEnabledKey verifies the shipped config
// template (config/meept.json5) parses cleanly with the new key present
// and that the template value is false (the template must ship the
// async-by-default posture, not the legacy opt-in).
func TestTemplateMeeptJSON5_SyncChatEnabledKey(t *testing.T) {
	path := filepath.Join("..", "..", "config", "meept.json5")
	cfg := DefaultConfig()
	if err := LoadJSON5(path, cfg); err != nil {
		t.Fatalf("template meept.json5 must parse with sync_chat_enabled present: %v", err)
	}
	if cfg.Orchestrator.SyncChatEnabled {
		t.Fatal("template meept.json5 must ship sync_chat_enabled=false (legacy opt-in stays off by default)")
	}
}
