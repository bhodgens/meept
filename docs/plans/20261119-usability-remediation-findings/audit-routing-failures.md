# Meept Daemon: Work Routing Correctness and Failure Paths
# Read-only audit at /tmp/meept-usability-head, HEAD 1cee89d7
# Perspective: interactive chat user. Requirement under test: no request fails outright.

Method: direct code reading of the dispatcher/classifier/handler chain plus four
merged sub-audits (index at the bottom). Every finding cites file:line relative
to /tmp/meept-usability-head. No repo files were modified. Plain ASCII.

Corrections to prior briefing assumptions (verified at HEAD):
- Liveness default is 600s, NOT 120s: orchestrator.turn_watchdog.stale_after_seconds
  defaults to 600 (internal/config/schema.go:4952); the field comment at
  schema.go:3921-3929 still describes the old 120s posture (doc drift).
- Researcher DOES ship file_write now: config/agents/researcher/AGENT.md:12-18
  grants web_fetch, web_search, file_read, list_directory, file_write,
  transcript_fetch, json_extract (no shell_execute). Writer also pairs
  file_write+web_fetch (config/agents/writer/AGENT.md:8-12). The old
  "no agent ships web_fetch+file_write together" claim is stale.
- selectAgent's research->researcher mapping exists and is table-driven
  (internal/config/tool_hints.go:55-80, internal/agent/tactical.go:2817-2849);
  the residual gap is planner-side hint emission, not the table (see R6).

===========================================================================
## 1. ROUTING DECISION CHAIN
===========================================================================

### 1.1 Lane order in ClassifyAndRoute (internal/agent/dispatcher.go:933-1527)

Executed top to bottom; first hit wins unless noted:

| # | Lane | Site | Threshold / rule | Destination |
|---|------|------|------------------|-------------|
| 0 | Late agent discovery | dispatcher.go:939-941 | one-shot registry refresh | n/a |
| 1 | Thread resolution | dispatcher.go:953-967 | topic detect on FULL input | thread conv id (memory lane only) |
| 2 | Clarification resume | dispatcher.go:972-980 | pending clarify for session | ResumeAfterClarification |
| 3 | /plan slash + plan-mode toggle | dispatcher.go:985-1030 | controller wired = toggle/steer | plan route or status reply |
| 4 | Skill / template invocation | dispatcher.go:1031-1053 | /name registry hit | skill executor or substituted text |
| 5 | Model directive parse | dispatcher.go:1056-1087 | clarification-needed directive | clarify reply (conf 1.0) |
| 6 | Named roster agent (name_match) | dispatcher.go:1114-1150 | "have bob ...", "@bob" vs registry names | named agent, conf 1.0, NO task created |
| 7 | Stage-0 embedding prefilter | dispatcher.go:1162-1266 | default OFF; cosine >= 0.70 + unanimous kNN + optional tfidf veto + embed-health gate; assert_only observer mode; H6 gates suppress task-creating/async verdicts (1207-1226) | inline intents only |
| 8 | True-intent analyzer | dispatcher.go:1271-1294 | ambiguity short-circuit ONLY when session digest empty (1282) | clarification or analysis carry-through |
| 9 | Anaphora resolution | dispatcher.go:1297 | context references | rewritten input |
| 10 | Instruction parser | dispatcher.go:1301-1316 | "always/never/whenever" + conf >= 0.5 | chat + instruction object |
| 11 | Compound multi-intent | dispatcher.go:1331-1334 | compound signal words, len >= 80 (4968) | routeCompoundWithModel |
| 12 | Media-URL guard | dispatcher.go:1350-1374 | YouTube URL + consumption verb evidence | analyst, conf 0.9 |
| 13 | classifyIntent chain | dispatcher.go:1554-1984 | see 1.2 | per lane |
| 14 | Session-state quickplan upgrade | dispatcher.go:1389-1392 | knob session_state_upgrade, default false (schema.go:4921) | quickplan |
| 15 | Client agent override | dispatcher.go:1411-1426 | registry hit; miss = Warn log only, silent fallback | override agent |
| 16 | Plan route | dispatcher.go:1433-1435 | planManager.ShouldCreatePlan | routeToPlanWithOverride |
| 17 | Task creation gate | dispatcher.go:1449-1451 | shouldCreateTask AND dispatchConsumesTask | task row + async dispatch |

### 1.2 classifyIntent inner chain (dispatcher.go:1554-1984)

a. short_message_guard: len < 50 chars or bare arithmetic -> chat conf 0.9
   (dispatcher.go:1559-1574, threshold const 4964).
b. media guard again (1587-1598) for the clarification-resume entry.
c. Capability matcher: conf >= 0.7 routes (1611-1639). Config
   agents.capability_match_enabled (schema.go:2974-2978), default OFF.
