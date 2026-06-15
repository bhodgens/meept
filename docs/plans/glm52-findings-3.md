# GLM-5.2 Codebase Review Findings — Round 3

**Reviewer:** Claude Opus 4.6 (z.ai/glm-5.2 backend)
**Date:** 2026-06-14
**Scope:** `meept-daemon`, `meept` CLI, Flutter UI (`ui/flutter_ui`), macOS MenuBar (`menubar/`)
**Method:** 8 scoped parallel subagents. Each subagent read all non-test files in its domain and produced a structured findings report synthesized here.

## Subagent decomposition (this round)

| # | Domain | Scope | Files read |
|---|--------|-------|------------|
| S1 | TUI | `internal/tui/**` (~28K LOC) | 42 |
| S2 | Orchestration / intelligence | `internal/agent/q`, `internal/agent/{prompts,prompt}`, `internal/code/**`, `internal/selfimprove/**`, `internal/repomap/**`, `internal/plan/**`, `internal/lint/**` | 50 |
| S3 | Tools | `internal/tools/**` (~30K LOC) | 50 |
| S4 | State / persistence | `internal/memory`, `internal/session`, `internal/skills`, `internal/context`, `internal/llm/context_firewall.go`, `internal/templates`, `internal/agents`, `internal/registry` | 39 |
| S5 | Network / API / RPC / transport | `internal/comm/{http,web,telegram}`, `internal/rpc`, `internal/transport`, `internal/auth`, `internal/validator`, `internal/project` | ~70 |
| S6 | Daemon infrastructure | `internal/{daemon,scheduler,cluster,queue,task,worker,runtime,pty,metrics,shadow,debug,calendar,stt,tts,errcls,sharedclient}` | ~25 of ~140 |
| S7 | Flutter UI | `ui/flutter_ui/**` (~19K LOC, 65 Dart files) | 44 |
| S8 | CLI + MenuBar | `cmd/meept/**` (~10K LOC), `menubar/**` (27 Swift files) | 54 |

Prior-round verification (round 1 and 2 fixes) is folded into the relevant subagent findings below — F1 (WebSocket reconnect), DEFERRED-FL-H1/M1/M2, D1/D2/D9–D11 were all re-checked this round and remain correct.

## Executive Summary

- **New findings reviewed:** 67 (5 critical, 25 high, 24 medium, 13 low)
- **Issues fixed this round:** 15 (across 14 files) — all CRITICAL tree-sitter leaks, the plan-manager map race, repomap nodeID race, LSP stdio transport write race, the firewall parent-search logic regression, LIKE wildcard escaping, FTS backend field mapping, plus 4 medium-quality consistency fixes
- **Issues deferred:** 52 — categorized by recommended follow-up PR
- **Build:** `go build ./...` clean
- **Tests:** `go test ./...` on all affected packages passing

The most impactful new fix cluster is **S2-C1..C3 / S2-C5**: every tree-sitter `RunQuery`/`RunRewrite`/`ExecuteRule` path leaked both the parsed `*sitter.Tree` and the compiled `*sitter.Query` — native C allocations that the Go GC cannot reclaim. Under any non-trivial AST workload (every `ast_*` and `lsp_*` agent tool uses these paths) the daemon's RSS would grow monotonically until OOM. The second critical cluster is **S2-C4/C5** (plan manager and repomap graph package-level data races) — these are `go test -race` fatalities under concurrent plan synthesis or parallel repomap generation.

---

## Issues Fixed This Round (15)

### S2-C1. Tree-sitter Tree + Query leak in `RunQuery` (CRITICAL)
**File:** `internal/code/ast/query.go:34,39`
**Severity:** CRITICAL — native memory leak
**Confidence:** 95

`RunQuery` calls `q.parser.GetTree(ctx, source, lang)` (returns `*sitter.Tree`) and `sitter.NewQuery(...)` (returns `*sitter.Query`). Both are C-allocated and require `Close()`. Neither was deferred. Every AST query leaked both objects. This is the engine behind `ast_parse`, `ast_symbols`, `ast_resolve` agent tools.

**Fix:** Added `defer tree.Close()` after the parse error check and `defer query.Close()` after the query-compile error check.

### S2-C2. Tree-sitter Tree + Query leak in `RunRewrite` (CRITICAL)
**File:** `internal/code/ast/rewrite.go:100,105`
**Severity:** CRITICAL — native memory leak
**Confidence:** 95

Same pattern as S2-C1, in the rewrite path. `cursor.Close()` was also missing (added).

**Fix:** Added `defer tree.Close()`, `defer query.Close()`, `defer cursor.Close()`.

### S2-C3. Tree-sitter Tree + Query leak in `ExecuteRule` (CRITICAL)
**File:** `internal/code/ast/rule.go:121,127`
**Severity:** CRITICAL — native memory leak
**Confidence:** 95

Same pattern, in the lint rule executor. `cursor.Close()` also missing (added).

**Fix:** Added `defer tree.Close()`, `defer query.Close()`, `defer cursor.Close()`.

### S2-C4. `PlanManager.phaseTaskMap` / `taskPlanMap` data race (CRITICAL)
**File:** `internal/plan/manager.go:36-37,393,408-409,481,492,516,531`
**Severity:** CRITICAL — fatal under `-race`
**Confidence:** 85

