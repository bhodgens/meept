package mcp

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/caimlas/meept/internal/llm"
	"github.com/caimlas/meept/internal/tools"
	"github.com/caimlas/meept/internal/tools/mcp/transport"
)

// ServerConfig defines the configuration for an MCP server.
type ServerConfig struct {
	Name        string            `json:"name"`
	Enabled     *bool             `json:"enabled,omitempty"`     // nil/absent = true (backward compat)
	Command     []string          `json:"command,omitempty"`     // For stdio transport
	URL         string            `json:"url,omitempty"`         // For HTTP transport
	Type        string            `json:"type,omitempty"`        // "stdio" or "http"
	Env         map[string]string `json:"env,omitempty"`
	Headers     map[string]string `json:"headers,omitempty"`     // For HTTP transport
	Description string            `json:"description,omitempty"` // Optional, for UI display
	Category    string            `json:"category,omitempty"`    // Optional, for UI grouping
}

// IsEnabled reports whether the server should be started. A nil Enabled
// pointer (absent field) is treated as true to preserve backward
// compatibility with existing configs that omit the field.
func (sc ServerConfig) IsEnabled() bool {
	return sc.Enabled == nil || *sc.Enabled
}

// ServerState represents the runtime status of an MCP server.
type ServerState string

const (
	StateDisabled ServerState = "disabled" // enabled: false
	StateInactive ServerState = "inactive" // enabled, not yet started
	StateActive   ServerState = "active"   // connected and ready
	StateError    ServerState = "error"    // failed to start, or enabled but not connected
)

// ServerStats holds per-server runtime stats. In-memory only; resets on
// daemon restart.
type ServerStats struct {
	State         ServerState `json:"state"`
	Requests      int64       `json:"requests"`                 // CallTool invocations (success + failure)
	Errors        int64       `json:"errors"`                   // failed CallTool invocations
	LastError     string      `json:"last_error"`               // trimmed; "" if none
	LastErrorAt   *time.Time  `json:"last_error_at,omitempty"`  // time of last error
	LastRequestAt *time.Time  `json:"last_request_at,omitempty"` // time of last request
}

// ServerStatusEntry pairs a config with its runtime stats for UI consumption.
type ServerStatusEntry struct {
	Config ServerConfig `json:"config"`
	Stats  ServerStats  `json:"stats"`
}

// Manager manages multiple MCP client connections.
type Manager struct {
	mu      sync.RWMutex
	clients map[string]*Client
	logger  *slog.Logger
	stats   map[string]*ServerStats // in-memory runtime stats, guarded by mu
	configs map[string]ServerConfig // snapshot of all configured servers (incl. disabled)
}

// NewManager creates a new MCP manager.
func NewManager(logger *slog.Logger) *Manager {
	if logger == nil {
		logger = slog.Default()
	}
	return &Manager{
		clients: make(map[string]*Client),
		stats:   make(map[string]*ServerStats),
		configs: make(map[string]ServerConfig),
		logger:  logger.With("component", "mcp-manager"),
	}
}

// SetConfigs records the full set of configured servers (including disabled
// ones) so AllServerStatuses can report on them. Does not affect the
// currently-running clients; callers use Reload for that.
func (m *Manager) SetConfigs(configs []ServerConfig) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.configs = make(map[string]ServerConfig, len(configs))
	for _, cfg := range configs {
		m.configs[cfg.Name] = cfg
	}
}

// ServerStatus returns the full status of a single server by name. The
// returned bool is false if the server is unknown (not in configs and not
// running).
func (m *Manager) ServerStatus(name string) (ServerConfig, ServerStats, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	cfg, cfgOk := m.configs[name]
	if !cfgOk {
		// Backward compat: fall back to running clients if SetConfigs was
		// never called or this is an ad-hoc server.
		if client, exists := m.clients[name]; exists {
			cfg = ServerConfig{Name: name}
			cfgOk = true
			// stats derived from presence.
			if client.IsConnected() {
				return cfg, makeStatsFromState(StateActive, m.stats[name]), true
			}
			return cfg, makeStatsFromState(StateInactive, m.stats[name]), true
		}
		return ServerConfig{}, ServerStats{}, false
	}

	// Merge logic per spec: disabled → force disabled; else use stats state
	// or "inactive" when no stats entry exists.
	if !cfg.IsEnabled() {
		return cfg, makeStatsFromState(StateDisabled, m.stats[name]), true
	}
	if st, ok := m.stats[name]; ok && st != nil {
		return cfg, *st, true
	}
	return cfg, ServerStats{State: StateInactive}, true
}

