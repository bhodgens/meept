# Iteration 6 Report — Base-corpus repair + margin fine-bracket

Date: 2026-09-08. Mechanical sweep + corpus repair. 5-fold seed 42.

## Corpus repairs (from iter-5's hand-audited wrong routes)

1. `create a scheduled job for cleanup` (code) — scheduling-shaped
   sentence in the code class, own-intent-absent-from-5-NN defect since
   iter-1. REPLACED with two genuinely code-shaped examples:
   `write a cron-style background worker for log rotation` and
   `create a queue consumer for email dispatch` (code class 25→26;
   corpus 136→139 base, 192 total).
2. report class 4→5 (`recap the work completed this sprint`), recall
   class 3→4 (`what did I tell you about the deployment config?`) —
   both near-synonym classes get one more distinctive anchor each.
3. NOT changed: `find comparison of monitoring tools` [search] — the
   search/analyze boundary is real product ambiguity; the chain is the
   right resolver. Left as an intentional fall-through case.

## Measurement (centroid margin fine-bracket, post-repair)

| margin | C | P | wrong | E2E | SCORE |
|---|---|---|---|---|---|
| **0.030** | **34.5%** | **96.6%** | **2** | **90.17%** | **+0.262** |
| 0.035 | 28.6% | 95.8% | 2 | 89.38% | +0.203 |
| 0.040 | 24.4% | 95.1% | 2 | 88.83% | +0.161 |
| 0.045 | 17.9% | 96.7% | 1 | 88.56% | +0.137 |

## Findings

1. **Corpus repair delivered the largest single-iteration E2E gain of
   the campaign: 88.94% → 90.17% (+1.23pts)** at unchanged coverage —
   precision 93.0→96.6% from removing label defects alone. Fixing the
   data beat every gate permutation tried so far.
2. Frontier point margin 0.030 now sits at P 96.6% — 0.4pts under the
   97% bar, with 2 wrong routes both being genuine boundary ambiguity
   (code/debug hotfix; search/analyze comparison). Neither is a gate
   defect; both are cases the LLM chain SHOULD resolve.
3. The remaining wrongs are now adversarial-by-design: master.md's
   invariant (wrong < fall-through) says these SHOULD abstain. A
   margin bump makes them abstain but at 3-4× coverage cost — the
   efficient lever is corpus growth around the boundary, not thresholds.

## Next (iteration 7)

Confusion-harvest adversarial growth: author near-boundary variants
AROUND the two known wrongs + the chat micro-cluster, per the dedup
guard (cosine > 0.95 reject). Then re-check whether new index mass
pushes the boundary cases to abstain naturally.
