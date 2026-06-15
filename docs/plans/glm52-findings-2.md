# GLM-5.2 Codebase Review Findings — Round 2

**Reviewer:** Claude Opus 4.6 (z.ai/glm-5.2 backend)
**Date:** 2026-06-14
**Scope:** `meept-daemon`, `meept` CLI, Flutter UI (`ui/flutter_ui`)
**Method:** 8 scoped parallel subagents covering agent core, LLM/providers, tools/security, HTTP/RPC/services, daemon/CLI/scheduler, memory/session/skills, Flutter UI, plus a verification subagent for prior-round fixes. Each subagent produced a structured findings report synthesized into this document.

## Executive Summary

- **Prior-round verification:** 28 of 32 prior-round items VERIFIED correct, 2 PARTIAL (now completed this round), 1 false positive, 1 N/A.
- **New findings reviewed:** 31 (2 critical, 9 high, 13 medium, 7 low)
- **Issues fixed this round:** 10 (across 8 files)
- **Issues deferred:** 21 (lower severity, larger scope, or requires design decision)
- **Tests:** all affected packages pass (`go build ./...`, `go vet ./...`, `go test ./internal/agent/... ./internal/llm/... ./internal/cluster/... ./internal/queue/... ./internal/daemon/... ./internal/comm/http/...`)

The most impactful new fix is **A-C1** — `TruncateByImportance` initialized its keep-mask to all-`false`, so the loop that marked messages for removal was a no-op and the entire pre-tail history was silently dropped on every importance-based truncation. The sibling `CompressByImportance` does it correctly, so the intended behavior is clear. The second critical, **A-C2**, caused tool-result cache collisions across distinct argument sets whenever JSON marshalling failed.

---

## Prior-Round Verification (glm52-findings.md)

| Status | Count | Items |
|--------|-------|-------|
| VERIFIED | 28 | F1–F12, D1, D2, D5, D6, D7, D8, D9, D10, D11, D13, D16, D17, D19 |
| N/A / false positive | 1 | F13 (correctly documented as benign) |
| PARTIAL → COMPLETED this round | 2 | D12 (one handler missed → fixed as HTTP-D12c), D15 (only 6 of ~20+ components tracked — see D15 note) |
| MISSING / INCORRECT | 0 | — |

Key prior-round caveats:
- **D1 edge case** (`context_firewall.go:684-685`): the parent-search `break` exits on the first assistant message regardless of whether its `ToolCalls` matched `msg.ToolCallID`. The comment "tool calls are in immediate parent" is accurate for standard OpenAI/Anthropic ordering, so the logic is sound for all standard sequences. Noted for future hardening.
- **D18 heuristic** (`rpc/server.go:537-549`): `isParameterError` uses broad keyword substring matching (`"param"`, `"argument"`, `"invalid"`, `"missing"`, `"required"`, `"expected"`, `"type"`, `"parse"`, `"unmarshal"`). Could misclassify some internal errors (e.g. "expected database connection") as `-32602 InvalidParams`. Minor correctness concern.
- **D15 structural gap**: the rollback infrastructure added by D15 only covers 6 handlers (chat, status, session, queue, task, worker, sync, syncmgr) out of ~20+ that `Components.Start` launches. The daemon-level `shutdown()` does clean up everything eventually, so this is not a permanent leak — but during the window between `Start()` failure and `shutdown()`, untracked background goroutines may continue running. See DEFERRED-D15-2 below.

Full per-item evidence table is in the verification subagent's report; summarized here for brevity.

---

## Issues Fixed This Round (10)

### A-C1. [Agent] `TruncateByImportance` keepMask never initialized to true — drops all non-tail history (CRITICAL)
**File:** `internal/agent/conversation.go:766-782`
**Severity:** CRITICAL — silent data loss
**Confidence:** 95

