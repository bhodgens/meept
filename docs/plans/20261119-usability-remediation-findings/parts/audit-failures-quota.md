# Audit: Failure Path Inventory and Quota-Disabled Behavior

Repo: /tmp/meept-usability-head (HEAD 1cee89d7). READ-ONLY audit. Every
file:line below was verified against this tree with grep/read. Perspective:
interactive chat user. Contract yardstick: chat.submit acks (turn_id), the
result must arrive as a turn.terminal bus event with status in the closed set
completed|failed|timeout|parked, emitted through the single funnel
ChatHandler.publishTurnTerminal (internal/agent/handler.go:2016) via
bus.PublishBlockingT (internal/bus/topic.go:57). The turn watchdog
(internal/agent/turn_watchdog.go:129) is the last-resort reaper: status=failed,
handler_case=turn_reaped, error "turn reaped: no progress for <window>",
reply "this turn stopped responding and was marked failed"
(turn_watchdog.go:136-150). Defaults: enabled=true, interval 30s,
stale_after 600s (internal/config/schema.go:4949-4953; wiring
internal/daemon/daemon.go:310-326).

Companion detail files (same rig, merged below where relevant):
audit-queue-gates.md (classes 7, 9, 10, 11), audit-classifier-routing.md
(classes 5, 6), audit-quota-config.md (full config inventory).

===========================================================================
SECTION A - FAILURE PATH INVENTORY
===========================================================================

## A0. The publishTurnTerminal funnel and its escape hatches

Verified mechanics:
- handleRequest (handler.go:749) early exits all emit: invalid payload
  (766-778, status failed, ConversationID=msg.ID sentinel, fresh synthetic
  turn id), empty message (796-812, failed), duplicate dispatch
  (894-919, status parked, "turn already submitted").
- Early-return safety net: after ClaimDispatch succeeds, a function-scoped
  defer emits failed turn.terminal "turn handler exited before the dispatch
  funnel completed it" + Complete on ANY exit that did not reach the funnel
  (handler.go:870-893). This covers panics during unwind.
- Main funnel handler.go:1424-1473: status completed | failed (err != nil,
  Error=response.Error or err.Error(), reply synthesized "I encountered an
  error: ..." when empty at 1438-1441) | timeout (sync wait ceiling,
  sync-only legacy mode) | parked (budget/quota park with error cleared,
  1428-1431; async dispatch ack, 1446-1455).
- completeTurn skipped when syncTaskID != "" so the task->turn registry
  edge survives for the relay (handler.go:1490-1493).
- publishTurnTerminal: nil-handler/nil-bus no-op return (handler.go:2017-
  2019); marshal failure logged and dropped (2031-2033, unreachable for the
  frozen payload); then PublishBlockingT (2035).

Findings:

A0-1. [MAJOR] Panic in handleRequest or any RPC handler kills the whole
daemon; there is no recover anywhere in the chat/RPC/HTTP request path.
Evidence: the chat goroutine calls h.handleRequest directly with no recover
(handler.go:386-399); the only recover in handler.go is inside
runEffectsResumeHook (handler.go:221). grep recover() across internal/rpc,
internal/comm, internal/transport: ZERO hits. RPC dispatch invokes the
handler bare (internal/rpc/server.go:532) inside a per-connection goroutine
launched without recover (server.go:418-423, handleConnection 424+). The
HTTP middleware chain (internal/comm/http/server.go:1985+) has no recovery
middleware either. A Go panic in any goroutine terminates the process: the
safety-net defer (handler.go:877-893) may emit one failed terminal during
unwind, then every in-flight turn, parked turn and session dies with the
daemon. Latent trigger example: dispatcher.go:1377-1380 logs classifyErr
and continues, then dispatcher.go:1395 dereferences intent unconditionally
(currently unreachable because classifyIntent never returns (nil, err),
but one future return flips it into a daemon crash).
Fix sketch: wrap handleRequest and rpc dispatch in recover that emits a
failed turn.terminal (handler_case=panic) and keeps the loop alive.