d. LLM classifier / prompt-router (1642-1897), with five arbitrations that
   discard or upgrade an untrusted verdict BEFORE the threshold check:
   - platform_recall_arbitration (1720-1765): platform/schedule-without-time/
     git-without-git-verb verdict + work-status recall shape -> recall/chat
     conf 0.85. Recall matchers run FIRST (branch order is load-bearing).
   - platform_action_arbitration (1766-1773): platform or time-less schedule
     verdict + leading imperative -> verdict discarded, chain continues.
   - git_verb_agreement_veto (1774-1795): git verdict, no git verb in input,
     leading imperative -> discarded. Default ON (dispatcher.go:642-645).
   - chat_imperative_arbitration (1796-1816): chat verdict + leading
     imperative + work artifact noun -> discarded.
   - quickplan_cue_upgrade (1817-1848): scatter-lane verdict + strong
     adjudicated orchestration cues -> quickplan/orchestrator conf 0.85.
   Per-lane acceptance thresholds (llm_classifier.go:264-284): git 0.85,
   schedule 0.80, code/debug/review 0.75, plan/platform/report/recall/security
   0.70, analyze/search 0.60, research 0.55, chat 0.85 (deliberately high so
   keyword work matches outrank small-talk), default 0.50 (1021-1026).
   Failure handling: EMPTY response -> chat conf 0.6 fallback
   (llm_empty_fallback_chat, 1884-1895); ANY OTHER error (timeout, cooldown,
   transport) -> falls to the keyword table (1870-1896 + 1900).
e. Keyword classifier: accept >= 0.3 (1900-1920). Score =
   pattern.confidence * len(keyword)/len(input+1), prefix match x1.2
   (dispatcher.go:4063-4066). Consequence: long prompts mathematically
   cannot keyword-route (a 12-char keyword at conf 0.9 needs input < ~36
   chars to clear 0.3), so the degraded lane for long work prompts is
   heuristic/fallback, not keyword.
f. Semantic index: match >= 0.6 (1923-1948).
g. heuristicFallback (1953-1963): targeted verb rules.
h. Final fallback: quickplan/orchestrator conf 0.3, RequiresPlanning=true,
   summary "Could not determine intent; planning and executing with
   clarification as needed" (1966-1982). Recorded as all_classifiers_failed.

Classifier availability mechanics (llm_classifier.go):
- 60s unavailability cooldown after ONE failure (37, 585-591); during the
  window every Classify returns an error without dialing (534-546), so all
  turns in the window degrade to keyword/heuristic/fallback. Alias failover
  can rotate during cooldown only when configured (549-558).
- Boot-readiness gate: classification waits up to 90s for classifier
  runtimes at startup (defaultClassifierBootWait, 44; awaitBootReadiness
  772). Analyzer waits stack on top (no per-turn deadline).
- Lane->agent resolution: frontmatter laneAgentIndex (AGENT.md intents:)
  first, then static agentMapping (llm_classifier.go:286-306), then
  IntentType.DefaultAgent, then chat (134-147). Adding an agent is a
  frontmatter-only change; two agents declaring the same lane: sorted
  first path wins (190-214), silently.

### 1.3 Post-classification routing (RouteToAgent, dispatcher.go:3041-3410)

- Thread id re-resolved on Intent.Summary (~100 chars), NOT the full input
  (3068 vs 955): the two resolutions can pick different threads (see 2.3).
- Platform introspection shortcut unless agent override (3108-3117);
  recall-from-stored-task answer first when digest has a prior task.
- Active-queue lane (3119-3213): steer vs follow-up by shouldSteer
  heuristic table (85-98, default follow-up); injects
  result.Intent.Summary (TRUNCATED, 3180/3194); receipt is the literal
  reply "message queued (steer)" / "message queued (follow-up)"
  (3191/3205); closed/full queue falls through to a NEW agent run.
- Recall continuity gate on Summary shape (3245-3260).
- resolveAgent builds/evicts per-agent session loops (3540+).
Step-level routing: tactical selectAgent via config.ToolHintAgent table
(tactical.go:2817-2849, tool_hints.go:55-80); explicit step.AgentID wins,
then roster-name scan of the description, then hint table, then chat
fallback (assignStepAgent 2865-2888). Parked/cooling endpoint emits
routing.warning advisory but NEVER changes the route (2832-2839).

### 1.4 What the user can observe

- turn.terminal event: intent_type, agent_id, classified_by, model,
  handler_case, status, error (handler.go:1457-1474; 13-key frozen contract).
- session.dispatch_trace RPC (rpc/dispatch_trace.go:20-41) reads
  metrics.dispatch_log: session_id, intent_type, agent_id, confidence,
  classifier_method, handler_case, task_id, has_parts, error, input_hash,
  model, margin, turn_no, outcome, corrected_agent
  (metrics/store.go:315-328 + migration 478-483). NO turn_id column.
  Row written at handler.go:1239 keyed on the SESSION-level conversation id.