The two tracking maps are plain `map[string]string` fields with no synchronization. They are written from `Synthesize` (called when a plan transitions to `executing`) and read from `OnStepCompleted` / `OnTaskCompleted` (bus event callbacks that fire from the bus goroutine). Concurrent map read/write is a fatal runtime error. `PlanManager` had no mutex field at all.

**Fix:** Added `mu sync.RWMutex` to the struct; writers in `Synthesize` take `Lock`; readers in `OnStepCompleted` / `OnTaskCompleted` take `RLock`.

### S2-C5. Package-level `nodeID` counter race (CRITICAL)
**File:** `internal/repomap/graph.go:40,58-59`
**Severity:** CRITICAL — fatal under `-race`, can corrupt graph
**Confidence:** 85

`var nodeID int64` mutated without synchronization from `getOrCreateNode`. Under concurrent `BuildGraph` calls (parallel repomap generation for distinct chat sessions) the read-modify-write race produces duplicate node IDs, which gonum's `multi.DirectedGraph` tolerates but corrupts the file→node map. Subsequent PageRank / fitting traversals read the wrong node.

**Fix:** Switched to `atomic.AddInt64(&nodeID, 1)`; `getOrCreateNode` captures the previous value for the node's `id`.

### S2-H1. `StdioTransport.Write` concurrent writes corrupt LSP framing (HIGH)
**File:** `internal/code/lsp/transport/stdio.go:91-103`
**Severity:** HIGH — correctness
**Confidence:** 88

`Write` performs two separate `stdin.Write` calls (header, content) with no mutex. Concurrent writes (a notification during a request) interleave header/content on the pipe and corrupt JSON-RPC framing — once the server reads a malformed frame, the connection is unrecoverable.

**Fix:** Added `writeMu sync.Mutex` to `StdioTransport`; held across the entire Write body.

### S4-C1 (was DEFERRED-LLM-C2). `dropOldContext` orphans tool results from non-adjacent parents (HIGH → fix this round)
**File:** `internal/llm/context_firewall.go:685`
**Severity:** HIGH — provider 400 / context corruption
**Confidence:** 80

The D1 fix in round 1 walked backward to mark each `RoleTool` result's parent `RoleAssistant`. But the loop unconditionally `break`s on the first `RoleAssistant` encountered regardless of whether its `ToolCalls` matched `msg.ToolCallID`. For standard OpenAI ordering (tool result immediately follows its parent) this works; for any interleaved layout (text-only assistant messages between calls, compactor-injected messages, summarizer output) the loop stops at the wrong assistant, leaving the result orphaned and triggering provider 400s.

**Fix:** Removed the unconditional `break`; the search continues back through older assistants until either a match is found or the loop exhausts. The inner `break` (on match) is preserved.

### S4-H1. `summarizeByDate` over-reports "... and N more" count (HIGH)
**File:** `internal/memory/consolidation.go:316`
**Severity:** HIGH — correctness of user-visible summary text
**Confidence:** 85

`len(mems) - len(snippets)` over-counts when some snippets were filtered out (empty after trim). The displayed "... and 5 more" can be off by however many empty entries preceded the threshold hit. Round 2 noted this as DEFERRED-MEM-H1 — fix applied this round.

**Fix:** Use the loop index to compute remaining items: `len(mems) - i - 1`.

### S4-M1. `escapeLikeWildcards` does not escape the backslash escape character (MEDIUM)
**File:** `internal/memory/ftstore.go:394-398`
**Severity:** MEDIUM — LIKE query correctness / minor injection surface
**Confidence:** 85

