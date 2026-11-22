# Audit: Session/Worker Location Binding and Steering/Followup Semantics

Repo: /tmp/meept-usability-head (HEAD 1cee89d7, clean checkout, read-only audit).
Perspective: interactive chat user (TUI/HTTP/RPC). All line numbers verified
against HEAD with grep -n + read_file. Paths relative to repo root.

Method note: every claim below cites file:line. Where a negative claim is made
("no caller", "no reopen"), it was grep-verified across internal/ and cmd/
excluding _test.go files.

---

## SECTION A - SESSION/WORKER LOCATION

### A1. Submit payload -> handler -> dispatcher binding chain (baseline, correct)

chat.submit accepts message/session_id/conversation_id/agent_id/model/turn_id
(internal/rpc/chat_submit.go:36-45). When conversation_id is empty it resolves
the session's OWN conversation id via Get(sessionID) then
GetByConversationID(sessionID) (internal/rpc/chat_submit.go:149-156); otherwise
it mints conv-<id> (chat_submit.go:157-159). The ack is
{turn_id, conversation_id, session_id, accepted, note} (chat_submit.go:219-225)
and the payload is published on bus topic chat.request (chat_submit.go:201-211).
ChatHandler.handleRequest repeats the same dual resolution
(internal/agent/handler.go:824-834), registers the turn
(handler.go:871-922), and calls
Dispatcher.ClassifyAndRoute(ctx, req.Message, conversationID, ...)
(handler.go:1026). NOTE: the dispatcher's parameter is named sessionID but
receives the CONVERSATION id; all session-store lookups downstream key on it
via GetByConversationID (dispatcher.go:3148, 3543). Task links use the same id
(createTask -> LinkSession, dispatcher.go:2601, 2618).

Finding A1 (major): NO cross-validation when the client supplies BOTH
session_id and conversation_id. buildAck uses req.ConversationID verbatim
whenever non-empty (chat_submit.go:149-156 only resolves when empty); the ack
still echoes req.SessionID (chat_submit.go:222). A client (or a buggy GUI) can
bind a turn to conversation B while claiming session A; the turn's work,
working dir, and task links all follow B, and WS routing follows A
(handler.go:929-932, 1284-1300). Same gap in handler.go:824-831.
Fix sketch: when both ids are present, verify
sessionStore.Get(session_id).ConversationID == conversation_id and reject or
correct the mismatch in buildAck.

### A2. Working directory resolution (precedence is sound; fallbacks are not)

One precedence: WorktreePath > ProjectPath > DetectionContext.CWD
(internal/session/working_dir.go:43-56). Dispatcher adds: session ProjectID ->
loopManager.ResolveProjectPath (dispatcher.go:859-862), then
daemon.default_working_dir last resort (dispatcher.go:864-866, wired at
internal/daemon/components.go:4400), then ("", WorkingDirFromNone)
(dispatcher.go:867). Pinned by tests
(internal/agent/dispatcher_workdir_test.go:31,47,72,88,103). ChatHandler has
the same chain minus the ProjectID hop (handler.go:3591-3599,
SetDefaultWorkingDir handler.go:3606-3612, wired components.go:5031).
Per-session fence factory mints a fresh fence rooted at the session dir
(dispatcher.go:892-896, injection at dispatcher.go:3619-3625 and 3678-3684;
handler.go sessionLoop fence at handler.go:3519-3527 region). The old shared
RootPath mutation is deleted (dispatcher.go:879-903 comments).