- Daemon log: "Dispatched request" Info with method/confidence
  (dispatcher.go:1513-1521); "routing decision" Debug (4731-4741).
- Prefilter margin persisted for routed/suppressed/abstained votes
  (dispatcher.go:4771-4775). Hint telemetry RecordHintRoute/RecordHintMiss
  (tactical.go:2821-2847). Re-route outcome proxy ok/corrected
  (dispatcher.go:4798-4817).

### 1.5 Routing findings

R1 (MAJOR) Degradation is invisible to the user. DispatchResult.
ClassificationNotice is declared (dispatcher.go:231-234) and read
(handler.go:1049-1050, 1258-1262) but NEVER assigned anywhere in the tree
(grep: zero write sites). A turn silently misrouted by the 60s-cooldown
degraded lane carries no notice; classified_by in the terminal event is
the only clue and no surface renders it as a warning.
Fix: set ClassificationNotice on every fallback lane (keyword/semantic/
heuristic/fallback/llm_empty_fallback_chat) and render it client-side.

R2 (MAJOR) Cooldown poisoning breadth. One classifier failure arms a 60s
window (llm_classifier.go:585-591) that degrades EVERY session's turns;
ClassifyMulti failures arm the same shared flag. Non-empty-response errors
(timeout, transport) fall to the verb-dense keyword table despite the
comment at dispatcher.go:1876-1883 documenting chat-fallback policy for a
down LLM lane; only the empty-response kind gets that policy (1884).
Fix: route all classifier-unavailable kinds to the chat fallback (full
tool registry), or per-session cooldown scoping.

R3 (MAJOR) dispatch_trace cannot be joined to a turn. No turn_id column
(metrics/store.go:315-328); turn_no is COUNT(*)+1 computed at record time
(dispatcher.go:4779), racy under concurrent dispatches; rows are keyed on
the session conv id while continuity/memory use the thread conv id, so a
trace query with the thread id the client sees returns nothing.
Fix: add turn_id to DispatchEntry + schema, pass req.TurnID through
RecordDispatch; accept thread ids by resolving to parent session.

R4 (MAJOR) Residual git-lane misroute. The git-verb veto requires a
LEADING imperative (dispatcher.go:1774-1777) and the recall arbitration
requires recall shape (1725-1727). A question-shaped or declarative input
containing "repository" but no git verb and no recall predicate keeps a
git verdict >= 0.85 and dispatches the contextless committer async.
Fix: veto any git verdict lacking inputContainsGitVerb regardless of
imperative shape (the lexical-bias evidence at 1779-1789 is shape-independent).

R5 (MINOR) Unknown client agent override is silently ignored: Warn log
only (dispatcher.go:1421-1424); the user asked for agent X and gets the
classifier's pick with no disclosure. Named-agent route (1114-1150)
creates NO task row, so long "have bob build X" work is untracked and
unsteerable via task surfaces. Fix: surface override-miss in the reply;
create a task for named-agent work above a size heuristic.

R6 (MAJOR) Planner hint emission, not the hint table, is the research
misroute source. The table maps research->researcher correctly
(tool_hints.go:62), but the planner template seeds tool_hint "code"
(planner_template.go:208) and the strategist accepts whatever the LLM
emits; a research-shaped step planned with hint=code executes on coder,
which has NO web_fetch (config/agents/coder/AGENT.md tools list). The
step then fails or deflects with no routing-level signal.
Fix: validate hint-vs-step-description capability fit at compile/schedule
time and emit routing.warning on mismatch (telemetry seam already exists).

R7 (MINOR) Lane-index collisions are silent: two AGENT.md files declaring
the same intent lane resolve by sorted path order (llm_classifier.go:
209-211), no log. Fix: Warn on duplicate lane declaration at index build.

R8 (MINOR) classifyIntent error is warn-and-continue then intent is
dereferenced unconditionally (dispatcher.go:1377-1395): today no classifier
returns (nil, nil-with-err) that reaches 1395 with nil intent, but one
future return shape turns this into an unrecovered panic (no recover in
handleRequest, see F1). Fix: nil-guard with explicit chat fallback.

===========================================================================
## 2. SESSION / WORKER LOCATION
===========================================================================
(Merged from parts/audit-session-steering.md Section A; all cites verified
there against HEAD.)

### 2.1 Binding chain (correct baseline)
chat.submit -> handler resolves the session's OWN conversation id before
minting (rpc/chat_submit.go:149-159; handler.go:824-834, dual lookup
Get + GetByConversationID). Dispatcher keeps session-level id for task
links/plan SourceSession (dispatcher.go:1019-1028, 1324-1330, 1429-1432)
and thread-scoped id ONLY for LLM continuity memory (RouteToAgent
3046-3080). Working-dir precedence worktree > project > detection CWD >
daemon default is sound (session/working_dir.go:43-56,
dispatcher.go:854-868).