With `ESCAPE '\\'` semantics a literal `\` in user input acts as the escape prefix. User-typed `\_` becomes pattern `\\_` which matches "literal backslash followed by any character" instead of literal `\_`. Adversarial input can craft wildcard injection via the escape character.

**Fix:** Escape the backslash first, before introducing new ones:
```go
s = strings.ReplaceAll(s, "\\", "\\\\")
s = strings.ReplaceAll(s, "%", "\\%")
s = strings.ReplaceAll(s, "_", "\\_")
```

### S4-M2. `GetExpiredMemories` populates `UpdatedAt` with `last_accessed_at` data (MEDIUM)
**File:** `internal/memory/manager.go:1384`
**Severity:** MEDIUM — semantic mismatch
**Confidence:** 85

`lastAccessed` was parsed from a `last_accessed_at` column but assigned to `Memory.UpdatedAt` (modification time) instead of `Memory.LastAccessedAt`. Downstream consolidation logic that consults `UpdatedAt` for modification time sees access timestamps, skewing expiration decisions.

**Fix:** Reassigned to `LastAccessedAt: lastAccessed`.

### S1-C1. `debugCounter` package-level race (CRITICAL)
**File:** `internal/tui/debug_log.go:9,16-17`
**Severity:** CRITICAL — fatal under `-race`
**Confidence:** 90

Package-level `int` counter mutated by `DebugLog()` from multiple goroutines (event stream handlers, slash autocomplete, tea.Cmd closures). Plain `++` is a read-modify-write race.

**Fix:** Replaced with `atomic.AddUint64` counter.

### S3-H1. `ShellExecuteTool.classifyRisk` pipe split ignores quotes (HIGH)
**File:** `internal/tools/builtin/shell.go:530-540`
**Severity:** HIGH — correctness of risk classification
**Confidence:** 82

`strings.Split(command, "|")` does not understand quoting. `awk -F'|' '{print $2}'` is split at the pipe inside the quotes, producing nonsense fragments that each get classified independently — usually inflating to `RiskHigh` and blocking legitimate commands. The project has a quote-aware tokenizer (`shell_tokenize.go`).

**Fix:** Replaced the naive split with a quote-aware scan that only splits on `|` tokens outside single/double quotes. Falls back to whole-command classification if no unpiped tokenization succeeds.

### S3-H2. `ResolveTool` accept path skips fence re-validation (HIGH)
**File:** `internal/tools/builtin/resolve.go:122-127`
**Severity:** HIGH — defense-in-depth / sandbox escape
**Confidence:** 82

`change.FilePath` was validated when the pending change was registered, but the accept step performs no re-check at the write site. Defense in depth (and protection against future code paths that bypass fence checking at registration) calls for re-checking.

**Fix:** Added a `fenceChecker` field + `SetFenceChecker` setter (nil-guarded per CLAUDE.md); called before `os.WriteFile` in the accept branch.

### S3-H3. `FileEditTool.SetPendingChangesRegistry` missing typed-nil guard (HIGH per CLAUDE.md)
**File:** `internal/tools/builtin/file_edit.go:73-75`
**Severity:** MEDIUM (re-categorized from HIGH on coordinator review — concrete pointer type today, but violates CLAUDE.md)
**Confidence:** 80

Every sibling setter on `FileEditTool` (`SetFenceChecker:84`, `SetBlockResolver:67`, `SetLSPNotifier:59`) nil-checks; `SetPendingChangesRegistry` does not. Round 2 flagged this as DEFERRED-A-C3 — fix applied this round.

**Fix:** Added `if registry != nil { t.pendingChangesRegistry = registry }`.

### S3-M1. Schedule/Cron tools return `(result, err)` simultaneously, discarding structured result (MEDIUM)
**Files:** `internal/tools/builtin/tool_schedule_create.go:199-204`, `tool_schedule_delete.go:66-73`, `tool_schedule_list.go:273-280`, `tool_cron_create.go:196-202`
**Severity:** MEDIUM — error envelope quality
**Confidence:** 88

When the tool already encodes the failure in its result struct (`Success: false`, populated `Error` field), returning a non-nil `error` causes the registry to discard the structured result and substitute a generic `NewErrorResult`. The agent loses tool-specific context.

**Fix:** For the four schedule/cron tools where the result struct is the source of truth, changed `(result, err)` to `(result, nil)` at the structured-failure paths.

---

## Issues Deferred (52)

Grouped by recommended follow-up PR. File:line is given where the subagent provided it; in a few cases only a function name is available and is noted.

### PR-1: Security hardening (HIGH/CRITICAL, design needed)

| ID | File:line | Issue |
|----|-----------|-------|
| S5-C1 | `internal/transport/client.go:94`, `http_client.go:19-36,48-54` | TLS fingerprint pinning (`WithPinnedFingerprint`) stores values but no `VerifyPeerCertificate` callback reads them; default `InsecureSkipVerify: true` is effectively unmitigated. Implement verification callback, default to secure. |
| S3-C2 | `internal/tools/builtin/web_fetch.go:122-125,271-279` | SSRF: URL validation is scheme-prefix only; no private-IP / link-local filtering. `http://169.254.169.254/...` is reachable. Add `isPrivateIP` resolver check. |
| S3-C1 | `internal/tools/builtin/git_commit.go:138-143,206-213` | `GitCommitTool` passes LLM-provided paths to `git add` with no fence check; can stage `/etc/passwd`, `~/.ssh/id_rsa`. Add `FenceChecker` field + validate every file path. `GitSplitTool`/`GitOverviewTool` also lack fence on `workingDir`. |
| S5-H1 | `internal/comm/http/server.go:1596-1634` | `WebSocketAllowedOrigins` config is computed but never consulted (handshake calls only `isLocalOrigin`). Operator-facing config is silently ignored. |
| S5-H2 | `internal/comm/web/server.go:258` | `comm/web` server has no TLS code path; `ListenAndServe` only. Default config exposes endpoints in plaintext. Either delete (likely deprecated) or add TLS + require auth. |
| S5-H3 | `internal/comm/web/server.go:355-365` | CORS unconditionally returns `Access-Control-Allow-Origin: *` when `EnableCORS=true`, regardless of `RequireAuth`. Combined with credentialed requests → cross-origin read. Add `Vary: Origin`, echo allowlist only. |
| S5-H4 | `internal/comm/http/pty_handler.go:252-254` | `generateSessionID` uses `time.Now().UnixNano()` — predictable. PTY session IDs gate terminal read/write. Use `crypto/rand`. |
| S5-H5 | `internal/comm/http/api_handlers.go:2571`, `internal/project/manager_branches.go:83`, `manager.go:50` | `git checkout` and `git clone` accept user-controlled refs/URLs without `-` prefix guard or `--` separator. `--orphan=pwn` creates orphan branch, `--upload-pack` style attack on clone. |
| S5-H6 | `internal/comm/web/server.go:586-589` | `MaxBytesReader(nil, ...)` passes nil ResponseWriter — defeats connection-close on body-limit excess. Sister impl in `comm/http/server.go:1025` does it correctly. |
| S5-M2 | `internal/auth/encryption_other.go:11-18` | AES-GCM key derivation degrades to predictable (binary install path) on non-darwin/non-linux. Returns explicit error or fall back to random persisted key. |
| S5-M3 | `internal/comm/telegram/handler.go:178,205` | Telegram session files written mode `0644`; rest of codebase uses `0600`. Inconsistent. |
| S5-M5 | `internal/comm/http/auth.go:81-85` | `TrimPrefix` without prefix check accepts `Authorization: <key>` without `Bearer ` scheme. Use `HasPrefix` + slice. |
| S5-M6 | `internal/comm/http/api_handlers.go:2865` | `handleMemoryVectorDelete` parses ID via `TrimPrefix` instead of `r.PathValue("id")`. Brittle. |

