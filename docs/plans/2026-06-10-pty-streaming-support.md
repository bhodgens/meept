# PTY Streaming Support for StreamingTool Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Add PTY (pseudo-terminal) support to StreamingTool interface for interactive tool sessions (gdb, ipython, replit, long-running servers) with real-time output streaming.

**Architecture:**
- New `internal/pty` package wrapping `github.com/creack/pty`
- Extend `StreamingTool` interface with PTY session management
- `ShellExecuteTool` gains PTY mode for interactive commands
- Session manager for concurrent PTY sessions
- WebSocket/HTTP streaming for real-time output to clients

**Tech Stack:** Go 1.24+, `github.com/creack/pty`, `golang.org/x/term`, WebSocket for streaming

---

### Phase 1: PTY Primitive Package

### Task 1: Define PTY Session Interface

**Files:**
- Create: `internal/pty/session.go`
- Test: `internal/pty/session_test.go`

**Step 1: Write interface test**

```go
package pty

import (
    "context"
    "testing"
    "github.com/stretchr/testify/assert"
)

func TestPTYSession_Interface(t *testing.T) {
    // Verify Session implements the interface
    var _ Session = (*ptySession)(nil)
}

func TestPTYSession_BasicIO(t *testing.T) {
    sess, err := NewSession(SessionConfig{
        Cmd: "cat", // Echo back input
    })
    if err != nil {
        t.Skipf("PTY not available: %v", err)
    }
    defer sess.Close()

    // Write input
    _, err = sess.Write([]byte("hello\n"))
    assert.NoError(t, err)

    // Read output (with timeout)
    ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
    defer cancel()

    output := make([]byte, 1024)
    n, err := sess.Read(ctx, output)
    assert.NoError(t, err)
    assert.Greater(t, n, 0)
}
```

**Step 2: Run test to verify it fails**

```bash
go test ./internal/pty/... -v
```
Expected: FAIL with "undefined: Session"

**Step 3: Define PTY session interface and types**

