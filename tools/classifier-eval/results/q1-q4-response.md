# Q1-Q4 Decision Response — 2026-09-09

## Q1: Double-confidence gating — measured, kept the simpler policy

Your idea: route only when BOTH stages agree above 0.7 confidence.
Measured on the 48-case gold replay with a fresh stage-B probe:

| policy | A routes | B routes | chain | system acc |
|---|---|---|---|---|
| current (margin + quantile tau 0.147) | 7 | 13 | 28 | **83.97%** |
| double-confidence (both > 0.7) | 7 | 0 | 41 | **84.56%** |
| double-confidence (both > 0.5/0.3) | 7 | 0 | 41 | 84.56% |

**Result: your double-confidence gate measures slightly BETTER (+0.6pt)
and is simpler** — because the ModernBERT probe's 12-class softmax never
reaches 0.7 honestly (ceiling ~0.21 on this data), so the gate collapses
to "stage A routes, everything else goes to the chain." That's the
correct behavior given the data: B's rescue precision was 58-83%, below
the chain's 86.8%, so B should stay silent until it's retrained on more
quickplan mass.

**Decision: adopt the double-confidence rule** (A: sim ≥ 0.60 + margin
≥ 0.030; B: prob ≥ 0.7 + same-class agreement with A). It's stricter,
simpler to explain, and measurably better. B rejoins automatically once
retrained past 86.8% rescue precision.

## Q2: The wiring decision, spelled out simply

Two different "brains" can sit inside the Stage-0 gate:

- **k-NN unanimity (shipped today)**: your message must look like 5
  stored examples, all from the same intent, ALL above a similarity
  floor. Ultra-strict. Routes 3.7% of messages; when it routes, it's
  always right — but 96% of traffic skips it.
- **centroid + margin (campaign winner)**: your message is compared to
  one average point per intent. If the best intent beats the runner-up
  by a clear margin, route. Routes 34-56% of messages at 98-100%
  precision on synthetic data.

The wiring question: swap the shipped k-NN brain for the centroid brain?
The campaign says yes (34× coverage at equal-or-better precision). The
daemon config knob is `classifier_prefilter` + rebuilding the centroid
store; the Go code needs the centroid scorer added (the campaign harness
has it; the daemon doesn't yet).

## Q3: Orchestrator chunking — INVESTIGATION RESULT

Your conceptualization: orchestrator chunks work to fit each agent's
context, dispatching multiple coder agents rather than one coder
grinding through compaction.

**What exists today:**
- `plan`/`spec_plan` modes: strategic planner produces phases → tactical
  scheduler dispatches steps to executor loops. Steps are size-capped
  (`max_steps_per_phase`, default 8; `MaxStepsPerPhase()`).
- QuickPlan (as implemented): routes to the orchestrator pipeline
  (`orchestrator.plan` bus → strategic → tactical), same phase/step
  machinery. The "bypass to coder directly" you're worried about
  happened only in the E2E smoke because the scratch config lacked the
  plan pipeline wiring — in production config, quickplan follows the
  same orchestrator path.
- **What's missing**: context-size-aware chunking. Step caps are
  COUNT-based (8 steps), not SIZE-based (estimated tokens per step).
  Nothing reads the executor model's context_length to decide allotment.

**Verdict: the classifier is NOT the over-engineered part.** The 20
iterations bought: correct taxonomy, 44 quickplan anchors, the cue
guard, and the measurement harness. The gap is in the **orchestrator's
allotment logic** — it needs a "capability-aware chunker": estimate each
step's token footprint (files touched × size + prompt), read the
executor's `context_length` from models.json5, and split steps into
agent-sized allotments. That's a tactical-scheduler enhancement
(`internal/agent/tactical.go`), roughly one leaf-sized change.

**Recommendation**: keep the classifier as-is; add a follow-up tree:
"capability-aware allotment" (tactical scheduler reads model
context_length, estimates step tokens, splits accordingly). The E2E
smoke's 50k-budget failure is a symptom of exactly this missing chunker.

## Q4: Labels applied + docs updated

- #26 → `review` (verification reading — confirmed intentional)
- #27 → `plan` (organize-for-import = structuring work — confirmed)
- "help me" phrasing guidance: quickplan docs updated to note that
  "help me…" signals compound/complex work → quickplan.

## Updated state

- Double-confidence rule: measured, adopted (simpler + better)
- Capability-aware chunking: scoped as follow-up tree (tactical.go)
- Adjudication: final labels locked