// makeStatsFromState copies the non-nil src (or starts from an empty stats
// value) and forces its State to the given value. Returns a value (not a
// pointer) so callers can return without holding the lock.
func makeStatsFromState(state ServerState, src *ServerStats) ServerStats {
	if src == nil {
		return ServerStats{State: state}
	}
	out := *src
	out.State = state
	return out
}

// AllServerStatuses returns configs + stats for every configured server,
// including disabled ones. Sorted by Category then Name for stable UI
// display. If SetConfigs was never called (backward compat), falls back to
// listing just the running clients.
func (m *Manager) AllServerStatuses() []ServerStatusEntry {
	m.mu.RLock()
	defer m.mu.RUnlock()

	entries := make([]ServerStatusEntry, 0, max(len(m.configs), len(m.clients)))

	if len(m.configs) > 0 {
		for name, cfg := range m.configs {
			var stats ServerStats
			if !cfg.IsEnabled() {
				stats = makeStatsFromState(StateDisabled, m.stats[name])
			} else if st, ok := m.stats[name]; ok && st != nil {
				stats = *st
			} else {
				stats = ServerStats{State: StateInactive}
			}
			entries = append(entries, ServerStatusEntry{Config: cfg, Stats: stats})
		}
	} else {
		// Backward compat: no SetConfigs called; list running clients.
		for name, client := range m.clients {
			cfg := ServerConfig{Name: name}
			var stats ServerStats
			if client.IsConnected() {
				stats = makeStatsFromState(StateActive, m.stats[name])
			} else {
				stats = makeStatsFromState(StateError, m.stats[name])
			}
			entries = append(entries, ServerStatusEntry{Config: cfg, Stats: stats})
		}
	}

	sort.SliceStable(entries, func(i, j int) bool {
		ci, cj := entries[i].Config.Category, entries[j].Config.Category
		if ci != cj {
			return ci < cj
		}
		return entries[i].Config.Name < entries[j].Config.Name
	})

	return entries
}

// StartHealthMonitor launches a background goroutine that periodically
// updates state for enabled-but-disconnected servers to "error". Cancels
// when ctx is done. Safe to call multiple times (each call spawns one
// goroutine; callers should typically call once at daemon startup).
func (m *Manager) StartHealthMonitor(ctx context.Context) {
	go func() {
		ticker := time.NewTicker(60 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				m.checkHealth()
			}
		}
	}()
}

// checkHealth walks the running clients and, for any client that is no
// longer connected, marks its stats entry as StateError (creating the
// entry if missing). Pure in-memory mutation under m.mu.
func (m *Manager) checkHealth() {
	m.mu.Lock()
	defer m.mu.Unlock()
	for name, client := range m.clients {
		if client.IsConnected() {
			continue
		}
		st, ok := m.stats[name]
		if !ok || st == nil {
			now := time.Now()
			st = &ServerStats{
				State:       StateError,
				LastError:   "client not connected",
				LastErrorAt: &now,
			}
			m.stats[name] = st
		} else {
			st.State = StateError
		}
	}
}

