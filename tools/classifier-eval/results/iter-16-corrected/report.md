# Iteration 16 CORRECTED — deterministic expected-credit E2E (audit 2026-09-08)

Status: **SUPERSEDES the verdict in results/iter-16/report.md** (that file is
kept unmodified for the record). Original commit: 613c9120 ("E2E=92.8%
clears bar"). This correction changes the verdict; the A/B stage numbers
are unchanged.

## What was wrong with the original

`iter16_cascade3.py:141` scored Stage C (the LLM chain) with a SAMPLED
draw:

```python
ok = np.random.random() < CHAIN_ACC   # seed 42 at line 149
```

109 Bernoulli draws at p=0.868 (seed 42) realized **98/109 = 89.9%** —
about +1σ of luck. Because the chain carries 109/274 of the corpus, that
+3.1pt stage-C over-realization alone manufactured the headline:
sampled E2E 92.80% vs deterministic 91.44%. The campaign's own convention
(eval_harness.py:579) is DETERMINISTIC expected credit — every
chain-destined case is credited exactly CHAIN_BASELINE — precisely to
avoid letting a seeded draw decide a promotion. The stage-A/B routing and
correctness numbers (89(87) A, 52(47) B at q=0.50) were computed
honestly and are unaffected.

## Deterministic recompute (analytic, from committed artifacts)

An offline re-run was not performed: it needs a live qwen3 embed server
(:8090) or a warm vector cache for all 274 texts plus ModernBERT weights
at /Volumes/LLMs — availability was not verified during this audit, and
none of it is needed for the correction, because E2E is an EXACT linear
function of the already-committed per-stage counts
(results/iter-16/summary-20260908-142037.json):

```
E2E = (A_ok + B_ok + 0.868 × C_n) / 274
```

| B-exit q | A routed(ok) | B routed(ok) | C cases | sampled E2E (orig) | **deterministic E2E** | bar 91.78% |
|---|---|---|---|---|---|---|
| 0.30 | 89 (87) | 25 (23) | 136 | 91.60% | **(87+23+0.868×136)/274 = 91.52%** | FAIL |
| 0.40 | 89 (87) | 37 (34) | 124 | 89.60% | **(87+34+0.868×124)/274 = 91.28%** | FAIL |
| 0.50 | 89 (87) | 52 (47) | 109 | 92.80% | **(87+47+0.868×109)/274 = 91.44%** | **FAIL** |

(These also equal iter-15's two-stage E2E values at matching partitions —
114/126/141 direct with 110/121/134 correct — an independent
cross-check: iter-16's cascade at q is iter-15's pipeline plus the chain
as an explicit stage, so identical A+B behavior must reproduce identical
E2E. It does.)

## Verdict: M3 entry bar NOT MET

The deterministic champion (q=0.50) scores **E2E 91.44%**, which is
**0.34pt BELOW the pre-registered 91.78% bar** and +4.6 over chain-only
86.8%. The original "clears bar" claim is withdrawn. Concretely:

- The q=0.50 cascade is statistically indistinguishable from iter-15's
  two-stage champion on identical A+B partitions (91.44% vs 91.45% —
  same routing, same credit; the difference is rounding).
- No configuration meets the M3 entry bar. M3 remains OPEN by its own
  pre-registered rule. Nothing advances to M4 daemon wiring on this
  evidence.
- The per-stage view stands: stage A 97.8% precision, stage B 90.4%
  precision, both beat the 86.8% chain they displace — the CASCADE is
  directionally right, it just does not clear the bar on the current
  corpus.

## Independent confirmation (m4-silver, iter-17 interim)

Commit ecbe086a (results/m4-silver/report.md) reached the same conclusion
by a completely different route: real-traffic silver replay (48 verbatim
Hermes messages, silver labels) measured expected system accuracy
**84.7%** — below the synthetic claim AND below chain-only 86.8%. The two
audits agree: the synthetic 92.8% was an artifact of the seeded draw, and
the cascade does not currently transfer to real traffic either. m4-silver
additionally identifies the taxonomy/corpus-mix gap (plan-execution,
document-writing intents missing from gold) as the dominant real-traffic
cause. See ITERATION-LOG.md CORRECTION entry, which references both.

## What would honestly change the verdict

1. Chain re-measurement: CHAIN_BASELINE 0.868 is itself stale (measured
   on the 136-case corpus; see eval_harness.py comment) — if the true
   chain accuracy on the 274-case adversarial-heavy corpus is LOWER than
   0.868, the cascade's E2E rises mechanically; if higher, it falls.
   This needs a live chain run (not faked here).
2. Corpus wave 4 / taxonomy expansion (per m4-silver), then re-run with
   this corrected script (deterministic).
3. The bar itself is a pre-registered constant; changing it requires an
   explicit user decision, not this audit.

Artifacts: results/iter-16/summary-20260908-142037.json (original run,
committed in 613c9120), iter16_cascade3.py (corrected in place), this
report. No new measurement run was performed; every number above is
algebra on committed data.