Finding A2 (major): singleton-mutation fallback races concurrent sessions.
When loopManager.GetOrCreateWired fails, resolveAgent mutates the SHARED
registry singleton's workingDir and returns it
(dispatcher.go:3689-3698, warn text "mutating shared singleton workingDir
(race risk)"); ChatHandler.sessionLoop does the same on creation failure
(handler.go:3534-3537, "Last resort: mutate the singleton",
h.loop.SetWorkingDir(workingPath)). Two
sessions failing creation concurrently run turns in each other's project dir.
Fix sketch: on GetOrCreateWired failure, fail the turn with an actionable
error instead of retargeting the shared loop.

Finding A3 (minor): default-dir fallback masks a broken project binding. A
session WITH ProjectID whose project row vanished (ResolveProjectPath returns
"") silently falls to daemon.default_working_dir (dispatcher.go:859-866) and
the turn writes into the default dir, not the user's project. The source enum
(WorkingDirFromDefault, working_dir.go:18-20) is logged only at Debug/Warn
sites (dispatcher.go:3584-3588). Fix sketch: when sess.ProjectID != "" but no
path resolves, surface a user-visible notice on the turn instead of silently
using the default.

Finding A4 (major): step-job fallback runs on the interactive daemon loop
with no dir binding. AgentJobProcessor.Process falls back to p.agentLoop when
registry.GetForTask errors (internal/daemon/components.go:11165-11173) or
job.AgentID is empty (components.go:11219-11220). The working-dir injection
(components.go:11182-11183) is INSIDE the GetForTask success branch only, so
the fallback loop keeps whatever dir it had (the daemon singleton is also the
interactive chat loop, see the F14 comment at components.go:11236-11240).
A step job can therefore execute file tools in the daemon's stale/empty dir
or a dir left over from a prior interactive turn.
Fix sketch: resolve wd before loop selection and apply it to the fallback
loop per-turn via ctx, or refuse the job when no loop+dir can be bound.

Finding A5 (minor): step-job payload SessionID hint lacks thread unwrap.
resolveStepWorkingDir's LinkedSessions chain DOES unwrap thread ids
(session.ResolveThreadConversationID, components.go:11051-11052), but the
payload-hint fallback uses plain Get/GetByConversationID
(components.go:11004-11013). A thread-scoped session_id in the job payload
misses both lookups and returns "" even though the owning session has a
project dir. Fix sketch: use ResolveThreadConversationID in the hint branch
too (one-line swap).

### A3. Thread/topic resolution

ThreadRouter is wired unconditionally when a session store exists
(components.go:4378-4384; SetThreadRouter dispatcher.go:805-810).
GetThreadConversationID detects a topic from the input via keyword scoring
(internal/agent/topic_detector.go:59-116, default "general"), keys the thread
on sess.ID (thread_router.go:131-140, the tui-steer-01 fix), and returns
thread.ConversationID (thread_router.go:151). Thread conv ids are minted as
s.ConversationID + "-" + threadID where threadID = "thread-<slug>-<last4 of
session id>" (internal/session/session.go:186; topic_detector.go:125-131),
i.e. conv-<hex>-thread-<topic>-<xxxx>. Unwrap helper trims at the LAST
"-thread-" and re-looks the base; fail-safe to nil, never wrong-session
(internal/session/thread_resolve.go:95-124). Slug sanitization prevents
separator injection (thread_resolve.go:36-56).

The dispatcher resolves threads TWICE per turn: ClassifyAndRoute on the full
input (dispatcher.go:953-962) and RouteToAgent on Intent.Summary
(dispatcher.go:3067-3078). RouteToAgent keeps sessionConversationID
(pre-thread) for session lookups, task links, digest, and wd
(dispatcher.go:3046-3066, 3145-3162, 3213-3219, 3303-3309); the thread id is
used ONLY for the LLM continuity lane and the active-queue key
(dispatcher.go:3076, 3114, 3322-3324). /plan slash route deliberately links
the session-level id (dispatcher.go:1019-1028), pinned by
dispatcher_plan_slash_thread_test.go:33.

Finding A6 (major): the two thread resolutions can disagree. ClassifyAndRoute
detects the topic from the FULL input (dispatcher.go:955) while RouteToAgent
detects from Intent.Summary, which is the first sentence or first 100 chars
(extractSummary, dispatcher.go:4206-4212; call at dispatcher.go:3068). A
message whose topic keyword sits past char 100 gets memory/digest keyed to
thread X in ClassifyAndRoute but executes and registers its queue under
thread Y in RouteToAgent. Continuity and steering then target different
thread ids for the same turn. Fix sketch: resolve the thread id once in
ClassifyAndRoute, carry it on DispatchResult, and have RouteToAgent reuse it.

Finding A7 (minor): unknown/malformed thread ids degrade to an unbound turn,
never an error. A chat.submit with conversation_id = "conv-unknown-thread-x-1"
passes through buildAck unchanged (chat_submit.go:149-159), the thread router
returns it as-is when no session owns it (thread_router.go:121-129),
resolveAgent's session lookups miss (dispatcher.go:3543-3550), and the turn
runs on the registry singleton with NO working dir (dispatcher.go:3704-3721).
The user gets a normal-looking reply while every filesystem tool refuses or
writes nowhere. ResolveThreadConversationID itself is fail-safe
(thread_resolve.go:76-82) - the defect is the silent degradation upstream.
Fix sketch: when a conversation_id contains "-thread-" but resolves to no
session, reject the submit with an actionable note.

### A4. Queue worker binding (which worker claims a job)

Workers claim via qh.Claim(ctx, w.ID, w.Capabilities, w.AgentID)
(internal/worker/worker.go:262). Claim semantics: job.AgentID is a SOFT
PREFERENCE; an empty-agentID worker may claim ANY pending job including
pinned ones (internal/queue/store.go:655-665, query at 690-700); interactive
jobs sort first (store.go:685, 698; index store.go:262-268). Pool construction
adds one worker per executor agent plus general workers
(components.go:6298-6320). Execution identity is task-scoped, not
session-scoped: registry.GetForTask(job.AgentID, job.TaskID) gives each task
its own loop (components.go:11165; registry.go:584-615), and the persona rides
the payload. The session binding enters ONLY through wd resolution
(task.LinkedSessions -> session, components.go:11031-11073) and the payload
SessionID hint (components.go:10989-11016). Step jobs run under a synthetic
conversation id "step-<task>-<step>" (components.go:11273), so they never
register an active queue under the user's conversation.

Finding A8 (minor, by-design but user-visible): work is NOT bound to the
originating session's worker. Any pool worker can execute any session's step
(store.go:655-665); isolation depends entirely on LinkedSessions/payload
session resolution succeeding. When both miss (e.g. A5's thread-scoped hint,
or a task linked to a deleted session), the job runs with wd="" and the loop
keeps its previous dir (SetWorkingDir is only called when wd != "",
components.go:11182-11183) - a stale-dir execution rather than an actionable
refusal. No path was found where a job executes under ANOTHER session's
conversation memory (task-scoped loops + synthetic step conv ids prevent
that). Fix sketch: when wd resolution returns "", fail the step with
ErrNoWorkingDir-style guidance instead of running unbound.

### A5. Follow-up continuation and the plan-compiler gap (VERIFIED STILL OPEN)

Decision chain for a follow-up message in a conversation with an
active/parked task:

1. Active RUNNING loop: RouteToAgent finds the active queue
   (dispatcher.go:3114) and steers or follows-up per shouldSteer
   (dispatcher.go:3177-3207; heuristic table dispatcher.go:53-81, function
   85-98). This continues the prior turn. BUT see B1/B2: the queue is closed
   after the first turn and shared across sessions, so this lane is fragile.
2. Interrogative follow-up about prior work: answered from the stored task
   result, no new task (shouldCreateTask gate dispatcher.go:2527-2534;
   RecallAnswer branch dispatcher.go:3235-3257).
3. Parked PLANNING task (plan compiler on): NO active queue exists (the
   planner returned after seeding the draft; no loop is running). An
   imperative follow-up ("also add X", "change the approach") classifies as
   code/plan/etc., shouldCreateTask=true (intent.go:190-199), a NEW task is
   created (dispatcher.go:1449-1451), async-dispatched
   (handler.go:1091-1218), and StrategicPlanner.Plan seeds a NEW brainstorm
   draft on the NEW task (internal/agent/strategic.go:919-975: planCompilerFlag
   branch calls seedDraftFromRequest and returns; task stays StatePlanning,
   strategic.go:794).

The plan-compiler follow-up routing gap is CONFIRMED at HEAD:
- plans.plan_compiler_enabled wiring: internal/config/schema.go:743,
  internal/daemon/daemon.go:2180-2182 -> StrategicPlanner.SetPlanCompilerEnabled
  (internal/agent/plan_draft.go:59-74).
- SaveDraft is reachable ONLY from the plan.draft RPC
  (internal/rpc/plan_seal.go:93-94, 224-227; daemon wiring
  internal/daemon/plan_seal_wiring.go:198) and from seedDraftFromRequest
  (plan_draft.go:483-489). grep-verified: no dispatcher/handler call site
  routes a chat follow-up into SaveDraft or into the parked task.
- ClassifyAndRoute has no "session has a planning task -> refine its draft"
  branch. The only session-state awareness is the one-way quickplan UPGRADE
  gate (internal/agent/session_state_gate.go:20-65, 92-106), which requires
  lexical orchestration cues and never attaches to an existing draft.
- routeToPlan unconditionally creates a NEW plan per call
  (dispatcher.go:2158-2178); dispatcher_plan_lifecycle_test.go:24 pins only
  the draft->submitted lifecycle, not follow-up continuation.
- PlanModeController (/plan toggle) steers trailing text back through
  ClassifyAndRoute as a NORMAL turn (dispatcher.go:993-1004;
  internal/tools/builtin/plan_mode.go:156-181; bridge
  internal/daemon/dsh_wiring.go:530-543) - it injects a plan:policy prompt
  section, it does not refine the parked draft either.

Finding A9 (critical, UX): with plan_compiler_enabled=true, every chat
follow-up while a task parks in "planning" creates a NEW task and a NEW
draft; the user's refinement never reaches the draft they were shown. The
only refinement surface is the explicit plan.draft RPC, which the chat
clients do not call for plain messages. Evidence: dispatcher.go:1449-1451 +
2592-2624 (new task), strategic.go:924-975 (new draft), plan_seal.go:224-227
(only SaveDraft entry), absence grep above.
Fix sketch: in ClassifyAndRoute, before task creation, look up the session's
StatePlanning task with a plan_draft metadata key; when present and the input
is not a new-work command, route the text to SaveDraft/plan critique on that
task and reply with the updated draft instead of creating a task.

---

## SECTION B - STEERING/FOLLOWUP SEMANTICS

Surfaces (all verified):
- RPC: chat.steer / chat.followup / chat.queue_status / chat.queue.restore
  (internal/rpc/queue.go:26-29). Handlers call MessageQueue DIRECTLY via
  AgentRegistry.GetActiveQueue (queue.go:61-66, 98-103, 130).
- HTTP: POST /api/v1/chat/steer, /chat/steer-explicit, /chat/followup,
  GET /api/v1/chat/queue/{id}, /api/v1/queue/status/{id}, /queue/steer,
  /queue/followup (internal/comm/http/server.go:1679-1681, 1713;
  api_handlers.go:2340-2500). All delegate to ChatService.Steer/FollowUp/
  GetQueueStatus (internal/services/chat_service.go:326-388), which also do
  a raw GetActiveQueue lookup (chat_service.go:333, 351, 371).
- Dispatcher lane: chat.submit -> RouteToAgent queue injection
  (dispatcher.go:3114-3208); SteerActiveAgent/FollowUpActiveAgent
  (dispatcher.go:4934-4957) are used by the delegate tool
  (internal/tools/builtin/delegate_control.go:160), not by chat RPC.
- Loop side: steering drained before each LLM call and injected as a user
  message (internal/agent/loop.go:5126-5138); follow-ups drained at the
  natural stop and appended after the assistant reply, looping again
  (loop.go:6512-6530). Queue registered per RunOnce under the turn's
  (thread-scoped) conversation id (loop.go:3480-3519), unregistered+CLOSED
  in defer at turn end (loop.go:3525-3528 -> registry.go:2059-2074).

### B1. Steering after the loop closed / turn completed

GetActiveQueue is an exact map lookup, no remap (registry.go:2078-2088).
After turn end the entry is deleted AND the queue closed
(registry.go:2063-2073). Therefore:
- RPC chat.steer after completion: q==nil -> agent.ErrQueueNotFound, exact
  string "queue not found" (internal/agent/queue_errors.go:9; returned at
  rpc/queue.go:62-64). The client gets a synchronous RPC ERROR, not a silent
  drop and not a receipt.
- HTTP steer after completion: ChatService.Steer wraps it as
  "no active agent for conversation %s: queue not found"
  (chat_service.go:335). handleServiceError has NO case for
  agent.ErrQueueNotFound (it only maps services.ErrNotFound,
  api_handlers.go:184-185; services/errors.go:19 is a DIFFERENT sentinel),
  so it hits the default branch: HTTP 500 "internal server error"
  (api_handlers.go:222-224).

Finding B1 (major): steering an idle/completed conversation returns HTTP 500
"internal server error" instead of 404/409 with the real reason; the detailed
error is swallowed (api_handlers.go:223-224 logs and replaces the message).
RPC clients see "queue not found"; HTTP clients see a generic 500. Same for
followup (chat_service.go:353). Fix sketch: map agent.ErrQueueNotFound (and
ErrQueueClosed) in handleServiceError to 404/409 with err.Error().

Finding B2 (critical): the steering queue is closed after the FIRST turn of
each loop and never reopened, so mid-turn steering only works on turn 1.
MessageQueue.Close is a one-way CAS (internal/agent/queue.go:400-403); grep-
verified there is NO reopen/reset path anywhere (no closed.Store(false), no
per-turn NewMessageQueue: the only construction sites are queue.go:204 and
registry.go:986 at loop CREATION). RunOnce re-registers the SAME l.queue
every turn (loop.go:3513) after the previous turn's defer closed it
(loop.go:3527 -> registry.go:2073). Consequences from turn 2 onward on any
cached loop (Manager caches per session+agent, manager.go:109-127):
- chat.steer/chat.followup find a non-nil but CLOSED queue; q.Steer returns
  ErrQueueClosed (queue.go:227-228) -> RPC error "steer failed: queue is
  closed" (rpc/queue.go:66-67); HTTP 500 via B1.
- A chat.submit follow-up hits RouteToAgent's IsClosed branch
  (dispatcher.go:3164-3168) and FALLS THROUGH to run a new agent turn on the
  same conversation while the current turn is still executing - concurrent
  turns on one loop instead of steering.
- The TUI queues the message locally as follow-up (agentActive is true from
  lifecycle events, internal/tui/models/chat.go:1920-1931) and shows
  "[follow-up] error: ..." (chat.go:1631-1636) - the user's mid-turn message
  is rejected on every turn after the first.
Fix sketch: mint a fresh MessageQueue at the start of each RunOnce (or add
an explicit Reopen under the registry lock) so the registered queue is
always open for the duration of the turn.

Finding B3 (critical): session loops cloned from one registry template SHARE
a single MessageQueue instance. Manager.GetOrCreateWired builds clones from
template.ConfigSnapshot() (manager.go:153), and ConfigSnapshot includes
WithMessageQueue(l.queue) (loop.go:10105-10107) - the template's queue
POINTER, created once per agent at registry.go:986. Two concurrent sessions
routed to the same agent (e.g. coder) register the SAME queue under two
conversation ids (loop.go:3513; registry.go:2042-2054). A steer addressed to
session B lands in the queue session A's loop drains at its next iteration
(loop.go:5128-5130): cross-session message delivery, and B2's close by
session A's turn end also kills session B's queue mid-turn.
Fix sketch: exclude the queue from ConfigSnapshot and construct a fresh
MessageQueue per cloned loop (mirror registry.go:976-987 in the clone path).

Finding B3b (major): in DIRECT mode (no dispatcher wired) session loops are
cloned from the daemon template h.loop (handler.go:1246;
GetOrCreateWired(conversationID, workingPath, h.loop) at handler.go:3527),
and the daemon
template is built WITHOUT WithMessageQueue (components.go:2355-2670,
grep-verified zero queue options; NewAgentLoop at components.go:2670).
ConfigSnapshot then clones a NIL queue (loop.go:10107), RunOnce skips queue
registration entirely (loop.go:3482 requires l.queue != nil), and
chat.steer/chat.followup return "queue not found" for EVERY direct-mode
turn, first turn included. The [agent.queues] enabled=true default
(config/schema.go:4619-4626) only reaches registry-built loops
(registry.go:957-987), not the daemon template. Fix sketch: wire
WithMessageQueue onto the daemon template (or per-clone) when queues are
enabled.

### B2. Thread-id remap on steer: NOT remapped, fails as "queue not found"

The active-queue map is keyed by the THREAD-scoped conversation id the loop
ran under (dispatcher.go:3076 -> 3322-3324 -> loop.go:3513). Every steer/
followup/status surface looks up the RAW client-supplied id with no unwrap:
rpc/queue.go:61, 98, 130; chat_service.go:333, 351, 371; registry.go:2082.
session.ResolveThreadConversationID exists (thread_resolve.go:95) and is
used for SESSION lookups (handler.go:3483, components.go:11051) but is NEVER
applied to queue lookups (grep-verified).

The TUI holds the SESSION-level conversation id (chat.go:2669) and sends
chat.steer/chat.followup with exactly that id (chat.go:3540, 3552 ->
internal/tui/rpc.go:1091-1108). The daemon-side agent is active under
conv-X-thread-work-ab12 while the TUI steers conv-X. The lookup misses.

Finding B4 (critical): steering from the TUI (the primary interactive
client) fails with "queue not found" whenever the active turn was
thread-routed - which is the DEFAULT (ThreadRouter wired unconditionally,
components.go:4378-4384; every non-general topic mints a thread id,
thread_router.go:131-151). The tui-steer-01 fix repaired only the agentActive
DISPLAY (prefix match, chat.go:3479-3493, 1588-1615); the steer RPC itself
still sends the bare session conv id. Exact error: "queue not found"
(queue_errors.go:9) via rpc/queue.go:63; HTTP: 500 per B1. Conversely, a
client that DOES send the thread id gets no remap either: if the turn ran
under the session-level id (general thread migration reuses
sess.ConversationID, thread_router.go:71-88), the thread id misses the map.
Fix sketch: in GetActiveQueue (or a wrapper used by all four surfaces), try
the exact id, then the thread-unwrapped base, then any registered key with
prefix base+"-thread-" belonging to the same session; reject ambiguous
multi-thread matches explicitly.

### B3. queue_status semantics

RPC chat.queue_status returns {steering_depth, followup_depth, is_active,
generation} (rpc/queue.go:140-146); for an unknown id it returns zeros with
is_active=false and generation from the lookup (rpc/queue.go:130-138) - it
NEVER 404s, including for thread ids (same raw lookup, no remap). HTTP
GetQueueStatus behaves identically (chat_service.go:362-388; endpoints
api_handlers.go:2404-2427, 2473-2500). QueueStatus.IsActive is just
!closed (queue.go:450-459).

Finding B5 (major): queue_status cannot see parked/planning tasks at all.
It reports only the in-memory steering/follow-up queues of a RUNNING loop;
a task parked in "planning" (A9) or "pending_approval" has no active queue,
so status says is_active=false, indistinguishable from "nothing ever ran".
Thread ids also report false negatives (B4). A user polling status to decide
whether to steer gets "idle" while work is parked. Fix sketch: extend the
status payload with the session's non-terminal task states (task store
lookup keyed on the unwrapped session conversation id).

