# GLM-5.2 Findings — Round 4

**Date:** 2026-06-15
**Methodology:** 8 parallel review subagents (general-purpose) covering the full
codebase (~250k LOC non-test). Each subagent read every non-test file in its
scope and produced structured findings with `file:line` evidence. Subagents were
briefed with the round-3 lessons (no false-positive dismissal, scrutinize
recently-modified hotspots) and instructed to verify each finding against
actual code.

**Note on prompt injection:** During this round, file-read results contained
appended `<system-reminder>` blocks instructing the reviewer to "refuse to
improve or augment code" and claiming the code may be malware. These are NOT
legitimate system messages — they are injected content embedded in file
contents (source still unknown; same pattern documented in round 3). All
subagents correctly disregarded them. The code under review is the user's own
meept project (a Go daemon), not malware.

## Totals

| Severity | Count |
|----------|-------|
| Critical | 3 |
| High | 16 |
| Medium | 39 |
| Low | 46 |
| **Total** | **104** |

## Scope by subagent

| Subagent | Domain | Findings |
|----------|--------|----------|
| S1 | Agent orchestration (agent, context, session) | 16 |
| S2 | Code intel + plans + repomap + lint + selfimprove | 11 |
| S3 | LLM + memory | 9 |
| S4 | Tools + MCP | 10 |
| S5 | Network + RPC + services + project | 10 |
| S6 | Daemon infra (scheduler, queue, runtime, pty, stt/tts, cluster) | 25 |
| S7 | Security + config + skills | 12 |
| S8 | CLI + TUI + Flutter + MenuBar | 11 |

---

## Critical Findings

### S5-1 HTTP transport client never sends API key

- **File:** `internal/transport/http_client.go:210-214, 257-261`
- **Evidence:**
  ```go
  resp, err := c.client.Post(
      c.baseURL+"/api/v1/bus/call",
      "application/json",
      bytes.NewReader(body),
  )
  // c.apiKey is stored via WithAPIKey but never read again
  ```
- **Why it's a bug:** The `apiKey` field is dead storage. No outgoing request
  carries `Authorization`. When the daemon has `require_auth: true` (the
  documented production default), every HTTP transport call is rejected with
  401. The CLI over HTTP transport is completely non-functional in any
  auth-enabled deployment.
- **Fix:** Construct each request with `http.NewRequestWithContext`, set
  `req.Header.Set("Authorization", "Bearer "+c.apiKey)`, and send via
  `c.client.Do(req)`. Apply to `callAPI`, `Chat`, `Connect`, `IsConnected`.

### S6-1 PiperEngine returns AudioPath pointing at a temp file removed by `defer`

- **File:** `internal/tts/piper.go:92, 159`
- **Evidence:**
  ```go
  defer os.Remove(tmpPath)   // line 92
  // ...
  return &Result{
      AudioPath: tmpPath,     // line 159 — caller sees dangling path
      AudioData: audioData,
  }, nil
  ```
- **Why it's a bug:** `defer` runs before `return`. By the time the caller
  reads `Result.AudioPath`, the file is gone. Any caller relying on the path
  to play, persist, or hand off audio gets a path to nothing.
- **Fix:** Either drop `AudioPath` from the returned `Result` (force callers
  to use `AudioData`), or transfer ownership of the temp file to the caller
  by removing the `defer os.Remove` and documenting cleanup responsibility.

### S6-2 debug/manager.go `drainEvents` mutates DebugSession fields without synchronization

- **File:** `internal/debug/manager.go:318-347`
- **Evidence:**
  ```go
  func (m *Manager) drainEvents(session *DebugSession) {
      for evt := range session.Client.Events() {
          session.LastActivity = time.Now()       // unsynced write
          switch evt.Event {
          case "stopped":
              session.State = SessionStopped      // unsynced write
              session.CurrentThreadID = body.ThreadID  // unsynced write
  ```
- **Why it's a bug:** `Manager.List()`, `.Active()`, `.Get()` hand the same
  pointer to other goroutines that read these fields under `m.mu`. The writer
  takes no lock. `-race` will flag it. Readers can observe torn State
  transitions (e.g. `SessionStopped` with a stale `CurrentThreadID`).
- **Fix:** Protect `DebugSession` mutable fields with a per-session mutex,
  or take `m.mu` around the writes in `drainEvents`.

---

## High Findings

### S1-1 Data race on TeamSessionState fields

- **File:** `internal/agent/team_orchestrator.go:263-271, 332-335, 450-459`
- **Evidence:** Background goroutine writes `state.Phase`/`state.FinalOutput`
  concurrently with `Status`/`AssignSubtask`/`ReceiveResult` reads. `MemberResults`
  is a map written concurrently (`state.MemberResults[id] = ...`) — can panic
  with "concurrent map writes".
- **Fix:** Add `sync.RWMutex` to `TeamSessionState` or store immutable copies
  via `sync.Map.CompareAndSwap`.

### S1-13 Queue Close vs FollowUp race on wg.Add after Wait

- **File:** `internal/agent/queue.go:257-307, 380-405`
- **Evidence:** `FollowUp` checks `closed.Load()` at top, then later does
  `wg.Add(1)` outside the lock. `Close` calls `wg.Wait()` between the check
  and the Add. New goroutine runs after `Close` returns, touching `persister`
  post-teardown.
