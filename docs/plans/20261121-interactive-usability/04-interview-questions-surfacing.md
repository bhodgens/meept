# Leaf 04 — Planner interview questions surfaced as session messages

## DISPATCH INSTRUCTION

Implement this leaf fully. Do NOT commit. Do NOT run git add. Write code,
run tests, report results only. Working dir: the plan worktree. Grep
first (verify-or-apply).

## Meta
Parent: master.md (20261121-interactive-usability).
Scope: make planner interview pauses visible and answerable.
Dependencies: leaf 01 (conversation identity for the answer path).
Estimated context: 75K.

## Goal

04 — Planner interview questions surfaced as session messages.

## Contract

Implements master C-3: interview questions persisted as an assistant
session message (metadata `planner_interview` + task_id); the next user
message in the same conversation is matched to the waiting planner and
delivered as answers. No new bus topic.

## Tasks

1. GATE (RED first): e2e suite `e2e/suites/planner-interview/`: a
   multi-phase request triggers interview questions; assert (a) an
   assistant message containing the questions appears in
   session_messages for that conversation within N seconds; (b) a
   follow-up user message resumes the task to completion (artifact
   check); (c) needs-attention/designation list includes the paused
   session (if wired — else assert the message alone). RED recorded.
2. `internal/agent/strategic.go` (search "Interview questions sent"):
   at the pause point, persist the questions as an assistant session
   message via the session store (same lane as formatTaskCompletedMessage
   writes) with metadata key planner_interview and the task_id. Keep
   the existing internal state machine unchanged.
3. Answer path: on the next chat submit in the same conversation, the
   dispatcher/handler checks for a waiting interview (metadata lookup);
   if present, route the message to the planner's answer lane (existing
   internal mechanism for delivering answers — locate it; if the
   current design has NO answer ingestion, add a minimal one: the
   handler passes the user text into the strategic planner's pending
   interview and the planner proceeds or asks again).
4. CLI one-shot honesty: `meept chat` one-shot with a turn that pauses
   for interview must NOT exit 1 with "turn stalled" silently — the
   terminal/wait path returns the questions text as the turn result
   with a "reply to continue" note (cmd/meept + handler turn-terminal
   composition; keep async semantics).
5. Tests GREEN: e2e suite; `go test -p 2 ./internal/agent/...`.
6. Report: where the answer ingestion seam was (or that you added one),
   file:line.

## Interface Contract Section

Persisted shape: session_messages row (role=assistant, content=formatted
questions, metadata.planner_interview=task_id). Consumers may rely on
that key; nothing else exported.

## Self-Verification Checklist

- [ ] Gate RED -> GREEN.
- [ ] Questions visible from BOTH `meept session messages <id>` and the
      TUI/GUI session view (they render session messages - no client
      change needed; verify by test).
- [ ] Duplicate-answer protection: a second user message while no
      interview waits behaves normally.
- [ ] Watchdog: a waiting interview must not be reaped as stalled while
      younger than the interview wait window (check turn_watchdog
      interaction; extend the touch set if needed).

## Review Checklist

- [ ] Pause/resume state machine not duplicated (reuse existing).
- [ ] One-shot path returns the questions as a result, exit 0.
- [ ] No new bus topic; no WS schema change.

## Do NOT commit

The orchestrator stages and commits after review.
