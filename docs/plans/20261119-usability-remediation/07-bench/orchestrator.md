# 07-BENCH - Acceptance Harness Hardening + Integration Gate

> **For the executing agent:** Branch orchestrator. Test gate first,
> validate in-session, then impl leaves; review IN-SESSION; commit after
> review. Do NOT implement code yourself. Worktree:
> .hermes/worktrees/usability-remediation. THIS BRANCH RUNS LAST (integration
> gate) - dispatch only after 01-06 REVIEWED.

## Meta

- **Role:** Branch
- **Parent:** ../master.md
- **Children:** 00T-gate.md + 2 implementation leaves
- **Scope:** Outcome-level acceptance for the whole remediation; the bench
  becomes the regression gate for interactive usability.

## Goal

Close I8 + R6/R7/R8 acceptance: harden the usability-probe suite (real
checkers, terminal-delivery assertion, reply-quality rubrics), run the full
battery on the isolated quota-free rig, compare against the 2026-11-19
baseline, and prove no CRITICAL finding reproduces.

## Architecture

Two leaves: harness hardening (01, meept-bench repo - NEW checkers + suite
edits; separate repo /Users/caimlas/git/meept-bench, worktree there too),
integration run + report (02, rig battery + comparison report). 01 is
meept-bench-side (Go); 02 is ops/run (no code, evidence). Sequential: 02
after 01. The meept-bench worktree is SEPARATE from the meept worktree
(cross-repo leaf - brief names both paths).

## Interface Contracts

Consumes C1-C7 (all daemon contracts) as assertion targets. Produces:
terminal-delivery checker (every submitted turn -> non-parked terminal
within deadline OR explicit actionable rejection), reply-quality llm_judge
rubrics (clutter/scaffolding/empty-reply), hardened usability-probe suite,
integration comparison report.

## Child Index

| # | Document | Type | Dependencies | Est. Context | Concurrency |
|---|----------|------|--------------|--------------|-------------|
| 00T | 00T-gate.md | leaf (tests) | none | 55K | A |
| 01 | 01-harness-hardening.md | leaf | 00T | 80K | A |
| 02 | 02-integration-run.md | leaf (ops) | 01, ALL branches REVIEWED | 70K | B (last) |

## Dispatch Protocol

Phase 0: 00T gate RED (checker unit tests in meept-bench), validate
in-session, commit, TESTS_VALIDATED. Phase 1: 01 (harness code). Phase 2:
02 (integration run) ONLY after root confirms 01-06 REVIEWED. Review
in-session; commit exact paths (meept-bench worktree for 01); max 3 cycles
then BLOCKED. Briefs: both repo worktree paths, consumed contracts,
no-commit/no-git-add rules. 02 is an OPS leaf - verify files-exist +
verdicts, not tests-pass (asset-pipeline pattern).

## Review Checklist

- [ ] All leaf tasks implemented; verify commands pass
- [ ] Gate tests GREEN and unmodified
- [ ] terminal-delivery checker catches F-ORCH-26/28 class (mutation-verified)
- [ ] reply-quality rubrics never leak expected answers (judge lint)
- [ ] Integration report compares per-task vs baseline; CRITICAL reproductions = 0
- [ ] Rig isolation proven (never live socket); quotas disabled proven
- [ ] No scope creep / artifacts / corruption

## Coding Conventions

Per ../master.md + meept-bench skill. Branch specifics: checkers read ONLY
the current run's transcript dir (MEEPT_BENCH_TRANSCRIPT_DIR, never bare
results/** glob); trusted evaluator code hash-checked, never from agent
worktree; judge rubrics lint against literal expected answers; suite
prompts FROZEN (never reword to pass).

## Completion Tracking Table

| Child | Status | Iterations | Review Notes |
|-------|--------|------------|--------------|
| 00T-gate | PENDING | 0 | |
| 01-harness-hardening | PENDING | 0 | |
| 02-integration-run | PENDING | 0 | |

## Integration Test Plan

1. meept-bench: `go build ./... && go test -p 2 ./internal/checkers/...
   ./internal/suite/...` (worktree).
2. Rig battery (02): rebuild daemon from meept worktree HEAD; boot isolated
   rig (quota-free, local models); run usability-probe suite + p1-p6 probes
   SERIALLY; run suites/regression.json (dev gate).
3. Acceptance: 0 CRITICAL reproductions; terminal-delivery green for every
   submitted turn; reply-quality >= baseline; regression suite no new fails.
4. Report: /tmp/usability-rig/REPORT-REMEDIATION-ACCEPTANCE.md (per-task
   prior vs new verdict + evidence paths + residual findings).

## Test Gate

Test leaf: 00T-gate.md. Matrix (root F14):

| # | Feature | Source | Unit tests | E2E tests | Status |
|---|---------|--------|-----------|-----------|--------|
| F14a | terminal-delivery checker | 01 T1 | TestTerminalDeliveryChecker (mutation: missing terminal -> fail) | bench self-test | PENDING |
| F14b | reply-quality rubrics | 01 T2 | TestReplyQualityJudgeLint, TestNoAnswerLeak | bench self-test | PENDING |
| F14c | hardened suite + real plan checker | 01 T3 | TestSuiteLoads, TestPlanArtifactChecker | bench validate-then-run-zero | PENDING |

Delta log: (none yet)

## Open Questions

- None blocking. Judge model = rig 8B (judge-local.py); double-judge for
  disagreement >0.20 per meept-bench skill.

## Notes

- Baseline: /tmp/usability-rig/bench-results-clean/ (3 pass / 3 fail) +
  REPORT.md section 8. The concurrent run-1 is load-contaminated - compare
  against the CLEAN baseline only.
- meept-bench has its own worktree convention; create
  /Users/caimlas/git/meept-bench/.hermes/worktrees/usability-acceptance.
