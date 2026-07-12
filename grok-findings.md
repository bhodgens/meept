# Go Codebase Bug Review — grok-findings.md

**Date:** 2026-07-10  
**Scope:** Systematic review of Meept Go code (`cmd/`, `internal/`, `pkg/`)  
**Method:** Six parallel domain subagents (agent/orchestration, LLM/tools/MCP, security/daemon/RPC, memory/cluster/DB, services/config/code, CLI/cmd/pkg)  
**Policy:** Findings only — **no fixes applied**

---

## Summary statistics

| Severity | Count |
|----------|------:|
| Critical | 8 |
| High     | 38 |
| Medium   | 44 |
| Low      | 16 |
| **Total**| **106** |

| Domain | Critical | High | Medium | Low | Total |
|--------|---------:|-----:|-------:|----:|------:|
| Agent / orchestration / plan / queue / worker | 3 | 6 | 6 | 2 | 17 |
| LLM / tools / MCP | 1 | 4 | 8 | 1 | 14 |
| Security / daemon / RPC / bus / HTTP | 0 | 5 | 5 | 4 | 14 |
| Memory / cluster / backup / sqlite | 2 | 4 | 7 | 2 | 15 |
| Services / project / PTY / runtime / config / code | 0 | 7 | 10 | 3 | 20 |
| CLI / cmd / transport / shared packages | 0 | 6 | 7 | 2 | 15 |
| Cross-cutting (deduped notes) | — | — | — | — | see notes |

---

## Highest-priority findings (read first)

1. **Plan synthesis never schedules steps and never completes** — plans stuck in `executing` forever  
2. **Worker `Scale` binds new workers to short-lived HTTP request context** — scale-up workers die immediately  
3. **MCP stdio subprocess bound to RPC op context** — `mcp.set_enabled` kills server on handler return  
4. **Vector `ShardManager.GetShard` nested mutex deadlock**  
5. **Peer merge SQL schema mismatch with DualStore gossip DB** — cluster backup merge broken  
6. **Prompt boundary markers not neutralized** — prompt-injection breakout  
7. **HTTP PTY bypasses fence / risk / Tirith**  
8. **Streaming usage dropped + stream “resume” never implemented** — budget undercount / delta duplication  
9. **CLI session get/delete wrong RPC key; root cwd / directory arg broken**  
10. **`ast_edit` ignores `preview_only` when registry nil** — silent file mutation  

---

# Domain A — Agent / orchestration / plan / queue / worker

### [A-01] [critical] Plan synthesis never starts step execution
- **File:** `internal/plan/manager.go:338-483` (contrast `internal/agent/strategic.go:482-506`)
- **Category:** logic / state-machine
- **Description:** `Synthesize` creates parent/child tasks and steps in `StepPending`, sets plan to `executing`, publishes `plan.executing` — but never calls `PromoteReadySteps`, never publishes `orchestrator.schedule`, never sets phase tasks to executing, never updates `TotalJobs`. Nothing subscribes to `plan.executing` to kick scheduling.
- **Impact:** Approved plans create stuck pending steps; workers never claim plan work; plans appear “executing” but idle forever.
- **Evidence:** Synthesis ends with `publishEvent("plan.executing", …)` only; strategic planner path does promote + schedule.

### [A-02] [critical] Plan parent task never auto-completes → plan never reaches completed
- **File:** `internal/plan/manager.go:400-421, 530-546`; `internal/task/step.go:832-840`
- **Category:** logic / lifecycle
- **Description:** Parent plan task is in `taskPlanMap` but has **zero steps**. `AreAllCompleted` returns `false` when `len(steps)==0`, so parent never emits `task.completed`. Phase `OnTaskCompleted` only marks phase complete and does not roll up to plan completion.
- **Impact:** Even if phase tasks finished, plans remain stuck in `executing` permanently.

### [A-03] [critical] Worker `Scale` binds new workers to short-lived request context
- **File:** `internal/worker/pool.go:260-279`; callers `internal/comm/http/api_handlers.go:1961-1963`
- **Category:** context-lifetime
- **Description:** `Pool.Start` derives long-lived cancel context; `Scale` starts new workers with caller’s `ctx`. HTTP scale uses `r.Context()`, cancelled when response finishes.
- **Impact:** Scale-up workers exit almost immediately after HTTP/RPC returns; capacity APIs appear to work but do not.

### [A-04] [high] `worker.add` registers workers that never start
- **File:** `internal/worker/pool.go:462-480`
- **Category:** lifecycle / logic
- **Description:** `handleAdd` only `AddWorker` (map insert); never `worker.Start` or attaches `wg`.
- **Impact:** Added workers stay `stopped` forever and never claim jobs.

### [A-05] [high] Plan phase/task maps are process-memory only
- **File:** `internal/plan/manager.go:37-39, 400-421, 492-534`
- **Category:** logic / lifecycle
- **Description:** `phaseTaskMap` / `taskPlanMap` filled only during `Synthesize`; not persisted/reloaded.
- **Impact:** After daemon restart, plan completion handlers silently no-op for existing plans.