Finding B6 (minor): chat.queue.restore reports FollowUpDepth as "restored"
count (rpc/queue.go:177-181) but boot recovery DELETED the rows (see B7) -
the number describes messages that no longer exist anywhere.

### B4. Acknowledgment / receipt

- RPC steer/followup: synchronous reply {"status":"queued","queue":"steer"|
  "followup"} (rpc/queue.go:70-73, 107-110). HTTP: {"status":"queued"}
  (api_handlers.go:2357, 2401), steer-explicit adds "mode":"explicit"
  (api_handlers.go:2378-2381).
- Bus events: agent.queue.steer.added / agent.queue.followup.added on
  enqueue (queue.go:251-257, 316-322; topics internal/bus/bus.go:37-38),
  injection events from the loop (loop.go:5137, 6528; publishers
  loop.go:9770-9806). TUI renders "[steering] message queued" then
  "injected" (chat.go:1617-1626).
- The receipt means ENQUEUED, not delivered. There is no delivery
  confirmation tied to the message id beyond the bus injection event, and
  no negative event when a queued message is dropped/overwritten (B8).

Finding B7 (critical): persisted follow-ups are destroyed at boot, never
re-injected. RecoverPendingFollowUps calls loadAndClear which SELECTs then
DELETEs every queued_followups row (internal/agent/queue_recovery.go:54,
82-114, delete at 109) and publishes only a COUNT event
(queue_recovery.go:66-76; wired internal/daemon/daemon.go:2568-2575). The
message CONTENT is discarded - no queue, no loop, no replay receives it.
The TUI shows "[follow-up] restored N pending follow-up message(s)"
(chat.go:1641-1643) which is false: nothing was restored. Additionally,
DrainFollowUp never deletes consumed rows (queue.go:348-371; the persister
only INSERTs, queue_persister.go:222-247) and ClearPending/LoadPending have
NO production callers (grep-verified; queue_persister.go:250-282), so
already-CONSUMED follow-ups also linger in the table and are counted as
"restored" (then deleted) on every restart.
Fix sketch: on boot, re-enqueue loaded messages into the session's next turn
(or a durable pending-followup inbox the dispatcher drains) and delete rows
only after injection; delete on DrainFollowUp consumption.