A0-2. [MAJOR] PublishBlockingT can silently LOSE the terminal event on a
full subscriber buffer: the 5s timeout logs Error and moves on, the event
is gone, no retry, no dead-letter.
Evidence: internal/bus/bus.go:249-258 (direct subs) and 278-287 (wildcard
subs): select case <-time.After(5*time.Second) -> logger.Error("bus:
blocking publish timed out (buffer full)"), no requeue. No-subscriber
publish of a blocking event is also a drop (WARN only, bus.go:192-197).
publishTurnTerminal ignores the delivered count (handler.go:2035). For a
chat.submit client whose WS relay subscriber stalled >5s at the exact
terminal moment, the turn's ONLY result event is lost; the registry row is
then Completed (handler.go:1492) so the watchdog cannot re-emit it either.
There is no turn-status query/replay RPC (grep turn.status/turns.get in
internal/rpc: none).
Fix sketch: on delivered==0 or timeout, persist the terminal to a replay
store keyed by turn_id and expose a turn.get RPC; or retry with backoff.

A0-3. [MAJOR] Async plan dispatch can vanish on the bus with only a WARN:
publishPlanRequest marshals the orchestrator.plan request and on marshal
error returns silently (handler.go:1796-1799); a zero-delivery publish logs
"Plan request published with no subscribers" (handler.go:1810-1814) and the
turn still terminalizes parked (async ack). Non-blocking Publish also drops
on full buffer (bus.go:259-270). The task row exists, no work ever starts,
no task.failed ever fires; the user waits 600s for an opaque turn_reaped.
Fix sketch: treat delivered==0/marshal failure as dispatch failure: emit
failed turn.terminal immediately (or fail the task via task.failed).

A0-4. [MINOR] Task relay can lose the originating turn id after a restart
or a premature Complete: publishTaskTerminalRelay recovers the id via
TurnIDForTask (handler.go:2202; turn_registry.go:293-305, in-memory map
only). Unknown -> FRESH turn id (handler.go:2203-2205), invisible to the
awaiting client. Ordering at dispatch is correct (attachTask at
handler.go:1160 BEFORE publishPlanRequest at 1170/1217), so the in-process
race window is closed; the residual loss paths are daemon restart (registry
is memory-only, turn_registry.go:35-38 comment admits it) and any path that
Completes the turn before the relay (watchdog reap at 600s, then a late
task.completed relays under a fresh id while the client already saw
failed). Status sequence parked -> failed(reaped) -> completed(fresh id).
Fix sketch: persist the task->turn edge (tasks table column) and resolve
the relay id from the store, not only the live registry.

A0-5. [MINOR] Watchdog liveness is fed by only two sources, and neither
covers a long single LLM call on an interactive turn: worker lifecycle
events touch via publishWorkerEvent (handler.go:1987-1991) and task.progress
touches via handleTaskProgressLiveness (handler.go:2631-2644). agent.progress
("thinking"/"executing"/"streaming", loop.go:5196, 5945, 9661, 9715) updates
worker state (handler.go:547-594) but only touches the registry when the
stage CHANGED (state_changed publish at handler.go:589-592); a steady stream
of identical stages never touches. A direct/routed turn whose single LLM
call or backoff chain runs >600s without a stage change is reaped as failed
while still executing; the late completion then fires a second terminal
(watchdog is mark-and-continue, turn_watchdog.go:14-17).
Fix sketch: touch the tracked turn on EVERY agent.progress for its
conversation, not only on state changes.

## A1. LLM provider error (non-quota: HTTP 500/400, connection refused)

Path: chatWithFailoverRaw (loop.go:7004). Generic error branch records alias
failure and attempts rotation bounded by the shared backoff budget
(loop.go:7634-7684; LLMBackoffConfig MaxAttempts=5, backoff.go:373-380);
transport timeout with a live endpoint cooldown surfaces directly
(loop.go:7692-7697); otherwise the raw error returns (loop.go:7699).
reasoningCycle wraps "LLM call failed: %w" (loop.go:5701, 5744-5755) and
runs state-based recovery; RunOnceWithParts keeps the loop's own wrap-up
text when present, else "I encountered an error during processing. Please
try again." and returns (errorMsg, err) (loop.go:3749-3770). Handler:
response.Error=err.Error(), reply=loop text (handler.go:1353-1356) ->
funnel status=failed with the raw wrapped error in Error
(handler.go:1433-1441). Routed path wraps once more: "agent execution
failed: %w" (dispatcher.go:3326-3328).

A1-1. [GOOD, verified] Non-quota provider errors reach turn.terminal failed
with honest text on direct and routed paths. No silent drop. Error quality:
technical but truthful ("LLM call failed: ... connection refused"); the
generic fallback sentence is actionable-lite ("Please try again").

A1-2. [MAJOR] All-endpoints-blocked timeouts PARK instead of failing
(error-driven, no config switch): when every alias candidate sits in an
endpoint cooldown the turn parks on the throttle machinery
(loop.go:7107-7118 resolve-time, loop.go:7655-7666 rotation-time;
parkEndpointBlockedTurn loop_park.go:522-557). See A2 for the parked-turn
user surface; a dead provider thus produces a parked turn, not a failed one.

A1-3. [MINOR] Absorbed-failure shape ("", nil) exists in the state layer:
reasoningCycle's handleErrorBasedOnState can absorb an LLM failure and
return ("", nil) (loop.go:5755+ comment block; the resume paths explicitly
document this at handler.go:2891-2898). The RESUME paths disambiguate via
HasPendingForConversation and terminalize honestly (handler.go:2899-2917,
3370-3403), but the FRESH handleRequest path has no such guard (see A2-1):
an absorbed failure on a fresh turn ships as completed with an empty reply.

## A2. Quota 429 / rate-limit parking (TurnParker, park stores, resume)

Mechanics verified:
- QuotaResetError is produced by the HTTP client purely from the response
  shape (429 quota-window / 402): internal/llm/client.go:2147-2157
  (classifyQuotaDecision + ParseQuotaResponse). NO config gate.
- Loop quota branch: episode Enter + resolver BlockQuotaEntry/Credential +
  rotation attempt; when rotation is impossible the original QuotaResetError
  returns for caller-side parking (loop.go:7506-7553).
- Handler quota park: on llm.AsQuotaResetError with a knowable unblock time,
  QuotaResumeWatcher.Park stores the turn; reply becomes UserMessage +
  "your message is queued and will run automatically when the quota resets."
  and Error is cleared -> funnel status=parked (handler.go:1329-1352,
  1428-1431). Park refuses waits > maxWait (quota_resume.go:292-299) and
  zero/past unblock times (285-291); refusal leaves status=failed with the
  quota UserMessage (honest, no park).
- Resume IS automatic: QuotaResumeWatcher.Start spawns the inner TurnParker
  poll loop (quota_resume.go:236-252; parked_turn.go:271-306), default poll
  10m (parked_turn.go:45), overridden to llm.quota_retry.defer_check_interval
  (default 10m) for the throttle parker (components.go:4506). drainDue
  resumes due records oldest-first, synchronously (parked_turn.go:530-654).
  Nobody external triggers resume; it is purely time-driven.
- Resume success: persistExchange + sendResponse + turn.terminal completed
  under the parked TurnID (handler.go:2920-2946). Resume failure: re-park on
  fresh quota error with Attempt+1 (handler.go:2847-2875), else
  failQuotaResumeTurn -> failed terminal "Auto-resume failed: ..."
  (handler.go:2756-2783). Attempt cap: DefaultQuotaMaxAttempts=5
  (quota_resume.go:48); capped records give up via giveUpQuotaParkedTurn ->
  same failed terminal "quota re-park cap reached after N attempts; turn
  abandoned" (handler.go:2793-2802; drain split parked_turn.go:578-617).
- Persistence: parks.db opened unconditionally (components.go:3342-3348);
  parked_turns table with owner column partitioning re-arm across owners
  quota-watcher|throttle|gate|episode|budget (park_store_sqlite.go:113-144).
  parking.queue_backend (default false, schema.go:143, default 4831) swaps
  storage to unclaimable jobs rows (park_backend_queue.go:1-30), never
  disables parking. Delete-before-callback gives at-most-once resume across
  restarts (parked_turn.go:587-594).
- Throttle park (ThrottleBackoffError, load-shedding 429): parkThrottledTurn
  (loop_park.go:360-486) schedules via the failure-policy BackoffPlan;
  wait > maxWait -> ThrottleGiveUpError surfaced (loop_park.go:381-397),
  which reasoningCycle passes through verbatim (loop.go:5740-5742) -> failed
  terminal. Successful park -> ErrTurnParked -> reasoningCycle returns
  ("", nil) (loop.go:5625-5631, 7396) -> RunOnceWithParts parked guard
  returns ("", nil) (loop.go:3795-3802). Resume: resumeThrottledTurn re-runs
  the ORIGINAL payload and delivers completed/failed terminals under the
  payload TurnID (loop_park.go:564-804, terminals at 676-686, 704-714,
  788-797).

Findings:

A2-1. [CRITICAL] A FRESH interactive turn that throttle-parks mid-flight is
reported to the user as turn.terminal status=COMPLETED with an EMPTY reply,
and the registry row is completed immediately. The real answer arrives
later under the same turn id (duplicate terminal), or never.
Evidence chain: park success -> ("", nil) (loop.go:5625-5631, 3795-3802);
handleRequest direct mode takes reply="" err=nil (handler.go:1254); the
funnel's parked detection ONLY fires when err != nil (handler.go:1427-1431)
so turnStatus stays "completed" (1424); publishTurnTerminal completed with
Reply="" (1457-1473); completeTurn tombstones the id (1491-1492). The
HasPendingForConversation disambiguation that the quota/budget RESUME paths
use (handler.go:2900, 3371) does NOT exist in handleRequest. The routed path
is worse: RouteToAgent returns ("", nil) (dispatcher.go:3321-3328, 3399)
and the handler records the empty exchange into session history
(recordOnTaskPath, handler.go:1409-1411). Only the agent.quota_wait park
event (loop_park.go:467) hints at the deferral; a turn-scoped client sees
"completed, no answer". The loop-side test pins ("", nil) as the park
contract (loop_throttle_park_test.go:142-145) but no handler test pins the
fresh-turn terminal status, so the gap is untested.
Fix sketch: in handleRequest, when reply=="" && err==nil, check
loop.TurnParker().HasPendingForConversation(conversationID): if parked emit
status=parked with a queue notice and keep the registry row; else emit
failed (absorbed-failure shape, mirroring handler.go:2911).

A2-2. [MAJOR] Routed (RouteToAgent) throttle parks lose the turn identity:
WithChatTurnID is stamped ONLY on the direct-mode call (handler.go:1254);
RouteToAgent's RunOnce/RunOnceWithParts calls carry no turn id
(dispatcher.go:3321-3325, zero WithChatTurnID hits in dispatcher.go). The
park payload TurnID is therefore "" (loop_park.go:332), and the resume
explicitly SKIPS client delivery and turn.terminal for legacy/empty-TurnID
rows (loop_park.go:750-761). A routed chat.submit turn that parks on
throttle never receives its resumed answer as a terminal event; the user
already got the false "completed" of A2-1.
Fix sketch: stamp WithChatTurnID(ctx, req.TurnID) on the routed path too
(thread it through DispatchResult or ClassifyAndRoute ctx).

A2-3. [MAJOR] Parked turns have no liveness and no upper bound visible to
the user beyond maxWait=24h: while parked, nothing touches the registry row
(no Touch call in any park path; grep Touch: handler.go:1991, 2642 only), so
the watchdog reaps a legitimately quota-parked turn at 600s as failed "turn
reaped" (turn_watchdog.go:136-150) even though the parker will resume it
hours later and emit a second terminal under the tombstoned id. Quota park
reply promises automatic delivery; the watchdog contradicts it. The
reaper's Complete also breaks the resume's TraceForTurn correlation
(handler.go:2935). Status sequence: parked -> failed(reaped, 600s) ->
completed(resume, up to 24h+ later).
Fix sketch: exclude turns with a live park record from Stale(), or Touch
the registry row on park and on each drain pass; alternatively emit the
reap only when no park row exists for the conversation.

A2-4. [MAJOR] If resume never comes (daemon down at resume time, park row
lost, parker not started): the turn is gone with at most a log line.
TurnParker.Stop drops non-persisted records with a log (parked_turn.go:313+
comment "without persistence they are logged and dropped"); the resume
callback panic path drops the record (parked_turn.go:638-646); an
undecodable/empty throttle payload is dropped with Warn and NO terminal
even when TurnID is known (loop_park.go:568-583); resumeRouterDefault drops
unknown-class records (handler.go:2743-2748). Persistence + re-arm
(parked_turn.go:846+) covers restarts for wired stores, but the
payload-drop paths above never terminalize the awaiting turn. The watchdog
is the only backstop and only while its row survives (see A2-3: it reaps at
600s, which at least gives the user a failed event, opaque text).
Fix sketch: every drop path with a known TurnID must emit a failed
turn.terminal (handler_case=park_drop) before deleting the record.

A2-5. [MINOR] Quota park with an UNKNOWN reset time does not park at all:
unblockAt zero -> Park skipped (handler.go:1336 guard) -> status=failed
with the quota UserMessage. Honest, but the user must retry manually; no
config estimate is applied at this seam (llm.quota_retry.default_estimate
exists, schema.go:1537, but the handler does not use it here).
Fix sketch: fall back to DefaultEstimate when the provider discloses no
reset time, so the turn parks instead of failing.

A2-6. [GOOD, verified] The parked turn DOES resume automatically when the
window passes: time-driven drain (parked_turn.go:530-654), attempt growth
across generations (loop_park.go:605, handler.go:2866), bounded give-up
with honest failed terminals (quota cap 5: quota_resume.go:48,
handler.go:2793-2802; throttle MaxWait give-up: loop_park.go:381-397).
Re-park preserves TurnID (handler.go:2859, loop_park.go:610-612). The
owner column prevents cross-owner replay (park_store_sqlite.go:110-122).

## A3. Context overflow (ContextOverflowError, firewall compaction)

Path: chatWithFailoverRaw overflow branch (loop.go:7308-7380). Small-request
overflow is treated as endpoint saturation: backoff + same-payload retry,
capped maxSaturatedOverflowRetries=3 (loop.go:7327-7352, 7714). Otherwise
ONE aggressive compaction via compactForOverflow (loop.go:7363-7379,
7760+) and retry; a SECOND overflow fails honestly: "context overflow
persisted after compaction: %w" (loop.go:7354-7361); compaction that cannot
shrink fails "context overflow and compaction exhausted: %w"
(loop.go:7365-7370). All return errors flow through reasoningCycle ->
RunOnceWithParts (loop.go:3749-3770) -> handler funnel status=failed with
the wrapped text. ContextTooSmall admission verdicts surface verbatim,
NonRetryable, no rotation (loop.go:7598-7608).

A3-1. [GOOD, verified] Compaction retry that also overflows produces an
honest failed turn.terminal with actionable-ish text naming provider/model
in the log and the overflow reason in Error. No silent drop, no infinite
retry. The one-shot contract is honored (overflowRetried flag,
loop.go:7355, 7363).

A3-2. [MINOR] The user-facing Error is the raw wrapped chain ("LLM call
failed: context overflow persisted after compaction: ..."); no remediation
hint (start a new conversation / pick a bigger-context model). The firewall
itself is config-gated (llm.context_firewall.enabled, schema.go:1482,
default true; off-switch verified at internal/llm/context_firewall.go:587-
589) but the overflow RETRY logic is error-driven and always active.
Fix sketch: map ContextOverflowError to a friendly message at the handler
error seam like BudgetExceededError.UserMessage (handler.go:1304-1306).

## A4. Tool execution error inside a step

Path: executeToolCalls (loop.go:9006) -> hook denies (hard security deny =
failed ExecutionResult "blocked by security policy: <reason>",
loop.go:9069-9083; soft decision-gate deny = requires_confirmation
handshake, loop.go:9085-9099) -> executeToolCallsUngated (loop.go:9161):
nil executor -> failed results "tool execution not configured"
(loop.go:9202-9212); executor.ExecuteAll returns per-tool results; failures
are fed back to the model as tool results, NOT surfaced as turn errors.
Repeat-error breaker vetoes identical failing inputs after the budget
(loop.go:9040-9063, "[refused without execution: this identical input
already failed the maximum number of times]"). The model then finishes the
turn with text; turn.terminal completed fires normally. On the task/step
path a failed tool result can fail the STEP -> job failure -> class 7 path.

A4-1. [GOOD, verified] Tool errors inside an interactive turn never drop
the turn: they become model-visible tool results and the turn completes
with the model's explanation. Error quality depends on the model; the raw
tool error is preserved in the result map.

A4-2. [MINOR] Executor confirmation dead-end on step jobs:
NeedsConfirm returns a bare failed result "action requires user
confirmation" with no confirmation seam (internal/agent/executor.go:1556-
1564, comment "we don't support async confirmation"); employee RequiresPlan
escalations map here (pkg/security/permissions.go:546-555). Opaque, no
approve path; the turn still completes with model text.
Fix sketch: route NeedsConfirm into the requires_confirmation shape.

## A5. Classifier unavailable / LLM classifier failure (60s cooldown)

Verified: cooldown 60s (llm_classifier.go:37), per-call timeout 10s (:34),
boot wait 90s (:44). Failure arms unavailable (llm_classifier.go:583-593,
704-718); cooldown short-circuit :534-563. Dispatcher.classifyIntent
(dispatcher.go:1554-1984) NEVER returns an error: LLM failure -> keyword
(:1899-1920) -> semantic (:1922-1948) -> heuristic (:1950-1963) -> final
QuickPlan fallback (:1965-1982). ClassifyAndRoute only logs classifyErr
(dispatcher.go:1377-1380).

A5-1. [GOOD, verified] The turn ALWAYS completes via fallback when the
classifier LLM is down; classification failure cannot fail the turn.
Boot-gate failure never fails a turn either (classifier_boot_readiness.go:
134-151).

A5-2. [MAJOR] Silent degradation: ClassificationNotice is declared
(dispatcher.go:231-234) and consumed (handler.go:1049-1058, 1258-1264) but
NEVER assigned anywhere in non-test code (repo-wide grep). The well-written
llm.ClassificationUserGuidance (internal/llm/errors.go:591-606) has zero
callers. The user cannot tell routing was degraded/misrouted.
Fix sketch: populate ClassificationNotice from ClassificationUserGuidance
in the fallback branches.

A5-3. [MINOR] Cooldown/timeout kinds fall into the keyword table although
the comment documents a chat-fallback policy for a down LLM lane
(dispatcher.go:1876-1895: only empty_response takes the chat branch).
Misroute risk on task-shaped prompts during the 60s window.

A5-4. [MINOR] ClassifyMulti failures arm the SAME shared 60s cooldown
(llm_classifier.go:704-718), degrading unrelated single-intent turns.

## A6. Agent runtime dead / loop closed / manager missing agent

Verified: missing agent -> resolveAgent falls back to chat agent, then
error "no agent available for %q" (dispatcher.go:3265-3267, 3707-3712) ->
handler funnel failed. Nil registry -> "no agent registry configured"
(dispatcher.go:3080-3082). Nil result/intent -> nil_result_error "dispatch
returned no actionable result" (handler.go:1221-1226). Nil LLM client ->
ErrNoLLMClient "no LLM client configured" (loop.go:56, 3334-3336) ->
failed terminal (routed wrap dispatcher.go:3326-3328). Manager.
GetOrCreateWired errors are swallowed with Warn + singleton fallback
(dispatcher.go:3635-3641; handler.go:3527-3537).

A6-1. [GOOD, verified] Missing agent / nil registry / nil LLM client all
reach turn.terminal failed with honest (technical) text. No hang, no drop.

A6-2. [MAJOR] No closed-loop guard exists: AgentLoop.Close sets closed
(loop.go:4442-4451) but RunOnceWithParts never checks it; ErrLoopClosed
does not exist anywhere (grep: zero hits). A turn submitted to a closed
loop RUNS on torn-down state (rewake consumer stopped, isActive=false) and
"completes" with no signal. Manager.GetOrCreateWired can hand out a cached
loop another path closed (manager.go:109-141 eviction race).
Fix sketch: check l.closed at RunOnceWithParts entry and return a typed
ErrLoopClosed; rebuild closed cached loops in GetOrCreateWired.

A6-3. [MAJOR] Dispatch can block FOREVER in staleLoop.Close():
GetOrCreateWired calls staleLoop.Close() synchronously on workingDir change
(manager.go:131-135); Close does wg.Wait() (loop.go:4446) and wg-tracked
shadow-capture goroutines run with context.Background() and no timeout
(loop.go:5903-5915, 6571-6585). One hung capture freezes the next turn for
that session; the chat handler goroutine is SERIAL (handler.go:386-399), so
ALL chat traffic stalls. No terminal while blocked (the safety net fires
only on exit); the watchdog reaps tracked turns at 600s but the goroutine
never recovers.
Fix sketch: bound shadow-capture contexts; give Close a wait deadline or
run stale Close asynchronously.

A6-4. [MINOR] Steer lane: "message queued (steer/follow-up)" returns as a
COMPLETED turn (dispatcher.go:3191/3205) but the queued message can be lost
if the owning loop exits before drain (registry.go:2059-2074 closes the
queue; latest-wins replacement queue.go:240-244). User told "queued", no
answer ever arrives.

## A7. Queue job dead-letter / max retries exceeded

Baseline (GOOD): worker failJob (internal/worker/worker.go:418, 690-693) ->
PersistentQueue.FailAttempt (internal/queue/queue.go:661) -> Store.
FailAttempt decides dead at retryCount >= maxRetries (store.go:1150-1153,
moveToDead 1238-1242, 1832) -> queue.job.failed published (queue.go:702-
706) -> Orchestrator.handleJobFailed (orchestrator.go:198, 553-580) ->
TacticalScheduler.OnJobFailed (tactical.go:2469): task.error chat-visible
(2582-2587), retryable requeue (2590-2634), else step failed (2646-2653)
and when no live steps remain task StateFailed + task.failed (2757-2774) ->
handleTaskFailed (handler.go:2297) -> publishTaskTerminalRelay failed
(handler.go:2361-2362) -> turn.terminal failed under the submitted turn id.
So ordinary retry exhaustion DOES reach the user.

A7-1. [CRITICAL] Pair-managed step failure is a silent drop: OnJobFailed's
pair branch marks step+session failed and returns nil BEFORE any task.error
or task finalization (tactical.go:2479-2503, return at 2502). No bus event,
task never terminalizes; user gets only the 600s watchdog reap.
Fix sketch: finalize the task failed + publish task.failed in the pair
branch (reuse tactical.go:2735-2774).

A7-2. [CRITICAL] PairManager never publishes task.completed/task.failed on
ANY terminal path: finalizeTask only writes state (pair_manager.go:354-402,
383/389); RunRound failures publish pair.round_failed (155-211) and
exhaustion publishes pair.exhausted (253-262), but the orchestrator's
handlers for those topics are log-only (orchestrator.go:689-749) and the
ChatHandler pair relay subscribes only pair.result from the OTHER
PairOrchestrator (handler.go:378; pair_orchestrator.go:481-486), which
itself never emits turn.terminal (handler.go:3738-3758). All spec_pair
outcomes are invisible to turn-scoped clients.
Fix sketch: make finalizeTask publish task.completed/task.failed.

A7-3. [MAJOR] Crash-stranded exhausted jobs are dead-lettered by the
sweeper with NO failure event: SweepAbandonedTerminalStates
(store.go:1910-1974, moveToDead at 1958; started daemon.go:2617) publishes
nothing; step/task never terminalize. Watchdog-only, opaque.
Fix sketch: sweeper publishes queue.job.failed per moved row.

A7-4. [MINOR] queue.job.failed carries no state/dead/retry_count
(queue.go:702-706); the user reply shows only the last raw error truncated
to 200 chars (handler.go:2377), never "gave up after N retries". Non-step
jobs (memory distill) dead-letter silently by design (tactical.go:2474-
2476) - acceptable, no chat turn involved.

A7-5. [MAJOR] Worker provider-wait requeue defers quota/throttle job
failures WITHOUT consuming retries and without any user surface
(worker.go:596-671, call site 389): error-driven, horizon-clamped, no
config switch (see Section B). The chat turn stays parked-ack until the
job eventually fails or the watchdog reaps.

## A8. Turn stall / reap (turn_watchdog.go)

Exact emission (turn_watchdog.go:136-150): Status "failed", HandlerCase
"turn_reaped", Error fmt.Sprintf("turn reaped: no progress for %s",
staleAfter) -> with defaults "turn reaped: no progress for 10m0s", Reply
"this turn stopped responding and was marked failed". Trace correlation
from the registry record. Complete AFTER emit; per-record panic isolation
(safeEmit 171-181). Mark-and-continue: never cancels work, never
blind-retries (contract comment 14-17) - verified.

A8-1. [GOOD, verified] Stalled turns surface failed, never blind-retried.
Liveness sources: worker lifecycle events (handler.go:1987-1991) and
task.progress (handler.go:2631-2644). Config-gated, fully disableable
(daemon.go:310-326).

A8-2. [MAJOR] False reaps of LEGITIMATELY waiting turns (approval waits,
parked turns, long single LLM calls): see A2-3 (quota/throttle parks),
A9-2/A10-2 (approval/seal waits), A0-5 (steady-stage interactive turns).
The reaper cannot distinguish "stuck" from "waiting for a human/provider";
the user gets a misleading failed terminal, then possibly a second
completed terminal under a tombstoned/fresh id.
Fix sketch: exclude park-backed and approval-waiting turns from Stale().

A8-3. [MINOR] Doc drift: StaleAfterSeconds field comment says "0 -> 120"
(schema.go:3928) while header (3921) and DefaultConfig (4952) use 600.

## A9. Plan compile failure

Compiler: plan.CompileSealed (internal/plan/compiler.go:23), deterministic,
zero-LLM, CompileError aggregates {Line, Message} (compiler.go:41, 61-66).

A9-1. [GOOD, verified] Critique-loop compile failure is honest and
actionable: first rejection -> ONE compile-fix revise round; second
rejection or revise transport failure -> failTaskWithReason
(plan_critique_loop.go:712-733) which persists StateFailed and publishes
task.failed (strategic.go:1905-1929) -> turn.terminal failed via the relay.
Reason text carries line numbers + compiler problems, bounded 800 runes
(plan_critique_loop.go:1043-1053); chat reply truncates to 200 chars
(handler.go:2377) but the event Error keeps the full text.

A9-2. [MAJOR] Non-compile Plan() failures are log-only: handlePlanRequest
logs "Strategic planning failed" and returns on ANY strategic.Plan error
with no task.failed and no state write (orchestrator.go:369-375). Silent
origins: seedDraftFromRequest (strategic.go:925-928), step persist
(strategic.go:1071-1074), quick_plan critique-flow error
(strategic.go:866-869; plan_critique_loop.go:724 does NOT fail the task),
self-seal errors (plan_critique_loop.go:783-788). Task stuck StatePlanning;
watchdog-only opaque failure at 600s.
Fix sketch: call failTaskWithReason in handlePlanRequest's error branch.

A9-3. [MAJOR] plan.seal RPC returns compile problems synchronously
(status="problems", rpc/plan_seal.go:146-158 - good for the RPC caller) but
the chat-originated task waits in StatePlanning for a human seal with no
liveness; the watchdog reaps the turn at 600s as failed, then a later seal
+ completion relays completed under the tombstoned id (false-failure
sequence, same shape as A10-2).

A9-4. [MAJOR] Post-approval synthesis failure is invisible: ApprovePlan
publishes plan.approved then Synthesize; on error it stores synthesis_error
and returns nil (plan/manager.go:594, 618-620, 634-645); exhaustion parks
the plan StateFailed with only a kanban-gated publishPlanState
(manager.go:1119-1136, 1876-1893); failPlan deliberately publishes no bus
event (manager.go:1626-1655). The user approved and never sees work start
or fail.
Fix sketch: publish a session-scoped notification or plan.failed the chat
handler relays.

## A10. Approval rejection / denial

A10-1. [CRITICAL] task.reject RPC APPROVES the plan: TaskApprovalHandler
has only ApproveFunc (internal/rpc/task_approval.go:18-22); handleReject
calls h.ApproveFunc(ctx, req.TaskID) at task_approval.go:69 and returns
{"status":"rejected"} (:72). Daemon wiring passes Strategic().ApprovePlan
as the only func (daemon.go:2161-2163, verified). ApprovePlan persists
pending steps, sets StateExecuting and schedules them
(strategic.go:2243-2332). The correct StrategicPlanner.RejectPlan
(strategic.go:2336-2363, StateRejected + task.rejected) has ZERO callers
(grep verified). A user rejecting a plan via TUI (internal/tui/rpc.go:926-
930) or HTTP (internal/transport/http_client.go:707-708) gets a "rejected"
ack while the daemon EXECUTES the rejected plan.
Fix sketch: add RejectFunc, wire Strategic().RejectPlan, call it in
handleReject.

A10-2. [MAJOR] Even a correctly wired rejection yields no turn terminal:
task.rejected (strategic.go:2356-2359) has NO subscribers (ChatHandler
subscribes only task.completed/task.failed/task.progress, handler.go:359-
368). The awaiting turn hangs until the 600s reap with opaque text instead
of "rejected: <reason>". Same for the approval WAIT itself:
awaitUserApproval publishes task.pending_approval (strategic.go:2194-2239,
2230) with no chat-side park/liveness; GateParker parks only PlanManager
plan.submitting events (daemon/gate_park_wiring.go:162-198), not this
strategic task gate. False reap at 600s, then completed after approval.
Fix sketch: subscribe task.rejected -> failed terminal with reason;
park/heartbeat approval-waiting turns.

A10-3. [GOOD, verified] GateParker mechanics for PlanManager plans are
sound: 10-year horizon, never auto-expire (gate_park.go:43-51); approval
resumes via ResumeByKey delete-before-callback (240-245, 393-436);
plan.rejected/plan.cancelled release WITHOUT resuming (gate_park.go:322-
350, wiring gate_park_wiring.go:206-230). Release emits an agent.quota_wait
lifecycle event (gate_rejected), not a turn.terminal - acceptable for
RPC/employee-originated plans, noted.

A10-4. [MINOR] Decision-gate deny is a soft confirmation handshake, not a
hang: BeforeToolCall Block -> requires_confirmation tool result
(internal/tools/builtin/decision_gate.go:133-234; loop.go:9085-9099);
confirmed=true re-invoke bypasses (decision_gate.go:156-173); the model
finishes with text, turn.terminal completed. Gate FAILS OPEN on decision
client error (decision_gate.go:176-186) - security posture, not a drop.

## A11. Employee gate failure

A11-1. [GOOD, verified] Constitution pre-exec denies are actionable and
audited: PreExecChecker.Check denies with specific reasons
(internal/employee/enforcement.go:614, 672-677, 692-696, 722-729; never[]
critical deny + auto-pause 617-628, 992-999). Deny reaches the model as a
failed tool result "permission denied: <reason>" (executor.go:1549-1553,
2085-2138; pkg/security/permissions.go:516-559); audit findings recorded
(enforcement.go:931-957); auto-pause publishes employee.paused
(employee/manager.go:1537-1563). Turn completes with text; no hang.

A11-2. [GOOD, verified] Goal quality-gate failure never claims success:
GoalLoop.Reflect degrades to GoalAtRisk on gate error/failed run
(internal/employee/goal_loop.go:1295-1323), health persisted + notify
(1330-1359). Autonomous rounds, not chat turns; no terminal expected.

A11-3. [MAJOR] Rejecting an employee goal plan strands the GOAL gate park
forever: goal gates park keyed "goal|<goalID>" (goal_loop.go:1534-1545);
rejection flows plan.rejected -> ReleaseByKey(planID)
(gate_park_wiring.go:218) which probes "plan|<id>" and "goal|<id>" with the
SAME id (gate_park.go:328-339, verified) - never matches the goal row. No
other release caller exists; the 10-year horizon row is never auto-expired
(gate_park.go:24-26, 51); the goal stops running rounds indefinitely,
including after approval-timeout auto-reject
(employee/scheduler_jobs.go:438-439). Silent dormancy.
Fix sketch: also ReleaseByKey(goalID) on goal-plan rejection.

## A12. chat.submit itself failing

Verified: BuildChatSubmitAck (internal/rpc/chat_submit.go:111-226) is the
single validation+publish+ack seam for RPC and HTTP. Invalid JSON -> RPC
error "invalid params" (:113-115). Empty/whitespace message -> ack
accepted=false, note "message is required", turn_id "" (:123-131) - nothing
published, no terminal expected (correct: no turn exists). Session not
found -> NOT an error: conversation resolution falls through to a fresh
conv id (:149-159); the turn runs unbound (filesystem tools then fail
actionably, handler.go:3497-3508 Warn). Idempotent retry: Register existing
-> ack accepted=true "turn already submitted", no re-publish (:161-177).
Marshal failure -> RPC error (:196-199). Nil bus -> publish skipped but ack
STILL accepted=true (:201-211, 219-225).

A12-1. [MAJOR] Nil-bus or zero-subscriber submit acks accepted=true while
the chat.request is dropped: h.bus.Publish is non-blocking drop-on-full
(bus.go:259-270) and no-subscriber drop (bus.go:177-218); the ack path
ignores delivery entirely (chat_submit.go:210). The turn id IS registered
(:162) so the watchdog reaps it at 600s as failed "turn reaped" - the user
gets an opaque failure 10 minutes after an "accepted" ack, and every retry
of the same turn_id is suppressed as duplicate (turn_registry.go:95-99)
until the reap. With the watchdog disabled: hang forever.
Fix sketch: check Publish delivery count; on 0 return accepted=false with
a retryable note and do NOT register the turn id.

A12-2. [MAJOR] No panic recovery on the submit path (see A0-1): a panic in
BuildChatSubmitAck or any RPC handler crashes the daemon; the client sees a
dropped connection, no ack, no terminal. HTTP /api/v1/chat/submit
(internal/comm/http/api_handlers.go:2309+) shares the seam and the
middleware chain has no recover (server.go:1985+).

A12-3. [GOOD, verified] Handler-side invalid payload / empty message emit
failed terminals with actionable text (handler.go:766-778, 796-812);
duplicate chat.request emits parked, never completed (handler.go:904-918).
The submit-side empty-message rejection is an ack error (accepted=false),
not a terminal - correct since no turn was minted.

## A13. Explicit enumeration: chat requests that fail OUTRIGHT with no
user-visible recovery path (user requirement: none should)

1. Daemon-killing panic on any chat/RPC/HTTP path (A0-1): all in-flight
   requests die with the process; at most one unwind terminal per claimed
   turn. handler.go:386-399, rpc/server.go:532, no recover anywhere.
2. Fresh throttle-park reported completed+empty (A2-1): the request
   "succeeds" with no answer; routed variant never delivers the resume
   (A2-2). handler.go:1424-1493, loop.go:3795-3802, loop_park.go:750-761.
3. Terminal event lost to a full bus buffer (A0-2): completed work, no
   event, no replay surface. bus.go:249-258, handler.go:2035.
4. Plan request dropped on publish (A0-3): parked ack, no work, 600s
   opaque reap. handler.go:1796-1814.
5. Pair-managed job failure / PairManager any outcome (A7-1, A7-2): zero
   events; 600s opaque reap; task row never terminal. tactical.go:2479-
   2503, pair_manager.go:354-402.
6. Plan() seed/persist/seal errors (A9-2): log-only; 600s opaque reap.
   orchestrator.go:369-375.
7. Post-approval synthesis failure (A9-4): no event at all, ever (kanban-
   gated only). plan/manager.go:634-645, 1626-1655.
8. task.reject executes the plan (A10-1): the rejection itself is dropped;
   user-visible outcome is the OPPOSITE of the request.
   rpc/task_approval.go:69.
9. Rejected/awaiting-approval turns (A10-2): no terminal until false reap.
   strategic.go:2356, 2230.
10. Goal-gate rejection strands the goal (A11-3): silent permanent
    dormancy. gate_park.go:328-339 vs goal_loop.go:1536.
11. Sweeper dead-letter without event (A7-3): crash-window silent drop.
    queue/store.go:1958.
12. Park payload drops with known TurnID (A2-4): undecodable/empty payload
    or panic in resume -> log-only drop. loop_park.go:568-583,
    parked_turn.go:638-646.
13. Submit ack accepted while chat.request dropped (A12-1): 600s opaque
    reap or forever hang with watchdog off; retries suppressed meanwhile.
    chat_submit.go:201-225.
14. Hung staleLoop.Close freezes ALL chat (A6-3): no terminal while
    blocked; serial goroutine. manager.go:131-135, loop.go:4446.
15. Closed-loop turns run degraded with no signal (A6-2). loop.go:4442+.
16. Steer "queued" message lost on loop exit (A6-4). dispatcher.go:3191.

Backstop note: items 3-7, 9, 11, 13 are bounded ONLY by the turn watchdog
(600s default, config-disableable). With orchestrator.turn_watchdog.
enabled=false they become permanent hangs. Items 1, 2, 8, 10, 12, 14-16
have NO backstop at all.

===========================================================================
SECTION B - QUOTA-DISABLED BEHAVIOR
===========================================================================

## B1. What llm.budget.enabled=false actually disables

The ONE global switch is applied at a single mapping seam:
llmBudgetConfigFromConfig (internal/daemon/daemon.go:65-82): Enabled=false
returns an ALL-ZERO llm.BudgetConfig. NewBudget with all-zero limits
enforces nothing: checkBudgetLocked short-circuits Exceeded=false when
every limit is zero (internal/llm/budget.go:927-931); RPM wait returns
immediately at rateLimitRPM<=0 (budget.go:734). The tracker is still built
unconditionally (components.go:1289) and still RECORDS usage, but no
BudgetExceededError can ever be produced.

Enforcement sites that are therefore inert with enabled=false (all read the
same tracker): client.go:820, 1107, 2700 (CheckBudgetWithScope before LLM
calls), anthropic.go:413, 661; handler.go:1127 (async dispatch pre-check,
budget_blocked case 1140-1147); budget park seam handler.go:1304-1326
(fires only on BudgetExceededError + time-windowed reason,
isTimeWindowedBudget budget_resume.go:19); BudgetResumeWatcher resume gate
budget_resume.go:607.

Hierarchical agent.budget: NO enable flag of its own (schema.go:2702-2708
comment, deliberate). agentBudgetEnabled = LLM.Budget.Enabled &&
Agent.Budget.Total > 0 (daemon.go:89-94); hierarchy wired only at
daemon.go:926-935 (SetBudgetConfig -> loop.go:10678). Default Total=0
(schema.go:4662) so it is off twice over. agent.budget.phases.enabled
cannot turn anything on by itself (schema.go:2717-2720).

What enabled=false does NOT disable (verified error-driven, see B2):
QuotaResetError/ThrottleBackoffError production, quota chat park, throttle
park, endpoint-blocked park, quota episode tracker, worker provider-wait
requeue, tactical quota deferral, resolver endpoint cooldowns, classifier
LLM calls, firewall summarizer calls.

## B2. Does provider-side parking still fire with budget disabled? YES.

Every parking mechanism below is ERROR-DRIVEN (fires on provider response
shape), gated only by wiring presence, never by llm.budget.enabled nor by
llm.quota_retry.enabled:

B2-1. [MAJOR] Chat quota park (QuotaResumeWatcher): SetQuotaResumeConfig
(cfg.LLM.QuotaRetry.MaxWait) is called UNCONDITIONALLY at BOTH handler
construction sites (components.go:4447 dispatcher mode, 4960 single-agent
mode; verified). It never reads QuotaRetry.Enabled (handler.go:2808-2817).
Park fires on AsQuotaResetError with non-zero UnblockAt whenever the
watcher exists (handler.go:1336) - always true in the daemon. maxWait<=0
clamps to 24h (quota_resume.go:198-199; llm/client.go:174). NO disable
switch. A budget-disabled rig still parks 429-quota turns for up to 24h.
Fix sketch: honor llm.quota_retry.enabled in SetQuotaResumeConfig wiring,
or add parking.enabled.

B2-2. [MAJOR] Throttle park (TurnParker): built unconditionally in the
dispatcher branch (components.go:4505-4536, verified: NewTurnParker at
4505, SetThrottleParker 4535, Start 4536). parkThrottledTurn is gated ONLY
by l.turnParker != nil (loop_park.go:361). Per-session clones inherit the
parker via ConfigSnapshot (loop.go:10029-10032). NO config switch.
Asymmetry: the single-agent branch (components.go:4932-4961) wires the
quota watcher but NOT the throttle parker - multi_agent.enabled=false is
the only config-reachable way to avoid throttle parking, a side effect.
Registry-built specialist loops are parker-less (registry.go:1007 uses
agentOpts which never include a parker; components.go:2355-2410).

B2-3. [MAJOR] Endpoint-blocked park: same always-wired parker; fires when
all alias candidates are endpoint-blocked after timeout-class failures
(loop.go:7107-7118, 7655-7666; loop_park.go:522-557). No switch. A rig
pointed at a dead/flaky endpoint gets parked turns, not failures.

B2-4. [MINOR] Quota episode tracker: NewQuotaEpisodeTracker built
unconditionally (components.go:2377-2378), Enter on every QuotaResetError
(loop.go:7511-7512); 12h/20h/24h escalation tiers drive agent state
running -> quota_wait -> blocked -> idle (quota_episode.go:106-115, 187).
State/observability only (does not itself park) but unavoidable noise.

B2-5. [MAJOR] Worker provider-wait requeue: requeueOnProviderWait requeues
quota/throttle job failures WITHOUT consuming a retry whenever the queue
is Requeueable and resumeAt <= horizon (worker.go:596-671, call site 389).
Reads only failure_policy horizon; no enable switch; horizon<=0 clamps to
24h (schema.go:1737-1739).

B2-6. [MAJOR] Tactical quota deferral (the "10 deferrals / 6h" logic):
lives in tactical.go, NOT agent_quota_aware.go (that file is a 16-line
AgentHealthSource interface, agent_quota_aware.go:1-16).
DefaultQuotaDeferralPolicy MaxDeferrals=10, MaxTotalDeferral=6h,
UnknownResetDelay=5m (tactical.go:37-43). quotaDeferralScheduled
(tactical.go:1048-1075) requeues quota-failed step jobs. A policy with
MaxDeferrals<=0 DISABLES it (tactical.go:1049-1051) and
TacticalSchedulerConfig.QuotaDeferral is the override seam - BUT the daemon
construction never sets it (components.go:4692-4705, verified) and NO
config key maps to it (grep quota_deferral schema.go: zero hits). Code
seam exists, config wiring missing.

B2-7. llm.quota_retry.enabled=false disables ONLY resolver quota-blocking/
rotation: quotaEnabled() = quotaCfg != nil && Enabled
(internal/llm/resolver.go:258-261, verified) gates the isQuotaBlocked
skips. The wait+retry wrapper quotaWaitChatter (resolver_direct.go:30,
54-66) that honors Enabled has NO production call site (dead wiring).
QuotaResetError production itself is ungated (client.go:2147-2157).

B2-8. Duration knobs cannot be zeroed: NormalizeQuotaRetryDefaults clamps
MaxWait/DefaultEstimate/DeferCheckInterval <=0 back to 24h/1h/10m
(schema.go:1771-1781, verified); NormalizeFailurePolicyDefaults clamps
horizon/base_throttle/poll_floor/short_retries (schema.go:1736-1763).
"max_wait=0" is NOT a disable. Only the Enabled bools are real switches,
and several mechanisms ignore even those (B2-1..B2-6).

## B3. Config keys for a quota-free test rig (toml path = json path)

Budget switches (fully disableable, defaults already off):
- llm.budget.enabled, bool, default false - schema.go:1867, default 4299.
- llm.budget.mode, string, "" -> enforce; "steer" advisory-only -
  schema.go:1868 (irrelevant when disabled).
- llm.budget.hourly_token_limit / daily_token_limit / daily_cost_limit /
  hourly_cost_limit / rate_limit_rpm / per_task_token_limit /
  per_session_token_limit / per_task_cost_limit / per_session_cost_limit:
  all default 0 = unlimited - schema.go:1869-1878, defaults 4300-4309.
- llm.budget.aggressiveness, float, default 0.5 - schema.go:1874 (4305).
- agent.budget.total, int, default 0 (hierarchy never wired) -
  schema.go:2710 (4662); reserved_ratio 0.1 (2711/4663); phases.enabled
  false (2722/4665); warning_thresholds 0.7/0.8/0.9 (2730-2732/4671-4673).

Parking / quota switches:
- llm.quota_retry.enabled, bool, default TRUE - schema.go:1530 (4342).
  Controls resolver blocking ONLY (B2-7). Set false for the rig.
- llm.quota_retry.max_wait, duration, default 24h - schema.go:1534 (4343);
  clamped, cannot zero.
- llm.quota_retry.default_estimate, duration, default 1h - schema.go:1537
  (4344).
- llm.quota_retry.defer_check_interval, duration, default 10m -
  schema.go:1540 (4345); also the throttle parker poll cadence
  (components.go:4506). Lower it (e.g. "5s") to make inevitable parks
  resume fast in the rig.
- parking.queue_backend, bool, default false - schema.go:143 (4831).
  Storage swap only; parks.db opens unconditionally (components.go:3342-
  3348). NO parking off-switch exists.

Throttle / failure policy (no section enable flag):
- llm.failure_policy.horizon 24h (schema.go:1651/4348), base_throttle 30s
  (1655/4349), base_quota_402_extra 5m (1659/4350), poll_floor 1h
  (1663/4351), short_retries 3 (1667/4352) - all clamped <=0 to defaults.
- llm.failure_policy.pacing.enabled, bool, default TRUE - schema.go:1682
  (4354). REAL off-switch: SetPacer only when true (components.go:1415-
  1420); nil pacer never waits (client.go:203-205, 1996). Set false.
- llm.failure_policy.pacing.target_429_per_hour 1 (1686/4355),
  min_interval 1s (1689/4356), max_interval 30s (1691/4357).
- agent.retry.enabled, bool, default true - schema.go:2673-2692 block,
  defaults 4640-4650; false skips the global backoff override install
  (daemon.go:962-991) but NOT the client short-retry loop.

Watchdogs:
- orchestrator.turn_watchdog.enabled, bool, default TRUE - schema.go:3924
  (4950); interval_seconds 30 (3926/4951); stale_after_seconds 600
  (3929/4952; field comment wrongly says 120 at 3928). enabled=false
  constructs nothing (daemon.go:310-326). For a rig: either disable (and
  accept that silent-drop defects become permanent hangs) or keep enabled
  with a small stale_after to surface drops fast.
- agent.watchdog.enabled, bool, default true - schema.go:2870-2881 block,
  defaults 4612-4618; timeout_minutes 10, max_iterations 50. Start no-ops
  when disabled (internal/agent/watchdog.go:103-107). Raise max_iterations
  for long rig runs if kept on.

Classifier LLM: NO enabled flag exists (MAJOR gap). Schema has only
orchestrator.classifier_fail_fast (schema.go:3837-3842, default false
4893), classifier_boot_fail_fast (3854, default TRUE 4897 - set false so a
sick local classifier runtime is not a fatal boot), classifier_prefilter.*
(3862, 3955-3990, default off), classifier.session_state_upgrade (3871).
The dispatcher builds the LLM classifier whenever any client exists
(dispatcher.go:655-682; dedicated classifier_model/small_model from
models.json5 else main-client fallback, components.go:1735-1760). The only
config way to eliminate classifier LLM calls: multi_agent.enabled=false
(schema.go:2948, default TRUE 4561; gate components.go:4082) which removes
the dispatcher entirely (single-agent path components.go:4932-4961) - and
as a side effect removes throttle-parker wiring (B2-2). Short/simple
messages bypass the classifier chain unconditionally
(dispatcher.go:1556-1574).

Context / compaction (removes extra LLM calls):
- llm.context_firewall.enabled, bool, default TRUE - schema.go:1482
  (4326). REAL off-switch (context_firewall.go:587-589). Set false (also
  kills summarize_history summarizer calls).
- compaction.enabled (top-level), bool, default FALSE - schema.go:921-947
  block, default 5146; wired loop.go:3014. Leave off.
- agent.compaction.* is DEAD CONFIG (no consumer; grep cfg.Agent.
  Compaction: zero non-test hits) - schema.go:2757-2765.
- agent.atomic_compaction.enabled false (schema.go:2770-2780/4687-4696);
  agent.compression.enabled false (4629); session.compaction false (974).

Sync mode:
- orchestrator.sync_chat_enabled, bool, default FALSE - schema.go:3895
  (4939); wired SetSyncMode at components.go:4430 and 4944. Keep false
  (async ack + turn.terminal is the audited contract).

Misc:
- employees.enabled default TRUE (schema.go:5182) - set false to avoid
  goal-loop episode parker and bot runtimes in the rig.

## B4. Minimal rig config (toml) and the non-disableable residue

```toml
[llm.budget]
enabled = false
[agent.budget]
total = 0
[llm.quota_retry]
enabled = false            # resolver blocking only; parks STILL fire
max_wait = "24h"           # clamped; cannot zero
defer_check_interval = "5s"  # fast resume for inevitable parks
[llm.failure_policy]
horizon = "24h"            # clamped
base_throttle = "30s"      # clamped
short_retries = 3          # clamped
[llm.failure_policy.pacing]
enabled = false            # real off-switch
[parking]
queue_backend = false
[orchestrator.turn_watchdog]
enabled = true             # keep as the drop-detector; or false + accept hangs
stale_after_seconds = 120  # surface silent drops fast in the rig
[agent.watchdog]
enabled = false
[llm.context_firewall]
enabled = false            # real off-switch; no summarizer LLM calls
[compaction]
enabled = false
[orchestrator]
sync_chat_enabled = false
classifier_boot_fail_fast = false
[multi_agent]
enabled = false            # no classifier LLM; side effect: no throttle parker
[agent.retry]
enabled = false
[employees]
enabled = false
```

CANNOT be disabled by config (verified):
1. Chat quota park on provider 429-quota/402 (B2-1) - in BOTH multi_agent
   modes; up to 24h wait; reply promises automatic delivery
   (handler.go:1348-1350). Rig must treat that reply as a quota event or
   use a provider that never returns quota-shaped errors.
2. Throttle park + endpoint-blocked park in multi_agent mode (B2-2/B2-3).
3. Quota episode tracker state transitions (B2-4).
4. Worker provider-wait requeue (B2-5) and tactical 10-deferrals/6h (B2-6)
   on queue-dispatched tasks. Avoid task dispatch in the rig or detect
   deferral strings.
5. QuotaResetError/ThrottleBackoffError production in the HTTP clients
   (client.go:2147-2157, 950-975) - no switch at all.
6. parks.db persistence itself (components.go:3342).
7. Duration zeroing is impossible (B2-8): fail-fast is reachable only when
   the provider's reset horizon EXCEEDS max_wait (Park refuses,
   quota_resume.go:292-299; throttle give-up loop_park.go:381-397), not by
   configuration.

