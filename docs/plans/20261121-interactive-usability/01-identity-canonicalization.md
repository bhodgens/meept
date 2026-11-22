# Leaf 01 — Canonical chat identity + steer/followup dual-resolution

## DISPATCH INSTRUCTION

Implement this leaf fully. Do NOT commit. Do NOT run git add. Write code,
run tests, report results only. Working dir: the plan worktree (see
master.md Meta). Before editing, grep the target symbols — a sibling tree
may have landed overlapping fixes (verify-or-apply rule).

## Meta
Parent: master.md (20261121-interactive-usability).
Scope: chat identity canonicalization across submit, steer/followup,
active-queue registration, progress publication.
Dependencies: none (first of wave 1; owns internal/agent/handler.go and
dispatcher.go — later leaves in group B depend on this leaf).
Estimated context: 90K.

## Goal

01 — Canonical chat identity + steer/followup dual-resolution.

## Contract

Implements master C-1: export
`Dispatcher.ResolveActiveQueue(id string) (ActiveQueue, bool)` and
normalize submit-path identity via
`session.ResolveThreadConversationID(sessionID, GetByConversationID, Get)`
before LinkSession, queue registration, progress publication, and
terminal relays. Thread-scoped ids stay INSIDE the LLM continuity lane.

## Tasks

1. GATE (RED first): new e2e suite `e2e/suites/chat-identity/` with
   scenarios: (a) steer by PRIMARY session id against a live dispatched
   turn must not 404; (b) follow-up by primary id likewise; (c) a task
   submitted from a GUI-shaped id (session-... as conversation_id)
   links so the next turn from a TUI-shaped id sees prior context.
   Add manifest + path_map entries (internal/agent, internal/services).
   Run: `make e2e-fast-area AREA=chat-identity` — must be RED for
   missing-implementation reasons.
2. `internal/agent/dispatcher.go`: add ResolveActiveQueue — try
   GetActiveQueue(id); on miss, resolve via thread router +
   session dual-lookup (ResolveThreadConversationID), probing each
   thread's conv id. Sites: near GetActiveQueue registration
   (loop.go:3672 RegisterActiveQueue) — do not change the registration
   key; add the resolver on the READ side.
3. Route all steer/followup surfaces through it:
   `internal/services/chat_service.go:333,351` (Steer, FollowUp),
   `internal/rpc/queue.go:39-110` (handleSteer, handleFollowUp),
   `internal/agent/dispatcher.go:5208,5222` (SteerActiveAgent,
   FollowUpActiveAgent).
4. `internal/agent/handler.go:824-834`: resolve conversationID ALWAYS
   (not only when empty) to the canonical primary id; carry it into
   LinkSession (dispatcher.go:2735,2752) and digest joins
   (session_digest.go:99; dispatcher.go:3402,3465,3476).
5. Progress delivery: `internal/agent/loop.go:9802-9818` +
   progress_synthesizer.go:115 — publish agent_progress under the
   canonical submit id. Alternative if cleaner: wire the session store
   into `internal/comm/http/server.go:868-890` handleWSEvent so the
   relay resolves eventSessionID before filtering. Pick ONE site;
   document the choice in the leaf report.
6. Working-dir dual-lookup fixes: `internal/agent/dispatcher.go:3296`
   and `:3809` — swap single GetByConversationID for
   ResolveThreadConversationID (med findings; same root).
7. ChatHandler singleton race: `internal/agent/handler.go:3792-3801`
   SetWorkingDir on the shared loop — port the dispatcher's
   scopedSessionLoop pattern (dispatcher.go:3849-3860). Fail
   actionably with tools.ErrNoWorkingDir when no session loop exists.
8. Run `make e2e-fast-area AREA=chat-identity` to GREEN;
   `go test -p 2 ./internal/agent/... ./internal/services/...`.
9. Report: files changed, test results, the progress-publication site
   chosen, any sibling-tree overlaps found.

## Interface Contract Section

Exports: Dispatcher.ResolveActiveQueue. Guarantees: any of {primary
session id, conv id, thread-scoped conv id} accepted by steer/followup;
agent_progress delivered to a subscriber on the primary id.

## Self-Verification Checklist

- [ ] Gate suite RED before implementation, GREEN after.
- [ ] grep: no remaining single-key GetActiveQueue callers on
      steer/followup paths.
- [ ] handler.go resolves identity unconditionally; empty-conversation
      case unchanged.
- [ ] No new bus topics; no changes to WS schema json (no payload
      fields added).
- [ ] go build + scoped tests green in the worktree.

## Review Checklist

- [ ] Resolution order matches submit path exactly (Get, then
      GetByConversationID, then thread router).
- [ ] No mutation inside resolution (read-only lookups).
- [ ] e2e manifest entries present; suite registered in same commit
      set as code (orchestrator commits).
- [ ] No drive-by refactors in handler.go/dispatcher.go.

## Do NOT commit

The orchestrator stages and commits after review.