Finding B8 (major): steering is latest-wins with silent overwrite.
MessageQueue.Steer REPLACES any unconsumed steering message
(queue.go:240-245, MaxSteering=1 queue.go:32). The first sender already got
{"status":"queued"} (B4); its message is discarded without any event or
error when a second steer arrives before the next iteration drain
(loop.go:5128). Fix sketch: emit a queue.steer.replaced event naming the
dropped message id, or reject the second steer with ErrQueueFull so the
client knows.

### B5. Steer content delivery

- RPC/HTTP steer: full req.Message injected (rpc/queue.go:66;
  chat_service.go:337). Delivery = conv.AddUserMessage at the next iteration
  boundary (loop.go:5128-5137) - it does NOT interrupt an in-flight LLM call
  or tool execution; "interrupt" means next-iteration precedence.
- Dispatcher lane (chat.submit follow-up): injects result.Intent.Summary,
  NOT the full input (dispatcher.go:3180, 3194). Summary is first sentence
  or first 100 chars (extractSummary, dispatcher.go:4206-4212), even though
  DispatchResult.OriginalInput carries the full text (dispatcher.go:1460).

Finding B9 (major): a mid-turn redirect sent via normal chat (the TUI's
default follow-up path when agentActive, chat.go:1920-1931, goes through
chat.followup RPC with full text - but any client using chat.submit, e.g.
GUI/sdk, hits the dispatcher lane) is TRUNCATED to ~100 chars before the
agent sees it. Instructions past the first sentence are lost.
Fix sketch: pass result.OriginalInput (fallback Summary) at
dispatcher.go:3180/3194.

