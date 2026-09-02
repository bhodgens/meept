# LLM Resilience Forest - Root Orchestrator

> **For the executing agent:** You are the FOREST root. Your job:
> (1) read DECISIONS.md + SHARED-CONVENTIONS.md, (2) dispatch each tree's
> root orchestrator in dependency order, (3) review tree-level completion
> reports in-session, (4) track forest completion.
> Do NOT implement code yourself. All implementation happens in the
> trees' leaf agents, dispatched by their tree orchestrators.

## Meta

- **Role:** Forest root (structural wrapper; the per-tree masters are the
  working orchestrators)
- **Parent:** none
- **Children:** 5 tree directories + 2 sibling reference documents
- **Scope:** Nine user features across LLM failure handling, model
  escalation, turn lifecycle, scheduling, and the model catalog,
  planned 2026-08-31.

## Goal

Consolidate and extend meept's LLM resilience: one failure-policy engine
(429/402/timeout, D4-D8), endpoint-level timeout semantics (D10),
universal turn park/resume (D9), per-agent verification-failure model
escalation (D1-D3, D14), interactive-first scheduling (D11), and catalog
completeness (context discovery + LM Studio, D12-D13).

## Child Tree Index

| Tree | Scope | Depends on | Leaves |
|------|-------|-----------|--------|
| 01-escalation | verification fix-loop model escalation | none | 4 |
| 02-failure-policy | unified 429/402/timeout policy engine | none | 5 |
| 03-turn-lifecycle | universal park/resume | 02 | 4 |
| 04-scheduling | interactive queue + slot priority | none | 3 |
| 05-catalog | context discovery + LM Studio | none | 3 |

**Dispatch order:** 01, 02, 04, 05 may start in parallel (disjoint
packages). 03 starts only after 02 reaches COMPLETE.

## Open Questions

tracked centrally in `DECISIONS.md` (Q1–Q4 with defaults). No tree is
blocked by an open question; each carries its default.

## Interface Contracts

Cross-tree contracts are frozen in `SHARED-CONVENTIONS.md` §4:
§4.1 PolicyVerdict (02 → 03/04), §4.2 EscalationModel (01), §4.3
endpoint timeout key (02), §4.4 Interactive job flag (04 → 03),
§4.5 ParkedTurnRecord (03 ↔ 02). The trees consume these; this root adds none.
(Contract consumer reality, audit 2026-09-01: tree 01 consumes only §4.2;
no 01/04 leaf cites PolicyVerdict — §4.1's arrow names the trees whose
package surfaces could consume it; tree 03's queue-requeue paths preserve
the §4.4 column rather than read it — see the CONSUMER NOTE in §4.4.)

## Child Index

| Child | Type | Dependencies |
|-------|------|--------------|
| 01-escalation/master.md | tree root | none |
| 02-failure-policy/master.md | tree root | none |
| 03-turn-lifecycle/master.md | tree root | 02 COMPLETE |
| 04-scheduling/master.md | tree root | none |
| 05-catalog/master.md | tree root | none |

## Dispatch Protocol

1. Read `DECISIONS.md` and `SHARED-CONVENTIONS.md` in full.
2. Dispatch tree roots 01, 02, 04, 05 — each is executed per its own
   master.md dispatch protocol (delegate_task of leaf agents, in-session
   review by the main model, orchestrator-only commits).
3. Gate: dispatch 03 only after 02's tracking table shows all leaves
   COMPLETE. A tree root reports its completion by updating this root's
   tracking table (orchestrator edits it after reviewing the tree).
4. Never dispatch two trees that touch the same package concurrently:
   01 and 02 both touch internal/llm + internal/agent — stagger them
   (01 first, or merge-serially) per SHARED-CONVENTIONS §7.

## Review Checklist

- [ ] Each tree's master tracking table shows all leaves COMPLETE before this root marks the tree COMPLETE
- [ ] Cross-tree contracts (SHARED-CONVENTIONS §4) unmodified, or amended here + in both consumers
- [ ] Full-suite gates: `go build ./... && go test ./internal/... -count=1`, `make analyzers`, `make graphs`
- [ ] No line-number corruption: `grep -rcE '^\s+[0-9]+\|' --include='*.go' --include='*.md' docs/plans/llm-resilience-forest internal/` returns zero
- [ ] AGENTS.md updated in the final commit of each tree (same-commit rule)

## Coding Conventions

Delegated: every tree's master carries the conventions block, sourced
from `SHARED-CONVENTIONS.md` §1-§3. This root defers to those.

## Integration Test Plan

Forest-level verification after all trees COMPLETE:

1. `go build ./... && go test ./internal/... -count=1` — full suite green.
2. `make lint-ci` (golangci-lint + analyzers + audit scripts).
3. `make graphs` — regenerated, committed, `make graphs-check` clean.
4. Cross-tree E2E: a scripted provider that (a) 429s bare → turn parks
   on the throttle plan; (b) quota-429s → parks on reset; (c) on
   recovery the coder agent's fix loop escalates to its escalation_model
   after max_fix_loops; (d) an interactive session's planner job claims
   before an older background job.
5. Reconcile DECISIONS.md open questions Q1-Q4 with what shipped
   (defaults became law or were overridden — record the outcome).

## Completion Tracking Table

| Tree | Status | Notes |
|------|--------|-------|
| 01-escalation | COMPLETE | 4/4 leaves (178cd2b3, 53e0dd48, c88cc545, a00b76a5); R1 persistent-override honored; tracking closed 2026-09-01 |
| 02-failure-policy | COMPLETE | 5/5 leaves (c9d7da28, adc44b16, 962e9924, a18802e8, 7d73b12f); tracking closed 5319ada4; pacer+policy daemon wiring b7f0a3d8 |
| 03-turn-lifecycle | COMPLETE | 4/4 leaves (285f67fe, 1d94e2da, 4739c151, 3eb6a3e3); tracking closed 9a93555b |
| 04-scheduling | COMPLETE | leaves 01-03 (ed652a9e, d7d2cddc+35c79887, b125e22b); WithPriority wiring af460b33; tracking 081898f8 |
| 05-catalog | COMPLETE | 3/3 leaves (dddb364e, 6f1a2e8a, 40e41b0e); tracking closed 2026-09-01 |

## Notes

- This root exists so the forest scans as one tree with a master.
  Per-tree execution follows each tree's own master.md.
- Siblings: `DECISIONS.md` (ratified decisions D1-D15 + open questions),
  `SHARED-CONVENTIONS.md` (frozen cross-tree contracts, conventions,
  pitfalls). Read both before dispatching anything.
