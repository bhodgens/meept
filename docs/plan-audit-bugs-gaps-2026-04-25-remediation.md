# Meept — Audit Bugs & Gaps 2026-04-25 Remediation Plan

**Date:** 2026-04-25
**Source Audit:** `docs/audit-bugs-gaps-2026-04-25.md` (115 issues)
**Status:** In Progress

---

## Executive Summary

| Severity | Total | Fixed | Still Present | Partially Fixed | Design Limitation | Pending Verification | Fix Rate |
|----------|-------|-------|---------------|-----------------|-------------------|---------------------|----------|
| **Critical** | 24 | 15 | 8 | 1 | - | - | 62.5% |
| **High** | 42 | 2 | 19 | - | - | 21 | 4.8% |
| **Medium** | 32 | - | - | - | - | 32 | 0% |
| **Low** | 17 | - | - | - | - | 17 | 0% |
| **TOTAL** | **115** | **17** | **27** | **1** | **1** | **69** | **14.8%** |

**Note:** The fix rate appears low because 69 issues (High: 21, Medium: 32, Low: 17) remain pending verification. The actual fix rate for verified issues is 17/46 = 37%.

---

## Completion Status

**CURRENT STATUS: 35% verified (40/115 issues)**

### Verified Fixed (40 issues):

| Category | Fixed | Still Present | Partial | Design Limitation |
|----------|-------|---------------|---------|-------------------|
| Core Infrastructure | 3 | 1 | - | - |
| Agent System | 4 | 3 | - | 1 |
| Security | 2 | 3 | - | - |
| LLM Integration | 5 | 1 | - | - |
| Memory System | 5 | 1 | 1 | - |
| Tools & Communication | 2 | 5 | - | - |
| CLI/Metrics | 0 | 0 | - | - |

### Verified Still Present (27 issues):

| Issue ID | Category | Severity | File | Fix Required |
|----------|----------|----------|------|--------------|
| CORE-1 | Core | Critical | `internal/rpc/proxy.go:272` | Store and unsubscribe per-topic subscribers |
| SEC-1 | Security | Critical | `internal/security/engine.go:250` | Read `BlockFinancial` config field |
| SEC-2 | Security | Critical | `internal/security/tirith.go:17-49` | Remove package-level sync.Once |
| SEC-4 | Security | Critical | `internal/security/engine.go:434` | Add path separator suffix check |
| AGENT-2 | Agent | Critical | `internal/agent/workspace.go:340` | Check `output` not `tagName` |
| AGENT-5 | Agent | Critical | `internal/agent/loop.go:904-906` | Guard config writes with mutex |
| AGENT-6 | Agent | Critical | `internal/agent/loop.go:801-816` | Propagate registry to executor |
| LLM-3 | LLM | Critical | `internal/llm/client.go:171-178` | Per-request timeout copy |
| MEM-4 | Memory | Critical | `internal/memory/consolidation.go:488` | Add sync.Once or guard |
| MEM-6 | Memory | Critical | `internal/memory/episodic.go:261-266` | Pass connection as parameter |
| TOOLS-2 | Tools | Critical | `internal/code/ast/parser.go:52-80` | Per-parser mutex or pool |
| TOOLS-3 | Tools | Critical | `internal/tools/builtin/calendar.go:132-136` | Safe type assertions |
| TOOLS-4 | Tools | Critical | `internal/comm/web/websocket.go:61-80` | Async unregister pattern |
| TOOLS-5 | Tools | Critical | `internal/comm/telegram/bot.go:210-223` | Nil check on msg.From |
| TOOLS-6 | Tools | Critical | `internal/tools/mcp/transport/stdio.go:132-153` | Goroutine lifecycle management |
| TOOLS-7 | Tools | Critical | `internal/comm/http/server.go:529-541` | Implement WebSocket streaming |

### Design Limitations (1 issue):

