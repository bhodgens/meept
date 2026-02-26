package mcp

import (
	"context"
	"log/slog"
	"os"
	"testing"
)

func TestNewManager(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))

	m := NewManager(logger)
	if m == nil {
		t.Fatal("NewManager returned nil")
	}

	if m.ServerCount() != 0 {
		t.Errorf("expected 0 servers, got %d", m.ServerCount())
	}
}

func TestManagerStartServerValidation(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	m := NewManager(logger)
	ctx := context.Background()

	// Test empty name
	err := m.StartServer(ctx, ServerConfig{})
	if err == nil {
		t.Error("expected error for empty server name")
	}

	// Test missing command and URL
	err = m.StartServer(ctx, ServerConfig{Name: "test"})
	if err == nil {
		t.Error("expected error when both command and url are missing")
	}

	// Test unknown transport type
	err = m.StartServer(ctx, ServerConfig{
		Name: "test",
		Type: "invalid",
		URL:  "http://localhost:8080",
	})
	if err == nil {
		t.Error("expected error for unknown transport type")
	}
}

func TestManagerGetClientNotFound(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	m := NewManager(logger)

	client := m.GetClient("nonexistent")
	if client != nil {
		t.Error("expected nil for nonexistent client")
	}
}

func TestManagerStopServerNotFound(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	m := NewManager(logger)

	err := m.StopServer("nonexistent")
	if err == nil {
		t.Error("expected error for stopping nonexistent server")
	}
}

func TestManagerIsServerConnected(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	m := NewManager(logger)

	if m.IsServerConnected("nonexistent") {
		t.Error("expected false for nonexistent server")
	}
}

func TestManagerAllToolsEmpty(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	m := NewManager(logger)

	tools := m.AllTools()
	if len(tools) != 0 {
		t.Errorf("expected empty tools list, got %d", len(tools))
	}
}

func TestManagerAllLLMDefinitionsEmpty(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	m := NewManager(logger)

	defs := m.AllLLMDefinitions()
	if len(defs) != 0 {
		t.Errorf("expected empty definitions list, got %d", len(defs))
	}
}

func TestManagerCallToolInvalidFormat(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	m := NewManager(logger)
	ctx := context.Background()

	// Test invalid tool name format (missing dot)
	_, err := m.CallTool(ctx, "toolwithoutserver", nil)
	if err == nil {
		t.Error("expected error for invalid tool name format")
	}

	// Test server not found
	_, err = m.CallTool(ctx, "nonexistent.tool", nil)
	if err == nil {
		t.Error("expected error for nonexistent server")
	}
}

func TestManagerListServers(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	m := NewManager(logger)

	servers := m.ListServers()
	if len(servers) != 0 {
		t.Errorf("expected empty server list, got %d", len(servers))
	}
}

func TestManagerStopAll(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	m := NewManager(logger)

	// Should not panic with empty manager
	m.StopAll()

	if m.ServerCount() != 0 {
		t.Errorf("expected 0 servers after StopAll, got %d", m.ServerCount())
	}
}
