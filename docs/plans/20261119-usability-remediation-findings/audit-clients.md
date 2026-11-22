# MEEPT CLIENT USABILITY AUDIT (TUI + Flutter GUI) - CONSOLIDATED
# Assembled by the orchestrator from four verified leaf audits after the
# client-audit subagent stalled past finalize orders. Sources (same HEAD
# 1cee89d7, clean worktree /tmp/meept-usability-head):
#   gui-notes.md        (GUI feature->RPC/route map, task_id drop, plans, WS)
#   notes-nav.md        (GUI routes + navigation reachability inventory)
#   notes-plan-hitl.md  (GUI plan/HITL/ACP/restore/thread fidelity)
#   notes-server.md     (server WS relay, reply provenance, double-render)
# Plus orchestrator LIVE TUI findings (findings-orchestrator.md F-ORCH-*) and
# audit-observability-plans.md (TUI+GUI observability surface matrix).
# Every claim cites file:line; LIVE items cite rig artifacts.

===========================================================================
AXIS 1 - CONTEXT BETWEEN MESSAGES
===========================================================================
1.1 History assembly / resume
- GUI: getMessagesPage (sdk_client.dart:628,635; chat_provider.dart:492,505,
  1252). In-memory transcript capped at 500 messages (_maxMessages,
  chat_provider.dart:126, copyWith truncates :261-269). Older history needs
  paging; no thread filter on GET /sessions/{id}/messages
  (api_handlers.go:960-1009, zero thread param) - transcript mixes threads.
- GUI restore: last session NOT restored on relaunch (top-tabs reuse-or-
  create empty session, home_screen.dart:388-415); pendingTurns memory-only,
  no turn-resume API; session.auto_resume pref honored ONLY in sidebar
  layout (sidebar_home_screen.dart:226-245; home_screen never reads it).
  StorageService persists key/theme/TTS/host/port/layout/input-history only
  (storage_service.dart:103-350). [notes-plan-hitl F2/F3 HIGH]
- TUI: resumes most-recent session (LIVE F-ORCH-7: stale failed-task sidebar
  + mixed UTC/local timestamps after daemon restart). Clients DISAGREE on
  resume policy.
- LIVE F-ORCH-29: conversation-only sessions lose ALL history to FOREIGN KEY
  failure (787) in persistExchange - 9 occurrences in rig log. Resume empty.
- LIVE F-ORCH-16/24: recall follow-ups answer from last-task-only digest;
  failed state described as "still in progress" (recall_answer.go:86-90);
  session's own codeword unreachable (task_get plumbing instead of memory).

1.2 Thread/topic
- GUI thread dropdown is COSMETIC for routing: SetActiveThread only flips
  IsActive flags (thread_router.go:170-203); GetActiveThread has NO caller
  in the chat dispatch path; daemon re-detects topic per message
  (topic_detector.go:60-115). Submit/history keys stay session-level.
  [notes-plan-hitl, verified]
- CODE A6: topic resolved twice per turn from different inputs (full text
  vs Intent.Summary first-100-chars, dispatcher.go:955 vs 3068+4206) -
  memory key and queue key can disagree.

===========================================================================
AXIS 2 - ROUTING VISIBILITY
===========================================================================
- Neither client renders intent_type/classifier_method/handler_case/agent_id
  from turn.terminal: TUI keeps only conv/reply/status/error/duration
  (chat_turn.go:389-396); GUI parses then drops (chat_provider.dart:664-746,
  ws_events.dart:166-179). classified_by stored in dispatch_log but
  session.dispatch_trace has ZERO client callers and NO HTTP route.
- GUI agent picker (agents_tab.dart:191,435 -> chat_input.dart:1719,1755)
  sets agent_id on send - executor selection, not mode; no per-message
  routing display. Unknown agent override silently ignored daemon-side
  (dispatcher.go:1421-1424 Warn only).
- Misroute correction: no surface. Steering is the only mid-turn control
  and it is broken (see AXIS 5 / routing report R2): TUI sends bare session
  conv id while queues live under thread ids -> "queue not found" default;
  GUI sendFollowUp (chat_provider.dart:1017) has NO widget caller (dead).
- LIVE F-ORCH-2/20: file-edit prompts misroute to committer/git; user sees
  "task completed" with zero file effects and no routing disclosure.

===========================================================================
AXIS 3 - REPLY CLUTTER
===========================================================================
- Double-render (LIVE F-ORCH-5 + notes-server 2.4): daemon sends the SAME
  reply as chat_message push AND turn.terminal relay, NO server suppression.
  TUI renders card (ChatTaskResultMsg, chat.go:1505-1523) AND terminal bubble;
  SuppressTaskCompletedFor (chat_turn.go:455-460) only gates the relay while
  pending. GUI dedupes by content vs LAST assistant bubble only
  (_lastAssistantContentEquals, chat_provider.dart:749-757) - fragile.