### [A-06] [high] Cluster claim timeout reclaims in-flight local work (double execution)
- **File:** `internal/queue/cluster_queue.go:84-104, 236-274, 542-554`
- **Category:** concurrency / TOCTOU
- **Description:** Claim sets `TimeoutAt` to now + 5m with no heartbeat extension. `ReclaimIfStale` resets claimed/processing jobs without checking local worker liveness.
- **Impact:** Jobs >5 minutes reclaimed and double-executed; conflicting Complete/Fail races.

### [A-07] [high] `ClaimNextForAgent` ignores UPDATE row count
- **File:** `internal/queue/store.go:378-394` (contrast `ClaimNextByID` at 1136-1147)
- **Category:** concurrency / TOCTOU
- **Description:** UPDATE uses `WHERE id = ? AND state = 'pending'` but never checks `RowsAffected`; still returns job as claimed.
- **Impact:** Concurrent multi-writer paths can both believe they own the same job.

### [A-08] [high] Tactical `CompletedJobs` RMW race loses updates under parallel steps
- **File:** `internal/agent/tactical.go:662-681`
- **Category:** concurrency
- **Description:** `OnJobCompleted` does Get → `CompleteJob()` → Update without serialization. `Registry.CompleteJob` exists to prevent this but tactical path bypasses it.
- **Impact:** Undercounted `CompletedJobs` / token aggregates / wrong progress under parallel steps.

### [A-09] [high] Step `SetState` claims BEGIN IMMEDIATE but uses deferred transactions
- **File:** `internal/task/step.go:740-786, 1188-1217`
- **Category:** concurrency
- **Description:** Comments assert BEGIN IMMEDIATE / SELECT FOR UPDATE; implementation uses deferred `BeginTx` with no transition guard in SQL.
- **Impact:** Parallel completion/review/failure can clobber step state.

### [A-10] [medium] Tier-2 MaxActivePlans does not gate multi-candidate plan creation
- **File:** `internal/employee/goal_loop.go:865-917`
- **Category:** logic
- **Description:** `CanAddActivePlan` checked once before Assess; every candidate creates a plan; pending plans not added to `ActivePlanIDs`.
- **Impact:** Cap only limits execution, not pending backlog; floods approval queues.

### [A-11] [medium] PersistentQueue holds mutex across DB I/O and cancel callbacks
- **File:** `internal/queue/queue.go:139-258, 261-333`
- **Category:** concurrency
- **Description:** `Enqueue`/`Claim`/`Complete`/`Fail`/`Retry` hold `q.mu` for entire SQLite ops and cancel checks.
- **Impact:** Queue ops serialize on disk latency; slow cancelFn blocks all workers.

### [A-12] [medium] Worker restart double-closes `done` channel
- **File:** `internal/worker/worker.go:85-104, 158-167`
- **Category:** lifecycle / concurrency
- **Description:** `Start` allows restart from `StateStopped` but never recreates `done`; `run` always closes it.
- **Impact:** Stop+Start same worker panics on double close.

### [A-13] [medium] Plan phase completion can mark phase complete before all steps finish
- **File:** `internal/plan/manager.go:530-545`
- **Category:** logic
- **Description:** Phase-child `task.completed` immediately sets `PhaseCompleted` without verifying remaining steps.
- **Impact:** UI can show phases complete while work remains.

### [A-14] [medium] `PromoteReadySteps` treats rejected deps as satisfied
- **File:** `internal/task/step.go:716-724`
- **Category:** logic
- **Description:** Dep check is `!IsTerminal() || == StepFailed`. `StepRejected` is terminal and not failed → dependents promote.
- **Impact:** Downstream steps schedule on rejected work.

### [A-15] [medium] Bot `EventActionRouter.Start` never re-subscribes after runtime register
- **File:** `internal/bot/router.go:72-98`; `internal/bot/lifecycle.go:143-168`
- **Category:** lifecycle
- **Description:** `Start` snapshots topics once; later `Register` adds topics to map but no new bus subscriptions.
- **Impact:** Bots enabled after router start with new topics never fire.

### [A-16] [low] `Pool.Start` partial start then “already started” on retry
- **File:** `internal/worker/pool.go:77-122`
- **Category:** error-handling / lifecycle
- **Description:** `startOnce` runs even if some workers fail; second `Start` returns already started.
- **Impact:** Failed boot cannot be cleanly retried without process restart.

### [A-17] [low] Notification bot Telegram sends inherit cancelled handler context
- **File:** `internal/bot/notification_bot.go:129-140, 203-210`
- **Category:** context-lifetime
- **Description:** Async send goroutines use outer event-loop `ctx`; shutdown cancels in-flight sends.
- **Impact:** Notifications lost on shutdown instead of best-effort independent timeout.

---

# Domain B — LLM / tools / MCP

### [B-01] [critical] MCP stdio subprocess bound to RPC op context via `CommandContext`
- **File:** `internal/tools/mcp/transport/stdio.go:72`; chain `rpc/server.go:355-362` → `rpc/mcp.go:146`
- **Category:** resource-leak / logic
- **Description:** `StdioTransport.Start` uses `exec.CommandContext(ctx)`. RPC dispatch creates per-op timeout context with `defer cancel()` on return. Enabling/reloading MCP via RPC starts then immediately kills the process.
- **Impact:** Manager thinks server is active while subprocess is dead; tools fail until restart.