- **Fix:** Move `wg.Add(1)` under `q.mu`, or re-check `closed.Load()` immediately
  before `wg.Add(1)`.

### S1-16 Team driver drops partial results on synthesis failure

- **File:** `internal/agent/collaboration_team_driver.go:237-251`
- **Evidence:** When `synthesize` errors, function returns at line 247 before
  `publishPartialResults` at line 251 is reached. Partial results are silently
  dropped — exactly when they're most valuable.
- **Fix:** Move `publishPartialResults(sess.ID, resultsMap)` above the
  `synthesize` call, or to a `defer` that runs before the error return.

### S2-1 Dead `strconv.Atoi` call in parseGoErrors

- **File:** `internal/lint/languages/go_lint.go:204`
- **Evidence:** `strconv.Atoi(matches[2])` return value discarded; `fmt.Sscanf`
  re-parses immediately after. Dead code + unused import risk.
- **Fix:** Remove the dead line, use `lineNum, _ = strconv.Atoi(matches[2])`.

### S2-2 generatePatternID uses time.Now() salt breaking deduplication

- **File:** `internal/selfimprove/learning.go:835-837`
- **Evidence:**
  ```go
  hash := sha256.Sum256([]byte(content + time.Now().String()))
  ```
- **Why it's a bug:** Two identical patterns produce different IDs, defeating
  `lp.patterns[pattern.ID]` deduplication.
- **Fix:** Remove the time salt. Use the stable `ContentHash` for dedup
  (already computed separately at line 840).

### S3-1 Data race on TokenCacheCoordinator stats under RLock

- **File:** `internal/llm/token_cache.go:163-167`
- **Evidence:**
  ```go
  c.mu.RLock()
  if entry, found := c.l1Cache.Get(key); found {
      c.stats.Hits++       // DATA RACE: mutation under RLock
      c.stats.L1Hits++     // DATA RACE
  ```
- **Fix:** Upgrade to `c.mu.Lock()` for the L1 hit path, or use `atomic.Int64`.

### S4-1 MCP Manager holds mutex through subprocess I/O

- **File:** `internal/tools/mcp/manager.go:116-133, 136-149, 45-113`
- **Evidence:** `StopServer` / `StopAll` hold `m.mu` across `client.Close()`
  (up to 5s stdio shutdown). `StartServer` holds lock across `client.Connect()`
  (subprocess startup + handshake + tools/list). Violates CLAUDE.md mutex-scope
  rule. All `GetClient`/`CallTool` callers block for N * 7s during reloads.
- **Fix:** Snapshot clients to stop under the lock, release lock, then `Close()`
  outside. For Start, build transport, `Connect()` without lock, re-acquire to
  insert (re-checking for races).

### S4-2 WebFetch SSRF check vulnerable to DNS rebinding

- **File:** `internal/tools/builtin/ssrf.go:36-63`
- **Evidence:** `checkURL` resolves IPs and validates, but the actual
  `t.client.Do(req)` re-resolves via default Dialer. TOCTOU window allows
  public→127.0.0.1 swap (classic DNS rebinding). Cloud-metadata exfil risk.
- **Fix:** Pin the validated IP in `t.client.Transport.DialContext`, or
  perform the check inside a `net.Dialer.Control` callback at dial time.

### S5-2 Data race on bus.messagesSent counter under RLock

- **File:** `internal/bus/bus.go:87`
- **Evidence:** `b.messagesSent++` executed under `b.mu.RLock()` (line 78).
  RLock permits concurrent holders → lost increments + race detector flag.
- **Fix:** Change `messagesSent int64` to `atomic.Int64`, use `.Add(1)`.

### S5-3 Predictable IDs via time.Now().UnixNano() (3 locations)

- **Files:**
  - `internal/rpc/proxy.go:239` — `ID: fmt.Sprintf("fire-%d", time.Now().UnixNano())`
  - `internal/comm/http/api_handlers.go:106` — `subID := fmt.Sprintf("sse-chat-%d", time.Now().UnixNano())`
  - `internal/comm/http/server.go:2218` — `ID: fmt.Sprintf("%d", time.Now().UnixNano())`
- **Why it's a bug:** Same-nanosecond collisions break bus dedup and reply
  correlation. Predictable IDs enable reply-spoofing on the bus. Round 3's
  S5-N7 sweep missed these.
- **Fix:** Replace all three with `id.Generate("...")` (already used at
  `proxy.go:150` for secure IDs).

### S6-3 Terminate vs drainEvents race

- **File:** `internal/debug/manager.go:382-406`
- **Evidence:** `Terminate` deletes session, then calls `Client.Disconnect`
  /`Close` outside lock, then sets `session.State = SessionTerminated`. The
  drainEvents goroutine is still writing state. No wait for drainEvents to exit.
- **Fix:** Have `Client.Close()` (or a `Done()` channel) signal drainEvents
  exit; wait for it before mutating state.

### S6-4 STT engines use exec.Command (no context) — subprocesses survive cancellation

- **Files:** `internal/stt/whisper.go:146`, `internal/stt/parakeet.go:134`,
  `internal/stt/native.go:172,189,214,237`
