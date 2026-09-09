# Iteration 13 Report — ModernBERT-base raw embeddings as alternate Stage-0

Date: 2026-09-08. Corpus 274 (transient parse artifact resolved — the
adversarial corpus re-parsed at 275; see iter-13/summary for the
comparator run at 274 vs champion at 275 — 1-case delta, immaterial to
the verdict). Weights: answerdotai/ModernBERT-base downloaded to
/Volumes/LLMs/answerdotai/ModernBERT-base (user-approved), loaded via
transformers, mean-pooled last hidden state, L2-normalized, MPS device.

## Measurement

| embedder + head | C | P | wrong | E2E | SCORE |
|---|---|---|---|---|---|
| ModernBERT-base centroid m0.025 | 6.8% | 94.1% | 1 | 87.30% | +0.040 |
| ModernBERT-base centroid m0.030 | 6.0% | 93.3% | 1 | 87.19% | +0.032 |
| ModernBERT-base centroid m0.035 | 4.8% | 100% | 0 | 87.43% | +0.048 |
| **qwen3-0.6B centroid m0.030 (champion)** | **35.6%** | **97.8%** | **2** | **90.70%** | **+0.300** |

## Findings

1. **ModernBERT-base REJECTED as Stage-0 embedder.** Raw mean-pooled
   embeddings from the base (non-fine-tuned) checkpoint yield ~6× less
   coverage than qwen3-0.6B at comparable precision. Intent clusters
   are far less separable in its representation space: the corpus's
   12-way margin distribution under ModernBERT is even flatter than
   the HANDOFF-STAGE0.md §9 finding for qwen3 plain pooling.
2. Its one wrong at m0.025-0.030 is 'fix it'→chat at 0.923 — the same
   micro-cluster capture, even more confident. The base model's
   representation makes short texts cluster by SURFACE form (shortness)
   rather than intent.
3. M3 entry bar (E2E ≥ 91.78%) requires beating the champion by a full
   point. ModernBERT raw: 87.43% best. FAILED the entry question — and
   per master.md the check is whether the stage "earns its latency":
   it does not.
4. Not yet tested (M3 remaining, needs training): the mlx-raclate
   FINE-TUNED ModernBERT classifier as an ADDITIVE Stage-0.5 (routes
   only on calibrated confidence when Stage-0 abstains). The raw-
   embedding failure does NOT predict the fine-tuned head's ceiling —
   supervised training reshapes the space. That experiment is iter-14,
   gated on training-loop feasibility (mlx-raclate early-release risk,
   per master.md risks).

## Verdict

Raw ModernBERT: REJECTED. Champion unchanged (qwen3 centroid m0.030).
Stage-0.5 fine-tuned experiment queued; bar unchanged (E2E ≥ 91.78%).
