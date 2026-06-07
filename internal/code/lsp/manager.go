package lsp

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"slices"
	"sync"
	"time"

	"github.com/caimlas/meept/internal/code/lsp/transport"
	"github.com/caimlas/meept/internal/config"
)

// Manager manages multiple LSP server connections.
type Manager struct {
	mu      sync.RWMutex
	servers map[string]*ServerInstance
	config  config.LSPConfig
	rootURI string
	logger  *slog.Logger
}

// ServerInstance holds a running LSP server.
type ServerInstance struct {
	Client    *Client
	DocMgr    *DocumentManager
	Languages []string
	Config    config.LSPServerConfig
	StartedAt time.Time
}

// ManagerOption configures the Manager.
type ManagerOption func(*Manager)

// WithManagerLogger sets the logger.
func WithManagerLogger(logger *slog.Logger) ManagerOption {
	return func(m *Manager) {
		m.logger = logger
	}
}

// WithRootURI sets the workspace root URI.
func WithRootURI(rootURI string) ManagerOption {
	return func(m *Manager) {
		m.rootURI = rootURI
	}
}

// NewManager creates a new LSP manager.
func NewManager(cfg config.LSPConfig, opts ...ManagerOption) *Manager {
	m := &Manager{
		servers: make(map[string]*ServerInstance),
		config:  cfg,
		logger:  slog.Default(),
	}

	for _, opt := range opts {
		opt(m)
	}

	return m
}

// GetServerForLanguage returns a server that handles the given language.
func (m *Manager) GetServerForLanguage(ctx context.Context, languageID string) (*ServerInstance, error) {
	m.mu.RLock()
	for _, srv := range m.servers {
		if slices.Contains(srv.Languages, languageID) {
			m.mu.RUnlock()
			return srv, nil
		}
	}
	m.mu.RUnlock()

	// Try to start server if auto-start is enabled
	if m.config.AutoStartServers {
		return m.StartServerForLanguage(ctx, languageID)
	}

	return nil, fmt.Errorf("no LSP server available for language: %s", languageID)
}

// StartServerForLanguage starts an LSP server for a language.
func (m *Manager) StartServerForLanguage(ctx context.Context, languageID string) (*ServerInstance, error) {
	// Find server config for this language
	var serverName string
	var serverCfg config.LSPServerConfig
	found := false

	for name, cfg := range m.config.Servers {
		if slices.Contains(cfg.Languages, languageID) {
			serverName = name
			serverCfg = cfg
			found = true
		}
		if found {
			break
		}
	}

	if !found {
		return nil, fmt.Errorf("no server configured for language: %s", languageID)
	}

	return m.StartServer(ctx, serverName, serverCfg)
}

// StartServer starts a specific LSP server.
func (m *Manager) StartServer(ctx context.Context, name string, cfg config.LSPServerConfig) (*ServerInstance, error) {
	m.mu.Lock()
	if srv, ok := m.servers[name]; ok {
		m.mu.Unlock()
		return srv, nil // Already running
	}
	m.mu.Unlock()

	m.logger.Info("Starting LSP server",
		"name", name,
		"command", cfg.Command,
		"languages", cfg.Languages,
	)

	// Create transport
	var t transport.Transport
	var err error

	switch cfg.Transport {
	case "tcp":
		timeout := time.Duration(m.config.ConnectionTimeoutSeconds) * time.Second
		t, err = transport.NewTCPTransport(cfg.Host, cfg.Port, timeout)
	case "stdio", "":
		t, err = transport.NewStdioTransport(cfg.Command, cfg.Args...)
	default:
		return nil, fmt.Errorf("unknown transport: %s", cfg.Transport)
	}

	if err != nil {
		return nil, fmt.Errorf("failed to create transport: %w", err)
	}

	// Create client
	client := NewClient(t)
	client.Start(ctx)

	// Initialize
	if err := client.Initialize(ctx, m.rootURI); err != nil {
		client.Close()
		return nil, fmt.Errorf("failed to initialize: %w", err)
	}

	srv := &ServerInstance{
		Client:    client,
		DocMgr:    NewDocumentManager(client),
		Languages: cfg.Languages,
		Config:    cfg,
		StartedAt: time.Now(),
	}

	m.mu.Lock()
	m.servers[name] = srv
	m.mu.Unlock()

	m.logger.Info("LSP server started",
		"name", name,
		"capabilities", client.Capabilities(),
	)

	return srv, nil
}