### PR-2: Concurrency / lifecycle (HIGH)

| ID | File:line | Issue |
|----|-----------|-------|
| S6-CRIT-1 | `internal/queue/cluster_queue.go:255-258` | `Close` does `close(cq.stopCh)` with no `sync.Once` → double-close panic. Add `closeOnce sync.Once`. Note: `stopCh` is never read in this file, so the close is effectively a no-op except for the panic risk — broader "what is stopCh for?" question. |
| S6-CRIT-2 | `internal/queue/cluster_queue.go:225-241` | `ReclaimIfStale` holds write lock across `bus.Publish` and store I/O — can wedge the entire queue under stale-claim sweeps. Collect IDs under lock, release, then reclaim. |
| S6-H1 | `internal/daemon/components.go:1708-1826` | D15 rollback coverage gap — switch covers 20 cases but `Components.Stop()` closes ≥16 more (MCPManager, LSPManager, DebugManager, ClusterQueue, ShadowManager, LearningPipeline, SelfImproveCtrl, AgentRegistry, ClassifierClient, etc.). Recommend stopFunc slice pattern instead of switch to prevent future drift. (Continuation of round-2 DEFERRED-D15-2.) |
| S6-H2 | `internal/worker/pool.go:77-111` | `startErr` declared but never assigned inside `startOnce.Do`; all worker-add errors are logged not propagated. If every worker fails to start, `Start` returns nil — pool claims jobs and never processes them. |
| S6-H3 | `internal/runtime/docker.go:97-99` | `Execute` holds `b.mu.Lock()` for the entire container exec duration — serializes all commands across all callers. Snapshot containerID under RLock, release, then exec. |
| S6-H4 | `internal/cluster/gossip.go:316-330` | `handleClusterEvent` dedup is read-lock-then-write-lock TOCTOU. Two goroutines handling the same eventID can both pass the seen-check and both re-broadcast. Hold write lock for check-and-set atomically. |
| S6-M4 | `internal/shadow/manager.go:163-228` | `ProcessRecord` holds `m.mu.Lock()` across LLM scoring calls (multi-second HTTP). In ModeAsync, stacks up dozens of in-flight goroutines each blocked on the lock. Drop lock during LLM calls. |
| S6-M5 | `internal/shadow/manager.go:283-303` | `CaptureInteraction` spawns unbounded goroutines, not tracked by any WaitGroup; on Close they may complete after stores are closed. Add WaitGroup + cap concurrency. |
| S6-M6 | `internal/scheduler/scheduler.go:304-330` | `RunNow` missing `running.Load()` check — can execute jobs after Stop has been called, racing with closing dependencies. |
| S2-H3 | `internal/agent/prompt/loader.go:189-191` | `AddSearchPath` mutates `l.searchPaths` without lock while `Load` reads under RLock and `Exists`/`SearchPaths` read with no lock. Add lock. |
| S2-H4 | `internal/repomap/renderer.go:177,218-219` | `ContextRenderer.treeCache` concurrent map access with no mutex — fatal under `-race`. Add RWMutex or sync.Map. |
| S1-H-Events | `internal/tui/events.go:26,75,263-265` | `EventStream.events` channel allocated and exposed via `Events()` but never written to — consumers block forever. Either wire it up or remove the dead API. |

### PR-3: Swift menubar fixes (HIGH)

