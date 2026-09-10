# Iteration 19 Report — quickplan class added; first real measurement WITH the class

Date: 2026-09-08. Corpus 314 → 338 (base 139 + adversarial 199; +24
quickplan anchors across the four adjudicated rules, plus 4 boundary
negatives protecting review/analyze/plan).

## Cascade on the adjudicated gold replay (n=48, real traffic)

| metric | iter-17 (no quickplan class) | iter-19 (with quickplan) |
|---|---|---|
| stage A | 11 routes, 8 correct (72.7%) | 6 routes, 4 correct (66.7%) |
| stage B | 4 routes, 4 correct (100%) | 5 routes, 2 correct (40%) |
| chain | 33 | 37 |
| **system accuracy** | **84.7%** | **79.4%** |

## The honest read

Adding quickplan as a 13th class made the numbers WORSE on the replay,
not better — but this is the expected first-training artifact, and the
per-intent view shows exactly why:

1. **Stage A (centroid) shrank to 6 routes**: quickplan anchors pulled
   the centroid geometry; formerly-routed cases now fall through
   (margin < 0.030). Stage-A precision dropped (4/6) because two
   quickplan-labeled messages routed to git/platform.
2. **Stage B's quickplan precision is the real problem**: 3 of its 5
   routes were quickplan-labeled messages predicted as `code`
   ("Implement Tasks 7 and 8: Add project fields..."). These are
   20 quickplan anchors vs ~35 code anchors — the code class dominates
   the shared vocabulary (implement/add/wire) and wins the boundary.
   With 5 B-routes total, 2/5 vs 4/4 is also within small-n noise, but
   the direction is consistent: the probe needs more quickplan mass
   with execution-framing, and/or the quickplan-vs-code boundary needs
   to lean on orchestration cues (subagents/tasks/waves/leaves) that
   code cases lack.
3. quickplan expected-correct is 13.0/20 (65%) — it's now the biggest
   measured intent, and the chain covers most of its misses.

## Why this is still progress

The adjudicated ruler now measures a taxonomy that matches reality.
The 79.4% is a REAL baseline for the 13-class space; the previous 84.7%
was measured against a 12-class taxonomy that couldn't even express
42% of the traffic. The path to > 86.8% is concrete:

- **quickplan anchor wave 2**: 20-30 more anchors weighted to the
  code-vocabulary collision ("Implement Tasks N and M..." shapes) and
  orchestration-cue phrasing.
- **Boundary feature for stage B**: append a coarse orchestration-cue
  indicator to the probe features (has "subagent|task|wave|leaf|plan.md"
  → +1) or duplicate quickplan anchors 2× to rebalance.
- Re-run replay; acceptance = system accuracy > 86.8% chain-only.

## Artifacts

- results/iter-19/quickplan-cascade.json (full per-intent table)
- testdata corpus: +24 tracked quickplan anchors (dedup vs replay-gold
  verbatim text: the anchors are synthetic paraphrases, not copies)

## CORRECTIONS 2026-09-10 (audit L13)

- **Corpus counts in the Date line above are off by one** ("Corpus 314
  → 338"; also "314→338→370" as carried in the iter-20 report and
  `820c016f`). Recounted through the committed loader
  (`eval_harness.py load_cases`) from the tracked corpus at the
  relevant commits: **313 → 337 → 369** (313 at `c5e3bcf0` verified;
  337 = 313 + the +24 iter-19 anchors; 369 = 337 + the +32 iter-20
  anchors, all recomputed via `load_cases` at HEAD). Both base (139)
  and adversarial files recount exactly. Deltas +24/+32 stand as
  originally reported — only the running totals were off by one.
- **Sibling correction, same wave** (q1-q4-response.md): the Q1 table
  labeled the current policy "tau 0.147" — that is the old iter-17
  quantile; the v2c/current policy tau is **0.152**
  (`results/iter-20/policy-sweep.json` top row). The 0.147 label is
  superseded, original row preserved.