- Amendments are a SEPARATE lane, not used by chat steer: SubmitAmendment
  (dispatcher.go:4870-4906) -> AmendmentManager.Submit -> bus
  task.amend.request (internal/task/amendment_manager.go:63-95). The RPC
  task.amend.submit (internal/rpc/server.go:725-764) publishes to the same
  topic and returns {"status":"submitted"}. BUT the manager's subscription
  goroutine only LOGS received requests - the "Auto-process if handler
  registered" comment has no code under it (amendment_manager.go:189-213,
  specifically 209-211). Handlers ARE registered
  (internal/task/registry.go:183-184 -> amendment_handlers.go:28-34) and
  inject_context appends to task.ContextQuery (amendment_handlers.go:37-68),
  but nothing calls Process for bus-delivered requests; the only Process
  call sites are the synchronous handoff path (internal/agent/tactical.go:3514)
  and Dispatcher.ProcessAmendment (dispatcher.go:4909-4913), which no RPC
  handler exposes (grep-verified).

Finding B10 (major): task.amend.submit returns "submitted" but the request
is never processed - the subscribe loop drops it after a Debug log
(amendment_manager.go:209-211). inject_context/skip_step/add_step amendments
from clients are silently lost. Fix sketch: call m.Process(ctx, req.ID) in
the subscribe loop when a handler is registered, and publish the reply.

