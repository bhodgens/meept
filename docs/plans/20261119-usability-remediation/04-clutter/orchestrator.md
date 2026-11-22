# 04-CLUTTER - Contextual, Clutter-Free Replies

> **For the executing agent:** Branch orchestrator. Test gate first,
> validate in-session, then impl leaves; review IN-SESSION; commit after
> review. Do NOT implement code yourself. Worktree:
> .hermes/worktrees/usability-remediation.

## Meta

- **Role:** Branch
- **Parent:** ../master.md
- **Children:** 00T-gate.md + 2 implementation leaves
- **Scope:** Replies are user-shaped: no empty completions, no scaffolding,
  no envelopes, no review chatter, no double renders, no job-ID prose.

## Goal

Close W3/W15/W18 + U1-U8 (report axis 3): F-ORCH-4/5/6/15/21/25,
notes-server 2.4 double-render, reply-guard/planner JSON conflict.

## Architecture

Two leaves: server reply hygiene (01, handler relay/format + loop empty-
reply synthesis + review-feedback routing + orchestrator side-job noise),
client render dedupe + trivial lane (02, dispatcher fast-path + TUI/GUI
render). 01 owns daemon prose; 02 owns routing fast-path + client render.
dispatcher.go overlap: 02's fast-path is the isShortSimpleMessage region
(~1559), disjoint from 02-routing's classification region but SAME FILE -
serialize 04/02 AFTER 02-routing branch REVIEWED (group C). 01 is group A
(handler.go relay region - after 01-safety/05 which owns handler.go park/
panic regions; relay format region disjoint but serialize for safety).

## Interface Contracts

Produces: reply hygiene contract (envelope strip on relay, review feedback
to task detail only, empty-reply synthesis, side-job completion noise
gated). Consumes C1 (reply_source, task_relay marking), C6 (blocking
publishers exist).

## Child Index

| # | Document | Type | Dependencies | Est. Context | Concurrency |
|---|----------|------|--------------|--------------|-------------|
| 00T | 00T-gate.md | leaf (tests) | none | 60K | A |
| 01 | 01-server-reply-hygiene.md | leaf | 00T, 01-safety/05 | 85K | B |
| 02 | 02-trivial-lane-dedupe.md | leaf | 00T, 02-routing (dispatcher serialization) | 80K | C |

## Dispatch Protocol

Phase 0: 00T gate RED, validate in-session, commit, TESTS_VALIDATED.
Phase 1: 01 (after 01-safety/05 REVIEWED). Phase 2: 02 (after 02-routing
REVIEWED). Review in-session; commit exact paths; max 3 cycles then BLOCKED.
Briefs: worktree path, consumed contracts, no-commit/no-git-add/no-read_file/
write-once rules. Q2 (master Open Questions): trivial fast-path ships behind
config orchestrator.trivial_fast_lane default-on, regression suite as guard.

## Review Checklist

- [ ] All leaf tasks implemented; verify commands pass
- [ ] Gate tests GREEN and unmodified
- [ ] Reply-quality rubrics (07-bench) improve vs baseline; no honest-answer
      regression (naive-user-chat e2e green)
- [ ] Double-render eliminated in BOTH clients (live TUI screenshot evidence)
- [ ] No scope creep / artifacts / corruption; gofmt + dart format clean

## Coding Conventions

Per ../master.md. Branch specifics: strippers extend EXISTING
StripClaimsEvidenceOrProse/StripPromptScaffolding (never a parallel
stripper); canned fallbacks stay user-language; fast-path must not bypass
security sanitization or session binding.

## Completion Tracking Table

| Child | Status | Iterations | Review Notes |
|-------|--------|------------|--------------|
| 00T-gate | PENDING | 0 | |
| 01-server-reply-hygiene | PENDING | 0 | |
| 02-trivial-lane-dedupe | PENDING | 0 | |

## Integration Test Plan

1. `go test -p 2 ./internal/agent/... ./internal/tui/...`; flutter test.
2. e2e output-filters + naive-user-chat suites.
3. Live rig: "What is 2+2?" -> ONE brief answer render; file-write turn ->
   non-empty reply naming the path; no "## review:" in chat stream; TUI
   screenshot evidence archived.

## Test Gate

Test leaf: 00T-gate.md. Matrix (root F11):

| # | Feature | Source | Unit tests | E2E tests | Status |
|---|---------|--------|-----------|-----------|--------|
| F11a | empty-reply synthesis + envelope strip + review routing + side-job noise | 01 T1-T4 | TestEmptyReplySynthesis, TestRelayEnvelopeStrip, TestReviewFeedbackNotChat, TestSideJobCompletionQuiet | e2e/suites/output-filters (extend) | PENDING |
| F11b | trivial lane + double-render dedupe | 02 T1-T3 | TestTrivialFastLane, TestTUITaskCardDedupe, TestGUIDedupeRobust | e2e/suites/naive-user-chat (extend) + tui-flows | PENDING |

Delta log: (none yet)

## Open Questions

- Q2 ratification: fast lane default-on vs default-off. Recommend default-on
  with regression guard; operator confirms at dispatch.

## Notes

- The reply guard fighting planner JSON (F-ORCH-5 correlate) is addressed
  by 01 T2 (planner output never passes through the user-reply guard -
  it is internal machinery; guard applies to USER-facing replies only).
