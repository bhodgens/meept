# Leaf 03 — Plan failure visibility + in-flight mode badge

## DISPATCH INSTRUCTION

Implement this leaf fully. Do NOT commit. Do NOT run git add. Write code,
run tests, report results only. Working dir: the plan worktree. Grep
first (verify-or-apply; a sibling 06-planning remediation tree may have
landed parts — leaf 06-planning/01/03 names overlap).

## Meta
Parent: master.md (20261121-interactive-usability).
Scope: plan.failed + task.planned + mode badge across TUI chat pump and
GUI plan cards.
Dependencies: leaf 01 (canonical ids assumed for session-scoped events).
Estimated context: 70K.

## Goal

03 — Plan failure visibility + in-flight mode badge.

## Contract

Consumes master C-1 (canonical session ids on plan.* events). Adds zero
new topics — plan.failed already exists (internal/plan/manager.go:1158,
1696; critical in internal/bus/critical.go:37). task.planned mode badge
consumes the existing orphan topic.

## Tasks

1. GATE (RED first): e2e suite `e2e/suites/plan-visibility/`: (a) a plan
   that fails mid-execution produces a chat-visible failure notification
   in the SAME session that created it; (b) a pending-approval gated plan
   shows the approval affordance in chat (TUI semantics; GUI parity via
   widget test); (c) task.planned mode field reaches the client event
   stream. RED recorded.
2. TUI chat pump `internal/tui/app.go:1983`: add "plan.failed" to the
   plan.* case set; render a failure card (reuse models/chat.go plan
   completed/rejected card shapes ~:3400-3430) with reason.
3. TUI mode badge: the RED-gate file internal/tui/mode_badge_test.go
   pins the gap — make it GREEN (subscribe task.planned at
   sidebar.go:590 already lists it; surface mode in the header/turn
   badge per the test's expectation).
4. GUI: render plan-notification bubbles in the chat stream from
   plan_update frames (source_topic discrimination,
   internal/comm/http/server.go:1140) — mirror TUI card semantics
   (models/chat.go:3232-3307) including plan.failed and empty-session
   evolver drop. File: ui/flutter_ui/lib/features/chat/ (new widget) +
   plan_provider wiring.
5. GUI approval cards: verify plans_tab.dart:681-826 approval flow
   covers failure states; add failed state rendering if missing.
6. Stalled-turn wording unification (P2-8): adopt the TUI text
   ("task may still be running") in chat_provider.dart:905.
7. Tests: e2e GREEN; `flutter analyze` + widget test for the new bubble;
   TUI: `go test -p 2 ./internal/tui/...`.
8. Report overlap findings with the 06-planning sibling tree explicitly.

## Interface Contract Section

No new exports. Client contract: plan.submitting/completed/confirmed/
rejected/failed all render in both TUI chat and GUI chat stream;
turn cards carry mode (direct / quick plan / spec-planned).

## Self-Verification Checklist

- [ ] Gate RED -> GREEN.
- [ ] plan.failed renders in TUI chat AND GUI chat (widget test).
- [ ] mode badge test green; no other TUI tests regressed.
- [ ] Evolver document proposals render "document proposal" wording,
      never "0 phases" (reuse planCountLine).

## Review Checklist

- [ ] Parity: both surfaces handle all five plan terminal states.
- [ ] Sibling-tree overlap resolved (no duplicate implementations).
- [ ] Lowercase UI text; no em-dashes in new Dart/Go strings.

## Do NOT commit

The orchestrator stages and commits after review.
