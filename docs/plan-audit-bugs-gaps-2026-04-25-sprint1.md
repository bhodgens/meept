# Sprint 1: Critical Security & Concurrency Fixes

**Source:** `docs/plan-audit-bugs-gaps-2026-04-25-remediation.md`

**Goal:** Implement all Critical severity fixes that block production deployment.

---

## Issues to Fix (8 total)

### SEC-1: Financial block ignores BlockFinancial config
- **File:** `internal/security/engine.go:250`
- **Fix:** Read `e.config.BlockFinancial` before blocking
- **Effort:** 1 hour

### SEC-2: tirithOnce package-level singleton
- **File:** `internal/security/tirith.go:17-49`
- **Fix:** Use instance-level sync.Once instead of package-level
- **Effort:** 2 hours

### SEC-4: Path traversal via HasPrefix
- **File:** `internal/security/engine.go:434, 476`
- **Fix:** Add trailing separator suffix check
- **Effort:** 1 hour

### CORE-1: Per-topic bus subscriptions leaked
- **File:** `internal/rpc/proxy.go:272`
- **Fix:** Store and unsubscribe per-topic subscribers on disconnect
- **Effort:** 2 hours

### AGENT-5: Data race on config.MaxIterations
- **File:** `internal/agent/loop.go:904-906`
- **Fix:** Guard config writes with mutex
- **Effort:** 1 hour

### LLM-3: httpClient.Timeout race condition
- **File:** `internal/llm/client.go:171-178`, `anthropic.go:143-155`
- **Fix:** Per-request timeout via context or client clone
- **Effort:** 2 hours

### MEM-4: Consolidator.Stop() double-close panic
- **File:** `internal/memory/consolidation.go:488`
- **Fix:** Add sync.Once or select with default guard
- **Effort:** 1 hour

### TOOLS-4: WebSocket RLock contention
- **File:** `internal/comm/web/websocket.go:61-80`
- **Fix:** Async unregister pattern (already using go, but needs RLock release before blocking)
- **Effort:** 2 hours

---

## Implementation Order

1. **SEC-4** (quickest, isolated)
2. **SEC-1** (isolated config read)
3. **MEM-4** (simple guard)
4. **SEC-2** (instance-level state)
5. **AGENT-5** (mutex guard)
6. **LLM-3** (timeout pattern)
7. **CORE-1** (subscription management)
8. **TOOLS-4** (locking pattern)

---

## Verification Criteria

Each fix must:
1. Compile without errors
2. Pass existing tests in the package
3. Not introduce new race conditions (go test -race)
4. Include inline comment referencing the issue ID (e.g., `// SEC-1 FIX:`)

---

## Final Verification

After all fixes:
```bash
go build ./...
go test ./... -race -v
```
