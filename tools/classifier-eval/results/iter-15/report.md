# Iteration 15 Report — Research-driven retrain: Stage-0.5 WORKS

Date: 2026-09-08. Corpus 274 (parse note: adversarial corpus parses at
275 via load_cases; the 274 here is one short-intent case deduped by a
hash collision in the fold cache — immaterial, both runs consistent).

## Research findings (what actually failed in iter-14)

Sources: pytorch/pytorch#82707 (MPS BERT fine-tune correctness bugs,
infinite-loss/NaN reports), AnswerDotAI ModernBERT GLUE notebook
(lr 8e-5 full fine-tune, batch 32), ModernBERT issue #225 (small-task
fine-tunes from the base checkpoint underperform because the paper's
RTE/MRPC/STS-B checkpoints start from the MNLI mid-checkpoint).

Verdict on cause: **wrong technique, not bad data.**
1. Primary: single-lr 2e-5 for BOTH backbone layers and the freshly
   initialized linear head. A random head needs ~1e-3; 2e-5 is a ~50×
   underdose. Arithmetic: AdamW at lr 2e-5 moves a zero-init head ~7.6e-4
   in weight space over 38 steps — the head cannot escape its near-uniform
   output. Uniform 1/12 confidence + loss pinned at ln(12) matches exactly.
2. Secondary (unproven but documented): MPS BERT-training kernel
   correctness issues. Mitigated by moving training to CPU (inference
   cost trivial at this scale).
3. Data: 202 train examples (~17/class) is adequate for a LINEAR probe
   (GLUE RTE trains on 2.5k with far more classes per example than
   here). Not the failure.

## Diagnosis experiment (iter15_probe_diag.py, fold 0)

Linear probe at head-lr 1e-3, CPU: loss 2.48 → 2.12 over 20 epochs;
forced accuracy 60.4% (chance 8.3%). Features ARE informative — iter-14
was an optimization failure, confirming the research verdict.

Second finding: max-softmax on 12 classes/202 examples is severely
underconfident (max conf ~0.21 even when correct). Fixed tau (0.70-0.90
from iter-14) is structurally wrong for this head — calibration must be
train-side QUANTILE-based.

## Retrained two-stage (iter15_stage05_v2.py)

Per fold: linear probe (lr 1e-3, 30 epochs, class-balanced loss, CPU),
tau = train-side (1-q) quantile of max-softmax. Stage-0 = qwen3
centroid m0.030 unchanged; probe rescues abstains.

| q | tau | C | P | wrong | E2E | stage-0.5 added/ok/bad |
|---|---|---|---|---|---|---|
| 0.30 | ~0.152 | 45.6% | 96.5% | 4 | 91.22% | 25 / 23 / 2 |
| 0.40 | ~0.140 | 50.4% | 96.0% | 5 | 91.45% | 37 / 34 / 3 |
| 0.50 | ~0.133 | 56.4% | 95.0% | 7 | 91.44% | 52 / 47 / 5 |

Champion (stage-0 only, same corpus): C 35.6% P 97.8% E2E 90.70%.

## Correction (2026-09-08 audit)

The parse note above is wrong. There was no hash collision. The actual
cause: the fold/embed cache contains an ORPHANED entry,
`38443fca0b06b30e`, left behind when iter-6 EDITED a corpus case's text
in place (the cache is keyed by text hash and never prunes). The orphan
key still sits in fold-assignment.json, so `assign_folds` counts it in
`saved` and the corpus-vs-cache key sets disagree by exactly one — which
is the 275-vs-274 parse discrepancy. Immaterial to results (both runs
used consistent inputs), but the mechanism is cache-staleness, not a
hash collision. The original note is retained above for the record.

## Verdict

- Stage-0.5 now WORKS: the two-stage pipeline lifts coverage +10 to
  +20pts over stage-0 alone. Rescue precision (23/25, 34/37, 47/52 =
  90-92%) is below the bar but the ADDED routes are net-positive for
  E2E at every q.
- Bar check (E2E ≥ 91.78%): NOT met — best E2E 91.45% (q=0.40).
  M3's pre-registered rule says ModernBERT still doesn't earn the slot.
- However the margin is now 0.23pt, not 4pts — the entry bar is within
  reach of harvest-wave corpus growth (which lifts both stages) or one
  calibration refinement. Iteration 16 candidates: (a) corpus wave 4
  then re-check bar, (b) per-class quantile tau, (c) raise q slightly
  with margin-gated rescue (rescue only when stage-0 margin < 0.02 —
  the true ambiguous band).
