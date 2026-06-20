package rpc

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"

	"github.com/caimlas/meept/internal/config"
	"github.com/caimlas/meept/internal/tools/mcp"
)

// stubToolRefresher is a minimal MCPToolRefresher that counts SyncMCPTools
// invocations. Used to verify handleSetEnabled calls the refresher exactly
// once after a successful Reload.
type stubToolRefresher struct {
	calls int32
	err   error // returned from SyncMCPTools; nil by default
}

func (s *stubToolRefresher) SyncMCPTools() error {
	atomic.AddInt32(&s.calls, 1)
	return s.err
}

// writeMCPConfigTmp writes servers to a temp mcp_servers.json5 and returns
// the path. Used to give handleSetEnabled a real on-disk config to mutate.
func writeMCPConfigTmp(t *testing.T, servers []mcp.ServerConfig) string {
	t.Helper()
	cfg := &config.MCPServersConfig{Servers: servers}
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "mcp_servers.json5")
	if err := config.SaveMCPConfig(cfgPath, cfg); err != nil {
		t.Fatalf("SaveMCPConfig: %v", err)
	}
	return cfgPath
}

// stubMCPServerScript writes a tiny bash script that responds to MCP
// initialize + tools/list with a single canned tool. Returns the path.
// Used so handleSetEnabled's Manager.Reload can actually start the server.
func stubMCPServerScript(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "stub-mcp.sh")
	// The script reads JSON-RPC requests line by line and replies. The
	// transport waits for the initialize result, then sends tools/list.
	content := `#!/usr/bin/env bash
while IFS= read -r line; do
    if echo "$line" | grep -q '"method":"initialize"'; then
        echo '{"jsonrpc":"2.0","id":1,"result":{"serverInfo":{"name":"stub","version":"1.0.0"},"capabilities":{"tools":{}}}}'
    elif echo "$line" | grep -q '"method":"tools/list"'; then
        echo '{"jsonrpc":"2.0","id":2,"result":{"tools":[{"name":"ping","description":"stub","inputSchema":{"type":"object","properties":{}}}]}}'
    fi
done
`
	if err := os.WriteFile(path, []byte(content), 0o755); err != nil {
		t.Fatalf("write stub: %v", err)
	}
	return path
}

// TestMCPHandler_HandleSetEnabled_InvokesRefresher verifies the
// end-to-end handleSetEnabled path: load config → mutate → save →
// manager.Reload → toolRefresher.SyncMCPTools. This guards against
// regressions where the refresher call is dropped or skipped. The
// server is toggled on with a real stub MCP subprocess so Reload
// succeeds and the refresher runs.
func TestMCPHandler_HandleSetEnabled_InvokesRefresher(t *testing.T) {
	if testing.Short() {
		t.Skip("spawns subprocess")
	}
	enabled := false
	stubPath := stubMCPServerScript(t)
	servers := []mcp.ServerConfig{
		{
			Name:    "test-srv",
			Enabled: &enabled,
			Type:    "stdio",
			Command: []string{stubPath},
		},
	}
	cfgPath := writeMCPConfigTmp(t, servers)

	manager := mcp.NewManager(nil)
	t.Cleanup(manager.StopAll)

	handler := NewMCPHandler(manager, cfgPath)
	refresher := &stubToolRefresher{}
	handler.SetToolRefresher(refresher)

	params := json.RawMessage(`{"name":"test-srv","enabled":true}`)
	result, err := handler.handleSetEnabled(context.Background(), params)
	if err != nil {
		t.Fatalf("handleSetEnabled: %v", err)
	}

	// Refresher must have been called exactly once after the reload.
	if got := atomic.LoadInt32(&refresher.calls); got != 1 {
		t.Errorf("expected 1 refresher call, got %d", got)
	}

	// Result must carry the updated config (enabled=true).
	entry, ok := result.(mcp.ServerStatusEntry)
	if !ok {
		t.Fatalf("expected mcp.ServerStatusEntry, got %T", result)
	}
	if !entry.Config.IsEnabled() {
		t.Error("expected config.IsEnabled()=true after toggle")
	}
	// RefreshWarning should be empty on a successful sync.
	if entry.RefreshWarning != "" {
		t.Errorf("expected empty RefreshWarning, got %q", entry.RefreshWarning)
	}

	// Disk must reflect the change.
	disk, err := config.LoadMCPConfig(cfgPath)
	if err != nil {
		t.Fatalf("reload from disk: %v", err)
	}
	if !disk.Servers[0].IsEnabled() {
		t.Error("expected persisted config to have enabled=true")
	}
}

