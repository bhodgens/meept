# Plan Critic Gate Tests - Implementation Leaf

> For the implementing agent: You are the implementer for this leaf.
> Write tests ONLY. Do NOT write implementation. Do NOT commit. Do NOT git add.
> Park every test file with a `.staged` extension so the Go toolchain and the
> pre-commit e2e hook do not compile them. Report files touched and the RED
> reason each test would hit on today's code.

## Meta

- Parent: master.md
- Scope: Author the hermetic gate tests for the fresh-context critic tree, parked so they do not fail the meept hook gauntlet.
- Dependencies: none
- Estimated Context: 40K
- Concurrency Group: Phase 0
- Audit references: R1, R2, R3, R4

## Goal

Create one e2e suite whose tests pin contracts C3 through C9. The files compile only after their owning impl leaf re-activates them. Until then they sit on disk as `.staged` and are invisible to `go test`.

## Context

Meept pre-commit runs affected e2e suites on staged internal/ changes. A committed RED suite poisons every later leaf. The parking sequence is in meept-development references/red-gate-test-parking.md: write the tests, rename to `.staged` before any commit, each impl leaf moves only its own file back.

New feature tests go in the e2e tier. Do not add a new `*_test.go` under internal/.

Suite path: `e2e/suites/plan-critic/`. Build tag `//go:build e2e`. Package name `plancritic`.

## Verified Anchors

- internal/agent/plan_critique_loop.go:600 - runCriticOnce sends the critic prompt to the planner loop today
- internal/agent/plan_critique_loop.go:363 - one clean round exits the loop
- internal/plan/compiler.go:125 - consume errors exist; no decision-lineage check yet
- internal/agent/strategic.go:495 - parseInterviewQuestions expects a questions string array
- internal/agent/verification_hook.go:436 - SpawnVerifier is the implementation verifier; tests must assert the plan critic does not call it
- e2e/suites/config-models/config_models_test.go:1 - `//go:build e2e` pattern to copy

## Interface Contracts (From Parent)

### What This Leaf Exposes

Parked test files, one per feature group:

- e2e/suites/plan-critic/fresh_critic_test.go.staged (F1, F2, F3)
- e2e/suites/plan-critic/hostile_pass_test.go.staged (F4, F5)
- e2e/suites/plan-critic/contest_test.go.staged (F6)
- e2e/suites/plan-critic/zero_decision_test.go.staged (F7)
- e2e/suites/plan-critic/interview_gates_test.go.staged (F8)
- e2e/suites/plan-critic/wiring_test.go.staged (F9)

Do not add e2e/manifest.json rows in this leaf. Leaf 07 adds the suite and path_map when the tests are live. A path_map entry before the suite compiles makes pre-commit-e2e select a broken suite.

### What This Leaf Consumes

- C3, C4, C5, C6, C7, C8, C9 from master.md. Tests call those names. They do not invent new ones.

## Tasks

| Task ID | Objective | Files | Verify command | RED reason | PASS state | Given/When/Then |
|---------|-----------|-------|----------------|------------|------------|-----------------|
| T1 | Write F1-F3 tests and park them | Create: e2e/suites/plan-critic/fresh_critic_test.go then rename to .staged | `test ! -f e2e/suites/plan-critic/fresh_critic_test.go && test -f e2e/suites/plan-critic/fresh_critic_test.go.staged` | On today's tree, SetFreshCriticEnabled is undefined and a child loop is not spawned | File parked; assertions match C3 and C4 | Given the parked test; When an impl leaf restores it before the child exists; Then `go test` fails on a missing symbol or a failed isolation assertion, not on a syntax error |
| T2 | Write F4-F5 tests and park them | hostile_pass_test.go.staged | same existence check | Same-model hostile pass would run on the planner today if someone reused the chatter | Assertions require skip metric and no SpawnVerifier call | Given hostile chatter nil or same model ref; When the pass is evaluated; Then it does not run and does not call SpawnVerifier |
| T3 | Write F6, F7, F8, F9 and park them | contest_test.go.staged, zero_decision_test.go.staged, interview_gates_test.go.staged, wiring_test.go.staged | existence check for each | CompileSealed accepts a mutating phase with no decision today; interview parser accepts only string questions; flags do not exist | Each file parked with one assertion per contract | Given today's compiler and parser; When the restored test runs before its leaf; Then the assertion fails for the missing behavior |

Test names (exact):

- TestFreshCriticFlagOffDoesNotSpawnChild
- TestFreshCriticChildHasNoTranscript
- TestEmptyParaphraseIsBlocking
- TestHostilePassSkipsSameModel
- TestHostilePassDoesNotCallSpawnVerifier
- TestContestWithoutEvidenceNotApplied
- TestMutatingPhaseWithoutDecisionFailsCompile
- TestInterviewFlagOffKeepsStringQuestions
- TestCriticFlagsDefaultOff

Each file starts with `//go:build e2e`. Import `github.com/caimlas/meept/internal/agent` and `github.com/caimlas/meept/internal/plan` as needed. Do not import verification_hook test helpers. Do not read or hash verifier.go as a golden file; assert call absence through a counter seam the impl leaf must export (`PlanCriticSpawnVerifierCalls() int`, owner 03, default 0).

F7 can call `plan.CompileSealed` today. Write the assertion so it fails now (`err == nil` is the current result) and passes when leaf 05 lands. That file still parks, because a live failing test would fail the hook if the suite were mapped.

## Self-Verification Checklist

- [ ] Nine test functions exist across the six parked files
- [ ] No `.go` test file remains in e2e/suites/plan-critic/ (only `.staged`)
- [ ] No implementation files created
- [ ] No e2e/manifest.json edit
- [ ] No forbidden file (C10) touched
- [ ] No deviations, or deviations listed in the report

**DO NOT COMMIT.** The orchestrator handles all git operations after review.

## Completion Report Format

1. Files touched: every created path
2. Verify command + output: the existence checks
3. Deviations: none, or a list with rationale

## Review Checklist (For Review Agent)

- [ ] Test names match the Test Gate matrix in master.md
- [ ] Assertions cite C3-C9 and do not weaken "skip on same model"
- [ ] No live `*_test.go` in the suite directory
- [ ] No manifest edit
- [ ] No C10 file in the diff

Output: APPROVED or specific gaps with file + line references.

## Notes

Do not edit config/agents/verifier/AGENT.md, internal/agent/prompts/verifier.go, internal/agent/verification_hook.go, or any AGENT.md under config/agents/.
