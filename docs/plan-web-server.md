# Plan: Web Server Completion

**Status:** Not Started
**Priority:** Low
**Estimated Effort:** 3-4 days

---

## Current State

The web server has **basic structure** but **many endpoints are TODO**:

| Component | File | Status |
|-----------|------|--------|
| Server | `internal/comm/web/server.go` | Basic structure (292 lines) |
| Auth | `internal/comm/web/auth.go` | Stubbed |

### What Exists

1. **Server Framework** (`server.go`)
   - HTTP server with configurable timeouts
   - CORS support
   - Middleware (logging, auth)
   - Route registration

2. **Working Endpoints**
   - `GET /health` - Health check
   - `GET /api/v1/status` - Daemon status
   - `POST /api/v1/chat` - Chat endpoint

3. **Stub Endpoints** (return empty arrays)
   - `GET /api/v1/memory/search` - TODO
   - `GET /api/v1/skills` - TODO
   - `GET /api/v1/jobs` - TODO

### What's Missing

1. **No daemon integration** - Server not started
2. **No handler implementation** - Handler interface not connected
3. **Many missing endpoints** - Sessions, agents, tools, etc.
4. **No WebSocket support** - Real-time updates
5. **No authentication** - Authenticator not implemented
6. **No static file serving** - No web UI support

---

## Implementation Plan

### Phase 1: Handler Implementation

**File:** `internal/comm/web/handler.go` (new)

Create a handler that connects to daemon components:

```go
package web

import (
    "context"

    "github.com/caimlas/meept/internal/agent"
    "github.com/caimlas/meept/internal/memory"
    "github.com/caimlas/meept/internal/queue"
    "github.com/caimlas/meept/internal/session"
    "github.com/caimlas/meept/internal/skills"
)

// DaemonHandler implements the Handler interface using daemon components.
type DaemonHandler struct {
    agentLoop    *agent.AgentLoop
    sessionMgr   *session.Manager
    memoryMgr    *memory.Manager
    skillReg     *skills.Registry
    jobStore     *queue.Store
}

// NewDaemonHandler creates a new handler.
func NewDaemonHandler(
    agentLoop *agent.AgentLoop,
    sessionMgr *session.Manager,
    memoryMgr *memory.Manager,
    skillReg *skills.Registry,
    jobStore *queue.Store,
) *DaemonHandler {
    return &DaemonHandler{
        agentLoop:  agentLoop,
        sessionMgr: sessionMgr,
        memoryMgr:  memoryMgr,
        skillReg:   skillReg,
        jobStore:   jobStore,
    }
}

func (h *DaemonHandler) Chat(ctx context.Context, message string) (string, error) {
    response, err := h.agentLoop.Run(ctx, message)
    if err != nil {
        return "", err
    }
    return response.Content, nil
}

func (h *DaemonHandler) Status(ctx context.Context) (map[string]any, error) {
    return map[string]any{
        "status":   "running",
        "sessions": h.sessionMgr.Count(),
        "jobs":     h.jobStore.Count(),
    }, nil
}

func (h *DaemonHandler) SearchMemory(ctx context.Context, query string, limit int) ([]memory.SearchResult, error) {
    return h.memoryMgr.Search(ctx, query, limit)
}

func (h *DaemonHandler) ListSkills(ctx context.Context) ([]skills.Skill, error) {
    return h.skillReg.List(), nil
}

func (h *DaemonHandler) ListJobs(ctx context.Context, status string, limit int) ([]queue.Job, error) {
    return h.jobStore.List(ctx, status, limit)
}
```

### Phase 2: Complete Endpoints

**File:** `internal/comm/web/server.go`

Add missing endpoints:

