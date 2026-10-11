# SHARED-CONVENTIONS — goal-completion tree

Every leaf and branch in this tree references this file instead of restating contracts.

## 1. Scope statement

Fully close the "express goals declaratively, workers fulfill them" capability:

- G-A: production goal-creation paths (definition file, RPC, CLI, HTTP).
- G-B: every Plan and Task traces to a Goal by default (implicit goals for chat-path work).
- G-C: steer-of-episode — user steering reaches a running goal-loop episode.
- G-D: surface parity (TUI/GUI goals view) + docs + e2e suite.

Non-goals (out of this tree): competitive/multi-solver fulfillment, cross-chain anything, payment settlement.

## 2. Frozen wire/storage contracts (quote these verbatim; never paraphrase)

### Contract C1 — GoalSpec (creation request shape)

```go
// Owner: 01-goal-ingestion leaf. Consumers: RPC handler, CLI, HTTP handler, definition-file parser.
type GoalSpec struct {
    EmployeeID string     `json:"employee_id"`
    Title      string     `json:"title"`
    Mandate    string     `json:"mandate"`
    Source     string     `json:"source,omitempty"`      // "user" | "trigger"; default "user"
    TriggerRef string     `json:"trigger_ref,omitempty"`
    Gate       *GoalGateSpec `json:"gate,omitempty"`
}
type GoalGateSpec struct {
    Command          string `json:"command,omitempty"`
    TimeoutSeconds   int    `json:"timeout_seconds,omitempty"`
    SkipWhenUnchanged bool  `json:"skip_when_unchanged,omitempty"`
}
```

### Contract C2 — new GoalSource value

```go
// internal/employee/goal.go — add to the GoalSource const block:
SourceImplicit GoalSource = "implicit"
// A goal auto-created to anchor chat-path Plans that arrived with no explicit goal.
// ValidateGoalSource (internal/employee/authority.go): SourceImplicit is valid at
// every tier and never requires signoff; implicit goals auto-retire when their
// last active plan completes and no new plan references them for 30 days.
```

### Contract C3 — goal linkage columns

```sql
-- migrations (append to the ALTER TABLE chain in GoalStore.migrate, internal/plan store, internal/task store):
ALTER TABLE employee_plans ADD COLUMN goal_id TEXT;   -- internal/plan store_sqlite
ALTER TABLE tasks        ADD COLUMN goal_id TEXT;     -- internal/task store
```

```go
// internal/plan/plan.go — Plan gains:
GoalID string `json:"goal_id,omitempty"` // owning goal; implicit-goal ID when chat-created
// internal/task/task.go — Task gains:
GoalID string `json:"goal_id,omitempty"`
```

Backfill rule: existing rows keep empty goal_id (valid: "no goal" is legacy-legal; new writes always set it).

### Contract C4 — new RPC / CLI / HTTP surfaces

```
RPC:  agents.goals.create  params: GoalSpec JSON  -> {"goal": <Goal>}
CLI:  meept agents goal-create <employee-id> --title T --mandate M [--gate-command CMD]
HTTP: POST /api/v1/agents/{id}/goals  body: GoalSpec JSON (employee_id from path wins)
```

Registration points (verified):
- RPC: internal/employee/handler.go `RPCHandler` method map (~line 117).
- HTTP: internal/employee/api_handlers.go `RegisterRoutes` (~line 59) + a handleGoalsCreate mirroring handleGoalsList (line 355).
- CLI: cmd/meept/agents_cmd.go — new cobra command beside `goals` (line 944).
- Definition file: `goals: [GoalSpec...]` array parsed in Manager.Hire (internal/employee/manager.go:1056) via a new `Goals []GoalSpec` field on HireRequest; each spec calls GoalStore.Create. This activates the currently-unparsed doc example in docs/workflows/employees.md:120.

### Contract C5 — steer-of-episode

```go
// internal/employee/goal_loop.go:
// TriggerEvent.Source gains "steer" (payload: {"source":"steer","content":...}).
// New method: func (l *GoalLoop) Steer(ctx context.Context, content string) error
//   - if an episode is parked/in-flight: park-resume path (park.go) re-enters
//     Decide with Source="steer"; the assess prompt appends the steering text.
//   - if no episode is active: no-op returning nil (steer of a quiet employee
//     is not an error).
// Wiring: Manager gains SteerEmployee(ctx, id, content) that acquires the same
// per-employee invoke mutex as triggerViaGoalLoop (manager.go:1455).
// RPC: agents.steer  params: {"employee_id":..., "content":...}
```

### Contract C6 — implicit-goal anchoring

- Chat-path plan creation (internal/agent orchestrator/quickplan paths that call plan.Manager.CreatePlan) resolves the session's active goal:
  1. explicit goal set on the session (new session metadata key `goal_id`) wins;
  2. else the employee's single active non-implicit goal, if exactly one;
  3. else create-or-reuse a SourceImplicit goal titled from the plan title.
- PlanCreator implementations (internal/employee/goal_loop.go:195 interface) set Plan.GoalID before CreatePlan returns.

## 3. Coding conventions (meept repo)

- Go 1.x, stdlib-first; JSON5 for config; lower-case UI text.
- Errors: wrap with %w; sentinels exported (pattern: ErrGoalNotFound).
- New feature tests go in e2e/suites/<name>/ (build tag `e2e`, manifest.json entry). Existing unit tests may gain cases for bug fixes only — new employee-unit coverage must ride existing internal/employee/*_test.go files or the e2e suite.
- SQLite: follow the ALTER TABLE + "duplicate column name" guard idiom in GoalStore.migrate (goal.go:539).
- Every Set*/mutator gets a nil guard (docs/agents/coding-practices.md).
- No time.Now().UnixNano()/math/rand for IDs — pkg/id.Generate.
- mutexio rule: no I/O under mutex (snapshot-then-IO, goal.go:703 pattern).
- Tests on macOS: go test -p 2 (ephemeral port exhaustion).
- AGENTS.md must be updated in the same commit for any new RPC/HTTP/CLI surface.
- Docs: docs/workflows/employees.md updated in the same leaf as its surface.

## 4. Pitfalls for this repo (pass to every dispatch)

- Do NOT use read_file output as write_file input (line-number corruption). Explore with search_files / terminal cat.
- internal/employee is 22.7K lines; read only the files named in the leaf.
- manager.go has Phase-5 ErrNotImplemented stubs (lines 1787-1973) — leave them; they degrade only when stores are nil.
- Goal model uses value-receiver helpers vs pointer-receiver — match existing receiver style per file.
- Persistent-queue/agent AGENTS.md invariants apply if touching internal/agent (session_id vs conversation_id).

## 5. Commit policy

Orchestrator-only commits. Implementation agents: "Do NOT commit. Do NOT run git add. Write code, run tests, report results only."
Commit messages: `feat(employees): <leaf summary> (goal-completion/<leaf>)`.
