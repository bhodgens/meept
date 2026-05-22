# Unified HTTP Server Implementation

> Consolidates REST API, WebSocket, and MCP over HTTP+SSE into a single configurable HTTP server.

## Architecture

Single `*http.Server` at port 8081 with functional options pattern for enabling modular transports.

```
┌─────────────────────────────────────────────────────────┐
│              Unified HTTP Server (:8081)                │
│  ┌─────────────────┐  ┌─────────────────────────────┐  │
│  │ POST /mcp       │  │ GET /ws                     │  │
│  │ - JSON-RPC req  │  │ - WebSocket upgrade         │  │
│  │ - JSON response │  │ - Bus event broadcast       │  │
│  └─────────────────┘  └─────────────────────────────┘  │
│  ┌─────────────────┐  ┌─────────────────────────────┐  │
│  │ GET /mcp/sse    │  │ REST /api/v1/*              │  │
│  │ - SSE stream    │  │ - Existing menubar endpoints│  │
│  │ - Async events  │  │ - 40+ endpoints             │  │
│  └─────────────────┘  └─────────────────────────────┘  │
└─────────────────────────────────────────────────────────┘
```

## Configuration

```json5
// ~/.meept/meept.json5
"transport": {
  "http": {
    "enabled": true,
    "addr": ":8081",
    "rest": true,       // REST API at /api/v1/* (default: true)
    "websocket": true,  // WebSocket at /ws for Flutter UI
    "ws_path": "/ws",
    "mcp": true,        // MCP over HTTP+SSE for AI agents
    "mcp_path": "/mcp",
  },
}
```

## Implementation Summary

### 1. Configuration Schema (`internal/config/schema.go`)

Added modular transport fields to `HTTPTransportConfig`:

```go
type HTTPTransportConfig struct {
    Enabled      bool     `json:"enabled"       toml:"enabled"`
    Addr         string   `json:"addr"          toml:"addr"`
    RequireAuth  bool     `json:"require_auth"  toml:"require_auth"`
    APIKeys      []string `json:"api_keys"      toml:"api_keys"`
    UseTLS       bool     `json:"use_tls"       toml:"use_tls"`
    AutoTLSCert  bool     `json:"auto_tls_cert" toml:"auto_tls_cert"`

    // Modular endpoints
    REST      bool   `json:"rest"       toml:"rest"`       // REST API at /api/v1/*
    WebSocket bool   `json:"websocket"  toml:"websocket"`  // WebSocket at /ws
    WSPath    string `json:"ws_path"    toml:"ws_path"`    // Default: "/ws"
    MCP       bool   `json:"mcp"        toml:"mcp"`        // MCP over HTTP+SSE
    MCPPath   string `json:"mcp_path"   toml:"mcp_path"`   // Default: "/mcp"
}
```

Defaults in `DefaultConfig()`:
- `REST: true` - REST API enabled by default when HTTP is enabled
- `WebSocket: false` - Disabled by default (opt-in for Flutter UI)
- `MCP: false` - Disabled by default (opt-in for AI agent access)

### 2. HTTP Server (`internal/comm/http/server.go`)

**WebSocket Support:**
- `WebSocketHub` - Manages WebSocket connections with broadcast capability
- `WithWebSocket(msgBus *bus.MessageBus)` - Functional option enabling `/ws` endpoint
- Subscribes to message bus and broadcasts events to connected clients

**MCP over HTTP+SSE:**
- `MCPSession` - Session management for MCP clients
- `SSEEvent` - Server-Sent Event structure
- `WithMCP(services *ServiceRegistry, mcpPath string)` - Functional option enabling `/mcp` endpoints
- `POST /mcp` - JSON-RPC request handler (implements initialize, tools/list, tools/call)
- `GET /mcp/sse` - Server-Sent Events stream for async notifications

**Functional Options Pattern:**
```go
func NewServer(cfg, configSvc, daemonCtrl, metricsSvc, svcRegistry, logger, opts ...ServerOption) *Server
```

