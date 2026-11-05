# Verified Anchors - branch 02-goals-plans (verified at 40f6fa5a)

Grep-verified. Use these exact shapes; do not invent APIs.

- `internal/employee/goal.go:158` — `type Goal struct` (ID, EmployeeID, Title, Mandate, State GoalState, Source GoalSource, TriggerRef, Health...). The autonomy fields your tests pin (MaxRounds, RoundsStarted, BlockedReason) DO NOT exist yet — that is the RED.
- `internal/employee/goal.go:40-43` — `GoalActive GoalState = iota`, `GoalPaused` (GoalState is an iota enum with String() at :50 and ParseGoalState at :66 — unknown values map to GoalActive WITH an error). Your tests pin the new `GoalBlocked` state + ParseGoalState("blocked") round-trip.
- `internal/employee/goal_loop.go:272` — `type GoalLoop struct` (employeeID, constitution, goalStore *GoalStore, runner bot.BotExecutor, planner PlanCreator, auditor, reflector, maxConsecutiveFailures, pauseFn PauseFunc). The round driver (F3) extends this.
- `internal/employee/goal_loop.go:393/413/426` — `WithEscalationGate`, `WithAuditStore`, `WithGateBackend` functional-option pattern — mirror it for new seams.
- `internal/plan/plan.go:52` — `type Plan struct` (plan-mode surface for F4).
- NO goal_* builtin tools exist today (`internal/tools/builtin/goal_tools.go` absent) — the tool tests pin the new registration (F2).

New symbols your tests reference (contracts F1-F4 in .hermes/contracts/02-goals-plans.md — undefined today, that IS the RED): Goal.MaxRounds/RoundsStarted/BlockedReason, GoalBlocked state, Goal.Activate/Disarm/IsArmed, the round driver, goal_create/goal_update/goal_list builtin tools, plan-mode wiring.

RED-gate convention (repo precedent commit 537c92fd): if a test file's
undefined-symbol errors break `go vet`/`go build` of the NON-test package,
park it as `<name>_test.go.staged` and say so. If `go build ./internal/employee`
stays green and only `go test` fails on undefined symbols, keep it live.

e2e: suite dir `e2e/suites/goals-plans/` (NEW). Build tag `//go:build e2e`,
import `github.com/caimlas/meept/e2e/harness`. Study the EXISTING
`e2e/suites/goal-loop/` suite first (real queue/store/parker infra, no mocks)
and follow its shape. Register the new suite in `e2e/manifest.json`.

ANTI-VACUOUS BAR: assert values, error identity, state transitions. t.Log-only
or commented-out bodies = rejection. Round-driver tests must assert the round
counter increments per fired round and the goal reaches GoalBlocked with a
non-empty BlockedReason after N consecutive failures — not that a method exists.
