# Meept usability audit: behind-the-scenes observability navigation and planning/delineation UX

Scope: READ-ONLY audit of clean HEAD checkout /tmp/meept-usability-head. All file:line
references are relative to that root unless absolute. Daemon-side claims verified
directly; TUI and GUI claims verified by dedicated leaf audits plus spot re-checks of
the load-bearing lines. Plain ASCII; non-ASCII glyphs in quoted source are transcribed
(e.g. the middle dot in "document proposal" strings).

Legend: TUI = internal/tui (Go, RPC over unix socket). GUI = ui/flutter_ui/lib
(Flutter, REST + WS). "Link from chat" = can a user go from a chat reply/turn to the
behind-the-scenes detail without knowing IDs.

---

## 1. OBSERVABILITY INVENTORY

Backend surfaces that expose what happened behind a chat turn, and their client coverage.

### 1.1 Task detail + steps
- Backend: task.get (internal/rpc/proxy.go:207), task.list (proxy.go:208),
  task.list_extended (proxy.go:209), task.steps (proxy.go:215). HTTP twins:
  GET /api/v1/tasks/{id} (internal/comm/http/server.go:1718),
  GET /api/v1/tasks/{id}/steps (server.go:1722).
  task.list_extended DOES hydrate linked_sessions (internal/task/store.go:480-489,
  comment cites the scopes-2 audit: List previously reported linked_sessions: null).
- TUI: EXPOSED. Palette ctrl+x then 't' (internal/tui/config.go:174, modal.go:206),
  slash /tasks and /task (internal/tui/command_handler.go:200-206, executeTasks at
  :1231 calling ListTasksExtended, internal/tui/rpc.go:862-864). Steps ride the list
  payload (internal/tui/types/types.go:492). Detail modal on enter
  (internal/tui/models/tasks.go:452-461, renderTaskDetailModal :1144, per-step render
  :1238-1303). Gap: GetTask/ListTaskSteps client methods exist
  (internal/tui/connection.go:40,43; rpc.go:887) with ZERO production callers; detail
  freshness is bounded by the last list fetch.
- GUI: PARTIAL/BROKEN. GET /api/v1/tasks list only (features/tasks/task_provider.dart:42
  -> services/sdk_client.dart:861,865); sessionId filter param supported by the client
  but never passed. Detail pane renders title/status/description only
  (features/tasks/tasks_detail.dart:31-35,104-125); Task.steps and TaskStep models are
  parsed but dead (models/api_models.dart:669,689-702). getTask REST method exists but
  uncalled (sdk_client.dart:878). Wiring bug: the detail pane is UNREACHABLE -
  activeTaskProvider (features/tasks/tasks_tab.dart:10) is only watched (:18,
  tasks_detail.dart:15), never written; TasksList is constructed without
  onTaskSelected (tasks_tab.dart:22; callback optional at tasks_list.dart:14, tap :170).
  linked_sessions: 0 hits in lib.
- Link from chat: MISSING both clients (see section 2).

### 1.2 Tool traces (session_tool_calls)
- Backend store: table created at internal/session/store_sqlite.go:263, trace_id/span_id
  correlation columns added at :290-291. Writer: SaveMessagesWithToolCalls
  (store_sqlite.go:1121-1128, INSERT at :1156). Readers: GetToolCalls /
  GetToolCallsForMessages (internal/session/store.go:127-128, sqlite impl :2615),
  used for resume rebuild (internal/agent/loop.go:10388,10456) and fork/branch copy
  (internal/session/tree.go:103-113, branch.go:229, store_sqlite.go:2361-2372).
- Writer liveness (historical purged-writer bug): FIXED at HEAD. persistExchange
  extracts this turn's tool calls via turnToolCalls (internal/agent/handler.go:1684)
  and calls SaveMessagesWithToolCalls at handler.go:1586-1590 ("wiring-gap-remediation
  leaf 05" comment at :1580-1583). persistExchange call sites: handler.go:1412 (sync
  chat), :2921 and :3407 (async turn completion paths). Interface declared at
  handler.go:53; store capability at internal/session/store.go:120-126.
- BUT no query surface exposes the rows: session.messages.get returns plain messages
  only - handleGetMessages (internal/session/session.go:2124-2148) calls
  store.GetMessages and returns {messages,total}; session.Message has NO ToolCalls
  field (internal/session/store.go:12-27; only ToolCallID string for tool-role rows).
  No RPC or HTTP route returns GetToolCallsForMessages results to a client.
- TUI: MISSING. No session_tool_calls reference in internal/tui; SessionMessage decode
  has no tool-call fields (internal/tui/types/types.go:301-307). Live tool visibility
  is only the animated sidebar viz fed by agent.event.tool_execution_start/update/end
  (internal/tui/sidebar.go:598-607, viz/dispatch.go:64-80); sidebar "activity" fakes
  ToolCalls from worker.list CurrentTool (sidebar.go:357-385).
- GUI: MISSING. session_tool_calls: 0 hits in lib; ChatMessage.toolCalls
  (models/api_models.dart:408, parsed :437) never read by any widget.
- Link from chat: MISSING. Data is written and correlated (trace_id/span_id) but
  read-only-to-the-engine; a user cannot see any historical tool trace anywhere.

### 1.3 Dispatch trace (intent_type / classifier_method / handler_case / agent)
- Backend: dispatch_log table with intent_type, agent_id, confidence,
  classifier_method, handler_case, task_id, error columns
  (internal/metrics/store.go:315-328). Written via Dispatcher.RecordDispatch
  (internal/agent/dispatcher.go:4678-4780 -> metrics.Store.RecordDispatch,
  internal/metrics/store.go:1092-1093). Query RPC: session.dispatch_trace
  (internal/rpc/dispatch_trace.go:14-42, requires session_id, default limit 50).
- HTTP: NO dispatch_trace route (grep of internal/comm/http: zero hits). GUI cannot
  reach it even via the bus/call bridge without knowing the method name.
- TUI: MISSING. Zero callers of session.dispatch_trace in internal/tui.
- GUI: MISSING. dispatch_trace/dispatchTrace/classifier_method: 0 hits in lib.
- Link from chat: MISSING. The evidence IS on the turn.terminal payload
  (IntentType/HandlerCase/ClassifiedBy/AgentID, internal/agent/handler.go:289-295)
  but both clients drop it (section 2). Users must know session_id and query the
  RPC manually (CLI/socket) to see routing decisions.

