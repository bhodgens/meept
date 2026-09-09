# Iteration 14 Report — Fine-tuned ModernBERT Stage-0.5: training divergent (loss flat, head outputs uniform)

Date: 2026-09-08. Two-stage per master.md: Stage-0 (qwen3 centroid
m0.030) abstains → Stage-0.5 (ModernBERT-base + linear head, last-2
layers + head fine-tuned, 3 epochs) routes at calibrated confidence.

## What happened

1. iter14_stage05.py: tau ∈ {0.90, 0.80, 0.70} added ZERO routes — the
   trained head's confidence never reached any useful tau.
2. iter14b_diag.py (single-fold instrumented retrain): training loss
   FLAT (2.4817 → 2.4792 over 3 epochs — ln(12)=2.485, i.e. the model
   outputs a uniform distribution and does not learn), test confidence
   pinned at ~0.087 = 1/12 across every query, forced accuracy 12.5%
   (chance level). The fine-tune did not train AT ALL.

## Diagnosis (from evidence, not assumption)

- Loss ≈ ln(12) exactly + uniform softmax = the head receives
  non-informative features. Two candidate causes, both consistent:
  (a) MPS + fused ModernBERT kernels silently no-op'ing the
  unfrozen-layer gradients; (b) lr 2e-5 over 3 epochs (~38 steps) is
  too small a dose for head+2-layers to escape the checkpoint's
  near-uniform init signal on 202 examples. The known mlx-raclate
  early-release risk (master.md) is the same family of problem.
- NOT a harness bug: the same fold logic, cache, and eval path produced
  consistent numbers for every other head (spot-checked repeatedly).

## Decision per master.md risk policy

"Mlx-raclate early-release bugs: ... fall back to HF transformers on
CPU if the MLX path proves broken (measure, don't assume)." Applied in
spirit: the MPS fine-tune path is the suspect. Options for iteration
15: (a) CPU fine-tune (~10× slower, ~30-40 min — acceptable for one
run), (b) frozen-backbone logistic head over ModernBERT features
(cheap, isolates whether the backbone is the problem), (c) abandon the
Stage-0.5 axis and declare M3 closed with the raw-embedding rejection.

Parked decision for the user per "prepare-then-stop": continue with
(a)+(b) combined in one iteration (CPU + frozen variants), or close M3
now and proceed to M4 with the qwen3-centroid champion.

## State

Champion UNCHANGED: qwen3 centroid m0.030 — C 35.6% P 97.8% E2E 90.70%.
M3 entry bar (E2E ≥ 91.78%) still unmet by any Stage-0.5 variant.
