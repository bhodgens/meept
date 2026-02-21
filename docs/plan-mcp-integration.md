# Plan: MCP Tools Integration

**Status:** Not Started
**Priority:** Medium
**Estimated Effort:** 2-3 days

---

## Current State

MCP (Model Context Protocol) client is **fully implemented** but **not connected** to the runtime:

| Component | File | Status |
|-----------|------|--------|
| Protocol | `internal/tools/mcp/protocol.go` | Implemented |
| Client | `internal/tools/mcp/client.go` | Implemented |
| Stdio Transport | `internal/tools/mcp/transport/stdio.go` | Implemented |
| HTTP Transport | `internal/tools/mcp/transport/http.go` | Implemented |

### What Exists

1. **MCP Client** (`client.go`, 308 lines)
   - Connect with initialize handshake
   - Tool discovery via `tools/list`
   - Tool execution via `tools/call`
   - LLM tool definition conversion
   - Notification support

2. **Protocol** (`protocol.go`)
   - JSON-RPC 2.0 implementation
   - Request/Response types
   - MCP-specific types (InitializeParams, ToolInfo, etc.)

3. **Transports**
   - Stdio: For local MCP servers (subprocess)
   - HTTP: For remote MCP servers

### What's Missing

1. **No daemon integration** - MCP clients not started
2. **No config loading** - `~/.meept/mcp_servers.json` not read
3. **No tool registration** - MCP tools not added to registry
4. **No tool execution** - MCP tool calls not routed to clients

---

## Implementation Plan

### Phase 1: MCP Manager

**File:** `internal/tools/mcp/manager.go` (new)

Create a manager to handle multiple MCP clients:

```go
package mcp

import (
    "context"
    "fmt"
    "log/slog"
    "sync"

    "github.com/caimlas/meept/internal/tools"
    "github.com/caimlas/meept/internal/tools/mcp/transport"
)

// ServerConfig describes an MCP server.
type ServerConfig struct {
    Name    string            `json:"name"`
    Command []string          `json:"command,omitempty"` // For stdio
    URL     string            `json:"url,omitempty"`      // For HTTP
    Type    string            `json:"type,omitempty"`     // "stdio" or "http"
    Env     map[string]string `json:"env,omitempty"`
}

// Manager manages multiple MCP clients.
type Manager struct {
    mu      sync.RWMutex
    clients map[string]*Client
    logger  *slog.Logger
}

// NewManager creates a new MCP manager.
func NewManager(logger *slog.Logger) *Manager {
    return &Manager{
        clients: make(map[string]*Client),
        logger:  logger,
    }
}

// StartServer starts an MCP server connection.
func (m *Manager) StartServer(ctx context.Context, cfg ServerConfig) error {
    m.mu.Lock()
    defer m.mu.Unlock()

    if _, exists := m.clients[cfg.Name]; exists {
        return fmt.Errorf("server %s already running", cfg.Name)
    }

    // Create transport
    var trans transport.Transport
    var err error

    if len(cfg.Command) > 0 {
        trans, err = transport.NewStdioTransport(cfg.Command, cfg.Env, m.logger)
    } else if cfg.URL != "" {
        trans, err = transport.NewHTTPTransport(cfg.URL, nil, m.logger)
    } else {
        return fmt.Errorf("server %s has no command or URL", cfg.Name)
    }

    if err != nil {
        return fmt.Errorf("failed to create transport: %w", err)
    }

    // Create and connect client
    client := NewClient(cfg.Name, trans, m.logger)
    if err := client.Connect(ctx); err != nil {
        return fmt.Errorf("failed to connect to %s: %w", cfg.Name, err)
    }

    m.clients[cfg.Name] = client
    m.logger.Info("MCP server connected",
        "name", cfg.Name,
        "tools", len(client.ListTools()))

    return nil
}

// StopServer stops an MCP server connection.
func (m *Manager) StopServer(name string) error {
    m.mu.Lock()
    defer m.mu.Unlock()

    client, exists := m.clients[name]
    if !exists {
        return fmt.Errorf("server %s not found", name)
    }

    delete(m.clients, name)
    return client.Close()
}

// StopAll stops all MCP servers.
func (m *Manager) StopAll() {
    m.mu.Lock()
    defer m.mu.Unlock()

    for name, client := range m.clients {
        if err := client.Close(); err != nil {
            m.logger.Error("failed to close MCP client", "name", name, "error", err)
        }
    }
    m.clients = make(map[string]*Client)
}

// GetClient returns a client by name.
func (m *Manager) GetClient(name string) *Client {
    m.mu.RLock()
    defer m.mu.RUnlock()
    return m.clients[name]
}

// AllTools returns all tools from all connected servers.
func (m *Manager) AllTools() []ToolInfo {
    m.mu.RLock()
    defer m.mu.RUnlock()

    var all []ToolInfo
    for _, client := range m.clients {
        all = append(all, client.ListTools()...)
    }
    return all
}

// CallTool calls a tool, routing to the correct server.
func (m *Manager) CallTool(ctx context.Context, fullName string, args map[string]any) (*tools.ToolResult, error) {
    // Parse "server.toolname" format
    serverName, toolName := parseMCPToolName(fullName)

    m.mu.RLock()
    client := m.clients[serverName]
    m.mu.RUnlock()

    if client == nil {
        return nil, fmt.Errorf("MCP server not found: %s", serverName)
    }

    return client.CallTool(ctx, toolName, args)
}
```