```go
// Package pty provides pseudo-terminal session management for interactive tools.
package pty

import (
    "context"
    "io"
    "os/exec"
    "sync"
    "time"

    "github.com/creack/pty"
)

// Session represents an interactive PTY session.
type Session interface {
    // Write sends input to the session.
    Write(data []byte) (int, error)

    // Read reads output from the session (blocking).
    Read(ctx context.Context, buf []byte) (int, error)

    // Output returns a channel for streaming output.
    Output() <-chan []byte

    // Errors returns a channel for error notifications.
    Errors() <-chan error

    // Size returns the terminal dimensions.
    Size() (rows, cols int)

    // Resize changes the terminal dimensions.
    Resize(rows, cols int) error

    // Close terminates the session.
    Close() error

    // IsRunning returns true if the session is active.
    IsRunning() bool

    // ExitCode returns the exit code (only valid after IsRunning returns false).
    ExitCode() int
}

// SessionConfig holds PTY session configuration.
type SessionConfig struct {
    // Cmd is the command to execute (e.g., "ipython", "gdb").
    Cmd string
    // Args are command-line arguments.
    Args []string
    // Dir is the working directory.
    Dir string
    // Env is the environment variables.
    Env []string
    // Rows is the initial terminal rows (default: 24).
    Rows int
    // Cols is the initial terminal columns (default: 80).
    Cols int
    // Timeout is the command timeout (0 = no timeout).
    Timeout time.Duration
}

// ptySession implements Session using github.com/creack/pty.
type ptySession struct {
    mu         sync.RWMutex
    cmd        *exec.Cmd
    ptmx       *os.File // PTY master
    outputChan chan []byte
    errorChan  chan error
    done       chan struct{}
    closed     bool
    exitCode   int
    rows       int
    cols       int
}

// NewSession creates a new PTY session.
func NewSession(cfg SessionConfig) (Session, error) {
    cmd := exec.Command(cfg.Cmd, cfg.Args...)
    cmd.Dir = cfg.Dir
    cmd.Env = cfg.Env

    rows := cfg.Rows
    if rows <= 0 {
        rows = 24
    }
    cols := cfg.Cols
    if cols <= 0 {
        cols = 80
    }

    // Start PTY
    ptmx, err := pty.StartWithSize(cmd, &pty.Winsize{Rows: uint16(rows), Cols: uint16(cols)})
    if err != nil {
        return nil, err
    }

    sess := &ptySession{
        cmd:        cmd,
        ptmx:       ptmx,
        outputChan: make(chan []byte, 100), // Buffered
        errorChan:  make(chan error, 10),
        done:       make(chan struct{}),
        rows:       rows,
        cols:       cols,
    }

    // Start output reader goroutine
    go sess.readLoop()

    // Wait for command exit
    go sess.waitLoop()

    return sess, nil
}

// Write sends input to the PTY.
func (s *ptySession) Write(data []byte) (int, error) {
    s.mu.RLock()
    defer s.mu.RUnlock()

    if s.closed {
        return 0, ErrSessionClosed
    }

    return s.ptmx.Write(data)
}

// Read reads output from the PTY (context-aware).
func (s *ptySession) Read(ctx context.Context, buf []byte) (int, error) {
    select {
    case <-ctx.Done():
        return 0, ctx.Err()
    case <-s.done:
        return 0, io.EOF
    case err := <-s.errorChan:
        return 0, err
    }
}

// Output returns the output streaming channel.
func (s *ptySession) Output() <-chan []byte {
    return s.outputChan
}

// Errors returns the error channel.
func (s *ptySession) Errors() <-chan error {
    return s.errorChan
}

// Size returns terminal dimensions.
func (s *ptySession) Size() (int, int) {
    s.mu.RLock()
    defer s.mu.RUnlock()
    return s.rows, s.cols
}

// Resize changes terminal dimensions.
func (s *ptySession) Resize(rows, cols int) error {
    s.mu.Lock()
    defer s.mu.Unlock()

    if s.closed {
        return ErrSessionClosed
    }

    s.rows = rows
    s.cols = cols

    return pty.Setsize(s.ptmx, &pty.Winsize{Rows: uint16(rows), Cols: uint16(cols)})
}

// Close terminates the session.
func (s *ptySession) Close() error {
    s.mu.Lock()
    defer s.mu.Unlock()

    if s.closed {
        return nil
    }

    s.closed = true

    // Close PTY master
    if s.ptmx != nil {
        s.ptmx.Close()
    }

    // Kill command if still running
    if s.cmd != nil && s.cmd.Process != nil {
        s.cmd.Process.Kill()
    }

    close(s.done)
    return nil
}

// IsRunning returns true if session is active.
func (s *ptySession) IsRunning() bool {
    s.mu.RLock()
    defer s.mu.RUnlock()
    return !s.closed
}

// ExitCode returns the command exit code.
func (s *ptySession) ExitCode() int {
    s.mu.RLock()
    defer s.mu.RUnlock()
    return s.exitCode
}

// readLoop continuously reads from PTY and pushes to output channel.
func (s *ptySession) readLoop() {
    buf := make([]byte, 4096)
    for {
        n, err := s.ptmx.Read(buf)
        if err != nil {
            if err != io.EOF {
                s.errorChan <- err
            }
            return
        }

        // Copy data to avoid buffer reuse issues
        output := make([]byte, n)
        copy(output, buf[:n])

        select {
        case s.outputChan <- output:
        case <-s.done:
            return
        }
    }
}

// waitLoop waits for command exit and captures exit code.
func (s *ptySession) waitLoop() {
    err := s.cmd.Wait()
    s.mu.Lock()
    if exitErr, ok := err.(*exec.ExitError); ok {
        s.exitCode = exitErr.ExitCode()
    } else if err != nil {
        s.errorChan <- err
    }
    s.mu.Unlock()

    close(s.outputChan)
    close(s.errorChan)
}

// ErrSessionClosed is returned when operating on a closed session.
var ErrSessionClosed = errors.New("session closed")
```

