package daemon

import (
	"log/slog"
	"strings"
	"sync"

	"github.com/caimlas/meept/internal/tools"
	"github.com/caimlas/meept/internal/tools/mcp"
)

// mcpToolRefresher re-syncs the daemon's ToolRegistry with the MCP
// manager's currently-available tools. It implements rpc.MCPToolRefresher
// and is invoked by the mcp.set_enabled RPC handler after a successful
// Reload, so toggling a server on/off is reflected in the agent-visible
// tool list without a daemon restart.
//
// Concurrency: the refresher is called from the RPC goroutine while
// other goroutines may register/unregister tools. The underlying
// tools.Registry is mutex-protected; we additionally hold mu to protect
// the registeredNames set.
type mcpToolRefresher struct {
	mu             sync.Mutex
	registry       *tools.Registry
	manager        *mcp.Manager
	logger         *slog.Logger
	registeredNames map[string]struct{} // tool names we previously registered
}

// newMCPToolRefresher constructs a refresher. registry and manager must
// be non-nil; the constructor returns nil otherwise so callers can pass
// the result unconditionally to rpc.MCPHandler.SetToolRefresher (which
// nil-guards).
func newMCPToolRefresher(registry *tools.Registry, manager *mcp.Manager, logger *slog.Logger) *mcpToolRefresher {
	if registry == nil || manager == nil {
		return nil
	}
	if logger == nil {
		logger = slog.Default()
	}
	return &mcpToolRefresher{
		registry:        registry,
		manager:         manager,
		logger:          logger,
		registeredNames: make(map[string]struct{}),
	}
}

// SyncMCPTools reconciles the registry with the manager's current tool
// set. It:
//  1. Snapshots the MCP manager's current tool definitions.
//  2. Registers every current tool (Registry.Register replaces dups, so
//     already-present tools are idempotently refreshed).
//  3. Unregisters any tool we previously registered that is no longer
//     present (e.g., its server was disabled).
//
// Tracking prior names locally (rather than scanning the registry for
// `<server>.*` prefixes) avoids accidentally unregistering non-MCP tools
// that happen to use dotted names.
func (r *mcpToolRefresher) SyncMCPTools() error {
	defs := r.manager.AllLLMDefinitions()
	current := make(map[string]struct{}, len(defs))

	for _, def := range defs {
		name := def.Function.Name
		if name == "" {
			continue
		}
		// Extract server name from the prefixed tool name (server.tool).
		serverName := ""
		if idx := strings.Index(name, "."); idx > 0 {
			serverName = name[:idx]
		}
		tool := mcp.NewMCPTool(def, r.manager, serverName)
		r.registry.Register(tool)
		current[name] = struct{}{}
	}

	// Snapshot under lock, operate outside, re-acquire to update.
	r.mu.Lock()
	prev := make([]string, 0, len(r.registeredNames))
	for n := range r.registeredNames {
		prev = append(prev, n)
	}
	r.mu.Unlock()

	var unregisteredCount int
	for _, name := range prev {
		if _, stillPresent := current[name]; stillPresent {
			continue
		}
		if err := r.registry.Unregister(name); err != nil {
			// Not fatal: tool may have been unregistered by another path.
			r.logger.Debug("mcp tool unregister skipped", "name", name, "error", err)
			continue
		}
		unregisteredCount++
	}

	// Update tracking set.
	r.mu.Lock()
	r.registeredNames = current
	r.mu.Unlock()

	r.logger.Info("synced mcp tools to registry",
		"registered", len(defs),
		"unregistered", unregisteredCount,
	)
	return nil
}
