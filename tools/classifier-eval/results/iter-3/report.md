# Iteration 3 Report — Instruction-prefixed embeddings (mechanical sweep)

Date: 2026-09-08. Mode: mechanical sweep (master.md), no subagents.
Permutation axis: embedder instruction prefix (official Qwen3-Embedding
query format) + kNN size/vote variants. Corpus: 189 (136 base + 53
adversarial). 5-fold, seed 42, cached embeddings (new cache tag).

## Hypothesis

HANDOFF-STAGE0.md §9 recorded instruction-prefixed centroid at 96.4%
precision-when-direct / 20.6% margin-clear coverage vs plain pooling's
flat margins. Expected: the prefix separates the query distribution from
the example distribution and lifts precision at equal coverage.

## Result: hypothesis REJECTED for the kNN gate

| permutation | C | P | wrong | E2E | SCORE |
|---|---|---|---|---|---|
| instr knn5 unanimity floor 0.65-0.80 | 10.3% | 88.2% | 2 | 86.95% | +0.020 |
| instr knn3 unanimity 0.70 | 19.4% | 78.1% | 7 | 85.12% | −0.094 |
| instr knn4 unanimity 0.70 | 14.5% | 83.3% | 4 | 86.30% | −0.020 |
| instr knn5 maj4 0.70 | 22.4% | 75.7% | 9 | 84.31% | −0.144 |
| instr knn4 maj3 0.70 | 28.5% | 70.2% | 14 | 82.08% | −0.284 |
| instr knn3 maj2 0.70 | 47.9% | 54.4% | 36 | 71.30% | −0.949 |
| **no-instr knn5 unanimity 0.70 (iter-2 ref)** | **9.1%** | **93.3%** | **1** | **87.39%** | **+0.049** |

Instruction prefixing LOWERS precision (93.3→88.2%) at slightly higher
coverage for the unanimity head, and every majority-vote variant is
E2E-negative (below the 86.8% chain-only baseline). Floors 0.65-0.80 are
identical under the prefix — the 5th-neighbor floor never binds. The
§9 centroid result does not transfer to per-example kNN: the prefix
compresses absolute cosine values upward, letting wrong-class neighbors
clear the membership floor.

## Verdict

Keep plain EOS pooling for Stage-0 kNN. Majority-vote heads are
permanently E2E-negative at this embedder — the LLM chain baseline wins
every time coverage is bought with precision. Mark: k-majority heads
REJECTED for M2 permutations (no re-testing at higher embedder tiers
unless that tier first shows unanimity P ≥ 97%).

File: results/iter-3/summary-*.json. Next (iter 4): head-family sweep —
centroid margins, logistic calibration, prototype-hybrid — on the plain
embedding space.
