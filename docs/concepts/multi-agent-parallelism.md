# Multi-Agent Parallelism

This document describes Meept's current parallel execution model, existing
collaboration modes, and the gaps that the planned Team Mode will fill.

## Current Parallel Execution Flow

When a user request requires decomposition, the following pipeline executes:

```
User Input
    |
    v
Dispatcher.ClassifyAndRoute()
    |
    v
Orchestrator.handlePlanRequest()
    |
    v
StrategicPlanner.Plan()
    |-- LLM decomposes request into N steps (with dependency graph)
    |-- Each step gets a ToolHint (code, debug, analyze, git, ...)
    |-- Steps with empty DependsOn are root steps (parallel-ready)
    |-- Steps are persisted to StepStore
    |
    v
Orchestrator.handleScheduleRequest()
    |
    v
TacticalScheduler.ScheduleReadySteps()
    |-- For each ready step:
    |   |-- Select agent by ToolHint
    |   |-- Acquire semaphore slot (global: 10, per-agent: 3)
    |   |-- Create queue job with StepJobPayload
    |   |-- Enqueue to job queue
    |
    v
Worker Pool
    |-- Workers pull jobs from queue by agent_id matching
    |-- Each worker runs its assigned agent independently
    |-- On completion: publish "queue.job.completed" or "queue.job.failed"
    |
    v
Orchestrator.handleJobCompleted() / handleJobFailed()
    |
    v
TacticalScheduler.OnJobCompleted()
    |-- Promote newly-unblocked steps to ready
    |-- Trigger ScheduleReadySteps for next wave
    |-- When all steps done: mark task complete
```

### Key characteristics

1. **Independent execution**: Each step runs as a separate queue job. Agents do not
   communicate with each other during execution.
2. **Semaphore-gated concurrency**: A global semaphore (default 10) and per-agent
   semaphores (default 3) limit parallelism to prevent resource exhaustion.
3. **Dependency-aware scheduling**: Steps declare `depends_on` links. A step only
   becomes "ready" after all its dependencies reach terminal state.
4. **No inter-agent coordination**: Once an agent picks up a job, it works in
   isolation. There is no mechanism for agents to share partial results, ask
   questions, or coordinate during parallel execution.

### Where parallel execution happens

- Multiple steps with no dependencies (empty `depends_on`) are scheduled
  simultaneously by `ScheduleReadySteps`.
- Each step becomes a separate queue job, picked up by different workers.
- The `TacticalScheduler` uses non-blocking semaphore acquisition, so if no slot
  is available the step stays in "ready" state and will be retried on the next
  scheduling cycle.

## Existing Collaboration Modes

Beyond independent parallel execution, Meept has two structured collaboration
modes and a bus-channel pairing system. These enable **two** agents to work
together on a single task, but they are distinct from N-agent parallel teams.

### Mode 1: Pair Programming (`pair_programming`)

**Driver**: `PairProgrammingDriver` in
`internal/agent/collaboration_pair_driver.go`

Two agents alternate holding an "editor token" in a shared workspace:

```
Agent A (driver) -- writes code, runs tests -->
Agent B (observer) -- reviews, approves or requests changes -->
    (repeat until approved or max turns reached)
```

- Uses `TurnManager` to enforce token-based alternation.
- Agents communicate through prompts (observer output becomes driver input next
  turn).
- Terminates on "approve" action or turn exhaustion.
- Session state: `created -> active -> converged | exhausted | failed`.

### Mode 2: Differential (`differential`)

**Driver**: `DifferentialDriver` in
`internal/agent/collaboration_diff_driver.go`

Four-phase A/B pipeline with a differentiator:

```
Phase 1: Fork      -- Create branch-a and branch-b workspaces
Phase 2: Implement -- Run independent PairManager sessions on each branch
Phase 3: Validate  -- Checkpoint results, handle fallback if one branch fails
Phase 4: Diff      -- Differentiator agent synthesizes best-of-both output
```

- Two agents implement the same task independently on separate branches.
- A third agent (differentiator) merges the best parts.
- **Important gap**: Phase 2 runs branches **sequentially** (`RunAllRounds` is
  called for branch A, then branch B). True parallel branch execution is not
  implemented.

### Mode 3: Bus Channel Pairing (Option C)

**Component**: `PairOrchestrator` in `internal/agent/pair_orchestrator.go`

Actor-reviewer loop driven entirely through the message bus:

```
Bus message "pair.start" -->
    PairOrchestrator.handleStartRequest()
    |-- Run actor agent
    |-- Publish turn to pair.{sessionID}.turn
    |-- Run reviewer agent
    |-- Classify verdict (APPROVED/REJECTED/NEEDS_MORE)
    |-- On APPROVED: publish to pair.result
    |-- On REJECTED: revise prompt, next turn
```

- Triggered by `IntentPair` classification.
- Free-form collaborative conversation (debate, brainstorm, exploratory
  debugging).
- Does not create step-based tasks (see `IntentPair.ShouldCreateTask()`).
- Uses bus topics for observability: `pair.{sessionID}.turn`, `pair.result`,
  `pair.error`.

### Collaboration Engine Registration