### 2.2 Wrong-dir paths (MAJOR)
- Singleton-loop fallback mutates the SHARED workingDir
  (dispatcher.go:3692-3698, handler.go:3534-3537): concurrent sessions
  racing the fallback can execute in each other's project dir.
- Step-job fallback runs on the interactive daemon loop with NO dir
  injection (components.go:11165-11220): steps whose LinkedSessions
  resolution fails run in the daemon CWD; spreadsheet_write-class tools
  hard-refuse, others silently write to the wrong place.
- Payload SessionID hint lacks thread unwrap (components.go:11004-11013).
Fix: never mutate a shared loop's dir (clone or fence); inject resolved
session dir into every step-job fallback; unwrap thread ids in hints.

### 2.3 Thread resolution (MAJOR)
ThreadRouter.GetThreadConversationID keys threads on sess.ID
(thread_router.go:141-151, the 2026-10-01 fix). But it runs TWICE per turn
on DIFFERENT text: full input at dispatcher.go:955 (memory lane) and
~100-char Intent.Summary at 3068 (execution lane). A long prompt whose
topic signal sits past char 100 can split memory vs execution across two
thread conv ids. Unknown/malformed thread ids fall back to the session id
silently (thread_router.go:127-129) - an unbound turn with no signal.
Fix: resolve once on full input, carry the resolved id on DispatchResult.

### 2.4 Queue worker binding
Queue claims are session-agnostic by design (queue/store.go:655-665);
isolation rides Task.LinkedSessions -> GetByConversationID. Any path that
writes a thread id into LinkedSessions breaks dir resolution (the
task-state-04 class; guarded at dispatcher.go:1324-1330 but the step-job
fallback above still misses).

### 2.5 Plan-compiler follow-up gap: CONFIRMED STILL OPEN (CRITICAL)
With plans.plan_compiler_enabled=true (schema.go:743), plan-mode tasks
park in planning behind a brainstorm draft. A chat follow-up does NOT
refine the draft: ClassifyAndRoute has no SaveDraft/plan-continuation
branch (grep SaveDraft in dispatcher.go: zero hits); the follow-up
re-classifies and createTask mints a NEW task + NEW draft
(dispatcher.go:1449, 2592; strategic.go:924-975). The draft is refinable
only via plan.draft RPC (rpc/plan_seal.go:224-227). An interactive user
"answering the brainstorm questions" in chat silently starts over.
Fix: when the session has a parked planning task with an unsealed draft,
route non-command follow-ups to plan.SaveDraft + re-compile instead of
createTask.

===========================================================================
## 3. FAILURE PATH INVENTORY
===========================================================================
(Merged from parts/audit-failures-quota.md Section A and
parts/audit-queue-gates.md; per-class user-visible outcome.)

Funnel baseline: handleRequest tail publishes ONE turn.terminal per turn
(handler.go:1424-1474) via PublishBlockingT (2035). Status derivation:
err != nil + empty Error + non-empty Reply -> parked; err != nil otherwise
-> failed with "I encountered an error: ..." reply fill; syncTurnTimeout
-> timeout; async_dispatch ack with task -> parked (1446-1456). An
early-return safety net defer emits failed/early_return_safety_net for
any claimed turn that exits before the funnel (handler.go:877-893) -
good, but it cannot cover paths that never claim or that hang.