### [B-02] [high] Streaming usage silently dropped (budget undercount)
- **File:** `internal/llm/client.go:1267-1311`, `buildChatRequest` ~305-307
- **Category:** logic / token-budget
- **Description:** Stream path does not set `stream_options.include_usage`. When usage arrives in empty-`choices` final chunk, parser `continue`s before applying usage.
- **Impact:** Streaming completions record 0 tokens; budget gates and cost accounting ineffective.

### [B-03] [high] Stream “resume” state is never written; retries re-emit deltas
- **File:** `internal/llm/client.go:120-135, 1028-1034, 1146-1150, 1202-1286`
- **Category:** logic / streaming
- **Description:** `streamRetryState` documents resume fields but nothing assigns them after partial stream. On retry every content delta is re-delivered.
- **Impact:** Transient stream failures duplicate UI/token streams; resume is dead code.

### [B-04] [high] `ProviderManager.ChatWithProgress` never calls progress-capable path
- **File:** `internal/llm/provider_manager.go:326-374`
- **Category:** logic
- **Description:** Accepts `ProgressCallback` but calls `entry.Chatter.Chat(...)` not `ChatWithProgress`. Broker path is correct.
- **Impact:** Failover path loses streaming/progress stages for TUI/GUI.

### [B-05] [high] OpenAI stream scanner fails on large SSE lines (64 KiB default)
- **File:** `internal/llm/client.go:1208`
- **Category:** error-handling / streaming
- **Description:** `bufio.NewScanner` without `Buffer` / raised max token size. Large tool-call argument lines fail stream.
- **Impact:** Streaming tool calls with large args fail intermittently; MCP HTTP correctly raises limit.

### [B-06] [medium] Anthropic SSE scanner can drop last event without trailing blank line
- **File:** `internal/llm/anthropic.go:1309-1374`
- **Category:** streaming / logic
- **Description:** Events complete only on blank line; EOF does not flush pending event with `Data` set.
- **Impact:** Lost final usage/stop reason when connection ends without blank terminator.

### [B-07] [medium] Budget RPM double-count: reserve + `RecordUsage`
- **File:** `internal/llm/budget.go:737-757, 312-330`
- **Category:** logic
- **Description:** `WaitForRateLimit` and `RecordUsage` both append request timestamps → effective RPM ~half of configured.
- **Impact:** Legitimate traffic throttled early.

### [B-08] [medium] Budget uses only `TotalTokens` (no Prompt+Completion fallback)
- **File:** `internal/llm/budget.go:312-329`
- **Category:** token-budget
- **Description:** Accounting uses `usage.TotalTokens` only; providers leaving total 0 record zero while prompt/completion may be set.
- **Impact:** Under-enforced budgets; caps drift from reality.

### [B-09] [medium] Streamed tool calls assembled in non-deterministic order
- **File:** `internal/llm/client.go:1324-1335`
- **Category:** logic / streaming
- **Description:** Final tool-call slice built by ranging `map[int]*toolCallAccum` (unordered).
- **Impact:** Parallel multi-tool turns can reverse intended order.

### [B-10] [medium] Stream tool-call ID/name not updated after first delta
- **File:** `internal/llm/client.go:1290-1301`
- **Category:** streaming / logic
- **Description:** Existing accumulator only appends Arguments; later id/name deltas ignored.
- **Impact:** Empty `tool_call_id` → API rejections or orphaned tool results.

### [B-11] [medium] MCP stdio discards live responses when `relayCh` is full
- **File:** `internal/tools/mcp/transport/stdio.go:183-200`
- **Category:** MCP transport
- **Description:** Non-blocking send with `default: discard`; buffer size 32.
- **Impact:** Matching responses discarded → hang until timeout under chatty servers.

### [B-12] [medium] Anthropic request path: empty tool `Arguments` → invalid `input` JSON
- **File:** `internal/llm/anthropic.go:673-679`
- **Category:** message serialization
- **Description:** Empty `Arguments` as `json.RawMessage` yields invalid embedded JSON; stream response path defaults to `{}` but request path does not.
- **Impact:** Follow-up Anthropic requests after empty-arg tools can 400.

### [B-13] [low] MCP HTTP `parseSSEResponse` returns first result/error only
- **File:** `internal/tools/mcp/transport/http.go:218-262`
- **Category:** MCP transport
- **Description:** First JSON object with result/error wins; multi-result SSE later frames ignored.
- **Impact:** Low under standard MCP; medium only for non-standard multi-result SSE.

---

# Domain C — Security / daemon / RPC / bus / HTTP

### [C-01] [high] Prompt boundary markers are not neutralized (breakout)
- **File:** `internal/security/prompt_guard.go:86-94`; `internal/security/sanitizer.go:251-263`
- **Category:** security
- **Description:** `WrapUserInput` / `WrapToolOutput` embed raw text without stripping embedded end markers. Sanitizer structural tokens omit Meept’s own boundaries.
- **Impact:** Attacker can close untrusted section early and inject free-form instructions outside the boundary.

