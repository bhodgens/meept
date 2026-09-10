# M4 Gold-Replay Acceptance Results

Date: 2026-09-10. Acceptance gate (master.md): system accuracy > 86.8%
(chain-only floor) on the 48-case adjudicated gold replay.

## Policy comparison (same replay, same fold rules)

| policy | A routes (correct) | chain | system acc | verdict |
|---|---|---|---|---|
| double-confidence (A margin; B prob>=0.7 + agreement) | 7 (5) | 41 | **84.56%** | FAIL |
| **tfidf-veto (ALT-5): A routes only when tfidf concurs** | 2 (2) | 46 | **87.35%** | **PASS** |

## Interpretation

- The adopted double-confidence policy (iter-17 measurement, 84.56%) is
  confirmed on the acceptance run: FAIL vs the 86.8% gate. Its Door A
  precision on replay is 71% (5/7) — the two wrong routes cost more
  than the coverage earns.
- The tfidf-veto policy PASSES: 100% Door-A precision (2/2) by
  requiring character-ngram agreement, pushing the rest to the chain.
  Coverage drops to 2/48 (4%) — the cost of the gate — but the system
  floor (chain at 86.8%) dominates the arithmetic.

## Decision consequences (for FINAL-REPORT and wiring)

1. **The wiring candidate is tfidf-veto centroid**, not bare centroid
   and not double-confidence. The tfidf-veto leaf (queued) is therefore
   PROMOTED from "nice to have" to "the acceptance-passing
   configuration."
2. Door B (ModernBERT probe) remains benched: its rescue precision is
   below the chain floor on this data. Revisit only after the outcome
   loop harvests enough corrected-row data to retrain on REAL
   misroutes.
3. Coverage honesty: 87.35% is chain-dominated. The prefilter's value
   at this corpus size is precision (never misroute), not coverage.
   Coverage grows as the harvest loop feeds corrected examples back
   into the centroids.

## Artifacts

- m4_gold_acceptance.py — double-confidence acceptance run (FAIL)
- alt_methods_sweep.py — the 5-variant comparison (4cf71b07)
- This file — the acceptance record