**Step 4: Add missing imports**

```go
import (
    "errors"
    "io"
    "os/exec"
    "sync"
    "time"

    "github.com/creack/pty"
)
```

**Step 5: Run tests**

```bash
go get github.com/creack/pty
go test ./internal/pty/... -v
```

**Step 6: Commit**

```bash
git add internal/pty/session.go internal/pty/session_test.go go.mod go.sum
git commit -m "feat(pty): define Session interface and ptySession implementation"
```

---

### Phase 2: PTY Session Manager

### Task 2: Implement Session Manager

**Files:**
- Create: `internal/pty/manager.go`
- Test: `internal/pty/manager_test.go`

**Step 1: Write manager tests**

```go
package pty

import (
    "testing"
    "github.com/stretchr/testify/assert"
    "github.com/stretchr/testify/require"
)

func TestManager_CreateSession(t *testing.T) {
    mgr := NewManager()
    defer mgr.Close()

    sess, err := mgr.CreateSession("pty-1", SessionConfig{
        Cmd:  "cat",
        Cols: 80,
        Rows: 24,
    })

    if err != nil {
        t.Skipf("PTY not available: %v", err)
    }
    defer mgr.DestroySession("pty-1")

    assert.NotNil(t, sess)
    assert.True(t, sess.IsRunning())
}

func TestManager_GetSession(t *testing.T) {
    mgr := NewManager()
    defer mgr.Close()

    // Create session
    _, err := mgr.CreateSession("test-sess", SessionConfig{Cmd: "cat"})
    if err != nil {
        t.Skipf("PTY not available: %v", err)
    }

    // Get session
    sess := mgr.GetSession("test-sess")
    assert.NotNil(t, sess)

    // Get unknown session
    unknown := mgr.GetSession("unknown")
    assert.Nil(t, unknown)
}

func TestManager_SessionLimit(t *testing.T) {
    mgr := NewManager()
    mgr.maxSessions = 2
    defer mgr.Close()

    // Create max sessions
    for i := 0; i < 2; i++ {
        _, err := mgr.CreateSession(fmt.Sprintf("sess-%d", i), SessionConfig{Cmd: "cat"})
        if err != nil {
            t.Skipf("PTY not available: %v", err)
        }
    }

    // Should fail: limit reached
    _, err := mgr.CreateSession("sess-extra", SessionConfig{Cmd: "cat"})
    assert.Error(t, err)
    assert.Contains(t, err.Error(), "session limit")
}
```

**Step 2: Implement Manager**

```go
package pty

import (
    "fmt"
    "sync"
)

// Manager handles PTY session lifecycle.
type Manager struct {
    mu          sync.RWMutex
    sessions    map[string]Session
    maxSessions int
}

// ManagerConfig holds manager configuration.
type ManagerConfig struct {
    MaxSessions int // Maximum concurrent sessions (0 = unlimited)
}

// NewManager creates a new PTY session manager.
func NewManager() *Manager {
    return &Manager{
        sessions:    make(map[string]Session),
        maxSessions: 10, // Default limit
    }
}

// CreateSession creates a new session with the given ID.
func (m *Manager) CreateSession(id string, cfg SessionConfig) (Session, error) {
    m.mu.Lock()
    defer m.mu.Unlock()

    // Check limit
    if m.maxSessions > 0 && len(m.sessions) >= m.maxSessions {
        return nil, fmt.Errorf("session limit reached (%d)", m.maxSessions)
    }

    // Check ID collision
    if _, exists := m.sessions[id]; exists {
        return nil, fmt.Errorf("session ID already exists: %s", id)
    }

    // Create session
    sess, err := NewSession(cfg)
    if err != nil {
        return nil, err
    }

    m.sessions[id] = sess
    return sess, nil
}

// GetSession retrieves a session by ID.
func (m *Manager) GetSession(id string) Session {
    m.mu.RLock()
    defer m.mu.RUnlock()
    return m.sessions[id]
}

// DestroySession closes and removes a session.
func (m *Manager) DestroySession(id string) error {
    m.mu.Lock()
    defer m.mu.Unlock()

    sess, exists := m.sessions[id]
    if !exists {
        return fmt.Errorf("session not found: %s", id)
    }

    if err := sess.Close(); err != nil {
        return err
    }

    delete(m.sessions, id)
    return nil
}

// ListSessions returns all active session IDs.
func (m *Manager) ListSessions() []string {
    m.mu.RLock()
    defer m.mu.RUnlock()

    ids := make([]string, 0, len(m.sessions))
    for id := range m.sessions {
        ids = append(ids, id)
    }
    return ids
}

// Close shuts down all sessions.
func (m *Manager) Close() error {
    m.mu.Lock()
    defer m.mu.Unlock()

    for id, sess := range m.sessions {
        sess.Close()
        delete(m.sessions, id)
    }

    return nil
}
```

