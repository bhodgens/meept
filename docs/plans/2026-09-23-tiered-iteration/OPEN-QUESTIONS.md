# OPEN-QUESTIONS — tiered-iteration tree

Recorded during planning; resolved items move here from master.md's open
list with their resolution. Never silently reconciled.

## 1. Critic model (RESOLVED — operator, 2026-09-23: option 1)

Critic runs on the same planner model for v1. A `critic_model` slot is added
only if the plan-quality corpus (leaf 4) shows critique misses attributable
to model capability rather than prompt shape.

## 2. Self-seal gating (RESOLVED — operator, 2026-09-23: option 1)

`plans.self_seal_enabled` gates autonomous sealing, default FALSE. The
TierComplex flow ships dark: rounds run, but quick_plan falls back to
single-shot and plan mode waits at the existing seal step. Flipping the
default is a product decision after corpus evidence, not a cleanup.

## 3. Review-verdict persistence (RESOLVED — operator, 2026-09-23: option 1)

Leaf 3's first work item is the repo discovery; both outcomes have a
specified path (persisted → read API on the review side; ephemeral → add
step-store persistence then the same read API shape).

## 4. ReplanAttempt source (RESOLVED — operator, 2026-09-23: option 1)

The escalation manager's level is recorded on task metadata; BOTH replan
paths (ReplanFailedTask and EscalationManager.triggerReplan) read the count
from there. One source of truth — no per-path best-effort. Leaf 1 wires
this; if the escalation level is not yet written to task metadata at every
replan site, leaf 1 adds the write at the missing site in the same commit.