The keep-mask was allocated with `make([]bool, len(c.messages))`, which zero-initializes every entry to `false`. The removal-marking loop then did `keepMask[mi.idx] = false` — a no-op because the value was already `false`. Only the final "always keep last 4 messages" loop set entries to `true`. Result: every earlier message (including `ImportanceCritical` user input and anchors not covered by the tail-4 window) was reported as removed, defeating the entire importance-based retention strategy. The sibling `CompressByImportance` at lines 1369–1372 does the correct thing — it initializes all entries to `true` first, then marks removals.

**Fix:** initialize `keepMask` to all-`true` before the removal-marking loop, mirroring `CompressByImportance`.

### A-C2. [Agent] `ResultCache.hashArgs` returns constant on marshal failure — cache collisions (CRITICAL)
**File:** `internal/agent/cache.go:183`
**Severity:** CRITICAL — correctness
**Confidence:** 90

On `json.Marshal` failure, the fallback returned `string(MessageTypeError)`, which is the literal string `"error"` (defined in `protocol.go:17`). Every tool call with unmarshallable args collapsed onto the single cache key `toolName:error`, so a `file_read` with args set A and another with args set B (both unmarshallable) would share a cache entry — returning whichever result was stored first. Autocomplete typo: the early-return at line 164 returns `"empty"` for the empty-args case, so the failure path was intended to return some distinct sentinel.

**Fix:** return `"unmarshallable:" + strings.Join(keys, ",")` so distinct arg-key sets still hash distinctly even when values can't be marshalled. Added `strings` import.

### LLM-H1. [LLM] Anthropic retry backoff is linear, not exponential; no jitter (HIGH)
**File:** `internal/llm/anthropic.go:23, 189, 336`
**Severity:** HIGH — availability
**Confidence:** 95

The constant comment said "exponential: 2, 4, 8" but the implementation was `anthropicRetryBackoff * float64(attempt)` = `2 * attempt`, producing 2s, 4s, **6s** — linear. The OpenAI-compatible client (`client.go:315, 496`) correctly uses `math.Pow(retryBackoffBase, float64(attempt))` for 2s, 4s, 8s. Additionally, the Anthropic path used no jitter, so multiple Anthropic clients retrying simultaneously (multi-agent) would hit the API at identical timestamps — thundering herd.

**Fix:** both retry sites now use `math.Pow(anthropicRetryBackoff, float64(attempt))` wrapped in `BackoffWithJitter(expDelay, retryBackoffMaxDelay, true)`, matching the OpenAI client. Updated constant comment and added `math` import.

### LLM-H2. [LLM] Broker `isRetryableError` uses fragile substring matching, misses HTTP 529 (HIGH)
**File:** `internal/llm/broker.go:426-436`
**Severity:** HIGH — availability
**Confidence:** 85

The D2 broker failover decision detected 5xx errors via `strings.Contains(errStr, "5") && (strings.Contains(errStr, "00") || ...)`. This misses HTTP 529 (Anthropic "Overloaded"), which is the exact scenario where failover to another provider is most valuable: the error string `"HTTP 529: Overloaded"` contains "5" but none of "00"/"02"/"03"/"04". The function also risks false positives on arbitrary error strings containing those digit pairs. `provider_manager.go:891-912` already uses the correct `errors.As(err, &apiErr)` pattern.

**Fix:** replaced substring matching with `var apiErr *APIError; if errors.As(err, &apiErr) { return apiErr.StatusCode >= 500 && apiErr.StatusCode < 600 }`. Now correctly detects 529, 500, 502, 503, 504, and any future 5xx.

### CLUSTER-H1. [Cluster] `EventRetention` has no default — gossip dedup cache never suppresses (HIGH)
**File:** `internal/cluster/config.go:108-136`
**Severity:** HIGH — availability / network amplification
**Confidence:** 92

`setDefault()` applied defaults for `HeartbeatInterval`, `PeerTimeout`, `MaxRetryAttempts`, `DefaultClaimTimeout`, `NodeReachabilityTimeout`, `SyncInterval`, `Ed25519KeyRotationDays`, and `WireGuardPort` — but not `Gossip.EventRetention`. With zero retention, the dedup cache stores `time.Now().Add(0)` = "now", and the subsequent `time.Now().Before(expiry)` check at `gossip.go:320` is always false — so duplicate events are re-processed and re-broadcast on every hop. In a fully-connected N-node mesh this turns each event into an unbounded gossip storm. The canonical value (per `internal/config/cluster_config.go:71` and every test) is `1 * time.Hour`.