### Phase 2: Config Loading

**File:** `internal/config/mcp.go` (new)

```go
package config

import (
    "encoding/json"
    "os"
    "path/filepath"

    "github.com/caimlas/meept/internal/tools/mcp"
)

// MCPConfig holds MCP server configurations.
type MCPConfig struct {
    Servers []mcp.ServerConfig `json:"servers"`
}

// LoadMCPConfig loads MCP server configs from a file.
func LoadMCPConfig(path string) (*MCPConfig, error) {
    // Expand path
    if path == "" {
        home, _ := os.UserHomeDir()
        path = filepath.Join(home, ".meept", "mcp_servers.json")
    }

    data, err := os.ReadFile(path)
    if err != nil {
        if os.IsNotExist(err) {
            return &MCPConfig{}, nil // No config file is OK
        }
        return nil, err
    }

    var cfg MCPConfig
    if err := json.Unmarshal(data, &cfg); err != nil {
        return nil, err
    }

    return &cfg, nil
}
```

### Phase 3: Daemon Integration

**File:** `internal/daemon/components.go`

**Changes:**

1. Initialize MCP manager:
```go
func NewComponents(cfg *config.Config, ...) (*Components, error) {
    // ...

    // Initialize MCP
    var mcpManager *mcp.Manager
    if cfg.MCP.Enabled {
        mcpManager = mcp.NewManager(logger)

        mcpCfg, err := config.LoadMCPConfig(cfg.MCP.ConfigFile)
        if err != nil {
            logger.Warn("failed to load MCP config", "error", err)
        } else {
            for _, serverCfg := range mcpCfg.Servers {
                if err := mcpManager.StartServer(ctx, serverCfg); err != nil {
                    logger.Error("failed to start MCP server",
                        "name", serverCfg.Name, "error", err)
                }
            }
        }
    }

    // ...
}
```

2. Register MCP tools in tool registry:
```go
    // After MCP manager is started
    if mcpManager != nil {
        for _, client := range mcpManager.clients {
            for _, def := range client.ToLLMDefinitions() {
                // Create a wrapper tool that routes to MCP
                tool := NewMCPTool(def, mcpManager)
                toolRegistry.Register(tool)
            }
        }
    }
```

### Phase 4: MCP Tool Wrapper

**File:** `internal/tools/mcp/tool.go` (new)

