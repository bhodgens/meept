# Orchestrator Plan Flow

> **Status:** Verified (2026-05-07)
> **Related:** `internal/agent/orchestrator.go`, `internal/agent/handler.go`, `internal/agent/strategic.go`

## Overview

This document describes the flow of `orchestrator.plan` events through the Meept multi-agent system, from initial user request through task decomposition and execution scheduling.

## Message Flow

```
┌─────────────────┐     ┌─────────────────┐     ┌──────────────────┐     ┌───────────────────┐
│   ChatHandler   │     │   Orchestrator  │     │ StrategicPlanner │     │ TacticalScheduler │
│                 │     │                 │     │                  │     │                   │
└────────┬────────┘     └────────┬────────┘     └─────────┬────────┘     └─────────┬─────────┘
         │                       │                        │                        │
         │ publishPlanRequest    │                        │                        │
         │ "orchestrator.plan"   │                        │                        │
         ├──────────────────────>│                        │                        │
         │                       │                        │                        │
         │                       │ handlePlanRequest      │                        │
         │                       ├───────────────────────>│                        │
         │                       │                        │                        │
         │                       │                        │ Plan()                 │
         │                       │                        │                        │
         │                       │                        │ create steps           │
         │                       │                        │                        │
         │                       │                        │ "task.planned"         │
         │                       │                        ├───────────────────────>│
         │                       │                        │ "orchestrator.schedule"│
         │                       ├───────────────────────>│                        │
         │                       │                        ├───────────────────────>│
         │                       │                        │                        │ ScheduleReadySteps()
         │                       │                        │                        │
         │                       │                        │                        │ enqueue jobs
         │                       │                        │                        │
         │                       │                        │                        │ "queue.job.*"
         │                       │                        │                        │
```

## Component Responsibilities

### ChatHandler (`internal/agent/handler.go`)

| Responsibility | Details |
|---------------|---------|
| **Creates PlanRequest** | Populates `TaskID`, `SessionID`, `Input`, `Intent` from `DispatchResult` |
| **Extracts compound metadata** | If `IntentCompound`, extracts `compound_type` from task metadata |
| **Publishes to `orchestrator.plan`** | Uses message bus with `MessageTypeRequest` |
| **Logs delivery count** | Warns if `delivered == 0` (no subscribers) |

**Key method:** `publishPlanRequest(result *DispatchResult, sessionID string)`

```go
delivered := h.bus.Publish("orchestrator.plan", msg)
if delivered == 0 {
    h.logger.Warn("Plan request published with no subscribers",
        "task_id", result.Task.ID)
}
```

### Orchestrator (`internal/agent/orchestrator.go`)

| Responsibility | Details |
|---------------|---------|
| **Subscribes to 4 topics** | `orchestrator.plan`, `orchestrator.schedule`, `queue.job.completed`, `queue.job.failed` |
| **Delegates plan requests** | Calls `StrategicPlanner.Plan()` |
| **Delegates schedule requests** | Calls `TacticalScheduler.ScheduleReadySteps()` |
| **Handles job completion/failure** | Routes to TacticalScheduler callbacks |

**Key method:** `handlePlanRequest(ctx context.Context, msg *models.BusMessage)`

```go
func (o *Orchestrator) handlePlanRequest(ctx context.Context, msg *models.BusMessage) {
    var req PlanRequest
    if err := json.Unmarshal(msg.Payload, &req); err != nil {
        o.logger.Error("Failed to parse plan request", "error", err)
        return
    }

    if err := o.strategic.Plan(ctx, req); err != nil {
        o.logger.Error("Strategic planning failed",
            "task_id", req.TaskID, "error", err)
    }
}
```

### StrategicPlanner (`internal/agent/strategic.go`)

| Responsibility | Details |
|---------------|---------|
| **Fetches task from store** | Validates task exists |
| **Sets task state to `planning`** | Updates task state in database |
| **Generates plan via LLM** | Uses planner agent to decompose into steps, or fallback for simple requests |
| **Persists steps** | Creates `TaskStep` records with dependencies |
| **Updates task state to `executing`** | Sets `TotalJobs` count |
| **Publishes `task.planned`** | Notifies UI of plan creation |
| **Publishes `orchestrator.schedule`** | Triggers tactical scheduling |