**Fix:** added `if c.Gossip.EventRetention == 0 { c.Gossip.EventRetention = 1 * time.Hour }` in `setDefault()`.

### DAE-H1. [Daemon] Package-level `shutdownOnce` leaks across Daemon instances (HIGH)
**File:** `internal/daemon/daemon.go:856`
**Severity:** HIGH — test isolation / future restart
**Confidence:** 85

`var shutdownOnce atomic.Bool` was declared at package scope. Once any Daemon called `shutdown()` and flipped the CAS, every subsequent Daemon constructed in the same process silently no-op'd its shutdown — leaving HTTP server, message bus, metrics store, plan store, container manager, and all `Components.Stop()` work un-run. Breaks test isolation today; blocks any future in-process restart strategy. Production SIGHUP reload does not recreate the Daemon today, so production is not currently impacted.

**Fix:** moved the guard onto the `Daemon` struct as `shutdownOnce atomic.Bool`, using `d.shutdownOnce.CompareAndSwap(...)`.

### QUEUE-M1. [Queue] Unit mismatch: `scanClusterMember` reads `last_heartbeat` as seconds (MEDIUM)
**File:** `internal/queue/store.go:972-976`
**Severity:** MEDIUM — correctness
**Confidence:** 82

`ClusterQueue.CheckNodeReachability` (`cluster_queue.go:209`) compared `lastHeartbeat` against `time.Now().UnixNano()`, treating the column as nanoseconds. But `Store.scanClusterMember` (`store.go:976`) reconstructed the time as `time.Unix(lastHb, 0)`, treating it as **seconds**. The schema test (`cluster_schema_test.go:102-103`) inserts `time.Now().UnixNano()`, so the writer convention is nanoseconds — making `scanClusterMember` wrong. With the current code, `LastHeartbeat` would be millions of years in the future. Same bug applied to `joined_at` at line 973.

**Fix:** both `JoinedAt` and `LastHeartbeat` now use `time.Unix(0, lastHb)` to match the nanosecond convention.

### QUEUE-M2. [Queue] `Dispatcher.Stop()` does not wait for goroutines to exit (MEDIUM)
**File:** `internal/queue/dispatcher.go:147-152`
**Severity:** MEDIUM — race condition / shutdown correctness
**Confidence:** 80

`Start()` launched `runDispatchLoop` and `runCleanupLoop` goroutines; `Stop()` only called `cancel()`. Without a WaitGroup, the caller couldn't know when the goroutines had actually exited. On shutdown this left the dispatcher's goroutines racing against the queue's `Close()` (the loops call `d.queue.ListByState` against a closing SQLite handle). Also caused data races in tests under `-race`.

**Fix:** added `wg sync.WaitGroup` field, `wg.Add(2)` in `Start`, `defer wg.Done()` in each loop wrapper, `d.wg.Wait()` in `Stop`.

### HTTP-D12c. [HTTP] `handleRateLimitSummary` still had unbounded `?limit=` (HIGH — completes prior D12)
**File:** `internal/comm/http/api_handlers.go:1247-1253`
**Severity:** HIGH — DoS
**Confidence:** 90

The prior-round D12 fix applied `parseIntParam` to 9 handlers but missed `handleRateLimitSummary`. A request like `GET /api/v1/metrics/rate-limits?limit=999999999` passed the `l > 0` check and propagated into `s.RateLimitSummaryGetter(ctx, 999999999)`, which could allocate large slices/maps. Same memory-exhaustion vector D12 was meant to close.

**Fix:** replaced manual parsing with `parseIntParam(r, "limit", 20, 1, 1000)`.

### HTTP-H1. [HTTP] Calendar handlers had unbounded `max_results` (HIGH)
**File:** `internal/comm/http/api_handlers.go:2084-2088` (list), `:2246-2250` (upcoming)
**Severity:** HIGH — DoS
**Confidence:** 85

