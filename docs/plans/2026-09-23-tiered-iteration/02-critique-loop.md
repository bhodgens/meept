# Leaf 2 — Critique loop (draft-critique-refine, self-seal)

## Meta

- parent: plan-20260923-tiered-iteration
- depends-on: leaf 1 (routing), leaf 3 (evidence assembly)
- status: planning

## Objective

For TierComplex requests: draft the plan in the brainstorm dialect, critique
it against artifact evidence, refine, and self-seal when clean — no human
required on the automatic path.

## Flow (config `plans.complex_max_critique_rounds`, default 2)

```
draft = planner.fill(scaffold, PlanCritiqueInput.context_sections)
for round in 1..maxRounds:
    critique = critic(draft, PlanCritiqueInput)     # list of objections,
                                                    # each references a
                                                    # draft section
    if critique.empty(): seal(draft, sealed_by=planner-self); break
    draft = planner.revise(draft, critique)
# exhausted with open objections:
draft.add_section("## Known Risks", critique.items())
seal(draft, sealed_by=planner-self)
compile(draft)   # existing zero-LLM compiler
```

## Critic contract

- Critic input: the draft + `PlanCritiqueInput` sections rendered by leaf 3.
- Critic output: JSON array of objections `{section, objection, severity}`
  where severity ∈ {blocking, advisory}. Parse failures on critic output =
  zero objections for the round (fail-open: never block a plan because the
  critic misformatted) + metric.
- Blocking objections prevent self-seal until fixed or rounds exhaust.
  Advisory objections roll into `## Known Risks` unconditionally.

## Model

Critic runs on the same planner model/chatter for v1. A `critic_model` slot
is deliberately deferred — add it only if the eval corpus shows critique
misses (master open question 1).

## Self-seal provenance

The seal record gains `sealed_by: "planner-self" | "user"`. `plan` mode
surfaces the refined draft + critique summary and still presents the seal
request (self-seal is the DEFAULT the user can override, not a bypass).
Draft metadata carries `critique_rounds_used` and `known_risks_count`.

## Degradation rules

- Planner draft call fails (transport) → fall through to legacy single-shot
  path with a Warn. TierComplex never hard-fails the task because the fancy
  path is down.
- Compiler rejects the sealed draft → the compile problems feed ONE more
  revise round (existing "compile errors feed another brainstorm round"
  principle), then the task fails honestly with the problems.
- Flag `plans.self_seal_enabled` (default FALSE) gates the autonomous seal:
  false = TierComplex flow runs the rounds then WAITS at the existing seal
  step (plan mode semantics) or falls back to single-shot (quick_plan mode,
  dark-launch). True = full autonomy. Ships dark by default.

## Pins

- Clean critique on round 1 → exactly 1 critic call, seal, compile.
- Blocking objection → revise → clean → seal; rounds_used=2.
- Rounds exhausted with blocking objections → Known Risks section present,
  sealed anyway, provenance=planner-self.
- Critic output malformed → fail-open (0 objections), metric recorded.
- self_seal_enabled=false + quick_plan → falls back to single-shot, Warn.
- self_seal_enabled=false + plan mode → rounds run, waits at seal step.
- Compile rejection → one revise round, then honest failure with problems.
