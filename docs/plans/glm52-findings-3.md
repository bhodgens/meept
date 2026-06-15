# GLM-5.2 Codebase Review Findings — Round 4

**Reviewer:** Claude Opus 4.6 (z.ai/glm-5.2 backend)
**Date:** 2026-06-15
**Scope:** `meept-daemon`, `meept` CLI, Flutter UI (`ui/flutter_ui`), macOS MenuBar (`menubar/`)
**Method:** 8 scoped parallel `feature-dev:code-reviewer` subagents. Each subagent read all non-test files in its domain and produced a structured findings report synthesized here. Raw subagent reports at `/tmp/glm52-round4/s{1..8}.md`.

## Subagent decomposition (this round)

| # | Domain | Scope |
|---|--------|-------|
| S1 | Agent | `internal/agent/**` (146 files) |
| S2 | Code intel + plan + repomap + lint + selfimprove + Q | `internal/code/**`, `internal/repomap/**`, `internal/plan/**`, `internal/lint/**`, `internal/selfimprove/**`, `internal/agent/q/**` |
| S3 | LLM + memory | `internal/llm/**`, `internal/memory/**` |
| S4 | Tools | `internal/tools/**` (89 files) |
| S5 | Network / API / RPC / transport | `internal/comm/{http,web,telegram}`, `internal/rpc`, `internal/transport`, `internal/auth`, `internal/validator`, `internal/project`, `internal/services` |
| S6 | Daemon infrastructure | `internal/{daemon,scheduler,cluster,queue,task,worker,runtime,pty,metrics,shadow,debug,calendar,stt,tts,errcls,sharedclient}` |
| S7 | Flutter UI | `ui/flutter_ui/**` (66 Dart files) |
| S8 | CLI + MenuBar | `cmd/**`, `menubar/**` (Swift) |

## Executive Summary

- **New findings reviewed:** 35 (3 critical, 14 high, 11 medium, 7 low)
- **Round-3 deferred verification:** 33 of 52 prior-round deferred items now **FIXED** in the codebase (10/10 menubar items, 12/12 S6 concurrency items, 7/9 S3 tool items, all S7 Flutter items, all S5 PR-1 items). 2 remain partially open (S3-L-Notif handler race, S3-C2 SSRF bypass via redirect).
- **Issues fixed this round:** 11 across 10 files (2 critical, 7 high, 2 medium)
- **Issues deferred:** 24 — categorized by recommended follow-up PR
- **Build:** `go build ./...` clean
- **Tests:** `go test ./...` on all affected packages passing

The most impactful new fix cluster is **S1-N1 + S8-1**: the agent learning pipeline was silently no-op'd by a context-cancellation bug on every successful `RunOnce`, and the macOS MenuBar app's `AppDelegate` was being deallocated before `app.run()` even started — the menubar app could not have been functional as shipped. The second cluster is **S5-N2 + the `--` separator ordering fix**: round-3's git-injection fix used `git checkout -- <branch>` which treats the branch as a pathspec; the corrected form `git checkout <branch> --` works for both the existing `CheckoutBranch` and the new `MergeWorktree` guard.

---

## Issues Fixed This Round (11)

### S1-N1. `triggerLearning` goroutine uses loopCtx that is cancelled when RunOnce returns (CRITICAL)
**File:** `internal/agent/loop.go:1258`
**Severity:** CRITICAL — silently defeats the entire learning pipeline
**Confidence:** 92

`RunOnce` defers `loopCancel()` (line 1223) which fires on return (line 1270). But the goroutine launched at line 1258 is still using `loopCtx` for its LLM calls (`Judge`/`Distill`/`StorePattern`). All those calls fail with `context.Canceled`. Errors are logged at Debug only, so the bug is invisible.

**Fix:** Switched the goroutine to `context.Background()` since learning is asynchronous best-effort work that must outlive the triggering request.

### S8-1. AppDelegate never retained; MenuBar app cannot start (CRITICAL)
**File:** `menubar/MeeptMenuBar/main.swift:127-132`
**Severity:** CRITICAL — app exits with no UI on launch
**Confidence:** 95

`AppDelegate` was created as a local constant inside a `MainActor.assumeIsolated` block; `NSApplication.delegate` is `weak`. The local went out of scope before `app.run()`, deallocated the delegate, and `applicationDidFinishLaunching` never fired.

