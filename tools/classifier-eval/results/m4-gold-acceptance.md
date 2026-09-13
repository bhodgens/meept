# M4 Gold-Replay Acceptance Results

Date: 2026-09-10. Acceptance gate (master.md): system accuracy > 86.8%
(chain-only floor) on the 48-case adjudicated gold replay.

## Policy comparison (same replay, same fold rules)

| policy | A routes (correct) | chain | system acc | verdict |
|---|---|---|---|---|
| double-confidence (A margin; B prob>=0.7 + agreement) | 7 (5) | 41 | **84.56%** | FAIL |
| **tfidf-veto (ALT-5): A routes only when tfidf concurs** | 2 (2) | 46 | **87.35%** | **UNVALIDATED** |

## Interpretation

- The adopted double-confidence policy (iter-17 measurement, 84.56%) is
  confirmed on the acceptance run: FAIL vs the 86.8% gate. Its Door A
  precision on replay is 71% (5/7) — the two wrong routes cost more
  than the coverage earns.
- The tfidf-veto row: 100% Door-A precision (2/2) by requiring
  character-ngram agreement, pushing the rest to the chain. Coverage
  drops to 2/48 (4%) — the cost of the gate — but the system floor
  (chain at 86.8%) dominates the arithmetic. **This row is NOT a
  measurement produced by the committed script** (see CORRECTIONS 1).

## Decision consequences (for FINAL-REPORT and wiring)

1. **The wiring candidate would be tfidf-veto centroid**, not bare
   centroid and not double-confidence — but see CORRECTIONS 1: the
   supporting number is unvalidated and the routed sample is 2 cases.
   Do not wire on it until it is re-measured (see CORRECTIONS 2).
2. Door B (ModernBERT probe) remains benched: its rescue precision is
   below the chain floor on this data. Revisit only after the outcome
   loop harvests enough corrected-row data to retrain on REAL
   misroutes.
3. Coverage honesty: 87.35% is chain-dominated. The prefilter's value
   at this corpus size is precision (never misroute), not coverage.
   Coverage grows as the harvest loop feeds corrected examples back
   into the centroids.

## Artifacts

- m4_gold_acceptance.py — the acceptance run (both policies)
- m4-gold-acceptance.json — the committed double-confidence run (FAIL)
- alt_methods_sweep.py — the 5-variant comparison (4cf71b07)
- alt-methods-correction.md — the 87.35% claim correction (2026-09-10)
- This file — the acceptance record

## CORRECTIONS (2026-09-12 audit wave)

Appended, not overwritten: the original table cells above stand as the
history of what was claimed; the entries below are the corrections of
record.

### 1. The tfidf-veto PASS verdict is UNVALIDATED (was: PASS)

The only committed producer of a results artifact on this file's date —
`m4_gold_acceptance.py` at 5fc03637 — implemented ONLY the
double-confidence policy and wrote
`results/m4-gold-acceptance.json = {"policy":"double-confidence","a_routes":7,
"a_correct":5,"chain":41,"system_accuracy":0.8456,"verdict":"FAIL"}`. No
committed script produced the 87.35% row: its only tool,
`alt_methods_sweep.py`, prints to stdout and writes no artifact, and it
needs the UNTRACKED replay corpus plus a live embed server on :8090.
A number no one can re-derive from the repository is not evidence, so the
veto verdict is recorded as **UNVALIDATED** everywhere it is cited (this
table; `docs/plans/classifier-iteration/FINAL-REPORT.md`;
`internal/agent/tfidf_veto.go`). `m4_gold_acceptance.py` has since been
extended to run BOTH policies and write one artifact per policy
(`results/m4-gold-acceptance-<policy>.json`, write-once — it never
overwrites the committed `.json`), but neither policy can be re-run here:
the replay corpus is gitignored and :8090 is not always up.

### 2. Acceptance sensitivity / coverage floor

The verdict flips on one route decision at n=48. 41 of the 48 cases are
chain-destined and each is credited at the hard-coded `CHAIN = 0.868`
constant, so:

| decomposition | score |
|---|---|
| 2/2 routes correct + 46 chain | 87.35% |
| 1/2 routes correct + 46 chain | 85.27% — below the 86.8% gate |
| 0 routes + 48 chain | 86.80% — equals the gate; strict `>` means FAIL |

87.35% is therefore 2 correct routes plus 95.8% chain credit at an
unverified constant, not a measured win over 84.56%. Effective routed
sample = 2 cases. `m4_gold_acceptance.py` now enforces a coverage floor
`MIN_ROUTED = 20`: when fewer than 20 cases are routed it reports
`verdict: "INSUFFICIENT_COVERAGE"` instead of PASS/FAIL. Both rows above
are below that floor (7 and 2 routed respectively), so under the current
rule neither is a claimable verdict — the 84.56% / 87.35% *values* are
unchanged; only their PASS/FAIL framing is withdrawn.

### 3. Corpus <-> replay disjointness guard (train-on-test)

The models are fit on the committed corpus; the ruler is the adjudicated
replay. They were not disjoint: the replay case "implement the plan using
subagents" is byte-identical to committed corpus row `h18-planexec-001`
(case_key match; that row's own note records it was aligned to "the
adjudicated replay gold for this verbatim text"). The committed script now
runs `eval_harness.replay_disjointness()` before scoring (exact case_key
match, plus cosine > 0.95 when the embed server is up) and REFUSES the run
(exit 2) on any leak — it never silently drops the case, because dropping
it would move the denominator. Reproduce the guard:
`python3 tools/classifier-eval/m4_gold_acceptance.py --check-overlap`
(currently reports the 1 leak). Resolving the leak (remove the corpus row
or the replay case) is a measurement-protocol decision, not a code fix.
The harvest path (`harvest_wave5.py`) applies the same ruler guard when
splicing new corpus rows.
