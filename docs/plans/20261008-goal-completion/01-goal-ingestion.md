# 01 Goal ingestion surfaces - Implementation Leaf

> **DISPATCH INSTRUCTION:** Any agent may implement this document end-to-end.

## Meta

- **Parent:** ../master.md
- **Scope:** GoalSpec type + goal creation via definition file, RPC, CLI, HTTP.
- **Dependencies:** none.
- **Estimated context:** ~70K.
- **Contracts:** SHARED-CONVENTIONS.md §2 C1 (GoalSpec), C4 (surfaces).

## Goal

`GoalStore.Create` (internal/employee/goal.go:604) exists but has no production caller. This leaf builds the ingestion seam so users can declare goals four ways. After this leaf, the documented `goals:` block (docs/workflows/employees.md:120) parses for real.

## Tasks

### Task 1: GoalSpec type

- File: internal/employee/goal_spec.go (new).
- Define `GoalSpec` and `GoalGateSpec` exactly per SHARED-CONVENTIONS C1.
- Add `func (s GoalSpec) Validate() error`: EmployeeID, Title, Mandate required; Source must be "" / "user" / "trigger"; Gate nil-safe.
- Add `func (s GoalSpec) toGoal(now time.Time) *Goal` mapping Source→employee.GoalSource, Gate→*GateConfig (Command empty → nil gate), State=GoalActive, Source default SourceUser, CreatedAt=now.
- TDD: table-driven TestGoalSpecValidate in goal_spec_test.go.

### Task 2: Manager.CreateGoal

- File: internal/employee/manager.go.
- Add `func (m *Manager) CreateGoal(ctx context.Context, spec GoalSpec) (*Goal, error)`:
  - nil goalStore → ErrNotImplemented (existing pattern, manager.go:1787).
  - Validate spec; run `employee.ValidateGoalSource(source, tier)` — fetch the employee's constitution tier via the existing list path; if constitution missing, reject tier-sensitive sources with a clear error.
  - Call goalStore.Create; return the stored Goal.
- TDD: TestManagerCreateGoal (happy path, invalid source, nil store) in manager goal test file.

### Task 3: Hire parses goals from definition

- Files: internal/employee/manager.go (HireRequest + Hire), internal/employee/handler.go (createRequest).
- Add `Goals []GoalSpec` to HireRequest and the JSON request struct (`json:"goals"`).
- In Hire after the employee row is created: for each spec, force spec.EmployeeID = req.ID, then CreateGoal; collect failures into the returned error (errors.Join).
- TDD: TestHireCreatesGoals — hire with 2 goals, ListGoals returns both.

### Task 4: RPC agents.goals.create

- File: internal/employee/handler.go.
- New `handleGoalsCreate` (mirror handleGoalsList at line 438); params = GoalSpec JSON; response `{"goal": goal}`.
- Register `"agents.goals.create": h.handleGoalsCreate` in the method map (~line 117).
- TDD: handler round-trip test beside existing goals.list handler tests.

### Task 5: HTTP route

- File: internal/employee/api_handlers.go.
- `POST /api/v1/agents/{id}/goals` → handleGoalsCreate → dispatch "agents.goals.create" with path id overriding body employee_id (C4).
- Register in RegisterRoutes (~line 59). Error sanitization via existing sanitizeAgentErr.
- TDD: route test mirroring handleGoalsList tests (api_gate_route_test.go style).

### Task 6: CLI meept agents goal-create

- File: cmd/meept/agents_cmd.go.
- `goal-create <employee-id> --title T --mandate M [--gate-command CMD] [--source user|trigger]` beside the `goals` command (line 944); calls "agents.goals.create"; prints goal id + health.
- TDD: command registration test only (arg parsing is cobra's); keep the change small.

## Interface Contract (exposed to siblings)

- `employee.GoalSpec`, `employee.GoalGateSpec`, `Manager.CreateGoal` — leaf 05's e2e drives all three surfaces through these.
- No changes to Goal struct or store schema in this leaf.

## Self-Verification Checklist

- [ ] `go build ./...` clean; `go vet ./...` clean
- [ ] `go test -p 2 ./internal/employee/... ./cmd/meept/... -short` green
- [ ] All four surfaces registered (method map, RegisterRoutes, cobra tree, HireRequest field)
- [ ] doc update in same change: docs/workflows/employees.md gains `meept agents goal-create` in the Goals CLI list (~line 368) and marks the `goals:` definition block as live
- [ ] No commits (orchestrator commits)
- [ ] No read_file-output-into-write_file corruption