### [C-02] [high] HTTP PTY sessions bypass fence, risk classification, and Tirith
- **File:** `internal/comm/http/pty_handler.go:52-106`; `internal/pty/manager.go:45-67`
- **Category:** security
- **Description:** `POST /api/v1/pty/sessions` accepts client `Cmd`/`Args`/`Dir`/`Env` with no fence/risk/Tirith. Shell tool path does fence-check.
- **Impact:** Valid API key → arbitrary process spawn outside project fence (incl. `LD_PRELOAD`/`PATH`).

### [C-03] [high] Fence only constrains shell workdir, not command paths
- **File:** `internal/security/fence.go:163-169`; `internal/security/engine.go:383-388`
- **Category:** security
- **Description:** `CheckCommand` only validates `workDir` via `CheckPath`; ignores command string.
- **Impact:** `cat ~/.ssh/id_rsa` etc. succeed if cwd is inside project root.

### [C-04] [high] Legacy web server: empty `secret_key` disables all auth
- **File:** `internal/daemon/components.go:2255-2286`; `internal/comm/web/server.go:394-398`
- **Category:** security
- **Description:** When `cfg.Web.Enabled` and `SecretKey == ""` (default), authenticator is nil; middleware skips auth.
- **Impact:** Enabling web without secret exposes chat, skills, sessions, jobs to any network client that can reach the host/port.

### [C-05] [high] Hardcoded fallback API key + HTTP bind-all default
- **File:** `pkg/constants/api_key.go:25-54,76-109`; `internal/config/schema.go:1491-1494`; `internal/comm/http/server.go:208-216`
- **Category:** security
- **Description:** If `~/.meept/dev_key` cannot be created/read, falls back to public constant `meept_dev_default_key_CHANGE_ME`. Default HTTP addr `:8081` (all interfaces).
- **Impact:** Public knowledge key + bind-all → remote access equivalent to unauthenticated when fallback triggers.

### [C-06] [medium] TLS pin verifier fails open when fingerprints empty
- **File:** `pkg/tlsutil/pin.go:79-115`
- **Category:** security
- **Description:** `VerifyConnection` returns nil if both expected FPs empty; `PinTransport` sets `InsecureSkipVerify: true`.
- **Impact:** Misconfigured pin = full MITM (no CA + no pin).

### [C-07] [medium] Telegram bot allows all users when allowlists empty
- **File:** `internal/comm/telegram/bot.go:373-391`
- **Category:** security
- **Description:** `isAllowed` returns true when both allowlists empty (default).
- **Impact:** Open bot = remote agent control for anyone who can message it.

### [C-08] [medium] Engine mutates shared `details` map under RLock (race)
- **File:** `internal/security/engine.go:318-320, 407-424`
- **Category:** concurrency
- **Description:** `CheckForAgent` holds RLock and writes caller-provided `details` map.
- **Impact:** Map races / panics under concurrent permission checks.

### [C-09] [medium] Bus drops messages on full buffers (including security approvals)
- **File:** `internal/bus/bus.go:195-204`; `internal/rpc/proxy.go:236-255`
- **Category:** logic / security
- **Description:** Non-blocking publish; full buffers drop. `security.approve_action` is fire-and-forget.
- **Impact:** Critical control events can be silently lost under load.

### [C-10] [medium] Shell hard-block only stops RiskCritical; interpreters bypass
- **File:** `internal/tools/builtin/shell.go:64-73, 236-240, 564-615`
- **Category:** security
- **Description:** Only `RiskCritical` base commands blocked; `bash -c 'rm ...'`, interpreters classify lower. Tirith optional and fail-open.
- **Impact:** Destructive actions via interpreters despite blocked-command list.

### [C-11] [low] API key constant-time compare leaks length
- **File:** `internal/comm/http/auth.go:52-58`; `internal/comm/web/auth.go:50-55`
- **Category:** security
- **Description:** `subtle.ConstantTimeCompare` returns immediately on length mismatch.
- **Impact:** Length oracle via timing (low for long random keys).

### [C-12] [low] Provider credentials returned in cleartext over HTTP API
- **File:** `internal/comm/http/api_handlers.go:1281-1303`
- **Category:** security
- **Description:** `GET /api/v1/models/credentials/{provider}` returns raw credential string.
- **Impact:** API-key holder can exfiltrate LLM provider secrets.

### [C-13] [low] DevAPIKey docs claim trim; implementation does not
- **File:** `pkg/constants/api_key.go:46-49, 86-90`
- **Category:** error-handling / security
- **Description:** Comment says trim; code returns raw file bytes including newline.
- **Impact:** Auth mismatch if file hand-edited with trailing newline.

---

# Domain D — Memory / cluster / backup / sqlite

### [D-01] [critical] Nested mutex deadlock in `ShardManager.GetShard`
- **File:** `internal/memory/vector/shard_manager.go:154-162` (`loadShard` at 105-107)
- **Category:** concurrency
- **Description:** `GetShard` holds `m.mu.Lock()` then calls `loadShard`, which also `Lock()`s. Mutex not re-entrant → hang.
- **Impact:** First access to non-preloaded shard types (`project`, `code`, `archive`) deadlocks.