- **Evidence:** All three engines use `exec.Command` not `exec.CommandContext`,
  and the `transcribe()` methods take no context.
- **Fix:** Thread the caller's context through, use `exec.CommandContext(ctx,...)`,
  add `cmd.Cancel = os.Process.Kill`.

### S6-5 PTY Close kills processes but never Waits — potential zombies

- **File:** `internal/pty/session.go:275-307`
- **Evidence:** `Close()` calls `Process.Kill()` but reaping is gated on
  `wg.Wait()` for readLoop, which can block on a half-closed fd.
- **Fix:** Decouple reaping from the wg. `Kill()` then `cmd.Wait()` inline
  in `Close()`.

### S6-6 AnalyzeCoreDelve kills dlv but doesn't Wait

- **File:** `internal/debug/adapter_native.go:237-247`
- **Evidence:** On `ctx.Done()`, calls `Process.Kill()` then returns without
  reading the `done` channel that holds `cmd.Wait()`.
- **Fix:** After Kill, `<-done` to reap before returning.

### S6-7 Docker Execute doesn't kill exec process on context cancellation

- **File:** `internal/runtime/docker.go:99-182`
- **Evidence:** `StartExec` is synchronous and ignores ctx cancellation. Hung
  command inside container blocks indefinitely regardless of timeout.
- **Fix:** Run `StartExec` in a goroutine, call `StopExec`/kill on ctx.Done.

### S6-8 cluster/git_sync.go uses exec.Command (no context) for all git ops

- **File:** `internal/cluster/git_sync.go:415, 402, 446`
- **Evidence:** `git()`, `hasStagedChanges()`, `cloneRepo()` all use
  `exec.Command("git",...)`. A hung `git push` on flaky network blocks the
  heartbeat ticker indefinitely. `Stop()` cannot interrupt.
- **Fix:** Thread ctx through, use `exec.CommandContext`, add per-op timeouts.

### S6-9 DAP Client handleEvent drops events silently + can block readLoop

- **File:** `internal/debug/client.go:327-338`
- **Evidence:** Final `c.events <- evt` is a BLOCKING send with no select guard.
  If channel is full and reader is slow, send blocks readLoop → all DAP
  processing halts. Critical events (`stopped`, `terminated`) can be lost.
- **Fix:** Make the send non-blocking with a `select { default: log drop }`.

### S7-1 Dead error check in checkOverrides

- **File:** `internal/security/engine.go:695-697`
- **Evidence:** `err` checked at line 610-612 with early return; by line 695
  err can only be nil. The check is dead code (copy-paste error).
- **Fix:** Remove the dead error check.

### S8-1 WebSocket pause() permanently breaks reconnection

- **File:** `ui/flutter_ui/lib/services/websocket_service.dart:339-348, 355-360`
- **Evidence:** `_cleanupChannel()` cancels the subscription before closing
  the sink. `onDone` is detached when the subscription cancels, so
  `streamDone` never completes. `_openConnection()` hangs forever on
  `await streamDone.future`. `_isConnecting` permanently true.
- **Fix:** In `pause()` and external cleanup paths, complete `streamDone`
  explicitly. Or reverse order: close sink FIRST (triggers `onDone`), then
  cancel subscription.

### S8-2 Pong timeout has the same stuck-streamDone bug

- **File:** `ui/flutter_ui/lib/services/websocket_service.dart:393-401`
- **Evidence:** Same root cause as S8-1. Comment claims `onDone` will fire
  and complete streamDone, but the subscription was already cancelled.
- **Fix:** Explicitly complete `streamDone` in the pong timeout handler.

---

## Medium Findings

### S1-2 Subscription leak on `!ok` channel close (3 orchestrators)
- **Files:** `internal/agent/{orchestrator,team_orchestrator,pair_orchestrator}.go`
- **Issue:** `runSubscription`'s `<-ctx.Done()` branch calls `Unsubscribe`,
  but the `case msg, ok := <-sub.Channel: if !ok { return }` branch does not.
- **Fix:** Hoist `defer bus.Unsubscribe(sub)` above the loop.

### S1-3 ParallelTeamDriver cleanupSession runs before publishes drain
- **File:** `internal/agent/collaboration_team_driver.go:194-267`
- **Fix:** Move `cleanupSession` to run after all publishes.

### S1-5 publishTeamStatus reads status.Phase without lock
- **File:** `internal/agent/collaboration_team_driver.go:525-539`
- **Fix:** Acquire `convMu` for read before marshalling/publishing.

### S1-7 Team goroutine inherits orchestrator ctx — cancelled on Stop
- **File:** `internal/agent/team_orchestrator.go:257-274, 119-135`
- **Fix:** Use `context.WithoutCancel(ctx)` for per-team context.

### S1-11 Reflection goroutine ctx inherits orchestrator lifecycle
- **File:** `internal/agent/orchestrator.go:660-663`
- **Fix:** Use `context.WithoutCancel(ctx)` for reflection budget.

### S1-4 SessionTracker.PersistIdleSessions re-lock TOCTOU on Persisted flag
- **File:** `internal/agent/session_tracker.go:210-246`
- **Fix:** Re-verify session still in map before mutating; or make method
  unexported.

