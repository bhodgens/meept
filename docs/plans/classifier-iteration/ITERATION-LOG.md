# Iteration Log

One row per iteration. SCORE = C × P² − 5 × (wrong/total). C=coverage of
direct routes on the full test split, P=precision of those routes, A=held-out
accuracy when forced (no abstain), OOD-R=OOD abstain rate, L=p50 latency.

| # | Permutation (embedder / head / threshold) | Corpus | C | P | A | F1 | OOD-R | L(ms) | Wrong | Fix applied | Verdict |
|---|---|---|---|---|---|---|---|---|---|---|---|
| 0 | qwen3-0.6B-4bit EOS / kNN k=5 unanimity / floor 0.70 (pre-campaign baseline, LOO) | 136 | 14.7% | 100% | — | — | untested | ~300 | 0 | self-match exclusion (f272771c) | baseline |
