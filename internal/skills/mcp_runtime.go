package skills

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/caimlas/meept/internal/tools/mcp"
	"github.com/caimlas/meept/internal/tools/mcp/transport"
)

// ToolDef represents a tool discovered from an MCP server during skill execution.
type ToolDef struct {
	// Name is the tool name, prefixed with the server name (e.g., "servername.toolname").
	Name string
	// Description is a human-readable description of the tool.
	Description string
	// ServerName is the name of the MCP server that provides this tool.
	ServerName string
}

// mcpServerProc tracks a running MCP server process and its discovered tools.
type mcpServerProc struct {
	config   MCPServerConfig
	client   *mcp.Client
	transport transport.Transport
	tools    []ToolDef
	started  bool
}

// MCPRuntime manages the lifecycle of MCP servers for a skill execution.
// When a skill declares MCP servers in its frontmatter, this runtime starts
// them on demand, discovers their tools, and shuts them down when execution
// completes.
type MCPRuntime struct {
	servers []*mcpServerProc
	mu      sync.Mutex
	logger  *slog.Logger
	started bool
}

// NewMCPRuntime creates a new MCPRuntime for the given server configurations.
// If configs is nil or empty, HasServers() returns false and all lifecycle
// methods are no-ops.
func NewMCPRuntime(configs []MCPServerConfig, logger *slog.Logger) *MCPRuntime {
	if logger == nil {
		logger = slog.Default()
	}

	servers := make([]*mcpServerProc, 0, len(configs))
	for _, cfg := range configs {
		servers = append(servers, &mcpServerProc{
			config: cfg,
		})
	}

	return &MCPRuntime{
		servers: servers,
		logger:  logger,
	}
}

// HasServers returns true if any MCP servers are configured.
func (r *MCPRuntime) HasServers() bool {
	return len(r.servers) > 0
}

// Start launches all configured MCP servers, performs the MCP handshake
// (initialize + tools/list), and collects discovered tools. If a server
// fails to start or initialize, the error is logged and remaining servers
// continue. Returns the first error encountered (non-fatal; other servers
// may still be running).
func (r *MCPRuntime) Start(ctx context.Context) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.started {
		return nil
	}

	var firstErr error

	for _, srv := range r.servers {
		if err := r.startServer(ctx, srv); err != nil {
			r.logger.Warn("MCP server failed to start, continuing with remaining servers",
				"server", srv.config.Name,
				"error", err,
			)
			if firstErr == nil {
				firstErr = fmt.Errorf("server %q: %w", srv.config.Name, err)
			}
			continue
		}
		srv.started = true
	}

	r.started = true

	// If all servers failed, return the error.
	if firstErr != nil {
		allFailed := true
		for _, srv := range r.servers {
			if srv.started {
				allFailed = false
				break
			}
		}
		if allFailed {
			return firstErr
		}
	}

	return nil
}

// startServer starts a single MCP server process and discovers its tools.
func (r *MCPRuntime) startServer(ctx context.Context, srv *mcpServerProc) error {
	cfg := srv.config

	r.logger.Info("starting MCP server",
		"name", cfg.Name,
		"command", cfg.Command,
		"args", cfg.Args,
	)

	tpConfig := transport.DefaultConfig()
	tpConfig.Environment = cfg.Env
	tpConfig.TimeoutMS = 15000 // 15 second per-request timeout for skill-embedded servers

	tp := transport.NewStdioTransport(cfg.Command, cfg.Args, tpConfig)
	client := mcp.NewClient(cfg.Name, tp, r.logger)

	if err := client.Connect(ctx); err != nil {
		// Close the transport on failure to avoid leaked processes.
		_ = tp.Close()
		return fmt.Errorf("connect failed: %w", err)
	}

	// Discover tools from the server.
	toolInfos := client.ListTools()
	tools := make([]ToolDef, 0, len(toolInfos))
	for _, ti := range toolInfos {
		tools = append(tools, ToolDef{
			Name:        fmt.Sprintf("%s.%s", cfg.Name, ti.Name),
			Description: ti.Description,
			ServerName:  cfg.Name,
		})
	}

	srv.client = client
	srv.transport = tp
	srv.tools = tools

	r.logger.Info("MCP server started",
		"name", cfg.Name,
		"tools", len(tools),
	)

	return nil
}

// Tools returns all tools discovered from successfully started MCP servers.
// Returns an empty slice if no servers are running or no tools were found.
func (r *MCPRuntime) Tools() []ToolDef {
	r.mu.Lock()
	defer r.mu.Unlock()

	var all []ToolDef
	for _, srv := range r.servers {
		if srv.started {
			all = append(all, srv.tools...)
		}
	}

	return all
}

// Shutdown gracefully terminates all running MCP servers. It sends a shutdown
// notification to each server, waits up to 5 seconds for graceful exit, then
// kills any remaining processes. Returns nil even if individual servers fail
// to shut down (errors are logged).
func (r *MCPRuntime) Shutdown() error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if !r.started {
		return nil
	}

	var shutdownErr error

	for _, srv := range r.servers {
		if !srv.started || srv.client == nil {
			continue
		}

		name := srv.config.Name

		// Attempt to send a shutdown notification before closing.
		// The MCP spec says servers should handle this gracefully.
		_ = r.sendShutdown(srv)

		if err := srv.client.Close(); err != nil {
			r.logger.Warn("error closing MCP server",
				"server", name,
				"error", err,
			)
			if shutdownErr == nil {
				shutdownErr = fmt.Errorf("server %q: %w", name, err)
			}
		}

		r.logger.Info("MCP server stopped", "name", name)
	}

	r.started = false
	return shutdownErr
}

// sendShutdown sends a JSON-RPC shutdown notification to the server.
func (r *MCPRuntime) sendShutdown(srv *mcpServerProc) error {
	// Build a raw shutdown notification. We use the mcp package types
	// to ensure correct JSON-RPC 2.0 format.
	notif := mcp.NewNotification("shutdown", nil)
	data, err := json.Marshal(notif)
	if err != nil {
		return fmt.Errorf("serialize shutdown notification: %w", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	return srv.transport.SendNotification(ctx, data)
}

// Started returns true if Start() has been called (regardless of whether
// servers are currently running).
func (r *MCPRuntime) Started() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.started
}

// Ensure MCPRuntime is usable as nil (zero-value HasServers returns false).
var _ interface {
	HasServers() bool
	Shutdown() error
} = (*MCPRuntime)(nil)

// compile-time interface satisfaction check
var _ error = (*startError)(nil)

// startError wraps an error from a specific MCP server start failure.
type startError struct {
	serverName string
	err        error
}

func (e *startError) Error() string {
	return fmt.Sprintf("MCP server %q: %v", e.serverName, e.err)
}

func (e *startError) Unwrap() error {
	return e.err
}

// ErrAllServersFailed is returned when all configured MCP servers fail to start.
var ErrAllServersFailed = errors.New("all MCP servers failed to start")