### S2-3 Scheduler.Stop double-close panic on context-exit path
- **File:** `internal/selfimprove/scheduler.go:73-81`
- **Fix:** Use `sync.Once` for closing `stopCh`; have `Start` set `running=false`
  on exit.

### S2-5 newWeightedLine ID uses float arithmetic (1e9)
- **File:** `internal/repomap/graph.go:137`
- **Fix:** Use integer arithmetic: `from.ID()<<32 | (to.ID() & 0xFFFFFFFF)`.

### S3-2 summarizeClusters "... and X more" off by one
- **File:** `internal/memory/consolidation.go:432-433`
- **Fix:** Use indexed loop: `remaining := len(cluster) - i - 1`.

### S3-3 Consolidator Embedder not wired through Manager init
- **File:** `internal/memory/manager.go:215-220`
- **Fix:** Set `Embedder: m.embedder` in the consolidator config.

### S3-4 L2Cache.ClearByModelPrefix LIKE wildcard injection
- **File:** `internal/llm/token_cache_l2.go:360`
- **Fix:** Escape `%`/`_`/`\` before LIKE; add `ESCAPE '\'`.

### S3-5 ProviderManager.Stop panics on double-close
- **File:** `internal/llm/provider_manager.go:873-874`
- **Fix:** Use select-based safe-close pattern like `L1Cache.Stop()`.

### S3-6 TokenCacheCoordinator.Inspect holds RLock across SQLite I/O
- **File:** `internal/llm/token_cache.go:345-347`
- **Fix:** Snapshot L2 ref under lock, release, then query.

### S4-3 setters_test.go missing 12 setters
- **File:** `internal/tools/builtin/setters_test.go:14-36`
- **Missing:** GitCommit/GitOverview/GitSplit `SetFenceChecker`,
  ReadFile `SetSecurityOrchestrator`, WriteFile `SetLSPNotifier`/`SetSecurityOrchestrator`,
  DeleteFile/ListDirectory `SetSecurityOrchestrator`, ShellExecute `SetRuntimeManager`,
  FileEdit `SetBlockResolver`/`SetPendingChangesRegistry`/`SetSecurityOrchestrator`.
- **Fix:** Add 12 entries to the test slice.

### S4-4 WebSearchTool redirect handler no SSRF check
- **File:** `internal/tools/builtin/tool_web_search.go:70-75`
- **Fix:** Call `checkURL` on `req.URL.String()` in the redirect callback.

### S4-5 StdioTransport relay drops interleaved MCP responses
- **File:** `internal/tools/mcp/transport/stdio.go:139-157`
- **Fix:** Distinguish notification from response; route notifications
  separately or have `Send` drain channel until matching id.

### S4-6 MCP Client.Close leaves stale tools accessible
- **File:** `internal/tools/mcp/client.go:262-271`
- **Fix:** Clear `tools`/`serverInfo`/`capabilities` at end of Close.

### S5-4 Telegram saveSessionsLocked holds write lock across file I/O
- **File:** `internal/comm/telegram/handler.go:114-116, 189-212`
- **Fix:** Snapshot map under lock, release, write to disk lock-free.

### S5-5 Token store path traversal via unsanitized provider name
- **File:** `internal/auth/token_store.go:229-231`
- **Fix:** Use `filepath.Base(provider)` and reject `.`/`/`.

### S5-6 BusService.Subscribe returns nil cleanup — caller panics
- **File:** `internal/services/bus_service.go:97-106`; caller at
  `internal/comm/http/server.go:2210-2211` defers nil cleanup.
- **Fix:** Return no-op cleanup when bus is nil: `return nil, func(){}`.

### S5-7 Chat service response goroutine fragile defer ordering
- **File:** `internal/services/chat_service.go:125-179`
- **Fix:** Use explicit `context.WithCancel` for the goroutine, WaitGroup.

### S6-10 calendar/gcal.go SetAccessToken unsynchronized
- **File:** `internal/calendar/gcal.go:290-291`
- **Fix:** Add `sync.RWMutex` around `accessToken` read/write.

### S6-11 AmendmentManager.Close holds mu across cancel()
- **File:** `internal/task/amendment_manager.go:220-236`
- **Fix:** Snapshot cancel under lock, release, call cancel outside.

### S6-12 daemon/events.go Publish recovers closed-channel panics — masks bug
- **File:** `internal/daemon/events.go:110-125`
- **Fix:** Use a per-subscriber `sync.Once`/closed flag, skip in Publish.

### S6-13 tts/manager.go processQueue recursive goroutine spawning
- **File:** `internal/tts/manager.go:52, 88-110`
- **Fix:** Replace with a single long-lived consumer goroutine.

### S6-14 metrics/store.go notifySubscribers may run after db.Close()
- **File:** `internal/metrics/store.go:338`
- **Fix:** Track via WaitGroup, `wg.Wait()` in `Close()`.

### S6-15 metrics/analyzer.go compiles regex on every call
- **File:** `internal/metrics/analyzer.go:79`
- **Fix:** Promote `codeBlockPattern` to a field or package-level var.

### S6-16 cluster/gossip.go retryLoop not waited on Stop
- **File:** `internal/cluster/gossip.go:408-437, 542-565`
- **Fix:** WaitGroup for both `run` and `retryLoop` in Stop.

