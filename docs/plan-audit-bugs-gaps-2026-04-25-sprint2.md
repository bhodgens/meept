# Sprint 2: High Severity Fixes

**Source:** `docs/plan-audit-bugs-gaps-2026-04-25-remediation.md`

**Goal:** Implement all High severity fixes (21 issues).

---

## Issues to Fix (21 total)

### Agent System (9 issues)
- AGENT-8: `internal/agent/escalation.go` - OriginalTask set to error string
- AGENT-9: `internal/agent/dispatcher.go` - Keyword patterns duplicated
- AGENT-10: `internal/agent/dispatcher.go` - Semantic index goroutine leaks
- AGENT-11: `internal/agent/session_tracker.go` - StopBackgroundPersistence is no-op stub
- AGENT-12: `internal/agent/handler.go` - generateMessageID() collision risk
- AGENT-13: `internal/agent/tactical.go` - validationGateCounter map grows without bound
- AGENT-14: `internal/agent/tactical.go` - json.Unmarshal on potentially nil data
- AGENT-15: `internal/agent/cache.go` - ResultCache.Stop() no WaitGroup
- AGENT-16: `internal/agent/collaborative.go` - TOCTOU race in Revise

### Security (3 issues)
- SEC-6: `internal/security/engine.go` - Check() holds RLock during SQLite queries
- SEC-7: `internal/security/taint/patterns.go` - Custom log2 broken for values 0-1
- SEC-8: `internal/security/tls.go` - InsecureSkipVerify() without build-tag protection

### LLM (5 issues)
- LLM-7: `internal/llm/broker.go` - Random map iteration for provider selection
- LLM-8: `internal/llm/client.go` - Double metrics recording
- LLM-9: `internal/llm/anthropic.go` - Streaming latency records TTFB not total
- LLM-10: `internal/llm/credentials.go` - No mutex on concurrent access
- LLM-11: `internal/llm/token_cache_l2.go` - LIKE metacharacter injection

### Memory (4 issues)
- MEM-7: `internal/memory/graph.go` - AddEdge panics on short IDs
- MEM-8: `internal/memory/manager.go` - GetExpiredMemories zero-value time.Time
- MEM-9: `internal/memory/session/store_sqlite.go` - Attach/Detach silent no-op
- MEM-10: `internal/memory/vector/store.go` - No transaction on insert

### Tools (5 issues)
- TOOLS-8: `internal/tools/tool_cron_create.go` - parseInt() returns 0 without error
- TOOLS-9: `internal/comm/web/auth.go` - Timing attack on API key lookup
- TOOLS-10: `internal/tools/mcp/transport/http.go` - No response size limit
- TOOLS-11: `internal/tools/tools/registry.go` - ExecuteWithRetry nil dereference
- TOOLS-12: `internal/tools/builtin/tool_web_search.go` - Response body not size-limited

### CLI (3 issues)
- CLI-5: `internal/queue/worker/pool.go` - Pool.Scale re-appends removed worker IDs
- CLI-6: `internal/selfimprove/controller.go` - RunFullCycle modifies state without mutex
- CLI-7: `cmd/meept/daemon.go` - Log file descriptor leaked

---

## Implementation Order

1. Quick fixes first (single line changes)
2. Group by package for efficiency
3. Verify each fix compiles before moving to next

## Verification Criteria

Each fix must:
1. Compile without errors
2. Pass existing tests in the package
3. Include inline comment referencing issue ID
