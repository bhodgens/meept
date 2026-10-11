# 02 Goal linkage (Plan + Task) - Implementation Leaf

> **DISPATCH INSTRUCTION:** Any agent may implement this document end-to-end.

## Meta

- **Parent:** ../master.md
- **Scope:** SourceImplicit goal source; goal_id columns on Plans and Tasks; migration + scan wiring.
- **Dependencies:** none.
- **Estimated context:** ~80K.
- **Contracts:** SHARED-CONVENTIONS.md §2 C2 (SourceImplicit), C3 (columns), C6 (anchoring rule — storage half only).

## Goal

Every Plan and Task gains an optional goal_id so fulfillment is traceable. The GoalLoop already writes Plans via its PlanCreator (internal/employee/goal_loop.go:195); this leaf makes the linkage persistable everywhere and defines the implicit-goal source that anchors goal-less chat work.

## Tasks

### Task 1: SourceImplicit goal source

- File: internal/employee/goal.go — add `SourceImplicit GoalSource = "implicit"` per SHARED-CONVENTIONS C2 (exact comment semantics).
- File: internal/employee/authority.go — ValidateGoalSource (line ~265): SourceImplicit valid at every tier.
- File: internal/employee/goal_loop.go — decideTier3/decideTier2 treat SourceImplicit goals like trigger goals (no signoff requirement).
- TDD: extend TestValidateGoalSource table with implicit rows; test implicit goals pass CanExecutePlan with ApproverID "system".

### Task 2: Plan.GoalID column

- Files: internal/plan/plan.go (field per C3), internal/plan/store_sqlite.go.
- Migration: `ALTER TABLE plans ADD COLUMN goal_id TEXT` guarded by the duplicate-column-name idiom (see employee/goal.go:539).
- Add goal_id to INSERT, UPDATE, and all SELECT/scan paths. Empty string = legacy-legal (backfill rule C3).
- TDD: store round-trip test — Create with GoalID, Get returns it; legacy row (NULL) scans to "".

### Task 3: Task.GoalID column

- Files: internal/task/task.go (field per C3), internal/task/store.go.
- Same migration idiom and scan wiring as Task 2 (this store's own file; do not touch the plan store).
- TDD: round-trip test mirroring Task 2.

### Task 4: PlanCreator sets GoalID

- File: internal/employee/goal_loop.go — the CreatePlan adapter (~line 700, planner.CreatePlan call site): set the created Plan's GoalID from the loop's active goal (lookupActiveGoal, line 1181) before returning the PlanRef.
- TDD: TestGoalLoopPlanCarriesGoalID — tier-3 decide with a seeded goal produces a plan whose GoalID matches.

### Task 5: Implicit goal helper

- File: internal/employee/goal.go — add `func (s *GoalStore) EnsureImplicitGoal(ctx context.Context, employeeID, title string) (*Goal, error)`: return the employee's active SourceImplicit goal if one exists (ListActive + filter), else Create one with Source=SourceImplicit, State=GoalActive, Title=title, Mandate=title.
- TDD: idempotency test — two calls return the same goal ID.

## Interface Contract (exposed to siblings)

- `Plan.GoalID`, `Task.GoalID` columns exist and round-trip (leaf 03 depends).
- `GoalStore.EnsureImplicitGoal`, `SourceImplicit` (leaf 03 depends).
- Leaf 05 e2e asserts plan rows carry goal_id after a tier-3 run.

## Self-Verification Checklist

- [ ] `go build ./...` clean; `go vet ./...` clean
- [ ] `go test -p 2 ./internal/employee/... ./internal/plan/... ./internal/task/... -short` green
- [ ] All three migrations use the duplicate-column guard
- [ ] Empty goal_id stays valid on legacy rows (scan test passes)
- [ ] No commits; no read_file corruption