// StartServer starts an MCP server connection. On success, records an
// `active` stats entry. On failure, records an `error` stats entry with
// LastError/LastErrorAt set before returning.
func (m *Manager) StartServer(ctx context.Context, cfg ServerConfig) error {
	if cfg.Name == "" {
		return fmt.Errorf("server name is required")
	}

	// Validate enabled state. Disabled servers should not be started; treat
	// as a programmer error rather than silently no-op'ing so callers get
	// clear feedback.
	if !cfg.IsEnabled() {
		return fmt.Errorf("server %q is disabled", cfg.Name)
	}

	// recordError captures a failure under m.mu as a StateError stats entry.
	recordError := func(errMsg string) {
		now := time.Now()
		trimmed := strings.TrimSpace(errMsg)
		m.mu.Lock()
		m.stats[cfg.Name] = &ServerStats{
			State:       StateError,
			LastError:   trimmed,
			LastErrorAt: &now,
		}
		m.mu.Unlock()
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
			err := fmt.Errorf("server %q: must specify command (stdio) or url (http)", cfg.Name)
			recordError(err.Error())
			return err
		}
	}

	transportCfg := transport.DefaultConfig()
	if cfg.Env != nil {
		transportCfg.Environment = cfg.Env
	}

	switch transportType {
	case "stdio":
		if len(cfg.Command) == 0 {
			err := fmt.Errorf("server %q: stdio transport requires command", cfg.Name)
			recordError(err.Error())
			return err
		}
		cmd := cfg.Command[0]
		args := cfg.Command[1:]
		trans = transport.NewStdioTransport(cmd, args, transportCfg)

	case "http":
		if cfg.URL == "" {
			err := fmt.Errorf("server %q: http transport requires url", cfg.Name)
			recordError(err.Error())
			return err
		}
		trans = transport.NewHTTPTransport(cfg.URL, cfg.Headers, transportCfg)

	default:
		err := fmt.Errorf("server %q: unknown transport type %q", cfg.Name, transportType)
		recordError(err.Error())
		return err
	}

	// Create client
	client := NewClient(cfg.Name, trans, m.logger)

	// Connect — no lock held during subprocess I/O
	if err := client.Connect(ctx); err != nil {
		wrapped := fmt.Errorf("server %q: failed to connect: %w", cfg.Name, err)
		recordError(wrapped.Error())
		return wrapped
	}

	// Re-acquire lock to insert client. Re-check for a concurrent StartServer
	// that may have inserted an entry while the lock was released.
	m.mu.Lock()
	if _, exists := m.clients[cfg.Name]; exists {
		m.mu.Unlock()
		// Another goroutine started the same server; close the duplicate
		if err := client.Close(); err != nil {
			m.logger.Warn("duplicate MCP client close failed", "name", cfg.Name, "error", err)
		}
		// Stats entry already exists from the first StartServer call; do not
		// overwrite it here.
		return fmt.Errorf("server %q already running", cfg.Name)
	}
	m.clients[cfg.Name] = client
	m.stats[cfg.Name] = &ServerStats{State: StateActive}
	toolCount := len(client.ListTools())
	m.mu.Unlock()

	m.logger.Info("started MCP server",
		"name", cfg.Name,
		"type", transportType,
		"tools", toolCount,
	)

	return nil
}

// StopServer stops a specific MCP server connection. The stats entry is
// retained with state=`inactive` so UI continues to show prior counts.
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

	// Re-acquire lock to remove the entry and demote stats state.
	m.mu.Lock()
	delete(m.clients, name)
	if st, ok := m.stats[name]; ok && st != nil {
		st.State = StateInactive
	}
	m.mu.Unlock()

	m.logger.Info("stopped MCP server", "name", name)

	return nil
}