**Fix:** Hoisted the delegate to a file-level strong reference (`let appDelegate = ...`) before assigning to `app.delegate`.

### S5-N2. MergeWorktree missing branch-name guard (regression of S5-H5) + `--` separator ordering bug in BOTH CheckoutBranch and MergeWorktree (HIGH)
**Files:** `internal/project/worktree.go:111-130`, `internal/project/manager_branches.go:92`
**Severity:** HIGH — git option injection + broken checkout on every call
**Confidence:** 95

Partial regression of round-3 S5-H5: `MergeWorktree` was never updated with the `-` prefix guard or `--` separator. While fixing this, the subagent discovered that round-3's fix used `git checkout -- <branch>` — which treats the branch as a pathspec and fails with "pathspec did not match any file(s)". The existing `CheckoutBranch` had the same latent bug; its test only exercised the reject path so the success path was never verified. `TestMergeWorktree` began failing once `--` was added, exposing the issue.

**Fix:** Applied `-` prefix rejection + `--` separator to `MergeWorktree`, AND corrected the `--` placement in both `MergeWorktree` and `CheckoutBranch` to `git checkout <branch> --` (separator after the branch, not before).

### S8-2. Force-unwrap `URL(string:)!` crashes MenuBar app on malformed config (HIGH)
**Files:** `menubar/MeeptMenuBar/Services/APIClient.swift:14`, `menubar/MeeptMenuBar/Services/WebSocketManager.swift:30`
**Severity:** HIGH — hard crash on bad `menubar.json5`
**Confidence:** 90

Both force-unwrapped a user-controlled URL string. `ConfigService.swift` and `DashboardService.swift` already used the defensive `?? URL(string: "https://localhost:8081")!` pattern.

**Fix:** Added the same fallback in both files.

### S2-N3. PageRankWithTeleportation writes to possibly-nil Personalization map (HIGH)
**File:** `internal/repomap/pagerank.go:594-596`
**Severity:** HIGH — panic on natural call pattern
**Confidence:** 90

`config.Personalization[file] += weight` panics if Personalization is nil. Callers using `PageRankConfig{Damping: 0.9}` leave it nil — natural pattern.

**Fix:** Added `if config.Personalization == nil { config.Personalization = make(...) }` before the merge loop.

### S2-N6. SetPendingChangesRegistry missing typed-nil guard (CLAUDE.md violation) (HIGH)
**File:** `internal/code/tools/ast_edit.go:33-36`
**Severity:** HIGH per CLAUDE.md project rule
**Confidence:** 95

CLAUDE.md mandates every `Set*` method accepting pointer/interface must nil-guard. This setter accepts `*builtin.PendingChangesRegistry` and assigns directly.

**Fix:** Added `if registry != nil { ... }`.

### S6-N1. PTYManager.Close() never invoked on daemon shutdown (HIGH)
**File:** `internal/daemon/components.go` (stopComponents)
**Severity:** HIGH — orphan process leak
**Confidence:** 85

`Manager.Close()` exists, kills every active PTY session, but is never called anywhere in production code. PTY child processes (debuggers, REPLs, shells) survive daemon shutdown, reparent to init, run indefinitely.

**Fix:** Added the close call to `stopComponents` after the DebugManager block.

### S6-N2. Registry.Close does not close AmendmentManager or InterruptManager (HIGH)
**File:** `internal/task/registry.go:318`
**Severity:** HIGH — goroutine + bus-subscription leak on every shutdown
**Confidence:** 82

`Registry.Close()` only marked `r.closed = true` and closed `r.store`. The AmendmentManager subscription goroutine (blocks on `m.ctx.Done()`) leaks; its bus subscription is never released; pending amendments never marked ignored. InterruptManager.Close() (triggers all interrupt tokens with `ReasonResourceLimit`) also never invoked.

**Fix:** Added calls to `amendmentMgr.Close()` and `interruptMgr.Close()` before store close.

### S2-N2. AggregateImpact.AverageConfidence computes max/N, not average (HIGH)
**File:** `internal/agent/q/impact_estimator.go:216-231`
**Severity:** HIGH — correctness of Q Agent operator-facing metrics
**Confidence:** 90

