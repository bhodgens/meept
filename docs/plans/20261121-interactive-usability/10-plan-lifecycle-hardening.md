# Leaf 10 — Plan lifecycle hardening

## DISPATCH INSTRUCTION

Implement this leaf fully. Do NOT commit. Do NOT run git add. Write code,
run tests, report results only. Working dir: the plan worktree. Grep
first (verify-or-apply — the 06-planning sibling tree names leaves that
overlap; verify what landed).

## Meta
Parent: master.md (20261121-interactive-usability).
Scope: require_approval default, evolver zero-phase guard, evolver
synthesis reclaimer, stalled-execution sweep.
Dependencies: none (C-group).
Estimated context: 90K.

## Goal

10 — Plan lifecycle hardening.

## Contract

Implements master C-6. Config default change is a product decision
already ratified by the report (P1-6): RequireApproval defaults true.

## Tasks

1. GATE (RED first): unit pins — (a) DefaultConfig()/NormalizePlansDefaults
   yields RequireApproval=true; (b) Synthesize with zero phases completes
   the plan without creating an orphan parent task; (c) a plan whose
   child tasks are all terminal-or-missing for > N hours is swept to
   failed with plan.failed. e2e scenario for (b) if the harness supports
   the evolver sink shape.
2. require_approval default: internal/config/schema.go:828 +
   NormalizePlansDefaults (:792) — default true; evolver sink override
   stays; update docs/agents/opt-in-defaults.md and the config template
   drift annotation in the same change set.
3. Zero-phase guard: internal/plan/manager.go:1186-1230 Synthesize —
   treat zero phases as not-a-plan: skip task creation, complete the
   plan (the approval bridge already applied the document), honest log.
   Also pass a no-op task creator to the evolver sink
   (internal/daemon/components_wiki.go:277-281) as defense in depth.
4. Evolver synthesis reclaimer: run StartSynthesisReclaimer on the
   EvolverPlanManager (internal/daemon/daemon.go:2558 currently
   reclaims only the shared manager; components_wiki.go:174-175 opens
   the separate store).
5. Stalled-execution sweep: internal/plan/manager.go — extend
   PruneStalePlans (or add a sibling sweep) to catch executing plans
   whose child tasks are terminal-or-missing beyond a configurable
   window (default 6h): transition to failed + publish plan.failed
   (rides leaf 03's client rendering). Config knob
   plans.stalled_executing_sweep with the opt-in-defaults doc row.
6. quick_plan await_seal fall-through: strategic.go:945-948 comment fix
   + skip draft-seeding (or stamp terminal) on the hostile-skip path so
   no orphaned seeded draft remains (P2 adjacent; small).
7. Tests GREEN: go test -p 2 ./internal/plan/... ./internal/config/...
   ./internal/daemon/...; make graphs + graphs-check (no topology change
   expected — verify).

## Interface Contract Section

New config knob: plans.stalled_executing_sweep (duration, 0 disables,
default 6h). plan.failed events from the sweep ride the existing
critical topic.

## Self-Verification Checklist

- [ ] Gates RED -> GREEN; mutation check on the zero-phase guard.
- [ ] Existing installs WITHOUT [plans] block now require approval
      (integration pin against a boot with bare config).
- [ ] opt-in-defaults.md updated in the same change set.

## Review Checklist

- [ ] No behavior change for evolver document proposals (they still
      apply on approval — only the orphan task creation stops).
- [ ] Sweep window respects disabled state (0 = off pin).

## Do NOT commit

The orchestrator stages and commits after review.