// StopAll stops all MCP server connections. Stats entries are preserved
// with state=`inactive` so any post-shutdown UI query can still display
// prior counts.
func (m *Manager) StopAll() {
	// Snapshot all clients under the lock, then release before I/O
	m.mu.Lock()
	snapshot := make(map[string]*Client, len(m.clients))
	maps.Copy(snapshot, m.clients)
	m.clients = make(map[string]*Client)
	// Mark existing stats entries inactive. Counts preserved.
	for _, st := range m.stats {
		if st != nil {
			st.State = StateInactive
		}
	}
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
// The toolName must be in the format "servername.toolname". Increments the
// per-server request counter under m.mu before dispatch and, on a failed
// call, increments the error counter and records LastError/LastErrorAt.
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

	// Increment request counter under lock before dispatching. The stats
	// entry should exist (server is active, per checks above) but guard
	// against nil just in case.
	now := time.Now()
	m.mu.Lock()
	if st, ok := m.stats[serverName]; ok && st != nil {
		st.Requests++
		st.LastRequestAt = &now
	} else {
		m.stats[serverName] = &ServerStats{
			State:         StateActive,
			Requests:      1,
			LastRequestAt: &now,
		}
	}
	m.mu.Unlock()

	m.logger.Debug("calling MCP tool",
		"server", serverName,
		"tool", toolName,
		"args", args,
	)

	result, err := client.CallTool(ctx, toolName, args)
	if err != nil {
		errTime := time.Now()
		trimmed := strings.TrimSpace(err.Error())
		m.mu.Lock()
		if st, ok := m.stats[serverName]; ok && st != nil {
			st.Errors++
			st.LastError = trimmed
			st.LastErrorAt = &errTime
		}
		m.mu.Unlock()
	}
	return result, err
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

// Reload reloads MCP servers from a new configuration. It stops servers
// that are no longer configured or have been disabled, starts new enabled
// servers, and restarts enabled servers whose configuration has changed.
// Disabled entries are skipped with a debug log; stats entries for disabled
// servers are preserved (their state is forced to `disabled` by the
// AllServerStatuses merge logic).
func (m *Manager) Reload(ctx context.Context, configs []ServerConfig) error {
	m.logger.Info("reloading MCP configuration", "server_count", len(configs))

	// Record the full set of configured servers (including disabled) so
	// AllServerStatuses can report on them.
	m.SetConfigs(configs)

	// Build a map of new configs by name
	newConfigs := make(map[string]ServerConfig)
	for _, cfg := range configs {
		newConfigs[cfg.Name] = cfg
	}

	// Phase 1: Determine what servers to stop and close them (under lock)
	// We use a helper function to ensure the lock is always released properly
	configsToStart := m.reloadPhase1(newConfigs)

	// Phase 2: Start servers (outside lock to avoid deadlock with StartServer).
	// Skip disabled configs with a debug log — do not treat as error.
	// Use errors.Join so that every failure is surfaced, not just the last.
	var errs []error
	for _, cfg := range configsToStart {
		if !cfg.IsEnabled() {
			m.logger.Debug("skipping disabled MCP server during reload", "name", cfg.Name)
			continue
		}
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

// reloadPhase1 handles stopping old servers and preparing configs to start.
// Returns configs that need to be started (enabled ones only are filtered
// in phase 2 by the caller; this function returns all new configs so the
// caller's disabled-skip logic has the full picture for state updates).
//
// Per CLAUDE.md mutex-scope rule, client.Close() calls (which perform
// subprocess I/O) are done OUTSIDE the lock. We snapshot the clients to
// close under the lock, release it, perform the I/O, then re-acquire the
// lock to delete the entries.
//
// Stats handling during phase 1:
//   - removed (not in newConfigs): client deleted, stats entry marked inactive.
//   - disabled (in newConfigs but IsEnabled()==false): client closed if
//     running, stats entry marked disabled.
//   - restart (in newConfigs and enabled): client closed so phase-2 can
//     re-create it; stats entry preserved (will be overwritten on the
//     new StartServer call).
func (m *Manager) reloadPhase1(newConfigs map[string]ServerConfig) []ServerConfig {
	// Phase 1a: under lock, snapshot what needs closing
	m.mu.Lock()

	// Determine what servers to stop
	var serversToStop []string
	for name := range m.clients {
		cfg, exists := newConfigs[name]
		if !exists {
			serversToStop = append(serversToStop, name)
			continue
		}
		// Also stop running clients whose config is now disabled so the
		// disabled-state merge logic is accurate after reload.
		if !cfg.IsEnabled() {
			serversToStop = append(serversToStop, name)
		}
	}

	// Determine what servers to start/refresh (all new entries; phase 2
	// filters disabled ones with a debug log).
	serversToStart := make([]string, 0, len(newConfigs))
	for name := range newConfigs {
		serversToStart = append(serversToStart, name)
	}

	// Collect clients to close (removed + restart-needed + now-disabled)
	type clientToClose struct {
		name   string
		client *Client
		reason string
	}

	var toClose []clientToClose
	for _, name := range serversToStop {
		if c, ok := m.clients[name]; ok {
			toClose = append(toClose, clientToClose{name, c, "removed"})
		}
	}
	for _, name := range serversToStart {
		if existingClient, exists := m.clients[name]; exists {
			// Skip ones already queued for stop above; otherwise queue for restart.
			alreadyQueued := false
			for _, s := range serversToStop {
				if s == name {
					alreadyQueued = true
					break
				}
			}
			if alreadyQueued {
				continue
			}
			toClose = append(toClose, clientToClose{name, existingClient, "restart"})
		}
	}

	m.mu.Unlock()

	// Phase 1b: outside lock, close clients with I/O
	for _, ctc := range toClose {
		m.logger.Info("closing MCP client for reload", "name", ctc.name, "reason", ctc.reason)
		if err := ctc.client.Close(); err != nil {
			m.logger.Error("error closing MCP client during reload", "name", ctc.name, "error", err)
		}
	}

	// Phase 1c: under lock, delete closed clients from the map and update
	// stats states for removed/disabled entries.
	m.mu.Lock()
	for _, ctc := range toClose {
		delete(m.clients, ctc.name)
		if st, ok := m.stats[ctc.name]; ok && st != nil {
			cfg, cfgExists := newConfigs[ctc.name]
			switch {
			case !cfgExists:
				// Removed from config entirely.
				st.State = StateInactive
			case !cfg.IsEnabled():
				// Disabled (state will be forced to `disabled` by merge logic
				// regardless, but set it here too for consistency).
				st.State = StateDisabled
			default:
				// Restart case: keep counts; state will be overwritten by
				// the upcoming StartServer call. Leave existing state so a
				// failed restart still shows `inactive`/`error` rather than
				// a misleading transient.
			}
		}
	}
	m.mu.Unlock()

	// Store configs to start
	configsToStart := make([]ServerConfig, 0, len(serversToStart))
	for _, name := range serversToStart {
		configsToStart = append(configsToStart, newConfigs[name])
	}

	return configsToStart
}
