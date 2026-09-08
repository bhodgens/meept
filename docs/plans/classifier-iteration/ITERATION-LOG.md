# Iteration Log

One row per iteration. SCORE = C × P² − 5 × (wrong/total). C=coverage of
direct routes on the full test split, P=precision of those routes, A=held-out
accuracy when forced (no abstain), OOD-R=OOD abstain rate, L=p50 latency.

| # | Permutation (embedder / head / threshold) | Corpus | C | P | A | F1 | OOD-R | L(ms) | Wrong | Fix applied | Verdict |
|---|---|---|---|---|---|---|---|---|---|---|---|
| 0 | qwen3-0.6B-4bit EOS / kNN k=5 unanimity / floor 0.70 (pre-campaign baseline, LOO) | 136 | 14.7% | 100% | — | — | untested | ~300 | 0 | self-match exclusion (f272771c) | baseline — CORRECTED at iter 1: 14.7% not reproducible (see results/iter-1/report.md); honest 5-fold value is C 3.7% |
| 1 | qwen3-0.6B-4bit EOS / kNN k=5 unanimity / floor 0.70 — 5-fold cross, seed 42 | 136 | 3.7% | 100% | 100% | 0.056 | n/a (no OOD cases yet) | ~8/embed cached | 0 | harness built (tools/classifier-eval); iter-0 reconciled | E2E 87.29% ≈ chain-only 86.8%; coverage structurally capped (top-1-NN agreement 43%) |
| 2 | same head; +53 adversarial cases (OOD/short/boundary/compound/para/inj/long) | 189 | 9.1% | 93.3% | 93.3% | 0.084 | 100% | ~8/embed cached | 1 | harness: OOD excluded from train index + OOD pooling fix | E2E 87.39%; OOD abstains 24/24; chat micro-cluster is a precision hazard (captures short inputs as chat) |
| 3 | instruction-prefixed embedder × kNN k/vote sweep (mechanical) | 189 | 10.3% | 88.2% | 88.2% | 0.094 | 100% | ~15/embed cached | 2 | none (pure measurement) | REJECTED: prefix lowers P 93→88% at kNN; all majority heads E2E-negative; keep plain EOS pooling |
