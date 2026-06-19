# meept-lite

## Overview

Minimalistic console client for meept (`cmd/meept-lite/`). Single-binary alternative to the full TUI (`internal/tui/`) — provides chat + session switching without the bubbletea dependency tree. Useful for low-bandwidth SSH sessions, scripted interaction, and as a reference client for transport wiring.

## Problem

The full TUI (`cmd/meept`) pulls in bubbletea, lipgloss, glamour, and the broader charmbracelet ecosystem. That's heavy when the use case is:
- A quick chat from a shell where the daemon is already running
- An SSH session to a remote box where the daemon runs
- A reference for how to wire the transport layer (`internal/transport`) to a client

`meept-lite` answers all three with ~3 files and only the `sharedclient` + `transport` packages.

## Behavior

### Entry point (`main.go`)

- Parses flags: `--socket`/`-s` (Unix socket path), `--session` (session name), `--transport` (`rpc` or `http`), `--http-url` (HTTP base URL).
- Default socket: `~/.meept/meept.sock`. Default HTTP URL: `http://localhost:8081`.
- Constructs `transport.Config` and calls `transport.New(cfg)` — this returns either an RPC client (Unix socket) or HTTP client depending on `--transport`.
- `client.Connect()` failure prints a hint pointing the user at `meept daemon start`.
- Creates a `sharedclient.SessionManager` and calls `LoadOrCreateSession(ctx, sessionName)`. An empty `sessionName` resolves to the most recent session or `"default"` if none exists.
- Hands off to `NewTUI(client, sessionMgr).Run()`.

### TUI (`tui.go`)

- Hand-rolled terminal loop — no bubbletea. Reads from stdin, writes ANSI-escaped output to stdout.
- Commands start with `/`; everything else is chat input sent via the session manager.
- Updates session metadata (e.g., description derived from the first user message) via `sessionMgr.UpdateSessionDescription`.

### Handlers (`handlers.go`)

- Wraps the transport client for the chat/send-message RPC and translates responses into renderable form.
- Routes errors back to the TUI for display.

## Configuration

No config file. All flags are command-line:
```
meept-lite [--socket path] [-s path]
           [--session name]
           [--transport rpc|http]
           [--http-url url]
```

Session persistence is handled by the daemon — `meept-lite` only holds the active session ID in memory.

## Edge Cases

- **Transport mismatch**: if `--transport=http` but the daemon only has RPC enabled (or vice versa), `Connect()` returns a clear error with a hint to use the other transport or specify a different URL.
- **Session creation race**: two `meept-lite` instances starting with the same empty `--session` will both try to create `"default"`. The daemon's session manager handles this idempotently — both clients land on the same session.
- **Graceful shutdown**: Ctrl-C triggers transport `Close()` which drains in-flight requests. No state needs flushing on the client side (all persistence is server-side).
- **RPC socket missing**: prints `"failed to connect to daemon"` with a hint to start the daemon. Exit code 1.

## When to use vs. full TUI

| Use case | Choose |
|----------|--------|
| Daily driver on local machine | `meept` (full TUI) |
| SSH to remote daemon | `meept-lite` |
| Scripted send-and-print | `meept-lite` piped from stdin |
- Learning the transport API | Read `cmd/meept-lite/main.go` |

---

*Documents the `cmd/meept-lite/` package.*
