# Iteration 11 Report — Micro-guards: word floor + absolute sim floor

Date: 2026-09-08. Mechanical sweep on the 264-case corpus. Two guard
ideas tested against the m0.030 champion base.

## A) Word-count floor (skip gate for inputs < N words)

| floor | n evaluated | C | P | wrong | E2E |
|---|---|---|---|---|---|
| none | 263 | 33.5% | 97.5% | 2 | 90.38% |
| 2 words | 251 | 29.5% | 97.0% | 2 | 89.81% |
| 3 words | 246 | 28.8% | 96.9% | 2 | 89.70% |

REJECTED: the floor only removes EASY coverage (the corpus's short-but-
correct cases route fine) while the wrongs ('fix it', changelog,
repo-connect) survive — 'fix it' is 2 words, right at the floor.
The chat micro-cluster hazard is NOT length-shaped after the iter-7
semantic chat anchors landed. Daemon-side length-floor: DON'T wire it.

## B) Absolute sim floor on top-1 (margin AND floor both required)

| floor | C | P | wrong | E2E |
|---|---|---|---|---|
| 0.65 | 33.5% | 97.5% | 2 | 90.38% |
| 0.70 | 33.5% | 97.5% | 2 | 90.38% |
| 0.75 | 32.6% | 97.4% | 2 | 90.27% |

INERT up to 0.70 (every routed case already clears 0.70), slightly
negative at 0.75. REJECTED — the margin rule subsumes it.

## Iteration verdict

Both micro-guards add config surface without E2E gain. The champion
stays plain centroid margin 0.030, floor 0.60. The two remaining wrongs
at m0.030 are NOT fixable by input-shape rules — they need either (a)
the boundary cases to move (they won't; they're genuine product
ambiguity), or (b) a stronger embedder (M3). M2's remaining lever is
corpus mass on the abstained single-intent cases (132 abstains
identified in iter-10 diagnosis — most have top1-correct with thin
margin: pure corpus-density targets).

Next: iter-12 = harvest wave 3 targeting EXACTLY the top1-correct/
margin<0.030 near-misses (the ~20 cases listed in the diag output).