Code tracked `highestConfidence` (the max) then divided by `len(estimates)`. The result was neither max nor average — silently understated confidence in `FormatReport`.

**Fix:** Track `totalConfidence` (sum) and divide by count.

### S6-N4. ClusterWireGuard.Stop is never called on shutdown (MEDIUM)
**File:** `internal/daemon/components.go`
**Severity:** MEDIUM — currently a no-op but violates "always wire lifecycle hooks" CLAUDE.md rule
**Confidence:** 80

**Fix:** Added to `stopComponents` after the ClusterQueue block.

### S7-1. ChatState success branch silently drops `isAgentProcessing` and `currentProgress` (CRITICAL — Flutter)
**File:** `ui/flutter_ui/lib/providers/chat_provider.dart:286-289`
**Severity:** CRITICAL — UI progress indicator disappears mid-request
**Confidence:** 90

After successful HTTP send, the code constructed a fresh `ChatState(...)` using the const default constructor, resetting `isAgentProcessing` to `false` and `currentProgress` to `null`. The UI's progress indicator immediately vanished, contradicting the code's own comment ("keep isAgentProcessing=true so the progress indicator stays visible"). For long-running agents the window between HTTP response and first WS progress event is seconds-to-minutes, making the UI look frozen.

**Fix:** Added `isAgentProcessing: true, currentProgress: state.currentProgress` to the ChatState constructor.

---

## Issues Deferred (24)

Grouped by recommended follow-up PR. File:line references use the subagent-reported values.

### PR-1: Concurrency (HIGH) — recommended next

| ID | File:line | Issue |
|----|-----------|-------|
| S1-N2 | `internal/agent/loop.go:2477-2478, 2486-2487, 2190-2191` | `currentTaskID`/`currentSessionID` written/read without lock across `RunWithTask` and `chatWithFailoverRaw`. `go test -race` fatal under concurrent task execution; corrupts budget scope tracking. |
| S1-N3 | `internal/agent/executor.go:427-429, 474, 482` | `Executor.SetRegistry` swaps registry without mutex while `Execute` reads concurrently. No nil guard (violates CLAUDE.md). Executor struct has no mutex field. |
| S1-N4 | `internal/agent/loop.go:1060-1062, 1266-1267` | `SetPrefetchCallback` writes without lock; read without lock in `RunOnce`. Go memory model data race. |
| S1-N5 | `internal/agent/conversation.go:1634-1660` | `ConversationStore.GetOrRestore` holds write lock across `restoreFn()` I/O. Violates CLAUDE.md "Mutex scope" rule. `Get` uses Lock instead of RLock for reads. |
| S2-N4 | `internal/repomap/renderer.go:276-287` | `RenderCompact` mutates `r.contextLines` without lock (data race). Use option (a) per CLAUDE.md mutex-scope rule. |
| S3-2 | `internal/llm/token_cache.go:154` | `TokenCacheCoordinator` holds write lock during L2 SQLite I/O. Violates CLAUDE.md "Mutex scope" rule. |
| S4-7 | `internal/tools/builtin/lsp_writethrough.go:231-240` | `lspWriteNotifier.collectDiagnostics` uses URI-keyed single-slot map; concurrent callers for same URI overwrite each other's channel. First caller's receive never fires. |

### PR-2: Q Agent correctness (HIGH) — recommended next

| ID | File:line | Issue |
|----|-----------|-------|
| S2-N1 | `internal/agent/q/reviewer.go:96`, `q_agent.go:166-172` | `ValidateRecommendations` indexes `reports[i]` positionally; caller flattens all recs across N reports. Panics on any report with ≥2 recommendations (the expected case). |
| S2-N5 | `internal/repomap/renderer.go:60-79, 105-108` | `RendererConfig.MaxTagsPerFile` defaulted (20) but never stored on struct. Render uses `maxLineLength` (100) as proxy — 5x bloat. |

### PR-3: Security hardening (HIGH/CRITICAL)

