# Classifier Evaluation Methodology

How meept's intent classifier is measured: corpora, scoring, provenance,
and the rules that keep a number from being trusted before it is
evidence. This document reconciles the historical campaign record
(`docs/plans/classifier-iteration/FINAL-REPORT.md`,
`tools/classifier-eval/results/`) with the scoring/provenance code as
shipped after the 2026-09-17 routing-repair tree (leaves 01-03).

## Corpora

| corpus | file | cases |
|---|---|---|
| base | `internal/agent/testdata/eval/classifier-test-corpus.json5` | 139 |
| adversarial | `internal/agent/testdata/eval/classifier-adversarial-corpus.json5` | 250 |
| combined (loader default) | via `eval_harness.H.load_cases()` | 389 (361 non-OOD / 28 OOD) |

- An **OOD case** is outside the supported intent categories. OOD cases
  never enter training; they test abstention.
- The loader is the single source of truth for population. Counts in
  prose are review evidence, never constants to hardcode.
- The adjudicated 48-case **gold replay** is a separate ruler (real
  traffic, 6 normative taxonomy rules). It is gitignored private
  transcript data; artifacts may carry its `case_key` hashes, never its
  text.
- Builder defaults (`scripts/build_prefilter_centroids.py`) equal the
  loader's eligible (non-OOD) base+adversarial key set. Explicit
  single-corpus runs remain supported as labeled subset experiments.

## Scoring definitions (C2)

Computed by `run_permutation` in `tools/classifier-eval/eval_harness.py`:

- `total`: non-OOD test cases; `direct`: non-OOD predictions with a
  label; `correct`: correct non-OOD direct predictions.
- `wrong`: incorrect non-OOD direct predictions PLUS OOD direct
  predictions (an OOD case that gets routed is a wrong route).
- `OOD_total` / `OOD_abstain` / `OOD_R`: OOD cases, abstentions, and
  their ratio.
- `E2E = (correct + 0.868 × (total − direct)) / total`. E2E is MODELED
  in-domain accuracy: the `(total − direct)` abstained cases are
  credited at the chain constant 0.868, an old estimate measured once
  on the 136-case base corpus and never re-measured. E2E is not a
  measured end-to-end accuracy.
- `SCORE` keeps the historical wrong-route penalty; its denominator is
  the non-OOD direct count plus penalty terms. Undefined ratios (zero
  denominator) serialize as JSON `null` and display as `n/a` — never a
  fabricated 100%.

**Every held-out case reaches `head.decide`.** Gold labels grade; they
never decide. The historical forced-abstention defect (OOD cases
short-circuited to `None` without a prediction call, then counted as
successful abstentions) was repaired by leaf 02 of the routing-repair
tree; see CORRECTIONS below for the invalidation scope.

## Provenance (C3)

Artifact metadata identifies source files, source content hashes, the
eligible key-set hash, document count, embedding model identity, and
preprocessing settings (`provenance` block; Go decoders tolerate the
additive fields).

Cache identity: a verified local model content fingerprint plus
preprocessing identifies a vector cache namespace (`model_identity()`).
An alias or URL alone cannot identify weights. A cache built without a
verified fingerprint lands in an explicit `unverified` namespace and is
fail-closed for acceptance use — legacy unverified caches cannot
silently support fresh acceptance claims.

Fold evidence: fold assignments are deterministic from the tracked fold
cache; corpus growth never silently reassigns existing keys. Acceptance
runs must record which fold evidence they used.

## Rules of evidence

1. **Selection-on-test.** Choosing a policy and judging it on the same
   examples inflates the result: the headline is a max over in-sample
   scores, not an estimate of held-out accuracy. Historical instances
   are corrected below; every future claim must state whether the
   scoring data is disjoint from the selection data.
2. **Chain credit is not a measurement.** A score dominated by `n_chain
   × 0.868` credit measures the constant, not the router. Route counts
   and coverage are always disclosed next to any such figure.
3. **Coverage floor.** `m4_gold_acceptance.py` refuses PASS/FAIL below
   `MIN_ROUTED = 20` routed cases and reports `INSUFFICIENT_COVERAGE`.
4. **No new headline without provenance.** A headline number needs a
   committed artifact, a measured provenance trail, and recorded
   coverage. Stdout-only numbers are unvalidated by definition.