**Step 3: Add missing import**

```go
import "fmt"
```

**Step 4: Run tests**

```bash
go test ./internal/pty/... -v -run TestManager
```

**Step 5: Commit**

```bash
git add internal/pty/manager.go internal/pty/manager_test.go
git commit -m "feat(pty): add Manager for session lifecycle"
```

---

### Phase 3: StreamingTool Interface Extension

### Task 3: Extend StreamingTool for PTY

**Files:**
- Modify: `internal/tools/interface.go`
- Modify: `internal/tools/builtin/shell.go`

**Step 1: Review current StreamingTool interface**

```bash
cat internal/tools/interface.go
```

**Step 2: Extend StreamingTool interface**

```go
// In internal/tools/interface.go:

// StreamingTool is a tool that can stream progress/output during execution.
type StreamingTool interface {
    Tool

    // ExecuteStreaming runs the tool with progress updates.
    ExecuteStreaming(ctx context.Context, args map[string]any, progress func(ProgressUpdate)) (any, error)
}

// PTYTool is a tool that supports interactive PTY sessions.
type PTYTool interface {
    StreamingTool

    // CreateSession creates a new PTY session.
    CreateSession(sessionID string, config PTYSessionConfig) (*PTYSessionInfo, error)

    // WriteToSession sends input to a PTY session.
    WriteToSession(sessionID string, input []byte) error

    // ReadFromSession reads output from a PTY session.
    ReadFromSession(ctx context.Context, sessionID string) ([]byte, error)

    // CloseSession terminates a PTY session.
    CloseSession(sessionID string) error

    // SessionOutput returns a channel for streaming session output.
    SessionOutput(sessionID string) (<-chan []byte, error)
}

// PTYSessionConfig holds PTY session configuration.
type PTYSessionConfig struct {
    Cmd     string
    Args    []string
    Dir     string
    Env     map[string]string
    Rows    int
    Cols    int
    Timeout time.Duration
}

// PTYSessionInfo holds session metadata.
type PTYSessionInfo struct {
    ID        string    `json:"id"`
    Cmd       string    `json:"cmd"`
    CreatedAt time.Time `json:"created_at"`
    Rows      int       `json:"rows"`
    Cols      int       `json:"cols"`
    IsRunning bool      `json:"is_running"`
}
```

**Step 3: Update ShellExecuteTool to implement PTYTool**