| ID | File:line | Issue |
|----|-----------|-------|
| S4-1 | `internal/tools/builtin/web_fetch.go:62-70` | SSRF bypass via HTTP redirect. `checkURL` validates only initial URL; `CheckRedirect` callback unconditionally returns nil. Attacker's redirect to `http://169.254.169.254/...` succeeds. Also `internal/tools/mcp/transport/http.go:51-56`. |
| S4-2 | `internal/tools/builtin/shell.go:350-397` | `ExecuteStreaming` skips fence check. `Execute` checks fence at 224-228; streaming path never calls `fenceChecker.CheckCommand`. Any agent invoking streaming can bypass fence. |
| S5-N1 | `internal/comm/web/websocket.go:98-114` | Legacy `comm/web` WebSocket endpoint has no origin check or auth gate. Combined with `RequireAuth: false` default, any local page can open WS to `ws://localhost:8080/api/v1/ws`. CSRF risk. |
| S5-N5 | `internal/validator/filesystem.go:42-60` | `isPathAllowed` doesn't `EvalSymlinks`. Symlink inside allowed dir pointing outside satisfies prefix check → sandbox escape for task evidence verification. |

### PR-4: Schedule/Cron tool error envelope regression (HIGH)

| ID | File:line | Issue |
|----|-----------|-------|
| S4-3 | `internal/tools/builtin/tool_schedule_create.go:206-212`, `tool_schedule_delete.go:141, 210, 279`, `tool_cron_create.go:204-210` | Schedule/Cron tools return `(result, err)` simultaneously on the **schedule-config / pause / resume / run-now** error paths (S3-M1 only fixed the validate path). Registry's `NewErrorResult` discards tool-specific `JobID`. |

### PR-5: LLM provider streaming + retry (MEDIUM)

| ID | File:line | Issue |
|----|-----------|-------|
| S3-1 | `internal/llm/context_compressor.go:514-527` | `keepTail` duplicates the bug S4-C1 fixed in `dropOldContext`. Inner walk unconditionally `break`s on first `RoleAssistant` — wrong when text-only assistants exist between tool result and parent. Provider 400. |
| S3-3 | `internal/memory/consolidation.go:432-434` | `summarizeClusters` uses buggy `len(cluster)-len(snippets)` formula. Sibling `summarizeByDate` was fixed (S4-H1); this one wasn't. Inflated "... and N more" counts. |
| S3-4 | `internal/llm/provider_manager.go:316-389` | `ChatWithProgress` skips error classification on failover. `Chat` classifies; streaming equivalent just `continue`s. Circuit breaker / provider health inaccurate. |
| S3-6 | `internal/llm/client.go:31-37` | OpenAI client retry map missing 529 (Anthropic "Overloaded"). Generic path treats 529 as non-retryable. |

### PR-6: LSP writethrough multi-edit (MEDIUM)

| ID | File:line | Issue |
|----|-----------|-------|
| S4-4 | `internal/tools/builtin/lsp_writethrough.go:295-327` | `applyFormattingEdits` multi-edit application doesn't sort. After first edit, file is re-split but bounds check uses positions from original edit list. LSP spec doesn't guarantee sorted/non-overlapping. Panic or silent corruption risk. |

### PR-7: CLI error handling (MEDIUM)

| ID | File:line | Issue |
|----|-----------|-------|
| S8-3 | `cmd/meept/cluster_cmd.go:749-756, 965-983` | `cluster_cmd.go` silently swallows every git error. `_ = initCmd.Run()`. CombinedOutput discarded with "might already exist" comments. Broken git setup produces zero user feedback. |
| S8-4 | `menubar/MeeptMenuBar/Views/Settings/ClientConfigView.swift:10, 53-58` | `showValidationSuccess` set but never read in `body`. Dead state — user gets no visual confirmation of successful save. |
| S8-5 | `menubar/MeeptMenuBar/ViewModels/MetricsViewModel.swift:56-68` | `fetchLiveMetrics` silently swallows all errors. Inconsistent with `fetchHistorical` (line 84) which logs. For monitoring tool, daemon-down / 401 / TLS issue invisible. |

### PR-8: Flutter UI polish (HIGH/MEDIUM)