| Class | User receives | Evidence |
|-------|---------------|----------|
| F1 LLM provider error (non-quota) | failed terminal, provider error text (technical but present); alias rotation burns retries first | parts/audit-failures-quota.md A1; loop.go retry/rotation seams |
| F2 Quota 429 chat park | parked ack "queued and will run automatically when the quota resets" (handler.go:1348-1350); resume re-emits under acked turn id IF the payload carries TurnID | A2; loop_park.go:750-761 |
| F2a Fresh throttle park (direct mode) | CRITICAL: loop returns ("", nil) for parked turns (loop.go:3795-3802); funnel parked-detection requires err != nil (handler.go:1427-1432) -> turn.terminal status=completed with EMPTY reply; registry row tombstoned | verified directly |
| F2b Routed-mode park | WithChatTurnID stamped ONLY on the direct-mode branch (handler.go:1254, sole call site); dispatcher-routed parks carry no TurnID -> resume delivery SKIPPED with a Warn (loop_park.go:750-761): work completes, user never gets it | verified directly |
| F3 Context overflow | typed ContextOverflowError -> one-shot firewall compaction + retry; second overflow fails the turn honestly (failed terminal) | A3 |
| F4 Tool error in step | step retry ladder, then task.failed relay -> failed terminal with provider/tool error; retry-exhaustion context missing from text (minor) | A4; queue.go:702, handler.go:2377 |
| F5 Classifier unavailable | turn STILL completes via fallback lanes (never fails outright) but misroute risk + zero disclosure (R1/R2) | dispatcher.go:1870-1982 |
| F6 Runtime dead / missing agent | nil registry / unknown agent / nil LLM client -> failed terminal with honest technical text; CLOSED loop has NO guard: accepts and runs turns degraded; hung staleLoop.Close() in GetOrCreateWired blocks the single serial chat goroutine -> ALL sessions hang, no terminal | parts/audit-classifier-routing.md Area 6; manager.go:131-135, loop.go:4442-4446 |
| F7 Queue job dead-letter | ordinary retry exhaustion relays failed terminal (good); pair-managed step failure publishes NOTHING (tactical.go:2479-2503 returns nil after MarkFailed); PairManager terminal paths never publish task.completed/task.failed (pair_manager.go:354-402) -> 600s opaque watchdog reap; sweeper dead-letters crash-stranded jobs without an event (queue/store.go:1958) | A7 + queue-gates findings 1-3 |
| F8 Turn stall/reap | watchdog emits failed/turn_reaped after stale_after (default 600s, schema.go:4952); FALSE-reaps legitimately parked/approval-waiting/await-seal turns (no park-state exclusion), then a late completed terminal fires under a tombstoned id | A8; strategic.go:2230, rpc/plan_seal.go:150-158 |
| F9 Plan compile failure | critique-loop compile errors fail the task honestly with line-numbered problems (good); BUT Plan() seed/persist/seal errors are log-only (orchestrator.go:369-375) and post-approval synthesis failure publishes no event (plan/manager.go:634-645) -> opaque 600s reap or permanent planning | A9 + queue-gates findings 5-8 |
| F10 Approval rejection | CRITICAL: task.reject RPC calls ApproveFunc (rpc/task_approval.go:55-73, call at :69); daemon wires ONLY Strategic().ApprovePlan (daemon.go:2161-2163). Rejecting a plan EXECUTES it and acks "rejected". StrategicPlanner.RejectPlan exists (strategic.go:2336) with zero RPC callers; task.rejected has no subscribers (strategic.go:2356) so even a correct rejection yields no terminal | verified directly |
| F11 Employee gate failure | constitution denies actionable + audited (good); goal-gate rejection strands the park: release pump subscribes ONLY plan.rejected/plan.cancelled (gate_park_wiring.go:266-273); goal rejection (goal_loop.go:1536) emits no matching event -> goal parked on a 10-year horizon forever. ReleaseByKey itself tries both plan| and goal| prefixes (gate_park.go:328-339) - the gap is the missing goal-event subscription, not the key loop | A11 + queue-gates finding 17; wiring verified directly |
| F12 chat.submit itself | invalid payload/empty message -> failed terminal (handler.go:796-812); duplicate -> parked/duplicate_suppressed (904-918); ack accepted but chat.request publish dropped -> 600s opaque reap or forever hang with watchdog off, retries suppressed meanwhile (chat_submit.go:201-225) | A12 |
| F13 Panic | NO recover in handleRequest / RPC dispatch / HTTP handlers (only runEffectsResumeHook handler.go:213-225 and watchdog safeEmit turn_watchdog.go:171): one panic kills the daemon; all in-flight requests die with it | verified directly (grep recover) |
| F14 Bus delivery | PublishBlockingT 5s per-subscriber timeout LOGS and DROPS the terminal (bus.go:249-258); no replay/query RPC for a lost terminal -> completed work, client waits forever | verified directly |

### 3.1 Outright-failure enumeration (no user-visible recovery path)
From parts/audit-failures-quota.md A13, condensed; items marked * have NO
backstop at all, others are bounded only by the config-disableable 600s
watchdog:
1.* Daemon-killing panic (F13).
2.* Fresh throttle-park completed+empty; routed park resume never delivered
   (F2a/F2b).
3. Terminal lost to full bus buffer (F14).
4. Plan request dropped on publish: parked ack, no work (handler.go:1796-1814).
5.* Pair-managed failure / all PairManager outcomes silent (F7).
6. Plan() seed/persist/seal errors log-only (F9).
7.* Post-approval synthesis failure: no event ever (F9).
8.* task.reject executes the plan - outcome is the OPPOSITE of the request
   (F10).
9. Rejected/awaiting-approval turns: no terminal until false reap (F8/F10).
10.* Goal-gate rejection strands the goal permanently (F11).
11. Sweeper dead-letter without event (F7).
12.* Park payload undecodable/panic-in-resume: log-only drop
    (loop_park.go:568-583, parked_turn.go:638-646).
13. Submit ack accepted while chat.request dropped (F12).
14.* Hung staleLoop.Close freezes ALL chat (F6).
15.* Closed-loop turns run degraded with no signal (F6).
16.* Steer "queued" receipt orphaned when the owning loop exits before
    drain (dispatcher.go:3191; see 4.5).
