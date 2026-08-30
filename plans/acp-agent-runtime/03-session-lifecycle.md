# ACP Session Lifecycle — Implementation Leaf

> **For the implementing agent:** Implement ALL tasks below using TDD.
> Do NOT commit. Do NOT use read_file on existing source files.

## Meta

- **Parent:** ../master.md
- **Scope:** internal/acp/session.go: spawn agent subprocess, ACP handshake, prompt send/cancel, subprocess death handling
- **Dependencies:** 02 (protocol + transport types)
- **Estimated Context:** 75K
- **Concurrency Group:** C

## Goal

One Session = one external agent subprocess speaking ACP over stdio. Start() launches the process (command+env+cwd from the catalog entry), runs the ACP initialize handshake over a Transport, and exposes Send/Cancel/Close. A fake agent process (test helper binary in the same package) makes every behavior testable without any real agent installed.

## Context

Key files to understand:
- internal/llm/runtime_manager.go — how meept already spawns/manages subprocesses (llama-server): patterns for process lifecycle, kill-with-escalation, restart gating. Read for convention, do not import.
- internal/acp/transport.go (from leaf 02) — the wire layer this leaf drives.

## Interface Contracts (From Parent)

### What This Leaf Exposes

```go
// internal/acp/session.go
type SessionConfig struct {
	AgentID     string
	Command     []string
	Env         map[string]string
	Cwd         string            // empty = inherit caller's dir at Start time
	DefaultMode string            // e.g. "read-only", "agent"
	DialTimeout time.Duration     // handshake budget
	CallTimeout time.Duration     // per-turn budget
}

type Session struct { /* proc, transport, state, cancel */ }

type SessionState int
const (StateStarting SessionState = iota; StateReady; StateBusy; StateClosed)

func Start(ctx context.Context, cfg SessionConfig) (*Session, error)
func (s *Session) State() SessionState
func (s *Session) Send(ctx context.Context, text string) (string, error) // returns agent's final chat text
func (s *Session) Cancel() error
func (s *Session) Close() error // idempotent; kills proc tree

// Notification for higher layers (04-manager subscribes):
type SessionEvent struct { Kind string; Text string } // kinds: "message_chunk","tool_call","permission_request","done","error","closed"
func (s *Session) Events() <-chan SessionEvent
```

### What This Leaf Consumes

From 02: Transport, Call, Notify, OnNotification, typed ACP structs, method-name constants.

## Tasks

### Task 1: Fake ACP agent test helper

**Files:** Create: internal/acp/testdata/fakeagent/main.go + internal/acp/fakeagent_test.go (helper wiring)

A tiny Go program: speaks ACP over its stdio — accepts initialize, replies with capabilities; on session/prompt emits 1-2 session/update notifications (message_chunk) then a final message. Flags: `-mode echo|slow|die|badhandshake` to exercise timeouts, mid-call process death, and malformed responses. Built via `go build` into a temp dir inside tests (TestMain), or invoked via `go run` — pick whichever the package's existing test style prefers and keep it hermetic.

### Task 2: Failing tests — handshake

**Files:** Create: internal/acp/session_test.go

Start with fakeagent echo mode -> State()==StateReady. Handshake timeout (slow mode with 100ms DialTimeout) -> error mentioning the agent id. Bad handshake -> error. Command not found -> wrapped error containing the command name.

### Task 3: Failing tests — send/cancel/close

Send echo round-trip returns the fake agent's final text. Cancel during slow turn returns promptly and session stays usable (or closed, per spec behavior — record which). Close kills the process (verify with cmd.ProcessState or pgrep-equivalent via os), second Close is a no-op. Process death mid-Send (die mode) -> error, State()==StateClosed, no hang.

### Task 4: Session implementation

**Files:** Create: internal/acp/session.go

Subprocess: exec.CommandContext with process-group kill (best-effort: Setpgid on unix + syscall.Kill(-pgid) — check how internal/llm/runtime_manager.go handles trees and match that convention instead of inventing). Events channel: buffered (size 32), notifications translated per the Kind table; when the channel is full, drop oldest and set an error event (never block the transport goroutine). Send: send session/prompt, collect message_chunks until final, return text. Mutex scope: state transitions under lock, wire I/O outside.

### Task 5: Package-level integration test

**Files:** Extend: internal/acp/session_test.go

Full loop with fakeagent: Start -> Send("hello") -> expect reply -> Cancel-safety probe -> Close. Assert no temp files left, no goroutine leak (repeat Close), Events drained.

## Self-Verification Checklist

- [ ] go build ./internal/acp/ green
- [ ] go test ./internal/acp/ -race -count=10 green (determinism: session tests run repeatedly)
- [ ] No real agent binaries referenced anywhere in tests (grep test files for "codex|opencode|gemini" -> zero)
- [ ] Process-tree kill works on macOS (this dev machine) — report how you verified
- [ ] predid clean (no time.Now-seeded IDs; session IDs come from pkg/id.Generate())
- [ ] No TODOs, no debug prints

**DO NOT COMMIT.**

**Deviations from spec:** [none / list with rationale]

## Review Checklist (For Review Agent)

- [ ] Contracts satisfied exactly (signatures, states, event kinds)
- [ ] Fake agent covers: echo, slow/timeout, die, bad handshake
- [ ] No lock held across exec/wait/io
- [ ] Close idempotent; no leaked processes after tests (check with `ps` during review)
- [ ] -race -count=10 green

## Notes

- Send's return contract: final assistant text only. Structured tool-call events flow through Events(), not the return value. 04-manager/05-tool build on this split.
- Do not implement permission-request ANSWERING logic in this leaf beyond emitting the event — see Task 5: the answering behavior (PermissionMode) is Task 5's job and lands here in session.go.

### Task 5: Permission-request handling (DECISION Q1)

**Files:** Modify: internal/acp/session.go; Test: internal/acp/session_test.go

The Session needs the permission mode at Start. Extend SessionConfig with `PermissionMode string` ("permissive"|"deny" — validated by config, trusted here). Behavior on an incoming `session/requestPermission` notification/request from the agent:

- "permissive" (default): auto-approve — reply with the ACP approve/outcome response per spec; emit a `"permission_request"` SessionEvent recording the request (id + kind) and the auto-approval, so events are auditable.
- "deny": auto-deny the same way; emit the same event recording denial.

Tests with fakeagent: add a `-mode permission` that issues one requestPermission mid-turn; assert permissive approves (turn completes) and deny denies (turn completes with agent-declined behavior per spec); assert the SessionEvent in both modes. Unknown modes never reach here (config validation); if SessionConfig receives one, treat as deny (defensive fail-closed).

Record the exact ACP permission response shape from the leaf-02 amber verification — do not guess the field names.
- ACP spec verification from leaf 02's amber check governs exact method names; this leaf consumes constants, never string literals.
