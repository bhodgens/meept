# Leaf: WS Session Filter for handleWSEvent

DISPATCH INSTRUCTION: Implement this leaf. Do NOT commit. Do NOT run git add. Write code, run tests, report results only.

## Parent
`master.md` (root)

## Scope
`internal/comm/http/server.go` — `handleWSEvent` function (~line 497).

## Problem
`handleWSEvent` broadcasts `chat_message` events to ALL connected WebSocket clients without filtering by session. `handleWSProgress` (line 515-580) correctly calls `ShouldSendProgress(wc, event.SessionID)` to filter per-connection. `handleWSEvent` has no equivalent.

## Tasks

### Task 1: Add session filtering to handleWSEvent
File: `internal/comm/http/server.go` (~line 497-511)

Read `handleWSProgress` (line 515-580) to see how it filters. Apply the same pattern to `handleWSEvent`:

```go
func (s *Server) handleWSEvent(msg *models.BusMessage) {
    // Extract session_id from the event data for filtering.
    data, _ := msg.Data.(map[string]any)
    eventSessionID, _ := data["session_id"].(string)
    if eventSessionID == "" {
        eventSessionID, _ = data["conversation_id"].(string)
    }

    frontendData := transformBusEventToWS(msg)
    eventType := frontendData["type"].(string)

    s.wsHub.mu.RLock()
    connections := make([]*WebSocketConnection, 0, len(s.wsHub.connections))
    connections = append(connections, s.wsHub.connections...)
    s.wsHub.mu.RUnlock()

    for _, wc := range connections {
        // Filter by session ID when available.
        if eventSessionID != "" && !s.wsHub.ShouldSendProgress(wc, eventSessionID) {
            continue
        }
        select {
        case wc.send <- frontendData:
        default:
        }
    }
}
```

Note: Check the actual method name — `ShouldSendProgress` may need renaming or a separate `ShouldSendToSession` method. Read the existing implementation for the exact pattern.

### Task 2: Test
File: `internal/comm/http/server_test.go`

Add a test that:
- Creates two mock WS connections with different session IDs
- Broadcasts a chat_message with session_id A
- Verifies only connection A receives it

## Self-Verification Checklist
- [ ] `go build ./internal/comm/...` compiles
- [ ] `go test ./internal/comm/...` passes
- [ ] Events without session_id still broadcast to all (backward compat)
- [ ] Events with session_id only reach connections matching that session
