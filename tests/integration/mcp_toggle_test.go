// Package integration contains the MCP toggle integration test for Phase 9
// of the MCP default catalog plan. It exercises the persist + reload flow
// end-to-end: write config → Manager.Reload → SaveMCPConfig → Reload again
// with a toggled entry → verify AllServerStatuses reflects the new state.
//
// This is a higher-level test than the unit tests in
// internal/tools/mcp/manager_stats_test.go. It does not spin up an RPC
// server; instead it drives Manager + config.SaveMCPConfig directly,
// mirroring what the mcp.set_enabled RPC handler does.
package integration

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/config"
	"github.com/caimlas/meept/internal/tools/mcp"
)

// stubServerScript is a minimal MCP-responder script that stays alive.
// Shared with the unit tests via duplication (tests/integration is a
// separate package and cannot import internal test helpers).
const stubServerScript = `#!/bin/bash
# Minimal MCP responder for integration tests.
while read -r line; do
  case "$line" in
    *'"method":"initialize"'*)
      echo '{"jsonrpc":"2.0","id":1,"result":{"serverInfo":{"name":"stub","version":"1.0.0"},"capabilities":{"tools":{}}}}'
      ;;
    *'"method":"notifications/initialized"'*)
      : # notification, no response
      ;;
    *'"method":"tools/list"'*)
      echo '{"jsonrpc":"2.0","id":2,"result":{"tools":[]}}'
      ;;
  esac
done
`

// writeStubScript writes the stub server script to a temp dir and returns
// its path.
func writeStubScript(t *testing.T) string {
	t.Helper()
	tempDir := t.TempDir()
	scriptPath := filepath.Join(tempDir, "stub.sh")
	//nolint:gosec // test script, temp dir
	if err := os.WriteFile(scriptPath, []byte(stubServerScript), 0o755); err != nil {
		t.Fatalf("write stub script: %v", err)
	}
	return scriptPath
}

// findStatus returns the ServerStatusEntry whose Config.Name matches name,
// or nil if not found.
func findStatus(entries []mcp.ServerStatusEntry, name string) *mcp.ServerStatusEntry {
	for i := range entries {
		if entries[i].Config.Name == name {
			return &entries[i]
		}
	}
	return nil
}

