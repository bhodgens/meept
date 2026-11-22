# 20261119 USABILITY REMEDIATION - Plan Tree Index

Date: 2026-11-19
Source: /tmp/usability-rig/REPORT.md (interactive-usability review, 27 work
items W1-W27, 29 live findings F-ORCH-1..29, ~60 code findings).
Tree: docs/plans/20261119-usability-remediation/ (master.md + 7 branches).
Status: AUTHORED + COMPLIANCE-SCANNED (ALL TREES COMPLIANT: True; 8
orchestrators, 30 leaves, 38 files). Covers W1-W27 AND F-NEW-1..14 from the
GUI rerun (REPORT-GUI-RERUN.md). NOT yet dispatched - awaits operator
go + reconciliation against sibling bughunt waves.

## Structure

| Branch | Scope | Leaves (incl gate) | Work items |
|--------|-------|--------------------|------------|
| 01-safety | terminal honesty, queues, delivery, watchdog, failure paths, evidence review, replan storm + plans panic | 8 | W1-W7, W24-W27, F-NEW-1/2/4/10/13 |
| 02-routing | correct agent/dir/session, visible evidence, continuation, scheduler | 5 | W8, W9, W12, W13, W22 |
| 03-context | session identity, history durability, recall fidelity | 3 | W10, W11 |
| 04-clutter | empty replies, envelopes, review chatter, double render, trivial lane | 3 | W3, W15, W18 |
| 05-navigation | reply->task links, traces, evidence popover, restore, queue visibility | 4 | W14, W19, W20, W21 |
| 06-planning | approval visibility, seal surface, delineation, failure copy | 4 | W16, W17 |
| 07-bench | acceptance harness hardening + integration gate | 3 | I8, R6-R8 |

## Execution order (dependency spine)

1. Reconcile vs sibling WIP (.hermes/audits/wave-2026-11-19b/, reports/);
   merge fix/usability-gui-openexternal (W21) first.
2. Isolation worktree: .hermes/worktrees/usability-remediation.
3. Test gates (00T per branch) RED -> validate in-session -> TESTS_VALIDATED.
4. Wave A: 01-safety. Wave B: 02-routing + 03-context. Wave C: 04-clutter +
   05-navigation. Wave D: 06-planning. Wave E: 07-bench (integration, LAST).
5. Acceptance: 0 CRITICAL reproductions on isolated quota-free rig;
   terminal-delivery checker green; regression suite no new fails.
6. Merge worktree -> main ONCE at top-level completion.

## Contracts (master.md Interface Contracts)

C1 terminal payload extensions (rejection_reason, routing_notice, reply_source)
C2 task approval RPC + HTTP twins + rejected relay
C3 steering queue lifecycle (fresh per turn, clone isolation, thread remap, full text)
C4 session identity + history durability + include_tool_calls
C5 dispatch evidence (dual-key, turn_id, input_summary, HTTP route)
C6 critical event delivery (blocking publishers, critical ring, TUI reconnect)
C7 watchdog park exemption

## Open questions for operator (master.md Open Questions)

- Q1: approval threshold config key name (recommend
  orchestrator.task_approval_step_threshold, default 5, 0 disables).
- Q2: trivial fast lane default-on vs off (recommend on + regression guard).
- Q3: GUI pixel-level verification needs operator browser pass (self-signed
  cert + loopback browser block); widget tests + API checks are agent floor.

## Companion findings (KEEP OUTSIDE tree dir - scanner treats in-tree .md as leaf)

Copy /tmp/usability-rig report set to
docs/plans/20261119-usability-remediation-findings/ before dispatch:
REPORT.md, findings-orchestrator.md, audit-clients.md,
audit-routing-failures.md, audit-observability-plans.md, parts/*.md,
notes-*.md, gui-notes.md, probes/*.json, bench-results-clean/.

## Parallel work in flight

- fix/usability-gui-openexternal (worktree .hermes/worktrees/usability-gui-fix):
  W21 GUI build fix + rerun battery + REPORT-GUI-RERUN.md. Subagent
  sa-0-5b9e19f2 running. Merge/cherry-pick before 05-navigation dispatch.