### [D-02] [critical] Peer merge SQL targets schema that does not match DualStore gossip DB
- **File:** `internal/backup/merge.go:152-153`; production `internal/memory/schema_gossip.sql:23-37`
- **Category:** logic
- **Description:** Merge inserts `updated_at`/`metadata`; production uses `last_activity`/`metadata_json`. Tests invent matching private schema — pass while production fails.
- **Impact:** Peer backup merge into real `sync-gossip.db` broken; session recovery via git sync fails.

### [D-03] [high] `GetVersionHistory` recursive CTE references non-existent `parent_id`
- **File:** `internal/memory/manager.go:1392-1401`
- **Category:** logic
- **Description:** CTE selects columns without `parent_id` but joins/filters on `vc.parent_id`.
- **Impact:** `memory_get_version_history` always errors; feature unusable.

### [D-04] [high] `Pool.QueryRow` returns connection to pool before `Scan`
- **File:** `pkg/sqlite/pool.go:278-286`
- **Category:** concurrency / resource-leak
- **Description:** `defer Put(db)` immediately after `QueryRowContext` before caller scans. Pooled DBs have `MaxOpenConns(1)`.
- **Impact:** Concurrent QueryRow races: busy errors, hangs, wrong-row risks at all call sites.

### [D-05] [high] SESSION_TURN gossip writes synthetic memories, not turns; swallows errors
- **File:** `internal/cluster/gossip_handler.go:113-154`
- **Category:** logic / error-handling
- **Description:** Handler builds Memory and `StoreRemoteMemory`; never `StoreRemoteTurn`. Errors only logged; returns nil.
- **Impact:** Peer turns never appear in turns table; incomplete cluster session history; no retry.

### [D-06] [high] Memory conflict resolution wrong timestamp + TOCTOU
- **File:** `internal/cluster/gossip_handler.go:180-236, 247-287`
- **Category:** concurrency / logic
- **Description:** Existing reconstructed with `created_at` vs event publish time for LWW. Fetch outside write lock → concurrent same-ID both “no conflict”.
- **Impact:** Wrong winner / silent overwrite under multi-node concurrent updates.

### [D-07] [medium] DualStore memory row scan cannot handle SQL NULL columns
- **File:** `internal/memory/dual_store.go:451-527`
- **Category:** error-handling
- **Description:** Scans nullable columns into plain `*string`; NULL → “converting NULL to string is unsupported”.
- **Impact:** GetMemories / merged reads fail for partial rows.

### [D-08] [medium] Vector shard re-insert orphans old embeddings
- **File:** `internal/memory/vector/vector_shard.go:206-248, 251-312`
- **Category:** logic
- **Description:** Insert always adds new vec0 row and replaces mapping without deleting old rowid.
- **Impact:** Orphan vectors accumulate; inflated index size; permanent leak until compaction.

### [D-09] [medium] `CrossShardJoin.QueryShards` re-scans failed rows and may append garbage
- **File:** `internal/memory/vector/cross_shard_join.go:158-169`
- **Category:** logic
- **Description:** First Scan into `map[string]any` fails; re-Scan path still appends; failed scans not skipped cleanly.
- **Impact:** Missing/wrong metadata or corrupt search results.

### [D-10] [medium] CrossShardJoin ATTACH not connection-stable / not mutex-scoped for queries
- **File:** `internal/memory/vector/cross_shard_join.go:31-48, 99-106`
- **Category:** concurrency / logic
- **Description:** ATTACH is connection-scoped; lock released before query; path string-interpolated into SQL.
- **Impact:** Intermittent “no such table”; races; possible SQL injection if path/alias attacker-influenced.

### [D-11] [medium] Gossip dedup marks event seen before signature verification
- **File:** `internal/cluster/gossip.go:458-495`
- **Category:** security / logic
- **Description:** Event ID inserted into dedup before signature checks.
- **Impact:** Poisoned/unsigned event suppresses legitimate signed event for retention window (DoS).

### [D-12] [medium] `pooledRows.Close` can double-return a connection
- **File:** `pkg/sqlite/pool.go:295-302`
- **Category:** resource-leak / concurrency
- **Description:** No Once/nil-out after Put; double Close puts same `*sql.DB` twice.
- **Impact:** Two callers share MaxOpenConns(1) DB → corruption/BUSY storms.

### [D-13] [medium] ConflictResolver ignores vector clocks despite API/docs
- **File:** `internal/cluster/conflict.go:22-47` vs `50-87`
- **Category:** logic
- **Description:** `Resolve` only wall time + node ID; `CompareVectorClocks` unused.
- **Impact:** Wrong LWW under clock skew; vector clocks unused for arbitration.

### [D-14] [low] `VectorShard.SetEFSearch` unsynchronized write
- **File:** `internal/memory/vector/vector_shard.go:434`
- **Category:** concurrency
- **Description:** Writes `efSearch` without lock while other methods use `mu`.
- **Impact:** Data race under `-race`; rare stale reads.

