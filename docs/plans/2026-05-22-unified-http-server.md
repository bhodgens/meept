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
- Subscribes to message bus with wildcard (`*`) and broadcasts events to connected clients

**MCP over HTTP+SSE:**
- `MCPSession` - Session management for MCP clients
- `SSEEvent` - Server-Sent Event structure
- `WithMCP(services *ServiceRegistry, mcpPath string)` - Functional option enabling `/mcp` endpoints
- `POST /mcp` - JSON-RPC request handler (implements initialize, tools/list, tools/call)
- `GET /mcp/sse` - Server-Sent Events stream with bus event forwarding

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

### 4. Service Wiring (`internal/services/service.go`)

Added `SessionStore session.Store` field to `ServiceRegistry` for direct MCP access to session operations.

## Endpoints

| Endpoint | Method | Purpose | Config Flag |
|----------|--------|---------|-------------|
| `/api/v1/*` | Various | Existing REST API (40+ endpoints) | `rest` |
| `/ws` | GET | WebSocket for Flutter UI real-time updates | `websocket` |
| `/mcp` | POST | MCP JSON-RPC requests | `mcp` |
| `/mcp/sse` | GET | MCP Server-Sent Events stream | `mcp` |

## MCP JSON-RPC Methods

| Method | Status | Description |
|--------|--------|-------------|
| `initialize` | Complete | Returns MCP protocol version 2024-11-05 |
| `notifications/initialized` | Complete | No-op notification |
| `tools/list` | Complete | Returns tool definitions from `mcp.ToolDefinitions()` |
| `tools/call` | **WIRED** | Calls MCP tools via SessionStore |

**MCP Tools (tools/call):**
| Tool | Status | Implementation |
|------|--------|----------------|
| `meept_sessions` | WIRED | Uses `SessionStore.List()`, `Create()`, `Get()` |
| `meept_send` | Stub | Returns placeholder response |
| `meept_events` | Stub | Returns empty events |
| `meept_status` | WIRED | Uses `DaemonService.Status()` |
| `meept_session_history` | WIRED | Uses `SessionStore.GetMessages()` |

## Testing

```bash
# Build and start daemon with HTTP transport enabled
go build -o bin/meept-daemon ./cmd/meept-daemon
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

# Test MCP JSON-RPC sessions list
curl -X POST http://localhost:8081/mcp \
  -H "Content-Type: application/json" \
  -d '{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"meept_sessions","arguments":{"action":"list"}}}'

# Run unit tests
go test ./internal/comm/http/... -v
```

## Files Changed

- `internal/config/schema.go` - Added modular transport config fields
- `internal/services/service.go` - Added `SessionStore` field to `ServiceRegistry`
- `internal/comm/http/server.go` - Added WebSocketHub, MCP handlers, functional options, SSE streaming
- `internal/comm/http/unified_http_test.go` - Integration tests for WebSocket and MCP options
- `internal/daemon/daemon.go` - Wired functional options based on config
- `CLAUDE.md` - Updated transport configuration section
- `docs/reference/http-api.md` - Added WebSocket and MCP endpoint documentation
- `docs/plans/2026-05-22-unified-http-server.md` - This plan document

## Status

**IMPLEMENTATION COMPLETE**

- [x] Sprint 1: Configuration Schema
- [x] Sprint 2: WebSocket Handler
- [x] Sprint 3: MCP HTTP+SSE Handler
- [x] Sprint 4: Functional Options Pattern
- [x] Sprint 5: Daemon Wiring
- [x] Sprint 6: MCP Tool Implementations (wired SessionStore)
- [x] Sprint 7: Integration Testing
- [x] Sprint 8: Documentation Updates

## Remaining Work (Optional Enhancements)

### Chat Service Wiring (Low Priority)
- Add `SendMessage(sessionID, message string)` method to `ChatService`
- Wire `meept_send` tool to actually publish messages to chat bus

### Bus Polling (Low Priority)
- Add `Poll(subscriptionID, since string)` method to `BusService`
- Wire `meept_events` tool to return actual bus events

### Testing Enhancements (Medium Priority)
- End-to-end WebSocket test with actual Flutter UI
- MCP JSON-RPC full integration test with session operations
- SSE stream test verifying bus event forwarding

## Known Limitations

1. **meept_send returns stub** - Messages are not actually published to chat bus
2. **meept_events returns empty** - Bus event polling not implemented
3. **No session persistence for MCP** - Sessions are in-memory only (same as rest of system)
