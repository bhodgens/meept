# Meept Interactive Usability Analysis - 2026-11-21

Scope: client (TUI/GUI) interactive usability within sessions. Five axes:
context continuity between messages, routing to the correct worker/session,
contextual clutter-free responses, behind-the-scenes drill-down, and
planner/direct-execution delineation with clean failure paths.

Method:
- 4 parallel read-only code audits (session/routing, client surfaces,
  planner delineation, failure paths). Full reports:
  /tmp/meept-usability/session-routing-audit.md
  /tmp/meept-usability/client-surface-audit.md
  /tmp/meept-usability/planner-execution-audit.md
  /tmp/meept-usability/failure-path-audit.md
- Live probing on an isolated scratch rig (daemon built from HEAD f04fa5da,
  local LFM2.5-8B-A1B Q4, no cloud providers, quotas not in play):
  4 CLI probes + meept-bench smoke (1/3 pass) + steering (0/3 pass) suites.
- Orchestrator independently re-verified the highest-severity audit claims
  against HEAD (corrections noted inline).

## Executive summary

The daemon's honesty contracts mostly hold at the backend seam (quota
parking, empty-completion rotation, gate blocks, completion dedup were all
verified graceful). The interactive-experience failures cluster in four
places:

1. IDENTITY: session vs conversation vs thread ids are resolved
   inconsistently across surfaces, breaking steering, cross-surface
   context, and live progress delivery.
2. INVISIBILITY: several genuinely-good daemon mechanisms (plan.failed,
   task.planned mode, interview questions, dispatch trace, degraded
   classifier marker) never reach any client surface.
3. FALSE COMPLETION: guards detect unbacked claims but review still
   approves narration; fence-denied tools still end in "completed".
4. ONE-SHOT CLI: `meept chat` one-shots have no way to surface
   mid-turn pauses (interview questions, approvals), so they stall to the
   120s liveness timeout and exit 1.

## P0 - No request fails outright (fix first)

P0-1. Trivial recall turn hard-fails through the planner.
  Live evidence: "What was the codeword?" -> Error: planner produced no
  plan; not a single-artifact task (exit 1, task-...0004, rig daemon.log
  16:24:28). The strategic planner's empty-plan path fails the turn
  instead of degrading to direct execution or an honest reply.
  Fix: on empty-plan-for-non-artifact-task, fall back to a single direct
  step (the existing degraded-planning lane) instead of erroring; only
  error when the fallback also fails.

P0-2. Planner interview questions are invisible; turn stalls.
  Live evidence: task-...0012 (16:28:46) "Interview questions sent,
  awaiting user answers" question_count=4 - the questions exist in NO
  client-readable store (not session_messages, not needs-attention,
  designation_count 0). One-shot client dies at 120s "turn stalled";
  interactive TUI/GUI user sees nothing either.
  Fix: persist interview questions as an assistant session message (or a
  needs-attention designation) at send time; add a reply path that feeds
  answers back to the waiting planner.

P0-3. Steering/follow-up 404 when session_id != conversation_id.
  Verified: internal/services/chat_service.go:333,351 call
  GetActiveQueue(req.ConversationID) with no dual resolution, while the
  active-queue map is keyed by the loop's resolved conv id
  (internal/agent/loop.go:3672) and the submit path DOES dual-resolve
  (internal/agent/handler.go:818-834). Flutter sends the raw session id
  (chat_provider.dart:1275-1286) -> 404 -> steering text lost.
  Fix: apply the submit path's resolution (Get -> GetByConversationID ->
  thread resolution) inside ChatService.Steer/FollowUp and
  internal/rpc/queue.go:61 before GetActiveQueue, or register the queue
  under an alias set {primary, conv, thread} ids.

P0-4. False completions: narration passes review; denials still "complete".
  Live evidence: probe B (task-...0007): "Unbacked file side-effect claims
  with zero tool executions, nudging for real tool use" fired, model
  replied with literal tool-call JSON, review "Skipping full review:
  trivial task (<3 steps), using heuristic" approved it -> "completed",
  zero files anywhere. Same shape in bench open-ended-judge row. Also:
  fence denials ("access denied: ...meept-bench/...") end in status
  completed with no artifact.
  Fix: (a) the unbacked-claim guard's nudge-once-then-approve must become
  nudge-then-FAIL when zero tool executions back a claimed file effect;
  (b) any tool permission denial on the step's primary action must mark
  the step errored (the "errored steps never pass review" contract);
  (c) render the daemon's existing [unverified: no tools] label on such
  replies.