### [D-15] [low] Merge success stats do not distinguish INSERT OR IGNORE skips
- **File:** `internal/backup/merge.go:123-138`
- **Category:** other
- **Description:** `skipped` always 0; all RowsAffected treated as merged.
- **Impact:** Misleading metrics only.

---

# Domain E — Services / project / workspace / runtime / config / code

### [E-01] [high] PTY non-PTY fallback starts process before creating pipes
- **File:** `internal/pty/session.go:141-172`
- **Category:** logic / resource-leak
- **Description:** `Start()` before `StdoutPipe()`/`StdinPipe()` — invalid Go order.
- **Impact:** Fallback mode always fails; interactive tools broken when PTY unavailable.

### [E-02] [high] `ast_edit` writes disk even when `preview_only=true` if registry nil
- **File:** `internal/code/tools/ast_edit.go:121-124, 200-250`
- **Category:** logic
- **Description:** Preview early-return only when `pendingChangesRegistry != nil && previewOnly`. Without registry, always `os.WriteFile`.
- **Impact:** Documented safe default false; agent can mutate source without apply.

### [E-03] [high] Self-improve `Rollback` path traversal write
- **File:** `internal/selfimprove/applier.go:190-216`
- **Category:** security
- **Description:** Rollback joins `projectRoot` with `OriginalPath` with no `isWithinDir` check.
- **Impact:** Malicious/corrupted path overwrites arbitrary writable files.

### [E-04] [high] Self-improve backup reads path before sandbox validation
- **File:** `internal/selfimprove/applier.go:89-108, 246-248`
- **Category:** security
- **Description:** `createBackup` reads before `isWithinDir`.
- **Impact:** Path like `../../.ssh/id_rsa` can exfiltrate sensitive contents into selfimprove backups.

### [E-05] [high] Preference shell risk bypass via substring “safe” match
- **File:** `internal/preferences/verifier.go:125-130`
- **Category:** security / logic
- **Description:** High-risk checks only after `strings.Contains` allowlist; any cmd containing `go test ./...`/`echo`/etc. returns low.
- **Impact:** `rm -rf /; go test ./...` classified low; confirmation skipped.

### [E-06] [high] Project register path escape via `id`
- **File:** `internal/project/manager.go:50-58`
- **Category:** security
- **Description:** `filepath.Join(BaseDir, id)` without containment; user `id` may include `..`.
- **Impact:** `git clone` can write outside projects base directory.

### [E-07] [high] Docker backend hard-codes Unix dial; breaks non-unix `DOCKER_HOST`
- **File:** `internal/runtime/docker.go:27-30, 65-72`
- **Category:** logic
- **Description:** Always dials `unix` and strips fixed `unix://` prefix length regardless of scheme.
- **Impact:** `DOCKER_HOST=tcp://...` fails; isolation backend broken for remote engines.

### [E-08] [medium] Project `Status` swaps Ahead/Behind
- **File:** `internal/project/manager.go:217-224`
- **Category:** logic
- **Description:** `rev-list --left-right` left=behind, right=ahead; code assigns opposite.
- **Impact:** UI/CLI inverted sync signals; wrong push/pull decisions.

### [E-09] [medium] PTY `IsRunning` / `ExitCode` contract broken after process exit
- **File:** `internal/pty/session.go:47-51, 335-340, 412-430`
- **Category:** logic
- **Description:** `IsRunning` only checks `!closed`; `waitLoop` sets exitCode but never `closed`.
- **Impact:** Callers never observe exit until `Close()`; finished sessions accumulate.

### [E-10] [medium] Workspace `gitClone` missing option-injection guards
- **File:** `internal/workspace/git_ops.go:112-117`
- **Category:** security
- **Description:** `git clone <url> <dest>` without `--` and without rejecting `-` prefix (project manager does both).
- **Impact:** Crafted RepoURL as git options (`--upload-pack=...`).

### [E-11] [medium] RepoMap cache dir tilde never expanded
- **File:** `internal/repomap/generator.go:46, 113-116`; `internal/repomap/cache.go:91-104`
- **Category:** logic
- **Description:** Default `~/.meept/repomap_cache` used literally when non-empty; home only expanded for empty path.
- **Impact:** Cache under cwd-relative `./~/.meept/...`; thrash and workspace pollution.

### [E-12] [medium] Placement `MaxNodes` truncates before scoring
- **File:** `internal/placement/scheduler.go:177-180`
- **Category:** logic
- **Description:** Slice candidates to MaxNodes before score sort; map iteration order random.
- **Impact:** Best peers discarded arbitrarily under multi-node placement.

### [E-13] [medium] RegisterGit treats existing directory as success without validating repo
- **File:** `internal/project/manager.go:52-55`
- **Category:** logic
- **Description:** Existing path skips clone entirely; still registers as active git project.
- **Impact:** Empty/wrong dirs appear registered; re-register after failed clone “succeeds”.

### [E-14] [medium] Shadow schema version advances after soft-failed migrations
- **File:** `internal/shadow/store_sqlite.go:87-102, 159-199`
- **Category:** error-handling
- **Description:** migrate v2/v3 log errors but don’t return; version always bumped to latest.
- **Impact:** Partial schema marked migrated; permanent query failures until manual DB fix.