| Issue ID | Description | Resolution |
|----------|-------------|------------|
| AGENT-7 | `NeedsConfirm` always returns error | Document as intentional; implement async confirmation as feature |

---

## Remediation Sprints

### Sprint 1: Critical Security & Concurrency (Week 1)

**Priority issues that block production deployment:**

| Issue | File | Estimated Effort |
|-------|------|------------------|
| SEC-1 | `internal/security/engine.go:250` | 1 hour |
| SEC-2 | `internal/security/tirith.go:17-49` | 2 hours |
| SEC-4 | `internal/security/engine.go:434` | 1 hour |
| CORE-1 | `internal/rpc/proxy.go:272` | 2 hours |
| AGENT-5 | `internal/agent/loop.go:904-906` | 1 hour |
| LLM-3 | `internal/llm/client.go:171-178` | 2 hours |
| MEM-4 | `internal/memory/consolidation.go:488` | 1 hour |
| TOOLS-4 | `internal/comm/web/websocket.go:61-80` | 2 hours |

**Total: ~12 hours**

---

### Sprint 2: Critical Agent & Tools (Week 2)

| Issue | File | Estimated Effort |
|-------|------|------------------|
| AGENT-2 | `internal/agent/workspace.go:340` | 1 hour |
| AGENT-6 | `internal/agent/loop.go:801-816` | 3 hours |
| TOOLS-2 | `internal/code/ast/parser.go:52-80` | 3 hours |
| TOOLS-3 | `internal/tools/builtin/calendar.go:132-136` | 1 hour |
| TOOLS-5 | `internal/comm/telegram/bot.go:210-223` | 1 hour |
| TOOLS-6 | `internal/tools/mcp/transport/stdio.go:132-153` | 3 hours |
| MEM-6 | `internal/memory/episodic.go:261-266` | 2 hours |

**Total: ~14 hours**

---

### Sprint 3: High Severity Verification & Fixes (Week 3)

**42 High severity issues - verification pending for 21:**

| Category | Issues | Status |
|----------|--------|--------|
| Agent System | AGENT-8 through AGENT-16 | 9 issues - pending verification |
| Security | SEC-6 through SEC-8 | 3 issues - pending verification |
| LLM | LLM-7 through LLM-11 | 5 issues - pending verification |
| Memory | MEM-7 through MEM-10 | 4 issues - pending verification |
| Tools | TOOLS-8 through TOOLS-12 | 5 issues - pending verification |
| CLI | CLI-5 through CLI-7 | 3 issues - pending verification |

**Total: ~40 hours (estimated 2-4 hours per issue)**

---

### Sprint 4: Medium & Low Severity (Week 4)

**32 Medium + 17 Low = 49 issues - verification pending:**

| Severity | Issues | Estimated Effort |
|----------|--------|------------------|
| Medium | 32 | ~80 hours |
| Low | 17 | ~34 hours |

**Total: ~114 hours**

---

## Implementation Notes

### Pattern 1: Fail-Closed Security (SEC-1)
**Fix:** Read `e.config.BlockFinancial` before blocking:
```go
if !e.config.BlockFinancial {
    return nil // Allow financial operations
}
```

### Pattern 2: Instance-Level State (SEC-2)
**Fix:** Use instance-level `sync.Once` instead of package-level:
```go
type TirithScanner struct {
    once sync.Once
    available bool
}
```

### Pattern 3: Path Containment (SEC-4)
**Fix:** Add trailing separator check:
```go
allowedPrefix := resolvedPath + string(filepath.Separator)
if !strings.HasPrefix(testPath, allowedPrefix) {
    return false // Path escapes allowed directory
}
```

### Pattern 4: Per-Request Timeout (LLM-3)
**Fix:** Clone http.Client or use per-request context timeout:
```go
reqCtx, cancel := context.WithTimeout(ctx, calculatedTimeout)
defer cancel()
httpReq := httpReq.WithContext(reqCtx)
```

