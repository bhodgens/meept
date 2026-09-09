# Iteration 12 Report — Harvest wave 3: near-miss reinforcement (+11)

Date: 2026-09-08. Corpus 264 → 275 (base 139 + adversarial 136).
Dedup guard: 15 candidates, 4 rejected (cos 0.952-0.981), 11 accepted.

## Measurement

| config | C | P | wrong | E2E | SCORE |
|---|---|---|---|---|---|
| centroid m0.030 | 35.6% | 97.8% | 2 | 90.70% | +0.300 |
| centroid m0.035 | 28.8% | 98.6% | 1 | 90.20% | +0.260 |
| kNN k=5 unanimity | 9.6% | 91.7% | 2 | 87.27% | +0.041 |

## Findings

1. Coverage at m0.030 rose 33.5 → 35.6% (+2.1pts) while precision held
   97.8% (wrong count unchanged at 2: the two known intentional boundary
   cases, changelog→git and repo-connect→git). E2E recovering from the
   iter-10 dip as predicted: 90.38 → 90.70%.
2. Near-miss queue after the wave (iter10_diag re-run): the thin-margin
   population is now dominated by top1-CORRECT debug/code/schedule
   cases at margin 0.027-0.030 — one more reinforcement wave (iter-13)
   should flip a chunk of them into routed territory.
3. review#2 ('are there any issues with this implementation?') remains
   top1-WRONG at 0.887 toward debug even with 5 review anchors. Hand
   read: the phrasing 'issues with this implementation' genuinely reads
   debug-flavored; the base corpus's review intent may be too narrow.
   Decision: leave as gold review (the corpus definition: feedback on
   provided work) but flag for the M4 replay validation — if real
   traffic agrees with debug more often, the review intent needs
   re-scoping, not more anchors.

## M2 progress tracker

- Corpus: 275/400 (M2 target).
- Frontier: m0.035 (P 98.6%, bar-compliant) and m0.030 (C 35.6%).
- Remaining M2 levers: harvest waves until corpus ~300 then measure the
  two-pass compound pre-stage; per-class threshold revisit ONLY if
  precision dips below 97% at m0.030.

## Correction (2026-09-08 audit)

The corpus count above is off by one: the actual case count is **274**,
not 275 (264 + 11 accepted = 275 candidates, but the parse nets 274 —
see the iter-15 report's Correction for the orphaned fold-cache key
`38443fca0b06b30e` that makes corpus-vs-cache counts disagree by one;
also the "275/400 (M2 target)" tracker line should read 274/400).
Every measured metric in this report is computed from the loaded case
list and is unaffected by the bookkeeping note. Original text retained
above for the record.
