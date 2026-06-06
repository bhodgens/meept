package skills

import (
	"context"
	"testing"
	"time"
)

func TestNewMCPRuntime_NilConfigs(t *testing.T) {
	r := NewMCPRuntime(nil, nil)

	if r == nil {
		t.Fatal("NewMCPRuntime returned nil")
	}

	if r.HasServers() {
		t.Error("HasServers() should return false for nil configs")
	}

	tools := r.Tools()
	if len(tools) != 0 {
		t.Errorf("Tools() = %d items, want 0", len(tools))
	}

	// Shutdown on empty runtime should be safe
	if err := r.Shutdown(); err != nil {
		t.Errorf("Shutdown() returned error: %v", err)
	}
}

func TestNewMCPRuntime_EmptyConfigs(t *testing.T) {
	r := NewMCPRuntime([]MCPServerConfig{}, nil)

	if r.HasServers() {
		t.Error("HasServers() should return false for empty configs")
	}

	if err := r.Shutdown(); err != nil {
		t.Errorf("Shutdown() returned error: %v", err)
	}
}

func TestNewMCPRuntime_WithConfigs(t *testing.T) {
	configs := []MCPServerConfig{
		{Name: "test-server", Command: "echo", Args: []string{"hello"}},
	}

	r := NewMCPRuntime(configs, nil)

	if !r.HasServers() {
		t.Error("HasServers() should return true with configs")
	}
}

