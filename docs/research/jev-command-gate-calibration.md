# Jev Command-Gate Calibration Research - Findings

> Plan: docs/plans/20261031-jev-command-gate/ (R0-research.md)
> Date: 2026-11-01
> Method: web research only (no code anchors consumed). Every claim carries a
> URL. This doc grounds the frozen contracts C1-C7 in master.md; where the
> research conflicts with a frozen contract, section 4 lists it explicitly.
> ASCII only.

## 1. Score thresholds + precision/recall tradeoffs for fail-closed gating

### 1.1 A threshold is a decision rule on top of a score, not a model property

A probabilistic classifier outputs a score; the threshold turns the score into
a decision. Threshold selection is separate from model training and can be
changed without retraining: the model produces the score, the threshold picks
one binary classifier from the family of all classifiers that score induces
[1] (https://www.tableau.com/blog/cost-sensitive-classifiers-better-machine-learning-decision-making),
[2] (https://addaly.com/learn/machine-learning-foundations/choosing-the-threshold).
Moving the threshold trades the two error types monotonically against each
other: lower threshold = higher recall, lower precision; higher threshold =
higher precision, lower recall. No threshold setting improves both at once;
that requires a better model [2].

The formal framing: threshold choice is a decision-theoretic problem of
minimizing expected classification loss under an operating condition (error
costs + class distribution). Different threshold-choice methods (fixed,
rate-driven, optimal cost-driven) map to different expected losses, and
calibration quality determines whether a chosen threshold means what it says
[3] (http://dl.acm.org/doi/10.5555/2503308.2503332).

### 1.2 The cost-asymmetry argument for fail-closed polarity

The core practice from the literature: assign a cost to each error type and
pick the threshold that minimizes total expected cost,
`cost = c_FP * FP + c_FN * FN`, evaluated over a threshold sweep on
validation data [2], [4]
(https://theneuralbase.com/deepeval/learn/advanced/threshold-calibration-methodology/).

For a safety gate on shell commands, the two error types are:

- False ALLOW (gate passes a dangerous command): potentially irreversible
  local side effect. This is the "false negative" of the safety filter.
- False ESCALATE (gate asks the human for a benign command): friction only.
  The human confirmation flow is the designed fallback and costs a click.

Every source on high-stakes deployment agrees the asymmetry is extreme and
resolves toward the safety direction:

- The LLM guardrails literature: "a missed harmful request (a false negative)
  is far costlier than a wrongly-blocked benign one", so recall is weighted
  above precision (F_beta with beta > 1) and the resulting threshold "lands
  well below 0.5" on a harmfulness probability. The same source states the
  fail-safe polarity directly: fail-open on guardrail unavailability is "not
  acceptable for consumer applications"; fail-closed (block + escalate) is
  the safe default for most applications [5]
  (https://prakashkagitha.github.io/llm-stack-book/12-production-mlops/04-safety-guardrails.html).
- The medical-analogy framing in scikit-learn's threshold-tuning doc: when a
  miss costs much more than a false alarm, classify as positive well below
  the naive 0.5 [6]
  (https://scikit-learn.org/stable/modules/classification_threshold.html).
- Microsoft's HAX pattern 10C: when system failure is more likely or more
  costly, fall back to a human (human-in-the-loop); the handoff must be
  clear and timely [7]
  (https://www.microsoft.com/en-us/haxtoolkit/pattern/g10-c-fall-back-to-other-strategies/).
- Maker-checker / approval-gate practice: require explicit human approval
  for irreversible actions, broad blast radius, and LOW MODEL CONFIDENCE
  (score below threshold -> escalate to human) [8]
  (https://github.com/LucaNerlich/lucanerlich.com/blob/main/docs/ai/human-in-the-loop.md).
- Production guardrail architecture: low-confidence or high-risk cases route
  to a human-review queue, and reviewer decisions are captured to feed the
  retraining loop [9]
  (https://beefed.ai/en/scalable-safety-filters-llms).

This matches the C3 contract exactly: score below threshold, low confidence,
Degraded, unparseable answer, or scorer unavailable all convert to the
confirmation flow (ASK), never silent allow. The operational argument: the
failure mode with the catastrophic tail (silent false-allow) is eliminated by
construction; the remaining error (over-asking) degrades gracefully into the
existing human flow and is also the mechanism that collects training data
(section 2).

### 1.3 Where to put the default threshold on a 0.0-1.0 rubric

Two complementary anchors from practice:

(a) Cost-ratio closed form. For a well-calibrated score, the loss-minimizing
threshold is t = c_FP / (c_FP + c_FN) [2]. For a command gate, read c_FP as
the cost of a false escalate (human click, seconds) and c_FN as the cost of a
false allow (potentially destructive). With even a conservative 10:1 to 20:1
ratio, the optimal allow-threshold lands around 0.90-0.95 when the score is
P(benign) -- i.e. the gate demands near-certainty of safety before
auto-allowing.

(b) Empirical default bands from deployed 0-1 guardrail scorers. Practitioner
deployments of 0.0-1.0 safety scorers converge on a narrow high band:
EvalGuard reports a default of 0.8 as the empirical breakpoint where the
false-positive rate of the grader drops sharply, with "most teams land within
0.05 of 0.8 after two iterations"; lower than that flags legitimate edge
cases, higher than ~0.85 starts missing real failures; and high-stakes
healthcare deployments tighten to 0.9 [10]
(https://evalguard.ai/docs/concepts/scoring-thresholds). Cisco's FPR
calibration for security detectors maps a fixed calibrated scale in which
operational allow/block tiers sit at 0.50 / 0.70 / 0.85 with target benign
FPRs from 1-in-1000 down to 1-in-100,000, noting thresholds should keep a
stable operational meaning across model releases and be picked from a
threshold sweep on held-out data, never on the training set [11]
(https://blogs.cisco.com/ai/consistent-security-model-deployment-with-fpr-calibration),
[12] (https://github.com/cisco-ai-defense/fpr-model-calibration).

RECOMMENDED DEFAULT BAND (what leaves 01-03 should freeze): initial gate
threshold in the 0.85-0.90 range on the 0.0-1.0 score (score >= threshold
AND confidence >= its own floor required to ALLOW), with the exact value
fixed by the teacher sweep in leaf 03 on held-out replay data -- not picked
by intuition and not re-tuned afterwards. Rationale: (i) the cost closed form
puts the optimum at a high band for any plausible cost ratio; (ii) deployed
0-1 guardrails converge on 0.8-0.9 with high-stakes deployments at 0.9;
(iii) the sweep-on-validation / freeze / never-tune-on-test discipline is
standard [2], [4], [6], [11].

Two calibration discipline points the sweep must respect:

- Never tune the threshold on the same data used to train/evaluate the
  scorer; tune on a validation split, report on held-out test [6], [2].
- If the score is not a calibrated probability, the closed form (a) does not
  apply directly; the sweep on replay data is then the primary method, and
  per-source FPR-calibration practice (map scores to a fixed operational
  scale so a threshold keeps its meaning across scorer releases) is the
  pattern for keeping the frozen threshold meaningful across future scorer
  retraining [11], [12].

### 1.4 Risk/coverage framing (secondary, for the eval harness)

Selective prediction gives the harness a better report shape than a single
operating point: the risk-coverage curve (risk on accepted samples as
coverage grows) characterizes the whole threshold family; a model can have
identical accuracy but drastically different selective behavior, so the
harness should report the curve (or AURC/AUGRC), not only metrics at one
threshold [13]
(https://github.com/ercedut/selective-prediction-toolkit), [14]
(https://arxiv.org/pdf/1705.08500), [15]
(https://arxiv.org/html/2407.01032v1). Leaf 03's replay-ruler reporting can
present teacher/student sweeps as risk-coverage-style curves over the score
threshold, which also makes the floor comparison (section 3) honest: compare
at matched coverage, not at arbitrarily chosen points [14], [15].

## 2. Contrastive/DPO curation from approval decisions (near-miss pairing)

### 2.1 Approvals-as-training-data is established practice

Capturing human-review decisions to feed the retraining loop is a standard
component of production guardrail architecture: the HITL adjudication queue
captures "reviewer decisions to feed the retraining loop", and automated
pipelines consume labeled HITL data on a schedule [9]. The row shape frozen
in C5 (prompt = command context, chosen = human decision, rejected = the
model's contrary position) is a valid DPO pair construction: DPO trains on
(prompt, chosen, rejected) triples without a separate reward model [16]
(https://arxiv.org/abs/2606.19607 - which instantiates exactly this pipeline
shape for DPO post-training).

### 2.2 Near-miss (contrastive, minimal-difference) pairs

The evidence that *close* pairs carry the learning signal:

- "What matters in data for DPO?" (NeurIPS 2023): contrastiveness between
  chosen and rejected responses helps -- but primarily by improving the
  chosen sample's quality signal; the quality of the CHOSEN response dominates
  DPO performance while the quality of the rejected response has relatively
  limited impact [17]
  (https://dl.acm.org/doi/10.52202/085713-1488). For the command gate: the
  human verdict (chosen) is the trustworthy side of every pair; the model's
  prior position (rejected) needs no curation.
- Curriculum-DPO: pairs whose two sides are far apart are "easy" (the model
  learns little beyond what it already knows); pairs with near-identical
  quality are "hard" and carry the fine discrimination signal; training that
  schedules easy -> hard (i.e. eventually trains on near-miss pairs)
  outperforms single-pair DPO on multiple benchmarks [18]
  (https://aclanthology.org/2024.findings-emnlp.754.pdf).
- Comparison curation theory: from a pool of candidate pairs per prompt, the
  informative pairs to label are the ones that resolve the decision
  boundary; indiscriminate all-pairs labeling wastes budget [16]. The
  near-miss rule in C5 (same command prefix, different human verdicts ->
  one contrastive pair per minimal differing group) is exactly a
  boundary-focused selection: it labels the region where the same context
  yields opposite human outcomes.
- Industrial safety practice corroborates the near-miss concept directly:
  near-miss reports (events that almost caused harm, differing from real
  harm by a small margin) are leading indicators and are systematically
  coded and learned from in safety management [19]
  (https://pmc.ncbi.nlm.nih.gov/articles/PMC7458492/), [20]
  (https://stacks.cdc.gov/view/cdc/215756).

PAIR-MIXING RATIO guidance (concrete, for leaf 02): no cited source mandates
a single universal ratio; the defensible synthesis is (i) keep ALL record
pairs (every human verdict is a chosen-quality signal, per [17]), (ii) add
near-miss pairs as the high-value minority -- the curated-mixture literature
shows a *smaller, boundary-focused* mixture beats a larger undifferentiated
one (UltraMix: 30% smaller than the best single corpus, better performance,
after removing noisy/redundant samples and keeping pairs with verified
preference order) [21]
(https://arxiv.org/html/2511.10985). Practical starting mix: near-miss pairs
supplementing (not replacing) record pairs, with near-miss groups deduped to
one pair per minimal differing prefix (the C5 rule), and a cap on the
near-miss share only if the training loop observes overfitting to the small
group (an open question, section 4).

### 2.3 Label noise handling

Human approval labels are noisy (inconsistent approvers, ambiguous context).
The literature gives three tiers of defense, all applicable to the C4/C5
pipeline:

1. Prevent at curation: filter/verify preference order with an independent
   signal before training. UltraMix keeps only pairs where a reward-model
   signal confirms chosen > rejected, and finds that misaligned preference
   orders "are common and degrade performance"; single-signal filtering is
   insufficient -- combine signals (input quality + preference-reward
   validation + dedup) [21].
2. Robust loss: rDPO re-weights the DPO loss by the estimated label-flip
   rate, provably robust to random preference flips; simple label smoothing
   is a weaker version of the same idea [22]
   (https://doi.org/10.48550/arxiv.2403.00409). R3M treats corrupted labels
   as sparse outliers via L1-regularized reward learning and extends to DPO
   [23] (https://papers.nips.cc/paper_files/paper/2024/file/e04101138a3c94544760c1dbdf2c7a2d-Paper-Conference.pdf).
   PrefCleanBench benchmarks 13 preference-data cleaning methods and finds
   cleaning before alignment is broadly effective [24]
   (https://dlnext.acm.org/doi/10.52202/085713-4149).
3. Consistency screening: train a cheap proxy scorer on the pairs, drop
   pairs whose preference order contradicts the proxy (self-curation) [25]
   (https://aclanthology.org/2025.naacl-long.606.pdf); fDPO discards pairs
   whose chosen side scores below the model's own generation during
   training [26] (https://doi.org/10.18653/v1/2024.emnlp-main.1266).

For this tree, the training loop lives OUTSIDE the repo (master.md scope);
the repo-side obligation is only to (a) record enough metadata for these
filters to run (C4's approver, prior score/confidence/degraded fields) and
(b) not bake noise into the near-miss derivation: the C5 prefix rule is
deterministic and programmatic, which is the right repo-side posture.
Estimating an overall flip rate for rDPO-style training is the external
loop's job.

## 3. Teacher-gate methodology

### 3.1 Why the teacher must beat a floor before distillation

Distillation only helps if the teacher's outputs are more informative than
what the student already has: "the teacher should be substantially better
than a student trained without distillation. If the teacher and student have
similar accuracy, distillation provides little benefit because the soft
labels are not much more informative than what the student could generate on
its own" [27]
(https://mbrenndoerfer.com/writing/knowledge-distillation-temperature-teacher-student-llm).
A teacher that is merely different from (not better than) the rule-based
floor transfers no net signal and risks distilling its own errors; the
literature on teacher selection shows "a teacher model with superior
performance does not necessarily result in a stronger student", which is
precisely why an explicit gate -- rather than assuming teacher quality
correlates with student quality -- is the right control [28]
(https://ar5iv.labs.arxiv.org/html/2305.09651v1).

Gating distillation on a measured teacher-vs-baseline comparison is
established practice:

- Hard Gate Knowledge Distillation gates each supervision decision on a
  calibrated teacher-vs-reference comparison -- the teacher is treated "not
  only as a source of knowledge but also as a gauge", and ungated
  distillation from an unqualified teacher measurably hurts [29]
  (https://aclanthology.org/2022.emnlp-main.665.pdf).
- Production promotion gates measure teacher and student on the SAME
  held-out split at the same attempt count and promote only if the student
  reaches a configured fraction of the teacher's rate with no regression
  against the pre-training baseline -- all three numbers persisted in a gate
  record [30]
  (https://github.com/experientiallabs/world-model-optimizer/blob/main/wmo/distill/gate.py).
- Teacher calibration matters as much as raw accuracy for student quality:
  per-class calibration of teacher scores is the property that most affects
  robust-student performance [31]
  (https://proceedings.mlr.press/v216/wang23e/wang23e.pdf). This supports
  the C1 design of carrying an explicit Confidence field alongside the score.

The C7 shape (teacher MUST beat the rule-based floor -- the
shell.GetRiskLevel mapping -- on held-out replay before ANY student run;
gate verdict PASS/FAIL persisted in the report) matches [29] + [30] exactly:
a like-for-like comparison on one held-out split, a frozen verdict, and a
hard stop on the student pipeline when the gate fails.

### 3.2 Frozen threshold from the teacher sweep, never re-tuned on the student

The threshold-freeze discipline is standard across the threshold-selection
and guardrail sources:

- The threshold is a fitted parameter: "Choosing the threshold on the test
  set" is listed failure #1; choose on validation, measure on test [2].
- deepeval methodology: select the threshold minimizing weighted cost, then
  "validate on held-out test set. Most teams skip step 5 and contaminate
  their calibration, leading to overfitted thresholds that fail in
  production" [4].
- scikit-learn: "never use the same data for training the classifier and
  tuning the decision threshold due to the risk of overfitting" [6].
- Once frozen, keep the threshold's operational meaning across scorer
  releases by shipping a recalibration artifact per release rather than
  re-tuning the downstream policy threshold [11], [12].

Application to C6/C7: the teacher sweep on the held-out replay corpus
produces the threshold; it is frozen into the contract file (hash into the
training manifest); the student is evaluated AT that threshold, and student
results must never move it. If the student later warrants a different
operating point, the correct loop is a new teacher-sweep + re-freeze (a new
contract hash), not an in-place re-tune. This is also what makes the
teacher-gate comparison meaningful: both teacher and floor are scored at the
same frozen point (or reported as full curves compared at matched coverage,
section 1.4).

### 3.3 Report schema corroboration for leaf 03

The C7 report fields map to established artifacts: floor/teacher/student
metric blocks [30]; a PASS/FAIL gate verdict with a reason string citing the
compared numbers [30]; frozen_threshold as the persisted fitted parameter
[2], [4]; and (recommended) a risk-coverage-style sweep summary rather than
only single-point metrics [13], [15]. No conflict with C7's field list; the
sweep summary is additive.

## 4. Open questions + conflicts vs contracts C1-C7

### Conflicts

- C3 confidence pre-condition vs self-reported confidence reliability:
  C3 requires result.Confidence >= confThreshold to allow. The literature
  warns that "models do not ship reliable calibrated confidence scores out
  of the box" and structured self-assessed confidence should be "treated as
  hint, not truth" [8]. This is NOT a conflict with the fail-closed polarity
  (low confidence still escalates, so unreliability only costs friction, per
  section 1.2), but the default confThreshold should be set LOW (treat
  confidence as a weak veto signal, not a strong allow signal) until the
  teacher sweep measures how well Confidence correlates with correctness on
  replay. Flagged for leaves 01/03; no contract edit made.
- C1 score semantics vs calibration: the closed-form threshold (section
  1.3a) is only valid for a calibrated probability [2], [11]. The C6 rubric
  is anchored-band, not probability-calibrated. Not a conflict (the teacher
  sweep is the primary method and does not require calibration), but the
  contract should not be read as "score = probability of safety".
- No conflicts with C2, C4, C5, C6, C7 found: the byte-stable
  serialization, two-chokepoint capture, DPO row shape, prefix-based
  near-miss derivation, and teacher-gate report schema are each consistent
  with the surveyed practice (sections 2.1, 2.2, 3.1, 3.3).

### Open questions (unresolved; stay out of contracts)

1. Near-miss pair cap / mixing ratio: no source pins a universal ratio;
   the record + near-miss mix (section 2.2) is a starting heuristic. The
   external training loop should treat the ratio as a tunable with
   UltraMix-style multi-signal filtering [21].
2. Label-flip rate for robust DPO: rDPO needs an estimated flip rate [22];
   the capture store does not (and should not) estimate it. Who estimates
   it, and from what inter-approver disagreement signal, is external-loop
   scope.
3. Confidence calibration of the LLM-as-scorer: whether the model's
   self-reported Confidence field has any monotone relationship with score
   correctness on command scoring is unknown until leaf 03's replay data
   exists.
4. Score drift over time: practitioner practice recalibrates when the score
   distribution drifts from baseline [4], [11]; how drift detection maps
   onto the frozen-contract/hash discipline (re-freeze cadence, who owns
   it) is unresolved and deliberately outside C1-C7.
5. Risk-coverage reporting adoption: whether leaf 03's report adds a sweep
   summary beyond the frozen single point (recommended in 1.4/3.3) is a
   scope decision for 03, not a contract requirement.

## Source list (distinct URLs)

1. https://www.tableau.com/blog/cost-sensitive-classifiers-better-machine-learning-decision-making
2. https://addaly.com/learn/machine-learning-foundations/choosing-the-threshold
3. http://dl.acm.org/doi/10.5555/2503308.2503332
4. https://theneuralbase.com/deepeval/learn/advanced/threshold-calibration-methodology/
5. https://prakashkagitha.github.io/llm-stack-book/12-production-mlops/04-safety-guardrails.html
6. https://scikit-learn.org/stable/modules/classification_threshold.html
7. https://www.microsoft.com/en-us/haxtoolkit/pattern/g10-c-fall-back-to-other-strategies/
8. https://github.com/LucaNerlich/lucanerlich.com/blob/main/docs/ai/human-in-the-loop.md
9. https://beefed.ai/en/scalable-safety-filters-llms
10. https://evalguard.ai/docs/concepts/scoring-thresholds
11. https://blogs.cisco.com/ai/consistent-security-model-deployment-with-fpr-calibration
12. https://github.com/cisco-ai-defense/fpr-model-calibration
13. https://github.com/ercedut/selective-prediction-toolkit
14. https://arxiv.org/pdf/1705.08500
15. https://arxiv.org/html/2407.01032v1
16. https://arxiv.org/abs/2606.19607
17. https://dl.acm.org/doi/10.52202/085713-1488
18. https://aclanthology.org/2024.findings-emnlp.754.pdf
19. https://pmc.ncbi.nlm.nih.gov/articles/PMC7458492/
20. https://stacks.cdc.gov/view/cdc/215756
21. https://arxiv.org/html/2511.10985
22. https://doi.org/10.48550/arxiv.2403.00409
23. https://papers.nips.cc/paper_files/paper/2024/file/e04101138a3c94544760c1dbdf2c7a2d-Paper-Conference.pdf
24. https://dlnext.acm.org/doi/10.52202/085713-4149
25. https://aclanthology.org/2025.naacl-long.606.pdf
26. https://doi.org/10.18653/v1/2024.emnlp-main.1266
27. https://mbrenndoerfer.com/writing/knowledge-distillation-temperature-teacher-student-llm
28. https://ar5iv.labs.arxiv.org/html/2305.09651v1
29. https://aclanthology.org/2022.emnlp-main.665.pdf
30. https://github.com/experientiallabs/world-model-optimizer/blob/main/wmo/distill/gate.py
31. https://proceedings.mlr.press/v216/wang23e/wang23e.pdf