- Scaffolding leaks (LIVE): claims/evidence JSON envelope raw in relay reply
  (F-ORCH-21); "## review: approved" internal feedback pushed to chat
  (F-ORCH-25); task-detail step row shows "## Session execution context"
  (F-ORCH-4); sidebar recent-memory shows "<<<MEMORY_CONT..." markers.
- Empty completed reply on direct file-write turns (LIVE F-ORCH-15).
- Job-ID prose in user replies ("job job-... completed by agent X:").
- Ack noise: "## starting task / id / mode / plan / 0 subtasks" markdown for
  every async turn; parked status re-arms awaiter with no text change (TUI
  chat_turn.go:115-130); GUI parked text quota-specific even for non-quota
  (chat_provider.dart:681).
- copyWith discipline (GUI): VERIFIED GOOD at HEAD - send path and
  clearMessages use copyWith preserving pendingTurns (chat_provider.dart:
  1075-1082, 1514-1530 with F20/L2 bughunt comments). The historical
  fresh-ChatState regression class is fixed; 52 copyWith sites, no
  mid-turn fresh construction found.

===========================================================================
AXIS 4 - BEHIND-THE-SCENES NAVIGATION
===========================================================================
- Reply->task link: MISSING both clients. task_id on turn.terminal dropped
  (TUI chat_turn.go:389-396, turn.go:66-74; GUI ChatMessage has no taskId,
  api_models.dart:399-409). No bubble tap handlers (chat_message_bubble.dart
  :136-149 MarkdownBody, no onTapLink). Stale comments claim "ack carries no
  task_id" - false at HEAD (handler.go:288).
- Tool traces: session_tool_calls writer ALIVE (handler.go:1586-1590) but NO
  read RPC (session.messages.get returns plain messages, session.go:2124-
  2148). TUI live viz only (sidebar.go:598-607); GUI toolCalls parsed never
  rendered (api_models.dart:408).
- GUI task detail pane UNREACHABLE: activeTaskProvider never written,
  TasksList built without onTaskSelected (tasks_tab.dart:10,22); Task.steps
  models dead (api_models.dart:669,689-702); getTask uncalled (sdk:878).
- GUI activity: /activity via palette only; tiles render summary+kind+time,
  IDs/detail parsed unrendered (activity_list.dart:244-274); deep link
  meept://session/<id> -> /activity?session=<id> DROPS the param (no widget
  reads queryParameters); no per-item tap.
- TUI: palette ctrl+x -> t/tasks, a/activity, q/queue, p/plans, m/memory,
  e/agents, b/kanban, v/memory-review, i/intake, o/projects, f/find,
  n/new, r/description, y/sidebar. Detail modal on enter; closes on q NOT
  Escape (LIVE). Tasks view "0/1" vs "1/1" unlabeled counters (LIVE F-ORCH-8).
- TUI event feed dies silently after reconnect (events.go:144,161-163,244-
  245, no re-subscribe); discards bus drop counter (events.go:248-251).
  LIVE F-ORCH-26: 100-event drop-oldest buffer lost 9853/9953 events incl.
  terminals under load.
- GUI WS reconnect re-arms (websocket_service.dart:332-356) - GOOD; kanban
  sidecar WS gives up after 3 reconnects (kanban_service.dart:386-405).
- Unreachable dead GUI features (notes-nav): skills panel, reflection panel
  (route exists but change content never rendered), tasks detail pane,
  late-failure badge, embedded chat-tab tool slot, sidebar find/Ctrl+P.
  /diff tells user "review in /tools/changes:" as TEXT, does not navigate.
  Search results navigate to tab roots without selecting the item.

===========================================================================
AXIS 5 - PLAN vs DIRECT DELINEATION
===========================================================================
- Mode signal: async ack markdown "**mode:** planned/quick plan/executing
  directly" (handler.go:2392-2426) is the ONLY signal; no structured badge.
  GUI has explicit TODO for a mode chip (chat_message_bubble.dart:130-135);
  suggestedMode field exists nowhere. Sync/direct turns: NO signal.
- Force planning: daemon /plan prefix route works (dispatcher.go:984-1030);
  GUI /plan falls through to it by accident (advertised list/show/approve
  subcommands do not exist daemon-side). TUI /plan BROKEN - calls
  session.set_mode which has NO daemon handler (command_handler.go:1632);
  chat card advertises nonexistent "/plan approve <id>" (chat.go:3140).
- Force direct: NO control anywhere (mode heuristic, <50 chars downgrades
  to direct, dispatcher.go:2035-2041).