```go
// In internal/tools/builtin/shell.go:

type ShellExecuteTool struct {
    // ... existing fields
    ptyMgr *pty.Manager
}

// Update constructor:
func NewShellExecuteTool(ptyMgr *pty.Manager, logger *slog.Logger) *ShellExecuteTool {
    return &ShellExecuteTool{
        ptyMgr: ptyMgr,
        logger: logger.With("component", "shell-tool"),
    }
}

// Implement PTYTool interface:
func (t *ShellExecuteTool) CreateSession(sessionID string, config tools.PTYSessionConfig) (*tools.PTYSessionInfo, error) {
    sess, err := t.ptyMgr.CreateSession(sessionID, pty.SessionConfig{
        Cmd:  config.Cmd,
        Args: config.Args,
        Dir:  config.Dir,
        Rows: config.Rows,
        Cols: config.Cols,
    })
    if err != nil {
        return nil, err
    }

    return &tools.PTYSessionInfo{
        ID:        sessionID,
        Cmd:       config.Cmd,
        CreatedAt: time.Now(),
        Rows:      config.Rows,
        Cols:      config.Cols,
        IsRunning: sess.IsRunning(),
    }, nil
}

func (t *ShellExecuteTool) WriteToSession(sessionID string, input []byte) error {
    sess := t.ptyMgr.GetSession(sessionID)
    if sess == nil {
        return fmt.Errorf("session not found: %s", sessionID)
    }
    _, err := sess.Write(input)
    return err
}

func (t *ShellExecuteTool) ReadFromSession(ctx context.Context, sessionID string) ([]byte, error) {
    sess := t.ptyMgr.GetSession(sessionID)
    if sess == nil {
        return nil, fmt.Errorf("session not found: %s", sessionID)
    }

    buf := make([]byte, 4096)
    n, err := sess.Read(ctx, buf)
    return buf[:n], err
}

func (t *ShellExecuteTool) CloseSession(sessionID string) error {
    return t.ptyMgr.DestroySession(sessionID)
}

func (t *ShellExecuteTool) SessionOutput(sessionID string) (<-chan []byte, error) {
    sess := t.ptyMgr.GetSession(sessionID)
    if sess == nil {
        return nil, fmt.Errorf("session not found: %s", sessionID)
    }
    return sess.Output(), nil
}
```

**Step 4: Run tests**

```bash
go test ./internal/tools/builtin/... -v -run Shell
```

**Step 5: Commit**

```bash
git add internal/tools/interface.go internal/tools/builtin/shell.go
git commit -m "feat(tools): extend StreamingTool with PTY support"
```

---

### Phase 4: HTTP/WebSocket Streaming

### Task 4: Create PTY HTTP Handler

**Files:**
- Create: `internal/comm/http/pty_handler.go`
- Test: `internal/comm/http/pty_handler_test.go`

**Step 1: Write handler tests**

```go
package http

import (
    "net/http/httptest"
    "testing"
)

func TestPTYHandler_CreateSession(t *testing.T) {
    // Test POST /api/v1/pty/sessions
}

func TestPTYHandler_WriteToSession(t *testing.T) {
    // Test POST /api/v1/pty/sessions/{id}/write
}

func TestPTYHandler_StreamSession(t *testing.T) {
    // Test GET /api/v1/pty/sessions/{id}/stream (WebSocket)
}
```

**Step 2: Implement HTTP handlers**

