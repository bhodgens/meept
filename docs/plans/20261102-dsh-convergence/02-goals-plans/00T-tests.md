# 00T-tests - Test Gate Leaf

> **For the test implementer:** Write tests ONLY for the 02-goals-plans branch.
> Do NOT write or modify implementation files. Tests WILL FAIL (RED) against
> the current tree before the implementation leaves land — that is the
> expected end state. Run them once to confirm they fail for the RIGHT reason
> (missing implementation, not a syntax error you introduced). Do NOT commit —
> the orchestrator handles all git operations after validating the gate.
> Do NOT use read_file on existing source files — explore with search_files
> or terminal cat. After writing a file, do NOT read it back.
> Corruption rules: write once, then stop; never feed read_file line numbers
> into write_file.

## Meta

- **Parent:** ./orchestrator.md
- **Scope:** Full unit + e2e test suite for every 02-goals-plans feature
  (F4, F4a, F4b, F5) BEFORE any implementation leaf dispatches.
- **Dependencies:** none (compile against the planned C4a-d contracts)
- **Estimated Context:** 30K
- **Concurrency Group:** gate (Phase 0)
- **Audit references:** /tmp/dsh-audit/goal-workflow-plan.md Feature 1, Feature 2

## Goal

Produce the executable form of the branch's coverage matrix. When GREEN
(after the implementation leaves land), the tests prove: (F4) the Goal model
carries rounds/activation/blocked state; (F4a) the three model goal tools work
with the authority gate; (F4b) the round driver continues goals automatically
with zero human turns and auto-blocks at the cap; (F5) plan-mode toggles and
`exit_plan_mode` drives the ApprovePlan gate. In the RED phase these tests
fail because the implementation does not exist yet — that is the gate.

## Context

- **Repo e2e policy (binding):** NEW feature tests go in the hermetic e2e tier
  (`e2e/suites/<name>/` with the `e2e` build tag; manifest path->suite entry in
  `e2e/manifest.json` same commit). Unit tests are ONLY permitted to EXTEND
  existing in-package test files (`internal/employee/goal_test.go`,
  `internal/employee/goal_loop_test.go`) for the additive data-structure and
  state-machine fields — not new standalone feature unit files. The e2e suite
  carries the behavioral coverage.
- **Existing suites to model on:** `e2e/suites/goal-loop/`, `e2e/suites/goal-completion/`
  (goal lifecycle), `e2e/suites/scheduler/` (deferred turns), `e2e/suites/plan-critic/`
  (plan gate). Mirror their harness usage: `e2e/harness` scratch MEEPT_HOME via
  `harness.WithPreBootHook`, assert through live RPC/WS surface.
- **Files the implementation will add** (the tests compile against these — the
  gate leaf defines the expected shapes, the impl leaves make them real):
  - `internal/employee/goal.go` — GoalState gains `GoalBlocked`; Goal gains
    `MaxRounds`, `RoundsStarted`, `BlockedReason`, live arm/disarm;
    `Activate()`, `Disarm()`, `IsArmed()`, `IsBlockedAfterNConsecutive()`.
  - `internal/employee/manager.go` — `SetGoalRounds(...)` helper (or equivalent
    per Contract C4a) letting tests/per-goal wiring set MaxRounds.
  - `internal/tools/builtin/goal_tools.go` — `goal_get`, `goal_create`,
    `goal_update` model tools.
  - `internal/employee/goal_round_driver.go` — autonomous round driver.
  - `internal/agent` session plan_mode toggle + `exit_plan_mode` tool (surface
    per Contract C4d; may live in internal/employee or internal/services).

## Verified Anchors

- internal/employee/goal.go:158-222 — `Goal` struct (fields to extend)
- internal/employee/goal.go:37-61 — `GoalState` enum + `String()`
- internal/employee/goal.go:50-77 — `ParseGoalState` (must accept "blocked")
- internal/employee/goal.go:67-75 — `ParseGoalState` default branch
- internal/employee/goal_loop.go:1376-1404 — `Decide()` dispatch by tier
- internal/employee/manager.go:1482-1534 — `Trigger()`; :1540-1609 — `triggerViaGoalLoop`
- internal/employee/manager.go:1965-2042 — CreateGoal / UpdateGoal / GetGoal
- internal/plan/manager.go:381-430 — `ApprovePlan`; :431-470 — `RejectPlan`; :509-560 — `ConfirmPlan`
- internal/tools/interface.go:24-54 — `Tool` interface
- e2e/manifest.json — path->suite registry to update

## Tasks