===========================================================================
RANKED TOP DEFECTS
===========================================================================

1. [CRITICAL] task.reject RPC executes ApprovePlan: rejecting a plan RUNS
   it; RejectPlan has zero callers. rpc/task_approval.go:69 +
   daemon.go:2161-2163. (A10-1)
2. [CRITICAL] Fresh throttle-parked turn terminalizes COMPLETED with an
   empty reply and tombstones the registry row; routed parks also lose the
   TurnID so the resume never delivers. handler.go:1424-1493 +
   loop.go:3795-3802 + loop_park.go:750-761 + dispatcher.go:3321-3325.
   (A2-1, A2-2)
3. [CRITICAL] Pair-managed job failure and ALL PairManager terminal paths
   publish no task event: silent drop, 600s opaque reap, task rows never
   terminal. tactical.go:2479-2503; pair_manager.go:354-402. (A7-1, A7-2)
4. [MAJOR] No panic recovery anywhere in chat/RPC/HTTP paths: one panic
   kills the daemon and every in-flight request. handler.go:386-399;
   rpc/server.go:532. (A0-1, A12-2)
5. [MAJOR] Quota/throttle/endpoint parking has NO config disable switch
   and fires with llm.budget.enabled=false; duration knobs clamp back to
   defaults. components.go:4447/4505/4960; loop_park.go:361;
   schema.go:1771-1781. (B2-1..B2-3, B2-8)