**Key method:** `Plan(ctx context.Context, req PlanRequest) error`

### TacticalScheduler (`internal/agent/tactical.go`)

| Responsibility | Details |
|---------------|---------|
| **Receives schedule request** | Via `orchestrator.schedule` bus event |
| **Finds ready steps** | Steps with no dependencies or all deps satisfied |
| **Acquires semaphore slots** | Respects global and per-agent concurrency limits |
| **Creates queue jobs** | Enqueues steps for execution |
| **Updates step state to `scheduled`** | Persists job assignment |
| **Publishes `task.progress`** | Silent progress updates (sidebar only) |

**Key method:** `ScheduleReadySteps(ctx context.Context, taskID string) error`

## Data Structures

### PlanRequest

```go
type PlanRequest struct {
    TaskID       string `json:"task_id"`
    SessionID    string `json:"session_id"`
    Input        string `json:"input"`
    Intent       string `json:"intent"`
    IsCompound   bool   `json:"is_compound,omitempty"`
    CompoundType string `json:"compound_type,omitempty"`
}
```

###orchestrator.schedule

```go
// Payload structure (anonymous inline struct)
{
    "task_id": "task-xxx"
}
```

## Error Handling

| Component | Error Scenario | Handling |
|-----------|---------------|----------|
| **ChatHandler** | No subscribers | Log warning, continue |
| **ChatHandler** | Marshal failure | Log error, return early |
| **Orchestrator** | Invalid payload | Log error, skip message |
| **Orchestrator** | Nil StrategicPlanner | Panics (should be configured) |
| **StrategicPlanner** | Task not found | Returns error |
| **StrategicPlanner** | Plan generation fails | Falls back to single-step plan |
| **TacticalScheduler** | Semaphore full | Returns error, step remains ready |
| **TacticalScheduler** | Queue enqueue fails | Releases semaphore, logs error |

## Testing

### Unit Tests

```bash
# Orchestrator subscription and parsing
go test ./internal/agent/... -run "TestOrchestrator" -v

# Handler publishPlanRequest
go test ./internal/agent/... -run "TestChatHandler_PublishPlanRequest" -v
```

### Integration Test

Full flow verification:
```bash
# Verify end-to-end flow (requires full daemon setup)
make go-daemon
./bin/meept chat "Build a feature with API and tests"
```

Expected flow:
1. Chat displays acknowledgment with task ID
2. Sidebar shows task planning
3. Subtasks appear and execute
4. Final result displays in chat

## Bus Topics Summary

| Topic | Publisher | Subscriber | Payload Type |
|-------|-----------|------------|--------------|
| `orchestrator.plan` | ChatHandler | Orchestrator | `PlanRequest` |
| `orchestrator.schedule` | StrategicPlanner | Orchestrator→TacticalScheduler | `{"task_id": string}` |
| `task.planned` | StrategicPlanner | TUI, ChatHandler | `{"task_id", "session_id", "total_steps", "ready_steps"}` |
| `task.progress` | TacticalScheduler | TUI, ChatHandler | `{"task_id", "completed_jobs", "total_jobs", "current_step", "silent"}` |
| `task.completed` | TacticalScheduler | ChatHandler, TUI | `{"task_id", "name", "completed_jobs", "total_jobs", "linked_sessions", "steps", "result"}` |
| `task.failed` | TacticalScheduler | ChatHandler, TUI | `{"task_id", "name", "failed_jobs", "error", "linked_sessions"}` |

## Related Documentation

- **Dispatcher:** `docs/concepts/multi-agent.md` - How requests are classified and routed
- **Task Store:** `docs/reference/task-store.md` - Task persistence and lifecycle
- **Message Bus:** `internal/bus/bus.go` - Pub/sub implementation details