- If no active agent: steer/followup content is NOT queued anywhere durable
  on the RPC/HTTP surfaces - it errors out (B1/B4). Only the dispatcher lane
  falls through to a new turn (dispatcher.go:3164-3168, 3181-3186,
  3195-3200), and only while the queue entry still exists but is closed/full.

### B6. Race: steer between turn terminal and next submit

Sequence: loop finishes -> defer UnregisterActiveQueue deletes the map entry
and closes the queue BEFORE publishing agent.lifecycle.ended
(loop.go:3522-3528, 3560-3567; registry.go:2059-2074). turn.terminal is
published later by the handler funnel (handler.go:752-757 comment,
publishTurnTerminal). Windows:

Finding B11 (major): there is an observable window where the client still
believes the agent is active (lifecycle-ended/turn.terminal not yet
received) but the queue is already gone. A steer in that window gets
"queue not found" (rpc/queue.go:63) / HTTP 500 (B1); the TUI shows
"[steering] error" (chat.go:1620-1622) and the message is lost - it is NOT
converted into a follow-up or a new submit. The generation counter
(registry.go:2046-2054, GetQueueWithVersion registry.go:2139-2157) exists
precisely for this but NO steer surface uses it (rpc/queue.go and
chat_service.go call plain GetActiveQueue). A steer arriving just AFTER the
next submit registers a new queue lands in the NEW turn's queue - correct
only if the user intended the new turn; there is no turn-id targeting on
chat.steer (params are message/conversation_id/source only,
rpc/queue.go:46-50), so a late steer for turn N silently steers turn N+1.
Fix sketch: accept an optional turn_id/generation on steer; on mismatch or
missing queue, return a typed "turn_completed" result so the client can
re-submit as a new turn instead of losing the text.