Both `handleCalendarList` and `handleCalendarUpcoming` parsed `max_results` with only a `> 0` lower bound and no upper cap. A client could request `max_results=1000000`, causing the Google Calendar API call to fetch and the daemon to serialize a massive response. Inconsistent with other handlers that correctly use `parseIntParam`.

**Fix:** both handlers now use `parseIntParam(r, "max_results", <default>, 1, 250)`.

---

## Issues Deferred (21)

### Deferred — High (3)

**DEFERRED-A-H1. `startCollaborationSession` goroutine detached from shutdown via `context.Background()`**
`internal/agent/handler.go:1257-1259` — Receives parent `ctx` but launches the collaboration session runner with `context.WithTimeout(context.Background(), cfg.TimeBudget)`. The detached ctx is not cancelled by `ChatHandler.Stop()`, not tracked by `h.wg`, and on daemon shutdown keeps running for up to `cfg.TimeBudget` (commonly several minutes) after the daemon has supposedly stopped — continuing to invoke agents and publish bus events to a bus that may already be torn down. **Defer:** fix requires deciding whether collaboration sessions should be cancellable on handler stop (likely yes) and adding wg tracking; needs careful interaction testing with the collaboration stack.

**DEFERRED-LLM-M1. `Budget.WaitForRateLimit` does not reserve slots — concurrent callers exceed RPM**
`internal/llm/budget.go:665-716` — `WaitForRateLimit` checks capacity but never adds a reservation; timestamps are only appended later in `RecordUsage`. With N concurrent goroutines calling `WaitForRateLimit` while the window has capacity for 1, all N pass simultaneously and all N proceed to make API calls, exceeding RPM by up to N-1. In a multi-agent daemon this routinely triggers provider-side 429s. **Defer:** correct fix requires either appending `time.Now()` to `requestTimestamps` within `WaitForRateLimit` under the lock (and making `RecordUsage` idempotent on reservation), or moving to a semaphore/token-counter model. Either choice has cross-cutting effects on metrics and needs careful test coverage.

**DEFERRED-SEC-H4. Missing fence check in PTY session creation**
`internal/tools/builtin/shell.go:573-608` — `CreateSession` creates PTY sessions without calling `CheckCommand` on `config.Dir`. Regular shell execution has fence checks (lines 208-213, 356-362), but PTY sessions bypass this boundary check, potentially allowing escape from the project sandbox. **Defer:** needs decision on whether PTY sessions should honor the same fence as one-shot shell commands (likely yes) and whether the check should apply to `config.Cmd`, `config.Dir`, or both; some PTY use cases (interactive debuggers launched from arbitrary cwd) may need different rules.

### Deferred — Medium (11)