All collaboration modes are registered through the `CollaborationEngine` in
`internal/agent/collaboration_engine.go`:

```go
collabEngine.RegisterMode("pair_programming", pairProgrammingDriver)
collabEngine.RegisterMode("differential", differentialDriver)
```

The engine provides:
- Session lifecycle management (`CreateSession`, `RunSession`).
- Nested session support (agent-initiated collaboration with depth limiting).
- Mode resolution (`resolveParticipants` picks default agents per mode).
- Bus event publishing for session lifecycle events.

### Collaboration Mode Interface

```go
// internal/agent/collaboration.go
type CollaborationMode interface {
    Name() string
    Run(ctx context.Context, sess *CollaborationSession) (*CollaborationResult, error)
    CanInitiate(agentID string, reason string) bool
}
```

## Identified Gaps

### Gap 1: No inter-agent communication during parallel execution

When N steps run in parallel (e.g., 3 independent coding steps), the assigned
agents work in complete isolation. They cannot:

- Share partial results or discoveries with each other.
- Ask questions about shared context (e.g., "has anyone modified config.go?").
- Coordinate to avoid conflicting file changes.
- Signal that the overall goal has shifted.

**Impact**: For simple tasks this is fine. For complex multi-step tasks where
steps have implicit dependencies (not captured in `depends_on`), agents may
produce conflicting outputs or duplicate work.

### Gap 2: Differential branches run sequentially

In `DifferentialDriver.phaseImplement`, both branches are executed one after
another:

```go
_, errA := d.pairMgr.RunAllRounds(ctx, sessionA.ID)  // blocks until A finishes
_, errB := d.pairMgr.RunAllRounds(ctx, sessionB.ID)  // then runs B
```

This misses the opportunity for true A/B parallelism since branch A and branch
B are independent by design.

### Gap 3: No N-agent team mode

Current collaboration modes support exactly 2 agents (pair programming, bus
pairing) or 2-3 agents (differential with a differentiator). There is no mode
for:

- A lead agent orchestrating N specialist agents in parallel.
- Shared task boards for team progress tracking.
- Broadcast or targeted inter-agent messaging during execution.
- Result aggregation from multiple parallel workers.

### Gap 4: No shared state during parallel step execution

Steps accumulate context in `AccumulatedContext`, but this is only passed
forward (step N+1 sees step N's output). There is no shared writeable state that
parallel steps can contribute to during execution.

## How Team Mode Will Extend the System

Team Mode will add a third collaboration mode (`team_parallel`) alongside the
existing `pair_programming` and `differential` modes:

### CollaborationMode Extension

Team Mode will implement the same `CollaborationMode` interface:

```go
type ParallelTeamDriver struct {
    // Lead agent + specialist roster
    // Uses message bus for coordination:
    //   team.{sessionID}.status   -- shared task board
    //   team.{sessionID}.message  -- inter-agent communication
    //   team.{sessionID}.result   -- partial results aggregation
}
```

### Key Differences from Existing Modes

| Aspect | Pair Programming | Differential | Team Mode (planned) |
|--------|-----------------|--------------|-------------------|
| Agents | 2 | 2-3 | 1 lead + N members (up to 8) |
| Communication | Turn-based alternation | Sequential branches | Bus-based messaging |
| Coordination | Editor token | Post-hoc synthesis | Real-time task board |
| Scope | Single task | A/B comparison | Parallel specialist work |

### Bus Topics for Team Mode

- `team.{sessionID}.status` -- Shared task board (lead publishes assignments,
  members publish progress).
- `team.{sessionID}.message` -- Inter-agent communication (broadcast or
  targeted).
- `team.{sessionID}.result` -- Partial results aggregation (members submit
  outputs, lead synthesizes).

### Integration Point

In `internal/daemon/components.go`, team mode will be registered alongside
existing modes:

```go
collabEngine.RegisterMode("team_parallel", parallelTeamDriver)
```

The `CollaborationEngine.RegisterMode` method and `CollaborationMode` interface
are the extension points. No changes to the existing pair programming or
differential modes are required.

## Related Files

| File | Role |
|------|------|
| `internal/agent/orchestrator.go` | Strategic/tactical coordination, bus subscriptions |
| `internal/agent/strategic.go` | Task decomposition, step creation, pair session planning |
| `internal/agent/tactical.go` | Step scheduling, semaphore management, job enqueueing |
| `internal/agent/collaboration_engine.go` | Mode registration, session lifecycle |
| `internal/agent/collaboration.go` | Core types: CollaborationMode interface, session state |
| `internal/agent/collaboration_pair_driver.go` | Pair programming driver |
| `internal/agent/collaboration_diff_driver.go` | Differential A/B driver |
| `internal/agent/pair_orchestrator.go` | Bus-channel pairing (Option C) |
| `internal/agent/pair_manager.go` | Multi-round actor/reviewer loop |
| `internal/agent/intent.go` | Intent types including IntentPair, IntentCollaborate |
| `internal/bus/bus.go` | Channel-based pub/sub message bus |
| `internal/queue/dispatcher.go` | Job routing to workers |
| `internal/worker/worker.go` | Job execution by agent |
| `internal/daemon/components.go` | Daemon wiring, collaboration engine setup |