```go
func (s *Server) setupRoutes(mux *http.ServeMux) {
    // Health
    mux.HandleFunc("GET /health", s.handleHealth)
    mux.HandleFunc("GET /api/v1/health", s.handleHealth)

    // Status
    mux.HandleFunc("GET /api/v1/status", s.handleStatus)

    // Chat
    mux.HandleFunc("POST /api/v1/chat", s.handleChat)
    mux.HandleFunc("POST /api/v1/chat/stream", s.handleChatStream) // NEW

    // Sessions
    mux.HandleFunc("GET /api/v1/sessions", s.handleSessionsList)
    mux.HandleFunc("POST /api/v1/sessions", s.handleSessionsCreate)
    mux.HandleFunc("GET /api/v1/sessions/{id}", s.handleSessionsGet)
    mux.HandleFunc("DELETE /api/v1/sessions/{id}", s.handleSessionsDelete)

    // Memory
    mux.HandleFunc("GET /api/v1/memory/search", s.handleMemorySearch)
    mux.HandleFunc("POST /api/v1/memory", s.handleMemoryStore)

    // Skills
    mux.HandleFunc("GET /api/v1/skills", s.handleSkillsList)
    mux.HandleFunc("POST /api/v1/skills/{name}/execute", s.handleSkillsExecute)

    // Jobs
    mux.HandleFunc("GET /api/v1/jobs", s.handleJobsList)
    mux.HandleFunc("POST /api/v1/jobs", s.handleJobsCreate)
    mux.HandleFunc("GET /api/v1/jobs/{id}", s.handleJobsGet)
    mux.HandleFunc("DELETE /api/v1/jobs/{id}", s.handleJobsCancel)

    // Agents
    mux.HandleFunc("GET /api/v1/agents", s.handleAgentsList)
    mux.HandleFunc("POST /api/v1/agents/{id}/delegate", s.handleAgentsDelegate)

    // Tools
    mux.HandleFunc("GET /api/v1/tools", s.handleToolsList)

    // WebSocket
    mux.HandleFunc("GET /api/v1/ws", s.handleWebSocket)
}
```

### Phase 3: WebSocket Support

**File:** `internal/comm/web/websocket.go` (new)

Add real-time updates:

```go
package web

import (
    "context"
    "encoding/json"
    "log/slog"
    "net/http"
    "sync"

    "github.com/gorilla/websocket"
)

var upgrader = websocket.Upgrader{
    ReadBufferSize:  1024,
    WriteBufferSize: 1024,
    CheckOrigin: func(r *http.Request) bool {
        return true // Configure for production
    },
}

// WebSocketHub manages WebSocket connections.
type WebSocketHub struct {
    mu      sync.RWMutex
    clients map[*websocket.Conn]bool
    logger  *slog.Logger
}

// NewWebSocketHub creates a new hub.
func NewWebSocketHub(logger *slog.Logger) *WebSocketHub {
    return &WebSocketHub{
        clients: make(map[*websocket.Conn]bool),
        logger:  logger,
    }
}

// Register adds a client.
func (h *WebSocketHub) Register(conn *websocket.Conn) {
    h.mu.Lock()
    h.clients[conn] = true
    h.mu.Unlock()
}

// Unregister removes a client.
func (h *WebSocketHub) Unregister(conn *websocket.Conn) {
    h.mu.Lock()
    delete(h.clients, conn)
    h.mu.Unlock()
    conn.Close()
}

// Broadcast sends a message to all clients.
func (h *WebSocketHub) Broadcast(msgType string, data any) {
    msg := map[string]any{
        "type": msgType,
        "data": data,
    }

    payload, err := json.Marshal(msg)
    if err != nil {
        return
    }

    h.mu.RLock()
    defer h.mu.RUnlock()

    for conn := range h.clients {
        if err := conn.WriteMessage(websocket.TextMessage, payload); err != nil {
            h.logger.Warn("ws write error", "error", err)
            go h.Unregister(conn)
        }
    }
}

// handleWebSocket handles WebSocket upgrade.
func (s *Server) handleWebSocket(w http.ResponseWriter, r *http.Request) {
    conn, err := upgrader.Upgrade(w, r, nil)
    if err != nil {
        s.logger.Error("ws upgrade error", "error", err)
        return
    }

    s.wsHub.Register(conn)

    // Read loop
    go func() {
        defer s.wsHub.Unregister(conn)
        for {
            _, message, err := conn.ReadMessage()
            if err != nil {
                break
            }
            s.handleWSMessage(conn, message)
        }
    }()
}

func (s *Server) handleWSMessage(conn *websocket.Conn, message []byte) {
    var msg struct {
        Type string          `json:"type"`
        Data json.RawMessage `json:"data"`
    }

    if err := json.Unmarshal(message, &msg); err != nil {
        return
    }

    switch msg.Type {
    case "chat":
        // Handle chat message
    case "subscribe":
        // Handle subscription
    }
}
```