### 3. Daemon Wiring (`internal/daemon/daemon.go`)

Conditional endpoint enabling based on configuration:

```go
var httpOpts []http.ServerOption

if fullCfg.Transport.HTTP.WebSocket && msgBus != nil {
    httpOpts = append(httpOpts, http.WithWebSocket(msgBus))
}

if fullCfg.Transport.HTTP.MCP && svcRegistry != nil {
    httpOpts = append(httpOpts, http.WithMCP(svcRegistry, mcpPath))
}

httpSrv = http.NewServer(httpCfg, ..., httpOpts...)
```

## Endpoints

| Endpoint | Method | Purpose |
|----------|--------|---------|
| `/api/v1/*` | Various | Existing REST API (40+ endpoints) |
| `/ws` | GET | WebSocket for Flutter UI real-time updates |
| `/mcp` | POST | MCP JSON-RPC requests |
| `/mcp/sse` | GET | MCP Server-Sent Events stream |

## MCP JSON-RPC Methods

| Method | Status | Description |
|--------|--------|-------------|
| `initialize` | Complete | Returns MCP protocol version 2024-11-05 |
| `notifications/initialized` | Complete | No-op notification |
| `tools/list` | Complete | Returns tool definitions |
| `tools/call` | Stubbed | Calls MCP tools (sessions, send, events, status, history) |

## Testing

```bash
# Start daemon with HTTP transport enabled
./bin/meept-daemon -f

# Test WebSocket (requires wscat)
wscat -c ws://localhost:8081/ws

# Test MCP SSE stream
curl -N http://localhost:8081/mcp/sse

# Test MCP JSON-RPC initialize
curl -X POST http://localhost:8081/mcp \
  -H "Content-Type: application/json" \
  -d '{"jsonrpc":"2.0","id":1,"method":"initialize"}'

# Test MCP JSON-RPC tools/list
curl -X POST http://localhost:8081/mcp \
  -H "Content-Type: application/json" \
  -d '{"jsonrpc":"2.0","id":2,"method":"tools/list"}'
```

## Files Changed

- `internal/config/schema.go` - Added modular transport config fields
- `config/meept.json5` - Updated template with modular transport example
- `internal/comm/http/server.go` - Added WebSocketHub, MCP handlers, functional options
- `internal/daemon/daemon.go` - Wired functional options based on config

## Status

**IMPLEMENTATION COMPLETE**

- [x] Sprint 1: Configuration Schema
- [x] Sprint 2: WebSocket Handler
- [x] Sprint 3: MCP HTTP+SSE Handler
- [x] Sprint 4: Functional Options Pattern
- [x] Sprint 5: Daemon Wiring
- [x] Sprint 6: MCP Tool Implementations (stubbed)
- [ ] Sprint 7: Integration Testing
- [ ] Sprint 8: Documentation Updates

## Remaining Work

### Service Wiring (High Priority)
The MCP tool implementations are stubbed. To fully wire them:

1. **SessionStore Access**: Add `SessionStore session.Store` to `ServiceRegistry` or expose via `SessionService`
2. **Chat Service**: Add `SendMessage(sessionID, message string)` method to `ChatService`
3. **Bus Service**: Add `Poll(subscriptionID, since string)` method to `BusService`

### MCP SSE Streaming (Medium Priority)
- Wire bus subscription in `handleMCPSSE` to forward events to SSE stream
- Implement session cleanup on client disconnect

### Testing (High Priority)
- End-to-end WebSocket test with Flutter UI
- MCP JSON-RPC integration tests
- SSE stream connection test

### Documentation (Low Priority)
- Update CLAUDE.md transport section
- Add HTTP API reference for WebSocket and MCP endpoints
- Update architecture diagrams

## Known Limitations

1. **MCP tools return stub responses** - Session management and message sending are not wired to actual services
2. **SSE stream is empty** - No bus events are forwarded to SSE clients
3. **No session persistence** - MCP sessions are in-memory only