// StopServer stops a specific LSP server.
func (m *Manager) StopServer(ctx context.Context, name string) error {
	m.mu.Lock()
	srv, ok := m.servers[name]
	if !ok {
		m.mu.Unlock()
		return nil
	}
	delete(m.servers, name)
	m.mu.Unlock()

	m.logger.Info("Stopping LSP server", "name", name)

	// Close all documents
	if err := srv.DocMgr.CloseAll(ctx); err != nil {
		m.logger.Warn("Failed to close documents", "error", err)
	}

	// Shutdown server
	shutdownCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	if err := srv.Client.Shutdown(shutdownCtx); err != nil {
		m.logger.Warn("Failed to shutdown server gracefully", "error", err)
	}

	return srv.Client.Close()
}

// StopAll stops all running LSP servers.
func (m *Manager) StopAll(ctx context.Context) error {
	m.mu.RLock()
	names := make([]string, 0, len(m.servers))
	for name := range m.servers {
		names = append(names, name)
	}
	m.mu.RUnlock()

	var lastErr error
	for _, name := range names {
		if err := m.StopServer(ctx, name); err != nil {
			lastErr = err
		}
	}

	return lastErr
}

// Close implements io.Closer by stopping all servers.
func (m *Manager) Close() error {
	return m.StopAll(context.Background())
}

// RunningServers returns names of running servers.
func (m *Manager) RunningServers() []string {
	m.mu.RLock()
	defer m.mu.RUnlock()

	names := make([]string, 0, len(m.servers))
	for name := range m.servers {
		names = append(names, name)
	}
	return names
}

// GetServer returns a server by name.
func (m *Manager) GetServer(name string) (*ServerInstance, bool) {
	m.mu.RLock()
	srv, ok := m.servers[name]
	m.mu.RUnlock()
	return srv, ok
}

// WillRenameFiles calls the LSP server's workspace/willRenameFiles endpoint.
// This is used for barrel file updates, re-export changes, and aliased import handling.
// Returns nil if the server doesn't support this capability.
func (m *Manager) WillRenameFiles(ctx context.Context, oldURI, newURI string) (*WorkspaceEditWithOperations, error) {
	m.mu.RLock()
	// Use the first available server - in practice you'd want to route to the right server
	var client *Client
	for _, srv := range m.servers {
		client = srv.Client
		break
	}
	m.mu.RUnlock()

	if client == nil {
		return nil, fmt.Errorf("no LSP server available")
	}

	// Check if server supports willRenameFiles capability
	caps := NewCapabilities(client.Capabilities())
	if !caps.HasWillRenameFiles() {
		return nil, nil // Server doesn't support this capability
	}

	return client.WillRenameFiles(ctx, oldURI, newURI)
}

// SupportsLanguage checks if any server supports a language.
func (m *Manager) SupportsLanguage(languageID string) bool {
	// Check running servers
	m.mu.RLock()
	for _, srv := range m.servers {
		if slices.Contains(srv.Languages, languageID) {
			m.mu.RUnlock()
			return true
		}
	}
	m.mu.RUnlock()

	// Check configured servers
	for _, cfg := range m.config.Servers {
		if slices.Contains(cfg.Languages, languageID) {
			return true
		}
	}

	return false
}

// ServerStatus returns status information for all servers.
func (m *Manager) ServerStatus() []ServerStatus {
	m.mu.RLock()
	defer m.mu.RUnlock()

	statuses := make([]ServerStatus, 0, len(m.servers))
	for name, srv := range m.servers {
		statuses = append(statuses, ServerStatus{
			Name:         name,
			Languages:    srv.Languages,
			Running:      true,
			Uptime:       time.Since(srv.StartedAt),
			Capabilities: srv.Client.Capabilities(),
		})
	}
	return statuses
}

// ServerStatus holds status information for an LSP server.
type ServerStatus struct {
	Name         string
	Languages    []string
	Running      bool
	Uptime       time.Duration
	Capabilities ServerCapabilities
}

// Ensure Manager implements io.Closer
var _ io.Closer = (*Manager)(nil)