### Pattern 5: Connection Passing (MEM-6)
**Fix:** Pass connection as parameter:
```go
func updateLastAccessed(ctx context.Context, db *sql.Conn, results []MemoryResult) error
```

---

## References

- Source Audit: `docs/audit-bugs-gaps-2026-04-25.md`
- Previous Remediation Plan: `docs/plan-bugs-gaps-remediation.md` (38/38 = 100% complete)
- Previous Audit: `docs/bugs-and-gaps.md`

---

## Appendix: Issue Tracking by Category

### Core Infrastructure (4 issues)
| ID | Status | Notes |
|----|--------|-------|
| CORE-1 | STILL_PRESENT | Per-topic bus subscriptions never unsubscribed |
| CORE-2 | FIXED | sync.Once prevents double-close |
| CORE-3 | FIXED | Uses proper ctx parameter |
| CORE-4 | FIXED | Collector captured and wired |

### Agent System (7 issues)
| ID | Status | Notes |
|----|--------|-------|
| AGENT-1 | FIXED | Lock held for entire acquireSlots |
| AGENT-2 | STILL_PRESENT | Checks tagName instead of output |
| AGENT-3 | FIXED | messageTypes synced in Truncate |
| AGENT-4 | FIXED | messageTypes and anchorMessages cloned |
| AGENT-5 | STILL_PRESENT | Config.MaxIterations written without lock |
| AGENT-6 | STILL_PRESENT | Registry swap not propagated to executor |
| AGENT-7 | DESIGN_LIMITATION | NeedsConfirm hard-blocks by design |

### Security (5 issues)
| ID | Status | Notes |
|----|--------|-------|
| SEC-1 | STILL_PRESENT | BlockFinancial config never read |
| SEC-2 | STILL_PRESENT | Package-level sync.Once |
| SEC-3 | FIXED | Substring bypass closed |
| SEC-4 | STILL_PRESENT | HasPrefix without separator |
| SEC-5 | FIXED | defer rows.Close() added |

### LLM Integration (6 issues)
| ID | Status | Notes |
|----|--------|-------|
| LLM-1 | FIXED | Tool results in separate user messages |
| LLM-2 | FIXED | Bounds check + role guard |
| LLM-3 | STILL_PRESENT | httpClient.Timeout mutated directly |
| LLM-4 | FIXED | Stats copies struct under RLock |
| LLM-5 | FIXED | Anthropic provider dispatch |
| LLM-6 | FIXED | Skills executor dispatch |

### Memory System (6 issues)
| ID | Status | Notes |
|----|--------|-------|
| MEM-1 | FIXED | Goroutine terminates correctly |
| MEM-2 | FIXED | sql.NullString for scan |
| MEM-3 | FIXED | ExecContext errors returned |
| MEM-4 | STILL_PRESENT | Double-close panic |
| MEM-5 | FIXED | Close calls StopPrefetchService |
| MEM-6 | PARTIALLY_FIXED | Second pool acquire remains |

### Tools & Communication (7 issues)
| ID | Status | Notes |
|----|--------|-------|
| TOOLS-1 | FIXED | Two-phase reload |
| TOOLS-2 | STILL_PRESENT | Shared parser concurrent use |
| TOOLS-3 | STILL_PRESENT | Bare type assertions |
| TOOLS-4 | STILL_PRESENT | RLock contention |
| TOOLS-5 | STILL_PRESENT | msg.From nil dereference |
| TOOLS-6 | STILL_PRESENT | Goroutine leak |
| TOOLS-7 | STILL_PRESENT | WebSocket stub |

### CLI/Metrics (4 issues)
| ID | Status | Notes |
|----|--------|-------|
| CLI-1 | PENDING | Deadlock in Record when batch full |
| CLI-2 | PENDING | SubscribeMetrics is stub |
| CLI-3 | PENDING | Placeholder values |
| CLI-4 | PENDING | API key prompt no read |
