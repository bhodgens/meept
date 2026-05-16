# MCP server stdin loses messages due to recreating bufio.Reader
**Date**: 2026-05-16
**Phase**: 4
**Severity**: high
**Component**: mcp

## Description
The MCP server's `ReadMessage` function creates a new `bufio.NewReader(r)` wrapping `os.Stdin` on **every** call. Since `bufio.Reader` buffers data ahead of what's actually read, the first call's internal buffer may consume data past the first newline that subsequent calls cannot see, causing MCP requests to be silently dropped.

This manifests when multiple MCP messages are piped to the server at once (the first message is processed, subsequent ones are lost). The server processes one message then appears to stall or miss requests.

## Reproduction
Pipe multiple JSON-RPC messages to `meept mcp-chat-server`:
```bash
printf '{"jsonrpc":"2.0","id":1,...}\n{"jsonrpc":"2.0","id":2,...}\n' | meept mcp-chat-server
```

Result: Only one response is emitted, the second message is silently dropped.

## Evidence
Using Python subprocess with per-message `flush()` and `time.sleep(0.3)` all messages are processed correctly. Using shell piping with all messages sent at once, only the first response is emitted.

The root cause: `bufio.NewReader(r)` at `internal/mcp/transport.go:36` creates a fresh buffered reader wrapping stdin on each `processOne()` → `ReadMessage()` call. The first call to `ReadBytes('\n')` may pull multiple lines into the buffer; subsequent calls create a new buffered reader on stdin that starts fresh, missing the unconsumed buffered data.

## Root Cause
`internal/mcp/transport.go` line 36:
```go
func ReadMessage(r io.Reader) (*JSONRPCRequest, error) {
    reader := bufio.NewReader(r)  // New buffer on every call!
    line, err := reader.ReadBytes('\n')
    ...
}
```

## Impact
- MCP server may silently drop messages when receiving quick bursts of requests
- Testing MCP via piped shells is unreliable
- For Claude Code integration, messages are sent one at a time by the MCP client so this is unlikely to affect production use, but it does make manual testing difficult

## Proposed Fix
Create a single `bufio.Reader` that persists across calls, e.g.:

```go
type Server struct {
    input   io.Reader
    reader  *bufio.Reader  // Add buffered reader field
    output  io.Writer
    ...
}

// In NewServer:
reader := bufio.NewReaderSize(input, 4096)
srv := &Server{input: input, reader: reader, ...}

// In ReadMessage or processOne:
line, err := srv.reader.ReadBytes('\n')
```

## Fix (2026-05-16)

Added a `BufferedReader` type (`transport.go`) that wraps `io.Reader` with a persistent `bufio.Reader` (4096-byte buffer). The `Server` struct now creates a `BufferedReader` in `NewServer` and uses the new `ReadMessageBuffered(br)` function instead of the deprecated `ReadMessage(r)`. The old `ReadMessage` function is preserved for backward compatibility but marked deprecated. A test `TestReadMessageBufferedMultiple` verifies multiple piped messages are read correctly.

## Classification
[x] Harness bug  [ ] Model quality  [ ] Design gap
