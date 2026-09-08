# Iteration 4 Report — Head-family sweep: centroid / logistic / prototype-hybrid / two-stage

Date: 2026-09-08. Mode: mechanical sweep. Corpus 189, 5-fold seed 42,
plain EOS pooling (iter-3 verdict). Sweeps in iter4_sweep.py; summary in
results/iter-4/summary-*.json.

## Results

| head | best config | C | P | wrong | E2E | SCORE |
|---|---|---|---|---|---|---|
| centroid (margin gate) | margin 0.03 | **34.5%** | **93.0%** | 4 | **88.94%** | **+0.177** |
| centroid | margin 0.05 | 16.4% | 96.3% | 1 | 88.35% | +0.121 |
| centroid | margin 0.08 | 5.5% | 100% | 0 | 87.52% | +0.055 |
| centroid | margin 0.01 | 75.2% | 79.8% | 25 | 81.57% | −0.279 |
| logistic | τ 0.60–0.90 | 0% | — | 0 | 86.80% | 0.000 |
| prototype-hybrid | pw 1.00 | 8.5% | 100% | 0 | 87.92% | +0.085 |
| two-stage kNN→logistic | τ 0.75–0.92 | 9.1% | 93.3% | 1 | 87.39% | +0.049 |
| baseline kNN k=5 (ref) | floor 0.70 | 9.1% | 93.3% | 1 | 87.39% | +0.049 |

## Key findings

1. **Centroid + margin beats everything measured so far.** margin 0.03
   delivers C 34.5% at P 93.0% — E2E 88.94%, +2.1pts over the chain-only
   baseline, +1.55pts over the best kNN config. This matches
   HANDOFF-STAGE0.md §9's LOO centroid signal (96.4% when direct) that
   the kNN reshaping had abandoned.
2. **P 93.0% still violates the ≥97% bar.** The 4 wrong routes at margin
   0.03 need identification; margin 0.05 (P 96.3%, C 16.4%) and 0.08
   (P 100%, C 5.5%) bracket the precision requirement. Per-class margins
   (M2 plan) are the next lever.
3. **Logistic head is dead at this embedder.** Max softmax prob ceiling
   0.537 (diag_iter4.py) — 12-way softmax over 4-bit 0.6B embeddings
   never reaches τ ≥ 0.6; every τ variant routes nothing (C=0%). Forced
   (τ=0.10) accuracy is only 60%. Two-stage rescue inherits this: no
   rescue above τ 0.75, so it degenerates to plain kNN.
4. **Prototype-hybrid pw 1.00 routes the iter-2 wrong route correctly**
   (cherry-pick: C drops to 8.5%, wrong 0, P 100%) — the intent
   description lines absorb the chat-micro-cluster hazard.

## Orchestrator spot-check

Re-ran the centroid margin-0.03 config standalone (fresh embedder
instance through run_permutation): same numbers reproduced (deterministic
path — cached vectors, fixed folds). The 4 wrong routes are recorded in
the confusion capture for the next iteration's corpus harvest.

## Decision (recorded for M2)

- Centroid-margin becomes the M2 base head; kNN-unanimity stays as the
  precision fallback comparator.
- Per-class margin floors next: classes with confusable neighbors
  (code/debug/analyze/plan) get tighter margins; discourse classes
  (recall/report/review) get rescues or abstain-by-design.
- Logistic head abandoned unless a stronger embedder lifts the
  probability ceiling (re-test at M3 with ModernBERT).

Fix agents: none needed this iteration (measurement + harness fixes
already committed in iter-3's harness commit).
