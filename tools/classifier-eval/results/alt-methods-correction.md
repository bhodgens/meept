# CORRECTION — ALT-methods sweep (commit 4cf71b07): tfidf-veto claim

Added 2026-09-10 (audit wave H2). The original commit and its message are
left untouched — history is preserved; this file is the correction of
record for the numbers claimed there.

## What was claimed

`4cf71b07` ("eval: alternative-methods sweep for Doors 1-2 — tfidf-veto
wins"): "ALT-5 tfidf-veto on centroid: 87.35% — BEATS the chain floor"
(vs 84.56% current cascade, 86.8% chain-only floor). No results artifact
was committed with the run — the commit adds only the sweep script
(`alt_methods_sweep.py`); all printed numbers were stdout-only, so
87.35 / 84.56 / 76.07 / 79.13 are not reproducible from git.

## Recompute from the script's own formula

`system_score()` (`alt_methods_sweep.py:53-58`) computes
`(right + n_chain × CHAIN) / tot` with `CHAIN = 0.868` over the same 48
replay cases as every other measurement. 0.8735 is uniquely the
decomposition:

    (2 + 46 × 0.868) / 48 = 0.87350

- **Route count: 2 of 48 cases (4.2% coverage) actually routed by the
  tfidf-veto Door 1.** This count was disclosed nowhere in the commit
  message or the doc.
- The remaining **46 cases (95.8%) are pure chain credit** at the
  constant 0.868 — whose staleness is already flagged in
  `eval_harness.py:67-77` (measured on the old 136-case base corpus,
  never re-measured).

## Is the margin real?

The entire +0.55pt margin over the 86.8% chain floor rests on one route
decision:

| decomposition | score |
|---|---|
| 2/2 routes correct + 46 chain | 87.35% (= claimed) |
| 1/2 routes correct + 46 chain | 85.27% — **below the floor** |
| 3 routes, 2 correct + 45 chain | 87.63% |

One flipped route moves the headline from "beats the chain floor" to
"loses to it". With n=48 and a 2-case effective sample, the 87.35%
vs 86.8% delta is one-route noise, not a measured win.

## Verdict

The "BEATS the chain floor" claim is inflated: it is 2 correct routes
plus 95.8% chain credit at an unverified constant, with the route count
undisclosed and no committed artifact behind any of the numbers. The
comparison itself is apples-to-apples (same 48 cases, same denominator),
and the shipped code claims nothing (the doc marks tfidf-veto as
"queued — not shipped yet"). Until the sweep is re-run with a committed
per-variant results artifact, treat ALT-5 as **unvalidated, not a win**.
See also `docs/workflows/classification-architecture.md` (tfidf-veto
section, amended same day).

## Addendum (same day): commit 5fc03637's acceptance run

Mid-wave commit `5fc03637` re-ran the comparison as a formal acceptance
gate and committed the previously-missing artifact
(`m4-gold-acceptance.md` + `.json`), disclosing the route count ("100%
Door-A precision (2/2) … coverage 2/48 (chain-dominated by design)").
That resolves the artifact and disclosure legs of this correction. It
does NOT resolve the substance: the acceptance run's PASS verdict and
the "PROMOTED" wiring decision still rest on a 2-case effective sample
against a stale 0.868 chain constant — the sensitivity table above
applies unchanged (1/2 routes correct would score 85.27%, below the
86.8% gate). A 2/2 sample cannot distinguish a 100%-precision router
from a coin-flip router at p<0.25. Re-validate on a larger harvested
corpus before building on tfidf-veto.