| Task ID | Objective | Files | Verify command | RED reason | PASS state | Given/When/Then |
|---------|-----------|-------|----------------|------------|------------|-----------------|
| G1 | Extension unit tests for the goal model autonomy fields (F4) | Create: `internal/employee/goal_autonomy_ext_test.go`; Modify: (none) | `go test -p 2 ./internal/employee/ -run 'Goal.*(Rounds\|Arm\|Blocked)' -v` | FAIL: `Goal` has no `MaxRounds`/`RoundsStarted`/`BlockedReason` fields; `GoalBlocked` undefined; methods missing | PASS: fields exist, phase transitions + arm/disarm + threshold tested | Given a `Goal`; When I add autonomy fields and transitions; Then the tests read real types and pass |
| G2 | Hermetic e2e: model goal tools + authority gate (F4a) | Create: `e2e/suites/goals-plans/suite_test.go`, `e2e/suites/goals-plans/goal_tools_test.go`; Modify: `e2e/manifest.json` (path map for `internal/tools/builtin/` + `internal/employee/` -> goals-plans) | `go test -p 2 -tags e2e ./e2e/suites/goals-plans/ -v` | FAIL: `goal_create`/`goal_get`/`goal_update` tools not registered (tool-not-found); daemon does not expose them | PASS: model calls the three tools over the RPC surface and sees objective/phase/rounds/activation; create requires direct-human; blocked rejected before threshold | Given a live daemon + model turn; When the model invokes goal tools; Then the tool results match Contract C4b and the authority gate enforces it |
| G3 | Hermetic e2e: autonomous continuation + auto-block (F4b) | Create: `e2e/suites/goals-plans/round_driver_test.go` | same goals-plans suite command | FAIL: round N+1 never queued on a settled armed goal (driver absent) | PASS: a goal left active advances to a second round with zero human turns, then blocks with code `round-limit` at `MaxRounds` | Given an armed active goal with MaxRounds=2; When quiescence+idle is reached; Then round 2 runs unattended and the goal then blocks (phase `blocked`, blockedReason.code=`round-limit`) |
| G4 | Hermetic e2e: plan-mode present-for-review (F5) | Create: `e2e/suites/goals-plans/plan_mode_test.go` | same goals-plans suite command | FAIL: no `/plan` command, no `exit_plan_mode` tool | PASS: `/plan` enters mode; `exit_plan_mode` with a markdown `#` plan presents Approve/Keep-planning; Approve lands the plan via the existing plan gate (plan moves to an approved state) and plan_mode reads off | Given plan mode active + a model plan; When the user approves; Then the durable plan is approved and plan mode exits |
| G5 | Manifest + coverage completeness | Modify: `e2e/manifest.json` (entry for the new suite + path_map for touched packages) | `python3 scripts/e2e-affected.sh` (or the pre-commit-e2e check) recognizes the new package dirs | FAIL: pre-commit-e2e blocks commits adding a NEW package dir without a suite+manifest entry | PASS: manifest lists the suite and maps `internal/employee/`, `internal/tools/builtin/`, plan/session paths to it | Given the new suite + touched packages; When the manifest is regenerated; Then coverage is non-empty |

## Self-Verification Checklist

- [ ] All test tasks above present; tests only, no implementation written
- [ ] Each test fails RED for a missing-implementation reason (compile against
      not-yet-existing symbols / missing tool registration / absent behavior),
      NOT a self-inflicted syntax error
- [ ] e2e suite + manifest entry complete; new package dirs mapped
- [ ] All files at exact specified paths
- [ ] No deviations (or documented in the report)
- [ ] No scope creep — tests cover F4/F4a/F4b/F5 only

**DO NOT COMMIT.** The orchestrator handles all git operations after review.

## Completion Report Format

1. **Files touched:** every created/modified path
2. **Verify command + output:** the four verify commands; for each, last 20
   lines showing RED-for-the-right-reason (missing symbol/tool, not syntax)
3. **Deviations:** none / list with rationale

## Review Checklist (For Review Agent)

- [ ] Every task row implemented; tests compile/parse and fail RED for
      missing-implementation reasons only (no syntax errors)
- [ ] Coverage matrix rows F4/F4a/F4b/F5 each have ≥1 test; e2e covers each
      user-visible/contract-crossing feature (unit only where e2e is `n/a`)
- [ ] No implementation written; no gate-test editing of implementation
- [ ] Manifest + path_map updated same commit; pre-commit-e2e would pass
- [ ] Conventions followed; no tautological assertions; e2e asserts through
      the real contract surface, not mocks of it

Output: APPROVED or specific gaps with file + line references.

## Notes

- The e2e goal-tools and round-driver scenarios share one suite target; keep
  them in separate `_test.go` files so the matrix rows map cleanly.
- The round-driver e2e (G3) needs the parent 01-delegation child-wait to be a
  nil-safe seam in the impl; assert the driver works with that seam absent too.
- Threshold constants come from the audit (maxGoalRounds=256 default,
  blockedAfterConsecutiveRounds=3) — re-read /tmp/deepseek-harness before
  pinning exact values in test expectations; prefer asserting the CONFIGURABLE
  behavior (a small MaxRounds=2) over the 256 default so the e2e is fast and
  deterministic.