```go
package mcp

import (
    "context"

    "github.com/caimlas/meept/internal/llm"
    "github.com/caimlas/meept/internal/tools"
)

// MCPTool wraps an MCP tool as a local tool.
type MCPTool struct {
    def     llm.ToolDefinition
    manager *Manager
}

// NewMCPTool creates a new MCP tool wrapper.
func NewMCPTool(def llm.ToolDefinition, manager *Manager) *MCPTool {
    return &MCPTool{
        def:     def,
        manager: manager,
    }
}

func (t *MCPTool) Name() string {
    return t.def.Function.Name
}

func (t *MCPTool) Description() string {
    return t.def.Function.Description
}

func (t *MCPTool) Parameters() llm.FunctionParameters {
    return t.def.Function.Parameters
}

func (t *MCPTool) Execute(ctx context.Context, args map[string]any) (*tools.ToolResult, error) {
    return t.manager.CallTool(ctx, t.Name(), args)
}

func (t *MCPTool) ToLLMDefinition() llm.ToolDefinition {
    return t.def
}
```

### Phase 5: Graceful Shutdown

**File:** `internal/daemon/components.go`

```go
func (c *Components) Stop() error {
    // ...

    // Stop MCP servers
    if c.mcpManager != nil {
        c.mcpManager.StopAll()
    }

    // ...
}
```

### Phase 6: Hot Reload

**File:** `internal/tools/mcp/manager.go`

Add methods for hot reload:

```go
// Reload reloads MCP servers from config.
func (m *Manager) Reload(ctx context.Context, cfg *MCPConfig) error {
    // Stop servers not in new config
    for name := range m.clients {
        found := false
        for _, s := range cfg.Servers {
            if s.Name == name {
                found = true
                break
            }
        }
        if !found {
            m.StopServer(name)
        }
    }

    // Start/update servers in new config
    for _, serverCfg := range cfg.Servers {
        if _, exists := m.clients[serverCfg.Name]; !exists {
            if err := m.StartServer(ctx, serverCfg); err != nil {
                m.logger.Error("failed to start server", "name", serverCfg.Name, "error", err)
            }
        }
    }

    return nil
}
```

---

## Example Configuration

**File:** `~/.meept/mcp_servers.json`

```json
{
  "servers": [
    {
      "name": "filesystem",
      "command": ["mcp-server-filesystem", "/path/to/allow"],
      "type": "stdio"
    },
    {
      "name": "fetch",
      "url": "http://localhost:8080",
      "type": "http"
    },
    {
      "name": "github",
      "command": ["mcp-server-github"],
      "env": {
        "GITHUB_TOKEN": "${GITHUB_TOKEN}"
      }
    }
  ]
}
```

---

## Testing Plan

### Unit Tests

1. **Protocol tests** (exist)
2. **Manager tests** (new)
3. **Tool wrapper tests** (new)

### Integration Tests

1. Test with `mcp-server-filesystem`
2. Test tool discovery
3. Test tool execution
4. Test hot reload

### Manual Testing

1. Install an MCP server: `npm install -g @anthropic/mcp-server-filesystem`
2. Configure in `mcp_servers.json`
3. Start daemon
4. Verify tools appear in `platform_tools` output
5. Execute MCP tool through chat

---

## Files to Modify

| File | Changes |
|------|---------|
| `internal/daemon/components.go` | Initialize MCP manager |
| `internal/config/schema.go` | Add MCP config |

## Files to Create

| File | Purpose |
|------|---------|
| `internal/tools/mcp/manager.go` | Multi-server management |
| `internal/tools/mcp/tool.go` | Tool wrapper |
| `internal/config/mcp.go` | Config loading |
| `tests/integration/mcp_test.go` | Integration tests |

---

## Success Criteria

1. MCP servers start on daemon startup
2. MCP tools appear in tool registry
3. MCP tools can be called through agent loop
4. MCP servers shutdown gracefully
5. Hot reload works
6. Tests pass
