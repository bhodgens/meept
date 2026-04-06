# Plan: Hierarchical Async Multi-Agent System

## Goal
Convert meept from synchronous single-agent dispatch to a three-layer hierarchical async system where complex requests are broken off into background tasks while chat remains responsive.

## Architecture

```
User Message → ChatHandler
  → Dispatcher.ClassifyAndRoute() (fast, keyword-based)
  → Simple chat? → agent.RunOnce() inline (unchanged)
  → Complex request? (code/debug/plan/schedule):
      1. Create Task, link to session
      2. Send acknowledgment immediately ("Working on task <id>")
      3. Publish "orchestrator.plan" event
      → Strategic Layer (planner agent decomposes → TaskSteps)
      → Tactical Layer (schedule ready steps → Jobs targeting agents)
      → Execution Layer (workers claim jobs, agent loops process)
      → Results flow back → chat.response via session linkage
```

## Implementation Phases

### Phase 1: Foundation (config + task steps)

**`internal/config/schema.go`** (MODIFY)
- Change `MultiAgentConfig.Enabled` default from `false` to `true` (line 534)
- Add `OrchestratorConfig` struct to `Config`:
  ```go
  type OrchestratorConfig struct {
      MaxPlanSteps     int `toml:"max_plan_steps"`      // default: 10
      MaxResearchSteps int `toml:"max_research_steps"`  // default: 3
      PlannerTimeout   int `toml:"planner_timeout"`     // seconds, default: 120
      TokenBudgetAlert int `toml:"token_budget_alert"`  // default: 5000
  }
  ```

**`internal/task/step.go`** (NEW)
- `StepState`: pending, ready, scheduled, running, completed, failed, skipped
- `TaskStep` struct: ID, TaskID, Description, DependsOn []string, ToolHint, AgentID, JobID, State, Result, Sequence, timestamps
- `StepStore` with SQLite table `task_steps`:
  - `Create(step)`, `Update(step)`, `GetByID(id)`, `ListByTaskID(taskID)`
  - `GetReadySteps(taskID)` — steps where all DependsOn are completed
  - `SetState(id, state)`, `SetJobID(id, jobID)`, `SetResult(id, result)`
  - `AreAllCompleted(taskID)`, `HasFailures(taskID)`

**`internal/task/store.go`** (MODIFY)
- Add `StepStore` creation alongside task store init
- Add `GetStepStore() *StepStore` accessor

### Phase 2: Strategic Layer

**`internal/agent/strategic.go`** (NEW)
- `StrategicPlanner` struct with registry, taskStore, stepStore, workspace, bus, logger
- `PlanRequest` message type: `{task_id, session_id, input, intent}`
- `Plan(ctx, req PlanRequest) error`:
  1. Set task state to `planning`
  2. Use `registry.Get("planner")` agent to generate structured JSON plan
  3. Parse planner output into `[]TaskStep`, persist via `StepStore`
  4. Fallback: if JSON parse fails, create single step with full input
  5. Set root steps (empty DependsOn) to `StepReady`
  6. Publish `"orchestrator.schedule"` event with `{task_id}`
  7. Publish `"task.planned"` event for TUI progress

**`internal/agent/collaborative.go`** (MODIFY)
- Reconcile `TaskStep` types with new `task.TaskStep`
- Keep approval workflow but gate it behind config flag (optional)

### Phase 3: Tactical Layer

**`internal/agent/tactical.go`** (NEW)
- `TacticalScheduler` struct with stepStore, taskStore, queue, registry, bus
- `ScheduleReadySteps(ctx, taskID)`:
  1. Query `StepStore.GetReadySteps(taskID)`
  2. For each: `selectAgent(step)` based on ToolHint mapping:
     - code/refactor → coder, debug/fix → debugger, analyze/research → analyst, git/commit → committer, default → chat
  3. Create `queue.Job` with AgentID, TaskID, Payload containing step info
  4. `queue.Enqueue(job)`, update step state to `scheduled`
