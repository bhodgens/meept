# MEEPT INTERACTIVE-USABILITY REVIEW
## Session context, routing, clutter-free replies, observability navigation, planning delineation

Date: 2026-11-19
Daemon HEAD: 1cee89d7 (clean worktree /tmp/meept-usability-head; primary tree
carries 58 uncommitted sibling-session edits and was NOT used for code claims)
Bench HEAD: 5ac2e509 (~/git/meept-bench)
Rig: /tmp/usability-rig (isolated MEEPT_HOME, own daemon, OS-assigned ports,
local-only models: LFM2.5-8B-A1B Q4 general + LFM2.5-Encoder-350M prompt-router
classifier; quotas disabled: llm.budget.enabled=false, quota_retry.enabled=false,
no cloud providers configured)

Evidence classes used below:
- LIVE: reproduced on the rig daemon, artifact paths given.
- CODE: file:line verified at HEAD by read-only audit (subagent reports merged).
- BUILD: compile/build output.

Companion artifacts (all under /tmp/usability-rig):
- findings-orchestrator.md  (29 live findings F-ORCH-1..29, raw log excerpts)
- audit-clients.md          (TUI+GUI client audit, 5 axes, top-10, inventories)
- audit-routing-failures.md (routing chain, failure inventory, steering, quota)
- audit-observability-plans.md (observability + plan UX audit, surface matrix)
- parts/audit-session-steering.md (session binding + steering semantics A1-A9, B1-B8)
- parts/audit-queue-gates.md (failure-path inventory classes 7/9/10/11)
- parts/audit-classifier-routing.md (classifier failure paths, areas 5/6)
- parts/audit-quota-config.md (quota/budget/parking config inventory F1-F20)
- notes-server.md, notes-nav.md, gui-notes.md (GUI/WS/server audits)
- probes/*.json (p1 naive continuity, p2 isolation, p3 failures, p4 plans,
  p5 steering, p6 positive control; full event captures + dispatch traces)
- probes/tui-*.txt|json (agent-tui screenshots of live TUI states)
- bench-results/ + bench-results-clean/ (meept-bench usability-probe suite)
- daemon.log (rig daemon log for the whole session)

--------------------------------------------------------------------------
## 1. EXECUTIVE SUMMARY
--------------------------------------------------------------------------

The async turn contract (submit -> ack -> turn.terminal) is sound on paper and
the daemon implements most of it. In live interactive use, five systemic gaps
break the user experience:

1. WORK ROUTING IS OFTEN WRONG AND INVISIBLE. File-edit prompts route to the
   git "committer" agent deterministically (4/4 naive runs, and even with
   explicit "this is a file edit, not a git operation" wording). The committer
   then completes tasks WITHOUT running any file tool, review approves, and
   the user is told "task completed" while the file is unchanged (LIVE,
   F-ORCH-2/20). No client shows the routing decision; dispatch_trace is
   keyed by conversation_id but clients hold session_id, so even the manual
   query returns empty (F-ORCH-3).

2. PLANNING DEAD-ENDS IN BOTH PIPELINES. With plan_compiler_enabled=true,
   every plan-mode turn parks forever behind a raw prompt-scaffold draft that
   no TUI/GUI surface can show, refine, or seal (F-ORCH-1). With the compiler
   off (default), plans >= 5 steps hit a task-level approval gate whose
   event (task.pending_approval) has ZERO subscribers: no card, no badge,
   plans view shows 0 plans, and the only working approval path is a CLI
   command the user cannot discover (F-ORCH-13/19). Worse: task.reject
   APPROVES and executes the rejected plan (CODE+LIVE, F-ORCH-12).

3. REPLIES ARE NOT CONTEXTUAL OR CLUTTER-FREE. Direct file-write turns
   complete with an EMPTY reply (F-ORCH-15). Simple questions take the full
   task machinery and render job-ID prose twice (card + markdown block,
   F-ORCH-5). Claims/evidence JSON envelopes leak raw into replies
   (F-ORCH-21). Internal review feedback ("## review: approved") pushes into
   the chat stream (F-ORCH-25). Recall follow-ups answer from a last-task-only
   digest, contradict their own printed state, and miss the session's actual
   artifacts (F-ORCH-16/24).

4. BEHIND-THE-SCENES NAVIGATION IS MOSTLY MISSING. turn.terminal carries
   task_id/intent_type/agent_id/classified_by; BOTH clients parse then drop
   these fields, so no reply can link to its task (CODE, audit-observability
   2.1). Tool traces are written to session_tool_calls but no RPC exposes
   them (2.2). GUI task detail pane is unreachable (activeTaskProvider never
   written). TUI event feed dies silently after reconnect and discards the
   bus drop counter; under load the 100-event drop-oldest poll buffer loses
   turn.terminal entirely (LIVE, F-ORCH-26: 9853 of 9953 events dropped).

5. "NO REQUEST FAILS OUTRIGHT" IS NOT MET. Silent-drop paths exist: pair-
   managed step failures never terminalize (CODE, queue-gates #1/#2); plan
   synthesis failure after approval is invisible (#8); non-compile Plan()
   errors leave tasks in planning until the 600s watchdog reaps them with
   opaque text (#6/#7); semaphore-blocked ready steps are never rescheduled
   (LIVE, F-ORCH-28: two bench tasks hung with 0 queue jobs); accepted steers
   are silently ignored or executed as unrelated new turns (LIVE,
   F-ORCH-23/27); conversation-only sessions lose all history to a FOREIGN
   KEY failure (LIVE, F-ORCH-29). The watchdog is the universal backstop but
   it reports "turn reaped: no progress for 10m0s" - never the real cause -
   and it FALSELY reaps legitimate human-approval waits (queue-gates #7/#11).

meept-bench verdict (usability-probe suite, 6 tasks, authored this session):
CLEAN SERIAL RUN (fresh daemon + fresh fixture repo, authoritative): 3 pass /
3 fail. The 3 failures are themselves findings: u-plan-mode-request stalled
302s at the invisible approval gate (even with --auto-approved); u-context-
two-step lost the prior turn's artifact between turns of one conversation;
u-route-research-write produced no file (planner hint-emission gap, not a
capability gap). A first concurrent-load run (1 pass / 5 fail) is reported
in section 8 as load-contaminated; its timeouts corroborate the un-
terminalized-turn and semaphore-hang findings. Full numbers + transcripts:
section 8 and bench-results-clean/.

--------------------------------------------------------------------------
## 2. AXIS 1 - CLEAR CONTEXT BETWEEN MESSAGES
--------------------------------------------------------------------------

What works:
- session_id/conversation_id dual resolution on submit is implemented at
  both RPC and handler (chat_submit.go:149-156, handler.go:824-834).
- Working-dir precedence WorktreePath > ProjectPath > DetectionContext.CWD
  is single-sourced (session/working_dir.go:43-56) and pinned by tests.
- Reasoning never persists in history (StripThinking on every append).
- GUI WS reconnect re-arms subscriptions (websocket_service.dart:332-356).

Defects (ranked):
C1. LIVE F-ORCH-29: conversation-only sessions (chat.submit with
    conversation_id, no session row) fail history persistence:
    "FOREIGN KEY constraint failed (787)" x9 in rig log. Resume shows
    nothing. The bench runner and GUI-test conversations all hit this.
    Fix: create/resolve the session row before persistExchange, or key
    messages on conversation_id with a sessions-row upsert.
C2. LIVE F-ORCH-16/24: recall follow-ups ("did the change get made?",
    "what files did you make?") answer from buildSessionContextDigest-
    Excluding - LAST TASK ONLY. Plural artifact questions get one unrelated
    task; a failed task is described as "still in progress"
    (recall_answer.go:86-90 default branch). Session A could not recall its
    own planted codeword; the recall task decomposed into task_get plumbing
    and returned "task ID was missing". Fix: digest must cover the session's
    task set (or query memory_search for fact questions); the state switch
    must handle failed/cancelled/rejected explicitly.
C3. CODE A6 (session-steering): thread topic resolved TWICE per turn from
    different inputs (full message vs Intent.Summary first-100-chars,
    dispatcher.go:955 vs 3068+4206). Memory/digest key and execution/queue
    key can disagree for the same turn. Fix: resolve once, carry on
    DispatchResult.
C4. CODE A1: no cross-validation when client supplies BOTH session_id and
    conversation_id; a mismatched pair binds work to one and WS routing to
    the other (chat_submit.go:149-156). Fix: verify membership, reject or
    correct.
C5. LIVE F-ORCH-22: unknown session/conversation ids are ACCEPTED and new
    conversations fabricated (phantom sessions). Fix: validate id existence
    at submit; return accepted:false with an actionable note.
C6. LIVE F-ORCH-7: TUI resumes the most-recent session after daemon restart
    with stale failed-task sidebar state and mixed UTC/local timestamps in
    one view. No "previous run" marker. Fix: stamp task rows with daemon
    boot id; render stale non-terminal tasks as "interrupted".
C7. CODE (quota-config 1.8): context firewall silently truncates the system
    prompt on ordinary turns (LIVE F-ORCH-17: 2 truncation WARNs on a fresh
    3-message session). Prompt components drop without disclosure.
C8. CODE (notes-plan-hitl): GUI does NOT restore the last session on
    relaunch - top-tabs layout reuses an empty session or creates "new
    session" (home_screen.dart:388-415); pendingTurns are memory-only (no
    persistence), so an in-flight turn is orphaned across app restart with
    no resume API call. session.auto_resume pref works ONLY in sidebar
    layout (home_screen never reads it). TUI resumes most-recent (C6) -
    the two clients disagree on resume policy.

--------------------------------------------------------------------------
## 3. AXIS 2 - WORK ROUTED TO THE CORRECT LOCATION/WORKER/SESSION
--------------------------------------------------------------------------

R1. LIVE F-ORCH-2/20 (CRITICAL): "Now change hello.txt so it says 'hello
    meept' instead." -> intent=git conf 0.856-0.8806 agent=committer on
    EVERY run (prompt-router lane). Explicit tool-named disambiguation did
    NOT change the lane. The committer task then ran 3 steps, ZERO file
    tools, all approved, file unchanged, user told "completed". ROOT CAUSE
    CODE-CONFIRMED: the 350M prompt-router has a deterministic lexical bias
    ("repository root" -> git lane, dispatcher.go:1779-1789 comment); the
    git-verb agreement veto that fixes it requires hasLeadingImperativeVerb
    (dispatcher.go:1774-1777) - "Now change..." / question-shaped inputs
    skip the veto, so the biased git verdict survives and the contextless
    committer runs. COMPOUNDED by a review defect (claim-without-evidence
    passed: "No explicit feedback provided"). Fix both: (a) drop the
    leading-imperative requirement from the veto (the lexical bias is
    shape-independent) and/or retrain the router lanes; (b) ReviewStep must
    force evidence review when a file-shaped step's tool ledger is empty.
R2. LIVE F-ORCH-23/27 (CRITICAL): steering is broken end-to-end.
    - p5: chat.steer accepted {"status":"queued"} while the loop was active;
      correction never acted on; all original steps ran; zero steer events.
    - p4: a queued steer text later executed as a NEW concurrent turn in the
      same conversation, producing a reply that contradicts disk state.
    CODE root causes (session-steering B2/B3/B4): the MessageQueue is closed
    after turn 1 and never reopened (one-way CAS, queue.go:400-403); session
    clones SHARE the template's queue pointer (ConfigSnapshot
    WithMessageQueue, loop.go:10105-10107) so one session's steer lands in
    another's loop and one session's turn-end closes the other's queue;
    queue lookups never unwrap thread ids (registry.go:2078-2088) while the
    TUI sends the bare session conv id - "queue not found" is the DEFAULT
    steer outcome for thread-routed turns. HTTP surfaces map the same error
    to 500 "internal server error" (api_handlers.go:222-224).
    Fix: fresh queue per RunOnce; exclude queue from ConfigSnapshot;
    GetActiveQueue tries exact -> unwrapped base -> session-owned thread
    keys; map ErrQueueNotFound to 404/409.
R3. CODE B7 (CRITICAL): persisted follow-ups are DELETED at boot and only a
    count event fires ("restored N" is false; queue_recovery.go:82-114).
    Consumed follow-ups also linger (DrainFollowUp never deletes). Fix:
    re-enqueue on boot, delete after injection.
R4. LIVE F-ORCH-28: semaphore-blocked ready steps never reschedule (no
    periodic cycle; tactical.go:513 comment promises a retry that does not
    exist). Two bench tasks hung executing with step ready/job_id NULL and
    an empty queue. Fix: bounded retry ticker + lease-leak audit on
    finalize; surface blocked steps in task.progress.
R5. CODE A2/A4: on GetOrCreateWired failure the dispatcher MUTATES the
    shared singleton loop's workingDir ("race risk" warn,
    dispatcher.go:3689-3698); step-job fallback runs on the interactive
    daemon loop with no dir binding (components.go:11165-11220). Concurrent
    sessions can run in each other's project dir. Fix: fail the turn with
    an actionable error instead of retargeting shared state.
R6. LIVE F-ORCH-3 + CODE (observability 1.3): dispatch_trace keyed by
    conversation_id only; the RPC param is named session_id and clients
    pass the primary id -> count=0. input_summary is empty in every row.
    Fix: dual-key lookup + populate input_summary.
R7. CODE A9 (CRITICAL UX): plan-compiler follow-up gap CONFIRMED at HEAD:
    a refinement message while a task parks in planning creates a NEW task
    and NEW draft; nothing routes chat text into SaveDraft. Live p4 D4
    confirms (new task, failed with "max ralph loop iterations").

--------------------------------------------------------------------------
## 4. AXIS 3 - CONTEXTUAL, CLUTTER-FREE RESPONSES
--------------------------------------------------------------------------

U1. LIVE F-ORCH-15 (CRITICAL): direct route_to_agent file-write turn
    completed with reply="" (chat.response len=0). Work done, user gets
    nothing; the explicit "tell me the full path" question unanswered.
    Daemon: "Agent completed action=close has_report=false". Fix: never ship
    an empty completed reply - synthesize from the tool ledger or re-prompt
    once for a summary.
U2. LIVE F-ORCH-5: "What is 2+2?" -> quickplan -> 10-step plan (later runs)
    or 1-step (early runs) -> reply is a job-log dump ("job job-... completed
    by agent orchestrator: ...") rendered TWICE (task-completed card +
    markdown block). ~15k tokens and 5s for a one-token answer. The reply
    guard fired twice on the planner's own JSON contract. CODE root cause
    (notes-server 2.4): the daemon emits the SAME full reply as BOTH a
    chat_message push AND turn.terminal (task_completed_relay) with NO
    server-side suppression. TUI renders the card from ChatTaskResultMsg
    (chat.go:1505-1523, RoleSystem) AND the terminal reply as an assistant
    bubble - SuppressTaskCompletedFor only gates the relay while a turn is
    pending, and the card path is separate. GUI dedupes by content against
    the LAST assistant bubble only (chat_provider.dart:715-757
    _lastAssistantContentEquals) - fragile when another message interleaves.
    Fix: trivial questions need a direct chat lane; server should mark the
    relay pair so clients render once; TUI must dedupe card vs terminal.
U3. LIVE F-ORCH-21: claims/evidence JSON envelope ({"status","accomplished",
    "not_done","issues","observations"}) renders RAW inside the
    task_completed_relay reply despite the StripClaimsEvidenceOrProse
    contract. Fix: extend the stripper to this envelope shape on the relay
    path (formatTaskCompletedMessage/bestSummaryResult).
U4. LIVE F-ORCH-25: "## review: approved" chat messages push internal
    reviewer feedback (including "[unverified: no tools were executed this
    turn]" and task_get failure narration) into the user stream. Fix:
    review feedback belongs in the task detail, not chat.response.
U5. LIVE F-ORCH-4: TUI task-detail step row renders raw planner scaffolding
    ("## Session execution context - Open tracked tas..."). The chat-lane
    StripPromptScaffolding is not applied on the task-detail render path.
U6. LIVE: sidebar "recent memory" renders raw "<<<MEMORY_CONT..." markers.
U7. CODE (observability 4.1): parked status re-arms the awaiter with no text
    change in TUI; GUI parked text is quota-specific even for non-quota
    parks. The ack note "accepted; result arrives via turn.terminal" is
    never shown on success. Fix: explicit pending-state copy per handler_case.
U8. LIVE F-ORCH-6: orchestrator ERROR spam "failed to find step for job"
    after every task (memory-capture side jobs with empty step_id). Noise
    that masks real errors in the log the user is told to read.

--------------------------------------------------------------------------
## 5. AXIS 4 - BEHIND-THE-SCENES NAVIGATION
--------------------------------------------------------------------------

Surface matrix (full version in audit-observability-plans.md section 7):

| Surface                  | TUI            | GUI                  | Link from chat |
|--------------------------|----------------|----------------------|----------------|
| task detail + steps      | exists (t)     | detail UNREACHABLE   | MISSING        |
| tool traces              | MISSING        | MISSING              | MISSING (no read RPC) |
| dispatch trace           | MISSING        | MISSING (no route)   | MISSING        |
| activity feed            | exists (a)     | exists (/activity)   | MISSING        |
| queue status             | exists (q)     | read-only            | partial        |
| plan phases              | exists (p)     | exists (/plans)      | partial/broken hint |
| plan draft/seal          | CLI only       | CLI only             | MISSING        |
| plan approve/reject      | a/v/c, no reject | REST buttons       | MISSING        |
| audit chain verify       | CLI only       | MISSING              | MISSING        |
| metrics                  | partial        | exists               | no per-turn attribution |
| bus drop accounting      | discarded      | n/a (WS)             | MISSING        |

N1. CODE (observability 2.1, CRITICAL): turn.terminal carries task_id,
    intent_type, agent_id, handler_case, classified_by, model, trace_id.
    TUI keeps only conversation_id/reply/status/error/duration_ms
    (chat_turn.go:389-396); GUI parses taskId then drops it
    (chat_provider.dart:719-725, ChatMessage has no taskId field). Stale
    comments assert "ack carries no task_id" - false at HEAD. This is the
    single biggest navigation gap: the correlation data is ON THE WIRE and
    both clients throw it away. Fix: add TaskID/IntentType/HandlerCase to
    both message models; render a tappable "task <id> - view" chip.
N2. CODE (observability 1.2): session_tool_calls writer is ALIVE
    (handler.go:1586-1590, regression-tested) but NO client-facing read
    path exists (session.messages.get returns plain messages; Message has
    no ToolCalls field). Fix: include_tool_calls option joining
    GetToolCallsForMessages; expandable trace under bubbles.
N3. CODE (observability top-10 #1, CRITICAL): TUI event feed dies silently
    after reconnect - poll errors ignored, no re-subscribe
    (events.go:144,161-163,244-245). LIVE correlate F-ORCH-26: the 100-event
    drop-oldest buffer lost 9853/9953 events under load including terminals.
    Fix: re-subscribe on "subscription not found"; critical-topic lane that
    never trims turn.terminal/task.completed/task.failed; treat dropped>0
    as re-query signal.
N4. CODE (observability 1.1): GUI task detail pane unreachable -
    activeTaskProvider never written, TasksList built without onTaskSelected
    (tasks_tab.dart:10,22); Task.steps models dead. Fix: wire selection +
    hydrate via GET /tasks/{id} + /steps.
N5. CODE (observability 1.3): dispatch evidence rendered NOWHERE; no HTTP
    route for dispatch_trace. The async ack markdown ("**mode:** planned")
    is the only user-visible routing signal, and only for async turns.
N6. LIVE: TUI tasks view shows "0/1" steps for a completed 1/1 task (two
    unlabeled counters); detail modal closes on q but not Escape; no link
    from a chat reply to its task (must match by title/time in tasks view).
N7. CODE (nav notes): GUI activity tiles render only summary+kind+time;
    taskId/stepId/detail parsed but unrendered; deep link
    meept://session/<id> -> /activity?session=<id> silently DROPS the param
    (no widget reads queryParameters). Search results navigate to tab roots
    without selecting the item. /diff tells the user "review in
    /tools/changes:" as text but does not navigate.

--------------------------------------------------------------------------
## 6. AXIS 5 - PLANNING / PLANNER / DIRECT EXECUTION DELINEATION
--------------------------------------------------------------------------

P1. LIVE F-ORCH-1 (CRITICAL): plan_compiler_enabled=true -> every plan-mode
    turn seeds a RAW PROMPT SCAFFOLD draft ("You are the planner agent in a
    brainstorm session... <no value>") and parks in planning forever.
    plan.list returns 0 (drafts live in task.Metadata, not plans.db). TUI
    shows "mode: planned | 0 subtasks" then "thinking..." indefinitely.
    Only recovery: CLI meept plans show-draft/edit-draft/seal. No TUI/GUI
    call sites for plan.draft/plan.seal (grep zero). Default config ships
    the flag OFF, so production is dark today - but the feature is
    unusable from chat the moment it flips on.
P2. LIVE F-ORCH-13/19 (CRITICAL): legacy task approval gate (>=5 steps,
    hardcoded threshold, no config key; plans.approval.require_approval
    does NOT control it) publishes task.pending_approval - an ORPHAN topic
    (bus-topology.md:230, zero subscribers). TUI chat handles only the
    plans.db plan.* family; plans view reads plan.list_by_session -> 0 rows.
    The gate is invisible; only CLI meept task approve works. The threshold
    clamps <=0 back to 5 (strategic.go:323-324) so it cannot be disabled.
P3. CODE+LIVE F-ORCH-12 (CRITICAL): task.reject calls ApproveFunc
    (task_approval.go:69) and returns {"status":"rejected"} while the plan
    EXECUTES. StrategicPlanner.RejectPlan exists (strategic.go:2336-2363)
    with ZERO callers. Live proof: rejected task moved to executing.
    GUI has NO task.approve/task.reject call sites and NO HTTP route exists
    for them - the strategic approval gate is a DEAD END in the GUI (gray
    row, cancel only). The GUI Plans-tab reject buttons ride the plans.db
    PlanManager path, which is CORRECT end-to-end (manager.go:652-681) but
    is a DIFFERENT gate that cannot release awaiting_approval tasks.
    TUI/CLI task.reject is the destructive surface. Fix: wire RejectFunc;
    add HTTP routes + GUI affordance; subscribe task.rejected (orphaned)
    and relay a terminal with the reason.
P4. LIVE F-ORCH-14: planner over-decomposes trivial questions (2+2 -> 10
    steps) which then trip the approval gate. Prompt tiers materially
    change plan size (1 step pre-prompt-copy vs 10 after). No triviality
    escape. max_plan_steps truncates the plan (strategic.go:1673) but the
    gate counts steps BEFORE truncation in some paths (11 steps logged
    with max_plan_steps=4).
P5. CODE (queue-gates #6/#7/#11): approval/seal waits are REAPED as failures
    at 600s ("turn reaped") even though the system correctly waits for a
    human; on later approval a SECOND terminal (completed) fires under the
    tombstoned turn id: parked -> failed(reaped) -> completed. Fix: exclude
    planning/awaiting_approval from watchdog staleness or heartbeat them.
P6. CODE (queue-gates #8): plan synthesis failure after approval is
    invisible - synthesis_error stored on the row, no bus event, failPlan
    deliberately publishes nothing. User approved and never sees work start
    or fail. Fix: session-scoped plan.failed notification.
P7. CODE (observability 4.2): TUI /plan is BROKEN - calls session.set_mode
    which has no daemon handler (command_handler.go:1632); the chat card
    advertises "/plan approve <id>" which does not exist. GUI /plan works
    only by fallthrough to the dispatcher prefix route; its advertised
    list/show/approve subcommands do not exist daemon-side. NO force-direct
    control exists anywhere (mode is heuristic, short-input <50 chars
    downgrades to direct).
P8. LIVE p4 D4: refinement follow-up created a NEW task that failed with
    "max ralph loop iterations reached without sufficient evidence" and
    contradictory progress "1/1 steps completed, 3 failed". The failure
    text is internal vocabulary; no recovery action offered.
P9. CODE (observability 3.4): evolver document proposals (0 phases/0 steps)
    render as literal zeros in TUI plans tab; the chat-card fix
    (planCountLine "document proposal") exists but was not reused in the
    tab/detail; GUI renders a bare card, filePath unrendered.

--------------------------------------------------------------------------
## 7. FAILURE-PATH INVENTORY ("no requests should fail outright")
--------------------------------------------------------------------------

Verdict: NOT MET. The terminal funnel (publishTurnTerminal,
PublishBlockingT) is honest for the paths that reach it. The gaps are paths
that never reach it, or reach it with the wrong story:

Silent / no-terminal paths (CODE unless noted):
- Pair-managed step failure: OnJobFailed pair branch returns before any
  publish (tactical.go:2479-2503). PairManager finalizeTask publishes
  NOTHING (pair_manager.go:354-402); orchestrator pair handlers log-only.
  User waits 600s for "turn reaped". (queue-gates #1/#2, CRITICAL)
- Non-compile Plan() errors: handlePlanRequest logs and returns
  (orchestrator.go:369-375); task stuck planning. (#6)
- Plan synthesis failure after approval: no event. (#8)
- Crash-stranded exhausted jobs: sweeper dead-letters with no event. (#3)
- Semaphore-blocked ready steps: never rescheduled (LIVE F-ORCH-28).
- Accepted steer dropped: receipt says queued, no delivery, no negative
  event (LIVE F-ORCH-23; CODE B8 latest-wins overwrite).
- Conversation-only history: FK failure, Warn only (LIVE F-ORCH-29).
- Closed-loop turns: no ErrLoopClosed exists; RunOnceWithParts never checks
  l.closed (classifier-routing F6-1). Turns run on torn-down loops.
- Hung staleLoop.Close(): shadow-capture goroutines use context.Background()
  with no timeout; Close does wg.Wait() on the dispatch path; ONE hung
  Close stalls ALL chat traffic (serial handler goroutine) with no terminal
  (classifier-routing F6-2, MAJOR).

Wrong-story paths:
- task.reject -> executes (F-ORCH-12).
- Watchdog reaps legitimate approval waits; late completed terminal after
  failed(reaped) (queue-gates #7/#11).
- Recall answers contradict printed state (F-ORCH-16).
- Completed tasks with zero tool effects (F-ORCH-20).
- Empty completed replies (F-ORCH-15).
- Classifier degradation is SILENT: ClassificationNotice field exists,
  ClassificationUserGuidance helper exists, ZERO writers/callers
  (classifier-routing F5-1). The boot-readiness doc names this exact defect;
  the fix wiring is dead.

Honest paths (verified good):
- Empty/whitespace submit: accepted:false + note (LIVE p3 F1/F2).
- Missing agent / nil LLM client: failed terminal with truthful (technical)
  text (classifier-routing area 6).
- Plan compile failure via critique loop: failTaskWithReason with line
  numbers (queue-gates #5).
- Ordinary step retry exhaustion: task.failed -> failed terminal
  (queue-gates class 7 baseline).
- Context overflow: typed ContextOverflowError + one-shot compaction retry
  (landed contract, skill-verified).

--------------------------------------------------------------------------
## 8. MEEPT-BENCH RESULTS
--------------------------------------------------------------------------

Suite: suites/usability-probe.json (NEW, authored this session, 6 tasks:
u-context-two-step, u-steer-midrun, u-route-research-write,
u-fail-nonexistent-read, u-plan-mode-request, u-direct-explicit).
Runner: meept-bench 5ac2e509, --auto-approved, judge-local.py against the
rig 8B. Quota-free local models.

Run 1 (concurrent with live probes - load-contaminated, reported honestly):
  u-context-two-step      TIMEOUT 426s  (task stuck executing, step ready,
                                         job never enqueued - F-ORCH-28)
  u-steer-midrun          TIMEOUT 426s  (same hang class; steer accepted
                                         via chat fallback, never acted on)
  u-route-research-write  FAIL 3.7s     (researcher capability gap: no
                                         web_fetch+file_write pairing;
                                         routed coder, empty reply, no file)
  u-fail-nonexistent-read TIMEOUT 300s  (committer git misroute of a read
                                         request; task never terminalized
                                         within deadline)
  u-plan-mode-request     PASS 30.9s    (VACUOUS checker exit_zero(true);
                                         transcript: planner routed, task
                                         completed, reply = step-log dump)
  u-direct-explicit       TIMEOUT 300s  (semaphore-starved under load)
  5/6 failed. NOTE: the timeouts are themselves the finding - the daemon
  left turns un-terminalized under concurrent interactive load.

Run 2 (clean serial, fresh daemon + fresh fixture repo project2):
  u-context-two-step      FAIL 54s   (primary wrote counter.txt=3; the
                                      +45s follow-up ran as a NEW task and
                                      its file_read found counter.txt
                                      MISSING - "file not found" at the
                                      exact path the primary wrote 45s
                                      earlier. Cross-turn artifact/context
                                      loss between turns of one conversation.
                                      Also: model hallucinated /workspace/
                                      counter.txt, security denied it.)
  u-steer-midrun          PASS 24s   (steer accepted via chat fallback;
                                      result2.txt written. NOTE: pass under
                                      clean load does not contradict
                                      F-ORCH-23 - the steer landed as a new
                                      turn that happened to do the work.)
  u-route-research-write  FAIL 5s    (fetch+write task: no file produced.
                                      CORRECTION: researcher DOES ship
                                      web_fetch+file_write at HEAD
                                      (config/agents/researcher/AGENT.md:12-18);
                                      the residual gap is planner hint
                                      emission / routing, not capability.)
  u-fail-nonexistent-read PASS 8s    (honest "does not exist" outcome)
  u-plan-mode-request     ERROR 302s (turn stalled: no terminal within
                                      liveness window. Daemon: "Plan awaiting
                                      user approval steps=21" - the invisible
                                      approval gate, F-ORCH-13, reproduced
                                      cleanly with max_plan_steps=4 set: the
                                      gate counted 21 steps BEFORE truncation.
                                      --auto-approved does NOT approve the
                                      strategic task gate.)
  u-direct-explicit       PASS 11s   (direct file write, correct)
  3/6 failed. The clean run confirms: the approval gate stalls plan turns
  even with --auto-approved; follow-up turns lose prior-turn artifacts;
  the fetch+write routing gap persists despite the capability being present.

Harness notes for future runs:
- The suite's turn delays use delay_s (no after_reply support in the runner).
- u-plan-mode-request needs a REAL checker (plan artifact existence or
  llm_judge on the reply) - exit_zero(true) is vacuous and was used only to
  satisfy the runner's >=1-checker validation.
- Bench sessions are conversation-keyed -> they hit F-ORCH-29 (history FK
  failure) on every task; transcripts still capture via the bus.

--------------------------------------------------------------------------
## 9. DISCRETE ACTIONABLE WORK ITEMS (ranked, with fix sketches)
--------------------------------------------------------------------------

Tier 1 - correctness/safety (do first):
W1. Wire task.reject to StrategicPlanner.RejectPlan; add RejectFunc seam;
    subscribe task.rejected in ChatHandler and relay turn.terminal with
    reason. Pin: reject must NOT execute. (F-ORCH-12; task_approval.go,
    daemon.go:2161-2163, strategic.go:2336)
W2. Fix steering queue lifecycle: fresh MessageQueue per RunOnce; exclude
    queue from ConfigSnapshot clones; GetActiveQueue thread-unwrap fallback;
    map ErrQueueNotFound to 404/409 on HTTP. (R2; queue.go:400, loop.go:3513/
    10105, registry.go:2078, api_handlers.go:222)
W3. Never ship empty completed replies: synthesize from tool ledger or one
    bounded summary re-prompt. (U1; handler.go route_to_agent completion)
W4. ReviewStep: force evidence review when a file-shaped step's tool ledger
    is empty; block "completed" with zero effects. (R1; review_manager.go)
W5. Terminal-event delivery: critical-topic lane exempt from the 100-event
    trim; TUI re-subscribe on poll error + dropped>0 re-query. (N3;
    proxy.go MaxEvents, events.go:144/244)
W6. Watchdog: exclude planning/awaiting_approval turns from staleness;
    heartbeat on draft/seal/approval activity. (P5; turn_watchdog.go)
W7. Pair failure paths: finalizeTask publishes task.completed/task.failed;
    OnJobFailed pair branch terminalizes. (queue-gates #1/#2;
    pair_manager.go:354, tactical.go:2479)

Tier 2 - routing/context:
W8. Classifier: drop the hasLeadingImperativeVerb requirement from the
    git-verb agreement veto (dispatcher.go:1774-1777) so biased git
    verdicts on edit/question-shaped inputs are vetoed too; retrain
    prompt-router lanes on the frozen p1/p6 edit/create/git corpus.
    (R1; audit-routing-failures R4)
W9. dispatch_trace: dual-key lookup (session_id OR conversation_id),
    populate input_summary, add HTTP route + per-turn "why this route"
    popover. (R6; dispatch_trace.go:20)
W10. Recall digest: session task-set coverage + explicit failed/cancelled
    state handling; fact questions route to memory_search. (C2;
    recall_answer.go:86, dispatcher digest)
W11. Session validation at submit: unknown ids rejected; both-ids mismatch
    corrected; conversation-only sessions get a session row before
    persistence. (C4/C5/C1; chat_submit.go:149, persistExchange)
W12. Semaphore-blocked steps: bounded retry ticker + lease audit. (R4;
    tactical.go:513)
W13. Follow-up boot recovery: re-enqueue persisted follow-ups, delete after
    injection; delete on drain. (R3; queue_recovery.go:82, queue.go:348)

Tier 3 - clutter/navigation/planning UX:
W14. Both clients: keep task_id/intent_type/handler_case on the message
    model; tappable task chip on assistant bubbles. (N1; chat_turn.go:389,
    chat_provider.dart:719)
W15. Strip claims/evidence envelopes on the relay path; stop pushing
    "## review:" feedback to chat; strip scaffolding in task-detail render.
    (U3/U4/U5)
W16. Plan approval visibility: subscribe task.pending_approval in TUI/GUI,
    render an approval card with approve/reject buttons calling task.approve/
    task.reject (after W1); expose the threshold as config. (P2;
    strategic.go:2230, app.go:1899)
W17. Plan compiler chat surface: draft card + seal/reject actions keyed off
    the ack task_id, or auto-run the brainstorm loop to fill the scaffold;
    route refinement follow-ups into SaveDraft. (P1/R7; plan_draft.go,
    dispatcher ClassifyAndRoute)
W18. Trivial-question direct lane: extend isShortSimpleMessage or add a
    fast-path for interrogatives with no artifact verb; dedupe card+relay
    double render. (U2; dispatcher.go:1556, chat.go render)
W19. session_tool_calls read surface: include_tool_calls on
    session.messages.get; expandable trace in both clients. (N2)
W20. GUI task detail wiring (onTaskSelected -> activeTaskProvider + steps
    hydration); activity tile tap-through + deep-link param consumption;
    TUI /plan fix (forward to chat.submit or register the RPC). (N4/N7/P7)
W21. GUI web build fix: rename openExternalWeb -> openExternal in
    platform_open_web.dart (one line; BUILD-verified failure at HEAD).
    (F-ORCH-18)
W22. ClassificationNotice: populate from ClassificationUserGuidance on
    every fallback branch so degradation is disclosed. (classifier-routing
    F5-1; dispatcher.go:1870-1982)
W23. Steer/followup content: the dispatcher queue-injection lane injects
    only Intent.Summary (~100 chars, extractSummary dispatcher.go:4206)
    despite a "queued" receipt - long corrections are truncated before the
    loop ever sees them. Inject result.OriginalInput. (audit-routing-
    failures top-10 #10; dispatcher.go:3180/3194)
W24. Parked-turn terminalization: fresh throttle-parked turns terminalize
    completed with EMPTY reply (park detection requires err != nil,
    handler.go:1427-1432; park returns "", nil loop.go:3795-3802);
    dispatcher-routed parks lose the turn id (WithChatTurnID only on direct
    mode, handler.go:1254) so the resume answer is never delivered.
    (audit-routing-failures top-10 #2)
W25. Panic recovery + terminal outbox: handleRequest has no recover (one
    panic kills the daemon and all in-flight requests); PublishBlockingT
    drops turn.terminal after a 5s full-buffer timeout with no replay.
    Add recover-in-handleRequest emitting a failed terminal + a turn.result
    query RPC / persisted outbox for lost-event recovery. (audit-routing-
    failures top-10 #8)
W26. Critical-topic publish scope: ONLY turn.terminal uses PublishBlockingT
    (handler.go:2035). ALL plan.* lifecycle events, task.completed/failed,
    queue.job.*, agent.progress publish NON-BLOCKING and silently drop on a
    full 100-buffer or absent subscriber (bus.go:132-135,249; manager.go:1907;
    audit-observability 5.4). A missed plan.rejected/completed means the chat
    card never renders; a disconnected client never learns the turn finished
    (no terminal replay store). Fix: move task.completed/failed + plan.*
    terminal events to PublishBlockingT and add the outbox/replay from W25.
W27. Goal-gate rejection strands parks forever: the gate-park release pump
    subscribes ONLY plan.rejected/plan.cancelled (gate_park_wiring.go:266-273);
    goal rejection (goal_loop.go:1536) emits no matching event, so a rejected
    goal parks on a 10-year horizon. ReleaseByKey already tries plan|/goal|
    prefixes - the gap is the missing goal.rejected/goal.cancelled
    subscription. Fix: subscribe both goal events to ReleaseByKey.
    (audit-routing-failures F11/top-10 #9)

--------------------------------------------------------------------------
## 10. RECOMMENDATIONS FOR FURTHER INVESTIGATION
--------------------------------------------------------------------------
I1. Lease-leak forensics: instrument acquireSlotLease/releaseStepSlot with
    per-step counters and reproduce F-ORCH-28 under controlled concurrency
    (2/4/8 simultaneous sessions) to find which finalize path skips release.
I2. Prompt-router lane audit: dump classification for a frozen 50-prompt
    edit/create/git corpus; measure the git-lane false-positive rate on
    file-edit verbs; the GEPA tuning pipeline (tools/gepa-tune) is the
    existing instrument for classifier prompt changes.
I3. Watchdog interaction matrix: enumerate every parked/waiting state
    (quota, throttle, gate, approval, seal, planning) x watchdog behavior;
    the quota-config audit shows parking cannot be disabled by config -
    decide whether a rig/test mode should exist.
I4. GUI live pass: the web build was fixed in the clean worktree only
    (one-line rename, NOT committed); a live GUI session against the rig
    (HTTPS 57734, pairing gate) should verify the WS chain end-to-end and
    the plans REST approve/reject behavior. Browser tooling blocked
    loopback URLs this session; use a manual browser or CDP attach.
I5. Memory scoping vs recall: p2 showed cross-session bleed of project
    facts; verify EffectiveMemoryScope narrowing on the recall lane
    (agent-groups ceiling) - the digest may bypass the scope filter.
I6. History durability: audit every persistExchange caller for the FK
    failure class; add a roundtrip pin (conversation-only submit -> resume
    -> messages present).
I7. The 58 uncommitted sibling edits in the primary tree touch TUI chat/
    sessions/models and context_firewall - reconcile this report's TUI
    findings against that WIP before fixing (some may already be addressed
    there; the clean-HEAD audit cannot see them).
I8. Bench suite hardening: replace the vacuous checker, add llm_judge
    rubrics for reply quality (clutter, scaffolding, empty-reply), and add
    a terminal-delivery assertion (every submitted turn must produce a
    non-parked terminal within deadline) as a first-class checker - that
    single assertion would have caught F-ORCH-26/28 automatically.

--------------------------------------------------------------------------
## 11. METHOD + LIMITATIONS
--------------------------------------------------------------------------
- Code claims: read-only audits on clean HEAD worktree by 3 parallel
  subagent trees (11 agents total) + orchestrator spot re-verification of
  load-bearing lines. The primary tree's sibling WIP was excluded.
- Live claims: rig daemon (local 8B + prompt-router), agent-tui-driven TUI,
  python RPC probes with full bus event capture, meept-bench suite.
- The rig ran probes CONCURRENTLY for part of the session; run-1 bench
  timeouts are load-contaminated and reported as such; run-2 is clean.
- GUI was audited statically + build-verified; no live GUI interaction
  (browser tool blocks loopback; web build broken at HEAD until the
  one-line fix). GUI findings are CODE/BUILD class, not LIVE.
- Quota behavior: budgets disabled as requested; the quota-config audit
  documents that provider-429 parking CANNOT be disabled by config - the
  local-only rig avoids it by construction (no cloud providers).
- One worktree edit was made: platform_open_web.dart rename to unblock the
  GUI build (backed up at /tmp/usability-rig/platform_open_web.dart.orig).
  No repo commits were made; the usability-probe.json suite is a new
  untracked file in meept-bench.
