# Audit: queue dead-letter, plan compile, approval rejection, employee gates

Repo: /tmp/meept-usability-head (HEAD 1cee89d7). Read-only audit; every line
number below was verified against the tree with grep/read. Scope: classes 7,
9, 10, 11 of the usability rig. Chat-turn contract used as the yardstick:
chat.submit -> ChatHandler.handleRequest (internal/agent/handler.go:749) ->
async dispatch ack turn.terminal status=parked (handler.go:1446-1455) ->
task-end relay publishTaskTerminalRelay (handler.go:2173) subscribed to
task.completed / task.failed (handler.go:359-360) -> publishTurnTerminal
(handler.go:2016). The turn watchdog (internal/agent/turn_watchdog.go:129)
reaps stale tracked turns as status=failed, handler_case=turn_reaped, error
"turn reaped: no progress for <window>" (turn_watchdog.go:136-150). Defaults:
enabled=true, interval 30s, stale_after 600s (internal/config/schema.go:4949-4953).
Liveness is fed only by worker lifecycle events (handler.go:1987-1991) and
task.progress (handler.go:2631-2644). Any failure path that emits neither a
task terminal event nor progress leaves the user waiting up to 600s for an
opaque "turn reaped" failure.

## Class 7: queue job dead-letter / max retries exceeded

Baseline propagation (the GOOD path, verified end to end):
- Failure origin: worker process error -> Worker.failJob
  (internal/worker/worker.go:418, 690-693) -> PersistentQueue.FailAttempt
  (internal/queue/queue.go:661) -> Store.FailAttempt
  (internal/queue/store.go:1100). The store decides failed vs dead:
  store.go:1150-1153 sets newState=StateDead when retryCount >= maxRetries;
  post-commit moveToDead at store.go:1238-1242 (moveToDead itself at
  store.go:1832, INSERT into dead_letter + DELETE from jobs).
- Emit site: queue.go:702-706 publishes "queue.job.failed" with job_id,
  error, claim_token. Kanban state event (queue.go:687-699) carries to=dead
  when the row vanished, but the bus failure event itself carries NO state
  or dead flag.
- Consumer: Orchestrator subscribes queue.job.failed
  (internal/agent/orchestrator.go:198) -> handleJobFailed
  (orchestrator.go:553-580) -> TacticalScheduler.OnJobFailed
  (internal/agent/tactical.go:2469).
- OnJobFailed non-pair path: publishes task.error chat-visible
  (tactical.go:2582-2587); retryable errors requeue via queue.Retry
  (tactical.go:2590-2634; Store.Retry budget guard at store.go:1382-1384);
  otherwise step set failed (tactical.go:2646-2653), and when no live steps
  remain the task is set StateFailed and "task.failed" is published with the
  job error text (tactical.go:2757-2774).
- Chat relay: handleTaskFailed (handler.go:2297) -> reply
  formatTaskFailedMessage (handler.go:2366-2381, error truncated to 200
  chars at 2377) -> publishTaskTerminalRelay(..., "task_failed_relay",
  "failed", ...) (handler.go:2361-2362) -> turn.terminal status=failed under
  the submitted turn id (handler.go:2202-2224). So retry exhaustion on an
  ordinary step-backed job DOES reach the user as failed; the dead-letter
  move is not itself silent.

Findings:

1. [CRITICAL] Pair-managed step failure is a silent drop: no task.failed, no
   task.error, no turn terminal.
   Origin: tactical.go:2479-2503 (OnJobFailed pair branch). When the failed
   job's step belongs to a PairManager session, the branch sets the step
   result/state failed (2494-2499), calls session.MarkFailed() (2500, state
   write only, internal/agent/pair_session.go:284-287), and returns nil at
   2502 BEFORE the task.error publish (2582) and before any task
   finalization. The parent task is never set failed and no bus event is
   emitted. The sibling reviewer step depends on the actor step, so nothing
   else ever runs. Propagation: none. Emit site: ABSENT. Status value: none
   (turn stays parked until watchdog). Error text: none. User outcome:
   turn.terminal status=failed "turn reaped: no progress for 10m0s" after
   ~600s (turn_watchdog.go:141), task row stuck non-terminal forever.
   Silent drop: YES.
   Fix sketch: in the pair branch, after MarkFailed, finalize the task
   failed and publish task.failed with jobErr (reuse the no-live-steps
   finalize block at tactical.go:2735-2774).