5. **Privacy.** Public artifacts carry case keys, labels, stage names,
   and numerics — never input excerpts, on disk or on stdout. Producers
   redact before serialization.

## CORRECTIONS of record (2026-09-18, routing-repair leaf 05)

Appended; the historical sections above describe current code, the
entries below the historical claims they qualify.

### 1. Forced-abstention fabrication scope (MEAS-04)

`eval_harness.py` historically inserted `None` for OOD cases without
calling `head.decide`, then counted the inserted `None` as a successful
abstention, so every summary row reported `OOD_abstain == OOD_total`,
`OOD_R = 1.0`. The OOD-bypass code path was removed by 0688a9b0 and the
scoring path was hardened by the routing-repair leaf 02 (self-tests in
`m4_gold_acceptance.py --self-test` now fail if any held-out case skips
prediction).

Invalidation scope — the `OOD_R` / `OOD_abstain` FIELDS of every
committed summary artifact produced before 0688a9b0 are fabricated
(without a prediction call there is no measurement): 119 rows across
`results/summary-20260907-235210.json`,
`results/summary-20260908-000456.json`, and the `summary-*.json` files
under `results/iter-3` … `results/iter-13`. iter-14..16 summaries carry
no OOD fields at all (they predate the field or used a different
producer) and make no OOD claim. The in-domain arithmetic in the same
rows (`total`, `direct`, `correct`, `wrong`, `E2E`, `SCORE`, `P`, `A`,
`F1`) is computed from real prediction calls and is NOT invalidated by
this defect — the files stay untouched (append-only corrections; the
numeric JSON artifacts are preserved).

The "zero wrong routes at m=0.030" M1 milestone and every
OOD-abstention-derived claim in the campaign record must be read with
`OOD_R = 1.0` struck: those rows never demonstrated OOD rejection.

### 2. Selection-on-test caveats already on record

- The 87.35% tfidf-veto acceptance figure (2 correct routes + 46/48
  chain credit) is UNVALIDATED — `results/alt-methods-correction.md`,
  CORRECTIONS 1/5 of FINAL-REPORT. It is a selection-run number from
  stdout-only tooling, not a fresh chain measurement.
- The iter-20 83.97% headline is selection-on-test over 13 policies on
  the same 48 replay cases — `results/iter-20/report.md` CAVEAT
  2026-09-10 (audit M6b). The margin-optimum analysis in
  `results/wave5-confusion.md` uses training data (in-sample margins)
  and is UNVALIDATED for the no-change decision until re-run per fold;
  the earlier review brief overstated it as a proven dead end.
- Policy selection and acceptance must use disjoint sets. The next
  campaign design (see
  `docs/plans/20260917-routing-repair/verification.md`) separates
  tuning examples from untouched acceptance examples and freezes
  thresholds before observing results.

### 3. Builder population, cache identity, fixture role (leaf 03)

- `scripts/build_prefilter_centroids.py` previously defaulted to a
  222-key subset of the loader's 361 eligible keys (MEAS-02). The
  default now equals the loader population; parity is pinned by
  `tools/classifier-eval/test_provenance.py`.
- Cache identity previously hashed the model ALIAS and instruction
  (MEAS-05), letting distinct weights reuse disk vectors. Identity is
  now local content hashing with fail-closed unverified namespaces.
- `internal/agent/testdata/prefilter_tfidf_veto.json` (202 docs) was
  fixture metadata, never proof of deployed-model staleness (MEAS-03).
  The fixture was deleted with approval (recorded decision 4 of the
  routing-repair master); tests build their own temporary models.
- MEAS-06 (fold evidence) is a POLICY repair — fold-evidence recording
  is now required for acceptance — not a demonstrated misassignment.

### 4. Live-verification boundary

AR-1 (path-question arithmetic misroute) was verified live on the
scratch rig after the leaf-04 repair: the input dispatched
intent=debug method=llm (previously chat/short_message_guard), same
input_hash 71bdf6b3 across the flip. All other repaired guards are
verified offline through production entry points
(`internal/agent/dispatcher_routing_repair_test.go`); live
model-quality claims require the separate campaign (Task 5 design) and
remain forbidden from offline evidence.
