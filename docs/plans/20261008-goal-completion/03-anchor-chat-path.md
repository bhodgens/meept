# 03 Anchor the chat path to goals - Implementation Leaf

> **DISPATCH INSTRUCTION:** Any agent may implement this document end-to-end.

## Meta

- **Parent:** ../master.md
- **Scope:** Chat-created Plans resolve to a Goal by default (explicit > sole active > implicit).
- **Dependencies:** 02-goal-linkage (columns + EnsureImplicitGoal must exist).
- **Estimated context:** ~65K.
- **Contracts:** SHARED-CONVENTIONS.md §2 C3, C6.

## Goal

Today chat-path Plans (orchestrator/quickplan) create goals-less work. Per C6, new Plans always carry a GoalID: session-explicit goal first, else the employee's single active non-implicit goal, else a SourceImplicit goal auto-created from the plan title. This makes "every piece of work has a goal" the default, which is how the user expects the system designed.

## Tasks

### Task 1: Session goal metadata

- File: internal/session/store_sqlite.go + internal/session/session.go.
- Add optional `goal_id` to session metadata (the existing detection_context / metadata JSON — extend, do not add a column, unless a column is already the established pattern; follow whichever the file already does for similar keys).
- Setter/getter helpers with nil guards.
- TDD: round-trip test.

### Task 2: Goal resolution at plan creation

- File: internal/agent — the orchestrator/quickplan path that calls plan creation (locate via search: `CreatePlan(` call sites in internal/agent; the plan.Manager wrapper). Add `resolveGoalID(ctx, sessionID, planTitle) string` implementing the C6 rule:
  1. session metadata goal_id (Task 1) — if set and the goal is active, use it;
  2. else query EmployeeGoalStore.ListActive for the session's employee; exactly one non-implicit active goal → use it; zero or multiple → skip to 3;
  3. else GoalStore.EnsureImplicitGoal(employeeID, planTitle).
- Employee identity resolution: use the session's bound employee if the session model has one; otherwise the daemon's default employee id from components (components.go:4603 wires the goal store — find the adjacent employee id source). If no employee exists at all, leave GoalID empty (chat without employees must keep working — this is a hard requirement).
- Set Plan.GoalID before the CreatePlan call's store write.
- TDD: unit tests for the three-branch rule with a stub goal store; test that no-employee sessions produce empty GoalID.

### Task 3: RPC to set a session goal

- File: internal/employee/handler.go — `agents.goals.bind_session` params `{"session_id":..., "goal_id":...}`: validates the goal is active and writes session metadata via Task 1's setter. Register in the method map (line ~117).
- TUI/GUI exposure is deferred to a follow-up; the RPC is the seam.
- TDD: handler test — bind, then resolveGoalID picks it up (integration via Task 2's unit test).

## Interface Contract (exposed to siblings)

- `resolveGoalID` rule C6 (explicit > sole active > implicit) — leaf 05 e2e asserts an implicit goal row appears after a chat-initiated plan.
- agents.goals.bind_session RPC.

## Self-Verification Checklist

- [ ] `go build ./...`; `go vet ./...`; `go test -p 2 ./internal/agent/... ./internal/session/... ./internal/employee/... -short` green
- [ ] Read internal/agent/AGENTS.md before touching internal/agent (session_id vs conversation_id; async turns)
- [ ] Chat with no employee configured produces zero goal rows (regression test)
- [ ] No commits; no read_file corruption