P0-5. Plan failure never reaches the chat that owns the plan.
  Corrected audit finding: plan.failed NOW EXISTS (internal/plan/manager.go
  :1158,:1696; declared critical in internal/bus/critical.go:37) - but the
  TUI chat pump handles only plan.submitting/completed/confirmed/rejected
  (internal/tui/app.go:1983) and the GUI has zero references. User sees
  the "plan ready for review" card, then silence on failure.
  Fix: add plan.failed to the TUI chat pump notification table and to the
  GUI plan-card bubble (see P1-4).

## P1 - Context, routing, delineation

P1-1. Canonicalize identity once at the chat boundary (root fix for a
  cluster). Audit evidence: tasks/digests link under whichever id shape the
  submitting surface sent (handler.go:824-834 only resolves when EMPTY),
  so a GUI-created session's follow-up from the TUI finds no prior task;
  live progress frames publish under the thread-resolved conv id
  (loop.go:9802-9818) while the WS hub exact-match filters on the
  subscribed primary id (comm/http/server.go:868-890) -> GUI turn looks
  frozen until 120s fallback.
  Fix: resolve (session_id, conversation_id) to the session row via
  session.ResolveThreadConversationID at submit; use the primary id for
  LinkSession, digests, queue registration, progress publication, and
  terminal relays. Closes P0-3's class plus 4 med findings at the root.

P1-2. Scaffolding still leaks one level deeper than the fixed chat card.
  Verified: zero StripPromptScaffolding references in internal/tui and
  ui/flutter_ui. TUI task-detail modal renders raw step descriptions
  (internal/tui/models/tasks.go:1260); GUI task detail same
  (ui/flutter_ui/lib/features/tasks/tasks_detail.dart:92).
  Fix: strip at the payload boundary (task/step RPC + HTTP responses) so
  every surface inherits it, instead of per-render patches.

P1-3. GUI verbosity control is a placebo.
  Verified: zero tier filtering in chat_provider.dart; every
  agent_progress frame renders regardless of quiet/normal/verbose. TUI
  gate is app.go:1893 (tier <= verbosity).
  Fix: drop frames with progress.tier > verbosity in the GUI progress
  subscription.

P1-4. Plan/direct mode is not visible in flight (parity + honesty).
  Audit evidence: only signal is the async ack "mode:" line
  (handler.go:2632-2662); task.planned carries mode but is an orphan
  (published, never subscribed - bus-topology doc); GUI has a TODO mode
  chip; TUI gap pinned by RED gate mode_badge_test.go.
  Fix: land the in-flight mode badge (turn.terminal + task.planned
  subscription) in both surfaces; render the plan_quality degraded marker
  in the completion card.

P1-5. /plan approve hint is a trap.
  TUI chat card says "/plan approve plan-xxxx" (models/chat.go:3264) but
  /plan takes no arguments - following it silently enters planning mode
  and misroutes the NEXT message (command_handler.go:425-428).
  Fix: implement /plan approve|reject|confirm <id> or drop the hint.

P1-6. require_approval code default false.
  Verified: internal/config/schema.go:828 plain bool; DefaultConfig sets
  no Plans block; only the shipped template sets true. Any install whose
  config lacks [plans] auto-approves plans - the condition behind the
  historical 922-executing-plan evolver backlog.
  Fix: default RequireApproval=true in NormalizePlansDefaults.

P1-7. Wrong working directory under two shapes.
  (a) Queue-lane working-dir fix consults only the conv-id index
  (dispatcher.go:3296) - GUI-shaped ids get tools.ErrNoWorkingDir
  mid-conversation. (b) ChatHandler still mutates the SHARED singleton
  loop's working dir (handler.go:3792-3801, self-described known race) -
  concurrent sessions can execute in each other's directories.
  Fix: ResolveThreadConversationID at both sites; port the dispatcher's
  scopedSessionLoop pattern into ChatHandler.

## P2 - Clutter, drill-down, polish

P2-1. Dispatch trace (why did my turn go there?) is CLI-only; no TUI/GUI
  surface, no HTTP bridge route. Surface it in both clients.
P2-2. TUI background-task boxes truncate answers to 45-50 chars
  (models/chat.go:1361-1387,1535-1545) while the daemon relay carries the
  full 12k-capped answer. Render unbounded.