### 1.4 Activity aggregator (activity.*)
- Backend: read-only cross-store aggregator (task_steps + session_messages + auditlog
  + employee findings -> ActivityRecord with kind/at/agent_id/session_id/task_id/
  step_id/plan_id/state/severity/summary/detail, internal/activity/types.go:10-23).
  RPCs registered in internal/daemon/activity_rpc.go: activity.query (:51),
  activity.recent (:101, ring live-tail), activity.overview (:127, per-agent rollup),
  activity.findings (:221), agents.review (:287). HTTP twins: GET /api/v1/activity,
  /activity/recent, /activity/overview, /activity/findings
  (internal/comm/http/server.go:1727-1730). Live ring fed by bus bridge subscribing
  queue.job.* (6 exact topics), task.completed/failed, agent.progress
  (internal/activity/busbridge.go:22-60,80). Ring capacity fixed at wiring
  (internal/daemon/activity_wiring.go:80).
- TUI: EXPOSED. Activity view palette 'a' (internal/tui/config.go:185, modal.go:211);
  activity.query (internal/tui/models/activity.go:266 -> rpc.go:478-479),
  activity.recent live-tail 't' (activity.go:279 -> rpc.go:495-497), findings panel
  'p' (activity.go:287-315 -> rpc.go:512-513); sort chrono/task/plan via tab
  (activity.go:444-481). activity.overview registered but never called by TUI.
  Also raw last-10 bus feed in sidebar (sidebar.go:676-695, events.go:283).
- GUI: EXPOSED (REST). /activity route (core/router.dart:95-101), palette 'a'
  (widgets/command_palette.dart:81-86). List (features/activity/activity_list.dart:49
  -> sdk_client.dart:1993-2014), recent 2s poll (activity_recent_panel.dart:30,38),
  findings (activity_findings_panel.dart:28), overview/drift
  (activity_drift_panel.dart:27). Filter params since/task_id/plan_id/agent_id/kinds
  supported by the client (sdk_client.dart:1994-2013) but only sort is passed.
  Record fields agentId/sessionId/taskId/stepId/planId/state/detail parsed
  (features/activity/activity_models.dart:35-43) but NEVER rendered; tiles show only
  summary/kind/at (activity_list.dart:244,257,267). No tap-through to task/plan.
- Link from chat: MISSING. The aggregator already resolves task_id/plan_id per record,
  so correlation exists server-side, but neither client renders the IDs or offers
  navigation, and no chat reply carries a pointer into the feed.

### 1.5 Queue status
- Backend: queue.stats/list/get/retry/dead_letter etc (internal/rpc/proxy.go:218-228);
  chat.queue_status (internal/rpc/queue.go:28); HTTP GET /api/v1/queue/jobs, /stats,
  /queue/status/{id} (internal/comm/http/server.go:1703,1709,1713).
- TUI: EXPOSED. Queue view palette 'q' (config.go:175), queue.stats+queue.list
  (models/queue.go:129-141, rpc.go:957-972), retry 'R' and filters
  (queue.go:170-230). Chat steer/follow-up badges from agent.queue.status
  (app.go:1857-1873 -> models/chat.go:3511, render :3590-3607). Sidebar queue-depth
  sparkline via 2s MetricsCollector poll (events.go:412-416).
- GUI: EXPOSED read-only. /queue HomeTab (core/router.dart:119-125,
  features/home/tab_content.dart:43-44); jobs+stats (features/queue/queue_tab.dart:166,172
  -> sdk_client.dart:949,965); detail shows taskId as a RAW STRING with no navigation
  (features/queue/queue_detail.dart:60). No cancel/retry/requeue UI.
- Link from chat: PARTIAL. TUI shows steer/follow-up counts inline; GUI queue detail
  shows task_id but no jump; neither links a specific reply to its queue job.

### 1.6 Plan phases/steps
- Backend: plan.list/get/list_by_session/get_phases (internal/rpc/plan.go:48-56);
  plan.list_by_session merges pending_approval evolver-sink plans that have no session
  link (plan.go:306-317, comment cites the live 2026-10-22 empty-tab incident).
  HTTP GET /api/v1/plans, /{id}, /{id}/phases, /{id}/handoffs,
  /sessions/{id}/plans (internal/comm/http/server.go:1899-1908).
- TUI: EXPOSED. Plans view palette 'p' (config.go:177), plan.list_by_session
  (models/plans.go:189), phase bars in detail modal (plans.go:730-775); sidebar active
  plan panel polls plan.list every 2s (sidebar.go:443-476) but SKIPS zero-phase plans
  (sidebar.go:456-458).
- GUI: EXPOSED. /plans HomeTab (router.dart:81-87); session-scoped or global list
  (features/plans/plan_provider.dart:57-67 -> sdk_client.dart:745,1722); WS plans
  channel push refresh (plan_provider.dart:37 -> websocket_service.dart:726-729);
  phase cards with artifacts (plans_tab.dart:361-425). getPlan (sdk_client.dart:1742)
  uncalled.
- Link from chat: PARTIAL TUI (plan notification cards render in chat,
  models/chat.go:3129-3173, but offer only the broken '/plan approve <id>' hint, see
  3.2); MISSING GUI (no plan card in chat, no reply->plan navigation).

### 1.7 Audit chain
- Backend: hash-chain store internal/auditlog (chain.go, anchor.go, store.go), opened
  for the activity aggregator at internal/daemon/activity_wiring.go:61-63; records
  surface as ActivityRecord kind "audit_record" (internal/activity/types.go:11,29).
  Employee audit FINDINGS + chain verify RPCs: agents.audit.list / resolve / verify
  (internal/employee/handler.go:184-186).
- CLI: EXPOSED. 'meept agents audit --verify' calls agents.audit.verify and prints
  "audit chain BROKEN at seq" on failure (cmd/meept/agents_cmd.go:1679,1703-1723).
- TUI: PARTIAL. Findings sub-view key '3' in agents panel (agents_panel.go:1085-1093,
  agents.audit.list fetch :792, renderAudit :1698, resolve :1196). NO chain-verify
  surface (no audit.verify/VerifyChain callers in internal/tui).
