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

## Package Overview

The PTY package (`internal/pty`) provides:

- **Session interface**: Abstracts interactive sessions
- **ptySession**: PTY-backed implementation using `github.com/creack/pty`
- **Manager**: Session lifecycle management with concurrency control
- **Graceful fallback**: Non-PTY subprocess mode when PTY unavailable

## Configuration

```json5
{
  pty: {
    // Enable PTY interactive sessions (default: true)
    enabled: true,

    // Maximum concurrent PTY sessions (0 = unlimited)
    max_sessions: 10,

    // Default session timeout in seconds (0 = no timeout)
    default_timeout: 3600,

    // Default terminal dimensions
    default_rows: 24,
    default_cols: 80,
  }
}
```

## API

### Creating a Session

```go
import "github.com/caimlas/meept/internal/pty"

// Create manager
mgr := pty.NewManager()

// Create session
sess, err := mgr.CreateSession("session-1", pty.SessionConfig{
    Cmd:  "ipython",
    Args: []string{"--no-banner"},
    Rows: 24,
    Cols: 80,
})
if err != nil {
    log.Fatal(err)
}
defer mgr.DestroySession("session-1")
```

### Writing Input

```go
// Send command
_, err := sess.Write([]byte("print('hello')\n"))
if err != nil {
    log.Fatal(err)
}
```

### Reading Output

```go
// Blocking read with context
ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
defer cancel()

buf := make([]byte, 1024)
n, err := sess.Read(ctx, buf)
if err != nil {
    log.Fatal(err)
}
fmt.Println(string(buf[:n]))
```

### Streaming Output

```go
// Get output channel
outputChan := sess.Output()

// Read streaming output
for output := range outputChan {
    fmt.Print(string(output))
}
```

### Resizing Terminal

```go
// Change terminal size
err := sess.Resize(40, 120)
if err != nil {
    log.Fatal(err)
}
```

## HTTP/WebSocket Endpoints

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

## Platform Compatibility

| Platform | PTY Support | Fallback |
|----------|-------------|----------|
| Linux    | ✅ Full     | None     |
| macOS    | ✅ Full     | None     |
| Windows  | ⚠️ Limited  | subprocess pipes |

When PTY is unavailable, the session automatically falls back to non-PTY subprocess mode where:
- `Read`/`Write` operate on stdin/stdout pipes
- Terminal resizing is not available
- Interactive tools may have limited functionality