- `OnJobCompleted(ctx, jobID)`:
  1. Find step by job_id, mark completed with result
  2. Update parent task's CompletedJobs counter
  3. Find newly unblocked steps (load all steps, check deps in-memory)
  4. Set unblocked steps to `ready`, call `ScheduleReadySteps()` again
  5. If all completed → set task to `completed`, publish `"task.completed"`
- `OnJobFailed(ctx, jobID, err)`:
  1. Mark step failed, update FailedJobs counter
  2. If retries exhausted and all paths blocked → set task to `failed`
  3. Publish `"task.failed"`
- `selectAgent(step)` — deterministic ToolHint-to-agent mapping

### Phase 4: Orchestrator (coordinator)

**`internal/agent/orchestrator.go`** (NEW)
- `Orchestrator` struct ties strategic + tactical together
- `Start(ctx)` subscribes to bus topics:
  - `"orchestrator.plan"` → calls `strategic.Plan()`
  - `"orchestrator.schedule"` → calls `tactical.ScheduleReadySteps()`
  - `"queue.job.completed"` → calls `tactical.OnJobCompleted()`
  - `"queue.job.failed"` → calls `tactical.OnJobFailed()`
- `Stop(ctx)` for clean shutdown
- Implements `Name() string` for registry

### Phase 5: Async Dispatch

**`internal/agent/handler.go`** (MODIFY)
- `handleRequest()` gains async branch:
  ```
  if dispatcher.ShouldDispatchAsync(result):
      send immediate ack: "Working on task <id>"
      publish "orchestrator.plan" event
      return (non-blocking)
  else:
      handle synchronously (unchanged)
  ```
- Add subscription to `"task.completed"` and `"task.failed"`:
  - Push `ChatResponse` with result/error back to linked session
- Add `publishPlanRequest()` helper

**`internal/agent/dispatcher.go`** (MODIFY)
- Add `ShouldDispatchAsync(result *DispatchResult) bool`:
  - Returns true for code/debug/plan/schedule intents or RequiresPlanning

### Phase 6: Execution Layer (modified job processor)

**`internal/daemon/components.go`** (MODIFY)
- Replace `AgentJobProcessor` with `MultiAgentJobProcessor`:
  - Uses `AgentRegistry.Get(job.AgentID)` to dispatch to agent-specific loops
  - Falls back to main AgentLoop if agent not found
  - Uses `RunWithTask()` when task context available, `RunOnce()` otherwise
- Wire new components in `NewComponents()`:
  - Create `StepStore` alongside TaskStore
  - Create `StrategicPlanner`, `TacticalScheduler`, `Orchestrator`
  - Start `Orchestrator` in `Start()`
  - Register Orchestrator for clean shutdown

### Phase 7: TUI — Task Detachment, Sidebar Status, and Task Dashboard

When a complex request goes async, it "detaches" from chat: the chat gets an acknowledgment and the task appears in both the sidebar task list and the Tasks dashboard (Ctrl+2).

#### 7a. Chat Detachment UX

**`internal/tui/models/chat.go`** (MODIFY)
- When the chat receives an async acknowledgment (response containing a task_id), render it as a special "detached task" message:
  ```
  ┌ task detached ─────────────────────────────┐
  │ Working on: "implement CSV parser + tests" │
  │ Task ID: task-20260228...                   │
  │ View progress: [2] tasks                   │
  └─────────────────────────────────────────────┘
  ```
- When `task.completed` or `task.failed` events arrive for a linked session, inject a result message back into chat:
  ```
  ┌ task completed ────────────────────────────┐
  │ Task: "implement CSV parser + tests"       │
  │ Steps: 3/3 completed                       │
  │ Result summary...                          │
  └─────────────────────────────────────────────┘
  ```
