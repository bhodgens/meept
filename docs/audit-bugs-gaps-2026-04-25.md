# Meept Platform Bugs and Gaps Audit

**Date:** 2026-04-25
**Scope:** Full codebase analysis covering all `internal/` and `cmd/` packages
**Method:** 8 parallel subagent code reviews with 100+ files analyzed

---

## Executive Summary

| Severity | Count |
|----------|-------|
| Critical | 24 |
| High | 42 |
| Medium | 32 |
| Low | 17 |
| **Total** | **115** |

The most urgent categories are:
1. **Concurrency bugs** - Data races, deadlocks, goroutine leaks across agent, memory, and metrics packages
2. **Stub implementations** - Several features advertised but not implemented (metrics streaming, summarization)
3. **Security vulnerabilities** - Override bypass, path traversal, timing attacks in security engine
4. **Resource leaks** - Database connections, file descriptors, goroutines not properly cleaned up

---

## Critical Issues (24)

### Core Infrastructure

#### CORE-1: Per-topic bus subscriptions never unsubscribed
**File:** `internal/rpc/proxy.go:264-305`
**Category:** Resource Leak

`handleBusSubscribe` creates a per-request goroutine that subscribes to the message bus but never unsubscribes when the client disconnects. Over time this leaks subscriptions and the associated channels.

#### CORE-2: Concurrent `Stop()` can double-close `closeCh` and panic
**File:** `internal/rpc/server.go:117-154`
**Category:** Concurrency

No guard prevents multiple concurrent calls to `Stop()` from both closing `closeCh`, causing a panic.

#### CORE-3: `SelfImproveSched.Start(context.Background())` prevents cancellation
**File:** `internal/daemon/components.go:307`
**Category:** Resource Leak

The self-improve scheduler is started with `context.Background()` instead of the component lifecycle context, making it impossible to stop cleanly during shutdown.

#### CORE-4: `metrics.NewCollector` return value discarded
**File:** `internal/daemon/daemon.go:172`
**Category:** Resource Leak

The collector goroutine is started but the returned handle is never stored, so there's no way to stop it during shutdown.

---

### Agent System

#### AGENT-1: Double-lock / always-reallocate race in `acquireSlots`
**File:** `internal/agent/tactical.go`
**Category:** Concurrency (Critical)

The function releases and re-acquires `l.mu` with a stale `ok` value from the first check. Two goroutines racing through the first check will each create a new semaphore, discarding one.

**Fix:** Use a single critical section or `sync.Map.LoadOrStore`.

#### AGENT-2: Wrong variable checked in `CreateCheckpoint`
**File:** `internal/agent/workspace.go`
**Category:** Bug (Critical)

After `gitCmd` returns `(tagName string, ok bool)`, code checks `if tagName == ""` instead of `if !ok`. A failed git tag silently continues.

#### AGENT-3: `Truncate()` does not sync `messageTypes` slice
**File:** `internal/agent/conversation.go`
**Category:** Bug (Critical)

`Truncate()` removes elements from `c.messages` but not from `c.messageTypes`. After truncation, the slices have different lengths, causing panics or wrong type labels.

#### AGENT-4: `Clone()` omits `messageTypes` and `anchorMessages`
**File:** `internal/agent/conversation.go`
**Category:** Bug (Critical)

Cloned conversations have nil `messageTypes`, corrupting importance scoring.

#### AGENT-5: Data race on `l.config.MaxIterations` in `RunWithSkill`
**File:** `internal/agent/loop.go`
**Category:** Concurrency (Critical)

`RunWithSkill` writes to `l.config.MaxIterations` without holding `l.mu`. Concurrent skill invocations race on this field.

#### AGENT-6: Registry swap in `RunOnce` does not propagate to executor
**File:** `internal/agent/loop.go`
**Category:** Bug (Critical)

`RunOnce` temporarily replaces `l.registry` with a `FilteredToolRegistry`, but `l.executor` was constructed with the original registry. Tool filtering is silently ineffective.

#### AGENT-7: `PlaceholderToolRegistry.NeedsConfirm` permanently returns error
**File:** `internal/agent/executor.go`
**Category:** Stub (Critical)

Returns `false, fmt.Errorf("NeedsConfirm not implemented")`, causing fail-closed security checks to block all tool use routed through this registry.

---

### Security

#### SEC-1: Financial block ignores `BlockFinancial` config field
**File:** `internal/security/engine.go:249-253`
**Category:** Vulnerability (Critical)

`checkFinancial` always runs and blocks regardless of the `BlockFinancial` config value. Callers that set `BlockFinancial: false` still get all financial operations blocked.