```go
package http

import (
    "encoding/json"
    "net/http"
    "time"

    "github.com/caimlas/meept/internal/pty"
    "github.com/caimlas/meept/internal/tools"
    "github.com/gorilla/websocket"
)

var upgrader = websocket.Upgrader{
    CheckOrigin: func(r *http.Request) bool {
        return true // Allow all for localhost
    },
}

// PTYHandler handles PTY session HTTP requests.
type PTYHandler struct {
    ptyMgr *pty.Manager
    logger *slog.Logger
}

// NewPTYHandler creates a new PTY HTTP handler.
func NewPTYHandler(ptyMgr *pty.Manager, logger *slog.Logger) *PTYHandler {
    return &PTYHandler{
        ptyMgr: ptyMgr,
        logger: logger.With("component", "pty-handler"),
    }
}

// RegisterRoutes registers PTY endpoints.
func (h *PTYHandler) RegisterRoutes(mux *http.ServeMux) {
    mux.HandleFunc("/api/v1/pty/sessions", h.handleSessions)
    mux.HandleFunc("/api/v1/pty/sessions/", h.handleSession)
}

// handleSessions handles POST /api/v1/pty/sessions (create session)
func (h *PTYHandler) handleSessions(w http.ResponseWriter, r *http.Request) {
    if r.Method != http.MethodPost {
        http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
        return
    }

    var req tools.PTYSessionConfig
    if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
        http.Error(w, err.Error(), http.StatusBadRequest)
        return
    }

    sessionID := generateSessionID()
    info, err := h.ptyMgr.CreateSession(sessionID, pty.SessionConfig{
        Cmd:  req.Cmd,
        Args: req.Args,
        Dir:  req.Dir,
        Rows: req.Rows,
        Cols: req.Cols,
    })

    if err != nil {
        http.Error(w, err.Error(), http.StatusInternalServerError)
        return
    }

    w.Header().Set("Content-Type", "application/json")
    json.NewEncoder(w).Encode(info)
}

// handleSession handles session-specific endpoints
func (h *PTYHandler) handleSession(w http.ResponseWriter, r *http.Request) {
    // Extract session ID from URL path
    sessionID := extractSessionID(r.URL.Path)

    switch r.Method {
    case http.MethodGet:
        // WebSocket stream
        h.streamSession(w, r, sessionID)
    case http.MethodPost:
        // Write to session
        h.writeToSession(w, r, sessionID)
    case http.MethodDelete:
        // Close session
        h.closeSession(w, r, sessionID)
    default:
        http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
    }
}

func (h *PTYHandler) streamSession(w http.ResponseWriter, r *http.Request, sessionID string) {
    conn, err := upgrader.Upgrade(w, r, nil)
    if err != nil {
        h.logger.Error("WebSocket upgrade failed", "error", err)
        return
    }
    defer conn.Close()

    outputChan, err := h.ptyMgr.GetSession(sessionID).Output()
    if err != nil {
        h.logger.Error("Session not found", "session_id", sessionID)
        return
    }

    for output := range outputChan {
        if err := conn.WriteMessage(websocket.BinaryMessage, output); err != nil {
            h.logger.Error("WebSocket send failed", "error", err)
            return
        }
    }
}

func (h *PTYHandler) writeToSession(w http.ResponseWriter, r *http.Request, sessionID string) {
    var req struct {
        Input string `json:"input"`
    }
    if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
        http.Error(w, err.Error(), http.StatusBadRequest)
        return
    }

    sess := h.ptyMgr.GetSession(sessionID)
    if sess == nil {
        http.Error(w, "Session not found", http.StatusNotFound)
        return
    }

    if _, err := sess.Write([]byte(req.Input)); err != nil {
        http.Error(w, err.Error(), http.StatusInternalServerError)
        return
    }

    w.WriteHeader(http.StatusOK)
}

func (h *PTYHandler) closeSession(w http.ResponseWriter, r *http.Request, sessionID string) {
    if err := h.ptyMgr.DestroySession(sessionID); err != nil {
        http.Error(w, err.Error(), http.StatusInternalServerError)
        return
    }
    w.WriteHeader(http.StatusOK)
}

// Helpers
func generateSessionID() string {
    return fmt.Sprintf("pty-%d", time.Now().UnixNano())
}

func extractSessionID(path string) string {
    // /api/v1/pty/sessions/{id}[/...]
    parts := strings.Split(path, "/")
    if len(parts) >= 5 {
        return parts[4]
    }
    return ""
}
```

**Step 3: Add missing imports**

```go
import (
    "encoding/json"
    "fmt"
    "log/slog"
    "net/http"
    "strings"
    "time"

    "github.com/caimlas/meept/internal/pty"
    "github.com/caimlas/meept/internal/tools"
    "github.com/gorilla/websocket"
)
```

**Step 4: Commit**

```bash
git add internal/comm/http/pty_handler.go
git commit -m "feat(http): add PTY WebSocket streaming handlers"
```

---

### Phase 5: Configuration and Documentation

### Task 5: Add PTY Configuration

**Files:**
- Modify: `internal/config/schema.go`
- Create: `config/pty.json5`

**Step 1: Add PTY config**

