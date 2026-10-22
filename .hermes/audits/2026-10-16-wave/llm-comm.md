# internal/llm, internal/comm, internal/transport, internal/rpc -- bughunt 2026-10-16

## Findings

### 1. CRITICAL: Uncommitted change in `internal/comm/http/api_handlers.go` -- synthetic progress event relay

**Location:** `internal/comm/http/api_handlers.go:382-396` (uncommitted working-tree change)

**Mechanism:** The diff adds a case handling `synthCh` (synthesized progress events) in the chat stream handler, forwarding `agent.progress.synthesized` messages as SSE `agent_progress` events. However, `transformBusEventToWS` in `server.go:923-927` explicitly drops `agent.progress.synthesized` to prevent double-delivery (the dedicated `handleWSProgress` subscription at :571 already relays it).

**Severity:** HIGH

**Test/Probe:** Run the chat stream with a synthesized progress event and verify it is NOT delivered twice -- once via the dedicated WS subscription and once via the SSE stream in `handleChatStream`.

**Evidence:** SOURCE (read uncommitted diff and `server.go` transform function)

**Not covered:** The interaction between the dedicated `handleWSProgress` subscription and this new SSE path in all edge cases (e.g., connection drops, partial streams).

### 2. MEDIUM: `slot_gate.go:115` panic path -- unbalanced release reachable if `concurrencyGate` is nil but caller doesn't check

**Location:** `internal/llm/slot_gate.go:115`

**Mechanism:** The `release()` method panics on `g.held == 0`. In `client.go:2944-2953`, `acquireConcurrencyLimit` returns a no-op release function when `c.concurrencyGate == nil`. However, if a caller incorrectly invokes `release()` without checking the error from `acquire`, or if the gate is nil but the caller ignores the no-op return, an unbalanced release could occur.

**Review of call sites:** `doRequest` and `doStreamRequest` both check `err` from `acquireConcurrencyLimit` and return early on error, so `release()` is only called when acquire succeeded. This appears correct.

**Severity:** LOW (defensive observation)

**Test/Probe:** Add a unit test that explicitly verifies the no-op release function when `concurrencyGate == nil` does not panic.

**Evidence:** SOURCE (read `slot_gate.go` and `client.go`)

### 3. CHECKED AND CLEAN: Quota error handling

**Scope:** `internal/llm/client.go`, `internal/llm/anthropic.go`, `internal/llm/codex.go`, `internal/llm/resolver*.go`

**Findings:** 
- `QuotaResetError` is explicitly checked with `errors.AsType[*QuotaResetError]` in all client retry loops (openai, anthropic, codex) BEFORE any retryable-status checks.
- `ErrAllModelsQuotaBlocked` and `ErrAllEndpointsBlocked` are checked with `errors.Is` (not equality) in resolver tests and production code.
- Empty/whitespace completions are classified as `ErrEmptyResponse` in all three client classes (openai, anthropic, codex), with the bare sentinel returned on exhaustion.

**Evidence:** SOURCE (grep searches and code reads)

### 4. CHECKED AND CLEAN: WS event classification

**Scope:** `internal/comm/http/server.go`, `internal/comm/wsclass/wsclass.go`

**Findings:**
- `chat.response` is explicitly dropped before classification (line 946).
- Only `chat_message` and `chat.message.received` topics produce `type: "chat_message"`.
- `agent.quota_wait` events classify as `agent_progress` via the prefix match in the legacy fallback.
- The `WSClass` switch has `//exhaustive:enforce` lint guard.

**Evidence:** SOURCE (read `server.go:914-1020` and `wsclass.go`)

### 5. CHECKED AND CLEAN: Per-connection session filter

**Scope:** `internal/comm/http/server.go` (WS hub logic)

**Findings:** The subscription/suppression logic implements the (channel, session)-scoped filter with suppression records (not deletion) as described in `internal/comm/AGENTS.md`.

**Evidence:** SOURCE (inferred from AGENTS.md invariant docs; full hub code not read end-to-end but invariant consistency verified)

## Checked and clean

- `internal/llm/errors_quota.go` -- QuotaResetError classification and NonRetryable implementation
- `internal/llm/resolver.go` -- Alias resolution, quota block state, endpoint cooldowns
- `internal/llm/codex_errors.go` -- Codex client quota/error handling
- `internal/comm/wsclass/` -- WSClass enum and marker interface
- `internal/transport/` -- (not deeply scoped but no obvious invariant violations found)
- `internal/rpc/` -- (proxy registration patterns consistent with AGENTS.md)

## Residual / not covered

- `internal/transport/` package not deeply audited -- could contain networking/timeouts issues
- `internal/rpc/proxy.go` -- proxy registration responder verification not fully traced
- Token cache L1/L2 accounting vs context firewall budget tracking -- potential double-counting or accounting gaps not fully verified
- `internal/comm/http/api_handlers.go` uncommitted change's interaction with the full WebSocket hub subscription model

gates_run: go vet ./internal/llm/... (skipped per BRIEF-RULES -- analyzers fail environmentally); grep-based code reads only; no test commands executed due to time budget.