- GUI: MISSING. No audit chain fields in any model; activity "audit_record" is only a
  kind label (features/activity/activity_models.dart:5); agents.review client method +
  model exist (sdk_client.dart:2053, activity_models.dart:216-236) but ZERO callers.
- Link from chat: MISSING.

### 1.8 Metrics
- Backend: GET /api/v1/metrics/live, /historical, /stream, /rate-limits
  (internal/comm/http/server.go:1656-1659); routing.recent / routing.by_model RPCs
  (internal/rpc/routing.go:19,37); dispatch_log lives in the same metrics store.
- TUI: PARTIAL. Sidebar sparklines from 2s polls of queue.stats/worker pool/worker.list
  (events.go:356-437, sidebar.go:1634-1651); /usage token totals
  (command_handler.go:630-655). llm.tokens.used handler exists (sidebar.go:572-573)
  but the topic is NEVER delivered (subscription gap, section 5.3).
- GUI: EXPOSED. /tools/metrics (router.dart:211-218), GET /metrics/live
  (features/metrics/metrics_provider.dart:138 -> sdk_client.dart:972) + WS metrics
  channel with HTTP fallback poll when disconnected (metrics_provider.dart:97-115,
  155,169-174); per-model/per-agent tables (metrics_panel.dart:333-393).
- Link from chat: MISSING (no per-turn token/model attribution rendered; model field
  on turn.terminal is dropped by both clients).

### 1.9 Other behind-the-turn surfaces
- session.get_child_tasks (internal/rpc/proxy.go:199): GUI calls it ONLY from the
  stop-confirmation flow (features/chat/chat_input.dart:1473); TUI sessions tree uses
  session.tree.get. No reply->child-task navigation.
- reasoning.get / budgets (internal/rpc/reasoning.go:69-77), routing.recent: no TUI/GUI
  observability screen found.
- delegate.status (internal/rpc/delegate_children.go:80): operator RPC; no client screen.
- intake.status (internal/rpc/intake_status.go:67): GUI has intake feature; not a
  per-turn trace surface.
- bus.stats (internal/rpc/server.go:720) + HTTP GET /api/v1/bus/stats (server.go:1868):
  no client screen; drop counters exist but are not surfaced (see 5).

---

## 2. CORRELATION GAPS

### 2.1 turn.terminal carries task_id; both clients drop it
- Daemon payload (FROZEN contract, internal/agent/handler.go:280-300):
  TurnTerminalEvent includes TaskID ("set iff the turn dispatched a task", :288),
  IntentType (:289), AgentID (:290), HandlerCase (:291), ClassifiedBy (:294),
  Model (:295), TraceID/TurnSpanID (:297+). Populated at handler.go:1461; single
  emission funnel publishTurnTerminal (handler.go:2017-2036) via
  bus.PublishBlockingT(TopicTurnTerminal) (:2035; typed topic declared
  internal/agent/topics.go:14). WS class: WSProgress, never chat_message
  (handler.go:318-320).
- TUI: DROPPED. DeliverTurnTerminal keeps ONLY conversation_id/reply/status/error/
  duration_ms (internal/tui/models/chat_turn.go:389-396 early-buffer path, :415-422
  notify path); turnTerminalMsg has no TaskID field (models/turn.go:66-74);
  ChatMessage has no TaskID (models/chat.go:74-87). Completed turn renders as a plain
  assistant bubble (chat_turn.go:141-151). STALE COMMENTS assert the opposite:
  "The turn.terminal ack carries no task_id" (chat_turn.go:449-450, app.go:1762-1763)
  contradicting handler.go:288. Legacy id-bearing cards (parseAsyncAck chat.go:3027-3045,
  detached-task card chat.go:1338-1348,1451-1462) are DEAD CODE: only reachable from
  ChatResponseMsg which no production code constructs. task.completed/failed relays do
  carry task_id into ChatTaskResultMsg (app.go:1775-1777) but the render omits TaskID
  (chat.go:1505-1523) and the relay is suppressed while a turn is pending
  (app.go:1765, SuppressTaskCompletedFor chat_turn.go:455-460) - i.e. suppressed in
  exactly the normal async flow. A fuzzy-finder jump exists (ctrl+p -> ViewTasks,
  app.go:1048-1056,2707-2716) but nothing prefills it from a reply.
- GUI: PARSED THEN DROPPED. TurnTerminalEvent.taskId exists
  (models/ws_events.dart:169-170; also handlerCase :166-167, intentType :172-173,
  agentId :175-176, classifiedBy :178-179, model :181-182) but _consumeTurnTerminal
  reads only status/reply/error/turnId (providers/chat_provider.dart:664-746) and
  builds ChatMessage(id 'turn_<id>', role, content, timestamp, sessionId) at :719-725.
  ChatMessage has NO taskId field (models/api_models.dart:399-409); PendingTurn has no
  taskId (chat_provider.dart:155-176); ChatSubmitAck has no task_id/mode
  (sdk_client.dart:2267-2294). No bubble tap/link handler exists
  (chat_message_bubble.dart:136-149 MarkdownBody without onTapLink).
- Verdict: turn_id/task_id correlation is AVAILABLE client-side on the wire but neither
  client stores it on the message model, so reply->task jump is impossible without
  manual ID lookup. This is the single biggest navigation gap.

### 2.2 session_tool_calls writer: alive; reader surface: absent
- Verified call sites (production): internal/agent/handler.go:1588 inside
  persistExchange, reached from handler.go:1412, :2921, :3407. The historical
  purged-writer bug is remediated (leaf-05 comment handler.go:1580-1583; regression
  test handler_persist_toolcalls_test.go:210-239). Caveat: MemoryStore keeps messages
  only (internal/session/session.go:1331-1334) - tool calls persist only on SQLite.
- No client-facing read path (section 1.2): session.messages.get omits tool calls
  (session.go:2124-2148; Message struct store.go:12-27). Writing without a reader
  means the trace data is engine-internal only.

### 2.3 Dispatch evidence user-visible anywhere?
- Only as free text: the async ack markdown includes "**mode:** <label>" and task id
  (FormatEnhancedAsyncTaskAck, handler.go:2409-2426; modeToLabel :2392-2407:
  direct -> "executing directly", plan -> "planned", quick_plan -> "quick plan",
  spec_plan -> "spec-planned (multi-phase)", spec_pair -> "pair session"). This ack
  text is the ONLY user-visible dispatch evidence, and only for async-dispatch turns.
