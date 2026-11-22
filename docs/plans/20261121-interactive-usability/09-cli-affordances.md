# Leaf 09 — CLI affordances: /plan subcommands, --session create, trace surfacing

## DISPATCH INSTRUCTION

Implement this leaf fully. Do NOT commit. Do NOT run git add. Write code,
run tests, report results only. Working dir: the plan worktree. Grep
first (verify-or-apply).

## Meta
Parent: master.md (20261121-interactive-usability).
Scope: CLI/TUI command affordances that the report found missing or
trap-shaped.
Dependencies: none (C-group).
Estimated context: 70K.

## Goal

09 — CLI affordances: /plan subcommands, --session create, trace surfacing.

## Contract

No daemon contract changes — consumes existing plan.approve/reject/
confirm RPCs and session.create.

## Tasks

1. GATE (RED first): TUI command tests — /plan approve <id> resolves a
   pending plan; /plan reject <id> likewise; `meept chat --session
   <new-name>` creates the session when missing (flag or default
   behavior change — pick --session-create-if-missing flag to avoid
   accidental session creation on typos, and make the error text for
   plain --session suggest the flag). RED recorded.
2. TUI /plan subcommands: command_handler.go:425-428 — implement
   /plan approve|reject|confirm <id> over internal/tui/rpc.go:917-930
   wrappers (plan.approve/reject/confirm RPCs; zero callers today).
   Keep bare /plan (planning mode) behavior; reject accepts optional
   reason.
3. Card hint correctness: models/chat.go:3264 — render the real key
   binding (keys.ViewPlans) in the plan card hint (overlaps leaf 08
   task 7; whichever leaf lands second verifies-or-applies).
4. CLI chat session affordance: cmd/meept chat.go — --session-create
   flag creating the named session before send; error text for missing
   session mentions the flag.
5. CLI plans list --state filter: cmd/meept/plans.go:140-141 — add the
   documented --state flag (client-side filter); add plans revise
   <id> subcommand over plan.revise RPC.
6. Dispatch trace surfacing (P2-1): add TUI action on the sessions
   view (key t on a selected session) calling session.dispatch_trace
   RPC; add GUI HTTP bridge route /api/v1/sessions/{id}/trace (bus
   call lane precedent: memory.reviewQueue, sdk_client.dart:2196) and a
   panel in sessions_detail. Split: TUI part in this leaf; if context
   runs tight, the GUI bridge may split to a sub-dispatch — pin the
   route shape in the report first.
7. Tests GREEN: go test -p 2 ./cmd/meept/... ./internal/tui/...;
   flutter analyze for the bridge change.

## Interface Contract Section

New HTTP route (if landed): GET /api/v1/sessions/{id}/trace returning
the dispatch_trace rows. Pin the JSON shape in the report before
implementing the GUI side.

## Self-Verification Checklist

- [ ] Gates RED -> GREEN.
- [ ] /plan approve/reject/confirm round-trip against a scratch daemon
      or hermetic fake (e2e if a suite exists; else in-process RPC
      test).
- [ ] Help text updated for new flags/subcommands.

## Review Checklist

- [ ] No accidental auto-create on typo'd --session (flag-gated).
- [ ] Lowercase UI text.

## Do NOT commit

The orchestrator stages and commits after review.