- Add `ChatDetachedTaskMsg` and `ChatTaskResultMsg` message types
- The chat model processes these by appending styled system messages

#### 7b. Sidebar Task List with Per-Task Status Bars

**`internal/tui/sidebar.go`** (MODIFY)

The existing sidebar `PanelTasks` currently shows basic task items. Enhance `SidebarTaskItem` and `renderTasksPanel()`:

- Extend `SidebarTaskItem` to include step progress:
  ```go
  type SidebarTaskItem struct {
      ID            string
      Title         string
      Status        string
      AgentID       string       // assigned agent
      CompletedJobs int
      TotalJobs     int
      Created       string
  }
  ```

- Each task in the sidebar gets a compact status bar:
  ```
  ▾ Tasks
    ● csv-parser [coder]
      ██░░░░░░ 1/3
    ◐ refactor-auth [planner]
      ░░░░░░░░ 0/2
    ✓ fix-typo [chat]
      ████████ 1/1
  ```
  Format per task: `{state_icon} {task_name_truncated} [{agent}]` on line 1, `{progress_bar} {completed}/{total}` on line 2.

- Modify `renderTasksPanel()` to:
  1. Show the assigned agent in brackets after the task name
  2. Render a compact progress bar (`renderProgressBar` already exists — reuse from tasks model)
  3. Limit to 4 tasks visible (2 lines each = 8 lines), with "+N more..." overflow

- Modify `refreshData()` to fetch tasks from `task.list` RPC with step-level data (use the existing `ListTasks` RPC, or add `ListTasksWithSteps` if needed)

#### 7c. Tasks Dashboard (Ctrl+2) — Step-Level Granularity

**`internal/tui/models/tasks.go`** (MODIFY)

The Tasks dashboard already has a task detail modal (`renderTaskDetailModal`). Extend it significantly:

**New: Task Steps sub-table in detail view**