P2-3. GUI hides mid-turn step errors/review feedback (failing+revising
  turn looks like silent spinning). Add transient system lines for
  task.error / review_completed payloads.
P2-4. Degraded-classifier downgrade is silent: 60s blanket cooldown after
  one failure (llm_classifier.go:587-799), heuristic_fallback recorded in
  meta but never rendered. Live rig showed EVERY dispatch at
  classification_method=fallback with off-topic replies. Append a
  one-line honesty note; render meta.classification_method in GUI footer.
P2-5. ContextTooSmall/TierFloor/generic LLM errors surface as raw machine
  strings ("llm: request does not fit..."). Map to user language at the
  terminal funnel; add one compaction-retry for IsContextTooSmall.
P2-6. Quota error with unknown schedule loses the "queued" note
  (handler.go:1448-1457 parks only when unblockAt non-zero). Synthesize a
  conservative unblockAt or append re-ask guidance.
P2-7. TUI errors share one system style - failures don't scan as errors.
P2-8. Stalled-turn wording differs TUI vs GUI ("task may still be
  running" vs "task may still complete") - unify.
P2-9. Evolver zero-phase synthesis parks plans in executing forever
  (manager.go:1186-1230 no zero-phase guard; evolver sink has separate
  store with no synthesis reclaimer, daemon.go:2558). Guard + reclaimer.
P2-10. TUI plans tab: no reject key (GUI/CLI have it); stale rows on plan
  events; wrong tab-key hint ([5] vs p). CLI plans list --state documented
  but unimplemented.
P2-11. meept chat --session <name> errors "session not found" with no
  create-if-missing; session create requires a name but chat help implies
  free-form ids. Add --session-create or name-based resolution.

## Further investigation (recommendations)

FI-1. The rig's LLM classifier NEVER accepted (all dispatches
  classification_method=fallback or heuristic_fallback, confidence 0.3-0.45,
  including on bench tasks that historically classify as llm). Determine
  whether the scratch-rig config starves the classifier lane (single
  runtime) or the boot-window cooldown poisoned it - the meept-bench skill
  documents both. This inflates every routing finding above.
FI-2. Why the coder narrates tool-call JSON instead of emitting tool calls
  under the local 8B (probe B + two bench rows): is the tool-call parser
  (llama.cpp b9660 floor) failing to parse and the text falling through,
  or is the prompt not arming tools on task-scoped loops? One transcript
 -level trace (tool_trace + raw completion) answers it.
FI-3. The turn watchdog reaps at 600s but only worker/task events touch
  the turn registry (streaming deltas do not) - a long silent LLM stream
  can be falsely reaped. Verify delta-touch coverage in the watchdog.
FI-4. Bench steering suite 0/3: controls failed too (heuristic_fallback
  routing + fence denials). Re-run after P0-4/P1-1 on a rig whose
  allowed_paths include the bench worktree root (rig config gap found:
  fence allows only $RIG/**, but bench forks worktrees under
  ~/.meept-bench/).

## Live-probe evidence index (rig /private/tmp/meept-scratch-rig.o6jXXn)

- probeA: /tmp/meept-usability/probeA-turn{1,2}.txt (codeword recall:
  turn1 off-topic reply with job-ID clutter; turn2 planner hard-fail)
- probeB: /tmp/meept-usability/probeB-turn1.txt (completed-with-zero-
  artifacts, narrated tool JSON)
- probeD: /tmp/meept-usability/probeD-turn1.txt (turn stalled 120s;
  interview questions invisible; no plans row; no files)
- bench: /tmp/meept-usability/bench-smoke/, bench-steering/ (results.jsonl
  + transcripts)
- rig daemon.log: $RIG/daemon.log (grep timestamps 16:23-16:29 for the
  cited lines)

Rig teardown when done: ~/git/meept-bench/scripts/scratch-rig.sh --down
/private/tmp/meept-scratch-rig.o6jXXn

## Verified-graceful (no action needed)

Quota parking end-to-end (park -> auto-resume -> honest give-up),
empty-completion classification+retry+rotation, tool-error degradation
with honest notes, ErrGateBlocked non-retryable envelopes, effects
fail-closed, completion dedup by queue state+attempt, boot-window
ClassifierBootReadiness parking, submit dedupe with parked
duplicate_suppressed terminals, phase-level failure publishing
plan.phase_state{failed}, rejection flow with reason + chat card.