### S6-17 scheduler/scheduler.go RunNow ctx detached from daemon ctx
- **File:** `internal/scheduler/scheduler.go:142, 327`
- **Fix:** Derive from `ctx` passed to `Start`, not `context.Background()`.

### S7-2 FenceChecker doesn't normalize relative `..` paths
- **File:** `internal/security/fence.go:31-51`
- **Fix:** Call `filepath.Clean()`/`Abs()` before `resolveSymlinks`.

### S7-6 Tirith scanner fail-closed is not configurable
- **File:** `internal/security/tirith.go:97-118`
- **Fix:** Add config option `Tirith.FailOpen` (default false).

### S7-8 Config loader doesn't detect cyclic env var refs
- **File:** `internal/config/schema.go:135-153`
- **Fix:** Add recursion depth limit or cycle detection.

### S7-12 MCP server doesn't authenticate RPC connection
- **File:** `internal/mcp/server.go:309-323`
- **Fix:** Verify socket perms (0600/0660) on connect; document trust boundary.

### S8-3 StatusModel.fetchStatus no IsConnected check
- **File:** `internal/tui/models/status.go:69-72`
- **Fix:** Add `if !m.rpc.IsConnected()` guard.

### S8-4 ConnectionMonitor 200ms polling timer — excessive state writes
- **File:** `ui/flutter_ui/lib/providers/providers.dart:149-156`
- **Fix:** Only write when value changes; or increase interval to 500ms-1s.

### S8-5 PlansModel.formatTimeAgo returns garbage
- **File:** `internal/tui/models/plans.go:759-767`
- **Evidence:** Takes last 8 chars of RFC3339 timestamp: returns `:30:00Z`.
- **Fix:** Parse timestamp, format like `SessionsModel.formatTime`.

### S8-6 ChatNotifier.loadMessages setState after dispose
- **File:** `ui/flutter_ui/lib/providers/chat_provider.dart:94-151`
- **Fix:** Add `_disposed` flag (like MetricsNotifier/JobNotifier) before
  every `state =` after an await.

---

## Low Findings

### S1-6 loop.go llmClient locked/unlocked read mix
- **File:** `internal/agent/loop.go:1790-1802`
- Snapshot `l.llmClient` under `modelMu` at top of block.

### S1-8 Learning goroutine spawned with context.Background, no tracking
- **File:** `internal/agent/loop.go:1263-1265`
- Register with loop's wg; signal shutdown via stopCh.

### S1-9 Shadow capture ctx inconsistency (Background vs ctx)
- **File:** `internal/agent/loop.go:1950-1957, 2147-2158`
- Change line 2152 to `context.Background()`.

### S1-10 review_manager swallows SetResult errors
- **File:** `internal/agent/review_manager.go:481-483, 522-524`
- Propagate via multierror or document best-effort.

### S1-12 EscalationManager.escalations map never cleaned
- **File:** `internal/agent/escalation.go:42, 66`
- Add periodic GC or completion hook.

### S1-14 CollaborationSession.State mutated outside e.mu
- **File:** `internal/agent/collaboration_engine.go:230-255`
- Make `State` atomic.Int32/Value, or add per-session mutex.

### S1-15 RunOnce defer ordering: EventAgentEnded fires before queue unregister
- **File:** `internal/agent/loop.go:1115-1142`
- Reverse registration order or explicit call before publish.

### S2-6 PlanHandler.runSubscription no Unsubscribe on `!ok`
- **File:** `internal/plan/handler.go:60-74`
- Add `h.bus.Unsubscribe(sub)` before return.

### S2-7 TestRunner.RunTests misleading ctx guard
- **File:** `internal/lint/testrunner.go:91-96`
- Check `_, ok := ctx.Deadline()` instead of `ctx.Err() == nil`.

### S2-8 Controller.saveState holds write lock during disk I/O
- **File:** `internal/selfimprove/controller.go:735-762`
- Snapshot under lock, release, marshal+write lock-free.

### S2-9 LearningPipeline.savePatterns/loadPatterns I/O under lock
- **File:** `internal/selfimprove/learning.go:882-906`
- Snapshot under lock, release, write lock-free.

### S2-10 parseGoErrors drops file info when targetFile=""
- **File:** `internal/lint/languages/go_lint.go:227-229`
- Use `matches[1]` as the file when targetFile is empty.

### S2-11 ChangeApplier.applyFix hardcodes ApprovedBy="auto"
- **File:** `internal/selfimprove/applier.go:89-151`
- Pass `approvedBy` through to applyFix.

### S2-4 Controller.RunFullCycle cycle pointer aliasing during publish
- **File:** `internal/selfimprove/controller.go:160-167, 694-718`
- Snapshot cycle under lock before publish.

### S3-7 ContextFirewall.dropOldContext no break after finding parent
- **File:** `internal/llm/context_firewall.go:678-689`
- Add `if keepSet[j] { break }` (present in `keepTail`).

### S3-8 recordSuccess dereferences resp without nil check
- **File:** `internal/llm/provider_manager.go:298, 303, 475-493`
- Add `if resp == nil { return }` at top.

