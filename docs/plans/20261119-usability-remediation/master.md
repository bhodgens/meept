# MEEPT USABILITY REMEDIATION - Implementation Orchestrator (ROOT)

> **For the executing agent:** You are the orchestrator for this tree.
> Dispatch implementation agents, review their work IN-SESSION (main model),
> re-dispatch if incomplete, track completion. Do NOT implement code
> yourself. Full operational detail: skill `hierarchical-plan-execution`.
> EXECUTE IN THE ISOLATION WORKTREE (mandatory): before wave 1 run
> `git worktree add .hermes/worktrees/usability-remediation -b plan/usability-remediation`
> from the primary checkout; every leaf brief names that absolute path;
> orchestrator commits there; merge to main ONCE at top-level completion.

## Meta

- **Role:** Root
- **Parent:** none
- **Children:** 7 branch orchestrators, 30 leaves (incl. 7 test-gate leaves)
- **Scope:** Implement all findings of the 2026-11-19 interactive-usability
  review (27 work items W1-W27, 29 live findings F-ORCH-1..29, ~60 code
  findings) PLUS the GUI-rerun findings (F-NEW-1..14, REPORT-GUI-RERUN.md)
  across daemon, TUI, GUI, and the bench harness.
- **Source of truth:** /tmp/usability-rig/REPORT.md (+ audit-clients.md,
  audit-routing-failures.md, audit-observability-plans.md, parts/*.md,
  findings-orchestrator.md) AND /tmp/usability-rig/REPORT-GUI-RERUN.md
  (F-NEW-1..14 from the ff19f10f rerun: plans REST typed-nil panic, replan
  storm, serial bus-drop terminal loss, fabricated success on errored
  file_edit, security-denied write finalized completed, GUI-path completion
  unobservable). Copy the report set into
  docs/plans/20261119-usability-remediation-findings/ (SIBLING of the tree
  dir, never inside it - the structural scanner treats every in-tree .md
  as a leaf) before dispatch.
- **Base HEAD at authoring:** ff19f10f. The audit base was 1cee89d7; 10
  commits landed between (TUI/GUI rename+drill-in fixes, firewall bound on
  every path, collaboration budget opt-in). RECONCILE before dispatch
  (skill: hierarchical-planning, Plan Reconciliation): grep each leaf's
  anchors at current HEAD; mark SUPERSEDED what the sibling bughunt waves
  already fixed (check .hermes/audits/wave-2026-11-19b/ disposition ledger
  and reports/ in the primary tree).
- **Known parallel work:** fix/usability-gui-openexternal branch (worktree
  .hermes/worktrees/usability-gui-fix) carries the W21 GUI build fix +
  rerun report. Merge or cherry-pick it FIRST; leaf 05/02 then verifies
  rather than re-implements (mark SUPERSEDED if landed).

## Goal

Make interactive sessions trustworthy: every request ends in a user-visible
honest terminal; work routes to the right agent/session/dir; replies are
contextual and clutter-free; every reply links to its behind-the-scenes
detail; planning vs direct execution is visible with a clean approval and
failure path. Acceptance is outcome-level: the usability-probe bench suite
plus new terminal-delivery and reply-quality checkers go green on the
isolated rig, and the live probe battery (p1-p6) reproduces none of the
CRITICAL findings.

## Architecture

Seven branches map to the report's tiers and axes. Dependency spine:
01-safety (terminal honesty + queue lifecycle) is the foundation; 02-routing
and 03-context build on its session/queue fixes; 04-clutter and 05-navigation
are client+relay work that consumes the terminal payload contract C1;
06-planning consumes the approval seams from 01; 07-bench hardens the
acceptance harness and runs LAST as the integration gate. Contracts below
are the integration surface; leaves cite contract IDs, never restate them.

## Interface Contracts

### C1: Turn terminal payload (FROZEN - extend, never rename)

```
turn.terminal JSON keys (existing 13, frozen):
conversation_id, session_id, turn_id, task_id, intent_type, agent_id,
handler_case, status, reply, duration_ms, classified_by, model, error
status closed set: completed | failed | timeout | parked
NEW optional keys (additive, declare in schemas/ws_events.schema.json in
the SAME change - TestWSSchemaDrift hard-fails undeclared keys):
  rejection_reason string   // set when handler_case=task_rejected_relay
  routing_notice   string   // ClassificationNotice text on degraded routes
  reply_source     string   // "loop" | "task_relay" | "recall" | "synthesized"
// Owner: 01-safety/01 (relay funnel), 02-routing/02 (notice)
// Consumers: 04-clutter/01, 05-navigation/01+02, 06-planning/01
```

### C2: Task approval RPC surface

```
task.approve {task_id} -> {task_id, status:"approved"}        (exists)
task.reject  {task_id, reason} -> {task_id, status:"rejected"} (exists, BUG: approves)
NEW HTTP twins: POST /api/v1/tasks/{id}/approve, /api/v1/tasks/{id}/reject
NEW bus: task.rejected relayed to turn.terminal status=failed,
handler_case=task_rejected_relay, error=reason (C1 rejection_reason)
// Owner: 01-safety/01
// Consumers: 06-planning/01 (TUI card), 06-planning/02 (GUI card)
```

### C3: Steering queue lifecycle

```
MessageQueue: one fresh instance per RunOnce registration; NEVER shared
via ConfigSnapshot clones. GetActiveQueue(conversationID) resolution
order: exact key -> thread-unwrapped base -> session-owned
base+"-thread-*" keys (ambiguous multi-match = explicit error).
ErrQueueNotFound maps to HTTP 404 (not 500). Steer/followup injection
carries the FULL original input, not Intent.Summary.
// Owner: 01-safety/02
// Consumers: 02-routing/03 (followup continuation), 05-navigation/03 (queue status UI)
```

### C4: Session identity + history durability

```
chat.submit validation: unknown session_id/conversation_id -> accepted:false
+ actionable note (never fabricate). Both ids present -> membership check.
Conversation-only submits upsert a sessions row BEFORE persistExchange
(no FOREIGN KEY 787). session.messages.get gains include_tool_calls bool
joining GetToolCallsForMessages; Message wire shape additive:
  tool_calls []{id, name, args_summary, result_summary, at}
// Owner: 03-context/01
// Consumers: 05-navigation/01 (trace render), 07-bench/01 (history assertions)
```

### C5: Dispatch evidence surface

```
session.dispatch_trace accepts session_id OR conversation_id (dual-key),
rows gain turn_id + input_summary (populated, never empty).
NEW HTTP: GET /api/v1/sessions/{id}/dispatch-trace
// Owner: 02-routing/02
// Consumers: 05-navigation/02 (why-this-route popover), 07-bench/01
```

### C6: Critical event delivery

```
PublishBlockingT for: turn.terminal (exists), task.completed, task.failed,
task.rejected, plan.completed, plan.rejected, plan.failed (NEW event).
bus.poll subscription buffer: critical topics exempt from drop-oldest trim
(separate ring, cap 256); dropped>0 stays in-band. TUI re-subscribes on
"subscription not found" poll error. plan.failed {plan_id, session_id, reason}.
// Owner: 01-safety/03
// Consumers: 05-navigation/01 (TUI reconnect), 06-planning/01+02, 07-bench/01
```

### C7: Watchdog park exemption

```
Turns whose task state is planning | awaiting_approval | awaiting_seal are
EXEMPT from staleness reaping; draft/seal/approval activity Touches liveness.
Parked (quota/throttle/gate) turns keep existing park semantics.
// Owner: 01-safety/04
// Consumers: 06-planning (approval waits no longer false-reaped)
```

## Child Index

| # | Document | Type | Dependencies | Est. Context | Concurrency |
|---|----------|------|--------------|--------------|-------------|
| 01 | 01-safety/orchestrator.md | branch | none | - | A |
| 02 | 02-routing/orchestrator.md | branch | 01/02 (queue), 01/04 (watchdog) | - | B |
| 03 | 03-context/orchestrator.md | branch | 01/02 | - | B |
| 04 | 04-clutter/orchestrator.md | branch | 01/01 (relay), C1 | - | C |
| 05 | 05-navigation/orchestrator.md | branch | C1, C4, C5, C6 | - | C |
| 06 | 06-planning/orchestrator.md | branch | 01/01 (C2), 01/04 (C7) | - | D |
| 07 | 07-bench/orchestrator.md | branch | ALL (integration gate) | - | E (last) |

Branches 02+03 dispatch together after 01 reaches TESTS_VALIDATED+impl;
04+05 together after C1/C4/C5/C6 producers land; 06 after 01/01+01/04;
07 last. Within branches, leaf concurrency groups are FILE-DISJOINT
(declared per branch). Max 3 concurrent leaf dispatches per batch
(delegation.max_concurrent_children); raise to 6 for gate-test waves.

## Dispatch Protocol

### Phase 0: Reconcile + worktree
1. Copy /tmp/usability-rig report set to docs/plans/20261119-usability-remediation-findings/.
2. Read .hermes/audits/wave-2026-11-19b/ + reports/ disposition ledgers;
   grep every leaf anchor at current HEAD; classify leaves
   SUPERSEDED/PARTIALLY SUPERSEDED/STILL_NEEDED/BLOCKED; record in each
   branch tracking table. Merge fix/usability-gui-openexternal first.
3. Create the isolation worktree (Meta). All dispatches name its path.

### Phase 1: Test gates (per branch, before ANY impl dispatch)
Dispatch each branch's 00T gate leaf: tests ONLY, fail RED for
missing-implementation reasons. Validate IN-SESSION against the branch
Test Gate matrix (delta protocol: lesser artifact updates to greater).
Commit gate tests; branch -> TESTS_VALIDATED. Gate leaves for different
branches may run in parallel (file-disjoint packages).

### Phase 2: Implementation waves
Wave A: 01-safety leaves (groups per branch). Wave B: 02+03. Wave C: 04+05.
Wave D: 06. Each leaf: delegate_task goal="Implement all tasks from <leaf>";
context = full leaf text + consumed contract IDs/signatures + conventions +
"Do NOT commit. Do NOT git add. Explore with search_files/terminal cat, not
read_file on source; write once, never read back. Worktree path: <abs>."

### Phase 3: Review + commit (per leaf, IN-SESSION main model)
Read changed files; verify contracts; run the leaf's verify commands;
gate-test diff must be EMPTY (any gate-test edit = automatic NEEDS_FIXES).
Pass -> orchestrator stages exact leaf paths, commits
"fix(<scope>): <leaf> [W<n>]", status REVIEWED. Gaps -> re-dispatch with
findings, max 3 cycles, then BLOCKED + escalate to user.

### Phase 4: Integration (07-bench)
After all branches REVIEWED: 07 hardens the suite, runs the full battery
on the isolated rig (quota-free, local models), compares against the
2026-11-19 baseline (REPORT.md section 8 + bench-results-clean/).
Acceptance: no CRITICAL finding reproduces; terminal-delivery checker
green for every submitted turn. Then merge worktree -> main per skill.

## Review Checklist

- [ ] All leaf tasks implemented; verify commands pass
- [ ] Interface contracts satisfied exactly (C1-C7 signatures/keys/paths)
- [ ] Gate tests GREEN and unmodified (git diff on gate files empty)
- [ ] schemas/ws_events.schema.json updated for every new terminal key
- [ ] No scope creep; no debug artifacts/TODOs/placeholders
- [ ] No line-number corruption (grep '^\s+[0-9]+|' on changed files)
- [ ] gofmt -l clean on changed Go files; dart format on changed Dart
- [ ] Live-gate evidence for wiring leaves (scratch rig boot + one real
      turn; hermetic suites alone do NOT prove daemon wiring)

## Coding Conventions

- Go 1.27 (GOTOOLCHAIN pinned in go.mod); gofmt; errors wrapped with %w;
  no `_ = ` on added lines (pre-commit-errors gate); capture-and-log shape.
- Tests: table-driven Go tests beside code; e2e suites under e2e/suites/
  with manifest.json rows (suite+scenario+path_map) in the SAME commit.
- Dart: dart format before reporting; conditional-import halves must
  export IDENTICAL symbol names (the W21 bug class).
- Bus: new 2+-segment topic families need explicit relay subscription
  patterns (wildcard * is single-segment); typed SubscribeT sites are
  invisible to the topology generator - run make graphs after surface
  changes and extend the generator regex if adding typed sites.
- Commits: orchestrator only; explicit pathspecs; single-line -m messages
  (consent gate); never --no-verify without a recorded blocked-gate reason.
- ASCII-only in new doc/test fixture files (pre-commit-ascii gate).

## Requirement Traceability

| Req | Source (user words) | Plan element | Verified by |
|-----|---------------------|--------------|-------------|
| R1 | "clear context between sent messages within a session" | 03-context (C4), 02-routing/03 | p1/p6 probes; bench u-context-two-step; history roundtrip pin |
| R2 | "work gets cleanly routed to the correct location/worker/session" | 02-routing (W8 veto, R5 dir), 01-safety/02 (C3), 01-safety/06 (W4 evidence gate) | dispatch-trace assertions; p6 control; bench routing tasks; zero-evidence review pin |
| R3 | "responses are contextual and clutter free" | 04-clutter (W3/W15/W18), 03-context/02 (recall) | reply-quality llm_judge rubrics; p1 T3/T4 |
| R4 | "information about what is happening behind the scenes is easy to navigate to" | 05-navigation (C1/C4/C5 chips+traces), 01-safety/03 (C6) | TUI/GUI live pass; surface matrix re-audit |
| R5 | "planning/planner/direct execution clearly delineated and have a clean failure path" | 06-planning (C2/C7), 01-safety/01+04 | p4 probe; bench u-plan-mode-request with REAL checker |
| R6 | "No requests should fail outright" | 01-safety (W1-W7,W24-W27 + 06 evidence + 07 replan-storm/plans-panic), 02-routing/04 (W12), 03-context/01 | terminal-delivery checker: every submit -> non-parked terminal or explicit actionable rejection |
| R7 | "quotas disabled for these tests" | 07-bench rig config (llm.budget.enabled=false, local-only) | rig provenance.json |
| R8 | "run a separate meept instance" | 07-bench isolation (MEEPT_HOME rig, never live socket) | isolation guard exit-2 check |
| R9 | "fix the GUI bug then rerun tests" | W21 via fix/usability-gui-openexternal (parallel, merge first) | flutter build web exit 0 + REPORT-GUI-RERUN.md |

## Completion Tracking Table

| Child | Status | Iterations | Review Notes |
|-------|--------|------------|--------------|
| 01-safety | IMPL_IN_PROGRESS | 6 | 01/04/06 GREEN committed 6c4e17fc; 02 partial committed bf3918e6 (gate-defect GATE-ADJUDICATION-T3.md); 03 bus-GREEN/ring-RED finisher sa-1-b671d901; 05 running (sa-0-0ecabcff); 07 ralph-GREEN finisher sa-0-be1e8017 (chain_stability regression + typednil Get) |
| 02-routing | IMPL_IN_PROGRESS | 2 | 01-veto+notice COMPLETE+verified (report on disk, uncommitted); 02 metrics-GREEN rpc finisher sa-0-2ef747cc; 03 running (sa-2-d7c65d55); 04 pending |
| 03-context | IMPL_IN_PROGRESS | 2 | 02-recall GREEN committed bf3918e6; 01 rpc-GREEN finisher sa-2-38f53efe (include_tool_calls + FK787) |
| 04-clutter | TESTS_VALIDATED | 0 | gate committed 89e996e2 (T1-T8 RED); impl wave C pending |
| 05-navigation | TESTS_VALIDATED | 0 | gate committed f326aef8 (T1-T6 RED; T7 restore e2e PENDING); impl wave C pending |
| 06-planning | TESTS_VALIDATED | 0 | gate committed f326aef8 (T1-T5 RED, F13c mode badge); impl wave D pending |
| 07-bench | TESTS_VALIDATED | 0 | gate committed 45c94c0 (meept-bench worktree, checkers RED); integration wave E last |

## Integration Test Plan

1. `go build ./...` + `go test -p 2 ./internal/agent/... ./internal/rpc/...
   ./internal/session/... ./internal/tui/...` in the worktree.
2. `cd ui/flutter_ui && flutter build web --release && flutter test`.
3. `make graphs-check` (bus surface changed).
4. Rig battery (07-bench leaf 02): rebuild daemon from worktree HEAD;
   boot isolated rig (quota-free); run usability-probe suite + p1-p6
   probes serially; diff verdicts vs /tmp/usability-rig/bench-results-clean.
5. Acceptance: 0 CRITICAL reproductions; terminal-delivery checker green;
   reply-quality rubrics >= baseline; no new regressions in
   suites/regression.json (run it too - the dev gate).

## Test Gate

Test leaves: one 00T-gate.md per branch (7 total), dispatched Phase 1,
validated in-session. Root-level matrix aggregates branch matrices:

| # | Feature | Source | Unit tests | E2E tests | Status |
|---|---------|--------|-----------|-----------|--------|
| F1 | reject honesty + terminal relay | 01/01 (W1) | 01-safety/00T | e2e task-approval suite | PENDING |
| F2 | queue lifecycle + thread remap | 01/02 (W2/W23) | 01-safety/00T | e2e steering suite | PENDING |
| F3 | critical delivery + reconnect | 01/03 (W5/W26) | 01-safety/00T | e2e bus-delivery suite | PENDING |
| F4 | watchdog park exemption | 01/04 (W6) | 01-safety/00T | e2e approval-wait suite | PENDING |
| F5 | pair/plan failure terminalization | 01/05 (W7/W24/W25/W27) | 01-safety/00T | e2e failure-paths suite | PENDING |
| F5b | evidence review gate (fabricated success) | 01/06 (W4, F-NEW-2/4/13) | 01-safety/00T | e2e failure-paths suite | PENDING |
| F5c | replan storm ceiling + plans typed-nil panic | 01/07 (F-NEW-10/1) | 01-safety/00T | e2e failure-paths suite | PENDING |
| F6 | routing veto + dir binding | 02/01+04 (W8/W12) | 02-routing/00T | e2e dispatcher suite | PENDING |
| F7 | dispatch evidence | 02/02 (W9/W22) | 02-routing/00T | e2e dispatch-trace suite | PENDING |
| F8 | followup continuation | 02/03 (W13/R7) | 02-routing/00T | e2e plan-lifecycle suite | PENDING |
| F9 | session validation + history | 03/01 (W11/C4) | 03-context/00T | e2e session-storage suite | PENDING |
| F10 | recall digest | 03/02 (W10) | 03-context/00T | e2e naive-user-chat suite | PENDING |
| F11 | reply hygiene | 04/01+02 (W3/W15/W18) | 04-clutter/00T | e2e output-filters + naive-user | PENDING |
| F12 | reply->task navigation | 05/01+02 (W14/W19/W20) | 05-navigation/00T | e2e tui-flows + gui parity | PENDING |
| F13 | approval visibility + seal | 06/01+02+03 (W16/W17) | 06-planning/00T | e2e plan-critic + tui-flows | PENDING |
| F14 | bench hardening | 07/01 (I8) | 07-bench/00T | bench suite self-test | PENDING |

Delta log: (none yet)

## Open Questions

- Q1: Approval threshold config key name (plans.task_approval_threshold?)
  needs operator ratification before 06/01 freezes the schema. Recommend:
  orchestrator.task_approval_step_threshold, default 5, 0 disables.
- Q2: Trivial-question fast lane (W18) risks regressing the quickplan
  upgrade gate; gate behind a config flag default-on with the regression
  suite as the guard. Confirm at 04 dispatch.
- Q3: GUI live verification needs a browser that accepts the self-signed
  rig cert; the rerun subagent documents API-level coverage. A manual
  operator pass may still be needed for pixel-level claims.

## Frozen Documents

Leaf documents freeze at dispatch. Corrections ride dispatch context as
dated addenda. Never edit a dispatched leaf mid-execution.

## Notes

- The primary checkout has sibling WIP (internal/comm/http/*,
  internal/tui/agents_panel*) - the isolation worktree excludes it; at
  merge time re-run gates scoped to the merged diff.
- F-ORCH-28 (semaphore lease leak) root cause is UNPROVEN - 02/04 starts
  with instrumentation (I1) before fixing; do not guess the leak site.
- Report findings reference audit-base 1cee89d7 line numbers; every leaf
  carries Verified Anchors re-grepped at ff19f10f. Re-grep again at
  dispatch if HEAD moved.
