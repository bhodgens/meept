package rpc

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/caimlas/meept/internal/config"
	"github.com/caimlas/meept/internal/tools/mcp"
)

// MCPHandler provides native RPC methods for MCP server management.
// It calls Manager directly so that CLI, TUI, and HTTP clients can list
// configured MCP servers and toggle their enabled state with persistence
// and reload.
type MCPHandler struct {
	manager    *mcp.Manager
	configPath string // path to mcp_servers.json5 for save/load
}

// NewMCPHandler creates a new handler. If manager is nil the registered
// methods return "mcp not enabled" errors. configPath is required for
// set_enabled; an empty configPath causes set_enabled to fail.
func NewMCPHandler(manager *mcp.Manager, configPath string) *MCPHandler {
	return &MCPHandler{
		manager:    manager,
		configPath: configPath,
	}
}

// RegisterMCPMethods registers MCP management RPC methods on the server.
func (h *MCPHandler) RegisterMCPMethods(server *Server) {
	server.RegisterHandler("mcp.list", h.handleList)
	server.RegisterHandler("mcp.set_enabled", h.handleSetEnabled)
}

// managerOrErr returns the manager or an error if it is nil. Centralizes
// the nil-guard so every handler applies the same check consistently.
func (h *MCPHandler) managerOrErr() (*mcp.Manager, error) {
	if h.manager == nil {
		return nil, fmt.Errorf("mcp not enabled")
	}
	return h.manager, nil
}

// handleList handles mcp.list RPC calls.
// Params: {} (ignored).
// Result: {"servers": []ServerStatusEntry}.
func (h *MCPHandler) handleList(_ context.Context, _ json.RawMessage) (any, error) {
	mgr, err := h.managerOrErr()
	if err != nil {
		return nil, err
	}
	return map[string]any{
		"servers": mgr.AllServerStatuses(),
	}, nil
}

// handleSetEnabled handles mcp.set_enabled RPC calls.
//
// Flow:
//  1. Load on-disk config fresh (avoids lost-update if user hand-edited).
//  2. Find entry by name; return error if not found.
//  3. Mutate entry.Enabled = &enabled.
//  4. SaveMCPConfig (atomic write).
//  5. manager.Reload (applies the new configuration to runtime).
//  6. Return the updated ServerStatusEntry.
//
// Params: {"name": string, "enabled": bool}.
// Result: ServerStatusEntry.
func (h *MCPHandler) handleSetEnabled(ctx context.Context, params json.RawMessage) (any, error) {
	mgr, err := h.managerOrErr()
	if err != nil {
		return nil, err
	}
	if h.configPath == "" {
		return nil, fmt.Errorf("mcp config path not configured")
	}

	var req struct {
		Name    string `json:"name"`
		Enabled bool   `json:"enabled"`
	}
	if err := json.Unmarshal(params, &req); err != nil {
		return nil, fmt.Errorf("invalid params: %w", err)
	}
	if req.Name == "" {
		return nil, fmt.Errorf("name is required")
	}

	// Step 1: read the on-disk config fresh to avoid lost-update.
	cfg, err := config.LoadMCPConfig(h.configPath)
	if err != nil {
		return nil, fmt.Errorf("failed to load mcp config: %w", err)
	}

	// Step 2: find the entry by name.
	idx := -1
	for i, srv := range cfg.Servers {
		if srv.Name == req.Name {
			idx = i
			break
		}
	}
	if idx < 0 {
		return nil, fmt.Errorf("mcp server %q not found", req.Name)
	}

	// Step 3: mutate the Enabled field. Always assign a fresh pointer so
	// the marshaled output includes the field even when the previous value
	// was nil (absent).
	enabled := req.Enabled
	cfg.Servers[idx].Enabled = &enabled

	// Step 4: persist atomically.
	if err := config.SaveMCPConfig(h.configPath, cfg); err != nil {
		return nil, fmt.Errorf("failed to save mcp config: %w", err)
	}

	// Step 5: reload the manager so runtime state matches on-disk config.
	// Reload also calls SetConfigs, which refreshes the configs map used by
	// AllServerStatuses.
	if err := mgr.Reload(ctx, cfg.Servers); err != nil {
		return nil, fmt.Errorf("mcp reload failed: %w", err)
	}

	// Step 6: return the updated entry.
	srvCfg, stats, ok := mgr.ServerStatus(req.Name)
	if !ok {
		return nil, fmt.Errorf("mcp server %q vanished after reload", req.Name)
	}
	return mcp.ServerStatusEntry{
		Config: srvCfg,
		Stats:  stats,
	}, nil
}