With orchestrator.turn_watchdog.enabled=false, items 3-7/9/11/13 become
permanent hangs. The user requirement "no requests fail outright" is
violated by every * item and by the false-reap class.

===========================================================================
## 4. STEERING / FOLLOWUP
===========================================================================
(Merged from parts/audit-session-steering.md Section B.)

### 4.1 Surfaces and receipts
chat.steer / chat.followup / chat.queue_status / chat.queue.restore are
native RPC (rpc/queue.go:25-30). Steer/followup return a sync receipt
{status:"queued", queue:"steer"|"followup"} (rpc/queue.go:70-74, 108-111)
plus bus queue events. The receipt means ENQUEUED, not delivered: the
loop drains steering at iteration top (loop.go:5127-5128) and follow-ups
at natural stop (6514-6515); latest-wins overwrite silently discards an
already-acked steer when a second arrives (queue.go:240-245). No turn-id
targeting: a steer arriving in the terminal window is lost or lands in
the NEXT turn (rpc/queue.go:46-50, loop.go:3522-3528).

### 4.2 Steering after loop close (CRITICAL)
MessageQueue.Close is a one-way CAS (queue.go:400-403); nothing ever
reopens it (grep closed.Store(false)/Reopen: zero). UnregisterActiveQueue
closes the queue on EVERY loop exit (registry.go:2059-2074), and the next
turn of a cached loop re-registers the SAME queue object (loop.go:3513).
Result: mid-turn steer/followup works on turn 1 and fails with
ErrQueueClosed on every later turn of that loop; the dispatcher lane then
falls through to a CONCURRENT new turn on one loop (dispatcher.go:3164-
3170) instead of continuing. Direct RPC steer returns "steer failed:
queue is closed". Fix: reset/reopen the queue at RegisterActiveQueue, or
mint a fresh queue per turn.

### 4.3 Shared template queue (CRITICAL)
ConfigSnapshot clones share ONE MessageQueue pointer across all session
loops built from a template (loop.go:10107 WithMessageQueue(l.queue),
manager.go:153): steering one session can deliver into another, and one
loop's exit closes the queue for all clones. Fix: clone the queue per
session loop.

### 4.4 Thread-id remap: absent (CRITICAL for default config)
GetActiveQueue is an EXACT map lookup (registry.go:2078-2088). No steer/
followup/status surface remaps conv-*-thread-* ids to the parent or vice
versa. The TUI sends the session conv id (tui/chat.go:3540, 2669) while
the queue is registered under the thread-resolved conv id
(thread_router.go:151): default-config steering dies with "queue not
found" (queue_errors.go:9, rpc/queue.go:63/100). HTTP maps that sentinel
to 500 "internal server error" instead of 404 (comm/http/api_handlers.go:
222-224). queue_status never 404s - it returns is_active:false for any
unknown id (rpc/queue.go:130-137), false-negatives thread ids, and cannot
see parked/planning state at all. Fix: resolve thread<->session both
directions before lookup; map ErrQueueNotFound to 404; expose park state
in queue_status.

### 4.5 Content truncation and loss (MAJOR)
The dispatcher steer/followup lane injects result.Intent.Summary - the
first sentence or 100 chars (dispatcher.go:3180/3194 + extractSummary
4206-4212). A 500-char steering instruction arrives as its first 100
chars; the rest is silently dropped even though the client got a
"queued" receipt. OriginalInput is preserved on the result but unused
here. Boot "restore" DELETES persisted follow-ups without re-injection
(queue_recovery.go:54/109); consumed rows never cleared (queue.go:348-
371) so the restore count is phantom. task.amend.submit acks "submitted"
but the amendment subscriber only logs (amendment_manager.go:209-211).
Direct-mode daemon template has NO queue at all (components.go:2355-
2670): steering is dead there by construction.
Fix: inject OriginalInput; re-inject or honestly report restored rows;
wire the amendment subscriber; document/flag queue-less direct mode.

===========================================================================
## 5. QUOTA-DISABLED BEHAVIOR
===========================================================================
(Merged from parts/audit-failures-quota.md Section B and
parts/audit-quota-config.md; wiring verified there.)

### 5.1 What llm.budget.enabled=false disables
ONE mapping seam: llmBudgetConfigFromConfig returns an all-zero
llm.BudgetConfig when !Enabled (daemon.go:65-82); zero limits mean
unlimited everywhere in the llm.Budget tracker (hourly/daily/per-task/
per-session token+cost, RPM). The hierarchical agent budget additionally
requires llm.budget.enabled AND agent.budget.total > 0
(agentBudgetEnabled, daemon.go:88-93). BudgetConfig itself:
schema.go:1866-1879, defaults all-zero/disabled at 4298-4310. Mode
"steer" (advisory-only breach) exists but is irrelevant when disabled.
Verdict: enabled=false genuinely zeroes ALL local budget enforcement.

