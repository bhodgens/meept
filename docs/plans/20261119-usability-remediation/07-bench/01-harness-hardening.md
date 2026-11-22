# Acceptance Harness Hardening - Implementation Leaf

> **For the implementing agent:** Implement ALL tasks using TDD. Gate tests
> ALREADY EXIST (00T T1/T2/T3) - make them pass; do NOT delete/skip/weaken
> them. Do NOT commit. Do NOT git add. Explore with search_files/terminal
> cat; write once, never read back. Worktree:
> /Users/caimlas/git/meept-bench/.hermes/worktrees/usability-acceptance.

## Meta

- **Parent:** orchestrator.md
- **Scope:** New acceptance checkers + hardened usability suite
- **Dependencies:** 00T-gate.md
- **Estimated Context:** 80K
- **Concurrency Group:** A
- **Audit references:** I8; R6; meept-bench skill (checker trust, judge hardening)

## Goal

Add a terminal-delivery checker (every submitted turn ends in a non-parked
terminal or explicit actionable rejection), reply-quality llm_judge rubrics
(clutter/scaffolding/empty-reply), and a real plan-artifact checker; replace
the vacuous exit_zero(true) in u-plan-mode-request; freeze prompts.

## Context

- internal/checkers/checkers.go:70 - type switch; Run(ctx, check, opts...)
  variadic RunOption (WithTranscriptDir precedent).
- internal/results/results.go - Transcript{Prompt,FinalReply,ToolTrace,Turns,
  ClassificationMethod}; Row{Verdict,Checks,ErrorDetail}.
- internal/suite/suite.go - Manifest/Task/Turn; Validate; Select.
- scripts/judge-local.py - judge contract; extra.expected lint; double-judge.
- suites/usability-probe.json - 6 tasks; u-plan-mode-request vacuous checker.
- C1 terminal keys/status set; C2 rejection_reason; accepted:false ack note.

## Verified Anchors

- internal/checkers/checkers.go:70 - checker switch
- internal/results/results.go - Transcript struct (re-grep)
- suites/usability-probe.json - u-plan-mode-request checkers

## Interface Contracts (From Parent)

### What This Leaf Exposes
- terminal_delivery checker type; reply_quality llm_judge rubric set;
  plan_artifact checker; hardened usability-probe.json.

### What This Leaf Consumes
- C1-C7 (assertion targets).

## Tasks

| Task ID | Objective | Files | Verify command | RED reason | PASS state | Given/When/Then |
|---------|-----------|-------|----------------|------------|------------|-----------------|
| T1 | terminal_delivery checker | Create: internal/checkers/terminal_delivery.go; register in switch | `go test ./internal/checkers/ -run TestTerminalDeliveryChecker -count=1` | gate T1 RED | GREEN | Given transcript turns; Then every submitted turn has non-parked terminal within deadline OR accepted:false rejection; parked-only/missing -> fail with turn_id detail; reads MEEPT_BENCH_TRANSCRIPT_DIR only |
| T2 | reply_quality rubrics | Create: internal/checkers/reply_quality.go (rubric templates + lint); suites rubric extras | `go test ./internal/checkers/ -run 'TestReplyQualityJudgeLint|TestNoAnswerLeak' -count=1` | gate T2 RED | GREEN | Given reply; Then rubric flags empty/raw-envelope/review-chatter/job-ID prose; lint refuses leaked expected answers; double-judge disagreement flagged |
| T3 | plan_artifact checker + suite hardening | Create: internal/checkers/plan_artifact.go; Modify: suites/usability-probe.json (real checkers, terminal_delivery+reply_quality on all tasks, frozen prompts) | `go test ./internal/suite/ ./internal/checkers/ -run 'TestSuiteLoads|TestPlanArtifactChecker' -count=1` | gate T3 RED | GREEN | Given plan task; Then checker requires draft/plan artifact or judge-verified plan reply; suite loads >=6 tasks, no vacuous checker; validate-then-run-zero passes |

## Self-Verification Checklist

- [ ] Gate T1/T2/T3 GREEN; gate diff empty
- [ ] Terminal checker mutation-verified; checkers read current-run dir only
- [ ] Judge rubrics never leak expected answers; prompts frozen verbatim
- [ ] No scope creep / artifacts / corruption; gofmt clean

**DO NOT COMMIT.** Orchestrator commits after review.

## Completion Report Format

1. Files touched. 2. Verify output. 3. Mutation evidence. 4. Deviations.

## Review Checklist (For Review Agent)

- [ ] Trusted evaluator code never sourced from agent worktree
- [ ] terminal_delivery never awards assumed chain credit (outcome-level)
- [ ] Rubric scoring rounded; disagreement >0.20 flagged
- [ ] Suite prompts byte-identical to frozen fixtures
- [ ] Gate untouched; conventions; no artifacts

Output: APPROVED or gaps with file+line.

## Notes

- This leaf is meept-bench-side; the daemon contracts it asserts land via
  branches 01-06. Run 02 (integration) only after those are REVIEWED.
