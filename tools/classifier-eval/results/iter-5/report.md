# Iteration 5 Report — Per-class margin floors (centroid head)

Date: 2026-09-08. Mechanical sweep. Corpus 189, 5-fold seed 42.
Sweep: iter5_sweep.py; summary results/iter-5/summary-*.json.

## Results

| config | C | P | wrong | E2E | SCORE |
|---|---|---|---|---|---|
| per-class, default 0.03 (= iter-4 winner) | 34.5% | 93.0% | 4 | 88.94% | +0.177 |
| per-class confusable-only tightening | 18.8% | 93.5% | 2 | 88.07% | +0.104 |
| per-class confusable + discourse abstain | 16.4% | 96.3% | 1 | 88.35% | +0.121 |
| per-class all-0.05 | 24.2% | 92.5% | 3 | 88.18% | +0.117 |
| per-class all-0.06 | 21.2% | 94.3% | 2 | 88.39% | +0.128 |
| uniform margin 0.040 | 25.5% | 92.9% | 3 | 88.34% | +0.129 |
| **uniform margin 0.045** | **18.8%** | **96.8%** | **1** | **88.67%** | **+0.146** |

## Findings

1. **No per-class config beats uniform 0.03 on SCORE.** Tightening
   confusables buys precision but costs 2-3× the coverage it saves;
   E2E peaks at default-0.03 (88.94%) and margin-0.045 (88.67%, P
   96.8%) is the best PRECISION-compliant config so far.
2. **The 4 wrong routes at 0.03, hand-audited:**
   - `write a hotfix for the null check crash` [code→debug, 0.829]:
     genuine code/debug ambiguity — adversarial case; LLM chain
     resolves correctly; acceptable abstain-vs-wrong trade.
   - `create a scheduled job for cleanup` [code→schedule, 0.784]:
     KNOWN corpus defect (iter-1: own intent absent from 5-NN). The
     code class contains a scheduling-shaped sentence.
   - `find comparison of monitoring tools` [search→analyze, 0.820]:
     search/analyze boundary; defensible either way — corpus should
     not have both at gold for this shape.
   - `summarize what was done today` [report→recall, 0.865]: report
     and recall are near-synonyms in this corpus (2-4 examples each).
3. **Corpus fixes beat gate fixes for 3 of the 4 wrongs.** Fix the
   labels/classes, not the threshold: (a) re-label or drop the
   scheduling-shaped code example, (b) merge or more-sharply-separate
   report/recall corpora, (c) split search-vs-analyze conventions in
   the corpus README. This is the confusion-driven corpus growth loop
   master.md intended, applied to BASE corpus defects rather than new
   adversarial cases.

## Decision

- Keep uniform margin 0.03 (SCORE winner) as the frontier point and
  margin 0.045 as the precision-compliant point; per-class floors
  REJECTED (complexity without E2E gain).
- Iteration 6: base-corpus repair (the 3 label defects above) +
  confusion-harvest adversarial additions; re-measure both frontier
  points. FIX agent not required (corpus-only edits, harness-verified).