// TestMCPHandler_HandleSetEnabled_RefresherFailureSoftWarns verifies
// that when SyncMCPTools returns an error, handleSetEnabled does NOT
// fail the whole RPC call (the toggle persisted and the manager reloaded)
// but surfaces a RefreshWarning on the returned entry.
func TestMCPHandler_HandleSetEnabled_RefresherFailureSoftWarns(t *testing.T) {
	if testing.Short() {
		t.Skip("spawns subprocess")
	}
	enabled := false
	stubPath := stubMCPServerScript(t)
	servers := []mcp.ServerConfig{
		{
			Name:    "fail-sync-srv",
			Enabled: &enabled,
			Type:    "stdio",
			Command: []string{stubPath},
		},
	}
	cfgPath := writeMCPConfigTmp(t, servers)

	manager := mcp.NewManager(nil)
	t.Cleanup(manager.StopAll)

	handler := NewMCPHandler(manager, cfgPath)
	refresher := &stubToolRefresher{err: errors.New("synthetic sync failure")}
	handler.SetToolRefresher(refresher)

	params := json.RawMessage(`{"name":"fail-sync-srv","enabled":true}`)
	result, err := handler.handleSetEnabled(context.Background(), params)
	if err != nil {
		t.Fatalf("expected no error from handleSetEnabled when sync fails softly; got %v", err)
	}
	entry, ok := result.(mcp.ServerStatusEntry)
	if !ok {
		t.Fatalf("expected mcp.ServerStatusEntry, got %T", result)
	}
	if entry.RefreshWarning == "" {
		t.Error("expected non-empty RefreshWarning when sync failed")
	}
}

// TestMCPHandler_HandleSetEnabled_NoRefresherIsFine verifies the
// handler works without a refresher wired (minimal deployments that
// don't run a tool registry). The call should succeed with no warning.
// The server is toggled on using a stub MCP script so Reload succeeds.
func TestMCPHandler_HandleSetEnabled_NoRefresherIsFine(t *testing.T) {
	if testing.Short() {
		t.Skip("spawns subprocess")
	}
	enabled := false
	stubPath := stubMCPServerScript(t)
	servers := []mcp.ServerConfig{
		{
			Name:    "no-refresh-srv",
			Enabled: &enabled,
			Type:    "stdio",
			Command: []string{stubPath},
		},
	}
	cfgPath := writeMCPConfigTmp(t, servers)

	manager := mcp.NewManager(nil)
	t.Cleanup(manager.StopAll)

	handler := NewMCPHandler(manager, cfgPath)
	// Deliberately do NOT call SetToolRefresher.

	params := json.RawMessage(`{"name":"no-refresh-srv","enabled":true}`)
	result, err := handler.handleSetEnabled(context.Background(), params)
	if err != nil {
		t.Fatalf("handleSetEnabled without refresher: %v", err)
	}
	entry, ok := result.(mcp.ServerStatusEntry)
	if !ok {
		t.Fatalf("expected mcp.ServerStatusEntry, got %T", result)
	}
	if entry.RefreshWarning != "" {
		t.Errorf("expected empty RefreshWarning with no refresher, got %q", entry.RefreshWarning)
	}
}

// TestMCPHandler_HandleSetEnabled_DisableSkipsSubprocess verifies that
// toggling a server from enabled→disabled persists + reloads without
// needing a real subprocess (Reload stops the server, doesn't start it).
// This also exercises the path with no refresher (nil toolRefresher is
// the default).
func TestMCPHandler_HandleSetEnabled_DisableSkipsSubprocess(t *testing.T) {
	enabled := true
	// Command points at a nonexistent binary; since the server is being
	// disabled, Reload never tries to spawn it, so the bogus command is
	// never invoked.
	servers := []mcp.ServerConfig{
		{
			Name:    "disable-srv",
			Enabled: &enabled,
			Type:    "stdio",
			Command: []string{"/nonexistent/binary"},
		},
	}
	cfgPath := writeMCPConfigTmp(t, servers)

	manager := mcp.NewManager(nil)
	t.Cleanup(manager.StopAll)
	// Mark the manager as having this server "running" so Reload has
	// something to stop. We don't actually start the subprocess.
	manager.SetConfigs(servers)

	handler := NewMCPHandler(manager, cfgPath)

	params := json.RawMessage(`{"name":"disable-srv","enabled":false}`)
	result, err := handler.handleSetEnabled(context.Background(), params)
	if err != nil {
		t.Fatalf("handleSetEnabled (disable): %v", err)
	}
	entry, ok := result.(mcp.ServerStatusEntry)
	if !ok {
		t.Fatalf("expected mcp.ServerStatusEntry, got %T", result)
	}
	if entry.Config.IsEnabled() {
		t.Error("expected config.IsEnabled()=false after disable")
	}
}

// TestMCPHandler_SetToolRefresher_NilGuard verifies the setter obeys
// the CLAUDE.md nil-guard requirement for setter methods on tool/service
// structs (typed-nil panic prevention).
func TestMCPHandler_SetToolRefresher_NilGuard(t *testing.T) {
	handler := NewMCPHandler(nil, "")
	// Passing nil must be a no-op, not a panic.
	handler.SetToolRefresher(nil)
	if handler.toolRefresher != nil {
		t.Error("expected nil toolRefresher after SetToolRefresher(nil)")
	}
}