- intent_type/classifier_method/handler_case: stored in dispatch_log
  (metrics/store.go:315-328), queryable via session.dispatch_trace
  (rpc/dispatch_trace.go:20), carried on turn.terminal - but rendered NOWHERE
  (TUI: zero callers; GUI: zero hits; both drop the terminal fields).
- GUI bubble code contains an explicit TODO acknowledging the gap: render a mode chip
  when ChatMessage carries a typed suggestedMode field instead of relying on daemon
  markdown (features/chat/chat_message_bubble.dart comment block; suggestedMode exists
  nowhere else in lib).

---

## 3. PLAN LIFECYCLE UX

### 3.1 States and events (daemon truth)
- Transition table (internal/plan/transitions.go:52-74): planning -> {draft,cancelled};
  draft -> {pending_approval,planning,cancelled}; pending_approval ->
  {approved,cancelled,planning}; approved -> {synthesizing,cancelled}; synthesizing
  (LEASE state) -> {executing,draft,failed,approved}; executing ->
  {completed,cancelled,failed}; completed -> {confirmed}; failed/cancelled ->
  {confirmed,planning} (rework loop); confirmed terminal.
- Events published (internal/plan/manager.go publishEvent :1896-1907, plain
  bus.Publish - drop-on-full): plan.created (:517), plan.submitting (:565),
  plan.approved (:616), plan.rejected (:668), plan.revised (:728),
  plan.revision_completed (:751), plan.confirmed (:830), plan.cancelled (:916),
  plan.phase_state (:1296,:1375,:1419,:1514), plan.executing (:1320),
  plan.completed (:1609). NO plan.failed event is ever published (grep of
  manager.go publish sites) - failure is state-only.
- Create path: CreatePlan writes row, writes markdown with EMPTY phases, transitions
  planning -> draft, publishes plan.created (manager.go:443-520).
- Evolver proposals: skills/lifecycle/evolver.go CreatePlan with empty session/project
  (:717), then SubmitPlan to pending_approval (:726-731); pending queue capped at 50
  (:35,:701-708). Store is the dedicated plans-evolver.db
  (internal/daemon/components_wiki.go:175). These plans NEVER compile to phases -
  plan_phases empty is the true shape ("document proposal"). Retention prunes stale
  drafts/cancelled/failed and cancels stale open states (manager.go:925-960).
- Plan-mode task park: with plans.plan_compiler_enabled, mode "plan" seeds the
  brainstorm draft scaffold and returns WITHOUT decomposition; task stays in
  StatePlanning (internal/agent/strategic.go:794 SetState earlier; compiler branch
  :924-982; draft edits allowed only while planning, internal/agent/plan_draft.go:124;
  seal requires task state planning, plan_draft.go:398). TierComplex additionally runs
  critique rounds then WAITS at the human seal (strategic.go:944-975,
  critiqueActionAwaitSeal). Chat follow-up does NOT refine the draft: the draft is
  edited only via plan.draft RPC / CLI edit-draft; a new chat message routes as a new
  turn/task through the dispatcher (no draft-refine route exists in dispatcher.go).

### 3.2 Approval/rejection/seal: where the user acts
- CLI: FULL. 'meept plans approve <id>' -> plan.approve (cmd/meept/plans.go:343-374),
  reject (:384-407), confirm; draft lifecycle: 'meept plans draft/show-draft/
  edit-draft/seal <task-id>' -> plan.draft / plan.seal RPCs
  (cmd/meept/plan_seal.go:3-14,48-120). plan.seal returns compile problems as a
  RESULT {status:"problems",problems,warnings} keeping the draft a draft
  (internal/rpc/plan_seal.go:148-160); empty compile with no problems is refused
  (:163-166 "plan compile produced no phases and no problems").
- TUI: PARTIAL + BROKEN.
  - Plans view keys: 'a' approve -> plan.approve, 'v' revise -> plan.revise,
    'c' confirm -> plan.confirm (models/plans.go:259-278,317-336); NO reject key
    although plan.reject is registered (internal/rpc/plan.go:51).
  - Detail-modal footer advertises "a: approve | v: revise | c: confirm"
    (plans.go:755) but the modal handler accepts ONLY esc/q (plans.go:230-236):
    advertised keys are dead inside the modal.
  - NO seal/draft surface: zero plan.seal/plan.draft callers in internal/tui.
    Compile problems therefore have NO TUI surface at all.
  - '/plan' slash command is BROKEN: executePlan calls session.set_mode
    (command_handler.go:1624-1655, RPC at :1632) which has NO daemon registration
    anywhere in the repo (only occurrence is the TUI caller itself) -> always
    "method not found". The chat plan card advertises '/plan approve <id>'
    (models/chat.go:3140) which also does not exist: the /plan handler ignores args
    (command_handler.go:223-224). Note the DAEMON does understand '/plan <text>' as
    chat input (dispatcher.go:984-1030: plan-mode toggle via controller wired at
    internal/daemon/dsh_wiring.go:519, else force-create plan) - but the TUI
    intercepts /plan locally and never sends it, so the working daemon route is
    unreachable from the TUI slash handler.
- GUI: PARTIAL. Approve/reject/revise buttons only when state == 'pending_approval'
  (features/plans/plans_tab.dart:499-516), confirm when 'completed' (:517-523),
  passive "executing..." spinner when 'approved' (:524-553). REST POST
  /api/v1/plans/{id}/approve|reject|confirm|revise (sdk_client.dart:1750-1818;
  HTTP routes server.go:1904-1907). 'seal': ZERO occurrences in lib - no draft/seal
  UI, no compile-problem display. Agents-tab goal cards reuse approve/reject
  (agents_tab.dart:677-690).