### Phase 4: Streaming Chat

**File:** `internal/comm/web/server.go`

Add Server-Sent Events for streaming:

```go
func (s *Server) handleChatStream(w http.ResponseWriter, r *http.Request) {
    // Set headers for SSE
    w.Header().Set("Content-Type", "text/event-stream")
    w.Header().Set("Cache-Control", "no-cache")
    w.Header().Set("Connection", "keep-alive")

    var req struct {
        Message string `json:"message"`
    }
    if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
        s.writeError(w, http.StatusBadRequest, "invalid request")
        return
    }

    flusher, ok := w.(http.Flusher)
    if !ok {
        s.writeError(w, http.StatusInternalServerError, "streaming not supported")
        return
    }

    // Stream response chunks
    ctx := r.Context()
    chunks := make(chan string)
    done := make(chan error)

    go func() {
        done <- s.handler.ChatStream(ctx, req.Message, chunks)
    }()

    for {
        select {
        case chunk, ok := <-chunks:
            if !ok {
                return
            }
            fmt.Fprintf(w, "data: %s\n\n", chunk)
            flusher.Flush()
        case err := <-done:
            if err != nil {
                fmt.Fprintf(w, "event: error\ndata: %s\n\n", err.Error())
            }
            fmt.Fprintf(w, "event: done\ndata: \n\n")
            flusher.Flush()
            return
        case <-ctx.Done():
            return
        }
    }
}
```

### Phase 5: Authentication

**File:** `internal/comm/web/auth.go`

Implement authentication:

```go
package web

import (
    "crypto/subtle"
    "net/http"
    "strings"
)

// Authenticator validates requests.
type Authenticator interface {
    Authenticate(r *http.Request) bool
}

// APIKeyAuth validates API key in header.
type APIKeyAuth struct {
    keys map[string]bool
}

// NewAPIKeyAuth creates API key authenticator.
func NewAPIKeyAuth(keys []string) *APIKeyAuth {
    keyMap := make(map[string]bool)
    for _, k := range keys {
        keyMap[k] = true
    }
    return &APIKeyAuth{keys: keyMap}
}

func (a *APIKeyAuth) Authenticate(r *http.Request) bool {
    // Check header
    key := r.Header.Get("X-API-Key")
    if key == "" {
        // Try Authorization Bearer
        auth := r.Header.Get("Authorization")
        if strings.HasPrefix(auth, "Bearer ") {
            key = strings.TrimPrefix(auth, "Bearer ")
        }
    }

    if key == "" {
        return false
    }

    return a.keys[key]
}

// JWTAuth validates JWT tokens.
type JWTAuth struct {
    secret []byte
}

// NewJWTAuth creates JWT authenticator.
func NewJWTAuth(secret string) *JWTAuth {
    return &JWTAuth{secret: []byte(secret)}
}

func (a *JWTAuth) Authenticate(r *http.Request) bool {
    auth := r.Header.Get("Authorization")
    if !strings.HasPrefix(auth, "Bearer ") {
        return false
    }

    token := strings.TrimPrefix(auth, "Bearer ")
    // Validate JWT token
    // ... JWT validation logic ...
    _ = token
    return true
}

// NoAuth allows all requests (for local use).
type NoAuth struct{}

func (a *NoAuth) Authenticate(r *http.Request) bool {
    return true
}
```

### Phase 6: Daemon Integration

**File:** `internal/daemon/components.go`

**Changes:**