### 5.2 Provider-side parking STILL fires (error-driven, not budget-gated)
- Chat quota park: QuotaResumeWatcher wired unconditionally
  (components.go:4447/4960); a provider 429-quota/402 parks the turn up
  to the 24h horizon in BOTH multi_agent modes.
- Throttle park: gated only by parker != nil (loop_park.go:361), wired in
  multi_agent mode; no config off-switch.
- Endpoint-blocked park, quota episode tracker (agent state quota_wait/
  blocked), worker quota requeue, and tactical 10-deferrals/6h all fire
  on provider errors regardless of budget config; the deferral bound has
  a code seam but NO config key.
- llm.quota_retry.enabled gates ONLY resolver quota-blocking/rotation;
  duration knobs (max_wait, horizon, base_throttle, poll_floor,
  short_retries) CLAMP back to defaults - 0 is not a disable.
- Real off-switches that exist: llm.budget.enabled,
  llm.failure_policy.pacing.enabled, llm.context_firewall.enabled,
  compaction.enabled, orchestrator.turn_watchdog.enabled,
  agent.watchdog.enabled, multi_agent.enabled=false (SIDE EFFECT: removes
  the dispatcher AND the classifier LLM AND the throttle parker; single-
  agent chat only; quota watcher park remains), employees.enabled=false
  (default TRUE at schema.go:5182; off avoids the goal-loop episode
  parker at components.go:6849).

### 5.3 Quota-free test rig config (toml)
```toml
[llm.budget]
enabled = false                 # all local budget enforcement off
[agent.budget]
total = 0                       # hierarchy never wired
[llm.quota_retry]
enabled = false                 # resolver blocking only; park still fires
[llm.failure_policy.pacing]
enabled = false                 # real off-switch
[parking]
queue_backend = false           # keep sqlite park store
[orchestrator.turn_watchdog]
enabled = false                 # long silent rig steps never reaped
[agent.watchdog]
enabled = false
[llm.context_firewall]
enabled = false
[compaction]
enabled = false
[agent.atomic_compaction]
enabled = false
[orchestrator]
sync_chat_enabled = false
classifier_boot_fail_fast = false
classifier_fail_fast = true     # honest classifier failures, no rotation
[multi_agent]
enabled = false                 # removes classifier LLM + throttle parker
[agent.retry]
enabled = false
[employees]
enabled = false                 # default true; avoids goal episode parker
[bots]
enabled = false
```
Non-disableable residue the rig must tolerate or avoid:
1. Provider quota-shaped 429/402 still parks chat turns up to 24h
   (QuotaResumeWatcher). Use an endpoint that never returns quota errors,
   or treat the "queued ... quota resets" reply as a quota event.
2. Queue-dispatched task runs still quota-defer (worker requeue + tactical
   10/6h). Prefer inline chat turns in the rig.
3. There is NO classifier enabled flag: eliminating classifier LLM calls
   requires multi_agent.enabled=false with its single-agent side effect.
Full key inventory with defaults and clamp behavior:
parts/audit-quota-config.md sections 1-3.

===========================================================================
## 6. RANKED TOP-10 DEFECTS / GAPS
===========================================================================

1. CRITICAL - task.reject EXECUTES the plan it is asked to reject.
   rpc/task_approval.go:69 (handleReject calls ApproveFunc),
   daemon.go:2161-2163 (only ApprovePlan wired);
   StrategicPlanner.RejectPlan (strategic.go:2336) has zero RPC callers.
   Fix: add RejectFunc to TaskApprovalHandler, wire Strategic().RejectPlan,
   and publish task.rejected -> turn terminal (subscriber also missing,
   strategic.go:2356).

2. CRITICAL - Fresh throttle-parked turns terminalize completed with an
   EMPTY reply; routed-mode parks lose the turn id so the resume answer
   is never delivered. handler.go:1427-1432 (parked detection requires
   err != nil), loop.go:3795-3802 (park returns "", nil),
   handler.go:1254 (WithChatTurnID only on direct mode),
   loop_park.go:750-761 (no-TurnID resume skips delivery).
   Fix: detect the explicit parked marker in the funnel regardless of
   err; stamp WithChatTurnID on the dispatcher-routed path too.

3. CRITICAL - Steering queue closes after turn 1 and is never reopened;
   cached loops re-register the same dead queue. queue.go:400-403
   (one-way CAS), registry.go:2073 (Close on unregister), loop.go:3513
   (re-register same object); dispatcher fallthrough spawns concurrent
   turns (dispatcher.go:3164). Compounded by ONE queue shared across all
   template-clone session loops (loop.go:10107, manager.go:153).
   Fix: fresh/reset queue per turn registration; clone queue per session.

