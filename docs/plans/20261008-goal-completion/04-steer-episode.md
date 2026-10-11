# 04 Steer-of-episode - Implementation Leaf

> **DISPATCH INSTRUCTION:** Any agent may implement this document end-to-end.

## Meta

- **Parent:** ../master.md
- **Scope:** User steering reaches a running goal-loop episode (chat steering already exists; this is its goal-loop counterpart).
- **Dependencies:** none.
- **Estimated context:** ~60K.
- **Contracts:** SHARED-CONVENTIONS.md §2 C5.

## Goal

The chat path has steering (internal/agent/queue.go steeringQueue, drained at loop.go:4295; TUI Ctrl+S). The goal loop has no equivalent: a running tier-3 episode cannot be re-routed. This leaf adds `GoalLoop.Steer` riding the existing park/resume machinery (internal/employee/park.go), so steering a quiet employee is a no-op and steering a running one re-enters Decide with Source="steer".

## Tasks

### Task 1: Steer on GoalLoop

- File: internal/employee/goal_loop.go.
- `func (l *GoalLoop) Steer(ctx context.Context, content string) error` per C5:
  - No active episode (no in-flight plan, no parked episode) → return nil.
  - Active episode → park-resume path: enqueue a resume TriggerEvent{Source: "steer", Payload: JSON {"content": content}} through the park store (park.go:430 shows the parked_resume source pattern).
- Assess prompt: when trigger.Source == "steer", buildAssessUserPrompt appends "User steering: <content>".
- TDD: TestGoalLoopSteerQuiet (nil return, no episode created); TestGoalLoopSteerActive (parked episode resumes with steer source; assess prompt contains the content).

### Task 2: Manager + RPC surface

- File: internal/employee/manager.go — `SteerEmployee(ctx, id, content string) error`: acquire the same per-employee invoke mutex as triggerViaGoalLoop (manager.go:1455), then loop.Steer.
- File: internal/employee/handler.go — `agents.steer` params `{"employee_id":..., "content":...}`; register in the method map (~line 111 block).
- File: internal/employee/api_handlers.go — `POST /api/v1/agents/{id}/steer`.
- TDD: manager test with a stub loop; handler round-trip test.

### Task 3: CLI

- File: cmd/meept/agents_cmd.go — `meept agents steer <id> <content...>` beside `trigger`; calls agents.steer; prints "steer queued" or "employee idle (no-op)".
- TDD: registration test only.

## Interface Contract (exposed to siblings)

- `GoalLoop.Steer`, `Manager.SteerEmployee`, `agents.steer` RPC + HTTP route — leaf 05 e2e drives steer into a tier-1 employee and asserts the reflect log records the steer source.

## Self-Verification Checklist

- [ ] `go build ./...`; `go vet ./...`; `go test -p 2 ./internal/employee/... ./cmd/meept/... -short` green
- [ ] Steer of a quiet employee is a nil no-op (test proves it)
- [ ] No I/O under mutex (mutexio rule) — SteerEmployee locks only around Steer dispatch
- [ ] docs/workflows/employees.md gains a short "steering an employee" note in the same change
- [ ] No commits; no read_file corruption