### 3.3 Wait/failure states: visible and actionable?
- hostile_skipped: critique_outcome metadata value stamped when the hostile pass is
  skipped (flag on but chatter nil or same model; internal/agent/plan_critique_loop.go:
  88-94, stamp sites :668,:768-770). Config ships hostile_critic_model EMPTY slot
  (internal/config/config.go:552; client only built when non-empty,
  internal/daemon/components.go:1875-1887, Warn "hostile second pass is skipped").
  Disable-form keys default-on: fresh_critic_disabled / hostile_critic_disabled /
  interview_options_disabled (internal/config/schema.go:768,776,782 - absent key
  decodes false = feature runs). self_seal_enabled default FALSE ships dark
  (schema.go:759; SetSelfSealEnabled plan_critique_loop.go:100-107): plan-mode
  TierComplex ALWAYS waits at the human seal.
  UX: the wait is INVISIBLE. TUI shows a plain 'draft' row; critique_outcome/
  known_risks metadata never fetched or rendered (no hostile/critique strings in
  non-test internal/tui); no seal command exists. GUI: 'hostile' 0 hits in lib;
  state badge is generic plan.state.replaceAll('_',' ') (plans_tab.dart:210,333)
  with color switch defaulting to gray for draft/planning/anything-unknown
  (:224-240,:342-358); no action branch for the wait. The turn/task looks stalled;
  the only actionable path is the CLI ('meept plans show-draft/seal') which the
  user must discover unaided.
- Compile failure: surfaced only via plan.seal result/problems (plan_seal.go:148-160)
  or error "plan compile failed" (:147). CLI prints them; TUI/GUI never call seal ->
  no surface. Sealing a raw scaffold fails deterministic compilation (dialect:
  version 1, kebab-case artifact names in backticks, Open Questions empty) - the
  user gets no in-client guidance.
- Critic rejection: plan.rejected event (manager.go:668) renders a TUI chat card with
  reason truncated to 45 chars (models/chat.go:3164-3173); semi-actionable (reason
  visible, no next-step hint; revise only via 'v' in plans tab). GUI: no dedicated
  rejection UI (critic/compile/verdict: 0 hits in features/plans).
- Plan failed: NO event, and PlanExtended has no error/reason field
  (internal/tui/types/types.go:605-622) - WHY it failed is never shown; state icon
  only after manual refresh (plans.go:820-821).
- 'synthesizing' (real lease state, transitions.go:63,75) falls into the TUI default
  icon branch "? synt" (plans.go:824-825,849-850); GUI gray default badge.
- Event-driven staleness: TUI app.go handles ONLY plan.submitting/completed/
  confirmed/rejected (app.go:1899); plan.created/approved/revised/cancelled/
  executing/phase_state match the plan.* subscription (events.go:89) but have NO
  handler case: silently dropped; plans tab only flashes (app.go:1955-1959), user
  must press 'r'. GUI plans refresh on WS plans channel push
  (plan_provider.dart:37) - better, but state copy is still generic.

### 3.4 Evolver '0 phases 0 steps' render handling
- TUI chat card: FIXED. planCountLine returns "document proposal - no executable
  steps" (middle dot in source) when both counts are zero
  (internal/tui/models/chat.go:3105-3118, comment :3102-3104 cites evolver skill
  plans; used by review card :3129-3143 and completed card :3151).
- TUI plans TAB: NOT fixed. Rows render literal "0" phases and "0/0" steps
  (models/plans.go:380-386); renderProgressBar with total==0 returns an all-empty bar
  (plans.go:777-780); detail shows "progress: [empty bar] (0%)" (plans.go:692-698).
  No "document proposal" wording in the tab. Sidebar SKIPS zero-phase plans entirely
  (sidebar.go:456-458). Since evolver sink plans carry session_id="" their chat
  notifications are deliberately dropped (app.go:1901-1916) and the tab is their ONLY
  surface (merged via plan.go:306-317) - so the one place users see them is the
  misleading "0 / 0/0 / empty bar" render.
- GUI: phases section guarded by isNotEmpty (plans_tab.dart:291-301) - a zero-phase
  proposal renders as bare title+description with an actions header; NO "document
  proposal" text (0 hits), and Plan.filePath (models/api_models.dart:965) is parsed
  but never rendered, so there is no link to the markdown document either.

---

## 4. DIRECT vs PLAN DELINEATION

### 4.1 How does a user KNOW which path a turn took?
- Async-dispatch turns: the ack markdown is the signal - "## starting task",
  "**id:** <task-id>", "**mode:** executing directly | planned | quick plan |
  spec-planned (multi-phase) | pair session", plan ref + subtask count
  (handler.go:2409-2460, modeToLabel :2392-2407, call site :1203). Rendered as
  ordinary markdown in both clients; no structured badge.
- Sync/direct turns: NO signal. turn.terminal IntentType/HandlerCase/ClassifiedBy
  dropped by TUI (chat_turn.go:389-396) and GUI (chat_provider.dart:664-746).
  TUI accepted-ack note "accepted; result arrives via turn.terminal"
  (internal/rpc/chat_submit.go:223-224) is never shown (Note rendered only on
  rejection, models/chat.go:1982-1986); pending line is generic "thinking..."
  (chat.go:1945-1948). 'parked' status silently re-arms the awaiter with no text
  change (chat_turn.go:115-130). GUI parked text is quota-specific
  (chat_provider.dart:681 "waiting for provider quota - will resume automatically").
- GUI has an explicit in-code TODO for a mode chip (chat_message_bubble.dart comment:
  backend includes a mode: markdown line; render a dedicated chip when a typed
  suggestedMode field exists) - field does not exist yet.
- Plan-mode parked tasks: task sits in 'planning' (strategic.go:794,924-982); the
  user sees the ack (if async) but no "awaiting seal" state copy anywhere (3.3).

### 4.2 Explicit user controls
- Force planning: daemon-side '/plan <text>' chat prefix forces plan creation or
  toggles plan-mode when the controller is wired (dispatcher.go:984-1030;
  SetPlanModeController :831; bridge wired internal/daemon/dsh_wiring.go:519).
  Config plans.mode off|always|threshold (internal/config/schema.go Validate,
  PlansConfig.Mode). GUI: /plan registered in slash registry with usage
  '/plan [list|show|approve]' (core/slash_commands.dart:112-116) but NO local case in
  chat_input switch (:1565-1699) -> forwarded to daemon as plain chat text, where the
  dispatcher prefix route DOES handle it (so GUI /plan works by accident of
  fallthrough; the advertised list/show/approve subcommands do not exist daemon-side).
  TUI: /plan intercepted locally and broken (session.set_mode unregistered, 3.2);
  also absent from TUI slash autocomplete (models/chat.go:3289-3306) though
  sharedclient registers it (internal/sharedclient/slash.go:70,212).
