# Leaf 4 — Observability

## Meta

- parent: plan-20260923-tiered-iteration
- depends-on: leaves 1-3
- status: planning

## Objective

Make tiered iteration measurable so the eval corpus (#58 capability 4) has
ground truth and regressions are visible per tier.

## Metrics (strategic_planner namespace)

| metric | labels | meaning |
|--------|--------|---------|
| tier | tier=trivial\|standard\|complex | per plan request (leaf 1) |
| tier_complex_fallback | reason=flow_disabled\|transport | complex tier degraded to single-shot |
| critique_rounds | rounds=1..N | distribution of rounds used |
| critique_outcome | outcome=clean\|exhausted\|critic_fail | how the loop ended |
| seal_provenance | by=planner-self\|user | autonomous vs human seals |
| known_risks | count | histogram of unresolved advisory objections |

## Log line

One structured line at plan time, tier-aware:

```
msg="plan tier selected" tier=complex replan_attempt=2 rounds_used=2
sealed_by=planner-self known_risks=1 task_id=...
```

## Eval-corpus hook

Extend `tools/classifier-eval` with a plan-quality mode (input → expected
tier, expected step count range, expected tool hints) so planner/critic model
changes are graded like classifier changes were. Reuse the replay-corpus
discipline (scrubbed text, pinned expectations) from the classifier campaign.

## Pins

- Every metric above emitted from its code path (counter assertions via the
  existing metrics test seam).
- Log line present exactly once per plan request, with tier + task_id.
- Corpus mode: golden inputs produce expected tier + shape; scrubbed text
  rule enforced.
