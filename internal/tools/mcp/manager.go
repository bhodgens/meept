package mcp

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"

	"github.com/caimlas/meept/internal/llm"
	"github.com/caimlas/meept/internal/tools"
	"github.com/caimlas/meept/internal/tools/mcp/transport"
)

// ServerConfig defines the configuration for an MCP server.
type ServerConfig struct {
	Name    string            `json:"name"`
	Command []string          `json:"command,omitempty"` // For stdio transport
	URL     string            `json:"url,omitempty"`     // For HTTP transport
	Type    string            `json:"type,omitempty"`    // "stdio" or "http"
	Env     map[string]string `json:"env,omitempty"`
	Headers map[string]string `json:"headers,omitempty"` // For HTTP transport
}

// Manager manages multiple MCP client connections.
type Manager struct {
	mu      sync.RWMutex
	clients map[string]*Client
	logger  *slog.Logger
}

// NewManager creates a new MCP manager.
func NewManager(logger *slog.Logger) *Manager {
	if logger == nil {
		logger = slog.Default()
	}
	return &Manager{
		clients: make(map[string]*Client),
		logger:  logger.With("component", "mcp-manager"),
	}
}

// StartServer starts an MCP server connection.
func (m *Manager) StartServer(ctx context.Context, cfg ServerConfig) error {
	if cfg.Name == "" {
		return fmt.Errorf("server name is required")
	}

	// Check if already running (under lock, released before I/O)
	m.mu.Lock()
	if _, exists := m.clients[cfg.Name]; exists {
		m.mu.Unlock()
		return fmt.Errorf("server %q already running", cfg.Name)
	}
	m.mu.Unlock()

	// Create transport based on config (no lock held)
	var trans transport.Transport
	transportType := cfg.Type
	if transportType == "" {
		// Infer type from config
		switch {
		case len(cfg.Command) > 0:
			transportType = "stdio"
		case cfg.URL != "":
			transportType = "http"
		default:
			return fmt.Errorf("server %q: must specify command (stdio) or url (http)", cfg.Name)
		}
	}

	transportCfg := transport.DefaultConfig()
	if cfg.Env != nil {
		transportCfg.Environment = cfg.Env
	}

	switch transportType {
	case "stdio":
		if len(cfg.Command) == 0 {
			return fmt.Errorf("server %q: stdio transport requires command", cfg.Name)
		}
		cmd := cfg.Command[0]
		args := cfg.Command[1:]
		trans = transport.NewStdioTransport(cmd, args, transportCfg)

	case "http":
		if cfg.URL == "" {
			return fmt.Errorf("server %q: http transport requires url", cfg.Name)
		}
		trans = transport.NewHTTPTransport(cfg.URL, cfg.Headers, transportCfg)

	default:
		return fmt.Errorf("server %q: unknown transport type %q", cfg.Name, transportType)
	}

	// Create client
	client := NewClient(cfg.Name, trans, m.logger)

	// Connect — no lock held during subprocess I/O
	if err := client.Connect(ctx); err != nil {
		return fmt.Errorf("server %q: failed to connect: %w", cfg.Name, err)
	}

	// Re-acquire lock to insert client. Re-check for a concurrent StartServer
	// that may have inserted an entry while the lock was released.
	m.mu.Lock()
	if _, exists := m.clients[cfg.Name]; exists {
		m.mu.Unlock()
		// Another goroutine started the same server; close the duplicate
		_ = client.Close()
		return fmt.Errorf("server %q already running", cfg.Name)
	}
	m.clients[cfg.Name] = client
	toolCount := len(client.ListTools())
	m.mu.Unlock()

	m.logger.Info("started MCP server",
		"name", cfg.Name,
		"type", transportType,
		"tools", toolCount,
	)

	return nil
}

// StopServer stops a specific MCP server connection.
func (m *Manager) StopServer(name string) error {
	// Snapshot the client under the lock, then release before I/O
	m.mu.Lock()
	client, exists := m.clients[name]
	if !exists {
		m.mu.Unlock()
		return fmt.Errorf("server %q not found", name)
	}
	m.mu.Unlock()

	// Close outside the lock to avoid blocking other callers during subprocess I/O
	if err := client.Close(); err != nil {
		m.logger.Error("error closing MCP client", "name", name, "error", err)
	}

	// Re-acquire lock to remove the entry
	m.mu.Lock()
	delete(m.clients, name)
	m.mu.Unlock()

	m.logger.Info("stopped MCP server", "name", name)

	return nil
}

// StopAll stops all MCP server connections.
func (m *Manager) StopAll() {
	// Snapshot all clients under the lock, then release before I/O
	m.mu.Lock()
	snapshot := make(map[string]*Client, len(m.clients))
	for k, v := range m.clients {
		snapshot[k] = v
	}
	m.clients = make(map[string]*Client)
	m.mu.Unlock()

	// Close each client outside the lock
	for name, client := range snapshot {
		if err := client.Close(); err != nil {
			m.logger.Error("error closing MCP client", "name", name, "error", err)
		}
		m.logger.Debug("stopped MCP server", "name", name)
	}

	m.logger.Info("stopped all MCP servers")
}

// GetClient returns the client for a specific server.
func (m *Manager) GetClient(name string) *Client {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.clients[name]
}

