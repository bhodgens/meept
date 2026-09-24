# Leaf 1 — Tier routing in Plan()

## Meta

- parent: plan-20260923-tiered-iteration
- status: planning

## Objective

`Plan()` consults `EvaluatePlanComplexity` before choosing a planning path.
TierComplex dispatches to the new self-sealed compile flow; Trivial/Standard
take the existing paths byte-identically. A second replan forces TierComplex.

## Work items

1. In `Plan()`'s `quick_plan` case (strategic.go): compute the tier FIRST.
   - TierTrivial/TierStandard → existing `planSinglePhase` path, untouched.
   - TierComplex → call the new flow entry (leaf 2). Until leaf 2 lands, log
     Warn `"tier complex but iterative planning unavailable; single-shot"`
     with a metric `strategic_planner.tier_complex_fallback=1` and proceed
     single-shot (safe dark launch).
2. Same tier check in the `plan` case BEFORE the compiler-flag branch: on
   TierComplex, the draft seeding gains a `complexity: complex` marker in the
   draft metadata so the interactive surface knows more critique rounds apply.
3. Second-replan-forces-complex: the escalation/replan paths set `IsReplan`
   already (2af298b1); add `ReplanAttempt int` on PlanRequest, set from the
   task's existing escalation level where available. EvaluatePlanComplexity
   is NOT changed (stays 3-signal); instead `Plan()` computes
   `tier = max(EvaluatePlanComplexity(req), tierFromReplanAttempt(req))` where
   attempt ≥ 2 → TierComplex. Keeps the evaluator pure and the policy local.
4. Metric `strategic_planner.tier` with label `tier=trivial|standard|complex`
   on every plan request (observability leaf will chart it).

## Interface

```go
// strategic.go
func tierForRequest(req PlanRequest) ComplexityTier {
    tier := EvaluatePlanComplexity(req)
    if req.ReplanAttempt >= 2 {
        return TierComplex
    }
    return tier
}
```

`PlanRequest` gains `ReplanAttempt int` (json `replan_attempt,omitempty`).
Callers that know the escalation level set it; 0 = not a replan or unknown.

## Pins

- TierTrivial quick_plan request → `planSinglePhase` invoked, no extra calls.
- TierStandard + malformed-then-valid output → repair retry fires (existing
  pins still pass).
- TierComplex → new-flow entry invoked (leaf 2 stub counts invocations).
- ReplanAttempt=2 single-artifact request → TierComplex (precedence pin).
- ReplanAttempt=0 → evaluator signal only.
- Metric `strategic_planner.tier` emitted per request.