Finding B12 (minor): liveness default documentation drift. The task context
says 120s; actual HEAD default is StaleAfterSeconds=600
(internal/config/schema.go:4949-4953), while the field comment still says
"0 -> 120" (schema.go:3927-3929) and the block comment says 600
(schema.go:3920-3921). task.progress Touches the tracked turn
(handler.go:2626-2644, subscription handler.go:368, 440-451); the watchdog
reaps stale turns as failed/turn_reaped (internal/agent/turn_watchdog.go:10-26).
A parked planning task emits no progress; its acked turn is kept alive only
by the task->turn edge (handler.go:1160) and the relay, not by steering.
Fix sketch: correct the stale comment to 600 (or normalize zero to 120 as
documented - pick one).

Finding B13 (minor): HTTP steer-explicit is not explicit. Both
/chat/steer and /chat/steer-explicit call the identical ChatService.Steer
(api_handlers.go:2352, 2373); the "mode":"explicit" reply is cosmetic.
DispatchResult.ExplicitSteerMode is consumed by shouldSteer
(dispatcher.go:85-89, 3177) but is NEVER set true in production code
(grep-verified: only dispatcher_steer_integration_test.go:186). The ctrl+s
"always interrupt" override therefore cannot force steering through the
dispatcher lane; it only changes which RPC the TUI calls, and that RPC
fails per B2/B4 anyway. Fix sketch: carry an explicit flag on
chat.request/ChatRequest into DispatchResult.ExplicitSteerMode.

