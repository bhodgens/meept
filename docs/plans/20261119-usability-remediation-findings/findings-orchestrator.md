# Usability rig - live findings log (orchestrator)

Rig: /tmp/usability-rig, daemon HEAD 1cee89d7 (clean worktree /tmp/meept-usability-head),
bench HEAD 5ac2e509. Quota-free: llm.budget.enabled=false, quota_retry.enabled=false,
local-only providers (8B general + prompt-router classifier, OS-assigned ports).
Config: plans.plan_compiler_enabled=true (deliberate probe), kanban sidecar off,
http+ws on free port, security.allowed_paths rig-only.

## F-ORCH-1 (CRITICAL, live-verified): plan-compiler pipeline parks every
## "plan"-mode turn forever with no user-visible path forward.
- Repro: TUI chat "Create a file named tui_test.txt containing the word banana.
  Use file_write with direct:true." -> dispatcher intent=code conf 0.8833 ->
  mode=plan -> tier=trivial -> "Plan draft saved" -> "brainstorm draft seeded"
  (daemon.log 15:56:59). Task stays state=planning indefinitely.
- TUI shows "## starting task / mode: planned / plan: <task-id> | 0 subtasks /
  subtasks: (empty) / you will receive updates as subtasks complete." then
  "thinking 1m29s..." forever. No approval prompt, no draft content, no key
  hint. Screenshot: probes/tui-create-file-2.txt.
- plan.list / plan.list_by_session return ZERO plans (drafts live in
  task.Metadata[plan_draft], not plans.db - plans.db has 0 rows). TUI plans
  view shows (0/0) empty. Screenshot: probes/tui-plans-view.txt.