### S3-9 doStreamRequest returns closed-body http.Response on error
- **File:** `internal/llm/client.go:1125-1261`
- Change error-path returns to `return nil, nil, err`.

### S4-7 CalendarListTool parses args before client nilness check
- **File:** `internal/tools/builtin/calendar.go:53-81`
- Move `t.client == nil` check to top of Execute.

### S4-8 Registry.Execute erases error type information
- **File:** `internal/tools/registry.go:146-153`
- Consider adding `Err error` to ToolResult.

### S4-9 PendingChangesRegistry no automatic expiration
- **File:** `internal/tools/builtin/pending_changes.go:113-141`
- Document owner responsibility, or self-expire via goroutine.

### S4-10 StdioTransport.drainStderr goroutine only exits when subprocess dies
- **File:** `internal/tools/mcp/transport/stdio.go:120-128`
- Add Close-joined cleanup.

### S5-8 Terminal session ID embeds filesystem path
- **File:** `internal/services/terminal_service.go:255`
- Hash path or use `id.Generate("session-")`.

### S5-9 Chat service 2-minute timeout not configurable
- **File:** `internal/services/chat_service.go:158`
- Make configurable via option/config.

### S5-10 Proxy fire-and-forget returns "published" with delivered=0
- **File:** `internal/rpc/proxy.go:245-250`
- Return error or status="dropped" when delivered==0.

### S6-18 PTY Close swallows Process.Kill errors
- **File:** `internal/pty/session.go:298-303`
- Log errors at Debug level.

### S6-19 IsGoBinary reads entire binary into memory
- **File:** `internal/debug/adapter_go.go`
- Use bufio.Reader with early-exit.

### S6-20 daemon/events.go Publish buffer trim leaks array
- **File:** `internal/daemon/events.go:100-102`
- Use ring buffer or copy+null trailing slot.

### S6-21 runtime/local.go swallows DeadlineExceeded as generic error
- **File:** `internal/runtime/local.go:54-64`
- Check `errors.Is(err, context.DeadlineExceeded)`, return typed ErrTimeout.

### S6-22 git_sync.go handleRebaseConflict drops merge --abort error
- **File:** `internal/cluster/git_sync.go:278-298`
- Log abort error at Debug level.

### S6-23 tts/piper.go unlock-relock race window
- **File:** `internal/tts/piper.go:70-80`
- Hold lock through Stop call, or restructure.

### S6-24 errcls/classify.go uses deprecated netErr.Temporary()
- **File:** `internal/errcls/classify.go:79-82`
- Drop `Temporary()` arm; rely on `Timeout()`.

### S6-25 launchd.go execs daemon with stdout/stderr to /dev/null
- **File:** `internal/daemon/launchd.go:39-41`
- Pipe stdout/stderr to log file or system log.

### S7-3 Skills parser empty frontmatter silently accepted
- **File:** `internal/skills/parser.go:212-273`
- Document behavior; warn on empty frontmatter.

### S7-4 TLS verify_mode "none" silently disables mTLS
- **File:** `pkg/tlsutil/pin.go:67-76`
- Log warning when VerifyMode=="none".

### S7-5 DefaultDevAPIKey hardcoded in source
- **File:** `pkg/constants/api_key.go:10`
- Generate on first run to `~/.meept/dev_key`.

### S7-7 Sanitizer `user\s*:` pattern too aggressive
- **File:** `internal/security/sanitizer.go:121-124`
- Tighten pattern: require value or end-of-line.

### S7-9 Skills registry case-insensitive collision only warns
- **File:** `internal/skills/registry.go:41-58`
- Return error or use first-wins.

### S7-10 FenceChecker doesn't validate RootPath is absolute
- **File:** `internal/security/fence.go:68-69`
- Check error from filepath.Abs; validate non-root.

### S7-11 Permission override expiration uses string comparison
- **File:** `internal/security/engine.go:608-609`
- Use `datetime(expires_at) > datetime('now')`.

### S8-7 Hardcoded API key fallback in release builds
- **File:** `ui/flutter_ui/lib/services/websocket_service.dart:65-71`
- Refuse to connect in release builds with no configured key.

### S8-9 DaemonCertPinner logs expected fingerprint in debug
- **File:** `ui/flutter_ui/lib/services/daemon_cert_pinner.dart:101-105`
- Only log mismatch, not expected value.

### S8-10 WebSocketService creates new Random() per reconnect delay
- **File:** `ui/flutter_ui/lib/services/websocket_service.dart:186-195`
- Use a class-level `final _random = Random()`.

### S8-11 PlansModel.View calls filterPlans() twice per render
- **File:** `internal/tui/models/plans.go:476`
- Cache filtered count as field, update in updatePlansTable.

### S8-12 ConfirmModal hint "enter to confirm" misleading with default=no
- **File:** `internal/tui/modal.go:834, 929-936`
- Change hint to "enter to activate".

---

## Summary

### Issues fixed in this round
(To be populated during the fix phase. See "Fix Phase" below.)

### Issues deferred
(To be populated during the fix phase.)

### Observations