2. [CRITICAL] PairManager round failure and exhaustion never publish a task
   terminal event; exhaustion writes StateFailed with no event at all.
   Origin: internal/agent/pair_manager.go RunRound actor failure 155-169,
   reviewer failure 197-211, context cancel 174-187: each MarkFailed +
   publishes "pair.round_failed" and returns. Exhaustion 253-262 publishes
   "pair.exhausted" and calls finalizeTask(false). finalizeTask
   (pair_manager.go:354-402) only writes task.StateCompleted/StateFailed
   (383, 389) and logs; it publishes NOTHING (no task.completed/task.failed
   anywhere in pair_manager.go, verified by grep). Consumers:
   orchestrator.go:202-204 subscribes pair.converged/pair.exhausted/
   pair.round_failed, but handlePairConverged (689-705), handlePairExhausted
   (708-726), handlePairRoundFailed (729-749) are ALL log-only. The
   ChatHandler pair relay subscribes only TopicPairResult "pair.result"
   (handler.go:378, pair_channel.go:111), which is published only by the
   separate bus-driven PairOrchestrator (pair_orchestrator.go:481-486), not
   by PairManager; and even handlePairResult (handler.go:3738-3758) only
   sends a chat response, never a turn.terminal. Round-failure path does not
   even set the task state. Emit site: ABSENT on every PairManager terminal
   path. Status: none; watchdog reap after ~600s. Error text: none (the
   round error exists only in the pair.round_failed payload nobody relays).
   Silent drop: YES (both success and failure of spec_pair tasks are
   invisible to turn-scoped clients; converged tasks also never relay).
   Fix sketch: make finalizeTask publish task.completed/task.failed with
   linked sessions (it has taskStore), and publish task.failed on the
   RunRound error paths.