6. [MAJOR] Watchdog falsely reaps legitimately waiting turns (parks,
   approval/seal waits) at 600s, then a late completed terminal fires
   under a tombstoned/fresh id. turn_watchdog.go:136-150; handler.go:2900
   guard absent from the fresh path; strategic.go:2230. (A2-3, A8-2,
   A9-3, A10-2)
7. [MAJOR] PublishBlockingT 5s timeout silently loses turn.terminal; no
   replay/query surface exists. bus.go:249-258; handler.go:2035. (A0-2)
8. [MAJOR] Plan() non-compile errors and post-approval synthesis failure
   are log-only/kanban-only: user never learns the task died.
   orchestrator.go:369-375; plan/manager.go:634-645. (A9-2, A9-4)
9. [MAJOR] Goal-gate rejection releases the wrong park key: rejected
   goals strand parked (10-year horizon) and stop running forever.
   gate_park.go:328-339 vs goal_loop.go:1534-1545. (A11-3)
10. [MAJOR] Hung staleLoop.Close (untimed background goroutines + wg.Wait)
    on the serial chat goroutine freezes ALL chat traffic.
    manager.go:131-135; loop.go:4446, 5903-5915. (A6-3)
11. [MAJOR] chat.submit acks accepted=true while chat.request can be
    dropped (nil bus / full buffer / no subscribers); retries suppressed
    until the 600s reap. chat_submit.go:201-225. (A12-1)