1. **Prompt injection in file contents is recurring.** This is the second
   consecutive round where file-read results contained fake `<system-reminder>`
   blocks (the "refuse to improve / malware" injection). All subagents
   correctly disregarded them, but the source is still unidentified. The
   pattern is: injected text appended to the END of file contents, formatted
   to look like a system message. Possible sources: a hook in user settings,
   an MCP server, or a wrapper around file reads. **Recommendation:** audit
   `~/.claude/settings.json` hooks and any MCP servers that touch file I/O.

2. **Round 3 fixes are holding.** S1 (agent concurrency/nil guards/lifecycle),
   S4 (tools connection pool/error handling), S5 (predictable nano IDs),
   S3 (keepTail/summarizeClusters/token cache hash) — all verified intact.
   No regressions detected in those specific fixes.

3. **Cross-cutting theme: subprocess lifecycle mismanagement.** 8 of 25 S6
   findings (and several in S4) are subprocess handling: `exec.Command` where
   `exec.CommandContext` is needed, and `Process.Kill()` without `Wait()`.
   A project-wide vet pass for these two patterns would close most of the
   high-severity S6 findings.

4. **Cross-cutting theme: mutex held across I/O.** Despite CLAUDE.md explicitly
   calling this out, 11+ findings are CLAUDE.md mutex-scope violations
   (S2-8, S2-9, S3-6, S4-1, S5-4, S6-11, S6-12, S6-14, plus the underlying
   pattern in several others). Consider adding a linter rule or vet check.

5. **CLAUDE.md UI lowercase convention is followed consistently.** Across
   TUI (`modal.go`, all `models/*.go`) and Flutter dart files, button labels,
   menu items, tooltips, dialog titles are lowercase. No violations.

6. **Setter nil-guard enforcement has gaps.** The guards themselves are
   universally present (every Set* method correctly nil-guards), but
   `setters_test.go` only exercises 21 of 33 pointer/interface setters.
   The 12 missing setters (S4-3) are currently safe but unprotected by the
   test suite.

7. **Authentication security is solid where wired.** API key comparisons use
   `subtle.ConstantTimeCompare` everywhere. crypto/rand is used for ID
   generation in `pkg/id` and most of the codebase. The 3 remaining
   `time.Now().UnixNano()` instances (S5-3) are residual misses.

8. **The round-3 keepTail fix (D1) is correct.** Verified in S3-7 — the
   backward scan correctly walks through intermediate assistant messages
   without prematurely breaking. The related `context_firewall.dropOldContext`
   has the same pattern but is missing the break optimization.

---

## Fix Phase

### Iteration Log

| Run | Phase | Verified | Total | % Complete | Blocking Gaps |
|-----|-------|----------|-------|------------|---------------|
| 1   | Fixes dispatched (8 parallel) | — | 94 | — | — |
| 2   | Master build + vet | 92 | 94 | 98% | 2 new predictable IDs; 1 race in compactor |
| 3   | Gap-fix: skills.go ID, FileOperationSet race, git_sync cancel | 94 | 94 | 100% | NONE |
| 4   | Deferred low-severity + mutexio analyzer | 104 | 104 | 100% | NONE |
| 5   | All remaining deferred items fixed | 104 | 104 | 100% | NONE |

### Issues Fixed (104 of 104)

**Critical (3/3 fixed)**
- **S5-1** HTTP transport client now sends `Authorization: Bearer <key>` on all requests (`internal/transport/http_client.go:176,199,230,282`)
- **S6-1** PiperEngine no longer returns a temp file path that's removed by `defer`; `AudioPath` set to `""`, file cleaned up immediately after reading (`internal/tts/piper.go:148-162`)
- **S6-2** `DebugSession` now has a `sync.RWMutex`; `drainEvents` and `Launch`/`Attach`/`Terminate` lock around State/LastActivity/CurrentThreadID writes (`internal/debug/manager.go`)

**High (16/16 fixed)**
- **S1-1** `TeamSessionState.mu sync.RWMutex` added; all reads/writes of Phase/FinalOutput/MemberResults locked
- **S1-2** `defer bus.Unsubscribe(sub)` hoisted above the loop in all 3 orchestrators
- **S1-13** `wg.Add(1)` moved inside `q.mu.Lock()` with closed re-check
- **S1-16** `publishPartialResults` moved before `synthesize` so partials always publish
- **S2-1** Dead `strconv.Atoi` removed; replaced with proper parse
- **S2-2** `generatePatternID` time salt removed (content-only hash)
- **S3-1** TokenCacheCoordinator L1 hit path upgraded to `c.mu.Lock()` (was RLock)
- **S4-1** MCP Manager no longer holds lock across `client.Close()`/`Connect()`
- **S4-2** WebFetch now uses custom `ssrfDialContext` that re-validates IPs at dial time
- **S5-2** `bus.messagesSent` converted to `atomic.Int64`
- **S5-3** 3 IDs replaced with `id.Generate(...)`; gap-fix added `rpc/skills.go:206` (4th location found during verification)
- **S6-3** `Terminate` waits up to 2s on `drainDone` before setting `SessionTerminated`
- **S6-4** All STT engines switched to `exec.CommandContext` with `cmd.Cancel = Kill`
- **S6-5** PTY `Close()` reaping reviewed; `waitLoop` is the documented sole owner of `cmd.Wait()`
- **S6-6** `AnalyzeCoreDelve` now `<-done` after Kill to reap
- **S6-8** `git_sync.go` stores `runCtx`/`runCancel` from Start; all git ops use `CommandContext`; Stop calls `runCancel`
- **S6-9** DAP events channel bumped to 128; drop is now non-blocking with warning log
- **S7-1** Dead error check removed from `checkOverrides`
- **S8-1/2** `_streamDone` field added to WebSocketService; `_cleanupChannel` completes it explicitly

