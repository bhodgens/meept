VERDICT: FAIL — 31/48 (64.58%) vs floor 86.8%

# Teacher-Mix Gate Report

Date: 2026-09-21. Ruler: 48 adjudicated replay cases
(`replay-gold.local.json5`, untracked). Workers: deepseek-v4-flash
(opencode-go) + glm-5.3-flash (Z.AI, HTTP fallback); judge:
deepseek-v4.1-flash (opencode-go), called only on worker disagreement
(11 judge calls, 37 worker-agreement cases). Zero transport errors —
all 48 cases produced final verdicts.

## Headline comparison

| system | replay accuracy | notes |
|---|---|---|
| chain-only floor (lfm-8b, measured A/B) | 86.8% (41.66/48) | the bar |
| iter-20 cascade (wave 2 + conf-guard 0.15) | 83.97% | prior best routed |
| **teacher-mix (this run)** | **64.58% (31/48)** | FAIL — 10.7 cases below floor |

The gate rule required >= 42/48. The mixture scored 31. This is not a
borderline miss; the mixture is far below both the floor and the existing
cascade.

## Per-intent results

| intent | n | correct | wrong case ids |
|---|---|---|---|
| quickplan | 20 | 11 | 2, 13, 18, 19, 20, 29, 33, 38, 44 |
| review | 10 | 6 | 6, 26, 34, 42 |
| planning | 3 | 1 | 27, 28 |
| git | 5 | 4 | 39 |
| platform | 1 | 0 | 48 |
| analysis | 3 | 3 | — |
| coding | 4 | 4 | — |
| debugging | 2 | 2 | — |

## Error classification (17 wrong cases)

- Workers agreed on the WRONG lane: 9 cases (53% of misses). Both frontier
  models made the same mistake with high confidence (0.72-0.95) —
  correlated errors, exactly the failure mode campaign lesson 16 predicts
  for two heads reading the same text.
- Judge decided wrongly after disagreement: 8 cases. In 8 disagreements
  the judge overrode at least one correct worker and picked wrong; in 3
  disagreements (7, 39-adjacent) the judge ruled correctly — net judge
  contribution on disagreements is negative.
- No agreement on a wrong lane was ever corrected (by design, the judge
  never runs on agreement): the correlated-agreement cases are structurally
  invisible to this mixture.

## Where the misses concentrate

- quickplan (9/20 wrong): the session-state ceiling (campaign hard rule 3)
  — messages like "implement tasks 7 and 8" read as plain coding without
  the plan-exists context. BOTH cloud frontier models hit this ceiling;
  it is not a model-capability gap.
- review-vs-analysis/recall boundary (5 cases): taxonomy-boundary errors,
  the same boundary the adjudication record documents for the local models.

## Honest caveats

1. n=48: one case = 2.1%. But the gap is 11 cases — an order of magnitude
   beyond ruler noise.
2. Zero-shot, single-question, argmax-only. The distillation design needs
   full per-lane probability vectors over the whole corpus; this run
   measured argmax intent only, which is the correct gate for "is there an
   edge worth distilling".
3. Worker-b ran over the HTTP fallback (Hermes Z.AI key), not the
   opencode plan key; same model id, same API family.
4. No confidence combining was used (prohibited by campaign history); the
   mixture shape tested is the user-specified workers+judge arbitration.
5. The 86.8% floor is itself a chain-of-cards figure: the local cascade
   answers with 100% backstop coverage, the mixture has no backstop stage.
   A production mixture would need the chain behind it anyway, which
   erases the cost argument.

## Gate decision (per master.md rule)

FAIL. The full-size mixture-of-models does NOT beat the existing cascade's
floor. Per the master.md decision rule: record the capability ceiling —
the ~84-87% plateau is label-bound and session-state-bound, not
capability-bound — and redirect classifier-effort to orchestrator-layer
quickplan resolution and corpus label repair.

A Jev student distilled from THIS teacher would inherit a 64.58% ceiling.
Do not proceed to distillation with this teacher.