// TestMCPToggle_PersistAndReload simulates the mcp.set_enabled flow:
//  1. Persist an initial config with one enabled server (stub script) and
//     one disabled server (guaranteed-fail `false` builtin).
//  2. Reload the Manager from that config; verify active + disabled states.
//  3. Toggle: persist a modified config (disable the active server) and
//     reload again.
//  4. Verify AllServerStatuses reflects the post-toggle state.
//
// Uses config.SaveMCPConfig for atomic writes (exercises the real persist
// path used by the RPC/HTTP handlers).
func TestMCPToggle_PersistAndReload(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping MCP toggle integration test in short mode")
	}

	scriptPath := writeStubScript(t)
	configPath := filepath.Join(t.TempDir(), "mcp_servers.json5")

	manager := mcp.NewManager(nil)
	t.Cleanup(manager.StopAll)

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	// --- Phase 1: initial config with one enabled, one disabled ---
	initial := &config.MCPServersConfig{
		Servers: []mcp.ServerConfig{
			{
				Name:        "alive",
				Enabled:     boolPtrTrue(true),
				Command:     []string{"bash", scriptPath},
				Type:        "stdio",
				Description: "stub server that stays running",
				Category:    "test",
			},
			{
				Name:        "broken",
				Enabled:     boolPtrFalse(false),
				Command:     []string{"false"},
				Type:        "stdio",
				Description: "guaranteed-fail command, disabled",
				Category:    "test",
			},
		},
	}

	if err := config.SaveMCPConfig(configPath, initial); err != nil {
		t.Fatalf("SaveMCPConfig (initial): %v", err)
	}

	// Verify the file landed on disk and can be read back.
	loaded, err := config.LoadMCPConfig(configPath)
	if err != nil {
		t.Fatalf("LoadMCPConfig (initial): %v", err)
	}
	if len(loaded.Servers) != 2 {
		t.Fatalf("expected 2 loaded servers; got %d", len(loaded.Servers))
	}

	// Reload the Manager from the loaded config.
	if err := manager.Reload(ctx, loaded.Servers); err != nil {
		t.Fatalf("Reload (initial): %v", err)
	}

	// Verify states: "alive" active, "broken" disabled.
	entries := manager.AllServerStatuses()
	if len(entries) != 2 {
		t.Fatalf("expected 2 entries after reload; got %d", len(entries))
	}

	alive := findStatus(entries, "alive")
	if alive == nil {
		t.Fatal("expected 'alive' entry")
	}
	if alive.Stats.State != mcp.StateActive {
		t.Errorf("expected 'alive' state=%q; got %q", mcp.StateActive, alive.Stats.State)
	}
	if !alive.Config.IsEnabled() {
		t.Error("expected 'alive' config to be enabled")
	}

	broken := findStatus(entries, "broken")
	if broken == nil {
		t.Fatal("expected 'broken' entry")
	}
	if broken.Stats.State != mcp.StateDisabled {
		t.Errorf("expected 'broken' state=%q; got %q", mcp.StateDisabled, broken.Stats.State)
	}
	if broken.Config.IsEnabled() {
		t.Error("expected 'broken' config to be disabled")
	}

	// --- Phase 2: toggle — disable "alive", enable "broken" ---
	// Find and mutate the entries by name (matches RPC handler logic).
	for i := range loaded.Servers {
		switch loaded.Servers[i].Name {
		case "alive":
			loaded.Servers[i].Enabled = boolPtrFalse(false)
		case "broken":
			loaded.Servers[i].Enabled = boolPtrTrue(true)
		}
	}

	if err := config.SaveMCPConfig(configPath, loaded); err != nil {
		t.Fatalf("SaveMCPConfig (toggled): %v", err)
	}

	// Reload from the freshly-persisted config (simulates the daemon's
	// reload-after-persist path). The "broken" server (command `false`)
	// is expected to fail during Connect; Reload surfaces this via
	// errors.Join but still records the StateError stats entry, so we
	// don't treat the returned error as fatal here — we verify the
	// resulting state below.
	if err := manager.Reload(ctx, loaded.Servers); err != nil {
		t.Logf("Reload (toggled) returned expected error for broken server: %v", err)
	}

	// Verify "broken" is now in error state (it was enabled but the command
	// fails) and "alive" is now disabled.
	entries = manager.AllServerStatuses()
	if len(entries) != 2 {
		t.Fatalf("expected 2 entries after toggle; got %d", len(entries))
	}

	aliveAfter := findStatus(entries, "alive")
	if aliveAfter == nil {
		t.Fatal("expected 'alive' entry after toggle")
	}
	if aliveAfter.Stats.State != mcp.StateDisabled {
		t.Errorf("expected 'alive' state=%q after toggle; got %q", mcp.StateDisabled, aliveAfter.Stats.State)
	}
	if aliveAfter.Config.IsEnabled() {
		t.Error("expected 'alive' config to be disabled after toggle")
	}
	// "alive" should no longer be in the clients map.
	if c := manager.GetClient("alive"); c != nil {
		t.Errorf("expected 'alive' client to be stopped after toggle; got %v", c)
	}

	brokenAfter := findStatus(entries, "broken")
	if brokenAfter == nil {
		t.Fatal("expected 'broken' entry after toggle")
	}
	if brokenAfter.Stats.State != mcp.StateError {
		t.Errorf("expected 'broken' state=%q after toggle; got %q", mcp.StateError, brokenAfter.Stats.State)
	}
	if !brokenAfter.Config.IsEnabled() {
		t.Error("expected 'broken' config to be enabled after toggle")
	}
	if brokenAfter.Stats.LastError == "" {
		t.Error("expected 'broken' to have LastError populated after failed start")
	}
}

// boolPtrTrue returns a pointer to b. Named distinctly from the unit-test
// helper to avoid confusion about which package owns it.
func boolPtrTrue(b bool) *bool { return &b }

// boolPtrFalse returns a pointer to b. Named distinctly from the unit-test
// helper to avoid confusion about which package owns it.
func boolPtrFalse(b bool) *bool { return &b }