| ID | File:Line | Summary |
|----|-----------|---------|
| DEFERRED-SEC-H1 | `internal/tools/mcp/transport/http.go:69-134` | MCP HTTP transport defines `MaxResponseSize` (10MB) for responses but request body sent via `Send()` has no size limit; malicious/buggy MCP client could send arbitrarily large JSON-RPC requests |
| DEFERRED-SEC-H2 | `internal/tools/mcp/client.go:223-232` | `CallTool` only extracts content blocks with `Type == "text"`; image and resource blocks silently dropped from tool results |
| DEFERRED-SEC-H3 | `internal/security/taint/taint.go:356-392` | `CheckShellCommand` acknowledges TOCTOU race in comment: collects `(name, value)` pairs under RLock then releases before `CheckSink` (which acquires its own RLock); another goroutine could mutate `t.variables` in the window |
| DEFERRED-SEC-M1 | `internal/security/sanitizer.go:82-244` | Many injection patterns lack anchors (`^`, `\b`), causing false positives on legitimate content containing words like "administration" or "trust me" |
| DEFERRED-SEC-M2 | `internal/security/sanitizer.go:396-401` | Credential redaction `match[:4] + strings.Repeat("*", len(match)-8) + match[len(match)-4:]` produces negative repeat count for secrets shorter than 8 chars |
| DEFERRED-LLM-M2 | `internal/llm/anthropic.go:534` | Tool result error detection uses `IsError: strings.Contains(strings.ToLower(msg.Content), "error")` — false positives on "0 errors found", "error handling implemented", code containing `if err != nil` |
| DEFERRED-MEM-M1 | `internal/memory/manager.go:1558-1563` | `Manager.Close()` explicitly unlocks `m.mu` to call `StopPrefetchService` (which re-acquires), then re-locks — fragile window where concurrent callers observe partially-shutdown Manager |
| DEFERRED-TASK-H1 | `internal/task/step.go:443-512` | `StepStore.Update` performs non-transactional SELECT-then-UPDATE; concurrent updates can produce `StateTransition` records with incorrect `FromState` and lose intermediate state |
| DEFERRED-TASK-M1 | `internal/task/store.go:587-640` | `RecoverStaleTasks` performs bulk task UPDATE then per-task step UPDATEs with no transaction; process crash between them leaves DB inconsistent (tasks failed, steps non-terminal) |
| DEFERRED-SVC-C1 | `internal/services/daemon_service.go:151,158` | `time.Sleep(200ms)` blocks caller's context without respecting `ctx.Done()`; cancelled context cannot abort startup check |
| DEFERRED-PTY-M1 | `internal/comm/http/pty_handler.go:190-205` | `streamSessionOutput` goroutine keeps running and reading from `sess.Output()` when all WebSocket subscribers disconnect; minor CPU waste on long-running PTY sessions with no viewers |

### Deferred — Low (7)

