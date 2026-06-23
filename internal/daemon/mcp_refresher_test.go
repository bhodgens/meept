package daemon

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/tools"
	"github.com/caimlas/meept/internal/tools/mcp"
)

// stubScript writes a bash script that pretends to be an MCP server,
// responding to JSON-RPC initialize and tools/list with canned payloads.
// toolName may be empty to advertise zero tools.
func stubScript(t *testing.T, toolName string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "stub-mcp.sh")
	var toolsLine string
	if toolName != "" {
		toolsLine = `        echo '{"jsonrpc":"2.0","id":2,"result":{"tools":[{"name":"` + toolName + `","description":"stub tool","inputSchema":{"type":"object","properties":{}}}]}}'`
	} else {
		toolsLine = `        echo '{"jsonrpc":"2.0","id":2,"result":{"tools":[]}}'`
	}
	content := `#!/usr/bin/env bash
while IFS= read -r line; do
    if echo "$line" | grep -q '"method":"initialize"'; then
        echo '{"jsonrpc":"2.0","id":1,"result":{"serverInfo":{"name":"stub","version":"1.0.0"},"capabilities":{"tools":{}}}}'
    elif echo "$line" | grep -q '"method":"tools/list"'; then
` + toolsLine + `
    fi
done
`
	if err := os.WriteFile(path, []byte(content), 0o755); err != nil {
		t.Fatalf("write stub: %v", err)
	}
	return path
}

// quietLogger discards output for quieter test runs.
func quietLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
}

// TestMCPToolRefresher_SyncRegistersAndUnregisters verifies that
// SyncMCPTools registers tools from active servers and unregisters
// tools from servers that go away. This exercises the daemon-side
// refresher that the mcp.set_enabled RPC handler invokes after a
// successful Reload.
func TestMCPToolRefresher_SyncRegistersAndUnregisters(t *testing.T) {
	if testing.Short() {
		t.Skip("integration-ish: spawns subprocess")
	}
	stubPath := stubScript(t, "ping")

	registry := tools.NewRegistry(quietLogger())
	manager := mcp.NewManager(quietLogger())
	t.Cleanup(manager.StopAll)

	refresher := newMCPToolRefresher(registry, manager, quietLogger())
	if refresher == nil {
		t.Fatal("expected non-nil refresher")
	}

	// Initially no tools registered.
	if err := refresher.SyncMCPTools(); err != nil {
		t.Fatalf("initial sync: %v", err)
	}
	if n := registry.Count(); n != 0 {
		t.Fatalf("expected 0 tools after empty sync, got %d", n)
	}

	// Start a stub MCP server that advertises a `ping` tool.
	enabled := true
	cfg := mcp.ServerConfig{
		Name:    "stub",
		Enabled: &enabled,
		Type:    "stdio",
		Command: []string{stubPath},
	}
	manager.SetConfigs([]mcp.ServerConfig{cfg})

	// 30s budget accommodates cold-cache scenarios and concurrent test load
	// where stub subprocess startup can exceed the previous 5s timeout.
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := manager.StartServer(ctx, cfg); err != nil {
		t.Fatalf("start stub: %v", err)
	}

	// Sync should register `stub.ping`.
	if err := refresher.SyncMCPTools(); err != nil {
		t.Fatalf("sync after start: %v", err)
	}
	if got := registry.Get("stub.ping"); got == nil {
		t.Fatalf("expected stub.ping registered; names=%v", registry.Names())
	}

	// Stop the server (simulates disable) and sync again — the tool
	// should be unregistered.
	if err := manager.StopServer("stub"); err != nil {
		t.Fatalf("stop stub: %v", err)
	}
	if err := refresher.SyncMCPTools(); err != nil {
		t.Fatalf("sync after stop: %v", err)
	}
	if got := registry.Get("stub.ping"); got != nil {
		t.Fatalf("expected stub.ping unregistered; names=%v", registry.Names())
	}
}

// TestMCPToolRefresher_NilGuards verifies the constructor returns nil
// when required dependencies are nil, so callers can pass the result
// unconditionally to rpc.MCPHandler.SetToolRefresher.
func TestMCPToolRefresher_NilGuards(t *testing.T) {
	if newMCPToolRefresher(nil, nil, nil) != nil {
		t.Error("expected nil refresher for nil inputs")
	}
	// Non-nil manager but nil registry should also return nil.
	mgr := mcp.NewManager(quietLogger())
	if newMCPToolRefresher(nil, mgr, nil) != nil {
		t.Error("expected nil refresher for nil registry")
	}
}

// TestNewMCPToolRefresher_InternalFieldInit guards against a future
// refactor that breaks the nil-vs-empty distinction on the
// registeredNames map (which would cause SyncMCPTools to panic).
func TestNewMCPToolRefresher_InternalFieldInit(t *testing.T) {
	registry := tools.NewRegistry(quietLogger())
	mgr := mcp.NewManager(quietLogger())
	r := newMCPToolRefresher(registry, mgr, quietLogger())
	if r == nil {
		t.Fatal("expected non-nil refresher")
	}
	if r.registeredNames == nil {
		t.Fatal("registeredNames map must be initialized")
	}
	// Verify field types compile as expected by checking zero state.
	if len(r.registeredNames) != 0 {
		t.Fatalf("expected empty registeredNames, got %d", len(r.registeredNames))
	}
	// Ensure unused-import style lint: strings is used in source but
	// referenced here to guard against accidental import removal.
	_ = strings.Contains
}