12. [MAJOR] ClassificationNotice dead wiring: classifier degradation is
    silent; fallback can misroute with zero disclosure. dispatcher.go:231-
    234; llm/errors.go:591 unused. (A5-2)
13. [MAJOR] No closed-loop guard: closed loops accept and run turns.
    loop.go:4442-4451; no ErrLoopClosed in repo. (A6-2)
14. [MAJOR] Worker provider-wait requeue + tactical 10-deferrals/6h have
    no config off-switch (code seam exists, never wired).
    worker.go:596-671; tactical.go:37-43, components.go:4692-4705.
    (B2-5, B2-6)
15. [MINOR] Sweeper dead-letters crash-stranded jobs without a failure
    event (queue/store.go:1958); dead-letter not flagged on the bus
    (queue.go:702-706); park payload drops with known TurnID never
    terminalize (loop_park.go:568-583); watchdog stale-after doc says 120
    vs actual 600 (schema.go:3928). (A7-3, A7-4, A2-4, A8-3)

Verified-good (no action): non-quota provider errors, overflow double-
fault, tool errors, classifier fallback completion, missing-agent/nil-LLM
errors, ordinary retry exhaustion relay, critique-loop compile failure,
decision-gate soft deny, constitution denies, goal quality-gate degradation,
quota resume/give-up terminals with TurnID parity, owner-partitioned park
persistence, at-most-once resume across restarts.