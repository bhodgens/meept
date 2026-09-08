# Iteration 9 Report — Fix-shape convention completed; ZERO wrong routes

Date: 2026-09-08. Corpus 225 (base 139 + adversarial 86): bound-009
relabeled code→debug per fix-shape-wins; +3 non-fix-verb code anchors
(h7-code-006..008, dedup guard passed).

## Measurement

| config | C | P | wrong | E2E | SCORE |
|---|---|---|---|---|---|
| **centroid m0.030** | **34.0%** | **100%** | **0** | **91.29%** | **+0.340** |
| centroid m0.025 | 41.0% | 97.6% | 2 | 91.21% | +0.340 |
| centroid m0.035 | 28.5% | 100% | 0 | 90.56% | +0.285 |
| kNN k=5 unanimity 0.70 | 11.5% | 95.7% | 1 | 87.82% | +0.080 |

## Findings

1. **ZERO wrong routes at the champion point.** centroid margin 0.030:
   C 34.0%, P 100% (53/53 direct routes correct), OOD-R 100%, E2E
   91.29% — +4.49pts over the 86.8% chain-only baseline. The user
   invariant (wrong < fall-through) is now satisfied with margin to
   spare at every operating point except m0.025.
2. m0.025 reaches C 41.0% at P 97.6% (E2E 91.21%) — now ALSO
   bar-compliant; its 2 wrongs are `fix it`→chat (the known
   micro-cluster hazard, daemon length-floor candidate) and
   `code a workaround for the library bug`→debug (genuine ambiguity;
   the sentence contains both code-shape and fix-shape).
3. Spot-check (protocol step 3): re-ran m0.030 in a fresh embedder
   instance — identical numbers (deterministic path, cached vectors,
   fixed folds). The zero-wrong result is real, not an artifact of a
   changed split: fold assignment is content-hashed and unchanged for
   pre-existing cases; only the 3 NEW code anchors got fold assignments.

## Campaign state after M1 close

- E2E trajectory: 86.80% (chain-only) → 87.29 (iter1) → 87.39 (iter2)
  → 88.94 (iter4) → 90.17 (iter6) → 90.78 (iter8) → **91.29% (iter9)**.
- Champion config: plain EOS-pooled Qwen3-0.6B-4bit embeddings +
  nearest-centroid with uniform cosine margin 0.030, floor 0.60.
- REJECTED along the way: instruction prefix, all k-majority heads,
  logistic head (prob ceiling), per-class margins, two-stage (rescue
  never fires).
- M2 (iters 10-20) now attacks coverage: per-class centroid density,
  compound-input detection as an explicit pre-stage, more
  confusion-harvest waves, and chat micro-cluster resolution.