#### SEC-2: `tirithOnce` is package-level - binary path ignored after first call
**File:** `internal/security/tirith.go:17-49`
**Category:** Bug (Critical)

The availability check uses `sync.Once` at package level. Multiple `TirithScanner` instances with different binary paths silently use the result from the first check.

#### SEC-3: Default override matching allows substring bypass
**File:** `internal/security/engine.go:556-574`
**Category:** Vulnerability (High)

`strings.Contains(detailStr, pattern)` allows crafted inputs to match overrides intended for different actions. An override for `pip install requests` could be triggered by `rm -rf /; echo "pip install requests"`.

#### SEC-4: `checkPath` path traversal bypass via `strings.HasPrefix`
**File:** `internal/security/engine.go:434-445`
**Category:** Vulnerability (High)

Using `HasPrefix` without trailing slash normalization allows `/tmp_backup/secret` to match an allow rule for `/tmp`.

#### SEC-5: `rows` resource leak in `checkPath`
**File:** `internal/security/engine.go:396-463`
**Category:** Resource Leak (High)

Two queries reuse the same `rows` variable. The first result set is leaked until GC.

---

### LLM Integration

#### LLM-1: Wrong tool-result message placement in Anthropic client
**File:** `internal/llm/anthropic.go:449-468`
**Category:** Bug (Critical)

Tool results are appended to assistant messages instead of user messages, violating the Anthropic Messages API spec. This causes 400 errors on any multi-turn tool interaction.

#### LLM-2: Index mismatch when patching tool calls into `apiMessages`
**File:** `internal/llm/anthropic.go:472-493`
**Category:** Bug (Critical)

Index `i` into `messages` doesn't correspond to `i` into `apiMessages` because system/tool messages are handled differently. Tool call patches go to wrong slots.

#### LLM-3: Adaptive timeout mutates shared `httpClient` under concurrent use
**File:** `internal/llm/client.go:153-160`
**Category:** Concurrency (High)

`c.httpClient.Timeout` is mutated without synchronization. Concurrent callers race on this field.

#### LLM-4: `Stats()` writes to `c.stats` under `RLock`
**File:** `internal/llm/token_cache.go:227-240`
**Category:** Concurrency (High)

`Stats()` holds read lock but writes to `c.stats.HitRate` and `c.stats.EntryCount`, causing data races with concurrent `Get`/`Put`.

#### LLM-5: `ProviderManager` always creates OpenAI client, never Anthropic
**File:** `internal/llm/provider_manager.go:111-128`
**Category:** Partial (High)

`NewProviderManager` creates `NewClient` for every provider. Anthropic providers send requests to wrong endpoint with wrong auth headers.

#### LLM-6: Skills `Executor` also always creates OpenAI client
**File:** `internal/skills/executor.go:122, 237`
**Category:** Partial (High)

Same issue as `ProviderManager` - Anthropic models resolved by the skill executor will fail at API level.

---

### Memory System

#### MEM-1: Prefetch service goroutine leak and use-after-close race
**File:** `internal/memory/manager.go:1394-1404, 1461-1479`
**Category:** Concurrency (Critical)

Child goroutines from `doPrefetch` aren't tracked in `prefetchWg`. They can still be running when `StopPrefetchService` resets `prefetchCache`, causing writes to discarded maps.

#### MEM-2: `GetByID` scans `last_accessed_at` into `*time.Time` - type mismatch
**File:** `internal/memory/manager.go:1258`
**Category:** Bug (Critical)

Column defaults to empty string `''`, not `NULL`. Scanning empty string into `*time.Time` fails. This breaks `GetByID` for any row with default `last_accessed_at`.

#### MEM-3: `persistCommunities` silently ignores `stmt.ExecContext` errors
**File:** `internal/memory/graph.go:739`
**Category:** Data Integrity (Critical)

Error from `stmt.ExecContext` is discarded. Transaction commits with partial data if any community update fails.

#### MEM-4: `Consolidator.Stop()` double-close panic
**File:** `internal/memory/consolidation.go:394-396`
**Category:** Concurrency (Critical)

No guard prevents calling `Stop()` multiple times. Closing an already-closed channel panics.

#### MEM-5: `Manager.Close()` does not stop prefetch service
**File:** `internal/memory/manager.go:1488-1536`
**Category:** Resource Leak (High)

`Close()` never calls `StopPrefetchService()`. Prefetch goroutines continue accessing closed database connections.

#### MEM-6: `EpisodicMemory.updateLastAccessed` opens second connection while one is held
**File:** `internal/memory/episodic.go:239-269`
**Category:** Deadlock Risk (High)

