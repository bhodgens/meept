# ACP Protocol Types + Stdio Transport — Implementation Leaf

> **For the implementing agent:** Implement ALL tasks below using TDD.
> Do NOT commit. Do NOT use read_file on existing source files.

## Meta

- **Parent:** ../master.md
- **Scope:** internal/acp package foundation: JSON-RPC 2.0 wire types, ACP method/params structs, newline-delimited stdio transport with request correlation
- **Dependencies:** 01 (config types compile — actually none at runtime; only ordering)
- **Estimated Context:** 50K
- **Concurrency Group:** B

## Goal

The wire layer every later leaf builds on. Package `internal/acp` with protocol.go (types), transport.go (read/write loop over an io.Reader/io.Writer pair), and the JSON-RPC pending-call map that correlates responses to requests and fans notifications out to subscribers.

## AMBER CONTRACT — verify before implementing

Before writing protocol.go, fetch https://agentclientprotocol.com (or the agentclientprotocol GitHub repo README/schema files) and verify:
1. Exact protocol version string clients send in initialize
2. Framing: newline-delimited JSON vs Content-Length headers
3. Exact core method names: initialize, session/new, session/prompt, session/update, session/requestPermission, session/cancel (or their true names)

Record verified values in plans/acp-agent-runtime/SHARED-CONVENTIONS.md §2 (edit that file) AND in your report. If ACP differs from master Contract 3, your corrected types ARE the contract — later leaves dispatch from your text. Use web tools; do not guess.

## Interface Contracts (From Parent)

### What This Leaf Exposes

```go
// internal/acp/protocol.go
const ProtocolVersion = "<verified>" // from the amber check

type Request struct { JSONRPC string; ID int64; Method string; Params any }
type Response struct { JSONRPC string; ID int64; Result json.RawMessage; Error *RPCError }
type Notification struct { JSONRPC string; Method string; Params json.RawMessage }
type RPCError struct { Code int; Message string; Data any }
func (e *RPCError) Error() string

// Typed ACP structures (verified shapes):
type InitializeParams struct { ProtocolVersion string; ClientCapabilities ... }
type InitializeResult struct { ProtocolVersion string; AgentCapabilities ...; AuthMethods ... }
// SessionNewParams/Result, SessionPromptParams/Result, SessionUpdate (union of
// agent_message_chunk/tool_call/request_permission/... variants) — per spec.

// internal/acp/transport.go
type Transport struct { /* rw pair + pending map + notify handlers */ }
func NewTransport(in io.Reader, out io.Writer) *Transport
func (t *Transport) Call(ctx context.Context, method string, params any, result any) error
func (t *Transport) Notify(method string, params any) error
func (t *Transport) OnNotification(fn func(Notification))  // register handler
func (t *Transport) Serve() // blocking read loop; call from goroutine
func (t *Transport) Close() error
```

### What This Leaf Consumes

Nothing outside stdlib + go.mod deps (check go.mod before adding ANY import; encoding/json suffices).

## Tasks

### Task 1: Failing tests — wire round-trip

**Files:** Create: internal/acp/protocol_test.go

Request marshals with `jsonrpc:"2.0"`, numeric id, method, params; Response/Notification round-trip; RPCError() message format.

### Task 2: Failing tests — transport call correlation

**Files:** Create: internal/acp/transport_test.go

Fake pipe pair (io.Pipe). Client calls `Call("initialize", ...)` in a goroutine; test server side writes a matching Response on the pipe; Call returns parsed result. Unknown-id response ignored without panic. Server-initiated Notification delivered to OnNotification handler. Concurrent Calls (3 goroutines) each get their own response. CallTimeout respected via ctx (use short ctx in test, not the config value — transport is config-agnostic).

### Task 3: Transport implementation

**Files:** Create: internal/acp/transport.go

json.Decoder over input (handles newline-delimited and Content-Length-free framing per amber verification). Pending map keyed by ID, mutex-guarded, collect-under-lock pattern (mutexio). Serve loop decodes into a generic envelope, dispatches response vs notification. Close: idempotent, unblocks pending with ctx-canceled errors.

### Task 4: Package docs

**Files:** Create: internal/acp/doc.go — one-paragraph package comment: ACP client-side types for meept; protocol version + framing facts recorded from the amber check with the date and source URL.

## Self-Verification Checklist

- [ ] Amber contract verified with web source; SHARED-CONVENTIONS.md §2 updated; report states verified values + URL
- [ ] go build ./internal/acp/ green
- [ ] go test ./internal/acp/ -race -count=1 green
- [ ] No goroutine leaks: transport tests end with Close() and no blocked goroutines (use goleak-style check or explicit verification in report)
- [ ] mutexio analyzer clean (`go run ./tools/analyzers/mutexio/... ./internal/acp/` or `make mutexio` scoped)
- [ ] No TODOs

**DO NOT COMMIT.**

**Deviations from spec:** [none / list with rationale]

## Review Checklist (For Review Agent)

- [ ] Types match the AMBER-verified spec (not the provisional master contract, if they differ)
- [ ] Call correlation correct under concurrency; no lock held across I/O
- [ ] Tests cover: round-trip, unknown-id, notifications, concurrent calls, timeout
- [ ] No unused code (U1000 gate)
- [ ] SHARED-CONVENTIONS.md updated

## Notes

- This package is pure stdlib — no meept imports except none. Keeps it testable in isolation.
- If ACP turns out to use LSP-style Content-Length framing, transport.go implements that framing INSTEAD of newline-delimited — decide from the spec, not from this plan's guess.
- Method-name constants (MethodInitialize etc.) belong in protocol.go; every later leaf references constants, never raw strings.