```go
type PTYConfig struct {
    // Enabled controls PTY session support.
    Enabled bool `json:"enabled"`
    // MaxSessions is the maximum concurrent PTY sessions.
    MaxSessions int `json:"max_sessions"`
    // DefaultTimeout is the session timeout in seconds.
    DefaultTimeout int `json:"default_timeout"`
}
```

**Step 2: Create config template**

```json5
// config/pty.json5
{
  pty: {
    // Enable PTY interactive sessions (default: true)
    enabled: true,

    // Maximum concurrent PTY sessions (0 = unlimited)
    max_sessions: 10,

    // Default session timeout in seconds (0 = no timeout)
    default_timeout: 3600,
  }
}
```

**Step 3: Commit**

```bash
git add internal/config/schema.go config/pty.json5
git commit -m "feat(config): add PTY configuration schema"
```

---

### Task 6: Documentation

**Files:**
- Create: `docs/concepts/pty-streaming.md`

**Step 1: Write documentation**

```markdown
# PTY Streaming Support

Meept supports pseudo-terminal (PTY) sessions for interactive tool execution with real-time output streaming.

## Use Cases

- **Interactive debuggers**: gdb, pdb, delve
- **REPLs**: ipython, node, go run
- **Long-running servers**: Development servers during testing
- **TUI applications**: vim, htop inside agent sessions

## Architecture

```
┌─────────────┐    WebSocket    ┌──────────────┐    PTY    ┌─────────────┐
│  TUI/Web    │◄───────────────►│ HTTP Handler │◄────────►│ Shell/Tool  │
│   Client    │    JSON/Binary  │  /api/v1/pty │         │  (ipython)  │
└─────────────┘                 └──────────────┘         └─────────────┘
```

## API Endpoints

| Endpoint | Method | Purpose |
|----------|--------|---------|
| `/api/v1/pty/sessions` | POST | Create new session |
| `/api/v1/pty/sessions/{id}` | GET | WebSocket stream |
| `/api/v1/pty/sessions/{id}` | POST | Write input |
| `/api/v1/pty/sessions/{id}` | DELETE | Close session |

## Example: Create IPython Session

```bash
# Create session
curl -X POST http://localhost:8081/api/v1/pty/sessions \
  -H "Content-Type: application/json" \
  -d '{"cmd": "ipython", "rows": 24, "cols": 80}'

# Response: {"id": "pty-123", "cmd": "ipython", ...}
```

## Example: WebSocket Stream

```javascript
const ws = new WebSocket('ws://localhost:8081/api/v1/pty/sessions/pty-123');
ws.onmessage = (event) => {
  console.log(new TextDecoder().decode(event.data));
};

// Send input
ws.send('print("hello")\n');
```

## Configuration

```json5
{
  pty: {
    enabled: true,
    max_sessions: 10,
    default_timeout: 3600,
  }
}
```
```

**Step 2: Commit**

```bash
git add docs/concepts/pty-streaming.md
git commit -m "docs: add PTY streaming documentation"
```

---

## Summary

**Total Tasks:** 6
**Estimated Time:** 3-4 hours
**Complexity:** Medium (PTY handling, WebSocket streaming)

### Files to Create:

| File | Purpose |
|------|---------|
| `internal/pty/session.go` | PTY session interface |
| `internal/pty/manager.go` | Session lifecycle |
| `internal/comm/http/pty_handler.go` | HTTP/WebSocket handlers |
| `config/pty.json5` | Config template |
| `docs/concepts/pty-streaming.md` | Documentation |

### Files to Modify:

| File | Change |
|------|--------|
| `internal/tools/interface.go` | Add PTYTool interface |
| `internal/tools/builtin/shell.go` | Implement PTYTool |
| `internal/config/schema.go` | Add PTYConfig |

---

**Plan complete and saved to** `docs/plans/2026-06-10-pty-streaming-support.md`.

Two execution options:

1. **Subagent-Driven** - Dispatch fresh subagents per task with review between phases
2. **Parallel Session** - Open new session with `superpowers:executing-plans`

Which approach for each plan?