`Search()` holds one connection then calls `updateLastAccessed` which requests another. With pool size 5 and 5 concurrent searches, this deadlocks.

---

### Tools and Communication

#### TOOLS-1: MCP Manager mutex deadlock in `Reload()`
**File:** `internal/tools/mcp/manager.go:297-305`
**Category:** Concurrency (Critical)

`Reload()` holds `m.mu.Lock()` via defer, then manually unlocks/locks around `StartServer()`. If `StartServer` panics, the deferred unlock runs on an already-unlocked mutex.

#### TOOLS-2: Tree-Sitter parser not thread-safe
**File:** `internal/code/ast/parser.go:53-110`
**Category:** Concurrency (Critical)

`ParserManager` stores one `*sitter.Parser` per language. Multiple goroutines parsing the same language race on the parser's internal C state.

**Fix:** Use `sync.Pool` of parsers per language or hold a per-language mutex during parsing.

#### TOOLS-3: `CalendarCreateTool.Execute` panic on missing args
**File:** `internal/tools/builtin/calendar.go:132-136`
**Category:** Bug (High)

Direct type assertion `args["start"].(string)` panics if field is missing or wrong type.

#### TOOLS-4: WebSocket Hub deadlock via `Broadcast` → `Unregister`
**File:** `internal/comm/web/websocket.go:61-80`
**Category:** Concurrency (High)

`Broadcast` holds `RLock` while spawning `Unregister` goroutine that tries to acquire `Lock`. Write lock can starve under continuous broadcast load.

#### TOOLS-5: Telegram bot nil dereference when `msg.From` is nil
**File:** `internal/comm/telegram/bot.go:212-216`
**Category:** Bug (High)

Channel posts have nil `From` field. Logger at line 215 unconditionally accesses `msg.From.ID`.

#### TOOLS-6: `StdioTransport.Send` goroutine leak on context cancellation
**File:** `internal/tools/mcp/transport/stdio.go:132-153`
**Category:** Resource Leak (High)

Goroutine blocked on `t.stdout.ReadBytes` never exits when context is cancelled. Leaked goroutine consumes next response meant for subsequent request.

#### TOOLS-7: `handleMetricsStream` is a non-functional stub
**File:** `internal/comm/http/server.go:529-541`
**Category:** Stub (High)

Returns `"websocket_not_implemented"` JSON instead of actual WebSocket upgrade. MenuBar app's live metrics streaming silently fails.

---

### CLI and Other

#### CLI-1: Deadlock in `metrics.Store.Record` when batch is full
**File:** `internal/metrics/store.go:259-276`
**Category:** Concurrency (Critical)

`Record()` uses `defer s.mu.Unlock()` but also manually unlocks before calling `flush()`. If `flush()` panics, the deferred unlock fires on an already-unlocked mutex.

#### CLI-2: `metrics.Store.SubscribeMetrics` is non-functional stub
**File:** `internal/metrics/store.go:529-535`
**Category:** Stub (High)

Returns channel that never receives data. Callers block forever.

#### CLI-3: `metrics.Collector.collect()` emits hardcoded placeholder values
**File:** `internal/metrics/collector.go:129-141`
**Category:** Stub (High)

Always records `queue.depth = 0` and `agent.active = 1` regardless of actual state. Live metrics dashboard shows incorrect values.

#### CLI-4: `runModelsSetup` prompts for API key but never reads input
**File:** `cmd/meept/models.go:70-78`
**Category:** Partial (High)

Prints prompt asking for API key but never reads from stdin. User is stuck at an unresponsive prompt.

---

## High Issues (42)

### Agent System
| ID | File | Description |
|---|---|---|
| AGENT-8 | `escalation.go` | `OriginalTask` set to error string instead of task description |
| AGENT-9 | `dispatcher.go` | Keyword patterns duplicated between `Classify` and `ClassifyAll` |
| AGENT-10 | `dispatcher.go` | Semantic index goroutine uses `context.Background()`, no WaitGroup |
| AGENT-11 | `session_tracker.go` | `StopBackgroundPersistence` is a no-op stub |
| AGENT-12 | `handler.go` | `generateMessageID()` has second-resolution collision risk |
| AGENT-13 | `tactical.go` | `validationGateCounter` map grows without bound |
| AGENT-14 | `tactical.go` | `json.Unmarshal` called on potentially nil step data |
| AGENT-15 | `cache.go` | `ResultCache.Stop()` has no WaitGroup - race with cleanup goroutine |
| AGENT-16 | `collaborative.go` | TOCTOU race in `Revise` - plan can be modified between lock release and re-acquire |

