# GLM-52 Deferred Items Implementation Plan

**Source:** `docs/plans/glm52-findings.md` - 20 deferred items requiring implementation.

**Priority Order:** As recommended in findings document:
1. PR 1 (Security): Already completed (F2, F3, F4)
2. PR 2 (Daemon Lifecycle): D5, D6, D17
3. PR 3 (LLM Context Integrity): D1, D8
4. PR 4 (LLM Failover): D2, D3, D4, D11
5. PR 5 (UX Polish): D12, D13

Additional deferred items from findings:
- D7: Budget asymmetry
- D9: Mutex held across LLM calls
- D10: Resolver cooldown gaps
- D14: Fence.go cleanup (verified as false positive - no fix needed)
- D15-D20: Low severity items

---

## Phase 1: Daemon Lifecycle Context Propagation (D5, D6, D17)

**Goal:** Fix daemon lifecycle to properly propagate cancellable context and ensure cleanup on all paths.

### D5. `internal/daemon/daemon.go:717-725` - shutdown() not called on StartAll failure
**Current:** Early returns bypass `d.shutdown()`, leaking RPC server listener, component goroutines, bus, and metricsStore.
**Fix:** Ensure `d.shutdown()` is called on all error paths. Make `shutdown()` idempotent.

### D6. `internal/daemon/daemon.go:738-744` - ContainerManager.StartAll uses context.Background()
**Current:** Background goroutine uses parent ctx (Background), unstoppable on reload. On SIGHUP reload, goroutine keeps running with old context.
**Fix:** Derive cancellable daemon-lifecycle context, add Wait synchronization.

### D17. `internal/daemon/components.go:1707-1721` - Components.Stop doesn't close ClassifierClient/SummarizerClient
**Current:** Leaks idle TCP connections on every restart.
**Fix:** Close client connections in Stop().

---

## Phase 2: LLM Context Integrity (D1, D8) - CRITICAL

### D1. `internal/llm/context_firewall.go:642-683` - dropOldContext breaks tool-call/tool-result pairing
**Current:** Hard limit keeps only `system + last 2 non-system messages`, orphaning tool results from their preceding assistant tool_call. OpenAI/Anthropic APIs reject with 400.
**Same pattern in:** `context_compressor.go keepTail()` (lines 485-520)
**Fix:** Rewrite truncation logic to walk backward and retain referenced assistant tool_call messages. Add tests for `[system, user, assistant(tool_calls), tool, tool, assistant]` patterns.

### D8. `internal/llm/context_firewall.go:588-607` - chunkMessage rewrites tool/assistant messages as RoleUser
**Current:** Corrupts conversation structure.
**Fix:** Preserve message roles during chunking.

---

## Phase 3: LLM Failover & Retry (D2, D3, D4, D11)

### D2. `internal/llm/broker.go:160-185` - ModelBroker has no failover on single-provider failure
**Current:** Selects first healthy entry and returns error verbatim if it fails. Unlike ProviderManager.Chat() which iterates providers.
**Fix:** Add rotation on runtime failure (5xx/rate-limit).

### D3. `internal/llm/anthropic.go:851-874` - streaming metrics dropped on mid-stream parse failure
**Current:** Success-metrics goroutine gated on `parseErr == nil && parsedResp != nil`. Mid-stream failure = no metrics recorded.
**Fix:** Implement "partial usage" metric type.

### D4. `internal/llm/client.go:856-1090` - ChatWithDeltaCallback has no retry on transient errors
**Current:** Single `httpClient.Do(req)` call, returns any non-200 directly.
**Fix:** Add retry logic for 429/502/503/504 with accumulator reset per attempt.

### D11. `internal/llm/anthropic.go:176-218` - Anthropic retry ignores Retry-After header
**Current:** RateLimitError (529 Overloaded specifically) has Retry-After header that is ignored.
**Fix:** Parse and respect Retry-After header in retry logic.

---

## Phase 4: Input Validation & CORS (D12, D13)

### D12. `internal/comm/http/api_handlers.go:2597-2601` (and 5 other handlers) - Unbounded ?limit= query parameters
**Current:** No bounds on limit parameter, memory exhaustion risk.
**Fix:** Use `parseIntParam(r, "limit", default, min, max)` helper consistently.

### D13. `internal/comm/http/server.go:905-930` - CORS preflight missing Vary: Origin header
**Current:** Missing `Vary: Origin` and `Access-Control-Allow-Credentials` in OPTIONS path.
**Fix:** Add proper CORS headers.

---

## Phase 5: Budget & Mutex Issues (D7, D9, D10)

### D7. `internal/llm/budget.go:168-178` - Asymmetric reset at UTC day boundary
**Current:** `hourlyCostWindow` truncated but `hourlyWindow` left intact.
**Fix:** Synchronize window resets.

### D9. `internal/llm/context_compactor.go:124-200` - Mutex held across LLM summarizer call
**Current:** Serializes all compactions system-wide (up to 30s × N waiters).
**Fix:** Copy-out-then-call pattern.

### D10. `internal/llm/resolver.go:223-235` - ResolveForAlias doesn't validate new model cooldown
**Current:** Fully-degraded alias silently serves known-bad model.
**Fix:** Validate replacement model is also out of cooldown.

---

## Phase 6: Low Severity Items (D15, D16, D18, D19, D20)

### D15. `internal/daemon/components.go:2051-2053` - Components.Start returns first error without stopping already-started handlers
**Fix:** Stop already-started handlers on error.

### D16. `internal/daemon/components.go:1878-1879` - ClusterConfig dereferenced without nil guard
**Fix:** Add nil guard.

### D18. `internal/rpc/server.go:318-369` - dispatch swallows handler errors into generic ErrCodeInternal
**Fix:** Return proper JSON-RPC 2.0 error codes (-32602 for malformed params).

### D19. `internal/comm/http/server.go:1708` - MCP POST handler uses 10 MB body limit
**Fix:** Document intentional difference or align with 1 MB default.

### D20. `ui/flutter_ui/lib/services/daemon_cert_pinner.dart:79-94` - TLS pinning fallback accepts any localhost cert
**Fix:** Remove fallback or strengthen validation.

---

## Test Plan

For each phase:
1. Read affected files to understand current implementation
2. Implement fixes
3. Add unit tests covering the specific bug scenarios
4. Run `go test ./...` to verify no regressions
5. Build and smoke test daemon

---

## Success Criteria

- All 20 deferred items addressed (fixed, documented as intentional, or closed as false positive)
- New tests added for each fix
- All existing tests pass
- Build succeeds with no new warnings
