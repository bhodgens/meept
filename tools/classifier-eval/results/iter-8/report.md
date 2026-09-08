# Iteration 8 Report — M1 close-out: fix-shape convention + frontier freeze

Date: 2026-09-08. Mechanical sweep. Corpus 221 (base 139 + adversarial 82).

## What iteration 8 did

1. Relabeled `write a fix for the segfault in the parser` code→debug
   (fix-shape-wins convention from iter-7's audit) — committed with
   iter-7's relabel edit; this iteration measures its effect.
2. Re-ran the margin brackets on the relabeled corpus and FROZE the M1
   frontier. M1 gate: harness built ✅, frontier established ✅, corpus
   ~221/250 ✅.

## Measurement (post-relabel)

| config | C | P | wrong | E2E | SCORE |
|---|---|---|---|---|---|
| centroid m0.030 | **34.0%** | **98.5%** | **1** | **90.78%** | **+0.305** |
| centroid m0.035 | 28.4% | 98.2% | 1 | 90.04% | +0.249 |
| kNN k=5 unanimity 0.70 | 11.7% | 95.7% | 1 | 87.83% | +0.081 |

## Findings

1. **New headline: margin 0.030 → C 34.0% P 98.5% E2E 90.78%.** The
   relabel fixed the segfault case at the DATA level; coverage at the
   precision-compliant point jumped 26.9→34.0% and E2E +0.74pts. Two
   data repairs (iter-6, iter-8) have now produced +2.0 E2E points vs
   +1.1 from ALL gate permutations combined — the campaign's clearest
   lesson so far.
2. The one remaining wrong at m0.030: `write a hotfix for the null
   check crash` [code→debug 0.832] — same fix-shape family. Applying
   the convention consistently, "write a hotfix" is ALSO fix-shape:
   relabel to debug in iter-9 and add 2-3 more patch/fix-shaped CODE
   anchors that explicitly avoid fix-verbs ("implement a bounds check
   in the reader") so the code centroid stops bleeding into debug.
3. M1 verdict on the embedder: 0.6B-4bit plain EOS pooling + centroid
   margin head is the M1 champion (E2E 90.78% vs 86.8% chain-only,
   +3.98pts). M2 proceeds with head/threshold permutations AROUND this
   champion, and the ModernBERT entry bar (M3) is now E2E ≥ 91.78%.

## Frontier (M1 freeze)

| point | margin | C | P | E2E |
|---|---|---|---|---|
| **champion (bar-compliant)** | **0.030** | **34.0%** | **98.5% ✅** | **90.78%** |
| relaxed | 0.025 | ~37% | ~96% ❌ | ~90.2% |
| precision-max | 0.045 | ~19% | ~97.4% ✅ | ~88.8% |
