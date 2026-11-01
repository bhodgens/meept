# Jev Command-Gate Research - Research Leaf

> **For the implementing agent:** You are the researcher for this leaf.
> This is a RESEARCH leaf: the deliverable is a findings document, not code.
> Use `web_search` (and web_extract where a source warrants depth) for every
> factual claim; do not answer from memory. Do NOT commit - the orchestrator
> handles all git operations after review.

## Meta

- **Parent:** ../master.md
- **Scope:** Calibration research for command-safety score gating and the
  approvals-as-training-data loop, grounding the threshold, rubric shape, and
  teacher-gate methodology used by leaves 01-03.
- **Dependencies:** none
- **Estimated Context:** 25K
- **Concurrency Group:** A
- **Audit references:** proposal 12 (close the gap)

## Goal

Produce `docs/research/jev-command-gate-calibration.md`: an ASCII-only,
cited findings doc that (a) surveys score-threshold calibration practice for
fail-closed safety classifiers, (b) surveys contrastive/DPO training-data
curation from approval decisions with near-miss pairing, and (c) pins the
teacher-gate methodology. Leaves 01-03 consume its conclusions; where the
research conflicts with this tree's frozen contracts, report the conflict -
do NOT edit contract values.

## Context

The daemon gates shell commands with categorical risk (HIGH/MEDIUM/...) and a
binary-confidence decision lane, but has no graded safety score. This leaf
supplies the external evidence for the design choices already frozen in
master.md contracts C1-C7: 0.0-1.0 score bands, a fail-closed threshold,
near-miss contrastive pairs from human approvals, and a teacher floor gate.

## Verified Anchors

(No code anchors consumed - research leaf. Tree-internal references:)

- internal/tools/builtin/decision_gate.go:150-158 - existing answer+confidence
  two-bar gate (fail-open lane); research should note classifier calibration
  work relevant to threshold placement.
- internal/shadow/exporter.go:208 - existing DPO export; research context for
  preference-pair curation from human decisions.

## Interface Contracts (From Parent)

### What This Leaf Exposes

```
docs/research/jev-command-gate-calibration.md with sections:
  1. Score thresholds + precision/recall tradeoffs for fail-closed gating
     (must state: recommended default threshold band for the 0.0-1.0 rubric,
     and the operational cost asymmetry argument for fail-closed).
  2. Contrastive/DPO curation from approval decisions (must state: near-miss
     pairing practice, pair-mixing ratios, label-noise handling).
  3. Teacher-gate methodology (must state: why the teacher must beat a
     rule-based floor before student distillation, and how to hold the
     threshold frozen from the teacher sweep).
  4. Open questions (explicit list; unresolved items stay out of contracts).
Each claim carries a URL citation.
```

### What This Leaf Consumes

- None (research leaf).

## Tasks

| Task ID | Objective | Files | Verify command | RED reason | PASS state | Given/When/Then |
|---------|-----------|-------|----------------|------------|------------|-----------------|
| T1 | Survey threshold calibration + precision/recall for fail-closed safety score gates; recommend a default band for the 0.0-1.0 rubric | Create: docs/research/jev-command-gate-calibration.md (section 1) | `grep -c 'http' docs/research/jev-command-gate-calibration.md` >= 6 | file absent | section 1 present, >= 4 distinct cited sources, default band stated | Given the C6 rubric bands; When calibration literature is surveyed; Then a concrete default threshold band is recommended with an operational-cost rationale for fail-closed |
| T2 | Survey approvals-as-training-data: DPO/contrastive curation from human approval decisions, near-miss pairing | same file (section 2) | `grep -c 'http' ...` >= 6 | section absent | section 2 present, near-miss pairing practice + pair ratios + noise handling cited | Given human allow/deny records with prior scores; When curation literature is surveyed; Then near-miss pairing guidance is stated concretely enough to freeze the C5 prefix rule |
| T3 | Pin teacher-gate methodology: teacher must beat a rule-based floor before student distillation; threshold frozen from teacher sweep, never re-tuned on student | same file (section 3) | `grep -c 'http' ...` >= 6 | section absent | section 3 present with >= 2 supporting citations or precedents | Given a rule-based floor (shell.GetRiskLevel mapping); When distillation methodology is surveyed; Then the floor-beating gate + frozen-threshold practice is justified and 03's report schema matches it |
| T4 | Write open questions + conflicts-vs-contracts list | same file (section 4) | `grep -q 'Open questions' docs/research/jev-command-gate-calibration.md` | section absent | unresolved items listed; zero silent conflicts with C1-C7 | Given research conclusions; When compared to frozen contracts; Then every divergence is listed explicitly (or "no conflicts") |

Worked expansion (T1): run `web_search` queries such as
"classifier score threshold calibration precision recall fail-safe default",
"abstention classifer high-stakes threshold selection", "selective prediction
risk coverage"; extract per-source: threshold practice, metric framing
(precision-at-high-recall vs recall-at-high-precision), and failure cost
asymmetry. Write section 1 only from gathered sources; ASCII only.

## Self-Verification Checklist

- [ ] All 4 tasks complete; the doc exists at the exact path
- [ ] Every factual claim carries a URL citation (>= 6 total distinct URLs)
- [ ] Sections 1-3 each state the concrete conclusion leaves 01-03 consume
- [ ] Open-questions section lists conflicts vs C1-C7 explicitly
- [ ] ASCII only (no unicode punctuation, no smart quotes)
- [ ] No scope creep - no code changes, no contract edits

**DO NOT COMMIT.** The orchestrator handles all git operations after review.

## Completion Report Format

1. **Files touched:** docs/research/jev-command-gate-calibration.md
2. **Verify command + output:** the grep counts above
3. **Deviations:** [none / list with rationale]

## Review Checklist (For Review Agent)

- [ ] Doc exists; all 4 sections present with citations
- [ ] Claims are source-backed, not model-memory; URLs resolve plausibly
- [ ] Recommended threshold band + near-miss rule + teacher-gate method are
      concrete (a downstream leaf could freeze them)
- [ ] No contract files were edited; ASCII only

Output: APPROVED or specific gaps.

## Notes

- This leaf runs BEFORE 01-03 dispatch; the orchestrator inlines its
  recommended-band conclusion into the 03 dispatch context.
- Budget: ~6-10 web searches, one document. Do not spiral - stop at the first
  solid source per claim.