When a task is selected and Enter is pressed, the detail modal shows each step/sub-job with:
- Agent name prefix (who's doing the work)
- Step description
- Per-step status bar
- Step state icon

```
╔══ Task: implement CSV parser + tests ════════════════╗
║                                                       ║
║ ID:       task-20260228153045.123                      ║
║ State:    ● executing                                 ║
║ Progress: ██████░░░░░░░░░░░░░░ 2/5 (40%)             ║
║           ✓ 2 completed  ○ 2 pending  ✗ 1 failed     ║
║                                                       ║
║ ─── Steps ───                                         ║
║  1. [coder]    Write CSV parser function        ✓ done║
║                ████████████████████ 100%               ║
║  2. [analyst]  Research CSV edge cases          ✓ done║
║                ████████████████████ 100%               ║
║  3. [coder]    Implement error handling         ● exec║
║                ██████████░░░░░░░░░░  50%              ║
║  4. [coder]    Write unit tests                 ○ pend║
║                ░░░░░░░░░░░░░░░░░░░░   0%  (blocked)  ║
║  5. [committer] Commit changes                  ○ pend║
║                ░░░░░░░░░░░░░░░░░░░░   0%  (blocked)  ║
║                                                       ║
║ ─── Memory Context ───                                ║
║ Inherited:  from task-20260228...                     ║
║ Memory refs: ⚡3 refs  📝1 created                    ║
║                                                       ║
║ [Esc/q] close                                         ║
╚═══════════════════════════════════════════════════════╝
```

Implementation details:

- **Add `TaskStepView` type** in `internal/tui/types/types.go`:
  ```go
  type TaskStepView struct {
      ID          string `json:"id"`
      TaskID      string `json:"task_id"`
      Description string `json:"description"`
      AgentID     string `json:"agent_id"`
      State       string `json:"state"` // pending, ready, scheduled, running, completed, failed, skipped
      Result      string `json:"result,omitempty"`
      Sequence    int    `json:"sequence"`
      DependsOn   []string `json:"depends_on,omitempty"`
      JobID       string `json:"job_id,omitempty"`
  }
  ```

- **Extend `TaskExtended`** in `internal/tui/types/types.go` with steps:
  ```go
  type TaskExtended struct {
      Task
      // existing memory fields...
      Steps []TaskStepView `json:"steps,omitempty"` // NEW
  }
  ```

- **Add `ListTaskSteps` RPC** — new RPC method `task.steps` that returns steps for a task:
  - In `internal/rpc/proxy.go`: register `"task.steps"` handler
  - In `internal/tui/rpc.go`: add `ListTaskSteps(taskID string)` method
  - In `internal/tui/types/types.go`: add `TaskStepsResponse` type

- **Modify `renderTaskDetailModal()`** to:
  1. Fetch steps for the selected task (cached after first fetch, refreshed on `r`)
  2. Render each step as two lines:
     - Line 1: `{sequence}. [{agent_id}]  {description}  {state_icon} {state_label}`
     - Line 2: `{progress_bar}  {percent}%  {blocked_indicator}`
  3. Steps in `running` state show a progress estimate (from `agent.progress` events if available)
  4. Steps in `pending` state with unsatisfied deps show "(blocked)" indicator
  5. Scrollable if more than ~8 steps (use viewport or truncate with scroll hint)

- **Modify `updateTasksTable()`** — the table itself gains a "Steps" column showing `completed/total`:
  ```
  Name            State     Agent       Steps     Progress      Memory    Updated
  csv-parser      ● exec    coder       2/5       ██████░░      ⚡3⬅1    2m ago
  auth-refactor   ◐ plan    planner     0/0       ░░░░░░░░      ⚡0⬅0    5m ago
  fix-typo        ✓ done    chat        1/1       ████████      ⚡1⬅0    12m ago
  ```
  Add a new "Steps" column (compact: "2/5") to the table columns in `setTasksColumns()`.

#### 7d. Event-Driven TUI Updates

**`internal/tui/sidebar.go`** (MODIFY)
- Handle new bus topics in `EventStreamDataMsg` processing:
  - `"task.planned"` → refresh tasks panel data, show step count
  - `"task.progress"` → update specific task's progress bar in sidebar
  - `"task.completed"` / `"task.failed"` → update state icon, trigger chat result injection

**`internal/tui/app.go`** (MODIFY)
- Add handling for `ChatDetachedTaskMsg`:
  - When received, append a styled "task detached" message in chat
  - Auto-switch sidebar tasks panel to expanded if collapsed
- Add handling for `ChatTaskResultMsg`:
  - When received, append a styled "task result" message in chat
  - Flash the Tasks tab indicator to draw attention
- Forward `task.completed`/`task.failed` events to chat model when they match the current session

**`internal/tui/events.go`** (MODIFY)
- Ensure EventStream subscribes to `task.*` topic (already does via existing bus.subscribe)
- Add specific handling for `task.step.progress` events that carry per-step progress data

#### 7e. New RPC Methods for TUI

**`internal/rpc/proxy.go`** (MODIFY)
- Register `"task.steps"` handler: `p.makeProxy("task.steps", "task.result", 10*time.Second)`

**`internal/tui/rpc.go`** (MODIFY)
- Add `ListTaskSteps(taskID string) (*types.TaskStepsResponse, error)` method
- Add `ListTasksWithSteps() (*types.TaskExtendedListResponse, error)` method (enhanced version that includes steps inline)

### Phase 8: Tests

**`internal/task/step_test.go`** (NEW)
- StepStore CRUD, GetReadySteps with dependency configs, edge cases

**`internal/agent/strategic_test.go`** (NEW)
- Mock planner agent, verify step creation, JSON fallback

**`internal/agent/tactical_test.go`** (NEW)
- Mock queue/store, verify scheduling, dep unlocking, completion detection

**`internal/agent/handler_test.go`** (MODIFY)
- Test async dispatch: ack sent, plan request published
- Test sync path unchanged for simple chat
- Test task completion pushes response

**`tests/orchestrator_test.go`** (NEW)
- Full end-to-end: send complex request → verify ack → steps created → jobs processed → result returned

## Bus Topics

| Topic | Publisher | Subscriber | Purpose |
|-------|-----------|------------|---------|
| `orchestrator.plan` | ChatHandler | Orchestrator | Trigger strategic planning |
| `orchestrator.schedule` | StrategicPlanner | Orchestrator | Trigger tactical scheduling |
| `task.planned` | StrategicPlanner | TUI sidebar, TUI chat | Planning done, show step count |
| `task.progress` | TacticalScheduler | TUI sidebar, TUI tasks | Steps advancing, update bars |
| `task.step.progress` | Agent (via bus) | TUI tasks detail | Per-step progress for active step |
| `task.completed` | TacticalScheduler | ChatHandler, TUI chat, TUI sidebar | Final result, inject into chat |
| `task.failed` | TacticalScheduler | ChatHandler, TUI chat, TUI sidebar | Task failure, inject into chat |

Existing topics (unchanged): `queue.job.completed`, `queue.job.failed`, `agent.progress`, `agent.action`, `agent.result`

## Existing Infrastructure Reused (no changes)

- `internal/queue/*` — PersistentQueue, Job model, ClaimNextForAgent
- `internal/worker/*` — Worker Pool, polling, Process interface
- `internal/bus/*` — MessageBus pub/sub
- `internal/task/task.go` — Task model (InheritedFrom, AssignedAgent, LinkedSessions, MemoryRefs already exist)
- `internal/agent/loop.go` — RunOnce(), RunWithTask(), reasoningCycle unchanged
- `internal/agent/registry.go` — lazy loop creation, tool filtering
- `internal/agent/spec.go` — 8 agent specs unchanged
- `internal/agent/workspace.go` — git-backed workspace manager

## TUI Files Modified

- `internal/tui/sidebar.go` — Enhanced task panel with per-task progress bars and agent labels; new event handlers for task.planned/progress/completed/failed
- `internal/tui/models/tasks.go` — Task detail modal shows step-level granularity with agent prefix, per-step progress bars, blocked indicators; new "Steps" column in table
- `internal/tui/models/chat.go` — Detached task messages (styled boxes for task dispatch + result injection)
- `internal/tui/app.go` — Route task events to chat model, flash tab indicators on task completion
- `internal/tui/events.go` — EventStream already polls task/queue/agent topics (unchanged, topics already covered)
- `internal/tui/types/types.go` — New TaskStepView type, Steps field on TaskExtended, TaskStepsResponse
- `internal/tui/rpc.go` — New ListTaskSteps() RPC method

## Key Design Decisions

1. **Simple chat stays sync**: Only code/debug/plan/schedule intents go async. Regular conversation is unaffected.
2. **Deterministic agent selection**: Tactical layer uses ToolHint→agent mapping, not LLM. Fast and predictable.
3. **In-memory DAG resolution**: Load all steps for a task, check deps in Go code rather than complex SQL with JSON arrays.
4. **Existing Planner agent**: Strategic layer uses the existing "planner" agent spec via `AgentRegistry.Get("planner")` — no new agent type needed.
5. **Graceful degradation**: If planning fails (LLM error), fall back to single-step execution with the full request.

## Verification

```bash
# Build
go build -o bin/meept-daemon ./cmd/meept-daemon && go build -o bin/meept ./cmd/meept

# Run tests
go test ./internal/task/... -v
go test ./internal/agent/... -v
go test ./tests/... -v

# Manual test
./bin/meept-daemon -f &
./bin/meept chat "Write a Go function to parse CSV files and add comprehensive tests"
# Expected: immediate ack → progress events → final result pushed to chat
# Check task tracking:
./bin/meept status
```
