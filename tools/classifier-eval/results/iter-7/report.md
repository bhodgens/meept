# Iteration 7 Report — Confusion-harvest corpus growth (+29 cases)

Date: 2026-09-08. Mechanical sweep + harvest. Corpus 192 → 221 (base 139
+ adversarial 82). Dedup guard: all 29 candidates passed (max cosine to
existing corpus 0.917; threshold 0.95). Harvest targeted: code/debug
boundary (5 code anchors around the hotfix-family wrongs), search/analyze
boundary (5 search + 3 analyze), chat micro-cluster (3 real-chat
semantic acknowledgments), plus thin classes git +4, schedule +3,
plan +3, platform +3.

## Measurement

| config | C | P | wrong | E2E | SCORE |
|---|---|---|---|---|---|
| centroid m0.025 | 37.6% | 95.9% | 3 | 90.24% | +0.270 |
| centroid m0.030 | 32.0% | 96.8% | 2 | 90.01% | +0.249 |
| **centroid m0.035** | **26.9%** | **98.1%** | **1** | **89.84%** | **+0.234** |
| kNN k=5 unanimity 0.70 | 11.7% | 95.7% | 1 | 87.83% | +0.081 |

## Findings

1. **First PRECISION-COMPLIANT config: centroid margin 0.035 → P 98.1%
   (≥97% bar) at C 26.9%, E2E 89.84%.** The harvested boundary anchors
   pushed the coverage-precision frontier outward on BOTH axes: at
   fixed margin 0.030, precision rose 96.6→96.8% while coverage held
   32-34.5%; at 0.035, precision crossed the bar.
2. The harvest also improved the relaxed point: margin 0.025 now
   reaches C 37.6% at E2E 90.24% (SCORE +0.270, new best) — but P 95.9%
   still under the bar, so the m0.035 point is the campaign's headline
   production candidate so far.
3. Remaining wrong at m0.035: `write a fix for the segfault in the
   parser` [code→debug, 0.843] — a case I authored this iteration as
   code. Hand-audit: the corpus convention treats "fix X" phrasing as
   debug and "write/implement X" as code; this sentence straddles both.
   This is a corpus convention question, not a gate defect: EITHER
   relabel it debug (fix-shape wins) or accept it as intentional
   ambiguity the chain resolves. Decision: relabel to debug in iter-8
   and add the "fix-shape wins over write-shape" convention to the
   corpus README.
4. New failure at m0.025: `fix it` [debug→chat 0.871] — the chat
   micro-cluster STILL captures ultra-short fix requests when margins
   relax. Confirms iter-2's hazard; the Go gate's empty-string skip
   should become a length-floor skip (≤2 tokens) — daemon-side fix
   candidate for M4 wiring, not corpus.

## Frontier (post-iter-7)

| point | margin | C | P | E2E |
|---|---|---|---|---|
| coverage-max | 0.025 | 37.6% | 95.9% ❌ | 90.24% |
| **bar-compliant** | **0.035** | **26.9%** | **98.1% ✅** | **89.84%** |
| precision-max | 0.045 | 19.3% | 97.4% ✅ | 88.84% |

kNN unanimity on the same corpus: C 11.7% P 95.7% — centroid-margin
dominates it at every point. M1 milestone (corpus ~250, frontier
established) is on track for iteration 8.
