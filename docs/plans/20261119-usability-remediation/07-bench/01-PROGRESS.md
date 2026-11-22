# 07-bench/01-harness-hardening - Progress

Worktree: /Users/caimlas/git/meept-bench/.hermes/worktrees/usability-acceptance (HEAD 45c94c0)
Status: ALL TASKS DONE - gate tests GREEN, no commit (per leaf instruction).

## T1 - terminal_delivery checker

- DONE: internal/checkers/terminal_delivery.go + registered in Run switch
  (internal/checkers/checkers.go).
- C1 closed set enforced (completed|failed|timeout; parked never delivers),
  distinct-turn_id counting (duplicates collapse; no chain credit), deadline
  measured from transcript StartedAt (extra.deadline_seconds, default 600s),
  accepted:false+non-empty note passes (surfaced in detail), silent
  rejection fails, reads ONLY WithTranscriptDir / MEEPT_BENCH_TRANSCRIPT_DIR
  (never results/**), missing extra.task_id / wrong task_id fails closed.
- TestTerminalDeliveryChecker: 18/18 subtests GREEN, incl.
  mutation_remove_terminal_flips_to_fail.

## T2 - reply_quality rubrics + answer-leak lint

- DONE: 6 llm_judge reply-quality rubrics added to suites/usability-probe.json
  (one per task), grading reply SHAPE: empty replies (<0.3), raw JSON
  envelope dumps (<0.4), "## review:" scaffolding chatter (<0.4), job-ID
  prose (<0.5), context-free bare replies (<0.6); 0.9+ only for clean
  contextual replies. Rubrics carry no literal expected answers; the
  existing lintRubric answer-leak gate (judge never sees extra.expected)
  is exercised by TestReplyQualityJudgeLint + TestNoAnswerLeak: GREEN.
- Note: "ok" (expected literal of u-direct-explicit) substring-matches
  words like "bookkeeping"/"token", so the rubric text avoids those words
  entirely; lint-exempt tiny literals (<2 runes: "7") need no care.

## T3 - plan_artifact checker + suite hardening

- DONE: internal/checkers/plan_artifact.go + registered in Run switch.
  Evidence paths: (a) non-empty plan.md/PLAN.md or non-empty plans/*.md in
  the worktree; (b) plan.created/plan.draft/plan.sealed/plan.submitting
  events in the CURRENT-RUN transcript tool_trace; (c) judge-verified plan
  reply (rubric + judge >= min_score), consulted only when no artifact or
  draft evidence exists. Prose alone fails; empty files fail; current-run
  dir only; association fails closed. Mutation subtest GREEN.
- suites/usability-probe.json hardened: vacuous exit_zero(["true"]) on
  u-plan-mode-request replaced by plan_artifact (+ terminal_delivery +
  reply-quality llm_judge); every task now carries terminal_delivery (R6)
  and an llm_judge reply-quality rubric. Prompts + scheduled turn messages
  FROZEN byte-identical (pinned by usability_suite_test.go - verified).
- suite.Validate hardened: rejects vacuous exit_zero(true) forms at load;
  accepts terminal_delivery + plan_artifact (both require extra.task_id).

## Verification

- go test ./internal/checkers/ ./internal/suite/ -count=1 : ok / ok
  (44 passing top-level+sub tests; zero failures).
- go test ./... -count=1 : clean (no FAIL anywhere).
- go build ./... : OK. gofmt -l internal/checkers internal/suite : clean.
- Pre-existing suite-package fixtures that used placeholder exit_zero
  ["true"] checkers were switched to real commands (test -f ...) -- the
  hardened Validate now (correctly) rejects the vacuous form at load.
  Gate test files themselves untouched (verified: git diff shows no
  changes under the four gate test files).
