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
- `WithWebSocket(msgBus *bus.MessageBus, wsPath string)` - Functional option enabling WebSocket endpoint at configurable path
- Subscribes to message bus with wildcard (`*`) and broadcasts events to connected clients

**MCP over HTTP+SSE:**
- `MCPSession` - Session management for MCP clients
- `SSEEvent` - Server-Sent Event structure
- `WithMCP(services *ServiceRegistry, mcpPath string)` - Functional option enabling `/mcp` endpoints
- `POST /mcp` - JSON-RPC request handler (implements initialize, tools/list, tools/call)
- `GET /mcp/sse` - Server-Sent Events stream with bus event forwarding

**Listener Management:**
- `Addr() string` - Returns actual bound address (useful with `:0` for kernel-assigned ports)
- Uses `net.Listen` + `Serve`/`ServeTLS` for port discovery instead of `ListenAndServe`

**Functional Options Pattern:**
```go
func NewServer(cfg, configSvc, daemonCtrl, metricsSvc, svcRegistry, logger, opts ...ServerOption) *Server
```

### 3. Daemon Wiring (`internal/daemon/daemon.go`)

Conditional endpoint enabling based on configuration:

```go
var httpOpts []http.ServerOption

if fullCfg.Transport.HTTP.WebSocket && msgBus != nil {
    wsPath := fullCfg.Transport.HTTP.WSPath
    if wsPath == "" {
        wsPath = "/ws"
    }
    httpOpts = append(httpOpts, http.WithWebSocket(msgBus, wsPath))
}

if fullCfg.Transport.HTTP.MCP && svcRegistry != nil {
    mcpPath := fullCfg.Transport.HTTP.MCPPath
    if mcpPath == "" {
        mcpPath = "/mcp"
    }
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
| Tool | HTTP Status | Stdio Status | Implementation |
|------|-------------|--------------|----------------|
| `meept_sessions` | WIRED | WIRED | HTTP: `SessionStore` direct; Stdio: RPC `ListSessions/CreateSession/AttachSession` |
| `meept_send` | WIRED | WIRED | HTTP: `BusService.Publish("chat.request")`; Stdio: RPC `chat` method |
| `meept_events` | WIRED | Wired (broken) | HTTP: MCPSession event buffer with `since` filtering; Stdio: RPC `bus.poll` (broken — bug 0051) |
| `meept_status` | WIRED | WIRED | HTTP: `DaemonService.Status()`; Stdio: RPC `Status()` |
| `meept_session_history` | WIRED | WIRED | HTTP: `SessionStore.GetMessages()`; Stdio: RPC `GetSessionMessages()` |

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

# Run unit tests (32 tests covering REST, WebSocket, MCP, SSE)
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

**IMPLEMENTATION COMPLETE** (with fixes applied 2026-05-22)

- [x] Sprint 1: Configuration Schema
- [x] Sprint 2: WebSocket Handler (with configurable path)
- [x] Sprint 3: MCP HTTP+SSE Handler
- [x] Sprint 4: Functional Options Pattern
- [x] Sprint 5: Daemon Wiring (with WSPath passthrough)
- [x] Sprint 6: MCP Tool Implementations (wired SessionStore)
- [x] Sprint 7: Integration Testing (functional route registration tests added)
- [x] Sprint 8: Documentation Updates

### Fixes Applied (2026-05-22, Round 1)

1. **CRITICAL: MCP route registration guard** — Route setup checked `s.mcpClient` (never assigned) instead of `s.mcpServices`. All MCP endpoints returned 404. Fixed to check `s.mcpServices != nil`.
2. **WebSocket path not configurable** — `WithWebSocket` accepted no path argument; daemon wiring hardcoded `/ws`. Fixed `WithWebSocket` to accept `wsPath` parameter; daemon now reads `WSPath` from config.
3. **Dead code removed** — Removed unused `mcpClient transport.Client` field and `internal/transport` import from `server.go`.
4. **Listener address discovery** — Changed `Start()` from `ListenAndServe`/`ListenAndServeTLS` to `net.Listen` + `Serve`/`ServeTLS`, enabling `Addr()` to return the actual bound address for `:0` testing.
5. **WebSocket Hijacker passthrough** — `loggingResponseWriter` now implements `http.Hijacker` and `http.Flusher` by delegating to the underlying `ResponseWriter`, fixing WebSocket upgrade and SSE streaming through the logging middleware.
6. **meept_send wired to bus** — HTTP MCP `meept_send` now publishes `chat.request` messages via `BusService.Publish()`, matching the stdio path behavior.
7. **meept_events event buffering** — HTTP MCP `meept_events` now reads from the MCPSession's event buffer (populated by the SSE bus subscription), with `since` timestamp filtering.
8. **Functional tests added** — Route registration tests, MCP initialize handshake, meept_send bus verification, tools/list completeness, meept_status.

### Fixes Applied (2026-05-22, Round 2)

9. **CRITICAL: MCP SSE goroutine leak** — `session.done` channel was never closed. When SSE client disconnected and eventChan was full, the forwarding goroutine blocked forever. Fixed with `defer close(session.done)`.
10. **notifications/initialized returns 204 No Content** — Was returning `null\n` as JSON body. Fixed to return `204 No Content` for nil responses (MCP notifications are fire-and-forget).
11. **Dead REST config field wired through** — `rest: false` in config had no runtime effect. Added `RESTEnabled bool` to `ServerConfig`, wired from daemon config, and refactored `setupRoutes()` to conditionally register REST routes via `setupRESTRoutes()`.
12. **Addr() lock downgrade** — Changed from exclusive `Lock()` to `RLock()` since `Addr()` only reads data.
13. **http-api.md added to mkdocs.yml nav** — Page was invisible to documentation site.
14. **CLAUDE.md stale diagram.md reference fixed** — Changed to `concepts/architecture.md`.
15. **architecture.md updated** — Added HTTP Server (REST+WebSocket+MCP/SSE), MenuBar, Flutter UI, and AI Agent clients to architecture diagram.
16. **Test coverage expanded** — 11 new tests: MCP error paths (invalid JSON, wrong content-type, unknown method, missing/unknown tool), notifications/initialized 204, meept_sessions/history/events tool calls, SSE headers, MCP-not-enabled 404, WebSocket connection+broadcast+client-count, SSE session event+bus forwarding, option nil-guards.

## Remaining Work (Optional Enhancements)

### Testing Enhancements (Low Priority)
- End-to-end WebSocket test with actual Flutter UI
- SSE stream test verifying bus event forwarding end-to-end

## Known Limitations

1. **No session persistence for MCP** — Sessions are in-memory only (same as rest of system)
2. **Stdio meept_events broken** — Stdio MCP path's `bus.poll` is broken due to bug 0051 (context cancellation); HTTP path works correctly via session buffering
