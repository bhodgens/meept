# Iteration 1 Report — Baseline re-measurement & iter-0 reconciliation

Date: 2026-09-07. Branch: classifier-iteration. Harness: tools/classifier-eval/eval_harness.py.

## What iteration 1 did

Per master.md M1: establish honest baseline numbers with the harness
(5-fold cross, fixed seed 42, fold cache), re-measure the iter-0 baseline
permutation (qwen3-0.6B-4bit EOS / kNN k=5 unanimity / floor 0.70), and
reconcile the harness numbers against iter-0's 14.7% C / 100% P.

## Headline (5-fold cross, pooled; E2E chain baseline 0.868)

| config | C | P | A | OOD-R | wrong | E2E | SCORE |
|---|---|---|---|---|---|---|---|
| kNN k=5 unanimity floor 0.70 | 3.7% | 100% | 100% | n/a (no OOD cases) | 0 | 87.29% | +0.037 |

No adversarial corpus exists yet (baseline-only iteration). No OOD cases
exist in the base corpus, so OOD-R is vacuous this iteration — adversarial
corpus growth (iters 2+) supplies them.

## Floor sweep (5-fold cross, pooled)

| floor | C | P | wrong | E2E |
|---|---|---|---|---|
| 0.90–0.75 | 0–3.7% | 100% | 0 | ≤87.29% |
| 0.70 | 3.7% | 100% | 0 | 87.29% |
| 0.65–0.50 | 4.4% | 83.3% | 1 | 86.65% |

E2E falls below the 86.8% chain-only baseline once the gate routes wrongly.
The gate only earns its latency at high floors; coverage is tiny either way.

## Iter-0 reconciliation (the 14.7%)

Reproduction attempts, same corpus, same live embedder:

| protocol | k=5 unanimity result |
|---|---|
| old `build_prefilter_centroids.py --sweep` (LOO, floor as membership) | 6/136 direct (4.4%), P 83% |
| same, τ=0.80 | 4/136 (2.9%), P 100% |
| harness 5-fold cross, floor 0.70 | 5/136 (3.7%), P 100% |
| harness 5-fold cross, floor 0.80 | 4/136 (2.9%), P 100% |

NONE of the honest-held-out protocols reproduces 14.7% C at 100% P.
The 14.7% exactly matches k=4 4-of-4 WITH SELF INCLUDED and NO floor:
direct=20/136 = 14.7%, P=100% — i.e. the iter-0 row's numbers were produced
by a warm-index, self-matching variant, not by the documented LOO
protocol. Commit 1668a8a4's doc table (docs/workflows/classifier-prefilter.md
"Measured numbers": k=5 unanimity 14.7%, 4-of-5 49%, 3-of-4 60%) is
**not reproducible** from the committed sweep code; every measured column is
2-10× optimistic vs both the old script's current output and the new
harness. The "4-of-5 49%" figure matches NO protocol variant I tried
(highest found anywhere: 24.3% warm with floor; 27.9% warm without floor).

Corroborating context: f272771c already documented that live behavior was
WORSE than the sweep predicted (verbatim repeats abstained). The live
daemon's real gate (self-match cutoff 0.999 + kNN) behaves like the
harness's cold-protocol numbers, not the doc table.

## Structural finding (bounds ALL future coverage gains)

Top-1-NN intent agreement is only 43.4%; k=5 unanimous neighborhoods are
5.1% of the corpus. 11 cases have their OWN intent absent from their
5-NN set entirely (e.g. "design the system architecture"[plan] → 5 analyze
neighbors; "the webhook signature validation fails"[debug] → 5 code
neighbors; "create a scheduled job for cleanup"[code] → 4 schedule). The
0.6B-4bit embedding space does not separate code/debug/analyze/plan well
enough for kNN unanimity to exceed ~5% coverage regardless of threshold
tuning. Corpus quality (near-duplicate intents with 1-2 example classes:
recall 3, report 4, review 4, plan 5, platform 5, schedule 5) is a
co-limiter: tiny classes can never win a k=5 unanimity vote outside their
own cluster.

## Hypothesis for iteration 2

Coverage gains will come from (a) instruction-prefixed embeddings
(HANDOFF-STAGE0.md measured 96.4% precision/20.6% margin-clear coverage
for instruction-prefixed centroids — the one strong signal already on
record), and (b) head changes that do not require unanimity, not from
threshold tuning of the current head.

## Verdict

Iter-0 log row corrected (C 14.7%→3.7% honest). Baseline established:
**E2E 87.29% at floor 0.70** — barely above the 86.8% chain-only baseline.
The campaign's coverage problem is structural, confirming master.md's plan
to attack it via embedder/head permutations and corpus growth, not τ tuning.