- Approval gates: TWO disjoint gates.
  (a) plans.db PlanManager gate: GUI Plans tab approve/reject/revise/confirm
      buttons (plans_tab.dart:499-523) -> REST /plans/{id}/* -> correct
      end-to-end (manager.go:594-681). TUI a/v/c keys, NO reject key.
  (b) strategic task gate (>=5 steps): task.pending_approval ORPHAN topic
      (zero subscribers). GUI: NO task.approve/reject call sites, NO HTTP
      route - DEAD END (gray row, cancel only). TUI: plans view reads
      plan.list_by_session -> 0 rows (gate lives in task.Metadata). Only
      CLI meept task approve works. task.reject APPROVES (task_approval.go:69,
      LIVE F-ORCH-12). [notes-plan-hitl F1 HIGH]
- Plan compiler (plan_compiler_enabled=true): draft/seal CLI only; zero
  plan.seal/plan.draft callers in TUI/GUI; raw scaffold parks forever
  (LIVE F-ORCH-1); follow-up creates NEW task not draft refinement (A9).
- Wait/failure states: hostile_skipped/await-seal/compile-failure render as
  gray badge or stalled turn, no actionable copy; watchdog reaps legitimate
  approval waits at 600s then late completed terminal fires (queue-gates
  #7/#11). plan.failed has NO event (manager.go failPlan publishes nothing).
- Evolver 0-phase proposals: TUI chat card fixed (planCountLine "document
  proposal", chat.go:3105-3118) but plans tab/detail still literal zeros
  (plans.go:380-386,777-780); GUI bare card, filePath unrendered.
- ACP permissions (GUI): non-dismissible dialog host (main.dart:331-341),
  allow/deny/always -> POST /acp/permissions/resolve, failure keeps entry
  for retry - GOOD. Unknown destructive-confirmation action = silent dead
  end (debugPrint only). [notes-plan-hitl F7 LOW]

===========================================================================
RANKED TOP-10 CLIENT DEFECTS
===========================================================================
1. CRITICAL task_id/intent/handler_case dropped by BOTH clients -> no
   reply->task navigation. chat_turn.go:389-396; chat_provider.dart:719-725.
   Fix: keep fields on message models; tappable task chip.
2. CRITICAL strategic approval gate invisible in TUI+GUI; task.reject
   executes. strategic.go:2230 orphan; task_approval.go:69. Fix: subscribe
   + card + RejectFunc + HTTP routes.
3. CRITICAL TUI event feed dies after reconnect; 100-event buffer drops
   terminals under load. events.go:144,244; proxy.go MaxEvents=100. Fix:
   re-subscribe + critical-topic lane + dropped>0 re-query.
4. CRITICAL plan compiler draft/seal has no client surface; parks forever.
   plan_seal.go RPCs, zero TUI/GUI callers. Fix: draft card + seal/reject.
5. MAJOR double-render of task replies (card + terminal), no server
   suppression. handler.go:2148-2165; chat.go:1505; chat_provider.dart:749.
   Fix: relay-pair marker + client dedupe.
6. MAJOR GUI task detail unreachable; steps dead. tasks_tab.dart:10,22.
   Fix: wire onTaskSelected + hydrate steps.
7. MAJOR tool traces written but unreadable. handler.go:1586 writer;
   session.go:2124 reader gap. Fix: include_tool_calls + expandable trace.
8. MAJOR TUI /plan broken (session.set_mode unregistered) + dead approve
   hint. command_handler.go:1632; chat.go:3140. Fix: forward to chat.submit.
9. MAJOR GUI resume fidelity: last session not restored, pendingTurns
   memory-only, auto_resume sidebar-only. home_screen.dart:388-415. Fix:
   persist active session + turn-resume query.
10. MAJOR scaffolding/envelope/review-feedback leaks into replies + empty
    completed replies. recall_answer.go:86; relay stripper; handler.go
    route_to_agent. Fix: strip on relay, synthesize empty replies.

===========================================================================
SURFACE INVENTORIES
===========================================================================
TUI keys (palette ctrl+x): c chat, s sessions, t tasks, q queue, m memory,
p plans, e employees/agents, a activity, b kanban, y sidebar, o projects,
v memory-review, u users, i intake, n new session, r edit description,
f find. Slash: /tasks /task /plan(broken) /usage /eval /facts /diff(text)
/clear /help. Detail modal enter; q closes (Escape ignored).

GUI routes (core/router.dart): / chat, /sessions, /tasks, /plans, /agents,
/activity (full-screen, palette only), /intake, /kanban, /queue, /settings,
/tools/{search,branches,skills,memory,reflection,changes,prompts,calendar,
metrics,voice}. Reachable-from-chat via input/palette: prompts, intake,
agents(toptabs), activity, kanban, voice, memory, memory-review, users,
branches, search(toptabs), all 6 tabs. Bubble affordances: NONE.
Dead/unreachable: skills panel, reflection change content, tasks detail,
late-failure badge, embedded chat-tab tool slot, sidebar find/Ctrl+P.