4. CRITICAL - No thread-id remap on chat.steer/followup/queue_status:
   default-config TUI steering fails "queue not found"; HTTP returns 500
   for the sentinel. registry.go:2082 (exact lookup), rpc/queue.go:63,
   queue_errors.go:9, comm/http/api_handlers.go:222-224,
   thread_router.go:151 (queue lives under thread id).
   Fix: resolve thread<->parent both directions before lookup; map the
   sentinel to 404; include parked/planning state in queue_status.

5. CRITICAL - Pair-managed step failure and ALL PairManager terminal
   paths publish no task event: silent drop, task never terminalizes,
   600s opaque reap. tactical.go:2479-2503, pair_manager.go:354-402.
   Fix: publish task.failed/task.completed on every pair terminal path
   through the same relay the ordinary step lane uses.

6. CRITICAL - Plan-compiler follow-up gap still open at HEAD: chat
   follow-ups to a parked planning task create a NEW task+draft instead
   of refining the draft. dispatcher.go:1449/2592 (no SaveDraft branch),
   strategic.go:924-975, rpc/plan_seal.go:224-227 (only refinement seam).
   Fix: route non-command follow-ups on sessions with an unsealed draft
   to plan.SaveDraft + recompile.

7. MAJOR - Classifier degradation is silent and broad: 60s cooldown after
   one failure degrades all sessions; non-empty errors hit the verb-dense
   keyword table; ClassificationNotice is dead wiring (never assigned).
   llm_classifier.go:37/585-591, dispatcher.go:1870-1896/231-234,
   handler.go:1258-1262. Fix: chat-fallback for ALL unavailable kinds;
   populate ClassificationNotice on fallback lanes; render classified_by.

8. MAJOR - No panic recovery on chat/RPC/HTTP paths (one panic kills the
   daemon and every in-flight request), and PublishBlockingT drops
   turn.terminal after a 5s full-buffer timeout with no replay surface.
   handler.go:749 (no recover; only 213-225 hook + watchdog safeEmit),
   rpc/server.go:532, bus.go:249-258, handler.go:2035.
   Fix: recover-in-handleRequest emitting failed terminal; persist a
   terminal outbox + turn.result query RPC for lost-event recovery.

9. MAJOR - Watchdog false-reaps legitimately parked/approval-waiting/
   await-seal turns at 600s, then late completed terminals fire under
   tombstoned ids; goal-gate rejection strands goal parks forever
   (release pump subscribes only plan.* events).
   schema.go:4952 + turn_watchdog.go (no park-state exclusion),
   strategic.go:2230, rpc/plan_seal.go:150-158,
   gate_park_wiring.go:266-273 vs goal_loop.go:1536.
   Fix: exclude park/gate states from reap (or Touch on park events);
   subscribe goal.rejected/goal.cancelled to ReleaseByKey.

10. MAJOR - Wrong-dir execution fallbacks + steer content truncation:
    shared singleton workingDir mutation races sessions
    (dispatcher.go:3692-3698), step-job fallback runs in daemon CWD with
    no dir injection (components.go:11165-11220), and the dispatcher
    steer/followup lane injects only the ~100-char Summary
    (dispatcher.go:3180/3194/4206) despite a "queued" receipt.
    Fix: fence/clone instead of mutating shared dir; inject session dir
    in every step fallback; inject result.OriginalInput into queues.

Runners-up (major, outside top 10): boot restore deletes persisted
follow-ups without re-injection (queue_recovery.go:54/109);
task.amend.submit acks but subscriber only logs
(amendment_manager.go:209-211); dispatch_trace lacks turn_id and is
keyed on session conv id (R3); residual git-lane misroute on
non-imperative inputs (R4); planner code-hint on research steps (R6);
hung staleLoop.Close freezing all chat (manager.go:131-135).

===========================================================================
## APPENDIX: merged part files (full evidence, same HEAD)
===========================================================================
- /tmp/usability-rig/parts/audit-failures-quota.md (1002 lines): failure
  classes A0-A13 with emit-site traces, outright-failure enumeration,
  quota-disabled verdicts B1-B4.
- /tmp/usability-rig/parts/audit-session-steering.md (565 lines): binding
  chain, working-dir precedence, thread resolution, plan-compiler gap,
  steering semantics B1-B13, ranked top-12.
- /tmp/usability-rig/parts/audit-queue-gates.md (398 lines): dead-letter,
  plan compile, approval, employee gates; 18 findings + verified-good list.
- /tmp/usability-rig/parts/audit-quota-config.md (524 lines): full config
  key inventory with defaults/clamps, config-gated vs error-driven wiring
  table, minimal rig config.
- /tmp/usability-rig/parts/audit-classifier-routing.md (99 lines):
  classifier-unavailable and runtime-deep failure mechanics, ranked top-10.
Section 1 of this report (routing decision chain, R1-R8) was audited
directly by the lead; all other sections merge the part files with lead
spot-verification of every top-10 citation against HEAD.