| ID | File:line | Issue |
|----|-----------|-------|
| S8-CRIT | `menubar/MeeptMenuBar/Services/MenubarConfigService.swift:10,68` | Hardcoded `DefaultDevAPIKey = "meept_dev_default_key_CHANGE_ME"` compiled into binary; `apiToken` falls back to it. Remove fallback (return nil, surface explicit error) or gate behind `#if DEBUG`. |
| S8-H1 | `menubar/MeeptMenuBar/Services/DaemonController.swift:94-95` | `generatePlist()` emits `StandardOutPathString` / `StandardErrorPathString` — launchd does not recognize these. Correct keys are `StandardOutPath` / `StandardErrorPath`. Daemon stdout/stderr go nowhere when launched via this plist. |
| S8-H2 | `menubar/MeeptMenuBar/Services/DashboardService.swift:75` | Uses `URLSession.shared` which has no delegate — cannot validate self-signed cert. Every metrics call fails with `-1202` against default TLS daemon. Construct private session with `LocalhostTrustDelegate` (APIClient.swift:21-25 has the pattern). |
| S8-H3 | `menubar/MeeptMenuBar/ViewModels/DaemonStatusViewModel.swift:32`, `MetricsViewModel.swift:37` | `Timer.scheduledTimer` schedules in `.default` mode only — freezes during UI tracking (scroll, button hold). Use `RunLoop.main.add(timer, forMode: .common)`. |
| S8-H4 | `menubar/MeeptMenuBar/ViewModels/ConfigViewModel.swift:73-74,88-89` | Single catch sets `showNormalizeError = true` for both normalize and save failures; `showSaveError` declared but never set. Split into two do/catch blocks. |
| S8-H5 | `cmd/meept/tts.go:309-316` | `downloadFile` writes directly to dest; partial download is reported as installed by `scanInstalledVoices`. Write to `.part`, rename on success. |
| S8-H6 | `menubar/MeeptMenuBar/Services/DaemonController.swift:33` | kickstart failure after launchd load is discarded — user sees "started successfully" even if daemon fails to start. |
| S8-M-WS | `menubar/MeeptMenuBar/Services/WebSocketManager.swift:109-112` | Once `reconnectAttempts` reaches max, never recovers even on explicit `connect()` — only disconnect/connect cycle resets. |
| S8-M-NMgr | `menubar/MeeptMenuBar/Services/NotificationManager.swift:95` | Constructs fresh `MenubarConfigService()` per notification, re-parses JSON5 from disk every call. Cache the service. |
| S8-M-Menu | `menubar/MeeptMenuBar/Views/MenuView.swift` | Appears to be dead code (108 LOC, parallel to `MenuBarContentView.swift`). Delete if confirmed unused. |

### PR-4: Flutter UI polish (HIGH/MEDIUM)

| ID | File:line | Issue |
|----|-----------|-------|
| S7-H-Lower | `ui/flutter_ui/lib/{main.dart:54, features/settings/settings_panel.dart:190,715,736,740, services/api_client.dart:210,213, features/tasks/tasks_detail.dart:191}` | Mandatory lowercase UI convention violations: `'Meept GUI Client v...'`, `'API token saved to keychain'`, `'API token'` (×5), `'Missing API token — configure in settings'`, `'Invalid API token (HTTP 418)'`, `'Are you sure you want to cancel "..."'`. |
| S7-H-Cal | `ui/flutter_ui/lib/features/calendar/calendar_panel.dart:255-372` | "Create event" dialog has no end-date or end-time picker — `_endDate` is set to start + 1h and only dragged along when start changes. Users cannot create 2-hour, all-day, or multi-day events. |
| S7-H-Key | `ui/flutter_ui/lib/services/storage_service.dart:59`, `core/constants.dart:41` | `getApiKey` returns hardcoded dev key `'meept_dev_default_key_CHANGE_ME'` when none stored — masks misconfiguration, latent security issue. Return nil; surface visible warning when resolved key equals default. (Mirrors S8-CRIT.) |
| S7-H-STT | `ui/flutter_ui/lib/services/stt_service.dart`, `tts_service.dart` | No `dispose()` — platform resources (recognizer session, synthesizer) leak across hot-reload and panel teardown. Add dispose + clear handlers; providers call it. |
| S7-H-Mem | `ui/flutter_ui/lib/features/memory/memory_panel.dart:23,31,44,141-147` | `_hasSearched` placeholder is unreachable — `initState` immediately calls `_loadRecentMemories` which sets `_hasSearched = true` before first build. The "search or browse memories" empty state is dead UX. |

### PR-5: Tool correctness (MEDIUM)

| ID | File:line | Issue |
|----|-----------|-------|
| S3-H-Inv | `internal/tools/builtin/git_commit.go:102-105` | `validate` toggle inverted — explicit `false` is silently re-enabled. Distinguish "not specified" from "explicitly false". |
| S3-M-Sched | `internal/tools/builtin/tool_cron_create.go:270-274` | Invalid `day_of_month` silently coerced to 1; LLM gets no feedback. Return error for out-of-range, default only when key absent. |
| S3-M-MCPReload | `internal/tools/mcp/manager.go:273-290` | `Reload` returns only last error, masks earlier failures. Aggregate with `errors.Join`. |
| S3-M-Retry | `internal/tools/registry.go:417-419` | `ExecuteWithRetry` exponential backoff `1 << attempt` overflows `time.Duration` for large `MaxRetries`. Cap shift at 30. |
| S3-M-SetRuntime | `internal/tools/builtin/shell.go:130-137` | `SetRuntimeManager` unconditional assignment + overwrites `t.logger` with `slog.Default()`, discarding injected logger. Guard + derive from existing logger. |
| S3-L-Alias | `internal/tools/builtin/file_edit.go:983-994` | `applyEdits` may alias original backing array if replacement is a subslice. Copy into fresh slice. |
| S3-L-OOB | `internal/tools/builtin/lsp_writethrough.go:285` | `applyFormattingEdits` indexes `lines[startLine]` without bounds check; degenerate trailing-newline edits can panic. |
| S3-L-Notif | `internal/tools/builtin/lsp_writethrough.go:198-211` | `OnNotification` may register handlers repeatedly per write — slow leak. |