```go
type Components struct {
    // ... existing fields
    webServer *web.Server
}

func NewComponents(cfg *config.Config, ...) (*Components, error) {
    // ...

    // Initialize web server
    var webServer *web.Server
    if cfg.Web.Enabled {
        handler := web.NewDaemonHandler(
            agentLoop,
            sessionMgr,
            memoryMgr,
            skillReg,
            jobStore,
        )

        var auth web.Authenticator
        if len(cfg.Web.APIKeys) > 0 {
            auth = web.NewAPIKeyAuth(cfg.Web.APIKeys)
        } else {
            auth = &web.NoAuth{}
        }

        webServer = web.NewServer(
            web.ServerConfig{
                Addr:       cfg.Web.Addr,
                EnableCORS: cfg.Web.EnableCORS,
            },
            handler,
            auth,
            logger,
        )
    }

    c.webServer = webServer
    // ...
}

func (c *Components) Start(ctx context.Context) error {
    // ...

    // Start web server
    if c.webServer != nil {
        go func() {
            if err := c.webServer.Start(ctx); err != nil {
                c.logger.Error("web server error", "error", err)
            }
        }()
    }

    return nil
}

func (c *Components) Stop() error {
    if c.webServer != nil {
        c.webServer.Shutdown(context.Background())
    }
    // ...
}
```

### Phase 7: Configuration

**File:** `internal/config/schema.go`

```go
type WebConfig struct {
    Enabled    bool     `toml:"enabled"`
    Addr       string   `toml:"addr"`
    EnableCORS bool     `toml:"enable_cors"`
    APIKeys    []string `toml:"api_keys"`
    JWTSecret  string   `toml:"jwt_secret"`
}
```

**File:** `~/.meept/meept.toml`

```toml
[web]
enabled = false
addr = ":8080"
enable_cors = true
api_keys = []  # Empty = no auth required
```

---

## API Documentation

### Chat API

```
POST /api/v1/chat
Content-Type: application/json

{"message": "Hello"}

Response:
{"response": "Hello! How can I help?"}
```

### Streaming Chat

```
POST /api/v1/chat/stream
Content-Type: application/json

{"message": "Tell me a story"}

Response (SSE):
data: Once upon a time...
data: there was a...
event: done
data:
```

### Sessions

```
GET /api/v1/sessions
Response: {"sessions": [...]}

POST /api/v1/sessions
{"name": "My Session"}
Response: {"id": "...", "name": "My Session"}

GET /api/v1/sessions/{id}
Response: {...session details...}

DELETE /api/v1/sessions/{id}
Response: {"ok": true}
```

### Memory

```
GET /api/v1/memory/search?q=query&limit=10
Response: {"results": [...]}

POST /api/v1/memory
{"content": "...", "type": "episodic"}
Response: {"id": "..."}
```

---

## Testing Plan

### Unit Tests

1. **Handler tests** - All endpoints
2. **Auth tests** - API key, JWT
3. **WebSocket tests** - Connection, broadcast

### Integration Tests

1. Test server startup/shutdown
2. Test all endpoints with real daemon
3. Test WebSocket streaming

### Manual Testing

1. Start daemon with web enabled
2. Test with curl:
   ```bash
   curl http://localhost:8080/health
   curl -X POST http://localhost:8080/api/v1/chat \
     -H "Content-Type: application/json" \
     -d '{"message": "Hello"}'
   ```

---

## Files to Modify

| File | Changes |
|------|---------|
| `internal/comm/web/server.go` | Add all endpoints |
| `internal/comm/web/auth.go` | Implement auth |
| `internal/daemon/components.go` | Initialize server |
| `internal/config/schema.go` | Add web config |
| `config/meept.toml` | Add web section |

## Files to Create

| File | Purpose |
|------|---------|
| `internal/comm/web/handler.go` | Daemon handler |
| `internal/comm/web/websocket.go` | WebSocket support |
| `tests/integration/web_test.go` | Integration tests |

---

## Success Criteria

1. Web server starts with daemon
2. All API endpoints functional
3. WebSocket streaming works
4. Authentication works
5. CORS configured correctly
6. Tests pass