3. [MAJOR] Crash-stranded exhausted jobs are dead-lettered by the sweeper
   with no failure event, so the step/task never terminalize.
   Origin: Store.FailAttempt computes StateDead and commits, but the
   queue.job.failed publish happens later in PersistentQueue.FailAttempt
   (queue.go:702); a crash between commit and publish (or a moveToDead
   failure, store.go:1239-1241 log-only) strands the row.
   SweepAbandonedTerminalStates (store.go:1910-1974, started at
   daemon.go:2617 with 15m interval / 1h age defaults, store.go:1882-1883)
   moves stranded failed-exhausted/dead rows to dead_letter via moveToDead
   (store.go:1958) and publishes NOTHING. The step stays scheduled; the
   task stays non-terminal. Emit site: ABSENT. User outcome: watchdog reap
   only; task row never fails. Silent drop: YES (narrow crash window).
   Fix sketch: have the sweeper publish queue.job.failed (error="retry
   budget exhausted, recovered by sweeper") for each moved row so
   OnJobFailed terminalizes the step.

4. [MINOR] Dead-letter is indistinguishable from a retryable failure on the
   bus, and the user-facing error lacks exhaustion context.
   queue.job.failed (queue.go:702-706) carries no state/dead/retry_count
   fields; OnJobFailed cannot tell attempt 1 from the final attempt. There
   is no queue.job.dead topic (only operator RPCs queue.dead_letter /
   queue.dead_stats, queue.go:1035-1036, 1339-1366, and
   RecoverFromDeadLetter queue.go:827). The chat reply shows only the last
   raw provider error truncated to 200 chars (handler.go:2377); nothing says
   "gave up after N retries". Error quality: partially actionable (raw
   provider text) but opaque about the retry outcome. Non-step jobs (memory
   distillation, internal/memory/distill_async.go:203-215) dead-letter with
   step==nil -> OnJobFailed returns nil (tactical.go:2474-2476): silent by
   design, no chat turn involved, acceptable.
   Fix sketch: add state/retry_count to the queue.job.failed payload and
   prefix the relay error with "job failed after N retries" when dead.

## Class 9: plan compile failure

Compiler: plan.CompileSealed (internal/plan/compiler.go:23), deterministic
zero-LLM, returns *CompileError aggregating {Line, Message} problems
(compiler.go:41, 61-66).

5. [GOOD, verified] The critique-loop compile failure path is honest and
   actionable. plan_critique_loop.go:712-733: first CompileSealed rejection
   feeds ONE compile-fix revise round (715); revise transport failure ->
   failTaskWithReason (718); second compile rejection -> failTaskWithReason
   (730). failTaskWithReason (strategic.go:1905-1929) persists StateFailed
   and publishes task.failed with name/error/linked_sessions (1923-1928;
   task-vanished fallback still publishes, 1911-1914). Reason text:
   "plan compile failed after the critique loop and one compile-fix revise
   round: line N: <problem>; ..." via compileProblemText
   (plan_critique_loop.go:1043-1053, bounded 800 runes). Relay:
   handleTaskFailed -> turn.terminal status=failed (handler.go:2361). Error
   quality: actionable (line numbers + compiler messages), though the chat
   reply truncates to 200 chars (handler.go:2377; full text survives in the
   event Error field). No silent drop on this path. The empty-plan sentinel
   is likewise honest (strategic.go:1505 failTaskWithReason).

6. [MAJOR] Non-compile Plan() failures are log-only: task stuck in planning,
   watchdog-only failure. orchestrator.handlePlanRequest
   (orchestrator.go:369-375) logs "Strategic planning failed" and returns on
   ANY strategic.Plan error, with no task.failed and no state write.
   Reachable silent origins: seedDraftFromRequest error (strategic.go:925-
   928, returns seedErr; SaveDraft state guard at plan_draft.go:123-125),
   step persist failure (strategic.go:1071-1074), quick_plan critique-flow
   error (strategic.go:866-869 returns flowErr; e.g. "persist compile-revised
   draft" at plan_critique_loop.go:724 which does NOT call
   failTaskWithReason), self-seal SealDraft/SealPlan errors
   (plan_critique_loop.go:783-788). In plan mode the critique-flow error is
   degraded to a seeded draft with a Warn (strategic.go:944-956, deliberate),
   but in quick_plan mode the same error kills the turn silently. Task stays
   StatePlanning (set at strategic.go:794). Emit site: ABSENT. Status: none
   until watchdog reap (~600s, opaque). Silent drop: YES.
   Fix sketch: in handlePlanRequest's error branch, call
   strategic.failTaskWithReason(req.TaskID, err.Error()) (or publish
   task.failed) before returning.

7. [MAJOR] plan.seal RPC compile problems are a good synchronous result, but
   the awaiting chat turn is never terminalized and gets reaped.
   rpc/plan_seal.go:146-158: compile problems returned as status="problems"
   with {line, message} views; draft stays a draft (actionable, correct for
   the RPC caller). However the chat-originated task sits in StatePlanning
   awaiting a human seal; the dispatch turn's registry edge survives
   (handler.go:1491 skips completeTurn when syncTaskID != ""), nothing parks
   or touches it, and no task.pending_approval/await-seal event feeds
   liveness. After 600s the watchdog emits turn.terminal failed "turn
   reaped" for a turn that is legitimately waiting for a human. When the
   human later seals and the task completes, the relay fires a second
   turn.terminal completed under the same (already tombstoned) turn id.
   Status sequence: parked -> failed(reaped) -> completed. Error quality:
   opaque reap text. Silent drop: no event is dropped, but the failure is
   FALSE. Same shape for the strategic approval gate, see finding 9.
   Fix sketch: park/heartbeat approval-waiting turns (exclude tasks in
   planning/awaiting_approval from watchdog staleness, or Touch on
   draft/seal activity).

8. [MAJOR] Plan synthesis failure after approval is invisible to the user.
   PlanManager.ApprovePlan (internal/plan/manager.go:594) publishes
   plan.approved (618-620) then Synthesize (634); on Synthesize error it
   stores synthesis_error on the plan row and returns nil (634-645) - no
   bus event, no notification. The reclaim pass re-drives; after
   MaxSynthesisAttempts the plan is parked to StateFailed (manager.go:1119-
   1136, release(true) at 1098-1101) and the only emission is
   publishPlanState (manager.go:1876-1893), a kanban board event gated on
   the kanbanEvents flag - not a chat/notification surface. failPlan
   (manager.go:1629-1655) likewise deliberately publishes no bus event
   (comment 1626-1628). Emit site for chat: ABSENT. User approved a plan and
   simply never sees work start or fail; error text lives only in the
   synthesis_error column. Silent drop: YES.
   Fix sketch: on synthesis park/failure publish a session-scoped
   notification (plan.SourceSession is available) or plan.failed event the
   chat handler relays.

## Class 10: approval rejection / denial

9. [CRITICAL] task.reject RPC APPROVES the plan. TaskApprovalHandler has
   only ApproveFunc (internal/rpc/task_approval.go:18-22); handleReject
   (task_approval.go:55-73) calls h.ApproveFunc(ctx, req.TaskID) at line 69
   and then returns {"status":"rejected"} (72). Daemon wiring passes
   Strategic().ApprovePlan as the only func (internal/daemon/daemon.go:2161-
   2163). ApprovePlan (strategic.go:2243-2332) persists the pending steps,
   sets StateExecuting, promotes and schedules them (2302-2328). So a user
   rejecting a plan through the TUI (RejectTask, internal/tui/rpc.go:926-
   930) or HTTP client (internal/transport/http_client.go:707-708) gets a
   "rejected" ack while the daemon EXECUTES the rejected plan. The correct
   implementation StrategicPlanner.RejectPlan (strategic.go:2336-2363,
   StateRejected + task.rejected) exists but has ZERO callers (verified by
   grep across the tree). Emit site: task.approved/orchestrator.schedule
   fire on rejection; no rejection event. Status: user sees "rejected",
   work runs. Silent drop: the rejection itself is dropped.
   Fix sketch: add RejectFunc to TaskApprovalHandler, wire
   Strategic().RejectPlan in daemon.go, call it in handleReject.

10. [MAJOR] Even when RejectPlan is reached, task.rejected has no
    subscribers: no turn.terminal for a rejected task. strategic.go:2356-
    2359 publishes task.rejected {task_id, reason}; grep shows no subscriber
    anywhere (ChatHandler subscribes only task.completed/task.failed/
    task.progress, handler.go:359-368). StateRejected is terminal in the
    store but the tracked turn's registry edge survives, so the awaiting
    client hangs until the watchdog reaps it as failed with opaque "turn
    reaped" text instead of "rejected: <reason>". Emit site: ABSENT
    (publishTurnTerminal / task.failed never fire for rejection). Silent
    drop: YES for turn-scoped clients.
    Fix sketch: subscribe task.rejected in ChatHandler and relay
    turn.terminal status=failed (or a rejected status) carrying the reason;
    or publish task.failed alongside task.rejected.

11. [MAJOR] Strategic approval-gate wait is reaped as a failure. Tasks with
    >= approvalStepThreshold steps enter awaitUserApproval
    (strategic.go:2194-2239): StateAwaitingApproval + task.pending_approval
    (2230). task.pending_approval has NO subscribers (grep: publisher only;
    the WS relay forwards it as a generic event via "task.*",
    internal/comm/http/server.go:743, but no chat turn terminal/park). The
    GateParker parks only PlanManager plan.submitting events
    (daemon/gate_park_wiring.go:162-198), NOT this strategic task gate. No
    progress events fire while waiting, so the watchdog reaps the turn at
    600s (turn_watchdog.go:136-150) even though the system is correctly
    waiting for a human. On later approval the task executes and relays
    completed under the reaped turn id. Status sequence parked ->
    failed(reaped) -> completed. Error quality: opaque, misleading.
    Fix sketch: exclude awaiting_approval tasks from watchdog staleness or
    checkpoint them through GateParker like plan gates.

12. [GOOD, verified] gate_park.go rejection mechanics are sound for
    PlanManager plans. Gate parks use a 10-year horizon and never
    auto-expire (gate_park.go:43-51, 24-26); approval resumes via
    ResumeByKey delete-before-callback (240-245, 393-436); plan.rejected /
    plan.cancelled release the park WITHOUT resuming via ReleaseByKey
    (gate_park.go:322-350) wired at gate_park_wiring.go:206-230, 273-275.
    The release emits a lifecycle event on agent.quota_wait with reason
    gate_rejected (gate_park.go:356-381) - observable, but NOT a
    turn.terminal; PlanManager.RejectPlan (plan/manager.go:651-680)
    publishes plan.rejected with session_id (668-676) which the TUI plans
    panel renders (tui/app.go:1899, 1949). For chat turns that dispatched
    PlanManager plans there is still no turn terminal on rejection (the
    plan and the turn are different objects); acceptable only because those
    plans are usually RPC/employee-originated, noted for completeness.

13. [MINOR] Decision-gate deny is a soft confirmation handshake, not a hang.
    DecisionGateHook.BeforeToolCall (internal/tools/builtin/decision_gate.go:
    133-234) returns BlockResult{Block:true} on sub-threshold answers (228-
    233); the loop renders requires_confirmation with confirm_arg=confirmed
    (internal/agent/loop.go:9062-9099); confirmed=true re-invoke bypasses
    (decision_gate.go:156-173). A human decline never re-enters the gate
    (comment 218-222); the model receives the confirmation-shaped tool
    result and finishes the turn with text, so turn.terminal completed
    fires normally - no hang. Note: the gate FAILS OPEN when the decision
    client errors (decision_gate.go:176-186), a security posture choice,
    not a usability drop. Hard security denies produce a failed tool result
    "blocked by security policy: <reason>" (loop.go:9077-9083) which the
    model sees; turn still completes. No defect.

14. [MINOR] Executor confirmation dead-end for step jobs.
    Executor.checkPermission -> NeedsConfirm returns a failed tool result
    "action requires user confirmation" with NO confirmation seam
    (internal/agent/executor.go:1556-1564, comment "we don't support async
    confirmation"). Employee RequiresPlan escalations map to NeedsConfirm
    (pkg/security/permissions.go:546-555), so a constitution plan-signoff
    escalation inside a step job is an opaque tool error the model cannot
    resolve (the RequiresPlan livelock risk is documented at
    internal/employee/enforcement.go:1010-1014). The turn still completes
    with model text; error quality: opaque, no actionable approve path.
    Fix sketch: route NeedsConfirm step-job denies into the phase-1
    confirmation shape (requires_confirmation result) instead of a bare
    error.

## Class 11: employee constitution / gate failure

15. [GOOD, verified] Constitution pre-exec denies are actionable and
    audited; the chat turn does not hang. PreExecChecker.Check
    (internal/employee/enforcement.go:614) denies with specific reasons:
    tools_forbidden (672-677 "tool %q is forbidden by constitution"),
    tools_allowed (692-696), unapproved voice caller (722-729), never[]
    rules (critical deny + auto-pause, 617-628 panic fail-safe, 992-999).
    Wiring: manager.go:1080 pc.SetPreExecChecker -> PermissionChecker.
    CheckPermissionForAgent Stage 0 (pkg/security/permissions.go:516-559)
    -> Executor.checkPermission (executor.go:2085-2138) -> failed
    ExecutionResult "permission denied: <reason>" (executor.go:1549-1553).
    Denials write audit findings (recordDenial, enforcement.go:931-957);
    auto-pause publishes employee.paused (employee/manager.go:1537-1563),
    relayed over WS via employee.* (comm/http/server.go:785). The model
    receives the deny as a tool result and the turn completes with text;
    no silent drop. Error quality: actionable (names the tool and rule).

16. [GOOD, verified] Goal quality-gate failure never claims silent success.
    GoalLoop.Reflect (internal/employee/goal_loop.go:1295-1323): gate error
    -> GoalAtRisk (1298-1300), skip-on-unchanged -> GoalAtRisk with INFO log
    (1301-1309), failed run -> GoalAtRisk (1314-1322); health persisted
    (1330-1345) + metric. Gate outcome chained to the audit log
    (emitGateResult, 1604, 1685-1690). Approval-gate park refusal degrades
    to at_risk, never completion (1288-1294). Round output reaches the user
    via employee.notify SpeakNotify (1353-1359, notifySpeakRound 636-660;
    WS bridge daemon/speak_push_bridge.go). These are autonomous rounds,
    not chat turns, so no turn.terminal is expected; the user-visible
    surface is the notify + goal health. Gate failure output itself rides
    the next-round feedback block, not the notify - minor opacity, by
    design (output may contain code, comment 1315-1317).

17. [MAJOR] Rejecting an employee goal plan strands the GOAL approval gate
    park forever. Goal approval gates park with GateKind="goal",
    GateID=goal.ID (goal_loop.go:1534-1545). Rejection flows
    agents.goals.reject (employee/handler.go:811-859) -> Manager.RejectPlan
    (employee/manager.go:2825-2867) -> planDisposer -> PlanManager.RejectPlan
    -> plan.rejected -> handleGateRelease -> GateParker.ReleaseByKey(planID)
    (gate_park_wiring.go:218). ReleaseByKey probes keys "plan|<id>" and
    "goal|<id>" with the SAME id (gate_park.go:328-339); the parked row is
    keyed "goal|<goalID>", so a rejection by planID never matches it. No
    other caller releases goal gates (grep: ReleaseByKey has exactly one
    production call site). The row carries a 10-year horizon and is never
    auto-expired (gate_park.go:24-26, 51), and the goal-round driver treats
    a parked gate as NOT quiescent (gate_park.go:256-262,
    daemon/dsh_wiring.go:71), so the goal stops running autonomous rounds
    indefinitely after a rejection - including the approval-timeout
    auto-reject (employee/scheduler_jobs.go:438-439). Emit site: the
    agent.quota_wait gate_rejected lifecycle event never fires for the goal
    row. User outcome: goal silently goes dormant; only a manual
    agents.goals.approve_gate resume clears it. Silent drop: YES.
    Fix sketch: on goal-plan rejection also ReleaseByKey(goalID), or key
    goal gate parks by plan id when a plan raised them.

18. [MINOR] Watchdog config doc drift. TurnWatchdogConfig.StaleAfterSeconds
    comment says "0 -> 120" (internal/config/schema.go:3928) while the
    struct doc (3921) and DefaultConfig (4952) use 600. Operators tuning
    the hang window from the field comment will mispredict reap latency by
    5x. Fix sketch: correct the comment to 600.

## Ranked top defects

1. Finding 9 (CRITICAL): task.reject executes ApprovePlan - rejection runs
   the rejected plan (rpc/task_approval.go:69 + daemon.go:2161-2163).
2. Finding 1 (CRITICAL): pair-managed job failure drops with zero events;
   turn hangs 600s, task never terminalizes (tactical.go:2479-2503).
3. Finding 2 (CRITICAL): PairManager never publishes task.completed/
   task.failed on any terminal path; all spec_pair outcomes are invisible
   to turn-scoped clients (pair_manager.go:354-402, orchestrator.go:689-749).
4. Finding 17 (MAJOR): goal-gate rejection releases the wrong park key;
   rejected goals strand parked and stop running (gate_park.go:328 vs
   goal_loop.go:1536).
5. Finding 6 (MAJOR): Plan() seed/persist/seal errors are log-only; task
   stuck planning, watchdog-only opaque failure (orchestrator.go:369-375).
6. Finding 11 (MAJOR): approval-waiting turns reaped as failed at 600s
   though legitimately awaiting a human (strategic.go:2230, no park/no
   subscriber).
7. Finding 7 (MAJOR): plan.seal await-seal turns share the false-reap
   shape; failed(reaped) then completed sequence (rpc/plan_seal.go:150-158).
8. Finding 8 (MAJOR): post-approval synthesis failure/exhaustion parks the
   plan failed with no user-visible event (plan/manager.go:634-645, 1128-
   1131).
9. Finding 10 (MAJOR): task.rejected has no subscribers; even a correctly
   wired rejection yields no turn terminal (strategic.go:2356).
10. Finding 3 (MAJOR): sweeper dead-letters crash-stranded jobs without a
    failure event (queue/store.go:1958).
11. Finding 14 (MINOR): executor NeedsConfirm dead-end, opaque "action
    requires user confirmation" with no approve seam (executor.go:1556).
12. Finding 4 (MINOR): dead-letter not flagged on the bus; retry-exhaustion
    context missing from user error text (queue.go:702, handler.go:2377).
13. Finding 18 (MINOR): watchdog stale-after doc says 120, default is 600
    (config/schema.go:3928 vs 4952).

Verified-good paths (no action): ordinary step-job retry exhaustion relays
turn.terminal failed with the provider error (class 7 baseline); critique-
loop compile failure fails the task honestly with line-numbered compiler
problems (finding 5); decision-gate soft deny completes the turn via the
confirmation handshake (finding 13); constitution denies are actionable,
audited, and auto-pause is published (finding 15); goal quality-gate
failure never claims completion and degrades to at_risk (finding 16).