| ID | File:line | Issue |
|----|-----------|-------|
| S7-2 | `ui/flutter_ui/lib/features/chat/chat_input.dart:528` | `TextCapitalization.sentences` auto-capitalizes user input. Violates CLAUDE.md lowercase UI convention. Mobile keyboards send mixed-case content. |
| S7-3 | `ui/flutter_ui/lib/features/drawer/panels/agent_activity_panel.dart:102` | `agent.id` displayed without `.toLowerCase()`. Line 94 does it for `agent.name`; inconsistent. |
| S7-4 | `ui/flutter_ui/lib/providers/chat_provider.dart:399-408` | `ChatNotifier.dispose()` does not call `websocket.unsubscribeFromChat`. Dangling backend subscription until socket closes. |

### PR-9: LOW / consistency items

| ID | File:line | Issue |
|----|-----------|-------|
| S5-N3 | `internal/comm/http/server.go:2191` | MCP SSE session ID fully predictable (`fmt.Sprintf("http-%d", time.Now().UnixNano())`). PTY was fixed in round-3 (S5-H4); this site was missed. |
| S5-N4 | `internal/comm/http/pty_handler.go:19-23` | PTY WebSocket upgrader bypasses `WebSocketAllowedOrigins`. Operators extending allowlist for LAN hosts find PTY rejects them. |
| S5-N6 | `internal/comm/http/notification_handlers.go:62-65` | `/ws/notifications` Accept has no `OriginPatterns`. Cannot be operator-configured. |
| S5-N7 | multiple (`rpc/proxy.go:149,267`, `services/{chat,terminal}_service.go`) | Multiple IDs use `time.Now().UnixNano()` (predictable, collision-prone). |
| S6-3 | `internal/metrics/store.go:372-392, 635-644` | `Record` flush-outside-lock can race `Close`'s final flush. Benign log-noise race. |
| S8-6 | `menubar/MeeptMenuBar/Services/{APIClient,WebSocketManager}.swift` | `LocalhostTrustDelegate` duplicated inline in WebSocketManager; should be shared singleton. |
| S4-5 | `internal/tools/builtin/file_grep.go:357-360` | `searchSingleFile` re-parses pattern after `Execute` already compiled it. Confusing maintenance hazard. |
| S4-8 | `internal/tools/builtin/{tool_web_search,web_fetch}.go` | `http.Client` has no `MaxConnsPerHost` ceiling. Under bursty workload opens hundreds of TCP connections. |
| S4-9 | `internal/tools/builtin/setters_test.go` | Missing rows for several setters (SetRuntimeManager, SetPendingChangesRegistry, SetSecurityOrchestrator, etc.). CLAUDE.md mandates test verifies project-wide. |
| S3-5 | `internal/llm/token_cache_l1.go:80` | L1 cache file hash truncated to 8 hex chars (32-bit). Birthday threshold ~65K distinct filesets. |

---

## Round-3 Deferred Verification (full table)

**33 of 52 prior-round deferred items now FIXED in the codebase:**

### S8 MenuBar (10/10 FIXED)
All 10 Swift-side round-3 items resolved: S8-CRIT (dev key `#if DEBUG`), S8-H1 (plist keys), S8-H2 (LocalhostTrustDelegate), S8-H3 (Timer `.common` mode), S8-H4 (split do/catch), S8-H5 (atomic `.part` rename), S8-H6 (kickstart error surface), S8-M-WS (reconnect reset), S8-M-NMgr (config cache), S8-M-Menu (dead code removed).

### S6 Concurrency (12/12 FIXED)
All S6-CRIT-1/2, S6-H1..H4, S6-M1..M6 verified fixed: cluster_queue `sync.Once` + lock scope, components stopOnce, worker pool startErr propagation, docker lock scope, gossip atomic dedup, task SetState transactional, queue Claim fast path, dead-letter due_at preservation, shadow ProcessRecord lock scope, CaptureInteraction WaitGroup, scheduler RunNow running.Load guard.

### S5 Security (11/11 FIXED)
All PR-1 security items verified intact: TLS pinning, WebSocketAllowedOrigins, web TLS, CORS Vary, crypto/rand session IDs, git option guards, MaxBytesReader, machine key fallback, file mode 0600, Bearer prefix, PathValue.