- Force direct: NO user control found. No force_direct/direct_mode flag or slash
  command in dispatcher.go, TUI command_handler.go, or GUI (grep 0). Mode selection
  is heuristic: SuggestedMode with short-input (<50 chars) downgrade to "direct"
  (dispatcher.go:2011,2035-2041). The GUI agent picker selects an executor agent,
  not a mode (agents_tab.dart:191,435; chat_input.dart:1719,1755); steer mode is
  mid-turn steering, not plan/direct selection (chat_input.dart:1758-1761).

### 4.3 Planner LLM failure mid-plan
- Compiler-pipeline plan mode: draft transport failure -> Warn + fallback metric,
  seeded draft still awaits seal (strategic.go:944-975 flow_error path;
  plan_critique_loop.go:530-543 draft_transport fallback returns
  critiqueActionFallback). TierComplex "never hard-fails the task because the fancy
  path is down" (plan_critique_loop.go:23). User-visible effect: a raw scaffold
  draft parked in planning with no refinement and no explanation in any client.
- Legacy path: planSinglePhase error -> Warn + createFallbackSteps generic
  decomposition (strategic.go:995-1005, metric reason "plan_failed", failed_replan
  mark :900-910); EXCEPT ErrPlannerNoPlanNotArtifact propagates so the task fails
  with its reason (strategic.go:998-1001, issue #53 "empty-plan honest failure").
- quick_plan TierComplex with self_seal disabled falls back to single-shot with a
  Warn (schema.go:753-758 comment). All of these degradations are log/metric-only;
  no client renders "planner degraded to fallback" to the user.

---

## 5. EVENT DELIVERY RELIABILITY (observability transport)

### 5.1 Two transports, different semantics
- TUI: RPC bus.subscribe + bus.poll over unix socket, 500ms poll
  (internal/tui/events.go:20-21,107,160,243; rpc.go:70). Daemon keeps a 100-event
  ring per subscription with drop-oldest + in-band dropped accounting
  (internal/rpc/proxy.go:443 MaxEvents:100, :542-556 collector, poll response
  dropped = trimmed + per-topic DroppedCount, proxy.go:634-641 "Explicit, in-band
  accounting; never silent"). TUI DISCARDS the dropped count
  (events.go:248-251 parses only events/next_seq) - bus-level loss is invisible.
- GUI: WebSocket only; bus.subscribe/bus.poll: 0 hits in lib. Subscribe frames
  {type:subscribe, channel, session_id} for session channels chat/progress plus
  global single-segment channels jobs/metrics/plans
  (services/websocket_service.dart:95-96,703,716,728,885-897). turn.terminal takes a
  second hold on progress and filters client-side: type==agent_progress AND
  session/conversation match AND turn_id+handler_case non-empty
  (websocket_service.dart:847-869; predicate ws_events.dart:229-236). Daemon relay
  classification: turn.* -> WSProgress (handler.go:318-320; wsclass.go:44-49),
  never chat_message (blank-bubble invariant, server.go comments :750-807).

### 5.2 Reconnect re-arm
- GUI: YES. Exponential backoff reconnect (websocket_service.dart:259-323,380-389),
  _flushPendingSubscriptions on every successful open (:487,:550) replays one
  subscribe per held (channel,session) pair (:332-356); ping/pong watchdog (:656-670);
  chat provider re-arms idempotently on history load (chat_provider.dart:545-597);
  metrics falls back to HTTP poll when WS down (metrics_provider.dart:97-115).
  Exception: kanban sidecar WS reconnect is BOUNDED - max 3 attempts then permanent
  linkError, no re-subscription (features/kanban/kanban_service.dart:150,386-405).
- TUI: NO - CRITICAL. subscribe() runs only from EventStream.Start
  (events.go:144), called only from Sidebar.Init on first connect
  (sidebar.go:285, app.go:1188-1191) and on compact->wide resize restore
  (app.go:3176-3194). RPCClient.Call transparently reconnects the socket
  (rpc.go:106-140) but the daemon cancels/deletes every subscription owned by the
  closed connection (proxy.go:414-431 connDoneCh watcher, cleanup :460-490). After
  reconnect bus.poll returns "subscription not found" (proxy.go:601-602); the error
  is swallowed end-to-end (events.go:244-245 returns Err; app.go:1532-1535 and
  sidebar.go:561-565 never check msg.Err). Result: after any daemon restart or
  socket blip, the TUI polls a dead subscription forever - turn terminals, plan
  cards, progress, activity feed all go SILENT with no error shown. Only a resize
  round-trip through compact mode or an app restart re-arms. Initial subscribe
  failure is also silent (events.go:161-163).

### 5.3 Wildcard single-segment limitation
- Matcher requires EQUAL segment counts: "agent.*" matches "agent.status" but NOT
  "agent.sub.topic" (internal/bus/bus.go:468-488; exported MatchWildcard :464-466).
- TUI subscriptions (events.go:76-105) deliberately enumerate segment counts
  ('agent.*.*', 'agent.*.*.*', 'chat.worker.*', 'acp.*.*'; comment :61-72; overlap
  pinned by events_topics_overlap_test.go:21-60). Remaining GAPS - handlers exist
  but topics can never be delivered:
  - llm.tokens.used (3 seg; publisher internal/agent/loop.go:9764) vs subscribed
    'llm.*' (2 seg, events.go:86). Handler sidebar.go:572-573 dead.
  - tool.execution.progress (3 seg; publisher internal/agent/executor.go:2370);
    no 'tool.*' pattern at all. Handler app.go:1721-1743 dead.
  - chat.message.received (3 seg; publisher handler.go:947) vs 'chat.*' (2 seg,
    events.go:87). Multi-client participant handler app.go:1876-1884 dead.
  - queue.job.* (3 seg; e.g. internal/queue/queue.go:400) vs 'queue.*' (2 seg,
    events.go:82); queue view relies on manual refresh.
- WS relay topic list (internal/comm/http/server.go:740-807) is segment-aware by
  design: "*", agent.*, agent.*.*, task.*, task.*.*, step.*, step.*.*,
  orchestrator.*, chat.*, chat.*.*, tool.*, llm.*, review.*, queue.*, queue.*.*,
  plan.*, plan.*.*, turn.*, terminal.*, daemon.*, collaboration.*, pair.*,
  acp.*.*, agent.turn.state, bot.result, employee.result, employee.*, kanban.*,
  kanban.*.*, storage.* - with long comments documenting each past single-segment
  drop bug (turn.* added after GUI hung pending forever, :778-783). Residual risk:
  any NEW 3+ segment topic outside these prefixes is silently unrelayed; the
  pattern list is manual, not derived.
- Topology doc under-reports: docs/generated/bus-topology.md is generated by
  scripts/gen-connectivity-graph.py whose RE_SUBSCRIBE matches only '.Subscribe('
  and '.SubscribeWildcard(' (scripts/gen-connectivity-graph.py:123-130). Typed
  bus.SubscribeT call sites (10 production sites, e.g. activity/busbridge.go via
  SubscribeT, agent/topics.go typed topics) are INVISIBLE to the generator - the
  graph is not a reliable inventory for this audit or for operators.

### 5.4 PublishBlockingT vs drop-on-full: what can silently drop
- Semantics: Publish = non-blocking select-default, drops when a subscriber channel
  is full (bus.go:132-135,249+); PublishBlocking = 5s timeout per subscriber, logs
  Error on timeout, Warn on no-subscriber drop (bus.go:139-145,193-199,249-258).
  Default subscriber buffer 100 (bus.go:106-110). PublishBlockingT typed wrapper
  (internal/bus/topic.go:51-68).
- Blocking (critical) publishers found: turn.terminal ONLY
  (internal/agent/handler.go:2035) plus RPC bus.publish proxy path
  (internal/rpc/proxy.go:379). ledger announcements deliberately use non-blocking
  PublishT (internal/daemon/ledger_publish.go:19,78 "drop-on-full is acceptable for
  announcements").
- Topics that can SILENTLY drop (non-blocking publish + full buffer or absent
  subscriber), relevant to observability:
  - plan.* ALL lifecycle events (manager.go:1907 m.bus.Publish): plan.created/
    submitting/approved/rejected/revised/revision_completed/confirmed/cancelled/
    executing/phase_state/completed. A missed plan.rejected/completed means the
    chat card never renders.
  - task.completed / task.failed (activity ring busbridge.go:58-60 subscribes;
    publishers non-blocking) - task result cards and activity records can vanish.
  - queue.job.claimed/completed/failed/retry/requeue/recovered
    (internal/queue/queue.go:400 etc).
  - agent.progress / agent.event.* / agent.quota_wait (loop.go:9661,9715 etc) -
    progress and quota-wait visibility.
  - llm.tokens.used, tool.execution.progress, chat.message.received - doubly dead
    for TUI (subscription gap 5.3) even when delivered on the bus.
  - turn.terminal itself: blocking, but a subscriber that stays full for 5s still
    loses it (bus.go:252-257 Error log only); and with NO subscriber connected the
    event is dropped with a Warn (bus.go:193-199) - there is no replay store for
    terminals, so a disconnected client never learns the turn finished except via
    history reload (GUI) or not at all (TUI dead-subscription case 5.2).
- Drop accounting exists (bus Subscriber.DroppedCount, proxy poll 'dropped' field)
  but no client renders it (TUI discards, GUI has no bus.poll).

---

## 6. RANKED TOP-10 GAPS

1. CRITICAL - TUI event feed dies silently after reconnect; no re-subscribe, poll
   errors ignored. Evidence: internal/tui/events.go:144,161-163,244-245;
   internal/tui/app.go:1532-1535; internal/rpc/proxy.go:414-431,601-602.
   Fix: on "subscription not found" poll error, re-run subscribe() and surface a
   reconnect banner; re-arm on every ConnectSuccessMsg, not just first.

2. CRITICAL - Reply-to-task correlation dropped in BOTH clients despite task_id on
   turn.terminal. Evidence: internal/agent/handler.go:288 vs
   internal/tui/models/chat_turn.go:389-396 + models/turn.go:66-74;
   ui/flutter_ui/lib/providers/chat_provider.dart:719-725 + models/api_models.dart:399-409.
   Fix: add TaskID/IntentType/HandlerCase to both message models and render a
   tappable "task <id> - view" chip on the assistant bubble that opens task detail.

3. CRITICAL - Plan seal/draft lifecycle has no TUI/GUI surface; hostile_skipped and
   await-seal waits look stalled. Evidence: internal/rpc/plan_seal.go:93-94 registered;
   zero plan.seal/plan.draft callers in internal/tui and ui/flutter_ui/lib;
   internal/agent/plan_critique_loop.go:88-94,768-770;
   internal/config/schema.go:759 (self_seal default false);
   ui/flutter_ui/lib/features/plans/plans_tab.dart:499-553 (no wait-state branch).
   Fix: render critique_outcome/await_seal as an explicit "awaiting your seal" card
   with show-draft + seal buttons calling plan.draft/plan.seal, showing compile
   problems inline.

4. MAJOR - TUI /plan is broken (session.set_mode unregistered) and the chat card
   advertises a nonexistent '/plan approve <id>'. Evidence:
   internal/tui/command_handler.go:223-224,1632; repo-wide grep session.set_mode has
   no daemon handler; internal/tui/models/chat.go:3140; working daemon route
   internal/agent/dispatcher.go:984-1030 unreachable from TUI.
   Fix: forward /plan text to chat.submit (dispatcher handles it) or register a
   real mode RPC; replace the card hint with 'open plans tab, press a'.

5. MAJOR - GUI task detail pane unreachable and steps never shown. Evidence:
   ui/flutter_ui/lib/features/tasks/tasks_tab.dart:10,18,22 (activeTaskProvider never
   written, onTaskSelected not passed); tasks_detail.dart:104-125;
   models/api_models.dart:669,689-702 dead step models; sdk_client.dart:878 getTask
   uncalled. Fix: wire onTaskSelected -> activeTaskProvider and hydrate detail via
   GET /tasks/{id} + /tasks/{id}/steps.

6. MAJOR - session_tool_calls written but unreadable by any client. Evidence:
   writer internal/agent/handler.go:1586-1590 (alive); reader gap
   internal/session/session.go:2124-2148 (messages only), internal/session/store.go:12-27
   (no ToolCalls field); GetToolCallsForMessages used only engine-side
   (internal/agent/loop.go:10456). Fix: extend session.messages.get with an
   include_tool_calls option joining GetToolCallsForMessages; render an expandable
   trace under assistant bubbles.

7. MAJOR - Dispatch evidence (intent_type/classifier_method/handler_case) invisible;
   session.dispatch_trace has no client and no HTTP route. Evidence:
   internal/rpc/dispatch_trace.go:20; internal/metrics/store.go:315-328; zero callers
   in internal/tui and ui/flutter_ui/lib; no /api/v1 dispatch-trace route in
   internal/comm/http/server.go. Fix: add GET /api/v1/sessions/{id}/dispatch-trace and
   a per-turn "why this route" popover fed by the terminal payload fields.

8. MAJOR - Evolver document proposals misrendered in the only surface that shows
   them: TUI plans tab "0 / 0/0 / empty bar", sidebar skips them, GUI bare card with
   no document link. Evidence: internal/tui/models/plans.go:380-386,777-780;
   internal/tui/sidebar.go:456-458; internal/tui/app.go:1901-1916 (chat notices
   dropped for session-less sink plans); internal/rpc/plan.go:306-317 (tab is the
   only surface); ui/flutter_ui/lib/features/plans/plans_tab.dart:291-301 +
   models/api_models.dart:965 filePath unrendered. Chat-card fix already exists as
   the pattern: internal/tui/models/chat.go:3105-3118. Fix: reuse planCountLine
   wording in tab/detail, and render filePath as an open-document action.

9. MAJOR - plan.failed has no event and no reason field; most plan.* events have no
   TUI handler; TUI discards bus drop counters. Evidence:
   internal/plan/manager.go:517-1609 (no plan.failed publish), :1907 non-blocking
   Publish; internal/tui/app.go:1899 (only 4 plan events handled);
   internal/tui/types/types.go:605-622 (no error field); internal/tui/events.go:248-251
   (dropped discarded) vs internal/rpc/proxy.go:634-641. Fix: publish plan.failed
   with reason, handle all plan.* events with a tab refresh, and show a "N events
   dropped" warning when poll reports dropped>0.

10. MINOR - Subscription/topology blind spots: TUI handlers for llm.tokens.used,
    tool.execution.progress, chat.message.received can never fire (segment-count
    mismatch); topology generator misses typed SubscribeT sites; GUI reflection
    applies proposals blind and search deep-links only to tab roots. Evidence:
    internal/tui/events.go:76-105 vs internal/bus/bus.go:470-488;
    publishers internal/agent/loop.go:9764, executor.go:2370, handler.go:947;
    scripts/gen-connectivity-graph.py:123-130;
    ui/flutter_ui/lib/features/reflection/reflection_panel.dart:311-368 (change
    content never rendered, reflection_models.dart:33,46);
    features/search/search_panel.dart:487-493 (tab-root navigation).
    Fix: add the missing 3-segment patterns (or per-topic exact subscriptions),
    extend the generator regex to SubscribeT/PublishT, render proposal diffs and
    per-item deep links.

Honorable mentions (not ranked): GUI agents.review client+model exist with zero
callers (sdk_client.dart:2053, activity_models.dart:216-236); audit chain verify is
CLI-only (cmd/meept/agents_cmd.go:1703-1723) with no TUI/GUI surface; kanban sidecar
WS gives up after 3 reconnects (kanban_service.dart:386-405); TUI plans detail-modal
footer advertises dead keys (plans.go:755 vs :230-236); no plan.reject key in TUI
(plan.go:51 registered); stale "carries no task_id" comments
(chat_turn.go:449-450, app.go:1762-1763).

---

## 7. SURFACE MATRIX (surface x TUI x GUI x link-from-chat)

| Surface | TUI | GUI | Link from chat |
|---|---|---|---|
| task detail + steps (task.get/list_extended/steps) | exists (palette t, /tasks; list-fresh only) | partial (list only; detail unreachable, steps dead) | missing (task_id dropped both clients) |
| tool traces (session_tool_calls) | missing (live viz only) | missing | missing (no read RPC at all) |
| dispatch trace (session.dispatch_trace) | missing | missing (no HTTP route either) | missing (terminal fields dropped) |
| activity.query/recent/findings | exists (palette a; overview unused) | exists (/activity; IDs parsed unrendered) | missing (no tap-through, no reply pointer) |
| activity.overview | missing | exists (drift panel) | missing |
| agents.review | missing | missing (client+model dead) | missing |
| queue status (queue.stats/list) | exists (palette q, retry R, steer badges) | exists (read-only; taskId raw string) | partial (TUI steer/follow-up counts; no job link) |
| plan phases/steps (plan.list*/get_phases) | exists (palette p; stale until r) | exists (/plans; WS push refresh) | partial TUI (chat plan cards; broken approve hint) / missing GUI |
| plan draft/seal (plan.draft/plan.seal) | missing (CLI only) | missing (CLI only) | missing |
| plan approve/reject/confirm/revise | partial (a/v/c keys; no reject; modal keys dead) | exists (pending_approval-gated REST buttons) | missing (advertised /plan approve does not exist) |
| audit findings (agents.audit.list/resolve) | exists (agents panel key 3) | missing | missing |
| audit chain verify (agents.audit.verify) | missing (CLI only) | missing | missing |
| metrics live/usage | partial (sparklines, /usage; tokens topic dead) | exists (/tools/metrics + WS + HTTP fallback) | missing (no per-turn attribution) |
| child tasks (session.get_child_tasks) | partial (sessions tree via tree.get) | partial (stop-confirm flow only) | missing |
| bus drop accounting (poll dropped / bus.stats) | missing (counter discarded) | missing (no bus.poll) | missing |
| planning-vs-direct mode signal | partial (ack markdown only; /plan broken) | partial (ack markdown only; TODO mode chip) | n/a (signal itself mostly absent) |

---

## 8. METHOD NOTES

- Daemon RPC inventory: 268 RegisterHandler methods enumerated across internal/daemon
  and internal/rpc (grep, non-test); activity.* registered via helper loop
  (internal/daemon/activity_rpc.go:44).
- TUI/GUI leaf audits ran as parallel read-only subagents; load-bearing claims
  (turn.terminal field drop, /plan breakage, 0-phase renders, seal absence, writer
  call sites, transition table, wildcard matcher, WS topic list, reconnect paths)
  were re-verified directly against HEAD before inclusion.
- Known limits: no runtime daemon was exercised (static audit only); "zero callers"
  claims are grep-based over non-test production code; the bus-topology doc was
  treated as unreliable per its generator's raw-Subscribe-only regex.