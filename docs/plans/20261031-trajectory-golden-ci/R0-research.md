# Trajectory Evaluation Research - Implementation Leaf

> **For the implementing agent:** You are the researcher for this leaf.
> Complete ALL tasks below. This is a RESEARCH leaf: the deliverable is a
> written digest (in your completion report), not code. Do NOT commit -
> the orchestrator handles all git operations after review. Do NOT use
> read_file on existing source files - explore with search_files or
> terminal cat.

## Meta

- **Parent:** ../master.md
- **Scope:** Ground the comparator design in current trajectory-evaluation
  practice so the pinned match modes and tolerance scheme are defensible.
- **Dependencies:** none (dispatches first)
- **Estimated Context:** 25K
- **Concurrency Group:** A
- **Audit references:** informs master C2 scoring; Requirement R9

## Goal

Produce a cited digest of best practices for grading agent trajectories
in CI, covering the four mandated topics, and give a KEEP/AMEND verdict
on the frozen contract C2 (MatchExact / MatchInOrder / MatchAnyOrder,
threshold >= 1.0 default, LCS-based score). The orchestrator uses the
verdict either to confirm C2 or to amend it BEFORE leaf 01 dispatches.

## Context

Meept will gate scripted agent runs on their tool-call sequence against
committed goldens. The comparator must tolerate benign variation
(retries, interleaved reads, argument shape drift) while catching real
regressions (dropped verify step, reordered read-after-write). The repo
has no golden-trajectory concept yet; internal/eval already grades
outcomes via Judge(). This leaf is pure web research plus a verdict -
no repo changes.

## Verified Anchors

- [docs/plans/20261031-trajectory-golden-ci/master.md] - contract C2 -
  the frozen scoring scheme this leaf verdicts.
- [internal/eval/judge.go:152] - Judge() - existing outcome grading the
  comparator complements (do not duplicate it).

## Interface Contracts (From Parent)

### What This Leaf Exposes

A research digest (in the completion report) with:
- Per-topic findings with source URLs.
- One explicit verdict line: `C2 VERDICT: KEEP` or
  `C2 VERDICT: AMEND <specific change>`.

### What This Leaf Consumes

- Master policy section (blocking gate, scripted-only goldens,
  investigate-then-regenerate) - taken as given, verdicts may flag risk.

## Tasks

| Task ID | Objective | Files | Verify command | RED reason | PASS state | Given/When/Then |
|---------|-----------|-------|----------------|------------|------------|-----------------|
| T1 | Survey trajectory grading practice: tau-bench / tau2-bench outcome+process reward structure; Anthropic-style outcome-vs-trajectory grading; agentevals and Google ADK match modes (strict/unordered/subset/superset; EXACT/IN_ORDER/ANY_ORDER) | none (research) | digest contains >= 4 distinct cited sources | no citations | digest with URLs | Given the four mandated topics; When sources are surveyed; Then each topic has >= 1 cited finding |
| T2 | Survey ordered-subsequence / LCS tolerance scoring and flake-resistant CI gating of nondeterministic agents (threshold semantics, pass^k vs pass@1, suite-level flake attribution, replay-vs-rerun gating) and record concrete recommendations | none (research) | digest contains recommendations section | no recommendations | actionable recommendations list | Given nondeterministic agent behavior; When CI gates on sequences; Then the digest names tolerance mechanisms that avoid flaky gates |

**Worked example (canonical row expansion):**

T1:
1. Search: "tau-bench trajectory evaluation", "agentevals trajectory
   match modes", "ADK TrajectoryEvaluator IN_ORDER", "process reward
   tool call sequence grading".
2. For each finding, record: source URL, mechanism, what it implies for
   C2.
3. Verify: the digest lists >= 4 sources with at least one per topic.

## Self-Verification Checklist

- [ ] All tasks completed; digest covers all four mandated topics
- [ ] Every finding carries a source URL
- [ ] Explicit C2 verdict line present
- [ ] No scope creep - research only, no repo edits

**DO NOT COMMIT.** The orchestrator handles all git operations after review.

## Completion Report Format

1. **Files touched:** none expected (research leaf)
2. **Digest:** findings per topic + recommendations + C2 verdict line
3. **Deviations:** [none / list with rationale]

## Review Checklist (For Review Agent)

- [ ] Digest covers all four mandated topics with citations
- [ ] C2 verdict is explicit and actionable
- [ ] Recommendations are concrete enough to act on (or the leaf says
      "no action")
- [ ] No repo files were modified

Output: APPROVED or specific gaps.

## Notes

Known good starting points (verify currentness yourself):
github.com/sierra-research/tau2-bench docs/evaluation.md (reward_basis
product semantics); google/adk-python trajectory_evaluator.py (EXACT /
IN_ORDER / ANY_ORDER implementations); LangChain agentevals
trajectory_match modes + tool_args_match_mode; pass^k reliability
framing (per-trial success compounded); deterministic replay-based CI
gates for agent traces.
