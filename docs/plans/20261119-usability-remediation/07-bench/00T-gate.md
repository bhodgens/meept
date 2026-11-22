# 07-BENCH Test Gate - Implementation Leaf (TESTS ONLY)

> **For the implementing agent:** Write tests ONLY for this branch's
> harness-hardening leaf. No implementation code. Every test fails RED for
> a missing-implementation reason. Do NOT commit. Do NOT git add. Explore
> with search_files/terminal cat; write once, never read back. Worktree:
> /Users/caimlas/git/meept-bench/.hermes/worktrees/usability-acceptance
> (create from meept-bench HEAD if absent).

## Meta

- **Parent:** orchestrator.md
- **Scope:** Gate suite for F14a/F14b/F14c (branch Test Gate matrix)
- **Dependencies:** none
- **Estimated Context:** 55K
- **Concurrency Group:** A
- **Audit references:** I8; R6 acceptance; meept-bench skill checker-trust rules

## Goal

Pin the new acceptance checkers BEFORE implementation: terminal-delivery
assertion, reply-quality judge rubrics (answer-leak lint), hardened suite
loading + real plan-artifact checker. Mutation-verify the terminal checker
(a missing terminal MUST fail it).

## Context

- meept-bench internal/checkers/checkers.go - case exact_file/file_contains/
  trusted_python/exit_zero/llm_judge; Run takes variadic RunOption.
- internal/suite/suite.go - Manifest/Task/Turn schema; Validate; Select.
- internal/results/results.go - Row/Transcript/TurnEvent; verdict semantics.
- scripts/judge-local.py - deterministic local judge; rubric lint refuses
  judges that saw the literal expected answer (extra.expected).
- suites/usability-probe.json - the 6-task suite (u-plan-mode-request has
  vacuous exit_zero(true) checker - replace).
- MEEPT_BENCH_TRANSCRIPT_DIR - current-run transcript dir env (checkers must
  read ONLY this, never bare results/** glob).
- Baseline: /tmp/usability-rig/bench-results-clean/results.jsonl.

## Verified Anchors

- internal/checkers/checkers.go:70 - checker type switch
- internal/suite/suite.go:154 - turn delay validation
- scripts/judge-local.py - judge contract (stdin/stdout)

## Interface Contracts (From Parent)

### What This Leaf Exposes
Gate tests for the three new checkers + suite validation.

### What This Leaf Consumes
- C1-C7 as assertion targets (terminal keys, status set, rejection_reason).

## Tasks

| Task ID | Objective | Files | Verify command | RED reason | PASS state | Given/When/Then |
|---------|-----------|-------|----------------|------------|------------|-----------------|
| T1 | Terminal-delivery checker pin | Create: internal/checkers/terminal_delivery_test.go | `go test ./internal/checkers/ -run TestTerminalDeliveryChecker -count=1` | checker absent | GREEN after 01 | Given transcript with submitted turn lacking non-parked terminal within deadline; Then checker FAILS; given parked-only -> FAILS; given completed/failed terminal -> PASSES; explicit actionable rejection (accepted:false) -> PASSES with note; MUTATION: remove terminal from a passing fixture -> must FAIL |
| T2 | Reply-quality rubric pins | Create: internal/checkers/reply_quality_test.go | `go test ./internal/checkers/ -run 'TestReplyQualityJudgeLint|TestNoAnswerLeak' -count=1` | rubric absent | GREEN after 01 | Given rubric with literal expected answer leaked; Then lint refuses (judge never called); given empty reply / raw envelope / "## review:" / job-ID prose; Then rubric scores fail; clean contextual reply passes |
| T3 | Suite + plan checker pins | Create: internal/suite/usability_suite_test.go; internal/checkers/plan_artifact_test.go | `go test ./internal/suite/ ./internal/checkers/ -run 'TestSuiteLoads|TestPlanArtifactChecker' -count=1` | vacuous checker; plan checker absent | GREEN after 01 | Given hardened usability-probe.json; Then loads, >=6 tasks, no vacuous exit_zero(true), u-plan-mode-request has real plan-artifact/judge checker; plan checker fails when no plan artifact/draft produced |

## Self-Verification Checklist

- [ ] Every test RED for documented reason (record output)
- [ ] Terminal checker mutation-verified (missing terminal -> FAIL)
- [ ] No implementation code; no existing tests weakened
- [ ] Checkers read MEEPT_BENCH_TRANSCRIPT_DIR only (no bare glob)

**DO NOT COMMIT.** Orchestrator handles git after review.

## Completion Report Format

1. Files touched. 2. RED output per task. 3. Deviations.

## Review Checklist (For Review Agent)

- [ ] Tests-only; RED reasons = missing checker
- [ ] Mutation discipline on T1 (the load-bearing acceptance pin)
- [ ] Judge lint never lets a judge see the expected answer
- [ ] No artifacts / corruption

Output: APPROVED or gaps with file+line.

## Notes

- This gate lives in the meept-bench repo worktree, NOT the meept worktree.
- Frozen prompts: never reword suite prompts to make checkers pass.