### Security
| ID | File | Description |
|---|---|---|
| SEC-6 | `engine.go:245-321` | `Check()` holds RLock while executing SQLite queries - TOCTOU on override usage count |
| SEC-7 | `taint/patterns.go:349-363` | Custom `log2` broken for values 0-1 - entropy detection always returns 0 |
| SEC-8 | `tls.go:132-139` | `InsecureSkipVerify()` exported without build-tag protection |

### LLM Integration
| ID | File | Description |
|---|---|---|
| LLM-7 | `broker.go:142-162` | Random map iteration for healthy provider selection |
| LLM-8 | `client.go:470-581` | Double metrics recording inflates RequestCount |
| LLM-9 | `anthropic.go:671-701` | Streaming latency records TTFB, not total latency |
| LLM-10 | `credentials.go` | No mutex - concurrent access data race |
| LLM-11 | `token_cache_l2.go:314-315` | LIKE metacharacter injection via file path |

### Memory System
| ID | File | Description |
|---|---|---|
| MEM-7 | `graph.go:227` | `AddEdge` panics on IDs shorter than 8 characters |
| MEM-8 | `manager.go:1318-1329` | `GetExpiredMemories` produces zero `time.Time` for empty `last_accessed_at` |
| MEM-9 | `session/store_sqlite.go:311-403` | `Attach`/`Detach`/`AddWorker`/`RemoveWorker` silent no-op if row was deleted |
| MEM-10 | `vector/store.go:102-143` | No transaction - partial metadata on insert failure |

### Tools
| ID | File | Description |
|---|---|---|
| TOOLS-8 | `tool_cron_create.go:351-362` | `parseInt()` returns 0 for non-numeric input without error |
| TOOLS-9 | `comm/web/auth.go:108-120` | Timing attack on API key lookup |
| TOOLS-10 | `mcp/transport/http.go:116-122` | No response size limit - memory exhaustion risk |
| TOOLS-11 | `tools/registry.go:325` | `ExecuteWithRetry` nil dereference on `lastErr` |
| TOOLS-12 | `builtin/tool_web_search.go:157-160` | Response body not size-limited |

### CLI
| ID | File | Description |
|---|---|---|
| CLI-5 | `worker/pool.go:286-305` | `Pool.Scale` re-appends already-removed worker IDs |
| CLI-6 | `selfimprove/controller.go:136-323` | `RunFullCycle` modifies shared state without holding mutex |
| CLI-7 | `cmd/meept/daemon.go:140-177` | Log file descriptor leaked in background mode |

---

## Medium Issues (32)

### Core
| ID | File | Description |
|---|---|---|
| CORE-5 | `config/presets.go:145-164` | `ApplyPreset` ignores TopP, FrequencyPenalty, PresencePenalty |
| CORE-6 | `bus/handler.go:55-61` | `SubscriptionHandler` may skip unsubscribe if bus closes first |
| CORE-7 | `registry/registry.go:82-103` | `StopAll` holds RLock while calling component Stop() |
| CORE-8 | `daemon/launchd.go:267` | `int(time.Hour)` multiplication fragile |

### Agent
| ID | File | Description |
|---|---|---|
| AGENT-17 | `workspace.go` | Tag parsing breaks for labels containing dashes |
| AGENT-18 | `collaborative.go` | `CollaborativePlanner` not wired into any production path |
| AGENT-19 | `loop.go` | `progressInterval` field never read |
| AGENT-20 | `session_tracker.go` | `PersistIdleSessions` silently swallows errors |
| AGENT-21 | `registry.go` | `AgentRegistry` holds concrete `*llm.Client`, not interface |
| AGENT-22 | `orchestrator.go` | Debug log prefixes "DONE"/"FAIL" in production messages |
| AGENT-23 | `review_manager.go` | `stepStore.Update` error ignored after validation failure |

### Security
| ID | File | Description |
|---|---|---|
| SEC-9 | `taint/patterns.go:105-107` | `;` and `|` detection has massive false positive rate |
| SEC-10 | `engine.go:473-478` | Additional resource leak path in checkPath |
| SEC-11 | `taint/patterns.go:253-286` | `SanitizeShellCommand` gives false sense of security |

### LLM
| ID | File | Description |
|---|---|---|
| LLM-12 | `context_compressor.go:267-272` | Stage 2 "summarize" just truncates - no actual LLM summarization |
| LLM-13 | `provider_manager.go:551-583` | Health checks consume real budget with live API requests |