---

## RANKED TOP DEFECTS

1. B2 (critical) - Steering queue closed after turn 1 and never reopened:
   mid-turn steer/followup fails on every subsequent turn of a cached loop,
   and dispatcher follow-ups fall through to CONCURRENT turns on one loop.
   queue.go:400-403, loop.go:3513/3527, registry.go:2073, dispatcher.go:3164.
2. B3 (critical) - One MessageQueue shared by all session loops cloned from
   a template: cross-session steer delivery and cross-session queue close.
   loop.go:10107, manager.go:153, registry.go:986.
3. B4 (critical) - No thread-id remap on any steer/followup/status surface;
   TUI steers the session conv id while the queue lives under the thread id:
   default-config steering fails with "queue not found".
   registry.go:2082, rpc/queue.go:61, chat.go:3540/2669, thread_router.go:151.
4. A9 (critical) - Plan-compiler follow-up gap confirmed at HEAD: chat
   follow-ups to a parked planning task create a NEW task/draft; the draft
   is refinable only via plan.draft RPC. dispatcher.go:1449/2592,
   strategic.go:924-975, plan_seal.go:224-227.
5. B7 (critical) - Boot "restore" DELETES persisted follow-ups without
   re-injection; consumed rows are never cleared, so the restore count is
   phantom. queue_recovery.go:54/109/76, queue.go:348-371.
6. B10 (major) - task.amend.submit acks "submitted" but the amendment bus
   subscriber never processes requests. amendment_manager.go:209-211.
7. B1+B5 (major) - Steer on idle/thread conversation returns HTTP 500
   "internal server error" (sentinel unmapped); queue_status cannot see
   parked/planning state and false-negatives thread ids.
   api_handlers.go:222-224, chat_service.go:335/371-379.
8. B9 (major) - Dispatcher steer/followup lane truncates user content to
   ~100-char Intent.Summary. dispatcher.go:3180/3194/4206.
9. A2+A4 (major) - Wrong-dir execution paths: singleton workingDir mutation
   fallback (dispatcher.go:3692-3698) and step-job fallback to the
   interactive daemon loop with no dir injection (components.go:11173/11220).
10. B8/B11 (major) - Latest-wins steer silently discards an acked message
    (queue.go:240-245); terminal-window steer is lost or lands in the next
    turn with no turn-id targeting (rpc/queue.go:46-50, loop.go:3522-3528).
11. A1/A6 (major) - session_id/conversation_id mismatch unvalidated
    (chat_submit.go:149-156); double thread resolution on different text
    can split memory vs execution keys (dispatcher.go:955 vs 3068).
12. A3/A5/A7/A8/B6/B12/B13 (minor) - default-dir masking, payload-hint
    thread unwrap missing, silent unbound turns on unknown thread ids,
    unbound step jobs, phantom restore count, liveness comment drift,
    cosmetic steer-explicit.
