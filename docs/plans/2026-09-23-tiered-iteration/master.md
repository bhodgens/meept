# Tiered Iterative Planning — master

## Meta

- plan_id: plan-20260923-tiered-iteration
- created: 2026-09-23
- status: planning
- parent: issues #57 (model residual), #58 (planner architecture gap)
- depends-on: 2e80c238 (EvaluatePlanComplexity, plan-repair retry), 2af298b1 (failure-aware replan, IsReplan)

## Goal

Both planning modes become iterative, with iteration DEPTH evaluated per job
instead of fixed. Today: `plan` mode is interactive-iterative (plan compiler,
human seal) and `quick_plan` mode is single-shot regardless of job complexity.
Target: a complexity tier routes each plan request to the right depth, and
TierComplex plans on the AUTOMATIC path iterate through a critique-refine loop
that digests review artifacts — no human in the loop, because there may not be
one.

## Design principle

Iteration depth is a function of the job, decided BEFORE the first LLM call and
re-evaluated on every replan. Trivial jobs must not pay iteration cost;
complex jobs must not get a single roll of the dice.

## Tiers (from EvaluatePlanComplexity, 2e80c238)

| Tier | Depth | Modes | Flow |
|------|-------|-------|------|
| TierTrivial | 0 extra calls | quick_plan, plan | single-shot (current path, byte-identical) |
| TierStandard | 1 repair call max | quick_plan, plan | single-shot + plan-repair retry (landed, 2e80c238) |
| TierComplex | draft-critique-refine loop | quick_plan, plan | new: self-sealed compile (below) |

Replans force at least TierStandard (landed, 2af298b1). A SECOND replan forces
TierComplex: two failures is evidence the job is not what the single-shot
assumed.

## TierComplex flow — self-sealed compile (the new piece)

Reuses the plan-compiler machinery (`internal/plan/compiler.go`,
`plan_draft.go`) with the HUMAN SEAL replaced by a SELF-CRITIQUE gate:

1. **Draft** — seed the brainstorm scaffold (existing `seedDraftFromRequest`
   shape) and ask the planner to fill goals/decisions/phases in prose.
2. **Critique** — a critic pass evaluates the draft against ARTIFACT
   EVIDENCE, not vibes:
   - ReviewStep verdicts from prior executions of similar plans in the
     session (the review pipeline already produces structured step verdicts)
   - the failure-aware replan block (landed, 2af298b1) when this is a replan
   - registry tool coverage: every phase's tool hints must name real tools
     (validation from 2e80c238, applied at draft time instead of parse time)
   - dependency sanity: no phase consumes an artifact no phase produces
3. **Refine** — the planner revises the draft addressing the critique items;
   max N rounds (config, default 2). Rounds exhausted with open critique
   items → the draft seals with a `## Known Risks` section carrying the
   unresolved items (honest degradation, never silent).
4. **Self-seal** — when the critique passes clean (or rounds are exhausted),
   the planner stamps the seal with a distinct provenance marker
   (`sealed_by: planner-self` vs `sealed_by: user`) so audits can
   distinguish automatic from human plans.
5. **Compile** — the existing zero-LLM compiler produces
   `[]plan.PlanPhaseSpec`; execution is unchanged downstream.

## Mode wiring

- **quick_plan TierComplex**: the full flow above, autonomous. Entry point:
  `Plan()` mode case `quick_plan` — tier check before `planSinglePhase`.
- **plan mode TierComplex**: same loop, but the critique summary is surfaced
  to the user with the draft (existing draft/RPC surface) and the seal
  REQUEST is still presented — the user may accept the self-seal or edit.
  Human remains the default sealer; the self-critique just does the rounds
  first so the human reviews a refined draft, not a first attempt.
- **plan/quick_plan TierTrivial+Standard**: unchanged (single-shot, repair
  retry already landed).

## Review-artifact digestion (the critique's evidence base)

New small type `PlanCritiqueInput` built by the caller (not the planner):
- `[]ReviewVerdict` — from ReviewStep for prior tasks in this session, if the
  review pipeline exposes them (needs a read API on the review side — leaf)
- `[]stepFailure` — existing shape from 2af298b1 (replans)
- `map[string]bool` valid tools — from the tool registry
- prior draft (for round ≥ 2)

The critic prompt renders these as sections; the critique output is a list of
concrete objections (each referencing a draft section), not prose.

## Children

- **Leaf 1 — tier routing in Plan()**: wire EvaluatePlanComplexity into
  `Plan()`'s quick_plan/plan cases; TierComplex dispatches to the new flow,
  others unchanged (byte-identical). Include the second-replan-forces-complex
  rule. Pins: tier → path mapping; trivial/standard paths byte-identical.
- **Leaf 2 — critique loop**: draft-critique-refine rounds (max 2, config
  `plans.complex_max_critique_rounds`), critic prompt template, Known Risks
  degradation, self-seal provenance marker. Pins: round cap, clean-pass
  short-circuit, risks section on exhaustion, provenance marker present.
- **Leaf 3 — PlanCritiqueInput assembly**: session review-verdict read API
  (review side), failure-block reuse, tool-coverage check, draft rendering.
  Pins: each input section present/absent as expected; review API nil-safe.
- **Leaf 4 — observability**: metrics (tier distribution, critique rounds
  used, self-seal vs user-seal counts, risks-exhausted count), plus the
  daemon log line at plan time naming tier and depth. Pins: metric names,
  log line shape.

## Sequencing

Leaf 1 first (routing skeleton; TierComplex can initially fall through to
single-shot with a Warn — safe intermediate). Then 3 (evidence assembly),
then 2 (the loop), then 4 (observability) last so it measures the real thing.

## Non-goals

- No agentic planner-with-tools (rejected in #58 — variance/cost).
- No change to TierTrivial/Standard paths beyond what 2e80c238 landed.
- No human-seal removal in `plan` mode (self-seal is additive provenance;
  the seal REQUEST still surfaces).
- No LLM in EvaluatePlanComplexity (stays deterministic; heuristic upgrades
  are future work behind the same interface).

## Open questions (for review)

1. Critic model: same planner model, or `critic_model` slot? Default
   proposal: same model first; slot only if the corpus shows critique misses.
2. Should TierComplex self-seal be gated behind a config flag for the first
   release (`plans.self_seal_enabled`, default false) so the flow can ship
   dark? Proposal: yes.
3. Review-verdict retention: does the review pipeline persist verdicts
   per-task today, or are they ephemeral? (Leaf 3 discovery task.)
