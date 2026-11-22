# 07-bench/01-harness-hardening - Completion Report

Worktree: /Users/caimlas/git/meept-bench/.hermes/worktrees/usability-acceptance
Base: 45c94c0 (gate tests committed RED). NOT COMMITTED per leaf instruction.

## 1. Files touched

Created (bench worktree):
- internal/checkers/terminal_delivery.go - terminal_delivery checker
- internal/checkers/plan_artifact.go - plan_artifact checker

Modified (bench worktree):
- internal/checkers/checkers.go - registered terminal_delivery + plan_artifact
  in the Run type switch (+10 lines)
- internal/suite/suite.go - hardened Validate: rejects vacuous exit_zero(true)
  forms at load; accepts terminal_delivery/plan_artifact requiring
  extra.task_id (+42)
- suites/usability-probe.json - vacuous exit_zero(["true"]) on
  u-plan-mode-request replaced by plan_artifact; every task carries
  terminal_delivery (extra.task_id + deadline_seconds=timeout) and a
  reply-quality llm_judge rubric; prompts + turn messages byte-identical
  to the frozen fixtures
- internal/suite/{suite_test,tools_config_test,turns_test}.go - placeholder
  exit_zero ["true"] fixtures switched to real commands (test -f ...); the
  hardened Validate now rejects the vacuous form, as the gate demands

Docs (meept repo, usability-remediation worktree):
- docs/plans/20261119-usability-remediation/07-bench/01-PROGRESS.md

Gate test files (terminal_delivery_test.go, reply_quality_test.go,
plan_artifact_test.go, usability_suite_test.go): UNTOUCHED.

## 2. Verify output

- go test ./internal/checkers/ -run TestTerminalDeliveryChecker -count=1 -v:
  18/18 subtests PASS (incl. mutation_remove_terminal_flips_to_fail)
- go test ./internal/checkers/ -run 'TestReplyQualityJudgeLint|TestNoAnswerLeak':
  ok (rubric coverage, no-leak sweep, leaked-rubric judge refusal with
  calls==0, double-judge disagreement flagging)
- go test ./internal/suite/ ./internal/checkers/ -run
  'TestSuiteLoads|TestPlanArtifactChecker': ok / ok
- go test ./internal/checkers/ ./internal/suite/ -count=1 -v: 44 PASS, 0 FAIL
- go test ./... -count=1: clean, no FAIL in any package
- go build ./...: OK; gofmt clean (gofmt -l internal/checkers internal/suite)

## 3. Mutation evidence

- terminal_delivery: fixture (progress + turn.terminal completed) passes;
  removing ONLY the turn.terminal event flips the same fixture to FAIL
  (gate subtest mutation_remove_terminal_flips_to_fail, GREEN).
- plan_artifact: worktree plan.md fixture passes; deleting the file flips
  to FAIL (gate subtest mutation_remove_artifact_flips_to_fail, GREEN).
- Additional falsification pins verified GREEN: parked-only fails;
  failed/timeout terminals pass (delivery != work success); late terminal
  (400s > 300s deadline; 700s > default 600s) fails; non-C1 status
  (done/ok/"") fails; empty turn_id fails; one terminal cannot cover two
  submitted turns (distinct-turn_id counting); duplicate turn_id counts
  once; silent accepted:false fails, noted rejection passes and is
  surfaced; results/** decoy ignored (reads current-run dir only);
  empty plan.md / unrelated notes.txt / plan-shaped prose fail.

## 4. Deviations / notes

- Rubric wording: the expected literal "ok" substring-matches common words
  ("bookkeeping", "token"), tripping the answer-leak lint, so the rubrics
  say "task-bookkeeping prose"/"a bare word" instead of those words. The
  gate-required needles (empty, envelope, "## review:", job, context) are
  all still present and the lint sweep is green.
- Checkers fail closed on missing transcript dir / missing task
  association; deadline measured from transcript StartedAt.
- plan_artifact consults the judge only when no artifact/draft evidence
  exists (no pointless judge burn; no leaked-answer path - lintRubric
  still applies inside llmJudge).
- No synthetic fixtures were committed: /tmp/usability-rig/bench-results-clean
  exists and was used only as shape reference; the gate tests build their
  own in-memory fixtures.

Unblocks 02-integration-run (acceptance battery over the real daemon).