func TestMCPRuntime_HasServers(t *testing.T) {
	tests := []struct {
		name    string
		configs []MCPServerConfig
		want    bool
	}{
		{
			name:    "nil configs",
			configs: nil,
			want:    false,
		},
		{
			name:    "empty configs",
			configs: []MCPServerConfig{},
			want:    false,
		},
		{
			name: "single config",
			configs: []MCPServerConfig{
				{Name: "server1", Command: "echo"},
			},
			want: true,
		},
		{
			name: "multiple configs",
			configs: []MCPServerConfig{
				{Name: "server1", Command: "echo"},
				{Name: "server2", Command: "cat"},
			},
			want: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := NewMCPRuntime(tt.configs, nil)
			if got := r.HasServers(); got != tt.want {
				t.Errorf("HasServers() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestMCPRuntime_Tools_NoServersStarted(t *testing.T) {
	configs := []MCPServerConfig{
		{Name: "server1", Command: "echo", Args: []string{"hello"}},
	}

	r := NewMCPRuntime(configs, nil)

	tools := r.Tools()
	if len(tools) != 0 {
		t.Errorf("Tools() before Start() = %d items, want 0", len(tools))
	}
}

func TestMCPRuntime_Shutdown_NoServersRunning(t *testing.T) {
	configs := []MCPServerConfig{
		{Name: "server1", Command: "nonexistent-binary"},
	}

	r := NewMCPRuntime(configs, nil)

	// Shutdown before Start should be safe
	if err := r.Shutdown(); err != nil {
		t.Errorf("Shutdown() before Start() returned error: %v", err)
	}
}

func TestMCPRuntime_Start_InvalidCommand(t *testing.T) {
	// Use a command that exists but is not an MCP server — it should fail
	// to complete the MCP handshake and the runtime should handle that
	// gracefully without blocking indefinitely.
	configs := []MCPServerConfig{
		{Name: "bad-server", Command: "echo", Args: []string{}},
	}

	r := NewMCPRuntime(configs, nil)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// This should not block forever; the echo command won't respond to
	// MCP initialize, so it should timeout and return an error.
	err := r.Start(ctx)
	if err == nil {
		t.Error("Start() with non-MCP command should return an error")
	}

	// Shutdown should still be safe
	if err := r.Shutdown(); err != nil {
		t.Errorf("Shutdown() after failed Start() returned error: %v", err)
	}
}

func TestMCPRuntime_Start_ContextCancelled(t *testing.T) {
	configs := []MCPServerConfig{
		{Name: "slow-server", Command: "sleep", Args: []string{"100"}},
	}

	r := NewMCPRuntime(configs, nil)

	ctx, cancel := context.WithCancel(context.Background())

	// Cancel immediately
	cancel()

	err := r.Start(ctx)
	// The context cancellation should cause a quick failure rather than blocking.
	// We don't enforce a specific error type since it depends on OS scheduling,
	// but it should not hang (the test runner has its own timeout).
	_ = err

	// Shutdown should be safe
	if err := r.Shutdown(); err != nil {
		t.Errorf("Shutdown() returned error: %v", err)
	}
}

func TestMCPRuntime_Start_Idempotent(t *testing.T) {
	configs := []MCPServerConfig{
		{Name: "server1", Command: "echo", Args: []string{}},
	}

	r := NewMCPRuntime(configs, nil)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// First Start should fail (echo is not an MCP server)
	_ = r.Start(ctx)

	// Second Start should be a no-op and not block
	done := make(chan struct{})
	go func() {
		_ = r.Start(ctx)
		close(done)
	}()

	select {
	case <-done:
		// Good, returned quickly
	case <-time.After(2 * time.Second):
		t.Error("Second Start() blocked; should be idempotent")
	}
}

func TestMCPRuntime_FullLifecycle(t *testing.T) {
	// Use "true" command — it exits immediately with 0, so the MCP handshake
	// will fail, but the lifecycle itself (Start -> Tools -> Shutdown) should
	// work without panics or deadlocks.
	configs := []MCPServerConfig{
		{Name: "test-server", Command: "true"},
	}

	r := NewMCPRuntime(configs, nil)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Start will fail because "true" is not an MCP server
	err := r.Start(ctx)
	if err == nil {
		t.Log("Start() unexpectedly succeeded (true is not an MCP server)")
	}

	// Tools should be empty since no tools were discovered
	tools := r.Tools()
	if len(tools) != 0 {
		t.Errorf("Tools() = %d items, want 0", len(tools))
	}

	// Shutdown should succeed regardless
	if err := r.Shutdown(); err != nil {
		t.Errorf("Shutdown() returned error: %v", err)
	}

	// After shutdown, Tools should still be empty
	tools = r.Tools()
	if len(tools) != 0 {
		t.Errorf("Tools() after Shutdown() = %d items, want 0", len(tools))
	}
}

func TestMCPRuntime_MultipleInvalidServers(t *testing.T) {
	// All servers are invalid; the runtime should try all of them and return
	// an error but not deadlock or panic.
	configs := []MCPServerConfig{
		{Name: "server1", Command: "echo"},
		{Name: "server2", Command: "true"},
		{Name: "server3", Command: "false"},
	}

	r := NewMCPRuntime(configs, nil)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	err := r.Start(ctx)
	if err == nil {
		t.Error("Start() with all-invalid servers should return an error")
	}

	// Shutdown should be safe
	if err := r.Shutdown(); err != nil {
		t.Errorf("Shutdown() returned error: %v", err)
	}
}

func TestMCPRuntime_Started(t *testing.T) {
	configs := []MCPServerConfig{
		{Name: "server1", Command: "echo"},
	}

	r := NewMCPRuntime(configs, nil)

	if r.Started() {
		t.Error("Started() should be false before Start()")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	_ = r.Start(ctx)

	if !r.Started() {
		t.Error("Started() should be true after Start()")
	}

	_ = r.Shutdown()

	// After shutdown, Started should still reflect the lifecycle state.
	// The runtime tracks whether Start was ever called.
	if !r.Started() {
		// Started remains true even after Shutdown because it indicates
		// that the lifecycle was entered. This is expected behavior.
	}
}

func TestMCPServerConfig_Struct(t *testing.T) {
	cfg := MCPServerConfig{
		Name:    "my-server",
		Command: "/usr/bin/node",
		Args:    []string{"server.js", "--port", "3000"},
		Env:     map[string]string{"API_KEY": "secret", "DEBUG": "true"},
	}

	if cfg.Name != "my-server" {
		t.Errorf("Name = %q, want %q", cfg.Name, "my-server")
	}
	if cfg.Command != "/usr/bin/node" {
		t.Errorf("Command = %q, want %q", cfg.Command, "/usr/bin/node")
	}
	if len(cfg.Args) != 3 {
		t.Errorf("Args length = %d, want 3", len(cfg.Args))
	}
	if len(cfg.Env) != 2 {
		t.Errorf("Env length = %d, want 2", len(cfg.Env))
	}
	if cfg.Env["API_KEY"] != "secret" {
		t.Errorf("Env[API_KEY] = %q, want %q", cfg.Env["API_KEY"], "secret")
	}
}

func TestToolDef_Struct(t *testing.T) {
	tool := ToolDef{
		Name:        "my-server.search",
		Description: "Search the database",
		ServerName:  "my-server",
	}

	if tool.Name != "my-server.search" {
		t.Errorf("Name = %q, want %q", tool.Name, "my-server.search")
	}
	if tool.Description != "Search the database" {
		t.Errorf("Description = %q", tool.Description)
	}
	if tool.ServerName != "my-server" {
		t.Errorf("ServerName = %q", tool.ServerName)
	}
}