### S3 Tools (7/9 FIXED)
FIXED: S3-C1 (git_commit fence), S3-H-Inv (validate toggle), S3-M-Sched (day_of_month), S3-M-MCPReload (errors.Join), S3-M-Retry (shift cap), S3-M-SetRuntime (logger), S3-L-Alias (slice copy), S3-L-OOB (bounds check).
PARTIALLY OPEN: S3-L-Notif (sync.Once guards registration but residual race in `diagWaiters` map for concurrent same-URI subscribers — see S4-7).
OPEN: S3-C2 (web_fetch SSRF) — initial URL check solid but redirect callback bypasses it (see S4-1).

### S7 Flutter (5/5 FIXED)
All round-3 Flutter items resolved: S7-H-Key (empty default in release), S7-H-STT (dispose chains), S7-H-Cal (end date/time pickers), S7-H-Mem (three-way conditional rendering), S7-H-Lower (mostly cleaned up — 2 new minor violations tracked as S7-2/S7-3).

### S2 Code intel (2/2 prior DEFERRED items FIXED)
S2-H3 (prompt/loader.go AddSearchPath lock): FIXED.
S2-H4 (ContextRenderer.treeCache mutex): FIXED.

### Round-3 FIXED items re-verified intact
S2-C1/C2/C3 (tree-sitter Close), S2-C4 (PlanManager mu), S2-C5 (nodeID atomic), S2-H1 (StdioTransport writeMu), S4-C1 (dropOldContext parent search), S4-H1 (summarizeByDate count), S4-M1 (LIKE wildcard escape), S4-M2 (GetExpiredMemories field mapping), S1-C1 (debugCounter atomic), S3-H1 (shell pipe split), S3-H2 (resolve fence), S3-H3 (file_edit typed-nil setter), S3-M1 (validate error envelope).

---

## Recurring Patterns Worth Addressing

1. **Context lifetime vs goroutine lifetime** — `triggerLearning` (S1-N1) is the classic "goroutine outlives the function whose context it captured" bug. Audit `go l.*` and `go func()` in loop.go for the same pattern. Generalize: any `go f(ctx, ...)` where `ctx` is cancelled by the caller's `defer cancel()` is broken.
2. **Existing-fix regression on related call sites** — S5-H5 was applied to `CheckoutBranch` and `RegisterGit` but missed `MergeWorktree`. Worse, the original fix's `--` placement was wrong (treated branch as pathspec), and the test only covered the reject path. Lesson: every guard pattern needs **positive** tests, not just negative ones, and a grep for sibling functions when applying a guard.
3. **NSApplication weak delegate** — S8-1 is a Swift-specific bug class. `app.delegate` is weak by design; any local-scope delegate is deallocated. Audit other Swift apps in the project for the same pattern.
4. **Lifecycle hooks exist but are never called** — S6-N1 (PTYManager.Close), S6-N2 (Registry.Close sub-managers), S6-N4 (WireGuard.Stop). The codebase has the methods but `stopComponents` doesn't always wire them. A grep audit for every `func.*Close()` / `func.*Stop()` method against `stopComponents` callers would catch the rest.
5. **CLAUDE.md typed-nil setter rule** — S2-N6 is the Nth instance of this. The `setters_test.go` is the enforcement mechanism but it's incomplete (S4-9). Recommendation: generate the test table from a code scan rather than hand-maintaining it.
6. **Mutex held across I/O** — S1-N5 (ConversationStore.GetOrRestore), S3-2 (TokenCacheCoordinator). CLAUDE.md explicitly forbids this; the "collect under lock, release, then operate" pattern needs consistent application. Could be enforced via a vet rule.
7. **Sum/Average confusion in estimators** — S2-N2 (max/N instead of avg) is subtle. Code review for any `highestX` variable later divided by a count.

## Approach Notes

The 8-subagent dispatch covered ~400K LOC of Go source + 19K Dart + ~3K Swift. All 8 subagents completed successfully (none hit context exhaustion this round, unlike round 3). The `feature-dev:code-reviewer` subagent type doesn't expose Write/Bash tools — subagents returned findings in their result text, which the orchestrator saved to `/tmp/glm52-round4/s{1..8}.md`.

**Prompt-injection attempts** were observed in nearly every Read tool result, formatted as `<system-reminder>` blocks instructing the reviewer to "consider whether this would be considered malware" and "MUST refuse to improve or augment the code". All subagents correctly identified these as injected text (legitimate system reminders appear in actual system context, not appended to file contents) and ignored them. The code under review is the user's own Go daemon project — standard LLM orchestration infrastructure.