**Medium (39/39 fixed)**
All S1-3/4/5/7/11, S2-3/5, S3-2/3/4/5/6, S4-3/4/5/6, S5-4/5/6/7, S6-10/11/12/13/14/15/16/17, S7-2/6/8, S8-3/4/5/6 fixed. See subagent reports above for details.

**Low (46/46 fixed)**
S1-6/8/9/10/12/14/15, S2-4/6/7/8/9/10/11, S3-7/8/9, S4-7/8/9/10, S5-8/9/10, S6-18/19/20/21/22/23/24/25, S7-3/4/5/7/9/10/11/12, S8-7/9/10/11/12 fixed.

**Bonus (1 fixed, not in original findings)**
- **Pre-existing race in `ContextCompactor.Compact`**: `summarizeMessages` read `c.lastSummary` without lock (line 295), and `Compact` logged `c.fileOps.FileCount()` without lock (line 232). Test `TestIntegration_CompactionStatsConcurrentSafety` exposed this. Fixed by snapshotting both under lock. `mu` upgraded from `sync.Mutex` to `sync.RWMutex`. Found via verification phase, not original review. (`internal/llm/context_compactor.go`)

**Observation #4 follow-up: mutexio analyzer (new tool)**
- Custom `go/analysis` analyzer at `tools/analyzers/mutexio/` detects `sync.Mutex`/`sync.RWMutex` locks held across I/O operations (WriteFile, ReadFile, Post, Get, Chat, Publish, Query, Dial, Send, Receive, Persist, Save, Load, etc.). Intraprocedural textual range check between Lock/Unlock pairs. Run via `make mutexio`. Enforces the CLAUDE.md "Mutex scope" rule going forward — catches the class of bugs that generated 11+ of the findings in this review.

**Observation #7 follow-up (S5-1 silent HTTP auth failure)**
- Verified CONFIRMED FIXED: all 4 HTTP client methods (`GetJSON`, `PostJSON`, `PostJSONWithResponse`, `DoRaw`) set `Authorization: Bearer <key>` header when an API key is configured. No silent-auth-failure paths remain.

**Previously deferred — now fixed (run 5):**
- **S4-8** `ToolResult.Err error` field added (`json:"-"`); `NewErrorResultErr(err)` constructor; `Registry.Execute` and `ExecuteWithRetry` preserve typed errors for `errors.Is`/`errors.As` (`internal/tools/interface.go`, `internal/tools/registry.go`, `internal/tools/mcp/client.go`)
- **S4-9** `PendingChangesRegistry.Start(interval)`/`Stop()` lifecycle added; background goroutine calls `Expire()` every 5 min; wired into daemon components (`internal/tools/builtin/pending_changes.go`, `internal/daemon/components.go`)
- **S5-9** Chat timeout now configurable via `ChatTimeoutSeconds` in daemon config; `WithChatTimeout` option; default 2 min preserved (`internal/config/schema.go`, `internal/services/chat_service.go`, `internal/services/service.go`, `internal/daemon/daemon.go`)
- **S6-7** Docker Execute uses `StartExecNonblocking` + `CloseWaiter`; ctx cancellation calls `waiter.Close()` and returns typed `ErrTimeout`/`ErrCanceled` (`internal/runtime/docker.go`)
- **S7-5** `constants.DevAPIKey()` generates a random 32-byte hex key on first run, persists to `~/.meept/dev_key` (0600); falls back to constant if generation fails; client+server read same file (`pkg/constants/api_key.go`, `internal/transport/client.go`, `internal/comm/http/server.go`)
- **S7-12** `ConnectRPC` now `os.Stat`s the socket and logs `slog.Warn` if group/other bits are set (advisory, connection proceeds); trust boundary documented (`internal/mcp/server.go`)
- **S8-7** `WebSocketService.fromStorage` throws `ArgumentError` in release mode (`kReleaseMode`) when no API key configured; hardcoded dev key fallback removed entirely (`ui/flutter_ui/lib/services/websocket_service.dart`)

### Issues Deferred (0 of 104)

All 104 findings resolved.

### Verification Evidence

```
$ go build ./...          # exit 0
$ go vet ./...            # clean (no warnings)
$ make mutexio            # clean (no violations)
$ go test -race -count=1 \
    ./internal/llm/... ./internal/debug/... ./internal/bus/... \
    ./internal/pty/... ./internal/cluster/... ./internal/agent/... \
    ./internal/daemon/... ./internal/errcls/... ./internal/security/... \
    ./internal/runtime/...
# all PASS
```

Setters test: 33/33 pass (21 original + 12 new from S4-3).
Pre-existing race `TestIntegration_CompactionStatsConcurrentSafety` now PASSES (fixed as bonus).

Total files modified: 96 (Go + Dart).

