# OPEN-QUESTIONS — tiered-iteration tree

Recorded during planning; resolved items move here from master.md's open
list with their resolution. Never silently reconciled.

## 1. Critic model (OPEN — default decided, slot deferred)

Critic runs on the same planner model for v1. A `critic_model` slot is added
only if the plan-quality corpus (leaf 4) shows critique misses attributable
to model capability rather than prompt shape.

## 2. Self-seal gating (RESOLVED — proposal adopted)

`plans.self_seal_enabled` gates autonomous sealing, default FALSE. The
TierComplex flow ships dark: rounds run, but quick_plan falls back to
single-shot and plan mode waits at the existing seal step. Flipping the
default is a product decision after corpus evidence, not a cleanup.

## 3. Review-verdict persistence (OPEN — leaf 3 discovery)

Whether ReviewStep verdicts are persisted per-task today is unverified. Leaf
3's first work item discovers this; both outcomes have a specified path.

## 4. ReplanAttempt source (OPEN — leaf 1 discovery)

The escalation manager knows its level; ReplanFailedTask's caller context may
not carry a count. Leaf 1 wires what is available; if neither path can supply
a reliable count, attempt tracking lands on the task metadata (escalation
level already recorded there) and leaf 1 reads it.