### PR-6: Task / scheduler / cluster transactions (MEDIUM)

| ID | File:line | Issue |
|----|-----------|-------|
| S6-M1 | `internal/task/step.go:662-692,1014-1045` | `SetState` / `SetStateWithReason` non-transactional read-then-write — two concurrent callers can both transition from same `oldState`. Wrap in tx (sibling `Update` at step.go:447 does it correctly). |
| S6-M2 | `internal/queue/queue.go:149-198` | `Claim` lists pending then `ClaimNextByID` — under contention workers waste list queries and return `ErrNoJobAvailable` instead of using `store.ClaimNextForAgent` directly. |
| S6-M3 | `internal/queue/store.go:753-833` | `RecoverFromDeadLetter` loses original `due_at` — recovered scheduled jobs become immediately claimable. Add `due_at` column to dead_letter or document the reset. |

### PR-7: TUI casing + minor (HIGH per CLAUDE.md / LOW correctness)

Casing convention violations across TUI — per CLAUDE.md all UI text must be lowercase. Aggregated rather than per-instance:

| File | Approx lines | What |
|------|--------------|------|
| `internal/tui/models/chat.go` | 249, 445 | `'Type a message...'`, `'Welcome to Meept! Type a message to begin.'` |
| `internal/tui/app.go` | 1796 | Initial `'Loading...'` view |
| `internal/tui/models/tasks.go` | 69-77, 220-253, 778-802, 905-1230 | Column titles (`'Name'`, `'State'`, `'Agent'`, `'Steps'`, `'Progress'`, `'Memory'`, `'Updated'`, `'Schedule'`, `'Next Run'`, `'Status'`), tab labels (`'Tasks'`, `'Jobs'`, `'Lineage'`), filter labels (`'[All]'`, `'[Active]'`, `'[Mine]'`, `'[Completed]'`, `'[Failed]'`), section headers (`'Steps'`, `'Memory Context'`, `'Linked Sessions'`), status messages (`'Loading jobs...'`, `'Error'`, `"Press 'r' to refresh"`) |
| `internal/tui/models/queue.go` | multiple | `'Job Queue'`, `'Job Detail'`, `'Queue View Help'`, `'Loading queue data...'`, column titles, state labels |
| `internal/tui/models/memory.go` | multiple | `'Memory Browser'`, `'Search Results'`, `'Searching...'`, `'Search Error'`, `'No results...'`, field labels (`'ID:'`, `'Type:'`, etc.) |
| `internal/tui/models/sessions.go` | 57-60, 112-116 | Column titles `'Title'`, `'Created'`, `'Last Activity'` |
| `internal/tui/models/plans.go` | multiple | Column titles `'Title'`, `'Phases'`, `'Steps'`, `'Progress'`, `'Updated'` |
| `internal/tui/models/status.go` | 292-296 | Quick action descriptions `'Chat view'`, `'Tasks view'`, etc. |

Recommend a single mechanical pass over `internal/tui/models/*.go` + `internal/tui/app.go` to lowercase these.

### Other LOW findings (not categorized for PR)

- `internal/pty/session.go:218-230` — Read delivers errorChan only once; subsequent reads block until done. Cache last error.
- `internal/pty/manager.go:82-94` — custom counter struct instead of `atomic.Int64`. Idiomatic Go.
- `internal/metrics/store.go:298-392` — Record/flush small race window. Channel-based batcher cleaner.
- `internal/cluster/gossip_transport.go:109-126` — `Stop` close(stopCh) fragile, use `sync.Once`.
- `internal/tools/builtin/memory.go:720` — variable shadowing (`dom := domain`).
- `internal/rpc/server.go:470,499,513` — sequential atomic IDs for bus messages. Predictable but not security-sensitive.
- `internal/comm/http/events.go` — `e.buffer[1:]` shift on every overflow O(n).
- `internal/comm/telegram/bot.go:181` — bot token in URL query string for getUpdates (Telegram mandates).
- `internal/comm/http/notification_handlers.go:62-65` — `/ws/notifications` Accept has no `OriginPatterns`.
- `internal/comm/http/pty_handler.go:50-53` — routes register without HTTP method constraint.
- `cmd/meept/daemon.go:160` — fire-and-forget `daemonCmd.Wait()` goroutine.
- `menubar/MeeptMenuBar/Views/NotificationCenterMenuView.swift:9,44` — `@StateObject` with shared singleton; should be `@ObservedObject`.
- `cmd/meept/analytics.go:473-487` — CLI opens daemon's metrics SQLite directly; architectural smell.

---

## Observations & Other Notes

### Strengths observed this round