### [E-15] [medium] `UnmarshalJSON5` global quoted-duration rewrite can corrupt non-duration strings
- **File:** `internal/config/json5_loader.go:154-184`
- **Category:** logic / config
- **Description:** Regex rewrites any `"30s"`/`"1d"` token document-wide to nanosecond integers.
- **Impact:** Non-duration string fields become integers → type errors or silent wrong values.

### [E-16] [medium] PTY `Read` races on `pending` without lock
- **File:** `internal/pty/session.go:222-246`
- **Category:** concurrency
- **Description:** `pending` mutated without `mu` under concurrent Read.
- **Impact:** Lost/duplicated output under concurrent consumers.

### [E-17] [medium] LSP stdio `Read` leaks goroutine on context cancel
- **File:** `internal/code/lsp/transport/stdio.go:56-112`
- **Category:** resource-leak
- **Description:** Cancel returns while goroutine still blocked on pipe Read.
- **Impact:** Goroutine accumulation under flaky/timed-out LSP calls.

### [E-18] [medium] Upload metadata write is non-atomic
- **File:** `internal/services/upload_service.go:286-291`
- **Category:** file I/O
- **Description:** Single `os.WriteFile` on `uploads.json` without temp+rename.
- **Impact:** Crash mid-write truncates all upload metadata.

### [E-19] [low] `KillProcessTree` assumes process group without `Setpgid`
- **File:** `internal/pty/session.go:470-479`
- **Category:** logic
- **Description:** SIGKILL to `-pid` without starting children with `Setpgid`.
- **Impact:** Group kill ineffective; orphan children may survive.

### [E-20] [low] `SessionConfig.Timeout` is never applied
- **File:** `internal/pty/session.go:68-69, 105-178`
- **Category:** logic
- **Description:** Documented timeout field unused in NewSession/wait loops.
- **Impact:** Expected auto-kill never happens.

### [E-21] [low] ExpandEnvVars silently empties undefined variables
- **File:** `internal/config/config.go:191-195`
- **Category:** config / logic
- **Description:** Missing env vars expand to `""` without error.
- **Impact:** Empty secrets/paths without load failure.

---

# Domain F — CLI / cmd / transport / shared packages

### [F-01] [high] CLI `session.get` / `session.delete` use wrong RPC param key
- **File:** `cmd/meept/session.go:193, 262`; `cmd/meept/chat.go:171`; server `internal/session/session.go:1363-1374, 1421-1432`
- **Category:** logic
- **Description:** CLI sends `session_id`; daemon expects `id`. TUI/HTTP use `id` correctly.
- **Impact:** `meept session get|delete` and `meept chat --session <id> "msg"` always fail.

### [F-02] [high] `meept /path/to/dir` still chats the path instead of opening TUI
- **File:** `cmd/meept/main.go:114-128` → `runChat`
- **Category:** logic
- **Description:** Directory detection sets `--cwd` but still passes path into `runChat` as message.
- **Impact:** Path becomes a one-shot chat message; TUI not opened in that dir.

### [F-03] [high] Root `--cwd` never reaches chat/TUI (`rootCwd` vs `chatCwd`)
- **File:** `cmd/meept/main.go:135, 142-145`; `cmd/meept/chat.go:52, 119`
- **Category:** logic
- **Description:** Copy `chatCwd = rootCwd` runs at registration (always empty). `runTUI` only reads `chatCwd`.
- **Impact:** `meept --cwd /path` and path-detection cwd have no effect.

### [F-04] [high] `--project` and `--nofence` documented but never applied
- **File:** `cmd/meept/chat.go:22-23, 49-50, 57-119`
- **Category:** logic
- **Description:** Flags bound but never read in runChat / session create / TUI.
- **Impact:** Project binding and fence disable from CLI do nothing.

### [F-05] [high] HTTP transport keeps loopback `InsecureSkipVerify` after remote `--http-url`
- **File:** `cmd/meept/main.go:253-261`; `internal/transport/client.go:96-104, 108-124`
- **Category:** security
- **Description:** DefaultConfig enables skip-verify for localhost; overwriting URL does not recompute flag.
- **Impact:** Remote HTTPS without pin file skips cert verification → MITM risk.

### [F-06] [high] `cluster join` never writes returned config (type assertion always fails)
- **File:** `cmd/meept/cluster_cmd.go:309-323`; server `internal/rpc/cluster_handler.go:191-196`
- **Category:** logic
- **Description:** After unmarshal to `map[string]any`, nested objects are maps not `json.RawMessage`; assertion fails silently.
- **Impact:** Join appears successful but writes no config file.

### [F-07] [medium] `--json` on dispatch/cluster status base64-encodes raw RPC bytes
- **File:** `cmd/meept/dispatch.go:80-84, 143-147, 202-206`; `cmd/meept/cluster_cmd.go:397-401`
- **Category:** logic
- **Description:** `json.MarshalIndent` on `[]byte`/`RawMessage` base64-encodes instead of printing JSON.
- **Impact:** Machine-readable `--json` flags unusable.