// ListServers returns the names of all connected servers.
func (m *Manager) ListServers() []string {
	m.mu.RLock()
	defer m.mu.RUnlock()

	names := make([]string, 0, len(m.clients))
	for name := range m.clients {
		names = append(names, name)
	}
	return names
}

// AllTools returns tool information from all connected servers.
// The returned ToolInfo structs have names prefixed with "servername.".
func (m *Manager) AllTools() []ToolInfo {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var allTools []ToolInfo
	for _, client := range m.clients {
		toolList := client.ListTools()
		// Prefix tool names with server name
		for _, t := range toolList {
			prefixedTool := ToolInfo{
				Name:        fmt.Sprintf("%s.%s", client.Name(), t.Name),
				Description: t.Description,
				InputSchema: t.InputSchema,
			}
			allTools = append(allTools, prefixedTool)
		}
	}
	return allTools
}

// AllLLMDefinitions returns LLM tool definitions from all connected servers.
func (m *Manager) AllLLMDefinitions() []llm.ToolDefinition {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var allDefs []llm.ToolDefinition
	for _, client := range m.clients {
		defs := client.ToLLMDefinitions()
		allDefs = append(allDefs, defs...)
	}
	return allDefs
}

// CallTool routes a tool call to the appropriate MCP server.
// The toolName must be in the format "servername.toolname".
func (m *Manager) CallTool(ctx context.Context, fullName string, args map[string]any) (*tools.ToolResult, error) {
	// Parse server name from full tool name
	parts := strings.SplitN(fullName, ".", 2)
	if len(parts) != 2 {
		return nil, fmt.Errorf("invalid tool name %q: must be in format 'server.tool'", fullName)
	}

	serverName := parts[0]
	toolName := parts[1]

	m.mu.RLock()
	client, exists := m.clients[serverName]
	m.mu.RUnlock()

	if !exists {
		return nil, fmt.Errorf("MCP server %q not found", serverName)
	}

	if !client.IsConnected() {
		return nil, fmt.Errorf("MCP server %q is not connected", serverName)
	}

	m.logger.Debug("calling MCP tool",
		"server", serverName,
		"tool", toolName,
		"args", args,
	)

	return client.CallTool(ctx, toolName, args)
}

// ServerCount returns the number of connected servers.
func (m *Manager) ServerCount() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.clients)
}

// IsServerConnected checks if a specific server is connected.
func (m *Manager) IsServerConnected(name string) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()

	client, exists := m.clients[name]
	if !exists {
		return false
	}
	return client.IsConnected()
}

// Reload reloads MCP servers from a new configuration.
// It stops servers that are no longer configured, starts new servers,
// and restarts servers whose configuration has changed.
func (m *Manager) Reload(ctx context.Context, configs []ServerConfig) error {
	m.logger.Info("reloading MCP configuration", "server_count", len(configs))

	// Build a map of new configs by name
	newConfigs := make(map[string]ServerConfig)
	for _, cfg := range configs {
		newConfigs[cfg.Name] = cfg
	}

	// Phase 1: Determine what servers to stop and close them (under lock)
	// We use a helper function to ensure the lock is always released properly
	configsToStart := m.reloadPhase1(newConfigs)

	// Phase 2: Start servers (outside lock to avoid deadlock with StartServer).
	// Use errors.Join so that every failure is surfaced, not just the last.
	var errs []error
	for _, cfg := range configsToStart {
		if err := m.StartServer(ctx, cfg); err != nil {
			m.logger.Error("failed to start MCP server during reload",
				"name", cfg.Name,
				"error", err,
			)
			errs = append(errs, fmt.Errorf("server %q: %w", cfg.Name, err))
		}
	}

	m.mu.RLock()
	activeCount := len(m.clients)
	m.mu.RUnlock()

	m.logger.Info("MCP configuration reloaded", "active_servers", activeCount)

	return errors.Join(errs...)
}

// reloadPhase1 handles the locked portion of reload - stopping old servers
// and preparing configs to start. Returns configs that need to be started.
func (m *Manager) reloadPhase1(newConfigs map[string]ServerConfig) []ServerConfig {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Determine what servers to stop
	var serversToStop []string
	for name := range m.clients {
		if _, exists := newConfigs[name]; !exists {
			serversToStop = append(serversToStop, name)
		}
	}

	// Determine what servers to start/refresh
	serversToStart := make([]string, 0, len(newConfigs))
	for name := range newConfigs {
		serversToStart = append(serversToStart, name)
	}

	// Stop servers that are no longer in the config
	for _, name := range serversToStop {
		client := m.clients[name]
		m.logger.Info("stopping removed MCP server", "name", name)
		if err := client.Close(); err != nil {
			m.logger.Error("error closing MCP client during reload", "name", name, "error", err)
		}
		delete(m.clients, name)
	}

	// Mark servers for restart (close them but don't start yet)
	for _, name := range serversToStart {
		if existingClient, exists := m.clients[name]; exists {
			m.logger.Info("restarting MCP server", "name", name)
			if err := existingClient.Close(); err != nil {
				m.logger.Error("error closing MCP client during restart", "name", name, "error", err)
			}
			delete(m.clients, name)
		}
	}

	// Store configs to start
	configsToStart := make([]ServerConfig, 0, len(serversToStart))
	for _, name := range serversToStart {
		configsToStart = append(configsToStart, newConfigs[name])
	}

	return configsToStart
}