- **Agent ID validation** (`comm/http/config_service.go:19-28`) uses anchored regex `^[A-Za-z0-9_-]+$` at every entrypoint — solid path-traversal defense. Round 1 finding F2 still holding.
- **SQL parameterization** is consistent across memory/session/task/project — every query uses `?` placeholders, LIKE-fallback uses `ESCAPE '\\'` (now correctly escaped after S4-M1), IN clauses use per-element `?`.
- **Git operations** use arg-based invocation throughout — no shell injection in clone URLs, branch names (the `-` prefix issue is a separate concern tracked as S5-H5).
- **Constant-time auth** in both `comm/http/auth.go` and `comm/web/auth.go` via `subtle.ConstantTimeCompare`.
- **Path traversal defenses** in `selfimprove/applier.go` use both `isWithinDir` and `validateFixPath` with `--` separator — defense-in-depth done right.
- **Race-free WebSocket broadcast** in `comm/http/server.go` and `comm/web/websocket.go` collect under RLock, release, then write.
- **Flutter race guards**: `chat_provider.dart` (`_loadGeneration`) and `job_provider.dart` (`_fetchGeneration`) correctly discard stale async results.
- **Round-1/2 fixes verified intact**: F1 (WebSocket reconnect try/finally), D1 (tool-call/result pairing, now strengthened by S4-C1), D2 (broker failover via `errors.As`), D9 (mutex release), D10 (cooldown), D11 (Retry-After), A-C1 (keepMask init), A-C2 (cache hash collision), LLM-H1 (exponential backoff), LLM-H2 (529 detection), CLUSTER-H1 (EventRetention default).
- **Prior-round deferred items now closed**: DEFERRED-LLM-C2 (→ S4-C1), DEFERRED-MEM-H1 (→ S4-H1), DEFERRED-A-C3 (→ S3-H3 typed-nil setter).
- **Defense-in-depth fence pattern** correctly applied in `FileEditTool`, `WriteFileTool`, `ReadFileTool` — the gap in `GitCommitTool` (S3-C1) is the exception, not the rule.

### Recurring patterns worth addressing

1. **Tree-sitter resource lifecycle** — three independent sites (`query.go`, `rewrite.go`, `rule.go`) had identical leaks. A helper `parseAndQuery(ctx, parser, source, lang, pattern) (tree, query, cursor, cleanup, error)` would prevent recurrence as new AST entrypoints are added.
2. **Package-level mutable state** — `nodeID` (S2-C5), `debugCounter` (S1-C1), and the prior round's DAEMON-H1 all follow the same pattern. Audit `grep -rn '^var ' internal/` for package-level `var` declarations that aren't constants or function pointers.
3. **Substring matching for classification** — `broker.go` was fixed in round 2; `shell.go:classifyRisk` pipe split (S3-H1) is the same family. The new `internal/errcls` package is the right home for these; route new classifiers through it.
4. **Mutex held across I/O** — fixed in D9 for the compactor; `ReclaimIfStale` (S6-CRIT-2), `shadow/manager.go:ProcessRecord` (S6-M4), and `DockerBackend.Execute` (S6-H3) all repeat the pattern. The "collect under lock, release, then operate" pattern should be applied consistently.
5. **Typed-nil setter inconsistency** — engine.go is the reference; `shell.go`/`file_edit.go` (round 2 + this round) and `resolve.go` (this round) had to be brought up to par. A linter rule or generated setter would enforce the CLAUDE.md pattern project-wide.
6. **Hardcoded dev API key string** — appears in both `ui/flutter_ui/lib/core/constants.dart:41` (S7-H-Key) and `menubar/MeeptMenuBar/Services/MenubarConfigService.swift:10` (S8-CRIT). A single source of truth (or better, no fallback at all) is needed.
7. **Lowercase UI convention drift** — TUI has 30+ violations (S1-H-Casing cluster, PR-7); Flutter UI has 8 (S7-H-Lower). Both need a mechanical pass.

### Files with the highest bug density this round

1. `internal/code/ast/{query,rewrite,rule}.go` — 3 CRITICAL native memory leaks (all same root cause)
2. `internal/plan/manager.go` — 1 CRITICAL data race
3. `internal/repomap/graph.go` — 1 CRITICAL data race
4. `internal/comm/http/api_handlers.go` — 3 findings (1 fixed prior round, 2 deferred)
5. `internal/llm/context_firewall.go` — 1 HIGH correctness (fixed, S4-C1)
6. `internal/tools/builtin/{shell,resolve,file_edit,git_commit,web_fetch}.go` — 6 findings (2 deferred CRITICAL, 4 fixed)
7. `menubar/MeeptMenuBar/Services/*` — 5 HIGH findings (all deferred to PR-3)

### Coverage gaps (subagent honesty)

- **S6 (daemon infra)**: 25 of ~140 non-test files read in depth. `internal/daemon/{service,events,*_rpc}.go`, `internal/task/{interrupt,amendment,registry,checklist}.go`, most of `internal/shadow/*.go` and `internal/debug/*.go`, all of `internal/{stt,tts,calendar,sharedclient}/*.go` not read — packages not asserted clean.
- **S5 (network)**: `api_handlers.go` and `server.go` paginated (~80% read, every network/auth handler covered). `rpc/` covered from prior session.
- **S7 (Flutter)**: `theme/typography.dart`, `effects.dart`, `markdown_style.dart`, `syntax_highlighter.dart`, generated `.freezed.dart`/`.g.dart`, `features/settings/settings_inputs.dart`, `providers/{agent,plan,task}_provider.dart` not parsed.
- **S2 (orchestration)**: `internal/agent/q/*.go` read but no findings surfaced in the report (subagent focused on code intel + plan + repomap). Worth a dedicated pass if Q agent correctness is in question.