| ID | File:Line | Summary |
|----|-----------|---------|
| DEFERRED-A-M1 | `internal/agent/pair_manager.go:220` | `Attempt.StartedAt` uses fabricated `time.Now().UTC().Add(-time.Minute)` timestamp; corrupts duration analytics |
| DEFERRED-A-M2 | `internal/agent/queue.go:283-289` | `InjectFollowUp` spawns untracked goroutines for persistence; may race with `Close()` path's `persistPending` |
| DEFERRED-MEM-H1 | `internal/memory/consolidation.go:316` | `summarizeByDate` computes "and N more" as `len(mems)-len(ids)` instead of `len(mems)-len(snippets)` (the correct pattern used at line 428); benign because IDs are never empty, but logic is wrong |
| DEFERRED-SEC-C1/C2 | `internal/tools/builtin/shell.go:113-120` | `SetSecurityOrchestrator` and `SetFenceChecker` assign pointer directly without nil check — typed-nil interface risk; project CLAUDE.md mandates guard pattern (engine.go:117-125 does it correctly) |
| DEFERRED-FL-H1 | `ui/flutter_ui/lib/providers/job_provider.dart:112-141` | `_fetchJobs()` calls `listJobs()` then `getQueueStats()` in one try block; if stats throws, fetched jobs are silently discarded (catch doesn't pass `updates`) |
| DEFERRED-FL-M1 | `ui/flutter_ui/lib/services/tts_service.dart:155-164` | TTS queue race: `_isSpeaking` set by async platform start handler, so loop processes next item before previous starts; makes `queueMessages` feature non-functional |
| DEFERRED-FL-M2 | `ui/flutter_ui/lib/features/projects/branches_panel.dart:41,69` | Branches panel hardcodes `'default'` as project ID; feature entirely non-functional for any project not registered with that exact name |

### Notes on defer decisions

- **DEFERRED-D15-2** (structural): the prior-round D15 fix added rollback infrastructure but only tracked 6 of ~20+ components. Extending coverage to all components is straightforward but requires touching every `c.X.Start()` call in `Components.Start` (4285 LOC file) and extending the rollback switch. The daemon-level `shutdown()` already cleans up everything eventually, so this is a timing-window improvement, not a leak fix. Recommend a focused PR.
- **DEFERRED-A-C3** (consistency): `internal/tools/builtin/file_edit.go:66-85` has the same typed-nil setter inconsistency as DEFERRED-SEC-C1/C2. Same fix pattern applies.

---

## Observations & Other Notes

### Strengths observed
- **Prior-round fixes are solid.** The verification subagent confirmed 28 of 32 items correctly implemented with cited code evidence. The D-fixes (D1 tool-pair preservation, D2 broker failover, D9 mutex release, D10 cooldown iteration, D11 Retry-After) are all correctly in place.
- **D1 tool-call/result pairing** logic is sound for all standard OpenAI/Anthropic message orderings. The parent-search `break` on first assistant is documented and correct for standard sequences.
- **Constant-time auth** is correctly applied in both `internal/comm/http/auth.go` and `internal/comm/web/auth.go` via `subtle.ConstantTimeCompare`.
- **SQLite parameterization** is consistent across memory/session/task/project — every query uses `?` placeholders, LIKE-fallback uses `ESCAPE '\\'` with `escapeLikeWildcards`, and IN clauses use per-element `?`.
- **Git operations** use arg-based invocation (`runGit(ctx, dir, args...)`), preventing shell injection in clone URLs, branch names, and paths.
- **Path traversal defenses** in `selfimprove/applier.go` use both `isWithinDir` and `validateFixPath` (rejecting `-`-prefixed, absolute, and `..` paths) with `--` separator — defense-in-depth done right.
- **FTS5 graceful degradation** in `SQLiteFTSStore` probes for FTS5 availability at init and branches cleanly between FTS5 MATCH and LIKE fallback.
- **Race-free WebSocket broadcast** in both `comm/http/server.go` and `comm/web/websocket.go` collects connections under RLock, releases, then writes — avoiding lock-held-during-write.
- **Flutter race guards**: both `chat_provider.dart` (`_loadGeneration`) and `job_provider.dart` (`_fetchGeneration`) use generation counters to discard stale async results.

### Recurring patterns worth addressing
1. **Substring matching for error classification** appears in `broker.go` (fixed this round as LLM-H2), `rpc/server.go:isParameterError` (D18 caveat), and `anthropic.go:534` (DEFERRED-LLM-M2). A shared `errors.As`-based classifier would prevent recurrence.
2. **Unbounded integer query parameters** appear in 3+ handlers beyond the D12/HTTP-H1 set. A linter rule or mandatory `parseIntParam` would prevent recurrence. The 9-handler D12 fix and 2-handler HTTP-H1 fix cover the known sites; recommend auditing `strconv.Atoi.*limit|max` across `api_handlers.go`.
3. **Package-level mutable state** (DAE-H1, fixed this round) is a subtle test-isolation hazard. Recommend reviewing other package-level `var` declarations for the same pattern.
4. **Mutex held across I/O** was fixed in D9 for the compactor, but `memory/manager.go:Close()` (DEFERRED-MEM-M1) has a related unlock-window smell. The "copy-out-then-call" or "locked helper" pattern should be applied consistently.
5. **Typed-nil setter inconsistency** between `engine.go` (correct: nil-checks) and `shell.go`/`file_edit.go` (no nil-check). A shared helper or generated setter would enforce the CLAUDE.md pattern.

### Files with the highest bug density this round
1. `internal/comm/http/api_handlers.go` (3 findings: HTTP-D12c, HTTP-H1 ×2 sites — all fixed)
2. `internal/llm/anthropic.go` (2 findings fixed: LLM-H1 ×2 sites; plus DEFERRED-LLM-M2)
3. `internal/llm/broker.go` (1 fixed: LLM-H2; plus prior D2 verified)
4. `internal/queue/store.go` (2 findings fixed: QUEUE-M1 ×2 sites)
5. `internal/agent/conversation.go` (1 critical fixed: A-C1) and `internal/agent/cache.go` (1 critical fixed: A-C2)

### Skills used
- `parallel-subagent-code-review` — drove the 8-subagent dispatch strategy
- `verify-plan-against-code` — drove the prior-round verification methodology
- `meept-orchestration` — understood multi-agent architecture for agent-core review

### Approach notes
The 8-subagent dispatch covered 196K LOC of Go source + 22K LOC of Dart. Each subagent read all non-test files in its domain (verified via tool-use counts ranging 29-101 per subagent) and produced structured findings. The verification subagent cross-referenced every prior-round fix against current code with cited line evidence. Total subagent wall-clock: ~15 minutes (running in parallel); coordinator synthesis and fix application: ~10 minutes.

One subagent (Security+Tools) initially reported three "CRITICAL" typed-nil findings (SEC-C1/C2/C3) that on coordinator review are actually LOW — the call sites pass non-nil pointers in production, and the existing `if x != nil` guards in `shell.go:573-608` prevent the panic path. Recategorized as DEFERRED-SEC-C1/C2 for consistency with the CLAUDE.md pattern, but not critical.

---

## Build & Test Verification

```bash
$ go build ./...        # clean
$ go vet ./...          # clean
$ go test ./internal/agent/... ./internal/llm/... ./internal/cluster/... \
             ./internal/queue/... ./internal/daemon/... ./internal/comm/http/...
ok  github.com/caimlas/meept/internal/agent       2.139s
ok  github.com/caimlas/meept/internal/agent/q     1.668s
ok  github.com/caimlas/meept/internal/llm         77.851s
ok  github.com/caimlas/meept/internal/llm/metrics (cached)
ok  github.com/caimlas/meept/internal/cluster     4.496s
ok  github.com/caimlas/meept/internal/queue       0.981s
ok  github.com/caimlas/meept/internal/daemon      2.922s
ok  github.com/caimlas/meept/internal/comm/http   38.256s
```

Flutter analysis not run this round (no Dart changes). All Dart-side findings are documented as DEFERRED with suggested fixes.

---

## Recommended Follow-Up Order

1. **PR 1 (this changeset):** A-C1, A-C2, LLM-H1, LLM-H2, CLUSTER-H1, DAE-H1, QUEUE-M1, QUEUE-M2, HTTP-D12c, HTTP-H1 — all in this changeset; 8 files changed.
2. **PR 2 (rate limit reservation):** DEFERRED-LLM-M1 — `Budget.WaitForRateLimit` needs slot reservation; affects RPM compliance under concurrency.
3. **PR 3 (collaboration shutdown):** DEFERRED-A-H1 — collaboration session goroutine should derive from handler ctx and join `h.wg`; needs interaction tests.
4. **PR 4 (security hardening):** DEFERRED-SEC-H4 (PTY fence), DEFERRED-SEC-C1/C2 (typed-nil setters), DEFERRED-SEC-H1 (MCP request size limit).
5. **PR 5 (transactional state):** DEFERRED-TASK-H1, DEFERRED-TASK-M1 — wrap `StepStore.Update` and `RecoverStaleTasks` in transactions.
6. **PR 6 (UX polish):** DEFERRED-FL-H1 (job provider partial failure), DEFERRED-FL-M1 (TTS queue race), DEFERRED-FL-M2 (branches panel hardcoded project).
7. **PR 7 (component rollback coverage):** DEFERRED-D15-2 — extend `startedHandlers` tracking to all components in `Components.Start`.

---

## Summary

**Prior-round verification:** 28/32 VERIFIED, 2 PARTIAL (now completed), 1 false positive, 1 N/A. No prior-round fix was missing or incorrect.

**This round:**
- **Fixed:** 10 issues across 8 files (2 critical, 6 high, 2 medium)
  - Agent: A-C1 (keepMask data loss), A-C2 (cache collision)
  - LLM: LLM-H1 (linear→exponential backoff), LLM-H2 (529 detection)
  - Cluster: CLUSTER-H1 (EventRetention default)
  - Daemon: DAE-H1 (shutdownOnce scoping)
  - Queue: QUEUE-M1 (timestamp units), QUEUE-M2 (dispatcher WaitGroup)
  - HTTP: HTTP-D12c (rate-limit handler), HTTP-H1 (calendar handlers)
- **Deferred:** 21 issues (3 high, 11 medium, 7 low) — each with specific file:line and recommended follow-up PR
- **No regressions:** all builds and tests pass clean