- The draft markdown is the RAW PLANNER PROMPT SCAFFOLD ("You are the planner
  agent in a brainstorm session... <no value>... Turn protocol"), not a plan
  (plan.draft RPC output captured). Sealing it raw fails deterministic compile.
- Only recovery surface: CLI `meept plans show-draft/edit-draft/seal <task-id>`
  (cmd/meept/plan_seal.go). NO TUI or GUI call sites for plan.draft/plan.seal
  (grep zero hits in internal/tui and ui/flutter_ui/lib).
- Chat follow-up does NOT refine the draft (routes as new task) - confirmed by
  p1 T2 "Now change hello.txt..." becoming a NEW committer task.
- Default config ships plan_compiler_enabled=false (absent from live
  ~/.meept/meept.json5 and repo template) so production is dark today; the
  moment it flips on, every plan-mode turn dead-ends in the chat client.
- Fix sketch: TUI/GUI draft card + seal/reject actions keyed off task_id from
  the ack; or auto-run the brainstorm LLM loop to fill the scaffold; or a
  terminal event status=parked with handler_case=awaiting_seal + actionable
  reply text naming the CLI command.

## F-ORCH-2 (MAJOR, live-verified): file-modification prompts misroute to
## committer (git intent) deterministically.
- "Now change hello.txt so it says 'hello meept' instead." -> intent=git
  conf=0.856 agent=committer, EVERY run (4/4 across sessions, classifier
  method=llm i.e. prompt-router). Dispatch trace captured via
  session.dispatch_trace. A file edit is not a git operation; committer then
  plans a git-shaped task that fails or parks.
- Positive control: "Create a file named tui_test.txt..." -> intent=code
  conf 0.8833 agent=coder (correct lane, wrong mode per F-ORCH-1).

## F-ORCH-3 (MAJOR, live-verified): session.dispatch_trace keyed by
## conversation_id only; primary session_id returns count=0.
- session.dispatch_trace{session_id: "session-52cc..."} -> count 0;
  {session_id: "conv-a2fa..."} -> count 3 with full evidence. The RPC param
  is named session_id; a client holding the primary id (what session.create
  returns as `id`) gets silence. internal/rpc/dispatch_trace.go:20-40 queries
  QueryDispatchLogBySession(p.SessionID) with no GetByConversationID fallback.
- Also: entries carry input_summary="" (empty) - the trace cannot show WHAT
  was routed without joining the task store.

## F-ORCH-4 (MAJOR, live-verified): TUI task-detail step row renders raw
## planner scaffolding.
- Task detail modal step 0 shows "## Session execution context - Open tracked
  tas..." (probes/tui-task-detail.txt). task.list_extended step.description
  carries the full "## Session context ... ## Your Task" injection. The
  chat-lane strip (StripPromptScaffolding) is NOT applied on the task-detail
  render path.

## F-ORCH-5 (MAJOR, live-verified): simple chat question takes the full
## task machinery; reply is a job-log dump, double-rendered.
- "What is 2+2? Answer briefly." -> intent=quickplan conf 0.8575 ->
  orchestrator task -> planner loop -> reply guard fires TWICE on planner
  JSON (tool_result_json) -> repair retry -> step job -> review ->
  task.completed relay. TUI renders BOTH a "task completed" card AND a
  "## task completed: what is 2+2? / ### steps: / result: / • job
  job-2026... completed by agent orchestrator: ... / summary: ... /
  completed in 5s" block (probes/tui-2plus2.json). The answer "4" is buried
  in job-ID prose. 5s wall for a one-token answer; ~15k tokens burned.
- Daemon log: "reply guard replaced a machine-shaped reply" x2 on planner
  output - the guard is fighting the planner's own JSON contract.

## F-ORCH-6 (MINOR, live-verified): orchestrator ERROR spam "failed to find
## step for job ...: step not found" after EVERY task completion (2 jobs per
## task: memory-capture/ambient side jobs with empty step_id/task_id).
## daemon.log 15:52:47/48, 15:53:28/29. Log noise; also signals the
## completion handler is invoked for non-step jobs it cannot map.

## F-ORCH-7 (MAJOR, live-verified): TUI resumes the OLD session after daemon
## restart with stale failed-turn history and mixed clock formats.
- New TUI launch loaded session-52cc (pre-restart) showing 22:46 UTC-stamped
  turns next to 15:53 local-stamped turns; failed "Now change hello." tasks
  from the previous daemon lifetime render as current sidebar state
  (◐ pending / ✗ fail) with no "stale/previous run" marker.
- Timestamps: store stamps are UTC (22:53Z) while TUI header/turn stamps are
  local (15:53) - two clocks in one view.

## F-ORCH-8 (MINOR, live-verified): tasks view steps column shows 0/1 for a
## completed 1/1 task (probes/tui-tasks-view.txt: "✓ done ... 0/1 ... 1/1").
## Two different counters rendered without labels.

## F-ORCH-9 (MAJOR, rig-setup, code-confirmed): security.allowed_paths
## literal-match vs symlink-resolved tool paths (/tmp vs /private/tmp).
- First rig boot: every file_write denied "access denied:
  /private/tmp/usability-rig/project/hello.txt" with allowed_paths
  ["/tmp/usability-rig/**"]. pkg/security/permissions.go:215-240
  expandPathAll stores raw+resolved forms of the PATTERN, but
  filepath.EvalSymlinks("/tmp/usability-rig/**") fails (glob suffix), so the
  resolved /private/tmp form is never added; the tool path resolves to
  /private/tmp/... and matchGlobPattern prefix-compares against /tmp/... .
  The scratch-rig.sh canonicalization comment documents the same fence.
  User-facing error is the bare "access denied: <path>" with NO hint about
  allowlists or symlinks; the model then tells the user to "try root
  privileges" (observed in TUI) - actively wrong advice.
- Fix sketch: resolve the pattern's DIRECTORY prefix before glob matching;
  error text should name the matched allowlist and suggest the resolved form.

## F-ORCH-10 (MINOR, live-verified): fresh rig without config/prompts copies
## logs 176 "Prompt component not found" WARNs per turn batch (base.constitution,
## base.restrictions, capabilities.memory/tasks). resolveBundledPath
## (internal/daemon/bundled_path.go:30) falls back to CWD-relative when the
## binary dir lacks config/; a binary-only install silently loses the prompt
## tiers. After copying prompts into MEEPT_HOME: 0 warnings.

## F-ORCH-11 (live-verified): session.create with top-level "cwd" param is
## silently ignored -> session UNBOUND (Warn only in daemon log); the real
## param is detection_context.cwd (internal/services/session_service.go:58-59).
## CLI/TUI/GUI all send detection_context correctly, but the RPC accepts and
## drops unknown keys with no error - a third-party client gets a broken
## session with no signal. Probe p1 first run hit this.

## F-ORCH-12 (CRITICAL, live-verified + code-confirmed): task.reject
## APPROVES the plan. Rejection is a destructive no-op that executes the work.
- internal/rpc/task_approval.go:55-73: handleReject parses a Reason, then
  calls h.ApproveFunc(ctx, req.TaskID) - the SAME function as handleApprove -
  and returns {"status":"rejected"}. Reason is discarded. No RejectFunc seam
  exists on the handler.
- Live proof: task-20261119230106 ("What is 2+2?", awaiting_approval, 10
  steps) -> rpc task.reject{reason:"usability probe"} -> response
  status=rejected -> task.list_extended 2s later: state=executing. The
  rejected plan RUNS.
- CLI `meept task reject` (cmd/meept/task.go:80) rides the same handler.
  TUI RPCClient.RejectTask (internal/tui/rpc.go:927) exists but has ZERO
  TUI call sites; CLI is the only wired surface and it executes the plan.
- Fix sketch: add RejectFunc seam (StrategicPlanner needs a RejectPlan:
  state=failed/cancelled + task.rejected event + user-visible terminal),
  wire daemon, pass reason through, pin with a test that asserts state
  after reject != executing.

## F-ORCH-13 (CRITICAL, live-verified): task-level approval gate is
## invisible in every interactive surface.
- "What is 2+2?" quickplan generated 10 steps (planner over-decomposition)
  >= approvalStepThreshold -> task.StateAwaitingApproval. StrategicPlanner
  publishes task.pending_approval (strategic.go:2230) - bus-topology.md:230
  lists it as publisher-with-NO-subscriber (orphan).
- TUI chat handles only plan.submitting/completed/confirmed/rejected
  (app.go:1899) - the plans.db family. Task-gate approvals never render a
  card. TUI plans view reads plan.list_by_session (models/plans.go:189) ->
  0 rows because task-gate plans live in task.Metadata[pending_steps], not
  plans.db. plans view footer advertises "a: approve" with nothing to select.
- turn.terminal for the turn fired at ack time (handler_case=async_dispatch,
  status=parked family) - the chat client considers the turn resolved while
  the task waits forever. No timeout, no reminder, no badge.
- Only working surface: CLI `meept task approve <task-id>` -> task.approve
  RPC (task_approval.go). GUI: zero task.approve/task.reject call sites in
  ui/flutter_ui/lib.
- Combined with F-ORCH-1: BOTH planning pipelines (compiler draft-seal and
  legacy task-gate) dead-end in chat clients today.

## F-ORCH-14 (MAJOR, live-verified): planner over-decomposes trivial
## questions; approval threshold then traps them.
- "What is 2+2? Answer briefly." -> task.planned total_steps=10
  ready_steps=2 (bus event captured). A 1-token answer became a 10-step
  plan requiring human approval. Earlier run (pre-prompt-copy) produced
  1 step and auto-approved - prompt tiers materially change plan size,
  and the approval gate has no triviality escape.

## F-ORCH-15 (CRITICAL, live-verified): direct file-write turn completes
## with EMPTY reply; work done but user gets nothing.
- p1 T1 "Create a file named hello.txt ... Tell me the full path" ->
  intent=tooluse agent=coder handler=route_to_agent. file_write succeeded
  (hello.txt on disk, 'hello world'). turn.terminal status=completed
  reply="" (chat.response len=0 too). Daemon: "Agent completed
  action=close agent=coder has_report=false". The loop produced no final
  text after the tool call and the handler shipped the empty string.
  probes/p1-naive-continuity.json turn 1.
- TUI rendered a "task completed" card for it (Steps 1/1) but no answer
  text; the user's explicit question ("tell me the full path") is unanswered.
- Fix sketch: route_to_agent completion with empty reply must synthesize
  from the tool ledger (file paths written) or re-prompt once for a
  user-facing summary; never ship reply="" on a completed turn.

## F-ORCH-16 (MAJOR, live-verified): recall answers read a stale digest
## snapshot; contradict the task state they just printed.
- p1 T3 "Did the change get made?" -> platform_recall_arbitration ->
  RecallAnswer (recall_answer.go:41). Reply: 'Prior task: "Now change
  hello." - status: failed ... The work above was still in progress; its
  stored result may not reflect the final state.' The task was TERMINAL
  (failed) - the "still in progress" sentence is the non-terminal branch
  text (recall_answer.go:86-90) fired on a state string the switch does
  not recognize ("failed" falls into default). Two defects: (a) default
  branch treats every non-completed state including FAILED as in-progress;
  (b) digest.LastTaskState carries a state the switch mishandles.
- p1 T4 "What files did you make for me in this session?" -> recall
  answered with ONLY the last task (capital-of-France, which belonged to
  the TUI's interleaved turn in the same conversation). The session's
  actual file-creation task (hello.txt) is absent from the answer. The
  digest is last-task-only (buildSessionContextDigestExcluding), so
  plural artifact questions get a single unrelated task. User-visible
  wrong answer with full confidence.
- Cross-client note: TUI and probe shared the conversation (TUI resumes
  most-recent session); the recall lane cannot distinguish which client's
  work the question refers to.

## F-ORCH-17 (MAJOR, live-verified): context firewall truncates the
## system prompt on ordinary turns.
- daemon.log 16:07:04: "context firewall: truncated oversized system
  message to fit model window" x2 on the FIRST turn of a fresh session
  (messages=3, 6364 tokens total, ctx 65536). The assembled system prompt
  exceeds the firewall's own budget ratios on a trivial turn; truncation
  is silent to the user and drops prompt components (constitution/
  capabilities) without disclosure.

## F-ORCH-18 (MAJOR, build-verified): GUI web build broken at HEAD -
## conditional-export symbol mismatch.
- `flutter build web --release` at HEAD 1cee89d7 fails:
  "Error: Method not found: 'openExternal'."
- platform_open_helpers.dart exports platform_open_web.dart on web, which
  defines openExternalWeb (platform_open_web.dart:9), while the caller
  platform_service.dart:84 invokes openhelpers.openExternal. Native half
  defines openExternal (platform_open_native.dart:15). The web half never
  renamed to match the export contract -> every web build fails.
- Consequence: the GUI cannot be built for web at HEAD; live GUI testing
  blocked. Fix: rename openExternalWeb -> openExternal in the web half
  (one line) or add an openExternal wrapper.

## F-ORCH-19 (MAJOR, code-confirmed): task approval gate threshold is
## hardcoded; plans.approval.require_approval does not control it.
- StrategicPlannerConfig.ApprovalStepThreshold defaults to 5 when <=0
  (strategic.go:323-324); the daemon construction (components.go:4606-4617)
  NEVER sets it and NO config key maps to it. Setting 0 cannot disable the
  gate (clamps back to 5). plans.approval.require_approval gates only the
  plans.db PlanManager path (internal/plan/manager.go:577), not the
  task-gate. Rig workaround: orchestrator.max_plan_steps=4 keeps plans
  under the threshold. Users cannot turn off the invisible approval gate
  by config; they can only shrink plans.
- Also live: "planner emitted unknown tool_hint hint=read" x2
  (daemon.log 16:10:43) - the planner invents hints outside the registry;
  validation drops them silently (Warn only).

## F-ORCH-20 (CRITICAL, live-verified): completed task fabricated its
## effect - committer ran ZERO file tools, review approved, file unchanged.
- p6 T2 explicit "Edit the file hello.txt: replace 'hello world' with
  'hello meept'. Use file_edit or file_write with direct:true. This is a
  file edit, not a git operation." STILL routed intent=git conf 0.8806
  agent=committer (prompt-router). The explicit disambiguation did not
  change the lane.
- Task ran 3 steps (replace / verify / confirm), ALL approved, status
  completed, 24s. Tool ledger for the whole task: ZERO file_edit/file_write
  calls (daemon.log Executing tool lines 16:14:17-16:14:41 show none for
  committer). hello.txt on disk still 'hello world'.
- Step 3 result (stored, relayed to user): "The user has requested me to
  confirm task completion... I don't have any context about what specific
  task was requested... no prior conversation context in this session" -
  the step loop had NO context, narrated completion, and review approved
  it ("No explicit feedback provided").
- This is the claim-without-evidence class the reviewer stack is supposed
  to force review on; it passed. User receives "task completed" for work
  that never happened. probes/p6-positive-control.json turn 2.
- Fix sketch: committer lane must not own file-edit intents (classifier
  lane fix); ReviewStep must force evidence review when a step's
  tool_hint is file-shaped and the ledger shows no file tool; step loops
  must receive the session context digest (step 3 had none).

## F-ORCH-21 (MAJOR, live-verified): task_completed_relay ships raw
## claims/evidence JSON envelope in the reply.
- p6 T1 reply contains 'job job-... completed by agent coder: { "status":
  "completed", "accomplished": [...], "not_done": [], "issues": [],
  "observations": [...' - the machine envelope renders raw inside the
  user reply despite StripClaimsEvidenceOrProse being the documented
  contract (internal/agent/AGENTS.md). The relay path
  (formatTaskCompletedMessage/bestSummaryResult) did not strip this
  envelope shape. probes/p6-positive-control.json turn 1 reply.

## F-ORCH-22 (MAJOR, live-verified): chat.submit accepts unknown
## session/conversation ids and fabricates new conversations.
- p3 F3: session_id=session-doesnotexist999 -> accepted:true, NEW
  conv-9ef63ef6 created. F4: conversation_id=conv-doesnotexist999 ->
  accepted:true. No validation error; work proceeds in a phantom session
  the client never asked for. A typo'd id silently forks context.
- F1/F2 empty/whitespace: accepted:false note='message is required' -
  clean ack-level rejection (good), but NO turn.terminal fires, so a
  client awaiting a terminal for the ack it never got must handle the
  accepted:false branch itself.
- F7/F8 steer/followup to closed/unknown queue: RPC error 'queue not
  found' (-32603) - honest error, no receipt event.

## F-ORCH-23 (CRITICAL, live-verified): accepted steer silently ignored;
## receipt says queued, work proceeds unchanged, no event trail.
- p5: 5-step file primary; chat.steer at +8s on the raw conv id ->
  {"queue":"steer","status":"queued"} (accepted). Steer said: skip
  step4/step5, create final.txt instead. Outcome: ALL 5 step files
  created, final.txt absent, terminal reply lists all five as success.
  queue_status after: is_active=false steering_depth=0. ZERO steer-related
  bus events in 237 captured events. probes/p5-steering.json.
- The steer was accepted into a queue the loop never drained (or drained
  after the decision point) and the client got a success receipt for a
  correction that never happened. No receipt event, no annotation on the
  terminal, no way for the user to know the correction was dropped.
- Matches skill-documented class (steer lands post-hoc / queue closed)
  but here the queue ACCEPTED it while active - a stronger failure than
  the documented 404 path.

## F-ORCH-24 (CRITICAL, live-verified): session A cannot recall its own
## codeword; recall task decomposes into task_get plumbing and fails.
- p2 A1 planted codeword ZEBRA-4417 + alpha_notes.txt (committer,
  completed, reply carries '[unverified: no tools were executed this
  turn]' - the file claim is unverified; alpha_notes.txt NOT on disk).
- p2 A3 "What was the codeword I told you earlier in this session?" ->
  quickplan task, 2 steps, completed, reply: "I couldn't retrieve the
  task details because the required task ID was missing... task_get
  requires a task_id". The codeword is NEVER answered. The session's
  own planted fact is unreachable through the recall lane; the memory
  store had it (ambient extraction wrote claims) but the task machinery
  tried to task_get instead of memory_search.
- p2 B (different session, same project): "What is the secret codeword?"
  -> committer git task, parked ack, then 3 '## review: approved' chat
  messages quoting alpha_notes.txt content and the unverified label -
  cross-session context bleed via the shared project/memory, and the
  turn NEVER reached a non-parked terminal within 300s (task stuck
  executing after last review at 16:16:45; probe deadline hit).
  probes/p2-isolation.json.
- User-visible: A's memory question fails with plumbing text; B sees A's
  private notes; B's turn hangs past 5 min with only a parked ack.

## F-ORCH-25 (MAJOR, live-verified): '## review: approved' messages push
## internal review feedback into the user chat stream.
- p2 B received 3 chat.response frames titled '## review: approved'
  carrying reviewer feedback prose (including '[unverified: no tools
  were executed this turn]' and task_get failure narration). Review
  feedback is internal scaffolding; it renders as assistant chat
  messages. The user sees the review machinery arguing with itself.

## F-ORCH-26 (CRITICAL, live-verified): bus.poll 100-event drop-oldest
## buffer loses turn.terminal under event floods; client waits forever.
- p4 captured 9953 events with dropped=9853 (bus.poll 'dropped' counter).
  The subscription buffer keeps only the last 100 records
  (internal/rpc/proxy.go MaxEvents=100, drop-oldest trim). agent.progress
  streaming frames (one per token chunk, per step, per task) flood the
  buffer; a turn.terminal emitted during a flood is trimmed before the
  next poll. p4 D1/D2 terminals were lost; the probe waited its full
  300s/420s deadlines while the daemon had already finalized the tasks.
- The TUI uses the SAME bus.subscribe/bus.poll mechanism (sub topics
  logged at every TUI connect). Under real multi-task load the TUI can
  miss the terminal of the turn it is displaying - the pending line
  never resolves. The 'dropped' counter is exposed but no client-side
  recovery (re-fetch task state on drop) exists in the TUI event path.
- Fix sketch: per-topic ring buffers or a critical-topic lane that never
  trims turn.terminal/task.completed/task.failed; clients must treat
  dropped>0 as a signal to re-query task/turn state.

## F-ORCH-27 (CRITICAL, live-verified): accepted steer executed as a
## NEW concurrent task in the same conversation; original turn unchanged.
- p4 D3 "I want to add a greetings module..." arrived while p5's steer
  correction was still queued in the shared conversation. The daemon
  executed the STEER text as a fresh route_to_agent turn: reply "I've
  created final.txt ... skipped step4.txt and step5.txt as instructed" -
  but step4.txt/step5.txt EXIST (p5's original task created them) and
  final.txt was created by this NEW turn. Two turns' work interleaved in
  one conversation; the reply claims a state that contradicts disk.
- p4 D4 refinement "make it five languages" -> NEW task (not draft
  refinement, confirms A9) -> failed: "max ralph loop iterations reached
  without sufficient evidence", progress "1/1 steps completed, 3 failed"
  (contradictory counters rendered to the user).
- plan.list returned 0 plans at every checkpoint (D2/D3/D4) - the
  greetings plan lived only in task metadata; the plans view is empty
  while plan work runs. probes/p4-plan-delineation.json.

## F-ORCH-28 (MAJOR, live-verified): semaphore-blocked ready steps are
## never rescheduled without a new event; task hangs in executing.
- Bench u-context-two-step + u-steer-midrun: tasks stuck state=executing,
  step 0 state=ready job_id=NULL, queue has 0 pending/0 claimed jobs,
  daemon logs "Scheduling ready steps count=1" then NO "ASSIGN step
  scheduled" and NO error. ScheduleReadySteps (tactical.go:513) skips
  semaphore-blocked steps at Debug level ("will be retried on the next
  scheduling cycle") but there IS no periodic scheduling cycle -
  reschedule fires only on step-completion/approval events
  (tactical.go:2172,2456,3616; orchestrator.go:400,632). A blocked wave
  with no in-flight steps waits forever; the bench timed out at 426s.
  Global semaphore cap 10, per-agent 3 (tactical.go:433-439); leaked
  leases from the concurrent probe load exhausted coder slots.
- Fix sketch: a bounded retry ticker for ready-but-unscheduled steps,
  or release-on-finalize audit; surface blocked steps in task.progress.

## F-ORCH-29 (MAJOR, live-verified): chat history persistence fails
## with FOREIGN KEY constraint for conversation-keyed sessions.
- daemon.log: 9x "failed to persist chat exchange ... FOREIGN KEY
  constraint failed (787)" for conv-* ids (bench sessions, GUI-test conv,
  unknown-id probes). session_messages insertion keys on a session row
  that does not exist for conversation-only sessions (chat.submit with
  conversation_id and no session_id never creates a session row).
  History for those conversations is silently lost - resume shows
  nothing. Warn-only; the turn proceeds.

## TUI navigation inventory (live, agent-tui):
- Ctrl+X palette: chat/sessions/tasks/queue/memory/plans/employees/activity/
  kanban/sidebar/projects/memory-review/users/intake/new-session/edit-desc/find.
- Tasks view: tabs tasks/jobs/lineage, filter, enter=detail modal (id, state,
  agent, steps, memory context, linked sessions). Detail modal closes on q
  but NOT on Escape (Escape ignored while modal open - probes sequence).
- No link from a chat reply to its task detail: reply card shows no task_id
  in the 2+2 case (the "## starting task" ack DOES show id for plan-mode).
  User must open tasks view and match by title/time.
- Sidebar: status/agent activity/tasks/plan/recent memory/metrics. "recent
  memory" renders raw "<<<MEMORY_CONT..." markers (scaffolding leak).
- No steering/followup UI found in palette; chat.steer exists only as RPC/CLI.