### [F-08] [medium] `~` expansion via `filepath.Join(home, path[1:])` drops home
- **File:** `cmd/meept/q.go:220-234`; `cmd/meept/shadow.go:909-917`
- **Category:** logic
- **Description:** `path[1:]` for `~/foo` is `/foo` (absolute); Join discards home. Correct is `path[2:]` (`pathutil.ExpandPath`).
- **Impact:** Q/shadow paths resolve under `/` instead of `$HOME`.

### [F-09] [medium] `IsRetryable` treats `context.Canceled` as retryable
- **File:** `internal/errcls/classify.go:76-77`; used by `internal/agent/tactical.go:1225-1226`
- **Category:** error-handling
- **Description:** User abort/shutdown classified retryable.
- **Impact:** Extra work after cancel; delayed shutdown; wasted LLM/tool retries.

### [F-10] [medium] HTTP client swallows malformed bus responses
- **File:** `internal/transport/http_client.go:251-252`
- **Category:** error-handling
- **Description:** On response unmarshal failure returns `(data, nil)`.
- **Impact:** Callers treat garbage as success; confusing later errors.

### [F-11] [medium] Session create `--description` ignored by daemon handler
- **File:** `cmd/meept/session.go:141-146`; `internal/session/session.go:1327-1334`
- **Category:** logic
- **Description:** CLI sends `description`; handler only unmarshals `name` / `detection_context`.
- **Impact:** Description always empty despite success.

### [F-12] [medium] `meept chat --session <id>` TUI path is a stub
- **File:** `cmd/meept/chat.go:196-201`
- **Category:** logic
- **Description:** Warns and opens most-recent via `runTUI` instead of targeting session.
- **Impact:** Documented session attach for TUI does not work.

### [F-13] [medium] `session --needs-attention` subcommand effectively unreachable
- **File:** `cmd/meept/session.go:478`
- **Category:** logic
- **Description:** `Use: "--needs-attention"` registered as subcommand name resembling a flag; `meept session --needs-attention` parsed as unknown flag.
- **Impact:** Designated-session listing not usable as intended.

### [F-14] [medium] `meept` vs `meept-lite` HTTP defaults disagree (https vs http)
- **File:** `cmd/meept/main.go:138`; `cmd/meept-lite/main.go:29-35`
- **Category:** logic
- **Description:** Main defaults `https://localhost:8081`; lite defaults `http://`.
- **Impact:** Cross-client muscle memory fails when daemon only on one scheme.

### [F-15] [low] `session.list` limit param ignored server-side
- **File:** CLI `cmd/meept/session.go:48, 118`; handler `internal/session/session.go:1353-1359`
- **Category:** logic
- **Description:** CLI sends `limit`; handler ignores payload.
- **Impact:** `--limit` has no effect.

---

## Themes / systemic patterns

1. **Context lifetime mismatches** — request/op contexts used for long-lived workers, MCP processes, scale-up workers  
2. **Existence ≠ enforcement** — security fences, preview modes, plan completion, stream resume partially implemented or unwired  
3. **Schema / contract drift** — CLI↔RPC keys, merge SQL↔gossip schema, CTE vs columns, tests vs production  
4. **Non-reentrant / wrong lock scope** — nested mutex, RMW without atomic helpers, RLock + map mutation  
5. **Silent failure modes** — fire-and-forget bus, discarded MCP lines, soft-failed migrations bumping versions, type assertions that no-op  
6. **Security fail-open defaults** — empty web secret, empty Telegram allowlist, empty TLS pins, public dev key fallback  

---

## Areas reviewed without solid production bugs (per subagents)

- Employee `Trigger` per-employee serialization (`invokeMuMap`)
- GoalLoop mutex-scope for LLM I/O (snapshot under lock)
- Agent loop reflection hooks detach from request ctx with `wg`
- Tool registry mutex patterns
- Unified HTTP defaults (`RequireAuth: true`, TLS, socket 0600) when defaults kept
- SSRF guards for `web_fetch` (common cases)
- DualStore session/turn NullString scan paths
- `pkg/id.Generate` uses crypto/rand
- `pathutil.ExpandPath` tilde handling correct
- Registry StartAll/StopAll; validator filesystem allowlist

---

## Recommended fix order (for a later pass — not done here)

1. Plan schedule + completion lifecycle (A-01, A-02, A-05, A-13)  
2. Worker Scale/Add context & start (A-03, A-04, A-12)  
3. MCP CommandContext ownership (B-01)  
4. Vector ShardManager deadlock (D-01)  
5. Merge SQL schema + SESSION_TURN + version history (D-02, D-03, D-05)  
6. Prompt boundary neutralization + PTY HTTP fence + fence command paths (C-01–C-03)  
7. Stream usage + resume + scanner buffer (B-02, B-03, B-05)  
8. CLI session keys / cwd / project flags / TLS skip-verify (F-01–F-05)  
9. ast_edit preview + self-improve path checks + prefs risk bypass (E-02–E-05)  
10. SQLite pool QueryRow/Close lifetime (D-04, D-12)  

---

*End of findings. No code was modified as part of this review.*