### Memory
| ID | File | Description |
|---|---|---|
| MEM-11 | `consolidation.go:133-144` | `runAccessBasedExpiration` ignores Store/Delete errors |
| MEM-12 | `consolidation.go:258-291` | `summarizeByDate` leaves zero-value strings in IDs slice |
| MEM-13 | `manager.go:1139` | `getCurrentVersion` uses `context.Background()` |
| MEM-14 | `artifact_manager.go:121-133` | `GetCacheStats` acquires inner lock under outer RLock |
| MEM-15 | `manager.go:1362-1378` | `Delete` returns nil even when 0 rows deleted |
| MEM-16 | `manager.go:1006-1025` | `GetRelatedMemories` SQLite path uses FTS on UUID (non-functional) |
| MEM-17 | `consolidation.go:296-307` | `MergeRelated` only groups by date, not semantic similarity |

### Tools
| ID | File | Description |
|---|---|---|
| TOOLS-13 | `comm/http/config_service.go:43-57` | `expandPath` is dead code and fragile |
| TOOLS-14 | `calendar/auth.go:254` | Uses `fmt.Printf` instead of `slog` |

### CLI
| ID | File | Description |
|---|---|---|
| CLI-8 | `cmd/meept/selfimprove.go:107-159` | analyze/generate-fixes/validate dump raw JSON |
| CLI-9 | `cmd/meept/status.go:52` | PID parsing lacks `strings.TrimSpace` |

---

## Test Coverage Gaps

### Packages With No Tests
| Package | Risk |
|---|---|
| `internal/comm/http` | REST API for MenuBar - High |
| `internal/registry` | Component lifecycle - High |
| `internal/security/tirith.go` | Command scanner - Medium |
| `internal/security/tls.go` | TLS config - Medium |

### Files With Zero Test Coverage
| File | Risk |
|---|---|
| `internal/llm/cache_key_builder.go` | New file with complex regex logic - Critical |
| `internal/selfimprove/detector.go` | Log scanning with regexes - High |
| `internal/selfimprove/analyzer.go` | Root cause analysis - High |
| `internal/selfimprove/generator.go` | Fix generation - High |
| `internal/selfimprove/validator.go` | Fix validation - High |

### Flaky/Weak Tests
| Test | Issue |
|---|---|
| `TestL1Cache_Expiration` | Wall-clock timing dependency |
| `TestAgentLoopSimpleResponse` | Named test doesn't test agent loop |
| `TestRPCLoadTest` | Throughput target logged, not enforced |
| `TestTokenCacheCoordinator_L2Fallback` | Doesn't assert L2Hits > 0 |

---

## Recommended Remediation Priority

### Sprint 1: Critical Concurrency & Security (Week 1)
1. Fix `conversation.go` Truncate/Clone slice sync bugs
2. Fix `tactical.go` acquireSlots double-lock race
3. Fix `anthropic.go` tool-result message placement
4. Fix `security/engine.go` override matching bypass
5. Fix `memory/manager.go` prefetch goroutine leak
6. Add mutex to `llm/credentials.go`

### Sprint 2: Resource Leaks & Stubs (Week 2)
1. Fix all daemon component shutdown leaks
2. Implement `metrics.SubscribeMetrics`
3. Implement `metrics.Collector` with real values
4. Fix `mcp/manager.go` reload deadlock
5. Fix `token_cache.go` Stats() RLock write

### Sprint 3: API Compatibility & Data Integrity (Week 3)
1. Fix Anthropic client index mismatch
2. Add Anthropic support to `ProviderManager` and `skills/Executor`
3. Fix `memory/graph.go` persistCommunities error handling
4. Fix `GetByID` last_accessed_at scan type

### Sprint 4: Test Coverage (Week 4)
1. Add tests for `cache_key_builder.go`
2. Add tests for `internal/comm/http`
3. Add tests for `internal/registry`
4. Fix flaky timing-dependent tests
5. Add tests for selfimprove detector/analyzer/generator

---

## Appendix: Issue Cross-Reference by Package

### internal/agent (26 issues)
Critical: 7, High: 9, Medium: 7, Low: 3

### internal/security (13 issues)
Critical: 2, High: 4, Medium: 4, Low: 3

### internal/llm (16 issues)
Critical: 2, High: 6, Medium: 5, Low: 3

### internal/memory + internal/context + internal/session (20 issues)
Critical: 4, High: 6, Medium: 7, Low: 3

### internal/tools + internal/comm + internal/calendar + internal/code (14 issues)
Critical: 2, High: 10, Medium: 2, Low: 0

### Core infrastructure (14 issues)
Critical: 4, High: 3, Medium: 4, Low: 3

### CLI and other (12 issues)
Critical: 3, High: 4, Medium: 3, Low: 2