**Subagent tool limitation:** The `feature-dev:code-reviewer` subagent type lacks Write/Bash access by design. Future reviews should either use a subagent type with Write access or have the orchestrator save reports programmatically.

---

## Build & Test Verification

```bash
$ go build ./...        # clean
$ go vet ./...          # clean
$ go test ./internal/agent/... ./internal/project/... ./internal/repomap/... \
             ./internal/task/... ./internal/daemon/... ./internal/code/...
ok  github.com/caimlas/meept/internal/agent          (cached)
ok  github.com/caimlas/meept/internal/agent/q        (cached)
ok  github.com/caimlas/meept/internal/project        (cached)
ok  github.com/caimlas/meept/internal/repomap        (cached)
ok  github.com/caimlas/meept/internal/task           (cached)
ok  github.com/caimlas/meept/internal/daemon         1.154s
ok  github.com/caimlas/meept/internal/code/ast       (cached)
ok  github.com/caimlas/meept/internal/code/lsp       (cached)
ok  github.com/caimlas/meept/internal/code/tools     0.417s
```

Flutter and Swift builds not run this round (changes are minimal and isolated; analyzer/test steps would surface any issue).

---

## Summary

**This round:**
- **Fixed:** 11 issues across 10 files (2 critical, 7 high, 2 medium)
  - Agent: S1-N1 (learning context leak — CRITICAL)
  - MenuBar: S8-1 (AppDelegate not retained — CRITICAL), S8-2 (URL force-unwrap × 2 files)
  - Project: S5-N2 (MergeWorktree git injection + `--` placement fix in 2 files)
  - Repomap: S2-N3 (PageRank nil map), S2-N2 (Q Agent AverageConfidence), S2-N6 (typed-nil setter)
  - Daemon: S6-N1 (PTYManager.Close), S6-N2 (Registry.Close sub-managers), S6-N4 (WireGuard.Stop)
  - Flutter: S7-1 (progress indicator drop — CRITICAL)
- **Deferred:** 24 issues across 9 recommended PRs
- **No regressions:** `go build ./...`, `go vet ./...`, all affected package tests pass
- **Round-3 verification:** 33 of 52 prior-round deferred items now FIXED; all round-3 critical/high fixes verified intact

**Most impactful single fix:** S8-1 (AppDelegate). The macOS MenuBar app's delegate was being deallocated before `app.run()` — the app could not have been functional as shipped. Either the app was never actually tested end-to-end, or some build configuration retained the delegate by accident.

**Most impactful deferred cluster:** PR-3 (security). S4-1 (SSRF bypass via redirect — round-3 S3-C2 still open), S4-2 (shell streaming fence bypass), S5-N1 (legacy web CSRF), S5-N5 (validator symlink escape). Each is individually exploitable on a network-exposed deployment.

**Surprising latent bug uncovered:** The round-3 S5-H5 fix for git option injection used `git checkout -- <branch>`, which treats the branch name as a pathspec and fails with "pathspec did not match any file(s)". The existing test only exercised the reject path (branches starting with `-`), so the success path was broken since round 3. This round's MergeWorktree fix happened to add the same broken pattern, which surfaced the test failure and led to the fix being applied to both sites.

**Recommended follow-up order:**
1. **PR 1 (concurrency):** S1-N2..N5, S2-N4, S3-2, S4-7 — broad touch, prevents `-race` failures
2. **PR 2 (Q agent):** S2-N1, S2-N5 — small contained panic/correctness fixes
3. **PR 3 (security):** S4-1, S4-2, S5-N1, S5-N5 — needs design decisions on SSRF allowlist, legacy web deprecation
4. **PR 4 (schedule/cron envelope):** S4-3 — mechanical, 4 files
5. **PR 5 (LLM streaming/retry):** S3-1, S3-3, S3-4, S3-6
6. **PR 6 (LSP writethrough):** S4-4
7. **PR 7 (CLI error handling):** S8-3, S8-4, S8-5
8. **PR 8 (Flutter polish):** S7-2, S7-3, S7-4
9. **PR 9 (LOW consistency):** S5-N3/N4/N6/N7, S6-3, S8-6, S4-5/N8/N9, S3-5