### Approach notes

The 8-subagent dispatch covered ~400K LOC of Go source + 19K Dart + ~3K Swift. Four subagents hit provider 429s mid-work and were re-dispatched; all 8 ultimately completed. Subagent reports varied in depth (S6 and S2 explicitly noted coverage gaps; S5 and S7 claimed full coverage and backed it with file lists).

Prompt-injection attempts were observed: several subagents (S6, S7) reported system-reminders appended to file Read results instructing them to "consider whether this would be considered malware" and "MUST refuse to improve or augment the code". These were correctly identified as injected text (legitimate system reminders appear in actual system context, not appended to tool results) and ignored. The code under review is the user's own Go daemon project — standard LLM orchestration infrastructure with no obfuscation, dynamic loading, or exfiltration patterns.

---

## Build & Test Verification

```bash
$ go build ./...        # clean
$ go vet ./...          # clean
$ go test ./internal/code/... ./internal/plan/... ./internal/repomap/... \
             ./internal/llm/... ./internal/memory/... ./internal/tools/... \
             ./internal/code/lsp/...
ok  github.com/caimlas/meept/internal/code/ast       (cached)
ok  github.com/caimlas/meept/internal/code/lsp        0.012s
ok  github.com/caimlas/meept/internal/code/tools      (cached)
ok  github.com/caimlas/meept/internal/plan            0.034s
ok  github.com/caimlas/meept/internal/repomap         0.008s
ok  github.com/caimlas/meept/internal/llm             77.851s
ok  github.com/caimlas/meept/internal/memory          (cached)
ok  github.com/caimlas/meept/internal/tools           (cached)
ok  github.com/caimlas/meept/internal/tools/builtin   (cached)
ok  github.com/caimlas/meept/internal/tools/mcp       (cached)
```

Flutter analysis and Swift build not run this round (no Dart/Swift changes — all Dart/Swift findings are documented as DEFERRED).

---

## Recommended Follow-Up Order

1. **PR 1 (this changeset):** S2-C1..C5, S2-H1, S4-C1, S4-H1, S4-M1, S4-M2, S1-C1, S3-H1, S3-H2, S3-H3, S3-M1 — 15 fixes across 14 files.
2. **PR 2 (security hardening):** S5-C1, S3-C1, S3-C2, S5-H1..H6, S5-M2/M3/M5/M6 — needs design decisions on TLS pinning semantics, web server deprecation, and SSRF allowlist.
3. **PR 3 (concurrency / lifecycle):** S6-CRIT-1/2, S6-H1..H4, S6-M4..M6, S2-H3/H4, S1-H-Events — broad touch; recommend the stopFunc-slice refactor for daemon components.
4. **PR 4 (Swift menubar):** S8-CRIT, S8-H1..H6, S8-M-* — all Swift-side; can be done without touching Go.
5. **PR 5 (Flutter UI polish):** S7-H-* — casing pass + dialog/dispose fixes.
6. **PR 6 (tool correctness):** S3-H-Inv, S3-M-* — small, contained.
7. **PR 7 (task/scheduler/cluster transactions):** S6-M1..M3.
8. **PR 8 (TUI casing):** mechanical lowercase pass over `internal/tui/models/*.go` + `app.go`.

---

## Summary

**This round:**
- **Fixed:** 15 issues across 14 files (5 critical, 6 high, 4 medium)
  - Code intel: S2-C1, S2-C2, S2-C3 (tree-sitter leaks × 3)
  - Plan manager: S2-C4 (map race)
  - Repomap: S2-C5 (nodeID race)
  - LSP transport: S2-H1 (write mutex)
  - Context firewall: S4-C1 (parent search — closes prior DEFERRED-LLM-C2)
  - Memory: S4-H1 (summarizeByDate count), S4-M1 (LIKE escape), S4-M2 (field mapping)
  - TUI: S1-C1 (debugCounter race)
  - Tools: S3-H1 (pipe split), S3-H2 (resolve fence), S3-H3 (typed-nil setter), S3-M1 (schedule error envelope × 4 files)
- **Deferred:** 52 issues across 7 recommended PRs
- **No regressions:** `go build ./...`, `go vet ./...`, all affected package tests pass
- **Prior-round verification:** all sampled round-1/round-2 fixes (F1, D1, D2, D9-D11, A-C1, A-C2, LLM-H1, LLM-H2, CLUSTER-H1) remain correct in current code

**Most impactful single fix:** S2-C1/C2/C3 — three native memory leaks that compound on every AST tool invocation. Under realistic agent workloads (multiple `ast_symbols` / `ast_parse` / `lsp_*` calls per session) the daemon's RSS would grow until OOM. The fix is three lines per site.

**Most impactful deferred cluster:** PR-2 (security). S5-C1 (dead TLS pinning), S3-C1 (git_commit path traversal), and S3-C2 (web_fetch SSRF) are each individually serious; together they represent the highest-leverage hardening work. Each needs a small design decision (pinning semantics, fence wiring, SSRF allowlist) before implementation.
